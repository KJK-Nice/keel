import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelToken } from './keel.ts';

// #59: the native window's translucent backdrop is tinted by the window's
// appearance. On macOS the window takes Keel's theme (?appearance=native,
// internal/desktop/appearance.go), so the chrome stays translucent for
// every choice. Elsewhere the backdrop follows the OS, so a theme that
// differs from it drops translucency: data-vibrancy is unset and the page
// keeps the opaque browser rendering. e2e cannot see the native window;
// this checks the page's half. The browser's own prefers-color-scheme
// stands in for the OS appearance.

const open = (app: any, query: string) => app.open(`/?keel_token=${keelToken()}&name=e2e${query}`);

const state = (browser: any) =>
  browser.evaluate(`(() => {
    const root = document.documentElement;
    // The body's background alpha, whatever colour syntax it computes to.
    const ctx = document.createElement('canvas').getContext('2d');
    ctx.fillStyle = getComputedStyle(document.body).backgroundColor;
    ctx.fillRect(0, 0, 1, 1);
    return {
      osDark: !matchMedia('(prefers-color-scheme: light)').matches,
      theme: root.dataset.theme,
      vibrancy: root.dataset.vibrancy ?? null,
      // The opaque fallback paints the page; translucency leaves alpha < 1.
      opaque: ctx.getImageData(0, 0, 1, 1).data[3] === 255,
    };
  })()`) as Promise<{ osDark: boolean; theme: string; vibrancy: string | null; opaque: boolean }>;

const apply = (browser: any, choice: string) => browser.evaluate(`window.__keelApplyTheme(${JSON.stringify(choice)})`);

test('native window without an appearance setter drops translucency off the OS theme', async ({ app, browser }) => {
  await open(app, '&native=1');
  for (const choice of ['system', 'light', 'dark'] as const) {
    await apply(browser, choice);
    const s = await state(browser);
    const matchesOS = choice === 'system' || (choice === 'dark') === s.osDark;
    expect(s.theme).toBe(choice === 'system' ? (s.osDark ? 'dark' : 'light') : choice);
    expect(s.vibrancy).toBe(matchesOS ? '1' : null);
    expect(s.opaque).toBe(!matchesOS);
  }
});

test('native window that takes the theme keeps translucency for every choice', async ({ app, browser }) => {
  await open(app, '&native=1&appearance=native');
  for (const choice of ['light', 'dark', 'system'] as const) {
    await apply(browser, choice);
    const s = await state(browser);
    expect(s.vibrancy).toBe('1');
    expect(s.opaque).toBe(false);
  }
});

// #75: each area paints its tint once, straight over the backdrop. A tint
// on html and body under the chrome multiplied (66% over 66% over 66% is
// 96%), so the native window looked opaque. For every area, the area and
// its ancestors up to html together must leave a visible share of the
// backdrop, and none of them may be opaque. (The WebGL canvas is not in
// the DOM; xterm's clear background shows on .xterm-scrollable-element.)
const cover = (browser: any, selectors: string[], band: [number, number] = [0.5, 0.9]) =>
  browser.evaluate(`(() => {
    const ctx = document.createElement('canvas').getContext('2d', { willReadFrequently: true });
    const alpha = (c) => {
      ctx.clearRect(0, 0, 1, 1);
      ctx.fillStyle = 'rgba(0, 0, 0, 0)';
      ctx.fillStyle = c;
      ctx.fillRect(0, 0, 1, 1);
      return ctx.getImageData(0, 0, 1, 1).data[3] / 255;
    };
    const name = (e) => e.tagName.toLowerCase() + (typeof e.className === 'string' && e.className ? '.' + e.className.trim().split(/\\s+/).join('.') : '');
    const bad = [];
    for (const sel of ${JSON.stringify(selectors)}) {
      const el = [...document.querySelectorAll(sel)].find((e) => e.getClientRects().length && getComputedStyle(e).visibility === 'visible');
      if (!el) { bad.push(sel + ': missing'); continue; }
      let clear = 1;
      for (let e = el; e; e = e.parentElement) {
        const cs = getComputedStyle(e);
        const a = alpha(cs.backgroundColor) * Number(cs.opacity);
        if (a >= 1) bad.push(sel + ': opaque ' + name(e));
        clear *= 1 - a;
      }
      // One tint is a 42–86% token mix; a second one under it passes 90%.
      const cover = 1 - clear;
      if (cover > ${band[1]} || cover < ${band[0]}) bad.push(sel + ': covers ' + Math.round(cover * 100) + '% of the backdrop');
    }
    return bad;
  })()`) as Promise<string[]>;

test('native vibrancy tints each area once, so the backdrop shows through', async ({ app, browser, screen }) => {
  await open(app, '&native=1&appearance=native&chrome=titlebar');
  for (const choice of ['dark', 'light'] as const) {
    await apply(browser, choice);
    expect((await state(browser)).vibrancy).toBe('1');
    // The New tab the window opens with, under the chrome.
    expect(await cover(browser, ['.workspace-bar', '.k-sidebar', '.k-status', '.start-page'])).toEqual([]);
  }
  // A solo terminal pane: its bar, its body and xterm's own layers.
  const vault = screen.getByRole('complementary', 'Vault');
  await vault.getByRole('button', 'Hosts').tap();
  await vault.getByRole('button', /^Local Shell\b/).tap();
  await expect(screen.getByRole('textbox', 'Terminal input')).toBeFocused();
  expect(await cover(browser, ['.term-tab.is-active .pane-bar'])).toEqual([]);
  // The body has its own vibrancy-term-* step (light Medium is 90%); the
  // per-step and per-theme bands are checked below.
  expect(
    await cover(
      browser,
      ['.term-tab.is-active .term-body', '.term-tab.is-active .xterm-scrollable-element', '.term-tab.is-active .xterm-screen'],
      [0.5, 0.95],
    ),
  ).toEqual([]);
  await screen.getByRole('button', 'Close tab').tap();
});

// Settings · Appearance · Window translucency: each step tints the chrome
// and the stage with its vibrancy-tint-* token, once; Off is opaque.
const setStep = (browser: any, step: string) =>
  browser.evaluate(`(() => {
    const ui = JSON.parse(localStorage.getItem('keel-ui') || '{}');
    ui.translucency = ${JSON.stringify(step)};
    localStorage.setItem('keel-ui', JSON.stringify(ui));
  })()`);

const token = (browser: any, name: string) =>
  browser.evaluate(`parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--${name}')) / 100`) as Promise<number>;

test('each translucency step tints the window by its token; Off is opaque', async ({ app, browser }) => {
  for (const step of ['low', 'medium', 'high'] as const) {
    await open(app, '&native=1&appearance=native&chrome=titlebar');
    await setStep(browser, step);
    await open(app, '&native=1&appearance=native&chrome=titlebar');
    const s = await state(browser);
    expect(s.vibrancy).toBe('1');
    expect(s.opaque).toBe(false);
    const tint = await token(browser, `vibrancy-tint-${step}`);
    expect(tint).toBeGreaterThan(0);
    // The tab strip, status bar and New tab carry the step's tint, once.
    expect(await cover(browser, ['.workspace-bar', '.k-status', '.start-page'], [tint - 0.02, tint + 0.02])).toEqual([]);
  }
  await setStep(browser, 'off');
  await open(app, '&native=1&appearance=native&chrome=titlebar');
  for (const choice of ['dark', 'light'] as const) {
    await apply(browser, choice);
    const s = await state(browser);
    expect(s.vibrancy).toBe(null);
    expect(s.opaque).toBe(true);
  }
  await setStep(browser, 'medium');
});

test('a browser tab never sets vibrancy', async ({ app, browser }) => {
  await open(app, '&appearance=native');
  for (const choice of ['light', 'dark', 'system'] as const) {
    await apply(browser, choice);
    expect((await state(browser)).vibrancy).toBe(null);
  }
});

// Every visible match of each selector: how much of the backdrop the match
// and its ancestors up to html cover, and whether any of them is opaque.
const covers = (browser: any, selector: string) =>
  browser.evaluate(`(() => {
    const ctx = document.createElement('canvas').getContext('2d', { willReadFrequently: true });
    const alpha = (c) => {
      ctx.clearRect(0, 0, 1, 1);
      ctx.fillStyle = 'rgba(0, 0, 0, 0)';
      ctx.fillStyle = c;
      ctx.fillRect(0, 0, 1, 1);
      return ctx.getImageData(0, 0, 1, 1).data[3] / 255;
    };
    return [...document.querySelectorAll(${JSON.stringify(selector)})]
      .filter((e) => e.getClientRects().length && getComputedStyle(e).visibility === 'visible')
      .map((el) => {
        let clear = 1, opaque = false, by = '';
        for (let e = el; e; e = e.parentElement) {
          const cs = getComputedStyle(e);
          const a = alpha(cs.backgroundColor) * Number(cs.opacity);
          if (a >= 1) { opaque = true; by += ' ' + e.tagName.toLowerCase() + '.' + String(e.className).trim().split(/\\s+/).join('.'); }
          clear *= 1 - a;
        }
        return { cover: Math.round((1 - clear) * 1000) / 1000, opaque, by };
      });
  })()`) as Promise<{ cover: number; opaque: boolean; by: string }[]>;

// The split gutters: split panes are tiles with a space-1.5 gap and no
// hairline. The sash is the gap's drag handle and paints nothing; the chrome
// ground under it is .tile-ground (one element, clipped by term.js to the
// stage minus each tile's rounded rect), so the tiles read as separate panes
// and nothing is tinted under a tile.
const sashes = (browser: any) =>
  browser.evaluate(`(() => {
    const gap = parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--space-1.5'));
    return [...document.querySelectorAll('.term-tab.is-active .split-sash')].map((s) => {
      const r = s.getBoundingClientRect();
      const bg = getComputedStyle(s).backgroundColor;
      return { size: Math.min(r.width, r.height), gap, clear: /^(transparent|rgba\\(0, 0, 0, 0\\))$/.test(bg) };
    });
  })()`) as Promise<{ size: number; gap: number; clear: boolean }[]>;

// Hit-tests the active tab's .tile-ground (pointer events on for the probe;
// clip-path limits hit testing to what it paints) at a tile's corner wedge
// (1px in from each corner of its box, outside the rounded corner), at each
// outer corner of the stage, at every gap crossing (2x2), in each gap's
// middle, and at each tile's centre. The ground must be hit everywhere but
// the tile centres.
const groundProbe = (browser: any) =>
  browser.evaluate(`(() => {
    const stage = document.querySelector('.term-tab.is-active .term-stage');
    const ground = stage && stage.querySelector(':scope > .tile-ground');
    if (!ground) return { ground: false };
    const tiles = [...stage.querySelectorAll(':scope > .term-wrap')]
      .filter((w) => w.getClientRects().length && getComputedStyle(w).visibility === 'visible')
      .map((w) => w.getBoundingClientRect());
    const s = stage.getBoundingClientRect();
    const gap = parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--space-1.5'));
    const hit = (x, y) => document.elementsFromPoint(x, y).includes(ground);
    ground.style.pointerEvents = 'auto';
    try {
      const corners = tiles.flatMap((t) => [[t.left + 1, t.top + 1], [t.right - 1, t.top + 1], [t.left + 1, t.bottom - 1], [t.right - 1, t.bottom - 1]]);
      const outer = [[s.left + 1, s.top + 1], [s.right - 1, s.top + 1], [s.left + 1, s.bottom - 1], [s.right - 1, s.bottom - 1]];
      // A crossing sits gap/2 past a tile's bottom-right corner when a tile
      // lies both to its right and below it.
      const crossings = tiles
        .filter((t) => tiles.some((o) => Math.abs(o.left - (t.right + gap)) < 1) && tiles.some((o) => Math.abs(o.top - (t.bottom + gap)) < 1))
        .map((t) => [t.right + gap / 2, t.bottom + gap / 2]);
      const gaps = [...stage.querySelectorAll(':scope > .split-sash')].map((el) => {
        const r = el.getBoundingClientRect();
        return [r.left + r.width / 2, r.top + r.height / 2];
      });
      const centres = tiles.map((t) => [t.left + t.width / 2, t.top + t.height / 2]);
      return {
        ground: true,
        tiles: tiles.length,
        corners: corners.filter(([x, y]) => !hit(x, y)).length,
        outer: outer.filter(([x, y]) => !hit(x, y)).length,
        crossings: crossings.length,
        crossingsMissed: crossings.filter(([x, y]) => !hit(x, y)).length,
        gaps: gaps.length,
        gapsMissed: gaps.filter(([x, y]) => !hit(x, y)).length,
        underTiles: centres.filter(([x, y]) => hit(x, y)).length,
      };
    } finally {
      ground.style.pointerEvents = '';
    }
  })()`) as Promise<{ ground: boolean; tiles?: number; corners?: number; outer?: number; crossings?: number; crossingsMissed?: number; gaps?: number; gapsMissed?: number; underTiles?: number }>;

// Live: the inline theme script reads data-translucency, as a reload would.
const live = (browser: any, choice: string, step: string) =>
  browser.evaluate(`(() => {
    document.documentElement.dataset.translucency = ${JSON.stringify(step)};
    window.__keelApplyTheme(${JSON.stringify(choice)});
  })()`);

const PANE_LAYERS = ['.term-body', '.xterm-scrollable-element', '.xterm-screen'];

// Which xterm renderer each visible pane of the active tab runs: every
// pane, solo or split, draws with WebGL in the native window too (#80).
// The WebGL renderer adds its link-layer canvas; the DOM one has rows.
const renderers = (browser: any) =>
  browser.evaluate(`[...document.querySelectorAll('.term-tab.is-active .term-wrap')]
    .filter((w) => w.getClientRects().length && getComputedStyle(w).visibility === 'visible')
    .map((w) => (w.querySelector('.xterm-screen canvas.xterm-link-layer') ? 'webgl' : w.querySelector('.xterm-rows') ? 'dom' : 'none'))`) as Promise<string[]>;

// xterm's canvases (the WebGL one and the link layer above it) must stay
// clear: a background on the upper one hid every glyph of a native split
// pane (#80).
const canvasBackgrounds = (browser: any) =>
  browser.evaluate(`[...document.querySelectorAll('.term-tab.is-active .xterm-screen canvas')]
    .map((c) => getComputedStyle(c).backgroundColor)
    .filter((bg) => !/^(transparent|rgba\\(0, 0, 0, 0\\))$/.test(bg))`) as Promise<string[]>;

// Every pane's body, solo or split, carries the step's vibrancy-term-*
// share of --term-bg once (denser in light, where the blurred desktop
// otherwise shows as dark shapes); xterm's own layers add nothing.
async function expectPanesAt(browser: any, panes: number, theme: string, step: string) {
  await live(browser, theme, step);
  expect((await state(browser)).vibrancy).toBe('1');
  const want = await token(browser, `vibrancy-term-${step}`);
  for (const layer of PANE_LAYERS) {
    const got = await covers(browser, `.term-tab.is-active ${layer}`);
    expect(got.length, `${theme} ${step} ${layer} count`).toBe(panes);
    for (const c of got) {
      expect(c.opaque, `${theme} ${step} ${layer} opaque by${c.by}`).toBe(false);
      expect(Math.abs(c.cover - want), `${theme} ${step} ${layer} covers ${c.cover}, want ${want}`).toBeLessThan(0.02);
    }
  }
  // The pane bars stay chrome-tinted, the focused one on surface.
  const bars = await covers(browser, '.term-tab.is-active .pane-bar');
  expect(bars.length).toBe(panes);
  for (const c of bars) expect(c.opaque, `${theme} ${step} pane bar opaque`).toBe(false);
  expect(await browser.evaluate(`document.querySelectorAll('.term-tab.is-active .term-wrap.is-focused .pane-bar').length`)).toBe(1);
}

test('solo and split panes are translucent at every step; light tints the terminal denser', async ({ app, browser, screen }) => {
  await open(app, '&native=1&appearance=native&chrome=titlebar');
  await setStep(browser, 'medium');
  await open(app, '&native=1&appearance=native&chrome=titlebar');
  for (const step of ['low', 'medium', 'high'] as const) {
    await live(browser, 'dark', step);
    const dark = await token(browser, `vibrancy-term-${step}`);
    expect(dark).toBeCloseTo(await token(browser, `vibrancy-tint-${step}`), 3);
    await live(browser, 'light', step);
    expect(await token(browser, `vibrancy-term-${step}`)).toBeGreaterThan(dark);
  }

  const vault = screen.getByRole('complementary', 'Vault');
  await vault.getByRole('button', 'Hosts').tap();
  await vault.getByRole('button', /^Local Shell\b/).tap();
  const terms = screen.getByRole('textbox', 'Terminal input');
  await expect(terms.first()).toBeFocused();
  for (const theme of ['dark', 'light'] as const) {
    for (const step of ['low', 'medium', 'high'] as const) await expectPanesAt(browser, 1, theme, step);
  }

  // Split: both panes keep WebGL and the clear theme.
  await terms.first().press('Meta+Shift+D');
  await expect(terms).toHaveCount(2);
  await expect.poll(() => renderers(browser)).toEqual(['webgl', 'webgl']);
  expect(await canvasBackgrounds(browser)).toEqual([]);
  for (const layer of ['.xterm-viewport']) {
    for (const c of await covers(browser, `.term-tab.is-active ${layer}`)) expect(c.opaque, `split ${layer} opaque by${c.by}`).toBe(false);
  }
  for (const theme of ['dark', 'light'] as const) {
    for (const step of ['low', 'medium', 'high'] as const) {
      await expectPanesAt(browser, 2, theme, step);
      const gutters = await sashes(browser);
      expect(gutters.length).toBe(1);
      expect(gutters[0].size).toBe(gutters[0].gap);
      expect(gutters[0].clear).toBe(true);
      const g = await groundProbe(browser);
      expect(g, `${theme} ${step} tile ground`).toEqual({ ground: true, tiles: 2, corners: 0, outer: 0, crossings: 0, crossingsMissed: 0, gaps: 1, gapsMissed: 0, underTiles: 0 });
      const tint = await token(browser, `vibrancy-tint-${step}`);
      for (const c of await covers(browser, '.term-tab.is-active .tile-ground')) {
        expect(c.opaque, `${theme} ${step} ground opaque by${c.by}`).toBe(false);
        expect(Math.abs(c.cover - tint), `${theme} ${step} ground covers ${c.cover}, want ${tint}`).toBeLessThan(0.02);
      }
    }
  }

  // Back to one pane: still WebGL, still clear; then split once more.
  await screen.getByRole('button', 'Close pane', { visible: true }).nth(1).tap();
  await expect(terms).toHaveCount(1);
  await expect.poll(() => renderers(browser)).toEqual(['webgl']);
  await expectPanesAt(browser, 1, 'dark', 'medium');
  await terms.first().press('Meta+Shift+D');
  await expect(terms).toHaveCount(2);
  await expect.poll(() => renderers(browser)).toEqual(['webgl', 'webgl']);
  expect(await canvasBackgrounds(browser)).toEqual([]);
  await expectPanesAt(browser, 2, 'light', 'high');

  // Text over a loud stand-in backdrop (the page has no OS backdrop): the
  // glyphs must stay crisp on the tinted, clear-backed panes.
  for (const t of [0, 1]) {
    await terms.nth(t).pressSequentially(`printf 'pane ${t + 1}  user@host:~$ ls -la  \\342\\226\\210\\342\\226\\210 0123456789\\n'`);
    await terms.nth(t).press('Enter');
  }
  // Each theme through a reload (keel-theme, as Settings stores it), so
  // xterm takes the theme's colours; the split comes back restored, on
  // WebGL and clear like a new pane.
  for (const theme of ['dark', 'light'] as const) {
    await browser.evaluate(`localStorage.setItem('keel-theme', ${JSON.stringify(theme)})`);
    await open(app, '&native=1&appearance=native&chrome=titlebar');
    await expect(terms).toHaveCount(2);
    await expect.poll(() => renderers(browser)).toEqual(['webgl', 'webgl']);
    expect(await canvasBackgrounds(browser)).toEqual([]);
    await expectPanesAt(browser, 2, theme, 'medium');
    await browser.evaluate(`(() => {
      const s = document.createElement('style');
      s.id = 'e2e-backdrop';
      s.textContent = 'html[data-vibrancy]{background:repeating-linear-gradient(35deg,#ff2d6f 0 60px,#1fd1ff 60px 120px,#ffe14d 120px 180px,#111 180px 240px) !important}';
      document.head.appendChild(s);
    })()`);
    await app.screenshot(`vibrancy-split-backdrop-${theme}`);
    await browser.evaluate(`document.getElementById('e2e-backdrop')?.remove()`);
  }
  await browser.evaluate(`localStorage.removeItem('keel-theme')`);

  // Off: every pane is opaque again, in both themes.
  await setStep(browser, 'off');
  await open(app, '&native=1&appearance=native&chrome=titlebar');
  await expect(terms).toHaveCount(2);
  for (const theme of ['dark', 'light'] as const) {
    await apply(browser, theme);
    expect((await state(browser)).vibrancy).toBe(null);
    for (const layer of PANE_LAYERS) {
      const got = await covers(browser, `.term-tab.is-active ${layer}`);
      expect(got.length).toBe(2);
      for (const c of got) expect(c.opaque, `off ${theme} ${layer}`).toBe(true);
    }
  }
  await setStep(browser, 'medium');
  await screen.getByRole('button', 'Close pane', { visible: true }).first().tap();
  await expect(terms).toHaveCount(1);
  await screen.getByRole('button', 'Close tab', { visible: true }).tap();
  await expect(terms).toBeHidden();
});

// A 2x2 split: the ground fills every gap, the crossing in the middle, the
// stage's outer corners and the wedge outside each tile's rounded corners,
// and never lies under a tile. All four panes keep WebGL and clear canvases.
test('a 2x2 split tints its gaps, crossing and tile corners, never under a tile', async ({ app, browser, screen }) => {
  await open(app, '&native=1&appearance=native&chrome=titlebar');
  await setStep(browser, 'medium');
  await open(app, '&native=1&appearance=native&chrome=titlebar');
  await live(browser, 'dark', 'medium');
  const vault = screen.getByRole('complementary', 'Vault');
  await vault.getByRole('button', 'Hosts').tap();
  await vault.getByRole('button', /^Local Shell\b/).tap();
  const terms = screen.getByRole('textbox', 'Terminal input');
  await expect(terms.first()).toBeFocused();
  await screen.getByRole('button', 'Split right', { visible: true }).first().tap();
  await expect(terms).toHaveCount(2);
  await screen.getByRole('button', 'Split down', { visible: true }).nth(1).tap();
  await expect(terms).toHaveCount(3);
  await screen.getByRole('button', 'Split down', { visible: true }).nth(0).tap();
  await expect(terms).toHaveCount(4);
  await expect.poll(() => renderers(browser)).toEqual(['webgl', 'webgl', 'webgl', 'webgl']);
  expect(await canvasBackgrounds(browser)).toEqual([]);
  for (const theme of ['dark', 'light'] as const) {
    await live(browser, theme, 'medium');
    const g = await groundProbe(browser);
    expect(g, `${theme} 2x2 tile ground`).toEqual({ ground: true, tiles: 4, corners: 0, outer: 0, crossings: 1, crossingsMissed: 0, gaps: 3, gapsMissed: 0, underTiles: 0 });
    await expectPanesAt(browser, 4, theme, 'medium');
  }
  for (let n = 4; n > 1; n--) {
    await screen.getByRole('button', 'Close pane', { visible: true }).first().tap();
    await expect(terms).toHaveCount(n - 1);
  }
  // One pane: full-bleed, no ground.
  expect((await groundProbe(browser)).ground).toBe(false);
  await screen.getByRole('button', 'Close tab', { visible: true }).tap();
  await expect(terms).toBeHidden();
});

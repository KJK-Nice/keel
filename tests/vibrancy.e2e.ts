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
const cover = (browser: any, selectors: string[]) =>
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
      // One tint is a 58–86% token mix; a second one under it passes 90%.
      const cover = 1 - clear;
      if (cover > 0.9 || cover < 0.5) bad.push(sel + ': covers ' + Math.round(cover * 100) + '% of the backdrop');
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
  expect(
    await cover(browser, [
      '.term-tab.is-active .pane-bar',
      '.term-tab.is-active .term-body',
      '.term-tab.is-active .xterm-scrollable-element',
      '.term-tab.is-active .xterm-screen',
    ]),
  ).toEqual([]);
  await screen.getByRole('button', 'Close tab').tap();
});

test('a browser tab never sets vibrancy', async ({ app, browser }) => {
  await open(app, '&appearance=native');
  for (const choice of ['light', 'dark', 'system'] as const) {
    await apply(browser, choice);
    expect((await state(browser)).vibrancy).toBe(null);
  }
});

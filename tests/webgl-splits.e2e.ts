import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelApi, keelToken } from './keel.ts';

// #80: every visible terminal pane, solo or split, draws with xterm's WebGL
// renderer, in the native window (native=1) as in a browser tab. Panes off
// screen (behind a zoomed pane, in another tab) give their context back, so
// the page holds one live WebGL context per visible pane and never leaks
// one through split, zoom, close and tab-switch cycles.

type Session = { id: string };

const open = (app: any, query: string) => app.open(`/?keel_token=${keelToken()}&name=e2e${query}`);

// Counts every WebGL context the page creates from here on, and how many
// are still live (not lost: dropWebgl loses a context on purpose).
const trackContexts = (browser: any) =>
  browser.evaluate(`(() => {
    if (window.__glTrack) return;
    const made = [];
    window.__glTrack = made;
    const orig = HTMLCanvasElement.prototype.getContext;
    HTMLCanvasElement.prototype.getContext = function (kind, ...rest) {
      const ctx = orig.call(this, kind, ...rest);
      if (ctx && /^(webgl|webgl2|experimental-webgl)$/.test(kind) && !made.includes(ctx)) made.push(ctx);
      return ctx;
    };
  })()`);

type Snap = { visible: number; webgl: number; dom: number; glCanvases: number; live: number; made: number };

const snapshot = (browser: any) =>
  browser.evaluate(`(() => {
    const shown = [...document.querySelectorAll('.term-tab.is-active .term-wrap')]
      .filter((w) => w.getClientRects().length && getComputedStyle(w).visibility === 'visible' && !w.classList.contains('is-parked'));
    const webgl = shown.filter((w) => w.querySelector('.xterm-screen canvas.xterm-link-layer')).length;
    return {
      visible: shown.length,
      webgl,
      dom: shown.length - webgl,
      // The WebGL renderer's own canvas has no class; count them page-wide.
      glCanvases: document.querySelectorAll('.term-wrap .xterm-screen canvas:not([class])').length,
      live: window.__glTrack.filter((gl) => !gl.isContextLost()).length,
      made: window.__glTrack.length,
    };
  })()`) as Promise<Snap>;

// One live WebGL context and one WebGL canvas per visible pane, nothing
// on the DOM renderer.
async function expectOnePerVisible(browser: any, visible: number, label: string) {
  await expect.poll(async () => {
    const s = await snapshot(browser);
    return [s.visible, s.webgl, s.dom, s.glCanvases, s.live].join(',');
  }, { message: label }).toBe([visible, visible, 0, visible, visible].join(','));
}

for (const mode of ['native', 'browser'] as const) {
  test(`${mode}: split, zoom, close and tab cycles keep one WebGL context per visible pane`, { tags: ['terminal'] }, async ({ app, browser, screen }) => {
    await open(app, mode === 'native' ? '&native=1&appearance=native&chrome=titlebar' : '');
    await trackContexts(browser);
    const before = await keelApi<Session[]>(app.baseUrl, '/api/sessions');

    const vault = screen.getByRole('complementary', 'Vault');
    await vault.getByRole('button', 'Hosts').tap();
    await vault.getByRole('button', /^Local Shell\b/).tap();
    const terms = screen.getByRole('textbox', 'Terminal input');
    await expect(terms.first()).toBeFocused();
    await expectOnePerVisible(browser, 1, 'solo');

    // Split right, then split the new pane down: three panes, three contexts.
    await terms.first().press('Meta+Shift+D');
    await expect(terms).toHaveCount(2);
    await expectOnePerVisible(browser, 2, 'split right');
    await terms.nth(1).press('Meta+Shift+E');
    await expect(terms).toHaveCount(3);
    await expectOnePerVisible(browser, 3, 'split down');

    // Each split pane takes typing and runs it in its own PTY.
    let sessions: Session[] = [];
    await expect.poll(async () => (sessions = await keelApi<Session[]>(app.baseUrl, '/api/sessions')).length).toBe(before.length + 3);
    const mine = sessions.filter((s) => !before.some((b) => b.id === s.id));
    for (let i = 0; i < 3; i++) {
      await terms.nth(i).pressSequentially(`echo webgl-pane-$((${i}+40))`);
      await terms.nth(i).press('Enter');
    }
    const text = async () =>
      (await Promise.all(mine.map((s) => keelApi<{ text: string }>(app.baseUrl, `/api/sessions/${s.id}/scrollback`)))).map((r) => r.text).join('\n');
    for (const want of ['webgl-pane-40', 'webgl-pane-41', 'webgl-pane-42']) {
      await expect.poll(text, { timeout: 10_000 }).toContain(want);
    }
    await app.screenshot(`webgl-split-${mode}`);

    // Zoom one pane and back, four times: hidden panes give their context
    // back and get a fresh one when shown; none stays live behind.
    for (let i = 0; i < 4; i++) {
      await screen.getByRole('button', 'Focus view', { visible: true }).nth(i % 3).tap();
      await expectOnePerVisible(browser, 1, `zoom ${i}`);
      await screen.getByRole('button', 'Exit focus view', { visible: true }).tap();
      await expectOnePerVisible(browser, 3, `unzoom ${i}`);
    }

    // Another tab: the split tab's panes are off screen and hold nothing.
    await screen.getByRole('button', 'New tab').first().tap();
    await expect.poll(async () => (await snapshot(browser)).live).toBe(0);
    await screen.getByRole('button', /^Local Shell\b/).first().tap();
    await expectOnePerVisible(browser, 3, 'back to the split tab');

    // Close panes down to one: still WebGL, and no context outlives its pane.
    await screen.getByRole('button', 'Close pane', { visible: true }).nth(2).tap();
    await expect(terms).toHaveCount(2);
    await expectOnePerVisible(browser, 2, 'closed one');
    await screen.getByRole('button', 'Close pane', { visible: true }).nth(1).tap();
    await expect(terms).toHaveCount(1);
    await expectOnePerVisible(browser, 1, 'solo again');
    const end = await snapshot(browser);
    expect(end.made, 'contexts were made and freed through the cycles').toBeGreaterThan(3);

    await screen.getByRole('button', 'Close tab').first().tap();
    await expect.poll(async () => (await snapshot(browser)).live).toBe(0);
  });
}

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

test('a browser tab never sets vibrancy', async ({ app, browser }) => {
  await open(app, '&appearance=native');
  for (const choice of ['light', 'dark', 'system'] as const) {
    await apply(browser, choice);
    expect((await state(browser)).vibrancy).toBe(null);
  }
});

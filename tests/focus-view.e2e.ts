import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelToken } from './keel.ts';

// Focus view (⌘⇧↵ on macOS, Ctrl+Shift+Enter elsewhere) toggles: the same
// chord zooms the focused pane and brings the split back, and so do the
// pane bar's button and the palette's Focus view.
//
// In the native window the View menu carries the same accelerator. When
// the page runs the chord, WKWebView may still hand it to the menu, whose
// item runs the action again through window.__keelShortcut; that call is
// queued behind the page's own work. Leaving focus view attaches a WebGL
// context per pane again, which in WKWebView can take longer than the
// page's 300 ms duplicate window, so the menu's copy used to arrive late
// and zoom the pane straight back in. The test plays the menu's part: a
// keydown listener queues window.__keelShortcut('focus-pane') right after
// the page's handler, and WebGL contexts are made slow to create.

const native = '&native=1&appearance=native&chrome=titlebar';

const menuEcho = `(() => {
  if (window.__fvEcho) return;
  window.__fvEcho = true;
  window.addEventListener('keydown', (ev) => {
    if (ev.key === 'Enter' && ev.shiftKey && (ev.metaKey || ev.ctrlKey)) {
      setTimeout(() => window.__keelShortcut && window.__keelShortcut('focus-pane'), 0);
    }
  }, true);
  const get = HTMLCanvasElement.prototype.getContext;
  const made = new WeakSet();
  HTMLCanvasElement.prototype.getContext = function (kind, ...rest) {
    if (/webgl/.test(String(kind)) && !made.has(this)) {
      made.add(this);
      const until = Date.now() + 400;
      while (Date.now() < until) { /* WKWebView-slow context */ }
    }
    return get.call(this, kind, ...rest);
  };
})()`;

const zoomed = `document.querySelectorAll('.term-wrap.is-zoomed').length`;

for (const mode of ['browser', 'native'] as const) {
  test(`${mode}: the focus view chord toggles the split`, { tags: ['terminal'] }, async ({ app, browser, screen }) => {
    await app.open(`/?keel_token=${keelToken()}&name=e2e${mode === 'native' ? native : ''}`);
    const vault = screen.getByRole('complementary', 'Vault');
    const terms = screen.getByRole('textbox', 'Terminal input');
    await vault.getByRole('button', 'Hosts').tap();
    await vault.getByRole('button', /^Local Shell\b/).tap();
    await expect(terms).toHaveCount(1);
    await terms.first().press('Meta+Shift+D');
    await expect(terms).toHaveCount(2);
    if (mode === 'native') await browser.evaluate(menuEcho);

    // The left pane has the keyboard.
    const box = await terms.nth(0).boundingBox();
    if (!box) throw new Error('first terminal has no box');
    await screen.tapAt({ x: box.x + box.width / 2, y: box.y + box.height / 2 });
    await expect(terms.nth(0)).toBeFocused();

    const exit = screen.getByRole('button', 'Exit focus view', { visible: true });
    for (let i = 0; i < 2; i++) {
      await browser.keyboard.press('Meta+Shift+Enter');
      await expect(exit).toHaveCount(1);
      await expect.poll(() => browser.evaluate(zoomed)).toBe(1);
      // Past the duplicate window: whatever the menu echoed has landed.
      await new Promise((r) => setTimeout(r, 1000));
      await expect.poll(() => browser.evaluate(zoomed)).toBe(1);

      await browser.keyboard.press('Meta+Shift+Enter');
      await expect(exit).toHaveCount(0);
      await new Promise((r) => setTimeout(r, 1000));
      await expect.poll(() => browser.evaluate(zoomed)).toBe(0);
      await expect(terms).toHaveCount(2);
    }

    // The pane bar button and the palette run the same toggle.
    await screen.getByRole('button', 'Focus view', { visible: true }).first().tap();
    await expect(exit).toHaveCount(1);
    await exit.tap();
    await expect(exit).toHaveCount(0);
    await browser.keyboard.press('Meta+k');
    const palette = screen.getByRole('combobox').first();
    await palette.pressSequentially('Focus view');
    await palette.press('Enter');
    await expect(exit).toHaveCount(1);
    await new Promise((r) => setTimeout(r, 600));
    await browser.keyboard.press('Meta+Shift+Enter');
    await expect(exit).toHaveCount(0);

    // Leave no tab behind: the workspace is restored on the next load.
    await terms.nth(1).press('Meta+w');
    await expect(terms).toHaveCount(1);
    await screen.getByRole('button', 'Close tab', { visible: true }).first().tap();
    await expect(terms).toHaveCount(0);
  });
}

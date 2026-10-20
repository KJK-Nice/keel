import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelToken } from './keel.ts';

// Focus view (⌘⇧↵ on macOS, Ctrl+Shift+Enter elsewhere) toggles: one press
// zooms the focused pane, the next brings the split back. A held chord
// toggles once: its auto-repeats run nothing (lib/shortcuts.js REPEATS).
// Leaving focus view re-attaches a WebGL context per pane, which is slow in
// WKWebView; the test makes contexts slow to create, so a repeat that ran
// would land well after the first press and zoom straight back in.
//
// The native menu carries the same accelerator. WKWebView should never
// hand it a chord the page preventDefaulted, but if it did, the echo
// (window.__keelShortcut with no input since the press) is dropped
// (lib/chordguard.js), while a menu call after a keyup still runs.

const native = '&native=1&appearance=native&chrome=titlebar';

const slowWebgl = `(() => {
  if (window.__slowGl) return;
  window.__slowGl = true;
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

// The e2e keyboard only presses whole keys, so a held chord is played as
// the keydowns a real one sends to the focused element: ⌘, ⇧, ↵, then ↵
// again with repeat set, then the keyups. One evaluate per event, so each
// lands after the page finished the one before, as a real repeat would.
const key = (type: 'keydown' | 'keyup', k: string, repeat = false) => `(() => {
  const code = { Meta: 'MetaLeft', Shift: 'ShiftLeft', Enter: 'Enter' }['${k}'];
  const down = '${type}' === 'keydown';
  const ev = new KeyboardEvent('${type}', {
    key: '${k}', code, repeat: ${repeat}, bubbles: true, cancelable: true,
    metaKey: '${k}' === 'Meta' ? down : true, shiftKey: '${k}' === 'Shift' ? down : '${k}' !== 'Meta',
  });
  (document.activeElement || document.body).dispatchEvent(ev);
})()`;
const holdDown = async (browser: any) => {
  await browser.evaluate(key('keydown', 'Meta'));
  await browser.evaluate(key('keydown', 'Shift'));
  await browser.evaluate(key('keydown', 'Enter'));
};
const holdUp = async (browser: any) => {
  await browser.evaluate(key('keyup', 'Enter'));
  await browser.evaluate(key('keyup', 'Shift'));
  await browser.evaluate(key('keyup', 'Meta'));
};
const keyLog = `JSON.stringify(window.__keelKeyLog || [])`;

for (const mode of ['browser', 'native'] as const) {
  test(`${mode}: focus view toggles per press, once per held key, and drops a menu echo`, { tags: ['terminal'] }, async ({ app, browser, screen }) => {
    await app.open(`/?keel_token=${keelToken()}&name=e2e${mode === 'native' ? native : ''}`);
    const vault = screen.getByRole('complementary', 'Vault');
    const terms = screen.getByRole('textbox', 'Terminal input');
    await vault.getByRole('button', 'Hosts').tap();
    await vault.getByRole('button', /^Local Shell\b/).tap();
    await expect(terms).toHaveCount(1);
    await terms.first().press('Meta+Shift+D');
    await expect(terms).toHaveCount(2);
    await browser.evaluate(slowWebgl);

    // The left pane has the keyboard.
    const box = await terms.nth(0).boundingBox();
    if (!box) throw new Error('first terminal has no box');
    await screen.tapAt({ x: box.x + box.width / 2, y: box.y + box.height / 2 });
    await expect(terms.nth(0)).toBeFocused();
    const exit = screen.getByRole('button', 'Exit focus view', { visible: true });
    const settled = async (want: number) => {
      await expect.poll(() => browser.evaluate(zoomed)).toBe(want);
      // Past any late repeat or echo: the state holds.
      await new Promise((r) => setTimeout(r, 800));
      expect(await browser.evaluate(zoomed)).toBe(want);
    };

    // Tap twice: in, then out, then in and out again, quickly.
    await browser.keyboard.press('Meta+Shift+Enter');
    await settled(1);
    await browser.keyboard.press('Meta+Shift+Enter');
    await settled(0);
    await browser.keyboard.press('Meta+Shift+Enter');
    await browser.keyboard.press('Meta+Shift+Enter');
    await settled(0);
    await expect(terms).toHaveCount(2);

    // Hold it: the first keydown toggles, the repeats run nothing.
    for (const want of [1, 0]) {
      await holdDown(browser);
      for (let i = 0; i < 5; i++) await browser.evaluate(key('keydown', 'Enter', true));
      await holdUp(browser);
      await settled(want);
    }
    const log = JSON.parse(await browser.evaluate(keyLog)) as string[];
    expect(log.filter((l) => l.includes('keyboard focus-pane repeat=true repeat skipped')).length).toBeGreaterThanOrEqual(10);

    // A menu echo of the press, before any other input, is dropped.
    await holdDown(browser);
    await browser.evaluate(`window.__keelShortcut('focus-pane')`);
    await settled(1);
    await holdUp(browser);
    expect(await browser.evaluate(`(window.__keelKeyLog || []).slice(-1)[0]`)).toContain('menu focus-pane repeat=false echo dropped');
    // After the keys are up, the menu item is the user's: it runs.
    await browser.evaluate(`window.__keelShortcut('focus-pane')`);
    await settled(0);
    await expect(exit).toHaveCount(0);

    // The pane bar button and the palette run the same toggle.
    await screen.getByRole('button', 'Focus view', { visible: true }).first().tap();
    await expect(exit).toHaveCount(1);
    await exit.tap();
    await expect(exit).toHaveCount(0);
    await browser.keyboard.press('Meta+k');
    const palette = screen.getByRole('combobox', 'Run a command or connect a host…');
    await palette.pressSequentially('Focus view');
    await palette.press('Enter');
    await expect(exit).toHaveCount(1);
    await browser.keyboard.press('Meta+Shift+Enter');
    await expect(exit).toHaveCount(0);

    // Leave no tab behind: the workspace is restored on the next load.
    await terms.nth(1).press('Meta+w');
    await expect(terms).toHaveCount(1);
    await screen.getByRole('button', 'Close tab', { visible: true }).first().tap();
    await expect(terms).toHaveCount(0);
  });
}

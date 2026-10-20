import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { openKeel } from './keel.ts';

// The native window is a WKWebView, which capitalizes and autocorrects text
// fields. Every text field in the live page, from templ, the Datastar
// stream or term.js, turns autocapitalize, autocorrect and spellcheck off;
// prose (agent composers, notes, the notebook body) keeps spellcheck.
// xterm's own hidden textarea is xterm's business and is skipped.

const missing = `(() => {
  const nonText = new Set(['hidden','checkbox','radio','file','range','color','submit','button','reset','image','number','date','time']);
  const prose = (el) => el.matches('#agent-input, #agentpage-input, [data-notes-input], [data-note-text], [data-note-edit], [data-notebook-editor]');
  const out = [];
  let n = 0;
  for (const el of document.querySelectorAll('input, textarea, [data-notebook-editor]')) {
    if (el.classList.contains('xterm-helper-textarea')) continue;
    if (el.tagName === 'INPUT' && nonText.has((el.getAttribute('type') || '').toLowerCase())) continue;
    n++;
    const want = prose(el) ? 'true' : 'false';
    if (el.getAttribute('autocapitalize') !== 'off' || el.getAttribute('autocorrect') !== 'off' || el.getAttribute('spellcheck') !== want) {
      out.push(el.outerHTML.slice(0, 160));
    }
  }
  return JSON.stringify({ n, out });
})()`;

test('text fields turn off autocapitalize, autocorrect and spellcheck', { tags: ['sidebar'] }, async ({ app, browser, screen }) => {
  await openKeel(app);
  const vault = screen.getByRole('complementary', 'Vault');
  await vault.getByRole('button', 'Hosts').tap();
  // The New tab's host field, the terminal's find bar, a renamed tab and the
  // palette are built in JS.
  await vault.getByRole('button', /^Local Shell\b/).tap();
  const terms = screen.getByRole('textbox', 'Terminal input');
  await expect(terms).toHaveCount(1);
  await terms.first().press('Meta+f');
  await expect(screen.getByRole('textbox', 'Find in terminal')).toBeVisible();
  await screen.getByRole('textbox', 'Find in terminal').press('Escape');
  await browser.keyboard.press('Meta+k');
  await expect(screen.getByRole('dialog', 'Command palette')).toBeVisible();

  const got = JSON.parse(await browser.evaluate(missing)) as { n: number; out: string[] };
  expect(got.out).toEqual([]);
  expect(got.n).toBeGreaterThan(20);

  // Leave no tab behind: the workspace is restored on the next load.
  await browser.keyboard.press('Escape');
  await expect(screen.getByRole('dialog', 'Command palette')).toBeHidden();
  await screen.getByRole('button', 'Close tab', { visible: true }).first().tap();
  await expect(terms).toHaveCount(0);
});

import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelApi, keelSend, openKeel } from './keel.ts';

// OSC 133 command marks (docs/ui-redesign.md §14). The test shell is
// /bin/sh with no integration, so the command prints the marks itself: one
// command that exits 0, one that exits 3, then an empty prompt (A, D with
// no C) that must draw nothing.
const MARKS =
  "printf '\\033]133;A\\007$ \\033]133;B\\007true\\n\\033]133;C\\007ok-out\\n\\033]133;D;0\\007" +
  "\\033]133;A\\007$ \\033]133;B\\007false\\n\\033]133;C\\007bad-out\\n\\033]133;D;3\\007" +
  "\\033]133;A\\007\\033]133;D;0\\007\\n'";

const count = (browser: any, sel: string) => browser.evaluate(`document.querySelectorAll(${JSON.stringify(sel)}).length`) as Promise<number>;

test('OSC 133 marks draw a gutter mark per command and an exit chip on failure', { tags: ['terminal'] }, async ({ app, screen, browser }) => {
  await openKeel(app);
  const vault = screen.getByRole('complementary', 'Vault');
  await vault.getByRole('button', 'Hosts').tap();
  await vault.getByRole('button', /^Local Shell\b/).tap();
  const input = screen.getByRole('textbox', 'Terminal input');
  await expect(input).toBeFocused();
  await input.pressSequentially(MARKS);
  await input.press('Enter');

  await expect.poll(() => count(browser, '.k-cmdmark--ok'), { timeout: 10_000 }).toBe(1);
  await expect.poll(() => count(browser, '.k-cmdmark--failed')).toBe(1);
  // One chip, on the failed command only, with its exit status.
  await expect.poll(() => browser.evaluate(`[...document.querySelectorAll('.k-cmdchip')].map((e) => e.textContent)`)).toEqual(['exit 3']);
  // The dot sits in the terminal's left inset, left of the first column;
  // the chip at the right end of the prompt line.
  const inset = await browser.evaluate(`(() => {
    const dot = document.querySelector('.k-cmdmark--failed .k-cmdmark-dot').getBoundingClientRect();
    const screenEl = document.querySelector('.term-body .xterm-screen').getBoundingClientRect();
    const chip = document.querySelector(".k-cmdchip").getBoundingClientRect();
    return { dotInGutter: dot.right <= screenEl.left + 0.5 && dot.width > 0, chipAtRight: chip.left > (screenEl.left + screenEl.right) / 2 };
  })()`);
  expect(inset).toEqual({ dotInGutter: true, chipAtRight: true });
  await app.screenshot('command-marks');

  // The palette offers command navigation; with marks present it does not
  // say the shell sends none.
  await input.press('Meta+k');
  const palette = screen.getByRole('dialog', 'Command palette');
  await expect(palette).toBeVisible();
  await screen.getByRole('combobox', 'Run a command or connect a host…').pressSequentially('Previous command');
  await expect(palette.getByRole('option', /^Previous command/)).toBeVisible();
  await screen.getByRole('combobox', 'Run a command or connect a host…').press('Enter');
  await expect(palette).toBeHidden();
  await expect(screen.getByText(/sends no command marks/)).toBeHidden();

  await screen.getByRole('button', 'Close tab').tap();
});

// §6.2 / §6.7 with marks: the agent reads the last command, its output and
// exit status (not the pane's tail) through the same scope, redaction and
// keel.log path. The fake provider sends nothing anywhere.
test('the agent reads the last marked command and logs it as a read', { tags: ['terminal', 'agent'] }, async ({ app, screen }) => {
  await keelSend(app.baseUrl, 'PUT', '/api/agent/keys/gemini', { key: 'e2e-not-a-real-gemini-key-0000' });
  await openKeel(app);
  const vault = screen.getByRole('complementary', 'Vault');
  await vault.getByRole('button', 'Hosts').tap();
  await vault.getByRole('button', /^Local Shell\b/).tap();
  const input = screen.getByRole('textbox', 'Terminal input', { visible: true });
  await expect(input).toBeFocused();
  await input.pressSequentially(
    "printf '\\033]133;A\\007$ \\033]133;B\\007make\\n\\033]133;C\\007build failed TOKEN=ghp_abcdefghijklmnopqrstuvwxyz0123456789\\n\\033]133;D;3\\007'",
  );
  await input.press('Enter');
  await expect.poll(() => screen.getByText('exit 3').count(), { timeout: 10_000 }).toBeGreaterThan(0);

  await input.press('Meta+i');
  const panel = screen.getByRole('complementary', 'Agent');
  const composer = panel.getByRole('textbox', 'Message the agent');
  await expect(composer).toBeFocused();
  await composer.fill('why did make fail?');
  await composer.press('Enter');
  await expect(panel).toContainText('Read Local Shell · last command, exit 3 · 1 line of output · 1 redacted');

  const entries = await keelApi<{ kind: string; message: string }[]>(app.baseUrl, '/api/logs');
  expect(entries.some((e) => e.kind === 'agent.read' && e.message.includes('last command, exit 3'))).toBe(true);
  expect(JSON.stringify(entries)).not.toContain('ghp_abcdefghijklmnopqrstuvwxyz0123456789');

  await keelSend(app.baseUrl, 'DELETE', '/api/agent/conversations');
  await keelSend(app.baseUrl, 'DELETE', '/api/agent/keys/gemini');
  await screen.getByRole('button', 'Close tab', { visible: true }).first().tap();
});

import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelApi, openKeel } from './keel.ts';

type Session = { id: string };

test('a local shell tab runs a command and closing it kills the PTY', { tags: ['terminal'] }, async ({ app, screen }) => {
  await openKeel(app);
  const before = await keelApi<Session[]>(app.baseUrl, '/api/sessions');

  await screen.getByRole('button', 'Terminal').tap();
  await expect(screen.getByRole('button', /^Local Shell\b/)).toBeVisible();
  const input = screen.getByRole('textbox', 'Terminal input');
  await expect(input).toBeFocused();
  // The typed line shows `$((6*7))`; only the shell's output has 42.
  await input.pressSequentially('echo keel-e2e-$((6*7))');
  await input.press('Enter');

  // xterm paints with WebGL, so the output is not in the DOM: read it from
  // the session's scrollback in keel serve.
  let sessions: Session[] = [];
  await expect.poll(async () => (sessions = await keelApi<Session[]>(app.baseUrl, '/api/sessions')).length).toBe(before.length + 1);
  const added = sessions.find((s) => !before.some((b) => b.id === s.id))!;
  await expect
    .poll(async () => (await keelApi<{ text: string }>(app.baseUrl, `/api/sessions/${added.id}/scrollback`)).text, { timeout: 10_000 })
    .toContain('keel-e2e-42');
  await app.screenshot('terminal-output');

  await screen.getByRole('button', 'Close tab').tap();
  await expect(screen.getByRole('textbox', 'Terminal input')).toBeHidden();
  await expect.poll(async () => (await keelApi<Session[]>(app.baseUrl, '/api/sessions')).some((s) => s.id === added.id)).toBe(false);
});

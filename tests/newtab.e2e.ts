import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelApi, openKeel } from './keel.ts';

// Phase 3 New tab page (docs/ui-redesign.md §5.3). The window opens on one,
// and it is the empty state when the last tab closes.

type Session = { id: string };
type Host = { id: string; name: string };

const page = (screen: any) => screen.getByRole('region', 'New tab');

async function closeSessionTab(screen: any) {
  await screen.getByRole('button', 'Close tab', { visible: true }).tap();
  await expect(screen.getByRole('textbox', 'Terminal input')).toBeHidden();
}

test('the New tab page has the input, recent hosts and the start actions', { tags: ['newtab'] }, async ({ app, screen }) => {
  await openKeel(app);
  const start = page(screen);
  await expect(start.getByRole('heading', 'Where to?')).toBeVisible();
  const input = start.getByRole('textbox', 'Host to connect to');
  await expect(input).toBeFocused();

  // Recent: the local shell is always there, with its Alt+1 hint.
  await expect(start.getByRole('button', /Local Shell/)).toBeVisible();
  await expect(start.getByText('⌥1')).toBeVisible();

  await expect(start.getByRole('button', /Quick connect/)).toBeVisible();
  await expect(start.getByRole('button', /Join session/)).toBeVisible();
  await expect(start.getByRole('button', /New host/)).toBeVisible();
  // The agent is a later phase: no entry for it yet.
  await expect(start.getByRole('button', /Ask agent/)).toHaveCount(0);

  // Typing filters the saved hosts.
  await input.fill('zzz-no-such-host');
  await expect(start.getByText(/No saved host matches/)).toBeVisible();
  await input.fill('local');
  await expect(start.getByRole('button', /Local Shell/)).toBeVisible();
  await app.screenshot('newtab-page');

  await start.getByRole('button', /New host/).tap();
  await expect(screen.getByRole('dialog').filter({ hasText: 'New host' })).toBeVisible();
  await screen.getByRole('button', 'Cancel', { visible: true }).tap();
});

test('Alt+1 opens the first recent host as a session', { tags: ['newtab'] }, async ({ app, screen }) => {
  await openKeel(app);
  const before = await keelApi<Session[]>(app.baseUrl, '/api/sessions');
  const input = page(screen).getByRole('textbox', 'Host to connect to');
  await expect(input).toBeFocused();
  await input.press('Alt+1');

  const terminal = screen.getByRole('textbox', 'Terminal input');
  await expect(terminal).toBeFocused();
  await expect.poll(async () => (await keelApi<Session[]>(app.baseUrl, '/api/sessions')).length).toBe(before.length + 1);
  await closeSessionTab(screen);
});

test('Cmd+Shift+T reopens the last closed tab as a new session', { tags: ['newtab'] }, async ({ app, screen }) => {
  await openKeel(app);
  const vault = screen.getByRole('complementary', 'Vault');
  await vault.getByRole('button', 'Hosts').tap();
  await vault.getByRole('button', /^Local Shell\b/).tap();
  await expect(screen.getByRole('textbox', 'Terminal input')).toBeFocused();
  const open = await keelApi<Session[]>(app.baseUrl, '/api/sessions');
  await closeSessionTab(screen);

  // Closing the last tab leaves the New tab page, with the closed tab listed.
  const start = page(screen);
  await expect(start.getByRole('heading', 'Where to?')).toBeVisible();
  const row = start.getByRole('region', 'Recently closed').getByRole('button', /Local Shell/);
  await expect(row).toBeVisible();
  await expect(row).toContainText('just now');
  await expect(row).toContainText('⌘⇧T');
  await app.screenshot('newtab-closed');

  const terminal = screen.getByRole('textbox', 'Terminal input');
  await start.getByRole('textbox', 'Host to connect to').press('Meta+Shift+t');
  await expect(terminal).toBeFocused();
  // A new PTY, not the old one.
  const now = await keelApi<Session[]>(app.baseUrl, '/api/sessions');
  expect(now.length).toBe(open.length);
  expect(now.some((s) => !open.some((o) => o.id === s.id))).toBe(true);
  await closeSessionTab(screen);
});

test('a pasted ssh command is read, offered as a new host, and not run', { tags: ['newtab'] }, async ({ app, screen }) => {
  await openKeel(app);
  const start = page(screen);
  const hostsBefore = await keelApi<Host[]>(app.baseUrl, '/api/hosts');
  const sessionsBefore = await keelApi<Session[]>(app.baseUrl, '/api/sessions');
  const input = start.getByRole('textbox', 'Host to connect to');
  await input.fill(
    "$ ssh -J bastion -o ProxyCommand='nc x 22' -o StrictHostKeyChecking=no -i ~/.ssh/id_ed25519 -L 8080:localhost:80 deploy@10.0.4.21 -p 2222 uptime",
  );

  const offer = start.getByRole('region', 'Pasted command');
  await expect(offer).toContainText('deploy@10.0.4.21:2222');
  await expect(offer).toContainText('Nothing has run.');
  await expect(offer).toContainText(/left out \(5\)/i);
  await expect(offer).toContainText('jump hosts are not supported yet');
  await expect(offer).toContainText('ProxyCommand');
  await expect(offer).toContainText('changes host key trust');
  await expect(offer).toContainText('opens a port forward');
  await expect(offer).toContainText('uptime');
  await expect(offer).toContainText('Key file ~/.ssh/id_ed25519 was not read');
  await app.screenshot('newtab-ssh');

  await input.press('Enter');
  const dialog = screen.getByRole('dialog').filter({ hasText: 'New host' });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByLabel('Hostname / IP')).toHaveValue('10.0.4.21');
  await expect(dialog.getByLabel('Port')).toHaveValue('2222');
  await expect(dialog.getByLabel('Username')).toHaveValue('deploy');
  await expect(dialog.getByLabel('Label')).toHaveValue('10.0.4.21');
  // The dropped options stay visible in the dialog.
  await expect(dialog).toContainText(/left out \(5\)/i);
  await expect(dialog).toContainText('-L 8080:localhost:80');
  await app.screenshot('newtab-ssh-host-dialog');
  await screen.getByRole('button', 'Cancel', { visible: true }).tap();

  // Nothing was saved or started.
  expect((await keelApi<Host[]>(app.baseUrl, '/api/hosts')).length).toBe(hostsBefore.length);
  expect((await keelApi<Session[]>(app.baseUrl, '/api/sessions')).length).toBe(sessionsBefore.length);
});

test('shell operators and substitutions in a pasted command give no host', { tags: ['newtab'] }, async ({ app, screen }) => {
  await openKeel(app);
  const start = page(screen);
  const input = start.getByRole('textbox', 'Host to connect to');
  for (const junk of ['ssh host; touch /tmp/keel-pwned', 'ssh $(id)', 'ssh `id`', 'ssh host | sh']) {
    await input.fill(junk);
    await input.press('Enter');
    await expect(start.getByRole('status')).toContainText(/not allowed|characters Keel does not accept/);
    await expect(screen.getByRole('dialog').filter({ hasText: 'New host' })).toBeHidden();
  }
  await expect(start.getByRole('region', 'Pasted command')).toHaveCount(0);
});

test('Connect once fills Quick connect with user@host:port', { tags: ['newtab'] }, async ({ app, screen }) => {
  await openKeel(app);
  const start = page(screen);
  await start.getByRole('textbox', 'Host to connect to').fill('ssh://deploy@[::1]:2200');
  await start.getByRole('button', 'Connect once').tap();
  const dialog = screen.getByRole('dialog').filter({ hasText: 'Quick connect' });
  await expect(dialog.getByLabel('Target')).toHaveValue('deploy@[::1]:2200');
  await screen.getByRole('button', 'Cancel', { visible: true }).tap();
});

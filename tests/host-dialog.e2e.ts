import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelApi, keelJson, keelSend, openKeel } from './keel.ts';

// Edit host: Jump host, Startup command, the host key line and Test
// connection (docs/ui-redesign.md §5.8). e2e has no SSH server, so Test
// connection is pinned on a loopback port nothing listens on (1): a
// readable "Connection refused", directly and through a jump host. Real
// jump connects are covered by the Go tests (internal/session).

type Host = { id: string; name: string; jumpHost?: string; startupCommand?: string };
type HostKey = { hostname: string; known: boolean; fingerprint?: string; keyType?: string };
type LogEntry = { kind: string; message: string };

const FP = 'SHA256:q3VtXz0b6kP1d8yRk2mJw4sTn7aEc9hLf5uGi3o9fKe';

test('Edit host: jump host and startup command round-trip, Test connection reports, host key forget', { tags: ['hosts'] }, async ({ app, screen }) => {
  const stamp = Date.now().toString(36);
  const names = { bastion: `e2e-bastion-${stamp}`, db: `e2e-db-${stamp}` };
  const password = `pw-${stamp}-scratch`;
  const bastion = await keelSend<Host>(app.baseUrl, 'POST', '/api/hosts', {
    name: names.bastion, address: '127.0.0.1', port: 1, username: 'jumper', authType: 'password', password: `jump-${password}`, color: '#5ee0b5',
  });
  try {
    await openKeel(app);
    const vault = screen.getByRole('complementary', 'Vault');
    await vault.getByRole('button', 'New host').tap();
    const dialog = screen.getByRole('dialog').filter({ hasText: 'New host' });
    await expect(dialog).toBeVisible();
    await dialog.getByLabel('Label').fill(names.db);
    await dialog.getByLabel('Hostname / IP').fill('127.0.0.1');
    await dialog.getByLabel('Port').fill('1');
    await dialog.getByLabel('Username').fill('deploy');
    await dialog.getByLabel('Password').fill(password);
    // A new host has no saved key.
    await expect(dialog.getByRole('group', 'Host key')).toContainText('Not saved');
    await expect(dialog.getByRole('button', 'Forget host key')).toBeHidden();

    // Test connection with the unsaved values: nothing listens on port 1.
    await dialog.getByRole('button', 'Test connection').tap();
    await expect(dialog.getByText('Connection refused')).toBeVisible();
    await expect(dialog).toContainText('nothing accepts SSH on that port');
    await expect(dialog.getByRole('button', 'Trust and save')).toBeHidden();
    await app.screenshot('host-test-refused');

    // Jump host: pick the saved bastion, then test again: the jump hop is named.
    await dialog.getByRole('combobox', 'Jump host').tap();
    await screen.getByRole('option', names.bastion).tap();
    await dialog.getByLabel('Startup command').fill('tmux new -A -s main');
    await dialog.getByRole('button', 'Test connection').tap();
    await expect(dialog).toContainText(`Jump host ${names.bastion}: nothing accepts SSH`);
    await app.screenshot('host-test-jump-refused');

    await dialog.getByRole('button', 'Save host').tap();
    await expect(dialog).toBeHidden();

    // Saved: the jump host is a reference, the startup command is config,
    // and no password reached keel.json.
    const hosts = await keelApi<Host[]>(app.baseUrl, '/api/hosts');
    const db = hosts.find((h) => h.name === names.db)!;
    expect(db).toMatchObject({ jumpHost: bastion.id, startupCommand: 'tmux new -A -s main' });
    expect(keelJson()).not.toContain(password);
    const logs = await keelApi<LogEntry[]>(app.baseUrl, '/api/logs?limit=200');
    expect(logs.some((e) => e.message === `connection test ${names.db}: refused`)).toBe(true);
    expect(JSON.stringify(logs)).not.toContain(password);

    // A key trusted earlier (set up through the API, as a first connect would).
    await keelSend(app.baseUrl, 'POST', '/api/known-hosts', { hostname: '127.0.0.1:1', fingerprint: FP, keyType: 'ssh-ed25519', replace: true });

    // Edit: the fields come back, and the host key line shows the key.
    await vault.getByRole('searchbox', 'Search hosts').fill(names.db);
    await vault.getByRole('button', 'Actions', { visible: true }).tap();
    await screen.getByRole('menuitem', 'Edit').tap();
    const editing = screen.getByRole('dialog').filter({ hasText: 'Edit host' });
    await expect(editing).toBeVisible();
    await expect(editing.getByRole('combobox', 'Jump host')).toContainText(names.bastion);
    await expect(editing.getByLabel('Startup command')).toHaveValue('tmux new -A -s main');
    const keyLine = editing.getByRole('group', 'Host key');
    await expect(keyLine).toContainText('Trusted');
    await expect(keyLine).toContainText('ED25519');
    await expect(keyLine).toContainText(FP);
    await app.screenshot('host-key-trusted');

    // Forget asks first, in the alertdialog; Cancel keeps the key.
    await keyLine.getByRole('button', 'Forget host key').tap();
    await expect(screen.getByRole('alertdialog')).toContainText('Forget host key?');
    await screen.getByRole('button', 'Cancel', { visible: true }).tap();
    await expect(editing).toBeVisible();
    expect((await keelApi<HostKey>(app.baseUrl, `/api/hosts/${db.id}/host-key`)).known).toBe(true);
    await keyLine.getByRole('button', 'Forget host key').tap();
    await screen.getByRole('button', 'Forget key').tap();
    await expect(keyLine).toContainText('Not saved');
    expect((await keelApi<HostKey>(app.baseUrl, `/api/hosts/${db.id}/host-key`)).known).toBe(false);
    const after = await keelApi<LogEntry[]>(app.baseUrl, '/api/logs?limit=200');
    expect(after.some((e) => e.message.startsWith(`forgot host key for ${names.db} (127.0.0.1:1`))).toBe(true);

    // Clear the jump host and the startup command; Save keeps that.
    await editing.getByRole('combobox', 'Jump host').tap();
    await screen.getByRole('option', 'None').tap();
    await editing.getByLabel('Startup command').fill('');
    await editing.getByRole('button', 'Save host').tap();
    await expect(editing).toBeHidden();
    const cleared = (await keelApi<Host[]>(app.baseUrl, '/api/hosts')).find((h) => h.id === db.id)!;
    expect(cleared.jumpHost ?? '').toBe('');
    expect(cleared.startupCommand ?? '').toBe('');
    await vault.getByRole('searchbox', 'Search hosts').fill('');
  } finally {
    const hosts = await keelApi<Host[]>(app.baseUrl, '/api/hosts');
    for (const h of hosts.filter((h) => Object.values(names).includes(h.name))) await keelSend(app.baseUrl, 'DELETE', `/api/hosts/${h.id}`);
  }
});

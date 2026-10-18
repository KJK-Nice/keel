import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelApi, keelJson, keelSend, openKeel, sshTest } from './keel.ts';

// Edit host: Jump host, Startup command, the host key line and Test
// connection (docs/ui-redesign.md §5.8), against the two loopback SSH
// servers scripts/e2e-serve.sh starts (bin/keel-sshtest): a jump host and a
// target that is reached through it.

type Host = { id: string; name: string; jumpHost?: string; startupCommand?: string };
type HostKey = { hostname: string; known: boolean; fingerprint?: string; keyType?: string };
type LogEntry = { kind: string; message: string };
type Session = { id: string; hostId: string; status: string };

test('Edit host: jump host, Test connection with Trust and save, startup command, host key forget', { tags: ['hosts'] }, async ({ app, screen }) => {
  const { jump, target } = sshTest();
  const stamp = Date.now().toString(36);
  const names = { bastion: `e2e-bastion-${stamp}`, db: `e2e-db-${stamp}` };
  // Set up the saved jump host (its password goes to the vault).
  await keelSend<Host>(app.baseUrl, 'POST', '/api/hosts', {
    name: names.bastion, address: jump.host, port: jump.port, username: jump.user, authType: 'password', password: jump.password, color: '#5ee0b5',
  });
  let sessionId = '';
  try {
    await openKeel(app);
    const vault = screen.getByRole('complementary', 'Vault');
    await vault.getByRole('button', 'New host').tap();
    const dialog = screen.getByRole('dialog').filter({ hasText: 'New host' });
    await expect(dialog).toBeVisible();
    await dialog.getByLabel('Label').fill(names.db);
    await dialog.getByLabel('Hostname / IP').fill(target.host);
    await dialog.getByLabel('Port').fill(String(target.port));
    await dialog.getByLabel('Username').fill(target.user);
    await dialog.getByLabel('Password').fill(target.password);
    await expect(dialog.getByRole('group', 'Host key')).toContainText('Not saved');
    await dialog.getByRole('combobox', 'Jump host').tap();
    await screen.getByRole('option', names.bastion).tap();

    // First test: the jump host's key is unknown. It is shown, not trusted.
    const trust = dialog.getByRole('button', 'Trust and save');
    await dialog.getByRole('button', 'Test connection').tap();
    await expect(dialog.getByText('Host key unknown')).toBeVisible();
    await expect(dialog).toContainText(`Jump host ${names.bastion}: ${jump.host}:${jump.port} presented a key`);
    await expect(dialog).toContainText(`ED25519 ${jump.fingerprint}`);
    await app.screenshot('host-test-jump-unknown');
    await trust.tap();

    // The test runs again: now the target's key, through the jump host.
    await expect(dialog).toContainText(`Through the jump host: ${target.host}:${target.port} presented a key`);
    await expect(dialog).toContainText(target.fingerprint);
    // Any edit drops the result, so a stale key can never be trusted.
    await dialog.getByLabel('Username').fill(`${target.user}x`);
    await expect(trust).toBeHidden();
    await expect(dialog.getByText('Host key unknown')).toBeHidden();
    await dialog.getByLabel('Username').fill(target.user);
    await dialog.getByRole('button', 'Test connection').tap();
    await expect(dialog).toContainText(target.fingerprint);
    await trust.tap();
    await expect(dialog.getByText(/^Connected in \d+ ms$/)).toBeVisible();
    await app.screenshot('host-test-connected');

    // A readable error: nothing listens on loopback port 1, behind the jump.
    await dialog.getByLabel('Port').fill('1');
    await dialog.getByRole('button', 'Test connection').tap();
    await expect(dialog.getByText('Connection refused')).toBeVisible();
    await expect(dialog).toContainText('Through the jump host: nothing accepts SSH on that port');
    await dialog.getByLabel('Port').fill(String(target.port));

    await dialog.getByLabel('Startup command').fill('echo hello-from-keel');
    await dialog.getByRole('button', 'Save host').tap();
    await expect(dialog).toBeHidden();

    const hosts = await keelApi<Host[]>(app.baseUrl, '/api/hosts');
    const db = hosts.find((h) => h.name === names.db)!;
    const bastion = hosts.find((h) => h.name === names.bastion)!;
    expect(db).toMatchObject({ jumpHost: bastion.id, startupCommand: 'echo hello-from-keel' });
    expect(keelJson()).not.toContain(target.password);
    expect(keelJson()).not.toContain(jump.password);
    const logs = await keelApi<LogEntry[]>(app.baseUrl, '/api/logs?limit=300');
    expect(logs.some((e) => e.message === `connection test ${names.db}: ok`)).toBe(true);
    expect(JSON.stringify(logs)).not.toContain(target.password);

    // Connect: through the jump host, and the startup command is typed once.
    await vault.getByRole('searchbox', 'Search hosts').fill(names.db);
    await vault.getByRole('button', new RegExp(`^${names.db}\\b`)).tap();
    await expect
      .poll(async () => (await keelApi<Session[]>(app.baseUrl, '/api/sessions')).find((s) => s.hostId === db.id)?.status)
      .toBe('ready');
    sessionId = (await keelApi<Session[]>(app.baseUrl, '/api/sessions')).find((s) => s.hostId === db.id)!.id;
    await expect
      .poll(async () => (await keelApi<{ text: string }>(app.baseUrl, `/api/sessions/${sessionId}/scrollback`)).text)
      .toContain('echo hello-from-keel');
    const after = await keelApi<LogEntry[]>(app.baseUrl, '/api/logs?limit=300');
    expect(after.filter((e) => e.message === `startup command sent to ${names.db}`)).toHaveLength(1);
    expect(after.some((e) => e.message.includes('hello-from-keel'))).toBe(false);
    await keelSend(app.baseUrl, 'DELETE', `/api/sessions/${sessionId}`);
    sessionId = '';

    // Edit: the fields come back, and the host key line shows the key.
    await openKeel(app);
    await vault.getByRole('searchbox', 'Search hosts').fill(names.db);
    // The filter is a debounced server round trip: wait for the
    // filtered tree, so this host's "Actions" is the only one left.
    await expect(vault.getByRole('button', new RegExp(`^${names.db}\\b`))).toBeVisible();
    await expect(vault.getByRole('button', /^Local Shell\b/)).toBeHidden();
    await vault.getByRole('button', 'Actions', { visible: true }).tap();
    await screen.getByRole('menuitem', 'Edit').tap();
    const editing = screen.getByRole('dialog').filter({ hasText: 'Edit host' });
    await expect(editing).toBeVisible();
    await expect(editing.getByRole('combobox', 'Jump host')).toContainText(names.bastion);
    await expect(editing.getByLabel('Startup command')).toHaveValue('echo hello-from-keel');
    const keyLine = editing.getByRole('group', 'Host key');
    await expect(keyLine).toContainText('Trusted');
    await expect(keyLine).toContainText('ED25519');
    await expect(keyLine).toContainText(target.fingerprint);
    await app.screenshot('host-key-trusted');

    // The saved password never goes to another address: a changed address
    // with the password left blank asks for it instead of testing.
    await editing.getByLabel('Hostname / IP').fill('127.0.0.2');
    await editing.getByRole('button', 'Test connection').tap();
    await expect(editing).toContainText('Address changed: type the password to test it');
    await editing.getByLabel('Hostname / IP').fill(target.host);

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
    const forgot = await keelApi<LogEntry[]>(app.baseUrl, '/api/logs?limit=300');
    expect(forgot.some((e) => e.message.startsWith(`forgot host key for ${names.db} (${target.host}:${target.port}`))).toBe(true);

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
    if (sessionId) await keelSend(app.baseUrl, 'DELETE', `/api/sessions/${sessionId}`).catch(() => {});
    const hosts = await keelApi<Host[]>(app.baseUrl, '/api/hosts');
    for (const h of hosts.filter((h) => Object.values(names).includes(h.name))) await keelSend(app.baseUrl, 'DELETE', `/api/hosts/${h.id}`);
  }
});

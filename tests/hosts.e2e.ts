import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelApi, keelJson, openKeel } from './keel.ts';

type Host = { id: string; name: string; address: string; username: string; authType: string };

test('a host is added, found, and removed from the Vault sidebar', { tags: ['hosts'] }, async ({ app, screen }) => {
  const name = `e2e-host-${Date.now().toString(36)}`;
  const password = `pw-${Date.now().toString(36)}-scratch`;
  await openKeel(app);
  const vault = screen.getByRole('complementary', 'Vault');

  await vault.getByRole('button', 'New host').tap();
  // The host modal is a <dialog> without an accessible name; its title is text.
  const dialog = screen.getByRole('dialog').filter({ hasText: 'New host' });
  await expect(dialog).toBeVisible();
  await dialog.getByLabel('Label').fill(name);
  await dialog.getByLabel('Hostname / IP').fill('192.0.2.10');
  await dialog.getByLabel('Username').fill('ubuntu');
  await dialog.getByLabel('Password').fill(password);
  await dialog.getByRole('button', 'Save host').tap();
  await expect(dialog).toBeHidden();

  // The tree row's name is the label (plus a live count once connected).
  const row = vault.getByRole('button', new RegExp(`^${name}\\b`));
  await expect(row).toBeVisible();
  await app.screenshot('host-added');

  // Side effect: keel serve stored the host, and the password went to the
  // vault, not to keel.json.
  const hosts = await keelApi<Host[]>(app.baseUrl, '/api/hosts');
  expect(hosts.find((h) => h.name === name)).toMatchObject({ address: '192.0.2.10', username: 'ubuntu', authType: 'password' });
  expect(keelJson()).toContain(name);
  expect(keelJson()).not.toContain(password);

  await vault.getByRole('searchbox', 'Search hosts').fill(name);
  await expect(row).toBeVisible();
  await expect(vault.getByRole('button', /^Local Shell\b/)).toBeHidden();

  // Every row's menu button is named just "Actions", so remove the host
  // while the filter leaves its row the only one in the tree.
  await vault.getByRole('button', 'Actions', { visible: true }).tap();
  await screen.getByRole('menuitem', 'Remove').tap();
  await expect(screen.getByRole('alertdialog')).toContainText(`Remove ${name}?`);
  await screen.getByRole('button', 'Remove').tap();
  await expect(row).toBeHidden();
  await vault.getByRole('searchbox', 'Search hosts').fill('');
  await expect(vault.getByRole('button', /^Local Shell\b/)).toBeVisible();

  const after = await keelApi<Host[]>(app.baseUrl, '/api/hosts');
  expect(after.some((h) => h.name === name)).toBe(false);
});

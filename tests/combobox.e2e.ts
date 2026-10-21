import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelApi, keelSend, openKeel, retire } from './keel.ts';

// k-combobox (docs/design/README.md, Combobox; js/lib/combobox.js): pick by
// typing, with the keyboard, in a modal dialog and in a page, in both
// themes. The value posts with the Datastar form (the hidden input).

type Identity = { id: string; name: string };
type Host = { id: string; name: string; identityId?: string; authType?: string };
type Group = { id: string; name: string };

const applyTheme = (browser: any, theme: string) => browser.evaluate(`window.__keelApplyTheme(${JSON.stringify(theme)})`);

for (const theme of ['dark', 'light']) {
  test(`combobox: pick an identity in the host dialog by typing (${theme})`, { tags: ['hosts', 'visual'] }, async ({ app, screen, browser }) => {
    const stamp = `${theme}-${Date.now().toString(36)}`;
    const ids: Identity[] = [];
    for (const name of [`cb-deploy-${stamp}`, `cb-backup-${stamp}`, `cb-root-${stamp}`]) {
      ids.push(await keelSend<Identity>(app.baseUrl, 'POST', '/api/identities', { name, kind: 'password', username: 'ops', password: `pw-${name}` }));
    }
    const hostName = `cb-host-${stamp}`;
    try {
      await openKeel(app);
      await applyTheme(browser, theme);
      await screen.getByRole('complementary', 'Vault').getByRole('button', 'New host').tap();
      const dialog = screen.getByRole('dialog').filter({ hasText: 'New host' });
      await expect(dialog).toBeVisible();
      await dialog.getByLabel('Label').fill(hostName);
      await dialog.getByLabel('Hostname / IP').fill('10.9.8.7');
      await dialog.getByLabel('Username').fill('ops');
      // Authentication is a k-seg (four fixed choices).
      await dialog.getByRole('button', 'Identity').tap();
      await expect(dialog.getByRole('button', 'Identity')).toHaveAttribute('aria-pressed', 'true');

      const combo = dialog.getByRole('combobox', 'Identity');
      await expect(combo).toHaveAttribute('aria-expanded', 'false');
      await combo.tap();
      await expect(combo).toHaveAttribute('aria-expanded', 'true');
      await combo.fill(`BACKUP-${stamp}`.toUpperCase());
      const list = screen.getByRole('listbox', 'Identity');
      await expect(list.getByRole('option')).toHaveCount(1);
      await expect(list.getByRole('option').first()).toContainText(`cb-backup-${stamp}`);
      await expect(list.getByRole('option').first()).toContainText('Password · ops');
      await app.screenshot(`${theme}-combobox-identity-filter`);

      // No matches is said, and Enter does not submit the dialog.
      await combo.fill('zz-nothing-matches');
      await expect(dialog.getByText('No matches', { visible: true })).toBeVisible();
      await combo.press('Enter');
      await expect(dialog).toBeVisible();
      await app.screenshot(`${theme}-combobox-no-matches`);

      // Escape closes the list, not the dialog, and puts the label back.
      await combo.press('Escape');
      await expect(combo).toHaveAttribute('aria-expanded', 'false');
      await expect(dialog).toBeVisible();

      // Type to filter, arrows and Enter pick.
      await combo.fill('cb-');
      await combo.press('End');
      await expect(combo).toHaveAttribute('aria-activedescendant', /-o\d+$/);
      await combo.press('Home');
      await combo.press('ArrowDown');
      await combo.fill(`root-${stamp}`);
      await combo.press('ArrowDown');
      await combo.press('Enter');
      await expect(combo).toHaveAttribute('aria-expanded', 'false');
      await expect(combo).toHaveValue(`cb-root-${stamp}`);
      await expect(dialog).toBeVisible();
      await app.screenshot(`${theme}-combobox-identity-picked`);

      await dialog.getByRole('button', 'Save host').tap();
      await expect(dialog).toBeHidden();
      const host = (await keelApi<Host[]>(app.baseUrl, '/api/hosts')).find((h) => h.name === hostName);
      expect(host).toMatchObject({ authType: 'identity', identityId: ids[2].id });
    } finally {
      const host = (await keelApi<Host[]>(app.baseUrl, '/api/hosts')).find((h) => h.name === hostName);
      if (host) await keelSend(app.baseUrl, 'DELETE', `/api/hosts/${host.id}`).catch(() => {});
      for (const id of ids) await keelSend(app.baseUrl, 'DELETE', `/api/identities/${id.id}`).catch(() => {});
      await applyTheme(browser, 'dark');
    }
  });

  test(`combobox: pick a role on the group page by typing (${theme})`, { tags: ['agent', 'visual'] }, async ({ app, screen, browser }) => {
    const gname = `cb-group-${theme}`;
    const groups = await keelApi<Group[]>(app.baseUrl, '/api/groups');
    const group = groups.find((g) => g.name === gname) ?? (await keelSend<Group>(app.baseUrl, 'POST', '/api/groups', { name: gname }));
    const made: Host[] = [];
    for (const name of [`cb-a-${theme}`, `cb-b-${theme}`]) {
      const have = (await keelApi<Host[]>(app.baseUrl, '/api/hosts')).find((h) => h.name === name);
      made.push(have ?? (await keelSend<Host>(app.baseUrl, 'POST', '/api/hosts', { name, authType: 'local' })));
    }
    await keelSend(app.baseUrl, 'POST', '/api/hosts/move', { ids: made.map((h) => h.id), groupId: group.id });
    await keelSend(app.baseUrl, 'PUT', `/api/groups/${group.id}/roles`, { hostId: made[0].id, role: 'control-plane' });
    await keelSend(app.baseUrl, 'PUT', `/api/groups/${group.id}/roles`, { hostId: made[1].id, role: '' });
    try {
      await openKeel(app);
      await applyTheme(browser, theme);
      await screen.getByRole('button', 'New host', { visible: true }).first().press('Meta+k');
      const palette = screen.getByRole('dialog', 'Command palette');
      await palette.getByRole('combobox').fill(`Group ${gname}`);
      await palette.getByRole('option').filter({ hasText: `Group ${gname}` }).first().tap();
      const page = screen.getByRole('region', 'Group');
      await expect(page).toBeVisible();

      const combo = page.getByRole('combobox', `Role of cb-b-${theme}`);
      await combo.tap();
      await combo.pressSequentially('CONTROL');
      const list = screen.getByRole('listbox', `Role of cb-b-${theme}`);
      await expect(list.getByRole('option')).toHaveCount(1);
      await app.screenshot(`${theme}-combobox-role-filter`);
      await combo.press('Enter');
      await expect
        .poll(async () => (await keelApi<{ members: { hostId: string; role?: string }[] }>(app.baseUrl, `/api/groups/${group.id}/members`)).members.find((m) => m.hostId === made[1].id)?.role)
        .toBe('control-plane');
      await expect(page.getByRole('combobox', `Role of cb-b-${theme}`)).toHaveValue('control-plane');
      await expect(page.getByRole('list', 'Hosts per role')).toContainText('2 control-plane');
      // The repainted row's picker works too (delegated listeners).
      await page.getByRole('combobox', `Role of cb-b-${theme}`).tap();
      await screen.getByRole('option', 'No role').tap();
      await expect
        .poll(async () => (await keelApi<{ members: { hostId: string; role?: string }[] }>(app.baseUrl, `/api/groups/${group.id}/members`)).members.find((m) => m.hostId === made[1].id)?.role ?? '')
        .toBe('');
      await app.screenshot(`${theme}-combobox-role-picked`);
    } finally {
      // Local hosts cannot be deleted as they are: retire turns them into
      // SSH hosts first, then removes them and the folder.
      await retire(app, new RegExp(`^cb-[ab]-${theme}$`), gname);
      await applyTheme(browser, 'dark');
    }
  });
}

// A saved value that no longer exists is never shown, or saved, as another
// one: a forward whose host was deleted says so and refuses Save until a
// host is picked; a Keychain-identity host whose identity was deleted (the
// vault clears it from its hosts) shows none chosen, not the first
// identity, and refuses Save too.
test('combobox: a deleted host or identity is not swapped for another', { tags: ['hosts'] }, async ({ app, screen }) => {
  const stamp = Date.now().toString(36);
  type Tunnel = { id: string; name: string; hostId: string };
  const mk = (name: string) => keelSend<Host>(app.baseUrl, 'POST', '/api/hosts', { name, address: '10.1.2.3', username: 'ops', authType: 'password', password: 'x' });
  const keep = await mk(`cb-keep-${stamp}`);
  const gone = await mk(`cb-gone-${stamp}`);
  const fwdName = `cb-fwd-${stamp}`;
  const tn = await keelSend<Tunnel>(app.baseUrl, 'POST', '/api/tunnels', { name: fwdName, hostId: gone.id, kind: 'local', bindAddr: '127.0.0.1', bindPort: 0, destHost: '127.0.0.1', destPort: 5432 });
  const idA = await keelSend<Identity>(app.baseUrl, 'POST', '/api/identities', { name: `cb-ida-${stamp}`, kind: 'password', username: 'ops', password: 'pw-a' });
  const idB = await keelSend<Identity>(app.baseUrl, 'POST', '/api/identities', { name: `cb-idb-${stamp}`, kind: 'password', username: 'ops', password: 'pw-b' });
  const hostName = `cb-idhost-${stamp}`;
  const idHost = await keelSend<Host>(app.baseUrl, 'POST', '/api/hosts', { name: hostName, address: '10.1.2.4', username: 'ops', authType: 'identity', identityId: idB.id });
  await keelSend(app.baseUrl, 'DELETE', `/api/hosts/${gone.id}`);
  await keelSend(app.baseUrl, 'DELETE', `/api/identities/${idB.id}`);
  try {
    await openKeel(app);
    const vault = screen.getByRole('complementary', 'Vault');

    // The forward: its host is gone.
    await vault.getByRole('button', 'Forwards').tap();
    await vault.getByRole('button', new RegExp(`^${fwdName}\\b`)).secondaryTap();
    await screen.getByRole('menu').getByRole('menuitem', 'Edit').tap();
    const fwd = screen.getByRole('dialog').filter({ hasText: 'Edit forward' });
    await expect(fwd).toBeVisible();
    const hostBox = fwd.getByRole('combobox', 'Host');
    await expect(hostBox).toHaveValue('Missing host — pick another');
    await app.screenshot('combobox-missing-host');
    await fwd.getByRole('button', 'Save forward').tap();
    await expect(fwd).toBeVisible();
    expect((await keelApi<Tunnel[]>(app.baseUrl, '/api/tunnels')).find((t) => t.id === tn.id)?.hostId).toBe(gone.id);
    await hostBox.tap();
    await hostBox.fill(`cb-keep-${stamp}`);
    await hostBox.press('Enter');
    await expect(hostBox).toHaveValue(`cb-keep-${stamp}`);
    await fwd.getByRole('button', 'Save forward').tap();
    await expect(fwd).toBeHidden();
    await expect.poll(async () => (await keelApi<Tunnel[]>(app.baseUrl, '/api/tunnels')).find((t) => t.id === tn.id)?.hostId).toBe(keep.id);

    // The host: its identity is gone.
    await vault.getByRole('button', 'Hosts').tap();
    await vault.getByRole('searchbox', 'Search hosts').fill(hostName);
    await expect(vault.getByRole('button', /^Local Shell\b/)).toBeHidden();
    await vault.getByRole('button', 'Actions', { visible: true }).tap();
    await screen.getByRole('menuitem', 'Edit').tap();
    const edit = screen.getByRole('dialog').filter({ hasText: 'Edit host' });
    await expect(edit).toBeVisible();
    const idBox = edit.getByRole('combobox', 'Identity');
    await expect(idBox).toHaveValue('');
    await edit.getByRole('button', 'Save host').tap();
    await expect(edit).toBeVisible();
    expect((await keelApi<Host[]>(app.baseUrl, '/api/hosts')).find((h) => h.id === idHost.id)?.identityId ?? '').toBe('');
    await idBox.tap();
    await screen.getByRole('option', new RegExp(`^cb-ida-${stamp}`)).tap();
    await edit.getByRole('button', 'Save host').tap();
    await expect(edit).toBeHidden();
    await expect.poll(async () => (await keelApi<Host[]>(app.baseUrl, '/api/hosts')).find((h) => h.id === idHost.id)?.identityId).toBe(idA.id);
  } finally {
    await keelSend(app.baseUrl, 'DELETE', `/api/tunnels/${tn.id}`).catch(() => {});
    for (const h of [keep, idHost]) await keelSend(app.baseUrl, 'DELETE', `/api/hosts/${h.id}`).catch(() => {});
    for (const id of [idA, idB]) await keelSend(app.baseUrl, 'DELETE', `/api/identities/${id.id}`).catch(() => {});
  }
});

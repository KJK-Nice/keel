import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelApi, keelSend, openKeel } from './keel.ts';

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
      for (const h of made) await keelSend(app.baseUrl, 'DELETE', `/api/hosts/${h.id}`).catch(() => {});
      await keelSend(app.baseUrl, 'DELETE', `/api/groups/${group.id}`).catch(() => {});
      await applyTheme(browser, 'dark');
    }
  });
}

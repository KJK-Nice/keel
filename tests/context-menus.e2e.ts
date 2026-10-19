import { describe, test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelApi, keelSend, openKeel, sshTest } from './keel.ts';

// Row context menus (k-menu, docs/design/components.css). A right-click on
// a row, ⇧F10 or the ContextMenu key opens the same items as the row's "…"
// button (internal/ui/rowmenus.templ, one menu per row kind); Escape closes
// it and gives focus back to the row; a list push leaves no menu behind.

type Host = { id: string; name: string; address: string; port: number; username: string; authType: string; agentReadable: boolean };
type Group = { id: string; name: string };
type Snippet = { id: string; name: string; command: string };
type Tunnel = { id: string; name: string; status: string };
type Notebook = { id: string; name: string; pinned: boolean; private: boolean };

// Copies land here instead of the system clipboard.
const CLIPBOARD = `(() => {
  window.__copied = [];
  const clip = { writeText: async (t) => { window.__copied.push(String(t)); }, readText: async () => '' };
  try { Object.defineProperty(navigator, 'clipboard', { value: clip, configurable: true }); } catch { navigator.clipboard.writeText = clip.writeText; }
})()`;
const copied = (browser: any) => browser.evaluate('window.__copied') as Promise<string[]>;
// <body>'s children and its portaled menus: a list push must leave both as they were.
const bodyShape = (browser: any) =>
  browser.evaluate(`({ children: document.body.children.length, menus: document.querySelectorAll('body > [data-tui-dropdownmenu-content]').length })`) as Promise<{ children: number; menus: number }>;
const pushes = (browser: any) => browser.evaluate('window.__pushes || 0') as Promise<number>;
const COUNT_PUSHES = `window.__pushes = 0; window.addEventListener('keel-vault-changed', () => { window.__pushes++; });`;

const stamp = () => Date.now().toString(36);

async function expectItems(menu: any, items: string[], absent: string[] = []) {
  for (const name of items) await expect(menu.getByRole(/^Agent can read$/.test(name) ? 'menuitemcheckbox' : 'menuitem', name)).toBeVisible();
  for (const name of absent) await expect(menu.getByRole('menuitem', name)).toHaveCount(0);
}

describe('context menus', { tags: ['context-menus', 'sidebar'], serial: true }, () => {
  test('host row: right-click, ⇧F10, copy SSH command, duplicate, agent toggle, delete', async ({ app, screen, browser }) => {
    const name = `cm-host-${stamp()}`;
    const password = `pw-${stamp()}-secret`;
    const host = await keelSend<Host>(app.baseUrl, 'POST', '/api/hosts', { name, address: '192.0.2.44', port: 2222, username: 'ops', authType: 'password', password });
    try {
      await browser.addInitScript(CLIPBOARD);
      await openKeel(app);
      const vault = screen.getByRole('complementary', 'Vault');
      const menu = screen.getByRole('menu');
      await vault.getByRole('button', 'Hosts').tap();
      await vault.getByRole('searchbox', 'Search hosts').fill(name);
      const row = vault.getByRole('button', new RegExp(`^${name}\\b`));
      await expect(row).toBeVisible();

      // Right-click: the "…" menu's items, with icons and shortcuts.
      await row.secondaryTap();
      await expect(menu).toBeVisible();
      await expectItems(menu, ['Connect', 'Open in split', 'Edit', 'Duplicate', 'Copy SSH command', 'Move to folder…', 'Agent can read', 'Delete']);
      await expect(menu.getByRole('menuitem', 'Connect')).toContainText('↵');
      await expect(menu.getByRole('menuitem', 'Open in split')).toContainText('⌥↵');
      await expect(menu.getByRole('menuitem', 'Open in split')).toHaveAttribute('aria-keyshortcuts', 'Alt+Enter');
      await expect(menu.getByRole('menuitemcheckbox', 'Agent can read')).toHaveAttribute('aria-checked', 'true');
      await app.screenshot('host-context-menu');

      // Copy SSH command: the address and port, never the password.
      await menu.getByRole('menuitem', 'Copy SSH command').tap();
      await expect(menu).toHaveCount(0);
      await expect.poll(() => copied(browser)).toEqual(['ssh -p 2222 ops@192.0.2.44']);
      expect((await copied(browser)).join('\n')).not.toContain(password);

      // The "…" button opens the very same menu.
      await row.hover();
      await vault.getByRole('button', 'Actions', { visible: true }).tap();
      await expectItems(menu, ['Connect', 'Open in split', 'Copy SSH command', 'Delete']);
      await browser.keyboard.press('Escape');
      await expect(menu).toHaveCount(0);

      // ⇧F10 on the focused row opens it with the first item focused;
      // arrows move, Escape closes and gives the row its focus back.
      await row.focus();
      await row.press('Shift+F10');
      await expect(menu).toBeVisible();
      await expect(menu.getByRole('menuitem', 'Connect')).toBeFocused();
      await browser.keyboard.press('ArrowDown');
      await expect(menu.getByRole('menuitem', 'Open in split')).toBeFocused();
      await browser.keyboard.press('Escape');
      await expect(menu).toHaveCount(0);
      await expect(row).toBeFocused();

      // Agent can read: one switch, the rest of the host as saved.
      await row.secondaryTap();
      await menu.getByRole('menuitemcheckbox', 'Agent can read').tap();
      await expect.poll(async () => (await keelApi<Host[]>(app.baseUrl, '/api/hosts')).find((h) => h.id === host.id)?.agentReadable).toBe(false);
      await row.secondaryTap();
      await expect(menu.getByRole('menuitemcheckbox', 'Agent can read')).toHaveAttribute('aria-checked', 'false');
      await browser.keyboard.press('Escape');

      // Duplicate.
      await row.secondaryTap();
      await menu.getByRole('menuitem', 'Duplicate').tap();
      await expect.poll(async () => (await keelApi<Host[]>(app.baseUrl, '/api/hosts')).filter((h) => h.name.startsWith(name)).length).toBe(2);

      // Delete asks first.
      await vault.getByRole('searchbox', 'Search hosts').fill(`${name} (copy)`);
      const copy = vault.getByRole('button', new RegExp(`^${name} \\(copy\\)`));
      await copy.secondaryTap();
      await menu.getByRole('menuitem', 'Delete').tap();
      await expect(screen.getByRole('alertdialog')).toContainText(`Remove ${name} (copy)?`);
      await screen.getByRole('button', 'Remove').tap();
      await expect(copy).toBeHidden();
      await vault.getByRole('searchbox', 'Search hosts').fill('');
    } finally {
      for (const h of await keelApi<Host[]>(app.baseUrl, '/api/hosts')) {
        if (h.name.startsWith(name)) await keelSend(app.baseUrl, 'DELETE', `/api/hosts/${h.id}`);
      }
    }
  });

  test('a list push leaves no menu behind', async ({ app, screen, browser }) => {
    // The push renames a snippet, so every list re-renders with the same rows.
    const sn = await keelSend<Snippet>(app.baseUrl, 'POST', '/api/snippets', { name: `cm-push-${stamp()}`, command: 'uptime' });
    await browser.addInitScript(COUNT_PUSHES);
    await openKeel(app);
    const vault = screen.getByRole('complementary', 'Vault');
    const menu = screen.getByRole('menu');
    await vault.getByRole('button', 'Hosts').tap();
    const local = vault.getByRole('button', /^Local Shell\b/);
    await local.secondaryTap();
    await expect(menu).toBeVisible();
    await browser.keyboard.press('Escape');
    await expect(menu).toHaveCount(0);
    const before = await bodyShape(browser);
    const n = await pushes(browser);
    try {
      for (let i = 0; i < 3; i++) {
        await keelSend(app.baseUrl, 'PUT', `/api/snippets/${sn.id}`, { ...sn, name: `${sn.name}-${i}` });
        await expect.poll(() => pushes(browser)).toBeGreaterThan(n + i);
      }
      await expect.poll(() => bodyShape(browser)).toEqual(before);
      // The menus still open after the morph.
      await local.secondaryTap();
      await expectItems(menu, ['Connect', 'Open in split', 'Edit', 'Move to folder…', 'Agent can read'], ['Duplicate', 'Copy SSH command', 'Delete']);
      await browser.keyboard.press('Escape');
    } finally {
      await keelSend(app.baseUrl, 'DELETE', `/api/snippets/${sn.id}`);
    }
  });

  test('folder row: the folder actions, Delete asks first', async ({ app, screen, browser }) => {
    const name = `cm-folder-${stamp()}`;
    const g = await keelSend<Group>(app.baseUrl, 'POST', '/api/groups', { name });
    try {
      await openKeel(app);
      const vault = screen.getByRole('complementary', 'Vault');
      const menu = screen.getByRole('menu');
      await vault.getByRole('button', 'Hosts').tap();
      const heading = vault.getByRole('button', new RegExp(`^${name}\\b`));
      await heading.secondaryTap();
      await expectItems(menu, ['Roles and runbooks', 'Run a runbook…', 'New host', 'New subgroup', 'Edit', 'Rename', 'Move into…', 'Delete']);
      await menu.getByRole('menuitem', 'Delete').tap();
      await expect(screen.getByRole('alertdialog')).toContainText(`Remove group ${name}?`);
      await screen.getByRole('button', 'Remove').tap();
      await expect(heading).toBeHidden();
    } finally {
      const left = (await keelApi<Group[]>(app.baseUrl, '/api/groups')).find((x) => x.id === g.id);
      if (left) await keelSend(app.baseUrl, 'DELETE', `/api/groups/${g.id}`);
    }
  });

  test('snippet and notebook rows in the sidebar', async ({ app, screen, browser }) => {
    const snName = `cm-snip-${stamp()}`;
    const command = 'df -h /var';
    const sn = await keelSend<Snippet>(app.baseUrl, 'POST', '/api/snippets', { name: snName, command });
    const nbName = `cm-nb-${stamp()}`;
    const nb = await keelSend<Notebook>(app.baseUrl, 'POST', '/api/notebooks', { name: nbName, cells: [{ body: 'notes' }] });
    const privName = `cm-private-${stamp()}`;
    const priv = await keelSend<Notebook>(app.baseUrl, 'POST', '/api/notebooks', { name: privName, cells: [{ body: 'mine' }] });
    await keelSend(app.baseUrl, 'PUT', `/api/notebooks/${priv.id}/private`, { private: true });
    try {
      await browser.addInitScript(CLIPBOARD);
      await openKeel(app);
      const vault = screen.getByRole('complementary', 'Vault');
      const menu = screen.getByRole('menu');
      await vault.getByRole('button', 'Snippets').tap();

      const snRow = vault.getByRole('button', new RegExp(`^${snName}\\b`));
      await snRow.secondaryTap();
      await expectItems(menu, ['Type into pane…', 'Copy', 'Edit', 'Delete']);
      await menu.getByRole('menuitem', 'Copy').tap();
      await expect.poll(() => copied(browser)).toEqual([command]);
      await snRow.secondaryTap();
      await menu.getByRole('menuitem', 'Delete').tap();
      await expect(screen.getByRole('alertdialog')).toContainText(`Delete snippet “${snName}”?`);
      await screen.getByRole('button', 'Cancel', { visible: true }).tap();
      await expect(snRow).toBeVisible();

      // The sidebar filter still narrows the rows the stream rendered.
      await vault.getByRole('searchbox').fill(snName);
      await expect(snRow).toBeVisible();
      await expect(vault.getByRole('button', /^disk\b/)).toBeHidden();
      await vault.getByRole('searchbox').fill('');

      const nbRow = vault.getByRole('button', nbName);
      await nbRow.secondaryTap();
      await expectItems(menu, ['Open', 'Rename', 'Pin', 'Make private', 'History', 'Ask the agent about this notebook', 'Delete']);
      await menu.getByRole('menuitem', 'Pin').tap();
      await expect.poll(async () => (await keelApi<Notebook[]>(app.baseUrl, "/api/notebooks")).find((x) => x.id === nb.id)?.pinned).toBe(true);
      // The pushed row (now pinned) carries the menu's new label.
      await expect(vault.getByRole('button', `Unpin ${nbName}`)).toBeVisible();
      await nbRow.secondaryTap();
      await expectItems(menu, ['Unpin'], ['Pin']);
      await browser.keyboard.press('Escape');

      // A private notebook is never offered to the agent (§6 rule 12).
      await vault.getByRole('button', privName).secondaryTap();
      await expectItems(menu, ['Open', 'Let the agent read it', 'History', 'Delete'], ['Ask the agent about this notebook', 'Make private']);
      await browser.keyboard.press('Escape');
      await expect(menu).toHaveCount(0);
    } finally {
      await keelSend(app.baseUrl, 'DELETE', `/api/snippets/${sn.id}`);
      await keelSend(app.baseUrl, 'DELETE', `/api/notebooks/${nb.id}?permanent=1`).catch(() => {});
      await keelSend(app.baseUrl, 'DELETE', `/api/notebooks/${priv.id}?permanent=1`).catch(() => {});
    }
  });

  test('forward row: start and stop, copy local address', async ({ app, screen, browser }) => {
    const { target } = sshTest();
    const host = await keelSend<Host>(app.baseUrl, 'POST', '/api/hosts', { name: `cm-fwd-host-${stamp()}`, address: target.host, port: target.port, username: target.user, authType: 'password', password: target.password });
    const port = 20000 + Math.floor(Math.random() * 20000);
    const name = `cm-fwd-${stamp()}`;
    const tn = await keelSend<Tunnel>(app.baseUrl, 'POST', '/api/tunnels', { name, hostId: host.id, kind: 'local', bindAddr: '127.0.0.1', bindPort: port, destHost: '127.0.0.1', destPort: target.port });
    const status = async () => (await keelApi<Tunnel[]>(app.baseUrl, '/api/tunnels')).find((t) => t.id === tn.id)?.status;
    try {
      await browser.addInitScript(CLIPBOARD);
      await openKeel(app);
      const vault = screen.getByRole('complementary', 'Vault');
      const menu = screen.getByRole('menu');
      await vault.getByRole('button', 'Forwards').tap();
      const row = vault.getByRole('button', new RegExp(`^${name}\\b`));
      await row.secondaryTap();
      await expectItems(menu, ['Start', 'Copy local address', 'Edit', 'Delete'], ['Stop']);
      await menu.getByRole('menuitem', 'Copy local address').tap();
      await expect.poll(() => copied(browser)).toEqual([`127.0.0.1:${port}`]);

      await row.secondaryTap();
      await menu.getByRole('menuitem', 'Start').tap();
      // The loopback SSH server's key is new to this vault: trust it once.
      const trust = screen.getByRole('button', 'Trust and connect');
      await expect.poll(async () => (await status()) === 'ready' || (await trust.isVisible())).toBe(true);
      if (await trust.isVisible()) await trust.tap();
      await expect.poll(status, { timeout: 15000 }).toBe('ready');
      await row.secondaryTap();
      await expectItems(menu, ['Stop'], ['Start']);
      await menu.getByRole('menuitem', 'Stop').tap();
      await expect.poll(status).not.toBe('ready');

      await row.secondaryTap();
      await menu.getByRole('menuitem', 'Delete').tap();
      await expect(screen.getByRole('alertdialog')).toContainText(`Delete forward “${name}”?`);
      await screen.getByRole('button', 'Cancel', { visible: true }).tap();
    } finally {
      await keelSend(app.baseUrl, 'DELETE', `/api/tunnels/${tn.id}`).catch(() => {});
      // Forget the trusted key: tests/host-dialog.e2e.ts expects it unknown.
      await keelSend(app.baseUrl, 'DELETE', `/api/hosts/${host.id}/host-key`).catch(() => {});
      await keelSend(app.baseUrl, 'DELETE', `/api/hosts/${host.id}`).catch(() => {});
    }
  });

  test('pane bar and Panes rows share the pane menu', async ({ app, screen, browser }) => {
    await openKeel(app);
    const vault = screen.getByRole('complementary', 'Vault');
    const menu = screen.getByRole('menu');
    const terms = screen.getByRole('textbox', 'Terminal input');
    await vault.getByRole('button', 'Hosts').tap();
    await vault.getByRole('button', /^Local Shell\b/).tap();
    await expect(terms).toHaveCount(1);

    // Pane bar right-click: Split right with its real shortcut.
    await screen.getByRole('button', 'More pane actions', { visible: true }).secondaryTap();
    await expect(menu).toBeVisible();
    await expectItems(menu, ['Split right', 'Split down', 'Share', 'Close pane'], ['Focus', 'Reconnect']);
    await expect(menu.getByRole('menuitem', 'Split right')).toContainText('⌘⇧D');
    await expect(menu.getByRole('menuitem', 'Split down')).toContainText('⌘⇧E');
    await expect(menu.getByRole('menuitem', 'Close pane')).toContainText('⌘W');
    await menu.getByRole('menuitem', 'Split right').tap();
    await expect(terms).toHaveCount(2);

    // The More button opens the same menu; with two panes Focus view shows.
    await screen.getByRole('button', 'More pane actions', { visible: true }).first().tap();
    await expectItems(menu, ['Split right', 'Split down', 'Focus view', 'Close pane'], ['Focus']);
    await expect(menu.getByRole('menuitem', 'Focus view')).toContainText('⌘⇧↵');
    await app.screenshot('pane-bar-menu');
    await browser.keyboard.press('Escape');
    await expect(menu).toHaveCount(0);

    // A Panes row: Focus, the splits, Share and Close.
    await vault.getByRole('button', 'Panes').tap();
    const rows = vault.getByRole('button', /^Local Shell\b/);
    await expect(rows).toHaveCount(2);
    await rows.nth(1).secondaryTap();
    await expectItems(menu, ['Focus', 'Split right', 'Split down', 'Share', 'Close pane'], ['Focus view', 'Reconnect']);
    await browser.keyboard.press('Escape');
    // ⇧F10 on a focused row, then Close pane.
    await rows.nth(1).focus();
    await rows.nth(1).press('Shift+F10');
    await expect(menu.getByRole('menuitem', 'Focus')).toBeFocused();
    await menu.getByRole('menuitem', 'Close pane').tap();
    await expect(terms).toHaveCount(1);

    await screen.getByRole('button', 'Close tab', { visible: true }).tap();
    await expect(terms).toHaveCount(0);
  });
});

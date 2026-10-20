import { describe, test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelApi, keelSend, openKeel } from './keel.ts';

// Context menus, part 2: the terminal body (Settings · Terminal ·
// Right-click in the terminal: Paste, Menu or Nothing), notebook code
// blocks, agent cards, and the Keychain and Snippets page rows. Every one
// is a k-menu opened through the same open-at machinery as the row menus
// (lib/rowmenu.js, components/dropdownmenu).

type Session = { id: string };
type Notebook = { id: string; name: string };
type Snippet = { id: string; name: string };
type Identity = { id: string; name: string };

// Copies land in window.__copied; reads come from window.__clip.
const CLIPBOARD = `(() => {
  window.__copied = [];
  window.__clip = '';
  window.__reads = 0;
  const clip = { writeText: async (t) => { window.__copied.push(String(t)); }, readText: async () => { window.__reads++; return window.__clip; } };
  try { Object.defineProperty(navigator, 'clipboard', { value: clip, configurable: true }); } catch { navigator.clipboard.writeText = clip.writeText; navigator.clipboard.readText = clip.readText; }
})()`;
const copied = (browser: any) => browser.evaluate('window.__copied') as Promise<string[]>;
const reads = (browser: any) => browser.evaluate('window.__reads') as Promise<number>;
const prefs = (browser: any) => browser.evaluate(`JSON.parse(localStorage.getItem('keel-term') || '{}')`) as Promise<Record<string, unknown>>;
const scrollback = async (app: any, id: string) => (await keelApi<{ text: string }>(app.baseUrl, `/api/sessions/${id}/scrollback`)).text;
const stamp = () => Date.now().toString(36);

async function openLocal(app: any, screen: any): Promise<Session> {
  const before = await keelApi<Session[]>(app.baseUrl, '/api/sessions');
  const vault = screen.getByRole('complementary', 'Vault');
  await vault.getByRole('button', 'Hosts').tap();
  await vault.getByRole('button', /^Local Shell\b/).first().tap();
  await expect(screen.getByRole('textbox', 'Terminal input', { visible: true })).toBeFocused();
  let now: Session[] = [];
  await expect.poll(async () => (now = await keelApi<Session[]>(app.baseUrl, '/api/sessions')).length).toBe(before.length + 1);
  return now.find((s) => !before.some((b) => b.id === s.id))!;
}

async function closeTabs(app: any, screen: any) {
  // Leave no tab behind: the next file restores this window's workspace.
  for (let i = 0; i < 8; i++) {
    const close = screen.getByRole('button', /^Close (tab|pane)$/, { visible: true });
    if (!(await close.count())) break;
    await close.first().tap();
  }
  await expect(screen.getByRole('textbox', 'Terminal input')).toHaveCount(0);
  await expect.poll(async () => JSON.stringify(await keelApi<Session[]>(app.baseUrl, '/api/sessions'))).toBe('[]');
}

describe('context menus, part 2', { tags: ['context-menus'], serial: true }, () => {
  test('terminal: right-click menu by default, Paste and Nothing from Settings', async ({ app, screen, browser }) => {
    // Prefs saved before the setting existed: rightClickPaste false → Menu.
    await browser.addInitScript(`try { if (!localStorage.getItem('keel-term-e2e')) { localStorage.setItem('keel-term', JSON.stringify({ rightClickPaste: false })); localStorage.setItem('keel-term-e2e', '1'); } } catch {}`);
    await browser.addInitScript(CLIPBOARD);
    await openKeel(app);
    const session = await openLocal(app, screen);
    const menu = screen.getByRole('menu');
    const term = screen.getByRole('textbox', 'Terminal input', { visible: true });

    await term.secondaryTap();
    await expect(menu).toBeVisible();
    for (const name of ['Copy', 'Paste', 'Select all', 'Clear', 'Find', 'Split right', 'Split down', 'Close pane']) {
      await expect(menu.getByRole('menuitem', name)).toBeVisible();
    }
    await expect(menu.getByRole('menuitem', 'Split right')).toContainText(/⌘⇧D|Ctrl\+⇧D/);
    await expect(menu.getByRole('menuitem', 'Close pane')).toContainText(/⌘W|Ctrl\+W/);
    await expect(menu.getByRole('menuitem', 'Find')).toContainText(/⌘F|Ctrl\+F/);
    // Nothing selected: Copy is disabled.
    await expect(menu.getByRole('menuitem', 'Copy')).toBeDisabled();
    await app.screenshot('terminal-context-menu');

    // Select all, then Copy copies the screen.
    await menu.getByRole('menuitem', 'Select all').tap();
    await expect(menu).toHaveCount(0);
    await term.secondaryTap();
    await expect(menu.getByRole('menuitem', 'Copy')).toBeEnabled();
    await menu.getByRole('menuitem', 'Copy').tap();
    await expect.poll(async () => (await copied(browser)).length).toBeGreaterThan(0);

    // Paste is the user's paste: the clipboard text goes into the pane.
    await browser.evaluate(`window.__clip = 'echo keel-menu-paste-' + 'ok'`);
    await term.secondaryTap();
    await menu.getByRole('menuitem', 'Paste').tap();
    await expect.poll(() => scrollback(app, session.id)).toContain('keel-menu-paste-ok');
    await term.press('Control+u');

    // Escape closes it and gives the terminal its focus back.
    await term.secondaryTap();
    await expect(menu).toBeVisible();
    await browser.keyboard.press('Escape');
    await expect(menu).toHaveCount(0);
    await expect(term).toBeFocused();

    // Find opens the find bar.
    await term.secondaryTap();
    await menu.getByRole('menuitem', 'Find').tap();
    await expect(screen.getByRole('textbox', 'Find in terminal', { visible: true })).toBeFocused();
    await browser.keyboard.press('Escape');

    // The migrated value is saved as the new setting.
    const page = await (async () => {
      await screen.getByRole('complementary', 'Vault').getByRole('button', 'Settings').tap();
      return screen.getByRole('region', 'Settings');
    })();
    await page.getByRole('tab', 'Terminal').tap();
    const seg = page.getByRole('group', 'Right-click in the terminal');
    await expect(seg.getByRole('button', 'Menu')).toHaveAttribute('aria-pressed', 'true');

    // Paste: right-click pastes as before, no menu.
    await seg.getByRole('button', 'Paste').tap();
    await expect(seg.getByRole('button', 'Paste')).toHaveAttribute('aria-pressed', 'true');
    await expect.poll(async () => (await prefs(browser)).rightClick).toBe('paste');
    expect((await prefs(browser)).rightClickPaste).toBeUndefined();
    await app.screenshot('settings-right-click');
    await screen.getByRole('button', /^Local Shell\b/).first().tap();
    const readsBefore = await reads(browser);
    await browser.evaluate(`window.__clip = 'echo keel-rc-paste-' + 'ok'`);
    await term.secondaryTap();
    await expect(menu).toHaveCount(0);
    await expect.poll(() => reads(browser)).toBe(readsBefore + 1);
    await expect.poll(() => scrollback(app, session.id)).toContain('keel-rc-paste-ok');
    await term.press('Control+u');

    // Nothing: no menu, no paste.
    await screen.getByRole('complementary', 'Vault').getByRole('button', 'Settings').tap();
    await page.getByRole('tab', 'Terminal').tap();
    await seg.getByRole('button', 'Nothing').tap();
    await expect.poll(async () => (await prefs(browser)).rightClick).toBe('nothing');
    await screen.getByRole('button', /^Local Shell\b/).first().tap();
    const readsNow = await reads(browser);
    await term.secondaryTap();
    await expect(menu).toHaveCount(0);
    expect(await reads(browser)).toBe(readsNow);

    // Back to the default for the other tests.
    await browser.evaluate(`localStorage.setItem('keel-term', JSON.stringify({ ...JSON.parse(localStorage.getItem('keel-term') || '{}'), rightClick: 'menu' }))`);
    await closeTabs(app, screen);
  });

  test('notebook code block: Type into the pane (approve first) and Copy', async ({ app, screen, browser }) => {
    const name = `cm-nb-${stamp()}`;
    const nb = await keelSend<Notebook>(app.baseUrl, 'POST', '/api/notebooks', { name, cells: [{ body: '```sh\necho keel-fence-menu\n```\n\n```json\n{"a": 1}\n```' }] });
    try {
      await browser.addInitScript(CLIPBOARD);
      await openKeel(app);
      const session = await openLocal(app, screen);
      const vault = screen.getByRole('complementary', 'Vault');
      await vault.getByRole('button', 'Snippets').tap();
      await vault.getByRole('button', new RegExp(`^${name}$`)).tap();
      const menu = screen.getByRole('menu');

      // A shell fence: Type into Local Shell… and Copy code.
      const typeBtn = screen.getByRole('button', 'Type into Local Shell');
      await expect(typeBtn).toBeVisible();
      await typeBtn.secondaryTap();
      await expect(menu).toBeVisible();
      await expect(menu.getByRole('menuitem', 'Type into Local Shell…')).toBeVisible();
      await expect(menu.getByRole('menuitem', 'Copy code')).toBeVisible();
      await app.screenshot('fence-context-menu');
      await menu.getByRole('menuitem', 'Copy code').tap();
      await expect.poll(() => copied(browser)).toEqual(['echo keel-fence-menu']);

      // Type goes through the same approve step; nothing is typed before.
      await typeBtn.secondaryTap();
      await menu.getByRole('menuitem', 'Type into Local Shell…').tap();
      const approve = screen.getByRole('dialog').filter({ hasText: 'Type into the terminal?' });
      await expect(approve).toBeVisible();
      await expect(approve).toContainText('echo keel-fence-menu');
      await approve.getByRole('button', 'Cancel').tap();
      await expect(approve).toBeHidden();
      await screen.getByRole('button', 'Cancel', { visible: true }).tap();
      expect(await scrollback(app, session.id)).not.toContain('keel-fence-menu');

      // The code itself is editable: it keeps the system's menu.
      await screen.getByText('echo keel-fence-menu', { exact: true }).first().secondaryTap();
      await expect(menu).toHaveCount(0);

      // A non-shell fence: Copy only.
      const copies = screen.getByRole('button', 'Copy code');
      await copies.nth(1).secondaryTap();
      await expect(menu.getByRole('menuitem', 'Copy code')).toBeVisible();
      await expect(menu.getByRole('menuitem', /^Type into/)).toBeHidden();
      await browser.keyboard.press('Escape');
      await expect(menu).toHaveCount(0);

      // ⇧F10 on the fence bar opens it with the first item focused.
      await typeBtn.focus();
      await typeBtn.press('Shift+F10');
      await expect(menu.getByRole('menuitem', 'Type into Local Shell…')).toBeFocused();
      await browser.keyboard.press('Escape');
      await expect(typeBtn).toBeFocused();
      await closeTabs(app, screen);
    } finally {
      await keelSend(app.baseUrl, 'DELETE', `/api/notebooks/${nb.id}`).catch(() => {});
    }
  });

  test('Keychain and Snippets page rows: right-click and ⇧F10', async ({ app, screen, browser }) => {
    const idName = `cm-id-${stamp()}`;
    const secret = `pw-${stamp()}-secret`;
    const id = await keelSend<Identity>(app.baseUrl, 'POST', '/api/identities', { name: idName, kind: 'password', username: 'ops', password: secret });
    const snName = `cm-snp-${stamp()}`;
    const sn = await keelSend<Snippet>(app.baseUrl, 'POST', '/api/snippets', { name: snName, command: 'uptime' });
    try {
      await openKeel(app);
      const vault = screen.getByRole('complementary', 'Vault');
      const menu = screen.getByRole('menu');
      await vault.getByRole('button', 'Keychain').tap();
      const keychain = screen.getByRole('region', 'Keychain');
      const row = keychain.getByRole('button', new RegExp(`^${idName}\\b`));
      await row.secondaryTap();
      await expect(menu.getByRole('menuitem', 'Edit')).toBeVisible();
      await expect(menu.getByRole('menuitem', 'Delete')).toBeEnabled();
      await expect(menu).not.toContainText(secret);
      await app.screenshot('identity-context-menu');
      await menu.getByRole('menuitem', 'Delete').tap();
      await expect(screen.getByRole('alertdialog')).toContainText(`Delete identity “${idName}”?`);
      await screen.getByRole('button', 'Cancel', { visible: true }).tap();
      await row.focus();
      await row.press('Shift+F10');
      await expect(menu.getByRole('menuitem', 'Edit')).toBeFocused();
      await browser.keyboard.press('Escape');
      await expect(row).toBeFocused();

      await vault.getByRole('button', 'Snippets').tap();
      await vault.getByRole('button', 'Manage snippets').tap();
      const page = screen.getByRole('region', 'Snippets');
      const snRow = page.getByRole('button', new RegExp(`^${snName}\\b`));
      await snRow.secondaryTap();
      for (const item of ['Type into pane…', 'Copy', 'Edit', 'Delete']) await expect(menu.getByRole('menuitem', item)).toBeVisible();
      await browser.keyboard.press('Escape');
      await expect(menu).toHaveCount(0);
      await closeTabs(app, screen);
    } finally {
      await keelSend(app.baseUrl, 'DELETE', `/api/identities/${id.id}`).catch(() => {});
      await keelSend(app.baseUrl, 'DELETE', `/api/snippets/${sn.id}`).catch(() => {});
    }
  });

  test('agent card: its own buttons; Type still asks approval', { tags: ['agent'] }, async ({ app, screen, browser }) => {
    await browser.addInitScript(CLIPBOARD);
    await keelSend(app.baseUrl, 'PUT', '/api/agent/keys/gemini', { key: 'e2e-not-a-real-gemini-key-0000' });
    await openKeel(app);
    const session = await openLocal(app, screen);
    await screen.getByRole('textbox', 'Terminal input', { visible: true }).press('Meta+i');
    const panel = screen.getByRole('complementary', 'Agent');
    const composer = panel.getByRole('textbox', 'Message the agent');
    await expect(composer).toBeFocused();
    await composer.fill('what is going on in this pane?');
    await composer.press('Enter');
    const card = panel.getByRole('group', 'Proposal').last();
    await expect(card).toContainText('echo keel-agent-ok');
    const menu = screen.getByRole('menu');

    await card.secondaryTap();
    await expect(menu.getByRole('menuitem', 'Edit command')).toBeVisible();
    await expect(menu.getByRole('menuitem', 'Copy command')).toBeVisible();
    await expect(menu.getByRole('menuitem', /^Type into/)).toBeVisible();
    await app.screenshot('agent-card-menu');
    await menu.getByRole('menuitem', 'Copy command').tap();
    await expect.poll(async () => (await copied(browser)).join('\n')).toContain('echo keel-agent-ok');

    // Type into pane from the menu opens the same approve dialog; nothing
    // is typed until the user approves.
    await card.secondaryTap();
    await menu.getByRole('menuitem', /^Type into/).tap();
    const approve = screen.getByRole('dialog').filter({ hasText: 'Type into the terminal?' });
    await expect(approve).toBeVisible();
    expect(await scrollback(app, session.id)).not.toContain('keel-agent-ok');
    await approve.getByRole('button', 'Cancel').tap();
    expect(await scrollback(app, session.id)).not.toContain('keel-agent-ok');

    // ⇧F10 on a card's button opens it too.
    await card.getByRole('button', 'Copy command').focus();
    await card.getByRole('button', 'Copy command').press('Shift+F10');
    await expect(menu.getByRole('menuitem', 'Edit command')).toBeFocused();
    await browser.keyboard.press('Escape');
    await expect(card.getByRole('button', 'Copy command')).toBeFocused();
    await closeTabs(app, screen);
  });
});

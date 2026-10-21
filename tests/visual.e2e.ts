import { describe, test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelApi, keelSend, keelToken, openKeel, retire, sshTest } from './keel.ts';

// Named screenshots of the main screens in both themes, for before/after
// comparisons of visual-only changes (`--output .e2e/proof/<name>`). They
// assert landmarks, not pixels: diffs are read by a person. Serial, and the
// terminal tests close their tabs: keel serve restores running sessions as
// tabs on the next page load.
const themeScript = (theme: string) =>
  `try { if (location.protocol.startsWith('http')) localStorage.setItem('keel-theme', '${theme}'); } catch {}`;

// Local Shell from the sidebar's Hosts segment (the segment is a window
// preference, so an earlier test may have left another one selected).
async function openLocalShell(screen: any) {
  const vault = screen.getByRole('complementary', 'Vault');
  await vault.getByRole('button', 'Hosts').tap();
  await vault.getByRole('button', /^Local Shell\b/).tap();
}

// The notebook shot's document (board Notebook): prose with inline code and
// a link, headings, lists, a shell and a yaml fence, and a callout.
const NOTEBOOK_MD = [
  'Replica **pg-2** falls 10–20 MB behind, and the stream drops every 30 s with `SSL SYSCALL error`. Failover rules are in the [Patroni docs](https://patroni.readthedocs.io).',
  '',
  '## Check',
  '',
  '1. Confirm what the leader sees for the replica.',
  '2. Compare `sent_lsn` with `replay_lsn`.',
  '3. If the gap grows, check the network path between the two hosts.',
  '',
  '```sh',
  'sudo -u postgres psql -xc "select client_addr, state, sent_lsn, replay_lsn, replay_lag from pg_stat_replication;"',
  '```',
  '',
  '### Notes',
  '',
  '- The leader is `pg-1` at 10.0.4.21.',
  '- TCP keepalives are set in `/etc/patroni.yml`.',
  '',
  '```yaml',
  'postgresql:',
  '  parameters:',
  '    tcp_keepalives_idle: 60',
  '```',
  '',
  '> **Before you touch anything:** if lag passes 500 MB, page the DB owner.',
].join('\n');

describe('screens', { tags: ['visual'], serial: true }, () => {
  // Context menus (k-menu): a host row's right-click and the pane bar's
  // More pane actions, the same items as their "…" buttons.
  for (const theme of ['dark', 'light'] as const) {
    test(`context menus (${theme})`, async ({ app, screen, browser }) => {
      const name = `visual-menu-${theme}`;
      const host = await keelSend<{ id: string }>(app.baseUrl, 'POST', '/api/hosts', { name, address: '10.0.4.21', port: 22, username: 'deploy', authType: 'password', password: 'visual-scratch' });
      try {
        await browser.addInitScript(themeScript(theme));
        await openKeel(app);
        const vault = screen.getByRole('complementary', 'Vault');
        const menu = screen.getByRole('menu');
        await vault.getByRole('button', 'Hosts').tap();
        await vault.getByRole('button', new RegExp(`^${name}\\b`)).secondaryTap();
        await expect(menu.getByRole('menuitem', 'Copy SSH command')).toBeVisible();
        await app.screenshot(`${theme}-host-context-menu`);
        await browser.keyboard.press('Escape');
        await expect(menu).toHaveCount(0);

        await openLocalShell(screen);
        await screen.getByRole('textbox', 'Terminal input').press('Meta+Shift+D');
        await expect(screen.getByRole('textbox', 'Terminal input')).toHaveCount(2);
        await screen.getByRole('button', 'More pane actions', { visible: true }).first().tap();
        await expect(menu.getByRole('menuitem', 'Focus view')).toBeVisible();
        await app.screenshot(`${theme}-pane-bar-menu`);
        await browser.keyboard.press('Escape');
        await expect(menu).toHaveCount(0);
        const terms = screen.getByRole('textbox', 'Terminal input');
        await terms.nth(1).press('Meta+w');
        await expect(terms).toHaveCount(1);
        await screen.getByRole('button', 'Close tab', { visible: true }).tap();
        await expect(terms).toHaveCount(0);
      } finally {
        await keelSend(app.baseUrl, 'DELETE', `/api/hosts/${host.id}`);
      }
    });
  }

  for (const theme of ['dark', 'light'] as const) {
    // The Vault sidebar (expanded, then collapsed) and each tab page.
    test(`vault sidebar and tab pages (${theme})`, async ({ app, screen, browser }) => {
      await browser.addInitScript(themeScript(theme));
      await openKeel(app);
      const vault = screen.getByRole('complementary', 'Vault');
      // The segment is a window preference: the dark run's later tests
      // leave Snippets selected for the light run.
      await vault.getByRole('button', 'Hosts').tap();
      await expect(vault.getByRole('searchbox', 'Search hosts')).toBeVisible();
      await app.screenshot(`${theme}-hosts`);

      await vault.getByRole('button', 'New host').tap();
      await expect(screen.getByRole('dialog').filter({ hasText: 'New host' })).toBeVisible();
      await app.screenshot(`${theme}-new-host`);
      await screen.getByRole('button', 'Cancel', { visible: true }).tap();

      // Edit host on a saved host, after Test connection and Trust and
      // save against the loopback SSH server (bin/keel-sshtest).
      const { target } = sshTest();
      const editName = `visual-edit-${theme}`;
      const edited = await keelSend<{ id: string }>(app.baseUrl, 'POST', '/api/hosts', {
        name: editName, address: target.host, port: target.port, username: target.user, authType: 'password', password: target.password, color: '#5ee0b5',
      });
      try {
        await openKeel(app);
        await vault.getByRole('searchbox', 'Search hosts').fill(editName);
        await expect(vault.getByRole('button', new RegExp(`^${editName}\\b`))).toBeVisible();
        // The filter is a debounced server round trip: wait for the
        // filtered tree, so this host's "Actions" is the only one left.
        await expect(vault.getByRole('button', /^Local Shell\b/)).toBeHidden();
        await vault.getByRole('button', 'Actions', { visible: true }).tap();
        await screen.getByRole('menuitem', 'Edit').tap();
        const editing = screen.getByRole('dialog').filter({ hasText: 'Edit host' });
        await expect(editing).toBeVisible();
        await editing.getByRole('button', 'Test connection').tap();
        const trusted = editing.getByRole('group', 'Host key');
        // The dark run trusts the key; the light run finds it trusted.
        await expect(editing.getByText(/^(Connected in \d+ ms|Host key unknown)$/)).toBeVisible();
        if (await editing.getByRole('button', 'Trust and save').isVisible()) {
          await editing.getByRole('button', 'Trust and save').tap();
        }
        await expect(editing.getByText(/^Connected in \d+ ms$/)).toBeVisible();
        await expect(trusted).toContainText('Trusted');
        await app.screenshot(`${theme}-edit-host`);
        await screen.getByRole('button', 'Cancel', { visible: true }).tap();
        await vault.getByRole('searchbox', 'Search hosts').fill('');
      } finally {
        await keelSend(app.baseUrl, 'DELETE', `/api/hosts/${edited.id}`);
      }

      await vault.getByRole('button', 'Snippets').tap();
      await expect(vault.getByRole('button', 'Manage snippets')).toBeVisible();
      await app.screenshot(`${theme}-sidebar-snippets`);
      await vault.getByRole('button', 'Manage snippets').tap();
      await expect(screen.getByRole('textbox', 'Snippet name')).toBeVisible();
      await app.screenshot(`${theme}-snippets`);

      await vault.getByRole('button', 'Forwards').tap();
      await vault.getByRole('button', 'Manage forwards').tap();
      await expect(screen.getByRole('region', 'Port forwarding').getByRole('button', 'New forward')).toBeVisible();
      await app.screenshot(`${theme}-forwards`);

      await vault.getByRole('button', 'Keychain').tap();
      await expect(screen.getByRole('button', 'New identity')).toBeVisible();
      await app.screenshot(`${theme}-keychain`);

      await vault.getByRole('button', 'Logs').tap();
      await expect(screen.getByRole('searchbox', 'Search events')).toBeVisible();
      await app.screenshot(`${theme}-logs`);

      await vault.getByRole('button', 'Settings').tap();
      await expect(screen.getByRole('heading', 'Appearance')).toBeVisible();
      await app.screenshot(`${theme}-settings`);

      await screen.getByRole('button', 'Toggle vault sidebar').tap();
      await expect(vault).toBeHidden();
      await app.screenshot(`${theme}-sidebar-collapsed`);
      // The sidebar state is a window preference: open it again for the
      // tests after this one.
      await screen.getByRole('button', 'Toggle vault sidebar').tap();
      await expect(vault).toBeVisible();
    });

    test(`notebooks page (${theme})`, async ({ app, screen, browser }) => {
      await browser.addInitScript(themeScript(theme));
      await openKeel(app);
      const vault = screen.getByRole('complementary', 'Vault');
      await vault.getByRole('button', 'Snippets').tap();
      await vault.getByRole('button', 'All notebooks').tap();
      await expect(screen.getByRole('region', 'Notebooks').getByRole('button', 'New notebook')).toBeVisible();
      await app.screenshot(`${theme}-notebooks`);
    });

    // A single-pane tab: full-bleed (no radius, gap or ring), the pane bar
    // on surface with no line under it, the selected tab a raised pill.
    test(`terminal and palette (${theme})`, async ({ app, screen, browser }) => {
      await browser.addInitScript(themeScript(theme));
      await openKeel(app);
      await openLocalShell(screen);
      const input = screen.getByRole('textbox', 'Terminal input');
      await expect(input).toBeFocused();
      await input.pressSequentially('printf "\\033[31mred \\033[32mgreen \\033[33myellow \\033[34mblue\\033[0m\\n"');
      await input.press('Enter');
      await app.screenshot(`${theme}-terminal`);

      await input.press('Meta+k');
      await expect(screen.getByRole('dialog', 'Command palette')).toBeVisible();
      await app.screenshot(`${theme}-palette`);
      await screen.getByRole('combobox', 'Run a command or connect a host…').press('Escape');
      await screen.getByRole('button', 'Close tab').tap();
      await expect(screen.getByRole('textbox', 'Terminal input')).toBeHidden();
    });

    // A split tab after a click into its first pane: the sidebar shows the
    // Panes section (board Panes). No assertions on it: the same test runs
    // on the base build.
    test(`split tab and panes (${theme})`, async ({ app, screen, browser }) => {
      await browser.addInitScript(themeScript(theme));
      await openKeel(app);
      await openLocalShell(screen);
      const terms = screen.getByRole('textbox', 'Terminal input');
      await expect(terms).toBeFocused();
      await terms.press('Meta+Shift+D');
      await expect(terms).toHaveCount(2);
      const box = await terms.nth(0).boundingBox();
      if (!box) throw new Error('first terminal has no box');
      await screen.tapAt({ x: box.x + box.width / 2, y: box.y + box.height / 2 });
      await expect(terms.nth(0)).toBeFocused();
      await app.screenshot(`${theme}-split-panes`);
      await terms.nth(0).press('Meta+w');
      await expect(terms).toHaveCount(1);
      await screen.getByRole('button', 'Close tab').tap();
      await expect(terms).toHaveCount(0);
    });

    // The agent panel (phase 6b) with a trace, an answer and a proposal,
    // from the test provider (KEEL_AGENT_PROVIDER=fake; no real key).
    test(`agent panel (${theme})`, async ({ app, screen, browser }) => {
      await browser.addInitScript(themeScript(theme));
      await keelSend(app.baseUrl, 'PUT', '/api/agent/keys/gemini', { key: 'e2e-not-a-real-gemini-key-0000' });
      await openKeel(app);
      await openLocalShell(screen);
      const input = screen.getByRole('textbox', 'Terminal input');
      await expect(input).toBeFocused();
      await input.pressSequentially('uptime');
      await input.press('Enter');
      await input.press('Meta+i');
      const panel = screen.getByRole('complementary', 'Agent');
      await expect(panel).toBeVisible();
      const ask = panel.getByRole('textbox', 'Message the agent');
      await ask.fill('Is this machine busy?');
      await ask.press('Enter');
      await expect(panel.getByRole('button', /Type into pane/)).toBeVisible();
      await app.screenshot(`${theme}-agent`);
      // An answer in markdown (heading, list, code block with Copy, quote,
      // table, links; its raw HTML and image stay text).
      await ask.fill('answer in markdown please');
      await ask.press('Enter');
      await expect(panel.getByRole('heading', 'Disk check')).toBeVisible();
      await browser.evaluate(`(() => { const t = document.getElementById('agent-thread'); const h = [...t.querySelectorAll('.k-md h2')].pop(); t.scrollTop = h.offsetTop - t.offsetTop - 8; })()`);
      await app.screenshot(`${theme}-agent-markdown`);
      await panel.getByRole('button', 'Close agent').tap();
      await expect(panel).toBeHidden();
      await screen.getByRole('button', 'Close tab').tap();
      await expect(screen.getByRole('textbox', 'Terminal input')).toBeHidden();
    });

    // The New tab page with a recent host and a closed tab, and the palette
    // grouped by Open now / Hosts / Actions / Snippets. No assertions on the
    // new copy: the same test runs on the base build.
    test(`new tab and palette groups (${theme})`, async ({ app, screen, browser }) => {
      await browser.addInitScript(themeScript(theme));
      await openKeel(app);
      await openLocalShell(screen);
      await expect(screen.getByRole('textbox', 'Terminal input')).toBeFocused();
      await screen.getByRole('button', 'Close tab', { visible: true }).tap();
      await expect(screen.getByRole('textbox', 'Terminal input')).toBeHidden();
      // Earlier tests leave tab pages open (they restore on load): ask for a New tab.
      await screen.getByRole('button', 'Toggle vault sidebar').press('Meta+t');
      await app.screenshot(`${theme}-newtab`);

      await openLocalShell(screen);
      const term = screen.getByRole('textbox', 'Terminal input');
      await expect(term).toBeFocused();
      await term.press('Meta+k');
      await screen.getByRole('combobox', 'Run a command or connect a host…').fill('local');
      await app.screenshot(`${theme}-palette-groups`);
      await screen.getByRole('combobox', 'Run a command or connect a host…').press('Escape');
      await screen.getByRole('button', 'Close tab').tap();
      await expect(screen.getByRole('textbox', 'Terminal input')).toBeHidden();
    });

    // A split session, focus on the new (right) pane: two radius-md tiles on
    // the chrome ground, 6px apart, pane bars on surface, a soft focus ring
    // on the focused tile and the other terminal dimmed, under the selected
    // tab's raised pill. (`split-panes` above is the same split with focus
    // on the left pane; `terminal` is a single full-bleed pane.)
    test(`split panes (${theme})`, async ({ app, screen, browser }) => {
      await browser.addInitScript(themeScript(theme));
      await openKeel(app);
      await openLocalShell(screen);
      await expect(screen.getByRole('textbox', 'Terminal input')).toBeFocused();
      await screen.getByRole('button', 'Split right', { visible: true }).tap();
      await expect(screen.getByRole('textbox', 'Terminal input')).toHaveCount(2);
      await app.screenshot(`${theme}-split`);
      await screen.getByRole('button', 'Close pane', { visible: true }).first().tap();
      await screen.getByRole('button', 'Close tab', { visible: true }).tap();
      await expect(screen.getByRole('textbox', 'Terminal input')).toBeHidden();
    });

    // Share and Join (needs scripts/e2e-serve.sh's keel-signal): the Share
    // popover, the admit prompt, the joined follower view with the typing
    // notice, and the control request. The locators accept the base build's
    // controls too (Request control, the old toasts), so the same test makes
    // the before shots. The link is read from what the page copies.
    test(`share and join (${theme})`, async ({ app, screen, browser }) => {
      await browser.addInitScript(themeScript(theme));
      await browser.addInitScript(
        `try { window.__keelCopied = []; Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: (t) => { window.__keelCopied.push(String(t)); return Promise.resolve(); } } }); } catch {}`,
      );
      await openKeel(app);
      await openLocalShell(screen);
      await expect(screen.getByRole('textbox', 'Terminal input')).toBeFocused();
      await screen.getByRole('button', 'Share').tap();
      let link = '';
      await expect.poll(async () => (link = String(await browser.evaluate('window.__keelCopied.at(-1) || ""'))), { timeout: 15_000 }).toMatch(/#k=/);
      // Let toasts finish sliding in (duration-slow) before each shot.
      const settle = async () => {
        const t = Date.now();
        await expect.poll(() => Date.now() - t, { interval: 100 }).toBeGreaterThan(400);
      };
      await settle();
      await app.screenshot(`${theme}-share`);
      const pop = screen.getByRole('dialog', 'Share session');
      if (await pop.isVisible()) await pop.press('Escape');

      await screen.getByRole('button', 'New tab').tap();
      await screen.getByRole('region', 'New tab').getByRole('button', /Join session/).tap();
      const dialog = screen.getByRole('dialog').filter({ hasText: 'Join a session' });
      await dialog.getByLabel('Join link').fill(link);
      await dialog.getByRole('button', 'Join').tap();
      const prompt = screen.getByRole('alertdialog').filter({ hasText: 'wants to watch' });
      await expect(prompt).toBeVisible({ timeout: 15_000 });
      await settle();
      await app.screenshot(`${theme}-share-admit`);
      await prompt.getByRole('button', 'Allow').tap();

      const ask = screen.getByRole('button', /^(Ask for control|Request control)$/, { visible: true }).first();
      await expect(ask).toBeVisible({ timeout: 30_000 });
      const term = screen.getByRole('textbox', 'Terminal input', { visible: true });
      await term.tap();
      await browser.keyboard.type('ls');
      await settle();
      await app.screenshot(`${theme}-share-follower`);
      await ask.tap();
      await expect(screen.getByText(/e2e (asks for|requested) control/).first()).toBeVisible({ timeout: 10_000 });
      await settle();
      await app.screenshot(`${theme}-share-request`);

      // Closing the joined tab, then the owner's, ends the share.
      await screen.getByRole('button', 'Close tab', { visible: true }).tap();
      // The owner's tab label comes before the sidebar's host row.
      await screen.getByText('Local Shell', { exact: true }).first().tap();
      await screen.getByRole('button', 'Close tab', { visible: true }).tap();
      await expect(screen.getByRole('textbox', 'Terminal input', { visible: true })).toBeHidden();
    });

    // An ended session: the pane bar keeps its dead dot and a flare banner
    // with Reconnect sits under it.
    test(`ended session banner (${theme})`, async ({ app, screen, browser }) => {
      await browser.addInitScript(themeScript(theme));
      await openKeel(app);
      await openLocalShell(screen);
      const input = screen.getByRole('textbox', 'Terminal input');
      await expect(input).toBeFocused();
      await input.pressSequentially('exit');
      await input.press('Enter');
      await expect(screen.getByRole('button', /Reconnect|Start over/, { visible: true })).toBeVisible();
      await app.screenshot(`${theme}-ended`);
      await screen.getByRole('button', 'Close tab', { visible: true }).tap();
      await expect(screen.getByRole('button', /Reconnect|Start over/, { visible: true })).toBeHidden();
    });

    // Phase 6d screens that have no base counterpart (last, so the numbered
    // shots above keep their names): Settings · Agent with a connected
    // provider, and the Logs page filtered to Agent. Not run on the base build.
    test(`agent settings and logs filter (${theme})`, async ({ app, screen, browser }) => {
      await browser.addInitScript(themeScript(theme));
      await keelSend(app.baseUrl, 'PUT', '/api/agent/keys/gemini', { key: 'e2e-not-a-real-gemini-key-0000' });
      await keelSend(app.baseUrl, 'PUT', '/api/agent/settings', { suggestions: 'on', detection: 'auto', defaultScope: 'focused' });
      await openKeel(app);
      const vault = screen.getByRole('complementary', 'Vault');
      await vault.getByRole('button', 'Settings').tap();
      await screen.getByRole('region', 'Settings').getByRole('tab', 'Agent').tap();
      await expect(screen.getByRole('region', 'Settings').getByRole('group', 'Google Gemini')).toContainText('Connected');
      await app.screenshot(`${theme}-settings-agent`);
      await vault.getByRole('button', 'Logs').tap();
      await screen.getByRole('group', 'Show events').getByRole('button', 'Agent').tap();
      await app.screenshot(`${theme}-logs-agent`);
      await keelSend(app.baseUrl, 'DELETE', '/api/agent/keys/gemini');
    });

    // The notebook pane beside a session (board Notebook): title, meta line,
    // headings, lists, inline code, a shell and a yaml fence, and a callout.
    // Last, so the shots above keep their numbers. The locators hold on the
    // base build too (no assertions on the new chrome).
    test(`notebook pane (${theme})`, async ({ app, screen, browser }) => {
      await browser.addInitScript(themeScript(theme));
      const name = `pg-2 replication lag ${theme}`;
      const nb = await keelSend<{ id: string }>(app.baseUrl, 'POST', '/api/notebooks', { name, cells: [{ body: NOTEBOOK_MD }] });
      try {
        await openKeel(app);
        await openLocalShell(screen);
        await expect(screen.getByRole('textbox', 'Terminal input')).toBeFocused();
        const vault = screen.getByRole('complementary', 'Vault');
        await vault.getByRole('button', 'Snippets').tap();
        await vault.getByRole('button', new RegExp(`^${name}$`)).tap();
        await expect(screen.getByRole('textbox', 'Rename notebook')).toHaveValue(name);
        await app.screenshot(`${theme}-notebook`);
        await screen.getByRole('button', 'Close pane', { visible: true }).first().tap();
        await expect(screen.getByRole('textbox', 'Terminal input')).toBeHidden();
        await screen.getByRole('button', 'Close tab', { visible: true }).tap();
        await expect(screen.getByRole('textbox', 'Rename notebook')).toBeHidden();
      } finally {
        await keelSend(app.baseUrl, 'DELETE', `/api/notebooks/${nb.id}`);
      }
    });

    // Light-theme coverage (issue #55), last so the shots above keep their
    // numbers: all 16 ANSI colours, the New identity dialog, a confirm
    // alertdialog, and the agent panel's Recent conversations list and an
    // Expired proposal card (board AgentSessions).
    test(`ansi, dialogs and agent conversations (${theme})`, async ({ app, screen, browser }) => {
      await browser.addInitScript(themeScript(theme));
      await keelSend(app.baseUrl, 'PUT', '/api/agent/keys/gemini', { key: 'e2e-not-a-real-gemini-key-0000' });
      await keelSend(app.baseUrl, 'DELETE', '/api/agent/conversations');
      await openKeel(app);
      const vault = screen.getByRole('complementary', 'Vault');

      await openLocalShell(screen);
      const input = screen.getByRole('textbox', 'Terminal input');
      await expect(input).toBeFocused();
      await input.pressSequentially('for i in 0 1 2 3 4 5 6 7; do printf "\\033[3${i}m ansi-$i \\033[9${i}m ansi-$((i+8)) \\033[0m\\n"; done');
      await input.press('Enter');
      await app.screenshot(`${theme}-terminal-ansi`);

      // A conversation with a proposal for this pane, then enough others
      // that keel serve drops it from memory (it keeps 16): resumed, its
      // proposal is Expired.
      const sessions = await keelApi<{ id: string }[]>(app.baseUrl, '/api/sessions');
      const pane = sessions[sessions.length - 1].id;
      await expect
        .poll(async () => {
          try {
            await keelSend(app.baseUrl, 'POST', '/api/agent/ask', { conversationId: `conv-visual-${theme}-${Date.now()}`, question: 'expired visual please touch a file', panes: [pane] });
            return true;
          } catch {
            return false;
          }
        })
        .toBe(true);
      for (let i = 0; i < 17; i++) {
        await keelSend(app.baseUrl, 'POST', '/api/agent/ask', { conversationId: `conv-visual-fill-${i}-${Date.now()}`, question: `filler ${i}` });
      }
      await input.press('Meta+i');
      const panel = screen.getByRole('complementary', 'Agent');
      await expect(panel).toBeVisible();
      await panel.getByRole('button', 'Recent conversations').tap();
      const pop = panel.getByRole('dialog', 'Recent conversations');
      await expect(pop).toContainText('expired visual please touch a file');
      await app.screenshot(`${theme}-agent-conversations`);
      await pop.getByRole('searchbox', 'Search conversations').fill('expired visual');
      await pop.getByRole('button', /^expired visual please touch a file/).tap();
      await expect(panel.getByRole('group', 'Proposal').first()).toContainText('Expired');
      await app.screenshot(`${theme}-agent-expired`);

      await panel.getByRole('button', 'Close agent').tap();
      await keelSend(app.baseUrl, 'DELETE', '/api/agent/conversations');
      await screen.getByRole('button', 'Close tab', { visible: true }).tap();
      await expect(screen.getByRole('textbox', 'Terminal input')).toBeHidden();

      // The dialogs last: the Settings page's masked key field would
      // otherwise show through the terminal shots.
      await vault.getByRole('button', 'Keychain').tap();
      await screen.getByRole('region', 'Keychain').getByRole('button', 'New identity').tap();
      await expect(screen.getByRole('dialog').filter({ hasText: 'New identity' })).toBeVisible();
      await app.screenshot(`${theme}-new-identity`);
      await screen.getByRole('button', 'Cancel', { visible: true }).tap();

      await vault.getByRole('button', 'Settings').tap();
      const settings = screen.getByRole('region', 'Settings');
      await settings.getByRole('tab', 'Agent').tap();
      await settings.getByRole('group', 'Google Gemini').getByRole('button', 'Disconnect').tap();
      const confirm = screen.getByRole('alertdialog');
      await expect(confirm).toBeVisible();
      await app.screenshot(`${theme}-confirm`);
      await confirm.getByRole('button', 'Cancel').tap();
      await expect(confirm).toBeHidden();
      await keelSend(app.baseUrl, 'DELETE', '/api/agent/keys/gemini');
    });
  }
});

// The Agent tab page (§5.4.2, board AgentPage): a live conversation, then
// one whose proposal is Expired.
describe('agent page', { tags: ['visual'], serial: true }, () => {
  for (const theme of ['dark', 'light'] as const) {
    test(`agent page (${theme})`, async ({ app, screen, browser }) => {
      await browser.addInitScript(themeScript(theme));
      await keelSend(app.baseUrl, 'PUT', '/api/agent/keys/gemini', { key: 'e2e-not-a-real-gemini-key-0000' });
      await keelSend(app.baseUrl, 'DELETE', '/api/agent/conversations');
      await openKeel(app);
      await openLocalShell(screen);
      const input = screen.getByRole('textbox', 'Terminal input');
      await expect(input).toBeFocused();
      const sessions = await keelApi<{ id: string }[]>(app.baseUrl, '/api/sessions');
      const pane = sessions[sessions.length - 1].id;

      // The expired one: asked, then pushed out of memory by 17 others
      // (keel serve keeps 16), which are deleted again.
      await expect
        .poll(async () => {
          try {
            await keelSend(app.baseUrl, 'POST', '/api/agent/ask', { conversationId: `conv-visual-page-old-${theme}-${Date.now()}`, question: 'Clean up the build scratch, please touch a file', panes: [pane] });
            return true;
          } catch {
            return false;
          }
        })
        .toBe(true);
      const fillers: string[] = [];
      for (let i = 0; i < 17; i++) {
        fillers.push(`conv-visual-page-fill-${i}-${Date.now()}`);
        await keelSend(app.baseUrl, 'POST', '/api/agent/ask', { conversationId: fillers[i], question: `filler ${i}` });
      }
      for (const id of fillers) await keelSend(app.baseUrl, 'DELETE', `/api/agent/conversations/${id}`);
      const live = `conv-visual-page-live-${theme}-${Date.now()}`;
      await keelSend(app.baseUrl, 'POST', '/api/agent/ask', { conversationId: live, question: 'Why is the replica lagging behind?', panes: [pane] });
      await keelSend(app.baseUrl, 'POST', '/api/agent/ask', { conversationId: live, question: 'Is it the network or the disk? no command', panes: [pane] });

      await input.press('Meta+k');
      const palette = screen.getByRole('dialog', 'Command palette');
      await palette.getByRole('combobox').fill('Agent conversations');
      await palette.getByRole('option').filter({ hasText: 'Agent conversations' }).first().tap();
      const page = screen.getByRole('region', 'Agent');
      const list = page.getByRole('navigation', 'Conversations');
      await list.getByRole('button', /^Why is the replica lagging behind/).tap();
      await expect(page.getByRole('region', 'Conversation').getByRole('group', 'Proposal')).toHaveCount(1);
      await expect(page.getByRole('complementary', 'Conversation context')).toContainText('echo keel-agent-ok');
      await app.screenshot(`${theme}-agent-page`);

      await list.getByRole('button', /^Clean up the build scratch/).tap();
      await expect(page.getByRole('region', 'Conversation').getByRole('group', 'Proposal').first()).toContainText('Expired');
      await app.screenshot(`${theme}-agent-page-expired`);

      await keelSend(app.baseUrl, 'DELETE', '/api/agent/conversations');
      await keelSend(app.baseUrl, 'DELETE', '/api/agent/keys/gemini');
      for (let i = 0; i < 3; i++) {
        const close = screen.getByRole('button', 'Close tab', { visible: true });
        if (!(await close.count())) break;
        await close.first().tap();
      }
      // keel serve restores running sessions as tabs on the next load.
      await keelSend(app.baseUrl, 'DELETE', `/api/sessions/${pane}`).catch(() => {});
    });
  }
});
// Notes for the agent (§15, boards HostNotes and MemoryPropose): Edit host
// with its notes (two written by the user, one the agent proposed and the
// user saved), then the agent panel with a note proposal from pane output.
describe('host notes', { tags: ['visual'], serial: true }, () => {
  for (const theme of ['dark', 'light'] as const) {
    test(`host notes (${theme})`, async ({ app, screen, browser }) => {
      await browser.addInitScript(themeScript(theme));
      await keelSend(app.baseUrl, 'PUT', '/api/agent/keys/gemini', { key: 'e2e-not-a-real-gemini-key-0000' });
      await keelSend(app.baseUrl, 'DELETE', '/api/agent/conversations');
      await keelSend(app.baseUrl, 'DELETE', '/api/agent/notes');
      const hosts = await keelApi<{ id: string; authType: string }[]>(app.baseUrl, '/api/hosts');
      const local = hosts.find((h) => h.authType === 'local')!;
      await keelSend(app.baseUrl, 'POST', '/api/agent/notes', { hostId: local.id, text: 'Patroni cluster "main". Config is `/etc/patroni.yml`; run `patronictl` as `sudo -u postgres`.' });
      await keelSend(app.baseUrl, 'POST', '/api/agent/notes', { hostId: local.id, text: 'Do not restart postgres during 08:00–20:00 ICT; page the on-call first.' });
      await openKeel(app);
      await openLocalShell(screen);
      const input = screen.getByRole('textbox', 'Terminal input');
      await expect(input).toBeFocused();
      const sessions = await keelApi<{ id: string }[]>(app.baseUrl, '/api/sessions');
      const pane = sessions[sessions.length - 1].id;
      // One proposal saved through the API, so the list shows an agent note.
      const conv = `conv-visual-notes-${theme}-${Date.now()}`;
      const res = await keelSend<{ notes: { id: string }[] }>(app.baseUrl, 'POST', '/api/agent/ask', { conversationId: conv, question: 'remember this, no command', panes: [pane] });
      await keelSend(app.baseUrl, 'POST', `/api/agent/conversations/${conv}/notes/${res.notes[0].id}`, {});

      // Edit host: the notes column.
      const vault = screen.getByRole('complementary', 'Vault');
      await vault.getByRole('button', 'Actions', { visible: true }).first().tap();
      await screen.getByRole('menuitem', 'Edit').tap();
      const dialog = screen.getByRole('dialog').filter({ hasText: 'Edit host' });
      await expect(dialog.getByRole('region', 'Notes for the agent').getByRole('listitem')).toHaveCount(3);
      await app.screenshot(`${theme}-host-notes`);
      await dialog.getByRole('button', 'Cancel').tap();
      await expect(dialog).toBeHidden();

      // The panel: a note from pane output waits for Save anyway.
      await input.press('Meta+i');
      const panel = screen.getByRole('complementary', 'Agent');
      await expect(panel).toBeVisible();
      const ask = panel.getByRole('textbox', 'Message the agent');
      await ask.fill('Why does replication restart? remember from the pane, no command');
      await ask.press('Enter');
      await expect(panel.getByRole('group', 'Proposed note')).toContainText('From pane output');
      await expect(panel).toContainText('Used host notes · Local Shell (3)');
      await app.screenshot(`${theme}-note-proposal`);

      await panel.getByRole('button', 'Close agent').tap();
      await screen.getByRole('button', 'Close tab').tap();
      await expect(screen.getByRole('textbox', 'Terminal input')).toBeHidden();
      await keelSend(app.baseUrl, 'DELETE', '/api/agent/notes');
      await keelSend(app.baseUrl, 'DELETE', '/api/agent/conversations');
      await keelSend(app.baseUrl, 'DELETE', '/api/agent/keys/gemini');
      await keelSend(app.baseUrl, 'DELETE', `/api/sessions/${pane}`).catch(() => {});
    });
  }
});

// Runbooks (§16, boards Runbooks and RunbookInUse): the page with a
// runbook open, a runbook in use in the panel with a step proposed, and a
// runbook the agent proposes.
describe('runbooks', { tags: ['visual'], serial: true }, () => {
  for (const theme of ['dark', 'light'] as const) {
    test(`runbooks (${theme})`, async ({ app, screen, browser }) => {
      await browser.addInitScript(themeScript(theme));
      await keelSend(app.baseUrl, 'PUT', '/api/agent/keys/gemini', { key: 'e2e-not-a-real-gemini-key-0000' });
      await keelSend(app.baseUrl, 'DELETE', '/api/agent/conversations');
      for (const rb of (await keelApi<{ runbooks: { id: string }[] }>(app.baseUrl, '/api/agent/runbooks')).runbooks) {
        await keelSend(app.baseUrl, 'DELETE', `/api/agent/runbooks/${rb.id}`);
      }
      const lag = await keelSend<{ runbook: { id: string } }>(app.baseUrl, 'POST', '/api/agent/runbooks', {
        name: 'Diagnose replication lag',
        when: 'A Patroni replica is behind or its WAL stream keeps restarting.',
        // No hosts: Local Shell is in no folder; the step pins the run to it.
        hosts: [],
        agentMayPick: true,
        steps: [
          { title: 'Check replication on the leader', description: 'Read lag and state for every replica.', command: 'sudo -u postgres psql -xc "select client_addr, state, replay_lag from pg_stat_replication;"', expect: 'state streaming' },
          { title: 'Look for network errors on the replica', description: 'SSL timeouts point at the network, not Postgres.', command: 'sudo journalctl -u patroni --since "30 min ago" | grep -iE "timeout|ssl"' },
          { title: 'Restart Patroni on the replica', description: 'Only after the log shows the stream is stuck.', command: 'sudo systemctl restart patroni' },
          { title: 'Ask before changing conntrack' },
        ],
      });
      // A runbook made from a notebook draft: keel serve sets its source,
      // so it waits for a review.
      const nb = await keelSend<{ id: string }>(app.baseUrl, 'POST', '/api/notebooks', {
        name: 'nginx 502 checklist',
        cells: [{ body: '## Find the failing upstream\n\nThe error log names the upstream and the reason.\n\n```sh\nsudo tail -n 50 /var/log/nginx/error.log\n```\n' }],
      });
      const draft = await keelSend<Record<string, unknown>>(app.baseUrl, 'POST', '/api/agent/runbooks/draft', { notebookId: nb.id });
      await keelSend(app.baseUrl, 'POST', '/api/agent/runbooks', { ...draft, when: 'nginx returns 502 or 504 for an upstream app.', hosts: ['nginx'] });
      await keelSend(app.baseUrl, 'DELETE', `/api/notebooks/${nb.id}`);
      await openKeel(app);
      await openLocalShell(screen);
      const input = screen.getByRole('textbox', 'Terminal input');
      await expect(input).toBeFocused();

      // The page, with the first runbook open.
      await input.press('Meta+k');
      const palette = screen.getByRole('dialog', 'Command palette');
      await palette.getByRole('combobox').fill('Runbooks');
      await palette.getByRole('option').filter({ hasText: 'Runbooks' }).first().tap();
      const page = screen.getByRole('region', 'Runbooks');
      await page.getByRole('button', 'Open Diagnose replication lag').tap();
      await expect(page.getByRole('textbox', 'Step 4 title')).toBeVisible();
      await app.screenshot(`${theme}-runbooks`);
      await page.getByRole('button', 'Open nginx 502 checklist').tap();
      await expect(page).toContainText('Not enabled yet. Review before enabling.');
      await app.screenshot(`${theme}-runbooks-review`);

      // In use: Use in agent proposes step 1 in the panel.
      await page.getByRole('button', 'Open Diagnose replication lag').tap();
      await page.getByRole('button', 'Use in agent').tap();
      const panel = screen.getByRole('complementary', 'Agent');
      await expect(panel.getByRole('group', 'Runbook Diagnose replication lag')).toBeVisible();
      await expect(panel.getByRole('group', 'Proposal').last()).toContainText('Step 1 · Run on');
      // The run card, as on the board, at the top of the thread.
      await browser.evaluate("document.querySelector('#agent-thread .k-rb-pick')?.scrollIntoView({ block: 'start' })");
      await app.screenshot(`${theme}-runbook-in-use`);

      // A runbook the agent proposes.
      const ask = panel.getByRole('textbox', 'Message the agent');
      await panel.getByRole('button', "Don't use").tap();
      await ask.fill('save this as a runbook');
      await ask.press('Enter');
      await expect(panel.getByRole('group', 'Proposed runbook')).toContainText('After reading panes');
      await app.screenshot(`${theme}-runbook-proposal`);

      await panel.getByRole('button', 'Close agent').tap();
      for (let i = 0; i < 3; i++) {
        const close = screen.getByRole('button', 'Close tab', { visible: true });
        if (!(await close.count())) break;
        await close.first().tap();
      }
      await keelSend(app.baseUrl, 'DELETE', '/api/agent/conversations');
      for (const rb of (await keelApi<{ runbooks: { id: string }[] }>(app.baseUrl, '/api/agent/runbooks')).runbooks) {
        await keelSend(app.baseUrl, 'DELETE', `/api/agent/runbooks/${rb.id}`);
      }
      await keelSend(app.baseUrl, 'DELETE', '/api/agent/keys/gemini');
      for (const s of await keelApi<{ id: string }[]>(app.baseUrl, '/api/sessions')) await keelSend(app.baseUrl, 'DELETE', `/api/sessions/${s.id}`).catch(() => {});
      void lag;
    });
  }
});

// Notebooks and the agent (§18): a notebook attached to the question; the
// agent's edit card and the edited notebook with its review banner; History
// with the diff; a private notebook (meta line, Notebooks list); and the
// delete card the user must confirm.
describe('agent notebooks', { tags: ['visual'], serial: true }, () => {
  for (const theme of ['dark', 'light'] as const) {
    test(`agent notebooks (${theme})`, async ({ app, screen, browser }) => {
      await browser.addInitScript(themeScript(theme));
      await keelSend(app.baseUrl, 'PUT', '/api/agent/keys/gemini', { key: 'e2e-not-a-real-gemini-key-0000' });
      await keelSend(app.baseUrl, 'DELETE', '/api/agent/conversations');
      const nb = await keelSend<{ id: string }>(app.baseUrl, 'POST', '/api/notebooks', {
        name: 'Patroni failover',
        cells: [{ body: NOTEBOOK_MD + '\n\nStatus: keel-edit-me\n' }],
      });
      const priv = await keelSend<{ id: string }>(app.baseUrl, 'POST', '/api/notebooks', { name: 'Break-glass keys', cells: [{ body: 'Kept from the agent.' }] });
      await keelSend(app.baseUrl, 'PUT', `/api/notebooks/${priv.id}/private`, { private: true });
      const old = await keelSend<{ id: string }>(app.baseUrl, 'POST', '/api/notebooks', { name: 'Old failover notes', cells: [{ body: 'Replaced.' }] });
      await openKeel(app);
      await openLocalShell(screen);
      const input = screen.getByRole('textbox', 'Terminal input');
      await expect(input).toBeFocused();
      await input.press('Meta+i');
      const panel = screen.getByRole('complementary', 'Agent');
      await panel.getByRole('button', 'Attach notebook').tap();
      await panel.getByRole('dialog', 'Notebooks to send with your question').getByRole('menuitemcheckbox', /Patroni failover/).tap();
      await app.screenshot(`${theme}-agent-attach-notebook`);
      await panel.getByRole('dialog', 'Notebooks to send with your question').press('Escape');

      // The agent edits the attached notebook: the card, and the pane.
      const ask = panel.getByRole('textbox', 'Message the agent');
      await ask.fill('edit the notebook patroni failover');
      await ask.press('Enter');
      const card = panel.getByRole('group', 'Edited notebook');
      await expect(card).toContainText('Keel kept the version before it');
      await browser.evaluate("document.querySelector('#agent-thread .k-prop--notebook')?.scrollIntoView({ block: 'end' })");
      await app.screenshot(`${theme}-notebook-edit-card`);
      await card.getByRole('button', 'Open').tap();
      await expect(screen.getByRole('note', 'Agent edit to review')).toBeVisible();
      await app.screenshot(`${theme}-notebook-agent-edited`);

      // History with the diff from the version before the edit.
      await screen.getByRole('note', 'Agent edit to review').getByRole('button', 'View changes').tap();
      const hist = screen.getByRole('dialog', /History · Patroni failover/);
      await expect(hist).toContainText(/lines? added/);
      await app.screenshot(`${theme}-notebook-history`);
      await hist.getByRole('button', 'Close').tap();

      // A delete the agent proposes.
      await ask.fill('drop the notebook old failover notes');
      await ask.press('Enter');
      await expect(panel.getByRole('group', 'Delete notebook?')).toContainText('Nothing is deleted unless you confirm');
      await browser.evaluate("[...document.querySelectorAll('#agent-thread .k-prop--notebook')].at(-1)?.scrollIntoView({ block: 'end' })");
      await app.screenshot(`${theme}-notebook-delete-card`);
      await panel.getByRole('button', 'Close agent').tap();

      // A private notebook: the meta line and the Notebooks list.
      const vault = screen.getByRole('complementary', 'Vault');
      await vault.getByRole('button', 'Snippets').tap();
      await vault.getByRole('button', /^Break-glass keys$/).tap();
      await expect(screen.getByRole('button', 'Private')).toHaveAttribute('aria-pressed', 'true');
      await app.screenshot(`${theme}-notebook-private`);
      await screen.getByRole('textbox', 'Rename notebook').last().press('Meta+k');
      const palette = screen.getByRole('dialog', 'Command palette');
      await palette.getByRole('combobox').fill('Notebooks');
      await palette.getByRole('option').filter({ hasText: 'Notebooks' }).first().tap();
      await expect(screen.getByRole('region', 'Notebooks')).toContainText('Private');
      await app.screenshot(`${theme}-notebooks-private-list`);

      for (let i = 0; i < 6; i++) {
        const pane = screen.getByRole('button', 'Close pane', { visible: true });
        const tab = screen.getByRole('button', 'Close tab', { visible: true });
        if (await pane.count()) await pane.last().tap();
        else if (await tab.count()) await tab.first().tap();
        else break;
      }
      for (const id of [nb.id, priv.id, old.id]) await keelSend(app.baseUrl, 'DELETE', `/api/notebooks/${id}`).catch(() => {});
      await keelSend(app.baseUrl, 'DELETE', '/api/agent/conversations');
      await keelSend(app.baseUrl, 'DELETE', '/api/agent/keys/gemini');
      for (const s of await keelApi<{ id: string }[]>(app.baseUrl, '/api/sessions')) await keelSend(app.baseUrl, 'DELETE', `/api/sessions/${s.id}`).catch(() => {});
    });
  }
});

// Groups, part 1 (§17, boards GroupRoles and GroupRunbook): the group page
// with roles and a note, a run across two Local Shell hosts with one host
// done and the other waiting, and the group approve dialog with one row
// per host.
describe('groups', { tags: ['visual'], serial: true }, () => {
  for (const theme of ['dark', 'light'] as const) {
    test(`groups (${theme})`, async ({ app, screen, browser }) => {
      await browser.addInitScript(themeScript(theme));
      for (const rb of (await keelApi<{ runbooks: { id: string }[] }>(app.baseUrl, '/api/agent/runbooks')).runbooks) {
        await keelSend(app.baseUrl, 'DELETE', `/api/agent/runbooks/${rb.id}`);
      }
      const hosts = await keelApi<{ id: string; name: string }[]>(app.baseUrl, '/api/hosts');
      const ids: string[] = [];
      for (const name of ['vx-cp-1', 'vx-wk-1', 'vx-wk-2']) {
        const have = hosts.find((h) => h.name === name);
        if (have) await keelSend(app.baseUrl, 'PUT', `/api/hosts/${have.id}`, { ...have, authType: 'local', agentReadable: true });
        ids.push(have ? have.id : (await keelSend<{ id: string }>(app.baseUrl, 'POST', '/api/hosts', { name, authType: 'local', agentReadable: true })).id);
      }
      const groups = await keelApi<{ id: string; name: string }[]>(app.baseUrl, '/api/groups');
      const group = groups.find((g) => g.name === 'k8s-visual') ?? (await keelSend<{ id: string }>(app.baseUrl, 'POST', '/api/groups', { name: 'k8s-visual' }));
      await keelSend(app.baseUrl, 'POST', '/api/hosts/move', { ids, groupId: group.id });
      const roles = ['control-plane', 'worker', 'worker'];
      for (let i = 0; i < ids.length; i++) await keelSend(app.baseUrl, 'PUT', `/api/groups/${group.id}/roles`, { hostId: ids[i], role: roles[i] });
      const notes = await keelApi<{ notes: { id: string; groupId?: string }[] }>(app.baseUrl, '/api/agent/notes');
      for (const n of notes.notes.filter((x) => x.groupId === group.id)) await keelSend(app.baseUrl, 'DELETE', `/api/agent/notes/${n.id}`);
      await keelSend(app.baseUrl, 'POST', '/api/agent/notes', { groupId: group.id, text: 'Kubernetes 1.31 via kubeadm. Drain a worker before restarting containerd.' });
      await keelSend(app.baseUrl, 'POST', '/api/agent/runbooks', {
        name: 'Rolling restart kubelet on workers',
        steps: [
          { title: 'Restart kubelet on each worker', command: "touch /tmp/keel-vx-kubelet; printf '\\033]133;C\\007active\\n\\033]133;D;0\\007'", role: 'worker' },
          { title: 'Look for pods in CrashLoopBackOff', command: 'kubectl get pods -A | grep -i crashloop', role: 'control-plane', oneHost: true },
          { title: 'Check the dashboards' },
        ],
      });
      await openKeel(app);
      await screen.getByRole('button', 'New host', { visible: true }).first().press('Meta+k');
      const palette = screen.getByRole('dialog', 'Command palette');
      await palette.getByRole('combobox').fill('Group k8s-visual');
      await palette.getByRole('option').filter({ hasText: 'Group k8s-visual' }).first().tap();
      const page = screen.getByRole('region', 'Group');
      await expect(page).toContainText('Drain a worker');
      await app.screenshot(`${theme}-group-page`);

      await page.getByRole('button', 'Run a runbook on this group').tap();
      await page.getByRole('menuitem', /Rolling restart kubelet/).tap();
      const run = screen.getByRole('region', 'Group run');
      await run.getByRole('button', 'Parallel').tap();
      for (const name of ['vx-wk-1', 'vx-wk-2']) {
        await run.getByRole('button', `Open a pane on ${name}`).tap();
        await expect(screen.getByRole('textbox', 'Terminal input', { visible: true })).toBeFocused();
        await screen.getByRole('button', /Run · Rolling restart/).first().tap();
      }
      const typeBtn = run.getByRole('button', /^Type into 2 panes/);
      await expect(typeBtn).toBeEnabled({ timeout: 10_000 });
      await typeBtn.tap();
      const dlg = screen.getByRole('dialog').filter({ hasText: 'Type into these panes?' });
      await dlg.getByRole('checkbox', 'Type into vx-wk-1').tap();
      await app.screenshot(`${theme}-group-approve`);
      await dlg.getByRole('button', 'Type into 1 pane').tap();
      await expect(run.getByRole('row', /^vx-wk-1/)).toContainText('✓ exit 0', { timeout: 15_000 });
      await app.screenshot(`${theme}-group-run`);

      await run.getByRole('button', 'Stop runbook').first().tap();
      await screen.getByRole('alertdialog').getByRole('button', 'Stop runbook').tap();
      for (let i = 0; i < 6; i++) {
        const close = screen.getByRole('button', 'Close tab', { visible: true });
        if (!(await close.count())) break;
        await close.first().tap();
      }
      for (const s of await keelApi<{ id: string }[]>(app.baseUrl, '/api/sessions')) await keelSend(app.baseUrl, 'DELETE', `/api/sessions/${s.id}`).catch(() => {});
      for (const rb of (await keelApi<{ runbooks: { id: string }[] }>(app.baseUrl, '/api/agent/runbooks')).runbooks) {
        await keelSend(app.baseUrl, 'DELETE', `/api/agent/runbooks/${rb.id}`);
      }
      await retire(app, /^vx-/, 'k8s-visual');
    });
  }
});

// Groups, part 2 (§17, boards CaptureVars, AnsibleExport and the GroupRoles
// drawer): group variables and the Ansible export drawer on the group page,
// a run whose step 1 captured a value, and the approve dialog with the
// values filled in.
describe('group variables', { tags: ['visual'], serial: true }, () => {
  for (const theme of ['dark', 'light'] as const) {
    test(`group variables and export (${theme})`, async ({ app, screen, browser }) => {
      await browser.addInitScript(themeScript(theme));
      for (const rb of (await keelApi<{ runbooks: { id: string }[] }>(app.baseUrl, '/api/agent/runbooks')).runbooks) {
        await keelSend(app.baseUrl, 'DELETE', `/api/agent/runbooks/${rb.id}`);
      }
      const hosts = await keelApi<{ id: string; name: string }[]>(app.baseUrl, '/api/hosts');
      const ids: string[] = [];
      for (const name of ['vv-cp-1', 'vv-wk-1']) {
        const have = hosts.find((h) => h.name === name);
        ids.push(have ? have.id : (await keelSend<{ id: string }>(app.baseUrl, 'POST', '/api/hosts', { name, authType: 'local', agentReadable: true })).id);
      }
      const groups = await keelApi<{ id: string; name: string }[]>(app.baseUrl, '/api/groups');
      const group = groups.find((g) => g.name === 'k8s-vars') ?? (await keelSend<{ id: string }>(app.baseUrl, 'POST', '/api/groups', { name: 'k8s-vars' }));
      await keelSend(app.baseUrl, 'POST', '/api/hosts/move', { ids, groupId: group.id });
      await keelSend(app.baseUrl, 'PUT', `/api/groups/${group.id}/roles`, { hostId: ids[0], role: 'control-plane' });
      await keelSend(app.baseUrl, 'PUT', `/api/groups/${group.id}/roles`, { hostId: ids[1], role: 'worker' });
      for (const [name, value] of [['k8s_version', '1.31.2'], ['api_vip', '10.20.0.10'], ['cri', 'containerd']]) {
        await keelSend(app.baseUrl, 'PUT', `/api/groups/${group.id}/vars`, { name, value });
      }
      await keelSend(app.baseUrl, 'POST', '/api/agent/runbooks', {
        name: 'Bootstrap k8s-vars',
        steps: [
          {
            title: 'kubeadm init on the control plane',
            command: `touch /tmp/keel-vv-token; t=abcdef; printf '\\033]133;C\\007kubeadm join 10.20.0.10:6443 --code %s.0123456789abcdef\\n\\033]133;D;0\\007' "$t"`,
            role: 'control-plane',
            oneHost: true,
            captures: [{ name: 'join_code', pattern: '--code\\s+(\\S+)' }],
          },
          { title: 'Join the workers', command: `touch /tmp/keel-vv-join; printf '\\033]133;C\\007joined %s %s\\n\\033]133;D;0\\007' {{join_code}} {{k8s_version}}`, role: 'worker' },
        ],
      });
      await openKeel(app);
      await screen.getByRole('button', 'New host', { visible: true }).first().press('Meta+k');
      const palette = screen.getByRole('dialog', 'Command palette');
      await palette.getByRole('combobox').fill('Group k8s-vars');
      await palette.getByRole('option').filter({ hasText: 'Group k8s-vars' }).first().tap();
      const page = screen.getByRole('region', 'Group');
      await expect(page.getByRole('region', 'Group variables')).toContainText('containerd');
      await page.getByRole('button', 'Export to Ansible').tap();
      const drawer = screen.getByRole('complementary', 'Export to Ansible');
      await expect(drawer).toContainText('[control_plane]');
      await app.screenshot(`${theme}-group-export`);
      await drawer.getByRole('button', 'Close').tap();

      await page.getByRole('button', 'Run a runbook on this group').tap();
      await page.getByRole('menuitem', /Bootstrap k8s-vars/).tap();
      const run = screen.getByRole('region', 'Group run');
      const back = () => screen.getByRole('button', /Run · Bootstrap k8s-vars/).first().tap();
      await run.getByRole('button', 'Open a pane on vv-cp-1').tap();
      await expect(screen.getByRole('textbox', 'Terminal input', { visible: true })).toBeFocused();
      await back();
      const typeBtn = run.getByRole('button', /^Type into 1 pane/);
      await expect(typeBtn).toBeEnabled({ timeout: 10_000 });
      await typeBtn.tap();
      const dlg = screen.getByRole('dialog').filter({ hasText: 'Type into these panes?' });
      await dlg.getByRole('button', 'Type into 1 pane').tap();
      await expect(run.getByRole('row', /^vv-cp-1/)).toContainText('✓ exit 0', { timeout: 15_000 });
      await run.getByRole('button', 'Open a pane on vv-wk-1').tap();
      await expect(screen.getByRole('textbox', 'Terminal input', { visible: true })).toBeFocused();
      await back();
      await expect(run).toContainText('Uses 1 group variable and 1 captured value');
      await expect(typeBtn).toBeEnabled({ timeout: 10_000 });
      await app.screenshot(`${theme}-group-capture`);
      await typeBtn.tap();
      await expect(dlg).toContainText('filled in by Keel');
      await app.screenshot(`${theme}-group-approve-filled`);
      await dlg.getByRole('button', 'Cancel').tap();

      await run.getByRole('button', 'Stop runbook').first().tap();
      await screen.getByRole('alertdialog').getByRole('button', 'Stop runbook').tap();
      for (let i = 0; i < 6; i++) {
        const close = screen.getByRole('button', 'Close tab', { visible: true });
        if (!(await close.count())) break;
        await close.first().tap();
      }
      for (const s of await keelApi<{ id: string }[]>(app.baseUrl, '/api/sessions')) await keelSend(app.baseUrl, 'DELETE', `/api/sessions/${s.id}`).catch(() => {});
      for (const rb of (await keelApi<{ runbooks: { id: string }[] }>(app.baseUrl, '/api/agent/runbooks')).runbooks) {
        await keelSend(app.baseUrl, 'DELETE', `/api/agent/runbooks/${rb.id}`);
      }
      await retire(app, /^vv-/, 'k8s-vars');
    });
  }
});

// Settings · Appearance as the native window shows it: Window translucency
// is there only with data-native. (Playwright cannot show the OS backdrop;
// this is the page's half.)
describe('native appearance settings', { tags: ['visual'], serial: true }, () => {
  for (const theme of ['dark', 'light'] as const) {
    test(`appearance settings, native window (${theme})`, async ({ app, screen, browser }) => {
      await browser.addInitScript(themeScript(theme));
      await app.open(`/?keel_token=${keelToken()}&name=e2e&native=1&appearance=native&chrome=titlebar`);
      await screen.getByRole('complementary', 'Vault').getByRole('button', 'Settings').tap();
      await expect(screen.getByRole('heading', 'Appearance')).toBeVisible();
      await expect(screen.getByRole('group', 'Window translucency').getByRole('button', 'Medium')).toHaveAttribute('aria-pressed', 'true');
      await app.screenshot(`${theme}-settings-appearance-native`);
    });
  }
});

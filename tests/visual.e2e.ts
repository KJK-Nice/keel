import { describe, test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelSend, openKeel, sshTest } from './keel.ts';

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

describe('screens', { tags: ['visual'], serial: true }, () => {
  for (const theme of ['dark', 'light'] as const) {
    // The Vault sidebar (expanded, then collapsed) and each tab page.
    test(`vault sidebar and tab pages (${theme})`, async ({ app, screen, browser }) => {
      await browser.addInitScript(themeScript(theme));
      await openKeel(app);
      const vault = screen.getByRole('complementary', 'Vault');
      await expect(vault.getByRole('button', 'New host')).toBeVisible();
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

    // A split session: one focused pane bar (surface + keel line) next to an
    // unfocused one (chrome, muted), under the selected tab's keel line.
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
  }
});

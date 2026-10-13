import { describe, test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { openKeel } from './keel.ts';

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
  }
});

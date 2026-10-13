import { describe, test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { openKeel } from './keel.ts';

// Named screenshots of the main screens in both themes, for before/after
// comparisons of visual-only changes (`--output .e2e/proof/<name>`). They
// assert landmarks, not pixels: diffs are read by a person. Serial, and the
// terminal test closes its tab: keel serve restores running sessions as
// tabs on the next page load.
const themeScript = (theme: string) =>
  `try { if (location.protocol.startsWith('http')) localStorage.setItem('keel-theme', '${theme}'); } catch {}`;

describe('screens', { tags: ['visual'], serial: true }, () => {
  for (const theme of ['dark', 'light'] as const) {
    test(`vault pages (${theme})`, async ({ app, screen, browser }) => {
      await browser.addInitScript(themeScript(theme));
      await openKeel(app);
      await expect(screen.getByRole('button', 'New host')).toBeVisible();
      await app.screenshot(`${theme}-hosts`);

      await screen.getByRole('button', 'New host').tap();
      await expect(screen.getByRole('dialog').filter({ hasText: 'New host' })).toBeVisible();
      await app.screenshot(`${theme}-new-host`);
      await screen.getByRole('button', 'Cancel', { visible: true }).tap();

      await screen.getByRole('button', 'Snippets', { visible: true }).first().tap();
      await expect(screen.getByRole('button', 'New snippet')).toBeVisible();
      await app.screenshot(`${theme}-snippets`);

      await screen.getByRole('button', 'Settings', { visible: true }).first().tap();
      await expect(screen.getByRole('heading', 'Appearance')).toBeVisible();
      await app.screenshot(`${theme}-settings`);
    });

    test(`terminal and palette (${theme})`, async ({ app, screen, browser }) => {
      await browser.addInitScript(themeScript(theme));
      await openKeel(app);
      await screen.getByRole('button', 'Terminal').tap();
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
  }
});

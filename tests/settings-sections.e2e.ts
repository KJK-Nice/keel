import { describe, test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelApi, keelToken, openKeel } from './keel.ts';

// Settings sections beyond Agent (docs/ui-redesign.md §5.9): Keyboard,
// Sharing, Vault & security, Runtime and About Keel. Each shows only what
// Keel does today. Stop runtime is only opened to its alertdialog and
// cancelled: confirming would end the app under test.

const themeScript = (theme: string) =>
  `try { if (location.protocol.startsWith('http')) localStorage.setItem('keel-theme', '${theme}'); } catch {}`;
const mod = process.platform === 'darwin' ? '⌘' : 'Ctrl+';

async function openSettings(screen: any) {
  const vault = screen.getByRole('complementary', 'Vault');
  await vault.getByRole('button', 'Settings').tap();
  return screen.getByRole('region', 'Settings');
}

describe('settings sections', { tags: ['settings'], serial: true }, () => {
  for (const theme of ['dark', 'light'] as const) {
    test(`every section renders (${theme})`, async ({ app, screen, browser }) => {
      await browser.addInitScript(themeScript(theme));
      await openKeel(app);
      const page = await openSettings(screen);

      // Nav matches the board: seven sections and the About footer.
      for (const name of ['Appearance', 'Terminal', 'Keyboard', 'Agent', 'Sharing', 'Vault & security', 'Runtime']) {
        await expect(page.getByRole('tab', name)).toBeVisible();
      }
      await expect(page.getByRole('tab', /^About Keel/)).toBeVisible();

      // Keyboard: read-only, from the palette's table.
      await page.getByRole('tab', 'Keyboard').tap();
      await expect(page.getByRole('heading', 'Keyboard')).toBeVisible();
      await expect(page.getByRole('img', `${mod}T`)).toBeVisible();
      await expect(page.getByRole('img', `${mod}⇧T`)).toBeVisible();
      await expect(page.getByRole('img', process.platform === 'darwin' ? '⌘I' : 'Ctrl+Shift+I')).toBeVisible();
      await expect(page.getByRole('img', process.platform === 'darwin' ? '⌘Z' : 'Ctrl+Shift+Z')).toBeVisible();
      await app.screenshot(`${theme}-settings-keyboard`);

      // Sharing: the signal server e2e-serve.sh started is shown.
      await page.getByRole('tab', 'Sharing').tap();
      await expect(page.getByRole('heading', 'Sharing')).toBeVisible();
      await expect(page.getByLabel('Display name')).toBeVisible();
      await expect(page.getByText(/^http:\/\/127\.0\.0\.1:\d+$/)).toBeVisible();
      await expect(page).toContainText('Joining needs your approval');
      await app.screenshot(`${theme}-settings-sharing`);

      // Vault & security: how it is unlocked, and where. No secret.
      await page.getByRole('tab', 'Vault & security').tap();
      await expect(page.getByRole('heading', 'Vault & security')).toBeVisible();
      await expect(page).toContainText('Unlocked');
      await expect(page).toContainText('Passphrase');
      await expect(page).toContainText('keel.vault');
      await app.screenshot(`${theme}-settings-vault`);

      // Runtime: status from GET /api/runtime.
      await page.getByRole('tab', 'Runtime').tap();
      await expect(page.getByRole('heading', 'Runtime')).toBeVisible();
      const rt = await keelApi<{ build: string; addr: string }>(app.baseUrl, '/api/runtime');
      await expect(page).toContainText(rt.build);
      await expect(page).toContainText(rt.addr);
      await expect(page).toContainText('Sessions keep running after the window closes');
      await app.screenshot(`${theme}-settings-runtime`);

      // Stop asks first and says it ends sessions. Cancel leaves it running.
      await page.getByRole('button', 'Stop runtime…').tap();
      const dialog = screen.getByRole('alertdialog');
      await expect(dialog).toContainText('Stop the Keel runtime?');
      await expect(dialog).toContainText('ends');
      await app.screenshot(`${theme}-settings-runtime-stop`);
      await dialog.getByRole('button', 'Cancel').tap();
      await expect(dialog).toBeHidden();
      expect((await keelApi<{ status: string }>(app.baseUrl, '/api/health')).status).toBe('ok');

      // About Keel: version, build and links.
      await page.getByRole('tab', /^About Keel/).tap();
      await expect(page.getByRole('heading', 'About Keel')).toBeVisible();
      await expect(page).toContainText(rt.build);
      await expect(page).toContainText('https://github.com/KJK-Nice/keel');
      await app.screenshot(`${theme}-settings-about`);
    });
  }

  test('search finds a setting in another section, and the name round-trips', async ({ app, screen }) => {
    await openKeel(app);
    const page = await openSettings(screen);
    const search = page.getByRole('searchbox', 'Search settings');

    // Appearance is selected; "uptime" lives in Runtime.
    await search.fill('uptime');
    await expect(page.getByRole('heading', 'Runtime')).toBeVisible();
    await expect(page.getByText('Uptime')).toBeVisible();
    await expect(page.getByRole('heading', 'Appearance')).toBeHidden();
    // A shortcut is found by its key.
    await search.fill(process.platform === 'darwin' ? '⌘I' : 'Ctrl+Shift+I');
    await expect(page.getByRole('heading', 'Keyboard')).toBeVisible();
    await search.fill('zzzz-no-such-setting');
    await expect(page).toContainText('No settings match.');
    await search.fill('');
    await expect(page.getByRole('heading', 'Appearance')).toBeVisible();

    // The display name is kept and used next time.
    await page.getByRole('tab', 'Sharing').tap();
    const name = page.getByLabel('Display name');
    await name.fill('Ada');
    await name.press('Enter');
    await openKeel(app); // a fresh load: ?name=e2e is the default, the saved name wins
    const again = await openSettings(screen);
    await again.getByRole('tab', 'Sharing').tap();
    await expect(again.getByLabel('Display name')).toHaveValue('Ada');
    // Clearing it goes back to the default.
    await again.getByLabel('Display name').fill('');
    await again.getByLabel('Display name').press('Enter');
    await expect(again.getByLabel('Display name')).toHaveValue('');
  });

  // Window opacity and blur are native-window settings: a browser tab has
  // no backdrop and never shows them, and only the macOS window
  // (appearance=native) sets a blur. Both are kept and applied before first
  // paint on the next load.
  test('window opacity and blur show only in the native window and persist', async ({ app, screen, browser }) => {
    await openKeel(app);
    let page = await openSettings(screen);
    await expect(page.getByRole('heading', 'Appearance')).toBeVisible();
    await expect(page.getByRole('slider', 'Window opacity')).toBeHidden();
    await expect(page.getByRole('slider', 'Window blur')).toBeHidden();
    for (const q of ['opacity', 'blur']) {
      await page.getByRole('searchbox', 'Search settings').fill(q);
      await expect(page).toContainText('No settings match.');
    }
    await page.getByRole('searchbox', 'Search settings').fill('');

    // Windows and Linux: opacity, no blur.
    await app.open(`/?keel_token=${keelToken()}&name=e2e&native=1`);
    page = await openSettings(screen);
    await expect(page.getByRole('slider', 'Window opacity')).toBeVisible();
    await expect(page.getByRole('slider', 'Window blur')).toBeHidden();
    await page.getByRole('searchbox', 'Search settings').fill('blur');
    await expect(page).toContainText('No settings match.');
    await page.getByRole('searchbox', 'Search settings').fill('');

    const native = `/?keel_token=${keelToken()}&name=e2e&native=1&appearance=native&chrome=titlebar`;
    await app.open(native);
    page = await openSettings(screen);
    const opacity = page.getByRole('slider', 'Window opacity');
    const blur = page.getByRole('slider', 'Window blur');
    await expect(opacity).toBeVisible();
    await expect(opacity).toHaveAttribute('aria-valuenow', '66');
    await expect(blur).toHaveAttribute('aria-valuenow', '20');
    await opacity.focus();
    await opacity.press('ArrowLeft');
    await expect(opacity).toHaveAttribute('aria-valuenow', '65');
    await expect(page).toContainText('65');
    expect(await browser.evaluate('document.documentElement.dataset.opacity')).toBe('65');
    expect(await browser.evaluate(`getComputedStyle(document.documentElement).getPropertyValue('--window-opacity').trim()`)).toBe('65%');
    await blur.focus();
    await blur.press('Home');
    await expect(blur).toHaveAttribute('aria-valuenow', '0');
    await app.screenshot('settings-opacity-blur-native');

    // 100 is applied live: the opaque rendering, no data-vibrancy.
    await opacity.focus();
    await opacity.press('End');
    await expect(opacity).toHaveAttribute('aria-valuenow', '100');
    expect(await browser.evaluate('document.documentElement.dataset.vibrancy ?? null')).toBe(null);

    await app.open(native);
    expect(await browser.evaluate('document.documentElement.dataset.opacity')).toBe('100');
    expect(await browser.evaluate('document.documentElement.dataset.blur')).toBe('0');
    expect(await browser.evaluate('document.documentElement.dataset.vibrancy ?? null')).toBe(null);
    page = await openSettings(screen);
    await expect(page.getByRole('slider', 'Window opacity')).toHaveAttribute('aria-valuenow', '100');
    await expect(page.getByRole('slider', 'Window blur')).toHaveAttribute('aria-valuenow', '0');
    // Back to the defaults for the tests after this one.
    await browser.evaluate(`(() => {
      const ui = JSON.parse(localStorage.getItem('keel-ui') || '{}');
      delete ui.opacity; delete ui.blur; delete ui.translucency;
      localStorage.setItem('keel-ui', JSON.stringify(ui));
    })()`);
    await app.open(native);
    expect(await browser.evaluate('document.documentElement.dataset.vibrancy ?? null')).toBe('1');
  });
});

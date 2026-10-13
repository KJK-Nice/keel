import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { openKeel } from './keel.ts';

// Phase 2 IA (docs/ui-redesign.md §4): the Vault is a sidebar on every tab,
// and Keychain, Port forwarding, Snippets, Notebooks, Logs and Settings are
// tab pages, one instance each.

test('each tab page opens once from the sidebar footer and segments', { tags: ['sidebar'] }, async ({ app, screen }) => {
  await openKeel(app);
  const vault = screen.getByRole('complementary', 'Vault');
  const tabStrip = (name: string) => screen.getByRole('button', name).filter({ hasText: name });

  await vault.getByRole('button', 'Keychain').tap();
  await expect(screen.getByRole('region', 'Keychain')).toBeVisible();
  await expect(screen.getByRole('button', 'New identity')).toBeVisible();
  // Reopening selects the open tab instead of adding a second one: the
  // footer button and the one tab are the only two named Keychain.
  await vault.getByRole('button', 'Keychain').tap();
  await expect(tabStrip('Keychain')).toHaveCount(2);

  await vault.getByRole('button', 'Logs').tap();
  await expect(screen.getByRole('region', 'Logs')).toBeVisible();
  await expect(screen.getByRole('searchbox', 'Search events')).toBeVisible();

  await vault.getByRole('button', 'Settings').tap();
  await expect(screen.getByRole('heading', 'Appearance')).toBeVisible();

  await vault.getByRole('button', 'Snippets').tap();
  await expect(vault.getByRole('button', 'Snippets')).toHaveAttribute('aria-pressed', 'true');
  await expect(vault.getByRole('button', /^disk\b/)).toBeVisible();
  await vault.getByRole('button', 'Manage snippets').tap();
  await expect(screen.getByRole('region', 'Snippets')).toBeVisible();
  await expect(screen.getByRole('textbox', 'Snippet name')).toBeVisible();

  await vault.getByRole('button', 'All notebooks').tap();
  await expect(screen.getByRole('region', 'Notebooks')).toBeVisible();

  await vault.getByRole('button', 'Forwards').tap();
  await expect(vault.getByRole('button', 'Forwards')).toHaveAttribute('aria-pressed', 'true');
  await vault.getByRole('button', 'Manage forwards').tap();
  await expect(screen.getByRole('region', 'Port forwarding')).toBeVisible();
  await expect(screen.getByRole('region', 'Port forwarding').getByRole('button', 'New forward')).toBeVisible();
});

test('a reload brings back running sessions and open tab pages as tabs', { tags: ['sidebar'] }, async ({ app, screen }) => {
  await openKeel(app);
  const vault = screen.getByRole('complementary', 'Vault');
  await vault.getByRole('button', 'Hosts').tap();
  await vault.getByRole('button', /^Local Shell\b/).tap();
  await expect(screen.getByRole('textbox', 'Terminal input')).toBeFocused();
  await vault.getByRole('button', 'Keychain').tap();
  await expect(screen.getByRole('region', 'Keychain')).toBeVisible();

  await openKeel(app);
  // Keychain was the selected tab and comes back selected, beside the session.
  await expect(screen.getByRole('region', 'Keychain')).toBeVisible();
  await expect(vault.getByRole('button', /^Local Shell 1\b/)).toBeVisible();
  await screen.getByRole('button', 'Local Shell', { visible: true }).tap();
  await expect(screen.getByRole('textbox', 'Terminal input')).toBeVisible();
  await screen.getByRole('button', 'Close tab', { visible: true }).tap();
  await expect(screen.getByRole('textbox', 'Terminal input')).toBeHidden();
});

test('Cmd+1 selects the first tab, Cmd+9 the last, Cmd+B folds the sidebar', { tags: ['sidebar'] }, async ({ app, screen }) => {
  await openKeel(app);
  const vault = screen.getByRole('complementary', 'Vault');
  const toggle = screen.getByRole('button', 'Toggle vault sidebar');
  await expect(screen.getByRole('heading', 'New tab')).toBeVisible();

  await vault.getByRole('button', 'Keychain').tap();
  await expect(screen.getByRole('region', 'Keychain')).toBeVisible();

  // The window opened on a New tab, so it is tab 1; Keychain is the last.
  await toggle.press('Meta+1');
  await expect(screen.getByRole('heading', 'New tab')).toBeVisible();
  await expect(screen.getByRole('region', 'Keychain')).toBeHidden();
  await toggle.press('Meta+9');
  await expect(screen.getByRole('region', 'Keychain')).toBeVisible();

  // Cmd+N on a tab page is that page's New.
  await toggle.press('Meta+n');
  await expect(screen.getByRole('dialog').filter({ hasText: 'New identity' })).toBeVisible();
  await screen.getByRole('button', 'Cancel', { visible: true }).tap();

  await expect(toggle).toHaveAttribute('aria-pressed', 'true');
  await toggle.press('Meta+b');
  await expect(vault).toBeHidden();
  await expect(toggle).toHaveAttribute('aria-pressed', 'false');
  await toggle.tap();
  await expect(vault).toBeVisible();
  await expect(toggle).toHaveAttribute('aria-pressed', 'true');
});

import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { openKeel } from './keel.ts';

// Doctor: keel serve starts, the token trades for a cookie, the window shows
// the Vault sidebar on Hosts beside a New tab (no session to restore).
test('the window opens with the Vault sidebar on Hosts', { tags: ['smoke'] }, async ({ app, screen, browser }) => {
  await openKeel(app);
  await expect(browser).toHaveURL('/?name=e2e');
  const vault = screen.getByRole('complementary', 'Vault');
  await expect(vault).toBeVisible();
  await expect(vault.getByRole('button', 'New host')).toBeVisible();
  await expect(vault.getByRole('searchbox', 'Search hosts')).toBeVisible();
  await expect(vault.getByRole('button', 'Hosts')).toHaveAttribute('aria-pressed', 'true');
  await expect(screen.getByRole('heading', 'New tab')).toBeVisible();
});

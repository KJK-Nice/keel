import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { openKeel } from './keel.ts';

// Doctor: keel serve starts, the token trades for a cookie, the Vault renders.
test('the Vault opens on Hosts', { tags: ['smoke'] }, async ({ app, screen, browser }) => {
  await openKeel(app);
  await expect(browser).toHaveURL('/?name=e2e');
  await expect(screen.getByRole('button', 'New host')).toBeVisible();
  await expect(screen.getByRole('searchbox', 'Search hosts')).toBeVisible();
});

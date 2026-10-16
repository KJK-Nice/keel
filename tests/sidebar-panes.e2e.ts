import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { openKeel } from './keel.ts';

// The sidebar's Panes section (docs/ui-redesign.md §5.1, board Panes): it
// exists only while the active tab has 2+ panes, opens when you click into
// a pane, lists the tab's panes with the focused one current, and a row or
// a map cell focuses its pane and gives its terminal the keyboard.

test('Panes appears for a split tab, follows pane focus and falls back to Hosts', { tags: ['sidebar'] }, async ({ app, screen }) => {
  await openKeel(app);
  const vault = screen.getByRole('complementary', 'Vault');
  const panesSeg = vault.getByRole('button', 'Panes');
  const hostsSeg = vault.getByRole('button', 'Hosts');
  const terms = screen.getByRole('textbox', 'Terminal input');

  await hostsSeg.tap();
  await vault.getByRole('button', /^Local Shell\b/).tap();
  await expect(terms).toHaveCount(1);
  await expect(terms.first()).toBeFocused();
  // One pane: no Panes segment.
  await expect(panesSeg).toBeHidden();

  // Split right: the segment appears, the sidebar stays on Hosts.
  await terms.first().press('Meta+Shift+D');
  await expect(terms).toHaveCount(2);
  await expect(panesSeg).toBeVisible();
  await expect(hostsSeg).toHaveAttribute('aria-pressed', 'true');

  // A click into the first pane opens Panes with that pane current, and
  // keeps the keyboard in its terminal.
  const box = await terms.nth(0).boundingBox();
  if (!box) throw new Error('first terminal has no box');
  await screen.tapAt({ x: box.x + box.width / 2, y: box.y + box.height / 2 });
  await expect(panesSeg).toHaveAttribute('aria-pressed', 'true');
  await expect(terms.nth(0)).toBeFocused();
  await expect(vault.getByRole('searchbox', 'Filter panes')).toBeVisible();
  const rows = vault.getByRole('button', /^Local Shell\b/);
  await expect(rows).toHaveCount(2);
  await expect(rows.nth(0)).toHaveAttribute('aria-current', 'true');
  await expect(vault.getByRole('button', 'Focus pane 1, Local Shell')).toHaveAttribute('aria-current', 'true');
  await app.screenshot('sidebar-panes');

  // A row focuses its pane and hands it the keyboard.
  await rows.nth(1).tap();
  await expect(rows.nth(1)).toHaveAttribute('aria-current', 'true');
  await expect(terms.nth(1)).toBeFocused();

  // So does a cell of the layout map.
  await vault.getByRole('button', 'Focus pane 1, Local Shell').tap();
  await expect(rows.nth(0)).toHaveAttribute('aria-current', 'true');
  await expect(terms.nth(0)).toBeFocused();

  // The filter narrows the rows, never the map.
  const filter = vault.getByRole('searchbox', 'Filter panes');
  await filter.pressSequentially('no-such-pane');
  await expect(rows).toHaveCount(0);
  await expect(vault.getByText('No panes match.')).toBeVisible();
  await expect(vault.getByRole('button', 'Focus pane 2, Local Shell')).toBeVisible();
  await filter.press('Escape');
  await expect(rows).toHaveCount(2);

  // Back to one pane: the segment goes and the sidebar falls back to Hosts.
  await terms.nth(0).press('Meta+w');
  await expect(terms).toHaveCount(1);
  await expect(panesSeg).toBeHidden();
  await expect(hostsSeg).toHaveAttribute('aria-pressed', 'true');
  await expect(vault.getByRole('searchbox', 'Search hosts')).toBeVisible();

  await screen.getByRole('button', 'Close tab', { visible: true }).tap();
  await expect(terms).toHaveCount(0);
});

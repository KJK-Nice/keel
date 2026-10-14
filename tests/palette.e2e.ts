import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelApi, openKeel } from './keel.ts';

// Phase 3 command palette (docs/ui-redesign.md §5.2): groups in the order
// Open now, Hosts, Actions, Snippets; picking an open tab focuses it; ⌥↵
// opens a host in a split; a snippet row always goes through Approve first.

type Session = { id: string };

const palette = (screen: any) => screen.getByRole('dialog', 'Command palette');
const combo = (screen: any) => screen.getByRole('combobox', 'Run a command or connect a host…');

async function openLocalShell(screen: any) {
  const vault = screen.getByRole('complementary', 'Vault');
  await vault.getByRole('button', 'Hosts').tap();
  await vault.getByRole('button', /^Local Shell\b/).tap();
  await expect(screen.getByRole('textbox', 'Terminal input')).toBeFocused();
}

async function closeSessionTab(screen: any) {
  await screen.getByRole('button', 'Close tab', { visible: true }).tap();
  await expect(screen.getByRole('textbox', 'Terminal input')).toBeHidden();
}

test('the palette lists Open now, Hosts, Actions, then Snippets', { tags: ['palette'] }, async ({ app, screen }) => {
  await openKeel(app);
  await openLocalShell(screen);
  await screen.getByRole('textbox', 'Terminal input').press('Meta+k');
  await expect(palette(screen)).toBeVisible();
  await expect(combo(screen)).toBeFocused();

  const options = palette(screen).getByRole('option');
  // Open now first: the session tab, with its place and role; the last row
  // is a snippet, marked Approve first.
  await expect(options.first()).toContainText('Local Shell');
  await expect(options.first()).toContainText('tab 1');
  await expect(options.first()).toContainText('driving');
  await expect(options.first()).toContainText(/⌘\s*1/);
  await expect(options.last()).toContainText('Approve first');
  for (const group of ['Open now', 'Hosts', 'Actions']) {
    await expect(palette(screen).getByText(group, { exact: true })).toBeVisible();
  }
  // Hosts name the action: a new session, not a duplicate of the open one.
  await expect(palette(screen).getByRole('option').filter({ hasText: 'New session on Local Shell' })).toBeVisible();
  await expect(palette(screen).getByRole('option').filter({ hasText: 'New tab' }).first()).toBeVisible();
  // The agent is a later phase: no row for it.
  await expect(palette(screen).getByText(/ask the agent/i)).toHaveCount(0);
  await expect(palette(screen)).toContainText('open in split');
  await app.screenshot('palette-groups');

  await combo(screen).press('Escape');
  await expect(palette(screen)).toBeHidden();
  await closeSessionTab(screen);
});

test('picking an open tab focuses it instead of starting another session', { tags: ['palette'] }, async ({ app, screen }) => {
  await openKeel(app);
  await openLocalShell(screen);
  const before = await keelApi<Session[]>(app.baseUrl, '/api/sessions');

  // A second tab (the New tab page) takes focus away from the session.
  await screen.getByRole('textbox', 'Terminal input').press('Meta+t');
  await expect(screen.getByRole('region', 'New tab').getByRole('heading', 'Where to?')).toBeVisible();
  await expect(screen.getByRole('textbox', 'Terminal input')).toBeHidden();

  await screen.getByRole('region', 'New tab').getByRole('textbox', 'Host to connect to').press('Meta+k');
  await combo(screen).fill('local shell');
  const open = palette(screen).getByRole('option').filter({ hasText: 'tab 1' });
  await expect(open).toBeVisible();
  await combo(screen).press('Enter');

  await expect(palette(screen)).toBeHidden();
  await expect(screen.getByRole('textbox', 'Terminal input')).toBeFocused();
  const after = await keelApi<Session[]>(app.baseUrl, '/api/sessions');
  expect(after.map((s) => s.id).sort()).toEqual(before.map((s) => s.id).sort());
  await closeSessionTab(screen);
});

test('Alt+Enter opens a host in a split of the focused pane', { tags: ['palette'] }, async ({ app, screen }) => {
  await openKeel(app);
  await openLocalShell(screen);
  const before = await keelApi<Session[]>(app.baseUrl, '/api/sessions');

  await screen.getByRole('textbox', 'Terminal input').press('Meta+k');
  await combo(screen).fill('new session on local');
  await expect(palette(screen).getByRole('option')).toHaveCount(1);
  await combo(screen).press('Alt+Enter');

  await expect(palette(screen)).toBeHidden();
  await expect.poll(async () => (await keelApi<Session[]>(app.baseUrl, '/api/sessions')).length).toBe(before.length + 1);
  // One tab, now with two panes.
  await expect(screen.getByRole('textbox', 'Terminal input')).toHaveCount(2);
  await app.screenshot('palette-split');

  // The palette now lists the tab and both panes under Open now.
  await screen.getByRole('textbox', 'Terminal input').first().press('Meta+k');
  await expect(palette(screen).getByRole('option').filter({ hasText: /pane in tab \d/ })).toHaveCount(2);
  await combo(screen).press('Escape');
  // Cmd+W closes the focused pane, then the tab with the last one.
  const terminal = screen.getByRole('textbox', 'Terminal input');
  await terminal.first().press('Meta+w');
  await expect(terminal).toHaveCount(1);
  // The same shortcut within 300 ms is dropped as a key repeat.
  await new Promise((r) => setTimeout(r, 400));
  await terminal.first().press('Meta+w');
  await expect(terminal).toBeHidden();
});

test('a snippet row asks Approve first and types nothing until you approve', { tags: ['palette'] }, async ({ app, screen }) => {
  await openKeel(app);
  await openLocalShell(screen);
  const sessions = await keelApi<Session[]>(app.baseUrl, '/api/sessions');
  const id = sessions[sessions.length - 1].id;
  const scrollback = async () => (await keelApi<{ text: string }>(app.baseUrl, `/api/sessions/${id}/scrollback`)).text;

  const input = screen.getByRole('textbox', 'Terminal input');
  await input.press('Meta+k');
  await combo(screen).fill('snippet: memory');
  const row = palette(screen).getByRole('option').filter({ hasText: 'Type snippet: memory' });
  await expect(row).toContainText('Approve first');
  await expect(row).toContainText('free -h');
  await combo(screen).press('Enter');

  // The approval names the exact bytes; nothing has been typed yet.
  const approve = screen.getByRole('dialog').filter({ hasText: 'Type into the terminal?' });
  await expect(approve).toBeVisible();
  await expect(approve).toContainText('free -h');
  expect(await scrollback()).not.toContain('free -h');

  // Cancel: still nothing in the terminal.
  await approve.getByRole('button', 'Cancel').tap();
  await expect(approve).toBeHidden();
  await new Promise((r) => setTimeout(r, 500));
  expect(await scrollback()).not.toContain('free -h');

  // Approve: the line is typed and run.
  await input.press('Meta+k');
  await combo(screen).fill('snippet: memory');
  await combo(screen).press('Enter');
  await expect(approve).toBeVisible();
  await approve.getByRole('button', 'Type').tap();
  await expect.poll(scrollback, { timeout: 10_000 }).toContain('free -h');
  await closeSessionTab(screen);
});

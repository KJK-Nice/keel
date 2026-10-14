import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelApi, openKeel } from './keel.ts';

// Share and Join (docs/ui-redesign.md §5.6, §5.7). The runner's keel serve
// has a loopback keel-signal (scripts/e2e-serve.sh), so sharing works. The
// joiner is this same window: it pastes the link into Join session, which
// runs the joiner's code (lib/joinpeer.js over WebRTC to keel serve's share
// hub) exactly as another Keel would. Both sides are named "e2e".

type Session = { id: string };

async function openLocalShell(app: any, screen: any): Promise<Session> {
  const before = await keelApi<Session[]>(app.baseUrl, '/api/sessions');
  const vault = screen.getByRole('complementary', 'Vault');
  await vault.getByRole('button', 'Hosts').tap();
  await vault.getByRole('button', /^Local Shell\b/).tap();
  await expect(screen.getByRole('textbox', 'Terminal input', { visible: true })).toBeFocused();
  let now: Session[] = [];
  await expect.poll(async () => (now = await keelApi<Session[]>(app.baseUrl, '/api/sessions')).length).toBe(before.length + 1);
  return now.find((s) => !before.some((b) => b.id === s.id))!;
}

const scrollback = async (app: any, id: string) =>
  (await keelApi<{ text: string }>(app.baseUrl, `/api/sessions/${id}/scrollback`)).text;

async function shareLink(screen: any): Promise<string> {
  await screen.getByRole('button', 'Share').tap();
  const pop = screen.getByRole('dialog', 'Share session');
  await expect(pop).toBeVisible();
  const link = pop.getByRole('textbox', 'Join link');
  await expect(link).toHaveValue(/\/join\?room=[^#]+#k=.+/, { timeout: 15_000 });
  return link.inputValue();
}

async function joinFromNewTab(screen: any, url: string) {
  await screen.getByRole('button', 'New tab').tap();
  await screen.getByRole('region', 'New tab').getByRole('button', /Join session/).tap();
  const dialog = screen.getByRole('dialog').filter({ hasText: 'Join a session' });
  await dialog.getByLabel('Join link').fill(url);
  await dialog.getByRole('button', 'Join').tap();
}

// Headless browsers refuse clipboard writes, so record what the page asks
// to copy. The real clipboard (and keel serve's pbcopy) is not checked.
const recordClipboard = `try {
  window.__keelCopied = [];
  const clip = navigator.clipboard || {};
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: Object.assign(Object.create(clip), {
    writeText: (t) => { window.__keelCopied.push(String(t)); return Promise.resolve(); },
  }) });
} catch {}`;

test('the Share popover shows the link, the people and Stop sharing', { tags: ['share'] }, async ({ app, screen, browser }) => {
  await browser.addInitScript(recordClipboard);
  await openKeel(app);
  const session = await openLocalShell(app, screen);

  const url = await shareLink(screen);
  const pop = screen.getByRole('dialog', 'Share session');
  await expect(pop.getByText('Live link')).toBeVisible();
  await expect(pop).toContainText("People with the link see this pane's terminal only");
  await expect(pop).toContainText('Works until you stop sharing or close this pane, 24 hours at most.');
  await expect(pop).toContainText('In this session · 1', { ignoreCase: true });
  await expect(pop.getByText('owner · this window')).toBeVisible();
  await expect(pop.getByText('Driving')).toBeVisible();
  await app.screenshot('share-popover');

  // Opening copies the link once (as the old Share button did); Copy
  // copies it again and says so.
  await expect(pop.getByRole('button', 'Copied')).toBeVisible();
  await browser.evaluate('window.__keelCopied.length = 0');
  await pop.getByRole('button', 'Copied').tap();
  await expect(pop.getByRole('button', 'Copied')).toBeVisible();
  expect(await browser.evaluate('window.__keelCopied')).toEqual([url]);

  // Escape closes it; reopening shows the same live link.
  await pop.getByRole('textbox', 'Join link').press('Escape');
  await expect(pop).toBeHidden();
  expect(await shareLink(screen)).toBe(url);

  // Stop sharing ends the share: the next Share makes a new room.
  await pop.getByRole('button', 'Stop sharing').tap();
  await expect(pop).toBeHidden();
  await expect(screen.getByText('Sharing stopped. The join link no longer works.')).toBeVisible();
  const again = await shareLink(screen);
  expect(again).not.toBe(url);
  await pop.getByRole('button', 'Stop sharing').tap();
  await expect(pop).toBeHidden();

  await screen.getByRole('button', 'Close tab', { visible: true }).tap();
  await expect.poll(async () => (await keelApi<Session[]>(app.baseUrl, '/api/sessions')).some((s) => s.id === session.id)).toBe(false);
});

test(
  'a joiner is admitted, follows read-only, asks for control and is handed it',
  { tags: ['share'], timeout: 180_000 },
  async ({ app, screen, browser }) => {
    await openKeel(app);
    const session = await openLocalShell(app, screen);
    const url = await shareLink(screen);
    await screen.getByRole('dialog', 'Share session').getByRole('textbox', 'Join link').press('Escape');

    // Admission prompt: Deny turns the joiner away.
    await joinFromNewTab(screen, url);
    const prompts = screen.getByRole('region', 'Share requests');
    const prompt = prompts.getByRole('alertdialog');
    await expect(prompt).toContainText('e2e wants to watch');
    await app.screenshot('share-admit-prompt');
    await prompt.getByRole('button', 'Deny').tap();
    await expect(prompt).toBeHidden();
    await expect(screen.getByText('The host declined your request to join.').first()).toBeVisible({ timeout: 15_000 });

    // Pasting the link again retries; Allow lets them in.
    await joinFromNewTab(screen, url);
    await expect(prompt).toContainText('e2e wants to watch');
    await prompt.getByRole('button', 'Allow').tap();
    await expect(prompt).toBeHidden();

    // Follower view: the tab says joined, a haze banner, no Vault sidebar.
    const banner = screen.getByRole('status').filter({ hasText: "Following e2e's session on" });
    await expect(banner).toBeVisible({ timeout: 30_000 });
    await expect(banner).toContainText('read-only');
    await expect(screen.getByRole('button', /· joined$/)).toBeVisible();
    await expect(screen.getByRole('complementary', 'Vault')).toBeHidden();
    await expect(screen.getByRole('button', 'Toggle vault sidebar')).toBeHidden();
    await expect(screen.getByText('You follow', { visible: true })).toBeVisible();
    await expect(screen.getByText('e2e is driving', { visible: true })).toBeVisible();
    await expect(screen.getByRole('button', 'Share')).toBeHidden();

    // Typing while following: a toast, and nothing reaches the PTY.
    const joined = screen.getByRole('textbox', 'Terminal input', { visible: true });
    await joined.tap();
    await browser.keyboard.type('echo follower-typed');
    await browser.keyboard.press('Enter');
    await expect(screen.getByRole('region', 'Following').getByText('Typing is off while you follow')).toBeVisible();
    await app.screenshot('share-follower-typing');
    await expect.poll(() => scrollback(app, session.id), { timeout: 3_000 }).not.toContain('follower-typed');

    // Ask for control: the owner gets a toast; Keep driving keeps it.
    await banner.getByRole('button', 'Ask for control').tap();
    const requests = screen.getByRole('region', 'Share requests');
    const ask = requests.getByRole('alertdialog').filter({ hasText: 'e2e asks for control' });
    await expect(ask).toBeVisible({ timeout: 10_000 });
    await app.screenshot('share-control-request');
    await ask.getByRole('button', 'Keep driving').tap();
    await expect(ask).toBeHidden();
    await expect(banner).toContainText('read-only');

    // Ask again and hand over: the joiner drives and its typing arrives.
    await banner.getByRole('button', 'Ask for control').tap();
    await expect(ask).toBeVisible({ timeout: 10_000 });
    await ask.getByRole('button', 'Hand over').tap();
    const driving = screen.getByRole('status').filter({ hasText: "Driving e2e's session on" });
    await expect(driving).toBeVisible({ timeout: 10_000 });
    await expect(driving).toContainText('you have control');
    await joined.tap();
    await browser.keyboard.type('echo joiner-$((6*7))');
    await browser.keyboard.press('Enter');
    await expect.poll(() => scrollback(app, session.id), { timeout: 10_000 }).toContain('joiner-42');

    // The owner's tab (the first) now follows: its pane banner offers Take
    // control, and taking it puts the joiner back to read-only.
    await browser.keyboard.press('Meta+1');
    const ownerBanner = screen.getByRole('status').filter({ hasText: 'Following · e2e is driving · read-only' });
    await expect(ownerBanner).toBeVisible();
    await expect(screen.getByText('You follow', { visible: true })).toBeVisible();
    await app.screenshot('share-owner-follows');
    await ownerBanner.getByRole('button', 'Take control').tap();
    await expect(ownerBanner).toBeHidden();
    await browser.keyboard.press('Meta+9');
    await expect(banner).toContainText('read-only');

    // Leave closes the joined tab; the Vault sidebar is back.
    await banner.getByRole('button', 'Leave').tap();
    await expect(screen.getByRole('button', /· joined$/)).toBeHidden();
    await expect(screen.getByRole('complementary', 'Vault')).toBeVisible();

    await browser.keyboard.press('Meta+1');
    await screen.getByRole('button', 'Share').tap();
    await screen.getByRole('dialog', 'Share session').getByRole('button', 'Stop sharing').tap();
    await screen.getByRole('button', 'Close tab', { visible: true }).tap();
    await expect.poll(async () => (await keelApi<Session[]>(app.baseUrl, '/api/sessions')).some((s) => s.id === session.id)).toBe(false);
  },
);

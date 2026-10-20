import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelApi, keelSend, openKeel } from './keel.ts';

// The Agent tab page (docs/ui-redesign.md §5.4.2, board AgentPage): saved
// conversations, the conversation and its context, opened from the panel's
// "Open all in a tab" or the palette. It shows the window's one
// conversation, the same one as the agent panel. keel serve runs with
// KEEL_AGENT_PROVIDER=fake, so nothing leaves the machine.

type Session = { id: string };
type Summary = { id: string; title: string; hosts: string[]; proposals: number };
type LogEntry = { kind: string; sessionId?: string };

const FAKE_KEY = 'e2e-not-a-real-gemini-key-0000';

const panel = (screen: any) => screen.getByRole('complementary', 'Agent');
const panelComposer = (screen: any) => panel(screen).getByRole('textbox', 'Message the agent');
const titleButton = (screen: any) => panel(screen).getByRole('button', 'Recent conversations');
const page = (screen: any) => screen.getByRole('region', 'Agent');
const list = (screen: any) => page(screen).getByRole('navigation', 'Conversations');
const thread = (screen: any) => page(screen).getByRole('region', 'Conversation');
const context = (screen: any) => page(screen).getByRole('complementary', 'Conversation context');
const approveDialog = (screen: any) => screen.getByRole('dialog').filter({ hasText: 'Type into the terminal?' });

async function openHost(app: any, screen: any): Promise<Session> {
  const before = await keelApi<Session[]>(app.baseUrl, '/api/sessions');
  const vault = screen.getByRole('complementary', 'Vault');
  await vault.getByRole('button', 'Hosts').tap();
  await vault.getByRole('button', /^Local Shell\b/).first().tap();
  await expect(screen.getByRole('textbox', 'Terminal input', { visible: true })).toBeFocused();
  let now: Session[] = [];
  await expect.poll(async () => (now = await keelApi<Session[]>(app.baseUrl, '/api/sessions')).length).toBe(before.length + 1);
  return now.find((s) => !before.some((b) => b.id === s.id))!;
}

async function openPanel(screen: any) {
  await screen.getByRole('textbox', 'Terminal input', { visible: true }).press('Meta+i');
  await expect(panel(screen)).toBeVisible();
}

async function askInPanel(screen: any, q: string) {
  const before = await panel(screen).getByRole('group', 'Proposal').count();
  await panelComposer(screen).fill(q);
  await panelComposer(screen).press('Enter');
  await expect(panel(screen).getByRole('group', 'Proposal')).toHaveCount(before + 1);
}

async function closeTabs(screen: any) {
  for (let i = 0; i < 5; i++) {
    const close = screen.getByRole('button', 'Close tab', { visible: true });
    if (!(await close.count())) return;
    await close.first().tap();
  }
}

const saved = async (app: any) => (await keelApi<{ sessions: Summary[] }>(app.baseUrl, '/api/agent/conversations')).sessions;
const scrollback = async (app: any, id: string) => (await keelApi<{ text: string }>(app.baseUrl, `/api/sessions/${id}/scrollback`)).text;

test('open from the panel: list, resume, type, rename, delete, all in step with the panel', { tags: ['agent'] }, async ({ app, screen }) => {
  await keelSend(app.baseUrl, 'PUT', '/api/agent/keys/gemini', { key: FAKE_KEY });
  await keelSend(app.baseUrl, 'DELETE', '/api/agent/conversations');
  await openKeel(app);
  const session = await openHost(app, screen);
  await openPanel(screen);
  await askInPanel(screen, 'page first question');
  await panel(screen).getByRole('button', 'New conversation').tap();
  await askInPanel(screen, 'page second question');
  await expect.poll(async () => (await saved(app)).length).toBe(2);

  // "Open all in a tab" in Recent conversations opens the page.
  await titleButton(screen).tap();
  const pop = panel(screen).getByRole('dialog', 'Recent conversations');
  await pop.getByRole('button', 'Open all in a tab').tap();
  await expect(pop).toBeHidden();
  await expect(page(screen)).toBeVisible();

  // Left: grouped, with the current conversation marked. Middle: the
  // panel's conversation. Right: what it read and proposed.
  await expect(list(screen)).toContainText(/today/i);
  await expect(list(screen).getByRole('button', /^page first question/)).toBeVisible();
  await expect(list(screen).getByRole('button', /^page second question/)).toBeVisible();
  await expect(page(screen).getByRole('heading', 'page second question')).toBeVisible();
  await expect(thread(screen)).toContainText('page second question');
  await expect(context(screen)).toContainText('Local Shell');
  await expect(context(screen)).toContainText(/\d+ lines?/);
  await expect(context(screen)).toContainText('echo keel-agent-ok');
  await expect(context(screen)).toContainText('Not typed');
  await app.screenshot('agent-page');

  // Search and the host filter narrow the list.
  await list(screen).getByRole('searchbox', 'Search conversations').fill('first');
  await expect(list(screen).getByRole('button', /^page second question/)).toHaveCount(0);
  await list(screen).getByRole('searchbox', 'Search conversations').fill('');
  await list(screen).getByRole('button', 'Local Shell').tap();
  await expect(list(screen).getByRole('button', 'Local Shell')).toHaveAttribute('aria-pressed', 'true');
  await expect(list(screen).getByRole('button', /^page second question/)).toBeVisible();
  await list(screen).getByRole('button', 'All').tap();

  // Resume the first: the page and the panel both show it.
  await list(screen).getByRole('button', /^page first question/).tap();
  await expect(page(screen).getByRole('heading', 'page first question')).toBeVisible();
  await expect(thread(screen)).toContainText(/resumed · /i);
  await expect(thread(screen)).not.toContainText('page second question');
  await expect(titleButton(screen)).toContainText('page first question');
  await expect(panel(screen)).toContainText(/resumed · /i);

  // Its proposal is still live (this run holds it): Type names the pane.
  // Read-only and one line, it goes without the approve dialog and without
  // Enter (§6 rule 13), and the pane it went to takes the focus.
  const card = thread(screen).getByRole('group', 'Proposal').first();
  await expect(card).not.toContainText('Expired');
  expect(await scrollback(app, session.id)).not.toContain('keel-agent-ok');
  await card.getByRole('button', /^Type into Local Shell/).tap();
  await expect(approveDialog(screen)).toBeHidden();
  await expect.poll(() => scrollback(app, session.id), { timeout: 10_000 }).toContain('keel-agent-ok');
  await expect(screen.getByRole('textbox', 'Terminal input', { visible: true })).toBeFocused();
  await screen.getByRole('textbox', 'Terminal input', { visible: true }).press('Enter');
  await expect.poll(() => scrollback(app, session.id), { timeout: 10_000 }).toMatch(/keel-agent-ok[\s\S]*keel-agent-ok/);
  await screen.getByRole('textbox', 'Terminal input', { visible: true }).press('Meta+k');
  await screen.getByRole('dialog', 'Command palette').getByRole('combobox').fill('Agent conversations');
  await screen.getByRole('dialog', 'Command palette').getByRole('option').filter({ hasText: 'Agent conversations' }).first().tap();
  await expect(page(screen)).toBeVisible();
  await expect(card).toContainText('Typed into Local Shell. Press Enter to run.');
  await expect(panel(screen).getByRole('group', 'Proposal').first()).toContainText('Typed into Local Shell');
  await expect(context(screen)).toContainText('Typed');
  await expect
    .poll(async () => (await keelApi<LogEntry[]>(app.baseUrl, '/api/logs')).some((e) => e.kind === 'agent.type' && e.sessionId === session.id))
    .toBe(true);

  // Ask from the page: the panel shows the same turn.
  const composer = thread(screen).getByRole('textbox', 'Message the agent');
  await composer.fill('page third question');
  await composer.press('Enter');
  await expect(thread(screen).getByRole('group', 'Proposal')).toHaveCount(2);
  await expect(panel(screen)).toContainText('page third question');
  await expect(panel(screen).getByRole('group', 'Proposal')).toHaveCount(2);

  // Rename from the page header: the list, the panel and keel serve follow.
  await page(screen).getByRole('button', 'Rename this conversation').tap();
  const title = page(screen).getByRole('textbox', 'Conversation title');
  await title.fill('Renamed on the page');
  await title.press('Enter');
  await expect(page(screen).getByRole('heading', 'Renamed on the page')).toBeVisible();
  await expect(titleButton(screen)).toContainText('Renamed on the page');
  await expect(list(screen).getByRole('button', /^Renamed on the page/)).toBeVisible();
  await expect.poll(async () => (await saved(app)).some((s) => s.title === 'Renamed on the page')).toBe(true);

  // Delete it: gone from the list and keel serve; both views start over.
  await page(screen).getByRole('button', 'Delete this conversation').tap();
  await expect(list(screen).getByRole('button', /^Renamed on the page/)).toHaveCount(0);
  await expect.poll(async () => (await saved(app)).map((s) => s.title)).toEqual(['page second question']);
  await expect(page(screen).getByRole('heading', 'New conversation')).toBeVisible();
  await expect(titleButton(screen)).toContainText('New conversation');
  await expect(thread(screen).getByText('Ask about your sessions')).toBeVisible();

  // Continue in panel focuses the panel's composer.
  await panel(screen).getByRole('button', 'Close agent').tap();
  await page(screen).getByRole('button', /^Continue in panel/).tap();
  await expect(panelComposer(screen)).toBeFocused();

  await keelSend(app.baseUrl, 'DELETE', '/api/agent/conversations');
  await closeTabs(screen);
});

test('from the palette: an Expired card has only Copy and Propose again', { tags: ['agent'] }, async ({ app, screen }) => {
  await keelSend(app.baseUrl, 'PUT', '/api/agent/keys/gemini', { key: FAKE_KEY });
  await keelSend(app.baseUrl, 'DELETE', '/api/agent/conversations');
  await openKeel(app);
  const session = await openHost(app, screen);

  // A conversation with a proposal for this pane, then enough others that
  // keel serve no longer holds it (it keeps 16), the way a restart would.
  const conv = `conv-e2e-page-${Date.now()}`;
  await expect
    .poll(async () => {
      try {
        await keelSend(app.baseUrl, 'POST', '/api/agent/ask', { conversationId: conv, question: 'expired page please touch a file', panes: [session.id] });
        return true;
      } catch {
        return false;
      }
    })
    .toBe(true);
  for (let i = 0; i < 17; i++) {
    await keelSend(app.baseUrl, 'POST', '/api/agent/ask', { conversationId: `conv-e2e-page-fill-${i}-${Date.now()}`, question: `page filler ${i}` });
  }

  await screen.getByRole('textbox', 'Terminal input', { visible: true }).press('Meta+k');
  const palette = screen.getByRole('dialog', 'Command palette');
  await palette.getByRole('combobox').fill('Agent conversations');
  await palette.getByRole('option').filter({ hasText: 'Agent conversations' }).first().tap();
  await expect(page(screen)).toBeVisible();

  await list(screen).getByRole('searchbox', 'Search conversations').fill('expired page');
  await list(screen).getByRole('button', /^expired page please touch a file/).tap();
  const card = thread(screen).getByRole('group', 'Proposal').first();
  await expect(card).toContainText('touch /tmp/keel-agent-e2e-touched');
  await expect(card).toContainText('Expired');
  await expect(card).toContainText('Panes may have changed since this was proposed.');
  await expect(card).toContainText(/Proposed .* · not typed/);
  await expect(card.getByRole('button', /^Type into/)).toHaveCount(0);
  await expect(card.getByRole('button', 'Edit command')).toHaveCount(0);
  await expect(card.getByRole('button', 'Copy')).toBeVisible();
  await expect(context(screen)).toContainText('Expired');
  await app.screenshot('agent-page-expired');

  // Propose again: a fresh card on the normal path, here and in the panel.
  await card.getByRole('button', 'Propose again').tap();
  await expect(thread(screen).getByRole('group', 'Proposal')).toHaveCount(2);
  const fresh = thread(screen).getByRole('group', 'Proposal').last();
  await expect(fresh).not.toContainText('Expired');
  await expect(fresh.getByRole('button', /^Type into Local Shell/)).toBeVisible();
  await page(screen).getByRole('button', /^Continue in panel/).tap();
  await expect(panel(screen)).toBeVisible();
  await expect(titleButton(screen)).toContainText('expired page please touch a file');
  await expect(panel(screen).getByRole('group', 'Proposal')).toHaveCount(2);
  await expect(panel(screen).getByRole('group', 'Proposal').last().getByRole('button', /Type into pane/)).toBeVisible();

  await keelSend(app.baseUrl, 'DELETE', '/api/agent/conversations');
  await closeTabs(screen);
});

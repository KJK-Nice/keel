import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelApi, keelSend, openKeel } from './keel.ts';

// Saved agent conversations (docs/ui-redesign.md §5.4.1, board
// AgentSessions). keel serve runs with KEEL_AGENT_PROVIDER=fake, so answers
// come from an in-process fake and nothing leaves the machine; the scratch
// vault (KEEL_VAULT_PASSPHRASE, KEEL_KEYRING=off) holds the saved
// conversations in keel.chats.

type Session = { id: string };
type Host = { id: string; name: string; authType: string; [k: string]: unknown };
type Summary = { id: string; title: string; hosts: string[]; proposals: number };
type LogEntry = { kind: string; sessionId?: string };

const FAKE_KEY = 'e2e-not-a-real-gemini-key-0000';

const panel = (screen: any) => screen.getByRole('complementary', 'Agent');
const composer = (screen: any) => panel(screen).getByRole('textbox', 'Message the agent');
const sessions = (screen: any) => panel(screen).getByRole('dialog', 'Recent conversations');
const titleButton = (screen: any) => panel(screen).getByRole('button', 'Recent conversations');

async function localShell(app: any): Promise<Host> {
  const hosts = await keelApi<Host[]>(app.baseUrl, '/api/hosts');
  return hosts.find((h) => h.authType === 'local')!;
}

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

async function closeTabs(screen: any) {
  for (let i = 0; i < 4; i++) {
    const close = screen.getByRole('button', 'Close tab', { visible: true });
    if (!(await close.count())) return;
    await close.first().tap();
  }
}

const list = async (app: any) => (await keelApi<{ sessions: Summary[] }>(app.baseUrl, '/api/agent/conversations')).sessions;

test('new chat keeps the old one: list, search, resume, rename, delete', { tags: ['agent'] }, async ({ app, screen }) => {
  await keelSend(app.baseUrl, 'PUT', '/api/agent/keys/gemini', { key: FAKE_KEY });
  await keelSend(app.baseUrl, 'DELETE', '/api/agent/conversations');
  await openKeel(app);
  await openHost(app, screen);
  await openPanel(screen);
  await expect(titleButton(screen)).toContainText('New conversation');
  await expect(panel(screen)).toContainText('Saved in the vault');

  // First conversation.
  await composer(screen).fill('first session marker question');
  await composer(screen).press('Enter');
  await expect(panel(screen).getByRole('group', 'Proposal')).toHaveCount(1);
  await expect(titleButton(screen)).toContainText('first session marker question');

  // New conversation: the first is not deleted.
  await panel(screen).getByRole('button', 'New conversation').tap();
  await expect(panel(screen).getByText('Ask about your sessions')).toBeVisible();
  await composer(screen).fill('second session question');
  await composer(screen).press('Enter');
  await expect(panel(screen).getByRole('group', 'Proposal')).toHaveCount(1);
  await expect.poll(async () => (await list(app)).length).toBe(2);

  // The list: grouped under Today, current one marked, filterable.
  await titleButton(screen).tap();
  const pop = sessions(screen);
  await expect(pop).toBeVisible();
  await expect(pop).toContainText(/today/i);
  await expect(pop).toContainText('first session marker question');
  await expect(pop).toContainText('Local Shell');
  await expect(pop).toContainText('1 proposal');
  await app.screenshot('agent-sessions-list');
  await pop.getByRole('searchbox', 'Search conversations').fill('marker');
  await expect(pop.getByRole('button', /^second session question/)).toHaveCount(0);

  // Resume the first: its thread comes back, with the divider.
  await pop.getByRole('button', /^first session marker question/).tap();
  await expect(pop).toBeHidden();
  await expect(titleButton(screen)).toContainText('first session marker question');
  await expect(panel(screen)).toContainText('first session marker question');
  await expect(panel(screen)).not.toContainText('second session question');
  await expect(panel(screen)).toContainText(/resumed · /i);
  await expect(composer(screen)).toHaveAttribute('placeholder', 'Continue this conversation…');

  // Rename it from the list.
  await titleButton(screen).tap();
  await pop.getByRole('button', 'Rename first session marker question').tap();
  const title = pop.getByRole('textbox', 'Conversation title');
  await title.fill('Renamed by e2e');
  await title.press('Enter');
  await expect(pop).toContainText('Renamed by e2e');
  await expect(titleButton(screen)).toContainText('Renamed by e2e');
  expect((await list(app)).some((s) => s.title === 'Renamed by e2e')).toBe(true);

  // Delete the second from the list.
  await pop.getByRole('button', 'Delete second session question').tap();
  await expect(pop.getByRole('button', /^second session question/)).toHaveCount(0);
  await expect.poll(async () => (await list(app)).map((s) => s.title)).toEqual(['Renamed by e2e']);
  await pop.press('Escape');
  await expect(pop).toBeHidden();

  await keelSend(app.baseUrl, 'DELETE', '/api/agent/conversations');
  await closeTabs(screen);
});

test('a resumed proposal is Expired: Copy or Propose again, never Type', { tags: ['agent'] }, async ({ app, screen }) => {
  await keelSend(app.baseUrl, 'PUT', '/api/agent/keys/gemini', { key: FAKE_KEY });
  await keelSend(app.baseUrl, 'DELETE', '/api/agent/conversations');
  await openKeel(app);
  const session = await openHost(app, screen);
  const local = await localShell(app);

  // Setup through the API: a conversation with a proposal for this pane,
  // then enough others that keel serve no longer holds it in memory (it
  // keeps 16), the way a restart would. The saved copy stays in the vault.
  const conv = `conv-e2e-old-${Date.now()}`;
  await expect
    .poll(async () => {
      try {
        await keelSend(app.baseUrl, 'POST', '/api/agent/ask', { conversationId: conv, question: 'expired e2e please touch a file', panes: [session.id] });
        return true;
      } catch {
        return false;
      }
    })
    .toBe(true);
  for (let i = 0; i < 17; i++) {
    await keelSend(app.baseUrl, 'POST', '/api/agent/ask', { conversationId: `conv-e2e-fill-${i}-${Date.now()}`, question: `filler ${i}` });
  }

  await openPanel(screen);
  await titleButton(screen).tap();
  const pop = sessions(screen);
  await pop.getByRole('searchbox', 'Search conversations').fill('expired e2e');
  await pop.getByRole('button', /^expired e2e please touch a file/).tap();

  const card = panel(screen).getByRole('group', 'Proposal').first();
  await expect(card).toContainText('touch /tmp/keel-agent-e2e-touched');
  await expect(card).toContainText('Expired');
  await expect(card).toContainText('Panes may have changed since this was proposed.');
  await expect(card).toContainText(/Proposed .* · not typed/);
  await expect(card.getByRole('button', /Type into pane/)).toHaveCount(0);
  await expect(card.getByRole('button', 'Copy')).toBeVisible();
  await app.screenshot('agent-sessions-expired');

  // Propose again: a fresh card that goes through the normal Type path.
  await card.getByRole('button', 'Propose again').tap();
  const fresh = panel(screen).getByRole('group', 'Proposal').last();
  await expect(panel(screen).getByRole('group', 'Proposal')).toHaveCount(2);
  await expect(fresh).not.toContainText('Expired');
  await expect(fresh).toContainText('touch /tmp/keel-agent-e2e-touched');
  await expect(fresh.getByRole('button', /Type into pane/)).toBeVisible();
  await expect
    .poll(async () => (await keelApi<LogEntry[]>(app.baseUrl, '/api/logs')).filter((e) => e.kind === 'agent.suggest' && e.sessionId === session.id).length)
    .toBeGreaterThanOrEqual(2);

  // A new ask in the resumed conversation reads the pane again, logged.
  const reads = async () => (await keelApi<LogEntry[]>(app.baseUrl, '/api/logs')).filter((e) => e.kind === 'agent.read' && e.sessionId === session.id).length;
  const before = await reads();
  await composer(screen).fill('and now?');
  await composer(screen).press('Enter');
  await expect(panel(screen)).toContainText(/Read Local Shell · last \d+ lines?/);
  await expect.poll(reads).toBe(before + 1);
  expect(local.id).toBeTruthy();

  await keelSend(app.baseUrl, 'DELETE', '/api/agent/conversations');
  await closeTabs(screen);
});

import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelApi, keelSend, openKeel } from './keel.ts';

// Host notes and the user's own notes for the agent (docs/ui-redesign.md
// §15, boards HostNotes and MemoryPropose). keel serve runs with
// KEEL_AGENT_PROVIDER=fake: "remember" in a question makes the fake
// propose a note, and every answer says how many notes it was given, so a
// test sees a note sent or not sent from the UI.

type Session = { id: string };
type Host = { id: string; name: string; authType: string; agentReadable?: boolean; [k: string]: unknown };
type Note = { id: string; hostId?: string; text: string; source: string; fromPane?: boolean };
type LogEntry = { time: string; kind: string; hostId?: string; count?: number; message: string };

const FAKE_KEY = 'e2e-not-a-real-gemini-key-0000';
const panel = (screen: any) => screen.getByRole('complementary', 'Agent');
const composer = (screen: any) => panel(screen).getByRole('textbox', 'Message the agent');
const notes = async (app: any) => (await keelApi<{ notes: Note[] }>(app.baseUrl, '/api/agent/notes')).notes;
const logs = (app: any) => keelApi<LogEntry[]>(app.baseUrl, '/api/logs');

async function localShell(app: any): Promise<Host> {
  const hosts = await keelApi<Host[]>(app.baseUrl, '/api/hosts');
  return hosts.find((h) => h.authType === 'local')!;
}

async function setReadable(app: any, on: boolean) {
  const h = await localShell(app);
  await keelSend(app.baseUrl, 'PUT', `/api/hosts/${h.id}`, { ...h, agentReadable: on, agentNotes: true });
}

async function reset(app: any) {
  await keelSend(app.baseUrl, 'PUT', '/api/agent/keys/gemini', { key: FAKE_KEY });
  await keelSend(app.baseUrl, 'DELETE', '/api/agent/conversations');
  await keelSend(app.baseUrl, 'DELETE', '/api/agent/notes');
  await setReadable(app, true);
}

async function openLocalShell(app: any, screen: any): Promise<Session> {
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
  await expect(composer(screen)).toBeFocused();
}

async function ask(screen: any, q: string) {
  await composer(screen).fill(q);
  await composer(screen).press('Enter');
}

async function closeTabs(app: any, screen: any) {
  for (let i = 0; i < 4; i++) {
    const close = screen.getByRole('button', 'Close tab', { visible: true });
    if (!(await close.count())) break;
    await close.first().tap();
  }
  for (const s of await keelApi<Session[]>(app.baseUrl, '/api/sessions')) await keelSend(app.baseUrl, 'DELETE', `/api/sessions/${s.id}`).catch(() => {});
}

async function editLocalShell(screen: any) {
  const vault = screen.getByRole('complementary', 'Vault');
  await vault.getByRole('button', 'Hosts').tap();
  await vault.getByRole('searchbox', 'Search hosts').fill('Local Shell');
  await expect(vault.getByRole('button', /^Local Shell\b/).first()).toBeVisible();
  await vault.getByRole('button', 'Actions', { visible: true }).first().tap();
  await screen.getByRole('menuitem', 'Edit').tap();
  const dialog = screen.getByRole('dialog').filter({ hasText: 'Edit host' });
  await expect(dialog).toBeVisible();
  return dialog;
}

test('a proposed note is saved only on Save, then sent with the next question and shown in the trace', { tags: ['agent', 'notes'] }, async ({ app, screen }) => {
  await reset(app);
  await openKeel(app);
  await openLocalShell(app, screen);
  await openPanel(screen);

  await ask(screen, 'please remember this');
  const card = panel(screen).getByRole('group', 'Proposed note');
  await expect(card).toContainText('Remember this?');
  await expect(card).toContainText('keel-note-e2e: backups run at 02:00 from pgbackrest');
  await expect(card).toContainText('Host notes · Local Shell');
  await expect(card).toContainText('Sent when Local Shell is in scope');
  await expect(card.getByText('From pane output')).toHaveCount(0);
  await app.screenshot('notes-proposed');
  // Nothing is kept before Save.
  expect(await notes(app)).toHaveLength(0);

  await card.getByRole('button', 'Save', { exact: true }).tap();
  await expect(card).toContainText('Saved to host notes · Local Shell');
  const kept = await notes(app);
  expect(kept).toHaveLength(1);
  expect(kept[0].source).toBe('agent');
  expect(kept[0].hostId).toBe((await localShell(app)).id);

  // Undo deletes it again; Save keeps it.
  await card.getByRole('button', 'Undo').tap();
  await expect(card.getByRole('button', 'Save', { exact: true })).toBeVisible();
  expect(await notes(app)).toHaveLength(0);
  await card.getByRole('button', 'Save', { exact: true }).tap();
  await expect(card).toContainText('Saved to host notes');

  // The next question reads Local Shell: its note goes with it.
  await ask(screen, 'what now?');
  await expect(panel(screen)).toContainText('Used host notes · Local Shell (1)');
  await expect(panel(screen)).toContainText('I was also given 1 host note and 0 of your notes.');
  await app.screenshot('notes-used');

  // §6.7: every send and every save is in keel.log, without the text.
  const entries = (await logs(app)).filter((e) => e.kind === 'agent.note');
  expect(entries.some((e) => /agent sent 1 host note for Local Shell/.test(e.message))).toBe(true);
  expect(entries.some((e) => /note proposed by the agent saved by you/.test(e.message))).toBe(true);
  expect(JSON.stringify(entries)).not.toContain('pgbackrest');
  await closeTabs(app, screen);
});

test('a note taken from pane output is flagged; Dismiss keeps nothing', { tags: ['agent', 'notes'] }, async ({ app, screen }) => {
  await reset(app);
  await openKeel(app);
  await openLocalShell(app, screen);
  await openPanel(screen);
  await ask(screen, 'remember from the pane');
  const card = panel(screen).getByRole('group', 'Proposed note');
  await expect(card).toContainText('From pane output');
  await expect(card).toContainText('This text came from terminal output, not from you.');
  await expect(card.getByRole('button', 'Save anyway')).toBeVisible();
  await expect(card.getByRole('button', 'Save', { exact: true })).toHaveCount(0);
  await app.screenshot('notes-from-pane');

  await card.getByRole('button', 'Dismiss').tap();
  await expect(card).toContainText('Dismissed. Nothing was saved.');
  expect(await notes(app)).toHaveLength(0);
  await closeTabs(app, screen);
});

test('Edit host and Settings: add, edit and delete notes; with Agent can read off a host note is not sent', { tags: ['agent', 'notes'] }, async ({ app, screen }) => {
  await reset(app);
  await openKeel(app);

  // Edit host: the notes column.
  let dialog = await editLocalShell(screen);
  const block = dialog.getByRole('region', 'Notes for the agent');
  await expect(block).toContainText('No notes.');
  const add = block.getByRole('textbox', 'Add a note');
  await add.fill('keel-note-e2e: deploys run from /srv/app');
  await add.press('Enter');
  await expect(block.getByRole('listitem')).toHaveCount(1);
  await expect(block).toContainText('1 note · ');
  await add.fill('keel-note-e2e: second note to delete');
  await block.getByRole('button', 'Add').tap();
  await expect(block.getByRole('listitem')).toHaveCount(2);
  // Edit the first, delete the second.
  await block.getByRole('listitem').first().getByRole('button', 'Edit note').tap();
  const edit = block.getByRole('textbox', 'Edit note');
  await edit.fill('keel-note-e2e: deploys run from /srv/app with make deploy');
  await edit.press('Enter');
  await expect(block.getByRole('listitem').first()).toContainText('with make deploy');
  await block.getByRole('listitem').last().getByRole('button', 'Delete note').tap();
  await expect(block.getByRole('listitem')).toHaveCount(1);
  let kept = await notes(app);
  expect(kept.map((n) => n.text)).toEqual(['keel-note-e2e: deploys run from /srv/app with make deploy']);
  // The dialog stayed open: ↵ in the notes never submitted the host form.
  await expect(dialog).toBeVisible();

  // Agent can read off: the column says the notes are not sent.
  await dialog.getByRole('switch', "Agent can read this host's panes").tap();
  await expect(block).toContainText('Agent can read is off, so these notes are not sent.');
  await app.screenshot('notes-edit-host-off');
  await dialog.getByRole('button', 'Save host').tap();
  await expect(dialog).toBeHidden();
  await expect.poll(async () => (await localShell(app)).agentReadable).toBe(false);

  // Settings · Agent: one note about the user.
  await screen.getByRole('complementary', 'Vault').getByRole('button', 'Settings').tap();
  await screen.getByRole('region', 'Settings').getByRole('tab', 'Agent').tap();
  const mine = screen.getByRole('region', 'Notes about you');
  await mine.getByRole('textbox', 'Add a note').fill('keel-note-e2e: prefer journalctl');
  await mine.getByRole('button', 'Add').tap();
  await expect(mine.getByRole('listitem')).toHaveCount(1);
  kept = await notes(app);
  expect(kept).toHaveLength(2);

  // With Agent can read off, neither the pane nor its notes (nor the
  // user's, which go only with a pane read) are sent.
  const since = new Date().toISOString();
  await openLocalShell(app, screen);
  await openPanel(screen);
  await ask(screen, 'anything to know?');
  // The panel leaves the pane out of scope; nothing of it is read.
  await expect(panel(screen)).toContainText('Read no pane');
  await expect(panel(screen)).toContainText('I was given 0 pane excerpts');
  await expect(panel(screen)).not.toContainText('Used host notes');
  await expect(panel(screen)).not.toContainText('I was also given');
  expect((await logs(app)).filter((e) => e.kind === 'agent.note' && e.time >= since && /agent sent/.test(e.message))).toHaveLength(0);

  // Asked for by id anyway (as a stale page would), keel serve skips it.
  const sessions = await keelApi<Session[]>(app.baseUrl, '/api/sessions');
  const res = await keelSend<{ answer: string; trace: { status: string }[] }>(app.baseUrl, 'POST', '/api/agent/ask', {
    conversationId: `conv-notes-off-${Date.now()}`,
    question: 'stale scope',
    panes: [sessions[sessions.length - 1].id],
  });
  expect(res.trace.map((t) => t.status)).toEqual(['skipped']);
  expect(res.answer).not.toContain('I was also given');
  expect((await logs(app)).filter((e) => e.kind === 'agent.note' && e.time >= since && /agent sent/.test(e.message))).toHaveLength(0);

  // On again: both go, and the trace says so.
  await setReadable(app, true);
  await closeTabs(app, screen);
  await openKeel(app);
  await openLocalShell(app, screen);
  // The panel stays open across the reload.
  await expect(panel(screen)).toBeVisible();
  await ask(screen, 'and now?');
  await expect(panel(screen)).toContainText('Used host notes · Local Shell (1)');
  await expect(panel(screen)).toContainText('Used your notes (1)');
  await expect(panel(screen)).toContainText('I was also given 1 host note and 1 of your notes.');
  await closeTabs(app, screen);
  await keelSend(app.baseUrl, 'DELETE', '/api/agent/notes');
  await keelSend(app.baseUrl, 'DELETE', '/api/agent/conversations');
});

import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelApi, keelSend, openKeel, retire } from './keel.ts';

// Type without the approve dialog (docs/ui-redesign.md §6 rule 13, for
// review). A read-only command of one line is typed with one click, with no
// dialog and no Enter: it waits on the prompt line until the user presses
// Enter. Anything else keeps Approve before type. keel serve decides
// (internal/httpapi/typedirect.go); keel serve runs with the fake agent
// provider (e2e.config.ts), and Local Shell hosts stand in for servers.

type Session = { id: string; hostId: string };
type Host = { id: string; name: string };
type Group = { id: string; name: string };
type Notebook = { id: string; name: string };
type Snippet = { id: string; name: string; command: string };
type LogEntry = { kind: string; sessionId?: string; bytes?: number; source?: string; message: string };

const FAKE_KEY = 'e2e-not-a-real-gemini-key-0000';
const panel = (screen: any) => screen.getByRole('complementary', 'Agent');
const composer = (screen: any) => panel(screen).getByRole('textbox', 'Message the agent');
const approveDialog = (screen: any) => screen.getByRole('dialog').filter({ hasText: 'Type into the terminal?' });
const groupApprove = (screen: any) => screen.getByRole('dialog').filter({ hasText: 'Type into these panes?' });
const runPage = (screen: any) => screen.getByRole('region', 'Group run');
const scrollback = async (app: any, id: string) => (await keelApi<{ text: string }>(app.baseUrl, `/api/sessions/${id}/scrollback`)).text;
const logs = (app: any) => keelApi<LogEntry[]>(app.baseUrl, '/api/logs');
const count = (text: string, needle: string) => text.split(needle).length - 1;
const terminal = (screen: any) => screen.getByRole('textbox', 'Terminal input', { visible: true });

async function openLocalShell(app: any, screen: any): Promise<Session> {
  const before = await keelApi<Session[]>(app.baseUrl, '/api/sessions');
  const vault = screen.getByRole('complementary', 'Vault');
  await vault.getByRole('button', 'Hosts').tap();
  await vault.getByRole('button', /^Local Shell\b/).first().tap();
  await expect(terminal(screen)).toBeFocused();
  let now: Session[] = [];
  await expect.poll(async () => (now = await keelApi<Session[]>(app.baseUrl, '/api/sessions')).length).toBe(before.length + 1);
  return now.find((s) => !before.some((b) => b.id === s.id))!;
}

async function closeTabs(app: any, screen: any) {
  for (let i = 0; i < 8; i++) {
    const pane = screen.getByRole('button', 'Close pane', { visible: true });
    const tab = screen.getByRole('button', 'Close tab', { visible: true });
    if (await pane.count()) await pane.last().tap();
    else if (await tab.count()) await tab.first().tap();
    else break;
  }
  for (const s of await keelApi<Session[]>(app.baseUrl, '/api/sessions')) await keelSend(app.baseUrl, 'DELETE', `/api/sessions/${s.id}`).catch(() => {});
}

// Stays put: what was typed is still only on the prompt line.
async function notRun(app: any, id: string, typed: string, output: string) {
  await expect.poll(() => scrollback(app, id), { timeout: 10_000 }).toContain(typed);
  await new Promise((r) => setTimeout(r, 600));
  expect(await scrollback(app, id)).not.toContain(output);
}

test('a read-only proposal is typed without a dialog and runs only on Enter; a state change still asks', { tags: ['agent'] }, async ({ app, screen }) => {
  await keelSend(app.baseUrl, 'PUT', '/api/agent/keys/gemini', { key: FAKE_KEY });
  await openKeel(app);
  try {
    const session = await openLocalShell(app, screen);
    await terminal(screen).press('Meta+i');
    await expect(composer(screen)).toBeFocused();

    await composer(screen).fill('what is going on in this pane?');
    await composer(screen).press('Enter');
    const card = panel(screen).getByRole('group', 'Proposal').last();
    await expect(card).toContainText('Read-only');
    // The card shows what Type sends: the command alone, no ^U, no ↵.
    await expect(card).toContainText('1 line · 18 bytes');
    await expect(card.getByLabel('Exact bytes')).toHaveText('echo keel-agent-ok');
    // No ellipsis: no dialog follows.
    const type = card.getByRole('button', /^Type into pane/);
    await expect(type).toHaveText(/^Type into pane\s*↵$/);
    expect(await scrollback(app, session.id)).not.toContain('keel-agent-ok');

    await type.tap();
    await expect(approveDialog(screen)).toBeHidden();
    await expect(card).toContainText('Typed into Local Shell. Press Enter to run.');
    // On the prompt line, once (the echo of what was typed), not run.
    await notRun(app, session.id, 'echo keel-agent-ok', 'keel-agent-ok\r\n');
    expect(count(await scrollback(app, session.id), 'keel-agent-ok')).toBe(1);
    await app.screenshot('type-direct-proposal');
    // The pane has the focus: the user's Enter runs it.
    await expect(terminal(screen)).toBeFocused();
    await terminal(screen).press('Enter');
    await expect.poll(async () => count(await scrollback(app, session.id), 'keel-agent-ok'), { timeout: 10_000 }).toBe(2);

    const direct = (await logs(app)).filter((e) => e.kind === 'agent.type' && e.sessionId === session.id);
    expect(direct).toHaveLength(1);
    expect(direct[0].source).toBe('direct');
    expect(direct[0].bytes).toBe(18);
    expect(direct[0].message).not.toContain('keel-agent-ok');

    // A state change: the label ends in "…" and the approve dialog decides.
    await composer(screen).fill('touch the marker');
    await composer(screen).press('Enter');
    const ch = panel(screen).getByRole('group', 'Proposal').last();
    await expect(ch).toContainText('Changes state', { timeout: 20_000 });
    await expect(ch.getByRole('button', /^Type into pane/)).toHaveText(/^Type into pane…/);
    await ch.getByRole('button', /^Type into pane/).tap();
    await expect(approveDialog(screen)).toBeVisible();
    await expect(approveDialog(screen)).toContainText('touch /tmp/keel-agent-e2e-touched');
    await approveDialog(screen).getByRole('button', 'Cancel').tap();
    await expect(approveDialog(screen)).toBeHidden();
    expect(await scrollback(app, session.id)).not.toContain('keel-agent-e2e-touched');
  } finally {
    await closeTabs(app, screen);
  }
});

test('a snippet of one read-only line is typed without a dialog; one of two lines asks', { tags: ['palette'] }, async ({ app, screen }) => {
  const made: string[] = [];
  try {
    const one = await keelSend<Snippet>(app.baseUrl, 'POST', '/api/snippets', { name: 'td-one', command: "printf '%s-out\\n' tdsnip\n" });
    const two = await keelSend<Snippet>(app.baseUrl, 'POST', '/api/snippets', { name: 'td-two', command: 'echo td-first\necho td-second\n' });
    made.push(one.id, two.id);
    await openKeel(app);
    const session = await openLocalShell(app, screen);

    await terminal(screen).press('Meta+k');
    const palette = screen.getByRole('dialog', 'Command palette');
    await palette.getByRole('combobox').fill('snippet: td-one');
    await palette.getByRole('combobox').press('Enter');
    await expect(approveDialog(screen)).toBeHidden();
    // Typed without its newline: on the prompt line, not run.
    await notRun(app, session.id, 'tdsnip', 'tdsnip-out');
    await expect(terminal(screen)).toBeFocused();
    await terminal(screen).press('Enter');
    await expect.poll(() => scrollback(app, session.id), { timeout: 10_000 }).toContain('tdsnip-out');
    const entry = (await logs(app)).find((e) => e.kind === 'snippet' && e.source === 'direct' && e.sessionId === session.id);
    expect(entry?.message).toContain(one.id);
    expect(entry?.message).not.toContain('tdsnip');

    // Two lines: Approve before type, as before.
    await terminal(screen).press('Meta+k');
    await palette.getByRole('combobox').fill('snippet: td-two');
    await palette.getByRole('combobox').press('Enter');
    await expect(approveDialog(screen)).toBeVisible();
    await expect(approveDialog(screen)).toContainText('echo td-second');
    await approveDialog(screen).getByRole('button', 'Cancel').tap();
    expect(await scrollback(app, session.id)).not.toContain('td-second');
  } finally {
    for (const id of made) await keelSend(app.baseUrl, 'DELETE', `/api/snippets/${id}`).catch(() => {});
    await closeTabs(app, screen);
  }
});

test('a notebook block of one read-only line is typed without a dialog; a block of two lines asks', { tags: ['notebook'] }, async ({ app, screen }) => {
  const name = `td-nb-${Date.now()}`;
  const nb = await keelSend<Notebook>(app.baseUrl, 'POST', '/api/notebooks', {
    name,
    cells: [{ body: "```sh\nprintf '%s-out\\n' tdnb\n```\n\n```sh\necho td-nb-first\necho td-nb-second\n```" }],
  });
  try {
    await openKeel(app);
    const session = await openLocalShell(app, screen);
    const vault = screen.getByRole('complementary', 'Vault');
    await vault.getByRole('button', 'Snippets').tap();
    await vault.getByRole('button', new RegExp(`^${name}$`)).tap();
    const types = screen.getByRole('button', 'Type into Local Shell');
    await expect(types).toHaveCount(2);

    // Two lines: the CLI input, then the approve dialog, as before.
    await types.nth(1).tap();
    await expect(approveDialog(screen)).toBeVisible();
    await expect(approveDialog(screen)).toContainText('echo td-nb-second');
    await approveDialog(screen).getByRole('button', 'Cancel').tap();
    await expect(approveDialog(screen)).toBeHidden();
    await screen.getByRole('button', 'Cancel', { visible: true }).tap();
    expect(await scrollback(app, session.id)).not.toContain('td-nb-second');

    // One read-only line: typed into the pane beside it, not run.
    await types.first().tap();
    await expect(approveDialog(screen)).toBeHidden();
    await notRun(app, session.id, 'tdnb', 'tdnb-out');
    await expect(terminal(screen)).toBeFocused();
    await terminal(screen).press('Enter');
    await expect.poll(() => scrollback(app, session.id), { timeout: 10_000 }).toContain('tdnb-out');
    expect((await logs(app)).some((e) => e.kind === 'notebook' && e.source === 'direct' && e.message.includes(nb.id))).toBe(true);
  } finally {
    await closeTabs(app, screen);
    await keelSend(app.baseUrl, 'DELETE', `/api/notebooks/${nb.id}`).catch(() => {});
  }
});

async function ensureHost(app: any, name: string): Promise<Host> {
  const hosts = await keelApi<Host[]>(app.baseUrl, '/api/hosts');
  const have = hosts.find((h) => h.name === name);
  if (have) {
    await keelSend(app.baseUrl, 'PUT', `/api/hosts/${have.id}`, { ...have, authType: 'local', agentReadable: true });
    return have;
  }
  return keelSend<Host>(app.baseUrl, 'POST', '/api/hosts', { name, authType: 'local', agentReadable: true });
}

test('a group run of a read-only step: one click types into both hosts, no Enter, no dialog', { tags: ['agent', 'runbooks'] }, async ({ app, screen }) => {
  try {
    for (const rb of (await keelApi<{ runbooks: { id: string }[] }>(app.baseUrl, '/api/agent/runbooks')).runbooks) {
      await keelSend(app.baseUrl, 'DELETE', `/api/agent/runbooks/${rb.id}`);
    }
    const h1 = await ensureHost(app, 'td-wk-1');
    const h2 = await ensureHost(app, 'td-wk-2');
    const groups = await keelApi<Group[]>(app.baseUrl, '/api/groups');
    const group = groups.find((g) => g.name === 'td-e2e') ?? (await keelSend<Group>(app.baseUrl, 'POST', '/api/groups', { name: 'td-e2e' }));
    await keelSend(app.baseUrl, 'POST', '/api/hosts/move', { ids: [h1.id, h2.id], groupId: group.id });
    for (const h of [h1, h2]) await keelSend(app.baseUrl, 'PUT', `/api/groups/${group.id}/roles`, { hostId: h.id, role: 'worker' });
    // The step prints the OSC 133 marks a shell with integration would
    // send, so the run page sees exit 0 once the user runs it.
    await keelSend(app.baseUrl, 'POST', '/api/agent/runbooks', {
      name: 'td check workers',
      agentMayPick: false,
      steps: [{ title: 'Check each worker', command: "printf '\\033]133;C\\007%s-out\\n\\033]133;D;0\\007' tdgrp", role: 'worker' }],
    });
    await openKeel(app);
    const vault = screen.getByRole('complementary', 'Vault');
    await vault.getByRole('button', 'Hosts').tap();
    await screen.getByRole('button', 'New host', { visible: true }).first().press('Meta+k');
    const palette = screen.getByRole('dialog', 'Command palette');
    await palette.getByRole('combobox').fill('Group td-e2e');
    await palette.getByRole('option').filter({ hasText: 'Group td-e2e' }).first().tap();
    const page = screen.getByRole('region', 'Group');
    await page.getByRole('button', 'Run a runbook on this group').tap();
    await page.getByRole('menuitem', /td check workers/).tap();
    await expect(runPage(screen)).toBeVisible();
    await runPage(screen).getByRole('button', 'Parallel').tap();
    await expect(runPage(screen)).toContainText('2 hosts have no pane open here.', { timeout: 10_000 });
    const back = async () => {
      await screen.getByRole('button', /Run · td check workers/).first().tap();
      await expect(runPage(screen)).toBeVisible();
    };
    for (const n of ['td-wk-1', 'td-wk-2']) {
      await runPage(screen).getByRole('button', `Open a pane on ${n}`).tap();
      await expect(terminal(screen)).toBeFocused();
      await back();
    }
    // No ellipsis: no dialog follows.
    const typeBtn = runPage(screen).getByRole('button', /^Type into 2 panes/);
    await expect(typeBtn).toBeEnabled({ timeout: 10_000 });
    await expect(typeBtn).toHaveText('Type into 2 panes');
    await typeBtn.tap();
    await expect(groupApprove(screen)).toBeHidden();
    await expect(screen.getByText('Typed into 2 panes. Press Enter in each to run.')).toBeVisible();

    const sessions = await keelApi<Session[]>(app.baseUrl, '/api/sessions');
    const s1 = sessions.find((s) => s.hostId === h1.id)!;
    const s2 = sessions.find((s) => s.hostId === h2.id)!;
    // In both panes, and run in neither: no Enter was sent.
    await notRun(app, s1.id, 'tdgrp', 'tdgrp-out');
    await notRun(app, s2.id, 'tdgrp', 'tdgrp-out');
    await expect(runPage(screen).getByRole('row', /^td-wk-1/)).not.toContainText('✓ exit 0');
    const typed = (await logs(app)).filter((e) => e.kind === 'agent.type' && e.source === 'direct' && (e.sessionId === s1.id || e.sessionId === s2.id));
    expect(new Set(typed.map((e) => e.sessionId)).size).toBe(2);
    await app.screenshot('type-direct-group');

    // The user's Enter in one pane runs it there only.
    await screen.getByRole('button', /td-wk-1/).first().tap().catch(() => {});
    const tabs = screen.getByRole('tab', /td-wk-1/);
    if (await tabs.count()) await tabs.first().tap();
    await expect(terminal(screen)).toBeVisible();
    await terminal(screen).press('Enter');
    await expect.poll(() => scrollback(app, s1.id), { timeout: 10_000 }).toContain('tdgrp-out');
    expect(await scrollback(app, s2.id)).not.toContain('tdgrp-out');
  } finally {
    await closeTabs(app, screen);
    await retire(app, /^td-wk-/, 'td-e2e');
  }
});

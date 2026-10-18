import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { join } from 'node:path';
import { randomBytes } from 'node:crypto';
import { keelApi, keelSend, openKeel, retire } from './keel.ts';

// Groups, part 1 (docs/ui-redesign.md §17, boards GroupRoles and
// GroupRunbook). Three Local Shell hosts stand in for a folder of servers:
// wx-wk-1 and wx-wk-2 (Agent can read on) and wx-off (off). The step's own
// command prints the OSC 133 marks a shell with integration would send,
// with exit 0 the first time it runs and exit 1 after that (a marker
// file), so a rolling run fails on its second host.

type Host = { id: string; name: string; groupId?: string };
type Group = { id: string; name: string };
type Session = { id: string; hostId: string };
type LogEntry = { kind: string; message: string };

const page = (screen: any) => screen.getByRole('region', 'Group');
const run = (screen: any) => screen.getByRole('region', 'Group run');
const approve = (screen: any) => screen.getByRole('dialog').filter({ hasText: 'Type into these panes?' });
const scrollback = async (app: any, id: string) => (await keelApi<{ text: string }>(app.baseUrl, `/api/sessions/${id}/scrollback`)).text;

async function ensureHost(app: any, name: string, readable: boolean): Promise<Host> {
  const hosts = await keelApi<Host[]>(app.baseUrl, '/api/hosts');
  const have = hosts.find((h) => h.name === name);
  if (have) {
    await keelSend(app.baseUrl, 'PUT', `/api/hosts/${have.id}`, { ...have, authType: 'local', agentReadable: readable });
    return have;
  }
  return keelSend<Host>(app.baseUrl, 'POST', '/api/hosts', { name, authType: 'local', agentReadable: readable });
}

async function setup(app: any, step: string) {
  for (const rb of (await keelApi<{ runbooks: { id: string }[] }>(app.baseUrl, '/api/agent/runbooks')).runbooks) {
    await keelSend(app.baseUrl, 'DELETE', `/api/agent/runbooks/${rb.id}`);
  }
  const wk1 = await ensureHost(app, 'wx-wk-1', true);
  const wk2 = await ensureHost(app, 'wx-wk-2', true);
  const off = await ensureHost(app, 'wx-off', false);
  const groups = await keelApi<Group[]>(app.baseUrl, '/api/groups');
  const group = groups.find((g) => g.name === 'gx-e2e') ?? (await keelSend<Group>(app.baseUrl, 'POST', '/api/groups', { name: 'gx-e2e' }));
  await keelSend(app.baseUrl, 'POST', '/api/hosts/move', { ids: [wk1.id, wk2.id, off.id], groupId: group.id });
  for (const h of [wk1, wk2]) await keelSend(app.baseUrl, 'PUT', `/api/groups/${group.id}/roles`, { hostId: h.id, role: '' });
  await keelSend(app.baseUrl, 'PUT', `/api/groups/${group.id}/roles`, { hostId: off.id, role: 'worker' });
  const notes = await keelApi<{ notes: { id: string; groupId?: string }[] }>(app.baseUrl, '/api/agent/notes');
  for (const n of notes.notes.filter((x) => x.groupId === group.id)) await keelSend(app.baseUrl, 'DELETE', `/api/agent/notes/${n.id}`);
  await keelSend(app.baseUrl, 'POST', '/api/agent/runbooks', {
    name: 'gx restart workers',
    agentMayPick: false,
    steps: [{ title: 'Restart the service on each worker', command: step, role: 'worker' }],
  });
  return { wk1, wk2, off, group };
}

async function closeTabs(app: any, screen: any) {
  for (let i = 0; i < 6; i++) {
    const close = screen.getByRole('button', 'Close tab', { visible: true });
    if (!(await close.count())) break;
    await close.first().tap();
  }
  for (const s of await keelApi<Session[]>(app.baseUrl, '/api/sessions')) await keelSend(app.baseUrl, 'DELETE', `/api/sessions/${s.id}`).catch(() => {});
}

async function openGroupPage(screen: any) {
  const vault = screen.getByRole('complementary', 'Vault');
  await vault.getByRole('button', 'Hosts').tap();
  await screen.getByRole('button', 'New host', { visible: true }).first().press('Meta+k');
  const palette = screen.getByRole('dialog', 'Command palette');
  await palette.getByRole('combobox').fill('Group gx-e2e');
  await palette.getByRole('option').filter({ hasText: 'Group gx-e2e' }).first().tap();
  await expect(page(screen)).toBeVisible();
}

async function backToRun(screen: any) {
  await screen.getByRole('button', /Run · gx restart workers/).first().tap();
  await expect(run(screen)).toBeVisible();
}

test('roles on the group page, then a rolling run: one host at a time, a failure stops the next host', { tags: ['agent', 'runbooks'] }, async ({ app, screen }) => {
  try {
  const marker = join(process.cwd(), '.e2e', `gx-marker-${randomBytes(4).toString('hex')}`);
  const step = `if [ -e ${marker} ]; then c=1; else c=0; : > ${marker}; fi; printf '\\033]133;C\\007gx-step\\n\\033]133;D;%s\\007' "$c"`;
  const { wk1, wk2, off, group } = await setup(app, step);
  await openKeel(app);
  await openGroupPage(screen);

  // Roles: a new role for wx-wk-1, the same for wx-wk-2 from the menu.
  await page(screen).getByRole('combobox', 'Role of wx-wk-1').selectOption('New role…');
  await page(screen).getByRole('textbox', 'New role for wx-wk-1').fill('worker');
  await page(screen).getByRole('textbox', 'New role for wx-wk-1').press('Enter');
  await page(screen).getByRole('combobox', 'Role of wx-wk-2').selectOption('worker');
  await expect(page(screen).getByRole('list', 'Hosts per role')).toContainText('3 worker');
  const members = await keelApi<{ members: { hostId: string; role?: string }[] }>(app.baseUrl, `/api/groups/${group.id}/members`);
  expect(members.members.filter((m) => m.role === 'worker').map((m) => m.hostId).sort()).toEqual([wk1.id, wk2.id, off.id].sort());

  // A group note, kept redacted in the vault.
  await page(screen).getByRole('textbox', 'Add a note').fill('gx-note drain a worker before restarting containerd');
  await page(screen).getByRole('button', 'Add', { exact: true }).tap();
  await expect(page(screen)).toContainText('gx-note drain a worker');
  await app.screenshot('group-page');

  // Run the runbook: rolling, the first worker is proposed; wx-off never.
  await page(screen).getByRole('button', 'Run a runbook on this group').tap();
  await page(screen).getByRole('menuitem', /gx restart workers/).tap();
  await expect(run(screen)).toBeVisible();
  await expect(run(screen)).toContainText('Left out: wx-off (Agent can read is off)');
  await expect(run(screen)).toContainText('wx-wk-1 has no pane open here.');
  await run(screen).getByRole('button', 'Open a pane on wx-wk-1').tap();
  await expect(screen.getByRole('textbox', 'Terminal input', { visible: true })).toBeFocused();
  await backToRun(screen);
  const typeBtn = run(screen).getByRole('button', /^Type into 1 pane/);
  await expect(typeBtn).toBeEnabled({ timeout: 10_000 });
  let sessions = await keelApi<Session[]>(app.baseUrl, '/api/sessions');
  const s1 = sessions.find((s) => s.hostId === wk1.id)!;
  expect(await scrollback(app, s1.id)).not.toContain('gx-step');

  // The approve dialog: the bytes and one row; nothing typed before Type.
  await typeBtn.tap();
  await expect(approve(screen)).toBeVisible();
  await expect(approve(screen)).toContainText('133;D;');
  await expect(approve(screen).getByRole('checkbox', 'Type into wx-wk-1')).toHaveAttribute('aria-checked', 'true');
  await approve(screen).getByRole('button', 'Type into 1 pane').tap();
  await expect(run(screen).getByRole('row', /^wx-wk-1/)).toContainText('✓ exit 0', { timeout: 15_000 });

  // wx-wk-2 is next: it needs a pane; then it fails (exit 1) and rolling stops.
  await expect(run(screen)).toContainText('wx-wk-2 has no pane open here.', { timeout: 10_000 });
  await run(screen).getByRole('button', 'Open a pane on wx-wk-2').tap();
  await expect(screen.getByRole('textbox', 'Terminal input', { visible: true })).toBeFocused();
  await backToRun(screen);
  await expect(typeBtn).toBeEnabled({ timeout: 10_000 });
  await typeBtn.tap();
  await approve(screen).getByRole('button', 'Type into 1 pane').tap();
  await expect(run(screen).getByRole('alert')).toContainText('wx-wk-2 failed with exit 1.', { timeout: 15_000 });
  await app.screenshot('group-run-halted');

  // Retry makes a new proposal; Skip moves past it and the run finishes.
  await run(screen).getByRole('button', 'Skip host').tap();
  await expect(run(screen)).toContainText('All steps are done or skipped.');
  sessions = await keelApi<Session[]>(app.baseUrl, '/api/sessions');
  expect(sessions.some((s) => s.hostId === off.id)).toBe(false);

  const logs = await keelApi<LogEntry[]>(app.baseUrl, '/api/logs');
  const runLogs = logs.filter((e) => e.kind === 'agent.runbook' && e.message.includes('group run'));
  expect(runLogs.some((e) => e.message.includes('exit 0 reported by the shell'))).toBe(true);
  expect(runLogs.some((e) => e.message.includes('exit 1 reported by the shell'))).toBe(true);
  expect(runLogs.some((e) => /gx restart|wx-wk|gx-e2e|gx-step/.test(e.message))).toBe(false);
  expect(logs.filter((e) => e.kind === 'agent.type').length).toBeGreaterThanOrEqual(2);
  } finally {
    await closeTabs(app, screen);
    await retire(app, /^wx-/, 'gx-e2e');
  }
});

test('a parallel run: one dialog lists every host, only ticked hosts are typed, each on its own', { tags: ['agent', 'runbooks'] }, async ({ app, screen }) => {
  try {
  const step = `printf '\\033]133;C\\007gx-par\\n\\033]133;D;0\\007'`;
  const { wk1, wk2, group } = await setup(app, step);
  for (const h of [wk1, wk2]) await keelSend(app.baseUrl, 'PUT', `/api/groups/${group.id}/roles`, { hostId: h.id, role: 'worker' });
  await openKeel(app);
  await openGroupPage(screen);
  await page(screen).getByRole('button', 'Run a runbook on this group').tap();
  await page(screen).getByRole('menuitem', /gx restart workers/).tap();
  await expect(run(screen)).toBeVisible();
  await run(screen).getByRole('button', 'Parallel').tap();
  await expect(run(screen).getByRole('button', 'Parallel')).toHaveAttribute('aria-pressed', 'true');
  await expect(run(screen)).toContainText('2 hosts have no pane open here.', { timeout: 10_000 });
  await run(screen).getByRole('button', 'Open a pane on wx-wk-1').tap();
  await expect(screen.getByRole('textbox', 'Terminal input', { visible: true })).toBeFocused();
  await backToRun(screen);
  await run(screen).getByRole('button', 'Open a pane on wx-wk-2').tap();
  await expect(screen.getByRole('textbox', 'Terminal input', { visible: true })).toBeFocused();
  await backToRun(screen);
  const typeBtn = run(screen).getByRole('button', /^Type into 2 panes/);
  await expect(typeBtn).toBeEnabled({ timeout: 10_000 });
  await typeBtn.tap();
  const dlg = approve(screen);
  await expect(dlg).toBeVisible();
  // Nothing is ticked in advance: the button asks for ticks.
  await expect(dlg.getByRole('checkbox', 'Type into wx-wk-1')).toHaveAttribute('aria-checked', 'false');
  await expect(dlg.getByRole('checkbox', 'Type into wx-wk-2')).toHaveAttribute('aria-checked', 'false');
  await expect(dlg.getByRole('button', 'Tick the hosts to type into')).toBeDisabled();
  await dlg.getByRole('checkbox', 'Type into wx-wk-2').tap();
  await app.screenshot('group-approve');
  await dlg.getByRole('button', 'Type into 1 pane').tap();
  await expect(run(screen).getByRole('row', /^wx-wk-2/)).toContainText('✓ exit 0', { timeout: 15_000 });
  const sessions = await keelApi<Session[]>(app.baseUrl, '/api/sessions');
  const s1 = sessions.find((s) => s.hostId === wk1.id)!;
  const s2 = sessions.find((s) => s.hostId === wk2.id)!;
  expect(await scrollback(app, s2.id)).toContain('gx-par');
  // wx-wk-1 was not ticked: nothing reached its pane, it still waits.
  expect(await scrollback(app, s1.id)).not.toContain('gx-par');
  await expect(run(screen).getByRole('row', /^wx-wk-1/)).toContainText('waiting for you');
  await app.screenshot('group-run-parallel');
  await run(screen).getByRole('button', 'Stop runbook').first().tap();
  await screen.getByRole('alertdialog').getByRole('button', 'Stop runbook').tap();
  await expect(run(screen)).toContainText('Runbook stopped at step 1.');
  } finally {
    await closeTabs(app, screen);
    await retire(app, /^wx-/, 'gx-e2e');
  }
});

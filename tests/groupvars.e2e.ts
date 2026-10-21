import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelApi, keelSend, openKeel, retire } from './keel.ts';

// Groups, part 2 (docs/ui-redesign.md §17, boards CaptureVars and
// AnsibleExport). Two Local Shell hosts stand in for a cluster: gv-cp
// (role cp) and gv-wk (role wk), in the folder gv-e2e. Step 1 prints a
// join line between OSC 133 marks; keel serve captures its code. Step 2
// names {{join_code}} and the group variable {{gv_version}}: the approve
// dialog shows them filled in, and only those bytes reach the pane. Step 3
// names a variable no one set: it cannot be typed. Then the Ansible export.

type Host = { id: string; name: string };
type Group = { id: string; name: string };
type Session = { id: string; hostId: string };
type LogEntry = { kind: string; message: string };

const CODE = 'tok7f3a.tok7f3a';
const page = (screen: any) => screen.getByRole('region', 'Group');
const run = (screen: any) => screen.getByRole('region', 'Group run');
const approve = (screen: any) => screen.getByRole('dialog').filter({ hasText: 'Type into these panes?' });
const scrollback = async (app: any, id: string) => (await keelApi<{ text: string }>(app.baseUrl, `/api/sessions/${id}/scrollback`)).text;

async function ensureHost(app: any, name: string): Promise<Host> {
  const have = (await keelApi<Host[]>(app.baseUrl, '/api/hosts')).find((h) => h.name === name);
  if (have) return have;
  return keelSend<Host>(app.baseUrl, 'POST', '/api/hosts', { name, authType: 'local', agentReadable: true });
}

async function setup(app: any) {
  for (const rb of (await keelApi<{ runbooks: { id: string }[] }>(app.baseUrl, '/api/agent/runbooks')).runbooks) {
    await keelSend(app.baseUrl, 'DELETE', `/api/agent/runbooks/${rb.id}`);
  }
  const cp = await ensureHost(app, 'gv-cp');
  const wk = await ensureHost(app, 'gv-wk');
  const groups = await keelApi<Group[]>(app.baseUrl, '/api/groups');
  const group = groups.find((g) => g.name === 'gv-e2e') ?? (await keelSend<Group>(app.baseUrl, 'POST', '/api/groups', { name: 'gv-e2e' }));
  await keelSend(app.baseUrl, 'POST', '/api/hosts/move', { ids: [cp.id, wk.id], groupId: group.id });
  await keelSend(app.baseUrl, 'PUT', `/api/groups/${group.id}/roles`, { hostId: cp.id, role: 'cp' });
  await keelSend(app.baseUrl, 'PUT', `/api/groups/${group.id}/roles`, { hostId: wk.id, role: 'wk' });
  await keelSend(app.baseUrl, 'PUT', `/api/groups/${group.id}/vars`, { name: 'gv_version', value: '' });
  await keelSend(app.baseUrl, 'POST', '/api/agent/runbooks', {
    name: 'gv bootstrap',
    agentMayPick: false,
    steps: [
      {
        title: 'Make a join code on the control plane',
        // touch: these steps change state, so the group approve dialog,
        // which shows the filled-in values, decides (§6 rule 13).
        command: `touch /tmp/keel-gv-e2e; t=tok7f3a; printf '\\033]133;C\\007kubeadm join 10.0.0.1:6443 --code %s.%s\\n\\033]133;D;0\\007' "$t" "$t"`,
        role: 'cp',
        oneHost: true,
        captures: [{ name: 'join_code', pattern: '--code\\s+(\\S+)' }],
      },
      { title: 'Join the worker', command: `touch /tmp/keel-gv-e2e; printf '\\033]133;C\\007gv-joined %s %s\\n\\033]133;D;0\\007' {{join_code}} {{gv_version}}`, role: 'wk' },
      { title: 'Use a variable no one set', command: 'echo {{gv_missing}}', role: 'wk' },
    ],
  });
  return { cp, wk, group };
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
  await palette.getByRole('combobox').fill('Group gv-e2e');
  await palette.getByRole('option').filter({ hasText: 'Group gv-e2e' }).first().tap();
  await expect(page(screen)).toBeVisible();
}

async function backToRun(screen: any) {
  await screen.getByRole('button', /Run · gv bootstrap/).first().tap();
  await expect(run(screen)).toBeVisible();
}

async function openPane(screen: any, host: string) {
  await expect(run(screen)).toContainText(`${host} has no pane open here.`, { timeout: 10_000 });
  await run(screen).getByRole('button', `Open a pane on ${host}`).tap();
  await expect(screen.getByRole('textbox', 'Terminal input', { visible: true })).toBeFocused();
  await backToRun(screen);
}

test('a captured value and a group variable are filled in at Type, shown first, and never logged', { tags: ['agent', 'runbooks'] }, async ({ app, screen }) => {
  try {
    const { cp, wk } = await setup(app);
    await openKeel(app);
    await openGroupPage(screen);

    // A group variable, added on the group page.
    const vars = page(screen).getByRole('region', 'Group variables');
    await vars.getByRole('textbox', 'Variable name').fill('gv_version');
    await vars.getByRole('textbox', 'Variable value').fill('1.31.2');
    await vars.getByRole('button', 'Add variable').tap();
    await expect(vars).toContainText('gv_version');
    await expect(vars).toContainText('1.31.2');
    await app.screenshot('group-vars');

    // Step 1 on gv-cp: no variables; keel serve captures the code.
    await page(screen).getByRole('button', 'Run a runbook on this group').tap();
    await page(screen).getByRole('menuitem', /gv bootstrap/).tap();
    await expect(run(screen)).toBeVisible();
    await openPane(screen, 'gv-cp');
    const typeBtn = run(screen).getByRole('button', /^Type into 1 pane/);
    await expect(typeBtn).toBeEnabled({ timeout: 10_000 });
    await typeBtn.tap();
    await approve(screen).getByRole('button', 'Type into 1 pane').tap();
    await expect(run(screen).getByRole('row', /^gv-cp/)).toContainText('✓ exit 0', { timeout: 15_000 });
    const box = run(screen).getByRole('region', 'Values captured from step 1');
    await expect(box).toContainText('Captured by Keel, never sent to the agent');
    await expect(box).toContainText(`${CODE.length} bytes`);
    await expect(run(screen)).not.toContainText(CODE);

    // Step 2 on gv-wk: the card keeps the names; the dialog shows the values.
    await openPane(screen, 'gv-wk');
    await expect(run(screen)).toContainText('Uses 1 group variable and 1 captured value · filled in at Type');
    await expect(run(screen)).toContainText('{{join_code}}');
    await expect(run(screen)).not.toContainText(CODE);
    await expect(typeBtn).toBeEnabled({ timeout: 10_000 });
    await typeBtn.tap();
    const dlg = approve(screen);
    await expect(dlg).toBeVisible();
    await expect(dlg).toContainText(`gv-joined %s %s`);
    await expect(dlg).toContainText(`${CODE} 1.31.2`);
    await expect(dlg).toContainText('filled in by Keel: {{join_code}} captured on gv-cp · step 1, {{gv_version}} group variable');
    await app.screenshot('group-approve-filled');
    await dlg.getByRole('button', 'Type into 1 pane').tap();
    await expect(run(screen).getByRole('row', /^gv-wk/)).toContainText('✓ exit 0', { timeout: 15_000 });
    const sessions = await keelApi<Session[]>(app.baseUrl, '/api/sessions');
    const sw = sessions.find((s) => s.hostId === wk.id)!;
    expect(await scrollback(app, sw.id)).toContain(`gv-joined ${CODE} 1.31.2`);
    expect(sessions.some((s) => s.hostId === cp.id)).toBe(true);

    // Step 3 names a variable no one set: the dialog says so, nothing can
    // be ticked, and nothing is typed.
    await expect(run(screen)).toContainText('Step 3 · Use a variable no one set', { timeout: 10_000 });
    await expect(typeBtn).toBeEnabled({ timeout: 10_000 });
    await typeBtn.tap();
    await expect(dlg).toContainText('{{gv_missing}} has no value for gv-wk');
    await expect(dlg.getByRole('checkbox', 'Type into gv-wk')).toBeDisabled();
    await dlg.getByRole('button', 'Cancel').tap();
    expect(await scrollback(app, sw.id)).not.toContain('echo {{gv_missing}}');
    await run(screen).getByRole('button', 'Skip gv-wk').tap();
    await expect(run(screen)).toContainText('All steps are done or skipped.');

    const logs = await keelApi<LogEntry[]>(app.baseUrl, '/api/logs');
    expect(logs.some((e) => e.message.includes(CODE) || e.message.includes('1.31.2'))).toBe(false);
    expect(logs.some((e) => e.kind === 'agent.runbook' && e.message.includes('1 value captured from the step\'s output'))).toBe(true);
    expect(logs.some((e) => e.kind === 'agent.runbook' && e.message.includes('2 variables filled in at Type (values not logged)'))).toBe(true);
  } finally {
    await closeTabs(app, screen);
    await retire(app, /^gv-/, 'gv-e2e');
  }
});

test('Export to Ansible: the inventory from roles and variables, a playbook from a runbook, no secrets', { tags: ['runbooks'] }, async ({ app, screen }) => {
  try {
    const { group } = await setup(app);
    await keelSend(app.baseUrl, 'PUT', `/api/groups/${group.id}/vars`, { name: 'gv_version', value: '1.31.2' });
    await openKeel(app);
    await openGroupPage(screen);
    await page(screen).getByRole('button', 'Export to Ansible').tap();
    const drawer = screen.getByRole('complementary', 'Export to Ansible');
    await expect(drawer).toBeVisible();
    await expect(drawer).toContainText('[gv_e2e]');
    await expect(drawer).toContainText('[cp]');
    await expect(drawer).toContainText('gv_version=1.31.2');
    await expect(drawer).toContainText('ansible_connection=local');
    await expect(drawer).toContainText('No passwords, keys or tokens are written into them');
    await app.screenshot('group-export-ini');

    await drawer.getByRole('button', 'YAML').tap();
    await expect(drawer.getByRole('button', 'inventory.yml')).toBeVisible();
    await expect(drawer).toContainText('"gv_version": "1.31.2"');

    await drawer.getByRole('combobox', 'Playbook from a runbook').tap();
    await screen.getByRole('option', /Playbook: gv bootstrap/).tap();
    await expect(drawer.getByRole('button', 'playbooks/gv-bootstrap.yml')).toHaveAttribute('aria-pressed', 'true');
    await expect(drawer).toContainText('{{ join_code | quote }}');
    await expect(drawer).toContainText('TODO: Keel captures join_code');
    await expect(drawer).toContainText('serial: 1');
    await expect(drawer).toContainText('ansible-playbook -i inventory.yml playbooks/gv-bootstrap.yml --check');
    await drawer.getByRole('button', 'Parallel').tap();
    await expect(drawer).not.toContainText('serial: 1');
    await app.screenshot('group-export-playbook');
    await drawer.getByRole('button', 'Close').tap();
    await expect(drawer).toBeHidden();

    const logs = await keelApi<LogEntry[]>(app.baseUrl, '/api/logs');
    expect(logs.some((e) => e.kind === 'group' && e.message.startsWith('ansible export of group'))).toBe(true);
    expect(logs.some((e) => e.message.startsWith('ansible export') && /gv-e2e|gv bootstrap|gv-cp|1.31.2/.test(e.message))).toBe(false);
  } finally {
    await closeTabs(app, screen);
    await retire(app, /^gv-/, 'gv-e2e');
  }
});

test('the Runbooks editor: a step captures a value; a pattern without a group is refused', { tags: ['runbooks'] }, async ({ app, screen }) => {
  try {
    await setup(app);
    await openKeel(app);
    const vault = screen.getByRole('complementary', 'Vault');
    await vault.getByRole('button', 'Hosts').tap();
    await screen.getByRole('button', 'New host', { visible: true }).first().press('Meta+k');
    const palette = screen.getByRole('dialog', 'Command palette');
    await palette.getByRole('combobox').fill('Runbooks');
    await palette.getByRole('option').filter({ hasText: 'Runbooks' }).first().tap();
    const rb = screen.getByRole('region', 'Runbooks');
    await rb.getByRole('button', 'Open gv bootstrap').tap();
    await expect(rb.getByRole('textbox', 'Step 1 capture 1 name')).toHaveValue('join_code');
    await expect(rb.getByRole('textbox', 'Step 1 capture 1 pattern')).toHaveValue('--code\\s+(\\S+)');

    await rb.getByRole('button', 'Capture a value').nth(1).tap();
    await rb.getByRole('textbox', 'Step 2 capture 1 name').fill('gv_out');
    await rb.getByRole('textbox', 'Step 2 capture 1 pattern').fill('no group here');
    await rb.getByRole('button', /^Save/).tap();
    await expect(rb.getByRole('alert')).toContainText('needs a group');
    await rb.getByRole('textbox', 'Step 2 capture 1 pattern').fill('gv-joined\\s+(\\S+)');
    await rb.getByRole('button', /^Save/).tap();
    await expect(screen.getByText('Runbook saved')).toBeVisible();
    const list = (await keelApi<{ runbooks: { name: string; steps: { captures?: { name: string; pattern: string }[] }[] }[] }>(app.baseUrl, '/api/agent/runbooks')).runbooks;
    const saved = list.find((b) => b.name === 'gv bootstrap')!;
    expect(saved.steps[1].captures).toEqual([{ name: 'gv_out', pattern: 'gv-joined\\s+(\\S+)' }]);
    await app.screenshot('runbook-captures');
  } finally {
    await closeTabs(app, screen);
    await retire(app, /^gv-/, 'gv-e2e');
  }
});

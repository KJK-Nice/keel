import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelApi, keelSend, openKeel } from './keel.ts';

// Runbooks (docs/ui-redesign.md §16, boards Runbooks and RunbookInUse).
// keel serve runs with KEEL_AGENT_PROVIDER=fake: with a runbook in use it
// says which step is current and proposes nothing itself (keel serve
// proposes the step), and "save this as a runbook" makes it propose one.
// The Local Shell is /bin/sh with no shell integration, so the step's own
// command prints the OSC 133 marks a shell with integration would send.

type Session = { id: string };
type Step = { id: string; title: string; command?: string };
type Runbook = { id: string; name: string; source: string; agentMayPick: boolean; steps: Step[] };
type LogEntry = { kind: string; message: string };

const FAKE_KEY = 'e2e-not-a-real-gemini-key-0000';
const STEP_CMD = "printf '\\033]133;C\\007rb-step-ok\\n\\033]133;D;0\\007'";
const panel = (screen: any) => screen.getByRole('complementary', 'Agent');
const page = (screen: any) => screen.getByRole('region', 'Runbooks');
const composer = (screen: any) => panel(screen).getByRole('textbox', 'Message the agent');
const approveDialog = (screen: any) => screen.getByRole('dialog').filter({ hasText: 'Type into the terminal?' });
const scrollback = async (app: any, id: string) => (await keelApi<{ text: string }>(app.baseUrl, `/api/sessions/${id}/scrollback`)).text;
const runbooks = async (app: any) => (await keelApi<{ runbooks: Runbook[] }>(app.baseUrl, '/api/agent/runbooks')).runbooks;

async function reset(app: any) {
  await keelSend(app.baseUrl, 'PUT', '/api/agent/keys/gemini', { key: FAKE_KEY });
  await keelSend(app.baseUrl, 'DELETE', '/api/agent/conversations');
  for (const rb of await runbooks(app)) await keelSend(app.baseUrl, 'DELETE', `/api/agent/runbooks/${rb.id}`);
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

async function openRunbooksPage(screen: any) {
  await screen.getByRole('textbox', 'Terminal input', { visible: true }).press('Meta+k');
  const palette = screen.getByRole('dialog', 'Command palette');
  await palette.getByRole('combobox').fill('Runbooks');
  await palette.getByRole('option').filter({ hasText: 'Runbooks' }).first().tap();
  await expect(page(screen)).toBeVisible();
}

async function closeTabs(app: any, screen: any) {
  for (let i = 0; i < 4; i++) {
    const close = screen.getByRole('button', 'Close tab', { visible: true });
    if (!(await close.count())) break;
    await close.first().tap();
  }
  for (const s of await keelApi<Session[]>(app.baseUrl, '/api/sessions')) await keelSend(app.baseUrl, 'DELETE', `/api/sessions/${s.id}`).catch(() => {});
}

test('create a runbook, use it: step 1 is proposed, approved, typed, and its exit status is recorded', { tags: ['agent'] }, async ({ app, screen }) => {
  await reset(app);
  await openKeel(app);
  const shell = await openLocalShell(app, screen);
  await openRunbooksPage(screen);

  // New runbook in the editor.
  await page(screen).getByRole('button', 'New', { exact: true }).tap();
  await page(screen).getByRole('textbox', 'Runbook name').fill('keel-rb-e2e marker check');
  await page(screen).getByRole('textbox', 'Use when').fill('The e2e marker needs checking');
  await page(screen).getByRole('textbox', 'Step 1 title').fill('Print the marker');
  await page(screen).getByRole('textbox', 'Step 1 command').fill(STEP_CMD);
  await page(screen).getByRole('button', 'Add step').tap();
  await page(screen).getByRole('textbox', 'Step 2 title').fill('Look at the output');
  await page(screen).getByRole('button', /^Save/).tap();
  await expect(screen.getByText('Runbook saved')).toBeVisible();
  let list: Runbook[] = [];
  await expect.poll(async () => (list = await runbooks(app)).length).toBe(1);
  expect(list[0]).toMatchObject({ name: 'keel-rb-e2e marker check', source: 'user', agentMayPick: true });
  expect(list[0].steps.map((s) => s.title)).toEqual(['Print the marker', 'Look at the output']);
  await expect(page(screen).getByRole('button', 'Open keel-rb-e2e marker check')).toBeVisible();
  await app.screenshot('runbooks-page');

  // Use in agent: the panel asks with the runbook; keel serve proposes step 1.
  await page(screen).getByRole('button', 'Use in agent').tap();
  await expect(panel(screen)).toBeVisible();
  const run = panel(screen).getByRole('group', 'Runbook keel-rb-e2e marker check');
  await expect(run).toBeVisible();
  await expect(panel(screen)).toContainText('Used runbook · keel-rb-e2e marker check');
  await expect(panel(screen)).toContainText('step 1 is current');
  await expect(run).toContainText('You picked this runbook.');
  const card = panel(screen).getByRole('group', 'Proposal').last();
  await expect(card).toContainText('Step 1 · Run on');
  // Nothing has been typed: the step waits for the user.
  expect(await scrollback(app, shell.id)).not.toContain("rb-step-ok");

  await card.getByRole('button', /Type into pane/).tap();
  await expect(approveDialog(screen)).toBeVisible();
  await expect(approveDialog(screen)).toContainText('133;D;0');
  await approveDialog(screen).getByRole('button', 'Type', { exact: true }).tap();
  await expect(card).toContainText('Typed into');
  // The shell's marks report exit 0 for the step.
  await expect(run).toContainText('exit 0 · reported', { timeout: 15_000 });
  await app.screenshot('runbook-in-use');
  const logs = await keelApi<LogEntry[]>(app.baseUrl, '/api/logs');
  expect(logs.some((e) => e.kind === 'agent.runbook' && e.message.includes('exit 0 reported by the shell'))).toBe(true);
  expect(logs.filter((e) => e.kind === 'agent.runbook').some((e) => e.message.includes('marker check'))).toBe(false);

  // Step 2 has no command: the user marks it done and the runbook ends.
  await expect(run).toContainText('Step 2 has no command');
  await run.getByRole('button', 'Mark done').tap();
  await expect(run).toContainText('All steps are done or skipped.');

  await panel(screen).getByRole('button', 'Close agent').tap().catch(() => {});
  await closeTabs(app, screen);
  await reset(app);
});

test('the agent proposes a runbook: kept only on Save, then reviewed and enabled', { tags: ['agent'] }, async ({ app, screen }) => {
  await reset(app);
  await openKeel(app);
  await openLocalShell(app, screen);
  await screen.getByRole('textbox', 'Terminal input', { visible: true }).press('Meta+i');
  await expect(composer(screen)).toBeFocused();
  await composer(screen).fill('save this as a runbook');
  await composer(screen).press('Enter');
  const card = panel(screen).getByRole('group', 'Proposed runbook');
  await expect(card).toBeVisible();
  await expect(card).toContainText('keel-runbook-e2e proposed');
  await expect(card).toContainText('After reading panes');
  expect(await runbooks(app)).toHaveLength(0);
  await app.screenshot('runbook-propose');
  await card.getByRole('button', 'Save anyway').tap();
  await expect(card).toContainText('Saved to Runbooks');
  let list: Runbook[] = [];
  await expect.poll(async () => (list = await runbooks(app)).length).toBe(1);
  expect(list[0]).toMatchObject({ source: 'agent', agentMayPick: false });

  // On the page it waits for a review; Enable turns Agent may pick on.
  await card.getByRole('button', 'Open Runbooks').tap();
  await expect(page(screen)).toBeVisible();
  await page(screen).getByRole('button', 'Open keel-runbook-e2e proposed').tap();
  await expect(page(screen)).toContainText('Not enabled yet. Review before enabling.');
  await page(screen).getByRole('button', 'Enable').tap();
  await expect.poll(async () => (await runbooks(app))[0].agentMayPick).toBe(true);
  await expect(page(screen)).not.toContainText('Not enabled yet');

  await closeTabs(app, screen);
  await reset(app);
});

test('edit and delete a runbook on the page', { tags: ['agent'] }, async ({ app, screen }) => {
  await reset(app);
  await keelSend(app.baseUrl, 'POST', '/api/agent/runbooks', { name: 'keel-rb-e2e edit me', agentMayPick: true, steps: [{ title: 'One', command: 'echo one' }] });
  await openKeel(app);
  await openLocalShell(app, screen);
  await openRunbooksPage(screen);
  await page(screen).getByRole('button', 'Open keel-rb-e2e edit me').tap();
  await page(screen).getByRole('textbox', 'Runbook name').fill('keel-rb-e2e edited');
  await page(screen).getByRole('textbox', 'Step 1 title').press('Meta+s');
  await expect.poll(async () => (await runbooks(app))[0]?.name).toBe('keel-rb-e2e edited');
  // The list's switch turns Agent may pick off.
  await page(screen).getByRole('switch', 'Agent may pick keel-rb-e2e edited').tap();
  await expect.poll(async () => (await runbooks(app))[0]?.agentMayPick).toBe(false);
  await page(screen).getByRole('button', 'Delete', { exact: true }).tap();
  const confirm = screen.getByRole('alertdialog');
  await expect(confirm).toBeVisible();
  await confirm.getByRole('button', 'Delete').tap();
  await expect.poll(async () => (await runbooks(app)).length).toBe(0);
  await expect(page(screen)).toContainText('No runbooks yet');
  await closeTabs(app, screen);
});

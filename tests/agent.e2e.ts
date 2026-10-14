import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelApi, keelJson, keelSend, openKeel } from './keel.ts';

// Agent panel (docs/ui-redesign.md §5.4, phase 6b) with the §6 invariants
// as acceptance criteria. keel serve runs with KEEL_AGENT_PROVIDER=fake
// (e2e.config.ts): answers come from an in-process fake that sends nothing
// anywhere and reports how many pane excerpts and redacted values it got.
// No real Gemini key is used; the "key" below is only stored in the
// scratch vault, so the panel leaves its Connect Gemini state.

type Session = { id: string };
type LogEntry = { kind: string; sessionId?: string; hostId?: string; lines?: number; bytes?: number; redactions?: Record<string, number> };
type Host = { id: string; name: string; authType: string; [k: string]: unknown };

async function localShell(app: any): Promise<Host> {
  const hosts = await keelApi<Host[]>(app.baseUrl, '/api/hosts');
  return hosts.find((h) => h.authType === 'local')!;
}

const FAKE_KEY = 'e2e-not-a-real-gemini-key-0000';

const panel = (screen: any) => screen.getByRole('complementary', 'Agent');
const composer = (screen: any) => panel(screen).getByRole('textbox', 'Message the agent');
const approveDialog = (screen: any) => screen.getByRole('dialog').filter({ hasText: 'Type into the terminal?' });

async function openHost(app: any, screen: any, name: RegExp): Promise<Session> {
  const before = await keelApi<Session[]>(app.baseUrl, '/api/sessions');
  const vault = screen.getByRole('complementary', 'Vault');
  await vault.getByRole('button', 'Hosts').tap();
  await vault.getByRole('button', name).first().tap();
  await expect(screen.getByRole('textbox', 'Terminal input', { visible: true })).toBeFocused();
  let now: Session[] = [];
  await expect.poll(async () => (now = await keelApi<Session[]>(app.baseUrl, '/api/sessions')).length).toBe(before.length + 1);
  return now.find((s) => !before.some((b) => b.id === s.id))!;
}

const scrollback = async (app: any, id: string) => (await keelApi<{ text: string }>(app.baseUrl, `/api/sessions/${id}/scrollback`)).text;
const logs = (app: any) => keelApi<LogEntry[]>(app.baseUrl, '/api/logs');

async function typeInShell(screen: any, line: string) {
  const input = screen.getByRole('textbox', 'Terminal input', { visible: true });
  await input.pressSequentially(line);
  await input.press('Enter');
}

async function openPanel(screen: any) {
  await screen.getByRole('textbox', 'Terminal input', { visible: true }).press('Meta+i');
  await expect(panel(screen)).toBeVisible();
  await expect(composer(screen)).toBeFocused();
}

async function closeTabs(screen: any) {
  for (let i = 0; i < 4; i++) {
    const close = screen.getByRole('button', 'Close tab', { visible: true });
    if (!(await close.count())) return;
    await close.first().tap();
  }
}

test('no key: Connect Gemini shows the free-tier warning and stores the key in the vault', { tags: ['agent'] }, async ({ app, screen }) => {
  await keelSend(app.baseUrl, 'DELETE', '/api/agent/keys/gemini');
  await openKeel(app);
  await openHost(app, screen, /^Local Shell\b/);

  // ⌘I opens the panel; the titlebar button says so.
  await openPanel(screen);
  await expect(screen.getByRole('button', /^Agent/)).toHaveAttribute('aria-pressed', 'true');
  await expect(panel(screen).getByRole('heading', 'Connect Gemini')).toBeVisible();
  await expect(panel(screen)).toContainText('On the free tier of the Gemini API');
  await expect(panel(screen).getByRole('link', 'Gemini API terms')).toHaveAttribute('href', 'https://ai.google.dev/gemini-api/terms');
  await app.screenshot('agent-connect');

  // Asking without a key keeps the Connect state; nothing is read.
  await panel(screen).getByLabel('Gemini API key').fill(FAKE_KEY);
  await panel(screen).getByRole('button', 'Connect').tap();
  await expect(panel(screen).getByText('Ask about your sessions')).toBeVisible();
  const settings = await keelApi<{ keys: Record<string, { configured: boolean }> }>(app.baseUrl, '/api/agent/settings');
  expect(settings.keys.gemini.configured).toBe(true);
  expect(keelJson()).not.toContain(FAKE_KEY);

  // ⌘I from the composer closes it again.
  await composer(screen).press('Meta+i');
  await expect(panel(screen)).toBeHidden();
  await expect(screen.getByRole('button', /^Agent/)).toHaveAttribute('aria-pressed', 'false');
  await closeTabs(screen);
});

test('ask: trace, answer and a read-only proposal typed only after approval', { tags: ['agent'] }, async ({ app, screen }) => {
  await keelSend(app.baseUrl, 'PUT', '/api/agent/keys/gemini', { key: FAKE_KEY });
  await openKeel(app);
  const session = await openHost(app, screen, /^Local Shell\b/);
  // A secret in the scrollback: the fake reports it arrived redacted.
  await typeInShell(screen, 'echo e2e-before-agent API_TOKEN=ghp_abcdefghijklmnopqrstuvwxyz0123456789');
  await expect.poll(() => scrollback(app, session.id)).toContain('e2e-before-agent');

  await openPanel(screen);
  // Default scope is the focused pane.
  await expect(panel(screen).getByRole('button', 'Choose what the agent can read')).toContainText(/Reads 1 of \d+ panes?/);
  await expect(panel(screen).getByText('Local Shell', { exact: true }).first()).toBeVisible();

  await composer(screen).fill('what is going on in this pane?');
  await composer(screen).press('Enter');
  await expect(panel(screen)).toContainText(/Read Local Shell · last \d+ lines? · \d+ redacted/);
  await expect(panel(screen)).toContainText(/given 1 pane excerpt and [1-9]\d* redacted values?/);
  await expect(panel(screen)).toContainText('Test provider · nothing left this machine');

  const card = panel(screen).getByRole('group', 'Proposal').last();
  await expect(card).toContainText('echo keel-agent-ok');
  await expect(card).toContainText('Read-only');
  await expect(card).toContainText('1 line · 20 bytes');
  await expect(card).toContainText('^Uecho keel-agent-ok↵'); // control bytes as glyphs
  await app.screenshot('agent-proposal');

  // The proposal is in no pane yet: only the agent's card shows it.
  expect(await scrollback(app, session.id)).not.toContain('keel-agent-ok');
  await card.getByRole('button', /Type into pane/).tap();
  await expect(approveDialog(screen)).toBeVisible();
  await expect(approveDialog(screen)).toContainText('echo keel-agent-ok');
  await app.screenshot('agent-approve');
  // Still nothing in the pane while the dialog waits.
  expect(await scrollback(app, session.id)).not.toContain('keel-agent-ok');
  await approveDialog(screen).getByRole('button', 'Type', { exact: true }).tap();
  await expect.poll(() => scrollback(app, session.id), { timeout: 10_000 }).toMatch(/keel-agent-ok[\s\S]*keel-agent-ok/);
  await expect(card).toContainText('Typed into Local Shell');

  // §6.7: the read and the type are in keel.log, with counts, not content.
  const entries = await logs(app);
  const read = entries.find((e) => e.kind === 'agent.read' && e.sessionId === session.id);
  const typed = entries.find((e) => e.kind === 'agent.type' && e.sessionId === session.id);
  expect(read?.lines).toBeGreaterThan(0);
  expect(Object.keys(read?.redactions || {}).length).toBeGreaterThan(0);
  expect(typed?.bytes).toBe(20);
  expect(JSON.stringify(entries)).not.toContain('ghp_abcdefghijklmnopqrstuvwxyz0123456789');
  await closeTabs(screen);
});

test('production host: a state change needs Shift; destructive needs Shift anywhere', { tags: ['agent'] }, async ({ app, screen }) => {
  await keelSend(app.baseUrl, 'PUT', '/api/agent/keys/gemini', { key: FAKE_KEY });
  // Local Shell is the only host with a shell here; mark it production for
  // this test and put it back after (local hosts cannot be deleted).
  const local = await localShell(app);
  await keelSend(app.baseUrl, 'PUT', `/api/hosts/${local.id}`, { ...local, production: true });
  try {
    await openKeel(app);
    const session = await openHost(app, screen, /^Local Shell\b/);
    await openPanel(screen);

    // A state change on production: plain Type does nothing.
    await composer(screen).fill('touch the marker');
    await composer(screen).press('Enter');
    const card = panel(screen).getByRole('group', 'Proposal').last();
    await expect(card).toContainText('touch /tmp/keel-agent-e2e-touched');
    await expect(card).toContainText('Changes state');
    await expect(card).toContainText('Production');
    await expect(card).toContainText('Changes state on a production host. Hold ⇧ when you press Type.');
    await card.getByRole('button', /Type into pane/).tap();
    await expect(card).toContainText('Not typed. Changes state on a production host: hold ⇧ and press Type.');
    await expect(approveDialog(screen)).toBeHidden();
    await app.screenshot('agent-shift');

    // ⇧ + Type opens the approve dialog; approving types it.
    await card.getByRole('button', /Type into pane/).tap({ modifiers: ['Shift'] });
    await expect(approveDialog(screen)).toBeVisible();
    await approveDialog(screen).getByRole('button', 'Type', { exact: true }).tap();
    await expect(card).toContainText('Typed into Local Shell');
    await expect.poll(() => scrollback(app, session.id)).toContain('keel-agent-e2e-touched');

    // Destructive: plain Type refused; ⇧ reaches the dialog, which we cancel.
    await composer(screen).fill('delete the scratch folder');
    await composer(screen).press('Enter');
    const de = panel(screen).getByRole('group', 'Proposal').last();
    await expect(de).toContainText('rm -rf /tmp/keel-agent-e2e');
    await expect(de).toContainText('Destructive');
    await de.getByRole('button', /Type into pane/).tap();
    await expect(approveDialog(screen)).toBeHidden();
    await de.getByRole('button', /Type into pane/).tap({ modifiers: ['Shift'] });
    await expect(approveDialog(screen)).toBeVisible();
    await approveDialog(screen).getByRole('button', 'Cancel').tap();
    await expect(approveDialog(screen)).toBeHidden();
    const types = (await logs(app)).filter((e) => e.kind === 'agent.type' && e.sessionId === session.id);
    expect(types).toHaveLength(1); // the touch; never the rm
    await closeTabs(screen);
  } finally {
    await keelSend(app.baseUrl, 'PUT', `/api/hosts/${local.id}`, { ...local, production: false });
  }
});

test('scope picker: unreadable hosts cannot be picked; new conversation resets', { tags: ['agent'] }, async ({ app, screen }) => {
  await keelSend(app.baseUrl, 'PUT', '/api/agent/keys/gemini', { key: FAKE_KEY });
  // Turn off "Agent can read" on Local Shell for this test, then restore it.
  const local = await localShell(app);
  await keelSend(app.baseUrl, 'PUT', `/api/hosts/${local.id}`, { ...local, agentReadable: false });
  try {
    await openKeel(app);
    const offSession = await openHost(app, screen, /^Local Shell\b/);
    await typeInShell(screen, 'echo NOREAD-MARKER-e2e');
    await openPanel(screen);
    // The focused pane is on a host the agent cannot read: nothing in scope.
    const chip = panel(screen).getByRole('button', 'Choose what the agent can read');
    await expect(chip).toContainText(/Reads 0 of \d+ panes?/);
    await chip.tap();
    const picker = panel(screen).getByRole('dialog', 'Panes the agent reads');
    await expect(picker).toBeVisible();
    const row = picker.getByRole('menuitemcheckbox').filter({ hasText: 'Local Shell' }).first();
    await expect(row).toContainText("Agent can't read");
    await expect(row).toHaveAttribute('aria-disabled', 'true');
    await expect(row).toHaveAttribute('aria-checked', 'false');
    await app.screenshot('agent-picker');
    await picker.press('Escape');
    await expect(picker).toBeHidden();

    // Asking reads nothing from it (§6.2).
    await composer(screen).fill('anything here?');
    await composer(screen).press('Enter');
    await expect(panel(screen)).toContainText('given 0 pane excerpts');
    expect((await logs(app)).some((e) => e.kind === 'agent.read' && e.sessionId === offSession.id)).toBe(false);

    // @ in the composer opens the picker too.
    await composer(screen).pressSequentially('@');
    await expect(picker).toBeVisible();
    await picker.press('Escape');

    await panel(screen).getByRole('button', 'New conversation').tap();
    await expect(panel(screen).getByText('Ask about your sessions')).toBeVisible();
    await expect(panel(screen).getByRole('group', 'Proposal')).toHaveCount(0);
    await closeTabs(screen);
  } finally {
    await keelSend(app.baseUrl, 'PUT', `/api/hosts/${local.id}`, { ...local, agentReadable: true });
  }
});

async function shareLink(screen: any): Promise<string> {
  await screen.getByRole('button', 'Share').tap();
  const pop = screen.getByRole('dialog', 'Share session');
  const link = pop.getByRole('textbox', 'Join link');
  await expect(link).toHaveValue(/\/join\?room=[^#]+#k=.+/, { timeout: 15_000 });
  const url = await link.inputValue();
  await pop.press('Escape');
  return url;
}

test('a joined (follower) tab has no agent panel and its pane is never in scope', { tags: ['agent'], timeout: 120_000 }, async ({ app, screen, browser }) => {
  await keelSend(app.baseUrl, 'PUT', '/api/agent/keys/gemini', { key: FAKE_KEY });
  await openKeel(app);
  await openHost(app, screen, /^Local Shell\b/);
  await openPanel(screen);
  const chip = panel(screen).getByRole('button', 'Choose what the agent can read');
  await expect(chip).toContainText(/Reads 1 of \d+ panes?/);
  // Earlier tests' sessions may still be restored as tabs: count, don't assume.
  const before = (await chip.textContent())?.trim();

  const url = await shareLink(screen);
  await screen.getByRole('button', 'New tab').tap();
  await screen.getByRole('region', 'New tab').getByRole('button', /Join session/).tap();
  const dialog = screen.getByRole('dialog').filter({ hasText: 'Join a session' });
  await dialog.getByLabel('Join link').fill(url);
  await dialog.getByRole('button', 'Join').tap();
  const prompt = screen.getByRole('region', 'Share requests').getByRole('alertdialog');
  await expect(prompt).toContainText('e2e wants to watch', { timeout: 15_000 });
  await prompt.getByRole('button', 'Allow').tap();
  await expect(screen.getByRole('status').filter({ hasText: "Following e2e's session on" })).toBeVisible({ timeout: 30_000 });

  // §5.7: no panel and no Agent button on the joined tab; ⌘I says why.
  await expect(panel(screen)).toBeHidden();
  await expect(screen.getByRole('button', /^Agent/)).toBeHidden();
  await browser.keyboard.press('Meta+i');
  await expect(screen.getByText('The agent is off in joined sessions')).toBeVisible();
  await app.screenshot('agent-joined');

  // Back on the owner's tab: the joined pane is not counted (§6.6).
  await screen.getByRole('button', 'Local Shell', { visible: true }).first().tap();
  await expect(panel(screen)).toBeVisible();
  await expect(chip).toHaveText(before!);
  await closeTabs(screen);
});

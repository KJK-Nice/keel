import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelApi, keelSend, openKeel } from './keel.ts';

// Notebooks and the agent (docs/ui-redesign.md §18, §6 rule 12). keel serve
// runs with KEEL_AGENT_PROVIDER=fake: it says how many attached notebooks
// arrived, and "write this up" makes it propose a notebook draft (with a
// [secret:1] fence for "placeholder").

type Session = { id: string };
type Notebook = { id: string; name: string; source?: string };
type LogEntry = { kind: string; message: string; source?: string };

const FAKE_KEY = 'e2e-not-a-real-gemini-key-0000';
const NB_NAME = 'keel-nb-e2e attached';
// A fake token: secret-looking, so keel serve must redact it before sending.
const NB_TOKEN = 'API_TOKEN=Zx81Kq0LmN4pR7tV2wY5bC9dF3gH6jK';
const DRAFT_NAME = 'keel-notebook-e2e draft';
const panel = (screen: any) => screen.getByRole('complementary', 'Agent');
const composer = (screen: any) => panel(screen).getByRole('textbox', 'Message the agent');
const notebooks = (app: any) => keelApi<Notebook[]>(app.baseUrl, '/api/notebooks');

async function reset(app: any) {
  await keelSend(app.baseUrl, 'PUT', '/api/agent/keys/gemini', { key: FAKE_KEY });
  await keelSend(app.baseUrl, 'DELETE', '/api/agent/conversations');
  for (const nb of await notebooks(app)) {
    if (nb.name.startsWith('keel-nb-e2e') || nb.name.startsWith('keel-notebook-e2e')) await keelSend(app.baseUrl, 'DELETE', `/api/notebooks/${nb.id}`);
  }
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

test('attach a notebook: only it is sent, redacted, and the trace says so', { tags: ['agent', 'notebook'] }, async ({ app, screen, browser }) => {
  await reset(app);
  const nb = await keelSend<Notebook>(app.baseUrl, 'POST', '/api/notebooks', { name: NB_NAME, cells: [{ body: `## Deploy\n\n${NB_TOKEN}\n\n\`\`\`sh\nsystemctl status app\n\`\`\`` }] });
  await keelSend<Notebook>(app.baseUrl, 'POST', '/api/notebooks', { name: 'keel-nb-e2e other', cells: [{ body: 'NOT-ATTACHED' }] });
  await openKeel(app);
  await openLocalShell(app, screen);
  await screen.getByRole('textbox', 'Terminal input', { visible: true }).press('Meta+i');
  await expect(composer(screen)).toBeFocused();

  // Asked with no notebook: none is sent.
  await composer(screen).fill('is this machine fine? no command');
  await composer(screen).press('Enter');
  await expect(panel(screen)).toContainText('Test provider: nothing left this machine');
  await expect(panel(screen)).not.toContainText('attached notebook');

  // Attach notebook: the menu lists saved notebooks; the ticked one becomes a chip.
  await panel(screen).getByRole('button', 'Attach notebook').tap();
  const menu = panel(screen).getByRole('dialog', 'Notebooks to send with your question');
  await expect(menu).toBeVisible();
  await menu.getByRole('menuitemcheckbox', new RegExp(NB_NAME)).tap();
  await menu.press('Escape');
  await expect(panel(screen).getByRole('button', `Stop sending ${NB_NAME}`)).toBeVisible();
  await composer(screen).fill('what does this notebook do? no command');
  await composer(screen).press('Enter');
  await expect(panel(screen)).toContainText('I was also given 1 attached notebook.');
  await expect(panel(screen)).toContainText(new RegExp(`Sent notebook ${NB_NAME} \\(\\d+ bytes, [1-9]\\d* redacted\\)`));
  await app.screenshot('agent-notebook-attached');

  // keel.log: agent.read with the notebook's id and bytes, never its name or text.
  const logs = await keelApi<LogEntry[]>(app.baseUrl, '/api/logs');
  const reads = logs.filter((e) => e.kind === 'agent.read' && e.message.includes(nb.id));
  expect(reads.length).toBeGreaterThan(0);
  expect(logs.filter((e) => e.kind.startsWith('agent.')).some((e) => e.message.includes(NB_NAME) || e.message.includes('Zx81Kq0'))).toBe(false);

  // Removing the chip stops sending it.
  await panel(screen).getByRole('button', `Stop sending ${NB_NAME}`).tap();
  await composer(screen).fill('and now? no command');
  await composer(screen).press('Enter');
  await expect.poll(() => browser.evaluate(`document.querySelectorAll('#agent-thread [data-agent-turn]').length`)).toBe(3);
  await expect
    .poll(() => browser.evaluate(`[...document.querySelectorAll('#agent-thread [data-agent-turn]')].at(-1)?.textContent.includes('nothing left this machine')`))
    .toBe(true);
  await expect
    .poll(() => browser.evaluate(`(document.querySelector('#agent-thread').textContent.match(/1 attached notebook/g) || []).length`))
    .toBe(1);

  // From the notebook pane: Ask the agent about this notebook attaches it.
  const vault = screen.getByRole('complementary', 'Vault');
  await vault.getByRole('button', 'Snippets').tap();
  await vault.getByRole('button', new RegExp(`^${NB_NAME}$`)).tap();
  await expect(screen.getByRole('textbox', 'Rename notebook')).toHaveValue(NB_NAME);
  await screen.getByRole('button', 'Ask the agent about this notebook').tap();
  await expect(panel(screen).getByRole('button', `Stop sending ${NB_NAME}`)).toBeVisible();

  await panel(screen).getByRole('button', 'Close agent').tap().catch(() => {});
  await closeTabs(app, screen);
  await reset(app);
});

test('the agent drafts a notebook: reviewed in the editor, saved only on Save, listed From agent', { tags: ['agent', 'notebook'] }, async ({ app, screen }) => {
  await reset(app);
  const before = (await notebooks(app)).length;
  await openKeel(app);
  await openLocalShell(app, screen);
  await screen.getByRole('textbox', 'Terminal input', { visible: true }).press('Meta+i');
  await composer(screen).fill('write this up with a placeholder');
  await composer(screen).press('Enter');
  const card = panel(screen).getByRole('group', 'Notebook draft');
  await expect(card).toBeVisible();
  await expect(card).toContainText(DRAFT_NAME);
  await expect(card).toContainText('Placeholders');
  await expect(card).toContainText('Nothing is saved until you open the draft');
  expect((await notebooks(app)).length).toBe(before);

  // Open draft: an unsaved notebook pane with the review banner.
  await card.getByRole('button', 'Open draft').tap();
  await expect(screen.getByRole('textbox', 'Rename notebook')).toHaveValue(DRAFT_NAME);
  const review = screen.getByRole('note', 'Review this draft');
  await expect(review).toContainText('Draft from the agent. Not saved yet.');
  await expect(screen.getByRole('note').filter({ hasText: 'This draft holds placeholders such as [secret:1]' })).toBeVisible();
  expect((await notebooks(app)).length).toBe(before);

  // The fence holding [secret:1] is not typed: no approve dialog opens.
  const types = screen.getByRole('button', 'Type into Local Shell');
  await expect(types).toHaveCount(2);
  await types.last().tap();
  await expect(screen.getByText(/Not typed: this block holds a redacted placeholder/)).toBeVisible();
  await expect(screen.getByRole('dialog').filter({ hasText: 'Type into the terminal?' })).toBeHidden();

  // Save notebook keeps it, as a new notebook marked from the agent.
  await review.getByRole('button', 'Save notebook').tap();
  await expect(review).toBeHidden();
  let list: Notebook[] = [];
  await expect.poll(async () => (list = await notebooks(app)).length).toBe(before + 1);
  const saved = list.find((n) => n.name === DRAFT_NAME)!;
  expect(saved.source).toBe('agent');
  await expect(card).toContainText('Saved to Notebooks');
  const logs = await keelApi<LogEntry[]>(app.baseUrl, '/api/logs');
  expect(logs.some((e) => e.kind === 'agent.notebook' && e.message.includes('saved by you') && e.message.includes(saved.id))).toBe(true);
  expect(logs.filter((e) => e.kind === 'agent.notebook').some((e) => e.message.includes(DRAFT_NAME))).toBe(false);

  // The Notebooks page lists it with a From agent badge.
  const vault = screen.getByRole('complementary', 'Vault');
  await vault.getByRole('button', 'Snippets').tap();
  await vault.getByRole('button', 'All notebooks').tap();
  const nbPage = screen.getByRole('region', 'Notebooks');
  await expect(nbPage.getByRole('button', new RegExp(`^${DRAFT_NAME}`))).toBeVisible();
  await expect(nbPage).toContainText('From agent');
  await app.screenshot('notebooks-from-agent');

  await panel(screen).getByRole('button', 'Close agent').tap().catch(() => {});
  await closeTabs(app, screen);
  await reset(app);
});

test('a notebook draft closed without Save is not kept', { tags: ['agent', 'notebook'] }, async ({ app, screen }) => {
  await reset(app);
  const before = (await notebooks(app)).length;
  await openKeel(app);
  await openLocalShell(app, screen);
  await screen.getByRole('textbox', 'Terminal input', { visible: true }).press('Meta+i');
  await composer(screen).fill('write this up');
  await composer(screen).press('Enter');
  const card = panel(screen).getByRole('group', 'Notebook draft');
  await expect(card).toBeVisible();
  await expect(card).not.toContainText('Placeholders');
  await card.getByRole('button', 'Open draft').tap();
  const title = screen.getByRole('textbox', 'Rename notebook');
  await expect(title).toHaveValue(DRAFT_NAME);
  // An edit does not save a draft either.
  await title.fill('keel-notebook-e2e edited draft');
  await screen.getByRole('button', 'Close pane', { visible: true }).last().tap();
  await expect(screen.getByRole('textbox', 'Rename notebook')).toBeHidden();
  await new Promise((r) => setTimeout(r, 800));
  expect((await notebooks(app)).length).toBe(before);
  // The card still offers the draft.
  await expect(card.getByRole('button', 'Open draft')).toBeVisible();

  await panel(screen).getByRole('button', 'Close agent').tap().catch(() => {});
  await closeTabs(app, screen);
  await reset(app);
});

import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { createServer } from 'node:http';
import { keelApi, keelSend, openKeel } from './keel.ts';

// Notebooks and the agent (docs/ui-redesign.md §18, §6 rule 12). keel serve
// runs with KEEL_AGENT_PROVIDER=fake: it says how many attached notebooks
// arrived and how many it read; "read / edit / drop the notebook <title>"
// asks to read it, edits it (keel-edit-me → keel-edited-by-agent) or
// proposes deleting it; "write this up" creates a notebook (with a
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

async function openNotebookFromVault(screen: any, name: string) {
  const vault = screen.getByRole('complementary', 'Vault');
  await vault.getByRole('button', 'Snippets').tap();
  await vault.getByRole('button', new RegExp(`^${name}$`)).tap();
  await expect(screen.getByRole('textbox', 'Rename notebook')).toHaveValue(name);
}

const text = async (app: any, id: string) => {
  const nb = (await keelApi<(Notebook & { cells: { body: string }[] })[]>(app.baseUrl, '/api/notebooks')).find((n) => n.id === id);
  return nb ? nb.cells.map((c) => c.body).join('\n\n') : null;
};

test('the agent reads a notebook on its own; a private one it never sees', { tags: ['agent', 'notebook'] }, async ({ app, screen }) => {
  await reset(app);
  const nb = await keelSend<Notebook>(app.baseUrl, 'POST', '/api/notebooks', { name: 'keel-nb-e2e runbook', cells: [{ body: `## Restart\n\n${NB_TOKEN}\n\n\`\`\`sh\nsystemctl restart app\n\`\`\`` }] });
  const priv = await keelSend<Notebook>(app.baseUrl, 'POST', '/api/notebooks', { name: 'keel-nb-e2e private', cells: [{ body: 'PRIVATE-E2E-BODY keel-edit-me' }] });
  await openKeel(app);
  await openLocalShell(app, screen);

  // Private is the user's switch, on the notebook's meta line.
  await openNotebookFromVault(screen, 'keel-nb-e2e private');
  await screen.getByRole('button', 'Agent can read').tap();
  await expect(screen.getByRole('button', 'Private')).toHaveAttribute('aria-pressed', 'true');
  await expect.poll(async () => (await keelApi<any[]>(app.baseUrl, '/api/notebooks')).find((n) => n.id === priv.id)?.private).toBe(true);

  await screen.getByRole('textbox', 'Rename notebook').press('Meta+i').catch(() => {});
  await screen.getByRole('button', 'Ask the agent about this notebook').tap();
  // Asking about a private notebook does not attach it.
  await expect(panel(screen).getByRole('button', 'Stop sending keel-nb-e2e private')).toHaveCount(0);
  // The Attach menu lists it with a Private badge, and it cannot be ticked.
  await panel(screen).getByRole('button', 'Attach notebook').tap();
  const menu = panel(screen).getByRole('dialog', 'Notebooks to send with your question');
  await expect(menu.getByRole('menuitemcheckbox', /keel-nb-e2e private/)).toHaveAttribute('aria-disabled', 'true');
  await menu.press('Escape');

  // Read on its own: no attachment; the trace says the agent asked.
  await composer(screen).fill('read the notebook keel-nb-e2e runbook');
  await composer(screen).press('Enter');
  await expect(panel(screen)).toContainText('I read 1 notebook I asked for.');
  await expect(panel(screen)).toContainText(/Read notebook keel-nb-e2e runbook \(\d+ bytes, [1-9]\d* redacted\) · the agent asked/);
  await expect(panel(screen)).toContainText(/Sent the titles of \d+ notebooks? \(not private\)/);

  // The private one: not in the index, so the agent cannot find it.
  await composer(screen).fill('read the notebook keel-nb-e2e private');
  await composer(screen).press('Enter');
  await expect(panel(screen)).toContainText('found no such notebook');
  await composer(screen).fill('edit the notebook keel-nb-e2e private');
  await composer(screen).press('Enter');
  await expect(panel(screen)).toContainText('I found no such notebook to edit.');
  expect(await text(app, priv.id)).toBe('PRIVATE-E2E-BODY keel-edit-me');

  const logs = await keelApi<LogEntry[]>(app.baseUrl, '/api/logs');
  const agentLogs = logs.filter((e) => e.kind.startsWith('agent.'));
  expect(agentLogs.some((e) => e.kind === 'agent.read' && e.message.includes(nb.id) && e.message.includes('at its own request'))).toBe(true);
  expect(agentLogs.some((e) => e.message.includes(priv.id) || e.message.includes('keel-nb-e2e') || e.message.includes('Zx81Kq0'))).toBe(false);

  await panel(screen).getByRole('button', 'Close agent').tap().catch(() => {});
  await closeTabs(app, screen);
  await reset(app);
});

test('the agent edits a notebook at once: the pane shows it, Undo and History restore, typing is kept', { tags: ['agent', 'notebook'] }, async ({ app, screen, browser }) => {
  await reset(app);
  const nb = await keelSend<Notebook>(app.baseUrl, 'POST', '/api/notebooks', { name: 'keel-nb-e2e edit', cells: [{ body: '## Steps\n\nkeel-edit-me\n\n```sh\necho step\n```' }] });
  await openKeel(app);
  await openLocalShell(app, screen);
  await openNotebookFromVault(screen, 'keel-nb-e2e edit');
  const doc = screen.getByText('keel-edit-me');
  await expect(doc).toBeVisible();
  await screen.getByRole('button', 'Ask the agent about this notebook').tap();

  await composer(screen).fill('edit the notebook keel-nb-e2e edit');
  await composer(screen).press('Enter');
  const card = panel(screen).getByRole('group', 'Edited notebook');
  await expect(card).toContainText('Keel kept the version before it');
  await expect.poll(() => text(app, nb.id)).toContain('keel-edited-by-agent');
  // The open pane loads the agent's text, with the review banner.
  await expect(screen.getByText('keel-edited-by-agent')).toBeVisible();
  const banner = screen.getByRole('note', 'Agent edit to review');
  await expect(banner).toBeVisible();
  await app.screenshot('notebook-agent-edited');

  // Undo on the card: back to the user's text.
  await card.getByRole('button', 'Undo').tap();
  await expect(card).toContainText('Undone');
  await expect.poll(() => text(app, nb.id)).toContain('keel-edit-me');
  await expect(screen.getByText('keel-edit-me')).toBeVisible();

  // Typing right before an ask is saved first, so the agent reads it and
  // nothing typed is lost.
  const md = screen.getByRole('textbox', 'Notebook markdown');
  await md.getByText('keel-edit-me').tap();
  await md.press('End');
  await md.pressSequentially(' typed-by-user');
  await composer(screen).fill('edit the notebook keel-nb-e2e edit');
  await composer(screen).press('Enter');
  await expect(panel(screen).getByRole('group', 'Edited notebook')).toHaveCount(2);
  await expect.poll(() => text(app, nb.id)).toContain('keel-edited-by-agent typed-by-user');
  await expect(md).toContainText('keel-edited-by-agent typed-by-user');

  // Mark reviewed clears the banner.
  await screen.getByRole('note', 'Agent edit to review').getByRole('button', 'Mark reviewed').tap();
  await expect(screen.getByRole('note', 'Agent edit to review')).toBeHidden();

  // History has every version; Restore brings back the user's text.
  await screen.getByRole('button', 'History').first().tap();
  const hist = screen.getByRole('dialog', /History · keel-nb-e2e edit/);
  await expect(hist).toBeVisible();
  await expect(hist.getByRole('option').first()).toContainText("Before the agent's edit");
  await expect(hist).toContainText(/lines? added/);
  await app.screenshot('notebook-history');
  await hist.getByRole('button', 'Restore this version').tap();
  await expect(hist).toBeHidden();
  await expect.poll(() => text(app, nb.id)).toContain('keel-edit-me typed-by-user');


  await panel(screen).getByRole('button', 'Close agent').tap().catch(() => {});
  await closeTabs(app, screen);
  await reset(app);
});

test('a delete the agent proposes waits for the user; Recently deleted restores it', { tags: ['agent', 'notebook'] }, async ({ app, screen }) => {
  await reset(app);
  const nb = await keelSend<Notebook>(app.baseUrl, 'POST', '/api/notebooks', { name: 'keel-nb-e2e drop', cells: [{ body: 'DROP-ME' }] });
  await openKeel(app);
  await openLocalShell(app, screen);
  await screen.getByRole('textbox', 'Terminal input', { visible: true }).press('Meta+i');
  await composer(screen).fill('drop the notebook keel-nb-e2e drop');
  await composer(screen).press('Enter');
  const card = panel(screen).getByRole('group', /^Delete notebook/);
  await expect(card).toContainText('Nothing is deleted unless you confirm');
  expect(await text(app, nb.id)).toBe('DROP-ME');
  await app.screenshot('notebook-delete-card');

  // The confirm dialog: Cancel keeps it.
  await card.getByRole('button', 'Delete…').tap();
  const dialog = screen.getByRole('alertdialog');
  await expect(dialog).toContainText('The agent asked for this');
  await dialog.getByRole('button', 'Cancel').tap();
  expect(await text(app, nb.id)).toBe('DROP-ME');
  await card.getByRole('button', 'Delete…').tap();
  await dialog.getByRole('button', 'Delete').tap();
  await expect(card).toContainText('Recently deleted');
  await expect.poll(() => text(app, nb.id)).toBe(null);

  // Restore on the card brings it back.
  await card.getByRole('button', 'Restore').tap();
  await expect(card).toContainText('Restored');
  await expect.poll(() => text(app, nb.id)).toBe('DROP-ME');
  const logs = await keelApi<LogEntry[]>(app.baseUrl, '/api/logs');
  expect(logs.some((e) => e.kind === 'agent.notebook' && e.message.includes('deleted by you, as the agent proposed') && e.message.includes(nb.id))).toBe(true);

  await panel(screen).getByRole('button', 'Close agent').tap().catch(() => {});
  await closeTabs(app, screen);
  await reset(app);
});

test('the agent creates a notebook: From agent, Undo moves it to Recently deleted', { tags: ['agent', 'notebook'] }, async ({ app, screen }) => {
  await reset(app);
  await openKeel(app);
  await openLocalShell(app, screen);
  await screen.getByRole('textbox', 'Terminal input', { visible: true }).press('Meta+i');
  await composer(screen).fill('write this up with a placeholder');
  await composer(screen).press('Enter');
  const card = panel(screen).getByRole('group', 'Created notebook');
  await expect(card).toContainText('Placeholders');
  let list: Notebook[] = [];
  await expect.poll(async () => (list = await notebooks(app)).filter((n) => n.name === DRAFT_NAME).length).toBe(1);
  expect(list.find((n) => n.name === DRAFT_NAME)?.source).toBe('agent');

  // Open: the placeholder block is refused before anything is typed.
  await card.getByRole('button', 'Open').tap();
  await expect(screen.getByRole('textbox', 'Rename notebook')).toHaveValue(DRAFT_NAME);
  await screen.getByRole('button', 'Type into Local Shell').last().tap();
  await expect(screen.getByText(/Not typed: this block holds a redacted placeholder/)).toBeVisible();

  await card.getByRole('button', 'Undo').tap();
  await expect(card).toContainText('Recently deleted');
  await expect.poll(async () => (await notebooks(app)).filter((n) => n.name === DRAFT_NAME).length).toBe(0);

  // Recently deleted on the Notebooks page lists it and restores it.
  await screen.getByRole('textbox', 'Message the agent').press('Meta+k');
  const palette = screen.getByRole('dialog', 'Command palette');
  await palette.getByRole('combobox').fill('Notebooks');
  await palette.getByRole('option').filter({ hasText: 'Notebooks' }).first().tap();
  await screen.getByRole('button', 'Recently deleted').tap();
  const trash = screen.getByRole('dialog', 'Recently deleted');
  await expect(trash.getByRole('option', new RegExp(DRAFT_NAME))).toBeVisible();
  await trash.getByRole('button', 'Restore notebook').tap();
  await expect.poll(async () => (await notebooks(app)).filter((n) => n.name === DRAFT_NAME).length).toBe(1);

  await panel(screen).getByRole('button', 'Close agent').tap().catch(() => {});
  await closeTabs(app, screen);
  await reset(app);
});

// A remote image in a notebook the agent writes (an exfiltration path: the
// URL is the model's choice) is never fetched: keel serve turns it into
// text, and every page carries img-src 'self' data: blob:, so even an
// <img> put in the page directly does not reach another origin (a listener
// on LEAK_PORT counts any request). The image is providertest.LeakImageURL.
const LEAK_PORT = 47199;

test('a remote image in a notebook the agent writes, or anywhere in the page, is never fetched', { tags: ['agent', 'notebook'] }, async ({ app, screen, browser }) => {
  const hits: string[] = [];
  const server = createServer((req, res) => {
    hits.push(req.url || '');
    res.writeHead(204);
    res.end();
  });
  await new Promise<void>((resolve, reject) => {
    server.once('error', reject);
    server.listen(LEAK_PORT, '127.0.0.1', () => resolve());
  });
  try {
    await reset(app);
    await openKeel(app);
    await openLocalShell(app, screen);
    await screen.getByRole('textbox', 'Terminal input', { visible: true }).press('Meta+i');
    await composer(screen).fill('write this up with an image');
    await composer(screen).press('Enter');
    const card = panel(screen).getByRole('group', 'Created notebook');
    await expect(card).toBeVisible();
    await card.getByRole('button', 'Open').tap();
    await expect(screen.getByRole('textbox', 'Rename notebook')).toHaveValue(DRAFT_NAME);
    await expect(screen.getByText(/keel-notebook-leak\.png/).first()).toBeVisible();
    expect(await browser.evaluate(`document.querySelectorAll('.notebook-pane img').length`)).toBe(0);
    const blocked = await browser.evaluate(`new Promise((resolve) => {
      const img = new Image();
      img.onload = () => resolve('loaded');
      img.onerror = () => resolve('blocked');
      img.src = 'http://127.0.0.1:${LEAK_PORT}/csp-check.png';
      document.body.appendChild(img);
      setTimeout(() => resolve('timeout'), 3000);
    })`);
    expect(blocked).toBe('blocked');
    await new Promise((r) => setTimeout(r, 500));
    expect(hits).toEqual([]);
  } finally {
    server.close();
    await panel(screen).getByRole('button', 'Close agent').tap().catch(() => {});
    await closeTabs(app, screen);
    await reset(app);
  }
});

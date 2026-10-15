import { describe, test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelApi, keelSend, openKeel } from './keel.ts';

// History-first suggestions (phase 6c+, docs/ui-redesign.md §5.5): before
// the model writes a command, keel serve gathers commands the user already
// ran in the pane and snippets marked "Let the agent suggest this
// snippet", and Jev picks one. keel serve runs with
// KEEL_AGENT_PROVIDER=fake (e2e.config.ts): the fake Jev
// (providertest.ScriptedJudge) picks the candidate holding "keel-" and the
// words joined by "-" ("history marker" → `echo keel-history-marker`), and
// the fake model (providertest.Scripted) answers `echo keel-suggest-ok`
// when it is asked instead. Nothing leaves the machine.

type Session = { id: string };
type Snippet = { id: string; name: string; command: string; agentUsable?: boolean };
type LogEntry = { kind: string; sessionId?: string; bytes?: number; source?: string; count?: number; message?: string; modelMs?: number };

const themeScript = (theme: string) =>
  `try { if (location.protocol.startsWith('http')) localStorage.setItem('keel-theme', '${theme}'); } catch {}`;
const pause = (ms: number) => new Promise((r) => setTimeout(r, ms));
const term = (screen: any) => screen.getByRole('textbox', 'Terminal input', { visible: true });
const card = (screen: any) => screen.getByRole('dialog', /^Suggested command/);
const scrollback = async (app: any, id: string) => (await keelApi<{ text: string }>(app.baseUrl, `/api/sessions/${id}/scrollback`)).text;
const logs = (app: any) => keelApi<LogEntry[]>(app.baseUrl, '/api/logs');

async function setup(app: any) {
  await keelSend(app.baseUrl, 'PUT', '/api/agent/keys/gemini', { key: 'e2e-not-a-real-gemini-key-0000' });
  await keelSend(app.baseUrl, 'PUT', '/api/agent/keys/jev', { key: 'e2e-not-a-real-jev-key-0000' });
  await keelSend(app.baseUrl, 'PUT', '/api/agent/settings', { suggestions: 'on', detection: 'auto' });
}

async function openLocalShell(app: any, screen: any): Promise<Session> {
  const before = await keelApi<Session[]>(app.baseUrl, '/api/sessions');
  const vault = screen.getByRole('complementary', 'Vault');
  await vault.getByRole('button', 'Hosts').tap();
  await vault.getByRole('button', /^Local Shell\b/).first().tap();
  await expect(term(screen)).toBeFocused();
  let now: Session[] = [];
  await expect.poll(async () => (now = await keelApi<Session[]>(app.baseUrl, '/api/sessions')).length).toBe(before.length + 1);
  const s = now.find((x) => !before.some((b) => b.id === x.id))!;
  await expect.poll(() => scrollback(app, s.id)).toMatch(/\$ $/);
  return s;
}

async function closeTabs(screen: any) {
  for (let i = 0; i < 4; i++) {
    const close = screen.getByRole('button', 'Close tab', { visible: true });
    if (!(await close.count())) return;
    await close.first().tap();
  }
}

describe('history-first suggestions', { tags: ['agent', 'suggest'], serial: true }, () => {
  for (const theme of ['dark', 'light'] as const) {
    test(`a command already run in the pane is the suggestion (${theme})`, async ({ app, screen, browser }) => {
      await setup(app);
      await browser.addInitScript(themeScript(theme));
      await openKeel(app);
      const session = await openLocalShell(app, screen);

      // Run a command: it is now in this pane's history.
      await term(screen).pressSequentially('echo keel-history-marker');
      await term(screen).press('Enter');
      await expect.poll(() => scrollback(app, session.id)).toMatch(/^keel-history-marker\r?$/m);
      await expect.poll(() => scrollback(app, session.id)).toMatch(/\$ $/);

      // Plain words the fake Jev maps to it.
      await term(screen).pressSequentially('history marker');
      await expect(screen.getByText('Looks like a request')).toBeVisible();
      await term(screen).press('Tab');
      await expect(card(screen)).toBeVisible();
      await expect(card(screen)).toContainText(/From this pane's history/i);
      await expect(card(screen)).toContainText('echo keel-history-marker');
      await expect(card(screen)).toContainText('Read-only');
      await expect(card(screen)).toContainText('You ran this in this pane before.');
      // No model call: the model's answer would be echo keel-suggest-ok.
      await expect(card(screen)).not.toContainText('keel-suggest-ok');
      await app.screenshot(`${theme}-suggest-history`);

      // ⇥ inserts it, not run.
      await term(screen).press('Tab');
      await expect(card(screen)).toBeHidden();
      await expect(screen.getByText('Inserted, not run')).toBeVisible();
      await expect.poll(() => scrollback(app, session.id)).toMatch(/echo keel-history-marker$/);

      // §6.7: the pick's egress (counts only), the suggestion and the
      // insert are logged with the source "history"; no words, no command.
      const entries = (await logs(app)).filter((e) => e.sessionId === session.id);
      const sent = entries.filter((e) => e.kind === 'agent.read' && e.source === 'history');
      expect(sent).toHaveLength(1);
      expect(sent[0].message).toContain("1 candidate command (1 from this pane's history, 0 from snippets)");
      const sug = entries.filter((e) => e.kind === 'agent.suggest');
      expect(sug).toHaveLength(1);
      expect(sug[0].source).toBe('history');
      expect(sug[0].modelMs ?? 0).toBe(0);
      const ins = entries.filter((e) => e.kind === 'agent.insert');
      expect(ins).toHaveLength(1);
      expect(ins[0].source).toBe('history');
      expect(ins[0].bytes).toBe('\u0015echo keel-history-marker'.length);
      expect(entries.some((e) => e.kind === 'agent.read' && e.source === 'gemini')).toBe(false);
      expect(JSON.stringify(entries)).not.toContain('history marker');
      expect(JSON.stringify(entries)).not.toContain('keel-history-marker');

      await term(screen).press('Meta+z');
      await expect.poll(() => scrollback(app, session.id)).toMatch(/history marker$/);
      await term(screen).press('Control+u');
      await closeTabs(screen);
    });
  }

  test('a destructive command from history still needs ⇧⇥', async ({ app, screen }) => {
    await setup(app);
    await openKeel(app);
    const session = await openLocalShell(app, screen);
    // rm -rf on a path that does not exist: harmless to run here.
    await term(screen).pressSequentially('rm -rf /tmp/keel-history-rm-e2e-none');
    await term(screen).press('Enter');
    await expect.poll(() => scrollback(app, session.id)).toMatch(/keel-history-rm-e2e-none\r?\n.*\$ $/);

    await term(screen).pressSequentially('history rm');
    await expect(screen.getByText('Looks like a request')).toBeVisible();
    await term(screen).press('Tab');
    await expect(card(screen)).toContainText(/From this pane's history/i);
    await expect(card(screen)).toHaveAttribute('data-risk', 'destructive');
    await expect(card(screen)).toContainText('Deletes files');
    await term(screen).press('Tab');
    await expect(card(screen)).toContainText('Press ⇧⇥ to insert this command.');
    await pause(300);
    expect((await logs(app)).filter((e) => e.kind === 'agent.insert' && e.sessionId === session.id)).toHaveLength(0);
    await term(screen).press('Shift+Tab');
    await expect(screen.getByText('Inserted, not run')).toBeVisible();
    await expect.poll(() => scrollback(app, session.id)).toMatch(/rm -rf \/tmp\/keel-history-rm-e2e-none$/);
    await term(screen).press('Meta+z');
    await expect.poll(() => scrollback(app, session.id)).toMatch(/history rm$/);
    await term(screen).press('Control+u');
    await closeTabs(screen);
  });

  test('a snippet is offered only after "Let the agent suggest this snippet"', async ({ app, screen }) => {
    await setup(app);
    const name = `keel-e2e-snippet-${Date.now()}`;
    const created = await keelSend<Snippet>(app.baseUrl, 'POST', '/api/snippets', { name, command: 'echo keel-snippet-marker\n' });
    expect(created.agentUsable).toBe(false);
    await openKeel(app);

    // Off (the default): the snippet is never sent; the model answers.
    let session = await openLocalShell(app, screen);
    await term(screen).pressSequentially('snippet marker');
    await expect(screen.getByText('Looks like a request')).toBeVisible();
    await term(screen).press('Tab');
    await expect(card(screen)).toContainText(/Suggested from your words/i);
    await expect(card(screen)).toContainText('echo keel-suggest-ok');
    await term(screen).press('Escape');
    await term(screen).press('Control+u');
    await closeTabs(screen);

    // Turn it on in the Snippets editor.
    const vault = screen.getByRole('complementary', 'Vault');
    await vault.getByRole('button', 'Snippets').tap();
    await vault.getByRole('button', 'Manage snippets').tap();
    const page = screen.getByRole('region', 'Snippets');
    await page.getByRole('button', new RegExp(name)).tap();
    await expect(page.getByRole('textbox', 'Snippet name')).toHaveValue(name);
    const sw = page.getByRole('switch', 'Let the agent suggest this snippet');
    await expect(sw).toHaveAttribute('aria-checked', 'false');
    await sw.tap();
    await expect(sw).toHaveAttribute('aria-checked', 'true');
    await app.screenshot('snippets-agent-switch');
    await page.getByRole('button', /^Save/).tap();
    await expect
      .poll(async () => (await keelApi<Snippet[]>(app.baseUrl, '/api/snippets')).find((s) => s.id === created.id)?.agentUsable)
      .toBe(true);

    // On: the snippet is the suggestion.
    session = await openLocalShell(app, screen);
    await term(screen).pressSequentially('snippet marker');
    await expect(screen.getByText('Looks like a request')).toBeVisible();
    await term(screen).press('Tab');
    await expect(card(screen)).toContainText(/From your snippets/i);
    await expect(card(screen)).toContainText('echo keel-snippet-marker');
    await app.screenshot('suggest-snippet');
    await term(screen).press('Tab');
    await expect(screen.getByText('Inserted, not run')).toBeVisible();
    await expect.poll(() => scrollback(app, session.id)).toMatch(/echo keel-snippet-marker$/);
    const ins = (await logs(app)).filter((e) => e.kind === 'agent.insert' && e.sessionId === session.id);
    expect(ins).toHaveLength(1);
    expect(ins[0].source).toBe('snippet');
    await term(screen).press('Meta+z');
    await expect.poll(() => scrollback(app, session.id)).toMatch(/snippet marker$/);
    await term(screen).press('Control+u');
    await closeTabs(screen);
    await keelSend(app.baseUrl, 'DELETE', `/api/snippets/${created.id}`);
  });
});

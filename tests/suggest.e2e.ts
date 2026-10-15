import { describe, test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelApi, keelSend, openKeel } from './keel.ts';

// Plain language → command in a pane (docs/ui-redesign.md §5.5, phase 6c)
// with the §6 invariants as acceptance criteria. keel serve runs with
// KEEL_AGENT_PROVIDER=fake (e2e.config.ts): suggestions come from the
// in-process fake model (providertest.Scripted) and detection and risk from
// the in-process fake Jev (providertest.ScriptedJudge). Neither sends
// anything anywhere; the "keys" below only leave the Connect state.
//
// Each flow runs in both themes and saves the four states as named
// screenshots (<theme>-suggest-notice, -card, -inserted, -destructive).

type Session = { id: string };
type LogEntry = { kind: string; sessionId?: string; bytes?: number; source?: string; count?: number; message?: string };

const FAKE_GEMINI = 'e2e-not-a-real-gemini-key-0000';
const FAKE_JEV = 'e2e-not-a-real-jev-key-0000';
const themeScript = (theme: string) =>
  `try { if (location.protocol.startsWith('http')) localStorage.setItem('keel-theme', '${theme}'); } catch {}`;

const pause = (ms: number) => new Promise((r) => setTimeout(r, ms));
const term = (screen: any) => screen.getByRole('textbox', 'Terminal input', { visible: true });
const card = (screen: any) => screen.getByRole('dialog', /^Suggested command/);
const scrollback = async (app: any, id: string) => (await keelApi<{ text: string }>(app.baseUrl, `/api/sessions/${id}/scrollback`)).text;
const logs = (app: any) => keelApi<LogEntry[]>(app.baseUrl, '/api/logs');

async function setup(app: any) {
  await keelSend(app.baseUrl, 'PUT', '/api/agent/keys/gemini', { key: FAKE_GEMINI });
  await keelSend(app.baseUrl, 'PUT', '/api/agent/keys/jev', { key: FAKE_JEV });
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

describe('suggestions in a pane', { tags: ['agent', 'suggest'], serial: true }, () => {
  for (const theme of ['dark', 'light'] as const) {
    test(`notice, suggest, insert, then ⌘Z restores the words (${theme})`, async ({ app, screen, browser }) => {
      await setup(app);
      await browser.addInitScript(themeScript(theme));
      await openKeel(app);
      const session = await openLocalShell(app, screen);

      // ⇥ is the shell's until a line is noticed: a command is not noticed.
      await term(screen).pressSequentially('ls -la /tmp');
      await pause(800);
      await expect(screen.getByText('Looks like a request')).toBeHidden();
      await term(screen).press('Control+u');

      // State 1 · Notice: words on the prompt line, after a typing pause.
      await term(screen).pressSequentially('show the files here');
      await expect(screen.getByText('Looks like a request')).toBeVisible();
      await app.screenshot(`${theme}-suggest-notice`);

      // State 2 · Suggest: ⇥ opens the card; nothing is in the pane yet.
      await term(screen).press('Tab');
      await expect(card(screen)).toBeVisible();
      await expect(card(screen)).toContainText('echo keel-suggest-ok');
      await expect(card(screen)).toContainText('Read-only');
      await expect(card(screen)).toContainText('Prints a marker');
      await expect(card(screen)).toContainText('echo keel-suggest-alt-2');
      await expect(card(screen)).toContainText('echo keel-suggest-alt-3');
      await app.screenshot(`${theme}-suggest-card`);
      expect(await scrollback(app, session.id)).not.toContain('keel-suggest-ok');
      // 2 / 3 and ↑↓ pick an option; 1 goes back.
      await term(screen).press('2');
      await expect(card(screen)).toHaveAttribute('data-option', '1');
      await term(screen).press('ArrowUp');
      await expect(card(screen)).toHaveAttribute('data-option', '0');

      // State 3 · Inserted: replaces the line, highlighted, not run.
      await term(screen).press('Tab');
      await expect(card(screen)).toBeHidden();
      await expect(screen.getByText('Inserted, not run')).toBeVisible();
      await expect.poll(() => scrollback(app, session.id)).toMatch(/echo keel-suggest-ok$/);
      await app.screenshot(`${theme}-suggest-inserted`);
      expect(await scrollback(app, session.id)).not.toMatch(/^keel-suggest-ok\r?$/m); // never run

      // §6.7: the suggestion and the insert are logged with their source.
      const entries = (await logs(app)).filter((e) => e.sessionId === session.id);
      expect(entries.some((e) => e.kind === 'agent.suggest' && e.source === 'generated')).toBe(true);
      const ins = entries.filter((e) => e.kind === 'agent.insert');
      expect(ins).toHaveLength(1);
      expect(ins[0].source).toBe('generated');
      expect(ins[0].bytes).toBe('\u0015echo keel-suggest-ok'.length);
      expect(entries.some((e) => e.kind === 'agent.read' && e.source === 'jev')).toBe(true);
      expect(JSON.stringify(entries)).not.toContain('show the files');

      // ⌘Z: back to the words. Enter then runs the words, not the command.
      await term(screen).press('Meta+z');
      await expect(screen.getByText('Inserted, not run')).toBeHidden();
      await expect.poll(() => scrollback(app, session.id)).toMatch(/show the files here$/);
      await term(screen).press('Enter');
      await expect.poll(() => scrollback(app, session.id)).toMatch(/show: (command )?not found/);
      expect(await scrollback(app, session.id)).not.toMatch(/^keel-suggest-ok\r?$/m);
      await closeTabs(screen);
    });

    test(`a destructive suggestion needs ⇧⇥ (${theme})`, async ({ app, screen, browser }) => {
      await setup(app);
      await browser.addInitScript(themeScript(theme));
      await openKeel(app);
      const session = await openLocalShell(app, screen);

      await term(screen).pressSequentially('clean up old files');
      await expect(screen.getByText('Looks like a request')).toBeVisible();
      await term(screen).press('Tab');

      // State 4 · Destructive: red edge, what it does, ⇧⇥ only.
      await expect(card(screen)).toBeVisible();
      await expect(card(screen)).toHaveAttribute('data-risk', 'destructive');
      await expect(card(screen)).toContainText('rm -rf /tmp/keel-suggest-e2e');
      await expect(card(screen)).toContainText('Deletes files');
      await expect(card(screen)).toContainText('insert anyway');
      await expect(card(screen)).toContainText('Plain ⇥ never inserts a destructive command');
      await app.screenshot(`${theme}-suggest-destructive`);

      // Plain ⇥: refused in the card, nothing reaches the pane or the log.
      await term(screen).press('Tab');
      await expect(card(screen)).toContainText('Press ⇧⇥ to insert this command.');
      await pause(300);
      expect(await scrollback(app, session.id)).not.toContain('keel-suggest-e2e');
      expect((await logs(app)).filter((e) => e.kind === 'agent.insert' && e.sessionId === session.id)).toHaveLength(0);

      // ⇧⇥ inserts it, still not run.
      await term(screen).press('Shift+Tab');
      await expect(screen.getByText('Inserted, not run')).toBeVisible();
      await expect.poll(() => scrollback(app, session.id)).toMatch(/rm -rf \/tmp\/keel-suggest-e2e$/);
      expect((await logs(app)).filter((e) => e.kind === 'agent.insert' && e.sessionId === session.id)).toHaveLength(1);
      // Back to the words, and clear the line without running anything.
      await term(screen).press('Meta+z');
      await expect.poll(() => scrollback(app, session.id)).toMatch(/clean up old files$/);
      await term(screen).press('Control+u');
      await closeTabs(screen);
    });
  }

  test('Jev offline: only #-lines are noticed, with no error', async ({ app, screen, browser }) => {
    await setup(app);
    await openKeel(app);
    await openLocalShell(app, screen);
    // The fake Jev fails for lines holding "jev-offline".
    await term(screen).pressSequentially('jev-offline show the files');
    await pause(800);
    await expect(screen.getByText('Looks like a request')).toBeHidden();
    await term(screen).press('Control+u');
    await term(screen).pressSequentially('# jev-offline show the files');
    await expect(screen.getByText('Looks like a request')).toBeVisible();
    await term(screen).press('Escape');
    await term(screen).press('Control+u');

    // Suggestions off: nothing is noticed, not even a #-line.
    await keelSend(app.baseUrl, 'PUT', '/api/agent/settings', { suggestions: 'off' });
    try {
      await term(screen).pressSequentially('# show the files');
      await pause(800);
      await expect(screen.getByText('Looks like a request')).toBeHidden();
      await term(screen).press('Control+u');
    } finally {
      await keelSend(app.baseUrl, 'PUT', '/api/agent/settings', { suggestions: 'on' });
    }
    await closeTabs(screen);
  });
});

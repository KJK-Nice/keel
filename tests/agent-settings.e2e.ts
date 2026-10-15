import { describe, test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelApi, keelJson, keelSend, openKeel } from './keel.ts';

// Phase 6d (docs/ui-redesign.md §5.8, §5.9, §5.13, §5.2): Settings · Agent,
// Edit host · Agent, the Logs Agent filter and the palette's Ask the agent
// row. keel serve runs with KEEL_AGENT_PROVIDER=fake (e2e.config.ts), so
// nothing leaves the machine; the keys below are fakes that only reach the
// scratch vault. Serial: the settings and keys are shared state.

type AgentSettings = {
  settings: {
    provider: string;
    model: string;
    detection: string;
    suggestions: string;
    defaultScope: string;
    scrollbackLines: number;
    redact: { secrets: boolean; ips: boolean; hostnames: boolean };
    historyDays: number;
  };
  keys: Record<string, { configured: boolean }>;
};
type Host = { id: string; name: string; authType: string; agentReadable?: boolean; production?: boolean; groupId?: string };
type Group = { id: string; name: string };
type Session = { id: string };
type LogEntry = { kind: string; message: string; lines?: number; source?: string };

const FAKE_GEMINI = 'e2e-not-a-real-gemini-key-0001';
const FAKE_GEMINI_2 = 'e2e-not-a-real-gemini-key-0002';
const FAKE_JEV = 'e2e-not-a-real-jev-key-0001';
const themeScript = (theme: string) =>
  `try { if (location.protocol.startsWith('http')) localStorage.setItem('keel-theme', '${theme}'); } catch {}`;

const settingsApi = (app: any) => keelApi<AgentSettings>(app.baseUrl, '/api/agent/settings');
const logsApi = (app: any) => keelApi<LogEntry[]>(app.baseUrl, '/api/logs');

async function resetAgent(app: any) {
  await keelSend(app.baseUrl, 'DELETE', '/api/agent/keys/gemini');
  await keelSend(app.baseUrl, 'DELETE', '/api/agent/keys/jev');
  await keelSend(app.baseUrl, 'PUT', '/api/agent/settings', {
    provider: 'gemini',
    detection: 'auto',
    suggestions: 'on',
    defaultScope: 'focused',
    scrollbackLines: 200,
    redact: { secrets: true, ips: false, hostnames: false },
    historyDays: 30,
  });
}

async function openAgentSettings(screen: any) {
  const vault = screen.getByRole('complementary', 'Vault');
  await vault.getByRole('button', 'Settings').tap();
  const page = screen.getByRole('region', 'Settings');
  await page.getByRole('tab', 'Agent').tap();
  await expect(page.getByRole('heading', 'Agent')).toBeVisible();
  return page;
}

describe('agent settings', { tags: ['agent', 'settings'], serial: true }, () => {
  for (const theme of ['dark', 'light'] as const) {
    test(`Settings · Agent: connect, change, disconnect, switches persist (${theme})`, async ({ app, screen, browser }) => {
      await resetAgent(app);
      await browser.addInitScript(themeScript(theme));
      await openKeel(app);
      const page = await openAgentSettings(screen);
      const gemini = page.getByRole('group', 'Google Gemini');
      const jev = page.getByRole('group', 'TypeSafe Jev');

      // Not connected: the free-tier warning and the links, then a key field.
      await expect(gemini).toContainText('Not connected');
      await expect(gemini).toContainText('On the free tier of the Gemini API');
      await expect(gemini.getByRole('link', 'Get a key')).toHaveAttribute('href', 'https://aistudio.google.com/apikey');
      await expect(gemini.getByRole('link', 'Terms')).toHaveAttribute('href', 'https://ai.google.dev/gemini-api/terms');
      await expect(gemini.getByLabel('Gemini API key')).toBeVisible();
      await expect(jev).toContainText('Plain-language detection');
      // Auto detection needs Jev: until it is connected, the note says what happens.
      await expect(page.getByRole('radio', /^Notice automatically/)).toHaveAttribute('aria-checked', 'true');
      await expect(page).toContainText('Jev is not connected, so Keel notices only lines that start with #');
      await app.screenshot(`${theme}-settings-agent-disconnected`);

      // Connect with a fake key: stored in the vault, field emptied, never returned.
      await gemini.getByLabel('Gemini API key').fill(FAKE_GEMINI);
      await gemini.getByRole('button', 'Connect').tap();
      await expect(gemini).toContainText('Connected');
      await expect(gemini.getByLabel('Gemini API key')).toHaveCount(0);
      await expect(gemini.getByRole('button', 'Change key')).toBeVisible();
      await expect(gemini.getByRole('button', 'Disconnect')).toBeVisible();
      let s = await settingsApi(app);
      expect(s.keys.gemini.configured).toBe(true);
      expect(JSON.stringify(s)).not.toContain(FAKE_GEMINI);
      expect(keelJson()).not.toContain(FAKE_GEMINI);

      // Change key: the field comes back empty; Cancel leaves the key alone.
      await gemini.getByRole('button', 'Change key').tap();
      const field = gemini.getByLabel('Gemini API key');
      await expect(field).toBeFocused();
      await gemini.getByRole('button', 'Cancel').tap();
      await expect(field).toHaveCount(0);
      await gemini.getByRole('button', 'Change key').tap();
      await field.fill(FAKE_GEMINI_2);
      await gemini.getByRole('button', 'Save key').tap();
      await expect(field).toHaveCount(0);
      await expect(gemini).toContainText('Connected');
      expect(keelJson()).not.toContain(FAKE_GEMINI_2);

      // Jev: connect, and the fallback note goes away.
      await jev.getByLabel('Jev API key').fill(FAKE_JEV);
      await jev.getByRole('button', 'Connect').tap();
      await expect(jev).toContainText('Connected');
      await expect(page).not.toContainText('Jev is not connected');
      expect((await settingsApi(app)).keys.jev.configured).toBe(true);
      await app.screenshot(`${theme}-settings-agent-connected`);

      // Disconnect asks first. Cancel keeps the key; Disconnect removes it.
      await jev.getByRole('button', 'Disconnect').tap();
      const dialog = screen.getByRole('alertdialog');
      await expect(dialog).toContainText('Disconnect TypeSafe Jev?');
      await expect(dialog).toContainText('only notice lines that start with #');
      await app.screenshot(`${theme}-settings-agent-disconnect`);
      await dialog.getByRole('button', 'Cancel').tap();
      await expect(dialog).toBeHidden();
      expect((await settingsApi(app)).keys.jev.configured).toBe(true);
      await jev.getByRole('button', 'Disconnect').tap();
      await dialog.getByRole('button', 'Disconnect').tap();
      await expect(jev).toContainText('Not connected');
      expect((await settingsApi(app)).keys.jev.configured).toBe(false);
      await expect(page).toContainText('Jev is not connected');

      // Suggestions, detection, default panes, redaction: each saves as it changes.
      const suggest = page.getByRole('switch', 'Suggest commands');
      await expect(suggest).toHaveAttribute('aria-checked', 'true');
      await suggest.tap();
      await expect(suggest).toHaveAttribute('aria-checked', 'false');
      await expect.poll(async () => (await settingsApi(app)).settings.suggestions).toBe('off');
      await suggest.tap();
      await expect.poll(async () => (await settingsApi(app)).settings.suggestions).toBe('on');
      await page.getByRole('radio', 'Only lines starting with #').tap();
      await expect.poll(async () => (await settingsApi(app)).settings.detection).toBe('hash-only');
      await expect(page.getByRole('radio', /^Notice automatically/)).toHaveAttribute('aria-checked', 'false');
      await page.getByRole('radio', 'Every pane in the tab').tap();
      await expect.poll(async () => (await settingsApi(app)).settings.defaultScope).toBe('tab');
      await page.getByRole('switch', 'Redact IP addresses').tap();
      await expect.poll(async () => (await settingsApi(app)).settings.redact).toEqual({ secrets: true, ips: true, hostnames: false });
      await page.getByRole('switch', 'Redact hostnames').tap();
      await expect.poll(async () => (await settingsApi(app)).settings.redact).toEqual({ secrets: true, ips: true, hostnames: true });

      // Safety: the always-on rows are text, not controls.
      await expect(page).toContainText('The agent never types into a pane');
      await expect(page).toContainText('Secrets are always redacted');
      await expect(page).toContainText('Risky commands need ⇧⇥');
      await expect(page.getByRole('switch', /secrets/i)).toHaveCount(0);
      await app.screenshot(`${theme}-settings-agent-safety`);

      // Persisted: a fresh load of the page shows the same values.
      await openKeel(app);
      const again = await openAgentSettings(screen);
      await expect(again.getByRole('switch', 'Suggest commands')).toHaveAttribute('aria-checked', 'true');
      await expect(again.getByRole('radio', 'Only lines starting with #')).toHaveAttribute('aria-checked', 'true');
      await expect(again.getByRole('radio', 'Every pane in the tab')).toHaveAttribute('aria-checked', 'true');
      await expect(again.getByRole('switch', 'Redact IP addresses')).toHaveAttribute('aria-checked', 'true');
      await expect(again.getByRole('switch', 'Redact hostnames')).toHaveAttribute('aria-checked', 'true');
      await expect(again.getByRole('group', 'Google Gemini')).toContainText('Connected');
      await expect(again.getByRole('group', 'TypeSafe Jev')).toContainText('Not connected');

      // Delete all history: asks first, then logs one entry (no content).
      await again.getByRole('button', 'Delete all history').tap();
      const confirm = screen.getByRole('alertdialog');
      await expect(confirm).toContainText('Delete all history?');
      await confirm.getByRole('button', 'Cancel').tap();
      const deletions = async () => (await logsApi(app)).filter((e) => e.message.startsWith('agent history deleted')).length;
      const had = await deletions();
      await again.getByRole('button', 'Delete all history').tap();
      await confirm.getByRole('button', 'Delete all history').tap();
      await expect.poll(deletions).toBe(had + 1);

      // The keys never reached the log either.
      const log = JSON.stringify(await logsApi(app));
      for (const k of [FAKE_GEMINI, FAKE_GEMINI_2, FAKE_JEV]) expect(log).not.toContain(k);
      await resetAgent(app);
    });
  }

  test('Edit host · Agent: the switches are saved, and a production group turns production on', async ({ app, screen }) => {
    const stamp = Date.now().toString(36);
    const group = await keelSend<Group>(app.baseUrl, 'POST', '/api/groups', { name: 'Production', parentId: '' });
    const names = { prod: `e2e-prod-${stamp}`, plain: `e2e-plain-${stamp}`, off: `e2e-off-${stamp}` };
    try {
      await openKeel(app);
      const vault = screen.getByRole('complementary', 'Vault');
      const dialog = screen.getByRole('dialog').filter({ hasText: 'New host' });
      const fill = async (name: string) => {
        await vault.getByRole('button', 'New host').tap();
        await expect(dialog).toBeVisible();
        await dialog.getByLabel('Label').fill(name);
        await dialog.getByLabel('Hostname / IP').fill('192.0.2.20');
        await dialog.getByLabel('Username').fill('ubuntu');
        await dialog.getByLabel('Password').fill('scratch-pass-not-real');
      };
      const prod = dialog.getByRole('switch', 'Treat as production');
      const readable = dialog.getByRole('switch', "Agent can read this host's panes");

      // A host with no group: readable on, production off.
      await fill(names.plain);
      await expect(readable).toHaveAttribute('aria-checked', 'true');
      await expect(prod).toHaveAttribute('aria-checked', 'false');
      await app.screenshot('host-agent-defaults');
      await dialog.getByRole('button', 'Save host').tap();
      await expect(dialog).toBeHidden();

      // Choosing the Production group turns production on; the dialog is
      // saved untouched otherwise.
      await fill(names.prod);
      await dialog.getByRole('combobox', 'Parent Group').tap();
      await screen.getByRole('option', 'Production').tap();
      await expect(prod).toHaveAttribute('aria-checked', 'true');
      await app.screenshot('host-agent-production-prefill');
      await dialog.getByRole('button', 'Save host').tap();
      await expect(dialog).toBeHidden();

      // The user can turn it off, and the agent off for the host.
      await fill(names.off);
      await dialog.getByRole('combobox', 'Parent Group').tap();
      await screen.getByRole('option', 'Production').tap();
      await expect(prod).toHaveAttribute('aria-checked', 'true');
      await prod.tap();
      await expect(prod).toHaveAttribute('aria-checked', 'false');
      await readable.tap();
      await expect(readable).toHaveAttribute('aria-checked', 'false');
      await dialog.getByRole('button', 'Save host').tap();
      await expect(dialog).toBeHidden();

      const hosts = await keelApi<Host[]>(app.baseUrl, '/api/hosts');
      const by = (n: string) => hosts.find((h) => h.name === n)!;
      expect(by(names.plain)).toMatchObject({ agentReadable: true, production: false });
      expect(by(names.prod)).toMatchObject({ agentReadable: true, production: true, groupId: group.id });
      expect(by(names.off)).toMatchObject({ agentReadable: false, production: false, groupId: group.id });
    } finally {
      const hosts = await keelApi<Host[]>(app.baseUrl, '/api/hosts');
      for (const h of hosts.filter((h) => Object.values(names).includes(h.name))) await keelSend(app.baseUrl, 'DELETE', `/api/hosts/${h.id}`);
      await keelSend(app.baseUrl, 'DELETE', `/api/groups/${group.id}`);
    }
  });

  test('Logs: the Agent filter shows agent events with their counts and source', async ({ app, screen }) => {
    await resetAgent(app);
    await keelSend(app.baseUrl, 'PUT', '/api/agent/keys/gemini', { key: FAKE_GEMINI });
    const hosts = await keelApi<Host[]>(app.baseUrl, '/api/hosts');
    const local = hosts.find((h) => h.authType === 'local')!;
    const session = await keelSend<Session>(app.baseUrl, 'POST', '/api/sessions', { hostId: local.id, cols: 80, rows: 24 });
    try {
      // One agent.read with a line count (setup through the API: the log is
      // the thing under test, not asking).
      await expect
        .poll(async () => {
          try {
            await keelSend(app.baseUrl, 'POST', '/api/agent/ask', { conversationId: `conv-e2e-${Date.now()}`, question: 'hello', panes: [session.id] });
            return true;
          } catch {
            return false;
          }
        })
        .toBe(true);
      await expect.poll(async () => (await logsApi(app)).some((e) => e.kind === 'agent.read')).toBe(true);

      await openKeel(app);
      const vault = screen.getByRole('complementary', 'Vault');
      await vault.getByRole('button', 'Logs').tap();
      const page = screen.getByRole('region', 'Logs');
      const chips = page.getByRole('group', 'Show events');
      await expect(chips.getByRole('button', 'Agent')).toBeVisible();
      await expect(page.getByRole('row').filter({ hasText: 'session' }).first()).toBeVisible();
      await chips.getByRole('button', 'Agent').tap();
      await expect(chips.getByRole('button', 'Agent')).toHaveAttribute('aria-pressed', 'true');

      const row = page.getByRole('row').filter({ hasText: 'agent read' }).first();
      await expect(row).toBeVisible();
      await expect(row.getByText('agent.read', { exact: true })).toBeVisible();
      await expect(row).toContainText(/\d+ lines?/);
      // Rows of other kinds are hidden.
      await expect(page.getByRole('row').filter({ hasText: 'host added' })).toBeHidden();
      await expect(page.getByRole('row', { visible: true }).filter({ hasText: /^\S+\s+(session|host|tunnel)\b/ })).toHaveCount(0);
      await app.screenshot('logs-agent-filter');
    } finally {
      await keelSend(app.baseUrl, 'DELETE', `/api/sessions/${session.id}`);
      await resetAgent(app);
    }
  });

  test('Palette: Ask the agent row and ⌘↵ open the panel with the query, unsent', async ({ app, screen }) => {
    await resetAgent(app);
    await keelSend(app.baseUrl, 'PUT', '/api/agent/keys/gemini', { key: FAKE_GEMINI });
    await openKeel(app);
    const vault = screen.getByRole('complementary', 'Vault');
    await vault.getByRole('button', 'Hosts').tap();
    await vault.getByRole('button', /^Local Shell\b/).first().tap();
    const term = screen.getByRole('textbox', 'Terminal input', { visible: true });
    await expect(term).toBeFocused();
    const before = (await logsApi(app)).filter((e) => e.kind === 'agent.read').length;

    await term.press('Meta+k');
    const palette = screen.getByRole('dialog', 'Command palette');
    const combo = screen.getByRole('combobox', 'Run a command or connect a host…');
    await expect(palette).toBeVisible();
    await combo.fill('prod-db');
    const ask = palette.getByRole('option').filter({ hasText: 'Ask the agent about' });
    await expect(ask).toContainText('prod-db');
    await expect(ask).toContainText(/⌘\s*↵/);
    await expect(palette.getByRole('option').last()).toContainText('Ask the agent');
    await app.screenshot('palette-ask-agent');

    // ⌘↵ from the input: the panel opens with the text in the composer. It
    // is not sent: no read, no message in the thread.
    await combo.press('Meta+Enter');
    await expect(palette).toBeHidden();
    const panel = screen.getByRole('complementary', 'Agent');
    await expect(panel).toBeVisible();
    const composer = panel.getByRole('textbox', 'Message the agent');
    await expect(composer).toHaveValue('prod-db');
    await expect(composer).toBeFocused();
    await expect(panel.getByText('Ask about your sessions')).toBeVisible();
    expect((await logsApi(app)).filter((e) => e.kind === 'agent.read').length).toBe(before);

    // ↵ in the panel is what sends it.
    await composer.press('Enter');
    await expect.poll(async () => (await logsApi(app)).filter((e) => e.kind === 'agent.read').length).toBeGreaterThan(before);

    // The row itself, with nothing typed: opens the panel with an empty composer.
    await composer.fill('');
    await screen.getByRole('button', 'Close agent').tap();
    await expect(panel).toBeHidden();
    await term.press('Meta+k');
    await expect(palette).toBeVisible();
    await palette.getByRole('option').filter({ hasText: 'Ask the agent' }).tap();
    await expect(panel).toBeVisible();
    await expect(composer).toHaveValue('');

    await screen.getByRole('button', 'Close agent').tap();
    for (let i = 0; i < 4; i++) {
      const close = screen.getByRole('button', 'Close tab', { visible: true });
      if (!(await close.count())) break;
      await close.first().tap();
    }
    await resetAgent(app);
  });
});

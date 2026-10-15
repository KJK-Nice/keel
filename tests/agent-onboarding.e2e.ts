import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { startScratchKeel } from './keel.ts';

// First run, step 3: Agent (docs/ui-redesign.md §5.15), against scratch keel
// serves (tests/keel.ts startScratchKeel: KEEL_KEYRING=off, the fixture
// ~/.ssh, a scratch HOME). Nothing is sent anywhere during onboarding: the
// scratch serves have no provider at all, and the checks below read what keel
// serve stored. The key is a fake.

type AgentSettings = { settings: { suggestions: string }; keys: Record<string, { configured: boolean }> };
type LogEntry = { kind: string; message: string };

const FAKE_KEY = 'e2e-not-a-real-gemini-key-0003';

const both = async (app: any, browser: any, name: string) => {
  for (const theme of ['dark', 'light']) {
    await browser.evaluate(`window.__keelApplyTheme(${JSON.stringify(theme)})`);
    await app.screenshot(`${theme}-${name}`);
  }
  await browser.evaluate('window.__keelApplyTheme("dark")');
};

// Steps 1 and 2: create a passphrase vault, then skip the import.
async function toAgentStep(app: any, screen: any, keel: any) {
  await app.open(keel.openUrl);
  const first = screen.getByRole('region', 'Welcome to Keel');
  await first.getByLabel('Passphrase', { exact: true }).fill('e2e-vault-pass');
  await first.getByLabel('Repeat passphrase').fill('e2e-vault-pass');
  await first.getByRole('button', 'Create vault').tap();
  const hosts = screen.getByRole('region', 'Bring your hosts');
  await expect(hosts).toBeVisible();
  await hosts.getByRole('button', 'Skip, add hosts later').tap();
  const step = screen.getByRole('region', 'An agent that suggests, never types');
  await expect(step).toBeVisible();
  return step;
}

test('first run step 3: the promises, then Set up later saves nothing', { tags: ['vault', 'first-run', 'agent'], timeout: 180_000 }, async ({ app, screen, browser }) => {
  const keel = await startScratchKeel('agent-later');
  try {
    const step = await toAgentStep(app, screen, keel);
    await expect(screen.getByRole('list', 'Setup progress')).toContainText('Agent');
    await expect(screen.getByRole('list', 'Setup progress').getByRole('listitem').filter({ hasText: 'Agent' })).toHaveAttribute('aria-current', 'step');
    const promises = step.getByRole('list', 'What the agent promises');
    await expect(promises).toContainText('Reads only the panes you pick');
    await expect(promises).toContainText('Suggests commands; you press Type');
    await expect(promises).toContainText('never types into a pane');
    await expect(promises).toContainText('Secrets are redacted, the vault stays closed');
    await expect(step.getByRole('radio', /^Not now/)).toHaveAttribute('aria-checked', 'true');
    await expect(step.getByRole('switch', 'Suggest commands when I type plain language')).toHaveAttribute('aria-checked', 'true');
    // Optional: no key field until Gemini is picked.
    await expect(step.getByLabel('Gemini API key')).toBeHidden();
    await both(app, browser, 'first-run-agent');

    await step.getByRole('button', 'Set up later').tap();
    await expect(step).toBeHidden();
    await expect(screen.getByRole('complementary', 'Vault')).toBeVisible();

    // Nothing was saved: no key, the default switch, no agent log entry.
    const settings = await keel.api<AgentSettings>('/api/agent/settings');
    expect(settings.keys.gemini.configured).toBe(false);
    expect(settings.keys.jev.configured).toBe(false);
    expect(settings.settings.suggestions).toBe('on');
    const logs = await keel.api<LogEntry[]>('/api/logs');
    expect(logs.filter((e) => e.kind.startsWith('agent'))).toEqual([]);
    expect(await keel.api<{ firstRun: boolean }>('/api/vault')).toMatchObject({ firstRun: false });
  } finally {
    await keel.stop();
  }
});

test('first run step 3: Gemini with a key and the suggestions switch off, then Open Keel', { tags: ['vault', 'first-run', 'agent'], timeout: 180_000 }, async ({ app, screen, browser }) => {
  const keel = await startScratchKeel('agent-open');
  try {
    const step = await toAgentStep(app, screen, keel);
    await step.getByRole('radio', /^Google Gemini/).tap();
    await expect(step.getByRole('radio', /^Google Gemini/)).toHaveAttribute('aria-checked', 'true');
    await expect(step).toContainText('On the free tier of the Gemini API');
    await expect(step.getByRole('link', 'Get a key')).toHaveAttribute('href', 'https://aistudio.google.com/apikey');
    const key = step.getByLabel('Gemini API key');
    await expect(key).toBeFocused();

    // A chosen provider needs its key, or Not now.
    await step.getByRole('button', 'Open Keel').tap();
    await expect(step.getByRole('alert').filter({ hasText: 'Paste your key' })).toBeVisible();
    await expect(step).toBeVisible();
    await both(app, browser, 'first-run-agent-gemini');

    await key.fill(FAKE_KEY);
    await step.getByRole('switch', 'Suggest commands when I type plain language').tap();
    await expect(step.getByRole('switch', 'Suggest commands when I type plain language')).toHaveAttribute('aria-checked', 'false');
    await step.getByRole('button', 'Open Keel').tap();
    await expect(step).toBeHidden();
    await expect(screen.getByRole('complementary', 'Vault')).toBeVisible();

    const settings = await keel.api<AgentSettings>('/api/agent/settings');
    expect(settings.keys.gemini.configured).toBe(true);
    expect(settings.settings.suggestions).toBe('off');
    // The key is in the vault, not in the API response, keel.json or the log.
    expect(JSON.stringify(settings)).not.toContain(FAKE_KEY);
    expect(readFileSync(join(keel.dataDir, 'keel.json'), 'utf8')).not.toContain(FAKE_KEY);
    expect(JSON.stringify(await keel.api('/api/logs'))).not.toContain(FAKE_KEY);
    expect(await keel.api<{ firstRun: boolean }>('/api/vault')).toMatchObject({ firstRun: false });
  } finally {
    await keel.stop();
  }
});

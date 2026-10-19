import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { sshDir, startScratchKeel } from './keel.ts';

// First run and Unlock (docs/ui-redesign.md §5.14, §5.15) against a scratch
// keel serve (tests/keel.ts startScratchKeel): no KEEL_VAULT_PASSPHRASE and
// KEEL_KEYRING=off, so the keychain "does not answer" and the vault needs a
// passphrase, and KEEL_SSH_DIR is the fixture in .e2e/ssh-home/.ssh
// (tests/fixtures/ssh-home plus two fresh keys from scripts/e2e-serve.sh).
// Nothing here reads the real ~/.ssh or the OS keychain.

type Host = { name: string; address: string; port: number; username: string; authType: string; identityId?: string };
type Identity = { id: string; name: string; kind: string; privateKey?: string; passphrase?: string };

// Every file under dir: bytes, mode and mtime, to prove the import left the
// originals alone.
function snapshot(dir: string, out: Record<string, string> = {}, rel = ''): Record<string, string> {
  for (const name of readdirSync(join(dir, rel)).sort()) {
    const p = join(dir, rel, name);
    const st = statSync(p);
    if (st.isDirectory()) snapshot(dir, out, join(rel, name));
    else out[join(rel, name)] = `${st.mode.toString(8)} ${st.mtimeMs} ${readFileSync(p).toString('base64')}`;
  }
  return out;
}

const both = async (app: any, browser: any, name: string) => {
  for (const theme of ['dark', 'light']) {
    await browser.evaluate(`window.__keelApplyTheme(${JSON.stringify(theme)})`);
    await app.screenshot(`${theme}-${name}`);
  }
  await browser.evaluate('window.__keelApplyTheme("dark")');
};

test('first run: passphrase vault, host import with one key copied, then Unlock after a restart', { tags: ['vault', 'first-run'], timeout: 180_000 }, async ({ app, screen, browser }) => {
  const before = snapshot(sshDir);
  const plainKey = readFileSync(join(sshDir, 'id_e2e_plain'), 'utf8');
  const lockedKey = readFileSync(join(sshDir, 'id_e2e_locked'), 'utf8');
  const keel = await startScratchKeel('flow');
  try {
    await app.open(keel.openUrl);

    // Step 1: the keychain is off, so the passphrase card is the choice.
    const first = screen.getByRole('region', 'Welcome to Keel');
    await expect(first).toBeVisible();
    await expect(screen.getByRole('list', 'Setup progress')).toContainText('Vault');
    const keychainCard = first.getByRole('radio', /^Unlock with/);
    await expect(keychainCard).toHaveAttribute('aria-disabled', 'true');
    await expect(keychainCard).toHaveAttribute('aria-checked', 'false');
    await expect(keychainCard).toContainText('KEEL_KEYRING=off');
    await expect(first.getByRole('radio', /^Use a passphrase/)).toHaveAttribute('aria-checked', 'true');
    await expect(first).toContainText('keel.vault');
    await expect(first).toContainText('Host passwords, private keys and key passphrases');
    await both(app, browser, 'first-run-vault');
    await first.getByRole('button', 'I already have a keel.vault…').tap();
    await expect(first).toContainText('keel serve stop');
    await first.getByRole('button', 'I already have a keel.vault…').tap();

    await first.getByLabel('Passphrase', { exact: true }).fill('e2e-vault-pass');
    await first.getByLabel('Repeat passphrase').fill('something-else');
    await first.getByRole('button', 'Create vault').tap();
    await expect(first.getByRole('alert')).toHaveText("The passphrases don't match.");
    await first.getByLabel('Repeat passphrase').fill('e2e-vault-pass');
    await first.getByRole('button', 'Create vault').tap();

    // Step 2: the fixture config.
    const hosts = screen.getByRole('region', 'Bring your hosts');
    await expect(hosts).toBeVisible();
    await expect(hosts).toContainText('Keel found ~/.ssh/config with 5 hosts');
    await expect(hosts.getByRole('checkbox', 'Add e2e-web')).toHaveAttribute('aria-checked', 'true');
    await expect(hosts.getByRole('checkbox', 'Add e2e-api')).toHaveAttribute('aria-checked', 'true');
    await expect(hosts.getByRole('checkbox', 'Add e2e-db')).toHaveAttribute('aria-checked', 'true');
    // Proxy hosts: flagged, off by default, still importable.
    await expect(hosts.getByRole('checkbox', 'Add e2e-legacy')).toHaveAttribute('aria-checked', 'false');
    await expect(hosts.getByRole('row').filter({ hasText: 'e2e-legacy' })).toContainText('ProxyCommand, not used');
    await expect(hosts.getByRole('checkbox', 'Add e2e-jump')).toHaveAttribute('aria-checked', 'false');
    await expect(hosts.getByRole('row').filter({ hasText: 'e2e-jump' })).toContainText('ProxyJump e2e-bastion, not used');
    // Wildcards are patterns, listed apart, never rows.
    const patterns = hosts.getByRole('group', 'Patterns');
    await expect(patterns).toContainText('*.e2e.internal');
    await expect(patterns).toContainText('web-?');
    await expect(hosts.getByRole('checkbox', /\*/)).toHaveCount(0);
    await expect(hosts).toContainText('3 of 5 selected');
    await hosts.getByText('1 note about this config').tap();
    await expect(hosts).toContainText('Match block skipped');
    // Keys: one switch each, off.
    const keys = hosts.getByRole('group', 'Private keys');
    const plainSwitch = keys.getByRole('switch', 'Copy ~/.ssh/id_e2e_plain into the vault');
    const lockedSwitch = keys.getByRole('switch', 'Copy ~/.ssh/id_e2e_locked into the vault');
    await expect(plainSwitch).toHaveAttribute('aria-checked', 'false');
    await expect(lockedSwitch).toHaveAttribute('aria-checked', 'false');
    await both(app, browser, 'first-run-hosts');

    // Copy the key with a passphrase; leave the other off. Add e2e-legacy
    // without its proxy.
    await hosts.getByRole('checkbox', 'Add e2e-legacy').tap();
    await expect(hosts).toContainText('4 of 5 selected');
    await lockedSwitch.tap();
    await expect(lockedSwitch).toHaveAttribute('aria-checked', 'true');
    const pass = keys.getByLabel('Passphrase for id_e2e_locked');
    await expect(pass).toBeFocused();
    await hosts.getByRole('button', 'Add 4 hosts').tap();
    await expect(keys.getByRole('alert')).toContainText('Enter the passphrase for id_e2e_locked');
    await pass.fill('not-it');
    await hosts.getByRole('button', 'Add 4 hosts').tap();
    await expect(keys.getByRole('alert')).toHaveText("That passphrase doesn't open id_e2e_locked.");
    await app.screenshot('dark-first-run-key-error');
    expect((await keel.api<Host[]>('/api/hosts')).length).toBe(1);
    await pass.fill('e2e-key-pass');
    await hosts.getByRole('button', 'Add 4 hosts').tap();

    // Step 3 follows the import (agent-onboarding.e2e.ts covers it); Set up
    // later ends the first run without saving anything.
    await expect(hosts).toBeHidden();
    const agentStep = screen.getByRole('region', 'An agent that suggests, never types');
    await expect(agentStep).toBeVisible();
    await agentStep.getByRole('button', 'Set up later').tap();
    await expect(agentStep).toBeHidden();
    const vault = screen.getByRole('complementary', 'Vault');
    await expect(vault.getByRole('button', /^e2e-db\b/)).toBeVisible();
    await app.screenshot('dark-first-run-done');

    // What keel serve saved.
    const saved = await keel.api<Host[]>('/api/hosts');
    const byName = Object.fromEntries(saved.map((h) => [h.name, h]));
    expect(Object.keys(byName).sort()).toEqual(['Local Shell', 'e2e-api', 'e2e-db', 'e2e-legacy', 'e2e-web']);
    expect(byName['e2e-web']).toMatchObject({ address: 'e2e-web.example.test', username: 'deploy', port: 22, authType: 'password' });
    expect(byName['e2e-db']).toMatchObject({ address: '10.20.0.5', username: 'postgres', port: 2222, authType: 'identity' });
    expect(byName['e2e-legacy']).toMatchObject({ address: 'legacy.example.test', authType: 'password' });
    const ids = await keel.api<Identity[]>('/api/identities');
    expect(ids.map((i) => [i.name, i.kind, i.privateKey, i.passphrase])).toEqual([['id_e2e_locked', 'key', '__saved__', '__saved__']]);
    expect(byName['e2e-db'].identityId).toBe(ids[0].id);
    const vaultState = await keel.api<{ firstRun: boolean; mode: string }>('/api/vault');
    expect(vaultState).toMatchObject({ firstRun: false, mode: 'passphrase' });

    // keel.json holds no key material and no passphrase; keel.vault is
    // encrypted.
    const json = readFileSync(join(keel.dataDir, 'keel.json'), 'utf8');
    for (const needle of ['PRIVATE KEY', 'e2e-key-pass', 'e2e-vault-pass', plainKey.split('\n')[1], lockedKey.split('\n')[1]]) {
      expect(json.includes(needle)).toBe(false);
    }
    expect(readFileSync(join(keel.dataDir, 'keel.vault'), 'utf8').includes('PRIVATE KEY')).toBe(false);

    // The palette's Import shows the imported hosts as saved, unchecked.
    await vault.getByRole('button', 'New host').press('Meta+k');
    const palette = screen.getByRole('dialog', 'Command palette');
    await expect(palette).toBeVisible();
    await screen.getByRole('combobox', 'Run a command or connect a host…').fill('Import hosts');
    await screen.getByRole('combobox', 'Run a command or connect a host…').press('Enter');
    const imp = screen.getByRole('region', 'Import hosts');
    await expect(imp).toBeVisible();
    await expect(imp.getByRole('list', 'Setup progress')).toHaveCount(0);
    for (const alias of ['e2e-web', 'e2e-api', 'e2e-db', 'e2e-legacy']) {
      await expect(imp.getByRole('checkbox', `Add ${alias}`)).toHaveAttribute('aria-checked', 'false');
      await expect(imp.getByRole('row').filter({ hasText: alias })).toContainText('Already saved');
    }
    await expect(imp.getByRole('checkbox', 'Add e2e-jump')).toHaveAttribute('aria-checked', 'false');
    await expect(imp.getByRole('button', 'Add hosts')).toBeDisabled();
    await both(app, browser, 'import-saved');
    await imp.getByRole('button', 'Cancel').tap();
    await expect(imp).toBeHidden();

    // The originals in ~/.ssh: same files, bytes, modes and mtimes.
    expect(snapshot(sshDir)).toEqual(before);

    // Restart keel serve: the passphrase vault is locked and the Unlock
    // screen asks for it.
    await keel.restart();
    await app.open(`${keel.baseUrl}/?name=e2e`);
    const unlock = screen.getByRole('region', 'Unlock your vault');
    await expect(unlock).toBeVisible();
    await expect(unlock).toContainText('keel serve is running.');
    await expect(unlock).toContainText('KEEL_KEYRING=off');
    await expect(unlock).toContainText("Keel can't recover the vault without the passphrase");
    await expect(unlock).toContainText('Sessions already running in keel serve keep working');
    const field = unlock.getByLabel('Vault passphrase');
    await expect(field).toBeFocused();
    await both(app, browser, 'unlock');
    await field.fill('wrong-pass');
    await unlock.getByRole('button', 'Unlock').tap();
    await expect(unlock.getByRole('alert')).toHaveText("That passphrase doesn't open the vault.");
    await unlock.getByRole('button', 'Show passphrase').tap();
    await expect(field).toHaveAttribute('type', 'text');
    await app.screenshot('dark-unlock-error');
    await field.fill('e2e-vault-pass');
    await field.press('Enter');
    await expect(unlock).toBeHidden();
    await expect(screen.getByRole('complementary', 'Vault').getByRole('button', /^e2e-db\b/)).toBeVisible();
    // Not a first run any more.
    await expect(screen.getByRole('region', 'Welcome to Keel')).toBeHidden();
  } finally {
    await keel.stop();
  }
});

// The Via cell of a ProxyJump host whose jump is not kept holds a flag, a
// checkbox and "Without the jump". k-table cells do not wrap, so that cell
// once ran the row past the table (812px in 754px at the default window):
// the table scrolled sideways and the label was cut at the card's edge.
const VIA_LAYOUT = `(() => {
  const box = (e) => { const b = e.getBoundingClientRect(); return { left: b.left, right: b.right, top: b.top, bottom: b.bottom }; };
  const row = document.querySelector('#onb-import [data-imp-row="e2e-jump"]');
  const table = row.closest('.k-table');
  const check = row.querySelector('[data-imp-nojump]');
  return {
    card: box(row.closest('.k-imp-table')),
    scrollWidth: table.scrollWidth,
    clientWidth: table.clientWidth,
    parts: [...row.querySelectorAll('td > *, td .k-imp-via > *, td .k-imp-nojump > *')].map((e) => ({ what: e.className || e.tagName, ...box(e) })),
    check: box(check),
    label: box(check.nextElementSibling),
  };
})()`;

type Box = { left: number; right: number; top: number; bottom: number };
type ViaLayout = { card: Box; scrollWidth: number; clientWidth: number; parts: (Box & { what: string })[]; check: Box; label: Box };

test('first run: the Without the jump box stays inside the host table', { tags: ['vault', 'first-run', 'visual'], timeout: 120_000 }, async ({ app, screen, browser }) => {
  const keel = await startScratchKeel('via-layout');
  try {
    await app.open(keel.openUrl);
    const first = screen.getByRole('region', 'Welcome to Keel');
    await first.getByLabel('Passphrase', { exact: true }).fill('e2e-vault-pass');
    await first.getByLabel('Repeat passphrase').fill('e2e-vault-pass');
    await first.getByRole('button', 'Create vault').tap();
    const hosts = screen.getByRole('region', 'Bring your hosts');
    const noJump = hosts.getByRole('checkbox', 'Add e2e-jump without the jump');
    await expect(noJump).toBeVisible();

    // The desktop window's default size (internal/desktop), then its
    // minimum width.
    for (const [width, height] of [[1280, 840], [800, 600]]) {
      await browser.setViewport({ width, height });
      const g = (await browser.evaluate(VIA_LAYOUT)) as ViaLayout;
      expect(g.scrollWidth, `${width}px: the host table scrolls sideways`).toBeLessThanOrEqual(g.clientWidth);
      expect(g.parts.length).toBeGreaterThan(6);
      for (const p of g.parts) {
        expect(p.left, `${width}px: ${p.what} starts left of the card`).toBeGreaterThanOrEqual(g.card.left);
        expect(p.right, `${width}px: ${p.what} runs past the card`).toBeLessThanOrEqual(g.card.right);
      }
      // The label sits beside its box, after a gap, on the same line.
      expect(g.label.left - g.check.right).toBeGreaterThan(0);
      expect(g.label.top).toBeLessThan(g.check.bottom);
      expect(g.label.bottom).toBeGreaterThan(g.check.top);
      for (const theme of ['dark', 'light']) {
        await browser.evaluate(`window.__keelApplyTheme(${JSON.stringify(theme)})`);
        await app.screenshot(`${theme}-${width}-first-run-hosts-via`);
      }
      await browser.evaluate('window.__keelApplyTheme("dark")');
    }

    // Still a working box.
    await noJump.tap();
    await expect(noJump).toHaveAttribute('aria-checked', 'true');
  } finally {
    await browser.setViewport({ width: 1280, height: 720 });
    await keel.stop();
  }
});

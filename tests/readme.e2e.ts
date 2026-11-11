import { describe, test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { homedir, hostname, userInfo } from 'node:os';
import { keelApi, keelSend, openKeel, retire, sshTest } from './keel.ts';

// The README and docs/ screenshots (docs/images/). Refresh them on macOS
// (so key hints show ⌘) with
//   npx e2e run tests/readme.e2e.ts --target webkit --output .e2e/proof/readme
// then copy hero-*, palette-dark, newtab-dark and vault-dark from
// .e2e/proof/readme into docs/images/. The hosts are made up: none of them
// connects, only Local Shell does.
const themeScript = (theme: string) =>
  `try { if (location.protocol.startsWith('http')) localStorage.setItem('keel-theme', '${theme}'); } catch {}`;

const GROUPS: Record<string, [string, string, string][]> = {
  'production': [
    ['api-1', '10.0.4.11', 'deploy'],
    ['api-2', '10.0.4.12', 'deploy'],
    ['pg-primary', '10.0.4.21', 'postgres'],
  ],
  'staging': [
    ['staging-web', '10.0.8.5', 'deploy'],
    ['build-runner', '10.0.8.40', 'ci'],
  ],
};
// The app modifier: ⌘ on macOS, Ctrl elsewhere (lib/shortcuts.js).
const MOD = process.platform === 'darwin' ? 'Meta' : 'Control';
// Demo output for the two panes, defined then cleared so the shot shows
// only the prompt, the command and its output (Local Shell is /bin/sh).
const SETUP = String.raw`PS1='$ '; deploy() { printf '\033[2m→\033[0m building api@4f2c9e1\n\033[32m✓\033[0m tests passed (212)\n\033[2m→\033[0m rolling out to api-1, api-2\n\033[32m✓\033[0m api-1 healthy\n\033[32m✓\033[0m api-2 healthy\n\033[1mDeployed in 42s\033[0m\n'; }; logs() { printf '\033[2m12:04:31\033[0m \033[36mINFO\033[0m  GET /healthz 200 1ms\n\033[2m12:04:32\033[0m \033[36mINFO\033[0m  POST /v1/orders 201 38ms\n\033[2m12:04:35\033[0m \033[33mWARN\033[0m  slow query orders_by_user 412ms\n\033[2m12:04:36\033[0m \033[36mINFO\033[0m  GET /v1/orders/9812 200 6ms\n\033[2m12:04:40\033[0m \033[31mERROR\033[0m upstream payments timeout, retrying\n\033[2m12:04:41\033[0m \033[36mINFO\033[0m  payments ok after retry 220ms\n'; }; clear`;
// The reference screens' own setup: ASCII only, a bare prompt and a clear screen (SETUP's → and ✓ are typed wrongly
// by this machine's WebKit, which left raw printf text in the pane).
const QUIET = "PS1='$ '; clear";
const pause = (ms: number) => new Promise((r) => setTimeout(r, ms));

async function run(term: any, line: string) {
  await term.pressSequentially(line);
  await term.press('Enter');
  await pause(400);
}

const HOSTS = /^(api-[12]|pg-primary|staging-web|build-runner)$/;

async function seed(app: any) {
  for (const [group, hosts] of Object.entries(GROUPS)) {
    const g = await keelSend<{ id: string }>(app.baseUrl, 'POST', '/api/groups', { name: group });
    for (const [name, address, username] of hosts) {
      await keelSend(app.baseUrl, 'POST', '/api/hosts', { name, address, port: 22, username, authType: 'password', password: 'readme-scratch', groupId: g.id });
    }
  }
}

async function unseed(app: any) {
  await retire(app, HOSTS, 'production');
  await retire(app, /^$/, 'staging');
}

describe('readme', { tags: ['readme'], serial: true }, () => {
  for (const theme of ['dark', 'light'] as const) {
    test(`readme screenshots (${theme})`, async ({ app, screen, browser }) => {
      await unseed(app);
      await seed(app);
      try {
        await browser.addInitScript(themeScript(theme));
        await openKeel(app);
        const vault = screen.getByRole('complementary', 'Vault');
        await vault.getByRole('button', 'Hosts').tap();
        await expect(vault.getByRole('button', /^pg-primary\b/)).toBeVisible();

        await expect(screen.getByText('Where to?')).toBeVisible();
        await app.screenshot(`newtab-${theme}`);

        await vault.getByRole('button', /^Local Shell\b/).tap();
        const terms = screen.getByRole('textbox', 'Terminal input');
        await expect(terms).toBeFocused();
        await run(terms, SETUP);
        await run(terms, 'deploy api');
        await app.screenshot(`vault-${theme}`);

        await terms.press(`${MOD}+Shift+D`);
        await expect(terms).toHaveCount(2);
        await run(terms.nth(1), SETUP);
        await run(terms.nth(1), 'logs');
        await app.screenshot(`hero-${theme}`);

        await terms.nth(1).press(`${MOD}+k`);
        await expect(screen.getByRole('dialog', 'Command palette')).toBeVisible();
        await screen.getByRole('combobox', 'Run a command or connect a host…').fill('api');
        await app.screenshot(`palette-${theme}`);
        await screen.getByRole('combobox', 'Run a command or connect a host…').press('Escape');

        await terms.nth(1).press(`${MOD}+w`);
        await expect(terms).toHaveCount(1);
        await screen.getByRole('button', 'Close tab', { visible: true }).first().tap();
        await expect(terms).toHaveCount(0);
      } finally {
        await unseed(app);
      }
    });
  }
});

// Reference screens for the native client (plan 016; docs/images/reference/
// <screen>-<theme>.png). Same made-up hosts as above. Anything a real
// machine would add to a page (this checkout's path, the user name, a join
// link's key) is replaced in the DOM before each shot. Copy the files named
// ref-* from .e2e/proof/readme, dropping the "ref-" prefix.
async function scrub(browser: any) {
  await browser.evaluate(`(() => {
    const subs = [[${JSON.stringify('~' + process.cwd().slice(homedir().length))}, '/srv/keel'], [${JSON.stringify(process.cwd())}, '/srv/keel'], [${JSON.stringify(homedir())}, '/home/vm'], [${JSON.stringify(hostname())}, 'vm'], [${JSON.stringify(hostname().split('.')[0])}, 'vm'], [${JSON.stringify(userInfo().username)}, 'vm']];
    const fix = (s) => { for (const [a, b] of subs) if (a) s = s.split(a).join(b); return s.replace(/#k=[^\\s"'<]+/g, '#k=…').replace(/room=[0-9a-f]+/g, 'room=…').replace(/keel_token=[0-9a-f]+/g, 'keel_token=…'); };
    const w = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
    for (let n; (n = w.nextNode()); ) { const v = fix(n.nodeValue); if (v !== n.nodeValue) n.nodeValue = v; }
    for (const el of document.querySelectorAll('input, textarea')) { if (el.type === 'password') continue; const v = fix(el.value); if (v !== el.value) el.value = v; }
  })()`);
}
async function refShot(app: any, browser: any, name: string) {
  await scrub(browser);
  await app.screenshot(`ref-${name}`);
}
async function localShell(screen: any) {
  const vault = screen.getByRole('complementary', 'Vault');
  await vault.getByRole('button', 'Hosts').tap();
  await vault.getByRole('button', /^Local Shell\b/).tap();
  const term = screen.getByRole('textbox', 'Terminal input');
  await expect(term).toBeFocused();
  await pause(1000); // the shell must be ready: typing at once loses the first characters
  await run(term, QUIET);
  return term;
}
async function closeAllTabs(screen: any, browser: any) {
  for (let i = 0; i < 8; i++) {
    const pane = screen.getByRole('button', 'Close pane', { visible: true });
    const tab = screen.getByRole('button', 'Close tab', { visible: true });
    if (await pane.count()) await pane.last().tap();
    else if (await tab.count()) await tab.first().tap();
    else break;
  }
  // Tab pages (Settings, Keychain…) come back with the window; ⌘W closes the selected tab.
  for (let i = 0; i < 8; i++) {
    await browser.keyboard.press(`${MOD}+w`);
    await pause(150);
  }
}

describe('reference screens', { tags: ['readme'], serial: true }, () => {
  for (const theme of ['dark', 'light'] as const) {
    test(`settings sections and the host dialog (${theme})`, async ({ app, screen, browser }) => {
      await unseed(app);
      await seed(app);
      try {
        await browser.addInitScript(themeScript(theme));
        await openKeel(app);
        const vault = screen.getByRole('complementary', 'Vault');
        await vault.getByRole('button', 'Hosts').tap();
        await vault.getByRole('button', 'Settings').tap();
        const page = screen.getByRole('region', 'Settings');
        for (const [tab, slug] of [['Appearance', 'appearance'], ['Terminal', 'terminal'], ['Keyboard', 'keyboard'], ['Agent', 'agent'], ['Sharing', 'sharing'], ['Vault & security', 'vault'], ['Runtime', 'runtime']] as const) {
          await page.getByRole('tab', tab).tap();
          await expect(page.getByRole('heading', tab)).toBeVisible();
          await refShot(app, browser, `settings-${slug}-${theme}`);
        }
        await page.getByRole('tab', /^About Keel/).tap();
        await expect(page.getByRole('heading', /About Keel/)).toBeVisible();
        await refShot(app, browser, `settings-about-${theme}`);

        await vault.getByRole('button', /^api-1\b/).secondaryTap();
        await screen.getByRole('menuitem', 'Edit').tap();
        const dialog = screen.getByRole('dialog').filter({ hasText: 'Edit host' });
        await expect(dialog).toBeVisible();
        await refShot(app, browser, `host-dialog-${theme}`);
        await screen.getByRole('button', 'Cancel', { visible: true }).tap();
      } finally {
        await closeAllTabs(screen, browser);
        await unseed(app);
      }
    });
  }

  test('pages: Keychain, Port forwarding, Snippets, Logs (dark)', async ({ app, screen, browser }) => {
    await unseed(app);
    await seed(app);
    const made: [string, string][] = [];
    // Leftovers of an aborted run would show as duplicate rows.
    for (const s of await keelApi<{ id: string; name: string }[]>(app.baseUrl, '/api/snippets')) {
      if (s.name === 'disk usage' || s.name === 'recent errors') await keelSend(app.baseUrl, 'DELETE', `/api/snippets/${s.id}`).catch(() => {});
    }
    try {
      const hosts = await keelApi<{ id: string; name: string }[]>(app.baseUrl, '/api/hosts');
      const pg = hosts.find((h) => h.name === 'pg-primary')!;
      const ident = await keelSend<{ id: string }>(app.baseUrl, 'POST', '/api/identities', { name: 'deploy', kind: 'password', username: 'deploy', password: 'readme-scratch' });
      made.push(['identities', ident.id]);
      const fwd = await keelSend<{ id: string }>(app.baseUrl, 'POST', '/api/tunnels', { name: 'pg-primary db', hostId: pg.id, kind: 'local', bindAddr: '127.0.0.1', bindPort: 15432, destHost: '127.0.0.1', destPort: 5432 });
      made.push(['tunnels', fwd.id]);
      for (const [name, command] of [['disk usage', 'df -h'], ['recent errors', 'journalctl -p err -n 50']]) {
        made.push(['snippets', (await keelSend<{ id: string }>(app.baseUrl, 'POST', '/api/snippets', { name, command })).id]);
      }
      await browser.addInitScript(themeScript('dark'));
      await openKeel(app);
      const vault = screen.getByRole('complementary', 'Vault');
      await vault.getByRole('button', 'Keychain').tap();
      await expect(screen.getByRole('button', 'New identity')).toBeVisible();
      await expect(screen.getByRole('region', 'Keychain')).toContainText('deploy');
      await refShot(app, browser, 'keychain-dark');
      await vault.getByRole('button', 'Forwards').tap();
      await vault.getByRole('button', 'Manage forwards').tap();
      await expect(screen.getByRole('region', 'Port forwarding')).toContainText('pg-primary db');
      await refShot(app, browser, 'forwards-dark');
      await vault.getByRole('button', 'Snippets').tap();
      await vault.getByRole('button', 'Manage snippets').tap();
      await expect(screen.getByRole('textbox', 'Snippet name')).toBeVisible();
      await refShot(app, browser, 'snippets-dark');
      await vault.getByRole('button', 'Logs').tap();
      await expect(screen.getByRole('searchbox', 'Search events')).toBeVisible();
      await refShot(app, browser, 'logs-dark');
    } finally {
      await closeAllTabs(screen, browser);
      for (const [kind, id] of made) await keelSend(app.baseUrl, 'DELETE', `/api/${kind}/${id}`).catch(() => {});
      await unseed(app);
    }
  });

  test('workspace screens: caps, Approve before type, Share, agent (dark)', async ({ app, screen, browser }) => {
    await unseed(app);
    await seed(app);
    const made: string[] = [];
    for (const s of await keelApi<{ id: string; name: string }[]>(app.baseUrl, '/api/snippets')) {
      if (s.name === 'status check') await keelSend(app.baseUrl, 'DELETE', `/api/snippets/${s.id}`).catch(() => {});
    }
    try {
      made.push((await keelSend<{ id: string }>(app.baseUrl, 'POST', '/api/snippets', { name: 'status check', command: 'uptime\ndf -h' })).id);
      await keelSend(app.baseUrl, 'PUT', '/api/agent/keys/gemini', { key: 'e2e-not-a-real-gemini-key-0000' });
      await keelSend(app.baseUrl, 'DELETE', '/api/agent/conversations');
      await browser.addInitScript(themeScript('dark'));
      await openKeel(app);
      const vault = screen.getByRole('complementary', 'Vault');
      await closeAllTabs(screen, browser);
      const terms = await localShell(screen);

      // The modifier held: key caps (the app shows them after 280 ms).
      await terms.press(`${MOD}+Shift+D`);
      await expect(terms).toHaveCount(2);
      await run(terms.nth(1), QUIET);
      await run(terms.first(), 'echo deploy ok');
      await terms.nth(1).click();
      // Playwright's keyboard here only presses, so the hold is the modifier's own keydown (term.js listens on window).
      await browser.evaluate(`window.dispatchEvent(new KeyboardEvent('keydown', { key: '${MOD}', metaKey: ${MOD === 'Meta'}, ctrlKey: ${MOD === 'Control'}, bubbles: true }))`);
      await expect.poll(() => browser.evaluate(`document.documentElement.classList.contains('show-kbd')`)).toBe(true);
      await refShot(app, browser, 'keycaps-dark');
      await browser.evaluate(`window.dispatchEvent(new KeyboardEvent('keyup', { key: '${MOD}', bubbles: true }))`);
      await terms.nth(1).press(`${MOD}+w`);
      await expect(terms).toHaveCount(1);

      // A snippet types through Approve before type.
      await vault.getByRole('button', 'Snippets').tap();
      await vault.getByRole('button', /^status check\b/).tap(); // two lines: always through Approve before type
      const approve = screen.getByRole('dialog').filter({ hasText: 'Type into the terminal?' });
      await expect(approve).toBeVisible();
      await refShot(app, browser, 'approve-before-type-dark');
      await approve.getByRole('button', 'Cancel').tap();
      await expect(approve).toBeHidden();
      await vault.getByRole('button', 'Hosts').tap();

      // The agent panel after one question to the fake provider.
      await terms.press(`${MOD}+i`);
      const panel = screen.getByRole('complementary', 'Agent');
      await expect(panel).toBeVisible();
      const ask = panel.getByRole('textbox', 'Message the agent');
      await ask.fill('Is this machine busy?');
      await ask.press('Enter');
      await expect(panel.getByRole('button', /Type into pane/)).toBeVisible();
      await refShot(app, browser, 'agent-panel-dark');
      await panel.getByRole('button', 'Close agent').tap();
      await expect(panel).toBeHidden();

      // The Share popover (the e2e config's loopback signal server).
      await screen.getByRole('button', 'Share').tap();
      await pause(1500);
      await refShot(app, browser, 'share-popover-dark');
      const pop = screen.getByRole('dialog', 'Share session');
      if (await pop.isVisible()) await pop.press('Escape');
    } finally {
      await closeAllTabs(screen, browser);
      for (const id of made) await keelSend(app.baseUrl, 'DELETE', `/api/snippets/${id}`).catch(() => {});
      await unseed(app);
    }
  });

  test('failed sessions (dark)', async ({ app, screen, browser }) => {
    await unseed(app);
    await seed(app);
    const { jump: target } = sshTest(); // forwards, so a direct session reaches its key prompt
    const made: string[] = [];
    try {
      // Nothing listens on loopback port 1: the connection is refused at once.
      const down = await keelSend<{ id: string }>(app.baseUrl, 'POST', '/api/hosts', { name: 'api-3', address: '127.0.0.1', port: 1, username: 'deploy', authType: 'password', password: 'readme-scratch' });
      const key = await keelSend<{ id: string }>(app.baseUrl, 'POST', '/api/hosts', { name: 'edge-1', address: target.host, port: target.port, username: target.user, authType: 'password', password: target.password });
      made.push(down.id, key.id);
      await keelSend(app.baseUrl, 'DELETE', `/api/hosts/${key.id}/host-key`).catch(() => {});
      await browser.addInitScript(themeScript('dark'));
      await openKeel(app);
      const vault = screen.getByRole('complementary', 'Vault');
      await vault.getByRole('button', 'Hosts').tap();
      await vault.getByRole('button', /^api-3\b/).tap();
      // The pane's banner and connection log; the app keeps its banner on "Connecting" while the log shows the refusal.
      await expect(screen.getByText(/connection refused/).first()).toBeVisible({ timeout: 20_000 });
      await refShot(app, browser, 'failed-session-dark');
      await closeAllTabs(screen, browser);
      await vault.getByRole('button', /^edge-1\b/).tap();
      // The loopback SSH fixture ends in "Connection failed" with Reconnect; its session never reaches the host key
      // prompt (no errorKind reaches the page), so there is no host key dialog reference.
      await expect(screen.getByRole('button', 'Reconnect', { visible: true })).toBeVisible({ timeout: 20_000 });
      await refShot(app, browser, 'failed-session-reconnect-dark');
    } finally {
      await closeAllTabs(screen, browser);
      for (const id of made) {
        await keelSend(app.baseUrl, 'DELETE', `/api/hosts/${id}/host-key`).catch(() => {});
        await keelSend(app.baseUrl, 'DELETE', `/api/hosts/${id}`).catch(() => {});
      }
      await unseed(app);
    }
  });
});

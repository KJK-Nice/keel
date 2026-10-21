import { describe, test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelSend, openKeel, retire } from './keel.ts';

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

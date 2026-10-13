// Bug-bash and live MCP config: see .agents/skills/verify-keel/SKILL.md.
import type { E2EConfig } from 'e2e';
import { web } from '@e2e-dev/web';
import { gateway } from 'ai';
import base from './e2e.config.ts';
import { keelToken } from './tests/keel.ts';

const shared: E2EConfig = base;
// The engine does not expose its browser, so keep it by target name (see e2e.config.ts).

const context =
  'Keel is a local SSH workspace in a browser tab. The Vault is a left sidebar on every tab (Hosts, Snippets, Forwards segments; Keychain, Logs, Settings in its footer); Keychain, Port forwarding, Snippets, Notebooks, Logs and Settings open as their own tab pages; other tabs are terminal sessions or a New tab. ' +
  'This local app has no reachable SSH server: connecting to any host except Local Shell is expected to fail or ask to trust a key. No signal server is configured, so Share and Join links do not work. ' +
  'Never type commands that delete files, change system settings, or reach the network in a terminal; harmless commands such as echo, pwd and ls are fine. ' +
  'Not bugs: the terminal is painted with WebGL, so its text is only in screenshots, not in the accessibility tree; ⌘-shortcuts the native desktop window owns may do nothing in this browser tab; a fresh Vault holds only the Local Shell host and the default snippets.';
const persona = { model: gateway('openai/gpt-6-luna-fast'), maxSteps: 40, maxModelCalls: 40, context };

export default {
  ...shared,
  tests: [shared.tests ?? 'tests/**/*.e2e.ts', 'tests/bugbash/**/*.e2e.ts'].flat(),
  // explore opens the app URL itself, with no token, which is keel serve's
  // 401 page. The header authenticates page and API requests; WebSockets
  // (/ws/session, /ws/herd) carry no extra headers, so an init script also
  // sets the per-port auth cookie the desktop app would have received.
  targets: shared.targets.map((t) => ({
    ...t,
    engine: web({
      browser: t.name === 'webkit' ? 'webkit' : undefined,
      headers: { 'X-Keel-Token': keelToken() },
      initScripts: [`document.cookie = 'keel_auth_' + (location.port || '80') + '=${keelToken()}; path=/; SameSite=Strict';`],
    }),
  })),
  retries: 0,
  reporters: ['list'],
  agents: {
    default: persona,
    skeptic: { ...persona, system: 'Distrust every number, count, label, and status on screen; cross-check each against every other place it appears, and against what you just did.' },
    fuzzer: { ...persona, system: "At every input, run the goal's input matrix before anything else, judging each entry before the next. Never take the happy path." },
  },
} satisfies E2EConfig;

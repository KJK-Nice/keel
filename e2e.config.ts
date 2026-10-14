import type { E2EConfig } from 'e2e';
import { web } from '@e2e-dev/web';
import { gateway } from 'ai';
import { keelData, keelToken } from './tests/keel.ts';

// The runner starts its own `keel serve` (built by `make build`) on a free
// loopback port with a scratch data dir under .e2e/, so a run never touches
// the user's Keel on 127.0.0.1:47241, their keel.json, or their vault.
// KEEL_VAULT_PASSPHRASE keeps the scratch vault off the OS keychain: without
// it keel serve would read or create the user's "keel"/"vault" keyring item.
// scripts/e2e-serve.sh first starts bin/keel-signal on a free loopback port
// and sets KEEL_SIGNAL, so Share and Join work (tests/share.e2e.ts).
const app = {
  url: 'http://127.0.0.1:0',
  readyUrl: 'http://127.0.0.1:{port}/api/health',
  command: {
    executable: './scripts/e2e-serve.sh',
    args: ['serve'],
    env: {
      KEEL_ADDR: '127.0.0.1:{port}',
      KEEL_DATA: keelData,
      KEEL_NAME: 'e2e',
      KEEL_VAULT_PASSPHRASE: process.env.KEEL_E2E_VAULT_PASSPHRASE ?? 'keel-e2e-scratch-vault',
      SHELL: '/bin/sh',
    },
    log: '.e2e/logs/keel-serve.log',
  },
};

export default {
  tests: 'tests/**/*.e2e.ts',
  // One file at a time: every test shares the one keel serve, and each page
  // load restores all of its running sessions as tabs, so a parallel file's
  // Local Shell would show up (and close) in another test's window.
  workers: 1,
  // webkit is primary (the macOS app is a WKWebView); chromium stands in for
  // Windows WebView2. Both share one app, so each run starts one keel serve.
  targets: [
    { name: 'webkit', engine: web({ browser: 'webkit' }), app },
    { name: 'chromium', engine: web(), app },
  ],
  // Only agent.* steps and `e2e explore` need this; set AI_GATEWAY_API_KEY.
  agents: {
    default: {
      model: gateway('openai/gpt-6-luna-fast'),
      system: 'You are a thorough QA agent. Verify every outcome.',
      context: 'Keel is a local SSH workspace. The Vault is a left sidebar on every tab (Hosts, Snippets, Forwards segments; Keychain, Logs, Settings in its footer); Keychain, Port forwarding, Snippets, Notebooks, Logs and Settings open as their own tab pages; other tabs are terminal sessions or a New tab. There is no network SSH server in tests: a host connect is expected to fail; "Local Shell" in the sidebar opens a shell on this machine.',
    },
  },
  // The scratch keel serve's auth token, so reports redact it from URLs.
  secrets: { 'keel-token': keelToken() },
} satisfies E2EConfig;

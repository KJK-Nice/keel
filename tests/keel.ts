// Shared by e2e.config.ts and the tests: where the scratch keel serve keeps
// its data, and the auth token it requires on /, /api/, /ws/ and /ui/.
import { spawn, type ChildProcess } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { createServer } from 'node:net';
import { join, resolve } from 'node:path';

const root = resolve(import.meta.dirname, '..');

// sshDir is the fixture ~/.ssh keel serve reads for the host import
// (KEEL_SSH_DIR). scripts/e2e-serve.sh rebuilds it from tests/fixtures on
// every run, with fresh keys; ~ in its config is .e2e/ssh-home.
export const sshDir = join(root, '.e2e', 'ssh-home', '.ssh');

// KEEL_E2E_DATA gives a run its own data dir: keel serve locks the one it
// uses, so parallel bug-bash explorers each need their own.
export const keelData = resolve(import.meta.dirname, '..', process.env.KEEL_E2E_DATA ?? '.e2e/keel-data');
const tokenFile = join(keelData, 'keel-serve.token');

// keelToken returns the token keel serve will load from keelData, writing
// one first if there is none (same format as internal/authtoken: 64 hex).
// The config calls it at load, before the runner spawns keel serve, so the
// value is known up front and registered as a secret.
export function keelToken(): string {
  try {
    const tok = readFileSync(tokenFile, 'utf8').trim();
    if (/^[0-9a-f]{64}$/.test(tok)) return tok;
  } catch {}
  mkdirSync(keelData, { recursive: true, mode: 0o700 });
  const tok = randomBytes(32).toString('hex');
  writeFileSync(tokenFile, tok + '\n', { mode: 0o600 });
  return tok;
}

// openKeel loads the UI the way the desktop app does: the token rides the
// first request, the server trades it for an HttpOnly cookie and redirects
// to / without it.
export async function openKeel(app: { open(path: string): Promise<unknown> }): Promise<void> {
  await app.open(`/?keel_token=${keelToken()}&name=e2e`);
}

// keelApi reads keel serve's JSON API with the token header, the way native
// clients do. Tests use it to check side effects, never to drive the UI.
export async function keelApi<T = unknown>(baseUrl: string | undefined, path: string): Promise<T> {
  const res = await fetch(new URL(path, baseUrl), { headers: { 'X-Keel-Token': keelToken() } });
  if (!res.ok) throw new Error(`GET ${path}: HTTP ${res.status}`);
  return (await res.json()) as T;
}

// keelSend changes keel serve's state through its JSON API. Tests use it
// only to set up or clean up (a test host, the fake agent key), never for
// the step under test.
export async function keelSend<T = unknown>(baseUrl: string | undefined, method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(new URL(path, baseUrl), {
    method,
    headers: { 'X-Keel-Token': keelToken(), 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!res.ok) throw new Error(`${method} ${path}: HTTP ${res.status}`);
  return (res.status === 204 ? null : await res.json()) as T;
}

// killSessions ends every session keel serve still runs. Files whose
// tests count terminals call it after each test (afterEach), so a test
// that fails before closing its tabs does not leave a session for the next
// file's page load to restore as a tab, failing that test too.
export async function killSessions(baseUrl: string | undefined): Promise<void> {
  const sessions = await keelApi<{ id: string }[]>(baseUrl, '/api/sessions');
  for (const s of sessions) {
    try {
      await keelSend(baseUrl, 'DELETE', `/api/sessions/${encodeURIComponent(s.id)}`);
    } catch {
      /* already gone */
    }
  }
}

// keelJson is the scratch keel.json as keel serve last wrote it.
export function keelJson(): string {
  return readFileSync(join(keelData, 'keel.json'), 'utf8');
}

// A second keel serve for the vault screens (Unlock, First run), which the
// runner's keel serve cannot show: that one opens its vault with
// KEEL_VAULT_PASSPHRASE. This one gets no passphrase and KEEL_KEYRING=off,
// so its vault needs setup (and later a passphrase) without ever touching
// the OS keychain; KEEL_SSH_DIR is the fixture and HOME a scratch folder.
// Its data lives in .e2e/first-run-data/<name>, emptied first. It is
// stopped by its PID, never by name.
export type ScratchKeel = {
  baseUrl: string;
  dataDir: string;
  openUrl: string;
  api<T = unknown>(path: string): Promise<T>;
  restart(): Promise<void>;
  stop(): Promise<void>;
};

async function freePort(): Promise<number> {
  return new Promise((ok, fail) => {
    const srv = createServer();
    srv.once('error', fail);
    srv.listen(0, '127.0.0.1', () => {
      const addr = srv.address();
      const port = typeof addr === 'object' && addr ? addr.port : 0;
      srv.close(() => ok(port));
    });
  });
}

export async function startScratchKeel(name: string): Promise<ScratchKeel> {
  const dataDir = join(root, '.e2e', 'first-run-data', name);
  rmSync(dataDir, { recursive: true, force: true });
  mkdirSync(dataDir, { recursive: true, mode: 0o700 });
  const token = randomBytes(32).toString('hex');
  writeFileSync(join(dataDir, 'keel-serve.token'), `${token}\n`, { mode: 0o600 });
  const home = join(dataDir, 'home');
  mkdirSync(home, { recursive: true });
  const port = await freePort();
  const baseUrl = `http://127.0.0.1:${port}`;
  let child: ChildProcess | null = null;

  async function launch() {
    child = spawn(join(root, 'bin', 'keel'), ['serve'], {
      cwd: root,
      env: {
        PATH: process.env.PATH ?? '/usr/bin:/bin',
        HOME: home,
        USER: 'e2e',
        SHELL: '/bin/sh',
        KEEL_ADDR: `127.0.0.1:${port}`,
        KEEL_DATA: dataDir,
        KEEL_NAME: 'e2e',
        KEEL_KEYRING: 'off',
        KEEL_SSH_DIR: sshDir,
        KEEL_VAULT_FAST: '1',
      },
      stdio: 'ignore',
    });
    for (let i = 0; i < 100; i++) {
      try {
        const res = await fetch(`${baseUrl}/api/health`);
        if (res.ok) return;
      } catch {}
      await new Promise((r) => setTimeout(r, 100));
    }
    throw new Error('scratch keel serve did not start');
  }

  async function stop() {
    const c = child;
    child = null;
    if (!c || c.exitCode !== null) return;
    const exited = new Promise((r) => c.once('exit', r));
    c.kill('SIGTERM');
    await Promise.race([exited, new Promise((r) => setTimeout(r, 5000))]);
    if (c.exitCode === null) c.kill('SIGKILL');
  }

  await launch();
  return {
    baseUrl,
    dataDir,
    openUrl: `${baseUrl}/?keel_token=${token}&name=e2e`,
    async api<T = unknown>(path: string): Promise<T> {
      const res = await fetch(new URL(path, baseUrl), { headers: { 'X-Keel-Token': token } });
      if (!res.ok) throw new Error(`GET ${path}: HTTP ${res.status}`);
      return (await res.json()) as T;
    },
    async restart() {
      await stop();
      await launch();
    },
    stop,
  };
}

// sshTest is the two loopback SSH servers scripts/e2e-serve.sh starts for
// the Edit host tests (bin/keel-sshtest): a jump host and a target behind
// it, with throwaway passwords. Nothing else listens for SSH in e2e.
export type SSHTestServer = { host: string; port: number; user: string; password: string; fingerprint: string };
export function sshTest(): { jump: SSHTestServer; target: SSHTestServer } {
  return JSON.parse(readFileSync(join(keelData, 'sshtest.json'), 'utf8'));
}

// retire removes a test's stand-in Local Shell hosts (tests/groups.e2e.ts)
// at the end, so other tests find only the seeded Local Shell. keel serve
// refuses to delete a local host, so each is made an ordinary SSH host
// first, then deleted; and the test's folder.
export async function retire(app: any, names: RegExp, group = '') {
  for (const h of await keelApi<{ id: string; name: string; authType: string }[]>(app.baseUrl, '/api/hosts')) {
    if (!names.test(h.name)) continue;
    if (h.authType === 'local') await keelSend(app.baseUrl, 'PUT', `/api/hosts/${h.id}`, { ...h, authType: 'password', address: 'retired.invalid', username: 'e2e', port: 22 });
    await keelSend(app.baseUrl, 'DELETE', `/api/hosts/${h.id}`);
  }
  for (const g of await keelApi<{ id: string; name: string }[]>(app.baseUrl, '/api/groups')) {
    if (group && g.name === group) await keelSend(app.baseUrl, 'DELETE', `/api/groups/${g.id}`);
  }
}

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

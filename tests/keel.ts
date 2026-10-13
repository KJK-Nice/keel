// Shared by e2e.config.ts and the tests: where the scratch keel serve keeps
// its data, and the auth token it requires on /, /api/, /ws/ and /ui/.
import { randomBytes } from 'node:crypto';
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';

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

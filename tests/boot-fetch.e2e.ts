import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelApi, keelSend, openKeel } from './keel.ts';

// Issue #54: boot loads the JSON state once (loadAll), and the stream's
// first push, which carries the same rev, does not load it again. A save
// and a stream reconnect still do (lib/statefollow.js, uistream.go rev).

type Host = { id: string; name: string };

// How many times the page fetched /api/hosts: once per loadAll.
const hostFetches = (browser: any) =>
  browser.evaluate(`performance.getEntriesByType('resource').filter((e) => new URL(e.name).pathname === '/api/hosts').length`) as Promise<number>;

// Long enough for the stream to open and push, and for a refresh it would
// start to show up; the count must hold still after it.
const settle = () => new Promise((r) => setTimeout(r, 1500));

test('boot loads the state once; a save and a stream reconnect reload it', { tags: ['stream'] }, async ({ app, screen, browser }) => {
  await openKeel(app);
  const vault = screen.getByRole('complementary', 'Vault');
  await expect(vault.getByRole('button', /^Local Shell\b/)).toBeVisible();
  await settle();
  expect(await hostFetches(browser)).toBe(1);

  // A save pushes the lists, and that push reloads the JSON state once.
  const name = `e2e-boot-${Date.now().toString(36)}`;
  await vault.getByRole('button', 'New host').tap();
  const dialog = screen.getByRole('dialog').filter({ hasText: 'New host' });
  await dialog.getByLabel('Label').fill(name);
  await dialog.getByLabel('Hostname / IP').fill('192.0.2.11');
  await dialog.getByLabel('Username').fill('ubuntu');
  await dialog.getByRole('button', 'Save host').tap();
  await expect(dialog).toBeHidden();
  await expect(vault.getByRole('button', new RegExp(`^${name}\\b`))).toBeVisible();
  await expect.poll(() => hostFetches(browser)).toBe(2);
  await settle();
  expect(await hostFetches(browser)).toBe(2);

  // A reconnect (the laptop slept, keel serve restarted) opens a new
  // stream; its first push reloads even though nothing changed.
  await browser.evaluate('window.dispatchEvent(new CustomEvent("keel-stream"))');
  await expect.poll(() => hostFetches(browser)).toBe(3);
  await settle();
  expect(await hostFetches(browser)).toBe(3);

  const host = (await keelApi<Host[]>(app.baseUrl, '/api/hosts')).find((h) => h.name === name);
  if (host) await keelSend(app.baseUrl, 'DELETE', `/api/hosts/${host.id}`);
});

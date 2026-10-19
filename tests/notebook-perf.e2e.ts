import { describe, test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelApi, keelSend, openKeel } from './keel.ts';

// Typing in a notebook stays fast (issue #82). Each notebook save is a
// store save, which pushes every vault list to the window; each push used
// to leave one portaled dropdown menu per menu on the page in <body>, and
// the component observers then scanned all of them on every DOM change.
// Saves also ran on every 400 ms pause. This pins: edits settle into a few
// saves, a push leaves <body> as it was, keystrokes stay well under a
// frame budget, and the last edit is saved even when the pane closes
// right after it.

type Notebook = { id: string; name: string; cells: { body: string }[] };
type Host = { id: string };

// About 200 lines: prose, lists, eight shell fences and callouts.
function bigDoc(): string {
  const lines: string[] = [];
  for (let s = 0; s < 8; s++) {
    lines.push(`## Section ${s}`, '', `Replica **pg-${s}** falls behind; compare \`sent_lsn\` with \`replay_lsn\`.`, '');
    for (let i = 0; i < 5; i++) lines.push(`- step ${i}: check host-${s}-${i} and note the output`);
    lines.push('', '```sh');
    for (let i = 0; i < 6; i++) lines.push(`psql -h db-${s} -c "select now() /* ${i} */"`);
    lines.push('```', '', `> If lag passes 500 MB on pg-${s}, page the DB owner.`, '');
    for (let i = 0; i < 3; i++) lines.push(`Paragraph ${i} of section ${s}: the quick brown fox jumps over the lazy dog.`, '');
  }
  return lines.join('\n');
}

// Counts the page's own API calls and times each keystroke from
// beforeinput to the frame after it.
const PROBE = `(() => {
  window.__nbPerf = { puts: 0, loads: 0, pushes: 0, lat: [] };
  const f = window.fetch;
  window.fetch = function (input, init) {
    const url = String(input && input.url ? input.url : input);
    const method = String((init && init.method) || 'GET').toUpperCase();
    if (method === 'PUT' && /\\/api\\/notebooks\\//.test(url)) window.__nbPerf.puts++;
    if (method === 'GET' && /\\/api\\/hosts$/.test(url)) window.__nbPerf.loads++;
    return f.apply(this, arguments);
  };
  window.addEventListener('keel-vault-changed', () => window.__nbPerf.pushes++);
  document.addEventListener('beforeinput', () => {
    const t0 = performance.now();
    requestAnimationFrame(() => {
      const ch = new MessageChannel();
      ch.port1.onmessage = () => window.__nbPerf.lat.push(performance.now() - t0);
      ch.port2.postMessage(0);
    });
  }, true);
})()`;

const perf = (browser: any) => browser.evaluate(`({ puts: window.__nbPerf.puts, loads: window.__nbPerf.loads, pushes: window.__nbPerf.pushes })`);
const portaledMenus = (browser: any) => browser.evaluate(`document.querySelectorAll('body > [data-tui-dropdownmenu-content]').length`);

describe('notebook typing', { tags: ['notebook'], serial: true }, () => {
  test('stays fast and saves in a few requests', async ({ app, screen, browser }) => {
    const stamp = Date.now();
    const hosts: Host[] = [];
    for (let i = 0; i < 40; i++) {
      hosts.push(
        await keelSend<Host>(app.baseUrl, 'POST', '/api/hosts', {
          name: `perf-host-${stamp}-${i}`,
          address: `10.9.0.${i + 1}`,
          port: 22,
          username: 'ops',
          authType: 'password',
        }),
      );
    }
    const name = `nb-perf-${stamp}`;
    const nb = await keelSend<Notebook>(app.baseUrl, 'POST', '/api/notebooks', { name, cells: [{ body: bigDoc() }] });
    try {
      await browser.addInitScript(PROBE);
      await openKeel(app);
      const vault = screen.getByRole('complementary', 'Vault');
      await vault.getByRole('button', 'Snippets').tap();
      await vault.getByRole('button', new RegExp(`^${name}$`)).tap();
      await expect(screen.getByRole('textbox', 'Rename notebook')).toHaveValue(name);
      const editor = screen.getByRole('textbox', 'Notebook markdown');
      await expect(editor).toBeVisible();
      // Let boot's pushes settle before counting.
      await expect.poll(() => portaledMenus(browser), { timeout: 10_000 }).toBeGreaterThan(40);
      const menusBefore = await portaledMenus(browser);
      const before = await perf(browser);

      // 100 characters in five bursts with a short thinking pause between
      // them. A pause shorter than the idle delay does not save: the old
      // 400 ms debounce saved after every pause.
      const words = 'perf typing in a notebook ';
      const started = Date.now();
      for (let burst = 0; burst < 5; burst++) {
        await editor.pressSequentially(words.slice(0, 20), { delay: 40 });
        await new Promise((r) => setTimeout(r, 600));
        if (burst === 0 && Date.now() - started < 4000) expect((await perf(browser)).puts).toBe(before.puts);
      }
      const typedMs = Date.now() - started;
      // Settle: the last save and its push.
      await expect
        .poll(async () => (await keelApi<Notebook[]>(app.baseUrl, '/api/notebooks')).find((n) => n.id === nb.id)?.cells.map((c) => c.body).join(''), { timeout: 10_000 })
        .toContain('perf typing in a no');
      await new Promise((r) => setTimeout(r, 1500));

      const after = await perf(browser);
      const lat: number[] = await browser.evaluate(`window.__nbPerf.lat.slice().sort((a, b) => a - b)`);
      const p95 = lat[Math.floor(lat.length * 0.95)];
      console.log(`notebook typing: ${lat.length} keys in ${typedMs} ms, p50 ${lat[Math.floor(lat.length / 2)].toFixed(1)} ms, p95 ${p95.toFixed(1)} ms, saves ${after.puts - before.puts}, pushes ${after.pushes - before.pushes}, state loads ${after.loads - before.loads}`);

      expect(lat.length).toBeGreaterThanOrEqual(100);
      // A handful of saves, not one per keystroke, and one state reload
      // per push at most.
      expect(after.puts - before.puts).toBeLessThanOrEqual(Math.floor(typedMs / 5000) + 5);
      expect(after.loads - before.loads).toBeLessThanOrEqual(after.puts - before.puts + 1);
      // A push replaces the menus it re-renders; it does not add to them.
      expect(await portaledMenus(browser)).toBe(menusBefore);
      // Generous for CI; it was hundreds of ms per keystroke near a save.
      expect(p95).toBeLessThan(100);

      // The last edit lands even when the pane closes right after it.
      await editor.pressSequentially(' closing-flush', { delay: 10 });
      await screen.getByRole('button', 'Close tab', { visible: true }).first().tap();
      await expect
        .poll(async () => (await keelApi<Notebook[]>(app.baseUrl, '/api/notebooks')).find((n) => n.id === nb.id)?.cells.map((c) => c.body).join(''), { timeout: 10_000 })
        .toContain('closing-flush');
    } finally {
      await keelSend(app.baseUrl, 'DELETE', `/api/notebooks/${nb.id}`);
      for (const h of hosts) await keelSend(app.baseUrl, 'DELETE', `/api/hosts/${h.id}`).catch(() => {});
    }
  });
});

#!/usr/bin/env node
// Usage: node .agents/skills/pr-screenshots/compose.mjs <before-dir> <after-dir> <out-dir>
// Pairs <before-dir>/**/NNN-<name>.png with <after-dir>/**/NNN-<name>.png (same file name)
// and writes <out-dir>/<name>.png: before | after, labelled. Needs only playwright-core
// (already in node_modules via e2e) and its WebKit build.
import { readdirSync, readFileSync, mkdirSync, statSync } from 'node:fs';
import { join, basename } from 'node:path';
import { webkit } from 'playwright-core';

const [beforeDir, afterDir, outDir] = process.argv.slice(2);
if (!outDir) { console.error('usage: compose.mjs <before-dir> <after-dir> <out-dir>'); process.exit(2); }

const walk = (d) => readdirSync(d).flatMap((f) => {
  const p = join(d, f);
  return statSync(p).isDirectory() ? walk(p) : p.endsWith('.png') && p.includes('screenshots') ? [p] : [];
});
const index = (d) => new Map(walk(d).map((p) => [basename(p), p]));
const before = index(beforeDir), after = index(afterDir);
mkdirSync(outDir, { recursive: true });

const browser = await webkit.launch();
for (const [file, bp] of before) {
  const ap = after.get(file);
  if (!ap) { console.warn('no after shot for', file); continue; }
  const name = file.replace(/^\d+-/, '');
  const b64 = (p) => readFileSync(p).toString('base64');
  const html = `<body style="margin:0;background:#888;font:600 20px -apple-system,sans-serif;color:#fff">
    <div style="display:flex;gap:12px;padding:12px">
      ${[['Before', bp], ['After', ap]].map(([l, p]) => `<figure style="margin:0;flex:1"><div style="padding:0 0 8px">${l}</div><img style="width:100%;display:block" src="data:image/png;base64,${b64(p)}"></figure>`).join('')}
    </div></body>`;
  const page = await browser.newPage({ viewport: { width: 2400, height: 800 } });
  await page.setContent(html);
  await page.waitForFunction(() => [...document.images].every((i) => i.complete));
  await page.screenshot({ path: join(outDir, name), fullPage: true });
  await page.close();
  console.log('wrote', join(outDir, name));
}
await browser.close();

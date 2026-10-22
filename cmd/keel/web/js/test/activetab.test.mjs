import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

// Which tab is active is written in one place (setActive), and renderTabs
// only reads the workspace: it paints what the state says. These tests
// read term.js as text because it is one closure with no seams to call
// (as approve.test.mjs does).
const dir = dirname(fileURLToPath(import.meta.url));
const src = readFileSync(join(dir, "..", "term.js"), "utf8");

// Top-level functions of the term.js closure (two-space indent), name -> body.
function functions() {
  const out = new Map();
  const re = /^ {2}(?:async )?function (\w+)\(/gm;
  const starts = [];
  for (let m; (m = re.exec(src)); ) starts.push([m[1], m.index]);
  starts.forEach(([name, at], i) => out.set(name, src.slice(at, i + 1 < starts.length ? starts[i + 1][1] : src.length)));
  return out;
}
const fns = functions();
const writers = (field) => [...fns].filter(([, body]) => new RegExp(`state\\.${field}\\s*=(?!=)`).test(body)).map(([name]) => name);

test("only setActive writes state.activeId and state.lastSessionTabId", () => {
  assert.deepEqual(writers("activeId"), ["setActive"]);
  assert.deepEqual(writers("lastSessionTabId"), ["setActive"]);
});

test("renderTabs writes no workspace state and makes no tab", () => {
  const body = fns.get("renderTabs");
  assert.ok(body, "renderTabs not found");
  assert.doesNotMatch(body, /state\.\w+\s*=(?!=)/);
  assert.doesNotMatch(body, /state\.(tabs|panes)\.(set|delete)\(/);
  assert.doesNotMatch(body, /(?<![\w.])(setActive|settleTabs|createStartTab|showPane)\(/);
});

test("closing a tab settles the workspace before it renders", () => {
  const body = fns.get("closeTab");
  const del = body.indexOf("state.tabs.delete(");
  const settle = body.indexOf("settleTabs(");
  assert.ok(del >= 0 && settle > del, "closeTab must call settleTabs after it deletes the tab");
  assert.ok(body.indexOf("renderTabs(") > settle, "closeTab must render after it settles");
});

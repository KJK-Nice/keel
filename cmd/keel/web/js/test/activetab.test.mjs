import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

// The workspace structure (which tabs, in what order, which one is active,
// how each lays out its panes) changes only through lib/workspace.js, and
// renderTabs only reads it: it paints what the state says. These tests
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
const lines = (re) => src.split("\n").map((l, i) => [i + 1, l]).filter(([, l]) => re.test(l)).map(([n, l]) => `${n}: ${l.trim()}`);

test("term.js changes the workspace structure only through lib/workspace.js", () => {
  assert.deepEqual(lines(/\.(activeId|lastSessionTabId|layout|focusedPaneId|zoomedPaneId)\s*=(?!=)/), []);
  assert.deepEqual(lines(/state\.(tabs|panes)\.(set|delete|clear)\(|state\.(tabs|panes)\s*=(?!=)/), []);
});

test("renderTabs writes no workspace state and makes no tab", () => {
  const body = fns.get("renderTabs");
  assert.ok(body, "renderTabs not found");
  assert.doesNotMatch(body, /state\.\w+\s*=(?!=)/);
  assert.doesNotMatch(body, /(?<![\w.])(W\.\w+|setActive|settleTabs|createStartTab|showPane)\(/);
});

test("closing a tab settles the workspace; the model's change renders", () => {
  const body = fns.get("closeTab");
  const del = body.indexOf("W.removeTab(");
  const settle = body.indexOf("settleTabs(");
  assert.ok(del >= 0 && settle > del, "closeTab must call settleTabs after it removes the tab");
  assert.doesNotMatch(body, /renderTabs\(/);
});

test("the model's changes schedule a render, but a change of focus alone does not", () => {
  const hook = src.slice(src.indexOf("state.onChange = "), src.indexOf("function renderTabs()"));
  assert.match(hook, /what === "focus"/);
  assert.match(hook, /queueMicrotask\(/);
  assert.match(fns.get("renderTabs"), /renderQueued = false/);
});

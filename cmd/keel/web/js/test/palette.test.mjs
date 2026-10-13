import { test } from "node:test";
import assert from "node:assert/strict";
import { clampCursor, filterPalette, moveCursor, paletteHTML, paletteMatches } from "../lib/palette.js";

const items = [
  { id: "a", group: "Tabs", title: "New tab", keys: "⌘T" },
  { id: "b", group: "Tabs", title: "Close pane or tab", keys: "⌘W" },
  { id: "c", group: "Connect", title: "Connect prod-db", hint: "root@10.0.0.5" },
];

test("every query word must appear somewhere in the item", () => {
  assert.ok(paletteMatches(items[2], "  CONNECT 10.0 "));
  assert.ok(paletteMatches(items[0], "⌘t"));
  assert.ok(!paletteMatches(items[0], "new pane"));
  assert.deepEqual(filterPalette(items, "tab").map((i) => i.id), ["a", "b"]);
  assert.equal(filterPalette(items, "").length, 3);
});

test("cursor wraps and clamps", () => {
  assert.equal(moveCursor(0, -1, 3), 2);
  assert.equal(moveCursor(2, 1, 3), 0);
  assert.equal(moveCursor(1, 0, 0), 0);
  assert.equal(clampCursor(5, 2), 1);
  assert.equal(clampCursor(3, 0), 0);
});

test("paletteHTML groups items, marks the selection and escapes text", () => {
  const html = paletteHTML([...items, { id: "x", group: "Connect", title: "<b>evil</b>" }], 1);
  assert.equal((html.match(/cmd-palette-group/g) || []).length, 2);
  assert.match(html, /data-palette-index="1" id="cmd-palette-opt-1" aria-selected="true"/);
  assert.match(html, /data-palette-index="0" id="cmd-palette-opt-0" aria-selected="false"/);
  assert.ok(!html.includes("<b>evil"));
  assert.match(html, /&lt;b&gt;evil/);
  assert.match(paletteHTML([], 0), /No matching commands/);
});

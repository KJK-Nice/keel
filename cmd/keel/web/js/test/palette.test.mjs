import { test } from "node:test";
import assert from "node:assert/strict";
import {
  GROUP_ORDER,
  clampCursor,
  filterPalette,
  highlightHTML,
  keyCapsHTML,
  moveCursor,
  orderGroups,
  paletteHTML,
  paletteMatches,
} from "../lib/palette.js";

const items = [
  { id: "a", group: "Actions", title: "New tab", keys: "⌘T" },
  { id: "b", group: "Actions", title: "Close pane or tab", keys: "⌘W" },
  { id: "c", group: "Hosts", title: "New session on prod-db", hint: "root@10.0.0.5" },
];

test("every query word must appear somewhere in the item", () => {
  assert.ok(paletteMatches(items[2], "  SESSION 10.0 "));
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

test("groups print in the order Open now, Hosts, Actions, Snippets", () => {
  assert.deepEqual(GROUP_ORDER, ["Open now", "Hosts", "Actions", "Snippets"]);
  const mixed = [
    { id: "s1", group: "Snippets", title: "Type snippet: disk" },
    { id: "a1", group: "Actions", title: "New tab" },
    { id: "h1", group: "Hosts", title: "New session on web" },
    { id: "o1", group: "Open now", title: "web" },
    { id: "a2", group: "Actions", title: "Settings" },
    { id: "h2", group: "Hosts", title: "New session on db" },
    { id: "o2", group: "Open now", title: "db" },
  ];
  assert.deepEqual(orderGroups(mixed).map((i) => i.id), ["o1", "o2", "h1", "h2", "a1", "a2", "s1"]);
  assert.deepEqual(filterPalette(mixed, "").map((i) => i.group), [
    "Open now", "Open now", "Hosts", "Hosts", "Actions", "Actions", "Snippets",
  ]);
  // Filtering keeps the order and drops empty groups.
  assert.deepEqual(filterPalette(mixed, "db").map((i) => i.id), ["o2", "h2"]);
});

test("unknown groups follow the known ones in order of appearance", () => {
  const out = orderGroups([
    { id: "x", group: "Zed", title: "x" },
    { id: "s", group: "Snippets", title: "s" },
    { id: "y", group: "Alpha", title: "y" },
    { id: "o", group: "Open now", title: "o" },
  ]);
  assert.deepEqual(out.map((i) => i.id), ["o", "s", "x", "y"]);
});

test("paletteHTML prints one heading per group, marks the selection and escapes text", () => {
  const list = filterPalette([...items, { id: "x", group: "Hosts", title: "<b>evil</b>" }], "");
  const html = paletteHTML(list, 1);
  assert.equal((html.match(/k-menu-label/g) || []).length, 2);
  assert.ok(html.indexOf(">Hosts<") < html.indexOf(">Actions<"));
  assert.match(html, /data-palette-index="1" id="cmd-palette-opt-1" aria-selected="true" data-highlighted/);
  assert.match(html, /data-palette-index="0" id="cmd-palette-opt-0" aria-selected="false">/);
  assert.equal((html.match(/data-highlighted/g) || []).length, 1);
  assert.ok(!html.includes("<b>evil"));
  assert.match(html, /&lt;b&gt;evil/);
  assert.match(paletteHTML([], 0), /No matching commands/);
});

test("a snippet row shows Approve first, a tab row its state dot and shortcut", () => {
  const html = paletteHTML(
    [
      { id: "t", group: "Open now", title: "prod-db", hint: "tab 1 · 3 panes", state: "live", keys: "⌘1", icon: "session" },
      { id: "s", group: "Snippets", title: "Type snippet: disk", hint: "df -h", badge: "Approve first", icon: "snippet" },
    ],
    0,
  );
  assert.match(html, /k-dot k-dot--live/);
  assert.match(html, /<span class="k-kbd">⌘<\/span><span class="k-kbd">1<\/span>/);
  assert.match(html, /<span class="k-badge k-badge--brass">Approve first<\/span>/);
  assert.match(html, /k-palette-sub">tab 1 · 3 panes</);
});

test("highlightHTML marks every query word, merges overlaps and escapes", () => {
  assert.equal(highlightHTML("prod-db-01", "prod"), "<mark>prod</mark>-db-01");
  assert.equal(highlightHTML("prod-db-01", "PROD 01"), "<mark>prod</mark>-db-<mark>01</mark>");
  assert.equal(highlightHTML("abcd", "ab bc"), "<mark>abc</mark>d");
  assert.equal(highlightHTML("<i>x</i>", "i"), "&lt;<mark>i</mark>&gt;x&lt;/<mark>i</mark>&gt;");
  assert.equal(highlightHTML("plain", ""), "plain");
  assert.equal(highlightHTML("plain", "zzz"), "plain");
});

test("keyCapsHTML splits symbols on macOS and words elsewhere", () => {
  assert.equal(keyCapsHTML("⌘⇧O"), '<span class="k-keys"><span class="k-kbd">⌘</span><span class="k-kbd">⇧</span><span class="k-kbd">O</span></span>');
  assert.equal(keyCapsHTML("Ctrl+T"), '<span class="k-keys"><span class="k-kbd">Ctrl</span><span class="k-kbd">T</span></span>');
  assert.match(keyCapsHTML("Ctrl++"), /Ctrl/);
});

test("a host row shows its tag as a neutral badge", () => {
  const html = paletteHTML([{ id: "h", group: "Hosts", title: "New session on db", tag: "patroni", icon: "host" }], 0);
  assert.match(html, /<span class="k-badge">patroni<\/span>/);
  assert.ok(!html.includes("k-badge--brass"));
});

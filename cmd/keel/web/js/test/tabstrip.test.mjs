import { test } from "node:test";
import assert from "node:assert/strict";
import { kbdHint, tabNumberKey, tabStripHTML } from "../lib/tabstrip.js";

const tab = (id, extra = {}) => ({ id, label: `Tab ${id}`, lead: `<i data-lead="${id}"></i>`, status: "", panes: 1, page: false, sync: false, ...extra });
const opts = { activeId: "b", mod: "⌘", closeGlyph: "<svg></svg>" };
const buttons = (html) => html.split("</button>").filter((s) => s.includes("<button"));

test("⌘1…8 are the first eight tabs and ⌘9 the last", () => {
  assert.equal(tabNumberKey(0, 3), "1");
  assert.equal(tabNumberKey(7, 12), "8");
  assert.equal(tabNumberKey(8, 12), "");
  assert.equal(tabNumberKey(11, 12), "9");
  assert.equal(tabNumberKey(2, 3), "3", "the third of three is ⌘3, not ⌘9");
});

test("one button per tab, in order, with its lead, label and number", () => {
  const html = tabStripHTML([tab("a"), tab("b"), tab("c")], opts);
  const b = buttons(html);
  assert.equal(b.length, 3);
  assert.match(b[0], /data-tab="a"/);
  assert.match(b[0], /<i data-lead="a"><\/i>/);
  assert.match(b[0], /<span class="tab-label">Tab a<\/span>/);
  assert.match(b[0], new RegExp(kbdHint("1").replace(/[.*+?^${}()|[\]\\]/g, "\\$&")));
  assert.match(b[2], /data-close="c" title="Close  ⌘W"/);
});

test("only the active tab is marked and shows ⌘W on its close", () => {
  const b = buttons(tabStripHTML([tab("a"), tab("b")], opts));
  assert.doesNotMatch(b[0], /is-active/);
  assert.match(b[1], /class="tab k-tab\s+is-active\s/);
  assert.doesNotMatch(b[0], />W<\/kbd>/);
  assert.match(b[1], />W<\/kbd>/);
});

test("status, page, sync and the pane count", () => {
  const b = buttons(tabStripHTML([tab("a", { status: "is-error", sync: true, panes: 3 }), tab("p", { page: true })], opts));
  assert.match(b[0], /is-error/);
  assert.match(b[0], /is-sync/);
  assert.match(b[0], /<span class="k-badge k-badge--mono tab-count" title="3 panes">3<\/span>/);
  assert.match(b[1], /is-page/);
  assert.doesNotMatch(b[1], /tab-count/, "one pane shows no count");
});

test("a tab's name is escaped", () => {
  const html = tabStripHTML([tab("a", { label: `<img src=x onerror="alert(1)">` })], opts);
  assert.doesNotMatch(html, /<img/);
  assert.match(html, /&lt;img/);
});

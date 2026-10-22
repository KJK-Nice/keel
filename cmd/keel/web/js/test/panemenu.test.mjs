import { test } from "node:test";
import assert from "node:assert/strict";
import { createPaneMenu } from "../ui/panemenu.js";

// Just enough DOM: the hidden trigger, the menu content and its items.
function setup() {
  const acts = ["focus", "split-h", "split-v", "zoom", "share", "reconnect", "close"];
  const items = acts.map((a) => ({ dataset: { paneAct: a }, hidden: false }));
  const content = { querySelectorAll: (sel) => (sel === "[data-pane-act]" ? items : []) };
  const dispatched = [];
  const trigger = { getAttribute: () => "pane-menu", dispatchEvent: (ev) => dispatched.push(ev) };
  const doc = { getElementById: (id) => ({ "pane-menu-trigger": trigger, "pane-menu": content })[id] || null };
  const panes = new Map([
    ["p1", { id: "p1", tabId: "t1", runtimeId: "r1", kind: "term", status: "is-error" }],
    ["p2", { id: "p2", tabId: "t1", runtimeId: "r2", kind: "term" }],
  ]);
  const tabs = new Map([["t1", { id: "t1", layout: 2 }]]);
  const ran = [];
  const act = Object.fromEntries(["focus", "splitH", "splitV", "zoom", "share", "reconnect", "close"].map((k) => [k, (p) => ran.push([k, p.id])]));
  const menu = createPaneMenu({
    doc,
    pane: (id) => panes.get(id),
    tab: (id) => tabs.get(id),
    isTermPane: (p) => p.kind === "term",
    panesIn: (t) => t.layout,
    act,
  });
  const shown = () => items.filter((i) => !i.hidden).map((i) => i.dataset.paneAct);
  return { menu, shown, dispatched, ran, trigger };
}

test("prepare shows the items for that pane and returns the trigger", () => {
  const { menu, shown, trigger } = setup();
  assert.equal(menu.prepare("p1", "bar"), trigger);
  assert.deepEqual(shown(), ["split-h", "split-v", "zoom", "share", "reconnect", "close"]);
  menu.prepare("p2", "row");
  assert.deepEqual(shown(), ["focus", "split-h", "split-v", "share", "close"]);
});

test("an unknown pane opens nothing", () => {
  const { menu, dispatched } = setup();
  assert.equal(menu.prepare("nope", "bar"), null);
  menu.open("nope", "bar", {});
  assert.equal(dispatched.length, 0);
});

test("run acts on the pane the menu was opened for", () => {
  globalThis.CustomEvent ??= class { constructor(type, init) { this.type = type; this.detail = init?.detail; } };
  const { menu, ran, dispatched } = setup();
  menu.open("p2", "row", { x: 1, y: 2 });
  assert.equal(dispatched[0].type, "tui-dropdownmenu-open-at");
  assert.deepEqual(dispatched[0].detail, { x: 1, y: 2 });
  for (const a of ["focus", "split-h", "split-v", "zoom", "share", "reconnect", "close", "bogus"]) menu.run(a);
  assert.deepEqual(ran, [["focus", "p2"], ["splitH", "p2"], ["splitV", "p2"], ["zoom", "p2"], ["share", "p2"], ["reconnect", "p2"], ["close", "p2"]]);
});

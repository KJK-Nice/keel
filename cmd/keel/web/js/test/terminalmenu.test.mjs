import { test } from "node:test";
import assert from "node:assert/strict";
import { createTerminalMenu } from "../ui/terminalmenu.js";

globalThis.CustomEvent ??= class {
  constructor(type, init) {
    this.type = type;
    this.detail = init?.detail;
  }
};

// Just enough DOM and xterm for terminalmenu.js.
function setup({ selection = "", driving = true, remote = false, search = true } = {}) {
  const acts = ["copy", "paste", "select-all", "clear", "find", "split-h", "split-v", "close"];
  const items = acts.map((a) => ({ dataset: { termAct: a }, hidden: false, disabled: false }));
  const sep = { hidden: false };
  const content = {
    querySelectorAll: (sel) => (sel === "[data-term-act]" ? items : []),
    querySelector: (sel) => (sel === '[data-term-sep="split"]' ? sep : null),
  };
  const dispatched = [];
  const trigger = { getAttribute: () => "term-menu", dispatchEvent: (ev) => dispatched.push(ev) };
  const doc = { getElementById: (id) => ({ "term-menu-trigger": trigger, "term-menu": content })[id] || null };
  const termCalls = [];
  const term = {
    hasSelection: () => !!selection,
    getSelection: () => selection,
    selectAll: () => termCalls.push("selectAll"),
    clear: () => termCalls.push("clear"),
  };
  const pane = { id: "p1", tabId: "t1", term, search: search ? {} : null, remote };
  const tabs = new Map([["t1", { id: "t1", remote }]]);
  const ran = [];
  const copied = [];
  const toasts = [];
  let clock = 1000;
  const menu = createTerminalMenu({
    doc,
    now: () => clock,
    pane: (id) => (id === "p1" ? pane : undefined),
    tab: (id) => tabs.get(id),
    isTermPane: () => true,
    isDriving: () => driving,
    copyText: async (t) => (copied.push(t), true),
    toast: (m, err) => toasts.push([m, !!err]),
    act: Object.fromEntries(["paste", "find", "splitH", "splitV", "close"].map((k) => [k, (p) => ran.push([k, p.id])])),
  });
  const state = (a) => items.find((i) => i.dataset.termAct === a);
  return { menu, pane, state, sep, dispatched, ran, copied, toasts, termCalls, tick: (ms) => (clock += ms) };
}

test("open: Copy needs a selection, Paste the driver; Find and splits as the pane allows", () => {
  const s = setup({ selection: "", driving: false, search: false });
  s.menu.open(s.pane, { x: 3, y: 4 });
  assert.equal(s.dispatched[0].type, "tui-dropdownmenu-open-at");
  assert.deepEqual(s.dispatched[0].detail, { x: 3, y: 4 });
  assert.equal(s.state("copy").disabled, true);
  assert.equal(s.state("paste").disabled, true);
  assert.equal(s.state("find").hidden, true);
  assert.equal(s.state("split-h").hidden, false);
  assert.equal(s.sep.hidden, false);
});

test("a joined pane has no splits, and no separator before them", () => {
  const s = setup({ remote: true });
  s.menu.open(s.pane, {});
  assert.equal(s.state("split-h").hidden, true);
  assert.equal(s.state("split-v").hidden, true);
  assert.equal(s.sep.hidden, true);
});

test("run: Copy copies the selection; Select all and Clear stay in xterm", async () => {
  const s = setup({ selection: "ls -la" });
  s.menu.open(s.pane, {});
  s.menu.run("copy");
  await new Promise((r) => setTimeout(r, 0));
  assert.deepEqual(s.copied, ["ls -la"]);
  s.menu.run("select-all");
  s.menu.run("clear");
  assert.deepEqual(s.termCalls, ["selectAll", "clear"]);
  assert.deepEqual(s.ran, []);
});

test("run: Paste, Find, the splits and Close go to the caller with the pane", () => {
  const s = setup();
  s.menu.open(s.pane, {});
  for (const a of ["paste", "find", "split-h", "split-v", "close", "bogus"]) s.menu.run(a);
  assert.deepEqual(s.ran, [["paste", "p1"], ["find", "p1"], ["splitH", "p1"], ["splitV", "p1"], ["close", "p1"]]);
});

test("keyJustOpened is true only right after the Menu key opened it", () => {
  const s = setup();
  assert.equal(s.menu.keyJustOpened(), false);
  s.menu.open(s.pane, {}, { fromKey: true });
  assert.equal(s.menu.keyJustOpened(), true);
  s.tick(700);
  assert.equal(s.menu.keyJustOpened(), false);
  s.menu.open(s.pane, {});
  assert.equal(s.menu.keyJustOpened(), false, "a right-click open does not count as the key");
});

import { test } from "node:test";
import assert from "node:assert/strict";
import { leafPaneIds } from "../lib/layout.js";
import * as W from "../lib/workspace.js";

const tab = (id, extra = {}) => ({ id, layout: null, focusedPaneId: "", zoomedPaneId: "", ...extra });
const pane = (id) => ({ id, tabId: "" });

// A workspace with session tab t1 (panes a | b), a New tab and a tab page.
function sample() {
  const ws = W.createWorkspace();
  const t1 = W.addTab(ws, tab("t1"), { activate: false });
  W.fillTab(ws, t1, pane("a"));
  W.splitPane(ws, "a", pane("b"));
  W.addTab(ws, tab("new", { start: true }), { activate: false });
  W.addTab(ws, tab("page", { page: "settings" }), { activate: false });
  W.setActive(ws, "t1");
  return { ws, t1 };
}

test("session tabs are the ones that hold panes", () => {
  const { ws, t1 } = sample();
  assert.equal(W.isSessionTab(t1), true);
  assert.equal(W.isSessionTab(ws.tabs.get("new")), false);
  assert.equal(W.isSessionTab(ws.tabs.get("page")), false);
  assert.equal(W.isSessionTab(tab("empty")), false);
  assert.equal(W.activeSessionTab(ws), t1);
  W.setActive(ws, "page");
  assert.equal(W.activeSessionTab(ws), null);
});

test("setActive accepts only a tab that exists and keeps the session shown last", () => {
  const { ws } = sample();
  assert.equal(ws.lastSessionTabId, "t1");
  W.setActive(ws, "page");
  assert.equal(ws.activeId, "page");
  assert.equal(ws.lastSessionTabId, "t1");
  W.setActive(ws, "nope");
  assert.equal(ws.activeId, "");
  assert.equal(ws.lastSessionTabId, "t1");
});

test("a New tab that connected in place is the session shown last once you leave it", () => {
  const { ws } = sample();
  W.setActive(ws, "new");
  const t = ws.tabs.get("new");
  W.fillTab(ws, t, pane("c"));
  t.start = false;
  W.setActive(ws, "page");
  assert.equal(ws.lastSessionTabId, "new");
});

test("addTab appends and selects unless told not to", () => {
  const ws = W.createWorkspace();
  W.addTab(ws, tab("x"));
  W.addTab(ws, tab("y"), { activate: false });
  assert.deepEqual([...ws.tabs.keys()], ["x", "y"]);
  assert.equal(ws.activeId, "x");
});

test("removing the active tab selects the last one; settle reports an empty workspace", () => {
  const { ws } = sample();
  assert.equal(W.removeTab(ws, "t1"), true);
  assert.equal(ws.activeId, "page");
  assert.equal(W.removeTab(ws, "t1"), false);
  W.setActive(ws, "new");
  W.removeTab(ws, "page");
  assert.equal(ws.activeId, "new", "removing another tab keeps the active one");
  W.removeTab(ws, "new");
  assert.equal(ws.activeId, "");
  assert.equal(W.settle(ws), true);
});

test("settle selects the last tab when none was selected (restore)", () => {
  const ws = W.createWorkspace();
  W.addTab(ws, tab("x"), { activate: false });
  W.addTab(ws, tab("y"), { activate: false });
  assert.equal(W.settle(ws), false);
  assert.equal(ws.activeId, "y");
});

test("moveTab reorders in place and reports a change", () => {
  const { ws } = sample();
  const tabs = ws.tabs;
  assert.equal(W.moveTab(ws, "page", 0), true);
  assert.deepEqual([...ws.tabs.keys()], ["page", "t1", "new"]);
  assert.equal(ws.tabs, tabs, "the same Map");
  assert.equal(W.moveTab(ws, "page", 0), false, "already there");
  assert.equal(W.moveTab(ws, "page", 1), false, "a drop just after itself");
  assert.equal(W.moveTab(ws, "page", 3), true);
  assert.deepEqual([...ws.tabs.keys()], ["t1", "new", "page"]);
  assert.equal(W.moveTab(ws, "nope", 0), false);
});

test("fillTab and splitPane lay out panes", () => {
  const { ws, t1 } = sample();
  assert.deepEqual(leafPaneIds(t1.layout), ["a", "b"]);
  assert.equal(t1.focusedPaneId, "b", "a split focuses the new pane");
  assert.equal(ws.panes.get("b").tabId, "t1");
  t1.zoomedPaneId = "a";
  assert.equal(W.splitPane(ws, "a", pane("c"), { dir: "v", focus: "target" }), true);
  assert.equal(t1.layout.a.dir, "v");
  assert.deepEqual(leafPaneIds(t1.layout), ["a", "c", "b"]);
  assert.equal(t1.focusedPaneId, "a");
  assert.equal(t1.zoomedPaneId, "", "a split leaves focus view");
  assert.equal(W.splitPane(ws, "nope", pane("d")), false);
  assert.equal(ws.panes.has("d"), false);
});

test("tabPanes, visiblePanes and focusedPane", () => {
  const { ws, t1 } = sample();
  assert.deepEqual(W.tabPanes(ws, t1).map((p) => p.id), ["a", "b"]);
  assert.deepEqual(W.visiblePanes(ws, t1).map((p) => p.id), ["a", "b"]);
  assert.equal(W.focusedPane(ws).id, "b");
  W.zoom(ws, t1, "a");
  assert.deepEqual(W.visiblePanes(ws, t1).map((p) => p.id), ["a"]);
  assert.equal(W.focusedPane(ws).id, "a");
  t1.focusedPaneId = "gone";
  assert.equal(W.focusedPane(ws).id, "a", "falls back to the first pane");
  W.setActive(ws, "page");
  assert.equal(W.focusedPane(ws), null);
  assert.deepEqual(W.tabPanes(ws, null), []);
});

test("detachPane fixes focus and zoom, and leaves the last pane to the caller", () => {
  const { ws, t1 } = sample();
  W.zoom(ws, t1, "b");
  assert.equal(W.detachPane(ws, "b"), t1);
  assert.deepEqual(leafPaneIds(t1.layout), ["a"]);
  assert.equal(t1.focusedPaneId, "a");
  assert.equal(t1.zoomedPaneId, "");
  assert.equal(ws.panes.has("b"), true, "the record stays");
  assert.equal(W.detachPane(ws, "a"), null);
  assert.deepEqual(leafPaneIds(t1.layout), ["a"], "the last pane is not taken out");
  W.splitPane(ws, "a", pane("c"));
  W.zoom(ws, t1, "c");
  W.detachPane(ws, "a", { unzoom: true });
  assert.equal(t1.zoomedPaneId, "");
  assert.equal(t1.focusedPaneId, "c", "focus stays on a pane still there");
});

test("movePane moves or swaps within a tab", () => {
  const { ws, t1 } = sample();
  assert.equal(W.movePane(ws, "a", "b", "center"), true);
  assert.deepEqual(leafPaneIds(t1.layout), ["b", "a"]);
  assert.equal(t1.focusedPaneId, "a");
  assert.equal(W.movePane(ws, "b", "a", "bottom"), true);
  assert.deepEqual(leafPaneIds(t1.layout), ["a", "b"]);
  assert.equal(t1.layout.dir, "v");
  assert.equal(W.movePane(ws, "a", "a", "left"), false);
  assert.equal(W.movePane(ws, "a", "b", ""), false);
});

test("dockTab moves a tab's panes beside a pane of another tab", () => {
  const { ws, t1 } = sample();
  const t2 = W.addTab(ws, tab("t2"));
  W.fillTab(ws, t2, pane("x"));
  assert.equal(W.dockTab(ws, "t2", "a", "center"), true);
  assert.deepEqual(leafPaneIds(t1.layout), ["a", "x", "b"]);
  assert.equal(ws.panes.get("x").tabId, "t1");
  assert.equal(t1.focusedPaneId, "x");
  assert.equal(ws.tabs.has("t2"), true, "the caller closes the source");
  assert.equal(W.dockTab(ws, "new", "a", "left"), false, "a New tab holds nothing to dock");
  assert.equal(W.dockTab(ws, "t1", "a", "left"), false, "not into itself");
});

test("focusPane and zoom", () => {
  const { ws, t1 } = sample();
  W.setActive(ws, "page");
  assert.equal(W.focusPane(ws, "a", { activate: false }), true);
  assert.equal(t1.focusedPaneId, "a");
  assert.equal(ws.activeId, "page");
  W.focusPane(ws, "b");
  assert.equal(ws.activeId, "t1");
  assert.equal(W.focusPane(ws, "nope"), false);
  assert.equal(W.zoom(ws, t1, "a"), true);
  assert.equal(t1.focusedPaneId, "a");
  assert.equal(W.zoom(ws, t1, ""), true);
  assert.equal(t1.zoomedPaneId, "");
  W.detachPane(ws, "b");
  assert.equal(W.zoom(ws, t1, "a"), false, "one pane does not zoom");
});

test("setLayout takes a restored layout with focus and zoom only when they are in it", () => {
  const ws = W.createWorkspace();
  const t = W.addTab(ws, tab("t"));
  W.fillTab(ws, t, pane("a"));
  W.addPane(ws, t, pane("b"));
  const layout = { type: "split", dir: "h", sizes: [50, 50], a: { type: "leaf", paneId: "a" }, b: { type: "leaf", paneId: "b" } };
  W.setLayout(ws, t, layout, { focusedPaneId: "b", zoomedPaneId: "b" });
  assert.equal(t.layout, layout);
  assert.equal(t.focusedPaneId, "b");
  assert.equal(t.zoomedPaneId, "b");
  const solo = tab("s");
  W.addTab(ws, solo);
  W.fillTab(ws, solo, pane("c"));
  W.setLayout(ws, solo, null, { focusedPaneId: "zz", zoomedPaneId: "c" });
  assert.equal(solo.focusedPaneId, "c");
  assert.equal(solo.zoomedPaneId, "", "one pane does not zoom");
});

test("onChange tells each change once, and nothing for a no-op", () => {
  const { ws, t1 } = sample();
  const seen = [];
  ws.onChange = (what, tab) => seen.push(tab ? `${what}:${tab.id}` : what);
  W.setActive(ws, "t1");
  W.focusPane(ws, "b");
  W.zoom(ws, t1, "");
  W.moveTab(ws, "page", 3);
  assert.deepEqual(seen, [], "nothing changed");
  W.setActive(ws, "page");
  W.focusPane(ws, "a", { activate: false });
  W.zoom(ws, t1, "a");
  W.moveTab(ws, "page", 0);
  W.addTab(ws, tab("x"), { activate: false });
  W.removeTab(ws, "x");
  assert.deepEqual(seen, ["active", "focus:t1", "layout:t1", "tabs", "tabs:x", "tabs"]);
});

test("a layout change marks the tab dirty for a repaint", () => {
  const { ws, t1 } = sample();
  t1.dirty = false;
  W.focusPane(ws, "a");
  assert.equal(t1.dirty, false, "focus alone repaints nothing");
  W.detachPane(ws, "b");
  assert.equal(t1.dirty, true);
  t1.dirty = false;
  W.splitPane(ws, "a", pane("c"));
  assert.equal(t1.dirty, true);
});

test("updatePane sets record fields, tells onChange once, and refuses other fields", () => {
  const { ws, t1 } = sample();
  const seen = [];
  ws.onChange = (what, tab) => seen.push(`${what}:${tab?.id}`);
  const a = ws.panes.get("a");
  assert.equal(W.updatePane(ws, a, { status: "is-ready", failKind: "" }), true);
  assert.equal(a.status, "is-ready");
  assert.equal(W.updatePane(ws, a, { status: "is-ready" }), false, "no change");
  assert.deepEqual(seen, ["pane:t1"]);
  assert.throws(() => W.updatePane(ws, a, { wrap: {} }), /not a pane record field/);
  assert.equal(t1.dirty, true, "set by the split in sample, not by updatePane");
});

test("logPane drops blanks and repeats and keeps the last 40 lines", () => {
  const { ws } = sample();
  const a = ws.panes.get("a");
  W.logPane(ws, a, "user", "  Starting   a local shell ");
  W.logPane(ws, a, "work", "Starting a local shell");
  W.logPane(ws, a, "fail", "   ");
  assert.deepEqual(a.failLog, [{ kind: "user", text: "Starting a local shell" }]);
  const before = a.failLog;
  for (let i = 0; i < 45; i++) W.logPane(ws, a, "", `line ${i}`);
  assert.equal(a.failLog.length, 40);
  assert.deepEqual(a.failLog.at(-1), { kind: "work", text: "line 44" });
  assert.equal(before.length, 1, "a new array each time, the old one untouched");
});

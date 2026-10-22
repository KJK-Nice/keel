import { test } from "node:test";
import assert from "node:assert/strict";
import { activeKey, parseWorkspace, savedActive, savedLeaf, workspaceSnapshot, WORKSPACE_KEY } from "../lib/savedworkspace.js";

const leaf = (paneId) => ({ type: "leaf", paneId });
const term = (id, runtimeId) => ({ id, kind: "term", runtimeId });
const notebook = (id, notebookId) => ({ id, kind: "notebook", notebookId });

test("the key is the one older builds wrote", () => {
  assert.equal(WORKSPACE_KEY, "keel-workspace");
});

test("a pane is saved by its runtime id or its notebook id", () => {
  assert.deepEqual(savedLeaf(term("p1", "r1")), { type: "leaf", runtimeId: "r1" });
  assert.deepEqual(savedLeaf(notebook("p2", "n1")), { type: "leaf", kind: "notebook", notebookId: "n1" });
  assert.equal(savedLeaf(term("p3", "")), null, "a pane with no session is dropped");
  assert.equal(savedLeaf(undefined), null);
});

test("the snapshot keeps split tabs and pages, and drops New, joined and empty tabs", () => {
  const panes = new Map([
    ["p1", term("p1", "r1")],
    ["p2", notebook("p2", "n1")],
    ["p3", term("p3", "")],
  ]);
  const tabs = [
    { id: "t1", customTitle: "Ops", sync: true, focusedPaneId: "p2", zoomedPaneId: "p1", layout: { type: "split", dir: "h", sizes: [40, 60], a: leaf("p1"), b: leaf("p2") } },
    { id: "page:settings", page: "settings" },
    { id: "t2", start: true, layout: leaf("p1") },
    { id: "t3", remote: true, layout: leaf("p1") },
    { id: "t4", layout: leaf("p3") },
  ];
  const snap = workspaceSnapshot({ tabs, panes, active: "r1" });
  assert.equal(snap.v, 1);
  assert.equal(snap.active, "r1");
  assert.deepEqual(snap.pages, ["settings"]);
  assert.equal(snap.tabs.length, 1);
  assert.deepEqual(snap.tabs[0], {
    title: "Ops",
    sync: true,
    focusedRuntimeId: "",
    zoomedRuntimeId: "r1",
    focusedNotebookId: "n1",
    zoomedNotebookId: "",
    layout: { type: "split", dir: "h", sizes: [40, 60], a: { type: "leaf", runtimeId: "r1" }, b: { type: "leaf", kind: "notebook", notebookId: "n1" } },
  });
});

test("the shown tab is saved as a page id, a runtime id or a notebook", () => {
  assert.equal(activeKey({ tab: { id: "page:agent", page: "agent" } }), "page:agent");
  assert.equal(activeKey({ tab: { id: "t" }, pane: notebook("p", "n1"), term: term("q", "r9") }), "r9", "a notebook following a terminal saves the terminal");
  assert.equal(activeKey({ tab: { id: "t" }, pane: notebook("p", "n1"), term: null }), "nb:n1");
  assert.equal(activeKey({ tab: { id: "t", start: true } }), "");
  assert.equal(activeKey({}), "");
});

test("a saved active key reads back as what it named", () => {
  assert.deepEqual(savedActive("page:agent", "page:"), { pageId: "page:agent" });
  assert.deepEqual(savedActive("nb:n1", "page:"), { notebookId: "n1" });
  assert.deepEqual(savedActive("r1", "page:"), { runtimeId: "r1" });
  assert.equal(savedActive("", "page:"), null);
  assert.equal(savedActive(42, "page:"), null);
});

test("only a version 1 workspace with a tab list is read back", () => {
  assert.deepEqual(parseWorkspace('{"v":1,"tabs":[],"pages":[]}'), { v: 1, tabs: [], pages: [] });
  assert.equal(parseWorkspace(null), null);
  assert.equal(parseWorkspace("{"), null);
  assert.equal(parseWorkspace('{"v":2,"tabs":[]}'), null);
  assert.equal(parseWorkspace('{"v":1}'), null);
});

test("a snapshot round-trips through JSON", () => {
  const panes = new Map([["p1", term("p1", "r1")]]);
  const snap = workspaceSnapshot({ tabs: [{ id: "t", focusedPaneId: "p1", layout: leaf("p1") }], panes, active: "r1" });
  assert.deepEqual(parseWorkspace(JSON.stringify(snap)), snap);
});

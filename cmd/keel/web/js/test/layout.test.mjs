import { test } from "node:test";
import assert from "node:assert/strict";
import {
  computeRects,
  dragSizes,
  insertBeside,
  layoutNotebookIds,
  layoutRuntimeIds,
  leafCount,
  leafPaneIds,
  pruneLayout,
  remapLayout,
  removeLeaf,
  replaceLeaf,
  serializeLayout,
  swapLeaves,
} from "../lib/layout.js";

const leaf = (paneId) => ({ type: "leaf", paneId });
const hsplit = (a, b, sizes = [50, 50]) => ({ type: "split", dir: "h", sizes, a, b });
const vsplit = (a, b, sizes = [50, 50]) => ({ type: "split", dir: "v", sizes, a, b });

// p1 | (p2 over p3)
const tree = () => hsplit(leaf("p1"), vsplit(leaf("p2"), leaf("p3")));

test("leaf walks, counts and edits", () => {
  const t = tree();
  assert.deepEqual(leafPaneIds(t), ["p1", "p2", "p3"]);
  assert.equal(leafCount(t), 3);
  assert.equal(leafCount(null), 0);
  assert.deepEqual(leafPaneIds(swapLeaves(t, "p1", "p3")), ["p3", "p2", "p1"]);
  assert.deepEqual(leafPaneIds(replaceLeaf(t, "p2", leaf("x"))), ["p1", "x", "p3"]);
});

test("removeLeaf collapses the parent split into the sibling", () => {
  const t = removeLeaf(tree(), "p2");
  assert.deepEqual(t, hsplit(leaf("p1"), leaf("p3")));
  assert.deepEqual(removeLeaf(removeLeaf(t, "p1"), "p3"), null);
});

test("insertBeside splits the target on the given edge", () => {
  const t = insertBeside(leaf("a"), "a", leaf("b"), "top");
  assert.deepEqual(t, vsplit(leaf("b"), leaf("a")));
  assert.deepEqual(insertBeside(leaf("a"), "a", leaf("b"), "right"), hsplit(leaf("a"), leaf("b")));
  assert.equal(insertBeside(t, "a", leaf("c"), "center"), t);
});

test("save, prune and restore a workspace layout", () => {
  const panes = { p1: "rt1", p2: "nb:n1", p3: "rt3" };
  const saved = serializeLayout(tree(), (id) =>
    panes[id].startsWith("nb:")
      ? { type: "leaf", kind: "notebook", notebookId: panes[id].slice(3) }
      : { type: "leaf", runtimeId: panes[id] },
  );
  assert.deepEqual(layoutRuntimeIds(saved), ["rt1", "rt3"]);
  assert.deepEqual(layoutNotebookIds(saved), ["n1"]);

  const pruned = pruneLayout(saved, new Set(["rt1", "rt3"]), new Set());
  assert.deepEqual(layoutNotebookIds(pruned), []);
  const live = remapLayout(pruned, new Map([["rt1", "q1"], ["rt3", "q3"]]), new Map());
  assert.deepEqual(live, hsplit(leaf("q1"), leaf("q3")));
});

test("serializeLayout drops panes it can't describe and cleans sizes", () => {
  const t = hsplit(leaf("keep"), leaf("drop"), ["x", 30]);
  assert.deepEqual(serializeLayout(t, (id) => (id === "keep" ? { type: "leaf", runtimeId: "r" } : null)), {
    type: "leaf",
    runtimeId: "r",
  });
  const both = serializeLayout(t, (id) => ({ type: "leaf", runtimeId: id }));
  assert.deepEqual(both.sizes, [50, 30]);
});

test("computeRects tiles the box with sashes between panes", () => {
  const t = tree();
  const { panes, splits } = computeRects(t, 206, 106, 6);
  assert.deepEqual(panes.get("p1"), { x: 0, y: 0, w: 100, h: 106 });
  assert.deepEqual(panes.get("p2"), { x: 106, y: 0, w: 100, h: 50 });
  assert.deepEqual(panes.get("p3"), { x: 106, y: 56, w: 100, h: 50 });
  assert.deepEqual(splits.get(t).sash, { x: 100, y: 0, w: 6, h: 106 });
  assert.deepEqual(splits.get(t.b).box, { x: 106, y: 0, w: 100, h: 106 });
  assert.deepEqual(splits.get(t.b).sash, { x: 106, y: 50, w: 100, h: 6 });
});

test("computeRects insets the layout by pad on every edge", () => {
  const t = hsplit(leaf("a"), leaf("b"), [1, 1]);
  const { panes, splits } = computeRects(t, 218, 112, 6, 6);
  assert.deepEqual(panes.get("a"), { x: 6, y: 6, w: 100, h: 100 });
  assert.deepEqual(panes.get("b"), { x: 112, y: 6, w: 100, h: 100 });
  assert.deepEqual(splits.get(t).sash, { x: 106, y: 6, w: 6, h: 100 });
  assert.deepEqual(splits.get(t).box, { x: 6, y: 6, w: 206, h: 100 });
});

test("computeRects honours uneven sizes", () => {
  const { panes } = computeRects(hsplit(leaf("a"), leaf("b"), [3, 1]), 406, 10, 6);
  assert.equal(panes.get("a").w, 300);
  assert.equal(panes.get("b").w, 100);
});

test("dragSizes clamps to 20-80% and refuses tiny boxes", () => {
  const box = { x: 0, y: 0, w: 200, h: 100 };
  assert.deepEqual(dragSizes("h", box, 0, 100), [50, 50]);
  assert.deepEqual(dragSizes("h", box, 0, 10), [20, 80]);
  assert.deepEqual(dragSizes("v", box, 0, 95), [80, 20]);
  assert.equal(dragSizes("h", { ...box, w: 4 }, 0, 2), null);
});

// Split-pane layout trees. Pure functions: no DOM, no state.
//
// A live layout is a binary tree of
//   { type: "leaf", paneId }
//   { type: "split", dir: "h" | "v", sizes: [a, b], a, b }
// "h" puts a left of b, "v" puts a above b; sizes are relative weights.
// A saved layout (localStorage) has the same shape with leaves
//   { type: "leaf", runtimeId } or { type: "leaf", kind: "notebook", notebookId }.

const cleanSizes = (sizes) =>
  Array.isArray(sizes) && sizes.length === 2 ? [Number(sizes[0]) || 50, Number(sizes[1]) || 50] : [50, 50];

const split = (node, a, b) => ({ type: "split", dir: node.dir === "v" ? "v" : "h", sizes: cleanSizes(node.sizes), a, b });

// mapLeaves rebuilds a tree with fn(leaf) for each leaf; a leaf that maps
// to null is dropped and its sibling takes the parent's place.
export function mapLeaves(node, fn) {
  if (!node) return null;
  if (node.type === "leaf") return fn(node) || null;
  const a = mapLeaves(node.a, fn);
  const b = mapLeaves(node.b, fn);
  if (!a) return b;
  if (!b) return a;
  return split(node, a, b);
}

// walkLeaves calls fn(leaf) for every leaf, left to right.
export function walkLeaves(node, fn) {
  if (!node) return;
  if (node.type === "leaf") {
    fn(node);
    return;
  }
  walkLeaves(node.a, fn);
  walkLeaves(node.b, fn);
}

export function leafPaneIds(node) {
  const out = [];
  walkLeaves(node, (leaf) => out.push(leaf.paneId));
  return out;
}

export function leafCount(node) {
  let n = 0;
  walkLeaves(node, () => n++);
  return n;
}

export function replaceLeaf(node, paneId, next) {
  if (!node) return node;
  if (node.type === "leaf") return node.paneId === paneId ? next : node;
  return { ...node, a: replaceLeaf(node.a, paneId, next), b: replaceLeaf(node.b, paneId, next) };
}

export function removeLeaf(node, paneId) {
  if (!node) return null;
  if (node.type === "leaf") return node.paneId === paneId ? null : node;
  const a = removeLeaf(node.a, paneId);
  const b = removeLeaf(node.b, paneId);
  if (!a) return b;
  if (!b) return a;
  return { ...node, a, b };
}

export function swapLeaves(node, aId, bId) {
  if (!node || aId === bId) return node;
  if (node.type === "leaf") {
    if (node.paneId === aId) return { ...node, paneId: bId };
    if (node.paneId === bId) return { ...node, paneId: aId };
    return node;
  }
  return { ...node, a: swapLeaves(node.a, aId, bId), b: swapLeaves(node.b, aId, bId) };
}

// insertBeside splits target's leaf, putting incoming on edge ("left",
// "right", "top", "bottom"); "center" or no edge leaves the layout alone.
export function insertBeside(layout, targetId, incoming, edge) {
  if (!layout || !incoming || !edge || edge === "center") return layout;
  const dir = edge === "left" || edge === "right" ? "h" : "v";
  const incomingFirst = edge === "left" || edge === "top";
  const targetLeaf = { type: "leaf", paneId: targetId };
  return replaceLeaf(layout, targetId, {
    type: "split",
    dir,
    sizes: [50, 50],
    a: incomingFirst ? incoming : targetLeaf,
    b: incomingFirst ? targetLeaf : incoming,
  });
}

// --- saved layouts ---------------------------------------------------------

// serializeLayout turns a live layout into a saved one; describe(paneId)
// returns the saved leaf for a pane, or null to drop it.
export function serializeLayout(node, describe) {
  return mapLeaves(node, (leaf) => describe(leaf.paneId));
}

export function layoutRuntimeIds(node) {
  const out = [];
  walkLeaves(node, (leaf) => {
    if (leaf.kind !== "notebook" && leaf.runtimeId && !out.includes(leaf.runtimeId)) out.push(leaf.runtimeId);
  });
  return out;
}

export function layoutNotebookIds(node) {
  const out = [];
  walkLeaves(node, (leaf) => {
    if (leaf.kind === "notebook" && leaf.notebookId && !out.includes(leaf.notebookId)) out.push(leaf.notebookId);
  });
  return out;
}

// pruneLayout drops saved leaves whose session (live: Set of runtime ids)
// or notebook (notebooks: Set of ids) no longer exists.
export function pruneLayout(node, live, notebooks) {
  return mapLeaves(node, (leaf) => {
    if (leaf.kind === "notebook") return notebooks?.has(leaf.notebookId) ? leaf : null;
    return live.has(leaf.runtimeId) ? leaf : null;
  });
}

// remapLayout turns a saved layout back into a live one, given the panes
// made for each runtime id and notebook id.
export function remapLayout(node, runtimeToPane, notebookToPane) {
  return mapLeaves(node, (leaf) => {
    const paneId = leaf.kind === "notebook" ? notebookToPane?.get(leaf.notebookId) : runtimeToPane.get(leaf.runtimeId);
    return paneId ? { type: "leaf", paneId } : null;
  });
}

// --- geometry --------------------------------------------------------------

// computeRects lays the tree out in a w×h box with sash-pixel dividers.
// panes maps paneId → {x, y, w, h}; splits maps each split node (by
// identity) → { box, sash } where box is the split's whole area and sash
// is its divider.
export function computeRects(layout, w, h, sash) {
  const panes = new Map();
  const splits = new Map();
  const walk = (node, x, y, rw, rh) => {
    if (!node) return;
    if (node.type === "leaf") {
      panes.set(node.paneId, { x, y, w: rw, h: rh });
      return;
    }
    const total = node.sizes[0] + node.sizes[1] || 1;
    const aFrac = node.sizes[0] / total;
    if (node.dir === "h") {
      const aW = Math.max(0, Math.floor((rw - sash) * aFrac));
      splits.set(node, { box: { x, y, w: rw, h: rh }, sash: { x: x + aW, y, w: sash, h: rh } });
      walk(node.a, x, y, aW, rh);
      walk(node.b, x + aW + sash, y, Math.max(0, rw - sash - aW), rh);
    } else {
      const aH = Math.max(0, Math.floor((rh - sash) * aFrac));
      splits.set(node, { box: { x, y, w: rw, h: rh }, sash: { x, y: y + aH, w: rw, h: sash } });
      walk(node.a, x, y, rw, aH);
      walk(node.b, x, y + aH + sash, rw, Math.max(0, rh - sash - aH));
    }
  };
  walk(layout, 0, 0, w, h);
  return { panes, splits };
}

// dragSizes is a split's new sizes when its sash is dragged to pos (a
// client coordinate along the split's axis); origin is where the split's
// box starts on that axis. Clamped to 20–80%. Null when the box is too
// small to drag.
export function dragSizes(dir, box, origin, pos) {
  const total = dir === "h" ? box.w : box.h;
  if (total < 8) return null;
  const pct = Math.min(80, Math.max(20, ((pos - origin) / total) * 100));
  return [pct, 100 - pct];
}

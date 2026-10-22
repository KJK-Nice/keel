// The workspace model: which tabs are open and in what order, which one is
// active, and how each tab lays out its panes. No DOM, no I/O: term.js owns
// the elements and the connections, and changes the structure only through
// these functions, so the rules for it live in one place.
//
// ws is { tabs: Map, panes: Map, activeId, lastSessionTabId, onChange? }
// (term.js passes its state). The model reads and writes only these fields
// of the records; everything else on them is the caller's:
//   tab   { id, layout, focusedPaneId, zoomedPaneId, dirty, start?, page? }
//   pane  { id, tabId }
// A layout is a lib/layout.js tree of pane ids.
//
// Every change that shows calls ws.onChange(what, tab), so the caller can
// render once for a burst of them. what is
//   "tabs"    a tab was added, removed or moved
//   "active"  another tab is active
//   "layout"  a tab's panes or focus view changed; the tab is marked dirty
//             (its panes need laying out again)
//   "focus"   only which pane of a tab has focus
// A call that changes nothing calls nothing.
import { insertBeside, leafCount, removeLeaf, replaceLeaf, swapLeaves, walkLeaves } from "./layout.js";

const leaf = (paneId) => ({ type: "leaf", paneId });

function changed(ws, what, tab) {
  if (what === "layout" && tab) tab.dirty = true;
  ws.onChange?.(what, tab);
}

export function createWorkspace() {
  return { tabs: new Map(), panes: new Map(), activeId: "", lastSessionTabId: "" };
}

// A session tab holds panes (terminals or notebooks); New tabs and tab
// pages do not.
export function isSessionTab(tab) {
  return !!tab && !tab.start && !tab.page && !!tab.layout;
}

export function activeTab(ws) {
  return ws.tabs.get(ws.activeId) || null;
}

export function activeSessionTab(ws) {
  const tab = activeTab(ws);
  return isSessionTab(tab) ? tab : null;
}

// tabPanes is a tab's panes in layout order.
export function tabPanes(ws, tab) {
  const out = [];
  walkLeaves(tab?.layout, ({ paneId }) => {
    const p = ws.panes.get(paneId);
    if (p) out.push(p);
  });
  return out;
}

// visiblePanes is what a tab shows: its zoomed pane alone, or all of them.
export function visiblePanes(ws, tab) {
  if (!tab) return [];
  if (tab.zoomedPaneId && ws.panes.has(tab.zoomedPaneId)) return [ws.panes.get(tab.zoomedPaneId)];
  return tabPanes(ws, tab);
}

// focusedPane is the active tab's focused pane, or its first.
export function focusedPane(ws) {
  const tab = activeTab(ws);
  if (!tab) return null;
  return ws.panes.get(tab.focusedPaneId) || tabPanes(ws, tab)[0] || null;
}

// ---- Tabs ----------------------------------------------------------------

// setActive makes tab id the active one, or none when there is no such
// tab. It also keeps lastSessionTabId, the tab that tab pages and snippets
// mean by "the session shown last": the session tab it selects, or the one
// it leaves (a New tab that connected in place is a session tab only by
// then).
export function setActive(ws, id) {
  const left = ws.tabs.get(ws.activeId);
  if (isSessionTab(left)) ws.lastSessionTabId = left.id;
  const next = ws.tabs.has(id) ? id : "";
  const moved = next !== ws.activeId;
  ws.activeId = next;
  if (isSessionTab(ws.tabs.get(ws.activeId))) ws.lastSessionTabId = ws.activeId;
  if (moved) changed(ws, "active");
}

// addTab puts a tab at the end of the strip and, unless activate is false,
// selects it.
export function addTab(ws, tab, { activate = true } = {}) {
  ws.tabs.set(tab.id, tab);
  changed(ws, "tabs", tab);
  if (activate) setActive(ws, tab.id);
  return tab;
}

// removeTab drops a tab, not its panes: the caller disposes or rehomes
// those first. When it was the active tab, the last one becomes active.
export function removeTab(ws, id) {
  if (!ws.tabs.delete(id)) return false;
  changed(ws, "tabs");
  settle(ws);
  return true;
}

// settle makes the active tab one that exists: the last tab when the
// active one is gone or none was selected (restore). It reports whether
// the workspace is empty, so the caller can open a New tab.
export function settle(ws) {
  if (!ws.tabs.has(ws.activeId)) setActive(ws, [...ws.tabs.keys()].at(-1) || "");
  return ws.tabs.size === 0;
}

// moveTab moves a tab to strip index toIndex, counted before the move (a
// drop between two tabs). It reports whether the order changed.
export function moveTab(ws, id, toIndex) {
  const ids = [...ws.tabs.keys()];
  const from = ids.indexOf(id);
  if (from < 0 || toIndex === from || toIndex === from + 1) return false;
  const next = ids.slice();
  next.splice(from, 1);
  next.splice(Math.max(0, Math.min(toIndex > from ? toIndex - 1 : toIndex, next.length)), 0, id);
  if (next.every((x, i) => x === ids[i])) return false;
  const tabs = next.map((x) => ws.tabs.get(x));
  ws.tabs.clear();
  for (const tab of tabs) ws.tabs.set(tab.id, tab);
  changed(ws, "tabs");
  return true;
}

// ---- Panes ---------------------------------------------------------------

// addPane makes pane one of tab's panes. It is not in the layout yet:
// fillTab, splitPane or setLayout place it, and tell onChange.
export function addPane(ws, tab, pane) {
  pane.tabId = tab.id;
  ws.panes.set(pane.id, pane);
  return pane;
}

// removePane forgets a pane. Take it out of its layout first (detachPane,
// which tells onChange), unless its whole tab is going.
export function removePane(ws, id) {
  return ws.panes.delete(id);
}

// fillTab lays a tab out as the one pane given and focuses it: a New tab
// that a session or a notebook takes over.
export function fillTab(ws, tab, pane) {
  addPane(ws, tab, pane);
  tab.layout = leaf(pane.id);
  tab.focusedPaneId = pane.id;
  tab.zoomedPaneId = "";
  changed(ws, "layout", tab);
}

// splitPane puts pane beside the pane targetId: dir "h" to its right, "v"
// below it. The tab leaves focus view; focus goes to the new pane, or stays
// on the target with focus "target".
export function splitPane(ws, targetId, pane, { dir = "h", focus = "new" } = {}) {
  const target = ws.panes.get(targetId);
  const tab = target && ws.tabs.get(target.tabId);
  if (!tab?.layout) return false;
  addPane(ws, tab, pane);
  tab.layout = replaceLeaf(tab.layout, targetId, { type: "split", dir: dir === "v" ? "v" : "h", sizes: [50, 50], a: leaf(targetId), b: leaf(pane.id) });
  tab.zoomedPaneId = "";
  tab.focusedPaneId = focus === "target" ? targetId : pane.id;
  changed(ws, "layout", tab);
  return true;
}

// detachPane takes a pane out of its tab's layout; the pane record stays
// (removePane, or a new tab, is the caller's). The tab leaves focus view
// when that pane was zoomed, or always with unzoom, and focuses its first
// pane when that pane had focus. Returns the tab, or null and changes
// nothing when it is the tab's last pane: the caller closes the tab.
export function detachPane(ws, paneId, { unzoom = false } = {}) {
  const pane = ws.panes.get(paneId);
  const tab = pane && ws.tabs.get(pane.tabId);
  const next = tab && removeLeaf(tab.layout, paneId);
  if (!next) return null;
  tab.layout = next;
  if (unzoom || tab.zoomedPaneId === paneId) tab.zoomedPaneId = "";
  if (tab.focusedPaneId === paneId) tab.focusedPaneId = tabPanes(ws, tab)[0]?.id || "";
  changed(ws, "layout", tab);
  return tab;
}

// movePane moves a pane next to another of the same tab (edge "left",
// "right", "top" or "bottom"), or swaps the two (edge "center"). The tab
// leaves focus view and the moved pane has focus.
export function movePane(ws, fromId, toId, edge) {
  const from = ws.panes.get(fromId);
  const to = ws.panes.get(toId);
  if (!from || !to || fromId === toId || from.tabId !== to.tabId || !edge) return false;
  const tab = ws.tabs.get(from.tabId);
  if (!tab?.layout) return false;
  if (edge === "center") {
    tab.layout = swapLeaves(tab.layout, fromId, toId);
  } else {
    const rest = removeLeaf(tab.layout, fromId);
    if (!rest) return false;
    tab.layout = insertBeside(rest, toId, leaf(fromId), edge);
  }
  tab.zoomedPaneId = "";
  tab.focusedPaneId = fromId;
  changed(ws, "layout", tab);
  return true;
}

// dockTab moves every pane of tab srcId next to the pane targetId of
// another tab ("center" docks to the right) and keeps the focus the
// source had. The source tab is left empty in place: the caller closes it.
export function dockTab(ws, srcId, targetId, edge) {
  const src = ws.tabs.get(srcId);
  const target = ws.panes.get(targetId);
  const dest = target && ws.tabs.get(target.tabId);
  if (!src || !dest || src.id === dest.id || src.start || dest.start || !src.layout || !dest.layout) return false;
  for (const p of tabPanes(ws, src)) p.tabId = dest.id;
  dest.layout = insertBeside(dest.layout, targetId, src.layout, edge === "center" || !edge ? "right" : edge);
  dest.zoomedPaneId = "";
  src.zoomedPaneId = "";
  dest.focusedPaneId = src.focusedPaneId || targetId;
  changed(ws, "layout", dest);
  return true;
}

// focusPane makes a pane the focused one of its tab and, unless activate is
// false, its tab the active one.
export function focusPane(ws, paneId, { activate = true } = {}) {
  const pane = ws.panes.get(paneId);
  const tab = pane && ws.tabs.get(pane.tabId);
  if (!tab) return false;
  if (tab.focusedPaneId !== pane.id) {
    tab.focusedPaneId = pane.id;
    changed(ws, "focus", tab);
  }
  if (activate) setActive(ws, tab.id);
  return true;
}

// zoom shows one pane of a split tab alone (focus view), or the whole
// tab again with paneId "". A tab of one pane does not zoom. The zoomed
// pane has focus.
export function zoom(ws, tab, paneId) {
  if (!tab) return false;
  if (paneId && (!ws.panes.has(paneId) || leafCount(tab.layout) < 2)) return false;
  if (tab.zoomedPaneId === (paneId || "")) return true;
  tab.zoomedPaneId = paneId || "";
  if (paneId) tab.focusedPaneId = paneId;
  changed(ws, "layout", tab);
  return true;
}

// setLayout gives a tab a layout restored from disk, with the pane that had
// focus and the one that was zoomed when they are in it.
export function setLayout(ws, tab, layout, { focusedPaneId = "", zoomedPaneId = "" } = {}) {
  if (layout) tab.layout = layout;
  const ids = new Set();
  walkLeaves(tab.layout, ({ paneId }) => ids.add(paneId));
  if (ids.has(focusedPaneId)) tab.focusedPaneId = focusedPaneId;
  if (ids.has(zoomedPaneId) && ids.size > 1) tab.zoomedPaneId = zoomedPaneId;
  changed(ws, "layout", tab);
}

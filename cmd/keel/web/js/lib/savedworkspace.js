// The saved workspace (localStorage "keel-workspace"): the session tabs and
// how each was split, the tab pages, and which tab was shown. No DOM and no
// I/O: term.js hands over its tabs and panes, stores the result, and
// restores from what parseWorkspace returns.
import { serializeLayout } from "./layout.js";

export const WORKSPACE_KEY = "keel-workspace";
const NOTEBOOK_PREFIX = "nb:";

const isNotebook = (pane) => pane?.kind === "notebook";

// savedLeaf is how a pane is stored in a saved layout: a notebook by its id,
// a terminal by its runtime id, or null (dropped) for neither.
export function savedLeaf(pane) {
  if (isNotebook(pane) && pane.notebookId) {
    return { type: "leaf", kind: "notebook", notebookId: pane.notebookId };
  }
  return pane?.runtimeId ? { type: "leaf", runtimeId: pane.runtimeId } : null;
}

// activeKey is how the shown tab is saved: the page tab's id, the runtime id
// of the terminal it shows, "nb:<notebook>", or "" (a New tab or nothing).
// pane is the shown tab's focused pane and term the terminal it follows.
export function activeKey({ tab, pane, term }) {
  if (tab?.page) return tab.id;
  if (term?.runtimeId) return term.runtimeId;
  if (isNotebook(pane) && pane.notebookId) return NOTEBOOK_PREFIX + pane.notebookId;
  return "";
}

// workspaceSnapshot is the workspace to save. tabs are the tab records in
// strip order, panes maps a pane id to its record and active is activeKey's.
// New tabs, joined tabs and tabs with no saved panes are left out.
export function workspaceSnapshot({ tabs, panes, active }) {
  const saved = [];
  const pages = [];
  for (const tab of tabs) {
    if (tab.page) {
      pages.push(tab.page);
      continue;
    }
    if (tab.start || tab.remote || !tab.layout) continue;
    const layout = serializeLayout(tab.layout, (paneId) => savedLeaf(panes.get(paneId)));
    if (!layout) continue;
    const focused = panes.get(tab.focusedPaneId);
    const zoomed = panes.get(tab.zoomedPaneId);
    saved.push({
      title: tab.customTitle || "",
      sync: !!tab.sync,
      focusedRuntimeId: focused?.runtimeId || "",
      zoomedRuntimeId: zoomed?.runtimeId || "",
      focusedNotebookId: isNotebook(focused) ? focused.notebookId || "" : "",
      zoomedNotebookId: isNotebook(zoomed) ? zoomed.notebookId || "" : "",
      layout,
    });
  }
  return { v: 1, active, tabs: saved, pages };
}

// parseWorkspace is the saved workspace in text, or null when there is
// none or it is not one this version wrote.
export function parseWorkspace(text) {
  try {
    const raw = JSON.parse(text || "null");
    if (!raw || raw.v !== 1 || !Array.isArray(raw.tabs)) return null;
    return raw;
  } catch {
    return null;
  }
}

// savedActive reads a saved active key back: { pageId }, { notebookId },
// { runtimeId } or null. pagePrefix starts a page tab's id.
export function savedActive(active, pagePrefix) {
  if (typeof active !== "string" || !active) return null;
  if (active.startsWith(pagePrefix)) return { pageId: active };
  if (active.startsWith(NOTEBOOK_PREFIX)) return { notebookId: active.slice(NOTEBOOK_PREFIX.length) };
  return { runtimeId: active };
}

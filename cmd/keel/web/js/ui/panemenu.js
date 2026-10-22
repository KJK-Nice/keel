// The pane menu (PaneMenu in internal/ui/rowmenus.templ): one menu for
// every pane, opened from a pane bar (its More button, a right-click,
// ⇧F10) or a row of the sidebar's Panes section. It acts on the pane it
// was opened for; which items show is lib/termmenu.js paneMenuItems.
//
// The panes stay with term.js; the caller passes:
//   pane(id), tab(id)   the pane and its tab, or undefined
//   isTermPane(pane)
//   panesIn(tab)        how many panes the tab's layout holds
//   act                 { focus, splitH, splitV, zoom, share, reconnect, close },
//                       each called with the pane
import { paneMenuItems } from "../lib/termmenu.js";

export function createPaneMenu({ doc = document, pane, tab, isTermPane, panesIn, act }) {
  let paneId = "";

  // prepare shows the items for this pane and returns the menu's hidden
  // trigger, or null when there is no such pane.
  function prepare(id, where) {
    const p = pane(id);
    const t = p && tab(p.tabId);
    const trigger = doc.getElementById("pane-menu-trigger");
    const content = trigger && doc.getElementById(trigger.getAttribute("aria-controls"));
    if (!p || !t || !content) return null;
    paneId = id;
    const show = paneMenuItems({
      where,
      term: isTermPane(p),
      runtime: !!p.runtimeId,
      remote: !!(t.remote || p.remote),
      panes: panesIn(t),
      zoomed: t.zoomedPaneId === id,
      failed: p.status === "is-error",
    });
    content.querySelectorAll("[data-pane-act]").forEach((item) => {
      item.hidden = !show[item.dataset.paneAct];
    });
    return trigger;
  }

  // open shows the menu for a pane at detail (an anchor or a point).
  function open(id, where, detail) {
    const trigger = prepare(id, where);
    if (!trigger) return;
    trigger.dispatchEvent(new CustomEvent("tui-dropdownmenu-open-at", { bubbles: true, detail }));
  }

  // run is a click on one of its items (data-pane-act).
  function run(name) {
    const p = pane(paneId);
    if (!p) return;
    const fn = {
      focus: act.focus,
      "split-h": act.splitH,
      "split-v": act.splitV,
      zoom: act.zoom,
      share: act.share,
      reconnect: act.reconnect,
      close: act.close,
    }[name];
    fn?.(p);
  }

  return { prepare, open, run };
}

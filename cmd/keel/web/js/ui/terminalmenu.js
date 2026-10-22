// The terminal menu (TermMenu in internal/ui/rowmenus.templ): a right-click
// on a terminal body when the setting says Menu, or the Menu key in a
// terminal. It acts on the pane it was opened for; which items show and
// which are disabled is lib/termmenu.js termMenuItems.
//
// Copy, Select all and Clear act on this window's xterm only (Clear drops
// its scrollback; nothing is sent to the shell). Paste is the caller's:
// it types into the PTY, so it stays with term.js, which refuses it to a
// follower. The caller passes:
//   pane(id), tab(id)        the pane and its tab, or undefined
//   isTermPane, isDriving
//   copyText(text)           resolves true when the clipboard took it
//   toast
//   act                      { paste, find, splitH, splitV, close }, each
//                            called with the pane
import { termMenuItems } from "../lib/termmenu.js";

// A Menu key that opened the menu may be followed by a contextmenu event
// from the same press (some engines fire both): within this many ms it is
// the same press, not a second right-click.
const KEY_ECHO_MS = 600;

export function createTerminalMenu({ doc = document, now = Date.now, pane, tab, isTermPane, isDriving, copyText, toast, act }) {
  let paneId = "";
  let keyAt = 0;

  // open shows the menu for a pane at detail (an anchor or a point);
  // fromKey says the Menu key opened it.
  function open(p, detail, { fromKey = false } = {}) {
    const trigger = doc.getElementById("term-menu-trigger");
    const content = trigger && doc.getElementById(trigger.getAttribute("aria-controls"));
    const t = p && tab(p.tabId);
    if (!p?.term || !t || !content) return;
    if (fromKey) keyAt = now();
    paneId = p.id;
    const items = termMenuItems({
      selection: !!p.term.hasSelection?.(),
      driving: isDriving(p) && isTermPane(p),
      canSplit: !t.remote && !p.remote,
      find: !!p.search,
    });
    content.querySelectorAll("[data-term-act]").forEach((item) => {
      const it = items[item.dataset.termAct];
      item.hidden = !it?.show;
      item.disabled = !!it?.disabled;
    });
    const sep = content.querySelector('[data-term-sep="split"]');
    if (sep) sep.hidden = !items["split-h"].show;
    trigger.dispatchEvent(new CustomEvent("tui-dropdownmenu-open-at", { bubbles: true, detail }));
  }

  // keyJustOpened reports whether the Menu key opened the menu a moment
  // ago, so the contextmenu event of the same press is swallowed.
  function keyJustOpened() {
    return now() - keyAt < KEY_ECHO_MS;
  }

  // run is a click on one of its items (data-term-act).
  function run(name) {
    const p = pane(paneId);
    const term = p?.term;
    if (!term) return;
    if (name === "copy") {
      const sel = term.getSelection();
      if (sel) copyText(sel).then((ok) => ok || toast("Copy blocked", true));
    } else if (name === "paste") act.paste(p);
    else if (name === "select-all") term.selectAll();
    else if (name === "clear") term.clear();
    else if (name === "find") act.find(p);
    else if (name === "split-h") act.splitH(p);
    else if (name === "split-v") act.splitV(p);
    else if (name === "close") act.close(p);
  }

  return { open, run, keyJustOpened };
}

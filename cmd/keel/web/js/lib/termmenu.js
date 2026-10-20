// Terminal right-click and the terminal menu (TermMenu in
// internal/ui/rowmenus.templ). Pure: no DOM.
//
// Settings · Terminal · Right-click in the terminal has three values:
//   paste    the clipboard goes into the pane (the old "Right-click to
//            paste" switch, unchanged: ⇧ too pastes)
//   menu     the terminal menu (the default)
//   nothing  Keel does nothing
// With menu or nothing, ⇧ right-click is left to the system (its own
// menu, if any), so a native menu is always one modifier away.

export const RIGHT_CLICK_MODES = ["paste", "menu", "nothing"];

// rightClickMode reads the setting from the saved prefs (localStorage
// "keel-term"). Prefs saved before the setting existed carry the old
// rightClickPaste switch: true becomes "paste", anything else (false,
// missing, a new install) "menu".
export function rightClickMode(raw) {
  const v = raw?.rightClick;
  if (RIGHT_CLICK_MODES.includes(v)) return v;
  return raw?.rightClickPaste === true ? "paste" : "menu";
}

// termRightClick is what a right-click on the terminal body does:
// "paste", "menu", "none" (swallow it) or "pass" (leave it to the system).
export function termRightClick(mode, ev) {
  if (mode === "paste") return "paste";
  if (ev?.shiftKey) return "pass";
  if (mode === "menu") return "menu";
  if (mode === "nothing") return "none";
  return "pass";
}

// termMenuItems is which terminal menu items show and which are disabled,
// keyed by data-term-act. Copy needs a selection; Paste needs the client
// to drive the pane (a follower cannot type into it); splits need a pane
// of this window (not a joined one); Find needs the search addon.
export function termMenuItems({ selection = false, driving = false, canSplit = false, find = false } = {}) {
  return {
    copy: { show: true, disabled: !selection },
    paste: { show: true, disabled: !driving },
    "select-all": { show: true, disabled: false },
    clear: { show: true, disabled: false },
    find: { show: !!find, disabled: false },
    "split-h": { show: !!canSplit, disabled: false },
    "split-v": { show: !!canSplit, disabled: false },
    close: { show: true, disabled: false },
  };
}

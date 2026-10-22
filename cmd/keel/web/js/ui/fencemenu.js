// The notebook code block menu (FenceMenu in internal/ui/rowmenus.templ):
// a right-click, ⇧F10 or the Menu key on a code block's bar (its language,
// Copy or Type). Its items click the fence's own Copy and Type buttons
// (notebook-editor.mjs), so Type still asks Approve before type and only a
// shell fence offers it. The code itself is editable text and keeps the
// system's menu (Cut, Copy, Paste).
import { isMenuKey } from "../lib/rowmenu.js";

export function createFenceMenu({ doc = document } = {}) {
  let current = null;

  const fenceOf = (el) => el?.closest?.(".notebook-fence-bar")?.closest(".notebook-fence") || null;

  // open shows the menu for a fence at detail (an anchor or a point).
  function open(fence, detail) {
    const trigger = doc.getElementById("fence-menu-trigger");
    const content = trigger && doc.getElementById(trigger.getAttribute("aria-controls"));
    if (!fence || !content) return;
    current = fence;
    const run = fence.querySelector("[data-notebook-fence]");
    const typeItem = content.querySelector('[data-fence-act="type"]');
    if (typeItem) {
      typeItem.hidden = !run || run.hidden;
      const label = typeItem.querySelector("[data-fence-type-label]");
      if (label) label.textContent = `${run?.textContent.trim() || "Type into pane"}…`;
    }
    trigger.dispatchEvent(new CustomEvent("tui-dropdownmenu-open-at", { bubbles: true, detail }));
  }

  // run is a click on one of its items (data-fence-act): the fence's own
  // Type or Copy button, when it is there and enabled.
  function run(name) {
    if (!current?.isConnected) return;
    const btn = current.querySelector(name === "type" ? "[data-notebook-fence]" : "[data-notebook-copy]");
    if (btn && !btn.hidden && !btn.disabled) btn.click();
  }

  // A right-click on a code block's bar opens its menu, unless ⇧ is held.
  function onContextMenu(ev) {
    const fence = fenceOf(ev.target);
    if (!fence || ev.shiftKey || ev.target.closest("select")) return;
    ev.preventDefault();
    const back = ev.target.closest("button") || fence.closest("[data-notebook-editor]");
    open(fence, { x: ev.clientX, y: ev.clientY, returnFocus: back, focusFirst: false });
  }

  // ⇧F10 or the Menu key on a code block's bar: the same menu, below the
  // focused control.
  function onMenuKey(ev) {
    if (!isMenuKey(ev)) return;
    const active = doc.activeElement;
    const fence = fenceOf(active);
    if (!fence) return;
    ev.preventDefault();
    ev.stopPropagation();
    open(fence, { anchor: active, returnFocus: active, focusFirst: true });
  }

  // bind listens for both on the document (keys in the capture phase, so
  // the notebook editor does not see them first).
  function bind() {
    doc.addEventListener("contextmenu", onContextMenu);
    doc.addEventListener("keydown", onMenuKey, true);
  }

  return { open, run, bind };
}

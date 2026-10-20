// Row context menus. Every row kind has one menu, rendered once in templ
// (internal/ui/rowmenus.templ) beside the row's "…" button. A right-click
// on the row, ⇧F10 or the ContextMenu key on its focused control open that
// same content, at the pointer or below the control
// (components/dropdownmenu: tui-dropdownmenu-open-at). Rows carry
// data-row-menu; a shared menu (the pane menu) names itself there and the
// caller's resolve() prepares it. Nothing is reachable only this way: the
// "…" button (or the pane bar's More button) opens every menu too.
import { keyCaps, keysFor } from "./shortcutmap.js";

// isMenuKey reports the keyboard's context-menu keys: ⇧F10 and the
// ContextMenu (Menu) key, with no other modifier.
export function isMenuKey(ev) {
  if (!ev || ev.metaKey || ev.ctrlKey || ev.altKey) return false;
  if (ev.key === "ContextMenu") return !ev.shiftKey;
  return ev.key === "F10" && !!ev.shiftKey;
}

// Keys of menu items that are not app-modifier chords (shortcutmap.js).
// The terminal menu's Copy, Paste and Select all are the native Edit
// menu's ⌘C ⌘V ⌘A on macOS; elsewhere Ctrl+C, Ctrl+V and Ctrl+A belong to
// the shell, so those items show no key.
const ROW_KEYS = {
  connect: { mac: "↵", other: "Enter" },
  "open-split": { mac: "⌥↵", other: "Alt+Enter" },
  copy: { mac: "⌘C", other: "" },
  paste: { mac: "⌘V", other: "" },
  "select-all": { mac: "⌘A", other: "" },
};

// shortcutLabel is what a menu item shows for its shortcut on this
// platform: macOS glyphs on macOS, words elsewhere (docs/design/README.md).
export function shortcutLabel(id, mac) {
  const row = ROW_KEYS[id];
  if (row) return mac ? row.mac : row.other;
  return keysFor(id, mac);
}

const ARIA_KEY = { "⌘": "Meta", "⇧": "Shift", "⌥": "Alt", "⌃": "Control", Ctrl: "Control", Shift: "Shift", Alt: "Alt", "↵": "Enter" };

// ariaKeys turns a label into aria-keyshortcuts ("⌘⇧D" -> "Meta+Shift+D").
export function ariaKeys(label) {
  const caps = keyCaps(String(label || "").replace(/^Alt\+/, "⌥"));
  if (!caps.length) return "";
  return caps.map((c) => ARIA_KEY[c] || (c.length === 1 ? c.toUpperCase() : c)).join("+");
}

// localizeShortcuts fills each [data-keys] in root with this platform's
// label and gives its item aria-keyshortcuts (the label is aria-hidden).
export function localizeShortcuts(root, mac) {
  root?.querySelectorAll?.("[data-keys]").forEach((el) => {
    const label = shortcutLabel(el.getAttribute("data-keys"), mac);
    const item = el.closest('[role^="menuitem"]');
    el.hidden = !label;
    if (!label) {
      item?.removeAttribute("aria-keyshortcuts");
      return;
    }
    if (el.textContent !== label) el.textContent = label;
    const aria = ariaKeys(label);
    if (item && aria && item.getAttribute("aria-keyshortcuts") !== aria) item.setAttribute("aria-keyshortcuts", aria);
  });
}

// rowControl is the element focus returns to when the row's menu closes:
// the control under the pointer or with focus, else the row's first.
function rowControl(row, from) {
  const own = from?.closest?.("button, a[href], [tabindex]");
  if (own && row.contains(own) && !own.closest("[data-tui-dropdownmenu-trigger]")) return own;
  return row.querySelector("button:not([data-tui-dropdownmenu-trigger]), a[href], [tabindex]") || row;
}

const SKIP = "input, textarea, select, [contenteditable], .xterm, [data-tui-dropdownmenu-content]";

// bindRowMenus wires right-click and the menu keys to rows.
//   mac      — macOS (shortcut labels)
//   resolve  — (row, at) => the trigger to open, false to leave the event
//              alone, or nothing for the row's own "…" trigger. at is
//              { x, y } for a pointer, { anchor } for a key.
export function bindRowMenus(doc, { mac = false, resolve } = {}) {
  const triggerOf = (row, at) => {
    const r = resolve ? resolve(row, at) : undefined;
    if (r === false) return null;
    return r || row.querySelector("[data-tui-dropdownmenu-trigger]");
  };
  let keyOpenedAt = 0;
  const openAt = (trigger, detail) => trigger.dispatchEvent(new CustomEvent("tui-dropdownmenu-open-at", { bubbles: true, detail }));

  doc.addEventListener("contextmenu", (ev) => {
    const t = ev.target;
    if (!t || typeof t.closest !== "function" || t.closest(SKIP)) return;
    const row = t.closest("[data-row-menu]");
    if (!row) return;
    // The keyboard already opened it (some engines also fire contextmenu
    // for ⇧F10 and the Menu key; a mouse right-click has button 2).
    if (ev.button !== 2 && Date.now() - keyOpenedAt < 600) {
      ev.preventDefault();
      return;
    }
    const at = { x: ev.clientX, y: ev.clientY };
    const trigger = triggerOf(row, at);
    if (!trigger) return;
    ev.preventDefault();
    openAt(trigger, { ...at, returnFocus: rowControl(row, t), focusFirst: false });
  });

  doc.addEventListener("keydown", (ev) => {
    if (!isMenuKey(ev)) return;
    const active = doc.activeElement;
    if (!active || typeof active.closest !== "function" || active.closest(SKIP)) return;
    const row = active.closest("[data-row-menu]");
    if (!row) return;
    const anchor = rowControl(row, active);
    const trigger = triggerOf(row, { anchor });
    if (!trigger) return;
    ev.preventDefault();
    ev.stopPropagation();
    keyOpenedAt = Date.now();
    openAt(trigger, { anchor, returnFocus: anchor, focusFirst: true });
  });

  doc.addEventListener("dropdownmenu-open-change", (ev) => {
    if (ev.detail?.open) localizeShortcuts(ev.target, mac);
  });
}

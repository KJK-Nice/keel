// Chrome keyboard shortcuts for term.js. Pure functions: no DOM, no state.
import { tabCycleDelta } from "./keys.js";

// isAppMod reports the app modifier alone: Cmd on macOS, Ctrl elsewhere.
export function isAppMod(ev, mac) {
  return mac ? !!ev.metaKey && !ev.ctrlKey : !!ev.ctrlKey && !ev.metaKey;
}

// classifyShortcut maps a keydown to the chrome action it triggers, or
// null when the key should go on to the terminal or the page.
//
// ctx.mac       — macOS (Cmd is the app modifier)
// ctx.modalOpen — a dialog is open: only tab, zoom and palette keys work
// ctx.findOpen  — () => boolean, whether terminal find is open (for Cmd+G)
// ctx.startPage — the New tab page is showing and no dialog is open: ⌥1…⌥6
//                 open its recent hosts (elsewhere ⌥ keys belong to the shell)
//
// Actions: { name } for runShortcut, { name, repeat } for prev-tab /
// next-tab (hold-to-repeat cycles), { name: "find-next" | "find-prev" },
// { name: "tab-index", index } for Cmd+1…9, and { name: "recent-host", index }
// for ⌥1…⌥6 on the New tab page.
export function classifyShortcut(ev, ctx) {
  if (ctx.startPage && ev.altKey && !ev.metaKey && !ev.ctrlKey && !ev.shiftKey) {
    // ev.code, not ev.key: ⌥1 types "¡" on a Mac keyboard.
    const m = /^Digit([1-6])$/.exec(ev.code || "");
    if (m) return { name: "recent-host", index: Number(m[1]) };
  }
  if (!isAppMod(ev, ctx.mac) || ev.altKey) return null;
  const raw = ev.key || "";
  const key = raw.length === 1 ? raw.toLowerCase() : raw;
  // Holding Cmd+T / Cmd+W must not open or close a tab per repeat.
  if (ev.repeat && (key === "t" || key === "w")) return null;
  // Cmd+W closes a pane or tab; Cmd+Shift+W is left for Close Window.
  if (key === "w" && !ev.shiftKey) return { name: "close-tab" };
  if (key === "t" && ev.shiftKey) return { name: "reopen-tab" };
  if (key === "t") return ctx.modalOpen ? null : { name: "new-tab" };
  if (key === "d" && ev.shiftKey) return { name: "split-right" };
  if (key === "e" && ev.shiftKey) return { name: "split-down" };
  if ((key === "enter" || ev.key === "Enter") && ev.shiftKey) return { name: "focus-pane" };
  if (key === "=" || key === "+" || ev.code === "Equal" || ev.code === "NumpadAdd") return { name: "zoom-in" };
  if (key === "-" || key === "_" || ev.code === "Minus" || ev.code === "NumpadSubtract") return { name: "zoom-out" };
  if ((key === "0" || ev.code === "Digit0" || ev.code === "Numpad0") && !ev.shiftKey) return { name: "zoom-reset" };
  if (key === "k" && !ev.shiftKey) return { name: "palette" };
  if (key === "p" && ev.shiftKey) return { name: "palette" };
  if (ctx.modalOpen) return null;
  if (key === "f" && !ev.shiftKey) return { name: "find-term" };
  if (key === "b" && !ev.shiftKey) return { name: "toggle-sidebar" };
  // Agent panel: ⌘I on macOS. Elsewhere the app modifier is Ctrl, and
  // Ctrl+I is Tab in a terminal (readline completion), so it stays with the
  // shell and the panel takes Ctrl+Shift+I.
  if (key === "i" && ev.shiftKey === !ctx.mac) return { name: "toggle-agent" };
  if (key === "g" && ctx.findOpen?.()) return { name: ev.shiftKey ? "find-prev" : "find-next" };
  if (key === "n" && !ev.shiftKey) return { name: "new-host" };
  if (key === "o" && ev.shiftKey) return { name: "quick-connect" };
  if (key === "j" && !ev.shiftKey) return { name: "join" };
  // Tab cycling runs before Cmd+, so Cmd+< (Shift+Comma on US layouts)
  // is never read as Settings.
  const delta = tabCycleDelta(ev);
  if (delta) return { name: delta < 0 ? "prev-tab" : "next-tab", repeat: !!ev.repeat };
  if (key === "," && !ev.shiftKey) return { name: "settings" };
  if (!ev.shiftKey && /^[1-9]$/.test(ev.key)) return { name: "tab-index", index: Number(ev.key) };
  return null;
}

// The native window's menu items carry the same accelerators (File and
// View in internal/desktop/desktop.go). On macOS WKWebView offers a ⌘-key
// to the page first and hands it to the menu only when keydown was not
// preventDefault-ed. A chord the page runs is therefore not run again by
// the menu; a chord the page declines (a dialog is open, the gate is up,
// a tab is being renamed, a held ⌘T) would be run by the menu instead.

// isChromeChord reports a key that classifyShortcut runs in the plain
// state: no dialog, no repeat. term.js swallows such a key when it
// declines it, so the native menu does not run it either.
export function isChromeChord(ev, mac) {
  const plain = {
    key: ev.key,
    code: ev.code,
    metaKey: ev.metaKey,
    ctrlKey: ev.ctrlKey,
    shiftKey: ev.shiftKey,
    altKey: ev.altKey,
    repeat: false,
  };
  return classifyShortcut(plain, { mac, modalOpen: false, findOpen: () => false }) !== null;
}

// Actions classifyShortcut still runs while a dialog is open, plus the
// menu-only items that sit with them (Split Notebook, Stop Keel runtime).
const DIALOG_ACTIONS = new Set([
  "close-tab",
  "reopen-tab",
  "split-right",
  "split-down",
  "split-notebook",
  "focus-pane",
  "zoom-in",
  "zoom-out",
  "zoom-reset",
  "palette",
  "stop-runtime",
]);

// menuShortcutAllowed says whether a native menu item (window.__keelShortcut)
// may run now, with the limits the keyboard has:
//
// ctx.gateOpen  — Unlock, First run or Import hosts covers the window
// ctx.renaming  — focus is in a tab's rename field
// ctx.modalOpen — a dialog is open
//
// Stop Keel runtime always runs: it is the way out of a stuck window.
export function menuShortcutAllowed(name, ctx) {
  if (name === "stop-runtime") return true;
  if (ctx.gateOpen || ctx.renaming) return false;
  if (ctx.modalOpen) return DIALOG_ACTIONS.has(name);
  return true;
}

import { test } from "node:test";
import assert from "node:assert/strict";
import { classifyShortcut, isAppMod, isChromeChord, menuShortcutAllowed } from "../lib/shortcuts.js";

const mac = { mac: true, modalOpen: false, findOpen: () => false };
const cmd = (key, extra = {}) => ({ key, code: "", metaKey: true, ctrlKey: false, shiftKey: false, altKey: false, repeat: false, ...extra });
const name = (ev, ctx = mac) => classifyShortcut(ev, ctx)?.name ?? null;

test("every chrome shortcut runs in JS", () => {
  const cases = [
    [cmd("t"), "new-tab"],
    [cmd("w"), "close-tab"],
    [cmd("T", { shiftKey: true }), "reopen-tab"],
    [cmd("n"), "new-host"],
    [cmd("O", { shiftKey: true }), "quick-connect"],
    [cmd("j"), "join"],
    [cmd("f"), "find-term"],
    [cmd("b"), "toggle-sidebar"],
    [cmd("D", { shiftKey: true }), "split-right"],
    [cmd("E", { shiftKey: true }), "split-down"],
    [cmd("Enter", { shiftKey: true }), "focus-pane"],
    [cmd("=", { code: "Equal" }), "zoom-in"],
    [cmd("+", { code: "Equal", shiftKey: true }), "zoom-in"],
    [cmd("-", { code: "Minus" }), "zoom-out"],
    [cmd("0", { code: "Digit0" }), "zoom-reset"],
    [cmd("k"), "palette"],
    [cmd("P", { shiftKey: true }), "palette"],
    [cmd(","), "settings"],
  ];
  for (const [ev, want] of cases) assert.equal(name(ev), want, `${ev.key} shift=${ev.shiftKey}`);
});

test("Cmd+Shift+W is left for Close Window", () => {
  assert.equal(name(cmd("W", { shiftKey: true })), null);
});

test("holding Cmd+T / Cmd+W does not repeat, but tab cycling does", () => {
  assert.equal(name(cmd("t", { repeat: true })), null);
  assert.equal(name(cmd("w", { repeat: true })), null);
  assert.deepEqual(classifyShortcut(cmd(">", { code: "Period", shiftKey: true, repeat: true }), mac), { name: "next-tab", repeat: true });
  assert.deepEqual(classifyShortcut(cmd("<", { code: "Comma", shiftKey: true }), mac), { name: "prev-tab", repeat: false });
});

test("tab cycling wins over Cmd+, on layouts where < is Shift+Comma", () => {
  assert.equal(name(cmd(",", { code: "Comma", shiftKey: true })), "prev-tab");
  assert.equal(name(cmd(",", { code: "Comma" })), "settings");
});

test("Cmd+1…9 activate tabs by position", () => {
  assert.deepEqual(classifyShortcut(cmd("1"), mac), { name: "tab-index", index: 1 });
  assert.deepEqual(classifyShortcut(cmd("3"), mac), { name: "tab-index", index: 3 });
  assert.deepEqual(classifyShortcut(cmd("9"), mac), { name: "tab-index", index: 9 });
  assert.equal(name(cmd("3", { shiftKey: true })), null);
});

test("Cmd+B toggles the vault sidebar; Ctrl+B stays with the terminal on macOS", () => {
  assert.equal(name(cmd("b")), "toggle-sidebar");
  assert.equal(name(cmd("B", { shiftKey: true })), null);
  assert.equal(name({ ...cmd("b"), metaKey: false, ctrlKey: true }), null, "readline/tmux Ctrl-B reaches the PTY");
  assert.equal(name(cmd("b"), { ...mac, modalOpen: true }), null, "not while a dialog is open");
});

test("agent panel: Cmd+I on macOS, Ctrl+Shift+I elsewhere; Ctrl+I (Tab) stays with the terminal", () => {
  const other = { mac: false, modalOpen: false, findOpen: () => false };
  const ctrl = (key, extra = {}) => ({ ...cmd(key, extra), metaKey: false, ctrlKey: true });
  assert.equal(name(cmd("i")), "toggle-agent");
  assert.equal(name(cmd("I", { shiftKey: true })), null);
  assert.equal(name(ctrl("i")), null, "macOS: Ctrl-I is Tab for readline");
  assert.equal(name(ctrl("i"), other), null, "Windows/Linux: Ctrl+I is Tab for readline, never the panel");
  assert.equal(name(ctrl("I", { shiftKey: true }), other), "toggle-agent");
  assert.equal(name(cmd("i"), { ...mac, modalOpen: true }), null, "not while a dialog is open");
});

test("Cmd+N is one action; term.js picks New host or the tab page's New", () => {
  assert.equal(name(cmd("n")), "new-host");
  assert.equal(name(cmd("N", { shiftKey: true })), null);
});

test("with a dialog open only tab, zoom and palette keys work", () => {
  const modal = { ...mac, modalOpen: true };
  assert.equal(name(cmd("t"), modal), null, "new tab");
  assert.equal(name(cmd("f"), modal), null, "find");
  assert.equal(name(cmd(","), modal), null, "settings");
  assert.equal(name(cmd("w"), modal), "close-tab");
  assert.equal(name(cmd("k"), modal), "palette");
  assert.equal(name(cmd("=", { code: "Equal" }), modal), "zoom-in");
});

test("Cmd+G steps through find matches only while find is open", () => {
  assert.equal(name(cmd("g")), null);
  const finding = { ...mac, findOpen: () => true };
  assert.equal(name(cmd("g"), finding), "find-next");
  assert.equal(name(cmd("G", { shiftKey: true }), finding), "find-prev");
});

test("the app modifier is Cmd on macOS and Ctrl elsewhere, never with Alt", () => {
  const linux = { mac: false, modalOpen: false, findOpen: () => false };
  assert.equal(name({ ...cmd("t"), metaKey: false, ctrlKey: true }, linux), "new-tab");
  assert.equal(name(cmd("t"), linux), null, "Cmd is not the app modifier off macOS");
  assert.equal(name({ ...cmd("t"), ctrlKey: true }), null, "Cmd+Ctrl is not a shortcut");
  assert.equal(name(cmd("t", { altKey: true })), null, "Alt combinations go to the terminal");
  assert.equal(isAppMod({ metaKey: true }, true), true);
  assert.equal(isAppMod({ ctrlKey: true }, true), false);
});

test("plain letters and unknown chords are not shortcuts", () => {
  assert.equal(name({ ...cmd("t"), metaKey: false }), null);
  assert.equal(name(cmd("q")), null);
  assert.equal(name(cmd("Escape")), null);
});

test("Alt+1…6 open recent hosts only on the New tab page", () => {
  const start = { ...mac, startPage: true };
  const alt = (n, extra = {}) => ({ key: "¡", code: `Digit${n}`, metaKey: false, ctrlKey: false, shiftKey: false, altKey: true, repeat: false, ...extra });
  for (let n = 1; n <= 6; n++) assert.deepEqual(classifyShortcut(alt(n), start), { name: "recent-host", index: n });
  // Not 7-9, not 0, and not off the New tab page: Alt keys belong to the shell there.
  for (const n of [0, 7, 8, 9]) assert.equal(classifyShortcut(alt(n), start), null);
  assert.equal(classifyShortcut(alt(1), mac), null);
  // Other modifiers keep their own meaning.
  assert.equal(classifyShortcut(alt(1, { metaKey: true }), start), null);
  assert.equal(classifyShortcut(alt(1, { shiftKey: true }), start), null);
  assert.equal(classifyShortcut(alt(1, { ctrlKey: true }), start), null);
  // Windows and Linux use the same Alt key.
  assert.deepEqual(classifyShortcut(alt(2, { key: "2" }), { mac: false, modalOpen: false, startPage: true }), { name: "recent-host", index: 2 });
});

// The native menu carries the same chords. term.js swallows a chrome chord
// it declines, so the menu does not run it in its place (#36).
test("isChromeChord: chords the page runs in the plain state", () => {
  assert.equal(isChromeChord(cmd("t"), true), true);
  assert.equal(isChromeChord(cmd("t", { repeat: true }), true), true, "a held Cmd+T is still a chord");
  assert.equal(isChromeChord(cmd("b"), true), true);
  assert.equal(isChromeChord(cmd("=", { code: "Equal" }), true), true);
  // Edit-menu keys and Close Window stay with the native menu.
  for (const k of ["c", "v", "x", "a", "z", "q"]) assert.equal(isChromeChord(cmd(k), true), false, k);
  assert.equal(isChromeChord(cmd("Z", { shiftKey: true }), true), false);
  assert.equal(isChromeChord(cmd("W", { shiftKey: true }), true), false);
  // Off macOS the app modifier is Ctrl.
  assert.equal(isChromeChord({ ...cmd("t"), metaKey: false, ctrlKey: true }, false), true);
  assert.equal(isChromeChord(cmd("t"), false), false);
});

test("menuShortcutAllowed: menu items keep the keyboard's limits", () => {
  const plain = { gateOpen: false, renaming: false, modalOpen: false };
  for (const n of ["new-tab", "toggle-agent", "search", "split-notebook"]) assert.equal(menuShortcutAllowed(n, plain), true, n);
  const modal = { ...plain, modalOpen: true };
  for (const n of ["new-tab", "toggle-sidebar", "toggle-agent", "find-term", "new-host", "quick-connect", "join", "search"]) {
    assert.equal(menuShortcutAllowed(n, modal), false, `${n} with a dialog open`);
  }
  for (const n of ["close-tab", "reopen-tab", "split-right", "split-down", "focus-pane", "zoom-in", "zoom-out", "zoom-reset"]) {
    assert.equal(menuShortcutAllowed(n, modal), true, `${n} with a dialog open`);
  }
  for (const ctx of [{ ...plain, gateOpen: true }, { ...plain, renaming: true }]) {
    assert.equal(menuShortcutAllowed("new-tab", ctx), false);
    assert.equal(menuShortcutAllowed("close-tab", ctx), false);
    assert.equal(menuShortcutAllowed("stop-runtime", ctx), true, "Stop Keel runtime always runs");
  }
});

test("focus view is ⌘⇧↵ on macOS and Ctrl+Shift+Enter elsewhere, also while a dialog is open", () => {
  const other = { mac: false, modalOpen: false, findOpen: () => false };
  const ctrl = (key, extra = {}) => ({ ...cmd(key, extra), metaKey: false, ctrlKey: true });
  assert.equal(name(cmd("Enter", { shiftKey: true })), "focus-pane");
  assert.equal(name(cmd("Enter", { shiftKey: true }), { ...mac, modalOpen: true }), "focus-pane");
  assert.equal(name(ctrl("Enter", { shiftKey: true }), other), "focus-pane");
  assert.equal(name(ctrl("Enter", { shiftKey: true }), { ...other, modalOpen: true }), "focus-pane");
  // Without Shift, or with the other platform's modifier, ↵ stays the shell's.
  assert.equal(name(cmd("Enter")), null);
  assert.equal(name(ctrl("Enter", { shiftKey: true })), null, "Ctrl+Shift+Enter reaches the PTY on macOS");
  assert.equal(name(cmd("Enter", { shiftKey: true }), other), null);
  assert.ok(isChromeChord(cmd("Enter", { shiftKey: true }), true));
  assert.ok(menuShortcutAllowed("focus-pane", { gateOpen: false, renaming: false, modalOpen: true }));
});

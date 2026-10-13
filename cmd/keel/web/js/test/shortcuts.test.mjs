import { test } from "node:test";
import assert from "node:assert/strict";
import { classifyShortcut, isAppMod } from "../lib/shortcuts.js";

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

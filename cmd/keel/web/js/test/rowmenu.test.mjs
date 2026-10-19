import { test } from "node:test";
import assert from "node:assert/strict";
import { ariaKeys, isMenuKey, shortcutLabel } from "../lib/rowmenu.js";
import { classifyShortcut } from "../lib/shortcuts.js";

const key = (k, extra = {}) => ({ key: k, metaKey: false, ctrlKey: false, altKey: false, shiftKey: false, ...extra });

test("isMenuKey: ⇧F10 and the ContextMenu key only", () => {
  assert.equal(isMenuKey(key("F10", { shiftKey: true })), true);
  assert.equal(isMenuKey(key("ContextMenu")), true);
  assert.equal(isMenuKey(key("F10")), false);
  assert.equal(isMenuKey(key("F10", { shiftKey: true, metaKey: true })), false);
  assert.equal(isMenuKey(key("F10", { shiftKey: true, altKey: true })), false);
  assert.equal(isMenuKey(key("ContextMenu", { ctrlKey: true })), false);
  assert.equal(isMenuKey(key("Enter", { shiftKey: true })), false);
  assert.equal(isMenuKey(null), false);
});

test("shortcutLabel: glyphs on macOS, words elsewhere", () => {
  assert.equal(shortcutLabel("connect", true), "↵");
  assert.equal(shortcutLabel("connect", false), "Enter");
  assert.equal(shortcutLabel("open-split", true), "⌥↵");
  assert.equal(shortcutLabel("open-split", false), "Alt+Enter");
  assert.equal(shortcutLabel("split-right", true), "⌘⇧D");
  assert.equal(shortcutLabel("split-right", false), "Ctrl+⇧D");
  assert.equal(shortcutLabel("split-down", true), "⌘⇧E");
  assert.equal(shortcutLabel("focus-pane-zoom", true), "⌘⇧↵");
  assert.equal(shortcutLabel("close-tab", true), "⌘W");
  assert.equal(shortcutLabel("nope", true), "");
});

test("the pane menu's shortcuts are the real bindings", () => {
  const ev = (k, shift) => ({ key: k, code: "", metaKey: true, ctrlKey: false, shiftKey: shift, altKey: false, repeat: false });
  const name = (e) => classifyShortcut(e, { mac: true, modalOpen: false, findOpen: () => false })?.name;
  assert.equal(name(ev("D", true)), "split-right");
  assert.equal(name(ev("E", true)), "split-down");
  assert.equal(name(ev("Enter", true)), "focus-pane");
  assert.equal(name(ev("w", false)), "close-tab");
});

test("ariaKeys turns labels into aria-keyshortcuts", () => {
  assert.equal(ariaKeys("⌘⇧D"), "Meta+Shift+D");
  assert.equal(ariaKeys("⌘⇧↵"), "Meta+Shift+Enter");
  assert.equal(ariaKeys("⌘W"), "Meta+W");
  assert.equal(ariaKeys("↵"), "Enter");
  assert.equal(ariaKeys("Enter"), "Enter");
  assert.equal(ariaKeys("⌥↵"), "Alt+Enter");
  assert.equal(ariaKeys("Alt+Enter"), "Alt+Enter");
  assert.equal(ariaKeys("Ctrl+Shift+I"), "Control+Shift+I");
  assert.equal(ariaKeys(""), "");
});

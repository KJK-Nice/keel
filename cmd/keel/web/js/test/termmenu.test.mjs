import { test } from "node:test";
import assert from "node:assert/strict";
import { RIGHT_CLICK_MODES, rightClickMode, termMenuItems, termRightClick } from "../lib/termmenu.js";
import { shortcutLabel, ariaKeys } from "../lib/rowmenu.js";
import { classifyShortcut } from "../lib/shortcuts.js";

test("rightClickMode migrates the old rightClickPaste switch", () => {
  assert.equal(rightClickMode({ rightClickPaste: true }), "paste");
  assert.equal(rightClickMode({ rightClickPaste: false }), "menu");
  // Never saved, or a new install: the menu.
  assert.equal(rightClickMode({}), "menu");
  assert.equal(rightClickMode(null), "menu");
  assert.equal(rightClickMode(undefined), "menu");
  // Only a real true pastes.
  assert.equal(rightClickMode({ rightClickPaste: "true" }), "menu");
  assert.equal(rightClickMode({ rightClickPaste: 1 }), "menu");
});

test("rightClickMode: a saved value wins over the old switch", () => {
  for (const v of RIGHT_CLICK_MODES) {
    assert.equal(rightClickMode({ rightClick: v }), v);
    assert.equal(rightClickMode({ rightClick: v, rightClickPaste: true }), v);
  }
  assert.equal(rightClickMode({ rightClick: "bogus", rightClickPaste: true }), "paste");
  assert.equal(rightClickMode({ rightClick: "bogus" }), "menu");
});

test("termRightClick: paste keeps pasting with ⇧; menu and nothing leave ⇧ to the system", () => {
  assert.equal(termRightClick("paste", { shiftKey: false }), "paste");
  assert.equal(termRightClick("paste", { shiftKey: true }), "paste");
  assert.equal(termRightClick("menu", { shiftKey: false }), "menu");
  assert.equal(termRightClick("menu", { shiftKey: true }), "pass");
  assert.equal(termRightClick("nothing", { shiftKey: false }), "none");
  assert.equal(termRightClick("nothing", { shiftKey: true }), "pass");
  assert.equal(termRightClick("weird", {}), "pass");
});

test("termMenuItems: Copy needs a selection, Paste needs the driver", () => {
  const none = termMenuItems({});
  assert.equal(none.copy.disabled, true);
  assert.equal(none.paste.disabled, true);
  assert.equal(none.find.show, false);
  assert.equal(none["split-h"].show, false);
  assert.equal(none["split-v"].show, false);
  assert.equal(none.close.show, true);
  assert.equal(none["select-all"].disabled, false);
  assert.equal(none.clear.disabled, false);
  const all = termMenuItems({ selection: true, driving: true, canSplit: true, find: true });
  assert.equal(all.copy.disabled, false);
  assert.equal(all.paste.disabled, false);
  assert.equal(all.find.show, true);
  assert.equal(all["split-h"].show, true);
  assert.equal(all["split-v"].show, true);
});

test("terminal menu shortcuts: macOS Edit keys, none for the shell's Ctrl keys", () => {
  assert.equal(shortcutLabel("copy", true), "⌘C");
  assert.equal(shortcutLabel("paste", true), "⌘V");
  assert.equal(shortcutLabel("select-all", true), "⌘A");
  assert.equal(shortcutLabel("copy", false), "");
  assert.equal(shortcutLabel("paste", false), "");
  assert.equal(shortcutLabel("select-all", false), "");
  assert.equal(shortcutLabel("find-term", true), "⌘F");
  assert.equal(shortcutLabel("find-term", false), "Ctrl+F");
  assert.equal(ariaKeys("⌘C"), "Meta+C");
});

test("the terminal menu's Find is the real ⌘F binding", () => {
  const ev = { key: "f", code: "KeyF", metaKey: true, ctrlKey: false, shiftKey: false, altKey: false, repeat: false };
  assert.equal(classifyShortcut(ev, { mac: true, modalOpen: false, findOpen: () => false })?.name, "find-term");
});

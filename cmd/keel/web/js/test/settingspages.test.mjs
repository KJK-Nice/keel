import test from "node:test";
import assert from "node:assert/strict";
import { SHORTCUTS, keyCaps, keysFor, shortcutGroups } from "../lib/shortcutmap.js";
import { classifyShortcut } from "../lib/shortcuts.js";
import { undoKey } from "../lib/suggest.js";
import { displayName, signalText, stopMessage, uptimeText, vaultModeText, versionOf } from "../lib/settingspages.js";

test("keysFor: app modifier per platform", () => {
  assert.equal(keysFor("new-tab", true), "⌘T");
  assert.equal(keysFor("new-tab", false), "Ctrl+T");
  assert.equal(keysFor("reopen-tab", false), "Ctrl+⇧T");
  assert.equal(keysFor("toggle-agent", true), "⌘I");
  assert.equal(keysFor("toggle-agent", false), "Ctrl+Shift+I");
  assert.equal(keysFor("suggest-undo", true), "⌘Z");
  assert.equal(keysFor("suggest-undo", false), "Ctrl+Shift+Z");
  assert.equal(keysFor("nope", true), "");
});

test("every id is unique and every group is listed", () => {
  const ids = SHORTCUTS.map((s) => s.id);
  assert.equal(new Set(ids).size, ids.length);
  const groups = shortcutGroups(true);
  assert.equal(groups.reduce((n, g) => n + g.items.length, 0), SHORTCUTS.length);
  assert.ok(groups.every((g) => g.items.length > 0));
});

test("the table agrees with classifyShortcut and undoKey", () => {
  const ev = (key, o = {}) => ({ key, metaKey: false, ctrlKey: false, shiftKey: false, altKey: false, ...o });
  for (const mac of [true, false]) {
    const mod = mac ? { metaKey: true } : { ctrlKey: true };
    const act = (key, o = {}) => classifyShortcut(ev(key, { ...mod, ...o }), { mac })?.name;
    assert.equal(act("t"), "new-tab");
    assert.equal(act("w"), "close-tab");
    assert.equal(act("t", { shiftKey: true }), "reopen-tab");
    assert.equal(act("b"), "toggle-sidebar");
    assert.equal(act("k"), "palette");
    assert.equal(act(","), "settings");
    assert.equal(act("i", { shiftKey: !mac }), "toggle-agent");
    assert.equal(undoKey(ev("z", { ...mod, shiftKey: !mac }), mac), true);
  }
});

test("keyCaps", () => {
  assert.deepEqual(keyCaps("Ctrl+Shift+I"), ["Ctrl", "Shift", "I"]);
  assert.deepEqual(keyCaps("⌘⇧T"), ["⌘", "⇧", "T"]);
  assert.deepEqual(keyCaps("Ctrl++"), ["Ctrl", "+"]);
  assert.deepEqual(keyCaps("⌘,"), ["⌘", ","]);
  assert.deepEqual(keyCaps("Ctrl+⇧↵"), ["Ctrl", "⇧", "↵"]);
});

test("formatting", () => {
  assert.equal(uptimeText(5), "5 s");
  assert.equal(uptimeText(125), "2 min");
  assert.equal(uptimeText(3 * 3600 + 300), "3 h 05 min");
  assert.equal(uptimeText(2 * 86400 + 4 * 3600), "2 d 4 h");
  assert.equal(versionOf("9d62912abcde-0123456789ab"), "9d62912");
  assert.equal(versionOf("9d62912abcde+dirty-0123"), "9d62912+dirty");
  assert.equal(versionOf("dev-abc"), "dev");
  assert.equal(versionOf(""), "");
  assert.equal(displayName(" Ann ", "bob"), "Ann");
  assert.equal(displayName("", "bob"), "bob");
  assert.equal(displayName("", ""), "guest");
  assert.equal(vaultModeText({ mode: "keychain" }).label, "OS keychain");
  assert.match(vaultModeText({ mode: "passphrase", keyringOff: true }).help, /KEEL_KEYRING=off/);
  assert.equal(signalText("").value, "Not configured");
  assert.equal(signalText("https://x.example").value, "https://x.example");
  assert.match(stopMessage(3), /all 3 running sessions/);
  assert.match(stopMessage(1), /1 running session/);
});

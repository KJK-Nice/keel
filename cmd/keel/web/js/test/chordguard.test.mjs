import { test } from "node:test";
import assert from "node:assert/strict";
import { createChordGuard } from "../lib/chordguard.js";

// Plays term.js's wiring: a chord's keydown is keyboard(name); any other
// keydown (not a repeat), a keyup and a mousedown are input(); a held key's
// repeat is neither; the native menu is menu(name), and runs the action
// when it returns true.
function harness() {
  const g = createChordGuard();
  const ran = [];
  return {
    ran,
    pending: () => g.pending(),
    key(name) {
      g.keyboard(name);
      ran.push(`keyboard ${name}`);
    },
    otherKey: () => g.input(),
    repeat() {
      /* a held key's repeat: runs nothing, clears nothing */
    },
    keyup: () => g.input(),
    mousedown: () => g.input(),
    menu(name) {
      if (g.menu(name)) ran.push(`menu ${name}`);
    },
  };
}

test("a menu echo of the chord the keyboard just ran is dropped", () => {
  const h = harness();
  h.key("focus-pane");
  h.menu("focus-pane");
  assert.deepEqual(h.ran, ["keyboard focus-pane"]);
  assert.deepEqual(h.pending(), []);
});

test("an echo that arrives after the held key's repeats is still dropped", () => {
  const h = harness();
  h.key("focus-pane");
  h.repeat();
  h.repeat();
  h.menu("focus-pane");
  assert.deepEqual(h.ran, ["keyboard focus-pane"]);
});

test("a deliberate menu click after a keyboard use runs", () => {
  const h = harness();
  h.key("focus-pane");
  h.keyup(); // releasing ⌘ / ⇧ before reaching for the menu
  h.menu("focus-pane");
  assert.deepEqual(h.ran, ["keyboard focus-pane", "menu focus-pane"]);

  const m = harness();
  m.key("split-right");
  m.mousedown();
  m.menu("split-right");
  assert.deepEqual(m.ran, ["keyboard split-right", "menu split-right"]);
});

test("typing after a chord ends it: a later menu call runs", () => {
  const h = harness();
  h.key("focus-pane");
  h.otherKey();
  h.menu("focus-pane");
  assert.deepEqual(h.ran, ["keyboard focus-pane", "menu focus-pane"]);
});

test("only one echo is dropped per press: a second menu call runs", () => {
  const h = harness();
  h.key("zoom-in");
  h.menu("zoom-in");
  h.menu("zoom-in");
  assert.deepEqual(h.ran, ["keyboard zoom-in", "menu zoom-in"]);
});

test("a menu call for another action runs, and the pending echo stays dropped", () => {
  const h = harness();
  h.key("focus-pane");
  h.menu("split-right");
  h.menu("focus-pane");
  assert.deepEqual(h.ran, ["keyboard focus-pane", "menu split-right"]);
});

test("two different chords in a row both run, and a late echo of either is dropped", () => {
  const h = harness();
  // ⌘⇧D then ⌘⇧↵ with ⌘⇧ held: two chord keydowns, no keyup between.
  h.otherKey(); // ⌘ down
  h.key("split-right");
  h.key("focus-pane");
  h.menu("split-right");
  h.menu("focus-pane");
  assert.deepEqual(h.ran, ["keyboard split-right", "keyboard focus-pane"]);
});

test("two presses of the same chord are two actions", () => {
  const h = harness();
  h.key("focus-pane");
  h.key("focus-pane"); // ⌘⇧ held, ↵ pressed twice
  h.keyup();
  h.key("focus-pane");
  assert.deepEqual(h.ran, ["keyboard focus-pane", "keyboard focus-pane", "keyboard focus-pane"]);
});

test("close-pane never runs twice from one press", () => {
  const h = harness();
  h.key("close-tab");
  h.repeat();
  h.menu("close-tab");
  assert.deepEqual(h.ran, ["keyboard close-tab"]);
  // A second, real press still closes the next pane, once.
  h.keyup();
  h.key("close-tab");
  h.menu("close-tab");
  assert.deepEqual(h.ran, ["keyboard close-tab", "keyboard close-tab"]);
});

test("a menu click with no keyboard use runs every time", () => {
  const h = harness();
  h.menu("focus-pane");
  h.menu("focus-pane");
  assert.deepEqual(h.ran, ["menu focus-pane", "menu focus-pane"]);
});

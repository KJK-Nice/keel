import { test } from "node:test";
import assert from "node:assert/strict";
import { createKeyRouter } from "../ui/keyrouter.js";

const key = (k, extra = {}) => {
  const ev = { key: k, code: "", metaKey: true, ctrlKey: false, shiftKey: false, altKey: false, repeat: false, target: null, prevented: false, ...extra };
  ev.preventDefault = () => (ev.prevented = true);
  return ev;
};

function setup(over = {}) {
  const ran = [];
  const state = { gate: false, modal: false, find: false, start: false, renaming: false, native: false };
  const storage = new Map();
  const toasts = [];
  const r = createKeyRouter({
    mac: true,
    native: () => state.native,
    ctx: {
      gateOpen: () => state.gate,
      modalOpen: () => state.modal,
      findOpen: () => state.find,
      startPage: () => state.start,
      renaming: () => state.renaming,
    },
    run: (name) => ran.push(name),
    find: (dir) => ran.push(`find:${dir}`),
    tabIndex: (i) => ran.push(`tab:${i}`),
    recentHost: (i) => ran.push(`recent:${i}`),
    storage: { getItem: (k) => storage.get(k) ?? null, setItem: (k, v) => storage.set(k, v), removeItem: (k) => storage.delete(k) },
    toast: (m) => toasts.push(m),
    ...over,
  });
  return { r, ran, state, storage, toasts };
}

test("a chord runs its action once and is taken", () => {
  const { r, ran } = setup();
  const ev = key("t");
  assert.equal(r.handle(ev), true);
  assert.equal(ev.prevented, true);
  assert.deepEqual(ran, ["new-tab"]);
  assert.match(r.log.at(-1), /keyboard new-tab repeat=false ran/);
});

test("⌘1…9, find next and the New tab page's ⌥ digits go to their own handlers", () => {
  const { r, ran, state } = setup();
  r.handle(key("3"));
  state.find = true;
  r.handle(key("g"));
  r.handle(key("G", { shiftKey: true }));
  state.start = true;
  r.handle(key("¡", { metaKey: false, altKey: true, code: "Digit1" }));
  assert.deepEqual(ran, ["tab:3", "find:next", "find:prev", "recent:1"]);
});

test("a key that is no chord goes on to the terminal", () => {
  const { r, ran } = setup();
  const ev = key("a", { metaKey: false });
  assert.equal(r.handle(ev), false);
  assert.equal(ev.prevented, false);
  assert.deepEqual(ran, []);
});

test("a held chord's repeat runs nothing and goes nowhere else", () => {
  const { r, ran } = setup();
  const ev = key("Enter", { shiftKey: true, repeat: true });
  assert.equal(r.handle(ev), true);
  assert.equal(ev.prevented, true);
  assert.deepEqual(ran, []);
  assert.match(r.log.at(-1), /focus-pane repeat=true repeat skipped/);
});

test("the gate and a tab rename decline chords; the native window keeps them from the menu", () => {
  const { r, ran, state } = setup();
  state.gate = true;
  const ev = key("t");
  assert.equal(r.handle(ev), false);
  assert.equal(ev.prevented, false);
  state.native = true;
  const native = key("t");
  assert.equal(r.handle(native), false);
  assert.equal(native.prevented, true, "the menu's accelerator must not run it instead");
  state.gate = false;
  r.handle(key("t", { target: { closest: (sel) => (sel === ".tab-rename" ? {} : null) } }));
  assert.deepEqual(ran, []);
  assert.match(r.log.at(-1), /new-tab repeat=false declined/);
});

test("a menu echo of a chord the keyboard ran is dropped; after other input it runs", () => {
  const { r, ran } = setup();
  r.handle(key("Enter", { shiftKey: true }));
  r.menu("focus-pane");
  assert.deepEqual(ran, ["focus-pane"]);
  assert.match(r.log.at(-1), /menu focus-pane repeat=false echo dropped/);
  r.menu("focus-pane");
  assert.deepEqual(ran, ["focus-pane", "focus-pane"], "a second menu call is the user's own");
  r.handle(key("t"));
  r.input();
  r.menu("new-tab");
  assert.deepEqual(ran, ["focus-pane", "focus-pane", "new-tab", "new-tab"]);
});

test("menu items keep the keyboard's limits", () => {
  const { r, ran, state } = setup();
  state.modal = true;
  r.menu("new-tab");
  r.menu("close-tab");
  state.modal = false;
  state.renaming = true;
  r.menu("settings");
  r.menu("stop-runtime");
  assert.deepEqual(ran, ["close-tab", "stop-runtime"]);
});

test("the debug log is off by default, saved when toggled, and keeps 50 lines", () => {
  const { r, storage, toasts } = setup();
  assert.equal(r.debugging(), false);
  r.toggleDebug();
  assert.equal(r.debugging(), true);
  assert.equal(storage.get("keel-debug-keys"), "1");
  const info = console.info;
  console.info = () => {};
  try {
    for (let i = 0; i < 60; i++) r.handle(key("t"));
  } finally {
    console.info = info;
  }
  assert.equal(r.log.length, 50);
  assert.ok(toasts.includes("keyboard new-tab repeat=false ran"));
  r.toggleDebug();
  assert.equal(storage.has("keel-debug-keys"), false);
  assert.equal(setup({ debugParam: true }).r.debugging(), true, "?debug=keys turns it on");
});

test("storage that throws leaves the log off", () => {
  const { r } = setup({ storage: { getItem: () => { throw new Error("blocked"); } } });
  assert.equal(r.debugging(), false);
});

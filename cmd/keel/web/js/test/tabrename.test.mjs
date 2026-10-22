import { test } from "node:test";
import assert from "node:assert/strict";
import { createTabRename } from "../ui/tabrename.js";

globalThis.CSS ??= { escape: (s) => s };

class Input {
  constructor() {
    Object.assign(this, { value: "", style: {}, attrs: {}, listeners: {}, scrollWidth: 40, focused: false });
  }
  setAttribute(k, v) {
    this.attrs[k] = v;
  }
  addEventListener(type, fn) {
    this.listeners[type] = fn;
  }
  focus() {
    this.focused = true;
  }
  select() {}
  blur() {
    this.listeners.blur?.();
  }
  setRangeText(text, start, end) {
    this.value = this.value.slice(0, start) + text + this.value.slice(end);
  }
}

function setup() {
  let replaced = null;
  const label = { replaceWith: (el) => (replaced = el) };
  const btn = { querySelector: () => label, addEventListener() {} };
  const strip = { querySelector: () => btn };
  const doc = { createElement: () => new Input() };
  const applied = [];
  let renders = 0;
  const r = createTabRename({
    doc,
    strip,
    title: (id) => `Tab ${id}`,
    canRename: (id) => id !== "page",
    apply: (id, value, ok) => applied.push([id, value, ok]),
    done: () => renders++,
  });
  return { r, applied, input: () => replaced, renders: () => renders, strip };
}

const key = (k) => ({ key: k, code: k === " " ? "Space" : k, preventDefault() {}, stopPropagation() {} });

test("Enter keeps the typed name, once", () => {
  const s = setup();
  s.r.begin("a");
  const input = s.input();
  assert.equal(input.value, "Tab a");
  assert.equal(input.focused, true);
  assert.equal(input.attrs["aria-label"], "Rename tab");
  assert.equal(s.r.editing(), "a");
  input.value = "prod";
  input.listeners.keydown(key("Enter"));
  input.blur();
  assert.deepEqual(s.applied, [["a", "prod", true]]);
  assert.equal(s.renders(), 1);
  assert.equal(s.r.editing(), null);
});

test("Escape puts the old name back; Space types a space", () => {
  const s = setup();
  s.r.begin("a");
  const input = s.input();
  input.value = "ab";
  input.selectionStart = input.selectionEnd = 1;
  input.listeners.keydown(key(" "));
  assert.equal(input.value, "a b");
  input.listeners.keydown(key("Escape"));
  assert.deepEqual(s.applied, [["a", "a b", false]]);
});

test("a tab page is not renamed, and only one rename at a time", () => {
  const s = setup();
  s.r.begin("page");
  assert.equal(s.r.editing(), null);
  s.r.begin("a");
  const first = s.input();
  s.r.begin("b");
  assert.equal(s.input(), first);
  s.r.keep();
  assert.deepEqual(s.applied, [["a", "Tab a", true]]);
});

test("a closing tab's rename is forgotten and does not end a later one", () => {
  const s = setup();
  s.r.begin("a");
  const old = s.input();
  s.r.forget("a");
  assert.equal(s.r.editing(), null);
  s.r.begin("b");
  old.blur();
  assert.equal(s.r.editing(), "b", "the old field's blur leaves b's rename open");
});

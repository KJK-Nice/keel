import { test } from "node:test";
import assert from "node:assert/strict";
import { createFenceMenu } from "../ui/fencemenu.js";

globalThis.CustomEvent ??= class {
  constructor(type, init) {
    this.type = type;
    this.detail = init?.detail;
  }
};

// A fence with its bar (language, Copy, Type), the menu content, and a doc.
function setup({ typeHidden = false, typeLabel = "Type into web" } = {}) {
  const clicks = [];
  const btn = (name, extra = {}) => ({ name, hidden: false, disabled: false, textContent: name, click: () => clicks.push(name), ...extra });
  const typeBtn = btn("type", { hidden: typeHidden, textContent: ` ${typeLabel} ` });
  const copyBtn = btn("copy");
  const editor = { id: "editor" };
  const fence = {
    isConnected: true,
    querySelector: (sel) => ({ "[data-notebook-fence]": typeBtn, "[data-notebook-copy]": copyBtn })[sel] || null,
    closest: (sel) => (sel === "[data-notebook-editor]" ? editor : null),
  };
  const bar = { closest: (sel) => (sel === ".notebook-fence" ? fence : null) };
  // An element inside the bar: closest(".notebook-fence-bar") finds the bar.
  const inBar = (extra = {}) => ({ closest: (sel) => (sel === ".notebook-fence-bar" ? bar : extra[sel] || null) });
  const label = { textContent: "" };
  const typeItem = { hidden: false, querySelector: (sel) => (sel === "[data-fence-type-label]" ? label : null) };
  const content = { querySelector: (sel) => (sel === '[data-fence-act="type"]' ? typeItem : null) };
  const dispatched = [];
  const trigger = { getAttribute: () => "fence-menu", dispatchEvent: (ev) => dispatched.push(ev) };
  const listeners = {};
  const doc = {
    activeElement: null,
    getElementById: (id) => ({ "fence-menu-trigger": trigger, "fence-menu": content })[id] || null,
    addEventListener: (type, fn, capture) => (listeners[type] = { fn, capture: !!capture }),
  };
  const menu = createFenceMenu({ doc });
  return { menu, fence, typeBtn, typeItem, label, dispatched, clicks, listeners, doc, inBar, editor };
}

const ev = (over) => ({ prevented: false, stopped: false, preventDefault() { this.prevented = true; }, stopPropagation() { this.stopped = true; }, ...over });

test("open names the fence's own Type, and hides it when the fence has none", () => {
  const s = setup();
  s.menu.open(s.fence, { x: 1, y: 2 });
  assert.equal(s.typeItem.hidden, false);
  assert.equal(s.label.textContent, "Type into web…");
  assert.equal(s.dispatched[0].type, "tui-dropdownmenu-open-at");
  const plain = setup({ typeHidden: true });
  plain.menu.open(plain.fence, {});
  assert.equal(plain.typeItem.hidden, true);
});

test("run clicks the fence's own buttons, so Type still asks approval", () => {
  const s = setup();
  s.menu.open(s.fence, {});
  s.menu.run("type");
  s.menu.run("copy");
  assert.deepEqual(s.clicks, ["type", "copy"]);
  s.typeBtn.disabled = true;
  s.menu.run("type");
  assert.deepEqual(s.clicks, ["type", "copy"], "a disabled Type is not clicked");
  s.fence.isConnected = false;
  s.menu.run("copy");
  assert.deepEqual(s.clicks, ["type", "copy"], "a fence that left the page is not clicked");
});

test("a right-click on the bar opens it; ⇧ or a select leaves it to the system", () => {
  const s = setup();
  s.menu.bind();
  assert.equal(s.listeners.keydown.capture, true);
  const right = ev({ target: s.inBar(), clientX: 5, clientY: 6 });
  s.listeners.contextmenu.fn(right);
  assert.equal(right.prevented, true);
  assert.deepEqual(s.dispatched[0].detail, { x: 5, y: 6, returnFocus: s.editor, focusFirst: false });
  const shifted = ev({ target: s.inBar(), shiftKey: true });
  s.listeners.contextmenu.fn(shifted);
  const onSelect = ev({ target: s.inBar({ select: {} }) });
  s.listeners.contextmenu.fn(onSelect);
  const elsewhere = ev({ target: { closest: () => null } });
  s.listeners.contextmenu.fn(elsewhere);
  assert.deepEqual([shifted.prevented, onSelect.prevented, elsewhere.prevented], [false, false, false]);
  assert.equal(s.dispatched.length, 1);
});

test("⇧F10 on a focused bar control opens it below that control", () => {
  const s = setup();
  s.menu.bind();
  const control = s.inBar();
  s.doc.activeElement = control;
  const key = ev({ type: "keydown", key: "F10", shiftKey: true });
  s.listeners.keydown.fn(key);
  assert.equal(key.prevented && key.stopped, true);
  assert.deepEqual(s.dispatched[0].detail, { anchor: control, returnFocus: control, focusFirst: true });
  s.doc.activeElement = { closest: () => null };
  s.listeners.keydown.fn(ev({ type: "keydown", key: "F10", shiftKey: true }));
  assert.equal(s.dispatched.length, 1, "not from outside a fence bar");
});

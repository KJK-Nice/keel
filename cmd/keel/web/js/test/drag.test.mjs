import { test } from "node:test";
import assert from "node:assert/strict";
import { createDrag, dropEdgeForRect } from "../ui/drag.js";

globalThis.CSS ??= { escape: (s) => s };

class ClassList {
  constructor() {
    this.set = new Set();
  }
  add(...c) {
    c.forEach((x) => this.set.add(x));
  }
  remove(...c) {
    c.forEach((x) => this.set.delete(x));
  }
  contains(c) {
    return this.set.has(c);
  }
}
class El {
  constructor(rect = { left: 0, top: 0, right: 0, bottom: 0, width: 0, height: 0 }, data = {}) {
    Object.assign(this, { rect, dataset: data, classList: new ClassList(), attrs: {}, listeners: {}, style: {}, isConnected: true, hidden: false });
  }
  getBoundingClientRect() {
    return this.rect;
  }
  setAttribute(k, v) {
    this.attrs[k] = v;
  }
  removeAttribute(k) {
    delete this.attrs[k];
  }
  addEventListener(type, fn) {
    (this.listeners[type] ||= []).push(fn);
  }
  setPointerCapture() {}
}
const box = (left, top, width, height) => ({ left, top, width, height, right: left + width, bottom: top + height });

function setup() {
  // Strip at y 0..30 with tabs t1 [0,100), t2 [100,200); one shown tab with
  // panes a [0,300) x [40,400) and b [300,600) x [40,400).
  const tabs = [new El(box(0, 0, 100, 30), { tab: "t1" }), new El(box(100, 0, 100, 30), { tab: "t2" })];
  const strip = new El(box(0, 0, 600, 30));
  strip.scrollLeft = 0;
  strip.closest = () => null;
  strip.querySelector = (sel) => tabs.find((t) => sel.includes(`"${t.dataset.tab}"`)) || null;
  strip.querySelectorAll = (sel) => (sel === ".tab[data-tab]" ? tabs : tabs.filter((t) => [...t.classList.set].some((c) => sel.includes(c))));
  const wraps = { a: new El(box(0, 40, 300, 360)), b: new El(box(300, 40, 300, 360)) };
  const terminals = new El();
  const docListeners = {};
  const body = new El();
  body.appendChild = () => {};
  const doc = { body, createElement: () => new El(), addEventListener: (type, fn) => (docListeners[type] ||= []).push(fn) };
  const timers = [];
  const win = { setTimeout: (fn) => timers.push(fn) };
  const acts = [];
  const record = (name) => (...args) => acts.push([name, ...args]);
  const d = createDrag({
    doc,
    win,
    strip,
    terminals,
    shownPanes: () => [
      { id: "a", tabId: "t1", wrap: wraps.a },
      { id: "b", tabId: "t1", wrap: wraps.b },
    ],
    canDragTab: (id) => id === "t1" || id === "t2",
    canDragPane: (id) => id in wraps,
    paneWrap: (id) => wraps[id],
    paneSplits: () => true,
    paneDragStart: record("paneDragStart"),
    act: { moveTab: record("moveTab"), dockTab: record("dockTab"), extractPane: record("extractPane"), movePane: record("movePane") },
  });
  d.bind();
  const fire = (target, type, ev) => target.listeners[type].forEach((fn) => fn(ev));
  const docFire = (type, ev) => docListeners[type].forEach((fn) => fn(ev));
  const ev = (x, y, extra = {}) => ({ clientX: x, clientY: y, pointerId: 1, button: 0, preventDefault() {}, ...extra });
  const onTab = (id) => ({ closest: (sel) => (sel === ".tab[data-tab]" ? tabs.find((t) => t.dataset.tab === id) : null) });
  const onBar = (id) => {
    const wrap = { dataset: { paneId: id } };
    const bar = new El();
    bar.closest = () => wrap;
    return { closest: (sel) => (sel === ".pane-bar" ? bar : null) };
  };
  return { d, acts, fire, docFire, ev, onTab, onBar, strip, terminals, tabs, timers, body };
}

test("dropEdgeForRect: the nearest edge, or center well inside", () => {
  const r = box(0, 0, 100, 100);
  assert.equal(dropEdgeForRect(r, 5, 50), "left");
  assert.equal(dropEdgeForRect(r, 95, 50), "right");
  assert.equal(dropEdgeForRect(r, 50, 5), "top");
  assert.equal(dropEdgeForRect(r, 50, 95), "bottom");
  assert.equal(dropEdgeForRect(r, 50, 50), "center");
});

test("a tab dropped on the strip moves there and its click is swallowed", () => {
  const s = setup();
  s.fire(s.strip, "pointerdown", s.ev(10, 10, { target: s.onTab("t1") }));
  s.docFire("pointermove", s.ev(180, 10));
  assert.ok(s.body.classList.contains("is-tab-dragging"));
  assert.ok(s.tabs[0].classList.contains("is-dragging"));
  s.docFire("pointerup", s.ev(180, 10));
  assert.deepEqual(s.acts, [["moveTab", "t1", 2]]);
  assert.equal(s.d.ignoreClick(), true);
  s.timers.shift()();
  assert.equal(s.d.ignoreClick(), false);
});

test("a press that does not move is a click, not a drag", () => {
  const s = setup();
  s.fire(s.strip, "pointerdown", s.ev(10, 10, { target: s.onTab("t1") }));
  s.docFire("pointermove", s.ev(13, 12));
  s.docFire("pointerup", s.ev(13, 12));
  assert.deepEqual(s.acts, []);
  assert.equal(s.d.ignoreClick(), false);
});

test("a tab dropped on a pane edge docks there; its own tab's panes are not a target", () => {
  const s = setup();
  s.fire(s.strip, "pointerdown", s.ev(150, 10, { target: s.onTab("t2") }));
  s.docFire("pointermove", s.ev(590, 200));
  s.docFire("pointerup", s.ev(590, 200));
  assert.deepEqual(s.acts, [["dockTab", "t2", "b", "right"]]);
  s.acts.length = 0;
  s.fire(s.strip, "pointerdown", s.ev(10, 10, { target: s.onTab("t1") }));
  s.docFire("pointermove", s.ev(590, 200));
  s.docFire("pointerup", s.ev(590, 200));
  assert.deepEqual(s.acts, []);
});

test("Esc drops a tab drag; a moved one swallows its click", () => {
  const s = setup();
  s.fire(s.strip, "pointerdown", s.ev(10, 10, { target: s.onTab("t1") }));
  s.docFire("pointermove", s.ev(180, 10));
  assert.equal(s.d.cancel(), true);
  assert.equal(s.d.ignoreClick(), true);
  assert.equal(s.body.classList.contains("is-tab-dragging"), false);
  s.docFire("pointerup", s.ev(180, 10));
  assert.deepEqual(s.acts, []);
  assert.equal(s.d.cancel(), false, "nothing left to cancel");
});

test("a pane dragged by its bar onto another pane moves next to it; onto the strip it becomes a tab", () => {
  const s = setup();
  s.fire(s.terminals, "pointerdown", s.ev(100, 50, { target: s.onBar("a") }));
  s.docFire("pointermove", s.ev(450, 50));
  s.docFire("pointerup", s.ev(450, 50));
  assert.deepEqual(s.acts, [["paneDragStart", "a"], ["movePane", "a", "b", "top"]]);
  s.acts.length = 0;
  s.fire(s.terminals, "pointerdown", s.ev(100, 50, { target: s.onBar("a") }));
  s.docFire("pointermove", s.ev(20, 10));
  s.docFire("pointerup", s.ev(20, 10));
  assert.deepEqual(s.acts, [["paneDragStart", "a"], ["extractPane", "a", 0]]);
});

test("a press with a modifier or on a button starts no drag", () => {
  const s = setup();
  s.fire(s.strip, "pointerdown", s.ev(10, 10, { target: s.onTab("t1"), metaKey: true }));
  const onButton = { closest: (sel) => (sel === "button" ? {} : null) };
  s.fire(s.terminals, "pointerdown", s.ev(100, 50, { target: onButton }));
  s.docFire("pointermove", s.ev(180, 10));
  s.docFire("pointerup", s.ev(180, 10));
  assert.deepEqual(s.acts, []);
});

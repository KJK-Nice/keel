import { test } from "node:test";
import assert from "node:assert/strict";
import { createTabMenu } from "../ui/tabmenu.js";

class El {
  constructor(attrs = {}) {
    Object.assign(this, { hidden: false, disabled: false, attrs: {} }, attrs);
  }
  setAttribute(k, v) {
    this.attrs[k] = v;
  }
  removeAttribute(k) {
    delete this.attrs[k];
  }
  getAttribute(k) {
    return this.attrs[k] ?? null;
  }
  hasAttribute(k) {
    return k in this.attrs;
  }
}

function setup(ids = ["a", "b", "c"], pages = []) {
  const items = {};
  for (const a of ["rename", "close", "close-others", "close-right", "close-all"]) items[`[data-tab-action="${a}"]`] = new El({ dataset: { tabAction: a } });
  items['[data-tab-menu-sep="rename"]'] = new El();
  const listeners = {};
  const doc = {
    querySelector: (sel) => items[sel] || null,
    getElementById: () => null,
    addEventListener: (type, fn) => (listeners[type] = fn),
  };
  const frames = [];
  const win = { requestAnimationFrame: (fn) => frames.push(fn), Element: El, MouseEvent: class {} };
  const calls = [];
  const menu = createTabMenu({
    doc,
    win,
    tabIds: () => ids,
    isPage: (id) => pages.includes(id),
    rename: (id) => calls.push(["rename", id]),
    close: (list) => calls.push(["close", list]),
  });
  const item = (a) => items[`[data-tab-action="${a}"]`];
  return { menu, item, items, calls, frames, listeners };
}

test("sync enables what makes sense for the tab", () => {
  const s = setup();
  s.menu.sync("c");
  assert.equal(s.item("close-right").disabled, true, "nothing to the right of the last tab");
  assert.equal(s.item("close-right").getAttribute("aria-disabled"), "true");
  assert.equal(s.item("close-others").disabled, false);
  assert.equal(s.item("rename").hidden, false);
  s.menu.sync("a");
  assert.equal(s.item("close-right").disabled, false);
  assert.equal(s.item("close-right").getAttribute("aria-disabled"), null);
});

test("a tab page keeps its name: no Rename", () => {
  const s = setup(["a", "page"], ["page"]);
  s.menu.sync("page");
  assert.equal(s.item("rename").hidden, true);
  assert.equal(s.item("rename").disabled, true);
  assert.equal(s.items['[data-tab-menu-sep="rename"]'].hidden, true);
});

test("a lone tab has no others to close", () => {
  const s = setup(["a"]);
  s.menu.sync("a");
  assert.equal(s.item("close-others").disabled, true);
  assert.equal(s.item("close-all").disabled, false);
});

test("the items act on the tab the menu was opened on", () => {
  const s = setup();
  s.menu.sync("b");
  s.menu.run("close-others");
  s.menu.run("close-right");
  s.menu.run("close-all");
  s.menu.run("close");
  assert.deepEqual(s.calls, [
    ["close", ["a", "c"]],
    ["close", ["c"]],
    ["close", ["a", "b", "c"]],
    ["close", ["b"]],
  ]);
});

test("Rename waits a frame for the menu to close", () => {
  const s = setup();
  s.menu.sync("b");
  s.menu.run("rename");
  assert.deepEqual(s.calls, []);
  s.frames.shift()();
  assert.deepEqual(s.calls, [["rename", "b"]]);
});

test("a click on a disabled item does nothing; the space beside the tabs has no menu", () => {
  const s = setup();
  s.menu.bind();
  s.menu.sync("c");
  const target = (el) => Object.assign(new El(), { closest: (sel) => (sel === "[data-tab-action]" ? el : null) });
  s.listeners.click({ target: target(s.item("close-right")) });
  assert.deepEqual(s.calls, []);
  s.listeners.click({ target: target(s.item("close")) });
  assert.deepEqual(s.calls, [["close", ["c"]]]);
  let prevented = false;
  const beside = Object.assign(new El(), { closest: (sel) => (sel === "#tab-ctx-trigger" ? {} : null) });
  s.listeners.contextmenu({ target: beside, preventDefault: () => (prevented = true), stopPropagation() {} });
  assert.equal(prevented, true);
});

import { test } from "node:test";
import assert from "node:assert/strict";
import { pinScroll } from "../lib/scrollpin.js";

function fakeDoc() {
  const listeners = [];
  const doc = {
    scrollingElement: { scrollTop: 0, scrollLeft: 0 },
    addEventListener(type, fn, opts) {
      assert.equal(type, "scroll");
      assert.equal(opts.capture, true, "scroll does not bubble; must capture");
      listeners.push(fn);
    },
    removeEventListener(type, fn) {
      const i = listeners.indexOf(fn);
      if (i >= 0) listeners.splice(i, 1);
    },
    fire(target) {
      for (const fn of listeners) fn({ target });
    },
    listeners,
  };
  return doc;
}

test("pinScroll snaps pinned elements back to the top", () => {
  const doc = fakeDoc();
  const main = { scrollTop: 0, scrollLeft: 0 };
  pinScroll(doc, [doc.scrollingElement, main]);
  main.scrollTop = 42;
  main.scrollLeft = 3;
  doc.fire(main);
  assert.deepEqual([main.scrollTop, main.scrollLeft], [0, 0]);

  doc.scrollingElement.scrollTop = 30;
  doc.fire(doc); // viewport scroll targets the document
  assert.equal(doc.scrollingElement.scrollTop, 0);
});

test("pinScroll leaves real scrollers alone", () => {
  const doc = fakeDoc();
  const main = { scrollTop: 0, scrollLeft: 0 };
  const list = { scrollTop: 120, scrollLeft: 0 };
  pinScroll(doc, [main]);
  doc.fire(list);
  assert.equal(list.scrollTop, 120);
});

test("pinScroll resets an already scrolled element and can be removed", () => {
  const doc = fakeDoc();
  const main = { scrollTop: 9, scrollLeft: 0 };
  const off = pinScroll(doc, [main, null]);
  assert.equal(main.scrollTop, 0);
  off();
  assert.equal(doc.listeners.length, 0);
  main.scrollTop = 5;
  doc.fire(main);
  assert.equal(main.scrollTop, 5);
});

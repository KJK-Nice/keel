import { test } from "node:test";
import assert from "node:assert/strict";
import { setHTML, setText } from "../lib/dom.js";

function fakeEl() {
  let writes = 0;
  let text = "";
  let html = "";
  return {
    get writes() {
      return writes;
    },
    get textContent() {
      return text;
    },
    set textContent(v) {
      writes++;
      text = v;
    },
    get innerHTML() {
      return html;
    },
    set innerHTML(v) {
      writes++;
      html = v;
    },
    get childNodes() {
      return html ? [1] : [];
    },
  };
}

test("setText writes only when the text changes", () => {
  const el = fakeEl();
  assert.equal(setText(el, "a"), true);
  assert.equal(setText(el, "a"), false);
  assert.equal(setText(el, "b"), true);
  assert.equal(el.writes, 2);
  assert.equal(setText(null, "x"), false);
});

test("setHTML skips identical markup but rewrites after someone cleared it", () => {
  const el = fakeEl();
  assert.equal(setHTML(el, "<b>1</b>"), true);
  assert.equal(setHTML(el, "<b>1</b>"), false);
  el.innerHTML = "";
  assert.equal(setHTML(el, "<b>1</b>"), true, "emptied element is repainted");
  assert.equal(setHTML(el, "<b>2</b>"), true);
});

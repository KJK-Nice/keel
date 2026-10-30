import { test } from "node:test";
import assert from "node:assert/strict";
import { clampWidth, dragWidth, keyWidth, savedWidth } from "../lib/sidebarwidth.js";

// The tokens' values: size-rail 208, size-sidebar 252 (max is twice it),
// space-4 16.
const b = { min: 208, max: 504, def: 252, step: 16 };

test("clampWidth rounds and holds the width in bounds", () => {
  assert.equal(clampWidth(300.4, b), 300);
  assert.equal(clampWidth(100, b), 208);
  assert.equal(clampWidth(9000, b), 504);
});

test("savedWidth keeps a number and drops anything else to the default", () => {
  assert.equal(savedWidth(320, b), 320);
  assert.equal(savedWidth("320", b), 320);
  assert.equal(savedWidth(50, b), 208);
  for (const raw of [null, undefined, "", "wide", NaN, Infinity, {}]) assert.equal(savedWidth(raw, b), null, String(raw));
});

test("dragWidth follows the pointer within bounds", () => {
  assert.equal(dragWidth(252, 252, 300, b), 300);
  assert.equal(dragWidth(252, 252, 200, b), 208);
  assert.equal(dragWidth(252, 252, 900, b), 504);
});

test("keyWidth steps, jumps to the bounds and resets", () => {
  assert.equal(keyWidth("ArrowRight", 252, b), 268);
  assert.equal(keyWidth("ArrowLeft", 252, b), 236);
  assert.equal(keyWidth("ArrowLeft", 210, b), 208);
  assert.equal(keyWidth("Home", 300, b), 208);
  assert.equal(keyWidth("End", 300, b), 504);
  assert.equal(keyWidth("Enter", 300, b), null);
  assert.equal(keyWidth("a", 300, b), undefined);
  assert.equal(keyWidth("ArrowUp", 300, b), undefined);
});

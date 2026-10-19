import { test } from "node:test";
import assert from "node:assert/strict";
import { createWebglGuard, webglPlan } from "../lib/webglguard.js";

test("a new pane may use WebGL", () => {
  const g = createWebglGuard();
  assert.equal(g.allowed(), true);
  assert.equal(g.reason(), "");
});

test("a context that cannot be created falls back to the DOM renderer at once", () => {
  const g = createWebglGuard();
  const why = g.failed(new Error("WebGL2 not supported"));
  assert.match(why, /WebGL2 not supported/);
  assert.equal(g.allowed(), false);
  // Only the first failure is reported.
  assert.equal(g.failed(new Error("again")), "");
});

test("a lost context is retried until it is lost maxLosses times in the window", () => {
  let t = 0;
  const g = createWebglGuard({ maxLosses: 3, windowMs: 1000, now: () => t });
  assert.equal(g.lost(), "");
  t = 100;
  assert.equal(g.lost(), "");
  assert.equal(g.allowed(), true);
  t = 200;
  assert.match(g.lost(), /lost 3 times/);
  assert.equal(g.allowed(), false);
});

test("losses spread wider than the window keep WebGL", () => {
  let t = 0;
  const g = createWebglGuard({ maxLosses: 3, windowMs: 1000, now: () => t });
  for (let i = 0; i < 10; i++) {
    assert.equal(g.lost(), "");
    t += 600;
  }
  assert.equal(g.allowed(), true);
});

test("plan: every visible allowed pane holds WebGL, split or solo", () => {
  const plan = webglPlan([
    { id: "a", visible: true, allowed: true, attached: true },
    { id: "b", visible: true, allowed: true, attached: false },
    { id: "c", visible: true, allowed: true, attached: false },
  ]);
  assert.deepEqual(plan, { attach: ["b", "c"], drop: [] });
});

test("plan: panes off screen give their context back", () => {
  const plan = webglPlan([
    { id: "zoomed", visible: true, allowed: true, attached: true },
    { id: "behind", visible: false, allowed: true, attached: true },
    { id: "other-tab", visible: false, allowed: true, attached: true },
    { id: "parked", visible: false, allowed: true, attached: false },
  ]);
  assert.deepEqual(plan, { attach: [], drop: ["behind", "other-tab"] });
});

test("plan: a pane that fell back stays on the DOM renderer", () => {
  const plan = webglPlan([
    { id: "a", visible: true, allowed: false, attached: false },
    { id: "b", visible: true, allowed: false, attached: true },
  ]);
  assert.deepEqual(plan, { attach: [], drop: ["b"] });
});

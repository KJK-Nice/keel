import { test } from "node:test";
import assert from "node:assert/strict";
import { createStateFollow } from "../lib/statefollow.js";

test("the stream's first push at boot's rev is skipped", () => {
  const f = createStateFollow();
  f.loaded("ab12.3");
  assert.equal(f.shouldRefresh({ rev: "ab12.3" }), false);
});

test("later pushes always refresh", () => {
  const f = createStateFollow();
  f.loaded("ab12.3");
  f.shouldRefresh({ rev: "ab12.3" });
  assert.equal(f.shouldRefresh({ rev: "" }), true);
  assert.equal(f.shouldRefresh({ rev: "" }), true);
});

test("a reconnect refreshes, even at the same rev", () => {
  const f = createStateFollow();
  f.loaded("ab12.3");
  f.shouldRefresh({ rev: "ab12.3" });
  assert.equal(f.shouldRefresh({ rev: "ab12.3" }), true);
});

test("a change between boot's rev and the stream opening refreshes", () => {
  const f = createStateFollow();
  f.loaded("ab12.3");
  assert.equal(f.shouldRefresh({ rev: "ab12.4" }), true);
});

test("a restarted keel serve (new epoch) refreshes", () => {
  const f = createStateFollow();
  f.loaded("ab12.0");
  assert.equal(f.shouldRefresh({ rev: "cd34.0" }), true);
});

test("without a boot rev every push refreshes", () => {
  const f = createStateFollow();
  assert.equal(f.shouldRefresh({ rev: "ab12.3" }), true);
  f.loaded(undefined);
  assert.equal(f.shouldRefresh({ rev: "" }), true);
  assert.equal(f.shouldRefresh(undefined), true);
  assert.equal(f.shouldRefresh(null), true);
});

test("a second boot (after an unlock) is checked again", () => {
  const f = createStateFollow();
  f.loaded("ab12.3");
  f.shouldRefresh({ rev: "ab12.3" });
  f.loaded("ab12.5");
  assert.equal(f.shouldRefresh({ rev: "ab12.5" }), false);
});

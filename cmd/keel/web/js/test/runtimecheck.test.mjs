import { test } from "node:test";
import assert from "node:assert/strict";
import { runtimeIsStale } from "../lib/runtimecheck.js";

test("a runtime from another build is stale", () => {
  assert.equal(runtimeIsStale("abc-123", "abc-123"), false);
  assert.equal(runtimeIsStale("abc-123", "abc-999"), true);
});

test("a runtime that reports no build predates build ids", () => {
  assert.equal(runtimeIsStale("abc-123", undefined), true);
  assert.equal(runtimeIsStale("abc-123", ""), true);
});

test("a page opened without app_build never warns", () => {
  assert.equal(runtimeIsStale(null, "abc"), false);
  assert.equal(runtimeIsStale("", undefined), false);
});

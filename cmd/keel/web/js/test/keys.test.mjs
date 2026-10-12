import { test } from "node:test";
import assert from "node:assert/strict";
import { tabCycleDelta, tabCycleTarget } from "../lib/keys.js";

test("tabCycleDelta maps Cmd+< / Cmd+> and Cmd+Shift+[ / ] across layouts", () => {
  const cases = [
    [{ key: "<", code: "Comma", shiftKey: true }, -1, "us shift comma"],
    [{ key: ",", code: "Comma", shiftKey: true }, -1, "us shift comma key comma"],
    [{ key: ">", code: "Period", shiftKey: true }, 1, "us shift period"],
    [{ key: ".", code: "Period", shiftKey: true }, 1, "us shift period key dot"],
    [{ key: "<", code: "IntlBackslash", shiftKey: false }, -1, "iso less-than"],
    [{ key: ">", code: "IntlBackslash", shiftKey: true }, 1, "iso greater-than"],
    [{ key: "{", code: "BracketLeft", shiftKey: true }, -1, "mac shift bracket left"],
    [{ key: "[", code: "BracketLeft", shiftKey: true }, -1, "mac shift bracket left key"],
    [{ key: "}", code: "BracketRight", shiftKey: true }, 1, "mac shift bracket right"],
    [{ key: "]", code: "BracketRight", shiftKey: true }, 1, "mac shift bracket right key"],
    [{ key: ",", code: "Comma", shiftKey: false }, 0, "settings comma"],
    [{ key: "[", code: "BracketLeft", shiftKey: false }, 0, "unshifted bracket is not back"],
    [{ key: "\\", code: "IntlBackslash", shiftKey: false }, 0, "uk backslash is not prev tab"],
    [{ key: "+", code: "Equal", shiftKey: true }, 0, "zoom in"],
    [{ key: "t", code: "KeyT", shiftKey: false }, 0, "new tab"],
  ];
  for (const [ev, want, name] of cases) assert.equal(tabCycleDelta(ev), want, name);
});

test("tabCycleTarget wraps and handles edge cases", () => {
  const tabs = ["vault", "a", "b"];
  assert.equal(tabCycleTarget(tabs, "vault", -1), "b", "wrap prev from first");
  assert.equal(tabCycleTarget(tabs, "b", 1), "vault", "wrap next from last");
  assert.equal(tabCycleTarget(tabs, "a", -1), "vault");
  assert.equal(tabCycleTarget(tabs, "a", 1), "b");
  assert.equal(tabCycleTarget(tabs, "gone", 1), "a", "missing id counts from the first tab");
  assert.equal(tabCycleTarget(["vault"], "vault", 1), "vault", "single tab wraps to itself");
  assert.equal(tabCycleTarget([], "vault", 1), "", "empty list");
  assert.equal(tabCycleTarget(["vault", "a"], "a", 0), "", "zero delta");
});

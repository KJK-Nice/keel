import { test } from "node:test";
import assert from "node:assert/strict";
import { parseJoinLink } from "../lib/joinlink.js";

test("parseJoinLink reads a full link", () => {
  assert.deepEqual(parseJoinLink(" https://sig.example/join?room=abc123 "), {
    origin: "https://sig.example",
    room: "abc123",
  });
});

test("parseJoinLink accepts a bare room id with a fallback origin", () => {
  assert.deepEqual(parseJoinLink("0123456789abcdef", "https://sig.example/"), {
    origin: "https://sig.example",
    room: "0123456789abcdef",
  });
});

test("parseJoinLink rejects empty input, links without a room, and bare ids without an origin", () => {
  assert.throws(() => parseJoinLink(""), /paste a Share link/);
  assert.throws(() => parseJoinLink("https://sig.example/join"), /link has no room/);
  assert.throws(() => parseJoinLink("0123456789abcdef"), /paste the full Share link/);
});

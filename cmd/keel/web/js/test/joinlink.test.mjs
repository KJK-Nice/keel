import { test } from "node:test";
import assert from "node:assert/strict";
import { parseJoinLink } from "../lib/joinlink.js";

const KEY = "00112233445566778899aabbccddeeff";

test("parseJoinLink reads a full link with its key", () => {
  assert.deepEqual(parseJoinLink(` https://sig.example/join?room=abc123#k=${KEY} `), {
    origin: "https://sig.example",
    room: "abc123",
    key: KEY,
  });
});

test("parseJoinLink refuses links without a valid key", () => {
  assert.throws(() => parseJoinLink("https://sig.example/join?room=abc123"), /no key/);
  assert.throws(() => parseJoinLink("https://sig.example/join?room=abc123#k=short"), /no key/);
  assert.throws(() => parseJoinLink(`https://sig.example/join?room=abc123#k=${KEY.toUpperCase()}`), /no key/);
});

test("parseJoinLink rejects empty input, links without a room, and bare ids", () => {
  assert.throws(() => parseJoinLink(""), /paste a Share link/);
  assert.throws(() => parseJoinLink(`https://sig.example/join#k=${KEY}`), /link has no room/);
  assert.throws(() => parseJoinLink("0123456789abcdef"), /paste the full Share link/);
});

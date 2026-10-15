import { test } from "node:test";
import assert from "node:assert/strict";
import {
  dispatchSessionMessage,
  parseSessionMessage,
  presenceFields,
  sessionSocketURL,
  statusClass,
} from "../lib/session.js";

test("sessionSocketURL follows the page scheme and fills defaults", () => {
  assert.equal(
    sessionSocketURL({ protocol: "http:", host: "127.0.0.1:47241" }, { runtimeId: "s1", cols: 120, rows: 40, name: "ada" }),
    "ws://127.0.0.1:47241/ws/session?id=s1&cols=120&rows=40&name=ada&kind=web",
  );
  assert.match(sessionSocketURL({ protocol: "https:", host: "h" }, { runtimeId: "s" }), /^wss:\/\/h\/ws\/session\?id=s&cols=80&rows=24&name=guest&kind=web$/);
});

test("statusClass", () => {
  assert.equal(statusClass("ready"), "is-ready");
  assert.equal(statusClass("connecting"), "is-connecting");
  assert.equal(statusClass("exited"), "is-error");
  assert.equal(statusClass("error"), "is-error");
  assert.equal(statusClass("", "keep"), "keep");
});

test("presenceFields keeps only what the message sets", () => {
  assert.deepEqual(presenceFields({ type: "presence", driverId: 0, title: "", you: { id: 2 } }), { driverId: 0, you: { id: 2 } });
});

test("parseSessionMessage rejects junk", () => {
  assert.equal(parseSessionMessage("not json"), null);
  assert.equal(parseSessionMessage("[1]"), null);
  assert.equal(parseSessionMessage('{"x":1}'), null);
  assert.deepEqual(parseSessionMessage('{"type":"ready"}'), { type: "ready" });
});

test("dispatchSessionMessage routes by type", () => {
  const seen = [];
  const h = {};
  for (const k of ["injectChallenge", "joinRequest", "joinResolved", "joinFailed", "status", "presence", "agentType", "agentSuggest", "agentSuggestRisk", "error"]) {
    h[k] = (arg) => seen.push([k, typeof arg === "string" ? arg : arg.type]);
  }
  for (const type of ["inject-challenge", "join-request", "join-resolved", "join-failed", "status", "ready", "presence", "control", "agent-type", "agent-suggest", "agent-suggest-risk", "error"]) {
    assert.ok(dispatchSessionMessage({ type, nonce: "n" }, h), type);
  }
  assert.ok(!dispatchSessionMessage({ type: "mystery" }, h));
  assert.ok(!dispatchSessionMessage(null, h));
  assert.ok(!dispatchSessionMessage({ type: "join-request" }, {}), "a missing handler is skipped");
  assert.deepEqual(seen.map((s) => s[0]), [
    "injectChallenge",
    "joinRequest",
    "joinResolved",
    "joinFailed",
    "status",
    "presence",
    "presence",
    "presence",
    "agentType",
    "agentSuggest",
    "agentSuggestRisk",
    "error",
  ]);
  assert.deepEqual(seen[0], ["injectChallenge", "n"]);
});

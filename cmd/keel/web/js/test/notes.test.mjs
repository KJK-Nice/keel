import { test } from "node:test";
import assert from "node:assert/strict";
import * as N from "../lib/notes.js";
import { traceText, readSummary } from "../lib/agent.js";

test("a note's source line says who wrote it, and from which pane", () => {
  const at = "2026-10-12T09:00:00Z";
  assert.match(N.sourceLabel({ source: "user", createdAt: at }), /^You · Oct 1[12]$/);
  assert.match(N.sourceLabel({ source: "agent", createdAt: at }), /^Proposed by the agent · Oct 1[12] · you approved$/);
  assert.match(
    N.sourceLabel({ source: "agent", fromPane: true, seenIn: "prod-db-02", edited: true, createdAt: at }),
    /^Proposed by the agent from pane prod-db-02 · Oct 1[12] · you edited$/,
  );
  assert.equal(N.sourceLabel({}), "You");
});

test("count line and one-line cleaning like keel serve", () => {
  assert.equal(N.countLabel([]), "0 notes · 0 chars");
  assert.equal(N.countLabel([{ text: "abc" }, { text: "de" }]), "2 notes · 5 chars");
  assert.equal(N.countLabel([{ text: "a" }]), "1 note · 1 char");
  const nl = String.fromCharCode(10);
  const ls = String.fromCodePoint(0x2028);
  const zw = String.fromCodePoint(0x200b);
  assert.deepEqual(N.cleanNote(`  one${nl}two${ls}three${zw}x  `), { text: "one two three x", ok: true, tooLong: false });
  assert.equal(N.cleanNote("   ").ok, false);
  const long = N.cleanNote("é".repeat(501));
  assert.equal(long.ok, false);
  assert.equal(long.tooLong, true);
});

test("host notes and your notes are picked apart", () => {
  const all = [{ id: "1", hostId: "h1" }, { id: "2" }, { id: "3", hostId: "h2" }, { id: "4", hostId: "h1" }];
  assert.deepEqual(N.hostNotes(all, "h1").map((n) => n.id), ["1", "4"]);
  assert.deepEqual(N.hostNotes(all, "").map((n) => n.id), []);
  assert.deepEqual(N.userNotes(all).map((n) => n.id), ["2"]);
});

test("a proposed note says whose notes it joins, where it came from and when it is sent", () => {
  const host = { for: "host", host: "prod-db-01", from: "conversation" };
  assert.equal(N.proposalTarget(host), "Host notes · prod-db-01");
  assert.equal(N.proposalSource(host), "From this conversation · agent summary");
  assert.equal(N.proposalScope(host), "Sent when prod-db-01 is in scope");
  const mine = { for: "user", from: "user" };
  assert.equal(N.proposalTarget(mine), "Your notes · every host");
  assert.equal(N.proposalSource(mine), "From your messages in this conversation");
  assert.match(N.proposalScope(mine), /^Sent with questions that read a pane/);
  assert.equal(N.proposalSource({ for: "host", fromPane: true, seenIn: "prod-db-02" }), "Seen in pane prod-db-02 · terminal output");
  assert.equal(N.proposalDone({ ...host, status: "saved" }), "Saved to host notes · prod-db-01");
  assert.equal(N.proposalDone({ ...mine, status: "saved" }), "Saved to your notes");
  assert.equal(N.proposalDone({ status: "dismissed" }), "Dismissed. Nothing was saved.");
  assert.match(N.FROM_PANE_WARNING, /terminal output, not from you/);
});

test("the trace says which notes went with a question; Read so far leaves them out", () => {
  assert.equal(traceText({ status: "notes", noteKind: "host", host: "prod-db-01", notes: 3 }), "Used host notes · prod-db-01 (3)");
  assert.equal(traceText({ status: "notes", noteKind: "user", notes: 2 }), "Used your notes (2)");
  const sum = readSummary([{ trace: [{ status: "notes", noteKind: "host", host: "web-1", notes: 2 }, { status: "read", host: "web-1", lines: 10 }] }]);
  assert.deepEqual(sum.map((r) => [r.host, r.lines]), [["web-1", 10]]);
  assert.equal(N.redactedToast(0), "");
  assert.equal(N.redactedToast(2), "2 secret-looking values were replaced before saving.");
});

import { test } from "node:test";
import assert from "node:assert/strict";
import * as A from "../lib/agent.js";

const panes = [
  { sessionId: "s1", label: "web-1", focused: true, inActiveTab: true, readable: true, production: false, driving: true },
  { sessionId: "s2", label: "prod-db-01", focused: false, inActiveTab: true, readable: true, production: true, driving: true },
  { sessionId: "s3", label: "secret-box", focused: false, inActiveTab: true, readable: false, driving: true },
  { sessionId: "s4", label: "other-tab", focused: false, inActiveTab: false, readable: true, driving: false },
  { sessionId: "r1", label: "joined", focused: false, inActiveTab: false, readable: true, remote: true },
];

test("scope chip text", () => {
  assert.equal(A.scopeLabel(2, 3), "Reads 2 of 3 panes");
  assert.equal(A.scopeLabel(1, 1), "Reads 1 of 1 pane");
  assert.equal(A.scopeLabel(0, 0), "Reads 0 of 0 panes");
});

test("joined panes are never counted or picked (§5.7)", () => {
  assert.deepEqual(A.candidatePanes(panes).map((p) => p.sessionId), ["s1", "s2", "s3", "s4"]);
  assert.deepEqual(A.toggleScope([], panes[4]), []);
});

test("default scope follows the setting and skips unreadable hosts (§6.2)", () => {
  assert.deepEqual(A.defaultScope("focused", panes), ["s1"]);
  assert.deepEqual(A.defaultScope("tab", panes), ["s1", "s2"]);
  assert.deepEqual(A.defaultScope("pick", panes), []);
  assert.deepEqual(A.defaultScope("focused", [{ ...panes[2], focused: true }]), [], "a focused unreadable pane is not read");
});

test("scope keeps only open readable panes and never adds an unreadable one", () => {
  assert.deepEqual(A.reconcileScope(["s2", "gone", "s3", "s2"], panes), ["s2"]);
  assert.deepEqual(A.toggleScope(["s1"], panes[2]), ["s1"]);
  assert.deepEqual(A.toggleScope(["s1"], panes[1]), ["s1", "s2"]);
  assert.deepEqual(A.toggleScope(["s1", "s2"], panes[0]), ["s2"]);
});

test("bytes show every control character as a glyph", () => {
  assert.equal(A.bytesHTML("ls -la\n"), 'ls -la<span class="k-ctl">↵</span>');
  assert.equal(A.bytesHTML("a\tb\u001b[0m\u0003\n"), 'a<span class="k-ctl">⇥</span>b<span class="k-ctl">⎋</span>[0m<span class="k-ctl">^C</span><span class="k-ctl">↵</span>');
  assert.equal(A.bytesHTML("echo <b>&\n"), 'echo &lt;b&gt;&amp;<span class="k-ctl">↵</span>');
  assert.equal(A.bytesHTML("one\ntwo\n"), 'one<span class="k-ctl">↵</span>\ntwo<span class="k-ctl">↵</span>');
});

test("byte and line counts match what is typed", () => {
  assert.equal(A.countLabel("uptime\n"), "1 line · 7 bytes");
  assert.equal(A.countLabel("a\nb\n"), "2 lines · 4 bytes");
  assert.equal(A.countLabel("echo é\n"), "1 line · 8 bytes");
});

test("risk badges", () => {
  assert.deepEqual(A.riskBadge("readonly"), { label: "Read-only", cls: "k-badge--tide" });
  assert.deepEqual(A.riskBadge("changes"), { label: "Changes state", cls: "k-badge--signal" });
  assert.deepEqual(A.riskBadge("destructive"), { label: "Destructive", cls: "k-badge--flare" });
  assert.equal(A.riskBadge("???").label, "Changes state", "unknown is never shown as read-only");
});

test("trace lines say what was read", () => {
  assert.equal(A.traceText({ status: "read", host: "prod-db-01", lines: 120 }), "Read prod-db-01 · last 120 lines");
  assert.equal(A.traceText({ status: "read", host: "web", lines: 1, redactions: { password_prompt: 1, aws_key: 2 } }), "Read web · last 1 line · 3 redacted");
  assert.equal(A.traceText({ status: "skipped", reason: "not-readable", host: "box" }), "Skipped box · the agent can't read this host");
  assert.equal(A.traceText({ status: "skipped", reason: "not-open" }), "Skipped a pane · it is closed");
});

test("answers are escaped, with code spans", () => {
  assert.equal(A.answerHTML("run `df -h` <now>"), "run <code>df -h</code> &lt;now&gt;");
});

test("answers render **bold** safely, never inside code or from raw HTML", () => {
  assert.equal(A.answerHTML("**Add your user** to `docker`"), "<strong>Add your user</strong> to <code>docker</code>");
  assert.equal(A.answerHTML("`a **b** c`"), "<code>a **b** c</code>");
  assert.equal(A.answerHTML("**<img src=x onerror=alert(1)>**"), "<strong>&lt;img src=x onerror=alert(1)&gt;</strong>");
  assert.equal(A.answerHTML("a ** b"), "a ** b");
});

test("Type is offered only to the driver of an open local pane (§6.6)", () => {
  const p = { sessionId: "s1", needsShift: false };
  assert.equal(A.typeOffer(p, panes[0]), "type");
  assert.equal(A.typeOffer({ sessionId: "s4" }, panes[3]), "following");
  assert.equal(A.typeOffer({ sessionId: "r1" }, panes[4]), "following");
  assert.equal(A.typeOffer({ sessionId: "gone" }, null), "closed");
  assert.equal(A.typeOffer({ sessionId: "" }, panes[0]), "none");
  assert.deepEqual(A.typeGate({ sessionId: "s4" }, panes[3], true), { ok: false, reason: "following" });
});

test("destructive, or changes on production, needs Shift (§6.5)", () => {
  const de = { sessionId: "s2", needsShift: true, risk: "destructive" };
  assert.equal(A.typeOffer(de, panes[1]), "shift");
  assert.deepEqual(A.typeGate(de, panes[1], false), { ok: false, reason: "shift" });
  assert.deepEqual(A.typeGate(de, panes[1], true), { ok: true });
  assert.equal(A.shiftReason(de), "Destructive");
  assert.equal(A.shiftReason({ risk: "changes" }), "Changes state on a production host");
  assert.deepEqual(A.typeGate({ sessionId: "s1", needsShift: false }, panes[0], false), { ok: true });
});

test("@ opens the pane picker only at the start of a word", () => {
  assert.ok(A.atTrigger("@", 1));
  assert.ok(A.atTrigger("look at @", 9));
  assert.ok(!A.atTrigger("deploy@host", 7));
  assert.ok(!A.atTrigger("abc", 3));
});

test("conversation ids match the server's pattern", () => {
  for (let i = 0; i < 20; i++) assert.match(A.conversationId(), /^[A-Za-z0-9_-]{8,64}$/);
});

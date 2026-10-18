import { test } from "node:test";
import assert from "node:assert/strict";
import * as R from "../lib/runbooks.js";
import { traceText } from "../lib/agent.js";

const book = (over = {}) => ({
  id: "rb_1",
  name: "Diagnose replication lag",
  when: "A Patroni replica is behind",
  hosts: ["production", "postgres"],
  source: "user",
  agentMayPick: true,
  createdAt: "2026-10-12T09:00:00Z",
  steps: [
    { id: "st_1", title: "Check replication", command: "psql -xc 'select 1'", risk: "readonly" },
    { id: "st_2", title: "Restart", command: "systemctl restart patroni", risk: "changes" },
    { id: "st_3", title: "Ask the team" },
  ],
  ...over,
});

test("the trace says which runbook was used", () => {
  assert.equal(traceText({ status: "runbook", runbook: "Diagnose replication lag" }), "Used runbook · Diagnose replication lag");
});

test("source badges and lines, and the review banner for what you did not write", () => {
  assert.deepEqual(R.sourceBadge(book()), { label: "You", cls: "k-badge" });
  assert.equal(R.sourceBadge(book({ source: "conversation" })).label, "From conversation");
  assert.match(R.sourceLine(book()), /^Written by you · Oct 1[12]$/);
  assert.match(R.sourceLine(book({ source: "conversation", sourceRef: "nginx 502" })), /^From the conversation “nginx 502” · Oct/);
  assert.match(R.sourceLine(book({ source: "agent", fromPane: true })), /^Proposed by the agent after reading panes/);
  assert.equal(R.needsReview(book()), false);
  assert.equal(R.needsReview(book({ source: "agent", agentMayPick: false })), true);
  assert.equal(R.needsReview(book({ source: "notebook", agentMayPick: true })), false);
  assert.match(R.reviewText(book({ source: "agent", fromPane: true })), /^The agent proposed it after reading terminal output\. .*Step 2 changes state\.$/);
});

test("steps line and kinds", () => {
  assert.equal(R.stepsLine(book()), "3 steps · 1 changes state");
  assert.equal(R.stepsLine(book({ steps: [{ title: "a", command: "ls", risk: "readonly" }] })), "1 step · all read-only");
  assert.equal(R.stepsLine(book({ steps: [{ title: "a" }] })), "1 step · no commands");
  assert.equal(R.stepKind({ command: "ls", risk: "readonly" }).label, "Read-only");
  assert.equal(R.stepKind({ command: "rm -rf /", risk: "destructive" }).label, "Destructive");
  assert.equal(R.stepKind({ title: "x" }).label, "Check");
});

test("used label", () => {
  const now = new Date(2026, 9, 18, 12, 0);
  assert.equal(R.usedLabel(book(), now), "Never used");
  assert.equal(R.usedLabel(book({ lastUsedAt: new Date(2026, 9, 18, 9, 15).toISOString(), lastUsedOn: "prod-db-02" }), now), "Last used on prod-db-02 · today 09:15");
  assert.equal(R.usedLabel(book({ lastUsedAt: new Date(2026, 9, 15, 9, 15).toISOString() }), now), "Last used · Oct 15, 09:15");
});

test("filter by name, Use when, hosts and step titles", () => {
  const list = [book(), book({ id: "rb_2", name: "nginx 502", when: "upstream errors", hosts: ["nginx"], steps: [{ title: "tail the log" }] })];
  assert.equal(R.filterRunbooks(list, "").length, 2);
  assert.deepEqual(R.filterRunbooks(list, "nginx").map((b) => b.id), ["rb_2"]);
  assert.deepEqual(R.filterRunbooks(list, "PATRONI behind").map((b) => b.id), ["rb_1"]);
  assert.deepEqual(R.filterRunbooks(list, "tail").map((b) => b.id), ["rb_2"]);
});

test("offered for a pane on a host in the runbook's folders or tags, like keel serve", () => {
  const rb = book();
  assert.equal(R.fitsPane(rb, { group: "Postgres" }), true);
  assert.equal(R.fitsPane(rb, { tags: ["production"] }), true);
  assert.equal(R.fitsPane(rb, { group: "web", tags: ["nginx"] }), false);
  assert.equal(R.fitsPane(book({ hosts: [] }), {}), true);
  assert.deepEqual(R.offerFor(rb, [{ group: "web", readable: true }]), { ok: false, reason: "Not offered: no production or postgres host in scope" });
  assert.equal(R.offerFor(rb, [{ group: "postgres", readable: false }]).ok, false);
  assert.equal(R.offerFor(rb, [{ group: "postgres", readable: true }]).ok, true);
});

test("the editor's input: cleaned lines, empty steps dropped, checks", () => {
  const input = R.toInput({
    name: "  Disk\nfull ",
    when: "runner\u2028above 85%",
    hosts: ["ci", " "],
    agentMayPick: true,
    steps: [{ title: "See\tdocker", command: "docker system df\r\n" }, { title: "", command: "" }, { title: "Prune", description: "Keeps\n3 days", command: "docker builder prune" }],
  });
  assert.equal(input.name, "Disk full");
  assert.equal(input.when, "runner above 85%");
  assert.deepEqual(input.hosts, ["ci"]);
  assert.equal(input.steps.length, 2);
  assert.equal(input.steps[0].title, "See docker");
  assert.equal(input.steps[0].command, "docker system df");
  assert.equal(input.steps[1].description, "Keeps 3 days");
  assert.equal(R.cleanLine("a\u200bb"), "a b");
  assert.equal(R.checkInput(input), "");
  assert.equal(R.checkInput({ ...input, name: "" }), "Name the runbook.");
  assert.equal(R.checkInput({ ...input, steps: [] }), "Add at least one step.");
  assert.equal(R.checkInput({ ...input, steps: [{ title: "", command: "ls" }] }), "Step 1 needs a title.");
  assert.deepEqual(R.addHost(["ci"], "CI"), ["ci"]);
  assert.deepEqual(R.addHost(["ci"], " nginx "), ["ci", "nginx"]);
  assert.deepEqual(R.moveStep(["a", "b", "c"], 0, -1), ["a", "b", "c"]);
  assert.deepEqual(R.moveStep(["a", "b", "c"], 0, 1), ["b", "a", "c"]);
});

const run = (states, over = {}) => ({
  id: "ru_1",
  runbookId: "rb_1",
  name: "Diagnose replication lag",
  pickedBy: "agent",
  why: "matched the WAL timeouts",
  status: "active",
  steps: states.map((st, i) => ({ id: `st_${i + 1}`, title: `Step ${i + 1}`, command: i === 3 ? "" : "ls", ...st })),
  ...over,
});

test("progress rows: done with a reported exit, the current step, the next ones", () => {
  const r = run([{ state: "done", result: "reported", exit: 0 }, { state: "proposed", proposalId: "p_2" }, { state: "next" }, { state: "next" }]);
  assert.equal(R.currentStep(r), 1);
  assert.deepEqual(R.stepRow(r, 0), { state: "done", mark: "✓", note: "exit 0 · reported" });
  assert.deepEqual(R.stepRow(r, 1), { state: "now", mark: "2", note: "proposed" });
  assert.deepEqual(R.stepRow(r, 2), { state: "next", mark: "3", note: "" });
  const f = run([{ state: "failed", result: "reported", exit: 3 }, { state: "next" }]);
  assert.deepEqual(R.stepRow(f, 0), { state: "failed", mark: "✗", note: "exit 3 · reported" });
  const m = run([{ state: "done", result: "user" }, { state: "skipped", result: "user" }, { state: "typed", typedAt: new Date().toISOString(), result: "unreported" }]);
  assert.equal(R.stepRow(m, 0).note, "marked done");
  assert.equal(R.stepRow(m, 1).mark, "–");
  assert.match(R.stepRow(m, 2).note, /^typed \d\d:\d\d · no exit status$/);
});

test("what the run card offers next: never a later step before this one", () => {
  const live = (id) => id === "p_2";
  assert.equal(R.nextAction(run([{ state: "done" }, { state: "next" }]), live).kind, "propose");
  assert.equal(R.nextAction(run([{ state: "done" }, { state: "next" }]), live).step.n, 2);
  assert.equal(R.nextAction(run([{ state: "done" }, { state: "proposed", proposalId: "p_2" }]), live).kind, "proposed");
  assert.equal(R.nextAction(run([{ state: "done" }, { state: "proposed", proposalId: "p_old" }]), live).kind, "propose");
  assert.equal(R.nextAction(run([{ state: "typed" }]), live).kind, "waiting");
  assert.equal(R.nextAction(run([{ state: "typed", result: "abandoned" }]), live).kind, "noexit");
  assert.equal(R.nextAction(run([{ state: "failed", exit: 2 }, { state: "next" }]), live).kind, "failed");
  assert.equal(R.nextAction(run([{ state: "done" }, { state: "done" }, { state: "skipped" }, { state: "next" }]), live).kind, "check");
  assert.equal(R.nextAction(run([{ state: "done" }], { status: "finished" }), live).kind, "finished");
  assert.equal(R.nextAction(run([{ state: "next" }], { status: "stopped" }), live).kind, "stopped");
  assert.equal(R.nextAction(null).kind, "none");
  assert.equal(R.waitingForShell(run([{ state: "typed" }])), true);
  assert.equal(R.waitingForShell(run([{ state: "typed", result: "abandoned" }])), false);
  assert.equal(R.waitingForShell(run([{ state: "typed" }], { status: "stopped" })), false);
});

test("why line and the questions the card asks", () => {
  assert.equal(R.whyLine(run([])), "Why: matched the WAL timeouts");
  assert.equal(R.whyLine(run([], { pickedBy: "user" })), "You picked this runbook.");
  assert.equal(R.continueQuestion({ n: 2, of: 4, title: "Look for errors" }), "Continue with step 2 of 4: Look for errors");
  assert.equal(R.startQuestion({ name: "nginx\n502" }), "Walk me through the runbook “nginx 502”.");
});

test("a runbook the agent proposes is warned about after any pane read", () => {
  assert.equal(R.proposalWarn({ panesRead: [] }), false);
  assert.equal(R.proposalWarn({ panesRead: ["web-1"] }), true);
  assert.equal(R.proposalWarn({ fromPane: true }), true);
  assert.match(R.proposalWarning({ fromPane: true, seenIn: "web-1" }), /matches pane web-1/);
  assert.match(R.proposalDone({ status: "saved" }), /may not pick it until you review it/);
  assert.equal(R.proposalDone({ status: "dismissed" }), "Dismissed. Nothing was saved.");
});

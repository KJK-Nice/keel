import { test } from "node:test";
import assert from "node:assert/strict";
import * as G from "../lib/grouprun.js";
import { groupNotes, userNotes, noteStepText } from "../lib/notes.js";
import { toInput, checkInput } from "../lib/runbooks.js";

const cell = (hostId, state, extra = {}) => ({ hostId, host: hostId.replace("h", "wk-"), state, ...extra });
const prop = (id, extra = {}) => ({ id: `p_${id}`, sessionId: `s_${id}`, bytes: "\u0015sudo systemctl restart kubelet\n", production: false, needsShift: false, ...extra });

test("roles: cleaned like keel serve keeps them, counted per role", () => {
  assert.deepEqual(G.cleanRole(" Control-Plane "), { role: "control-plane", ok: true });
  assert.equal(G.cleanRole("two words").ok, false);
  assert.equal(G.cleanRole("").ok, true);
  const members = [{ role: "worker" }, { role: "worker" }, { role: "control-plane" }, { role: "" }];
  assert.deepEqual(G.roleCounts(members), [
    { role: "worker", count: 2 },
    { role: "control-plane", count: 1 },
    { role: "no role", count: 1, none: true },
  ]);
  assert.deepEqual(G.roleNames(members), ["control-plane", "worker"]);
  assert.equal(G.groupLine([{ hostId: "a" }, { hostId: "b" }], [{ id: "a", production: true }, { id: "b", production: true }]), "Folder · 2 hosts · production");
});

test("where a step runs", () => {
  assert.equal(G.targetLabel({ command: "x", role: "worker", cells: [{}, {}] }), "Role worker · 2 hosts");
  assert.equal(G.targetLabel({ command: "x", role: "control-plane", oneHost: true, cells: [{}] }), "One control-plane host");
  assert.equal(G.targetLabel({ command: "x", cells: [] }), "No host the agent may read");
  assert.equal(G.targetLabel({}), "No command · you check it");
});

test("a cell's words never say more than the shell reported", () => {
  assert.equal(G.cellView(cell("h1", "done", { exit: 0, result: "reported" })).text, "exit 0");
  assert.equal(G.cellView(cell("h1", "done", { result: "user" })).text, "marked done");
  assert.equal(G.cellView(cell("h1", "failed", { exit: 3 })).text, "exit 3");
  assert.equal(G.cellView(cell("h1", "typed", { exit: 0, confirm: true })).text, "exit 0 · confirm");
  assert.equal(G.cellView(cell("h1", "typed", { result: "unreported" })).text, "no exit status");
  assert.equal(G.cellView(cell("h1", "waiting", { blocked: "No pane" })).text, "needs a pane");
  assert.equal(G.cellView(cell("h1", "typing")).text, "typing");
});

test("the approve dialog: one row per host, nothing ticked unless it is the only one", () => {
  const cells = [cell("h1", "proposed", { proposal: prop(1) }), cell("h2", "proposed", { proposal: prop(2, { production: true, needsShift: true }) })];
  const rows = G.approveRows(cells);
  assert.equal(rows.length, 2);
  assert.ok(rows.every((r) => r.checked === false));
  assert.equal(rows[0].proposalId, "p_1");
  assert.equal(rows[1].sessionId, "s_2");
  assert.equal(G.typeLabel(rows), "Tick the hosts to type into");
  assert.equal(G.needsShift(rows), false, "an unticked production host asks for nothing");
  rows[1].checked = true;
  assert.equal(G.needsShift(rows), true);
  assert.equal(G.typeLabel(rows), "Type into 1 pane");
  assert.ok(G.sameBytes(rows));
  assert.equal(G.approveRows([cells[0]])[0].checked, true, "one host: the ordinary approval");
  assert.equal(G.bytesLine(rows[0].bytes), "1 line · 32 bytes");
});

test("only proposals waiting in an open pane can be typed", () => {
  const run = { status: "active", current: 0, steps: [{ cells: [cell("h1", "proposed", { proposal: prop(1) }), cell("h2", "typed", { proposal: prop(2) }), cell("h3", "proposed", { proposal: prop(3) })] }] };
  assert.deepEqual(G.typeable(run, (sid) => sid !== "s_3").map((c) => c.hostId), ["h1"]);
  assert.deepEqual(G.typeable({ ...run, status: "stopped" }), []);
});

test("rolling halt says which hosts were not touched", () => {
  const run = { halted: "wk-3", current: 0, steps: [{ cells: [cell("h1", "done"), cell("h2", "done"), cell("h3", "failed", { exit: 1 }), cell("h4", "waiting"), cell("h5", "waiting")] }] };
  assert.deepEqual(G.haltLine(run), { title: "wk-3 failed with exit 1. Rolling stopped before wk-4.", sub: "wk-4 and wk-5 were not touched.", hostId: "h3" });
  assert.equal(G.haltLine({ ...run, halted: "" }), null);
  assert.equal(G.summary(run.steps[0]), "2 of 5 done · 1 failed");
  assert.equal(G.stepMark(run, 0).state, "failed");
});

test("group notes are apart from the user's own notes", () => {
  const notes = [{ id: 1, text: "mine" }, { id: 2, groupId: "g", text: "group" }, { id: 3, hostId: "h", text: "host" }];
  assert.deepEqual(userNotes(notes).map((n) => n.id), [1]);
  assert.deepEqual(groupNotes(notes, "g").map((n) => n.id), [2]);
  assert.equal(noteStepText({ status: "notes", noteKind: "group", host: "k8s", notes: 3 }), "Used group notes · k8s (3)");
});

test("a runbook step keeps its role and one host", () => {
  const input = toInput({ name: "r", steps: [{ title: "a", command: "x", role: " Worker ", oneHost: true }] });
  assert.deepEqual([input.steps[0].role, input.steps[0].oneHost], ["worker", true]);
  assert.equal(checkInput(input), "");
  assert.match(checkInput(toInput({ name: "r", steps: [{ title: "a", role: "two words" }] })), /role/);
});

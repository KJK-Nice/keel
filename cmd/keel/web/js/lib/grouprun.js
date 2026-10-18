// Groups, part 1 (docs/ui-redesign.md §17): the words and decisions of the
// group page (roles) and the group run page (a runbook across a group).
// Pure: no DOM, no fetch.

export const ROLE_RE = /^[a-z0-9][a-z0-9_.-]{0,31}$/;

// cleanRole is a typed role as keel serve keeps it: trimmed, lower case.
// ok is false for anything that is not one word of letters, digits, -, _
// or . (empty is ok: no role).
export function cleanRole(text) {
  const r = String(text || "").trim().toLowerCase();
  return { role: r, ok: r === "" || ROLE_RE.test(r) };
}

// roleCounts is the head's chips: hosts per role, most first, then "no
// role" when some hosts have none.
export function roleCounts(members) {
  const counts = new Map();
  let none = 0;
  for (const m of members || []) {
    if (m.role) counts.set(m.role, (counts.get(m.role) || 0) + 1);
    else none++;
  }
  const out = [...counts.entries()].map(([role, count]) => ({ role, count })).sort((a, b) => b.count - a.count || a.role.localeCompare(b.role));
  if (none) out.push({ role: "no role", count: none, none: true });
  return out;
}

// roleNames are the roles used in the group, for the role menus.
export function roleNames(members) {
  return [...new Set((members || []).map((m) => m.role).filter(Boolean))].sort();
}

// groupLine is the page's sub line: "Folder · 8 hosts · production".
export function groupLine(members, hosts) {
  const n = (members || []).length;
  const byId = new Map((hosts || []).map((h) => [h.id, h]));
  const prod = (members || []).filter((m) => byId.get(m.hostId)?.production).length;
  const parts = ["Folder", `${n} ${n === 1 ? "host" : "hosts"}`];
  if (prod && prod === n) parts.push("production");
  else if (prod) parts.push(`${prod} production`);
  return parts.join(" · ");
}

export const MODE_HELP = {
  rolling: "Rolling: one host at a time, in order. The first failure stops the step before the next host.",
  parallel: "Parallel: every host at once. A failure does not stop the others. Best for read-only steps.",
};

export function modeShort(mode) {
  return mode === "parallel" ? "parallel" : "rolling, one at a time";
}

// targetLabel says where a step runs: "Role worker · 5 hosts", "One
// control-plane host", "Every host · 8 hosts", "You check it".
export function targetLabel(step) {
  if (!step?.command) return "No command · you check it";
  const n = step.oneHost ? 1 : (step.cells || []).length;
  if (step.oneHost) return step.role ? `One ${step.role} host` : "One host in the group";
  if (!n) return step.role ? `Role ${step.role} · no host has it` : "No host the agent may read";
  const hosts = `${n} ${n === 1 ? "host" : "hosts"}`;
  return step.role ? `Role ${step.role} · ${hosts}` : `Every host · ${hosts}`;
}

const GLYPH = { done: "✓", failed: "✗", typed: "◌", proposed: "·", waiting: "·", skipped: "–" };

// cellView is how a host × step shows: its glyph, words and class.
export function cellView(c) {
  if (!c) return { glyph: "", text: "", cls: "k-gx-st k-gx-st--wait", state: "none" };
  const exit = c.exit !== undefined && c.exit !== null ? c.exit : null;
  let text;
  let cls = "k-gx-st k-gx-st--wait";
  switch (c.state) {
    case "done":
      text = exit !== null && c.result === "reported" ? `exit ${exit}` : c.result === "user" ? "marked done" : "done";
      cls = "k-gx-st k-gx-st--ok";
      break;
    case "failed":
      text = exit !== null ? `exit ${exit}` : "failed";
      cls = "k-gx-st k-gx-st--fail";
      break;
    case "typed":
      if (c.confirm) text = "exit 0 · confirm";
      else if (c.result === "unreported") text = "no exit status";
      else if (c.result === "abandoned") text = "shell went away";
      else text = "running";
      cls = c.confirm ? "k-gx-st k-gx-st--run" : "k-gx-st k-gx-st--run";
      break;
    case "proposed":
      text = "waiting for you";
      cls = "k-gx-st k-gx-st--run";
      break;
    case "skipped":
      text = "skipped";
      break;
    default:
      text = c.blocked ? "needs a pane" : "waiting";
  }
  return { glyph: GLYPH[c.state] || "·", text, cls, state: c.state };
}

// summary is the results head: "2 of 5 exit 0 · 1 failed".
export function summary(step) {
  const cells = step?.cells || [];
  if (!cells.length) return step?.done ? "Checked" : step?.skipped ? "Skipped" : "No hosts";
  const ok = cells.filter((c) => c.state === "done").length;
  const failed = cells.filter((c) => c.state === "failed").length;
  const skipped = cells.filter((c) => c.state === "skipped").length;
  if (cells.every((c) => c.state === "waiting" && !c.proposal)) return "Not started";
  const parts = [`${ok} of ${cells.length} done`];
  if (failed) parts.push(`${failed} failed`);
  if (skipped) parts.push(`${skipped} skipped`);
  return parts.join(" · ");
}

// typeable are the current step's cells whose proposal waits to be typed,
// with a pane open in this window (paneOpen(sessionId) → bool).
export function typeable(run, paneOpen = () => true) {
  if (!run || run.status !== "active" || run.current < 0) return [];
  const step = run.steps[run.current];
  return (step?.cells || []).filter((c) => c.state === "proposed" && c.proposal && paneOpen(c.proposal.sessionId));
}

// approveRows are the approve dialog's checklist: one row per host, never
// ticked in advance unless it is the only one (then the dialog is the
// ordinary one-host approval).
export function approveRows(cells) {
  const one = (cells || []).length === 1;
  return (cells || []).map((c) => ({
    hostId: c.hostId,
    host: c.host,
    sessionId: c.proposal.sessionId,
    proposalId: c.proposal.id,
    bytes: c.proposal.bytes,
    production: !!c.proposal.production,
    needsShift: !!c.proposal.needsShift,
    checked: one,
  }));
}

// sameBytes reports whether every row types the same bytes (they come from
// one saved command, so they should).
export function sameBytes(rows) {
  return (rows || []).every((r) => r.bytes === rows[0].bytes);
}

// needsShift: any ticked row needs ⇧ (a changes-state step on a production
// host, or a destructive one).
export function needsShift(rows) {
  return (rows || []).some((r) => r.checked && r.needsShift);
}

// typeLabel is the dialog's button: "Type into 3 panes".
export function typeLabel(rows) {
  const n = (rows || []).filter((r) => r.checked).length;
  if (!n) return "Tick the hosts to type into";
  return `Type into ${n} ${n === 1 ? "pane" : "panes"}`;
}

// bytesLine is "1 line · 75 bytes".
export function bytesLine(bytes) {
  const b = String(bytes || "");
  const body = b.replace(/^\u0015/, "").replace(/\n$/, "");
  const lines = body ? body.split("\n").length : 0;
  const n = new TextEncoder().encode(b).length;
  return `${lines} ${lines === 1 ? "line" : "lines"} · ${n} bytes`;
}

// haltLine is the rolling stop banner.
export function haltLine(run) {
  if (!run?.halted || run.current < 0) return null;
  const step = run.steps[run.current];
  const cells = step.cells || [];
  const i = cells.findIndex((c) => c.host === run.halted && c.state === "failed");
  const failed = cells[i];
  const rest = cells.slice(i + 1).filter((c) => c.state !== "done" && c.state !== "skipped").map((c) => c.host);
  const exit = failed?.exit !== undefined && failed?.exit !== null ? ` with exit ${failed.exit}` : "";
  const next = rest[0] ? ` Rolling stopped before ${rest[0]}.` : "";
  const untouched = rest.length ? `${listWords(rest)} ${rest.length === 1 ? "was" : "were"} not touched.` : "";
  return { title: `${run.halted} failed${exit}.${next}`, sub: untouched, hostId: failed?.hostId || "" };
}

function listWords(xs) {
  if (xs.length <= 1) return xs.join("");
  return `${xs.slice(0, -1).join(", ")} and ${xs[xs.length - 1]}`;
}

// stepMark is a step's mark in the steps list.
export function stepMark(run, i) {
  const st = run.steps[i];
  const cells = st.cells || [];
  if (st.skipped) return { mark: "–", state: "skipped" };
  if (st.done || (cells.length && cells.every((c) => c.state === "done" || c.state === "skipped"))) return { mark: "✓", state: "done" };
  if (cells.some((c) => c.state === "failed")) return { mark: "✗", state: "failed" };
  if (i === run.current) return { mark: String(i + 1), state: "now" };
  return { mark: String(i + 1), state: "next" };
}

// statusLine is the run's line in the status bar and the head.
export function statusLine(run) {
  if (!run) return "";
  if (run.status === "finished") return "Runbook finished";
  if (run.status === "stopped") return `Runbook stopped at step ${run.current + 1 || run.steps.length}`;
  return `Runbook · step ${run.current + 1} of ${run.steps.length} · ${modeShort(run.mode)}`;
}

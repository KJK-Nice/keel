// Runbooks (docs/ui-redesign.md §16): the words and states the UI shows
// for a saved runbook, for a runbook in use in a conversation and for a
// runbook the agent proposes. Pure: no DOM, no fetch.

import { stampLabel } from "./agent.js";
import { dayLabel } from "./notes.js";
import { checkCaptures, cleanCaptures } from "./groupvars.js";

export const MAX_STEPS = 30;

const SOURCES = {
  user: { label: "You", cls: "k-badge", long: "Written by you" },
  conversation: { label: "From conversation", cls: "k-badge k-badge--haze", long: "From the conversation" },
  notebook: { label: "From notebook", cls: "k-badge k-badge--haze", long: "From the notebook" },
  agent: { label: "Proposed", cls: "k-badge k-badge--signal", long: "Proposed by the agent" },
};

// sourceBadge is the small badge on a list row.
export function sourceBadge(rb) {
  const s = SOURCES[rb?.source] || SOURCES.user;
  return { label: s.label, cls: s.cls };
}

// sourceLine is the detail's Source: "Written by you · Oct 12", "From the
// conversation "nginx 502" · Oct 15", "Proposed by the agent after reading
// panes · Oct 15".
export function sourceLine(rb) {
  const s = SOURCES[rb?.source] || SOURCES.user;
  let head = s.long;
  if ((rb?.source === "conversation" || rb?.source === "notebook") && rb.sourceRef) head += ` “${rb.sourceRef}”`;
  if (rb?.source === "agent" && rb.fromPane) head += " after reading panes";
  return [head, dayLabel(rb?.createdAt)].filter(Boolean).join(" · ");
}

// needsReview is a runbook that was not written by the user and is not
// turned on yet: the page asks for a review before Agent may pick.
export function needsReview(rb) {
  return !!rb && rb.source && rb.source !== "user" && !rb.agentMayPick;
}

// reviewText is the review banner's words.
export function reviewText(rb) {
  const from =
    rb?.source === "agent"
      ? "The agent proposed it"
      : rb?.source === "notebook"
        ? `Made from the notebook${rb.sourceRef ? ` “${rb.sourceRef}”` : ""}`
        : `Made from the conversation${rb?.sourceRef ? ` “${rb.sourceRef}”` : ""}`;
  const pane = rb?.fromPane ? " after reading terminal output" : "";
  const changes = (rb?.steps || []).map((s, i) => (s.risk && s.risk !== "readonly" ? i + 1 : 0)).filter(Boolean);
  const note = changes.length ? ` ${changes.length === 1 ? `Step ${changes[0]} changes state.` : `Steps ${changes.join(", ")} change state.`}` : "";
  return `${from}${pane}. The agent reads a runbook as a checklist, so read every step.${note}`;
}

// usedLabel is "Last used on prod-db-02 · today 09:15", or "Never used".
export function usedLabel(rb, now = new Date()) {
  if (!rb?.lastUsedAt) return "Never used";
  const when = stampLabel(rb.lastUsedAt, now);
  const today = !/,/.test(when);
  return `Last used${rb.lastUsedOn ? ` on ${rb.lastUsedOn}` : ""} · ${today ? `today ${when}` : when}`;
}

// stepsLine is "4 steps · all read-only" or "3 steps · 1 changes state".
export function stepsLine(rb) {
  const steps = rb?.steps || [];
  const changes = steps.filter((s) => s.command && s.risk && s.risk !== "readonly").length;
  const n = `${steps.length} ${steps.length === 1 ? "step" : "steps"}`;
  if (!steps.some((s) => s.command)) return `${n} · no commands`;
  return `${n} · ${changes === 0 ? "all read-only" : `${changes} ${changes === 1 ? "changes" : "change"} state`}`;
}

// riskBadge is a step's kind on the page: Read-only, Changes or
// Destructive (Keel's own rating of the saved command).
export function stepKind(step) {
  if (!step?.command) return { label: "Check", cls: "k-badge" };
  if (step.risk === "readonly") return { label: "Read-only", cls: "k-badge k-badge--tide" };
  if (step.risk === "destructive") return { label: "Destructive", cls: "k-badge k-badge--flare" };
  return { label: "Changes", cls: "k-badge k-badge--signal" };
}

// filterRunbooks narrows the list by name, Use when, hosts and step titles.
export function filterRunbooks(list, q) {
  const words = String(q || "")
    .toLowerCase()
    .split(/\s+/)
    .filter(Boolean);
  if (!words.length) return list || [];
  return (list || []).filter((rb) => {
    const hay = [rb.name, rb.when, ...(rb.hosts || []), ...(rb.steps || []).map((s) => s.title)].join(" ").toLowerCase();
    return words.every((w) => hay.includes(w));
  });
}

// fitsPane is keel serve's rule for offering a runbook: no hosts, or a
// pane whose host's folder or one of its tags is listed (any case).
export function fitsPane(rb, pane) {
  const hosts = (rb?.hosts || []).map((h) => String(h).toLowerCase());
  if (!hosts.length) return true;
  if (!pane) return false;
  if (pane.group && hosts.includes(String(pane.group).toLowerCase())) return true;
  return (pane.tags || []).some((t) => hosts.includes(String(t).toLowerCase()));
}

// offerFor says whether a runbook can be started for the panes in scope,
// and why not ("Not offered: no nginx host in scope").
export function offerFor(rb, panes) {
  const list = (panes || []).filter((p) => p.readable !== false);
  if (!list.length) return { ok: false, reason: "Put a pane the agent may read in scope first" };
  if (list.some((p) => fitsPane(rb, p))) return { ok: true, reason: "" };
  return { ok: false, reason: `Not offered: no ${(rb.hosts || []).join(" or ")} host in scope` };
}

// INVISIBLE is a control character, a line or paragraph separator or a
// zero-width or format character: keel serve turns them into spaces.
const INVISIBLE = new RegExp("[" + ["0000-001f", "007f-009f", "2028-2029", "200b-200f", "2060", "feff"].map((r) => r.split("-").map((h) => "\\u" + h).join("-")).join("") + "]", "g");

// cleanLine is one line, as keel serve keeps a runbook's text fields.
export function cleanLine(text) {
  return String(text || "")
    .replace(INVISIBLE, " ")
    .replace(/\s+/g, " ")
    .trim();
}

// toInput is the editor's state as POST/PUT /api/agent/runbooks takes it.
// Empty steps (no title and no command) are dropped.
export function toInput(ed) {
  const steps = (ed?.steps || [])
    .map((s) => ({
      id: s.id || "",
      title: cleanLine(s.title),
      description: cleanLine(s.description),
      command: String(s.command || "").replace(/\r\n/g, "\n").trim(),
      expect: cleanLine(s.expect),
      // Where it runs in a group run (§17): a role, one host of it.
      role: String(s.role || "").trim().toLowerCase(),
      oneHost: !!s.oneHost,
      // Values Keel captures from its output in a group run (§17 part 2).
      captures: cleanCaptures(s.captures),
    }))
    .filter((s) => s.title || s.command);
  return {
    name: cleanLine(ed?.name),
    when: cleanLine(ed?.when),
    hosts: (ed?.hosts || []).map(cleanLine).filter(Boolean),
    agentMayPick: !!ed?.agentMayPick,
    steps,
    // keel serve takes the source from its own record of the draft.
    draftId: ed?.draftId || "",
  };
}

// checkInput says what must change before a save, or "" when it can be
// saved.
export function checkInput(input) {
  if (!input.name) return "Name the runbook.";
  if (!input.steps.length) return "Add at least one step.";
  if (input.steps.length > MAX_STEPS) return `A runbook has at most ${MAX_STEPS} steps.`;
  const untitled = input.steps.findIndex((s) => !s.title);
  if (untitled >= 0) return `Step ${untitled + 1} needs a title.`;
  const badRole = input.steps.findIndex((s) => s.role && !/^[a-z0-9][a-z0-9_.-]{0,31}$/.test(s.role));
  if (badRole >= 0) return `Step ${badRole + 1}: a role is one word of letters, digits, -, _ or .`;
  return checkCaptures(input.steps);
}

// addHost adds a folder or tag chip (no duplicates, any case).
export function addHost(hosts, text) {
  const t = cleanLine(text);
  if (!t) return hosts || [];
  if ((hosts || []).some((h) => h.toLowerCase() === t.toLowerCase())) return hosts;
  return [...(hosts || []), t];
}

// moveStep moves step i by delta (-1 up, +1 down), within bounds.
export function moveStep(steps, i, delta) {
  const j = i + delta;
  if (j < 0 || j >= steps.length) return steps;
  const out = steps.slice();
  [out[i], out[j]] = [out[j], out[i]];
  return out;
}

// --- a runbook in use in a conversation ---

// currentStep is the first step not done or skipped, or -1.
export function currentStep(run) {
  return (run?.steps || []).findIndex((s) => s.state !== "done" && s.state !== "skipped");
}

// stepRow is one row of the progress list: its state for the marks, the
// mark, and a short note.
export function stepRow(run, i, now = new Date()) {
  const st = run.steps[i];
  const cur = currentStep(run);
  const exit = st.exit != null ? `exit ${st.exit}` : "";
  const reported = st.result === "reported" ? `${exit} · reported` : "";
  switch (st.state) {
    case "done":
      return { state: "done", mark: "✓", note: (st.result === "user" ? "marked done" : reported || "done") + (st.edited ? " · edited" : "") };
    case "skipped":
      return { state: "skipped", mark: "–", note: "skipped" };
    case "failed":
      return { state: "failed", mark: "✗", note: st.result === "user" ? "failed" : reported };
    case "typed": {
      const t = (st.typedAt ? `typed ${stampLabel(st.typedAt, now)}` : "typed") + (st.edited ? " · edited" : "");
      if (st.result === "reported" && st.exit === 0) return { state: "now", mark: String(i + 1), note: "exit 0 · reported — confirm" };
      if (st.result === "abandoned") return { state: "now", mark: String(i + 1), note: `${t} · shell went away` };
      if (st.result === "unreported") return { state: "now", mark: String(i + 1), note: `${t} · no exit status` };
      return { state: "now", mark: String(i + 1), note: t };
    }
    case "proposed":
      return { state: i === cur ? "now" : "next", mark: String(i + 1), note: "proposed" };
    default:
      return { state: i === cur ? "now" : "next", mark: String(i + 1), note: "" };
  }
}

// nextAction says what the run card offers for the current step:
//   { kind: "propose", step }   Propose step N (an ask)
//   { kind: "proposed", step }  the step's card is in the thread
//   { kind: "waiting", step }   typed, no result yet: Mark done / Skip
//   { kind: "noexit", step }    ended with no exit status reported
//   { kind: "failed", step }    Retry / Mark done / Skip
//   { kind: "check", step }     a step with no command: Mark done / Skip
//   { kind: "finished" } / { kind: "stopped" }
export function nextAction(run, proposalLive) {
  if (!run) return { kind: "none" };
  if (run.status === "stopped") return { kind: "stopped" };
  const i = currentStep(run);
  if (i < 0 || run.status === "finished") return { kind: "finished" };
  const st = run.steps[i];
  const step = { index: i, n: i + 1, of: run.steps.length, title: st.title, id: st.id };
  if (st.state === "failed") return { kind: "failed", step, exit: st.exit };
  if (st.state === "typed" && st.result === "reported" && st.exit === 0) return { kind: "confirm", step };
  if (st.state === "typed") return { kind: st.result === "abandoned" || st.result === "unreported" ? "noexit" : "waiting", step };
  if (!st.command) return { kind: "check", step };
  if (st.state === "proposed" && proposalLive?.(st.proposalId)) return { kind: "proposed", step };
  return { kind: "propose", step };
}

// waitingForShell is true while a typed step has no result: the panel
// polls GET …/runbook for the exit status the pane's shell reports.
export function waitingForShell(run) {
  return !!run && run.status === "active" && (run.steps || []).some((s) => s.state === "typed" && !s.result);
}

// whyLine is the run card's second line.
export function whyLine(run) {
  if (run?.pickedBy === "agent") return run.why ? `Why: ${run.why}` : "The agent picked this runbook.";
  return "You picked this runbook.";
}

// continueQuestion is what Propose step N asks the agent.
export function continueQuestion(step) {
  return `Continue with step ${step.n} of ${step.of}: ${step.title}`;
}

// startQuestion is what starting a runbook asks.
export function startQuestion(rb) {
  return `Walk me through the runbook “${cleanLine(rb?.name)}”.`;
}

// --- a runbook the agent proposes ---

// proposalWarn is any proposal from an answer that read a pane: its steps
// may hold text from terminal output.
export function proposalWarn(p) {
  return !!(p?.fromPane || (p?.panesRead || []).length);
}

export function proposalWarning(p) {
  const where = p?.fromPane && p.seenIn ? ` Some of it matches pane ${p.seenIn}.` : "";
  return `This runbook was written after reading terminal output.${where} Check every step before saving: a saved runbook can be offered to the agent, and text in a pane can be written to mislead it.`;
}

export function proposalDone(p) {
  if (p?.status === "saved") return "Saved to Runbooks. The agent may not pick it until you review it and turn it on.";
  if (p?.status === "dismissed") return "Dismissed. Nothing was saved.";
  return "";
}

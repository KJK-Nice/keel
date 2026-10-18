// Agent panel logic (docs/ui-redesign.md §5.4). Pure functions: no DOM, no
// state. ui/agentpanel.js renders with them; the node tests pin them.
//
// The panel never writes to a pane. typeOffer and typeGate only decide
// whether to show Type into pane and whether a press may open the approve
// dialog; keel serve checks the same rules again (agent service
// AuthorizeType) before the approved bytes are written.

import { escapeHtml } from "./html.js";

// A pane as the panel sees it (built by term.js from its pane state):
//   { sessionId, label, sub, tabId, focused, inActiveTab, readable,
//     production, driving, remote }

// scopeLabel is the chip text: "Reads 2 of 3 panes".
export function scopeLabel(n, m) {
  return `Reads ${n} of ${m} ${m === 1 ? "pane" : "panes"}`;
}

// candidatePanes are the panes the panel counts (M): local terminal panes
// with a session. Joined (remote) panes are never in scope (§5.7).
export function candidatePanes(panes) {
  return (panes || []).filter((p) => p && p.sessionId && !p.remote);
}

// defaultScope picks the panes for a new conversation from the setting:
// "focused" (the focused pane), "tab" (every pane in the active tab) or
// "pick" (none until the user picks). Hosts the agent cannot read are
// never picked.
export function defaultScope(mode, panes) {
  const ok = candidatePanes(panes).filter((p) => p.readable);
  if (mode === "pick") return [];
  if (mode === "tab") return ok.filter((p) => p.inActiveTab).map((p) => p.sessionId);
  const f = ok.find((p) => p.focused);
  return f ? [f.sessionId] : [];
}

// reconcileScope keeps the chosen ids that still name a readable pane, in
// their order.
export function reconcileScope(ids, panes) {
  const ok = new Set(candidatePanes(panes).filter((p) => p.readable).map((p) => p.sessionId));
  return (ids || []).filter((id, i, all) => ok.has(id) && all.indexOf(id) === i);
}

// toggleScope adds or removes one pane. An unreadable pane is never added.
export function toggleScope(ids, pane) {
  if (!pane) return ids.slice();
  if (ids.includes(pane.sessionId)) return ids.filter((id) => id !== pane.sessionId);
  if (!pane.readable || pane.remote) return ids.slice();
  return [...ids, pane.sessionId];
}

const encoder = new TextEncoder();

export function byteLength(s) {
  return encoder.encode(String(s || "")).length;
}

// lineCount counts lines the way the server logs them: a trailing newline
// ends the last line, it does not start another.
export function lineCount(bytes) {
  const s = String(bytes || "").replace(/\n+$/, "");
  if (!s) return 0;
  return s.split("\n").length;
}

export function countLabel(bytes) {
  const l = lineCount(bytes);
  const b = byteLength(bytes);
  return `${l} ${l === 1 ? "line" : "lines"} · ${b} ${b === 1 ? "byte" : "bytes"}`;
}

const GLYPHS = { "\n": "↵", "\t": "⇥", "\r": "⏎", "\u001b": "⎋", "\u007f": "⌫" };

// bytesHTML shows the exact bytes Type will send, with every control
// character as a visible glyph (k-ctl): ↵ newline, ⇥ tab, ⎋ escape, ^X for
// the rest. A newline still breaks the line after its glyph.
export function bytesHTML(bytes) {
  let out = "";
  for (const ch of String(bytes || "")) {
    const code = ch.codePointAt(0);
    if (GLYPHS[ch]) {
      out += `<span class="k-ctl">${GLYPHS[ch]}</span>`;
      if (ch === "\n") out += "\n";
    } else if (code < 0x20) {
      out += `<span class="k-ctl">^${String.fromCharCode(code + 64)}</span>`;
    } else if (code >= 0x80 && code < 0xa0) {
      out += `<span class="k-ctl">\\x${code.toString(16)}</span>`;
    } else {
      out += escapeHtml(ch);
    }
  }
  return out.replace(/\n$/, "");
}

const RISK = {
  readonly: { label: "Read-only", cls: "k-badge--tide" },
  changes: { label: "Changes state", cls: "k-badge--signal" },
  destructive: { label: "Destructive", cls: "k-badge--flare" },
};

export function riskBadge(risk) {
  return RISK[risk] || RISK.changes;
}

function redactedNote(counts) {
  const n = Object.values(counts || {}).reduce((a, b) => a + (Number(b) || 0), 0);
  if (!n) return "";
  return ` · ${n} redacted`;
}

// traceText is one line of the agent trace: what it read, or why not.
export function traceText(step) {
  const host = step?.host || "a pane";
  if (step?.status === "read" && step.command) {
    // From the shell's OSC 133 marks: the last command, its output and how it ended.
    return `Read ${host} · last command, ${step.command} · ${step.lines} ${step.lines === 1 ? "line" : "lines"} of output${redactedNote(step.redactions)}`;
  }
  if (step?.status === "read") {
    return `Read ${host} · last ${step.lines} ${step.lines === 1 ? "line" : "lines"}${redactedNote(step.redactions)}`;
  }
  switch (step?.reason) {
    case "not-readable":
      return `Skipped ${host} · the agent can't read this host`;
    case "not-open":
      return "Skipped a pane · it is closed";
    default:
      return `Skipped ${host} · could not read it`;
  }
}

// answerHTML escapes the answer first, then sets `code` spans in mono and
// **bold** in <strong>. Only the tags written here are ever produced, and
// bold is not applied inside code spans.
export function answerHTML(text) {
  const parts = escapeHtml(String(text || "")).split(/(`[^`\n]+`)/);
  return parts
    .map((part, i) =>
      i % 2 ? `<code>${part.slice(1, -1)}</code>` : part.replace(/\*\*([^*\n]+)\*\*/g, "<strong>$1</strong>"),
    )
    .join("");
}

// typeOffer decides what the proposal card offers for its target pane:
//   "type"      Type into pane
//   "shift"     Type into pane, but only with ⇧ (destructive, or changes
//               state on a production host)
//   "following" no Type: you follow that pane (§6.6)
//   "closed"    no Type: the pane is not open in this window
//   "none"      no Type: the proposal names no pane
//   "expired"   no Type: a saved proposal from an earlier run (Copy or
//               Propose again only)
export function typeOffer(proposal, pane) {
  if (proposal?.expired) return "expired";
  if (!proposal?.sessionId) return "none";
  if (!pane || pane.remote) return pane?.remote ? "following" : "closed";
  if (!pane.driving) return "following";
  return proposal.needsShift ? "shift" : "type";
}

// typeGate says whether a Type press may open the approve dialog.
export function typeGate(proposal, pane, shift) {
  const offer = typeOffer(proposal, pane);
  if (offer === "type") return { ok: true };
  if (offer === "shift") return shift ? { ok: true } : { ok: false, reason: "shift" };
  return { ok: false, reason: offer };
}

// shiftReason says why a proposal needs ⇧.
export function shiftReason(proposal) {
  if (proposal?.risk === "destructive") return "Destructive";
  return "Changes state on a production host";
}

// atTrigger reports whether the character just typed before the caret is
// an @ that starts a word: "@" alone or after a space or newline opens the
// pane picker; an @ inside a word (user@host) does not.
export function atTrigger(value, caret) {
  const s = String(value || "");
  const i = (caret ?? s.length) - 1;
  if (i < 0 || s[i] !== "@") return false;
  return i === 0 || /\s/.test(s[i - 1]);
}

// conversationId makes a fresh id for the server's in-memory history.
export function conversationId(rand = Math.random) {
  let s = "c";
  while (s.length < 20) s += Math.floor(rand() * 36).toString(36);
  return s;
}

// --- saved conversations (§5.4.1) ---

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

function toDate(v) {
  const d = v instanceof Date ? v : new Date(v);
  return Number.isNaN(d.getTime()) ? null : d;
}

function dayStart(d) {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
}

const pad2 = (n) => String(n).padStart(2, "0");

// dayGroup puts a time in "Today", "Yesterday" or "Older" by local date.
export function dayGroup(when, now = new Date()) {
  const d = toDate(when);
  if (!d) return "Older";
  const diff = Math.round((dayStart(now) - dayStart(d)) / 86400000);
  if (diff <= 0) return "Today";
  if (diff === 1) return "Yesterday";
  return "Older";
}

// sessionWhen is the list's time: 16:02 today and yesterday, Oct 14 before.
export function sessionWhen(when, now = new Date()) {
  const d = toDate(when);
  if (!d) return "";
  if (dayGroup(d, now) === "Older") return `${MONTHS[d.getMonth()]} ${d.getDate()}`;
  return `${pad2(d.getHours())}:${pad2(d.getMinutes())}`;
}

// stampLabel is a message's time in a transcript: 16:02 today, Oct 14,
// 16:02 before.
export function stampLabel(when, now = new Date()) {
  const d = toDate(when);
  if (!d) return "";
  const hm = `${pad2(d.getHours())}:${pad2(d.getMinutes())}`;
  return dayGroup(d, now) === "Today" ? hm : `${MONTHS[d.getMonth()]} ${d.getDate()}, ${hm}`;
}

// agoLabel says how long ago a conversation was last used (the Resumed
// divider).
export function agoLabel(when, now = new Date()) {
  const d = toDate(when);
  if (!d) return "earlier";
  const mins = Math.floor((now.getTime() - d.getTime()) / 60000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins} ${mins === 1 ? "minute" : "minutes"} ago`;
  if (dayGroup(d, now) === "Today") {
    const hours = Math.floor(mins / 60);
    return `${hours} ${hours === 1 ? "hour" : "hours"} ago`;
  }
  const days = Math.round((dayStart(now) - dayStart(d)) / 86400000);
  if (days <= 1) return "yesterday";
  return `${days} days ago`;
}

export function proposalsLabel(n) {
  const k = Number(n) || 0;
  if (!k) return "no proposals";
  return `${k} ${k === 1 ? "proposal" : "proposals"}`;
}

// sessionGroups filters the saved conversations by the search text (title
// or host, any case) and groups them Today / Yesterday / Older, keeping
// the server's order (most recent first). Empty groups are left out.
export function sessionGroups(sessions, query = "", now = new Date()) {
  const q = String(query || "").trim().toLowerCase();
  const match = (s) =>
    !q || String(s.title || "").toLowerCase().includes(q) || (s.hosts || []).some((h) => String(h).toLowerCase().includes(q));
  const groups = [
    { label: "Today", rows: [] },
    { label: "Yesterday", rows: [] },
    { label: "Older", rows: [] },
  ];
  for (const s of sessions || []) {
    if (!s || !s.id || !match(s)) continue;
    groups.find((g) => g.label === dayGroup(s.updatedAt, now)).rows.push(s);
  }
  return groups.filter((g) => g.rows.length);
}

// savedLabel is the sub row's note on where the conversation lives. A
// save error (keel serve could not write or read keel.chats) wins: the
// panel must not say "Saved" when it is not.
export function savedLabel(historyDays, saveError = "") {
  if ((Number(historyDays) || 0) <= 0) return "Not saved · kept while open";
  return saveError ? "Not saved" : "Saved in the vault";
}

// retentionNote is the session list's footnote.
export function retentionNote(historyDays) {
  const d = Number(historyDays) || 0;
  if (d <= 0) return "History is off";
  if (d === 365) return "Kept 1 year";
  return `Kept ${d} ${d === 1 ? "day" : "days"}`;
}

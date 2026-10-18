// Notes for the agent (docs/ui-redesign.md §15): the words the UI shows
// for a note and for a note the agent proposes. Pure: no DOM, no fetch.

export const MAX_NOTE_BYTES = 1000;

const months = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

// dayLabel is "Oct 12" for a timestamp, "" for none.
export function dayLabel(at) {
  const d = at ? new Date(at) : null;
  if (!d || Number.isNaN(d.getTime())) return "";
  return `${months[d.getMonth()]} ${d.getDate()}`;
}

// sourceLabel says where a note came from: "You · Oct 12", or "Proposed by
// the agent from pane prod-db-02 · Oct 14 · you approved" (and "you
// edited" when the user changed it).
export function sourceLabel(n) {
  const day = dayLabel(n?.createdAt);
  if (n?.source !== "agent") return ["You", day].filter(Boolean).join(" · ");
  const from = n.fromPane ? `Proposed by the agent from pane ${n.seenIn || "output"}` : "Proposed by the agent";
  return [from, day, n.edited ? "you edited" : "you approved"].filter(Boolean).join(" · ");
}

// countLabel is the notes list's head: "4 notes · 412 chars".
export function countLabel(notes) {
  const list = notes || [];
  const chars = list.reduce((s, n) => s + String(n?.text || "").length, 0);
  return `${list.length} ${list.length === 1 ? "note" : "notes"} · ${chars} ${chars === 1 ? "char" : "chars"}`;
}

// isInvisible is a control character, a line or paragraph separator or a
// zero-width character: keel serve turns them into spaces, and so does
// cleanNote.
function isInvisible(c) {
  return c <= 0x1f || (c >= 0x7f && c <= 0x9f) || c === 0x2028 || c === 0x2029 || (c >= 0x200b && c <= 0x200f) || c === 0x2060 || c === 0xfeff;
}

// cleanNote makes typed text one line, as keel serve keeps it, and says
// whether it can be saved.
export function cleanNote(text) {
  let out = "";
  for (const ch of String(text || "")) out += isInvisible(ch.codePointAt(0)) ? " " : ch;
  const t = out.replace(/\s+/g, " ").trim();
  const bytes = new TextEncoder().encode(t).length;
  return { text: t, ok: t.length > 0 && bytes <= MAX_NOTE_BYTES, tooLong: bytes > MAX_NOTE_BYTES };
}

// hostNotes and userNotes pick a host's notes, or the user's own, from
// GET /api/agent/notes, oldest first.
export function hostNotes(notes, hostId) {
  return (notes || []).filter((n) => hostId && n.hostId === hostId);
}

export function userNotes(notes) {
  return (notes || []).filter((n) => !n.hostId);
}

// proposalTarget is the badge on a proposed note: whose notes it joins.
export function proposalTarget(p) {
  return p?.for === "user" ? "Your notes · every host" : `Host notes · ${p?.host || "this host"}`;
}

// proposalSource is the line under a proposed note's text.
export function proposalSource(p) {
  if (p?.fromPane) return `Seen in pane ${p.seenIn || p.host || "output"} · terminal output`;
  if (p?.from === "user") return "From your messages in this conversation";
  return "From this conversation · agent summary";
}

// proposalScope says when a saved note would be sent.
export function proposalScope(p) {
  if (p?.for === "user") return "Sent with questions that read a pane the agent may read";
  return `Sent when ${p?.host || "this host"} is in scope`;
}

// proposalDone is the line a closed proposal leaves.
export function proposalDone(p) {
  if (p?.status === "saved") return p.for === "user" ? "Saved to your notes" : `Saved to host notes · ${p.host || "this host"}`;
  if (p?.status === "dismissed") return "Dismissed. Nothing was saved.";
  return "";
}

// FROM_PANE_WARNING is shown on a note whose text came from terminal output.
export const FROM_PANE_WARNING =
  "This text came from terminal output, not from you. Check it before saving: a saved note is sent to the agent with every question that reads this host, and text in a pane can be written to mislead the agent.";

// proposalWarn reports whether a proposed note gets the warning
// treatment: its text was found in pane output, or any pane was read for
// the answer that proposed it. Keel's own "from pane output" check is a
// hint a crafted pane can evade, so a read pane is reason enough.
export function proposalWarn(p) {
  return !!p?.fromPane || (p?.panesRead || []).length > 0;
}

// proposalWarning is the warning a proposed note shows, "" for none. A note
// about the user is sent on every host, so its warning is the strongest.
export function proposalWarning(p) {
  if (!proposalWarn(p)) return "";
  if (p.for === "user") {
    return "The agent read terminal output for this answer, and this note may come from it, not from you. A note about you is sent with questions on every host the agent may read. Save it only if you would have written it yourself.";
  }
  if (p.fromPane) return FROM_PANE_WARNING;
  return "The agent read terminal output for this answer, and this text may come from it, not from you. Check it before saving: a saved note is sent to the agent with every question that reads this host.";
}

// proposalOrigin says where the text was read and where the note would go,
// when they differ: "Seen in pane db-2; it would be saved to the notes of
// web-1." or "This answer read db-2 and web-1; the note would be saved to
// the notes of web-1." "" when there is nothing to tell apart.
export function proposalOrigin(p) {
  const read = (p?.panesRead || []).filter(Boolean);
  const list = read.length > 1 ? `${read.slice(0, -1).join(", ")} and ${read[read.length - 1]}` : read[0] || "";
  if (p?.for === "user") return list ? `This answer read ${list}; this note would be sent on every host.` : "";
  const host = p?.host || "this host";
  if (p?.seenIn && p.seenIn !== p.host) return `Seen in pane ${p.seenIn}; it would be saved to the notes of ${host}.`;
  if (read.some((h) => h !== p?.host)) return `This answer read ${list}; the note would be saved to the notes of ${host}.`;
  return "";
}

// noteStepText is a trace line for notes sent with a question.
export function noteStepText(step) {
  const n = Number(step?.notes) || 0;
  if (step?.noteKind === "user") return `Used your notes (${n})`;
  return `Used host notes · ${step?.host || "a host"} (${n})`;
}

// redactedToast says what was replaced before a note was kept.
export function redactedToast(n) {
  if (!n) return "";
  return n === 1 ? "1 secret-looking value was replaced before saving." : `${n} secret-looking values were replaced before saving.`;
}

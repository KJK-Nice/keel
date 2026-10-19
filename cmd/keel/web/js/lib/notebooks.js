// Notebooks and the agent (docs/ui-redesign.md §18). Pure functions: no
// DOM, no state. ui/agentpanel.js, ui/agentthread.js, ui/agentpage.js and
// term.js render with them; the node tests pin them.
//
// The panel sends only the notebooks the user attached (their ids); keel
// serve reads, redacts and frames them. A draft the agent proposes is only
// shown here: keel serve keeps its origin (the draftId) and saves it only
// when the user presses Save in the notebook editor.

// MAX_ATTACHED is how many notebooks one question may carry (keel serve
// refuses more).
export const MAX_ATTACHED = 2;

// placeholderRe matches a redaction placeholder such as [secret:1]: a
// shell glob, never the value. keel serve refuses to type one.
const placeholderRe = /\[(secret|ip|host):\d+\]/;

export function hasPlaceholder(s) {
  return placeholderRe.test(String(s || ""));
}

// redactedCount adds up a trace step's redactions, withheld values too.
export function redactedCount(counts) {
  let n = 0;
  for (const c of Object.values(counts || {})) n += Number(c) || 0;
  return n;
}

// notebookStepText is the trace line for an attached notebook:
// "Sent notebook Patroni failover (2048 bytes, 3 redacted)".
export function notebookStepText(step) {
  if (step?.status === "skipped" && step.reason === "notebook-missing") return "Skipped a notebook · it was deleted";
  const name = step?.notebook || "Notebook";
  const bytes = Number(step?.bytes) || 0;
  const parts = [`${bytes} ${bytes === 1 ? "byte" : "bytes"}`];
  if (step?.truncated) parts.push("cut by Keel");
  parts.push(`${redactedCount(step?.redactions)} redacted`);
  return `Sent notebook ${name} (${parts.join(", ")})`;
}

// --- attaching ---

// attachable are the notebooks the picker lists: saved ones, pinned first,
// then by name.
export function attachable(notebooks) {
  return (notebooks || [])
    .filter((n) => n && n.id)
    .slice()
    .sort((a, b) => (b.pinned ? 1 : 0) - (a.pinned ? 1 : 0) || String(a.name || "").localeCompare(String(b.name || "")));
}

// reconcileAttached keeps the attached ids that still name a notebook.
export function reconcileAttached(ids, notebooks) {
  const ok = new Set((notebooks || []).map((n) => n?.id).filter(Boolean));
  return (ids || []).filter((id, i, all) => ok.has(id) && all.indexOf(id) === i);
}

// toggleAttached adds or removes one notebook; never more than
// MAX_ATTACHED.
export function toggleAttached(ids, id) {
  const list = (ids || []).slice();
  if (!id) return list;
  if (list.includes(id)) return list.filter((x) => x !== id);
  if (list.length >= MAX_ATTACHED) return list;
  return [...list, id];
}

export function attachHint(n) {
  if (!n) return "";
  return `${n === 1 ? "This notebook is" : "These notebooks are"} sent, redacted, with each question until you remove ${n === 1 ? "it" : "them"}.`;
}

// --- drafts the agent proposes ---

// proposalWarn: any draft written after reading a pane gets the warning.
export function proposalWarn(p) {
  return !!(p?.fromPane || (p?.panesRead || []).length);
}

export function proposalWarning(p) {
  const where = p?.fromPane && p.seenIn ? ` Some of it matches pane ${p.seenIn}.` : "";
  return `This draft was written after reading terminal output.${where} Read it, and every command in it, before you save it: text in a pane can be written to mislead the agent.`;
}

export function proposalDone(p) {
  if (p?.status === "saved") return "Saved to Notebooks as a new notebook, marked From agent.";
  if (p?.status === "dismissed") return "Dismissed. Nothing was saved.";
  return "";
}

// preview is the first few non-empty lines of a draft, as plain text, for
// the card. The editor shows the whole draft.
export function preview(body, max = 6) {
  const lines = String(body || "")
    .split("\n")
    .map((l) => l.replace(/\s+$/, ""))
    .filter((l) => l.trim() !== "");
  const out = lines.slice(0, max);
  return { lines: out, more: Math.max(0, lines.length - out.length) };
}

export function sizeLabel(p) {
  const n = Number(p?.bytes) || 0;
  const fences = (String(p?.body || "").match(/^```/gm) || []).length / 2;
  const f = Math.floor(fences);
  return `${n} ${n === 1 ? "byte" : "bytes"}${f ? ` · ${f} ${f === 1 ? "command block" : "command blocks"}` : ""}`;
}

export const REVIEW_TITLE = "Draft from the agent. Not saved yet.";
export const REVIEW_TEXT = "Review it, edit what you need, then press Save notebook. Closing this pane discards it.";
export const PLACEHOLDER_TEXT =
  "This draft holds placeholders such as [secret:1] where Keel removed a value before the agent saw it. Fill them in (an environment variable, or a read -s prompt) before you use a command: Keel will not type a block that holds one.";
export const PLACEHOLDER_REFUSED =
  "Not typed: this block holds a redacted placeholder such as [secret:1], which a shell would read as a pattern. Edit it to the real value or an environment variable first.";

export function savedText(res) {
  const n = Number(res?.redacted) || 0;
  return n ? `Saved to Notebooks. ${n} secret-looking ${n === 1 ? "value was" : "values were"} replaced before saving.` : "Saved to Notebooks.";
}

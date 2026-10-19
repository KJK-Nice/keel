// Notebooks and the agent (docs/ui-redesign.md §18, §6 rule 12). Pure
// functions: no DOM, no state. ui/agentpanel.js, ui/agentthread.js,
// ui/agentpage.js and term.js render with them; the node tests pin them.
//
// keel serve decides everything: the agent sees an index of the notebooks
// that are not private, reads the ones the user attached or asks for, and
// creates and edits non-private notebooks at once (the store keeps the
// version before each edit, so the card's Undo restores it). A delete the
// agent proposes waits for the user's confirm. Private notebooks never
// reach the agent.

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

function bytesText(n) {
  const b = Number(n) || 0;
  return `${b} ${b === 1 ? "byte" : "bytes"}`;
}

// notebookStepText is a trace line about notebooks: the titles sent, a
// notebook sent (attached) or read (the agent asked), or why one was not.
export function notebookStepText(step) {
  if (step?.status === "notebook-index") {
    const n = Number(step.count) || 0;
    return `Sent the titles of ${n} ${n === 1 ? "notebook" : "notebooks"} (not private)`;
  }
  if (step?.status === "skipped") {
    if (step.reason === "notebook-missing") return "Skipped a notebook · it was deleted";
    if (step.reason === "notebook-private") return "Not sent · a private notebook (the agent cannot read it)";
    if (step.reason === "notebook-limit") return "Stopped reading notebooks · Keel's limit for one question";
  }
  const name = step?.notebook || "Notebook";
  const parts = [bytesText(step?.bytes)];
  if (step?.truncated) parts.push("cut by Keel");
  parts.push(`${redactedCount(step?.redactions)} redacted`);
  return step?.asked ? `Read notebook ${name} (${parts.join(", ")}) · the agent asked` : `Sent notebook ${name} (${parts.join(", ")})`;
}

export function isNotebookStep(step) {
  return step?.status === "notebook" || step?.status === "notebook-index" || /^notebook-/.test(String(step?.reason || ""));
}

// --- attaching ---

// attachable are the notebooks the picker lists: saved ones, pinned first,
// then by name. Private ones are listed but cannot be ticked.
export function attachable(notebooks) {
  return (notebooks || [])
    .filter((n) => n && n.id)
    .slice()
    .sort((a, b) => (b.pinned ? 1 : 0) - (a.pinned ? 1 : 0) || String(a.name || "").localeCompare(String(b.name || "")));
}

// reconcileAttached keeps the attached ids that still name a notebook the
// agent may read (not private).
export function reconcileAttached(ids, notebooks) {
  const ok = new Set((notebooks || []).filter((n) => n?.id && !n.private).map((n) => n.id));
  return (ids || []).filter((id, i, all) => ok.has(id) && all.indexOf(id) === i);
}

// toggleAttached adds or removes one notebook; never more than
// MAX_ATTACHED, never a private one.
export function toggleAttached(ids, id, notebooks) {
  const list = (ids || []).slice();
  if (!id) return list;
  if (list.includes(id)) return list.filter((x) => x !== id);
  if (list.length >= MAX_ATTACHED) return list;
  if (notebooks && (notebooks.find((n) => n?.id === id) || {}).private) return list;
  return [...list, id];
}

export const PRIVATE_REFUSED = "This notebook is private: the agent cannot read it. Turn Private off on the notebook first.";

export function attachHint(n) {
  if (!n) return "";
  return `${n === 1 ? "This notebook is" : "These notebooks are"} sent, redacted, with each question until you remove ${n === 1 ? "it" : "them"}.`;
}

// --- what the agent did to a notebook (the cards) ---

// warned: a change written after reading a pane gets the warning look.
export function warned(p) {
  return !!(p?.fromPane || (p?.panesRead || []).length);
}

export function paneWarning(p) {
  const where = p?.fromPane && p.seenIn ? ` Some of it matches pane ${p.seenIn}.` : "";
  return `The agent wrote this after reading terminal output.${where} Check every command in it before you type one: text in a pane can be written to mislead the agent.`;
}

const REFUSED = {
  conflict: "You changed it after the agent read it, so Keel did not apply the agent's edit. Your text is as you left it.",
  editing: "You were typing in it, so Keel did not apply the agent's edit.",
  private: "It is private, so the agent cannot change it.",
  "not-read": "The agent had not read it in this question, so Keel did not apply the edit.",
  "not-found": "It is not a notebook the agent may change.",
  deleted: "It was deleted.",
  "no-match": "The text the agent meant to change was not found exactly once, so nothing changed.",
  redacted: "Keel had removed values from it before the agent saw it, so the agent may change it only piece by piece. Nothing changed.",
  truncated: "Keel had sent only part of it, so the agent may change it only piece by piece. Nothing changed.",
  "too-large": "The edit would make the notebook larger than Keel allows. Nothing changed.",
  history: "Keel could not keep the previous version, so it made no change.",
};

export function refusedText(reason) {
  return REFUSED[reason] || "The agent's edit was not valid. Nothing changed.";
}

// cardHead is the card's title line.
export function cardHead(p) {
  switch (p?.kind) {
    case "create":
      return p.status === "refused" ? "Notebook not created" : "Created notebook";
    case "edit":
      return p.status === "refused" ? "Notebook not changed" : "Edited notebook";
    case "delete":
      return p.status === "proposed" ? "Delete notebook?" : "Delete notebook";
  }
  return "Notebook draft";
}

// cardStatus is the line under the title, or "" for none.
export function cardStatus(p) {
  const k = p?.kind;
  const s = p?.status;
  if (k === "create") {
    if (s === "created") return "Created by the agent and saved to Notebooks, marked From agent.";
    if (s === "undone") return "Undone. The notebook is in Notebooks · Recently deleted.";
    if (s === "refused") return refusedText(p.reason);
  }
  if (k === "edit") {
    if (s === "edited") return "The agent changed this notebook. Keel kept the version before it: Undo puts it back, and History has every version.";
    if (s === "undone") return "Undone. The notebook is back to the version before the agent's edit.";
    if (s === "refused") return refusedText(p.reason);
  }
  if (k === "delete") {
    if (s === "proposed") return "The agent asks to delete this notebook. Nothing is deleted unless you confirm.";
    if (s === "deleted") return "Deleted. It is in Notebooks · Recently deleted.";
    if (s === "dismissed") return "Kept. Nothing was deleted.";
    if (s === "restored") return "Restored to Notebooks.";
  }
  if (!k) {
    if (s === "saved") return "Saved to Notebooks as a new notebook, marked From agent.";
    if (s === "dismissed") return "Dismissed. Nothing was saved.";
    return "A draft from an earlier version of Keel. It was not saved; ask the agent again.";
  }
  return "";
}

// cardActions lists the buttons a card offers now: open, undo, delete
// (asks first), keep, restore.
export function cardActions(p) {
  if (!p || p.expired) {
    return p?.notebookId && (p.status === "created" || p.status === "edited" || p.status === "undone" || p.status === "saved") && p.kind !== "delete" ? ["open"] : [];
  }
  const k = p.kind;
  const s = p.status;
  if (k === "create") {
    if (s === "created") return ["open", "undo"];
    if (s === "undone" && p.versionId) return ["restore"];
  }
  if (k === "edit") {
    if (s === "edited") return ["open", "undo"];
    if (s === "undone" || s === "refused") return p.notebookId && p.reason !== "private" && p.reason !== "not-found" && p.reason !== "deleted" ? ["open"] : [];
  }
  if (k === "delete") {
    if (s === "proposed") return ["open", "keep", "delete"];
    if (s === "deleted" && p.versionId) return ["restore"];
    if (s === "restored" || s === "dismissed") return ["open"];
  }
  if (!k && s === "saved" && p.notebookId) return ["open"];
  return [];
}

export function deleteConfirmText(p) {
  return `Delete the notebook “${p?.title || "Notebook"}”? The agent asked for this. It goes to Notebooks · Recently deleted, where you can restore it for 30 days.`;
}

export const PLACEHOLDER_REFUSED =
  "Not typed: this block holds a redacted placeholder such as [secret:1], which a shell would read as a pattern. Edit it to the real value or an environment variable first.";

export const AGENT_EDIT_REVIEW =
  "The agent edited this notebook. Check what changed before you type a command from it: Keel kept the version before the edit.";

// --- history ---

const EVENT = {
  "before-agent-edit": "Before the agent's edit",
  "agent-version": "The agent's edit",
  session: "Your version",
  "before-restore": "Before a restore",
  deleted: "Deleted",
};

// versionLabel is a History row: who, when and what.
export function versionLabel(v, now = Date.now()) {
  const who = v?.by === "agent" ? "Agent" : "You";
  const what = EVENT[v?.event] || "Earlier version";
  return { who, what, when: whenText(v?.at, now), summary: v?.event === "before-agent-edit" && v.summary ? `Then: ${v.summary}` : "" };
}

export function whenText(at, now = Date.now()) {
  const d = at ? new Date(at) : null;
  if (!d || Number.isNaN(d.getTime())) return "";
  const mins = Math.round((now - d.getTime()) / 60000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins} min ago`;
  const sameDay = new Date(now).toDateString() === d.toDateString();
  const time = d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
  if (sameDay) return `today ${time}`;
  return `${d.toLocaleDateString(undefined, { month: "short", day: "numeric" })} ${time}`;
}

export function versionText(v) {
  return (v?.cells || []).map((c) => String(c?.body || "")).join("\n\n");
}

// diffLines compares two texts line by line (longest common subsequence):
// [{ op: "same" | "add" | "del", text }], b's lines added, a's removed.
// Past maxCells line pairs it gives up on alignment and shows a as
// removed and b as added.
export function diffLines(a, b, maxCells = 250000) {
  const x = String(a ?? "").split("\n");
  const y = String(b ?? "").split("\n");
  // Trim the common head and tail first: an edit is usually small.
  let head = 0;
  while (head < x.length && head < y.length && x[head] === y[head]) head++;
  let tail = 0;
  while (tail < x.length - head && tail < y.length - head && x[x.length - 1 - tail] === y[y.length - 1 - tail]) tail++;
  const xs = x.slice(head, x.length - tail);
  const ys = y.slice(head, y.length - tail);
  const out = x.slice(0, head).map((text) => ({ op: "same", text }));
  if (xs.length * ys.length > maxCells) {
    for (const text of xs) out.push({ op: "del", text });
    for (const text of ys) out.push({ op: "add", text });
  } else {
    const n = xs.length;
    const m = ys.length;
    const L = Array.from({ length: n + 1 }, () => new Uint32Array(m + 1));
    for (let i = n - 1; i >= 0; i--) for (let j = m - 1; j >= 0; j--) L[i][j] = xs[i] === ys[j] ? L[i + 1][j + 1] + 1 : Math.max(L[i + 1][j], L[i][j + 1]);
    let i = 0;
    let j = 0;
    while (i < n && j < m) {
      if (xs[i] === ys[j]) {
        out.push({ op: "same", text: xs[i] });
        i++;
        j++;
      } else if (L[i + 1][j] >= L[i][j + 1]) out.push({ op: "del", text: xs[i++] });
      else out.push({ op: "add", text: ys[j++] });
    }
    while (i < n) out.push({ op: "del", text: xs[i++] });
    while (j < m) out.push({ op: "add", text: ys[j++] });
  }
  for (const text of x.slice(x.length - tail)) out.push({ op: "same", text });
  return out;
}

// diffSummary counts a diff's added and removed lines.
export function diffSummary(d) {
  let add = 0;
  let del = 0;
  for (const l of d || []) {
    if (l.op === "add") add++;
    else if (l.op === "del") del++;
  }
  if (!add && !del) return "No changes";
  return `${add} ${add === 1 ? "line" : "lines"} added · ${del} removed`;
}

// diffView keeps the changed lines and up to `context` lines around each,
// with { op: "gap", count } for what it leaves out.
export function diffView(d, context = 2) {
  const keep = new Array(d.length).fill(false);
  d.forEach((l, i) => {
    if (l.op !== "same") for (let k = Math.max(0, i - context); k <= Math.min(d.length - 1, i + context); k++) keep[k] = true;
  });
  const out = [];
  let gap = 0;
  d.forEach((l, i) => {
    if (keep[i]) {
      if (gap) out.push({ op: "gap", count: gap });
      gap = 0;
      out.push(l);
    } else gap++;
  });
  if (gap) out.push({ op: "gap", count: gap });
  return out;
}

export function privateLabel(nb) {
  return nb?.private ? "Private" : "Agent can read";
}

// Groups, part 2 (docs/ui-redesign.md §17): the words and decisions for
// group variables, captured values and the Ansible export. Pure: no DOM,
// no fetch. The rules mirror internal/runvars (Go), which keel serve
// enforces; these only shape what the page shows.

import { escapeHtml } from "./html.js";

export const VAR_NAME_RE = /^[a-z_][a-z0-9_]{0,31}$/;
const REF_RE = /\{\{([A-Za-z_][A-Za-z0-9_]{0,31})(\|q)?\}\}/g;
// Words Go templates write alone in braces ({{end}}): never variables.
const RESERVED = new Set(["end", "else", "break", "continue", "nil", "true", "false"]);
export const MAX_CAPTURES = 4;
export const MAX_PATTERN = 256;

// refs lists the variable references in a command, in order.
export function refs(cmd) {
  const out = [];
  for (const m of String(cmd || "").matchAll(REF_RE)) {
    if (RESERVED.has(m[1]) && !m[2]) continue;
    out.push({ name: m[1], quoted: !!m[2], start: m.index, end: m.index + m[0].length });
  }
  return out;
}

// validName: a variable name as keel serve accepts one.
export function validName(name) {
  return VAR_NAME_RE.test(name) && !RESERVED.has(name);
}

// badChar: a control, C1, zero-width, bidi, separator or BOM character
// (keel serve refuses them; runvars.CheckValue).
function badChar(v) {
  for (const ch of v) {
    const c = ch.codePointAt(0);
    if (c < 0x20 || (c >= 0x7f && c <= 0x9f) || (c >= 0x200b && c <= 0x200f) || (c >= 0x2028 && c <= 0x202e) || (c >= 0x2060 && c <= 0x2064) || c === 0xfeff) return true;
  }
  return false;
}

// cleanVar is a typed variable as keel serve keeps it.
export function cleanVar(name, value) {
  const n = String(name || "")
    .trim()
    .toLowerCase();
  const v = String(value || "").trim();
  if (!validName(n)) return { ok: false, error: "A name is lower-case letters, digits and _, not starting with a digit (for example api_vip)." };
  if (badChar(v)) return { ok: false, error: "A value is one line of visible characters." };
  if (new TextEncoder().encode(v).length > 256) return { ok: false, error: "A value is at most 256 bytes." };
  return { ok: true, name: n, value: v };
}

// bytesWithVars is the exact bytes as HTML (bytesHTML for the text) with
// every {{name}} drawn as a variable chip. Names are [A-Za-z0-9_] only.
export function bytesWithVars(bytes, bytesHTML) {
  const s = String(bytes || "");
  let out = "";
  let last = 0;
  for (const r of refs(s)) {
    // bytesHTML drops a trailing line break; inside the text it is kept.
    const seg = s.slice(last, r.start);
    out += bytesHTML(seg) + (seg.endsWith("\n") ? "\n" : "");
    out += `<span class="k-gx-var">{{${escapeHtml(r.name)}${r.quoted ? "|q" : ""}}}</span>`;
    last = r.end;
  }
  return out + bytesHTML(s.slice(last));
}

// capturedBefore: the names steps before index i capture.
export function capturedBefore(steps, i) {
  const out = new Set();
  for (const st of (steps || []).slice(0, Math.max(0, i))) for (const c of st.captures || []) out.add(c.name);
  return out;
}

// usesLine is the step card's note: "Uses 2 captured values · filled in
// at Type", "Uses 1 group variable and 1 captured value · filled in at
// Type", or "" when the command names none.
export function usesLine(step, steps, i) {
  const vars = step?.vars || [];
  if (!vars.length) return "";
  const names = [...new Set(vars.map((v) => v.name))];
  const cap = capturedBefore(steps, i);
  const nCap = names.filter((n) => cap.has(n)).length;
  const nVar = names.length - nCap;
  const parts = [];
  if (nVar) parts.push(`${nVar} group ${nVar === 1 ? "variable" : "variables"}`);
  if (nCap) parts.push(`${nCap} captured ${nCap === 1 ? "value" : "values"}`);
  return `Uses ${parts.join(" and ")} · filled in at Type`;
}

const CAPTURE_TEXT = {
  captured: "captured",
  "no-match": "no match in the output",
  blocked: "not kept: it holds a value from the vault",
  refused: "not kept: a line break, control character or over 512 bytes",
  "no-output": "not captured: the shell sent no command marks (OSC 133)",
  expired: "expired",
};

// captureView is one host's capture as words: never the value. A captured
// value shows as a mask and its length.
export function captureView(cv) {
  if (!cv) return { mask: "", text: "not captured yet", state: "waiting" };
  if (cv.state === "captured") return { mask: "••••••••••••", text: `${cv.bytes} ${cv.bytes === 1 ? "byte" : "bytes"}`, state: "captured" };
  return { mask: "", text: CAPTURE_TEXT[cv.state] || cv.state, state: cv.state };
}

// expiryLine is "Kept in keel serve's memory for this run only. Deleted
// when the runbook ends, or at 11:14."
export function expiryLine(step) {
  let first = null;
  for (const c of step?.cells || []) {
    for (const cv of c.captures || []) {
      if (cv.state !== "captured" || !cv.expiresAt) continue;
      const t = new Date(cv.expiresAt);
      if (!Number.isNaN(t.getTime()) && (!first || t < first)) first = t;
    }
  }
  const at = first ? `at ${String(first.getHours()).padStart(2, "0")}:${String(first.getMinutes()).padStart(2, "0")}` : "after 1 hour";
  return `Kept in keel serve's memory for this run only. Deleted when the runbook ends, or ${at}.`;
}

// usedLine lists what went into one host's bytes: "{{join_token}} captured
// on cp-1 · step 1, {{ver}} group variable".
export function usedLine(used) {
  return (used || []).map((u) => `{{${u.name}${u.quoted ? "|q" : ""}}} ${u.from}`).join(", ");
}

// cleanCaptures is the editor's captures as keel serve takes them (empty
// rows dropped).
export function cleanCaptures(list) {
  return (list || [])
    .map((c) => ({
      name: String(c.name || "")
        .trim()
        .toLowerCase(),
      pattern: String(c.pattern || "").trim(),
    }))
    .filter((c) => c.name || c.pattern);
}

// checkCaptures says what must change in the steps' captures, or "".
export function checkCaptures(steps) {
  const seen = new Set();
  for (let i = 0; i < (steps || []).length; i++) {
    const caps = steps[i].captures || [];
    if (caps.length > MAX_CAPTURES) return `Step ${i + 1}: at most ${MAX_CAPTURES} captures.`;
    for (const c of caps) {
      if (!validName(c.name)) return `Step ${i + 1}: a capture's name is lower-case letters, digits and _ (for example join_token).`;
      if (!c.pattern) return `Step ${i + 1}: ${c.name} needs a pattern with a group around the value, such as --token\\s+(\\S+).`;
      if (c.pattern.length > MAX_PATTERN) return `Step ${i + 1}: a pattern is at most ${MAX_PATTERN} characters.`;
      if (seen.has(c.name)) return `Step ${i + 1}: ${c.name} is captured by another step already.`;
      seen.add(c.name);
    }
  }
  return "";
}

// approveGroups splits the dialog's rows by their bytes: one group when
// every host types the same, else one per distinct bytes (values captured
// per host can differ).
export function approveGroups(rows) {
  const out = [];
  for (const r of rows || []) {
    const g = out.find((x) => x.bytes === r.bytes);
    if (g) g.hosts.push(r.host);
    else out.push({ bytes: r.bytes, hosts: [r.host], used: r.used || [] });
  }
  return out;
}

// fillRows puts keel serve's fills (POST …/fill) on the approve dialog's
// rows: each row's exact bytes, what went in, and the ⇧ rule on the filled
// command. A row whose fill failed (a missing or unsafe value) keeps no
// bytes, cannot be ticked and says why.
export function fillRows(rows, fills) {
  const byId = new Map((fills || []).map((f) => [f.proposalId, f]));
  return (rows || []).map((r) => {
    const f = byId.get(r.proposalId);
    if (!f) return { ...r, bytes: "", used: [], checked: false, error: "This host's proposal is no longer waiting; it is proposed again." };
    if (f.error) return { ...r, bytes: "", used: [], checked: false, error: f.error };
    return { ...r, bytes: f.bytes, used: f.used || [], needsShift: !!f.needsShift || r.needsShift };
  });
}

// exportQuery is the export route's query string.
export function exportQuery({ format = "ini", runbookId = "", mode = "rolling", user = true, jump = false } = {}) {
  const q = new URLSearchParams({ format: format === "yaml" ? "yaml" : "ini" });
  if (runbookId) {
    q.set("runbookId", runbookId);
    q.set("mode", mode === "parallel" ? "parallel" : "rolling");
  }
  if (!user) q.set("user", "0");
  if (jump) q.set("jump", "1");
  return q.toString();
}

// fileMeta is "32 lines · YAML" / "15 lines · INI".
export function fileMeta(f) {
  const kind = /\.ya?ml$/.test(f?.name || "") ? "YAML" : "INI";
  const n = f?.lines || 0;
  return `${n} ${n === 1 ? "line" : "lines"} · ${kind}${f?.kind === "inventory" ? " · from this folder's roles and variables" : ""}`;
}

// dryRun is the board's hint after a download.
export function dryRun(files) {
  const inv = (files || []).find((f) => f.kind === "inventory");
  const pb = (files || []).find((f) => f.kind === "playbook");
  if (!inv) return "";
  return pb ? `ansible-playbook -i ${inv.name} ${pb.name} --check` : `ansible-inventory -i ${inv.name} --graph`;
}

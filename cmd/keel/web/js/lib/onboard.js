// First run and host import (docs/ui-redesign.md §5.14, §5.15): pure
// functions from keel serve's /api/ssh-config/preview to the import table,
// its selection and the import request. Every value goes through
// escapeHtml; the markup uses the same k-* classes as templ.
//
// Nothing here reads files or keys. keel serve parses ~/.ssh/config
// (internal/sshconfig) and copies a key into the vault only when the
// request names it; these helpers only describe that and build the request.

import { escapeHtml } from "./html.js";

const PATHS = {
  tick: '<path d="M5 14L8.5 17.5L19 6.5"/>',
  key: '<path d="M15 15.5C18.6 15.5 21.5 12.6 21.5 9C21.5 5.4 18.6 2.5 15 2.5C11.4 2.5 8.5 5.4 8.5 9C8.5 9.4 8.5 9.8 8.6 10.2C8.8 11 8.8 11.3 8.8 11.6C8.7 11.8 8.5 12 8.1 12.5L3.7 16.8C3.1 17.4 2.8 17.7 2.7 18.1C2.5 18.4 2.5 18.8 2.5 19.7V21.5H6V19.5H8V17.5H10L11.6 15.9C12 15.5 12.2 15.3 12.4 15.2C12.7 15.2 13 15.2 13.8 15.4C14.2 15.5 14.6 15.5 15 15.5Z"/>',
  alert:
    '<path d="M13.9 21H10.1C5.4 21 3.1 21 2.3 19.5C1.4 18 2.6 16 5 12L6.9 8.8C9.2 4.9 10.3 3 12 3C13.7 3 14.8 4.9 17.1 8.8L19 12C21.4 16 22.6 18 21.7 19.5C20.9 21 18.6 21 13.9 21Z"/><path d="M12 9V13"/><path d="M12 16.8H12.01" stroke-width="2.2"/>',
};

function icon(name, width = 1.5) {
  return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="${width}" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${PATHS[name]}</svg>`;
}

const TICK = `<span class="k-i">${icon("tick", 2.4)}</span>`;

const baseName = (path) => String(path || "").split(/[\\/]/).pop();

// defaultSelection: every host that is not saved yet and needs no proxy is
// on; saved hosts and proxy hosts are off (the user may still turn them
// on). Every key copy is off: copying a private key is always opt-in.
export function defaultSelection(preview) {
  const hosts = new Set();
  for (const h of preview?.hosts || []) {
    if (!h.saved && !h.proxy) hosts.add(h.alias);
  }
  const keys = new Map();
  for (const k of preview?.keys || []) keys.set(k.path, { on: false, passphrase: "" });
  return { hosts, keys };
}

export function hostAddress(h) {
  const addr = String(h.hostName || h.alias || "");
  return h.port && h.port !== 22 ? `${addr}:${h.port}` : addr;
}

// copyable is false for a key keel serve cannot read (missing, too large,
// not a private key): its switch is disabled.
export const copyable = (k) => !!k && k.exists && !k.problem;

export function leadText(preview) {
  const path = preview?.path || "~/.ssh/config";
  if (!preview?.found) return `Keel found no ${path}. Add hosts later with New host.`;
  const n = (preview.hosts || []).length;
  const hosts = n === 1 ? "1 host" : `${n} hosts`;
  return `Keel found ${path} with ${hosts}. Pick the ones to add. Your config file is not changed.`;
}

export function selectedCount(preview, sel) {
  return (preview?.hosts || []).filter((h) => sel.hosts.has(h.alias)).length;
}

export function addLabel(n) {
  if (n === 0) return "Add hosts";
  return n === 1 ? "Add 1 host" : `Add ${n} hosts`;
}

export function summaryText(preview, sel) {
  const total = (preview?.hosts || []).length;
  return `${selectedCount(preview, sel)} of ${total} selected`;
}

function viaCell(h) {
  if (!h.proxy) return '<span class="k-faint">—</span>';
  const what = h.proxy === "ProxyJump" && h.proxyJump ? `ProxyJump ${h.proxyJump}` : h.proxy;
  return `<span class="k-badge k-badge--signal" title="${escapeHtml(what)} is not supported yet; the host is added without it">${icon("alert", 1.8)}${escapeHtml(what)}, not used</span>`;
}

function keyCell(h) {
  const names = (h.identityFiles || []).map(baseName);
  if (!names.length) return '<span class="k-faint">—</span>';
  return `<span class="k-mono k-muted">${escapeHtml(names.join(", "))}</span>`;
}

// checkbox: state is true, false or "mixed".
function checkbox(state, label, attrs) {
  const aria = state === "mixed" ? "mixed" : state ? "true" : "false";
  return `<button type="button" class="k-check" role="checkbox" aria-checked="${aria}" aria-label="${escapeHtml(label)}" ${attrs}>${TICK}</button>`;
}

// allState is the select-all box: "true", "false" or "mixed".
export function allState(preview, sel) {
  const n = selectedCount(preview, sel);
  const total = (preview?.hosts || []).length;
  if (n === 0) return "false";
  return n === total ? "true" : "mixed";
}

const allValue = (preview, sel) => {
  const st = allState(preview, sel);
  return st === "mixed" ? "mixed" : st === "true";
};

export function hostTableHTML(preview, sel) {
  const hosts = preview?.hosts || [];
  if (!hosts.length) {
    return `<p class="k-help k-imp-empty">${escapeHtml(preview?.found ? "No hosts to add: every Host line in the config is a pattern." : "Nothing to import.")}</p>`;
  }
  const rows = hosts
    .map((h) => {
      const on = sel.hosts.has(h.alias);
      const saved = h.saved ? ' <span class="k-badge k-badge--haze">Already saved</span>' : "";
      return `<tr data-imp-row="${escapeHtml(h.alias)}">
<td>${checkbox(on, `Add ${h.alias}`, `data-imp-host="${escapeHtml(h.alias)}"`)}</td>
<td><span class="k-mono">${escapeHtml(h.alias)}</span>${saved}</td>
<td class="k-mono k-muted">${escapeHtml(hostAddress(h))}</td>
<td class="k-mono k-muted">${escapeHtml(h.user || "")}</td>
<td>${keyCell(h)}</td>
<td>${viaCell(h)}</td>
</tr>`;
    })
    .join("");
  return `<div class="k-imp-table">
<table class="k-table" aria-label="Hosts in ${escapeHtml(preview.path || "~/.ssh/config")}">
<thead><tr><th>${checkbox(allValue(preview, sel), "Select all hosts", 'data-imp-all=""')}</th><th>Host</th><th>Address</th><th>User</th><th>Key</th><th>Via</th></tr></thead>
<tbody>${rows}</tbody>
</table>
<div class="k-imp-foot"><span id="imp-summary">${escapeHtml(summaryText(preview, sel))}</span></div>
</div>`;
}

export function patternsHTML(preview) {
  const pats = preview?.patterns || [];
  if (!pats.length) return "";
  const items = pats.map((p) => `<li class="k-badge k-badge--mono k-badge--outline" title="${escapeHtml(p.source)}">${escapeHtml(p.pattern)}</li>`).join("");
  return `<div class="k-imp-patterns" role="group" aria-label="Patterns">
<p class="k-help">${pats.length === 1 ? "This pattern sets" : `These ${pats.length} patterns set`} options for the hosts above. ${pats.length === 1 ? "It is" : "They are"} not ${pats.length === 1 ? "a host" : "hosts"} to add.</p>
<ul class="k-chips">${items}</ul>
</div>`;
}

// notesList merges the config's notes with the hosts' own (bad Port,
// unexpanded tokens), proxy notes aside: the Via column shows those.
export function notesList(preview) {
  const out = (preview?.notes || []).map((n) => ({ source: n.source, text: n.text }));
  for (const h of preview?.hosts || []) {
    for (const t of h.notes || []) {
      if (h.proxy && t.startsWith(h.proxy)) continue;
      out.push({ source: h.alias, text: t });
    }
  }
  return out;
}

export function notesHTML(preview) {
  const notes = notesList(preview);
  if (!notes.length) return "";
  const items = notes.map((n) => `<li><span class="k-mono k-muted">${escapeHtml(n.source)}</span> ${escapeHtml(n.text)}</li>`).join("");
  return `<details class="k-imp-notes"><summary>${notes.length === 1 ? "1 note" : `${notes.length} notes`} about this config</summary><ul>${items}</ul></details>`;
}

export function keysHTML(preview, sel) {
  const keys = preview?.keys || [];
  if (!keys.length) return "";
  const rows = keys
    .map((k, i) => {
      const st = sel.keys.get(k.path) || { on: false, passphrase: "" };
      const ok = copyable(k);
      const used = (k.usedBy || []).join(", ");
      const badge = k.encrypted ? ' <span class="k-badge k-badge--outline">Passphrase</span>' : "";
      const problem = ok ? "" : `<span class="k-error">${escapeHtml(k.problem || "cannot be read")}</span>`;
      const label = `Copy ${k.display || k.name} into the vault`;
      const sw = `<button type="button" class="k-switch" role="switch" aria-checked="${st.on && ok ? "true" : "false"}" aria-label="${escapeHtml(label)}" data-imp-key="${i}"${ok ? "" : ' disabled aria-disabled="true"'}></button>`;
      const pass = k.encrypted
        ? `<div class="k-field k-imp-pass" data-imp-pass-field="${i}"${st.on ? "" : " hidden"}>
<label class="k-label" for="imp-pass-${i}">Passphrase for ${escapeHtml(k.name)}</label>
<input class="k-input" type="password" id="imp-pass-${i}" data-imp-pass="${i}" autocomplete="off" spellcheck="false" value="${escapeHtml(st.passphrase)}"/>
<p class="k-error" data-imp-key-error="${i}" role="alert" hidden></p>
</div>`
        : `<p class="k-error" data-imp-key-error="${i}" role="alert" hidden></p>`;
      return `<li class="k-imp-key">
<div class="k-imp-key-row"><div class="k-imp-key-main"><span class="k-mono">${escapeHtml(k.display || k.path)}</span>${badge}<span class="k-help">${used ? `Used by ${escapeHtml(used)}` : ""}</span>${problem}</div>${sw}</div>
${pass}
</li>`;
    })
    .join("");
  return `<div class="k-box k-imp-keys" role="group" aria-label="Private keys">
<div class="k-imp-keys-head">${icon("key")}<span>Copy private keys into the vault</span></div>
<ul class="k-imp-key-list">${rows}</ul>
<p class="k-help">Each key is off until you turn it on. The files in <span class="k-mono">~/.ssh</span> stay where they are; Keel only reads them. A copied key becomes a Keychain identity, encrypted in the vault with its passphrase. Hosts whose key stays off are added with password sign-in; edit them to pick a key.</p>
</div>`;
}

export function importHTML(preview, sel) {
  if (!preview?.found) return "";
  return [hostTableHTML(preview, sel), patternsHTML(preview), notesHTML(preview), keysHTML(preview, sel)].filter(Boolean).join("\n");
}

// importRequest is the body of POST /api/ssh-config/import: host aliases
// and the keys to copy. Only keys that are on and copyable are sent, and a
// passphrase only for a key that has one.
export function importRequest(preview, sel) {
  const hosts = (preview?.hosts || []).filter((h) => sel.hosts.has(h.alias)).map((h) => h.alias);
  const keys = [];
  for (const k of preview?.keys || []) {
    const st = sel.keys.get(k.path);
    if (!st?.on || !copyable(k)) continue;
    keys.push(k.encrypted ? { path: k.path, passphrase: st.passphrase || "" } : { path: k.path });
  }
  return { hosts, keys };
}

// missingPassphrase is the index of the first key turned on whose
// passphrase is still empty, or -1.
export function missingPassphrase(preview, sel) {
  return (preview?.keys || []).findIndex((k) => {
    const st = sel.keys.get(k.path);
    return st?.on && copyable(k) && k.encrypted && !st.passphrase;
  });
}

export function importedText(res) {
  const h = (res?.hosts || []).length;
  const k = (res?.identities || []).length;
  const hosts = h === 1 ? "1 host" : `${h} hosts`;
  if (!k) return `Added ${hosts}`;
  return `Added ${hosts} and ${k === 1 ? "1 key" : `${k} keys`}`;
}

// Vault markup term.js paints from the page's selection and state: the
// glyphs, the Snippets editor preview, the Keychain detail, and the tunnel
// and notebook helpers the sidebar sections use. The lists themselves are
// templ (internal/ui/lists.templ) pushed over the Datastar stream
// (docs/ui-redesign.md §12). Every value from the vault goes through
// escapeHtml.

import { escapeHtml } from "./html.js";

const svg = (paths) =>
  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths}</svg>`;

const GLYPHS = {
  chevron: '<path d="M18 9.00005C18 9.00005 13.5811 15 12 15C10.4188 15 6 9 6 9"/>',
  more: '<path d="M11.992 12H12.001"/><path d="M18 12H18.009"/><path d="M6 12H6.009"/>',
  server:
    '<path d="M19 4H5C4.06812 4 3.60218 4 3.23463 4.15224C2.74458 4.35523 2.35523 4.74458 2.15224 5.23463C2 5.60218 2 6.06812 2 7C2 7.93188 2 8.39782 2.15224 8.76537C2.35523 9.25542 2.74458 9.64477 3.23463 9.84776C3.60218 10 4.06812 10 5 10H19C19.9319 10 20.3978 10 20.7654 9.84776C21.2554 9.64477 21.6448 9.25542 21.8478 8.76537C22 8.39782 22 7.93188 22 7C22 6.06812 22 5.60218 21.8478 5.23463C21.6448 4.74458 21.2554 4.35523 20.7654 4.15224C20.3978 4 19.9319 4 19 4Z"/><path d="M19 14H5C4.06812 14 3.60218 14 3.23463 14.1522C2.74458 14.3552 2.35523 14.7446 2.15224 15.2346C2 15.6022 2 16.0681 2 17C2 17.9319 2 18.3978 2.15224 18.7654C2.35523 19.2554 2.74458 19.6448 3.23463 19.8478C3.60218 20 4.06812 20 5 20H19C19.9319 20 20.3978 20 20.7654 19.8478C21.2554 19.6448 21.6448 19.2554 21.8478 18.7654C22 18.3978 22 17.9319 22 17C22 16.0681 22 15.6022 21.8478 15.2346C21.6448 14.7446 21.2554 14.3552 20.7654 14.1522C20.3978 14 19.9319 14 19 14Z"/>',
  local:
    '<path d="M7.5 7.5L8.72654 8.55719C9.24218 9.00163 9.5 9.22386 9.5 9.5C9.5 9.77614 9.24218 9.99836 8.72654 10.4428L7.5 11.5"/><path d="M11.5 12.5H15.5"/><path d="M12 21C15.7497 21 17.6246 21 18.9389 20.0451C19.3634 19.7367 19.7367 19.3634 20.0451 18.9389C21 17.6246 21 15.7497 21 12C21 8.25027 21 6.3754 20.0451 5.06107C19.7367 4.6366 19.3634 4.26331 18.9389 3.95491C17.6246 3 15.7497 3 12 3C8.25027 3 6.3754 3 5.06107 3.95491C4.6366 4.26331 4.26331 4.6366 3.95491 5.06107C3 6.3754 3 8.25027 3 12C3 15.7497 3 17.6246 3.95491 18.9389C4.26331 19.3634 4.6366 19.7367 5.06107 20.0451C6.3754 21 8.25027 21 12 21Z"/>',
  key: '<path d="M15.5 14.5C18.8137 14.5 21.5 11.8137 21.5 8.5C21.5 5.18629 18.8137 2.5 15.5 2.5C12.1863 2.5 9.5 5.18629 9.5 8.5C9.5 9.38041 9.68962 10.2165 10.0303 10.9697L2.5 18.5V21.5H5.5V19.5H7.5V17.5H9.5L13.0303 13.9697C13.7835 14.3104 14.6196 14.5 15.5 14.5Z"/><path d="M17.5 6.5L16.5 7.5"/>',
  password:
    '<path d="M4 16C4 13.1716 4 11.7574 4.87868 10.8787C5.75736 10 7.17157 10 10 10H14C16.8284 10 18.2426 10 19.1213 10.8787C20 11.7574 20 13.1716 20 16C20 18.8284 20 20.2426 19.1213 21.1213C18.2426 22 16.8284 22 14 22H10C7.17157 22 5.75736 22 4.87868 21.1213C4 20.2426 4 18.8284 4 16Z"/><path d="M8 10V7C8 4.79086 9.79086 3 12 3C14.2091 3 16 4.79086 16 7V10"/><path d="M12 15V17"/>',
  snippet: '<path d="M15 4L9 20"/><path d="M5.99997 16C5.99997 16 2.00001 13.054 2 12C1.99999 10.9459 6 8 6 8"/><path d="M18 8C18 8 22 10.946 22 12C22 13.0541 18 16 18 16"/>',
  notebook: '<path d="M9.99219 11H13.4922M9.99219 7H16.9922"/><path d="M5.99219 6H2.99219M5.99219 12H2.99219M5.99219 18H2.99219"/><path d="M8.99219 21.9388C7.41097 21.8363 6.39738 21.562 5.66376 20.8284C4.49219 19.6569 4.49219 17.7712 4.49219 14V10C4.49219 6.22877 4.49219 4.34315 5.66376 3.17158C6.83533 2 8.72096 2 12.4922 2H12.9922C16.7634 2 18.649 2 19.8206 3.17157C20.9922 4.34315 20.9922 6.22877 20.9922 10V10.5"/>',
  alert: '<path d="M5.32171 9.6829C7.73539 5.41196 8.94222 3.27648 10.5983 2.72678C11.5093 2.42437 12.4907 2.42437 13.4017 2.72678C15.0578 3.27648 16.2646 5.41196 18.6783 9.6829C21.092 13.9538 22.2988 16.0893 21.9368 17.8293C21.7376 18.7866 21.2469 19.6548 20.535 20.3097C19.241 21.5 16.8274 21.5 12 21.5C7.17265 21.5 4.75897 21.5 3.46496 20.3097C2.75308 19.6548 2.26239 18.7866 2.06322 17.8293C1.70119 16.0893 2.90803 13.9538 5.32171 9.6829Z"/><path d="M11.992 16H12.001"/><path d="M12 13L12 8.99997"/>',
};

export function glyphSVG(kind) {
  return svg(GLYPHS[kind] || GLYPHS.server);
}

// snippetPreview is the exact bytes a snippet types after Approve: each
// line and its Enter (shown as ↵), with the line and byte count.
export function snippetPreview(text) {
  let body = String(text ?? "");
  if (body && !body.endsWith("\n")) body += "\n";
  const lines = body ? body.split("\n").slice(0, -1) : [];
  const html = lines.map((l) => `${escapeHtml(l)}<span class="k-ctl" aria-label="Enter">↵</span>`).join("");
  const bytes = new TextEncoder().encode(body).length;
  const summary = `${lines.length} ${lines.length === 1 ? "line" : "lines"} · ${bytes} ${bytes === 1 ? "byte" : "bytes"}. Typing always goes through Approve before type.`;
  return { html, summary, bytes, lines: lines.length };
}

// sortedNotebooks puts pinned notebooks first, then the most recently
// updated, then by name.
export function sortedNotebooks(notebooks) {
  return [...(notebooks || [])].sort(
    (a, b) =>
      Number(!!b.pinned) - Number(!!a.pinned) ||
      String(b.updatedAt || "").localeCompare(String(a.updatedAt || "")) ||
      String(a.name || "").localeCompare(String(b.name || "")),
  );
}

// identityHosts are the saved hosts that authenticate with identity id.
export function identityHosts(hosts, id) {
  return (hosts || []).filter((h) => h.authType === "identity" && h.identityId === id);
}

function identityKind(id) {
  return id?.kind === "key" ? "key" : "password";
}

function dateLabel(iso) {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" });
}

// identityDetailHTML is the Keychain page's detail for the selected
// identity. It never shows a secret: only whether one is stored. An
// identity hosts still use cannot be deleted from here.
export function identityDetailHTML(id, hosts) {
  if (!id) return `<div class="k-detail-empty"><p class="k-help">Select an identity, or add one with New identity.</p></div>`;
  const kind = identityKind(id);
  const used = identityHosts(hosts, id.id);
  const added = dateLabel(id.createdAt);
  const secretRow =
    kind === "key"
      ? `<dt>Private key</dt><dd>${id.privateKey ? "Encrypted in the vault" : "Not stored"}</dd>
         <dt>Passphrase</dt><dd>${id.passphrase ? "Stored in the vault" : "None"}</dd>`
      : `<dt>Password</dt><dd>${id.password ? "Encrypted in the vault" : "Not stored"}</dd>`;
  const usedBy = used.length
    ? `<div class="k-chiprow">${used.map((h) => `<span class="k-badge k-badge--mono">${escapeHtml(h.name)}</span>`).join("")}</div>`
    : `<p class="k-help">No host uses it yet. Pick it in a host's Authentication.</p>`;
  const inUse = used.length > 0;
  const hostWord = `${used.length} ${used.length === 1 ? "host" : "hosts"}`;
  return `<div class="k-detail-head">
      <span class="k-tile k-tile--lg">${glyphSVG(kind)}</span>
      <div class="k-detail-title"><h2>${escapeHtml(id.name)}</h2><span class="k-item-sub">${kind === "key" ? "SSH key" : "Password"}${added ? ` · added ${escapeHtml(added)}` : ""}</span></div>
      <button type="button" class="k-btn" data-edit-identity="${escapeHtml(id.id)}">Edit</button>
    </div>
    <div class="k-box">
      <dl class="k-kv">
        <dt>Username</dt><dd class="k-mono">${escapeHtml(id.username || "—")}</dd>
        ${secretRow}
      </dl>
    </div>
    <div class="k-field">
      <span class="k-label">Used by ${hostWord}</span>
      ${usedBy}
    </div>
    <div class="k-detail-foot">
      <span class="k-spacer"></span>
      <button type="button" class="k-btn k-btn--danger k-btn--sm" data-delete-identity="${escapeHtml(id.id)}"${inUse ? ` aria-disabled="true" title="Remove it from ${hostWord} first"` : ""}>Delete identity</button>
    </div>
    ${inUse ? `<p class="k-help k-detail-note">Remove it from ${hostWord} before you can delete it.</p>` : ""}`;
}

export function tunnelListen(t) {
  return t.listenAddr || `${t.bindAddr}:${t.bindPort || "auto"}`;
}

export function tunnelDest(t) {
  return t.kind === "dynamic" ? "SOCKS5" : `${t.destHost}:${t.destPort}`;
}

export function tunnelRoute(t) {
  return `${tunnelListen(t)} → ${tunnelDest(t)}`;
}

// tunnelState is how a forward reads: a dot and a word, never colour alone.
export function tunnelState(t) {
  switch (t?.status) {
    case "ready":
      return { label: "Running", dot: "k-dot--live", state: "k-state--live", filter: "running", on: true };
    case "connecting":
      return { label: "Connecting", dot: "k-dot--connecting", state: "k-state--connecting", filter: "running", on: true };
    case "error":
      return { label: t.error ? `Failed · ${t.error}` : "Failed", dot: "k-dot--dead", state: "k-state--dead", filter: "stopped", on: false };
    default:
      return { label: "Stopped", dot: "k-dot--idle", state: "", filter: "stopped", on: false };
  }
}

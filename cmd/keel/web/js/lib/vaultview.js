// Vault markup for the sidebar host tree and the tab pages (Keychain, Port
// forwarding, Snippets, Notebooks, Logs). Pure functions from data to HTML
// strings with the same k-* classes and data-* hooks as the templ
// fragments (internal/ui/lists.templ); term.js uses them when a fragment
// cannot be fetched and for the parts painted from the selection.
// Every value from the vault goes through escapeHtml.

import { escapeHtml } from "./html.js";
import { hostSubtitle } from "./groups.js";

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

// A plain (no menu library) action trigger and its items, for the fallback
// lists; the templ fragments use the dropdown menu component instead.
const actionButtons = (items) =>
  items
    .map(
      ([attr, id, label, danger]) =>
        `<button type="button" class="k-btn k-btn--ghost k-btn--sm${danger ? " k-btn--danger" : ""}" ${attr}="${escapeHtml(id)}">${escapeHtml(label)}</button>`,
    )
    .join("");

export function sideEmptyHTML(title, desc) {
  return `<div class="k-side-empty" data-slot="empty"><p class="k-side-empty-title">${escapeHtml(title)}</p><p class="k-help">${escapeHtml(desc)}</p></div>`;
}

function hostTile(h) {
  const style = h.color ? ` style="--host:${escapeHtml(h.color)}"` : "";
  return `<span class="k-tile k-tile--sm"${style}>${glyphSVG(h.authType === "local" ? "local" : "server")}</span>`;
}

export function treeHostHTML(h) {
  const id = escapeHtml(h.id);
  const menu = [["data-edit", h.id, "Edit"]];
  if (h.authType !== "local") menu.push(["data-duplicate", h.id, "Duplicate"], ["data-delete", h.id, "Remove", true]);
  return `<div class="k-tree-row k-tree-hostrow" data-id="${id}" draggable="true" data-drag-host="${id}">
      <button type="button" class="k-tree-host" data-connect="${id}" title="${escapeHtml(hostSubtitle(h))}">
        ${hostTile(h)}
        <span class="k-tree-name">${escapeHtml(h.name)}</span>
        <span class="k-tree-live" data-host-live="${id}" hidden></span>
      </button>
      <div class="k-tree-menu k-tree-menu--inline">${actionButtons(menu)}</div>
    </div>`;
}

export function treeGroupHTML(n) {
  const g = n.group;
  const id = escapeHtml(g.id);
  const children = (n.hosts || []).map(treeHostHTML).join("") + (n.kids || []).map(treeGroupHTML).join("");
  return `<div class="group-tree-node k-tree-group" data-group-id="${id}" data-group-drop="${id}" data-group-parent="${escapeHtml(g.parentId || "")}" data-group-depth="${n.depth}" data-group-name="${escapeHtml(g.name)}">
      <div class="group-tree-heading k-tree-row" draggable="true" data-drag-group="${id}">
        <button type="button" class="k-tree-grp" data-group-toggle="${id}" aria-expanded="true">
          <span class="k-tree-chev">${glyphSVG("chevron")}</span>
          <span class="k-tree-grp-name" data-group-title="${id}">${escapeHtml(g.name)}</span>
          <span class="k-tree-count">${n.count}</span>
        </button>
        <div class="k-tree-menu k-tree-menu--inline" data-group-menu="${id}">${actionButtons([
          ["data-group-edit", g.id, "Edit"],
          ["data-group-new", g.id, "New subgroup"],
          ["data-group-rename", g.id, "Rename"],
          ["data-group-move", g.id, "Move into…"],
          ["data-group-delete", g.id, "Remove", true],
        ])}</div>
      </div>
      <div class="group-tree-children">${children}</div>
    </div>`;
}

// hostsHTML is the sidebar tree: hosts outside any group, then the groups
// (from nestGroupTree) with their hosts and subgroups.
export function hostsHTML({ tree, loose }) {
  if (!tree.length && !loose.length) return sideEmptyHTML("No hosts match", "Add a host or clear the filter.");
  return loose.map(treeHostHTML).join("") + tree.map(treeGroupHTML).join("");
}

const itemHTML = ({ attr, id, glyph, name, sub, badge, filter }) =>
  `<button type="button" class="k-item" ${attr}="${escapeHtml(id)}" data-filter-text="${escapeHtml(filter)}" aria-selected="false">
      <span class="k-tile">${glyphSVG(glyph)}</span>
      <span class="k-item-main"><span class="k-item-name">${escapeHtml(name)}</span><span class="k-item-sub">${escapeHtml(sub)}</span></span>
      ${badge ? `<span class="k-badge">${escapeHtml(badge)}</span>` : "<span></span>"}
    </button>`;

export function snippetFirstLine(command) {
  const lines = String(command || "").trim().split("\n");
  return lines.length > 1 ? `${lines[0].trim()} …` : lines[0].trim();
}

export function snippetsHTML(snippets) {
  if (!snippets?.length) {
    return sideEmptyHTML("No snippets yet", "Save commands you type often. Typing one always goes through Approve before type.");
  }
  return snippets
    .map((s) =>
      itemHTML({
        attr: "data-snippet-select",
        id: s.id,
        glyph: "snippet",
        name: s.name,
        sub: snippetFirstLine(s.command),
        filter: `${s.name} ${s.command}`,
      }),
    )
    .join("");
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

export function notebookPreview(nb) {
  for (const cell of nb?.cells || []) {
    const line = String(cell.body || "")
      .split("\n")
      .map((s) => s.trim())
      .find(Boolean);
    if (line) return line;
  }
  return "Empty notebook";
}

export function notebooksHTML(notebooks) {
  if (!notebooks?.length) {
    return sideEmptyHTML(
      "No notebooks yet",
      "Write markdown runbooks. Fenced sh and bash blocks fill a terminal's input; nothing is typed until you confirm.",
    );
  }
  return sortedNotebooks(notebooks)
    .map((nb) => {
      const id = escapeHtml(nb.id);
      const pin = nb.pinned ? "Unpin" : "Pin";
      return `<div class="k-rowitem" data-id="${id}" data-filter-text="${escapeHtml(`${nb.name || ""} ${notebookPreview(nb)}`)}">
          <button type="button" class="k-rowitem-main" data-notebook-open="${id}">
            <span class="k-tile">${glyphSVG("notebook")}</span>
            <span class="k-item-main"><span class="k-item-name">${escapeHtml(nb.name || "Notebook")}</span><span class="k-item-sub">${escapeHtml(notebookPreview(nb))}</span></span>
          </button>
          ${nb.pinned ? `<span class="k-badge">Pinned</span>` : ""}
          ${actionButtons([
            ["data-notebook-rename", nb.id, "Rename"],
            ["data-notebook-pin", nb.id, pin],
            ["data-delete-notebook", nb.id, "Remove", true],
          ])}
        </div>`;
    })
    .join("");
}

// identityHosts are the saved hosts that authenticate with identity id.
export function identityHosts(hosts, id) {
  return (hosts || []).filter((h) => h.authType === "identity" && h.identityId === id);
}

function identityKind(id) {
  return id?.kind === "key" ? "key" : "password";
}

function identitySubtitle(id, hosts) {
  const n = identityHosts(hosts, id.id).length;
  return [identityKind(id) === "key" ? "SSH key" : "Password", id.username || "", `${n} ${n === 1 ? "host" : "hosts"}`].filter(Boolean).join(" · ");
}

export function identitiesHTML(identities, hosts) {
  if (!identities?.length) return sideEmptyHTML("No identities yet", "Add an SSH key or a password and attach it to hosts.");
  const out = [];
  for (const [kind, label] of [
    ["key", "SSH keys"],
    ["password", "Passwords"],
  ]) {
    const list = identities.filter((id) => identityKind(id) === kind);
    if (!list.length) continue;
    out.push(`<div class="k-split-label">${label}</div>`);
    for (const id of list) {
      out.push(
        itemHTML({
          attr: "data-identity-select",
          id: id.id,
          glyph: kind,
          name: id.name,
          sub: identitySubtitle(id, hosts),
          badge: kind === "key" && id.passphrase ? "passphrase" : "",
          filter: `${id.name} ${id.username || ""}`,
        }),
      );
    }
  }
  return out.join("");
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

const TUNNEL_FLAGS = { local: "-L", remote: "-R", dynamic: "-D" };

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

export function tunnelEveryInterface(t) {
  return ["0.0.0.0", "::", "[::]", "*"].includes(String(t?.bindAddr || "").trim());
}

export function tunnelsHTML(tunnels, hosts) {
  if (!tunnels?.length) {
    return `<tr class="k-table-empty"><td colspan="7">${sideEmptyHTML("No forwards yet", "Local (-L), remote (-R) or dynamic SOCKS5 (-D), each through a saved host.")}</td></tr>`;
  }
  return tunnels
    .map((t) => {
      const host = (hosts || []).find((h) => h.id === t.hostId);
      const st = tunnelState(t);
      const id = escapeHtml(t.id);
      const every = tunnelEveryInterface(t) ? ` <span class="k-badge k-badge--signal">${glyphSVG("alert")}Every interface</span>` : "";
      const retry = t.status === "error" ? ` <button type="button" class="k-btn k-btn--ghost k-btn--sm" data-tunnel-start="${id}">Retry</button>` : "";
      const toggle = st.on ? `data-tunnel-stop="${id}"` : `data-tunnel-start="${id}"`;
      return `<tr data-id="${id}" data-status="${st.filter}" data-filter-text="${escapeHtml(`${t.name} ${host?.name || ""} ${tunnelRoute(t)}`)}">
          <td><span class="k-cell-name">${escapeHtml(t.name)}</span></td>
          <td><span class="k-badge k-badge--mono">${TUNNEL_FLAGS[t.kind] || "-L"}</span></td>
          <td><span class="k-flow">${escapeHtml(tunnelListen(t))}<span class="k-flow-arrow" aria-hidden="true">→</span>${escapeHtml(tunnelDest(t))}</span>${every}</td>
          <td><span class="k-mono k-muted">${escapeHtml(host?.name || t.hostId || "")}</span></td>
          <td><span class="k-state ${st.state}"><span class="k-dot ${st.dot}" aria-hidden="true"></span>${escapeHtml(st.label)}</span>${retry}</td>
          <td class="k-cell-end"><button type="button" class="k-switch" role="switch" aria-checked="${st.on}" aria-label="${escapeHtml(t.name)} on" ${toggle}></button></td>
          <td class="k-cell-end">${actionButtons([
            ["data-edit-tunnel", t.id, "Edit"],
            ["data-delete-tunnel", t.id, "Remove", true],
          ])}</td>
        </tr>`;
    })
    .join("");
}

export function logParts(iso) {
  if (!iso) return { date: "", time: "" };
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return { date: "", time: "" };
  return {
    date: d.toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" }),
    time: d.toLocaleTimeString(undefined, { hour12: false, hour: "2-digit", minute: "2-digit", second: "2-digit" }),
  };
}

// logGroup is the Logs chip a log kind belongs to.
export function logGroup(kind) {
  const k = String(kind || "").toLowerCase();
  if (k === "session") return "sessions";
  if (k === "tunnel") return "forwards";
  // agent.read / agent.suggest / agent.insert / agent.type and agent
  // settings changes (the Agent chip on the Logs page).
  if (k === "agent" || k.startsWith("agent.")) return "agent";
  if (["vault", "host", "group", "identity", "snippet", "notebook"].includes(k)) return "vault";
  return "other";
}

function logTarget(e, ctx) {
  if (e.tunnelId) {
    const t = (ctx?.tunnels || []).find((x) => x.id === e.tunnelId);
    if (t) return t.name;
  }
  if (e.hostId) {
    const h = (ctx?.hosts || []).find((x) => x.id === e.hostId);
    if (h) return h.name;
  }
  return "—";
}

// logFacts is the list of short labels an agent entry carries: lines, bytes,
// prompt lines sent, redactions, source, model and Jev timings (the same as
// internal/ui logFacts, which renders the server-side rows).
export function logFacts(e) {
  const out = [];
  const n = (v) => Number(v) || 0;
  if (n(e.count) > 1) out.push(`${n(e.count)} sent`);
  else if (n(e.lines) > 0) out.push(`${n(e.lines)} ${n(e.lines) === 1 ? "line" : "lines"}`);
  if (n(e.bytes) > 0) out.push(`${n(e.bytes)} ${n(e.bytes) === 1 ? "byte" : "bytes"}`);
  const red = Object.values(e.redactions || {}).reduce((a, b) => a + n(b), 0);
  if (red > 0) out.push(`${red} redacted`);
  if (e.source) out.push(`via ${e.source}`);
  if (n(e.modelMs) > 0) out.push(`model ${n(e.modelMs)} ms`);
  if (n(e.jevMs) > 0) out.push(`Jev ${n(e.jevMs)} ms`);
  return out;
}

function logFactsHTML(e) {
  const facts = logFacts(e);
  if (!facts.length) return "";
  const kinds = Object.keys(e.redactions || {}).sort();
  const title = kinds.length ? ` title="${escapeHtml("Redacted: " + kinds.map((k) => `${k} ${e.redactions[k]}`).join(", "))}"` : "";
  return `<span class="k-log-facts"${title}>${facts.map((f) => `<span class="k-badge k-badge--outline">${escapeHtml(f)}</span>`).join("")}</span>`;
}

// logsHTML lists log entries whose kind or message contains filter.
export function logsHTML(logs, filter, ctx) {
  const q = String(filter || "").trim().toLowerCase();
  const rows = (logs || []).filter((e) => !q || `${e.kind} ${e.message}`.toLowerCase().includes(q));
  if (!rows.length) {
    return `<tr class="k-table-empty"><td colspan="4">${sideEmptyHTML("No log entries", "Sessions, forwards and vault changes show up here.")}</td></tr>`;
  }
  return rows
    .map((e) => {
      const { date, time } = logParts(e.time);
      const k = String(e.kind || "").toLowerCase();
      const flare = k === "error" || k === "fail" || k === "denied" ? " k-badge--flare" : "";
      return `<tr data-log-kind="${logGroup(e.kind)}">
          <td><span class="k-mono k-muted" title="${escapeHtml(date)}">${escapeHtml(time)}</span></td>
          <td><span class="k-badge${flare}">${escapeHtml(e.kind)}</span></td>
          <td><span class="k-mono k-muted">${escapeHtml(logTarget(e, ctx))}</span></td>
          <td class="k-cell-wide"><span class="k-cell-msg">${escapeHtml(e.message)}</span>${logFactsHTML(e)}</td>
        </tr>`;
    })
    .join("");
}

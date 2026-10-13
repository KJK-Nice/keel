// Vault list markup: hosts and group folders, snippets, notebooks,
// identities, port forwards and logs. Pure functions from data to HTML
// strings; term.js puts them in the page and wires the data-* hooks.
// Every value from the vault goes through escapeHtml.

import { escapeHtml } from "./html.js";
import { ancestry, hostSubtitle } from "./groups.js";

const card = (inner, attrs = "") =>
  `<div class="ring-foreground/10 bg-card text-card-foreground host-tile overflow-hidden rounded-xl py-0 text-sm ring-1 hover:bg-muted/50" data-slot="card" data-size="sm"${attrs}>
          <div data-slot="card-content" class="flex flex-row items-center gap-3 px-3 py-3">${inner}
          </div>
        </div>`;
const iconBtn = (attr, id, title, label) =>
  `<button type="button" class="hover:bg-muted hover:text-foreground inline-flex size-6 items-center justify-center rounded-md text-xs" ${attr}="${escapeHtml(id)}" title="${escapeHtml(title)}">${label}</button>`;
const section = (title, body, cls = "tile-grid") =>
  `<section class="flex flex-col gap-2"><h2 class="text-muted-foreground text-xs font-medium tracking-wider uppercase">${escapeHtml(title)}</h2><div class="${cls}">${body}</div></section>`;

export function glyphSVG(kind) {
  if (kind === "folder") {
    return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" class="size-4"><path d="M8 7H16.75C18.8567 7 19.91 7 20.6667 7.50559C20.9943 7.72447 21.2755 8.00572 21.4944 8.33329C22 9.08996 22 10.1433 22 12.25C22 15.7612 22 17.5167 21.1573 18.7779C20.7926 19.3238 20.3238 19.7926 19.7779 20.1573C18.5167 21 16.7612 21 13.25 21H12C7.28595 21 4.92893 21 3.46447 19.5355C2 18.0711 2 15.714 2 11V7.94427C2 6.1278 2 5.21956 2.38032 4.53806C2.65142 4.05227 3.05227 3.65142 3.53806 3.38032C4.21956 3 5.1278 3 6.94427 3C8.10802 3 8.6899 3 9.19926 3.19101C10.3622 3.62712 10.8418 4.68358 11.3666 5.73313L12 7"/></svg>`;
  }
  if (kind === "chevron") {
    return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" class="size-4"><path d="M9 6L15 12L9 18"/></svg>`;
  }
  if (kind === "local") {
    return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" class="size-4"><path d="M7.5 7.5L8.72654 8.55719C9.24218 9.00163 9.5 9.22386 9.5 9.5C9.5 9.77614 9.24218 9.99836 8.72654 10.4428L7.5 11.5"/><path d="M11.5 12.5H15.5"/><path d="M12 21C15.7497 21 17.6246 21 18.9389 20.0451C19.3634 19.7367 19.7367 19.3634 20.0451 18.9389C21 17.6246 21 15.7497 21 12C21 8.25027 21 6.3754 20.0451 5.06107C19.7367 4.6366 19.3634 4.26331 18.9389 3.95491C17.6246 3 15.7497 3 12 3C8.25027 3 6.3754 3 5.06107 3.95491C4.6366 4.26331 4.26331 4.6366 3.95491 5.06107C3 6.3754 3 8.25027 3 12C3 15.7497 3 17.6246 3.95491 18.9389C4.26331 19.3634 4.6366 19.7367 5.06107 20.0451C6.3754 21 8.25027 21 12 21Z"/></svg>`;
  }
  if (kind === "password") {
    return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" class="size-4"><path d="M16 9H19"/><path d="M16 13H19"/><path d="M4.99806 16.6018C5.28942 15.7247 5.83071 14.9651 6.5472 14.4277C7.26369 13.8904 8.12005 13.6018 8.99806 13.6018C9.87606 13.6018 10.7324 13.8904 11.4489 14.4277C12.1654 14.9651 12.7067 15.7247 12.9981 16.6018"/><path d="M8.94922 11.3984C10.0538 11.3984 10.9492 10.503 10.9492 9.39844C10.9492 8.29387 10.0538 7.39844 8.94922 7.39844C7.84465 7.39844 6.94922 8.29387 6.94922 9.39844C6.94922 10.503 7.84465 11.3984 8.94922 11.3984Z"/><path d="M15 4H9C5.70017 4 4.05025 4 3.02513 5.02513C2 6.05025 2 7.70017 2 11V13C2 16.2998 2 17.9497 3.02513 18.9749C4.05025 20 5.70017 20 9 20H15C18.2998 20 19.9497 20 20.9749 18.9749C22 17.9497 22 16.2998 22 13V11C22 7.70017 22 6.05025 20.9749 5.02513C19.9497 4 18.2998 4 15 4Z"/></svg>`;
  }
  if (kind === "key") {
    return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" class="size-4"><path d="M15.5 14.5C18.8137 14.5 21.5 11.8137 21.5 8.5C21.5 5.18629 18.8137 2.5 15.5 2.5C12.1863 2.5 9.5 5.18629 9.5 8.5C9.5 9.38041 9.68962 10.2165 10.0303 10.9697L2.5 18.5V21.5H5.5V19.5H7.5V17.5H9.5L13.0303 13.9697C13.7835 14.3104 14.6196 14.5 15.5 14.5Z"/><path d="M17.5 6.5L16.5 7.5"/></svg>`;
  }
  if (kind === "snippet") {
    return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" class="size-4"><path d="M15 4L9 20"/><path d="M5.99997 16C5.99997 16 2.00001 13.054 2 12C1.99999 10.9459 6 8 6 8"/><path d="M18 8C18 8 22 10.946 22 12C22 13.0541 18 16 18 16"/></svg>`;
  }
  if (kind === "notebook") {
    return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" class="size-4"><path d="M8 7h8M8 11h5"/><path d="M6 3h9.5A3.5 3.5 0 0 1 19 6.5V21H7a3 3 0 0 1-3-3V6a3 3 0 0 1 3-3z"/><path d="M19 21H7"/></svg>`;
  }
  if (kind === "log") {
    return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" class="size-4"><path d="M2 7C2 7.94281 2 8.41421 2.29289 8.70711C2.58579 9 3.05719 9 4 9H6V5C6 3.89543 5.10457 3 4 3C2.89543 3 2 3.89543 2 5V7Z"/><path d="M10 8H15M10 11H13"/><path d="M19 16V7C19 5.11438 19 4.17157 18.4142 3.58579C17.8284 3 16.8856 3 15 3H4"/></svg>`;
  }
  return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" class="size-4"><path d="M19 4H5C4.06812 4 3.60218 4 3.23463 4.15224C2.74458 4.35523 2.35523 4.74458 2.15224 5.23463C2 5.60218 2 6.06812 2 7C2 7.93188 2 8.39782 2.15224 8.76537C2.35523 9.25542 2.74458 9.64477 3.23463 9.84776C3.60218 10 4.06812 10 5 10H19C19.9319 10 20.3978 10 20.7654 9.84776C21.2554 9.64477 21.6448 9.25542 21.8478 8.76537C22 8.39782 22 7.93188 22 7C22 6.06812 22 5.60218 21.8478 5.23463C21.6448 4.74458 21.2554 4.35523 20.7654 4.15224C20.3978 4 19.9319 4 19 4Z"/><path d="M19 14H5C4.06812 14 3.60218 14 3.23463 14.1522C2.74458 14.3552 2.35523 14.7446 2.15224 15.2346C2 15.6022 2 16.0681 2 17C2 17.9319 2 18.3978 2.15224 18.7654C2.35523 19.2554 2.74458 19.6448 3.23463 19.8478C3.60218 20 4.06812 20 5 20H19C19.9319 20 20.3978 20 20.7654 19.8478C21.2554 19.6448 21.6448 19.2554 21.8478 18.7654C22 18.3978 22 17.9319 22 17C22 16.0681 22 15.6022 21.8478 15.2346C21.6448 14.7446 21.2554 14.3552 20.7654 14.1522C20.3978 14 19.9319 14 19 14Z"/></svg>`;
}

export function tileGlyph(color, kind) {
  if (kind === "folder") {
    return `<span class="bg-primary/15 text-primary flex size-9 shrink-0 items-center justify-center rounded-lg">${glyphSVG("folder")}</span>`;
  }
  const c = color || "#5ee0b5";
  const inner = kind === "L" || kind === "R" || kind === "D"
    ? `<span class="text-xs font-semibold">${kind}</span>`
    : glyphSVG(kind);
  return `<span class="flex size-9 shrink-0 items-center justify-center rounded-lg" style="--host-color:${c};background:color-mix(in srgb, var(--host-color) 22%, transparent);color:var(--host-color)">${inner}</span>`;
}

export function emptyHTML(title, desc) {
  return `<div data-slot="empty" class="gap-4 rounded-xl border-dashed p-6 flex w-full flex-col items-center justify-center text-center"><div data-slot="empty-title" class="text-sm font-medium tracking-tight">${escapeHtml(title)}</div><div data-slot="empty-description" class="text-muted-foreground text-sm">${escapeHtml(desc)}</div></div>`;
}

export function tileSection(title, cards) {
  return `<section class="flex flex-col gap-2"><h2 class="text-muted-foreground text-xs font-medium tracking-wider uppercase">${escapeHtml(title)}</h2><div class="tile-grid">${cards}</div></section>`;
}

export function hostCardHTML(h) {
  const kind = h.authType === "local" ? "local" : "ssh";
  const groupBadge = h.group && h.group !== "Hosts" ? `<span data-slot="badge" class="bg-secondary text-secondary-foreground h-5 rounded-4xl px-2 text-xs">${escapeHtml(h.group)}</span>` : "";
  return `
      <div class="ring-foreground/10 bg-card text-card-foreground host-tile overflow-hidden rounded-xl py-0 text-sm ring-1 hover:bg-muted/50" data-slot="card" data-size="sm" data-id="${escapeHtml(h.id)}" draggable="true" data-drag-host="${escapeHtml(h.id)}">
        <div data-slot="card-content" class="flex flex-row items-center gap-3 px-3 py-3">
          ${tileGlyph(h.color, kind)}
          <div class="min-w-0 flex-1 cursor-pointer" role="button" tabindex="0" data-connect="${escapeHtml(h.id)}">
            <div data-slot="card-title" class="truncate text-sm font-medium">${escapeHtml(h.name)}</div>
            <p data-slot="card-description" class="text-muted-foreground flex flex-wrap items-center gap-1 text-sm">
              <span data-slot="badge" class="border-border h-5 rounded-4xl border px-2 text-xs">${kind}</span>
              ${groupBadge}
              ${hostTagBadges(h)}
              <span class="truncate">${escapeHtml(hostSubtitle(h))}</span>
            </p>
          </div>
          <div class="flex items-center gap-1">
            <button type="button" class="hover:bg-muted hover:text-foreground inline-flex size-6 items-center justify-center rounded-md text-xs" data-edit="${escapeHtml(h.id)}" title="Edit">Edit</button>
            ${h.authType === "local" ? "" : `<button type="button" class="hover:bg-muted hover:text-foreground inline-flex size-6 items-center justify-center rounded-md text-xs" data-duplicate="${escapeHtml(h.id)}" title="Duplicate">Duplicate</button>`}
            ${h.authType === "local" ? "" : `<button type="button" class="hover:bg-muted hover:text-foreground inline-flex size-6 items-center justify-center rounded-md text-xs" data-delete="${escapeHtml(h.id)}" title="Remove">✕</button>`}
          </div>
        </div>
      </div>`;
}

export function groupFolderHTML(n) {
  const g = n.group;
  const kids = n.kids.map(groupFolderHTML).join("");
  return `
    <div class="group-tree-node" data-group-id="${escapeHtml(g.id)}" data-group-drop="${escapeHtml(g.id)}" data-group-parent="${escapeHtml(g.parentId || "")}" data-group-depth="${n.depth}" data-group-name="${escapeHtml(g.name)}">
      <div class="ring-foreground/10 bg-card text-card-foreground group-tree-heading host-tile overflow-hidden rounded-xl py-0 text-sm ring-1 hover:bg-muted/50" data-slot="card" data-size="sm" draggable="true" data-drag-group="${escapeHtml(g.id)}">
        <div data-slot="card-content" class="flex flex-row items-center gap-2 px-3 py-3">
          <button type="button" class="group-tree-toggle tiny shrink-0 hover:bg-muted inline-flex size-6 items-center justify-center rounded-md" data-group-toggle="${escapeHtml(g.id)}" aria-label="Expand folder" aria-expanded="true">
            <span class="group-tree-chevron">${glyphSVG("chevron")}</span>
          </button>
          <div class="min-w-0 flex-1 cursor-pointer" role="button" tabindex="0" data-group-filter="${escapeHtml(g.name)}" data-group-open="${escapeHtml(g.id)}">
            <div class="flex flex-row items-center gap-3">
              ${tileGlyph("", "folder")}
              <div class="min-w-0 flex-1">
                <div data-slot="card-title" class="truncate text-sm font-medium"><span data-group-title="${escapeHtml(g.id)}">${escapeHtml(g.name)}</span></div>
                <p data-slot="card-description" class="text-muted-foreground text-sm">${n.count === 1 ? "1 Host" : `${n.count} Hosts`}</p>
              </div>
            </div>
          </div>
          <div data-group-menu="${escapeHtml(g.id)}">
            <button type="button" class="hover:bg-muted hover:text-foreground inline-flex size-6 items-center justify-center rounded-md text-xs" data-group-edit="${escapeHtml(g.id)}" title="Edit">Edit</button>
            <button type="button" class="hover:bg-muted hover:text-foreground inline-flex size-6 items-center justify-center rounded-md text-xs" data-group-new="${escapeHtml(g.id)}" title="New subgroup">+</button>
            <button type="button" class="hover:bg-muted hover:text-foreground inline-flex size-6 items-center justify-center rounded-md text-xs" data-group-rename="${escapeHtml(g.id)}" title="Rename">Rename</button>
            <button type="button" class="hover:bg-muted hover:text-foreground inline-flex size-6 items-center justify-center rounded-md text-xs" data-group-move="${escapeHtml(g.id)}" title="Move">Move</button>
            <button type="button" class="hover:bg-muted hover:text-foreground inline-flex size-6 items-center justify-center rounded-md text-xs" data-group-delete="${escapeHtml(g.id)}" title="Remove">✕</button>
          </div>
        </div>
      </div>
      <div class="group-tree-children">${kids}</div>
    </div>`;
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

export function logColor(kind) {
  const k = String(kind || "").toLowerCase();
  if (k === "error" || k === "fail" || k === "denied") return "#ff6b7a";
  if (k === "session") return "#5ee0b5";
  if (k === "tunnel") return "#6ea8ff";
  return "#8a8a8a";
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

export function hostTagBadges(h) {
  if (!Array.isArray(h.tags) || !h.tags.length) return "";
  return h.tags
    .map(
      (tag) =>
        `<span data-slot="badge" class="border-border h-5 cursor-pointer rounded-4xl border px-2 text-xs" data-tag-filter="${escapeHtml(tag)}" title="Filter by ${escapeHtml(tag)}">${escapeHtml(tag)}</span>`
    )
    .join("");
}

export function statusColor(status) {
  if (status === "ready") return "#5ee0b5";
  if (status === "connecting") return "#f5c15d";
  if (status === "stopped") return "#8a8a8a";
  return "#ff6b7a";
}

// groupFocusPathHTML is the "All / Infra / db" breadcrumb above a
// focused group; empty with no focus.
export function groupFocusPathHTML(groups, focus) {
  if (!focus) return "";
  const parts = [`<button type="button" class="hover:text-foreground" data-group-open="">All</button>`];
  for (const g of ancestry(groups, focus).reverse()) {
    parts.push(`<span aria-hidden="true">/</span><button type="button" class="hover:text-foreground" data-group-open="${escapeHtml(g.id)}">${escapeHtml(g.name)}</button>`);
  }
  return `<nav class="group-focus-path text-muted-foreground flex flex-wrap items-center gap-1 text-xs" aria-label="Group">${parts.join("")}</nav>`;
}

// hostsHTML is the Hosts section: breadcrumb, folder tree (from
// nestGroupTree) and host cards.
export function hostsHTML({ tree, hosts, groups, focus }) {
  if (!tree.length && !hosts.length) return emptyHTML("No hosts match", "Add a server or clear the search.");
  const html = [groupFocusPathHTML(groups, focus)];
  if (tree.length) html.push(section("Groups", tree.map(groupFolderHTML).join(""), "group-tree"));
  if (hosts.length) html.push(section("Hosts", hosts.map(hostCardHTML).join("")));
  return html.join("");
}

export function snippetsHTML(snippets) {
  if (!snippets.length) {
    return emptyHTML("No snippets yet", "Save commands you type all the time. Click one to send it into the focused pane.");
  }
  const cards = snippets.map((s) =>
    card(
      `
            ${tileGlyph("#6ea8ff", "snippet")}
            <div class="min-w-0 flex-1 cursor-pointer" role="button" tabindex="0" data-run="${escapeHtml(s.id)}">
              <div data-slot="card-title" class="truncate text-sm font-medium">${escapeHtml(s.name)}</div>
              <p data-slot="card-description" class="text-muted-foreground snippet-cmd truncate text-sm">${escapeHtml(String(s.command || "").trim())}</p>
            </div>
            <div class="flex items-center gap-1">
              ${iconBtn("data-edit-snippet", s.id, "Edit", "Edit")}
              ${iconBtn("data-delete-snippet", s.id, "Remove", "✕")}
            </div>`,
      ` data-id="${escapeHtml(s.id)}"`,
    ),
  );
  return tileSection("Snippets", cards.join(""));
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

export function notebooksHTML(notebooks) {
  if (!notebooks?.length) {
    return emptyHTML(
      "No notebooks yet",
      "Write markdown runbooks here. Open a notebook without a session. Fenced sh/bash blocks can fill CLI input — nothing is typed until you confirm.",
    );
  }
  const cards = sortedNotebooks(notebooks).map((nb) => {
    const preview = nb.pinned ? `Pinned · ${notebookPreview(nb)}` : notebookPreview(nb);
    const pin = nb.pinned ? "Unpin" : "Pin";
    return card(
      `
            ${tileGlyph("#e8b849", "notebook")}
            <div class="min-w-0 flex-1 cursor-pointer" role="button" tabindex="0" data-notebook-open="${escapeHtml(nb.id)}">
              <div data-slot="card-title" class="truncate text-sm font-medium">${escapeHtml(nb.name || "Notebook")}</div>
              <p data-slot="card-description" class="text-muted-foreground notebook-preview truncate text-sm">${escapeHtml(preview)}</p>
            </div>
            <div class="flex items-center gap-1">
              ${iconBtn("data-notebook-rename", nb.id, "Rename", "Rename")}
              ${iconBtn("data-notebook-pin", nb.id, pin, pin)}
              ${iconBtn("data-delete-notebook", nb.id, "Remove", "✕")}
            </div>`,
      ` data-id="${escapeHtml(nb.id)}"`,
    );
  });
  return tileSection("Notebooks", cards.join(""));
}

export function identitiesHTML(identities) {
  if (!identities.length) return emptyHTML("No identities yet", "Add a password or key and attach it to hosts.");
  const cards = identities.map((id) => {
    const kind = id.kind === "key" ? "key" : "password";
    const label = kind === "key" ? "Private key" : "Auth password";
    const color = kind === "key" ? "#c084fc" : "#6ea8ff";
    return card(
      `
            ${tileGlyph(color, kind)}
            <div class="min-w-0 flex-1 cursor-pointer" role="button" tabindex="0" data-edit-identity="${escapeHtml(id.id)}">
              <div data-slot="card-title" class="truncate text-sm font-medium">${escapeHtml(id.name)}</div>
              <p data-slot="card-description" class="text-muted-foreground flex flex-wrap items-center gap-1 text-sm">
                <span data-slot="badge" class="border-border h-5 rounded-4xl border px-2 text-xs">${escapeHtml(label)}</span>
                ${id.username ? `<span class="truncate">${escapeHtml(id.username)}</span>` : ""}
              </p>
            </div>
            <div class="flex items-center gap-1">
              ${iconBtn("data-edit-identity", id.id, "Edit", "Edit")}
              ${iconBtn("data-delete-identity", id.id, "Remove", "✕")}
            </div>`,
      ` data-id="${escapeHtml(id.id)}"`,
    );
  });
  return tileSection("Identities", cards.join(""));
}

const TUNNEL_KINDS = {
  local: { letter: "L", label: "Local" },
  remote: { letter: "R", label: "Remote" },
  dynamic: { letter: "D", label: "Dynamic" },
};

export function tunnelsHTML(tunnels, hosts) {
  if (!tunnels.length) return emptyHTML("No forwards yet", "Local (−L), remote (−R), or dynamic SOCKS5 (−D).");
  const textBtn = (attr, id, label) =>
    `<button type="button" class="hover:bg-muted hover:text-foreground inline-flex h-6 items-center rounded-md px-2 text-xs" ${attr}="${escapeHtml(id)}">${label}</button>`;
  const cards = tunnels.map((t) => {
    const host = (hosts || []).find((h) => h.id === t.hostId);
    const dest = t.kind === "dynamic" ? "SOCKS5" : `${t.destHost}:${t.destPort}`;
    const listen = t.listenAddr || `${t.bindAddr}:${t.bindPort || "auto"}`;
    const st = t.status || "stopped";
    const kind = TUNNEL_KINDS[t.kind] || TUNNEL_KINDS.local;
    return card(
      `
              ${tileGlyph(statusColor(st), kind.letter)}
              <div class="min-w-0 flex-1">
                <div data-slot="card-title" class="truncate text-sm font-medium">${escapeHtml(t.name)}</div>
                <p data-slot="card-description" class="text-muted-foreground flex flex-wrap items-center gap-1 text-sm">
                  <span data-slot="badge" class="border-border h-5 rounded-4xl border px-2 text-xs">${kind.label}</span>
                  <span data-slot="badge" class="bg-secondary text-secondary-foreground h-5 rounded-4xl px-2 text-xs">${escapeHtml(st)}</span>
                  <span class="truncate">${escapeHtml(host?.name || t.hostId)} · ${escapeHtml(listen)} → ${escapeHtml(dest)}</span>
                  ${t.error ? `<span class="text-destructive truncate">${escapeHtml(t.error)}</span>` : ""}
                </p>
              </div>
              <div class="flex items-center gap-1">
                ${textBtn("data-tunnel-start", t.id, "Start")}
                ${textBtn("data-tunnel-stop", t.id, "Stop")}
                ${iconBtn("data-edit-tunnel", t.id, "Edit", "Edit")}
                ${iconBtn("data-delete-tunnel", t.id, "Remove", "✕")}
              </div>`,
      ` data-id="${escapeHtml(t.id)}"`,
    );
  });
  return tileSection("Port forwarding", cards.join(""));
}

// logsHTML lists log entries whose kind or message contains filter.
export function logsHTML(logs, filter) {
  const q = String(filter || "").trim().toLowerCase();
  const rows = (logs || []).filter((e) => !q || `${e.kind} ${e.message}`.toLowerCase().includes(q));
  if (!rows.length) return emptyHTML("No log entries", "Connection and vault events show up here.");
  const cards = rows.map((e) => {
    const { date, time } = logParts(e.time);
    return card(`
            ${tileGlyph(logColor(e.kind), "log")}
            <div class="log-when">
              <div data-slot="card-title" class="text-sm font-medium">${escapeHtml(date)}</div>
              <p data-slot="card-description" class="text-muted-foreground text-sm">${escapeHtml(time)}</p>
            </div>
            <span data-slot="badge" class="kind bg-secondary text-secondary-foreground h-5 shrink-0 rounded-4xl px-2 text-xs">${escapeHtml(e.kind)}</span>
            <div class="text-muted-foreground min-w-0 flex-1 truncate text-sm">${escapeHtml(e.message)}</div>`);
  });
  return section("Logs", cards.join(""), "flex flex-col gap-2");
}

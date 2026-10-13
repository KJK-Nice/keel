// New tab page: recent host cards, start actions, recently closed rows and
// the offer shown for a pasted ssh command. Pure functions from data to
// HTML strings (same k-* classes as the rest of the UI), plus the small
// bookkeeping for the recently closed list. No DOM, no state, no storage:
// term.js owns those. Every value goes through escapeHtml.

import { escapeHtml } from "./html.js";
import { describeDropped } from "./sshparse.js";
import { ICONS } from "./palette.js";

export const RECENT_CARDS = 6;
export const CLOSED_MAX = 10;
export const CLOSED_SHOWN = 3;

// ---- recently closed ---------------------------------------------------

// normalizeClosed reads the stored list defensively: only entries naming a
// host, newest first, at most CLOSED_MAX. An entry is
// { hostId, title, kind: "tab" | "pane", at: ms since epoch }. It holds no
// secret: the host id is looked up again when it is reopened.
export function normalizeClosed(raw) {
  if (!Array.isArray(raw)) return [];
  const out = [];
  for (const e of raw) {
    if (!e || typeof e.hostId !== "string" || !e.hostId) continue;
    out.push({
      hostId: e.hostId,
      title: typeof e.title === "string" ? e.title.slice(0, 120) : "",
      kind: e.kind === "pane" ? "pane" : "tab",
      at: Number.isFinite(e.at) ? e.at : 0,
    });
    if (out.length >= CLOSED_MAX) break;
  }
  return out;
}

// pushClosed puts a closed tab or pane first. An older entry for the same
// host and kind is replaced, so closing and reopening the same host does not
// fill the list.
export function pushClosed(list, entry) {
  const [fresh] = normalizeClosed([entry]);
  if (!fresh) return normalizeClosed(list);
  const rest = normalizeClosed(list).filter((e) => !(e.hostId === fresh.hostId && e.kind === fresh.kind));
  return normalizeClosed([fresh, ...rest]);
}

// liveClosed drops entries whose host no longer exists.
export function liveClosed(list, hostIds) {
  const have = new Set(hostIds);
  return normalizeClosed(list).filter((e) => have.has(e.hostId));
}

// ageLabel: "just now", "12 min ago", "3 h ago", "yesterday", "5 days ago".
export function ageLabel(at, now) {
  const s = Math.max(0, Math.floor((now - at) / 1000));
  if (s < 60) return "just now";
  const m = Math.floor(s / 60);
  if (m < 60) return `${m} min ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h} h ago`;
  const d = Math.floor(h / 24);
  return d === 1 ? "yesterday" : `${d} days ago`;
}

// ---- cards ---------------------------------------------------------------

// recentCards picks up to RECENT_CARDS hosts. With no query they are the
// recent hosts (and the local shell, always); with a query, every host that
// `match` accepts, recent ones first.
export function recentCards(hosts, recentIds, query, match, local) {
  const byId = new Map(hosts.map((h) => [h.id, h]));
  const q = String(query || "").trim();
  const ordered = [];
  const seen = new Set();
  const add = (h) => {
    if (h && !seen.has(h.id)) {
      seen.add(h.id);
      ordered.push(h);
    }
  };
  if (!q) {
    for (const id of recentIds) add(byId.get(id));
    if (local && !seen.has(local.id)) ordered.unshift(local);
    return ordered.slice(0, RECENT_CARDS);
  }
  for (const id of recentIds) {
    const h = byId.get(id);
    if (h && match(h, q)) add(h);
  }
  for (const h of hosts) if (match(h, q)) add(h);
  return ordered.slice(0, RECENT_CARDS);
}

// cardHTML renders one host card. opts.sub is the second line, opts.live a
// count of open sessions on the host, opts.alt the Alt key label ("⌥").
export function cardHTML(h, index, opts = {}) {
  const id = escapeHtml(h.id);
  const color = h.color ? ` style="--host:${escapeHtml(h.color)}"` : "";
  const icon = ICONS[h.authType === "local" ? "session" : "host"];
  const live = opts.live > 0 ? `<span class="k-dot k-dot--live" aria-hidden="true"></span>${opts.live} live · ` : "";
  const sub = opts.connecting ? "Connecting…" : `${live}${escapeHtml(opts.sub || "")}`;
  const hint = index < RECENT_CARDS ? `<span class="k-keyhint" aria-hidden="true">${escapeHtml(opts.alt || "⌥")}${index + 1}</span>` : "";
  return `<button type="button" class="k-start-card" data-start-connect="${id}"${opts.picked ? " data-highlighted" : ""} title="${escapeHtml(opts.sub || "")}">
      <span class="k-tile"${color}><span class="k-i">${icon}</span></span>
      <span class="k-start-card-name">${escapeHtml(h.name)}</span>
      <span class="k-start-card-sub">${sub}</span>${hint}
    </button>`;
}

export function cardsHTML(cards, optsFor) {
  return cards.map((h, i) => cardHTML(h, i, optsFor(h))).join("");
}

// actionsHTML: the Start actions. `mod` is "⌘" or "Ctrl+".
export function actionsHTML(mod) {
  const row = (attr, icon, label, keys) =>
    `<button type="button" class="k-start-action" ${attr}><span class="k-i">${ICONS[icon]}</span>${escapeHtml(label)}<span class="k-kbd">${escapeHtml(keys)}</span></button>`;
  return [
    row("data-start-quick", "action", "Quick connect", `${mod}⇧O`),
    row("data-start-join", "link", "Join session", `${mod}J`),
    row("data-start-newhost", "add", "New host", `${mod}N`),
  ].join("");
}

// closedHTML renders the recently closed rows. `list` is already filtered
// with liveClosed; nameOf(hostId) gives the host's current name.
export function closedHTML(list, nameOf, now, mod) {
  return list
    .slice(0, CLOSED_SHOWN)
    .map((e, i) => {
      const name = nameOf(e.hostId) || e.title || "Session";
      const label = e.kind === "pane" ? `${name} · split pane` : name;
      const keys = i === 0 ? `<span class="k-kbd">${escapeHtml(mod)}⇧T</span>` : "";
      return `<button type="button" class="k-start-closed" data-start-reopen="${i}"><span class="k-i">${ICONS.reopen}</span><span class="k-start-closed-name">${escapeHtml(label)}</span><span class="k-start-closed-when">${escapeHtml(ageLabel(e.at, now))}</span>${keys}</button>`;
    })
    .join("");
}

// ---- pasted ssh command ------------------------------------------------

// hostPrefill: the New host fields a parsed command fills. The name is the
// host; the user edits it. Auth stays the form's default: nothing about keys
// or passwords is carried over.
export function hostPrefill(p) {
  return { name: p.host, address: p.host, port: p.port, username: p.user };
}

// targetText: "deploy@10.0.4.21:2222" (IPv6 gets brackets).
export function targetText(p) {
  const host = p.host.includes(":") ? `[${p.host}]` : p.host;
  const port = p.port && p.port !== 22 ? `:${p.port}` : "";
  return `${p.user ? p.user + "@" : ""}${host}${port}`;
}

// notesHTML lists what the user must know about a parsed command: options
// that were left out (and why) and the key-file note. Empty string when
// there is nothing to say.
export function notesHTML(p) {
  const out = [];
  const dropped = describeDropped(p.dropped);
  if (dropped.length) {
    out.push(
      `<p class="k-start-note-head">Left out (${dropped.length}), not applied</p><ul class="k-start-note-list">${p.dropped
        .map((d) => `<li><code class="k-mono">${escapeHtml(d.option)}</code> ${escapeHtml(d.reason)}</li>`)
        .join("")}</ul>`,
    );
  }
  for (const n of p.notes || []) out.push(`<p class="k-start-note">${escapeHtml(n)}</p>`);
  return out.join("");
}

// offerHTML is the panel under the input for a parsed command: what was
// read, what was left out, and the two ways to go on. Nothing has run.
export function offerHTML(p) {
  const once = p.user
    ? `<button type="button" class="k-btn" data-start-offer-once>Connect once</button>`
    : `<span class="k-help">Add a user (user@host) to connect once.</span>`;
  return `<section class="k-start-offer" aria-label="Pasted command" data-start-offer>
      <p class="k-start-offer-line">Read as <span class="k-mono k-start-offer-target">${escapeHtml(targetText(p))}</span>. Nothing has run.</p>
      ${notesHTML(p)}
      <div class="k-start-offer-actions">
        <button type="button" class="k-btn k-btn--primary" data-start-offer-save>Save as host<span class="k-kbd">↵</span></button>
        ${once}
      </div>
    </section>`;
}

export function offerErrorHTML(error) {
  return `<p class="k-start-offer-error" role="status" data-start-offer-error>${escapeHtml(error)}</p>`;
}

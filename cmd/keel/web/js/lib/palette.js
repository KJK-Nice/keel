// Command palette filtering, ordering and markup. Pure functions: no DOM,
// no state.
//
// An item is { id, group, title, hint?, keys?, icon?, state?, badge?, run }.
// Groups print in GROUP_ORDER (docs/ui-redesign.md §5.2): Open now, Hosts,
// Actions, Snippets. A group heading is printed each time the group changes.
//   icon   a key of ICONS
//   state  "live" | "connecting" | "dead" | "idle": a state dot (open tabs)
//   tag    a neutral label (a host tag)
//   badge  a brass label next to the shortcut ("Approve first" on snippets)
// `run(opts)` gets { split } = true for ⌥↵ (open in split); rows that have
// no split form ignore it.

import { escapeHtml } from "./html.js";
import { glyphSVG } from "./vaultview.js";

export const GROUP_ORDER = ["Open now", "Hosts", "Actions", "Snippets"];

const svg = (paths) =>
  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths}</svg>`;

const ICON_PATHS = {
  action:
    '<path d="M14 4H10C6.22876 4 4.34315 4 3.17157 5.17157C2 6.34315 2 8.22876 2 12C2 15.7712 2 17.6569 3.17157 18.8284C4.34315 20 6.22876 20 10 20H14C17.7712 20 19.6569 20 20.8284 18.8284C22 17.6569 22 15.7712 22 12C22 8.22876 22 6.34315 20.8284 5.17157C19.6569 4 17.7712 4 14 4Z"/><path d="M7 9L8.83981 10.5858C9.61327 11.2525 10 11.5858 10 12C10 12.4142 9.61327 12.7475 8.83981 13.4142L7 15"/><path d="M13 16H17"/>',
  add: '<path d="M12 5V19"/><path d="M19 12H5"/>',
  reopen: '<path d="M20 2V5.1C20 5.4 19.6 5.6 19.5 5.3C17.6 3.3 15 2 12 2C6.5 2 2 6.5 2 12C2 17.5 6.5 22 12 22C17.5 22 22 17.5 22 12"/>',
  link: '<path d="M15 17H17C19.7614 17 22 14.7614 22 12C22 9.23858 19.7614 7 17 7H15M9 17H7C4.23858 17 2 14.7614 2 12C2 9.23858 4.23858 7 7 7H9"/><path d="M8.5 12H15.5"/>',
  split:
    '<path d="M11.9922 21L11.9922 20L11.9922 4L11.9922 3"/><path d="M7.99219 19.9995C6.69208 19.8699 5.81694 19.5772 5.16376 18.9157C3.99219 17.7291 3.99219 15.8193 3.99219 11.9997C3.99219 8.18016 3.99219 6.27038 5.16376 5.08379C5.81694 4.42224 6.69207 4.12952 7.99219 4"/><path d="M15.9922 19.9998C17.2923 19.8703 18.1674 19.5776 18.8206 18.916C19.9922 17.7294 19.9922 15.8196 19.9922 12.0001C19.9922 8.1805 19.9922 6.27072 18.8206 5.08413C18.192 4.44741 17.3577 4.15236 16.1368 4.01562"/>',
  sidebar:
    '<path d="M13 3H11C7.22876 3 5.34315 3 4.17157 4.17157C3 5.34315 3 7.22876 3 11V13C3 16.7712 3 18.6569 4.17157 19.8284C5.34315 21 7.22876 21 11 21H13C16.7712 21 18.6569 21 19.8284 19.8284C21 18.6569 21 16.7712 21 13V11C21 7.22876 21 5.34315 19.8284 4.17157C18.6569 3 16.7712 3 13 3Z"/><path d="M9 3V21"/>',
};

export const ICONS = {
  ...Object.fromEntries(Object.entries(ICON_PATHS).map(([k, v]) => [k, svg(v)])),
  session: glyphSVG("local"),
  host: glyphSVG("server"),
  snippet: glyphSVG("snippet"),
  notebook: glyphSVG("notebook"),
  key: glyphSVG("key"),
};

const words = (query) => String(query || "").trim().toLowerCase().split(/\s+/).filter(Boolean);

// matches reports whether every word of query appears in the item's title,
// hint, group or keys (case-insensitive).
export function paletteMatches(item, query) {
  const ws = words(query);
  if (!ws.length) return true;
  const hay = `${item.title} ${item.hint || ""} ${item.group} ${item.keys || ""}`.toLowerCase();
  return ws.every((w) => hay.includes(w));
}

// orderGroups sorts items by GROUP_ORDER, keeping the given order inside a
// group. Groups not in GROUP_ORDER follow, in order of first appearance.
export function orderGroups(items) {
  const rank = new Map(GROUP_ORDER.map((g, i) => [g, i]));
  let next = GROUP_ORDER.length;
  for (const item of items) {
    if (!rank.has(item.group)) rank.set(item.group, next++);
  }
  return items.map((item, i) => [item, i]).sort((a, b) => rank.get(a[0].group) - rank.get(b[0].group) || a[1] - b[1]).map(([item]) => item);
}

export function filterPalette(items, query) {
  return orderGroups(items.filter((item) => paletteMatches(item, query)));
}

// moveCursor steps the selection by delta, wrapping at both ends.
export function moveCursor(cursor, delta, count) {
  if (count <= 0) return 0;
  return (((cursor + delta) % count) + count) % count;
}

// clampCursor keeps a cursor inside a list that may have shrunk.
export function clampCursor(cursor, count) {
  return Math.max(0, Math.min(cursor, count - 1));
}

export const optionId = (i) => `cmd-palette-opt-${i}`;

// highlightHTML escapes text and wraps the parts that match a query word in
// <mark>.
export function highlightHTML(text, query) {
  const s = String(text ?? "");
  const lower = s.toLowerCase();
  const ranges = [];
  for (const w of words(query)) {
    let from = 0;
    for (;;) {
      const at = lower.indexOf(w, from);
      if (at < 0) break;
      ranges.push([at, at + w.length]);
      from = at + w.length;
    }
  }
  if (!ranges.length) return escapeHtml(s);
  ranges.sort((a, b) => a[0] - b[0]);
  const merged = [];
  for (const r of ranges) {
    const last = merged.at(-1);
    if (last && r[0] <= last[1]) last[1] = Math.max(last[1], r[1]);
    else merged.push([...r]);
  }
  let out = "";
  let at = 0;
  for (const [a, b] of merged) {
    out += escapeHtml(s.slice(at, a)) + `<mark>${escapeHtml(s.slice(a, b))}</mark>`;
    at = b;
  }
  return out + escapeHtml(s.slice(at));
}

// keyCapsHTML renders a shortcut as one key cap per key: "⌘⇧O" on macOS,
// "Ctrl+Shift+O" elsewhere.
export function keyCapsHTML(keys) {
  const parts = String(keys).includes("+") && String(keys).length > 1 ? String(keys).split("+").filter(Boolean) : Array.from(String(keys));
  return `<span class="k-keys">${parts.map((p) => `<span class="k-kbd">${escapeHtml(p)}</span>`).join("")}</span>`;
}

// paletteHTML renders the visible items with item `cursor` selected. The
// items must already be ordered (filterPalette does that).
export function paletteHTML(items, cursor, query = "") {
  if (!items.length) return `<p class="cmd-palette-empty">No matching commands</p>`;
  const parts = [];
  let lastGroup = "";
  items.forEach((item, i) => {
    if (item.group !== lastGroup) {
      lastGroup = item.group;
      parts.push(`<div class="k-menu-label" role="presentation">${escapeHtml(item.group)}</div>`);
    }
    const selected = i === cursor;
    const icon = `<span class="k-i">${ICONS[item.icon] || ICONS.action}</span>`;
    const hint = item.hint ? `<span class="k-palette-sub">${escapeHtml(item.hint)}</span>` : "";
    const dot = item.state ? `<span class="k-dot k-dot--${escapeHtml(item.state)}" aria-hidden="true"></span>` : "";
    const tag = item.tag ? `<span class="k-badge">${escapeHtml(item.tag)}</span>` : "";
    const badge = item.badge ? `<span class="k-badge k-badge--brass">${escapeHtml(item.badge)}</span>` : "";
    const keys = item.keys ? keyCapsHTML(item.keys) : "";
    const meta = dot || tag || badge || keys ? `<span class="cmd-palette-meta">${dot}${tag}${badge}${keys}</span>` : "";
    parts.push(
      `<button type="button" class="k-palette-item" role="option" data-palette-index="${i}" id="${optionId(i)}" aria-selected="${selected}"${selected ? " data-highlighted" : ""}>${icon}<span class="cmd-palette-title">${highlightHTML(item.title, query)}</span>${hint}${meta}</button>`,
    );
  });
  return parts.join("");
}

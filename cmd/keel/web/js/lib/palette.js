// Command palette filtering and markup. Pure functions: no DOM, no state.
//
// An item is { id, group, title, hint?, keys?, run }. Items arrive grouped
// (the catalog adds them group by group); a group heading is printed each
// time the group changes.

import { escapeHtml } from "./html.js";

// matches reports whether every word of query appears in the item's title,
// hint, group or keys (case-insensitive).
export function paletteMatches(item, query) {
  const words = String(query || "").trim().toLowerCase().split(/\s+/).filter(Boolean);
  if (!words.length) return true;
  const hay = `${item.title} ${item.hint || ""} ${item.group} ${item.keys || ""}`.toLowerCase();
  return words.every((w) => hay.includes(w));
}

export function filterPalette(items, query) {
  return items.filter((item) => paletteMatches(item, query));
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

// paletteHTML renders the visible items with item `cursor` selected.
export function paletteHTML(items, cursor) {
  if (!items.length) return `<p class="cmd-palette-empty">No matching commands</p>`;
  const parts = [];
  let lastGroup = "";
  items.forEach((item, i) => {
    if (item.group !== lastGroup) {
      lastGroup = item.group;
      parts.push(`<div class="cmd-palette-group">${escapeHtml(item.group)}</div>`);
    }
    const selected = i === cursor;
    const hint = item.hint ? `<span class="cmd-palette-item-hint">${escapeHtml(item.hint)}</span>` : "";
    const keys = item.keys ? `<kbd class="cmd-palette-item-keys">${escapeHtml(item.keys)}</kbd>` : "";
    parts.push(
      `<button type="button" class="cmd-palette-item${selected ? " is-active" : ""}" role="option" data-palette-index="${i}" id="${optionId(i)}" aria-selected="${selected}">
          <span class="cmd-palette-item-text"><span class="cmd-palette-item-title">${escapeHtml(item.title)}</span>${hint}</span>
          ${keys}
        </button>`,
    );
  });
  return parts.join("");
}

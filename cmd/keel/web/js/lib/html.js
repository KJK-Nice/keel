// HTML helpers for term.js. Pure functions: no DOM, no state.

// escapeHtml makes a value safe inside element text and inside quoted
// attribute values (single or double quotes).
export function escapeHtml(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

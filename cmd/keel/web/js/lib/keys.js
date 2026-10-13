// Keyboard helpers for term.js. Pure functions: no DOM, no state.

// tabCycleDelta classifies a Cmd/Ctrl key event as previous tab (-1),
// next tab (1) or neither (0): Cmd+< / Cmd+> on US and ISO layouts, and
// Cmd+Shift+[ / Cmd+Shift+] (the macOS convention).
export function tabCycleDelta(ev) {
  const raw = ev.key || "";
  const key = raw.length === 1 ? raw.toLowerCase() : raw;
  const code = ev.code || "";
  const shift = !!ev.shiftKey;
  const prev =
    key === "<" ||
    key === "{" ||
    (shift && (key === "," || key === "[" || code === "Comma" || code === "BracketLeft"));
  const next =
    key === ">" ||
    key === "}" ||
    (shift && (key === "." || key === "]" || code === "Period" || code === "BracketRight"));
  if (prev === next) return 0;
  return prev ? -1 : 1;
}

// tabCycleTarget returns the tab id delta steps from activeId in list,
// wrapping at both ends. An unknown activeId counts from the first tab.
export function tabCycleTarget(list, activeId, delta) {
  if (!list.length || !delta) return "";
  const i = list.indexOf(activeId);
  const from = i < 0 ? 0 : i;
  const len = list.length;
  return list[(from + (delta % len) + len) % len] || "";
}

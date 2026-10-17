// Write helpers that leave the DOM alone when nothing changed.
//
// Assigning textContent or innerHTML replaces the children even when the
// value is the same, and every replacement is a childList mutation. The
// component scripts (components/*.js) re-scan the document on each one, so
// chrome that re-renders on every presence message (tab strip, pane bars,
// status bar) made opening a tab cost hundreds of mutations.

export function setText(el, text) {
  if (!el) return false;
  const next = String(text ?? "");
  if (el.textContent === next) return false;
  el.textContent = next;
  return true;
}

// setHTML compares against the last markup this helper wrote, not the
// element's serialized innerHTML, which browsers normalize.
export function setHTML(el, html) {
  if (!el) return false;
  if (el._keelHTML === html && el.childNodes.length) return false;
  el.innerHTML = html;
  el._keelHTML = html;
  return true;
}

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

// noTextAssist turns off what the native WKWebView does to a text field it
// did not get from markup: capitalizing the first letter, autocorrecting
// words and underlining them. Fields in markup (templ and the HTML strings
// in term.js and ui/*.js) carry the same three attributes inline; this is
// for fields made with document.createElement.
export function noTextAssist(el) {
  if (!el) return el;
  el.setAttribute("autocapitalize", "off");
  el.setAttribute("autocorrect", "off");
  el.setAttribute("spellcheck", "false");
  return el;
}

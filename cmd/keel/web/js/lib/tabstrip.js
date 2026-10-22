// The tab strip's markup (#tabs). No DOM: term.js decides what each tab
// shows and paints the result with setHTML.
import { escapeHtml } from "./html.js";

// kbdHint is a shortcut hint, shown while the app modifier is held.
export function kbdHint(key) {
  return `<kbd class="kbd-hint" aria-hidden="true">${escapeHtml(key)}</kbd>`;
}

// tabNumberKey is the digit that selects strip position i of n: ⌘1…8 the
// first eight tabs, ⌘9 the last.
export function tabNumberKey(i, n) {
  if (i < 8) return String(i + 1);
  return i === n - 1 ? "9" : "";
}

// tabStripHTML is the strip for tabs in order. A tab is
//   { id, label, lead, status, panes, page, sync }
// where lead is the markup before the label (a status dot or an icon),
// status a pane status class ("is-ready" …) and panes its pane count.
// activeId is the selected tab, mod the modifier's label (⌘ or Ctrl+) and
// closeGlyph the close button's icon.
export function tabStripHTML(tabs, { activeId, mod, closeGlyph }) {
  return tabs
    .map((t, i) => {
      const num = tabNumberKey(i, tabs.length);
      const active = t.id === activeId;
      const cls = ["tab", "k-tab", t.page ? "is-page" : "", active ? "is-active" : "", t.status || "", t.sync ? "is-sync" : ""].join(" ");
      const id = escapeHtml(t.id);
      return `<button type="button" class="${cls}" data-tab="${id}" title="Drag to reorder">
            ${num ? kbdHint(num) : ""}
            ${t.lead}
            <span class="tab-label">${escapeHtml(t.label)}</span>
            ${t.panes > 1 ? `<span class="k-badge k-badge--mono tab-count" title="${t.panes} panes">${t.panes}</span>` : ""}
            <span class="close k-tab-close" data-close="${id}" title="Close  ${mod}W">${closeGlyph}${active ? kbdHint("W") : ""}</span>
          </button>`;
    })
    .join("");
}

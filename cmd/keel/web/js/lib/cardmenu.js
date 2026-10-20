// Agent card menus. A right-click (or ⇧F10 / the Menu key) on a card in an
// agent thread (a proposal, a note, a runbook or notebook proposal, a
// runbook in use) opens a k-menu of the card's own buttons, the ones it
// shows right now, in the same order. Picking an item clicks that button,
// so every action keeps its one path: Type into pane still opens
// Approve before type (§6 rule 1), Delete… still asks, and nothing here
// writes to a PTY or reads a value the card does not show.
import { escapeHtml } from "./html.js";

// The data attribute that names each card action, with the icon it gets
// (keys of the agent thread's ICON set). Buttons not listed get none.
const ACTION_ICON = [
  ["data-prop-type", "cmd"],
  ["data-prop-edit-btn", "edit"],
  ["data-prop-copy", "copy"],
  ["data-prop-again", "redo"],
  ["data-prop-save", "tick"],
  ["data-prop-cancel", "x"],
  ["data-note-save", "tick"],
  ["data-note-edit-btn", "edit"],
  ["data-note-cancel", "x"],
  ["data-note-dismiss", "x"],
  ["data-note-undo", "history"],
  ["data-rbp-save", "tick"],
  ["data-rbp-dismiss", "x"],
  ["data-rbp-undo", "history"],
  ["data-rbp-open", "book"],
  ["data-nbp-notebook", "notebook"],
  ["data-nbp-keep", "tick"],
  ["data-nbp-undo", "history"],
  ["data-nbp-restore", "history"],
  ["data-nbp-delete", "trash"],
  ["data-rb-propose", "ai"],
  ["data-rb-done", "tick"],
  ["data-rb-retry", "redo"],
  ["data-rb-skip", "skip"],
  ["data-rb-choose", "book"],
  ["data-rb-move", "redo"],
  ["data-rb-stop", "x"],
];

function hiddenIn(el, card) {
  for (let n = el; n && n !== card; n = n.parentElement) {
    if (n.hidden) return true;
  }
  return false;
}

// buttonLabel is what the button says: its text without the key hint
// (.k-kbd), else its aria-label.
function buttonLabel(btn) {
  let text = "";
  for (const node of btn.childNodes || []) {
    if (node.nodeType === 3) text += node.textContent;
    else if (node.nodeType === 1 && !node.classList?.contains("k-kbd")) text += node.textContent;
  }
  text = text.replace(/\s+/g, " ").trim();
  return text || (btn.getAttribute("aria-label") || "").trim();
}

// cardActions lists a card's buttons as menu items: { button, label, icon,
// keys, danger }. A disabled or hidden button is left out; so is one with
// no label.
export function cardActions(card) {
  if (!card?.querySelectorAll) return [];
  const out = [];
  for (const btn of card.querySelectorAll("button.k-btn")) {
    if (btn.disabled || btn.getAttribute("aria-disabled") === "true" || hiddenIn(btn, card)) continue;
    const label = buttonLabel(btn);
    if (!label) continue;
    const icon = ACTION_ICON.find(([attr]) => btn.hasAttribute(attr))?.[1] || "";
    const keys = (btn.querySelector?.(".k-kbd")?.textContent || "").trim();
    const danger = !!btn.classList?.contains("k-btn--danger");
    out.push({ button: btn, label, icon, keys, danger });
  }
  return out;
}

// The classes components/dropdownmenu itemClasses gives an item.
const ITEM_CLASS =
  "k-menu-item group/dropdown-menu-item relative outline-hidden select-none data-disabled:pointer-events-none data-disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:shrink-0 w-full text-left";

// cardMenuHTML is the menu's items, the markup of components/dropdownmenu
// Item (k-menu-item, a leading .k-i icon, .k-shortcut), each naming its
// action by index (data-card-act).
export function cardMenuHTML(actions, icons = {}) {
  return actions
    .map((a, i) => {
      const cls = `${ITEM_CLASS}${a.danger ? " k-menu-item--danger" : ""}`;
      const ic = icons[a.icon] ? `<span class="k-i" aria-hidden="true">${icons[a.icon]}</span>` : "";
      const keys = a.keys ? `<span data-slot="dropdown-menu-shortcut" aria-hidden="true" class="k-shortcut">${escapeHtml(a.keys)}</span>` : "";
      return `<button type="button" data-slot="dropdown-menu-item" data-variant="${a.danger ? "destructive" : "default"}" data-tui-dropdownmenu-item role="menuitem" class="${cls}" data-card-act="${i}">${ic}${escapeHtml(a.label)}${keys}</button>`;
    })
    .join("");
}

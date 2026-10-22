// The agent card menu (CardMenu in internal/ui/rowmenus.templ): a
// right-click, ⇧F10 or the Menu key on an agent card offers the card's
// own buttons (lib/cardmenu.js cardActions), and an item clicks the very
// button it was made from. Type on a card is the card's Type, so it still
// goes through the panel's typeProposal and Approve before type; this
// module never reaches a pane.
//
// ⇧ while the menu is open is read from the keys (WebKit does not set
// shiftKey reliably on a keyboard-made click), so ⇧↵ or ⇧-click on an
// item reaches the card's button with shiftKey, as agentthread.js does.
//   icons   the glyphs the items show (agentthread.js ICON)
import { actionLive, cardActions, cardMenuHTML } from "../lib/cardmenu.js";

export function createAgentCardMenu({ doc = document, win = window, icons = {} } = {}) {
  let card = null;
  let buttons = [];
  let shift = false;
  // The text selection as it was when the right button went down: WebKit
  // selects the word under the pointer before contextmenu, so the
  // selection then says nothing about what the user had selected.
  let rightDownSelection = null;

  // prepare fills in the menu for a card and returns its hidden trigger,
  // or null when the card has no buttons, or when the right-click was on
  // text selected in the card (the exact bytes, a note): the system's menu
  // copies that. at is the point of a right-click, or an anchor.
  function prepare(target, at) {
    const trigger = doc.getElementById("card-menu-trigger");
    const content = trigger && doc.getElementById(trigger.getAttribute("aria-controls"));
    const popup = content?.querySelector("[data-card-menu-items]");
    if (!popup) return null;
    if (at && "x" in at && rightDownSelection && target.contains(rightDownSelection)) return null;
    const actions = cardActions(target);
    if (!actions.length) return null;
    card = target;
    buttons = actions.map((a) => a.button);
    shift = false;
    popup.innerHTML = cardMenuHTML(actions, icons);
    return trigger;
  }

  // run clicks the very button item index was made from. A card repaints
  // with innerHTML, so if it repainted while the menu was open (a runbook
  // step marked done, a new status) the button is gone and nothing runs:
  // labels repeat across steps ("Skip", "Mark done").
  function run(index, shiftKey) {
    const btn = buttons[index];
    if (!actionLive(card, btn)) return;
    btn.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true, shiftKey: !!(shiftKey || shift) }));
  }

  function onRightDown(ev) {
    if (ev.button !== 2) return;
    const sel = win.getSelection?.();
    rightDownSelection = sel && !sel.isCollapsed ? sel.anchorNode : null;
  }

  function trackShift(ev) {
    if (ev.key === "Shift") shift = ev.type === "keydown";
  }

  // bind listens for the right button going down and for ⇧, in the
  // capture phase, and forgets ⇧ when the window loses focus.
  function bind() {
    doc.addEventListener("pointerdown", onRightDown, true);
    doc.addEventListener("keydown", trackShift, true);
    doc.addEventListener("keyup", trackShift, true);
    win.addEventListener("blur", () => {
      shift = false;
    });
  }

  return { prepare, run, bind };
}

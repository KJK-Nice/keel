import { test } from "node:test";
import assert from "node:assert/strict";
import { createAgentCardMenu } from "../ui/agentcardmenu.js";

globalThis.MouseEvent ??= class {
  constructor(type, init) {
    this.type = type;
    Object.assign(this, init);
  }
};

// A card with buttons as lib/cardmenu.js reads them, the menu, a doc and a window.
function setup() {
  let selection = null;
  const clicks = [];
  const mk = (attrs, label) => {
    const b = {
      disabled: false,
      hidden: false,
      parentElement: null,
      isConnected: true,
      childNodes: [{ nodeType: 3, textContent: label }],
      classList: { contains: () => false },
      hasAttribute: (n) => n in attrs,
      getAttribute: (n) => (n in attrs ? attrs[n] : null),
      querySelector: () => null,
      dispatchEvent: (ev) => clicks.push([label, ev.type, ev.shiftKey]),
    };
    return b;
  };
  const typeBtn = mk({ "data-prop-type": "" }, "Type into web-1");
  const skip = mk({ "data-rb-skip": "" }, "Skip");
  const inside = { id: "text-in-card" };
  const card = {
    isConnected: true,
    querySelectorAll: () => [typeBtn, skip],
    contains: (n) => n === card || n === typeBtn || n === skip || n === inside,
  };
  typeBtn.parentElement = card;
  skip.parentElement = card;
  const popup = { innerHTML: "" };
  const content = { querySelector: (sel) => (sel === "[data-card-menu-items]" ? popup : null) };
  const trigger = { getAttribute: () => "card-menu" };
  const listeners = {};
  const winListeners = {};
  const doc = {
    getElementById: (id) => ({ "card-menu-trigger": trigger, "card-menu": content })[id] || null,
    addEventListener: (type, fn, capture) => (listeners[type] = { fn, capture: !!capture }),
  };
  const win = {
    getSelection: () => (selection ? { isCollapsed: false, anchorNode: selection } : { isCollapsed: true, anchorNode: null }),
    addEventListener: (type, fn) => (winListeners[type] = fn),
  };
  const menu = createAgentCardMenu({ doc, win, icons: {} });
  return { menu, card, typeBtn, skip, popup, trigger, clicks, listeners, winListeners, inside, select: (n) => (selection = n) };
}

test("prepare lists the card's own buttons and returns the trigger", () => {
  const s = setup();
  assert.equal(s.menu.prepare(s.card, { anchor: {} }), s.trigger);
  assert.match(s.popup.innerHTML, /Type into web-1/);
  assert.match(s.popup.innerHTML, /Skip/);
});

test("a right-click on text selected in the card leaves it to the system menu", () => {
  const s = setup();
  s.menu.bind();
  // Text in the card was selected when the right button went down.
  s.select(s.inside);
  s.listeners.pointerdown.fn({ button: 2 });
  assert.equal(s.menu.prepare(s.card, { x: 1, y: 2 }), null);
  // From the keyboard (an anchor, not a point) the menu still opens.
  assert.equal(s.menu.prepare(s.card, { anchor: {} }), s.trigger);
  // Nothing selected at the next right-click: the menu opens.
  s.select(null);
  s.listeners.pointerdown.fn({ button: 2 });
  assert.equal(s.menu.prepare(s.card, { x: 1, y: 2 }), s.trigger);
  // A left button leaves the remembered selection alone.
  s.select(s.inside);
  s.listeners.pointerdown.fn({ button: 0 });
  assert.equal(s.menu.prepare(s.card, { x: 1, y: 2 }), s.trigger);
});

test("run clicks the very button the item was made from, with ⇧ from the keys", () => {
  const s = setup();
  s.menu.bind();
  s.menu.prepare(s.card, { anchor: {} });
  s.menu.run(1, false);
  s.listeners.keydown.fn({ key: "Shift", type: "keydown" });
  s.menu.run(0, false);
  s.listeners.keyup.fn({ key: "Shift", type: "keyup" });
  s.menu.run(0, true);
  assert.deepEqual(s.clicks, [["Skip", "click", false], ["Type into web-1", "click", true], ["Type into web-1", "click", true]]);
  s.listeners.keydown.fn({ key: "Shift", type: "keydown" });
  s.winListeners.blur();
  s.menu.run(0, false);
  assert.deepEqual(s.clicks.at(-1), ["Type into web-1", "click", false], "blur forgets ⇧");
});

test("a button the card repainted away is not clicked", () => {
  const s = setup();
  s.menu.prepare(s.card, { anchor: {} });
  s.typeBtn.isConnected = false;
  s.card.contains = () => false;
  s.menu.run(0, false);
  assert.deepEqual(s.clicks, []);
});

import { test } from "node:test";
import assert from "node:assert/strict";
import { actionLive, cardActions, cardMenuHTML } from "../lib/cardmenu.js";

// A tiny stand-in for the card's DOM: buttons with attributes, text and
// a key hint child.
function text(t) {
  return { nodeType: 3, textContent: t };
}
function el(cls, t) {
  return { nodeType: 1, textContent: t, classList: { contains: (c) => c === cls } };
}
function button(attrs, children, opts = {}) {
  const b = {
    disabled: !!opts.disabled,
    hidden: !!opts.hidden,
    parentElement: opts.parent || null,
    childNodes: children,
    classList: { contains: (c) => (opts.classes || []).includes(c) },
    hasAttribute: (n) => n in attrs,
    getAttribute: (n) => (n in attrs ? attrs[n] : null),
    querySelector: (sel) => (sel === ".k-kbd" ? children.find((c) => c.nodeType === 1 && c.classList.contains("k-kbd")) || null : null),
  };
  return b;
}
function card(buttons) {
  const c = { querySelectorAll: () => buttons };
  for (const b of buttons) if (!b.parentElement) b.parentElement = c;
  return c;
}

test("cardActions: the card's shown, enabled buttons, in order", () => {
  const typeBtn = button({ "data-prop-type": "" }, [text("Type into web-1"), el("k-kbd", "⇧↵")]);
  const c = card([
    button({ "data-prop-edit-btn": "", "aria-label": "Edit command" }, [el("svg", "")]),
    button({ "data-prop-copy": "", "aria-label": "Copy command" }, [el("svg", "")]),
    typeBtn,
    button({ "data-note-save": "" }, [text("Save")], { disabled: true }),
    button({ "data-note-undo": "", "aria-disabled": "true" }, [text("Undo")]),
    button({ "data-nbp-delete": "" }, [text("Delete…")], { classes: ["k-btn--danger"] }),
    button({ "data-rbp-open": "" }, [text("Open Runbooks")], { hidden: true }),
    button({}, []),
  ]);
  const got = cardActions(c);
  assert.deepEqual(
    got.map((a) => [a.label, a.icon, a.keys, a.danger]),
    [
      ["Edit command", "edit", "", false],
      ["Copy command", "copy", "", false],
      ["Type into web-1", "cmd", "⇧↵", false],
      ["Delete…", "trash", "", true],
    ],
  );
  assert.equal(got[2].button, typeBtn);
});

test("cardActions: a button inside a hidden part of the card is left out", () => {
  const c = card([]);
  const part = { hidden: true, parentElement: c };
  const inner = button({ "data-rb-done": "" }, [text("Mark done")], { parent: part });
  c.querySelectorAll = () => [inner];
  assert.deepEqual(cardActions(c), []);
  assert.deepEqual(cardActions(null), []);
});

test("cardMenuHTML: k-menu items, escaped, with icons and keys", () => {
  const html = cardMenuHTML(
    [
      { label: "Type into <b>", icon: "cmd", keys: "↵", danger: false },
      { label: "Delete…", icon: "trash", keys: "", danger: true },
      { label: "Odd", icon: "nope", keys: "", danger: false },
    ],
    { cmd: "<svg>c</svg>", trash: "<svg>t</svg>" },
  );
  assert.match(html, /class="k-menu-item [^"]*" data-card-act="0"><span class="k-i" aria-hidden="true"><svg>c<\/svg><\/span>Type into &lt;b&gt;<span [^>]*class="k-shortcut">↵<\/span><\/button>/);
  assert.match(html, /data-variant="destructive"[^>]*class="k-menu-item [^"]*k-menu-item--danger" data-card-act="1">/);
  assert.match(html, /data-card-act="2">Odd<\/button>/);
  assert.equal((html.match(/role="menuitem"/g) || []).length, 3);
  assert.equal((html.match(/data-tui-dropdownmenu-item/g) || []).length, 3);
});

test("actionLive: a repainted card runs nothing, even for a same-label button", () => {
  // A runbook-in-use card on step 3 offers Skip; the menu keeps that very
  // button. Step 3 is then marked done (exit 0) and the card repaints with
  // innerHTML: step 4's Skip is a new button with the same label.
  const step3Skip = button({ "data-rb-skip": "", "data-rb-step": "s3" }, [text("Skip")]);
  const c = card([step3Skip]);
  c.isConnected = true;
  c.contains = (n) => !!n?.isConnected && (n === step3Skip || n === step4Skip);
  step3Skip.isConnected = true;
  const [offered] = cardActions(c);
  assert.equal(offered.button, step3Skip);
  assert.equal(actionLive(c, offered.button), true);

  step3Skip.isConnected = false;
  const step4Skip = button({ "data-rb-skip": "", "data-rb-step": "s4" }, [text("Skip")], { parent: c });
  step4Skip.isConnected = true;
  c.querySelectorAll = () => [step4Skip];
  assert.equal(cardActions(c)[0].label, "Skip");
  assert.equal(actionLive(c, offered.button), false);
  assert.equal(actionLive(c, step4Skip), true);
});

test("actionLive: hidden, disabled, detached or foreign buttons run nothing", () => {
  const b = button({ "data-prop-copy": "" }, [text("Copy")]);
  const c = card([b]);
  c.isConnected = true;
  c.contains = (n) => n === b;
  b.isConnected = true;
  assert.equal(actionLive(c, b), true);
  b.disabled = true;
  assert.equal(actionLive(c, b), false);
  b.disabled = false;
  b.hidden = true;
  assert.equal(actionLive(c, b), false);
  b.hidden = false;
  c.isConnected = false;
  assert.equal(actionLive(c, b), false);
  c.isConnected = true;
  const other = button({}, [text("Copy")]);
  other.isConnected = true;
  assert.equal(actionLive(c, other), false);
  assert.equal(actionLive(c, undefined), false);
});

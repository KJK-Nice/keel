import { test } from "node:test";
import assert from "node:assert/strict";
import { createActionToasts } from "../ui/actiontoast.js";

// Just enough DOM for actiontoast.js.
class El {
  constructor(tag) {
    this.tag = tag;
    this.children = [];
    this.parent = null;
    this.attrs = {};
    this.listeners = {};
    this.textContent = "";
    this.className = "";
  }
  get isConnected() {
    let n = this;
    while (n.parent) n = n.parent;
    return n.tag === "body";
  }
  setAttribute(k, v) { this.attrs[k] = v; }
  append(...nodes) { for (const n of nodes) this.appendChild(n); }
  appendChild(n) { n.parent = this; this.children.push(n); return n; }
  remove() {
    if (!this.parent) return;
    this.parent.children = this.parent.children.filter((c) => c !== this);
    this.parent = null;
  }
  addEventListener(type, fn) { this.listeners[type] = fn; }
  get text() { return this.textContent + this.children.map((c) => c.text).join(""); }
  find(pred) {
    if (pred(this)) return this;
    for (const c of this.children) {
      const hit = c.find(pred);
      if (hit) return hit;
    }
    return null;
  }
}

function setup() {
  const body = new El("body");
  const doc = {
    body,
    createElement: (t) => new El(t),
    // Only the stack lookup uses it: match by every class in the selector.
    querySelector: (sel) => {
      const want = sel.split(".").filter(Boolean);
      return body.find((n) => n !== body && want.every((c) => n.className.split(" ").includes(c)));
    },
  };
  return { body, doc };
}

test("one toast per key, actions run once and close it, text is text", () => {
  const { body, doc } = setup();
  const toasts = createActionToasts({ doc, className: "k-toasts--center", label: "Following" });
  const ran = [];
  const evil = "<img src=x onerror=alert(1)> asks for control";
  assert.equal(toasts.show({ key: "a", tone: "haze", title: evil, desc: "d", actions: [{ label: "Hand over", primary: true, run: () => ran.push("hand") }, { label: "Keep driving" }] }), true);
  assert.equal(toasts.show({ key: "a", title: "again" }), false);
  const stack = body.children[0];
  assert.equal(stack.attrs["aria-label"], "Following");
  assert.equal(stack.children.length, 1);
  const toast = stack.children[0];
  assert.equal(toast.className, "k-toast k-toast--haze");
  assert.equal(toast.find((n) => n.className === "k-toast-title").textContent, evil);
  const hand = toast.find((n) => n.tag === "button" && n.textContent === "Hand over");
  assert.equal(hand.className, "k-btn k-btn--sm");
  hand.listeners.click();
  assert.deepEqual(ran, ["hand"]);
  assert.equal(stack.children.length, 0);
  assert.equal(toasts.has("a"), false);
});

test("dismissWhere keeps the one asked for", () => {
  const { doc } = setup();
  const toasts = createActionToasts({ doc, className: "x" });
  toasts.show({ key: "typing\np1", title: "t" });
  toasts.show({ key: "typing\np2", title: "t" });
  toasts.show({ key: "other", title: "t" });
  toasts.dismissWhere("typing\n", "typing\np2");
  assert.deepEqual(["typing\np1", "typing\np2", "other"].map((k) => toasts.has(k)), [false, true, true]);
});

test("a stack with the same class is shared, not drawn over", () => {
  const { body, doc } = setup();
  const existing = new El("div");
  existing.className = "k-toasts join-prompts";
  body.appendChild(existing);
  const toasts = createActionToasts({ doc, className: "join-prompts", label: "Share requests" });
  toasts.show({ key: "r", title: "e2e asks for control" });
  assert.equal(body.children.length, 1);
  assert.equal(existing.children.length, 1);
});

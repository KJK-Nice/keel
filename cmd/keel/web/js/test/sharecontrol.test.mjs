import { test } from "node:test";
import assert from "node:assert/strict";
import { createShareControl } from "../ui/sharecontrol.js";

// Just enough DOM for actiontoast.js, which sharecontrol.js draws with.
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

function setup({ driving = true, owner = false } = {}) {
  const body = new El("body");
  const doc = {
    body,
    createElement: (t) => new El(t),
    querySelector: (sel) => {
      const want = sel.split(".").filter(Boolean);
      return body.find((n) => n !== body && want.every((c) => n.className.split(" ").includes(c)));
    },
  };
  const sent = [];
  const toasts = [];
  const sc = createShareControl({
    doc,
    sendSessionJSON: (pane, msg) => sent.push([pane.id, msg]),
    toast: (m) => toasts.push(m),
    isDriving: () => driving,
    isOwner: () => owner,
    isTermPane: () => true,
  });
  const pane = { id: "p1", runtimeId: "r1", title: "web", you: { id: 1 }, attached: [{ id: 1, name: "me" }, { id: 2, name: "ana" }] };
  return { body, sc, sent, toasts, pane };
}

const buttons = (body) => {
  const out = [];
  body.find((n) => (n.tag === "button" && out.push(n), false));
  return out;
};

test("a request becomes a toast for the driver, and Hand over grants control", () => {
  const { body, sc, sent, toasts, pane } = setup();
  sc.noteControl(pane, { type: "control", fromId: 2, message: "ana asks for control" });
  assert.match(body.text, /asks for control/);
  assert.equal(toasts.length, 0);
  buttons(body).find((b) => b.textContent === "Hand over").listeners.click();
  assert.deepEqual(sent, [["p1", { type: "grant-control", targetId: 2 }]]);
});

test("my own request, or one while I only follow, is not offered to me", () => {
  const self = setup();
  self.sc.noteControl(self.pane, { type: "control", fromId: 1, message: "m" });
  assert.equal(buttons(self.body).length, 0);
  const follower = setup({ driving: false });
  follower.sc.noteControl(follower.pane, { type: "control", fromId: 2, message: "m" });
  assert.equal(buttons(follower.body).length, 0);
});

test("any other control event clears the session's requests and is a plain toast", () => {
  const { body, sc, toasts, pane } = setup();
  sc.noteControl(pane, { type: "control", fromId: 2, message: "m" });
  sc.noteControl(pane, { type: "control", message: "ana is driving" });
  assert.equal(buttons(body).length, 0);
  assert.deepEqual(toasts, ["ana is driving"]);
});

test("forgetSession drops that session's requests", () => {
  const { body, sc, pane } = setup();
  sc.noteControl(pane, { type: "control", fromId: 2, message: "m" });
  sc.forgetSession("r1");
  assert.equal(buttons(body).length, 0);
});

test("a follower's typing shows the notice without stopping the key", () => {
  const { body, sc, sent, pane } = setup({ driving: false, owner: false });
  pane.wrap = new El("div");
  sc.wireFollowerNotice(pane);
  let stopped = false;
  pane.wrap.listeners.keydown({ type: "keydown", key: "a", target: null, preventDefault: () => (stopped = true), stopPropagation: () => (stopped = true) });
  assert.equal(stopped, false);
  assert.match(body.text, /Typing is off while you follow/);
  buttons(body).find((b) => b.textContent === "Ask for control").listeners.click();
  assert.deepEqual(sent, [["p1", { type: "request-control" }]]);
  sc.followFocus("");
  assert.doesNotMatch(body.text, /Typing is off/);
});

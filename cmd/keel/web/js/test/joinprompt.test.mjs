import { test } from "node:test";
import assert from "node:assert/strict";
import { createJoinPrompts } from "../ui/joinprompt.js";

// Just enough DOM for joinprompt.js.
class El {
  constructor(tag) {
    this.tag = tag;
    this.children = [];
    this.parent = null;
    this.attrs = {};
    this.listeners = {};
    this.textContent = "";
    this.className = "";
    this.disabled = false;
  }
  get isConnected() {
    let n = this;
    while (n.parent) n = n.parent;
    return n.tag === "body";
  }
  setAttribute(k, v) { this.attrs[k] = v; }
  append(...nodes) { for (const n of nodes) this.appendChild(typeof n === "string" ? Object.assign(new El("#text"), { textContent: n }) : n); }
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

function setup(answer = async () => {}) {
  const body = new El("body");
  const doc = { body, createElement: (t) => new El(t) };
  const errors = [];
  const prompts = createJoinPrompts({ doc, answer, onError: (e) => errors.push(e) });
  const cards = () => body.children[0]?.children ?? [];
  const button = (card, label) => card.find((n) => n.tag === "button" && n.textContent === label);
  return { prompts, cards, button, errors };
}

test("one prompt per session and peer, removed when resolved", () => {
  const { prompts, cards } = setup();
  prompts.request({ sessionId: "s1", peerId: "p1", name: "bob", title: "prod" });
  prompts.request({ sessionId: "s1", peerId: "p1", name: "bob", title: "prod" });
  prompts.request({ sessionId: "s1", peerId: "p2", name: "eve", title: "prod" });
  assert.equal(cards().length, 2);
  assert.match(cards()[0].text, /bob wants to watch prod/);
  prompts.resolve({ sessionId: "s1", peerId: "p1" });
  assert.equal(cards().length, 1);
  assert.match(cards()[0].text, /eve/);
});

test("the name is text, never markup", () => {
  const { prompts, cards } = setup();
  prompts.request({ sessionId: "s", peerId: "p", name: "<img src=x onerror=alert(1)>" });
  const who = cards()[0].find((n) => n.tag === "b");
  assert.equal(who.textContent, "<img src=x onerror=alert(1)>");
  assert.equal(who.children.length, 0);
});

test("Allow and Deny send the answer and close the prompt", async () => {
  const sent = [];
  const { prompts, cards, button } = setup(async (...a) => sent.push(a));
  prompts.request({ sessionId: "s", peerId: "a" });
  prompts.request({ sessionId: "s", peerId: "d" });
  await button(cards()[0], "Allow").listeners.click();
  await button(cards()[0], "Deny").listeners.click();
  assert.deepEqual(sent, [["s", "a", true], ["s", "d", false]]);
  assert.equal(cards().length, 0);
});

test("a failed answer is reported and the stale prompt removed", async () => {
  const { prompts, cards, button, errors } = setup(async () => {
    throw new Error("no such join request");
  });
  prompts.request({ sessionId: "s", peerId: "p" });
  await button(cards()[0], "Allow").listeners.click();
  assert.equal(errors[0].message, "no such join request");
  assert.equal(cards().length, 0);
});

test("forget drops a session's prompts only", () => {
  const { prompts, cards } = setup();
  prompts.request({ sessionId: "s1", peerId: "p" });
  prompts.request({ sessionId: "s10", peerId: "p" });
  prompts.forget("s1");
  assert.equal(cards().length, 1);
});

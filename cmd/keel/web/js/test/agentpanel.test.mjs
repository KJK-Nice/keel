import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { createAgentPanel } from "../ui/agentpanel.js";
import { paneTarget } from "../lib/agent.js";

const dir = dirname(fileURLToPath(import.meta.url));

// Just enough DOM for agentpanel.js and its thread view.
class El {
  constructor(tag = "div") {
    this.tag = tag;
    this.children = [];
    this.attrs = {};
    this.dataset = {};
    this.style = { setProperty() {}, removeProperty() {} };
    this.classList = { add() {}, remove() {}, toggle() {} };
    this.innerHTML = "";
    this.textContent = "";
    this.hidden = false;
    this.value = "";
    this.scrollTop = 0;
    this.scrollHeight = 0;
    this.content = { firstElementChild: null };
  }
  setAttribute(k, v) { this.attrs[k] = String(v); }
  getAttribute(k) { return this.attrs[k] ?? null; }
  addEventListener() {}
  appendChild(n) { this.children.push(n); return n; }
  insertAdjacentHTML() {}
  querySelector() { return null; }
  querySelectorAll() { return []; }
  contains() { return false; }
  focus() {}
}

function setup() {
  const els = new Map();
  const doc = {
    documentElement: new El("html"),
    defaultView: null,
    getElementById: (id) => {
      if (id === "agent-sash") return null;
      if (!els.has(id)) els.set(id, new El());
      return els.get(id);
    },
    createElement: (t) => new El(t),
    addEventListener() {},
  };
  const pending = [];
  const api = (path) => {
    if (path.endsWith("/again")) return new Promise((resolve) => pending.push(resolve));
    return Promise.resolve({});
  };
  const panel = createAgentPanel({ doc, api, toast() {}, isMac: true, listPanes: () => [], copyText() {}, typeProposal: async () => null });
  return { conv: panel.conversation, pending };
}

const fresh = { id: "p2", sessionId: "s1", host: "web-1", command: "echo hi", bytes: "\u0015echo hi\n", risk: "readonly" };

test("Propose again lands in the conversation it was asked in", async () => {
  const { conv, pending } = setup();
  const asked = conv.again("p1");
  pending.shift()(fresh);
  assert.equal(await asked, "p2");
  assert.equal(conv.snapshot().model.turns.length, 1);
  assert.ok(conv.proposal("p2"));
});

test("Propose again is dropped when the conversation changed meanwhile", async () => {
  const { conv, pending } = setup();
  const before = conv.snapshot().id;
  const asked = conv.again("p1");
  conv.newConversation();
  assert.notEqual(conv.snapshot().id, before);
  pending.shift()(fresh);
  assert.equal(await asked, null);
  assert.equal(conv.snapshot().model.turns.length, 0, "no turn pushed into the new conversation");
  assert.equal(conv.proposal("p2"), null, "no card added to the new conversation");
});

test("a pane is named with its tab unless the tab only repeats it", () => {
  assert.equal(paneTarget("prod-db-01", "deploy"), "prod-db-01 · deploy");
  assert.equal(paneTarget("Local Shell", "Local Shell"), "Local Shell");
  assert.equal(paneTarget("web-1", ""), "web-1");
  assert.equal(paneTarget("", "deploy"), "deploy");
});

// The Agent page's container-query breakpoints are px (container queries
// cannot read tokens); they must stay the sums keel.css says they are.
test("Agent page breakpoints are the sums of the size tokens", () => {
  const tokens = JSON.parse(readFileSync(join(dir, "..", "..", "..", "..", "..", "docs", "design", "tokens.json"), "utf8"));
  const px = (name) => parseFloat(tokens.size.tokens.find((t) => t.name === name).value);
  const list = px("size-agentpage-list");
  const ctx = px("size-agentpage-context");
  const agent = px("size-agent");
  const css = readFileSync(join(dir, "..", "..", "css", "keel.css"), "utf8");
  const bps = [...css.matchAll(/@container \(max-width: (\d+)px\)/g)].map((m) => Number(m[1]));
  assert.deepEqual(bps, [list + ctx + agent, list + agent]);
});

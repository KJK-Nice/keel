import { test } from "node:test";
import assert from "node:assert/strict";
import * as NB from "../lib/notebooks.js";
import { traceText } from "../lib/agent.js";
import { createAgentPanel } from "../ui/agentpanel.js";

// Notebooks and the agent (docs/ui-redesign.md §18).

test("the trace says which notebook was sent, its size and redactions", () => {
  assert.equal(
    traceText({ status: "notebook", notebook: "Patroni failover", bytes: 2048, redactions: { secret: 2, captured: 1 } }),
    "Sent notebook Patroni failover (2048 bytes, 3 redacted)",
  );
  assert.equal(traceText({ status: "notebook", notebook: "Big", bytes: 32768, truncated: true }), "Sent notebook Big (32768 bytes, cut by Keel, 0 redacted)");
  assert.equal(traceText({ status: "skipped", reason: "notebook-missing" }), "Skipped a notebook · it was deleted");
});

test("placeholders are found as keel serve finds them", () => {
  assert.equal(NB.hasPlaceholder("export T=[secret:1]"), true);
  assert.equal(NB.hasPlaceholder("ping [ip:12]"), true);
  assert.equal(NB.hasPlaceholder("ssh [host:3]"), true);
  assert.equal(NB.hasPlaceholder("ls [abc] [secret:]"), false);
  assert.equal(NB.hasPlaceholder(""), false);
});

test("attaching: at most two, only saved notebooks, in order", () => {
  let ids = NB.toggleAttached([], "a");
  ids = NB.toggleAttached(ids, "b");
  assert.deepEqual(NB.toggleAttached(ids, "c"), ["a", "b"], "a third is not added");
  assert.deepEqual(NB.toggleAttached(ids, "a"), ["b"]);
  assert.deepEqual(NB.reconcileAttached(["a", "gone", "a", "b"], [{ id: "a" }, { id: "b" }]), ["a", "b"]);
  const list = NB.attachable([{ id: "z", name: "zeta" }, { id: "p", name: "pinned", pinned: true }, { id: "a", name: "alpha" }, { name: "unsaved" }]);
  assert.deepEqual(list.map((n) => n.id), ["p", "a", "z"]);
});

test("draft card words: warning after a pane read, done states, preview", () => {
  assert.equal(NB.proposalWarn({ panesRead: [] }), false);
  assert.equal(NB.proposalWarn({ panesRead: ["web-1"] }), true);
  assert.match(NB.proposalWarning({ fromPane: true, seenIn: "db-2" }), /matches pane db-2/);
  assert.match(NB.proposalDone({ status: "saved" }), /new notebook, marked From agent/);
  assert.equal(NB.proposalDone({ status: "dismissed" }), "Dismissed. Nothing was saved.");
  const pv = NB.preview("## A\n\nline 1\n\n```sh\necho x\n```\nmore\nand more\nlast", 4);
  assert.deepEqual(pv.lines, ["## A", "line 1", "```sh", "echo x"]);
  assert.equal(pv.more, 4);
  assert.equal(NB.sizeLabel({ bytes: 120, body: "```sh\na\n```\n```sh\nb\n```" }), "120 bytes · 2 command blocks");
  assert.match(NB.savedText({ redacted: 2 }), /2 secret-looking values were replaced/);
});

globalThis.requestAnimationFrame ??= (fn) => setTimeout(fn, 0);

// --- the panel: only attached notebooks are named in an ask, and a draft
// card is escaped and opens the editor through the draft route ---

class El {
  constructor(tag = "div", deep = false) {
    this.tag = tag;
    this.deep = deep;
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
  }
  get content() {
    return { firstElementChild: this.deep ? new El("div", true) : null };
  }
  setAttribute(k, v) { this.attrs[k] = String(v); }
  getAttribute(k) { return this.attrs[k] ?? null; }
  addEventListener() {}
  appendChild(n) { this.children.push(n); return n; }
  insertAdjacentHTML(_, html) { this.innerHTML += html; }
  querySelector() { return this.deep ? new El("div", true) : null; }
  querySelectorAll() { return []; }
  contains() { return false; }
  focus() {}
}

function setup(notebooks, reply = {}) {
  const els = new Map();
  const doc = {
    documentElement: new El("html"),
    defaultView: null,
    getElementById: (id) => {
      if (id === "agent-sash") return null;
      if (!els.has(id)) els.set(id, new El());
      return els.get(id);
    },
    createElement: (t) => new El(t, t === "template"),
    addEventListener() {},
  };
  const calls = [];
  const opened = [];
  const api = async (path, opts = {}) => {
    calls.push({ path, method: opts.method || "GET", body: opts.body ? JSON.parse(opts.body) : null });
    if (path === "/api/agent/ask") return { conversationId: JSON.parse(opts.body).conversationId, trace: [], proposals: [], notes: [], runbooks: [], ...reply };
    if (path.endsWith("/draft")) return { draftId: "nbd_1", name: "T", body: "B", placeholders: true };
    return {};
  };
  const panel = createAgentPanel({
    doc,
    api,
    toast() {},
    isMac: true,
    listPanes: () => [],
    listNotebooks: () => notebooks,
    copyText() {},
    typeProposal: async () => null,
    openNotebookDraft: (d, o) => opened.push({ d, o }),
  });
  return { panel, conv: panel.conversation, calls, opened, els };
}

const lastAsk = (calls) => calls.filter((c) => c.path === "/api/agent/ask").at(-1);

const nbs = [
  { id: "nb1", name: "Ops" },
  { id: "nb2", name: "Deploy" },
];

test("an ask names no notebook until one is attached, then only the attached ones", async () => {
  const { panel, conv, calls } = setup(nbs);
  await conv.ask("first");
  assert.equal(lastAsk(calls).body.notebooks, undefined, "nothing attached, nothing named");
  panel.attachNotebook("nb2");
  panel.attachNotebook("nb-unknown");
  assert.deepEqual(panel.attached(), ["nb2"]);
  await conv.ask("second");
  assert.deepEqual(lastAsk(calls).body.notebooks, ["nb2"]);
  // Still attached for the next question, until removed.
  await conv.ask("third");
  assert.deepEqual(lastAsk(calls).body.notebooks, ["nb2"]);
  conv.detachNotebook("nb2");
  await conv.ask("fourth");
  assert.equal(lastAsk(calls).body.notebooks, undefined);
  // A new conversation starts with none.
  panel.attachNotebook("nb1");
  conv.newConversation();
  assert.deepEqual(panel.attached(), []);
});

test("a notebook deleted after it was attached is not named", async () => {
  const list = nbs.slice();
  const { panel, conv, calls } = setup(list);
  panel.attachNotebook("nb1");
  list.splice(0, 1);
  await conv.ask("q");
  assert.equal(lastAsk(calls).body.notebooks, undefined);
});

test("chips and the picker escape notebook names", () => {
  const { conv } = setup([{ id: "x", name: '<img src=x onerror="alert(1)">' }]);
  conv.attachNotebook("x");
  const chips = conv.attachedChipsHTML();
  assert.ok(!chips.includes("<img"), chips);
  assert.ok(chips.includes("&lt;img"));
  assert.ok(!conv.nbPickerHTML().includes("<img"));
});

test("a notebook draft card is escaped and Open draft goes through the draft route", async () => {
  const draft = { id: "nbp_1", title: "<b>title</b>", body: "<script>alert(1)</script>\n```sh\necho hi\n```", bytes: 40, placeholders: true, panesRead: ["web-1"], status: "open" };
  const { conv, calls, opened, els } = setup(nbs, { notebooks: [draft] });
  await conv.ask("write this up");
  const card = els.get("agent-thread").children.find((n) => n?.dataset?.nbpId === "nbp_1");
  assert.ok(card, "the card was added");
  assert.ok(!card.innerHTML.includes("<script>") && !card.innerHTML.includes("<b>title"), card.innerHTML);
  assert.match(card.innerHTML, /Notebook draft/);
  assert.match(card.innerHTML, /After reading panes/);
  assert.match(card.innerHTML, /Placeholders/);
  assert.match(card.innerHTML, /Nothing is saved until you open the draft/);
  assert.ok(!calls.some((c) => c.path.includes("/api/notebooks")), "nothing saved from the card");
  assert.ok(card.innerHTML.includes("data-nbp-open"));
  // Open draft: the server's draft (with its draftId) goes to the editor;
  // nothing is saved until the editor's Save, which reports back.
  await conv.openNbDraft("nbp_1");
  const call = calls.at(-1);
  assert.equal(call.method, "POST");
  assert.match(call.path, /^\/api\/agent\/conversations\/[^/]+\/notebooks\/nbp_1\/draft$/);
  assert.equal(opened.length, 1);
  assert.equal(opened[0].d.draftId, "nbd_1");
  opened[0].o.onSaved({ notebook: { id: "nb9" }, proposal: { ...draft, status: "saved", notebookId: "nb9" } });
  assert.match(card.innerHTML, /Saved to Notebooks/);
  assert.match(card.innerHTML, /data-nbp-notebook="nb9"/);
});

import { test } from "node:test";
import assert from "node:assert/strict";
import * as NB from "../lib/notebooks.js";
import { traceText } from "../lib/agent.js";
import { createAgentPanel } from "../ui/agentpanel.js";

// Notebooks and the agent (docs/ui-redesign.md §18).

test("the trace says which notebooks were sent, read or not sent", () => {
  assert.equal(
    traceText({ status: "notebook", notebook: "Patroni failover", bytes: 2048, redactions: { secret: 2, captured: 1 } }),
    "Sent notebook Patroni failover (2048 bytes, 3 redacted)",
  );
  assert.equal(traceText({ status: "notebook", notebook: "Big", bytes: 32768, truncated: true }), "Sent notebook Big (32768 bytes, cut by Keel, 0 redacted)");
  assert.equal(traceText({ status: "notebook", notebook: "Ops", bytes: 10, asked: true }), "Read notebook Ops (10 bytes, 0 redacted) · the agent asked");
  assert.equal(traceText({ status: "notebook-index", count: 3 }), "Sent the titles of 3 notebooks (not private)");
  assert.equal(traceText({ status: "skipped", reason: "notebook-missing" }), "Skipped a notebook · it was deleted");
  assert.match(traceText({ status: "skipped", reason: "notebook-private" }), /private notebook/);
  assert.match(traceText({ status: "skipped", reason: "notebook-limit" }), /limit/);
});

test("placeholders are found as keel serve finds them", () => {
  assert.equal(NB.hasPlaceholder("export T=[secret:1]"), true);
  assert.equal(NB.hasPlaceholder("ping [ip:12]"), true);
  assert.equal(NB.hasPlaceholder("ssh [host:3]"), true);
  assert.equal(NB.hasPlaceholder("ls [abc] [secret:]"), false);
  assert.equal(NB.hasPlaceholder(""), false);
});

test("attaching: at most two, only saved notebooks, never a private one", () => {
  const list0 = [{ id: "a" }, { id: "b" }, { id: "c" }, { id: "p", private: true }];
  let ids = NB.toggleAttached([], "a", list0);
  assert.deepEqual(NB.toggleAttached(ids, "p", list0), ["a"], "a private notebook is not attached");
  ids = NB.toggleAttached(ids, "b", list0);
  assert.deepEqual(NB.toggleAttached(ids, "c", list0), ["a", "b"], "a third is not added");
  assert.deepEqual(NB.toggleAttached(ids, "a", list0), ["b"]);
  assert.deepEqual(NB.reconcileAttached(["a", "gone", "a", "b", "p"], list0), ["a", "b"], "made private: dropped");
  const list = NB.attachable([{ id: "z", name: "zeta" }, { id: "p", name: "pinned", pinned: true }, { id: "a", name: "alpha" }, { name: "unsaved" }]);
  assert.deepEqual(list.map((n) => n.id), ["p", "a", "z"]);
});

test("card words and actions follow what keel serve did", () => {
  assert.equal(NB.cardHead({ kind: "create", status: "created" }), "Created notebook");
  assert.deepEqual(NB.cardActions({ kind: "create", status: "created", notebookId: "n" }), ["open", "undo"]);
  assert.deepEqual(NB.cardActions({ kind: "create", status: "undone", versionId: "v" }), ["restore"]);
  assert.equal(NB.cardHead({ kind: "edit", status: "edited" }), "Edited notebook");
  assert.match(NB.cardStatus({ kind: "edit", status: "edited" }), /Undo puts it back/);
  assert.deepEqual(NB.cardActions({ kind: "edit", status: "edited", notebookId: "n" }), ["open", "undo"]);
  assert.match(NB.cardStatus({ kind: "edit", status: "refused", reason: "conflict" }), /You changed it after the agent read it/);
  assert.deepEqual(NB.cardActions({ kind: "edit", status: "refused", reason: "private", notebookId: "n" }), []);
  assert.equal(NB.cardHead({ kind: "delete", status: "proposed" }), "Delete notebook?");
  assert.deepEqual(NB.cardActions({ kind: "delete", status: "proposed", notebookId: "n" }), ["open", "keep", "delete"]);
  assert.deepEqual(NB.cardActions({ kind: "delete", status: "deleted", versionId: "v" }), ["restore"]);
  assert.deepEqual(NB.cardActions({ kind: "edit", status: "edited", notebookId: "n", expired: true }), ["open"], "an expired card cannot undo");
  assert.match(NB.cardStatus({ status: "open" }), /earlier version of Keel/);
  assert.match(NB.deleteConfirmText({ title: "Ops" }), /“Ops”.*Recently deleted/);
  assert.equal(NB.warned({ panesRead: [] }), false);
  assert.equal(NB.warned({ panesRead: ["web-1"] }), true);
  assert.match(NB.paneWarning({ fromPane: true, seenIn: "db-2" }), /matches pane db-2/);
});

test("diffLines aligns the lines that stayed and marks the rest", () => {
  const d = NB.diffLines("a\nb\nc\nd", "a\nB\nc\nd\ne");
  assert.deepEqual(
    d.map((l) => `${l.op}:${l.text}`),
    ["same:a", "del:b", "add:B", "same:c", "same:d", "add:e"],
  );
  assert.equal(NB.diffSummary(d), "2 lines added · 1 removed");
  assert.equal(NB.diffSummary(NB.diffLines("x", "x")), "No changes");
  // Too large to align: all of a removed, all of b added.
  const big = NB.diffLines("1\n2\n3", "4\n5\n6", 4);
  assert.deepEqual(big.map((l) => l.op), ["del", "del", "del", "add", "add", "add"]);
  const v = NB.diffView(NB.diffLines("1\n2\n3\n4\n5\n6\n7\n8", "1\n2\n3\n4\n5\n6\n7\nX"), 1);
  assert.deepEqual(v.map((l) => l.op), ["gap", "same", "del", "add"]);
  assert.equal(v[0].count, 6);
});

test("history rows say who and what", () => {
  const now = Date.parse("2026-10-19T12:00:00Z");
  const l = NB.versionLabel({ by: "user", event: "before-agent-edit", summary: "Adds a step", at: "2026-10-19T11:58:00Z" }, now);
  assert.deepEqual([l.who, l.what, l.when, l.summary], ["You", "Before the agent's edit", "2 min ago", "Then: Adds a step"]);
  assert.equal(NB.versionLabel({ by: "agent", event: "agent-version" }, now).who, "Agent");
  assert.equal(NB.versionText({ cells: [{ body: "a" }, { body: "b" }] }), "a\n\nb");
  assert.equal(NB.privateLabel({ private: true }), "Private");
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
  const confirms = [];
  const flushed = [];
  const confirmAnswer = { value: true };
  const api = async (path, opts = {}) => {
    calls.push({ path, method: opts.method || "GET", body: opts.body ? JSON.parse(opts.body) : null });
    if (path === "/api/agent/ask") return { conversationId: JSON.parse(opts.body).conversationId, trace: [], proposals: [], notes: [], runbooks: [], ...reply };
    if (path.endsWith("/undo")) return { proposal: { ...reply.notebooks[0], status: "undone" }, notebook: { id: "nb1", name: "Ops", rev: 3 } };
    if (path.endsWith("/delete")) return { proposal: { ...reply.notebooks[0], status: "deleted", versionId: "nbv_1" } };
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
    confirm: async () => (confirms.push(1), confirmAnswer.value),
    flushNotebooks: async () => flushed.push(1),
    editingNotebooks: () => ["nb2"],
    notebookChanged: (rec, o) => opened.push({ changed: rec, o }),
    notebookRemoved: (id) => opened.push({ removed: id }),
  });
  return { panel, conv: panel.conversation, calls, opened, els, confirms, flushed, confirmAnswer };
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

test("an ask saves typing first and names notebooks still saving", async () => {
  const { conv, calls, flushed } = setup(nbs);
  await conv.ask("q");
  assert.equal(flushed.length, 1);
  assert.deepEqual(lastAsk(calls).body.editing, ["nb2"]);
});

test("a private notebook is listed but cannot be attached", () => {
  const { panel, conv } = setup([{ id: "p", name: "Keys", private: true }]);
  panel.attachNotebook("p");
  assert.deepEqual(panel.attached(), []);
  const html = conv.nbPickerHTML();
  assert.match(html, /aria-disabled="true"/);
  assert.match(html, /Private/);
});

test("an edit card is escaped, the page shows the edited notebook, and Undo is the user's route", async () => {
  const card = { id: "nbc_1", kind: "edit", status: "edited", notebookId: "nb1", title: "<b>title</b>", summary: "<script>x</script>", bytes: 40, rev: 2, versionId: "nbv_1", panesRead: ["web-1"], record: { id: "nb1", name: "Ops", rev: 2 } };
  const { conv, calls, opened, els } = setup(nbs, { notebooks: [card] });
  await conv.ask("mark it");
  const node = els.get("agent-thread").children.find((n) => n?.dataset?.nbpId === "nbc_1");
  assert.ok(node, "the card was added");
  assert.ok(!node.innerHTML.includes("<script>") && !node.innerHTML.includes("<b>title"), node.innerHTML);
  assert.match(node.innerHTML, /Edited notebook/);
  assert.match(node.innerHTML, /After reading panes/);
  assert.match(node.innerHTML, /data-nbp-undo/);
  assert.deepEqual(opened[0], { changed: card.record, o: { byAgent: true } });
  await conv.nbPropAction("nbc_1", "undo");
  const call = calls.at(-1);
  assert.equal(call.method, "POST");
  assert.match(call.path, /^\/api\/agent\/conversations\/[^/]+\/notebooks\/nbc_1\/undo$/);
  assert.equal(opened.at(-1).changed.rev, 3);
  assert.match(node.innerHTML, /Undone/);
});

test("a delete card deletes only after the user confirms", async () => {
  const card = { id: "nbc_2", kind: "delete", status: "proposed", notebookId: "nb1", title: "Ops", rev: 1 };
  const { conv, calls, opened, els, confirms, confirmAnswer } = setup(nbs, { notebooks: [card] });
  await conv.ask("drop it");
  const node = els.get("agent-thread").children.find((n) => n?.dataset?.nbpId === "nbc_2");
  assert.match(node.innerHTML, /Delete notebook\?/);
  assert.match(node.innerHTML, /Nothing is deleted unless you confirm/);
  confirmAnswer.value = false;
  await conv.nbPropAction("nbc_2", "delete");
  assert.equal(confirms.length, 1);
  assert.ok(!calls.some((c) => c.path.endsWith("/delete")), "no delete without the confirm");
  confirmAnswer.value = true;
  await conv.nbPropAction("nbc_2", "delete");
  assert.ok(calls.at(-1).path.endsWith("/nbc_2/delete"));
  assert.deepEqual(opened.at(-1), { removed: "nb1" });
  assert.match(node.innerHTML, /Recently deleted/);
  assert.match(node.innerHTML, /data-nbp-restore/);
});

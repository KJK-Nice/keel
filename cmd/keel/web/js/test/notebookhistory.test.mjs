import { test } from "node:test";
import assert from "node:assert/strict";
import { createNotebookHistory } from "../ui/notebookhistory.js";

// Just enough DOM: elements by id that keep what is written to them.
function setup({ responses = {}, confirmed = true, notebooks = {} } = {}) {
  const els = {};
  const doc = {
    getElementById: (id) =>
      (els[id] ||= {
        id,
        innerHTML: "",
        textContent: "",
        disabled: false,
        listeners: {},
        addEventListener(type, fn) { this.listeners[type] = fn; },
      }),
  };
  const calls = [];
  const bodies = [];
  const toasts = [];
  const log = [];
  const api = async (path, opts = {}) => {
    calls.push([opts.method || "GET", path]);
    if (opts.body) bodies.push(JSON.parse(opts.body));
    const r = responses[`${opts.method || "GET"} ${path}`];
    if (r instanceof Error) throw r;
    return r;
  };
  const modal = { id: "notebook-history-modal" };
  const h = createNotebookHistory({
    doc,
    modal,
    api,
    toast: (m, err) => toasts.push([m, !!err]),
    confirm: async () => confirmed,
    openModal: (m) => log.push(["open", m.id]),
    closeModal: (m) => log.push(["close", m.id]),
    notebook: (id) => notebooks[id],
    flush: async (id) => log.push(["flush", id]),
    remember: (rec) => log.push(["remember", rec.id]),
    changed: (rec) => log.push(["changed", rec.id]),
  });
  return { h, els, calls, bodies, toasts, log };
}

test("Recently deleted lists the deleted notebooks and shows the first as text", async () => {
  const { h, els, log } = setup({
    responses: {
      "GET /api/notebook-trash": { versions: [{ id: "t1", name: "Deploy <prod>", keptAt: new Date().toISOString() }] },
      "GET /api/notebook-trash/t1": { cells: [{ body: "kubectl get pods" }] },
    },
  });
  await h.openTrash();
  assert.deepEqual(log, [["open", "notebook-history-modal"]]);
  assert.equal(els["notebook-history-title"].textContent, "Recently deleted");
  assert.match(els["notebook-history-list"].innerHTML, /Deploy &lt;prod&gt;/);
  assert.match(els["notebook-history-list"].innerHTML, /aria-selected="true"/);
  assert.match(els["notebook-history-diff"].innerHTML, /notebook-diff--plain/);
  assert.equal(els["notebook-history-restore"].textContent, "Restore notebook");
  assert.equal(els["notebook-history-restore"].disabled, false);
});

test("an empty trash says so and leaves Restore off", async () => {
  const { h, els } = setup({ responses: { "GET /api/notebook-trash": { versions: [] } } });
  await h.openTrash();
  assert.match(els["notebook-history-list"].innerHTML, /Nothing was deleted in the last 30 days/);
  assert.equal(els["notebook-history-restore"].disabled, true);
});

test("History saves the notebook first and diffs a version against now", async () => {
  const { h, els, log } = setup({
    notebooks: { n1: { id: "n1", name: "Ops", rev: 3, cells: [{ body: "line a\nline b" }] } },
    responses: {
      "GET /api/notebooks/n1/history": { versions: [{ id: "v1", event: "before-agent-edit", by: "agent" }] },
      "GET /api/notebooks/n1/history/v1": { cells: [{ body: "line a" }] },
    },
  });
  await h.openHistory("n1", { agentEdit: true });
  assert.deepEqual(log[0], ["flush", "n1"]);
  assert.equal(els["notebook-history-title"].textContent, "History · Ops");
  assert.match(els["notebook-history-diff"].innerHTML, /notebook-diff-line--add/);
  assert.match(els["notebook-history-diff"].innerHTML, /line b/);
});

test("restoring a version saves first, posts the notebook's rev and hands back the result", async () => {
  const s = setup({
    notebooks: { n1: { id: "n1", name: "Ops", rev: 3, cells: [] } },
    responses: {
      "GET /api/notebooks/n1/history": { versions: [{ id: "v1" }] },
      "GET /api/notebooks/n1/history/v1": { cells: [] },
      "POST /api/notebooks/n1/history/v1/restore": { id: "n1", rev: 4 },
    },
  });
  await s.h.openHistory("n1");
  s.h.bind();
  s.log.length = 0;
  s.calls.length = 0;
  s.els["notebook-history-restore"].listeners.click();
  await new Promise((r) => setTimeout(r, 0));
  assert.deepEqual(s.calls, [["POST", "/api/notebooks/n1/history/v1/restore"]]);
  assert.deepEqual(s.log, [["flush", "n1"], ["changed", "n1"], ["close", "notebook-history-modal"]]);
  assert.match(s.toasts.at(-1)[0], /The version it replaced is in History/);
  assert.deepEqual(s.bodies, [{ rev: 3 }]);
});

test("Restore, Delete and a row click go through bind", async () => {
  const s = setup({
    confirmed: false,
    responses: {
      "GET /api/notebook-trash": { versions: [{ id: "t1", name: "A" }, { id: "t2", name: "B" }] },
      "GET /api/notebook-trash/t1": { cells: [] },
      "GET /api/notebook-trash/t2": { cells: [] },
      "POST /api/notebook-trash/t2/restore": { id: "n9", name: "B" },
    },
  });
  await s.h.openTrash();
  s.h.bind();
  // A row click selects it.
  await s.els["notebook-history-list"].listeners.click({ target: { closest: () => ({ dataset: { notebookVersion: "t2" } }) } });
  await new Promise((r) => setTimeout(r, 0));
  assert.match(s.els["notebook-history-list"].innerHTML, /aria-selected="true" data-notebook-version="t2"/);
  // Delete asks first; a No changes nothing.
  s.calls.length = 0;
  s.els["notebook-history-purge"].listeners.click();
  await new Promise((r) => setTimeout(r, 0));
  assert.deepEqual(s.calls, []);
  // Restore puts it back in Notebooks and closes the dialog.
  s.els["notebook-history-restore"].listeners.click();
  await new Promise((r) => setTimeout(r, 0));
  assert.deepEqual(s.calls, [["POST", "/api/notebook-trash/t2/restore"]]);
  assert.deepEqual(s.log.slice(-2), [["remember", "n9"], ["close", "notebook-history-modal"]]);
  assert.match(s.toasts.at(-1)[0], /Restored “B” to Notebooks/);
});

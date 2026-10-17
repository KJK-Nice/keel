import { test } from "node:test";
import assert from "node:assert/strict";
import { createLatestRequest } from "../lib/latest.js";

function harness() {
  let timers = [];
  let nextId = 1;
  const controllers = [];
  const latest = createLatestRequest({
    delayMs: 100,
    setTimer: (fn) => {
      const id = nextId++;
      timers.push({ id, fn });
      return id;
    },
    clearTimer: (id) => {
      timers = timers.filter((t) => t.id !== id);
    },
    makeController: () => {
      const c = { aborted: false, signal: {}, abort() { this.aborted = true; } };
      c.signal = c;
      controllers.push(c);
      return c;
    },
  });
  return {
    latest,
    controllers,
    pending: () => timers.length,
    fire() {
      const run = timers;
      timers = [];
      for (const t of run) t.fn();
    },
  };
}

test("typing quickly sends one request, for the last value", async () => {
  const h = harness();
  const sent = [];
  for (const q of ["p", "pr", "pro", "prod"]) {
    h.latest.schedule(() => sent.push(q));
  }
  assert.equal(h.pending(), 1, "earlier keystrokes' timers are cleared");
  h.fire();
  await new Promise((r) => setTimeout(r, 0));
  assert.deepEqual(sent, ["prod"]);
});

test("a slow older response cannot paint over a newer one", async () => {
  const h = harness();
  let releaseOld;
  const painted = [];
  const old = h.latest.schedule(async (signal, isStale) => {
    await new Promise((r) => (releaseOld = r));
    if (!isStale()) painted.push("old");
    return "old";
  });
  h.fire();
  await new Promise((r) => setTimeout(r, 0));

  const fresh = h.latest.schedule(async (signal, isStale) => {
    if (!isStale()) painted.push("new");
    return "new";
  });
  assert.equal(h.controllers[0].aborted, false, "not aborted until the newer job starts");
  h.fire();
  assert.equal(await fresh, "new");
  assert.equal(h.controllers[0].aborted, true, "the in-flight request is aborted");

  releaseOld();
  await old;
  assert.deepEqual(painted, ["new"]);
});

test("an AbortError from the superseded fetch is swallowed", async () => {
  const h = harness();
  const first = h.latest.now(async () => {
    await new Promise((r) => setTimeout(r, 5));
    const err = new Error("aborted");
    err.name = "AbortError";
    throw err;
  });
  h.latest.now(async () => "second");
  assert.equal(await first, undefined);
});

test("now runs at once and cancels a pending debounce", async () => {
  const h = harness();
  const sent = [];
  h.latest.schedule(() => sent.push("debounced"));
  await h.latest.now(() => sent.push("enter"));
  assert.equal(h.pending(), 0);
  assert.deepEqual(sent, ["enter"]);
});

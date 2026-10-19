import { test } from "node:test";
import assert from "node:assert/strict";
import { createAutosave } from "../lib/autosave.js";

// A manual clock: advance(ms) runs the timers that come due, in order.
function fakeTimers() {
  let now = 0;
  let seq = 0;
  const due = new Map();
  return {
    setTimeout(fn, ms) {
      const id = ++seq;
      due.set(id, { at: now + ms, fn });
      return id;
    },
    clearTimeout(id) {
      due.delete(id);
    },
    async advance(ms) {
      const end = now + ms;
      for (;;) {
        const next = [...due.entries()].filter(([, t]) => t.at <= end).sort((a, b) => a[1].at - b[1].at)[0];
        if (!next) break;
        due.delete(next[0]);
        now = next[1].at;
        next[1].fn();
        await settle();
      }
      now = end;
      await settle();
    },
  };
}

const settle = () => new Promise((r) => setImmediate(r));

function setup(opts = {}) {
  const timers = fakeTimers();
  const saves = [];
  let release = null;
  const auto = createAutosave({
    idleMs: 1000,
    maxWaitMs: 5000,
    timers,
    save: () => {
      saves.push(timers);
      if (opts.slow) return new Promise((r) => (release = r));
      return Promise.resolve();
    },
  });
  return { timers, saves, auto, release: () => release?.() };
}

test("edits settle into one save after the idle delay", async () => {
  const { timers, saves, auto } = setup();
  for (let i = 0; i < 10; i++) {
    auto.schedule();
    await timers.advance(150);
  }
  assert.equal(saves.length, 0);
  await timers.advance(1000);
  assert.equal(saves.length, 1);
  assert.equal(auto.pending, false);
});

test("a long burst still saves every maxWait", async () => {
  const { timers, saves, auto } = setup();
  for (let i = 0; i < 100; i++) {
    auto.schedule();
    await timers.advance(100); // 10 s of typing, never idle for 1 s
  }
  // At 5 s and 10 s; the last edit (9.9 s) went out with the 10 s save.
  assert.equal(saves.length, 2);
  await timers.advance(10000);
  assert.equal(saves.length, 2);
  assert.equal(auto.pending, false);
});

test("flush saves a pending edit now and nothing when clean", async () => {
  const { saves, auto } = setup();
  await auto.flush();
  assert.equal(saves.length, 0);
  auto.schedule();
  await auto.flush();
  assert.equal(saves.length, 1);
  assert.equal(auto.pending, false);
});

test("one save at a time; an edit during a save runs one more after it", async () => {
  const { timers, saves, auto, release } = setup({ slow: true });
  auto.schedule();
  await timers.advance(1000);
  assert.equal(saves.length, 1);
  auto.schedule();
  await timers.advance(1000); // due, but the first save is still running
  assert.equal(saves.length, 1);
  assert.equal(auto.pending, true);
  release();
  await settle();
  await settle();
  assert.equal(saves.length, 2);
  release();
  await settle();
  assert.equal(auto.pending, false);
});

test("a failed save does not stop later ones", async () => {
  const timers = fakeTimers();
  let calls = 0;
  const auto = createAutosave({
    timers,
    save: () => {
      calls++;
      return Promise.reject(new Error("offline"));
    },
  });
  auto.schedule();
  await auto.flush();
  auto.schedule();
  await auto.flush();
  assert.equal(calls, 2);
});

import { test } from "node:test";
import assert from "node:assert/strict";
import { createFrameThrottle } from "../ui/framethrottle.js";

// A fake clock with frames and timers run by hand.
function harness(intervalMs = 50) {
  let t = 1000;
  let frames = [];
  let timers = [];
  const throttle = createFrameThrottle({
    intervalMs,
    now: () => t,
    raf: (fn) => frames.push(fn),
    setTimer: (fn, ms) => timers.push({ fn, at: t + ms }),
  });
  return {
    throttle,
    frames: () => frames.length,
    timers: () => timers.length,
    flushFrames() {
      const run = frames;
      frames = [];
      for (const fn of run) fn();
    },
    advance(ms) {
      t += ms;
      const due = timers.filter((x) => x.at <= t);
      timers = timers.filter((x) => x.at > t);
      for (const x of due) x.fn();
    },
  };
}

test("after a quiet spell the first call runs on the next frame", () => {
  const h = harness();
  let runs = 0;
  h.throttle.schedule(() => runs++);
  assert.equal(h.frames(), 1);
  assert.equal(h.timers(), 0);
  h.flushFrames();
  assert.equal(runs, 1);
});

test("calls while a frame is queued share it", () => {
  const h = harness();
  let runs = 0;
  for (let i = 0; i < 5; i++) h.throttle.schedule(() => runs++);
  assert.equal(h.frames(), 1);
  h.flushFrames();
  assert.equal(runs, 1);
});

test("a burst runs at most once per interval, with a trailing run", () => {
  const h = harness(50);
  let runs = 0;
  let seen = -1;
  let state = 0;
  const job = () => {
    runs++;
    seen = state;
  };
  h.throttle.schedule(job);
  h.flushFrames();
  // Streaming: a call every 10 ms for 200 ms.
  for (let i = 1; i <= 20; i++) {
    h.advance(10);
    state = i;
    h.throttle.schedule(job);
    h.flushFrames();
  }
  assert.ok(runs <= 1 + Math.ceil(200 / 50), `runs = ${runs}`);
  // Let the last timer fire: the final state is always republished.
  h.advance(50);
  h.flushFrames();
  assert.equal(seen, 20);
  assert.equal(h.timers(), 0);
  assert.equal(h.frames(), 0);
});

test("the trailing run uses the latest job", () => {
  const h = harness(50);
  const ran = [];
  h.throttle.schedule(() => ran.push("a"));
  h.flushFrames();
  h.advance(10);
  h.throttle.schedule(() => ran.push("b"));
  h.throttle.schedule(() => ran.push("c"));
  h.advance(50);
  h.flushFrames();
  assert.deepEqual(ran, ["a", "c"]);
});

import { test } from "node:test";
import assert from "node:assert/strict";
import { createSharePopover } from "../ui/sharepop.js";

// Just enough DOM for sharepop.js.
function el() {
  return {
    hidden: true,
    attrs: {},
    style: {},
    innerHTML: "",
    focused: 0,
    listeners: {},
    setAttribute(k, v) { this.attrs[k] = v; },
    getBoundingClientRect: () => ({ bottom: 30, right: 900 }),
    focus() { this.focused++; },
    querySelector: () => null,
    addEventListener(type, fn) { this.listeners[type] = fn; },
  };
}

function setup({ target = { id: "p1", runtimeId: "r1", title: "web" }, apiImpl } = {}) {
  const pop = el();
  const button = el();
  button.hidden = false;
  const calls = [];
  const toasts = [];
  const stopped = [];
  let refocused = 0;
  const panes = new Map([["p1", target], ["p2", { id: "p2", runtimeId: "r2" }]]);
  const sp = createSharePopover({
    doc: { activeElement: null, addEventListener() {} },
    win: { innerWidth: 1000, addEventListener() {} },
    pop,
    button,
    badge: el(),
    pane: (id) => panes.get(id),
    shareTarget: () => target,
    stopTarget: () => target,
    caps: () => ({ people: [], canGrant: true, canTake: false, canAsk: false }),
    api: apiImpl || (async (path, opts) => {
      calls.push([path, opts.method]);
      return { url: "https://join.example/s/abc" };
    }),
    copyText: async () => true,
    toast: (m, err) => toasts.push([m, !!err]),
    onStopped: (p) => stopped.push(p.runtimeId),
    refocus: () => refocused++,
  });
  return { sp, pop, button, calls, toasts, stopped, refocused: () => refocused };
}

const tick = () => new Promise((r) => setTimeout(r, 0));

test("open shares the focused session once and shows the link", async () => {
  const { sp, pop, button, calls } = setup();
  sp.open();
  assert.equal(sp.isOpen(), true);
  assert.equal(button.attrs["aria-expanded"], "true");
  assert.equal(pop.style.right, "100px");
  await tick();
  assert.deepEqual(calls, [["/api/share", "POST"]]);
  assert.match(pop.innerHTML, /join\.example\/s\/abc/);
  // The link is remembered: opening again does not post again.
  sp.close();
  sp.open();
  await tick();
  assert.equal(calls.length, 1);
});

test("no target, no popover", () => {
  const { sp } = setup({ target: null });
  sp.open();
  assert.equal(sp.isOpen(), false);
});

test("follow closes on another pane or a joined one", async () => {
  const { sp } = setup();
  sp.open();
  await tick();
  sp.follow({ id: "p1" });
  assert.equal(sp.isOpen(), true);
  sp.follow({ id: "p2" });
  assert.equal(sp.isOpen(), false);
  sp.open();
  sp.follow({ id: "p1", remote: true });
  assert.equal(sp.isOpen(), false);
});

test("stop deletes the share, forgets it and refocuses", async () => {
  const s = setup();
  s.sp.open();
  await tick();
  await s.sp.stop();
  assert.deepEqual(s.calls.at(-1), ["/api/share/r1", "DELETE"]);
  assert.deepEqual(s.stopped, ["r1"]);
  assert.equal(s.sp.isOpen(), false);
  assert.equal(s.refocused(), 1);
  assert.match(s.toasts.at(-1)[0], /Sharing stopped/);
});

test("a failed share shows the error in the popover", async () => {
  const { sp, pop, toasts } = setup({ apiImpl: async () => { throw new Error("signal down"); } });
  sp.open();
  await tick();
  assert.match(pop.innerHTML, /signal down/);
  assert.equal(toasts.length, 0);
});

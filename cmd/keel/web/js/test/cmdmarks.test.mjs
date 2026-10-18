import { test } from "node:test";
import assert from "node:assert/strict";
import { attachCommandMarks, exitLabel, parseMark } from "../lib/cmdmarks.js";

test("parseMark is strict", () => {
  assert.deepEqual(parseMark("A"), { kind: "A", exit: null });
  assert.deepEqual(parseMark("A;cl=m;aid=1"), { kind: "A", exit: null });
  assert.deepEqual(parseMark("D;0"), { kind: "D", exit: 0 });
  assert.deepEqual(parseMark("D;127;aid=3"), { kind: "D", exit: 127 });
  assert.deepEqual(parseMark("D"), { kind: "D", exit: null });
  assert.deepEqual(parseMark("D;2147483647"), { kind: "D", exit: 2147483647 });
  for (const bad of ["D;-1", "D;+1", "D; 1", "D;1x", "D;2147483648", "D;99999999999", "D;<img>"]) {
    assert.equal(parseMark(bad).exit, null, bad);
  }
  for (const bad of ["", "Z", "AB", "P;k=v", "A;é", "A\u0007", null, undefined, 42, "A;" + "x".repeat(2043)]) {
    assert.equal(parseMark(bad), null, String(bad).slice(0, 20));
  }
  assert.equal(exitLabel(2), "exit 2");
});

// fakeTerm is the part of xterm's API the module uses: an OSC hook, markers
// at the cursor line, decorations that render into fake elements, and a
// viewport to jump in.
function fakeTerm() {
  let osc = null;
  const decorations = [];
  const t = {
    cursorLine: 0,
    scrolled: [],
    buffer: { active: { viewportY: 0 } },
    parser: {
      registerOscHandler(id, fn) {
        assert.equal(id, 133);
        osc = fn;
        return { dispose() { osc = null; } };
      },
    },
    registerMarker() {
      const m = { line: t.cursorLine, isDisposed: false, dispose() { this.isDisposed = true; this.line = -1; } };
      return m;
    },
    registerDecoration(opts) {
      const d = { opts, el: fakeEl(), onRender(fn) { fn(d.el); fn(d.el); } };
      decorations.push(d);
      return d;
    },
    scrollToLine(n) { t.scrolled.push(n); },
    send(data, line) {
      if (line !== undefined) t.cursorLine = line;
      return osc?.(data);
    },
    decorations,
  };
  return t;
}

function fakeEl() {
  const el = {
    dataset: {},
    classes: new Set(),
    children: [],
    title: "",
    classList: { add: (...c) => c.forEach((x) => el.classes.add(x)) },
    ownerDocument: { createElement: () => ({ className: "", textContent: "" }) },
    appendChild: (c) => el.children.push(c),
  };
  return el;
}

test("a mark per finished command, a chip only on failure", () => {
  const t = fakeTerm();
  const marks = attachCommandMarks(t);
  t.send("A", 0);
  t.send("B");
  t.send("C", 1);
  t.send("D;0", 2);
  t.send("A", 2);
  t.send("C", 3);
  t.send("D;2", 5);
  // Enter on an empty line and a stray D draw nothing.
  t.send("A", 5);
  t.send("D;0");
  t.send("D;1");
  assert.equal(t.decorations.length, 3);
  const [ok, fail, chip] = t.decorations;
  assert.ok(ok.el.classes.has("k-cmdmark--ok"));
  assert.equal(ok.opts.marker.line, 0);
  assert.equal(ok.el.children.length, 1, "rendered once although onRender fires twice");
  assert.ok(fail.el.classes.has("k-cmdmark--failed"));
  assert.equal(fail.el.title, "Command finished · exit 2");
  assert.equal(chip.opts.anchor, "right");
  assert.equal(chip.el.children[0].textContent, "exit 2");
  assert.match(chip.el.children[0].className, /k-badge--flare/);
  assert.equal(marks.count(), 3);
});

test("malformed marks are consumed and ignored", () => {
  const t = fakeTerm();
  attachCommandMarks(t);
  assert.equal(t.send("C"), true);
  assert.equal(t.send("D;<script>"), true);
  assert.equal(t.send("Q"), true);
  assert.equal(t.decorations.length, 1);
  assert.equal(t.decorations[0].el.title, "Command finished · exit status not reported");
});

test("jump moves between prompts relative to the viewport", () => {
  const t = fakeTerm();
  const marks = attachCommandMarks(t);
  for (const line of [3, 10, 20]) t.send("A", line);
  t.buffer.active.viewportY = 10;
  assert.equal(marks.jump(-1), true);
  assert.equal(marks.jump(1), true);
  assert.deepEqual(t.scrolled, [3, 20]);
  t.buffer.active.viewportY = 20;
  assert.equal(marks.jump(1), false);
  marks.dispose();
  assert.equal(marks.count(), 0);
  assert.equal(marks.jump(-1), false);
});

test("no xterm APIs, no marks", () => {
  assert.equal(attachCommandMarks({}), null);
  assert.equal(attachCommandMarks(null), null);
});

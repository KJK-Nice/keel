import { test } from "node:test";
import assert from "node:assert/strict";
import * as S from "../lib/suggest.js";

test("the prompt is split from what was typed", () => {
  assert.deepEqual(S.promptInput("sh-3.2$ show the files here   "), { prompt: "sh-3.2$ ", input: "show the files here", start: 8 });
  assert.equal(S.promptInput("deploy@prod-db-01:~$ which process is listening on 5432").input, "which process is listening on 5432");
  assert.equal(S.promptInput("root@box:~# # restart it").input, "# restart it");
  assert.equal(S.promptInput("[me@h dir]$ ").input, "");
  assert.equal(S.promptInput("❯ list containers").input, "list containers");
  assert.equal(S.promptInput("no prompt on this line"), null);
});

test("only #-lines and two or more words are worth a detection call", () => {
  assert.equal(S.worthDetecting("show the files"), true);
  assert.equal(S.worthDetecting("# restart"), true);
  assert.equal(S.worthDetecting("#"), false);
  assert.equal(S.worthDetecting("uptime"), false);
  assert.equal(S.worthDetecting("x".repeat(S.MAX_LINE + 1) + " y"), false);
  assert.equal(S.worthDetecting("a\u0003 b"), false);
});

test("plain ⇥ never inserts an option that needs ⇧ (§6.5)", () => {
  assert.deepEqual(S.insertGate({ needsShift: true }, false), { ok: false, reason: "shift" });
  assert.deepEqual(S.insertGate({ needsShift: true }, true), { ok: true });
  assert.deepEqual(S.insertGate({ needsShift: false }, false), { ok: true });
});

test("undo is ⌘Z on macOS and Ctrl+Shift+Z elsewhere; Ctrl+Z stays a signal", () => {
  assert.equal(S.undoKey({ key: "z", metaKey: true }, true), true);
  assert.equal(S.undoKey({ key: "z", ctrlKey: true }, true), false);
  assert.equal(S.undoKey({ key: "z", ctrlKey: true }, false), false, "Ctrl+Z is SIGTSTP");
  assert.equal(S.undoKey({ key: "Z", ctrlKey: true, shiftKey: true }, false), true);
  assert.equal(S.undoKey({ key: "z", metaKey: true, shiftKey: true }, true), false);
});

test("restore bytes clear the line and cannot press Enter", () => {
  assert.equal(S.restoreBytes("show files\r\nrm -rf /"), "\u0015show filesrm -rf /");
});

test("cards: badge, alternatives, keys and the destructive rule", () => {
  const sug = {
    options: [
      { command: "ss -ltnp 'sport = :5432'", why: "Lists listeners.", risk: "readonly", needsShift: false },
      { command: "lsof -iTCP:5432", risk: "readonly" },
      { command: "<b>x</b>", risk: "readonly" },
    ],
  };
  const html = S.cardHTML(sug, 0, { mac: true });
  assert.match(html, /class="k-sug"/);
  assert.match(html, /k-badge--tide">Read-only/);
  assert.match(html, /data-option="1"><span class="k-kbd">2<\/span>/);
  assert.match(html, /&lt;b&gt;x&lt;\/b&gt;/, "commands are escaped");
  assert.match(html, /⇥<\/span>insert/);
  const de = { options: [{ command: "rm -rf /tmp/x", risk: "destructive", needsShift: true }] };
  const dh = S.cardHTML(de, 0, { mac: false });
  assert.match(dh, /k-sug k-sug--danger/);
  assert.match(dh, /Deletes files/);
  assert.match(dh, /Shift<\/span><span class="k-kbd">Tab<\/span>insert anyway/);
  assert.match(dh, /Plain Tab never inserts a destructive command/);
  const prod = { options: [{ command: "touch /x", risk: "changes", needsShift: true }] };
  assert.match(S.cardHTML(prod, 0, { mac: true }), /Production host/);
});

function machine(over = {}) {
  const sent = [];
  const renders = [];
  const timers = [];
  let line = { input: "show the files here", start: 8, top: 0, cols: 80 };
  let driving = true;
  let t = 1000;
  const inserts = [];
  const restores = [];
  const m = S.createSuggestMachine({
    readLine: () => line,
    canSuggest: () => driving,
    sendDetect: (seq, l) => sent.push({ kind: "detect", seq, line: l }),
    sendSuggest: (seq, l) => sent.push({ kind: "suggest", seq, line: l }),
    insert: async (sug, i, shift) => {
      inserts.push({ sug, i, shift });
      return { ok: true };
    },
    restore: (b) => restores.push(b),
    render: (v) => renders.push(v.state),
    setTimer: (fn) => timers.push(fn) && timers.length,
    clearTimer: () => {},
    now: () => t,
    mac: true,
    ...over,
  });
  return {
    m,
    sent,
    renders,
    inserts,
    restores,
    fire: () => timers.splice(0).forEach((fn) => fn()),
    setLine: (input) => (line = input == null ? null : { ...line, input }),
    setDriving: (d) => (driving = d),
    tick: (ms) => (t += ms),
  };
}

const key = (k, mods = {}) => ({ type: "keydown", key: k, ...mods });

test("Notice → Suggest → Insert → ⌘Z", async () => {
  const h = machine();
  // ⇥ is the shell's until a line is noticed.
  assert.equal(h.m.onKey(key("Tab")), false);
  h.m.onInput("e");
  assert.equal(h.sent.length, 0, "never per key");
  h.fire();
  assert.deepEqual(h.sent[0], { kind: "detect", seq: 1, line: "show the files here" });
  h.m.onDetect({ seq: 1, plain: true, source: "jev" });
  assert.equal(h.m.state, "notice");
  assert.equal(h.m.onKey(key("Tab")), true);
  assert.equal(h.m.state, "loading");
  assert.equal(h.sent[1].kind, "suggest");
  const sug = { id: "s_1", options: [{ command: "echo a", risk: "readonly" }, { command: "echo b", risk: "readonly" }] };
  h.m.onSuggest({ seq: h.sent[1].seq, ok: true, suggestion: sug });
  assert.equal(h.m.state, "card");
  assert.equal(h.m.onKey(key("ArrowDown")), true);
  assert.equal(h.m.view().selected, 1);
  assert.equal(h.m.onKey(key("1")), true);
  assert.equal(h.m.view().selected, 0);
  assert.equal(h.m.onKey(key("Tab")), true);
  await new Promise((r) => setImmediate(r));
  assert.deepEqual(h.inserts, [{ sug, i: 0, shift: false }]);
  assert.equal(h.m.state, "inserted");
  h.setLine("echo a");
  h.m.onRender();
  assert.equal(h.m.view().seen, true);
  // ⌘Z restores the words as the user's own keystrokes.
  assert.equal(h.m.onKey(key("z", { metaKey: true })), true);
  assert.deepEqual(h.restores, ["\u0015show the files here"]);
  assert.equal(h.m.state, "idle");
});

test("a destructive option needs ⇧⇥; ⇥ only shows the rule", async () => {
  const h = machine();
  h.m.onInput("x");
  h.fire();
  h.m.onDetect({ seq: 1, plain: true, source: "hash" });
  h.m.onKey(key("Tab"));
  h.m.onSuggest({ seq: h.sent[1].seq, ok: true, suggestion: { id: "s", options: [{ command: "rm -rf /tmp/x", risk: "destructive", needsShift: true }] } });
  h.m.onKey(key("Tab"));
  await new Promise((r) => setImmediate(r));
  assert.equal(h.inserts.length, 0);
  assert.match(h.m.view().note, /Press ⇧⇥/);
  h.m.onKey(key("Tab", { shiftKey: true }));
  await new Promise((r) => setImmediate(r));
  assert.equal(h.inserts.length, 1);
  assert.equal(h.inserts[0].shift, true);
});

test("Esc keeps the words and the notice does not come back for them", () => {
  const h = machine();
  h.m.onInput("x");
  h.fire();
  h.m.onDetect({ seq: 1, plain: true, source: "jev" });
  h.m.onKey(key("Tab"));
  h.m.onSuggest({ seq: h.sent[1].seq, ok: true, suggestion: { id: "s", options: [{ command: "ls", risk: "readonly" }] } });
  assert.equal(h.m.onKey(key("Escape")), true);
  assert.equal(h.m.state, "idle");
  h.m.onInput("x");
  h.fire();
  assert.equal(h.sent.filter((s) => s.kind === "detect").length, 1, "dismissed words are not sent again");
});

test("a follower gets nothing (§6.6)", () => {
  const h = machine();
  h.setDriving(false);
  h.m.onInput("x");
  h.fire();
  assert.equal(h.sent.length, 0);
  assert.equal(h.m.onKey(key("Tab")), false);
  // Control passed away while a notice showed: it goes away, ⇥ is the shell's.
  h.setDriving(true);
  h.m.onInput("x");
  h.fire();
  h.m.onDetect({ seq: 1, plain: true, source: "jev" });
  assert.equal(h.m.state, "notice");
  h.setDriving(false);
  assert.equal(h.m.onKey(key("Tab")), false);
  assert.equal(h.m.state, "idle");
});

test("alt screen, a stale answer, Enter and an off pane", () => {
  const h = machine();
  h.setLine(null); // full-screen app or no prompt
  h.m.onInput("x");
  h.fire();
  assert.equal(h.sent.length, 0);
  h.setLine("show the files here");
  h.m.onInput("x");
  h.fire();
  h.setLine("show the files here and more");
  h.m.onDetect({ seq: 1, plain: true, source: "jev" });
  assert.equal(h.m.state, "idle", "words changed since the question");
  h.m.onInput("x");
  h.fire();
  h.m.onDetect({ seq: 2, plain: false, source: "off" });
  h.m.onInput("y");
  h.setLine("other words here");
  h.fire();
  assert.equal(h.sent.length, 2, "backs off after off");
  h.tick(S.OFF_BACKOFF_MS + 1);
  h.m.onInput("y");
  h.fire();
  assert.equal(h.sent.length, 3);
  h.m.onDetect({ seq: 3, plain: true, source: "jev" });
  assert.equal(h.m.state, "notice");
  h.m.onInput("\r");
  assert.equal(h.m.state, "idle", "Enter runs the words; the notice goes");
});

test("cached answers are not asked again; fallback answers are not cached", () => {
  const h = machine();
  h.m.onInput("x");
  h.fire();
  h.m.onDetect({ seq: 1, plain: false, source: "fallback" });
  h.m.onInput("x");
  h.fire();
  assert.equal(h.sent.length, 2, "fallback is asked again");
  h.m.onDetect({ seq: 2, plain: true, source: "jev" });
  h.m.onInput("\r");
  h.m.onInput("x");
  h.fire();
  assert.equal(h.sent.length, 2, "jev answer cached");
  assert.equal(h.m.state, "notice");
});

test("an insert refused by keel serve keeps the card with the reason", async () => {
  const h = machine({ insert: async () => ({ ok: false, message: "the insert expired; press ⇥ again" }) });
  h.m.onInput("x");
  h.fire();
  h.m.onDetect({ seq: 1, plain: true, source: "jev" });
  h.m.onKey(key("Tab"));
  h.m.onSuggest({ seq: h.sent[1].seq, ok: true, suggestion: { id: "s", options: [{ command: "ls", risk: "readonly" }] } });
  h.m.onKey(key("Tab"));
  await new Promise((r) => setImmediate(r));
  assert.equal(h.m.state, "card");
  assert.match(h.m.view().note, /expired/);
});

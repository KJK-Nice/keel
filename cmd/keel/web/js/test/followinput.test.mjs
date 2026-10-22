import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// Pins the follower input path in term.js. Phase 4 of the redesign (Share
// and Join) is UI only: a follower's keystrokes must stay blocked exactly as
// before, and the "Typing is off while you follow" toast is feedback, not a
// new gate. term.js is one browser IIFE with no exports, so these tests read
// its source: a change to any pinned line fails here and has to be made on
// purpose. The server enforces the same rule (runtime Session.Write returns
// ErrReadOnly for anyone but the driver); this pins the client side.

const src = readFileSync(new URL("../term.js", import.meta.url), "utf8");
const sharePopSrc = readFileSync(new URL("../ui/sharepop.js", import.meta.url), "utf8");
const shareControlSrc = readFileSync(new URL("../ui/sharecontrol.js", import.meta.url), "utf8");

// body returns the source of `function name(...) { ... }` (brace matched)
// in src, or in another module's source.
function body(name, source = src) {
  const start = source.indexOf(`function ${name}(`);
  assert.notEqual(start, -1, `function ${name} not found`);
  let i = source.indexOf("{", start);
  let depth = 0;
  for (let j = i; j < source.length; j++) {
    if (source[j] === "{") depth++;
    else if (source[j] === "}" && --depth === 0) return source.slice(start, j + 1);
  }
  throw new Error(`unbalanced ${name}`);
}

const squash = (s) => s.replace(/\s+/g, " ").trim();

test("isDriving is still the driver role and nothing else", () => {
  assert.equal(squash(body("isDriving")), 'function isDriving(pane) { return pane?.you?.role === "driver"; }');
});

// It only ever blocks more: §6 rule 13 adds the hold while the key that
// pressed a direct Type is still down.
test("ptyWritesBlocked is unchanged", () => {
  assert.equal(
    squash(body("ptyWritesBlocked")),
    "function ptyWritesBlocked() { return !!(sidebarFocused() || state.openModal || ptyInjectWaiter || Date.now() < ptyChromeHoldUntil || (reviewHold && heldKeys.size) || isNotebookPane(focusedPane()) || termInputOpen()); }",
  );
});

test("keystrokes reach a PTY only through ptyWritesBlocked and isDriving", () => {
  const wire = squash(body("wirePaneTerminal"));
  assert.ok(
    wire.includes(
      "term.attachCustomKeyEventHandler(termKeyHandler); term.onData((data) => { if (ptyWritesBlocked()) return; if (!isDriving(pane) || !isTermPane(pane)) return; const bytes = new TextEncoder().encode(data);",
    ),
    "the onData gate changed",
  );
  assert.ok(wire.includes("if (isDriving(p) && isTermPane(p)) sendSessionBytes(p, bytes);"), "sync input no longer checks isDriving per pane");
  // Only wirePaneTerminal, the approved inject path and the notebook's
  // confirmed CLI input send bytes; the share and follow code never does.
  for (const fn of ["syncFollowBanner", "syncJoinBanner"]) {
    assert.doesNotMatch(body(fn), /sendSessionBytes|\.send\(|term\.write|paste\(/, `${fn} must not write to a session`);
  }
  // The control-request and typing toasts live in ui/sharecontrol.js: they
  // send control messages only, and term.js hands them nothing else.
  for (const fn of ["wireFollowerNotice", "noteControl"]) {
    assert.doesNotMatch(body(fn, shareControlSrc), /sendSessionBytes|\.send\(|term\.write|paste\(/, `${fn} must not write to a session`);
  }
  assert.doesNotMatch(shareControlSrc, /sendSessionBytes|\.send\(|term\.write|paste\(/, "ui/sharecontrol.js must not write to a session");
  const control = src.slice(src.indexOf("createShareControl({"), src.indexOf("});", src.indexOf("createShareControl({")));
  assert.match(control, /^createShareControl\(\{ sendSessionJSON, toast, isDriving, isOwner, isTermPane $/, "createShareControl gets control messages only");
  // The Share popover lives in ui/sharepop.js: nothing in it writes to a
  // session, and term.js hands it no way to.
  assert.doesNotMatch(sharePopSrc, /sendSession|\.send\(|term\.write|paste\(/, "ui/sharepop.js must not write to a session");
  const wiring = src.slice(src.indexOf("createSharePopover({"), src.indexOf("});", src.indexOf("createSharePopover({")));
  assert.doesNotMatch(wiring, /sendSession|\.send\(|term\.write|paste\(/, "the Share popover must get no way to write to a session");
});

test("a follower's xterm stays read-only", () => {
  const rc = squash(body("renderControl"));
  assert.ok(rc.includes("if (pane.term) pane.term.options.disableStdin = !isDriving(pane);"), "disableStdin no longer follows the role");
  assert.ok(squash(body("sendResize")).startsWith("function sendResize(pane) { if (!pane || !isDriving(pane) || !pane.term) return;"));
});

test("the typing toast only listens: it neither cancels nor sends keys", () => {
  const notice = body("wireFollowerNotice", shareControlSrc);
  assert.doesNotMatch(notice, /preventDefault|stopPropagation|stopImmediatePropagation|return false/);
  // Its one action asks for control the way the old buttons did.
  const sent = [...notice.matchAll(/sendSessionJSON\(pane, \{ type: "([a-z-]+)" \}\)/g)].map((m) => m[1]);
  assert.deepEqual(sent.sort(), ["request-control", "take-control"]);
});

test("Hand over sends the same grant-control as Pass control", () => {
  assert.match(body("noteControl", shareControlSrc), /sendSessionJSON\(pane, \{ type: "grant-control", targetId: msg\.fromId \}\)/);
  assert.match(src, /if \(grant\) sendControl\("grant-control", \{ targetId: Number\(grant\.dataset\.grant\) \}\);/);
});

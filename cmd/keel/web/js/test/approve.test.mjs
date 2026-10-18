import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

// Approve before type (docs/design/README.md principle 4, docs/ui-redesign.md
// §5.2 and §6): nothing from outside the keyboard may reach a PTY without
// the user approving the exact bytes. The server enforces it too (a nonce
// from inject-challenge, internal/inject); these tests pin the client side,
// so a new path that types a snippet without the gate fails here. They read
// term.js as text because it is one closure with no seams to call.
const dir = dirname(fileURLToPath(import.meta.url));
const src = readFileSync(join(dir, "..", "term.js"), "utf8");

// Top-level functions of the term.js closure (two-space indent), name -> body.
function functions() {
  const out = new Map();
  const re = /^ {2}(?:async )?function (\w+)\(/gm;
  const starts = [];
  for (let m; (m = re.exec(src)); ) starts.push([m[1], m.index]);
  starts.forEach(([name, at], i) => out.set(name, src.slice(at, i + 1 < starts.length ? starts[i + 1][1] : src.length)));
  return out;
}
const fns = functions();
const calls = (name, text = src) => [...text.matchAll(new RegExp(`(?<![\\w.])${name}\\(`, "g"))].map((m) => m.index);

test("every caller of sendApprovedInject awaits approveBeforeType first and aborts on no", () => {
  // insertSuggestion is the one exception, pinned in its own test below.
  const callers = [...fns].filter(([name, body]) => name !== "sendApprovedInject" && name !== "insertSuggestion" && calls("sendApprovedInject", body).length);
  assert.ok(callers.length >= 2, "expected the palette snippet and the terminal input to inject");
  for (const [name, body] of callers) {
    const send = calls("sendApprovedInject", body)[0];
    const approve = body.indexOf("await approveBeforeType(");
    assert.ok(approve >= 0 && approve < send, `${name} must await approveBeforeType before sendApprovedInject`);
    const abort = body.indexOf("if (!ok) return");
    assert.ok(abort > approve && abort < send, `${name} must return when the approval is cancelled`);
  }
});

test("only sendApprovedInject sends an inject message, and only with a server nonce", () => {
  const holders = [...fns].filter(([, body]) => body.includes('type: "inject"'));
  assert.deepEqual(holders.map(([name]) => name), ["sendApprovedInject"]);
  const body = fns.get("sendApprovedInject");
  assert.match(body, /await requestInjectTicket\(/);
  assert.match(body, /type: "inject", command, nonce/);
  assert.ok(!src.includes("approved: true"), "the client must never claim approval itself");
});

// §6.1: the agent never writes to a PTY. Its proposals reach a pane only
// through the user's Type: the approve dialog, then sendApprovedInject with
// the agent marker, to that one pane (never Sync input's other panes).
test("agent proposals are typed only through approve-before-type, to one pane", () => {
  const typers = [...fns].filter(([name, body]) => name !== "sendApprovedInject" && /agent: \{/.test(body) && calls("sendApprovedInject", body).length);
  assert.deepEqual(typers.map(([name]) => name), ["typeAgentProposal"]);
  const body = fns.get("typeAgentProposal");
  assert.ok(body.indexOf("await approveBeforeType(proposal.bytes") < calls("sendApprovedInject", body)[0]);
  assert.match(body, /if \(!isDriving\(pane\)\) return/);
  assert.match(fns.get("sendApprovedInject"), /const single = !!\(opts\.agent \|\| opts\.suggestion\);/);
  assert.match(fns.get("sendApprovedInject"), /single \? \[pane\]/);
  // The panel, its thread views and the Agent page (§5.4.2) reach a pane
  // only through deps.typeProposal.
  for (const file of ["agentpanel.js", "agentthread.js", "agentpage.js"]) {
    const src = readFileSync(join(dir, "..", "ui", file), "utf8");
    for (const bad of ["sendSession", "inject", "/ws/", ".send(", "WebSocket", "new WebSocket("]) assert.ok(!src.includes(bad), `${file} must not use ${bad}`);
  }
});

// §5.5, §6.1, §6.5: an in-pane suggestion reaches a pane only through the
// user's ⇥ / ⇧⇥ on its card. The card shows the command in the pane it is
// for, so ⇥ is the approval and there is no dialog; in exchange the insert
// goes to that one pane, only for the driver, never with a newline (it is
// not run), and keel serve checks the suggestion, the bytes and the ⇧ rule
// (AuthorizeInsert) before the ticket writes.
test("suggestions are inserted only by the user's ⇥, to one pane, never run", () => {
  const inserters = [...fns].filter(([name, body]) => name !== "sendApprovedInject" && /suggestion: \{/.test(body) && calls("sendApprovedInject", body).length);
  assert.deepEqual(inserters.map(([name]) => name), ["insertSuggestion"]);
  const body = fns.get("insertSuggestion");
  const send = calls("sendApprovedInject", body)[0];
  assert.ok(body.indexOf("!canSuggestIn(pane)) return") < send, "insertSuggestion checks the driver first");
  assert.ok(body.indexOf("/[\\r\\n]/.test(opt.bytes)) return") < send, "insertSuggestion refuses a newline");
  // Re-checked right before the write: prompt line, alt screen, a blocking dialog.
  const recheck = body.indexOf("const line = readPromptLine(pane.term);");
  assert.ok(recheck > 0 && recheck < send, "insertSuggestion re-reads the prompt line before the write");
  const gate = body.slice(recheck, send);
  for (const want of ["ptyWritesBlocked()", 'buffer?.active?.type !== "normal"', "line.input !== words", "dropped: true"]) {
    assert.ok(gate.includes(want), `insertSuggestion must check ${want} before the write`);
  }
  assert.match(fns.get("canSuggestIn"), /isDriving\(pane\)/);
  assert.match(fns.get("canSuggestIn"), /pane\.remote/);
  // Only the suggestion machine's insert, which runs on ⇥ / ⇧⇥, calls it.
  assert.deepEqual(calls("insertSuggestion").length, 2, "one definition, one call (wirePaneSuggest)");
  assert.match(fns.get("wirePaneSuggest"), /insert: \(sug, index, shift, words\) => insertSuggestion\(pane, sug, index, shift, words\)/);
  const ui = readFileSync(join(dir, "..", "ui", "suggest.js"), "utf8");
  const lib = readFileSync(join(dir, "..", "lib", "suggest.js"), "utf8");
  for (const bad of ["sendSession", "inject", "/ws/", "WebSocket"]) {
    assert.ok(!ui.includes(bad), `ui/suggest.js must not use ${bad}`);
    assert.ok(!lib.includes(bad), `lib/suggest.js must not use ${bad}`);
  }
});

test("raw session bytes are sent only from the keyboard handler, which the chrome can block", () => {
  const holders = [...fns].filter(([name, body]) => name !== "sendSessionBytes" && calls("sendSessionBytes", body).length);
  assert.deepEqual(holders.map(([name]) => name), ["wirePaneTerminal"]);
  const body = fns.get("wirePaneTerminal");
  assert.ok(body.indexOf("if (ptyWritesBlocked()) return") < calls("sendSessionBytes", body)[0]);
  assert.match(fns.get("ptyWritesBlocked"), /ptyInjectWaiter/);
});

test("snippets reach a PTY only through paletteRunSnippet", () => {
  // The only reader of a snippet's command that writes is paletteRunSnippet.
  const writers = [...fns].filter(([name, body]) => /sn\.command/.test(body) && /approveBeforeType|sendApprovedInject/.test(body));
  assert.deepEqual(writers.map(([name]) => name), ["paletteRunSnippet"]);
  // Everything that runs a snippet calls it.
  for (const needle of [`closest("[data-snippet-run]")`, `closest("[data-run]")`]) {
    const at = src.indexOf(needle);
    assert.ok(at >= 0, needle);
    assert.ok(src.slice(at, at + 400).includes("paletteRunSnippet("), `${needle} must call paletteRunSnippet`);
  }
});

test("the palette's snippet rows say Approve first and ignore the split key", () => {
  const catalog = fns.get("paletteCatalog");
  const at = catalog.indexOf('group: "Snippets"');
  assert.ok(at > 0, "the catalog has a Snippets group");
  const row = catalog.slice(catalog.lastIndexOf("items.push({", at), catalog.indexOf("});", at));
  assert.match(row, /badge: "Approve first"/);
  // run takes no options, so ⌥↵ cannot change what a snippet row does.
  assert.match(row, /run: \(\) => paletteRunSnippet\(sn\.id\)/);
  assert.ok(!/inject|sendSession|approved/.test(catalog), "the catalog itself never writes");
});

test("paletteRunSnippet awaits approval, names the session, then injects with the line cleared", () => {
  const body = fns.get("paletteRunSnippet");
  const approve = body.indexOf("await approveBeforeType(command,");
  const send = body.indexOf("await sendApprovedInject(");
  assert.ok(approve > 0 && approve < send);
  assert.ok(body.indexOf("if (!ok) return") > approve && body.indexOf("if (!ok) return") < send);
  assert.match(body, /isDriving\(pane\)/);
});

test("approveBeforeType shows the dialog and settles only on a user action", () => {
  const body = fns.get("approveBeforeType");
  assert.match(body, /ptyInjectWaiter = resolve/);
  assert.match(body, /pty-inject-preview/);
  // settlePtyInject is the only caller that resolves the waiter.
  const settlers = [...fns].filter(([name, text]) => name !== "approveBeforeType" && /ptyInjectWaiter\)? ?\(|done\(!!ok\)/.test(text));
  assert.deepEqual(settlers.map(([name]) => name), ["settlePtyInject"]);
});

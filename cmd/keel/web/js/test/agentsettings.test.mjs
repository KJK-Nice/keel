import { test } from "node:test";
import assert from "node:assert/strict";
import * as S from "../lib/agentsettings.js";
import { askRow, paletteHTML } from "../lib/palette.js";

const gemini = {
  id: "gemini",
  name: "Google Gemini",
  kind: "model",
  keyLabel: "Gemini API key",
  keyHelpUrl: "https://aistudio.google.com/apikey",
  termsUrl: "https://ai.google.dev/gemini-api/terms",
  warning: "On the free tier <b>Google</b> may use it.",
  models: ["a", "b"],
};
const jev = { id: "jev", name: "TypeSafe Jev", kind: "detector", keyLabel: "Jev API key" };

test("provider card: not connected shows the warning, a password field and the links", () => {
  const html = S.providerCardHTML(gemini, false);
  assert.match(html, /Not connected/);
  assert.match(html, /type="password"/);
  assert.match(html, /data-agent-save="gemini"/);
  assert.match(html, /Get a key/);
  assert.match(html, /href="https:\/\/ai\.google\.dev\/gemini-api\/terms"/);
  // The warning text is escaped, not markup.
  assert.ok(!html.includes("<b>Google</b>"));
  assert.match(html, /&lt;b&gt;Google&lt;\/b&gt;/);
  assert.ok(!/data-agent-disconnect/.test(html));
});

test("provider card: connected shows Change key and Disconnect, and no key field until Change", () => {
  const html = S.providerCardHTML(gemini, true);
  assert.match(html, /Connected/);
  assert.match(html, /data-agent-change="gemini"/);
  assert.match(html, /data-agent-disconnect="gemini"/);
  assert.ok(!/type="password"/.test(html));
  const changing = S.providerCardHTML(gemini, true, { changing: true, error: "bad key" });
  assert.match(changing, /Save key/);
  assert.match(changing, /data-agent-cancel="gemini"/);
  assert.match(changing, /bad key/);
});

test("provider card: jev says what it is for and has no warning or links", () => {
  const html = S.providerCardHTML(jev, false);
  assert.match(html, /Plain-language detection/);
  assert.ok(!/k-agent-warning/.test(html));
  assert.ok(!/Get a key/.test(html));
});

test("links must be https", () => {
  assert.equal(S.safeURL("javascript:alert(1)"), "");
  assert.equal(S.safeURL("http://x.test"), "");
  assert.equal(S.safeURL("https://x.test/a"), "https://x.test/a");
  assert.ok(!S.providerCardHTML({ ...gemini, keyHelpUrl: "javascript:alert(1)" }, false).includes("javascript:"));
});

test("detection note: auto needs Jev, # lines never does", () => {
  const auto = { suggestions: "on", detection: "auto" };
  assert.match(S.detectionNote(auto, false), /Jev is not connected.*only lines that start with #/);
  assert.equal(S.detectionNote(auto, true), "");
  assert.match(S.detectionNote({ suggestions: "on", detection: "hash-only" }, false), /comment/);
  assert.equal(S.detectionNote({ suggestions: "off", detection: "auto" }, false), "");
});

test("history, lines and models", () => {
  assert.equal(S.historyValue(0), "0");
  assert.equal(S.historyValue(30), "30");
  assert.equal(S.historyValue(45), "30");
  assert.equal(S.historyValue(300), "365");
  assert.equal(S.clampLines(10), 50);
  assert.equal(S.clampLines(5000), 1000);
  assert.equal(S.clampLines("abc"), 200);
  assert.deepEqual(S.modelChoices(gemini, "z"), ["z", "a", "b"]);
  assert.deepEqual(S.modelChoices(gemini, "a"), ["a", "b"]);
});

test("models of every model provider, labelled with the provider, and the provider each picks", () => {
  const claude = { id: "anthropic", name: "Anthropic Claude", shortName: "Claude", kind: "model", models: ["c1", "c2"] };
  const items = S.modelItems([gemini, claude, jev], { provider: "gemini", model: "a" });
  assert.deepEqual(
    items.map((it) => [it.value, it.provider]),
    [["a", "gemini"], ["b", "gemini"], ["c1", "anthropic"], ["c2", "anthropic"]],
  );
  assert.equal(items[0].label, "a · Google Gemini");
  assert.equal(items[2].label, "c1 · Claude");
  // A custom model stays first, under the saved provider.
  const custom = S.modelItems([gemini, claude], { provider: "anthropic", model: "x" });
  assert.deepEqual(custom[0], { value: "x", label: "x · Claude", provider: "anthropic" });
  // One model provider: bare model names.
  assert.deepEqual(
    S.modelItems([gemini, jev], { provider: "gemini", model: "a" }).map((it) => it.label),
    ["a", "b"],
  );
});

test("palette: the ask row names the query, escapes it, and has no group heading", () => {
  const row = askRow("  <img src=x> ", "⌘↵", () => {});
  assert.equal(row.title, "Ask the agent about “<img src=x>”");
  assert.equal(askRow("", "⌘↵", () => {}).title, "Ask the agent");
  const html = paletteHTML([{ id: "a", group: "Hosts", title: "h", run() {} }, row], 1, "x");
  assert.ok(!html.includes("<img"));
  assert.ok(html.includes("k-palette-item--ask"));
  assert.equal((html.match(/k-menu-label/g) || []).length, 1);
});

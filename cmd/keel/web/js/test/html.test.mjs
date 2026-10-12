import { test } from "node:test";
import assert from "node:assert/strict";
import { escapeHtml } from "../lib/html.js";

test("escapeHtml escapes markup and both quote styles", () => {
  assert.equal(escapeHtml(`<a href="x" title='y'>&</a>`), "&lt;a href=&quot;x&quot; title=&#39;y&#39;&gt;&amp;&lt;/a&gt;");
});

test("escapeHtml stringifies null, undefined and numbers", () => {
  assert.equal(escapeHtml(null), "");
  assert.equal(escapeHtml(undefined), "");
  assert.equal(escapeHtml(42), "42");
});

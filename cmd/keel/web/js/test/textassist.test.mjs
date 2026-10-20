import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { noTextAssist } from "../lib/dom.js";

// The native window is a WKWebView, which capitalizes, autocorrects and
// spellchecks text fields. Every text-like input and textarea built in JS
// turns autocapitalize and autocorrect off, and spellcheck off except in
// prose (notes). internal/ui/textassist_test.go checks the templ side; the
// notebook body (notebook-editor.mjs) keeps spellcheck for the same reason.

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const sources = [
  "term.js",
  ...readdirSync(join(root, "ui")).filter((f) => f.endsWith(".js")).map((f) => `ui/${f}`),
  ...readdirSync(join(root, "lib")).filter((f) => f.endsWith(".js")).map((f) => `lib/${f}`),
];

const NON_TEXT = new Set(["hidden", "checkbox", "radio", "file", "range", "color", "submit", "button", "reset", "image", "number", "date", "time"]);
// Prose fields: spellcheck stays on (autocapitalize and autocorrect stay off).
const PROSE = ["data-note-text", "data-note-edit"];

// tagEnd finds the > that closes the tag opened at i, skipping quoted
// values and ${…} expressions.
function tagEnd(s, i) {
  let quote = "";
  let depth = 0;
  for (; i < s.length; i++) {
    const c = s[i];
    if (quote) {
      if (c === quote) quote = "";
    } else if (depth) {
      if (c === "{") depth++;
      else if (c === "}") depth--;
    } else if (c === '"' || c === "'") quote = c;
    else if (c === "{") depth++;
    else if (c === ">") return i;
  }
  return -1;
}

export function fieldsIn(src) {
  const out = [];
  const re = /<(input|textarea)\b/g;
  let m;
  while ((m = re.exec(src))) {
    const end = tagEnd(src, m.index + m[0].length);
    if (end < 0) continue;
    const tag = src.slice(m.index, end + 1);
    const type = /\btype="([^"]*)"/.exec(tag)?.[1] ?? "";
    if (m[1] === "input" && NON_TEXT.has(type.toLowerCase())) continue;
    out.push(tag);
  }
  return out;
}

export function problems(tag) {
  const out = [];
  if (!/\bautocapitalize="off"/.test(tag)) out.push('autocapitalize="off"');
  if (!/\bautocorrect="off"/.test(tag)) out.push('autocorrect="off"');
  const prose = PROSE.some((a) => tag.includes(a));
  if (!new RegExp(`\\bspellcheck="${prose ? "true" : "false"}"`).test(tag)) out.push(`spellcheck="${prose ? "true" : "false"}"`);
  return out;
}

test("every text field in a JS-built HTML string turns text assistance off", () => {
  let fields = 0;
  for (const file of sources) {
    const src = readFileSync(join(root, file), "utf8");
    for (const tag of fieldsIn(src)) {
      fields++;
      const missing = problems(tag);
      assert.deepEqual(missing, [], `${file}: ${tag.replace(/\s+/g, " ").slice(0, 140)} lacks ${missing.join(", ")}`);
    }
  }
  // A scan that finds a handful means the scan, not the markup, broke.
  assert.ok(fields >= 20, `scanned only ${fields} JS-built fields`);
});

test("the scan sees a field without the attributes", () => {
  const src = 'x = `<input class="a" value="${v > 1 ? "b" : "c"}" />` + `<input type="checkbox">` + `<textarea data-note-edit spellcheck="false">`';
  const tags = fieldsIn(src);
  assert.equal(tags.length, 2, "the checkbox is not a text field");
  assert.deepEqual(problems(tags[0]), ['autocapitalize="off"', 'autocorrect="off"', 'spellcheck="false"']);
  assert.deepEqual(problems(tags[1]), ['autocapitalize="off"', 'autocorrect="off"', 'spellcheck="true"']);
});

test("fields made with createElement go through noTextAssist", () => {
  for (const file of sources) {
    const src = readFileSync(join(root, file), "utf8");
    const re = /const (\w+) = document\.createElement\("(input|textarea)"\);/g;
    let m;
    while ((m = re.exec(src))) {
      const after = src.slice(m.index, m.index + 600);
      // The clipboard fallback is a hidden, read-only textarea nobody types in.
      if (after.includes('setAttribute("readonly"')) continue;
      assert.ok(after.includes(`noTextAssist(${m[1]})`), `${file}: ${m[0]} without noTextAssist(${m[1]})`);
    }
  }
});

test("noTextAssist sets the three attributes", () => {
  const attrs = {};
  const el = { setAttribute: (k, v) => (attrs[k] = v) };
  assert.equal(noTextAssist(el), el);
  assert.deepEqual(attrs, { autocapitalize: "off", autocorrect: "off", spellcheck: "false" });
  assert.equal(noTextAssist(null), null);
});

test("the notebook body keeps spellcheck but not autocapitalize or autocorrect", () => {
  const src = readFileSync(join(root, "notebook-editor.mjs"), "utf8");
  const attrs = /attributes: \{([\s\S]*?)\}/.exec(src)?.[1] || "";
  assert.match(attrs, /spellcheck: "true"/);
  assert.match(attrs, /autocapitalize: "off"/);
  assert.match(attrs, /autocorrect: "off"/);
  const bundle = readFileSync(join(root, "..", "vendor", "notebook", "editor.js"), "utf8");
  assert.ok(bundle.includes('spellcheck:"true",autocapitalize:"off",autocorrect:"off"'), "rebuild the bundle: make notebook-editor");
});

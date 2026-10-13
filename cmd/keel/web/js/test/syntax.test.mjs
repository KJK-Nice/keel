import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { readdirSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";

// Every JS file served under cmd/keel/web (app code and vendored bundles) must
// parse. A vendored bundle once shipped with an unbalanced brace, which made
// browsers drop the whole file and silently degraded the notebook editor.
const root = join(dirname(fileURLToPath(import.meta.url)), "..", "..");

function walk(dir) {
  const out = [];
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    const p = join(dir, e.name);
    if (e.isDirectory()) {
      if (e.name === "node_modules") continue;
      out.push(...walk(p));
    } else if (/\.m?js$/.test(e.name)) out.push(p);
  }
  return out;
}

const files = walk(root).sort();

test("web JS files are discovered", () => {
  assert.ok(files.some((f) => f.endsWith("vendor/notebook/editor.js")));
  assert.ok(files.some((f) => f.endsWith("js/term.js")));
});

for (const f of files) {
  test(`parses: ${relative(root, f)}`, () => {
    try {
      execFileSync(process.execPath, ["--check", f], { stdio: "pipe" });
    } catch (err) {
      const msg = String(err.stderr || err).split("\n").slice(0, 4).join("\n").slice(0, 600);
      assert.fail(`${relative(root, f)} does not parse:\n${msg}`);
    }
  });
}

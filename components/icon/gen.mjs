#!/usr/bin/env node
// Regenerates icon_data.go and icon_defs.go from @hugeicons/core-free-icons.
//
//   node components/icon/gen.mjs [path-to-package]
//
// Package path should contain dist/esm/*Icon.js (npm pack extract or node_modules).

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const pkgRoot = process.argv[2]
  ? path.resolve(process.argv[2])
  : path.resolve(here, "../../node_modules/@hugeicons/core-free-icons");
const esmDir = path.join(pkgRoot, "dist/esm");

const SKIP_ATTRS = new Set(["key"]);
const INHERIT_ATTRS = {
  stroke: "currentColor",
  strokeWidth: "1.5",
  strokeLinecap: "round",
  strokeLinejoin: "round",
};

// Existing templates call icon.Settings(); free pack only has Settings01+.
const ALIASES = [{ go: "Settings", kebab: "settings-01" }];

function kebab(name) {
  return name
    .replace(/([a-z0-9])([A-Z])/g, "$1-$2")
    .replace(/([A-Z]+)([A-Z][a-z])/g, "$1-$2")
    .replace(/([a-zA-Z])(\d)/g, "$1-$2")
    .toLowerCase();
}

function attrName(key) {
  return key.replace(/[A-Z]/g, (m) => "-" + m.toLowerCase());
}

function escapeAttr(value) {
  return String(value)
    .replace(/&/g, "&amp;")
    .replace(/"/g, "&quot;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
}

function innerSvg(elements) {
  const parts = [];
  for (const entry of elements) {
    if (!Array.isArray(entry) || entry.length < 2) continue;
    const [tag, attrs] = entry;
    const bits = [];
    for (const [k, v] of Object.entries(attrs || {})) {
      if (SKIP_ATTRS.has(k)) continue;
      if (k in INHERIT_ATTRS && String(v) === INHERIT_ATTRS[k]) continue;
      bits.push(`${attrName(k)}="${escapeAttr(v)}"`);
    }
    parts.push(bits.length ? `<${tag} ${bits.join(" ")} />` : `<${tag} />`);
  }
  return parts.join("");
}

function parseIconFile(filePath) {
  const src = fs.readFileSync(filePath, "utf8");
  const match = src.match(/=\s*(\[[\s\S]*?\]);\s*(?:export default|$)/);
  if (!match) {
    throw new Error(`could not parse ${filePath}`);
  }
  return Function(`"use strict"; return (${match[1]});`)();
}

function goRawString(s) {
  if (!s.includes("`") && !s.includes("\r")) {
    return "`" + s + "`";
  }
  return JSON.stringify(s);
}

function main() {
  if (!fs.existsSync(esmDir)) {
    console.error(`missing ${esmDir}`);
    process.exit(1);
  }
  const pkg = JSON.parse(fs.readFileSync(path.join(pkgRoot, "package.json"), "utf8"));
  const version = pkg.version;
  const files = fs
    .readdirSync(esmDir)
    .filter((f) => f.endsWith("Icon.js") && !f.endsWith(".min.js"))
    .sort((a, b) => a.localeCompare(b));

  const icons = [];
  const usedGo = new Set();
  for (const file of files) {
    const go = file.slice(0, -"Icon.js".length);
    if (!go || usedGo.has(go)) {
      console.warn("skip", file);
      continue;
    }
    if (!/^[A-Z][A-Za-z0-9]*$/.test(go) || go === "Icon" || go === "Props") {
      console.warn("skip invalid go name", go);
      continue;
    }
    const elements = parseIconFile(path.join(esmDir, file));
    const key = kebab(go);
    icons.push({ go, key, svg: innerSvg(elements) });
    usedGo.add(go);
  }

  const dataLines = [
    "package icon",
    "",
    "// This file is auto generated",
    `// Using Hugeicons @hugeicons/core-free-icons version ${version}`,
    "",
    `const HugeiconsVersion = "${version}"`,
    "",
    "var internalSvgData = map[string]string{",
  ];
  for (const icon of icons) {
    dataLines.push(`\t"${icon.key}": ${goRawString(icon.svg)},`);
  }
  dataLines.push("}");
  dataLines.push("");

  const defLines = [
    "package icon",
    "",
    "// This file is auto generated",
    `// Using Hugeicons @hugeicons/core-free-icons version ${version}`,
    "",
  ];
  for (const icon of icons) {
    defLines.push(`var ${icon.go} = Icon("${icon.key}")`);
  }
  for (const alias of ALIASES) {
    if (usedGo.has(alias.go)) continue;
    defLines.push(`var ${alias.go} = Icon("${alias.kebab}")`);
  }
  defLines.push("");

  fs.writeFileSync(path.join(here, "icon_data.go"), dataLines.join("\n"));
  fs.writeFileSync(path.join(here, "icon_defs.go"), defLines.join("\n"));
  console.log(`wrote ${icons.length} icons (v${version})`);
}

main();

#!/bin/sh
# Rebuild cmd/keel/web/vendor/notebook/editor.js from notebook-editor.mjs.
# Installs pinned dependencies in a temp dir, so the result does not drift
# with whatever is newest on npm. Never hand-edit the bundle; edit the .mjs,
# run `make notebook-editor`, and commit both.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
out="$here/../vendor/notebook/editor.js"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

cp "$here/notebook-editor.mjs" "$tmp/entry.mjs"
cd "$tmp"
npm init -y >/dev/null
# Direct deps are the newest releases before the editor first shipped
# (2026-09-06); the rest are the transitive packages that end up in the bundle.
npm install --silent --save-exact \
  esbuild@0.28.2 \
  prosemirror-state@1.4.4 \
  prosemirror-view@1.42.3 \
  prosemirror-markdown@1.13.7 \
  prosemirror-keymap@1.2.3 \
  prosemirror-commands@1.7.2 \
  prosemirror-history@1.5.0 \
  prosemirror-dropcursor@1.8.3 \
  prosemirror-gapcursor@1.4.1 \
  prosemirror-inputrules@1.5.1 \
  prosemirror-model@1.25.11 \
  prosemirror-transform@1.12.1 \
  markdown-it@14.3.2 \
  linkify-it@5.0.2 \
  mdurl@2.1.0 \
  entities@4.5.0 \
  uc.micro@2.1.0 \
  orderedmap@2.1.1 \
  rope-sequence@1.3.4 \
  w3c-keyname@2.2.8
npx esbuild entry.mjs --bundle --format=iife --global-name=KeelMarkdownEditor \
  --outfile="$out" --minify
node --check "$out"
echo "wrote $out"

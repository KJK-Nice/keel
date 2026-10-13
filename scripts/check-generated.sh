#!/bin/sh
# check-generated.sh: run after `make ui`. Fails when the committed templ
# output, tokens.css (from docs/design/tokens.json) or output.css differs
# from what the pinned tools just generated.
# output.css is minified to one line, so its diff is shown one CSS rule
# per line to stay readable.
set -u
css=cmd/keel/web/css/output.css
changed=$(git diff --name-only -- '*_templ.go' "$css" docs/design/tokens.css cmd/keel/web/css/tokens.css)
[ -z "$changed" ] && exit 0

echo "generated files are stale: run make ui and commit the result" >&2
tmp=$(mktemp -d)
for f in $changed; do
	if [ "$f" = "$css" ]; then
		git show "HEAD:$f" | tr '}' '\n' >"$tmp/committed.css"
		tr '}' '\n' <"$f" >"$tmp/generated.css"
		echo "--- $f (committed) vs generated, one rule per line ---"
		diff -U0 "$tmp/committed.css" "$tmp/generated.css" | head -n 150
	else
		git diff -- "$f" | head -n 150
	fi
done
rm -rf "$tmp"
exit 1

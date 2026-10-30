#!/bin/sh
# Writes THIRD_PARTY_NOTICES for a release archive: the license of Go and
# every Go module linked into keel and keel-signal (macOS and Linux builds),
# of every JS package embedded in the web UI (versions read from the vendored
# files and notebook-editor.build.sh), and the fonts' OFL. Needs the network
# (go mod download, npm pack, the Datastar tag on GitHub). Fails rather than
# writing a notice without its license text.
#
#   sh scripts/notices.sh THIRD_PARTY_NOTICES
set -eu

out=${1:?usage: scripts/notices.sh OUTPUT}
root=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
cd "$root"

section() {
	printf '\n================================================================================\n%s\n================================================================================\n\n' "$1" >>"$tmp/notices"
}

# license_files DIR: the license and notice files at the top of DIR.
license_files() {
	find "$1" -maxdepth 1 -type f \( -iname 'licen[cs]e*' -o -iname 'copying*' -o -iname 'notice*' \) | sort
}

printf 'Keel includes the third-party software below, under the licenses that follow.\n' >"$tmp/notices"

section "Go (the standard library and runtime)"
cat "$(go env GOROOT)/LICENSE" >>"$tmp/notices"

# Go modules linked into the shipped binaries, for each OS the release builds.
go mod download
tags=$(sh scripts/go-tags.sh | sed 's/^-tags=//')
for os in darwin linux; do
	GOOS=$os CGO_ENABLED=1 go list -deps -tags "$tags" \
		-f '{{with .Module}}{{if not .Main}}{{.Path}}@{{.Version}}{{"\t"}}{{.Dir}}{{end}}{{end}}' \
		./cmd/keel ./cmd/keel-signal
done | sort -u >"$tmp/modules"
while IFS="$(printf '\t')" read -r mod dir; do
	files=$(license_files "$dir")
	[ -n "$files" ] || { echo "notices: no license file in $mod ($dir)" >&2; exit 1; }
	section "$mod"
	for f in $files; do cat "$f" >>"$tmp/notices"; printf '\n' >>"$tmp/notices"; done
done <"$tmp/modules"

# JS packages: xterm and its addons (version in each file's banner), the
# notebook editor bundle (pins in its build script, esbuild is the bundler
# only), and Datastar (not on npm at this version: its tag on GitHub).
{
	for f in cmd/keel/web/vendor/xterm/*.js; do
		head -c 200 "$f" | sed -n 's#^/\*! \(@xterm/[a-z-]*@[0-9][0-9.]*\) \*/.*#\1#p'
	done
	grep -oE '[a-z0-9./@-]+@[0-9][0-9.]*' cmd/keel/web/js/notebook-editor.build.sh | grep -v '^esbuild@'
} | sort -u >"$tmp/npm"
mkdir "$tmp/pack"
while read -r pkg; do
	(cd "$tmp/pack" && rm -rf package ./*.tgz && npm pack --silent "$pkg" >/dev/null && tar -xzf ./*.tgz)
	files=$(license_files "$tmp/pack/package")
	[ -n "$files" ] || { echo "notices: no license file in npm $pkg" >&2; exit 1; }
	section "$pkg (npm)"
	for f in $files; do cat "$f" >>"$tmp/notices"; printf '\n' >>"$tmp/notices"; done
done <"$tmp/npm"

datastar=$(head -c 100 cmd/keel/web/js/datastar.js | sed -n 's#^// Datastar v\([0-9.]*\).*#\1#p' | head -1)
[ -n "$datastar" ] || { echo "notices: no version banner in datastar.js" >&2; exit 1; }
section "Datastar v$datastar (github.com/starfederation/datastar)"
curl -fsSL "https://raw.githubusercontent.com/starfederation/datastar/v$datastar/LICENSE.md" >>"$tmp/notices"

for f in cmd/keel/web/fonts/OFL-*.txt; do
	name=$(basename "$f" .txt | sed 's/^OFL-//')
	section "$name (font)"
	cat "$f" >>"$tmp/notices"
done

mv "$tmp/notices" "$out"
echo "notices: $(grep -c '^================' "$out" | awk '{print int($1/2)}') entries in $out"

#!/bin/sh
# Writes THIRD_PARTY_NOTICES for a release archive: the license of Go and
# every Go module linked into keel and keel-signal (macOS and Linux builds),
# libghostty-vt, and the fonts' OFL. Needs the network (go mod download, the
# Ghostty license on GitHub). Fails rather than writing a notice without its
# license text.
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
for os in darwin linux; do
	GOOS=$os CGO_ENABLED=0 go list -deps \
		-f '{{with .Module}}{{if not .Main}}{{.Path}}@{{.Version}}{{"\t"}}{{.Dir}}{{end}}{{end}}' \
		./cmd/keel ./cmd/keel-signal
done | sort -u >"$tmp/modules"
while IFS="$(printf '\t')" read -r mod dir; do
	files=$(license_files "$dir")
	[ -n "$files" ] || { echo "notices: no license file in $mod ($dir)" >&2; exit 1; }
	section "$mod"
	for f in $files; do cat "$f" >>"$tmp/notices"; printf '\n' >>"$tmp/notices"; done
done <"$tmp/modules"

# libghostty-vt, the terminal engine of the native window (MyGo's terminal plugin downloads it, SHA-256 pinned, and
# Keel.app and the Linux archives carry it). Its source is a Ghostty commit named in the plugin's manifest; the
# license is read at that commit.
mygo=$(go list -m -f '{{.Dir}}' github.com/egoist/mygo)
ghostty=$(sed -n 's#.*"source": "https://github.com/ghostty-org/ghostty/tree/\([0-9a-f]*\)".*#\1#p' "$mygo/plugins/terminal/mygo-plugin.json" | head -1)
[ -n "$ghostty" ] || { echo "notices: no Ghostty commit in MyGo's terminal manifest" >&2; exit 1; }
section "libghostty-vt (github.com/ghostty-org/ghostty, $ghostty)"
curl -fsSL "https://raw.githubusercontent.com/ghostty-org/ghostty/$ghostty/LICENSE" >>"$tmp/notices"

# The fonts of the window are internal/native/fonts.
for f in internal/native/fonts/OFL-*.txt; do
	name=$(basename "$f" .txt | sed 's/^OFL-//')
	section "$name (font)"
	cat "$f" >>"$tmp/notices"
done

mv "$tmp/notices" "$out"
echo "notices: $(grep -c '^================' "$out" | awk '{print int($1/2)}') entries in $out"

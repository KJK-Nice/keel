#!/bin/sh
# Keel perf check: store save/lock contention and scrollback ring benchmarks.
# Writes everything to perf-results.txt in the repo root (gitignored) and
# exits non-zero if any step fails.
set -u
cd "$(dirname "$0")/.."
out=perf-results.txt
prof=$(mktemp -d)
trap 'rm -rf "$prof"' EXIT
# Each step appends to $prof/failed on error: the pipe through tee would
# otherwise hide its exit status.
step() {
	"$@" 2>&1 || echo "$*" >>"$prof/failed"
}
{
	echo "# keel perf check $(date '+%Y-%m-%d %H:%M:%S')"
	echo "# $(go version) | $(uname -sm) | commit $(git rev-parse --short HEAD)"
	echo
	echo "## store (fsync + global lock)"
	step sh scripts/go.sh test ./internal/store -run '^$' \
		-bench 'NotebookAutosave|ReadIdle|ReadDuringSaves' \
		-benchmem -count 3 -benchtime 2s \
		-cpuprofile "$prof/store.cpu" -o "$prof/store.test"
	echo
	echo "## store CPU profile (top 20)"
	step go tool pprof -top -nodecount 20 "$prof/store.test" "$prof/store.cpu" | sed -n '1,30p'
	echo
	echo "## scrollback ring"
	step sh scripts/go.sh test ./internal/runtime -run '^$' -bench RingWrite -benchmem -count 3
} | tee "$out"
echo
echo "Saved to $out"
if [ -s "$prof/failed" ]; then
	echo "Failed:" >&2
	cat "$prof/failed" >&2
	exit 1
fi

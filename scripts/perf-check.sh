#!/bin/sh
# Keel perf check: store save/lock contention and scrollback ring benchmarks.
# Writes everything to perf-results.txt in the repo root.
set -u
cd "$(dirname "$0")/.."
out=perf-results.txt
prof=$(mktemp -d)
{
	echo "# keel perf check $(date '+%Y-%m-%d %H:%M:%S')"
	echo "# $(go version) | $(uname -sm) | commit $(git rev-parse --short HEAD)"
	echo
	echo "## store (fsync + global lock)"
	sh scripts/go.sh test ./internal/store -run '^$' \
		-bench 'NotebookAutosave|ReadIdle|ReadDuringSaves' \
		-benchmem -count 3 -benchtime 2s \
		-cpuprofile "$prof/store.cpu" -o "$prof/store.test" 2>&1
	echo
	echo "## store CPU profile (top 20)"
	go tool pprof -top -nodecount 20 "$prof/store.test" "$prof/store.cpu" 2>&1 | sed -n '1,30p'
	echo
	echo "## scrollback ring"
	sh scripts/go.sh test ./internal/runtime -run '^$' -bench RingWrite -benchmem -count 3 2>&1
} | tee "$out"
rm -rf "$prof"
echo
echo "Saved to $out"

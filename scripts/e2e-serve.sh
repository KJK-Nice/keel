#!/bin/sh
# e2e only (e2e.config.ts app.command): bring up a loopback keel-signal on a
# free port so Share and Join work in tests, then run `bin/keel serve` with
# KEEL_SIGNAL pointing at it. Both are stopped by PID when the runner stops
# this script; nothing here looks processes up by name, and it never touches
# the user's Keel on 127.0.0.1:47241.
set -eu
cd "$(dirname "$0")/.."

port=$(node -e 'const s=require("net").createServer().listen(0,"127.0.0.1",()=>{console.log(s.address().port);s.close()})')
mkdir -p .e2e/logs
KEEL_SIGNAL_ADDR="127.0.0.1:$port" ./bin/keel-signal >>.e2e/logs/keel-signal.log 2>&1 &
sig=$!

# Wait until it answers.
i=0
until node -e 'fetch(process.argv[1]).then((r)=>process.exit(r.ok?0:1),()=>process.exit(1))' "http://127.0.0.1:$port/healthz" 2>/dev/null; do
	i=$((i + 1))
	if [ "$i" -gt 50 ] || ! kill -0 "$sig" 2>/dev/null; then
		echo "keel-signal did not start; see .e2e/logs/keel-signal.log" >&2
		kill "$sig" 2>/dev/null || true
		exit 1
	fi
	sleep 0.1
done

KEEL_SIGNAL="http://127.0.0.1:$port" ./bin/keel "$@" &
srv=$!
stop() {
	kill "$srv" 2>/dev/null || true
	wait "$srv" 2>/dev/null || true
	kill "$sig" 2>/dev/null || true
}
trap 'stop; exit 0' INT TERM
wait "$srv" || true
kill "$sig" 2>/dev/null || true

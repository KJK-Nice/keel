#!/bin/sh
# ci-run.sh TITLE CMD...
# Runs CMD. On failure, also posts the last 80 lines of its output as one
# GitHub Actions error annotation titled TITLE, so the failure shows on the
# PR's checks page without opening the job log.
set -u
title=$1
shift
out=$(mktemp)
"$@" >"$out" 2>&1
status=$?
cat "$out"
if [ "$status" -ne 0 ]; then
	msg=$(tail -n 80 "$out" | sed -e 's/%/%25/g' -e 's/\r/%0D/g' | awk 'BEGIN { ORS = "%0A" } { print }')
	printf '::error title=%s::%s\n' "$title" "$msg"
fi
rm -f "$out"
exit "$status"

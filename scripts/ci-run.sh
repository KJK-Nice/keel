#!/bin/sh
# ci-run.sh TITLE CMD...
# Runs CMD. On failure, posts one GitHub Actions error annotation titled
# TITLE before echoing the output, so it lands within the per-step
# annotation limit ahead of problem-matcher hits. The annotation leads with
# the lines that name the failure (failed tests, panics, vulnerabilities),
# then the tail of the output.
set -u
title=$1
shift
out=$(mktemp)
"$@" >"$out" 2>&1
status=$?
if [ "$status" -ne 0 ]; then
	msg=$(
		{
			grep -E -e '--- FAIL|^FAIL|^panic:|_test\.go:[0-9]+:|^Vulnerability #|Found in:|Fixed in:|^[^ ]+\.go:[0-9]+:[0-9]+: ' "$out" | head -n 60
			echo '----- tail -----'
			tail -n 40 "$out"
		} | sed -e 's/%/%25/g' -e 's/\r/%0D/g' | awk 'BEGIN { ORS = "%0A" } { print }'
	)
	printf '::error title=%s::%s\n' "$title" "$msg"
fi
cat "$out"
rm -f "$out"
exit "$status"

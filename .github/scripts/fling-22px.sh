#!/bin/sh
# fling-22px.sh NAME [go test flags...]: TestFlingScrollNoGapsCompact's
# history case COUNT times (flake-check's fling-22px job), then a notice
# with the failed and passed runs and each failed run's log (the idle
# move's row diagnostics, scroll_gaps_test.go logMove). BURN=N keeps N
# busy loops running meanwhile.
set +e
name=$1; shift
log="$RUNNER_TEMP/fling-$name.log"
pids=
i=0
while [ "$i" -lt "${BURN:-0}" ]; do
	sh -c 'while :; do :; done' & pids="$pids $!"
	i=$((i + 1))
done
go test -v "$@" -count="$COUNT" -timeout 60m -run '^TestFlingScrollNoGapsCompact$/^history$' ./internal/ui/messages > "$log" 2>&1
rc=$?
[ -n "$pids" ] && kill $pids 2>/dev/null
fails=$(grep -c -- '--- FAIL: TestFlingScrollNoGapsCompact/history ' "$log")
passes=$(grep -c -- '--- PASS: TestFlingScrollNoGapsCompact/history ' "$log")
echo "::notice title=fling-22px ($name)::${fails} of ${COUNT} runs failed (${passes} passed, exit ${rc})"
awk '/^=== RUN   TestFlingScrollNoGapsCompact\/history$/ { n = 0 } { buf[n++] = $0 } /--- FAIL: TestFlingScrollNoGapsCompact\/history / { print "::group::failed run"; for (i = 0; i < n; i++) if (buf[i] !~ /users\/info: slack edge/) print buf[i]; print "::endgroup::"; n = 0 }' "$log" | head -4000
grep -nE -m3 -A25 '^(panic: |fatal error: |test timed out)' "$log" | head -80
tail -5 "$log"
# REPRO=1 (the diagnostics phase): reproducing the move is what the run is
# for, so it stays green when that is the only failure (failed runs ping
# Karim's phone); anything else (another test, a panic, a build) fails it
if [ "${REPRO:-0}" = 1 ] && [ "$rc" != 0 ] && [ "$fails" -gt 0 ]; then
	others=$(grep -E '^--- FAIL: ' "$log" | grep -v -- '--- FAIL: TestFlingScrollNoGapsCompact ')
	if [ -z "$others" ] && ! grep -qE '^(panic: |fatal error: |test timed out)|\[build failed\]|\[setup failed\]' "$log"; then
		echo "::warning title=fling-22px ($name)::reproduced in ${fails} of ${COUNT} runs (the diagnostics are in the failed runs above)"
		exit 0
	fi
fi
exit $rc

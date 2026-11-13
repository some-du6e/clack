#!/bin/sh
# flake-check's counting step: go test -count=$COUNT -run "^$TEST$" $PKG
# with any extra flags given, then a notice with the failed and passed
# runs, each failed run's last 150 lines, and any race reports.
set +e
log="$RUNNER_TEMP/flake.log"
go test -v "$@" -count="$COUNT" -timeout 60m -run "^${TEST}\$" "$PKG" > "$log" 2>&1
rc=$?
fails=$(grep -cE "^--- FAIL: ${TEST} \(" "$log")
passes=$(grep -cE "^--- PASS: ${TEST} \(" "$log")
echo "::notice title=${TEST}::${fails} of ${COUNT} runs failed (${passes} passed, exit ${rc})"
awk -v run="=== RUN   ${TEST}" -v done="--- FAIL: ${TEST} (" -v ok="--- PASS: ${TEST} (" '
  $0 == run { n = 0; inrun = 1 }
  inrun { buf[n++ % 150] = $0 }
  index($0, done) == 1 { print "::group::failed run"; for (i = (n > 150 ? n - 150 : 0); i < n; i++) print buf[i % 150]; print "::endgroup::"; inrun = 0 }
  index($0, ok) == 1 { inrun = 0 }' "$log" | head -3000
awk '/^WARNING: DATA RACE/,/^==================$/' "$log" | head -400
# a run that crashed the whole binary (no --- FAIL line): print the crash's
# head, which the tail below (goroutine dumps) cuts off
grep -nE -m3 -A25 '^(panic: |fatal error: |Exception 0x|unexpected fault|test timed out|exit status )' "$log" | head -120
tail -"${FLAKE_TAIL:-5}" "$log"
exit $rc

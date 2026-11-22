#!/usr/bin/env bash
# fanout.sh BIN TEST PARENT SET...
#
# Runs one test's subtests in parallel processes of one test binary (built
# once with go test -c), from the package's directory:
#   - one process per SET, a |-separated list of TEST/PARENT's subtests;
#   - one more for everything else in TEST (-test.skip of every SET), so a
#     subtest added later runs too.
# Each process boots its own App (ui needs one per process). Flags for
# every process go in FANOUT_FLAGS (-test.timeout 20m); -test.v,
# -test.count=1 and -test.paniconexit0 are always passed, as go test does.
#
# Fails if a process fails or runs no test, if a SET names a subtest that
# didn't run, or if a subtest ran twice: together the processes run every
# subtest once. Each process's output streams to the log as it comes, its
# lines marked [i], so a job its budget stops keeps what was printed (a
# Go timeout's panic and goroutine dump too); a failed process's output is
# printed again at the end, whole.
#
#   cd internal/ui && fanout.sh "$RUNNER_TEMP/ui.test" TestIdleFrames motion 'views|unreads' 'pointer|code'
set -u
if [ $# -lt 4 ]; then
	echo "usage: fanout.sh BIN TEST PARENT SET..." >&2
	exit 2
fi
bin=$1 test=$2 parent=$3
shift 3
tmp=${RUNNER_TEMP:-${TMPDIR:-/tmp}}
tmp=${tmp//\\//} # D:\a\_temp on Windows: D:/a/_temp for Git Bash's mktemp
out=$(mktemp -d "$tmp/fanout.XXXXXX")
all=$(
	IFS='|'
	echo "$*"
)
# -test.run and -test.skip split their patterns at slashes: TEST, PARENT,
# then PARENT's subtests (a partial -test.skip match never skips a parent)
n=$(($# + 1))
runs=() skips=()
for set in "$@"; do
	runs+=("^$test\$/^$parent\$/^($set)\$")
	skips+=("")
done
runs+=("^$test\$")
skips+=("^$test\$/^$parent\$/^($all)\$")
pids=()
i=0
while [ "$i" -lt "$n" ]; do
	args=(-test.v -test.count=1 -test.paniconexit0 "-test.run=${runs[$i]}")
	[ -z "${skips[$i]}" ] || args+=("-test.skip=${skips[$i]}")
	# the output to its file and, marked, to the log as it comes (awk's
	# fflush: line by line on GNU, BSD and Git Bash); the exit code aside
	{
		# shellcheck disable=SC2086 # FANOUT_FLAGS splits on purpose
		"$bin" "${args[@]}" ${FANOUT_FLAGS:-} 2>&1
		echo "$?" >"$out/$i.rc"
	} | tee "$out/$i.log" | awk -v p="[$i] " '{ print p $0; fflush() }' &
	pids+=($!)
	echo "process $i: ${args[*]} ${FANOUT_FLAGS:-}"
	i=$((i + 1))
done
rc=0
i=0
while [ "$i" -lt "$n" ]; do
	wait "${pids[$i]}" # the stream's end: the process has exited
	st=$(cat "$out/$i.rc" 2>/dev/null || echo 1)
	log=$out/$i.log
	what="-test.run=${runs[$i]}${skips[$i]:+ -test.skip=${skips[$i]}}"
	if [ "$st" -eq 0 ] && grep -q '^testing: warning: no tests to run' "$log"; then
		echo "::error::fanout process $i ($what) ran no test"
		st=1
	fi
	if [ "$st" -eq 0 ]; then
		echo "process $i passed: $what"
	else
		echo "== process $i failed (exit $st): $what"
		cat "$log"
		echo "::error::fanout process $i ($what) failed (exit $st)"
		rc=1
	fi
	i=$((i + 1))
done
# each subtest ran once, and every one a SET names ran (TEST's children,
# PARENT among them, run in every process to reach their own: from the
# grandchildren down)
grep -h -o -E -- "--- (PASS|FAIL|SKIP): $test/[^ /]+/[^ ]+ " "$out"/*.log | awk '{print $3}' | sort >"$out/ran"
dups=$(uniq -d "$out/ran" | tr '\n' ' ')
if [ -n "$dups" ]; then
	echo "::error::fanout: subtests that ran more than once: $dups"
	rc=1
fi
for set in "$@"; do
	IFS='|' read -r -a names <<<"$set"
	for name in "${names[@]}"; do
		if ! grep -qx -- "$test/$parent/$name" "$out/ran"; then
			echo "::error::fanout: $test/$parent/$name didn't run (renamed or gone?)"
			rc=1
		fi
	done
done
echo "fanout: $(wc -l <"$out/ran" | tr -d ' ') subtest results of $test from $n processes"
exit $rc

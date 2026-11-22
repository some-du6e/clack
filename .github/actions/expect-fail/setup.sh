#!/usr/bin/env bash
# expect_fail's setup (action.yml): the evidence-branch rule, then the
# wrapper built and put in GOFLAGS for the job's later steps.
set -euo pipefail
ref=${GITHUB_REF_NAME:-}
if [ -z "${XF_TESTS//[[:space:],]/}" ]; then
	# -red, -red4, -red13c, -r3red5, -mut1, -r3mut1 (2026-11-21's proofs)
	if [[ "$ref" =~ -(r[0-9]+)?(red|mut)([0-9]+[a-z]?)?$ ]]; then
		echo "::error title=expect_fail::$ref is an evidence branch: its run must name the tests it proves fail, or it ends red and pings Karim's phone. Dispatch with -f expect_fail=TestA,TestB/sub (docs/CI-SPEED.md, \"Proof runs\")."
		exit 1
	fi
	exit 0
fi
if [ "${GITHUB_REF_TYPE:-}" = tag ] || [ "$ref" = ui ] || [ "$ref" = main ]; then
	echo "::error title=expect_fail::expect_fail is for proof branches, not $ref: ui, main and tags pass only when nothing fails."
	exit 1
fi
root=$(cd "$GITHUB_ACTION_PATH/../../.." && pwd)
dir="${RUNNER_TEMP//\\//}/expect-fail"
mkdir -p "$dir/results"
exe=
if [ "${RUNNER_OS:-}" = Windows ]; then
	exe=.exe
fi
(cd "$root" && go build -o "$dir/xf$exe" ./tools/expectfail)
bin="$dir/xf$exe"
win=$dir
if [ "${RUNNER_OS:-}" = Windows ]; then
	bin=$(cygpath -w "$bin")
	win=$(cygpath -w "$dir")
fi
list=$(EXPECT_FAIL="$XF_TESTS" "$bin" -validate)
{
	echo "EXPECT_FAIL=$list"
	echo "XF_DIR=$win"
	echo "XF_BIN=$bin"
	echo "XF_LEG=${XF_LEG_IN:-${GITHUB_JOB:-job}}"
	echo "XF_ID=expect-fail-a${GITHUB_RUN_ATTEMPT:-1}-${XF_LEG_IN:-${GITHUB_JOB:-job}}-${RUNNER_OS:-x}-$RANDOM$RANDOM"
	echo "GOFLAGS=${GOFLAGS:+$GOFLAGS }-exec=$bin"
} >>"$GITHUB_ENV"
echo "expect_fail: $list must fail and nothing else; go test runs its binaries through $bin"

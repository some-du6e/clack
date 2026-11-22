#!/usr/bin/env bash
# prior-full-test.sh: release.yml's test job asks whether ci's full-test
# already passed on a commit with the checkout's tree (docs/CI-SPEED.md,
# "The release's test job"). The job is ci's full-test step for step (the
# same go test ./... on the same runner image and a clean cache), so on the
# same tree it adds no coverage, only another roll of the suite's flakes
# (alpha.135 lost two attempts, 2026-11-21). The tree, not the sha: a ui
# commit squashes a branch whose pre-merge full-test (-f only=full-test)
# ran on the branch's commit, and ui's own full-test is often cancelled.
#
# Not a proof run's: with expect_fail its listed tests' failures exit 0
# and full-test ends green while a test fails on that tree (and a listed
# test that panics ends its package's binary early), so a run whose
# expect-fail job ran doesn't count. Any API error runs the suite.
#
# Writes passed=true|false to $GITHUB_OUTPUT, and on true the run it found
# to the job's summary. Needs GH_TOKEN with actions: read. PRIOR_TREE
# stands in for the checkout's tree (tools/expectfail's tests).
set -uo pipefail
no() {
	echo "$1: the release runs the suite"
	echo "passed=false" >>"$GITHUB_OUTPUT"
	exit 0
}
tree=${PRIOR_TREE:-$(git rev-parse 'HEAD^{tree}')} || no "no tree"
since=$(date -u -d '-3 days' +%Y-%m-%dT%H:%M:%SZ)
# ci's green runs that can have run full-test (dispatches, ui and main
# pushes: a branch's push run skips it), each asked of the server, newest
# first; one page of each reaches back far enough (a ui push a few times
# an hour, ~100 dispatches a day)
runs=
for q in "event=workflow_dispatch" "branch=ui&event=push" "branch=main&event=push"; do
	page=$(gh api "repos/$GITHUB_REPOSITORY/actions/workflows/ci.yml/runs?status=success&per_page=100&created=%3E$since&$q" \
		--jq '.workflow_runs[] | [.created_at, .id, .head_sha, .head_branch, .event] | @tsv') ||
		no "the runs list ($q) failed"
	runs+="$page"$'\n'
done
declare -A treeOf
found=
while IFS=$'\t' read -r _ id sha branch ev; do
	[ -n "$id" ] || continue
	t=${treeOf[$sha]:-}
	if [ -z "$t" ]; then
		t=$(gh api "repos/$GITHUB_REPOSITORY/git/commits/$sha" --jq .tree.sha) || no "commit $sha's tree failed"
		treeOf[$sha]=$t
	fi
	[ "$t" = "$tree" ] || continue
	ft=$(gh api "repos/$GITHUB_REPOSITORY/actions/runs/$id/jobs?per_page=100" \
		--jq '([.jobs[] | select(.name == "expect-fail" and .conclusion != "skipped")] | length) as $xf | ([.jobs[] | select(.name == "full-test")][0] // {}) | "\(if $xf > 0 then "proof" else .conclusion end) \(.html_url)"') ||
		no "run $id's jobs failed"
	case $ft in
	success\ *)
		found="$id $sha $branch $ev ${ft#success }"
		break
		;;
	proof\ *)
		echo "run $id ($ev on $branch, $sha) is a proof run (expect_fail): its green full-test doesn't count"
		;;
	esac
done < <(printf '%s' "$runs" | sort -r)
[ -n "$found" ] || no "no ci full-test passed on tree $tree in the last 3 days"
read -r id sha branch ev joburl <<<"$found"
msg="ci's full-test passed on this tree ($tree) in run $id ($ev on $branch, $sha): $joburl. The release's test job is that job step for step, so it doesn't run the suite again (docs/CI-SPEED.md, \"The release's test job\")."
echo "$msg"
echo "$msg" >>"$GITHUB_STEP_SUMMARY"
echo "passed=true" >>"$GITHUB_OUTPUT"

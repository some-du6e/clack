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
# Writes passed=true|false to $GITHUB_OUTPUT, and on true the run it found
# to the job's summary. Needs GH_TOKEN with actions: read.
set -euo pipefail
tree=$(git rev-parse 'HEAD^{tree}')
since=$(date -u -d '-3 days' +%Y-%m-%dT%H:%M:%SZ)
# ci's runs that succeeded, newest first (ui/main pushes, full-test dispatches)
runs=$(gh api "repos/$GITHUB_REPOSITORY/actions/workflows/ci.yml/runs?status=success&per_page=100&created=%3E$since" \
	--jq '.workflow_runs[] | [.id, .head_sha, .head_branch, .event] | @tsv')
declare -A treeOf
found=
while IFS=$'\t' read -r id sha branch ev; do
	[ -n "$id" ] || continue
	# full-test runs on ui and main pushes and on dispatches only: a branch's
	# push run skipped it (each look-up is an API call of the shared quota)
	case $ev:$branch in
	workflow_dispatch:* | push:ui | push:main) ;;
	*) continue ;;
	esac
	t=${treeOf[$sha]:-}
	if [ -z "$t" ]; then
		t=$(gh api "repos/$GITHUB_REPOSITORY/git/commits/$sha" --jq .tree.sha 2>/dev/null || echo none)
		treeOf[$sha]=$t
	fi
	[ "$t" = "$tree" ] || continue
	ft=$(gh api "repos/$GITHUB_REPOSITORY/actions/runs/$id/jobs?per_page=100" \
		--jq '[.jobs[] | select(.name == "full-test")][0] // {} | "\(.conclusion) \(.html_url)"')
	case $ft in
	success\ *)
		found="$id $sha $branch $ev ${ft#success }"
		break
		;;
	esac
done <<<"$runs"
if [ -z "$found" ]; then
	echo "no ci full-test passed on tree $tree in the last 3 days: the release runs the suite"
	echo "passed=false" >>"$GITHUB_OUTPUT"
	exit 0
fi
read -r id sha branch ev joburl <<<"$found"
msg="ci's full-test passed on this tree ($tree) in run $id ($ev on $branch, $sha): $joburl. The release's test job is that job step for step, so it doesn't run the suite again (docs/CI-SPEED.md, \"The release's test job\")."
echo "$msg"
echo "$msg" >>"$GITHUB_STEP_SUMMARY"
echo "passed=true" >>"$GITHUB_OUTPUT"

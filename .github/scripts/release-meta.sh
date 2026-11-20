#!/usr/bin/env bash
# release.yml's tag, version and publish decision, from the run's context.
# Every job runs this itself instead of waiting for a meta job: a 6-second
# job ahead of the others queued each release a second time (26-32 min on
# 2026-11-19, docs/CI-SPEED.md).
#
#   INPUT_TAG  the dispatch's tag input (empty on a tag push or a dry run)
#
# Writes tag, version, num, ref, publish and prerelease to $GITHUB_OUTPUT,
# and TAG, VERSION and NUM to $GITHUB_ENV for the job's later steps. The
# ref is what each job's checkout takes, written there as an expression
# (inputs.tag's refs/tags/ ref, else github.sha).
set -eu
if [ -n "${INPUT_TAG:-}" ]; then
	tag=$INPUT_TAG ref=refs/tags/$INPUT_TAG publish=true
elif [ "$GITHUB_REF_TYPE" = tag ]; then
	tag=$GITHUB_REF_NAME ref=$GITHUB_SHA publish=true
else
	tag="v0.0.0-dev+${GITHUB_SHA::7}" ref=$GITHUB_SHA publish=false
fi
version=${tag#v}
# numeric x.y.z for Info.plist, the Windows version resource and Inno Setup
num=$(printf '%s\n' "$version" | grep -oE '^[0-9]+\.[0-9]+\.[0-9]+' || true)
if [ "${tag#v}" = "$tag" ] || [ -z "$num" ]; then
	echo "::error::tag '$tag' must look like v1.2.3 or v1.2.3-something"
	exit 1
fi
case $tag in *-*) prerelease=true ;; *) prerelease=false ;; esac
{
	echo "tag=$tag"
	echo "version=$version"
	echo "num=$num"
	echo "ref=$ref"
	echo "publish=$publish"
	echo "prerelease=$prerelease"
} | tee -a "$GITHUB_OUTPUT"
{
	echo "TAG=$tag"
	echo "VERSION=$version"
	echo "NUM=$num"
} >>"$GITHUB_ENV"

#!/bin/sh
# apt-get for GitHub's runners, same arguments. On 2026-11-11 their
# lookups of *.ubuntu.com failed or hung for hours (no timeout: jobs sat
# 20+ min in "apt-get update"). An update is limited to 5 min and an
# install to 10 (both take about 10 s; ttf-mscorefonts-installer fetches
# from SourceForge as it installs); after a failure the Ubuntu sources move
# to the kernel.org mirror for one more try, so a hung mirror costs minutes,
# not the 640-1,280 s steps of 2026-11-19/20, and stays inside each job's
# budget (timeout-minutes, docs/CI-SPEED.md).
set -u
export DEBIAN_FRONTEND=noninteractive
opts='-o Acquire::Retries=2 -o Acquire::http::Timeout=30 -o Acquire::https::Timeout=30'
run() {
	t=300
	[ "$1" = update ] || t=600
	# shellcheck disable=SC2086 # opts splits on purpose
	timeout $t apt-get $opts "$@"
}
run "$@" && exit 0
echo "::warning::apt-get $1 failed; using mirrors.edge.kernel.org"
sed -i -E 's#https?://(azure\.archive|archive|security)\.ubuntu\.com/ubuntu/?#http://mirrors.edge.kernel.org/ubuntu/#g' \
	/etc/apt/sources.list.d/*.sources /etc/apt/sources.list 2>/dev/null
[ "$1" = update ] || run update -qq || true
run "$@"

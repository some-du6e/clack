#!/bin/sh
# apt-get for GitHub's runners, same arguments. On 2026-11-11 their
# lookups of *.ubuntu.com failed or hung for hours (no timeout: jobs sat
# 20+ min in "apt-get update"). Each try is time-limited; after two
# failures the Ubuntu sources move to the kernel.org mirror.
set -u
export DEBIAN_FRONTEND=noninteractive
opts='-o Acquire::Retries=2 -o Acquire::http::Timeout=30 -o Acquire::https::Timeout=30'
run() { timeout 900 apt-get $opts "$@"; }
for i in 1 2; do
	run "$@" && exit 0
	sleep 10
done
echo "::warning::apt-get $1 failed twice; using mirrors.edge.kernel.org"
sed -i -E 's#https?://(azure\.archive|archive|security)\.ubuntu\.com/ubuntu/?#http://mirrors.edge.kernel.org/ubuntu/#g' \
	/etc/apt/sources.list.d/*.sources /etc/apt/sources.list 2>/dev/null
[ "$1" = update ] || run update -qq || true
run "$@"

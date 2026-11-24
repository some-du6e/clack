#!/bin/sh
# apt-get for GitHub's runners, same arguments. On 2026-11-11 their
# lookups of *.ubuntu.com failed or hung for hours (no timeout: jobs sat
# 20+ min in "apt-get update"), so an update is limited to 5 min and an
# install to 10, and after a failure the Ubuntu mirrors move to kernel.org's
# for one more try. On 2026-11-24 the first try's
# install stood still for its whole 10 min, the fallback's start then too
# late for the job's budget: huddle's ffmpeg on train-238 (45306370027) and
# windows' gcc-mingw-w64-x86-64 on bk-data-viz-5 (45313713198), both
# printing nothing at all. So a try is also stopped once it gets nowhere:
# apt-get's status (APT::Status-Fd) gives the file it is fetching and the
# download's percent, then dpkg's steps, and the first try stops after 60 s
# without a file done or a point gained, 5 min without a dpkg step (a
# postinst may run long: ttf-mscorefonts-installer fetches from
# SourceForge). apt's own 30 s timeout misses a mirror that sends a byte
# now and then, which moves a small .deb's percent (0.0031 a byte for
# hello's 26 kB) and gets nowhere. A slow mirror that moves keeps its
# 10 min: green runs' "gio deps" took 14 s at the median and up to 5 min.
# After a stopped install dpkg finishes what it left before the next try.
# Each try says how long it took.
set -u
export DEBIAN_FRONTEND=noninteractive
opts='-o Acquire::Retries=2 -o Acquire::http::Timeout=30 -o Acquire::https::Timeout=30'
st=$(mktemp)
trap 'rm -f "$st"' EXIT

# got_on STEP PERCENT BEFORE: apt got on from BEFORE, its percent then at
# the same STEP: dpkg's percent moved, or a download's rose a whole point
# (or fell: more files queued)
got_on() {
	case $1 in
	pmstatus:*) [ "$2" != "$3" ] ;;
	*) awk -v p="$2" -v b="$3" 'BEGIN { exit !(p + 0 >= b + 1 || p + 0 < b + 0) }' ;;
	esac
}

# run TOTAL STALL ARGS...: apt-get ARGS, stopped after TOTAL s, or once it
# has got nowhere for STALL s (5 × STALL while dpkg works)
run() {
	total=$1 stall=$2
	shift 2
	: >"$st"
	t0=$(date +%s)
	# timeout puts apt-get in a process group of its own and passes a TERM
	# on to all of it: the download methods, dpkg
	# shellcheck disable=SC2086 # opts splits on purpose
	timeout -k 10 "$total" apt-get $opts -o APT::Status-Fd=3 "$@" 3>"$st" &
	pid=$!
	step='' at='' since=$t0
	while kill -0 "$pid" 2>/dev/null; do
		sleep 2
		now=$(date +%s)
		# the last status: dlstatus:FILE:PERCENT:... (the file it fetches,
		# from 1) or pmstatus:PACKAGE:PERCENT:... (dpkg)
		line=$(tail -n 1 "$st")
		s=$(printf '%s\n' "$line" | cut -d: -f1-2)
		p=$(printf '%s\n' "$line" | cut -d: -f3)
		if [ "$s" != "$step" ] || got_on "$s" "$p" "$at"; then
			step=$s at=$p since=$now
			continue
		fi
		limit=$stall
		case $s in pmstatus:*) limit=$((stall * 5)) ;; esac
		if [ $((now - since)) -ge "$limit" ]; then
			echo "::warning::apt-get $1: no progress for $limit s (${line:-no status yet}); stopping it"
			pgrep -af 'apt-get|dpkg|/usr/lib/apt/methods' | head -8
			kill -TERM "$pid" 2>/dev/null
			break
		fi
	done
	wait "$pid"
	rc=$?
	echo "apt.sh: apt-get $1: exit $rc after $(($(date +%s) - t0)) s"
	return $rc
}

total=600
[ "$1" = update ] && total=300
run "$total" 60 "$@" && exit 0
echo "::warning::apt-get $1 failed; using mirrors.edge.kernel.org"
# The runners' sources name no host: they read mirror+file:/etc/apt/
# apt-mirrors.txt (azure.archive.ubuntu.com, then archive and security,
# runner-images' configure-apt-sources.sh), which apt moves down only when
# a mirror errors, not when it stands still. Rewriting the sources alone
# moved nothing there (flake-check apt-stall 45351732813: every fallback
# went back to the hung mirror), so the list's hosts move too. ARM's
# ports.ubuntu.com stays: kernel.org has no ubuntu-ports.
sed -i -E 's#https?://(azure\.archive|archive|security|[a-z0-9-]+\.ec2\.archive)\.ubuntu\.com/ubuntu/?#http://mirrors.edge.kernel.org/ubuntu/#g' \
	/etc/apt/apt-mirrors.txt /etc/apt/sources.list.d/*.sources /etc/apt/sources.list 2>/dev/null
echo "apt.sh: the Ubuntu archive is now $(grep -hoE 'https?://(mirrors\.edge\.kernel\.org|[a-z0-9.-]*archive\.ubuntu\.com|security\.ubuntu\.com|ports\.ubuntu\.com)/ubuntu(-ports)?/?' /etc/apt/apt-mirrors.txt /etc/apt/sources.list.d/*.sources /etc/apt/sources.list 2>/dev/null | sort -u | tr '\n' ' ')"
if [ "$1" != update ]; then
	timeout 300 dpkg --configure -a || true
	run 300 120 update -qq || true
fi
run "$total" 120 "$@"

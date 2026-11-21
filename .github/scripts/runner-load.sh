#!/usr/bin/env bash
# runner-load.sh SECONDS: started in the background of a long test step;
# every SECONDS, one line on the runner's memory, swap and load and its
# three biggest processes, then at the end (when go test's step ends and
# kills it, or GitHub stops the job) the most memory any line saw. Two
# full-test jobs lost their runner on 2026-11-21 (train-126: "lost
# communication with the server" after 66 min; train-130 attempt 1: "the
# runner has received a shutdown signal" at 22 min), and nothing in their
# logs said whether the suite was starving it: this does. Linux.
set -u
peak=0
trap 'echo "runner-load: the most memory in use: $peak MiB of $(free -m | awk "/^Mem:/ {print \$2}") MiB"' EXIT
while :; do
	# used = total - available (what the kernel can't give back)
	line=$(free -m | awk '/^Mem:/ {u = $2 - $7; m = sprintf("mem %d/%d MiB", u, $2)} /^Swap:/ {s = sprintf("swap %d MiB", $3)} END {print u, m ", " s}')
	used=${line%% *}
	[ "$used" -gt "$peak" ] && peak=$used
	top=$(ps -eo rss=,comm= --sort=-rss | head -3 | awk '{printf "%s%s %d MiB", (NR > 1 ? ", " : ""), $2, $1 / 1024}')
	echo "runner-load: ${line#* }, load $(cut -d' ' -f1-3 /proc/loadavg); $top"
	sleep "$1"
done

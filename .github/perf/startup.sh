#!/usr/bin/env bash
# The startup timeline (internal/applog/startup.go) of the real binary on
# Xvfb, against a Hack Club-sized fakeslackd that answers like a network
# (LATENCY per API call, IMG_LATENCY per image) and whose account has a
# custom theme, so a start that paints Slack's default first shows it.
#
# Per side, in one config dir: a cold start (no caches), then warm starts.
# HEAD also runs "nocache" warm starts with CLACK_NO_BOOT_CACHE=1: the same
# binary booting without the boot snapshot, the way Clack started before
# it existed, so before/after compare with the same instrumentation.
# Each start runs RUN_SECS (past core's boot snapshot save, 20 s after
# boot) and is then killed.
#
# usage: startup.sh <fakeslackd> <outdir> <side>...
#   each side's binary is <outdir>/<side>/clack-perf; results go to
#   <outdir>/<side>/startup/ (startup.tsv: run, then the timeline line)
set -uo pipefail
fake=$1 out=$2; shift 2
sides=("$@")
LATENCY=${LATENCY:-150ms}
IMG_LATENCY=${IMG_LATENCY:-40ms}
RUN_SECS=${RUN_SECS:-30}
THEME='{"primary":{"palette":"jade"},"highlight1":{"palette":"jade"},"highlight2":{"palette":"aubergine"},"important":{"palette":"jade"},"brightness":6,"sidebarInverted":true,"useCustomHex":false}'
export GALLIUM_DRIVER=llvmpipe LIBGL_ALWAYS_SOFTWARE=1
credsdir=$(mktemp -d)
"$fake" -addr 127.0.0.1:8931 -creds "$credsdir/creds.json" -latency "$LATENCY" -img-latency "$IMG_LATENCY" -ia-theme "$THEME" \
  > "$out/fakeslackd-startup.log" 2>&1 &
fpid=$!
Xvfb :98 -screen 0 1440x900x24 > /dev/null 2>&1 &
xpid=$!
trap 'kill $fpid $xpid 2>/dev/null' EXIT
export DISPLAY=:98
for i in $(seq 1 100); do [ -s "$credsdir/creds.json" ] && [ -e /tmp/.X11-unix/X98 ] && break; sleep 0.1; done

declare -A home
for s in "${sides[@]}"; do
  mkdir -p "$out/$s/startup"
  : > "$out/$s/startup/startup.tsv"
  home[$s]=$(mktemp -d)
  cp "$credsdir/creds.json" "${home[$s]}/creds.json"
done
nchome=$(mktemp -d) # HEAD without the boot snapshot (its own caches)
cp "$credsdir/creds.json" "$nchome/creds.json"

one() { # side run home [env...]
  local s=$1 run=$2 h=$3; shift 3
  local bin="$out/$s/clack-perf" p="$out/$s/startup"
  [ -x "$bin" ] || return
  env "$@" CLACK_HOME="$h" CLACK_DEBUG=1 "$bin" > "$p/$run.log" 2>&1 &
  local apid=$!
  sleep "$RUN_SECS"
  kill $apid 2>/dev/null; wait $apid 2>/dev/null
  local line paint ready
  line=$(grep -o 'startup: timeline .*' "$p/$run.log" | head -1)
  paint=$(sed -n 's/.*ui: first frame painted \([0-9.]*m\?s\) after start.*/\1/p' "$p/$run.log" | head -1)
  ready=$(sed -n 's/.*ui: core ready \([0-9.]*m\?s\) after boot start.*/\1/p' "$p/$run.log" | head -1)
  printf '%s\t%s\t%s\t%s\n' "$run" "${paint:-?}" "${ready:-?}" "${line:-}" | tee -a "$p/startup.tsv" | sed "s/^/$s\t/"
}

# the cold start first (empty home), then warm ones, sides interleaved and
# alternating which goes first
n=0
for run in cold warm1 warm2 warm3; do
  order=("${sides[@]}")
  if [ $((n % 2)) = 1 ]; then order=(); for ((i=${#sides[@]}-1; i>=0; i--)); do order+=("${sides[$i]}"); done; fi
  for s in "${order[@]}"; do one "$s" "$run" "${home[$s]}"; done
  n=$((n + 1))
done
# HEAD's "before": the same binary with the boot snapshot off
for s in "${sides[@]}"; do
  [ "$s" = head ] || continue
  for run in cold warm1 warm2; do one head "nocache-$run" "$nchome" CLACK_NO_BOOT_CACHE=1; done
done
# what the warm starts read: every file 0600, every directory 0700
for s in "${sides[@]}"; do
  [ -d "${home[$s]}/cache" ] && find "${home[$s]}/cache" -maxdepth 3 -printf '%M %m %10s %P\n' 2>/dev/null | sort -k4 | head -40 > "$out/$s/startup/cache-files.txt"
done
exit 0

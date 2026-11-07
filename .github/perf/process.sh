#!/usr/bin/env bash
# Runs the real clack binary on Xvfb against fakeslackd: one cold start,
# then warm starts. Records first-paint and core-ready times from the
# CLACK_TIMING log, and RSS / Go heap after 20 s idle.
# Usage: process.sh <clack binary> <fakeslackd binary> <outdir>
set -uo pipefail
bin=$1 fake=$2 out=$3
mkdir -p "$out"
export CLACK_HOME=$(mktemp -d) GALLIUM_DRIVER=llvmpipe CLACK_TIMING=1 LIBGL_ALWAYS_SOFTWARE=1
"$fake" -addr 127.0.0.1:8930 -creds "$CLACK_HOME/creds.json" > "$out/fakeslackd.log" 2>&1 &
fpid=$!
Xvfb :99 -screen 0 1440x900x24 > /dev/null 2>&1 &
xpid=$!
export DISPLAY=:99
for i in $(seq 1 50); do [ -s "$CLACK_HOME/creds.json" ] && [ -e /tmp/.X11-unix/X99 ] && break; sleep 0.1; done
: > "$out/process.tsv"
for run in cold warm1 warm2 warm3; do
  extra=()
  [ "$run" = warm3 ] && extra=(CLACK_CPUPROFILE="$out/startup.cpu.pprof" CLACK_CPUPROFILE_SECS=8)
  env "${extra[@]}" CLACK_PPROF=127.0.0.1:6061 "$bin" > "$out/$run.log" 2>&1 &
  apid=$!
  sleep 20
  rss=$(awk '/^VmRSS/{print $2}' /proc/$apid/status)
  hwm=$(awk '/^VmHWM/{print $2}' /proc/$apid/status)
  anon=$(awk '/^RssAnon/{print $2}' /proc/$apid/status)
  heap=$(curl -s 127.0.0.1:6061/mem | awk '/^heap_alloc/{print $2}')
  if [ "$run" = warm3 ]; then
    curl -s -o "$out/idle.heap.pprof" "127.0.0.1:6061/heap?gc=1"
    awk '/^[0-9a-f]+-[0-9a-f]+ /{n=($6==""?"[anon]":$6)} /^Rss:/{r[n]+=$2} END{for(k in r) if(r[k]>1024) print r[k], k}' /proc/$apid/smaps | sort -rn > "$out/idle.smaps.txt"
  fi
  paint=$(sed -n 's/.*ui: first frame painted \([0-9.]*m\?s\) after start (\([0-9]*\) sidebar rows).*/\1 \2/p' "$out/$run.log" | head -1)
  ready=$(sed -n 's/.*ui: core ready \([0-9.]*m\?s\) after boot start.*/\1/p' "$out/$run.log" | head -1)
  boot=$(sed -n 's/.*boot: ready in \([0-9.]*m\?s\) (cache \([0-9.]*[mµ]\?s\).*/\1 \2/p' "$out/$run.log" | head -1)
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$run" "${paint:-? ?}" "${ready:-?}" "${boot:-? ?}" "$rss" "$hwm" "$anon" "$heap" | tee -a "$out/process.tsv"
  kill $apid; wait $apid 2>/dev/null
done
kill $fpid $xpid 2>/dev/null
exit 0

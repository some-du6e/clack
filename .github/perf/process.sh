#!/usr/bin/env bash
# Runs the real clack binary of each side on Xvfb against one fakeslackd
# (HEAD's): a cold start, then warm starts, then a long idle. Records
# first-paint and core-ready times from the CLACK_TIMING log, and RSS / Go
# heap after 20 s idle (and after 95 s on the last warm start).
#
# The sides are interleaved run by run, alternating which goes first, so a
# runner that slows down halfway hits both alike.
#
# usage: process.sh <fakeslackd> <outdir> <side>...
#   each side's binary is <outdir>/<side>/clack-perf; results go to
#   <outdir>/<side>/process/
set -uo pipefail
fake=$1 out=$2; shift 2
sides=("$@")
export GALLIUM_DRIVER=llvmpipe CLACK_TIMING=1 LIBGL_ALWAYS_SOFTWARE=1
credsdir=$(mktemp -d)
"$fake" -addr 127.0.0.1:8930 -creds "$credsdir/creds.json" > "$out/fakeslackd-process.log" 2>&1 &
fpid=$!
Xvfb :99 -screen 0 1440x900x24 > /dev/null 2>&1 &
xpid=$!
trap 'kill $fpid $xpid 2>/dev/null' EXIT
export DISPLAY=:99
for i in $(seq 1 100); do [ -s "$credsdir/creds.json" ] && [ -e /tmp/.X11-unix/X99 ] && break; sleep 0.1; done
declare -A home
for s in "${sides[@]}"; do
  mkdir -p "$out/$s/process"
  : > "$out/$s/process/process.tsv"
  home[$s]=$(mktemp -d) # its own config dir: the cold run starts with no caches
  cp "$credsdir/creds.json" "${home[$s]}/creds.json"
done

one() { # side run
  local s=$1 run=$2 bin="$out/$1/clack-perf" p="$out/$1/process"
  [ -x "$bin" ] || return
  local extra=()
  [ "$run" = warm3 ] && extra=(CLACK_CPUPROFILE="$p/startup.cpu.pprof" CLACK_CPUPROFILE_SECS=8)
  env "${extra[@]}" CLACK_HOME="${home[$s]}" CLACK_PPROF=127.0.0.1:6061 "$bin" > "$p/$run.log" 2>&1 &
  local apid=$!
  sleep 20
  local rss hwm anon heap paint ready boot
  rss=$(awk '/^VmRSS/{print $2}' /proc/$apid/status 2>/dev/null)
  hwm=$(awk '/^VmHWM/{print $2}' /proc/$apid/status 2>/dev/null)
  anon=$(awk '/^RssAnon/{print $2}' /proc/$apid/status 2>/dev/null)
  heap=$(curl -s 127.0.0.1:6061/mem | awk '/^heap_alloc/{print $2}')
  if [ "$run" = warm3 ]; then
    curl -s -o "$p/idle.heap.pprof" "127.0.0.1:6061/heap?gc=1"
    awk '/^[0-9a-f]+-[0-9a-f]+ /{n=($6==""?"[anon]":$6)} /^Rss:/{r[n]+=$2} END{for(k in r) if(r[k]>1024) print r[k], k}' /proc/$apid/smaps 2>/dev/null | sort -rn > "$p/idle.smaps.txt"
  fi
  paint=$(sed -n 's/.*ui: first frame painted \([0-9.]*m\?s\) after start (\([0-9]*\) sidebar rows).*/\1 \2/p' "$p/$run.log" | head -1)
  ready=$(sed -n 's/.*ui: core ready \([0-9.]*m\?s\) after boot start.*/\1/p' "$p/$run.log" | head -1)
  boot=$(sed -n 's/.*boot: ready in \([0-9.]*m\?s\) (cache \([0-9.]*[mµ]\?s\).*/\1 \2/p' "$p/$run.log" | head -1)
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$run" "${paint:-? ?}" "${ready:-?}" "${boot:-? ?}" "${rss:-0}" "${hwm:-0}" "${anon:-0}" "$heap" | tee -a "$p/process.tsv" | sed "s/^/$s\t/"
  if [ "$run" = warm3 ]; then # long idle: background sweeps (rows 30 s, images 60 s, trim) have run
    sleep 75
    local irss ianon iheap
    irss=$(awk '/^VmRSS/{print $2}' /proc/$apid/status 2>/dev/null)
    ianon=$(awk '/^RssAnon/{print $2}' /proc/$apid/status 2>/dev/null)
    iheap=$(curl -s 127.0.0.1:6061/mem | awk '/^heap_alloc/{print $2}')
    curl -s -o "$p/idle95.heap.pprof" "127.0.0.1:6061/heap?gc=1"
    awk '/^[0-9a-f]+-[0-9a-f]+ /{n=($6==""?"[anon]":$6)} /^Rss:/{r[n]+=$2} END{for(k in r) if(r[k]>1024) print r[k], k}' /proc/$apid/smaps 2>/dev/null | sort -rn > "$p/idle95.smaps.txt"
    printf 'idle95\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "${paint:-? ?}" "${ready:-?}" "${boot:-? ?}" "${irss:-0}" "$hwm" "${ianon:-0}" "$iheap" | tee -a "$p/process.tsv" | sed "s/^/$s\t/"
  fi
  kill $apid 2>/dev/null; wait $apid 2>/dev/null
}

# one throwaway start per side, in a home that's then dropped: the first
# process on a fresh runner pays for the page cache and Mesa's shader cache,
# which would land on whichever side's cold run went first. "cold" below
# means no Clack caches, not a cold machine.
for s in "${sides[@]}"; do
  [ -x "$out/$s/clack-perf" ] || continue
  prime=$(mktemp -d); cp "$credsdir/creds.json" "$prime/creds.json"
  CLACK_HOME="$prime" "$out/$s/clack-perf" > "$out/$s/process/prime.log" 2>&1 &
  ppid_=$!; sleep 12; kill $ppid_ 2>/dev/null; wait $ppid_ 2>/dev/null
done

n=0
for run in cold warm1 warm2 warm3; do
  order=("${sides[@]}")
  if [ $((n % 2)) = 1 ]; then order=(); for ((i=${#sides[@]}-1; i>=0; i--)); do order+=("${sides[$i]}"); done; fi
  for s in "${order[@]}"; do one "$s" "$run"; done
  n=$((n + 1))
done
exit 0

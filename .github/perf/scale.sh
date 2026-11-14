#!/usr/bin/env bash
# The memory workflow's Linux leg (perf.yml's scale-linux job): the real
# binary of each side on Xvfb + llvmpipe against fakeslackd -scale (the
# real account's counts: fakeslack.HackClubScale), driven through a
# session by the perfharness driver (internal/ui/perfdrive.go).
#
# Each side starts twice in its own config dir:
#   prime  first start ever: core fetches everything, and every directory
#          user is loaded (a long session's metadata cache); it saves the
#          cache and exits
#   warm   the start that's measured, from that cache: startup, the emoji
#          picker opened and scrolled through, long channels paged back
#          and scrolled, 40 more channels, then CLACK_PERF_IDLE alone
# The driver writes <stage>.mem.txt / .trimmed.txt / .heap.pprof per stage;
# this script adds <stage>.external.txt (/proc/<pid>/status read from
# outside as the stage lands).
#
# Like startup.sh, every home opens on #announcements (ui.json route) and,
# when <outdir>/library holds the published plugin Library (perf.yml
# downloads it), runs Karim-like plugins: Slick's ChatTabs and five Taut
# plugins, so the JS engine and its state mirror are in the numbers.
#
# usage: scale.sh <fakeslackd> <outdir> <side>...
#   each side's binary is <outdir>/<side>/clack-perf; results go to
#   <outdir>/<side>/scale/{prime,warm}/
set -uo pipefail
fake=$1 out=$2; shift 2
sides=("$@")
export GALLIUM_DRIVER=llvmpipe LIBGL_ALWAYS_SOFTWARE=1 CLACK_TIMING=1
work=$(mktemp -d)
"$fake" -scale -gifs 300 -addr 127.0.0.1:8931 -creds "$work/creds.json" -userids "$work/users.txt" > "$out/fakeslackd-scale.log" 2>&1 &
fpid=$!
Xvfb :98 -screen 0 1440x900x24 > /dev/null 2>&1 &
xpid=$!
trap 'kill $fpid $xpid 2>/dev/null' EXIT
export DISPLAY=:98
for i in $(seq 1 600); do [ -s "$work/creds.json" ] && [ -e /tmp/.X11-unix/X98 ] && break; sleep 0.2; done
[ -s "$work/creds.json" ] || { echo "::error::fakeslackd -scale wrote no credentials"; cat "$out/fakeslackd-scale.log"; exit 1; }
wc -l < "$work/users.txt" | sed 's/^/directory users: /'

lib="$out/library"
seed() { # home: ui.json and the plugins
  local h=$1 id cfg=""
  printf '{"route":"c/C0FAKE00000","last_channel":"C0FAKE00000"}\n' > "$h/ui.json"
  [ -s "$lib/index.json" ] || { echo "plugins: none" > "$out/scale-plugins.txt"; return 0; }
  mkdir -p "$h/taut/plugins" "$h/slick/plugins"
  local tauts=(HumanCount LastSeen WhoReacted NoTrack ShutUpSlackbot)
  for id in "${tauts[@]}"; do
    [ -f "$lib/taut-$id.js" ] || continue
    cp "$lib/taut-$id.js" "$h/taut/plugins/$id.js"
    cfg="$cfg${cfg:+,}\"$id\":{\"enabled\":true}"
  done
  printf '{"plugins":{%s}}\n' "$cfg" > "$h/taut/config.json"
  if [ -f "$lib/slick-ChatTabs.js" ]; then
    cp "$lib/slick-ChatTabs.js" "$h/slick/plugins/ChatTabs.js"
    [ -f "$lib/slick-ChatTabs.main.js" ] && cp "$lib/slick-ChatTabs.main.js" "$h/slick/plugins/ChatTabs.main.js"
    printf '{"enabled":true,"plugins":{"ChatTabs":{"enabled":true}}}\n' > "$h/slick/settings.json"
  fi
  echo "plugins: ChatTabs + ${tauts[*]}" > "$out/scale-plugins.txt"
}

# idle_seconds <go duration>: CLACK_PERF_IDLE (10m, 70m, 1h10m) in seconds
# (600 when it doesn't parse): the warm session's limit grows with a longer
# idle stage (scale_idle), so it still gets its snapshot.
idle_seconds() {
  local d=$1 n=0 v u
  while [[ $d =~ ^([0-9]+)([hms])(.*)$ ]]; do
    v=${BASH_REMATCH[1]} u=${BASH_REMATCH[2]} d=${BASH_REMATCH[3]}
    case $u in h) n=$((n + 3600 * v)) ;; m) n=$((n + 60 * v)) ;; s) n=$((n + v)) ;; esac
  done
  [ "$n" -gt 0 ] || n=600
  echo "$n"
}

# run <side> <home> <name> <script> <timeout-s>
run() {
  local s=$1 home=$2 name=$3 script=$4 limit=$5
  local d="$out/$s/scale/$name"
  mkdir -p "$d"
  CLACK_HOME="$home" CLACK_PERF_DRIVE="$d" CLACK_PERF_SCRIPT="$script" CLACK_PERF_USERS="$work/users.txt" \
    "$out/$s/clack-perf" > "$d/clack.log" 2>&1 &
  local pid=$! seen="" t0=$SECONDS
  while kill -0 $pid 2>/dev/null; do
    for f in "$d"/*.mem.txt; do
      [ -e "$f" ] || continue
      local st
      st=$(basename "$f" .mem.txt)
      case " $seen " in *" $st "*) continue ;; esac
      seen="$seen $st"
      { echo "stage $st"; grep -E '^(VmRSS|VmHWM|RssAnon|RssFile|RssShmem)' /proc/$pid/status 2>/dev/null; } > "$d/$st.external.txt"
      echo "$s $name: stage $st at $((SECONDS - t0)) s: $(grep -E '^VmRSS' "$d/$st.external.txt")"
    done
    [ -e "$d/done" ] && break
    if [ $((SECONDS - t0)) -gt "$limit" ]; then
      echo "::warning::$s $name: no done after ${limit}s"
      break
    fi
    sleep 1
  done
  kill $pid 2>/dev/null
  wait $pid 2>/dev/null
  grep -E 'perfdrive|core ready|first frame' "$d/clack.log" | tail -40
}

for s in "${sides[@]}"; do
  [ -x "$out/$s/clack-perf" ] || continue
  home=$(mktemp -d)
  cp "$work/creds.json" "$home/creds.json"
  seed "$home"
  run "$s" "$home" prime "users,exit" 900
  run "$s" "$home" warm "${CLACK_PERF_WARM_SCRIPT:-startup,picker,picker-all,scroll,channels,idle,gifs,gifs-left}" $((6600 + $(idle_seconds "${CLACK_PERF_IDLE:-10m}")))
done
exit 0

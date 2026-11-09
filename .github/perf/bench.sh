#!/usr/bin/env bash
# Go benchmarks of each side against one fakeslackd (HEAD's, in its own
# process so benchmark heaps are the client's alone). One process per
# benchmark per side, so each gets clean CPU and heap profiles; the sides
# alternate benchmark by benchmark (and which goes first), so both see the
# same runner.
#
# usage: bench.sh <fakeslackd> <outdir> <side>...
#   each side's test binaries are <outdir>/<side>/{ui,core}.test (built with
#   the perfharness tag, see build.sh); results go to <outdir>/<side>/
#   bench.txt and <outdir>/<side>/prof/
set -uo pipefail
fake=$1 out=$2; shift 2
sides=("$@")
"$fake" -addr 127.0.0.1:8931 > "$out/fakeslackd-bench.log" 2>&1 &
fpid=$!
trap 'kill $fpid 2>/dev/null' EXIT
for i in $(seq 1 100); do curl -s -o /dev/null http://127.0.0.1:8931/img/1x1/x.png && break; sleep 0.1; done
export CLACK_FAKESLACK_URL=http://127.0.0.1:8931/
declare -A warm
for s in "${sides[@]}"; do
  mkdir -p "$out/$s/prof"
  : > "$out/$s/bench.txt"
  # each side's own warm config dir: cache formats differ between commits
  warm[$s]=$(mktemp -d)
  [ -x "$out/$s/ui.test" ] && CLACK_BENCH_WARMDIR="${warm[$s]}" "$out/$s/ui.test" -test.run '^TestPerfMakeWarmDir$' -test.v 2>&1 | tail -3
done

n=0
run() { # bin bench benchtime
  local bin=$1 name=$2 bt=$3 order=("${sides[@]}")
  if [ $((n % 2)) = 1 ]; then order=(); for ((i=${#sides[@]}-1; i>=0; i--)); do order+=("${sides[$i]}"); done; fi
  n=$((n + 1))
  for s in "${order[@]}"; do
    [ -x "$out/$s/$bin.test" ] || continue
    echo "::group::$s $name"
    CLACK_BENCH_PROFDIR="$out/$s/prof" CLACK_BENCH_WARMDIR="${warm[$s]}" \
      CLACK_BENCH_HEAPPROFILE="$out/$s/prof/$name.heap.pprof" \
      "$out/$s/$bin.test" -test.run '^$' -test.bench "^$name\$" -test.benchtime "$bt" -test.benchmem -test.count 1 \
      -test.cpuprofile "$out/$s/prof/$name.cpu.pprof" -test.memprofile "$out/$s/prof/$name.mem.pprof" \
      -test.timeout 15m 2>&1 | tee -a "$out/$s/bench.txt"
    echo "::endgroup::"
  done
}
run core BenchmarkCoreBoot 3x
run core BenchmarkLoadCache 10x
run ui BenchmarkStartup 5x
run ui BenchmarkBootCold 3x
run ui BenchmarkChannelSwitch 200x
run ui BenchmarkScroll500 400x
run ui BenchmarkEmojiPickerOpen 20x
run ui BenchmarkEmojiSearch 60x
run ui BenchmarkQuickSwitcher 20x
run ui BenchmarkTypingFrame 300x
run ui BenchmarkIdleFrame 300x
run ui BenchmarkSessionMemory 1x

for s in "${sides[@]}"; do
  d="$out/$s/prof"
  # top of every profile, for the log and the summary
  for p in "$d"/*.cpu.pprof; do
    [ -f "$p" ] || continue
    nm=$(basename "$p" .cpu.pprof); bin=ui; [[ $nm == BenchmarkCore* || $nm == BenchmarkLoadCache ]] && bin=core
    go tool pprof -top -nodecount 15 "$out/$s/$bin.test" "$p" > "$d/$nm.cpu.top.txt" 2>/dev/null || true
  done
  # per-op allocations of each measured loop (profile after minus before)
  for a in "$d"/*.loop-after.pprof; do
    [ -f "$a" ] || continue
    nm=$(basename "$a" .loop-after.pprof)
    go tool pprof -sample_index=alloc_space -top -nodecount 25 -base "$d/$nm.loop-before.pprof" "$a" > "$d/$nm.loop.alloc_space.txt" 2>/dev/null || true
    go tool pprof -sample_index=alloc_objects -top -nodecount 25 -base "$d/$nm.loop-before.pprof" "$a" > "$d/$nm.loop.alloc_objects.txt" 2>/dev/null || true
  done
  [ -f "$d/BenchmarkSessionMemory.heap.pprof" ] && go tool pprof -sample_index=inuse_space -top -nodecount 25 "$out/$s/ui.test" \
    "$d/BenchmarkSessionMemory.heap.pprof" > "$d/BenchmarkSessionMemory.heap.top.txt" 2>/dev/null || true
done
exit 0

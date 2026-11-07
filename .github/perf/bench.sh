#!/usr/bin/env bash
# Go benchmarks against internal/fakeslack, one process per benchmark so
# each gets clean CPU and heap profiles. Usage: bench.sh <outdir>
set -uo pipefail
out=${1:-perf-out}
mkdir -p "$out/prof"
go test -c -o "$out/ui.test" ./internal/ui
go test -c -o "$out/core.test" ./internal/core
: > "$out/bench.txt"
# the fake runs in its own process so benchmark heaps are the client's alone
go build -o "$out/fakeslackd" ./internal/fakeslack/fakeslackd
"$out/fakeslackd" -addr 127.0.0.1:8931 > "$out/fakeslackd.log" 2>&1 &
fpid=$!
trap 'kill $fpid 2>/dev/null' EXIT
for i in $(seq 1 100); do curl -s -o /dev/null http://127.0.0.1:8931/img/1x1/x.png && break; sleep 0.1; done
export CLACK_FAKESLACK_URL=http://127.0.0.1:8931/ CLACK_BENCH_PROFDIR="$out/prof"
export CLACK_BENCH_WARMDIR=$(mktemp -d)
"$out/ui.test" -test.run '^TestPerfMakeWarmDir$' -test.v 2>&1 | tail -3
run() { # pkg bench benchtime
  local bin=$1 name=$2 bt=$3
  echo "::group::$name"
  "$out/$bin.test" -test.run '^$' -test.bench "^$name\$" -test.benchtime "$bt" -test.benchmem -test.count 1 \
    -test.cpuprofile "$out/prof/$name.cpu.pprof" -test.memprofile "$out/prof/$name.mem.pprof" \
    -test.timeout 15m 2>&1 | tee -a "$out/bench.txt"
  echo "::endgroup::"
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
CLACK_BENCH_HEAPPROFILE="$out/prof/BenchmarkSessionMemory.heap.pprof" run ui BenchmarkSessionMemory 1x
# top of every profile, for the log and the summary
for p in "$out"/prof/*.cpu.pprof; do
  n=$(basename "$p" .cpu.pprof); bin=ui; [[ $n == BenchmarkCore* || $n == BenchmarkLoadCache ]] && bin=core
  go tool pprof -top -nodecount 15 "$out/$bin.test" "$p" > "$out/prof/$n.cpu.top.txt" 2>/dev/null || true
done
# per-op allocations of each measured loop (profile after minus before)
for a in "$out"/prof/*.loop-after.pprof; do
  n=$(basename "$a" .loop-after.pprof)
  go tool pprof -sample_index=alloc_space -top -nodecount 25 -base "$out/prof/$n.loop-before.pprof" "$a" > "$out/prof/$n.loop.alloc_space.txt" 2>/dev/null || true
  go tool pprof -sample_index=alloc_objects -top -nodecount 25 -base "$out/prof/$n.loop-before.pprof" "$a" > "$out/prof/$n.loop.alloc_objects.txt" 2>/dev/null || true
done
go tool pprof -sample_index=inuse_space -top -nodecount 25 "$out/ui.test" "$out/prof/BenchmarkSessionMemory.heap.pprof" \
  > "$out/prof/BenchmarkSessionMemory.heap.top.txt" 2>/dev/null || true
exit 0

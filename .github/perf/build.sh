#!/usr/bin/env bash
# Builds one side of the perf comparison from its own tree, with that
# tree's own go.mod, client and fakeslack package. Nothing from another
# commit is compiled in, except (optionally) the two perf test files.
#
# usage: build.sh <side> <tree> <outdir> [<overlay-tree>]
#
# Writes <outdir>/<side>/: clack (stripped release build), clack-perf
# (perfharness tag: accepts the loopback fake), clack-unstripped (symbol
# sizes), ui.test, core.test, binsize.txt, binsize-packages.txt and
# harness.txt (which perf tests the benchmarks were compiled from).
#
# With <overlay-tree>, that tree's internal/{ui,core}/perf_test.go replace
# the side's own, so both sides run the same benchmark code. They are
# white-box tests: when the overlay doesn't compile against this tree the
# side's own perf_test.go is used instead and harness.txt says so. A side
# whose benchmarks don't compile at all gets no test binary (its rows show
# "—"); only a failing client build is fatal.
set -uo pipefail
side=$1 tree=$(cd "$2" && pwd) out=$3 overlay=${4:-}
dst=$(mkdir -p "$out/$side" && cd "$out/$side" && pwd)
: > "$dst/harness.txt"

cd "$tree" || exit 1
export CGO_ENABLED=1
go build -trimpath -ldflags="-s -w" -o "$dst/clack" ./cmd/clack || exit 1
go build -tags perfharness -trimpath -ldflags="-s -w" -o "$dst/clack-perf" ./cmd/clack || exit 1
go build -trimpath -o "$dst/clack-unstripped" ./cmd/clack || exit 1
s=$(stat -c %s "$dst/clack")
echo "$(awk "BEGIN{printf \"%.1f MB\", $s/1048576}") stripped ($s bytes)" > "$dst/binsize.txt"

# binary size by package (top 40)
go tool nm -size -sort size "$dst/clack-unstripped" | python3 -c '
import sys, collections
tot = collections.Counter()
for l in sys.stdin:
    f = l.split()
    if len(f) < 4: continue
    name = " ".join(f[3:])
    for p in ("type:", "go:", "go.shape."): name = name.removeprefix(p)
    head, slash, tail = name.rpartition("/")
    tot[head + slash + tail.split(".")[0]] += int(f[1])
for k, v in tot.most_common(40): print(f"{v/1048576:8.2f} MB  {k}")
' > "$dst/binsize-packages.txt"

for pkg in ui core; do
  f=internal/$pkg/perf_test.go
  orig=$(mktemp)
  had=0; [ -f "$f" ] && had=1 && cp "$f" "$orig"
  used="own"
  if [ -n "$overlay" ] && [ -f "$overlay/$f" ]; then
    if [ $had = 1 ] && cmp -s "$overlay/$f" "$f"; then used="HEAD's (same file)"; else cp "$overlay/$f" "$f"; used="HEAD's (overlaid)"; fi
  fi
  if ! go test -c -tags perfharness -o "$dst/$pkg.test" "./internal/$pkg" 2> "$dst/$pkg.test.err"; then
    err=$(grep -m1 -E '\.go:[0-9]+' "$dst/$pkg.test.err" || head -1 "$dst/$pkg.test.err")
    if [ "$used" = "HEAD's (overlaid)" ] && [ $had = 1 ]; then
      cp "$orig" "$f"
      used="own (HEAD's doesn't compile here: $err)"
      go test -c -tags perfharness -o "$dst/$pkg.test" "./internal/$pkg" 2>> "$dst/$pkg.test.err" || used="none (neither HEAD's nor its own perf tests compile: $err)"
    else
      used="none ($err)"
    fi
    cat "$dst/$pkg.test.err" >&2
  fi
  echo "$pkg: $used" | tee -a "$dst/harness.txt"
done
exit 0

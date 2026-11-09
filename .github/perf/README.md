# perf harness

Scripts behind `.github/workflows/perf.yml`. The workflow compares HEAD against a base commit for the whole client: the real binary on Xvfb, Go benchmarks of `internal/ui` and `internal/core`, and binary size. Everything runs against `internal/fakeslack`, a synthetic Hack Club-sized workspace (65k custom emoji, 400 channels, 3000 users). Nothing talks to Slack.

| script | does |
|---|---|
| `build.sh <side> <tree> <out> [<overlay-tree>]` | builds one side from its own tree: `clack` (stripped, for size), `clack-perf` (`perfharness` tag, accepts the loopback fake), `ui.test`, `core.test`, size by package |
| `process.sh <fakeslackd> <out> base head` | the real binary: cold start, three warm starts, 95 s idle; first paint, core ready, RSS, Go heap, startup CPU profile |
| `bench.sh <fakeslackd> <out> base head` | the Go benchmarks, one process per benchmark per side, with CPU, heap and per-loop allocation profiles |
| `summary.py <out>/<side> <sha> <ref>` | one side's full summary (`summary.md`) and its perf-history row |
| `compare.py <out> <base> <head>` | the before/after table (`compare.md`), which leads the run page |
| `fontcheck.py` | embedded Lato vs the faces Slack serves (public CDN, no cookie) |

## Design

The base side is built entirely from the base tree, including its own `go.mod`, client and `internal/fakeslack`. The old workflow kept HEAD's `internal/fakeslack` (and a keep-list of other files) on top of the base tree, then patched the base with `patch-base.py`. That broke as soon as fakeslack imported something the base didn't have (`internal/slack/tlsfp`, which needs utls), and the job then failed on every push from about alpha.10 on. Now the two sides share no source, so fakeslack can grow freely.

The workload is the same on both sides because the fake server runs as its own process (`fakeslackd`) and speaks Slack's wire API. One `fakeslackd`, built from HEAD, serves both sides. In process mode the client loads the credentials file fakeslackd writes. In benchmark mode the perf tests call `fakeslack.Remote(url, size)`, and they use only `Remote`, `Start`, `Creds`, `Fetch`, `ChannelIDs`, `HackClub` and `Small`, all from the side's own copy of fakeslack. That small surface is the harness interface. Keep it stable: a perf test that needs more of the fake should ask the server over HTTP instead of reaching into the package.

The base side runs HEAD's benchmark code whenever it can. `build.sh` copies HEAD's `internal/{ui,core}/perf_test.go` into the base tree. These tests are white-box (they use App internals), so if HEAD's copy doesn't compile against the base, the base's own copy is used. Each side's `harness.txt`, and the notes under the report, say which copy ran. Nothing on the base side fails the job: a base that doesn't build, boot or benchmark shows `—` and gets a note. A HEAD that doesn't build, or whose perf tests don't compile, fails the job.

Both sides run in one job, interleaved. `process.sh` alternates base and head run by run, and `bench.sh` alternates them benchmark by benchmark, switching which side goes first each time. Two jobs land on two runners, and shared runners differ by up to 2x, so the old matrix's two summaries didn't compare. Each number is still a single run, so treat deltas under about 10% as noise. `compare.py` flags only changes beyond that.

By default the base is the newest `v*` tag before HEAD (`git describe --tags --abbrev=0 --match 'v*' HEAD^`). The report then answers "what changed since the last release", and the base keeps moving forward, so it never drifts far from the harness. Every release tag has the `perfharness` build tag and the perf tests. To compare against anything else (a branch, a sha), dispatch the workflow with `base`.

Run time is about 2 × (2.5 min process + 7–14 min benchmarks), plus builds.

## Reading the report

The run page starts with **Before/after**. It has headline rows (first paint, core ready, RSS, heap, binary size), then one row per benchmark: time/op, allocs/op, alloc/op and the custom metrics (`sidebar-ms`, `heap-MB`, `wakeups/op`, …), then the largest per-package size changes. Lower is better everywhere except `msgs`, which counts workload. Next comes HEAD's full summary (per-run table, benchmark table, session heap top, idle RSS by mapping, size by package), then the base's summary, folded. The `perf` artifact has `base/` and `head/` with every log, `.pprof` profile and pprof top.

## perf vs bench-compare

`bench-compare.yml` covers a different layer. It runs the package microbenchmarks (CSS engine, slackc, `BenchmarkListFrame`) for a branch against `ui` (by default), interleaved for N rounds, and benchstat gives confidence intervals. It finishes in minutes, and plugin/CSS work uses it while iterating (see docs/PLUGINS-components.md). `perf` is the whole-client run against the last release and takes about half an hour. They follow the same rule (both sides on one runner, interleaved), but merging them would either slow the quick loop down to perf's run time or drop perf's real-process numbers, so they stay separate. To get a branch-vs-`ui` whole-client comparison, dispatch `perf` on the branch with `base: ui`.

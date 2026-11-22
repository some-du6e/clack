# CI speed

Clack's CI as measured on GitHub Actions, what was changed to make it fast
without letting it lie, and what was ruled out on the numbers. Branch
`ci-speed`, 2026-11-20/21. Every number here comes from real runs
(scratch scripts: `/root/clack-ops/scratch/ci-speed/bin/`).

Three rules held throughout:

1. The full suite still runs before anything reaches ui or main: ci's
   full-test, every area workflow and the release dry run.
2. No failure is hidden: no retries, no wider timeouts, no skipped tests.
3. No check is dropped: lint, vet, the guard tests and required checks stay
   or grow.

## The audit

Window: 2026-11-18 22:00 to 2026-11-20 23:00 UTC (49 h). 4,570 runs; the
1,979 that ended in success or failure were sampled (up to 40 per workflow,
plus every release-candidate run), 3,851 jobs with each step's times (check
runs through GraphQL, which has its own quota). Per job:

- **queue**: the job's start minus when it was queued (the run's creation,
  or its `needs:` jobs' end);
- **setup**: Set up job, checkout, setup-go (Go and the cache restore), the
  apt installs (`gio deps`, mingw), download-artifact;
- **teardown**: post steps (setup-go's cache save), upload-artifact;
- **run**: everything else. Compiling inside `go test` counts as run.

Job-minutes weighted by each workflow's 48 h volume:

| bucket | job-minutes per 48 h | share of runner time |
|---|---|---|
| **queue (waiting, no runner)** | **322,817** | (6.7x the runner time) |
| setup | 6,683 | 13.8% |
| run | 40,321 | 83.2% |
| teardown | 695 | 1.4% |
| runner time, completed runs | 48,460 | |
| runner time, cancelled runs (estimated from 260 of 1,337) | ~8,100 | |

**The biggest bucket is the queue.** Jobs waited 6.7 times as long as they
ran. On 2026-11-21 01:38 the account had 39 jobs running against its cap of
40 (30 ubuntu-latest, 6 windows-latest, 3 macos-latest) and 214 queued
(183 ubuntu-latest). So the first fix is less demand, then a shorter
critical path.

| OS | jobs sampled | queue p50 / p95 | setup p50 / p95 | run p50 / p95 | runner-min per 48 h | setup share |
|---|---|---|---|---|---|---|
| Linux | 3359 | 24.2 / 70.2 | 0.6 / 1.9 | 2.0 / 12.2 | 32,042 | 16% |
| Windows | 366 | 13.9 / 60.6 | 1.1 / 1.3 | 4.4 / 36.0 | 10,786 | 12% |
| macOS | 126 | 29.9 / 62.1 | 0.7 / 1.2 | 4.1 / 30.4 | 5,632 | 4% |

The scheduler serves runner labels in turns: in the same 15 release runs,
jobs on `ubuntu-22.04` started after a median 1.4 min while `ubuntu-latest`
jobs queued at the same moment waited 19.6 (up to 58). A probe
(`ci-speed-probe` branch, one `echo` job per label, dispatched at once):
`ubuntu-24.04` 5.5 min, `ubuntu-24.04-arm` 5.8, `ubuntu-22.04` 7.0,
`windows-2022` 7.1, while `ubuntu-latest`, `windows-latest` and
`macos-latest` were still queued after 40 min. With the account at its
cap, moving jobs to other labels only reorders Clack's own jobs (it adds
no capacity); a label kept for releases would be a fast lane.

### Setup

| step | Linux p50 / p95 | Windows p50 / p95 | macOS p50 / p95 |
|---|---|---|---|
| checkout | 3 s / 7 s | 10 s / 13 s | 5 s / 7 s |
| setup-go (Go + cache restore) | 17 s / 22 s | 56 s / 68 s | 37 s / 68 s |
| setup-go post (cache save) | 4 s / 6 s | 0 s / 1 s | 18 s / 34 s |
| apt (gio deps) | 13 s / 100 s | - | - |

- **setup-go's cache never restored on Linux or macOS.** setup-go installed
  go.mod's `go 1.24.0`; its `go env` then downloaded the `toolchain go1.24.9`
  into the module cache, and the restore's tar failed on those files
  (`Failed to restore: tar ... exit code 2`) in every job. The job counted a
  miss, and each branch saved a 300-680 MB entry of its own: the repo's
  caches were at 10.14 GB of 10 GB (22 entries), evicting each other.
  main's entries, which every branch tries first, were 288 MB (Linux) and
  156 MB (macOS) builds saved on 11-16 by whichever job finished first.
- **apt** is 13 s at p50, but on 2026-11-19/20 apt steps took 640-1,280 s
  in seven workflows (a 15 min timeout per try, twice).
- **checkout** is already shallow (depth 1); the history is 2.4 MB without
  blobs, so nothing to gain there.

### Per workflow

| workflow | runs / 48 h | queue p50 / p95 | setup p50 / p95 | run p50 / p95 | wall p50 / p95 | runner-min / run | runner-min / 48 h |
|---|---|---|---|---|---|---|---|
| ci | 179 | 39.7 / 76.9 | 0.6 / 1.4 | 4.0 / 20.1 | 96.8 / 143.0 | 142.4 | 25486 |
| perf | 64 | 20.3 / 59.0 | 0.6 / 1.4 | 21.1 / 36.0 | 80.1 / 124.4 | 83.8 | 5363 |
| flake-check | 340 | 53.5 / 90.6 | 0.6 / 1.4 | 1.7 / 13.8 | 42.9 / 72.4 | 15.4 | 5248 |
| plugin-bench | 51 | 27.3 / 64.3 | 0.5 / 2.0 | 7.2 / 7.9 | 60.7 / 111.4 | 45.5 | 2319 |
| demo | 61 | 20.2 / 45.0 | 0.6 / 1.6 | 11.4 / 13.3 | 32.4 / 56.7 | 12.5 | 763 |
| release | 15 | 6.2 / 54.0 | 0.5 / 1.2 | 2.9 / 19.6 | 79.5 / 125.9 | 41.6 | 625 |
| ui-slick | 71 | 22.2 / 53.5 | 0.5 / 1.4 | 7.2 / 7.5 | 30.4 / 61.5 | 7.4 | 528 |
| supply-chain | 321 | 30.6 / 65.7 | 0.2 / 0.3 | 0.0 / 0.1 | 35.6 / 68.1 | 1.4 | 458 |
| i18n | 69 | 17.1 / 54.0 | 0.6 / 12.4 | 3.4 / 4.5 | 22.3 / 57.6 | 5.6 | 390 |
| code-channels | 56 | 19.0 / 53.0 | 0.6 / 1.1 | 6.2 / 7.6 | 26.1 / 59.6 | 7.0 | 390 |
| hover-card | 70 | 17.2 / 54.7 | 0.6 / 1.7 | 4.8 / 5.8 | 23.6 / 59.4 | 5.4 | 380 |
| updater | 20 | 20.5 / 48.9 | 0.9 / 1.8 | 1.8 / 2.9 | 32.0 / 58.2 | 16.9 | 338 |
| huddle | 19 | 19.8 / 63.3 | 0.7 / 1.4 | 2.4 / 4.9 | 30.2 / 69.5 | 14.9 | 284 |
| ui-taut | 64 | 22.9 / 55.3 | 0.6 / 2.2 | 3.0 / 3.6 | 26.4 / 58.9 | 4.1 | 263 |
| threads-audit | 49 | 19.2 / 49.2 | 0.6 / 6.0 | 3.5 / 4.7 | 23.6 / 52.4 | 4.9 | 242 |
| emoji-picker | 33 | 12.5 / 51.5 | 0.7 / 1.4 | 1.3 / 1.8 | 20.2 / 54.9 | 7.1 | 233 |
| files-shots | 52 | 18.1 / 55.5 | 0.7 / 2.0 | 2.2 / 9.3 | 24.7 / 57.8 | 4.4 | 227 |
| image-send | 35 | 16.6 / 45.3 | 0.6 / 4.5 | 4.8 / 5.9 | 22.9 / 52.4 | 6.2 | 218 |
| embeds-shots | 62 | 22.4 / 57.9 | 0.6 / 1.9 | 2.2 / 3.5 | 26.7 / 60.0 | 3.3 | 207 |
| bk-surfaces | 67 | 20.6 / 47.0 | 0.6 / 1.6 | 2.0 / 2.9 | 22.8 / 49.0 | 3.0 | 198 |
| activity-shots | 54 | 16.8 / 38.1 | 0.6 / 1.7 | 2.7 / 3.7 | 20.4 / 41.5 | 3.7 | 198 |
| site | 19 | 8.1 / 59.9 | 0.1 / 0.5 | 3.1 / 5.8 | 14.8 / 64.4 | 10.1 | 192 |
| channels-audit | 26 | 12.9 / 49.0 | 0.7 / 1.5 | 6.1 / 8.0 | 21.9 / 55.3 | 7.3 | 190 |
| bk-gallery-shots | 76 | 25.0 / 53.3 | 0.6 / 1.5 | 1.4 / 2.6 | 28.2 / 55.6 | 2.4 | 183 |
| messages-audit-shots | 43 | 21.1 / 50.9 | 0.7 / 3.6 | 2.6 / 4.0 | 25.4 / 54.3 | 4.1 | 178 |
| views-audit | 60 | 20.8 / 57.8 | 0.6 / 1.8 | 1.8 / 2.9 | 23.4 / 60.0 | 2.8 | 170 |
| compact-shots | 41 | 18.8 / 55.6 | 0.6 / 1.9 | 2.8 / 3.5 | 22.2 / 59.0 | 3.8 | 155 |
| command-modals | 59 | 23.0 / 44.8 | 0.6 / 1.6 | 1.6 / 2.6 | 25.8 / 46.7 | 2.6 | 155 |
| icon-shots | 44 | 16.9 / 47.3 | 0.6 / 2.5 | 1.9 / 3.2 | 19.9 / 50.2 | 3.0 | 132 |
| channel-actions-shots | 25 | 16.2 / 51.4 | 0.7 / 3.5 | 3.9 / 4.7 | 21.0 / 55.5 | 5.3 | 131 |
| experiments | 39 | 13.7 / 47.1 | 0.5 / 2.5 | 2.1 / 2.5 | 16.7 / 49.5 | 2.9 | 114 |
| status-shots | 18 | 13.9 / 48.8 | 0.6 / 1.8 | 5.0 / 6.3 | 20.0 / 54.2 | 6.3 | 113 |
| prefs-audit-shots | 39 | 20.3 / 52.6 | 0.6 / 2.2 | 1.7 / 2.8 | 22.9 / 55.4 | 2.8 | 110 |
| messages-taut | 47 | 23.6 / 59.1 | 0.5 / 2.2 | 1.5 / 1.8 | 25.9 / 61.0 | 2.3 | 107 |
| exp-channels | 39 | 14.3 / 47.6 | 0.6 / 1.3 | 1.8 / 2.8 | 16.3 / 50.2 | 2.7 | 105 |
| spacing-shots | 44 | 24.5 / 55.5 | 0.7 / 2.5 | 1.0 / 2.1 | 25.8 / 57.9 | 2.4 | 104 |
| sender-identity | 63 | 23.5 / 55.0 | 0.6 / 1.6 | 0.6 / 1.4 | 24.9 / 56.1 | 1.5 | 97 |
| ch-header | 50 | 14.6 / 59.8 | 0.6 / 1.5 | 0.9 / 2.0 | 16.6 / 60.9 | 1.9 | 96 |

(minutes; the top 38 by runner time; `wall` is the run's first queue to its
last job's end.)

### ci, per job

| ci job | n | OS | queue p50/p95 | setup p50/p95 | run p50/p95 | total p50/p95 |
|---|---|---|---|---|---|---|
| macos | 42 | macOS | 39.7 / 71.4 | 0.7 / 1.2 | 29.1 / 31.7 | 30.4 / 33.1 |
| full-test | 22 | Linux | 52.5 / 83.2 | 0.4 / 1.3 | 19.2 / 20.4 | 19.8 / 21.6 |
| windows-idle | 42 | Windows | 14.6 / 47.0 | 1.1 / 1.4 | 15.5 / 15.8 | 16.8 / 17.2 |
| linux-idle | 42 | Linux | 47.5 / 77.6 | 0.6 / 1.4 | 12.6 / 13.7 | 13.4 / 14.5 |
| windows-applog | 42 | Windows | 14.9 / 41.7 | 1.1 / 1.5 | 8.1 / 9.2 | 9.4 / 10.5 |
| linux-test (3/4) | 42 | Linux | 50.0 / 77.5 | 0.6 / 1.5 | 7.5 / 8.8 | 8.4 / 9.9 |
| windows-shell | 42 | Windows | 13.2 / 43.8 | 1.2 / 1.4 | 7.0 / 9.1 | 8.4 / 10.4 |
| linux-gio | 41 | Linux | 40.9 / 74.0 | 0.6 / 1.1 | 5.7 / 7.4 | 6.7 / 8.1 |
| linux-race-taut | 42 | Linux | 55.5 / 77.8 | 0.6 / 1.0 | 5.8 / 6.2 | 6.5 / 6.9 |
| linux-test (rest) | 42 | Linux | 51.5 / 81.1 | 0.6 / 1.1 | 4.6 / 5.9 | 5.4 / 6.6 |
| linux-test (2/4) | 42 | Linux | 45.7 / 85.0 | 0.6 / 0.9 | 4.2 / 6.1 | 5.1 / 6.8 |
| linux-test (1/4) | 42 | Linux | 49.8 / 76.8 | 0.6 / 1.6 | 4.0 / 5.5 | 4.7 / 6.4 |
| linux-test (4/4) | 42 | Linux | 54.3 / 80.0 | 0.7 / 1.4 | 3.1 / 4.9 | 4.2 / 5.7 |
| windows-scroll | 42 | Windows | 16.2 / 59.9 | 1.1 / 1.3 | 2.4 / 2.6 | 3.6 / 3.9 |
| linux-build | 42 | Linux | 43.4 / 78.0 | 0.6 / 0.8 | 1.9 / 3.3 | 2.9 / 4.2 |
| linux-race-boot | 42 | Linux | 56.6 / 78.2 | 0.6 / 1.5 | 1.8 / 2.4 | 2.8 / 3.4 |
| windows | 42 | Linux | 48.2 / 81.2 | 0.7 / 5.7 | 1.2 / 1.5 | 2.3 / 6.9 |
| parity | 42 | Linux | 45.4 / 76.5 | 0.6 / 1.8 | 0.8 / 2.0 | 1.7 / 2.8 |
| newer-go (1.25.x) | 42 | Linux | 44.7 / 76.5 | 0.1 / 0.2 | 0.2 / 0.3 | 0.4 / 0.5 |
| newer-go (stable) | 42 | Linux | 39.5 / 75.5 | 0.2 / 0.2 | 0.2 / 0.3 | 0.4 / 0.5 |
| linux-coverage | 42 | Linux | 21.8 / 58.1 | 0.1 / 0.2 | 0.0 / 0.0 | 0.1 / 0.2 |

### By trigger (runner-minutes, 48 h)

| trigger | runner-min | biggest |
|---|---|---|
| dispatch on a branch | 28,877 | ci 11,161 (72 runs: the pre-merge full-test dispatch ran all 21 jobs again), perf 4,939, plugin-bench 2,277, flake-check 1,388 |
| push to a branch | 14,294 | ci 13,669 (106 runs), supply-chain 445 |
| push to ui / main / a tag | 959 / 355 / 175 | (most are cancelled by the release practice) |

### A release

| | runs | jobs | runner-min | wall (first queued to last done) |
|---|---|---|---|---|
| release-111 candidate | 59 | 105 | 528 | 66 min |
| release-112 candidate | 43 | 84 | 448 | 76 min |
| v0.1.0-alpha.111 tag run | 1 | 8 | 37 | 99 min (meta queued 31.6 min) |
| v0.1.0-alpha.112 tag run | 1 | 8 | 37 | 86 min (meta queued 26.1 min) |

An agent push runs ci and supply-chain: ~134 runner-minutes, 25 jobs.

## Changes

Each change shipped as its own commit; "after" is from runs on `ci-speed`.

| change | before | after |
|---|---|---|
| **ci `-f only=<job id prefix>`**: `only=full-test` is the pre-merge full-test alone; the branch's push run on the same sha has run the other jobs (keep it, don't cancel it) | a pre-merge dispatch: 21 jobs, ~155 runner-min (72 in 48 h: 11,161 runner-min, 25% of all CI) | 1 job, 19.0 runner-min (run 44840091536) |
| **release.yml without the meta job**: each job works out the tag, version and publish decision itself (`release-meta.sh`) | the 6 s meta job queued alone first: 26.1 / 31.6 min before any build job was even queued (α.112 / α.111) | all five build jobs queued at once; the linux builds started 30 s after the dispatch (dry run 44840095722) |
| **TestIdleFrames in 4 processes**: its six groups are subtests, each on its own client; `fanout.sh` runs them from one test binary at once and checks each ran once | TestIdleFrames 658-663 s on each OS; linux-idle 13.4, windows-idle 16.8, macos 30.4 min (p50) | linux-idle 6.3, windows-idle 9.8, macos 23.2 min (run 44840062509): -21 runner-min a ci run |
| **linux-test shards packed by recorded time** (`.github/test-times.json`, `test-shard.py --times`) | shard p50 4.7 / 5.1 / 8.4 / 4.2 min (internal/ui's share 144-409 s) | 5.1 / 5.2 / 4.9 / 5.2 min; linux-coverage: 92 packages, 3,959 top-level tests, each once |
| **go.mod `go 1.24.9`** (no toolchain switch, so the cache restores) | every Linux and macOS job: `Failed to restore: tar ... exit code 2`, a miss, and a 300-680 MB save per branch | `Cache restored successfully` in every job checked (runs 44854132904, 44860564804); branches stop saving entries of their own |
| **go-cache.yml**: on each main push, a build of everything the jobs build replaces main's setup-go entry for every OS | main's entries: a 288 MB (Linux) and 156 MB (macOS) partial build from 11-16 | the seed: 4.8 / 6.7 / 8.6 min (Windows / Linux / macOS), 561 / 833 / 588 MB, the same key and version setup-go restores (run 44854133106). Same code, jobs on the seed (44854132904) against main's old entry (44860564804): linux-build 0.9 vs 4.0 min, macos 8.9 vs 12.3, the windows build 1.0 vs 1.7, linux-test shards 5.1-5.3 vs 6.9-7.3 |
| **macos-idle beside macos** | macos 30.4 min p50 (23.2 with the idle processes) | macos 8.9 and macos-idle 11.0 min at once (run 44854132904): the longest ci job is now ~11 min |
| **macos's launch budget** (launch-speed, 2026-11-21: `.github/perf/launch-budget.sh`): a perfharness Clack.app launched through LaunchServices, cold once and seven times warm | — | +2.3 min on the macos job (run 44940276644: 1.4 min the build, 0.9 the launches); go-cache seeds the perfharness build |
| **apt.sh**: an update gets 5 min, an install 10, then the kernel.org mirror | 15 min a try, twice: 640-1,280 s steps | (a tail; shows over days) |
| **budgets**: 106 jobs' `timeout-minutes` from their p95 (1.5 x p95 + 3, at least 15) | GitHub's 360 min | a job past its budget fails |
| **menu-keys on macOS**: the test reads Copy message's rich copy (`writeRich`), which on macOS goes to the pasteboard, not through Gio | TestMenuKeysMessageActions and TestMenuTypeAhead red on every macOS run since 11-14 (70 failure events, 15 branches, never resolved) | both pass, with `linux/context/C/Ctrl` and `darwin/context/C/⌘` (run 44854164035) |

## Ruled out on the numbers

- **Test selection by the import graph.** Replayed over the 531 branch
  pushes of the window (`sel_estimate*.py`): with go.mod, the Gio patches,
  the workflows, testdata and fakeslack rerunning everything, 517 reran
  everything (470 touched `.github/`, 421 `internal/fakeslack/`). Narrowed
  to the files ci itself reads, and fakeslack and the vendored Gio through
  the graph: 2.4 runner-min a push skipped (under 2%), 94% of the test
  seconds still run, because 396 of 531 diffs reach internal/ui, which
  imports almost everything. Not built: it isn't worth the soundness risk
  (52 test files read outside their package; tlsfp's, shortcut's and ui's
  guards scan the whole tree).
- **One test binary per job for every step.** After a job's first compile,
  each later ui test binary starts in 4-6 s (link and start): about a
  minute a macOS job. Done only where it fans out (the idle processes).
- **Folding small jobs** (newer-go, parity, the race jobs, supply-chain's
  five): their setup is 0.2-0.6 min each, and under a cap four fewer jobs
  move a run's last job start by about 30 s.
- **Runner labels**: see the audit (the cap binds, so it only reorders).
- **An apt .deb cache**: a restore step in ~100 jobs for ~5% of runner
  time; the timeouts come first. The next candidate.
- **Splitting the other tests over 60 s.** TestIdleFrames (660 s) runs in
  four processes. TestDemoSurfaces (134 s) stays whole: its steps share one
  demo session's timeline (the pages first, so Unreads and Activity show
  the demo as it starts), and time packing fits it in a 242 s shard.
  TestMacIdleCPU (168 s), TestWheelFrameProbe (128 s), TestRealWindowMemory
  (120 s) and the frame cost tests (60 s) measure CPU, frames or memory
  over a period: their length is the measurement, and processes beside
  them would change it.

## Tests: two weeks of failures

1,961 failed runs and 2,307 failed jobs, 2026-11-06..20 (flake-check and
-red/-mut evidence branches left out). The failing tests come from every
one of those jobs' logs (top-level `--- FAIL`; 1,625 of the 1,953 that ran
tests named one): 3,093 failures of 752 tests. Each failure is classed by
what happened next on its branch, the same workflow's next green run and
the diff to it (`bin/classify.py`):

| class | failures | share |
|---|---|---|
| code changed before it passed (a regression caught, or code finished) | 2,082 | 67% |
| never green again on the branch (abandoned, or still red) | 471 | 15% |
| only tests, testdata, goldens or CI changed (a change detected) | 404 | 13% |
| the same sha passed later (a flake) | 136 | 4% |

**No test is deleted.** A delete or merge needs at least 3 failures on at
least 2 branches, every one ended by a test change alone. One test meets
that: TestSlickViewThemeOverlays, and only because ui-taut and ui-slick
both run it. Its two episodes are the theme-overlays branch writing it
(11-08) and prefs-colors (11-09) changing a colour it checks: one intended
change detected, so it stays. Every other test-changed-only test is one
branch's episode, a new test adjusted until it passed
(TestThreadsUnreadRowEdgesLikeSlack, TestGIFShots, TestHuddleShots*: 4-5
failures on one branch each). The nearest after it,
TestLoungeBannerLikeSlack (8 test-changed on 5 branches), also ended with
code fixed 5 times: it catches regressions. Every flake event (136) is
ui-slick's 15 TestSlickView* tests failing together on 2 branches: ui-slick
builds Slick main, unpinned, so an upstream change reds every branch at
once (pinning it, with Slick main as a ui/main-only job, is a choice for
Karim: it would change what ui-slick checks).

### Flakes fixed (each a bug in the test, found and fixed)

| test | cause | fix |
|---|---|---|
| TestMenuKeysMessageActions, TestMenuTypeAhead (menu-keys, macOS) | read the clipboard through Gio's router; macOS's rich copy goes to the pasteboard off the UI goroutine | read it through `writeRich` (run 44854164035 green) |
| TestWarmWindowWritesSurviveBoot (core) | held the first `users.channelSections.list`: when MoveToSection's own reread reached the fake before Boot's, the write waited on itself 60 s (8 failures, 6 branches) | the writes wait until Boot's four calls are held (`holdStale`'s `held`) |
| TestRailPeekDMsRowMenu (ui) | `waitFor(60 ms)`'s frames could all land before the peek's 30 ms throttle's trailing check was due | `waitFor` ends with a frame after its wait (all 19 callers) |
| TestSpellcheckNeverBlocksFrame (composer) | timed each frame's layout (48 ms against 40 on α.119's release runner) | the engine is held shut while the frames run: a frame that waited on it never returns |
| TestThreadsRefreshGapLikeSlack/straddle (views) | reproduced on ci-speed under load (flake-check 44860567669: a getView before the hold) | fixed by history-gap on train-125c |

flake-check's `flakes-1121` runs the first three fixes under load.

### Flakes still open (no fix here yet)

- TestMacIdleCPU "channel again: 20 frames in 10 s" (once, 11-21): read
  the frames' requesters in the next failure's log.
- TestTautZeroCost "51 allocs/frame (none: 50)": AllocsPerRun's integer
  average flips on one background allocation.
- TestRealWindowCaretCost, TestFrameCostDamage: CPU ratios and pixels on
  loaded or slow runners.
- ui-slick's TestSlickView* (unpinned upstream, above).
- TestMessageMenuOldMenuRows (popups) "timed out waiting for the old row
  next to Act one: the old row's click", once, in ci-speed's full-test
  (run 44896761367, 2026-11-21). First failure in the two weeks; no
  ci-speed change touches it. (Round 2: the "copy_link after 451ms" line in
  that log is TestMessageMenuStalePickDoesNotRun's own, which sets the
  deadline to 150 ms; it says nothing about this failure.)

## For Karim: accounts and money

Nothing here was done: each needs an account, a secret or money. Clack is
public, so GitHub's standard runners cost nothing per minute; what limits
CI is the account's 40 concurrent jobs (5 macOS), which the queue above
measures. Volume before these changes: ~16,000 Linux, ~5,400 Windows and
~2,800 macOS runner-minutes a day (completed runs; cancelled ones add
~4,000, mostly Linux).

| # | what | needs | about a month | what it buys |
|---|---|---|---|---|
| 1 | ask GitHub Support to raise the concurrent-job cap (GitHub's limits page: "You can ask GitHub Support to raise these caps") | a ticket from Karim's account | $0 | the queue, the biggest bucket, shrinks with every slot added |
| 2 | move clack into an organization on GitHub Team | an org, a seat | ~$4 (one seat) | 60 concurrent jobs instead of 40 (macOS stays at 5) |
| 3 | GitHub Enterprise Cloud for that org | a plan | ~$21 (one seat) | 500 concurrent jobs, 50 macOS: today's peak (~250 queued + 40 running) fits with no queue |
| 4 | Namespace's open-source program | Karim applies | $0 if accepted | Linux, Windows and macOS runners outside GitHub's cap |
| 5 | a self-hosted Linux runner machine (8-16 cores, 4-8 runners) | a machine and a runner registration token (a secret) | ~$40-60 rented, $0 on Karim's own hardware | 4-8 Linux slots outside the cap; first set fork pull requests to need approval for all outside contributors |
| 6 | a self-hosted Mac mini (M4) | ~$600 once, a token | ~$5 power | macOS jobs about twice as fast as GitHub's 3-core M1, outside the 5-job macOS cap |
| 7 | per-minute runners (Ubicloud, Blacksmith, WarpBuild) | an account, a card | Linux only: ~$620-960 (Ubicloud, $0.0013-0.002/min) to ~$1,900 (Blacksmith, $0.004/min); macOS adds ~$6,700 at $0.08/min | no cap for what moves there |
| 8 | make ui the default branch (a setting) | Karim's say | $0 | branches restore ui's caches (fresher than main's) and a workflow on ui can be dispatched before a release; but main stops being what the repo page shows |

Prices are list prices from GitHub's docs and compiler.dev's runner price
table (checked 2026-10-22); check each provider's page before buying.
1-3 need no change to any workflow; 4-7 need `runs-on` labels.

## Totals

| | before | after |
|---|---|---|
| ci on a branch push, runner-minutes (sum of its jobs) | 133.6 (p50s) | 104.8 with the seed (44854132904); 118.5 on main's old entry (44860564804), and 119.1 merged with α.122's ui (44896761533: its windows-idle has the video steps) |
| ci's longest job (the branch's verdict, past the queue) | macos 30.4 min | macos-idle 11.0 min with the seed, 14.1 on main's old entry |
| a pre-merge ci dispatch | 21 jobs, ~155 runner-min | `only=full-test`: 1 job, 19.0 |
| a tag's release run, before its builds even queue | 26-32 min (meta's own queue) | 0 |
| linux-test's slowest shard | 8.4 min | 5.2 |
| a job's timeout | 360 min (GitHub's) | its budget, 15-40 min |
| CI per day, runner-minutes | ~24,000 completed + ~4,000 cancelled | about 21,000 (est.: -4,900 pre-merge dispatches, -1,600 branch pushes, the seed on top) |
| CI cost | $0: public, standard runners | $0 |

The queue, the biggest bucket, falls with every runner-minute taken out:
at the cap a job waits for the work ahead of it. A release candidate's
full set (448-528 runner-minutes, 66-76 min of wall clock on 11-19) loses
its ci dispatch's 136 minutes and gets the longest ci job down from 30 to
11 minutes; perf (84 runner-minutes a dispatch, every branch: it has no
`paths:`) and plugin-bench (45) are the next biggest per candidate.

## Round 2

Branch `ci-speed-2` (2026-11-21), on train-130 (α.127, ci-speed's squash)
and then ui. Each item has a run that shows the old behaviour (red) and
one that shows the new (green), on evidence branches `ci-speed-2-*` that
carry a mutant or an injected fault and are deleted afterwards; the runs
stay.

| item | change | red | green |
|---|---|---|---|
| 1. a hung idle group's dump | the idle jobs' fanout `-test.timeout 10m` (it was their budget: 20, 25); `fanout.sh` streams each process's lines marked `[i]`; a failed or cancelled idle step uploads `$RUNNER_TEMP/fanout.*` | 44916667648: a hung TestIdleFrames group; the job stopped at its 20 min budget, no panic, none of the group's output | 44916670354: `panic: test timed out after 10m0s` and every goroutine, streamed, at minute 12.5 of 20; artifact fanout-logs-linux-idle |
| 1. full-test, release's test job | `dump-before-budget.sh 35`: the test binaries still running 35 min into the step get SIGQUIT, so each prints its goroutines and go test its output. Its packages start up to 5 min in (`-json` run 44920029289: ui at 2.5, the last at 5.0), so 30 min from there was ~36 of the 40, and later on a slow runner (train-130's attempt 2: stopped at 40, nothing printed) | 44922855172: train-130's full-test with a hung `ui/views` test that starts late (5 min of slowness first): "exceeded the maximum execution time of 40m0s", nothing printed | 44922860457: 35 min into the step, SIGQUIT to `views.test` (running 23:47) and its goroutines, the hang's TestTodosSectionListsLikeSlack among them; the job over at 35.4 of its 40 min |
| 1. every Go timeout in every job | `budget-check.py` (linux-build) counts a timeout from when its binary starts: the step's recorded start (`.github/step-starts.json`, written by `budget-check.py record` from `gh api .../runs/ID/jobs`) a quarter slower, plus the build; a later command in a step from the step's recorded end; a step with no record from minute 4 if it is the job's first, else from the job's p95. 83 timeouts lowered to the most that still dumps in time (never under twice the step's slowest recorded run, plus a minute), 4 steps given `dump-before-budget.sh` (Linux and macOS), windows-idle's budget 20 to 22 | on train-130's workflows: 88 timeouts whose dump prints past the budget | 0 |
| 2. a republished old tag | the jobs run `release-meta.sh` from the run's own commit (`git show $GITHUB_SHA:...`), not the tag's tree | 44917159512, job meta-from-the-tags-tree: no `release-meta.sh` in v0.1.0-alpha.126's tree | the same run, meta-from-the-runs-commit: TAG v0.1.0-alpha.126, publish true |
| 3. a frame that waits a while | TestSpellcheckNeverBlocksFrame: the median frame over still text with the engine's answers held back within 4x a keystroke's frame (its layout, the same run) plus 2 ms. Keystrokes look up no word being typed, so typing frames never wait (a version that compared them passed a mutant: 44919645996) | 44918439106: train-130's test passes frames that wait up to 250 ms each | 44920636274: fails them (median 250.9 ms, bound 4.5 ms); 44920639031: x5 on the real editor, held frames 47-73 µs against keystrokes 447-746 µs |
| 4. a typo in `only` | `only-check` fails when `only` starts no job's id; `run-name: ci only=<only>` | 44921857436: train-130's ci.yml, `only=fulltest`: all 17 jobs skipped, the run "skipped", titled "ci" | 44921860168: `only="fulltest" starts no job's id, so every job would be skipped`, titled "ci only=fulltest" |
| 5. a cached result counted as a run | `test-shard.py` runs `go test -count=1`, and `check` fails on any package replayed from the cache | train-130's checker passes shards with an `ok ... (cached)` package | `check` fails on it, passes the real shards |
| 6. the warm-window test's held answers | `held` waits until each held call's answer is computed; `stale(at)` fails the test when one was computed after the writes began, or still was being | 44917808169: train-130's test with a 2 s answer and marks not journaled passes (the answer carried the writes); 44917813724: the old `held` trips the new check ("still being computed") | 44917810968: the new test fails the same broken merge ("C0FAKE00001 marked read is unread again"); 44917816778: passes with a 2 s answer |

### Flakes (round 2)

| test | cause | fix | red | green |
|---|---|---|---|---|
| TestTautZeroCost | `testing.AllocsPerRun` counts the whole process's allocations: one burst elsewhere during the count raised its integer mean | the fewest of five counts, each side | 44924146874: train-130's test, a 300-allocation burst during the count: 53 vs 50 | 44924153026: the same burst, 50 vs 50; 44923396796: an idle Taut that allocates every frame still fails (54 vs 50) |
| TestClackRollbackKeys, `eglInitialize` 0x3001 | every headless window brought `EGL_DEFAULT_DISPLAY` up (a new X connection) and its Release terminated it, under any other window too; Mesa failed one of those inits (train-129, linux-gio 136849179543; none in ~500, 44922213522) | gio patch 55: the default display stays initialized for the process | 44924405540: TestClackWindowOutlivesAnother on the old egl.go, `eglMakeCurrent error 0x3008` | 44924410293: it passes, and gpu/headless's TestClack tests 20 times over |
| TestFrameCostDamage | `:explodes:` in view moved on between draws of the same ops on a slow WARP runner (44434705984) despite `SetAnimate(false)` | `images.HoldLive(true)` through the still scenes, off before the emoji's part | not reproduced on Linux: the old test passed with animations on (44923877407) | 44923882764 (Linux); all three OSes: 44925245368 |
| TestMessageMenuOldMenuRows | not reproduced | the test now says what its click aimed at and what the rows are when the click never comes | | |
| TestRealWindowCaretCost | a second failure seen: "readback: no blink's partial frame in 2.4s" at 1440x900 (Linux, 44923882764), after its whole-frames turns; 3 of 13 partial frames there damaged more than a caret | open | | |
| TestMacIdleCPU | "channel again: 20 frames in 10 s" (once, 11-21) | open | | |
| TestSnapshotsWhileBusy (jsrt), new | read the runtime's snapshot the instant its Do returned, but the task's microtask checkpoint and bookkeeping run after (`runTask`): 2 ms busy still under the full suite (44928390402, release dry run 44942336766) | wait for the runtime to go idle | 44945131657: train-130's test against a runtime still busy 20 ms after its Do: 3 of 3 fail at the idle check | 44945137083: 3 of 3 pass |

### The runners lost on 2026-11-21

Two full-test jobs lost their runner: train-126's (136813058254) "lost
communication with the server" after 66 min, its log gone (404), and
train-130's attempt 1 (136854209282) "the runner has received a shutdown
signal", exit 143, at 22.1 min. Nothing in either log tells infra loss from
the suite starving the runner: no memory or load was recorded. The other 41
full-test jobs of the day ran 21.7-24.9 min; their five failures were tests
(TestSocketGapListDrawsEveryMessage, TestPreviewFooterUnderComposerPlugin,
TestReadsAnnounce and two more), not runners. full-test and release's test
job now log the runner's memory, swap, load and three biggest processes
each minute (`runner-load.sh`), and the first run with it says it's the
suite. In a normal green full-test (44925236847) the memory in use climbed
to **15,686 MiB of the runner's 15,983, with no swap**, near minute 22:
ui.test alone at 13.9 GB, the minute train-130's attempt 1 lost its runner.
internal/ui alone (flake-check, 44928443199) climbs the same way, and its
heap profile at the end still holds 9,053 MB: every test's op buffers
(2.3 GB), font faces (1.3 GB), images (0.7 GB), App subscriptions (0.6 GB),
rich text styles, fakeslack histories. The ui tests' Apps are never let go.
full-test and release's test job now run go test with `GOMEMLIMIT=11GiB`,
above ui.test's live heap: the peak was 11,612 MiB, internal/ui took
1,196 s against 1,187 and the job 23.0 min (44942313462; again green in
44942310732). 7GiB held it to 10,330 MiB (44928390402), but under the live
heap the GC ran back to back at half the CPU in the ui tests' last minutes,
and TestJoinBanner's 10 s wait ran out (44939446171). The fix after this
one is to let each test's App go.

### Item 8: perf.yml as a regression gate

Karim (#clack): "What about adding stuff like codspeed or an action to see
if you regressed". perf.yml already ran base and head interleaved on one
runner (`.github/perf/README.md`), but nothing failed when a number got
worse.

- **The gate** (`.github/perf/gate.py`, the perf job's last step on a
  dispatch): each gated metric, head against base, fails when head is worse
  by more than that metric's noise band, relative and absolute. Lower is
  better for all of them; only worse fails. The summary has a line per
  metric: which one, base → head, by how much, and its band. The metrics:
  frame time p50 and p95 of a scroll, a typing and an idle frame (new:
  `frameTimes` in `internal/ui/perf_test.go` records each op; each side
  runs those three benchmarks three times, interleaved, and the gate reads
  its median: in four runs of single passes, one slow stretch of the runner
  put an unchanged tree's typing frame 13.4% apart), idle CPU
  (new: the real binary's CPU from 20 to 95 s idle, `process.sh`), idle
  frames (BenchmarkIdleFrame's frames that ask for another), allocations a
  frame (the three), and memory (the real binary's idle RSS and Go heap at
  95 s, BenchmarkSessionMemory's heap). Pushes to ui and main report and
  don't gate: their base is the last release, not a branch point.
- **The bands** (`.github/perf/noise.json`): from base-vs-base runs (both
  sides the same commit: `-f base=<its sha> -f force=true -f jobs=perf`),
  each metric's band is 1.5 times the widest gap they showed, never under
  its floor (5% for frame times, 10% for idle CPU, 2% for allocations, 3%
  for memory, with an absolute floor each). `gate.py sample` leaves each
  run's numbers in its artifact (`gate-sample.json`) and `gate.py
  calibrate` turns samples into the file. Six base-vs-base runs
  (44934859075, 44934861506, 44934864117, 44934866471, 44934868863,
  44934870872) gave: frame medians and p95s ±5 to 11.5% (the typing frame's
  median ±21.5%: one run's head had two of its three typing passes in a slow
  stretch; its p95 is ±5%), allocations ±2 to 5%, memory ±3 to 10%, idle CPU
  ±76% (about 1 ms/s, ±0.5), idle frames exact. Four earlier runs of single
  passes (44932200567, 44932203634, 44932207050, 44932210304) were wider
  still, up to 13.4% on a frame median, which is why the gated benchmarks
  now run three times a side.
- **The skip**: a first job (`affects.py`) works out the base and fails
  nothing: a dispatch whose diff from it touches only docs, tests (`*_test.go`,
  testdata) and CI files (`.github/`) can't move a number (both sides run
  HEAD's harness and fakeslackd), so every perf job skips and the summary
  says why; `-f force=true` runs them anyway. A branch's default base is now
  where it left ui (as the plugins job's already was), so the numbers say
  what the branch changed; ui and main keep the newest release tag.
  `-f jobs=perf` runs the gated job alone (no scale or plugins jobs).
- **Red/green** (count 3 each): a 20 ms sleep at the top of `App.frame`
  failed the gate in all three runs, at the gate step alone (44939402075:
  5 metrics, scroll median +18.8%, scroll p95 +19.1%, typing p95 +16.1%,
  idle median +13.3%, idle p95 +11.9%; 44939406672: 4; 44939411243: 5);
  the unchanged tree passed all three (44939404419, 44939409028,
  44939413760: every metric inside its band, the widest scroll p95 at
  -6.2% of ±11.5%). ci-speed-2's own perf run against where it left ui
  passed (44939450567).

**CodSpeed, considered and not used.** Its Go support measures wall time
only and is early, its numbers are stable on its own bare-metal runners
(on GitHub's shared ones they'd have the noise this gate measures), and it
reports on pull requests, where Clack works on branches and dispatches.
perf.yml already has what a gate needs: both sides on one runner,
interleaved, each built from its own tree.

## Round 3

Branch `ci-speed-3` (2026-11-21), from ui at 821a5101 (α.133) and merged
with ui since. As in round 2, each red and green run is on an evidence
branch `ci-speed-3-*` whose head commit carries `[skip ci]`, so pushing it
starts nothing; the branches are deleted afterwards and the runs stay.
From the EGL item on, a proof is one run that passes only when the fix
passes and its mutant fails as named (taut-flush-proof), so a red proof no
longer shows up as a failed run.

### The ui tests let each finished App go

internal/ui's tests make over 500 Apps in one process, 509 from startEnv
alone, and before this round none was ever collected. By the end ui.test
held 9.1 GB live and the runner reached 15.6 of its 16 GB. Reading the code
found the first roots. A tool found the rest. `internal/ui/retain_test.go`
writes a heap dump with `runtime/debug.WriteHeapDump`, reads it back in the
same process and prints every root that reaches a given object, with the
shortest path. A root is a global, a goroutine's frame or a timer. It names each object
on the path by the symbols its words hold: a closure's function, an itab
like `go:itab.*ui.App,ui/messages.Host`. `go test` links its temporary
binary with `-s -w`, so globals print as bare addresses there; build with
`go test -c` to get their names.

What held the Apps, in the order the dumps found it:

1. Package maps keyed by the session: core's 33 maps keyed by `*App`
   (now `newPerApp`, which `Release` empties), fakeslack's 27 keyed by
   `*Server`, store's three keyed by `*Store`, the shaper caches in theme,
   messages, blockkit and slackc/kit, messages' hubs, blockkit's app DMs
   and message hosts, popups', views' and composer's registries, and
   fourteen sync.Maps and four maps keyed by `*App` in internal/ui
   (`release`; `TestReleaseCoversAppMaps` reads the package for any it
   misses).
2. Goroutines ranging over channels nobody closed: per App an invalidator,
   an image cache sweep, core's pump, the Interactive's raw feed, messages'
   hub, composer's typing, blockkit's views, the backend's Taut and Slick
   watchers. Core's `Release` and Taut's and Slick's `Close` now close
   every channel they handed out. A subscription's stop used to delete its
   channel without closing it, so a consumer stopped before `Release`
   (blockkit's, from `backend.stop`) waited forever. Now whoever takes a
   channel out of the set closes it, under the lock its sends hold.
3. Listener lists nothing removed from: `i18n.OnChange` and `onOSScheme`
   kept every App's callback. Both return a stop now, and `release` runs
   the App's.
4. Caches keyed by conversation that kept the last session's entries:
   composer's `attStore` and messages' `listCache` (with `lastChannel`,
   `lastThread`, `paneLast`, `lastHost`). These held more Apps than any
   other root in the first leak run. `composer.ForgetCore` and
   `messages.ForgetCore` now drop them, from `backend.stop` too, so a
   sign-out or an account switch lets the old session go in the real app
   as well.
5. Lookups after `Release` that put the App back. Something still ran
   `LoadOrStore` for an App or a store after its `Release` (a goroutine
   finishing late, or a stale list drawn). A released App's or store's
   entries are no longer kept, and a late subscriber gets a closed
   channel.
6. Timers: core's temporary-mute expiry, and the image cache's animation
   sweep, which re-arms itself while a kept player plays and nothing
   draws. `Release` and `Cache.Close` stop them.

`TestFinishedSessionIsCollected` guards all of it: two sessions in a row,
and the first one's App, core App, fake server and shaper must be
collected within 10 s. It checks the first of two because `ui.New` points
process-wide hooks at the newest App (one App per process). On a failure it
prints the goroutines the sessions left and the heap dump's roots. Red:
44958300566 (blockkit's goroutine and `blockkit.notDM`), 44959106725
(`blockkit.notDM`). Green: 44959763759.

To see what holds the Apps across the whole package, flake-check's
release-leaks job runs internal/ui in four parts by test name. Each part
traces the first live App of every test that leaves one, and the end-of-run
report (`CLACK_TEST_HEAP_REPORT`, `CLACK_RETAIN_DUMP=n`) groups the live
Apps by test. Its first run (44962912991) found 265 of 509 Apps live and
the roots in items 4 to 6. The second (44964925469) found 28.

internal/ui alone, without GOMEMLIMIT (flake-check ui-heap):

| run | what changed | live heap at the end | Apps live | ui.test peak | runner peak | time |
|---|---|---|---|---|---|---|
| 44950485950 | none, the measurement only | 9,106 MiB | all | 14,871 MiB | 15,628 MiB | 1,236 s |
| 44950488360 | item 1, core and fakeslack | 8,577 MiB | | | 15,201 MiB | 1,227 s |
| 44959766093 | items 1 to 3 | 4,846 MiB | 248 of 509 | 9,501 MiB | 10,315 MiB | 1,236 s |
| 44964928177 | items 1 to 5, part of 6 | 1,846 MiB | 15 of 509 | 4,078 MiB | 4,946 MiB | 1,218 s |
| 44966386962 | all six | 1,798 MiB | 13 of 509 | 4,206 MiB | 5,024 MiB | 1,231 s |

full-test (`go test ./...`, one job), without GOMEMLIMIT:

| run | what changed | runner peak | ui.test peak | job | internal/ui |
|---|---|---|---|---|---|
| 44925236847 (round 2, no limit) | none | 15,686 MiB | 13.9 GB | | |
| 44960051436 | items 1 to 3 | 10,468 MiB | 9,575 MiB | 24.7 min | 1,239 s |
| 44966389317 | all six | 8,124 MiB | 4,220 MiB | 23.9 min | 1,212 s |

So the GOMEMLIMIT=11GiB stopgap from round 2 is gone from full-test and
release's test job. Round 2's full-test with the limit peaked at 11,612 MiB
in 23.0 min (44942313462). Without it, the peak minute is now other
packages beside ui.test: messages.test at 3,400 MiB and views.test at
2,575 MiB in 44966389317. Their own tests keep sessions the same way, and
the same tools will find what holds them.

The second leak run shows what's left. A few single slots hold the newest
App that used them (`canvasview.copyText`, `slackc.stack`,
`popups.epCoreApp`, `popups.stackCur`). wazero's compile cache is
process-wide on purpose. A few tests end with an upload in flight or a
Slick goroutine still running. That's a handful of Apps, not the package.

### Flakes (round 3)

| test | cause | fix | red | green |
|---|---|---|---|---|
| TestCornerDockButton (videoplayer), α.133's release test job (44948141967) | Not GC pressure. The sampler had the runner at 2.8 to 3.9 GB, ui.test at 0.5 to 1.3 GB and the load at 4.7 to 5.6 on 4 vCPUs while videoplayer ran: CPU. The test gave the mini player 5 s of wall time to play | the waits read the tile's own state: while it loads they wait (a minute for a hang), and one that neither loads nor plays for 5 s fails | 44953782259: a fake player whose second play takes 6 s, 3 of 3 fail | 44953787134: the same player, 3 of 3 pass |
| TestOnePlaysAtATime (videoplayer), train-143's linux-test rest shard (44970400272), "no the second playing within 5s" | A tile is Playing from its play event, which the fake sends at once, before its data. The test clicked the second video on the first's play event, and when the first's data came its start paused the second, as Slack's coordinator does (an autoplay's start pauses the others). The second, paused while it waited, never played. A loaded runner made the first's metadata that late. Plain CPU load didn't (44973568440: 0 of 18 with the old waits under twice the CPUs busy) | the test waits for the first to play, its spinner gone. A subtest with the first's data late gives the order every time. Every tile wait in the package is now `waitForTile`: its limit counts only while the video neither loads nor is what the test waits for, and a failure prints the tile's and the player's state | 44974193400: the mutant, "no the second playing": the second paused, all its data in (ready 4), no event dropped | 44974193400: 10 times under -race, the package 3 times under load |
| TestRealWindowCaretCost at 1440x900, "no blink's partial frame in 2.4s" | The readback started right after partial frames came back on. The window takes a few frames to settle: a fresh frame texture fills, the sticky atlas's first repack makes the next frame's damage too big (straight into the window), and two more fill the texture again. At 1440x900 that took four blinks, the whole wait. `gpu.FrameStats.Whole` (new) says why each frame was whole | the readback waits for the first partial frame first | 44960352218: 2 of 25; 44963639630: 1 of 25, with the four reasons. The earlier 0 of 35 (44950757123, 44952864910) ran the 800x600 mode | 44965770262: 0 of 25 |
| TestPatchedReactionPillLikeNative/dark/cls (train-138, 44960329088) | Not EGL. linux-test has no display, so every pixel subtest gets `eglInitialize` 0x3001 and skips (all eight did). This one failed because its TempDir cleanup raced Taut's debounced data flush, "directory not empty". The timer's flush took the dirty data under the lock and wrote it after, so Close's last flush found nothing and returned while storage/ was still being written | Taut's flushes run one at a time, writes included, so Close returns with its data on disk. It also stops an older flush's rename from landing over a newer one's | 44963637245, the mutant: "a flush returned while another was still writing" | 44963637245: 20 times under -race |
| TestMacIdleCPU | not reproduced | the count failures print the pass's frame timeline: each frame, when it came, what asked for it, and the caret's blink interval | | |

## Round 3 (quiet-ci): red only when something is wrong

Every run here starts from Karim's account, so every failed run is a push
notification on their phone: 194 in the 24 h to 2026-11-21 23:40Z. Most were
red by design (proofs) or not about the change (flakes, a base that was red).

### Proof runs (expect_fail)

A proof (a red or mutant branch, the tests on the old code) names the tests
it proves fail, and the run passes only if each of them fails and nothing
else does:

```sh
# flake-check, one job: the usual proof
gh workflow run flake-check.yml --repo some-du6e/clack --ref <branch>-red1 -f only=<job> -f count=1 -f expect_fail=TestA,TestB/sub
# an area workflow that takes it (threads-audit, files-view, hover-card, bk-gallery-shots,
# ui-taut, ch-details-shots, ui-slick, composer-taut, bk-agent-shots)
gh workflow run threads-audit.yml --repo some-du6e/clack --ref <branch>-red1 -f expect_fail=TestA
# ci: any of its only= jobs (linux-test runs the whole suite in its five shards)
gh workflow run ci.yml --repo some-du6e/clack --ref <branch>-red1 -f only=linux-test -f expect_fail=TestA
```

- The names are as `go test -json` reports them: `TestA`, a subtest
  `TestA/sub` (spaces become `_`), comma-separated. A listed test covers its
  subtests; a listed subtest's failure also fails its parents, which is fine
  unless a parent printed lines of its own (its own `t.Error`, or a `t.Log`:
  test2json can't tell them apart): then list the parent too.
- Each job that sets up Go runs `.github/actions/expect-fail` right after
  setup-go: it builds `tools/expectfail` and sets `GOFLAGS=-exec=<it>`, so
  every `go test` in the job (text or `-json`, `xvfb-run`, test-shard.py)
  runs its binary through it. It judges each binary: listed failures exit 0;
  a listed test that passes or is skipped (in any of its `-count` runs), any
  other failed test, a crash or timeout outside a listed test, or a binary
  that fails with no failed test (TestMain) keeps it red. Build and vet
  errors stop `go test` before the binary, so they stay red too: a proof
  that doesn't compile proves nothing.
- Each job ends with `.github/actions/expect-fail-report` (its summary lists
  each listed test it ran with the first lines of its failure, and uploads
  its records), and an `expect-fail` job, needing every job, checks across
  them: every listed test failed somewhere and passed nowhere, every job
  that ran succeeded. Its summary is the proof, readable without the log.
  A step without `-v` shows an expected failure only there: to `go test`
  the package passed, so it prints nothing of it.
- Not judged: binaries a job builds with `go test -c` and runs itself
  (flake-check's Windows fanouts, some stress jobs), and a step that sets
  `XF_OFF=1` because it needs a binary's real status (gio-layer-fix.sh's
  tests that must panic on stock Gio, in linux-gio and macos-idle). A
  listed failure there stays red, never a false pass.
- Rerunning a proof run's failed jobs: each record carries its leg (job and
  matrix index) and the run's attempt, and the check keeps each leg's
  latest, so the failed attempt's records don't count.
- A new job in one of these workflows needs the two steps and an entry in
  `expect-fail`'s needs; `tools/expectfail`'s TestWorkflowsWireExpectFail
  fails ci until it has them. After a merge (a train taking a branch made
  before quiet-ci), wire them with the script, which needs only Python:

  ```sh
  python3 -I .github/scripts/expect_fail_wire.py --check .github/workflows/flake-check.yml  # what's missing, exit 1
  python3 -I .github/scripts/expect_fail_wire.py .github/workflows/flake-check.yml          # add it, in place
  ```

  It adds the input, the setup step after setup-go, the report step last
  and the check job's needs, exactly as the guard wants them, and leaves a
  wired file byte for byte as it was. It refuses (exit 2) a file not laid
  out like the repo's (jobs at 2 spaces, steps at 6).

Proven on quiet-ci (throwaway branches, deleted): the listed test failing
alone was green (flake-check 44974452814; ci only=linux-test over the whole
suite 44976014400); a listed test that passed was red (44974455267,
"TestXFProofPasses passed (1 of 1 runs)"); an unlisted failure was red
(44974458023, "TestXFProofExtra failed, and expect_fail doesn't list it").

### The evidence-branch rule

A branch named like a proof, ending `-red`, `-redN`, `-redNx`, `-mutN`,
`-rNredN` or `-rNmutN` (`thread-load-speed-red4`, `ci-speed-3-red13c`,
`blockkit-coverage-r3red5`):

- starts no ci or supply-chain run on push (`branches-ignore`): its push run
  would only be red;
- fails at once, in the expect-fail setup step, when dispatched without
  `expect_fail`, with a message saying to pass it (44974460708). So every
  proof names what it expects to fail.

`expect_fail` on ui, main or a tag fails the same way: those pass only when
nothing fails. Diagnostic branches (`-probeN`, `-dbgN`) aren't covered: a
probe that is meant to fail is a proof, so name it `-redN` and list its
tests.

### Before a push (prepush.sh)

```sh
.github/ci/prepush.sh            # changed since the merge base with origin/ui
.github/ci/prepush.sh <base>     # or since <base>
```

It runs `gofmt -l` on the Go files changed since the base (and the working
tree's), then `go vet` on their packages, with the GOOS their `_windows` and
`_darwin` files need (Windows with cgo where mingw is installed). On swiss
vet goes through the go gate, so a package like internal/ui takes minutes.
Of the 24 h's agent-branch failures it would have caught 3, all of them
compile or vet errors (`method Modal.Rect already declared`, `not enough
arguments in call to a.jumpToMessage` in a test, vet's `unreachable code`);
gofmt had none.

### The release's test job

release.yml's test job is ci's full-test step for step (the same
`go test ./...` on the same runner image, a clean cache). On a tree that
already passed it, it adds no coverage, only another roll of the flakes:
alpha.135's tree had passed full-test on its squash source (409e358f, run
44955419010), and the release lost two attempts to flakes. It now looks for
a successful ci full-test on a commit with the tag's tree (prior-full-test.sh,
ci's runs of the last 3 days) and, finding one, skips the suite with a
summary linking that run; otherwise it runs as before. The tree, not the
sha: ui squashes a branch whose pre-merge full-test ran on the branch's
commit, and ui's own full-test is often cancelled. A proof run's green
full-test doesn't count (its expect-fail job ran: the listed failures exit
0, and a listed test that panics ends its package early), and any API
error runs the suite. It asks the server for ci's green dispatches and
ui and main pushes of the 3 days (a branch's push run skips full-test).
Not covered: nothing keeps ci's full-test and this job step for step
alike; a change to one should go to both.

### The 24 h of failures, classified

194 failed runs to 2026-11-21 23:40Z. Of the 104 not on an evidence branch
by the old naming: 4 on ui and main (menu-keys' macOS clipboard reads,
fixed on ui at a913a1fe; code-channels' TestTautZeroCost, fixed at
821a5101; the runs after both were cancelled), 5 on trains, 27 on proof or
diagnostic branches the old naming missed (`-red13c`, `-r3red5`, `-probe2`,
`-dbg1`, `-slow3`, `-nolimit`), and 68 on agent branches:

| class | runs | what |
|---|---|---|
| real, fixed later | 38 | the branch's own tests or jobs, passing on a later push |
| real | 8 | the branch's own tests, not passing again inside the window (quote-paste-crash's TestWinQuotePaste ×5, ci-speed-3's heap test, acrows-race, threads-ship) |
| flake | 19 | below |
| broken push | 3 | build or vet errors: prepush.sh |

The flakes, by test:

| test | runs | state |
|---|---|---|
| TestMenuKeysMessageActions, TestMenuTypeAhead (menu-keys, macOS) | 6 | red on every branch until ui a913a1fe |
| thread-open-root's own tests, the same sha green later | 4 | |
| TestThreadsRefreshGapLikeSlack | 2 | open: the next to fix (it also failed alpha.114's release) |
| TestPreviewFooterMembershipUnknownAtOpen, TestPreviewFooterUnderComposerPlugin | 2 | a real race, fixed: preview-footer-race (ResolveConv's joiners) |
| launch-budget (launch-speed), the same sha green later | 2 | |
| TestJoinBanner | 1 | GOMEMLIMIT=7GiB's GC (ci-speed-2 kept 11 GiB) |
| TestSnapshotsWhileBusy (jsrt) | 1 | |
| TestInspectPicksToast (windows-applog) | 1 | |

Fixed alongside, from the releases: TestMessageMenuOldMenuRows
(menu-click-rerender: slackc dropped a press's scope on a re-render) and
Media Foundation's first-frame timing in TestMFRGBA, TestMFPlaysFixture and
TestMFColdStart (mf-first-frame).

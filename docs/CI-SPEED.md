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
| **go.mod `go 1.24.9`** (no toolchain switch, so the cache restores) | every Linux and macOS restore failed | (measuring: run 44854132904) |
| **go-cache.yml**: on each main push, a build of everything the jobs build replaces main's setup-go entry for every OS | main's entries: a 288 MB (Linux) and 156 MB (macOS) partial build from 11-16 | (measuring: runs 44854133106 and the push after it) |
| **macos-idle beside macos** | macos 23.2 min with the idle processes (30.4 before) | (measuring: run 44854132904) |
| **apt.sh**: an update gets 5 min, an install 10, then the kernel.org mirror | 15 min a try, twice: 640-1,280 s steps | (a tail; shows over days) |
| **budgets**: 106 jobs' `timeout-minutes` from their p95 (1.5 x p95 + 3, at least 15) | GitHub's 360 min | a job past its budget fails |
| **menu-keys on macOS**: the test reads Copy message's rich copy (`writeRich`), which on macOS goes to the pasteboard, not through Gio | TestMenuKeysMessageActions and TestMenuTypeAhead red on every macOS run since 11-14 (35 failures, 14 branches) | (measuring: run 44854164035) |

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

## Tests: two weeks of failures

(in progress: 1,961 failed runs, 2,307 failed jobs, 2026-11-06..20,
flake-check and -red/-mut evidence branches left out)

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

(below)

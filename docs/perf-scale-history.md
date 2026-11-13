# Clack memory at Hack Club scale

One row per platform per run of perf.yml's scale jobs (fakeslackd -scale: 65,846 emoji entries, 32k users, 2,006 user groups, 300 channels, long channels with images). The stages are the driver's (internal/ui/perfdrive.go); memory is the private working set on Windows (Task Manager's Memory column; no GPU on the runner: the Basic Render Driver) and private RSS on Linux (llvmpipe).

| date | commit | branch | platform | startup | picker scrolled | long channels | +40 channels | idle 10 min | idle Go heap | idle stacks |
|---|---|---|---|---|---|---|---|---|---|---|
| 2026-11-13 | `ac9e898` | mem-diet | linux | 390 MB | 646 MB | 1644 MB | 1206 MB | — | — | — |
| 2026-11-13 | `ac9e898` | mem-diet | windows | 303 MB | 885 MB | 1067 MB | 1136 MB | 680 MB | 795 MB | 2 MB |
| 2026-11-13 | `3b864ed` | mem-diet | windows | 307 MB | 453 MB | 926 MB | 838 MB | 540 MB | 423 MB | 2 MB |
| 2026-11-13 | `c853691` | mem-diet | linux | 314 MB | 641 MB | 902 MB | 910 MB | 926 MB | 368 MB | 1 MB |
| 2026-11-13 | `c853691` | mem-diet | windows | 278 MB | 475 MB | 927 MB | 1070 MB | 474 MB | 490 MB | 2 MB |

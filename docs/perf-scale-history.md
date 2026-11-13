# Clack memory at Hack Club scale

One row per platform per run of perf.yml's scale jobs (fakeslackd -scale: 65,846 emoji entries, 32k users, 2,006 user groups, 300 channels, long channels with images). The stages are the driver's (internal/ui/perfdrive.go); memory is the private working set on Windows (Task Manager's Memory column; no GPU on the runner: the Basic Render Driver) and private RSS on Linux (llvmpipe).

| date | commit | branch | platform | startup | picker scrolled | long channels | +40 channels | idle 10 min | idle Go heap | idle stacks |
|---|---|---|---|---|---|---|---|---|---|---|
| 2026-11-13 | `ac9e898` | mem-diet | linux | 390 MB | 646 MB | 1644 MB | 1206 MB | — | — | — |
| 2026-11-13 | `ac9e898` | mem-diet | windows | 303 MB | 885 MB | 1067 MB | 1136 MB | 680 MB | 795 MB | 2 MB |
| 2026-11-13 | `3b864ed` | mem-diet | windows | 307 MB | 453 MB | 926 MB | 838 MB | 540 MB | 423 MB | 2 MB |
| 2026-11-13 | `c853691` | mem-diet | linux | 314 MB | 641 MB | 902 MB | 910 MB | 926 MB | 368 MB | 1 MB |
| 2026-11-13 | `c853691` | mem-diet | windows | 278 MB | 475 MB | 927 MB | 1070 MB | 474 MB | 490 MB | 2 MB |
| 2026-11-13 | `78cda35` | mem-diet | linux | 328 MB | 429 MB | 906 MB | 969 MB | 712 MB | 400 MB | 1 MB |
| 2026-11-13 | `78cda35` | mem-diet | windows | 298 MB | 488 MB | 906 MB | 917 MB | 462 MB | 288 MB | 2 MB |
| 2026-11-13 | `aedc760` | mem-diet | linux | 330 MB | 438 MB | 903 MB | 928 MB | 704 MB | 398 MB | 1 MB |
| 2026-11-13 | `aedc760` | mem-diet | windows | 273 MB | 434 MB | 894 MB | 851 MB | 547 MB | 314 MB | 2 MB |
| 2026-11-13 | `0d66c02` | mem-diet | linux | 296 MB | 649 MB | 835 MB | 853 MB | 630 MB | 321 MB | 1 MB |
| 2026-11-13 | `0d66c02` | mem-diet | windows | 309 MB | 480 MB | 933 MB | 1009 MB | 396 MB | 431 MB | 2 MB |
| 2026-11-13 | `27364e0` | mem-diet | linux | 311 MB | 642 MB | 805 MB | 910 MB | 651 MB | 343 MB | 1 MB |
| 2026-11-13 | `27364e0` | mem-diet | windows | 313 MB | 477 MB | 1068 MB | 958 MB | 389 MB | 422 MB | 2 MB |
| 2026-11-13 | `8d77d7e` | ui | linux | 300 MB | 580 MB | 937 MB | 937 MB | 738 MB | 442 MB | 1 MB |
| 2026-11-13 | `8d77d7e` | ui | windows | 262 MB | 545 MB | 965 MB | 836 MB | 430 MB | 320 MB | 2 MB |
| 2026-11-13 | `8d77d7e` | main | linux | 303 MB | 643 MB | 901 MB | 939 MB | 692 MB | 370 MB | 2 MB |
| 2026-11-13 | `8d77d7e` | main | windows | 260 MB | 457 MB | 876 MB | 1051 MB | 380 MB | 422 MB | 2 MB |

# Clack perf history

One row per push (perf workflow, GitHub runner, llvmpipe). Paint and RSS are the real binary on Xvfb against fakeslackd.

| date | commit | branch | warm first paint | cold first paint | idle RSS | session heap | channel switch | scroll frame | typing frame / allocs | picker open | binary |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 2026-11-07 | `7bf25ed` | perf | — | — | — | 113 MB | 228.91 ms | 115.94 ms | 118.74 ms / 2,749 | 274.04 ms | 30.9 |
| 2026-11-07 | `d3253a1` | perf | — | — | — | — | — | — | — / — | 297.65 ms | 39.3 |
| 2026-11-07 | `c81e9ff` | perf | — | — | — | 113 MB | 170.20 ms | 84.33 ms | 89.11 ms / 1,362 | 201.84 ms | 39.3 |
| 2026-11-08 | `7c37b4e` | perf | 347 ms | 1,161 ms | 286 MB | 113 MB | 296.93 ms | 147.76 ms | 157.21 ms / 1,437 | 356.19 ms | 39.3 |

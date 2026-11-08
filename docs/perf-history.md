# Clack perf history

One row per push (perf workflow, GitHub runner, llvmpipe). Paint and RSS are the real binary on Xvfb against fakeslackd.

| date | commit | branch | warm first paint | cold first paint | idle RSS | session heap | channel switch | scroll frame | typing frame / allocs | picker open | binary |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 2026-11-07 | `7bf25ed` | perf | — | — | — | 113 MB | 228.91 ms | 115.94 ms | 118.74 ms / 2,749 | 274.04 ms | 30.9 |
| 2026-11-07 | `d3253a1` | perf | — | — | — | — | — | — | — / — | 297.65 ms | 39.3 |
| 2026-11-07 | `c81e9ff` | perf | — | — | — | 113 MB | 170.20 ms | 84.33 ms | 89.11 ms / 1,362 | 201.84 ms | 39.3 |
| 2026-11-08 | `7c37b4e` | perf | 347 ms | 1,161 ms | 286 MB | 113 MB | 296.93 ms | 147.76 ms | 157.21 ms / 1,437 | 356.19 ms | 39.3 |
| 2026-11-08 | `37075a0` | ui | 353 ms | 4,869 ms | 286 MB | 113 MB | 307.04 ms | 147.53 ms | 152.86 ms / 1,437 | 344.90 ms | 39.4 |
| 2026-11-08 | `87258ae` | ui | 360 ms | 1,002 ms | 285 MB | 113 MB | 299.53 ms | 145.88 ms | 149.60 ms / 1,436 | 348.34 ms | 39.4 |
| 2026-11-08 | `165e108` | ui | 358 ms | 983 ms | 285 MB | 106 MB | 297.03 ms | 151.87 ms | 151.30 ms / 1,438 | 345.71 ms | 39.4 |
| 2026-11-08 | `fb420d7` | ui | 297 ms | 1,004 ms | 294 MB | 104 MB | 225.96 ms | 112.48 ms | 114.30 ms / 1,405 | 267.50 ms | 39.4 |
| 2026-11-08 | `19616b7` | ui | 336 ms | 14,693 ms | 299 MB | 106 MB | 291.56 ms | 153.68 ms | 151.99 ms / 1,438 | 350.23 ms | 39.4 |

# Clack perf history

One row per push (perf workflow, GitHub runner, llvmpipe). Paint and RSS are the real binary on Xvfb against fakeslackd.

| date | commit | branch | warm first paint | cold first paint | idle RSS | session heap | channel switch | scroll frame | typing frame / allocs | picker open | binary |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 2026-11-07 | `7bf25ed` | perf | — | — | — | 113 MB | 228.91 ms | 115.94 ms | 118.74 ms / 2,749 | 274.04 ms | 30.9 |

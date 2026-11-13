# Memory

Karim's Clack (alpha.45) used 1,248 MB on Windows on 2026-11-12, idle, by Task Manager's Memory column (the private working set). This page is how Clack's memory is measured, what held it, and the budget each cache now keeps.

## Measuring it

perf.yml's scale jobs (`jobs: scale` on a dispatch; also every push to ui and main) run the real binary at the real account's scale and record what holds the memory at each stage of a session. `.github/perf/README.md` has the details.

- **Workload.** `fakeslackd -scale` serves `fakeslack.HackClubScale`, the Hack Club Enterprise Grid at the account's counts: 65,846 emoji.list entries (5,846 of them aliases), 2,006 user groups, 32,000 users (all in the metadata cache), 300 channels and 120 DMs in the sidebar, three channels of 6,000 messages with an image every fifth, and text in other scripts. The homes run Karim-like plugins (ChatTabs and five Taut plugins).
- **Session.** The perfharness driver (`internal/ui/perfdrive.go`) goes through: startup settled, the emoji picker opened, the picker scrolled through every row, two long channels paged back to 3,000 messages and scrolled through, 40 more channels opened, then 10 minutes idle.
- **Numbers.** After each stage: runtime.MemStats, the OS's counters split by owner (Windows: private working set and commit through `QueryWorkingSet`, split into the Go heap's arenas, other private memory and DLLs; Linux: `/proc/self/smaps` sums), the app's caches, and a heap profile. `scale.py` attributes each allocation to the Clack code that asked for it.
- **History.** `docs/perf-scale-history.md` on the perf-history branch has a row per platform per run.

Neither runner has a GPU. On Windows, Gio's Direct3D 11 device runs on the Microsoft Basic Render Driver (WARP), which keeps textures and buffers in the process's private memory, as an integrated GPU's shared memory does; a discrete GPU keeps them in VRAM. The Go heap doesn't depend on the GPU.

## Budgets

Slack's web client bounds its own caches with a CacheManager (client-boot `WcSL`): about once an hour, when the objects it may evict reach `cacheSizeMaximum` it keeps the newest `cacheSizeTarget` and removes the rest. Clack follows Slack's numbers where Slack has a cache, and bounds its own caches by bytes.

| cache | budget | from |
|---|---|---|
| messages (store timelines and threads, the lists' models and rows, the plugin mirror) | 3,000 evictable messages or 3 MB of JSON, trimmed to 2,000 or 2 MB; evictable: read messages of conversations not on screen, everything in conversations the viewer isn't in, whole histories of quiet conversations not read for an hour; oldest first, recently read conversations last. Checked every 2 minutes (`core.EvictEvery`); a conversation read in the last 10 minutes is kept whole | Slack's `messages` CacheManager (`getUnusedMessages`, `evictUncountedChannelHistories`) |
| decoded images (`imgcache`) | 64 MB, unused for 60 s dropped | Clack |
| downloaded files (`core.Fetch`, memory only) | 48 MB, unused for 5 minutes dropped | Clack |
| image loads waiting | a queue per lane, 8 loads at a time | Clack |
| shaped words (`richview`) | two generations, each up to 8,000 words or 8 MB of glyphs and outlines | Clack |
| native styles (`slackc`) | two generations of 2,048 | Clack |
| Gio's shaped lines and outlines | 1,000 each per shaper | Gio |
| emboldened runs (`theme`) | 1,500 runs or 16 MB of outlines | Clack |
| emoji picker index | dropped 5 minutes after the last use | Clack |
| font glyphs | parsed as they're used; in-memory fonts' tables read in place | go-text patches (`third_party/typesetting`) |
| Go heap | soft limit 768 MiB (`GOMEMLIMIT` wins); a backstop, not a budget | Go runtime |

| users saved in `meta.json` | the viewer and the 1,000 fetched most recently (`store.MaxSavedUsers`); anyone else is fetched when a message, mention or profile needs them | Slack keeps no directory between starts |

Slack's other managers: `members` 700 → 500 (by last access), `channels` 200 → 100, `files` 250 → 200. Within a session Clack still keeps every user it fetched; bounding that like Slack's members cache is the next cut (it needs every lookup to fetch a missing user, which not all of the ~110 call sites do yet).

## What held the memory (2026-11-13)

perf.yml run 43367063510: the session above at Hack Club scale, both sides against the same fakeslackd and driven by the same driver. "Before" is ui as of 2026-11-12 (968e4619) with only the measurement added (ac9e898b). Windows, idle 10 minutes after the session, live Go heap 689 MB of 1,156 MB commit:

| what | before | after | change |
|---|---|---|---|
| word outlines (richview's shaped-word cache) | 126 MB | 13 MB | byte budget per generation (Gio patch 15) |
| messages: documents, rows, models, store, plugin trees | 145 MB | 48 MB | Slack's message cache budget |
| plugin JS engine (wasm heap) | 104 MB | 106 MB | none: plugins read the mirrored messages, and wasm memory never shrinks |
| fonts | 77 MB | 7 MB | glyphs parsed as used, embedded fonts read in place, fallback fonts mapped (go-text patches) |
| native styles (slackc) | 61 MB | 8 MB | two generations of 2,048 |
| users and emoji from meta.json | 48 MB | 28 MB | 1,000 saved users (the session fetched 25,600 again) |
| downloaded files | 41 MB | 3 MB | 5-minute sweep |
| goroutine structs | 17 MB | 0 | image loads in a queue (the picker scroll had parked 30,000 goroutines, 294 MB of stacks) |
| emoji picker index | 14 MB | 10 MB | the closed picker no longer pinned in the overlay slice |
| emboldened runs | 11 MB | 6 MB | 16 MB budget |
| everything else | 45 MB | 52 MB | |

Outside the Go heap, the runner's Direct3D device (WARP, no GPU) held 135 MB of private memory at startup and about 290 MB after the session, in fifteen 7–30 MB heaps; a real GPU keeps textures and buffers in its own memory.

Before and after, the same session (after: 78cda359, perf.yml run 43370191533; before: run 43367063510's ac9e898b side):

| Windows | startup | picker scrolled | long channels | +40 channels | idle 10 min | idle, after GC + trim |
|---|---|---|---|---|---|---|
| private working set, before | 310 MB | 865 MB | 1,496 MB | 1,145 MB | 633 MB | 621 MB |
| private working set, after | 298 MB | 488 MB | 906 MB | 917 MB | 462 MB | 458 MB |
| commit, before | 335 MB | 1,009 MB | 2,380 MB | 1,264 MB | 1,156 MB | — |
| commit, after | 286 MB | 564 MB | 1,090 MB | 1,141 MB | 691 MB | 667 MB |
| live Go heap, before | 135 MB | 232 MB | 894 MB | 756 MB | 695 MB | |
| live Go heap, after | 85 MB | 152 MB | 493 MB | 542 MB | 264 MB | |

| Linux (llvmpipe) | startup | picker scrolled | long channels | +40 channels | idle 10 min | idle, after GC + trim |
|---|---|---|---|---|---|---|
| private, before | 334 MB | 731 MB | 945 MB | 1,191 MB | 986 MB | 944 MB |
| private, after | 328 MB | 429 MB | 906 MB | 969 MB | 712 MB | 641 MB |
| live Go heap, before | 144 MB | 277 MB | 568 MB | 609 MB | 603 MB | |
| live Go heap, after | 103 MB | 232 MB | 519 MB | 479 MB | 326 MB | |

Of the 667 MB Windows commit left at idle, about 235 MB is the WARP device's and 244 MB the Go heap's; most of the rest of the Go heap is the plugin engine (106 MB), the users the session fetched (28 MB) and the kept messages (48 MB).

Before 78cda359 the idle numbers of the after side caught an eviction pass's garbage (Windows: 205 MB not yet collected; Linux: 241 MB collected but not yet returned); the trim loop now collects it.

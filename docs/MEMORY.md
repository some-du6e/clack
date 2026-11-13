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
| native styles (`slackc`) | two generations of 4,096 | Clack |
| Gio's shaped lines and outlines | 1,000 each per shaper | Gio |
| emboldened runs (`theme`) | 1,500 runs or 16 MB of outlines | Clack |
| emoji picker index | dropped 5 minutes after the last use | Clack |
| font glyphs | parsed as they're used; in-memory fonts' tables read in place | go-text patches (`third_party/typesetting`) |
| Go heap | soft limit 512 MiB (`GOMEMLIMIT` wins); a backstop, not a budget | Go runtime |

Slack's other managers: `members` 700 → 500 (by last access), `channels` 200 → 100, `files` 250 → 200. Clack keeps every user it has seen (and saves them to `meta.json`); the members bound is the next cut.

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
| members (the users the store holds; the plugin mirror's `members`) | 700 members, trimmed to 500: the viewer and whoever lookups found most recently (message and sidebar rows touch theirs as they draw); ids users/info didn't return count. Checked on a lookup (and every 2 minutes) once an hour ±20% has passed since the last pass, while online; a member it let go is fetched again at its next lookup (below) | Slack's `members` CacheManager (`compare_members_store_cache_objects`) and UnknownFetchManager |
| messages (store timelines and threads, the lists' models and rows, the plugin mirror) | 3,000 evictable messages or 3 MB of JSON, trimmed to 2,000 or 2 MB; evictable: read messages of conversations not on screen, everything in conversations the viewer isn't in, whole histories of quiet conversations not read for an hour; oldest first, recently read conversations last. Checked every 2 minutes (`core.EvictEvery`); a conversation read in the last 10 minutes is kept whole; a pass that lets 1,000 or more go is collected 2 s later (`core.AfterEvict`) | Slack's `messages` CacheManager (`getUnusedMessages`, `evictUncountedChannelHistories`) |
| decoded images (`imgcache`) | 64 MB, unused for 60 s dropped. Animated GIFs count in it: each one's first frame like any image, its encoded bytes (at most a quarter of the budget, dropped first) and, only while something draws it, its player (below) | Clack |
| downloaded files (`core.Fetch`, memory only) | 48 MB, unused for 5 minutes dropped | Clack |
| image loads waiting | a queue per lane, 8 loads at a time | Clack |
| shaped words (`richview`) | two generations, each up to 8,000 words or 8 MB of glyphs and outlines | Clack |
| native styles (`slackc`) | two generations of 2,048 | Clack |
| Gio's shaped lines and outlines | 1,000 each per shaper | Gio |
| emboldened runs (`theme`) | 1,500 runs or 16 MB of outlines | Clack |
| emoji picker index | dropped 5 minutes after the last use | Clack |
| font glyphs | parsed as they're used; in-memory fonts' tables read in place | go-text patches (`third_party/typesetting`) |
| Go heap | soft limit 768 MiB (`GOMEMLIMIT` wins); a backstop, not a budget | Go runtime |
| users saved in `meta.json` | the viewer and the 1,000 fetched most recently (`store.MaxSavedUsers`); anyone else is fetched when a message, mention or profile needs them | Slack persists its members store (`ukWt` persistenceEnabled), which its members cache bounds |

Slack's other managers: `channels` 200 → 100, `files` 250 → 200.

## What held the memory (2026-11-13)

perf.yml run 43397108627: the session above at Hack Club scale, both sides in the same run against the same fakeslackd and driven by the same driver. "Before" is ui as of 2026-11-12 (968e4619) with only the measurement added (ac9e898b); "after" is mem-diet at 27364e0c. Windows, idle 10 minutes after the session, the heap profile's live Go heap by the Clack code that allocated it:

| what | before | after | what changed |
|---|---|---|---|
| word outlines (richview's shaped-word cache) | 112 MB | 9 MB | a byte budget per generation (Gio patch 16) |
| native styles (slackc) | 104 MB | 11 MB | two generations of 2,048 |
| messages: documents, rows, models, store, plugin trees | 133 MB | 56 MB | Slack's message cache budget |
| plugin JS engine (wasm heap) | 107 MB | 107 MB | nothing: plugins read the mirrored messages, and wasm memory never shrinks |
| fonts | 68 MB | 12 MB | glyphs parsed as used, embedded fonts read in place, fallback fonts mapped (go-text patches) |
| users and emoji | 58 MB | 57 MB | at startup 32,000 users → 6,800 (1,000 from meta.json, the rest fetched as messages needed them); by the idle stage the session had fetched 25,600 again |
| saving meta.json | 64 MB | 0 | users streamed to the file one at a time (the snapshot caught a save) |
| downloaded files | 42 MB | 3 MB | 5-minute sweep |
| goroutine structs | 14 MB | 0 | image loads in a queue (the picker scroll had parked 30,000 goroutines, 294 MB of stacks) |
| emoji picker index | 8 MB | 0.5 MB | a closed picker is let go (below), so its index goes 5 minutes after the last use |
| emboldened runs | 10 MB | 4 MB | 16 MB budget |
| decoded images | 3 MB | 2 MB | |
| everything else | 42 MB | 29 MB | |
| **live Go heap** | **764 MB** | **291 MB** | |

A closed emoji picker (and so its index) stayed reachable three ways, each holding the last of something: the theme's shared text shaper kept the reader of the last editor it shaped (Gio patch 19), Gio's router and renderer refilled per-frame arrays without clearing what a longer earlier frame left past the end (patch 18: tags, and images' pixels), and the popups stacking order kept the last frame's overlays (`popups.ForgetDismissed`). `internal/ui` `TestClosedEmojiPickerReleased` checks it, and names the chain of fields when something still holds one.

Before and after, the same run:

| Windows | startup | picker scrolled | long channels | +40 channels | idle 10 min | idle, after GC + trim |
|---|---|---|---|---|---|---|
| private working set, before | 367 MB | 763 MB | 1,976 MB | 1,542 MB | 669 MB | 663 MB |
| private working set, after | 313 MB | 477 MB | 1,068 MB | 958 MB | 389 MB | 384 MB |
| commit, before | 329 MB | 1,082 MB | 2,106 MB | 2,371 MB | 1,414 MB | 1,254 MB |
| commit, after | 268 MB | 595 MB | 1,101 MB | 1,117 MB | 775 MB | 631 MB |
| live Go heap, before | 135 MB | 209 MB | 761 MB | 966 MB | 786 MB | |
| live Go heap, after | 83 MB | 155 MB | 491 MB | 578 MB | 271 MB | |

| Linux (llvmpipe) | startup | picker scrolled | long channels | +40 channels | idle 10 min | idle, after GC + trim |
|---|---|---|---|---|---|---|
| private, before | 335 MB | 471 MB | 1,414 MB | 1,358 MB | 1,018 MB | 963 MB |
| private, after | 311 MB | 642 MB | 805 MB | 910 MB | 651 MB | 612 MB |
| live Go heap, before | 143 MB | 276 MB | 569 MB | 617 MB | 609 MB | |
| live Go heap, after | 97 MB | 236 MB | 492 MB | 458 MB | 290 MB | |

Karim's 1,248 MB is about the before side's commit: with memory to spare Windows doesn't trim a working set, so Task Manager's column reads close to commit. After a collection the after side commits 631 MB at idle: the Go heap's arenas 359 MB (271 MB of live objects and 80 MB free inside spans that still hold some: Go doesn't move objects) and 253 MB of private memory outside the Go heap (322 MB on the before side; 154 MB at startup on both). On the runner most of that is most likely the Direct3D device on WARP, which grows with what was drawn (the picker, the long channels) and keeps in the process the textures and buffers a GPU holds in its own memory; Clack's own C code (huddle audio and video) runs only in a huddle.

The idle numbers move between runs with where the snapshot falls against the last message cache pass: the 40 channels opened before the idle stage leave the 10-minute keep window right at the snapshot, and a pass that lets 4,000–5,000 messages go can run seconds before it (since 27364e0c that pass is collected 2 s later). The same session in four runs:

| run | after side | private working set, as is / after GC | commit, as is / after GC | live Go heap | before side (ac9e898b): working set / commit after GC |
|---|---|---|---|---|---|
| 43370191533 | 78cda359 | 462 / 458 MB | 691 / 667 MB | 264 MB | (not run) |
| 43374716594 | aedc7606 | 547 / 542 MB | 714 / 703 MB | 302 MB | 624 / 1,151 MB |
| 43391221183 | 0d66c025 | 396 / 390 MB | 800 / 659 MB | 280 MB | 649 / 1,246 MB |
| 43397108627 | 27364e0c | 389 / 384 MB | 775 / 631 MB | 271 MB | 663 / 1,254 MB |

## The members cache (2026-11-13)

Slack's web client bounds the members it holds with the same CacheManager as its messages (client-boot gIKW and WcSL, build 0e695dd18302fa08). Clack now does the same (`store/members.go`, `core/members.go`):

- **Size.** Once the store holds 700 members (`cacheSizeMaximum`), the pass keeps 500 (`cacheSizeTarget`) and removes the rest (`bulkRemoveMembersById` with `isCacheEviction`). Ids users/info didn't return are stored as non-existent members and count, sort and go like the others.
- **What stays.** `compare_members_store_cache_objects` sorts the viewer (`is_self`) last and everyone else by `lastAccess`, oldest first; a member not found since it was stored counts as 0. `lastAccess` is set by `getMemberById`, which every component that shows a member calls on each render: the sidebar's DM rows, the message rows on screen, the typing line, a huddle's people, an open profile. So Slack pins only the viewer, and the rest stays because something keeps reading it. In Clack every lookup through `store.User` records the access (`MemberAvatar`, `UserName`, `MemberName` and the views' `core.User` included), message rows and sidebar DM rows touch their members as they draw (`TouchMember`), since they draw from what they resolved earlier, and a pass first touches the sidebar's people (Slack's sidebar sections read every DM's and group DM's members on each store change; Clack's sidebar rules read them at each snapshot). Reads of the cache as a whole (search over the directory, the Taut mirror's `state.members`) don't count, as `getRawMemberById` and Slack's selectors over every member don't.
- **When.** Every `recordHit` and `recordMiss` checks whether `evictionInterval` (an hour with ±20% jitter, drawn once per session) has passed since the last check while the client is online (`isOnline`: `clientOnline` and `serviceOnline`); the pass then runs in an idle callback and the next one is an interval later. Clack checks on every lookup and in the 2-minute cache loop (standing in for the lookups Slack's mounted components make on every store change while Clack draws nothing). Online is the machine's network (`core/online.go`) and a session that wasn't ended.
- **Fetching a missing member.** `getMemberById` on a member the store doesn't hold returns an unknown member (its id for a name, no display or real name, the unknown avatar; Clack's rows draw the id) and asks the UnknownFetchManager (Gx04) for it: the first miss after a quiet spell is fetched on the next tick, later ones once 200 ms have passed since the previous request began, one request at a time, every pending id in it. `fetchMembersForTeamId` leaves out ids the store got meanwhile and calls users/info over flannel (`fetchMembersById`: chunks of 100 ids, two in flight, `check_interaction`, `include_profile_only_users`, `updated_ids` at 0; `pending_ids` asked again up to three times, 500 ms × the round apart); the fetch gets 65 s and a timed-out one is tried up to three times. What comes back is upserted and the rows draw again ("names" in Clack); what doesn't becomes non-existent. `users.list` isn't used for this, and neither is the Web API's `users.info` (Slack's `fetchRawMembersFromApi`, 250 ids a call, is only for clients without the flannel socket). The reasons Slack passes (`UnknownFetchManager`, `flannel-call:users/info`) only label its logs. Slack also treats a request that failed outright as "not returned"; Clack asks for those again at the next lookup instead, since it has no members subscription to refresh them when the socket comes back.
- **The views.** About 280 places read a user: in core, the UI root, messages, popups, views, listview, blockkit, the composer, huddleui, the slackc kit, richtext's resolvers and the Taut mirror. Each one that shows a user goes through `store.User` (directly, or through `core.User`, popups' `lookupUser`, the views' `base.user` and the resolvers), which asks for a missing member itself; logic over the whole cache reads it raw. The ask-once paths that went around it are gone (messages' and the kit's `ensureUser`, the backend's avatar map), "names" makes message rows resolve their senders again (the messages hub bumped its users revision only for user, presence, emoji and prefs events), and `members_evicted` makes the sidebar and the rows read their members again, as Slack's components re-render on the removal. The Taut mirror reads `state.members` raw and drops evicted members, and its `ensureMembersArePresent` thunk makes the JS side read the members it fetched (a plugin's `members.getMember` saw the absent entry it had cached until its 3 s retry; `internal/taut` `TestGetMemberFetchesMissing`). `internal/ui` `TestEvictedMembersComeBack` evicts everyone but the viewer with each main view open (channel, thread, DMs, Activity, search, profile, the Huddles page) and checks that every member name drawn before is drawn again and none is left drawn as its id.

### Measured

perf.yml run 43426985732 (`jobs: scale`, `scale_idle: 70m`): the session above with a 70-minute idle stage, since the pass is due 48 to 72 minutes after the first lookup (the scale scripts' time limit now grows with `scale_idle`; Windows used to stop a long idle before its snapshot). Both sides ran in the same run against the same fakeslackd, with the same driver. "Before" is ui at e57a4f3d (mem-diet merged); "after" is mem-diet-2 at d33ed0e7. The pass ran 49 minutes in on Linux and within the idle stage on Windows, and let 25,134 (Windows) and 25,200 (Linux) of about 25,600 members go in 26 ms and 23 ms. Users held, and the live Go heap the profile attributes to users (the store's members, users/info's answers, meta.json's users):

| | startup | long channels | +40 channels | idle 70 min |
|---|---|---|---|---|
| Windows: users held, before → after | 6,818 → 6,831 | 22,445 → 22,440 | 25,592 → 25,634 | 25,592 → 500 |
| Windows: users cache, before → after | 9 → 8 MB | 33 → 39 MB | 36 → 42 MB | 36 MB → under 1 MB |
| Linux: users held, before → after | 6,824 → 6,908 | 22,460 → 22,530 | 25,603 → 25,700 | 25,603 → 500 |
| Linux: users cache, before → after | 8 → 10 MB | 29 → 31 MB | 34 → 34 MB | 34 MB → under 1 MB |

Until the first pass the two sides hold the same users (the session fetches 25,600 in its first 9 minutes); the cache only bounds them from the pass on. At the idle snapshot:

| idle 70 min | private working set (Linux: private) | after GC + trim | commit, after GC + trim | live Go heap |
|---|---|---|---|---|
| Windows, before | 396 MB | 390 MB | 651 MB | 288 MB |
| Windows, after | 322 MB | 317 MB | 579 MB | 220 MB |
| Linux (llvmpipe), before | 629 MB | 594 MB | | 276 MB |
| Linux (llvmpipe), after | 575 MB | 566 MB | | 236 MB |

The users account for 34 to 36 MB of those differences. The rest moves from run to run: Windows' messages held 64 MB on the before side and 34 MB on the after side, depending on where the snapshot fell against the last message pass (the earlier run 43415590578, cut short on Windows, measured Linux at 279 → 253 MB live with the plugin engine 19 MB the other way). That first run's Linux leg (the pass 71 minutes in, the snapshot at 76) had the users at 40 MB → 2 MB (25,672 → 618 held: 118 were asked for again after the pass), before a pass that lets most members go copied the maps into ones of their new size (Go maps keep the room of their largest size: about 3 MB here).

What the after side's idle heap still holds (Windows / Linux): the plugin JS engine 105 / 88 MB (below), messages 34 / 51 MB (Slack's message budget), fonts, outlines and runs about 30 / 50 MB, the emoji map 8 / 12 MB, the GPU's vertex cache and the spell checker's dictionary a few MB each. Outside the Go heap the renderer's private memory (Direct3D on WARP, llvmpipe) is 179 / 218 MB of the working set, now the largest single owner.

## Fetching members as Slack does (2026-11-13)

Until the hourly pass the members cache doesn't bound anything, so what fills it is what the session fetches. Clack used to fetch everyone a loaded page names: each message's sender, reply users, editor, reactors and text mentions, for every history page (prefetch included), thread, search page, pins list and Threads view, before storing it. Slack fetches none of them at load time (client-boot build 3889f0c93d30):

- Its history and replies fetchers (QJKZ `fetchHistory`, `fetchReplies`, both with `no_user_profile`) pass the messages to `processIncomingMessages` (udbq), which asks for no member. The legacy API's `ensureModelForLegacy` skips the member fetch for `activity.mentions`, `conversations.history`, `pins.list` and `unread.history` (`shouldSkipMemberFetch`).
- `ensureMembersArePresent` (iog+) has 16 callers, and none of them is a message list: the unread threads popover's rows, autocomplete's affinity pool, agents, user groups, Quip, meeting headers.
- A member is asked for by `getMemberById` (gIKW) in the component that shows it, as that component renders: the sender, a mention (MemberSlug), the reply bar's avatars, a pin's "Pinned by". A reaction's users are read only by the tooltip (ReactionTip Qvn6's TooltipMemberName NZA6), which mounts when the tooltip shows.

What Clack does now (`core/lazy_users.go`):

| where | fetched before | now |
|---|---|---|
| history pages, prefetch, older pages, history previews, the Unreads view | sender, reply users, editor, reactors, mentions | nobody: rows ask as they draw |
| threads (`conversations.replies`, both callers), search results, pins, Threads view, saved and activity messages (`messages.list`) | the same | nobody |
| `message_changed`, `message_replied` (reply users), `reaction_added` (the reactor) | the ids in the event | nobody |
| a new socket message | the same five | sender, reply users and mentions: its notification names them before any row draws |
| user mentions in message text | fetched with the page | `richview.MemberRead` reads each drawn mention's member every frame (a miss is asked for; the text is laid out again when the name arrives) |
| a reaction's users | fetched with the page | when its tooltip is due (300 ms on the pill), not on the first hover frame |
| the reply bar's avatars, the sender, "Pinned by" | fetched with the page | already looked up as they draw |
| the DMs tab | DM people and the latest messages' people | DM people only: `dmsTabShows` leaves out deleted members and bots |
| Activity's authors and actors, typing, profiles, lists, canvases, files, huddles, drafts, channel details, the sidebar's DM people at boot | as before | unchanged: not message lists (Activity's rows are described for every item at once; Slack's sidebar reads every DM's members on each store change) |

Clack has no read receipts. `internal/ui` `TestReactorsFetchedOnTooltip` opens a channel with a reaction from people nothing else names, checks users/info was asked for none of them, rests on the pill, checks they're asked for only once the tooltip is due, and that the tooltip then names them. `internal/ui/richview` `TestMentionReadsMemberWhenDrawn` covers the mention hook. `TestEvictedMembersComeBack` (flake-check `members`, run 43448597706) still sees every view draw its people again after an eviction.

### Measured

perf.yml run 43448329808 (`jobs: scale`, `scale_base: 3bb45cbd`, idle 10 minutes, so every stage is before the hourly pass): both sides in the same run against the same fakeslackd, with the same driver. "Before" is ui at 3bb45cbd; "after" is lazy-users at 502bb8b1. Users held is what the store holds (about 1,050 of them read from meta.json); users asked is what the session sent to users/info (`app_users_asked`, which the before side doesn't count: its fetches are about the held count less 1,050). Live Go heap is after a collection.

| | startup | long channels scrolled | +40 channels | idle 10 min |
|---|---|---|---|---|
| Linux: users held, before → after | 6,845 → 1,125 | 22,493 → 11,870 | 25,637 → 12,429 | 25,637 → 12,429 |
| Linux: users asked, after | 76 | 10,821 | 11,380 | 11,380 |
| Linux: `PutUsers` heap, before → after | 4.5 MB → under 1 MB | 18.5 → 10.0 MB | | 21.0 → 10.5 MB |
| Linux: live Go heap, before → after | 93 → 95 MB | 492 → 468 MB | 460 → 416 MB | 293 → 254 MB |
| Windows: users held, before → after | 6,813 → 1,126 | 22,451 → 14,173 | 25,648 → 14,534 | 25,648 → 14,534 |
| Windows: users asked, after | 77 | 13,128 | 13,489 | 13,489 |
| Windows: `PutUsers` heap, before → after | 3.5 MB → under 1 MB | 20.1 → 10.1 MB | | 23.6 → 9.1 MB |
| Windows: live Go heap, before → after | 84 → 86 MB | 542 → 481 MB | 489 → 458 MB | 293 → 278 MB |

At startup the session now fetches 76 people instead of about 5,800: the people the first screen draws, not those of the 24 prefetched channels. After the long channels it fetches half as many (the senders, mentions and reply users of the 6,000 messages it scrolled past, no reactors). The users' own heap halves. The rest of the live heap moves from run to run as the earlier sections describe (messages, the plugin engine); the startup heap is the same within noise. Linux scrolls twice as far a frame (`CLACK_PERF_SCROLL_STEP`), so it draws fewer of the rows and fetches fewer people than Windows.

## The plugin engine's high-water mark (2026-11-13)

The plugin JS engine is one wasm instance (wazero), and wasm memory grows but never shrinks: what it held at its busiest stays for the session. In the scale session it holds about 25 MB at startup and 80–100 MB from the long-channels or the 40-channels stage on (it lands anywhere in that range from run to run and side to side; the same session, the same code). What fills it is LastSeen's `readStore`, which every 15 s (while a LastSeen surface is mounted and `watchActivity` is on) walks every message of every conversation in `state.messages`: the mirror turns each conversation it reads into JS objects (`messageJSON`, plus the prototype copy Slack's slices have) and keeps them until a change invalidates them. At the 40-channels stage the store holds about 10,000 messages in 63 conversations (two 3,000-message channels and 40 just read, all kept whole as on screen), and the JS heap holds them at once.

- **Is Slack's plugin-visible store bounded the same way?** Yes, and no tighter: plugins in Slack read Slack's own store, which the messages CacheManager bounds to the same budget (3,000 evictable messages → 2,000) but checks only hourly, so at that point in the session Slack's store holds at least as many messages. The mirror follows Clack's store, which applies that budget every 2 minutes and drops what it evicts on the JS side too (`evicted` → `invalidate`). The difference is the engine: V8 gives a walked store's garbage back after a collection, wasm can't.
- **Can the engine be recycled when idle?** It can: Taut already restarts a dead runtime (`restartAfterCrash`: a fresh instance, the UI patches dropped, the running plugins registered again; the runtime starts in about 350 ms and the plugins are up by about 1.4 s). Recycling an idle one the same way would give back the high-water mark (about 75 MB here), at the cost of every plugin's in-memory state, as turning the plugins off and on does (LastSeen's unsaved sightings wait on a timer, so it needs each plugin's stop to run first). Not done here: it changes what Karim's plugins keep and needs its own measurement. Bounding the mirror's cached messages (a least-recently-read limit on the JS side) would lower the peak a walk reaches, without the restart.

## Animated GIFs (2026-11-14)

Slack plays every GIF it shows (client-boot build `2c0d02ca130f`): a file's gallery image is FileImageThumbnail's `getThumbSrcForFile` at maxSize 720 (2HRR), which picks `thumb_480_gif`, then `thumb_360_gif`; attachments and image blocks draw `image_url` through the image proxy (NANx, FnNq). Only with Accessibility > Allow animations off (`a11y_animations`) does it draw stills: a file's `deanimate` (or `deanimate_gif`, or a static thumb), the proxy's `o1=gu` single frame for the rest, and custom emoji through the proxy the same way (vbPY). Clack used to animate only images requested at 160px or less (`MaxAnimPx`), so every message GIF sat on its first frame, and it decoded every frame of a GIF (`image/gif`'s DecodeAll) even to draw that frame.

What Clack holds for a GIF now (`internal/imgcache/anim.go`, `gifstream.go`):

| part | when | size |
|---|---|---|
| the still: its first frame at the drawn size | from the first draw, like any decoded image (LRU, 60 s idle sweep) | 4 bytes a drawn pixel (a 419×235 gallery tile: 394 KB) |
| its encoded bytes and their index | kept with the still so playing it again needs no download (Slack's proxy and Giphy aren't cached elsewhere); at most a quarter of the budget, the least recently drawn going first, and dropped before stills when the budget is tight | the file's size (a 480px Giphy rendition: about 1 MB) |
| its player | only while something draws it: it starts once the GIF has been drawn for 150 ms (a fast scroll's GIFs show their stills and start nothing), and goes once nothing has drawn it for 1 s past its frame's due time (scrolled away, the conversation left, the window hidden) | a player whose frames at the drawn size fit in 2 MB keeps them all (custom emoji, avatars: no work a frame); a larger one streams: its canvas (4 bytes a GIF pixel), the scaler's buffers (at most 1.25 bytes a GIF pixel) and three frames at the drawn size; a 480×270 GIF drawn at 419×235 holds 1.9 MB |

Every part counts in the decoded-pixel budget (`DefaultLimit`, 64 MB). What goes when the budget is short, in order: encoded bytes nobody drew in the last second, then such stills (least recently drawn first), then the players on screen (largest first; each draws its still for 3 s), and only then images on screen. A player starts only if its bytes fit after the first two; otherwise the GIF shows its still and asks again 3 s later, and one wanting over half the budget never starts. So a burst of large GIFs can't push the decoded bytes past the budget, nor make GIFs load and evict each other in a loop. A streaming player composes each frame on a worker just before it's due (at most 2–4 at once, by GOMAXPROCS): LZW for that frame only, Chromium's composition and disposal, then a box filter while the canvas is twice the drawn size or more, and bilinear for the rest. Frames play for their GIF delays (10 ms or less as 100 ms) and loop counts are honoured; a late frame shows when its decode is ready, never as the still. The caps: a canvas over 4 megapixels shows its still, and an index lists at most 5,000 frames (the rest are left out and logged).

Animations off draws stills (`SetAnimate`, from `a11y_animations`): files their `deanimate`, attachments and image blocks their first frame (the pixels the proxy's `o1=gu` returns), emoji theirs. Each such file (and each attachment image that decoded as an animation) is Slack's PlayPauseAnimations (CfuZ): the round Play button over the still, a click to play it (through `GetPlaying`) and another to stop it. Its Expand opens the viewer while it's hovered: FileActions' renderExpand for GIF and PNG files with `deanimate` (zoom-in, top 8, right 8), AttachmentImageActions' Expand for attachments (search). The bars' other buttons aren't drawn yet (W45). Clack's own viewer plays a GIF but has no Play button of its own with animations off (Slack's ImageViewerWithZoom renders one): it shows the still there.

### Measured

The perf job's "GIF memory" step (perf.yml, `internal/ui/perf_gif_test.go`) runs on every push to ui and main: `fakeslackd -gifs 300` adds #gif-wall to the Hack Club-sized workspace, 300 large animated GIFs (files with `thumb_480_gif` and /giphy attachments, about 1 MB each). The test pages back through all of them and scrolls down through every row, a few on screen at a time. It fails when the decoded bytes pass the budget at any frame, when more players run than a screen holds once the scroll rests, when the GIFs on screen don't play at their drawn size, or when any player is left 4 s after leaving the channel. perf.yml run 43578301612 (9f83494e): decoded bytes at most 64.0 MB of 64 (49 MB of it encoded bytes, before the quarter cap), at most 5 players (8.4 MB) while scrolling, 4 players (6.6 MB) resting with 202 frames advanced in 2 s (25 fps each, the largest frame 419×235), none 4 s after leaving; the live Go heap 33 MB before, 125 MB scrolled, 116 MB after leaving (the stills and bytes stay until the 60 s sweep).

The scale session's `gifs` and `gifs-left` stages (after idle, so the other stages don't move) do the same at the real account's scale on both runners. Same run, before = ui (e42ee89c) driven by the same driver:

| | Windows: private working set | after GC + trim | live Go heap | images | Linux: private | after GC + trim | live Go heap | images |
|---|---|---|---|---|---|---|---|---|
| #gif-wall scrolled, resting: before | 636 MB | 621 MB | 391 MB | 64 MB (538 entries) | 901 MB | 693 MB | 389 MB | 64 MB (319) |
| … after | 616 MB | 580 MB | 344 MB | 38 MB (52, 3 playing: 4 MB) | 927 MB | 643 MB | 337 MB | 38 MB (47, 3 playing: 5 MB) |
| left 10 s: before | 628 MB | 621 MB | 386 MB | 64 MB | 727 MB | 681 MB | 378 MB | 64 MB |
| … after | 602 MB | 596 MB | 340 MB | 33 MB (0 playing) | 673 MB | 638 MB | 332 MB | 33 MB (0 playing) |

Playing the GIFs on screen costs less than the old stills did: each of those was decoded through DecodeAll (every frame's indices at once, then the first frame copied), and the transient heap it left grew the arenas. Linux's "as is" column at the resting stage is higher after (927 MB): 107 MB of heap the GC had freed and not yet returned; after a collection it's 50 MB under.

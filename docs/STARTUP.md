# Startup: the timeline and the warm boot

## Timeline

Every start logs its milestones to clack.log (`internal/applog/startup.go`). `debug:` lines are on for the first 60 s, and always with `CLACK_DEBUG=1`:

```
debug: startup +14ms cache-read (warm: 467 convs, 12 sections, 96 users, 210 emoji, 50 msgs in 9ms, saved 4m2s ago)
debug: startup +41ms window
debug: startup +118ms first-frame (background, fonts loading)
...
startup: timeline settled=1830ms warm cache-read=14ms window=41ms first-frame=118ms first-text=301ms theme=301ms sidebar=301ms messages=320ms ... relayouts=0
```

| milestone | when |
|---|---|
| `cache-read` | the boot snapshot was read (or "cold" and why) |
| `window` | the window exists |
| `first-frame` | the first frame was submitted (background only while fonts load) |
| `first-text` | the first frame with text |
| `theme` | the first frame with the account's own theme (`account`) or Slack's default when the account has none |
| `sidebar-model` / `sidebar` | the sidebar's model came from the snapshot / the first frame showing rows (`cached` or `live`) |
| `messages` | the first frame showing the open channel's messages (`cached`: from history.json) |
| `core-ready` | core.Boot finished (conversations, counts, sections, prefs… answered) |
| `sidebar-live` | Slack's answers replaced the sidebar |
| `history-live` | the cached page of the last channel was refetched |
| `images` | no image fetch or decode left after messages and sidebar were shown |
| `plugins` | the plugin runtimes attached (`none` without plugins) |
| `settled` | the last change before 1.5 s with nothing new (or `[timeout]` at 60 s) |

`relayouts` counts frames where something already painted moved, by kind: `sidebar` (rows added, removed, reordered or relabelled), `unreads` (a row's unread or mention state), `messages` (a visible message row moved or resized), `theme` (the account theme changed after the first frame, the default-theme flash of a cold start).

The perf workflow (`.github/perf/startup.sh`) runs cold and warm starts of the base and HEAD against a Hack Club-sized fakeslackd answering every API call after 150 ms and every image after 40 ms, with a non-default account theme. It also runs HEAD with `CLACK_NO_BOOT_CACHE=1`, the same binary booting without the snapshot, so before/after compare with the same instrumentation. The table is on the run page ("Startup timeline").

## What is cached

`<cache root>/v1/<team>-<user>/` (`internal/store/bootcache.go`); the cache root is the OS's user cache folder (`~/Library/Caches/Clack`, `~/.cache/clack`, `%LocalAppData%\Clack`), `<CLACK_HOME>/cache` when `CLACK_HOME` is set, or `CLACK_CACHE_DIR`. Directories are 0700, files 0600, written through temp files renamed into place.

| file | holds | written |
|---|---|---|
| boot.json | team (name, domain, rail icon), self, users.prefs as Slack sent them (ia_theme, window_background_gradients, sidebar prefs…), the sidebar's conversations (with topics and purposes), sections, client.counts, which DMs are open (sidebar.dms), experiment groups, stars; plus the users, custom emoji, channel names, usergroups and bots the rows and the cached messages name | 20 s after a successful boot, every 5 min, at quit |
| history.json | the newest 50 messages of the conversation open at the save | same |
| meta.json | the user directory, all custom emoji, usergroups, prefs, channel names, bots | as before |
| img/ | avatars, emoji, message thumbnails (`files-tmb`); 384 MB, least recently used out | as images decode |

Sign-out deletes the directory (`core.App.DropCache`, cmd/clack's SignOut for img/), and nothing is written after it. A file whose `"v"` isn't `store.CacheVersion` is ignored; older `vN` directories are deleted at start. The pre-2026-11-09 shared `~/.config/clack/cache` moves into the account's directory when its sidebar.json was the same user's, and is deleted otherwise. `CLACK_NO_BOOT_CACHE=1` starts without the snapshot.

Plugins: the TautAPI prelude and every installed plugin bundle are compiled to QuickJS bytecode once (`jsrt.Ctx.EvalCached`, keyed by engine + source hash, SHA-256 checked) and read back on later starts; wazero's compiled engine was already cached.

## How it reconciles (and the Slack evidence)

From Slack's web client on the public CDN (client-boot, gantry-v2-async-client-boot-shared/render, build of 2026-11-08):

- **Per team and user, versioned.** The persisted store is IndexedDB key `persist:<appName>-<teamId>-<userId>` (`getStoredStateFromIDB`). `getStoredStateIfValid` boots cold with a `coldBootReason` of `forced`, `team_expired`, `store_out_of_date` (the stored `versionDataTs` is older than the minimum data version; the store is purged), `indexeddb_unavailable`, `indexeddb_read_failed` or `no_stored_state`. Clack: per-account directory, `v1` and `"v"`.
- **What Slack persists.** Reducers with `persistenceEnabled: true` include `bootData`, `channels`, `channelSections`, `channelSidebar`, `members`, `messages`, `channelHistory`, `channelCursors`, `channelLatests`, `unreadCounts`, `unreadChannelCounts`, `prefs`, `userPrefs`, `teamPrefs`, `flannelEmoji`, `stars`, `experiments`, `route`. Not persisted: `presence`, `typing`, `consistency`, `unreadMarkers`, `channelLoading`, `deletedMessages`, `ephemeralMessages`. Clack keeps the same split: presence is cleared from cached users.
- **Paint, then fetch everything.** The warm boot sets the initial state from the stored state ("Set initial state (and rehydrating cached data during a warm boot)") and still calls the boot APIs, as deferred data (`fetchBootData`: `isBootingWarm ? "deferred-data" : "initial-data"`, with `min_channel_updated` for an incremental channel list). Clack has no incremental API, so Boot fetches as on a cold start and replaces each cached part whole: conversations (a channel left elsewhere leaves the sidebar), sections, open DMs, experiments, stars. A failed call keeps the cached part.
- **Unreads.** `unreadChannelCounts` is persisted and replaced by `bulkSetClientCounts` ("Bulk set client counts from API response") when client.counts answers; `unreadCounts.countsApplied` and `countsLastFetched` track whether the counts are this session's. Clack paints the cached counts, replaces them in one step, and holds the macOS Dock badge (visible outside the app) until they are live (`core.App.CountsLive`).
- **The last channel.** Slack persists `messages` and `route`; Clack keeps the open conversation's newest page (history.json), shows it at once, refetches it with the boot calls and has the list re-read it (`poll`). `core.History` answers a cached page without waiting for its side fetches before Boot finished.

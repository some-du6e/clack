# Startup: the timeline, the cold path and the boot placeholder

## Timeline

Every start logs its milestones to clack.log (`internal/applog/startup.go`). `debug:` lines are on for the first 60 s, and always with `CLACK_DEBUG=1`:

```
debug: startup +3ms window
debug: startup +118ms first-frame (background, fonts loading)
...
debug: startup +172ms js-runtime (in 154ms, err=<nil>)
...
debug: startup +668ms data-ready
debug: startup +937ms reveal (cap, was waiting for plugin renders; 70 plugin deliveries from 68 roots (most: root 1, 3))
startup: timeline settled=937ms window=1ms first-text=83ms js-runtime=172ms … data-ready=668ms reveal=937ms relayouts=0
```

| milestone | when |
|---|---|
| `window` | the window exists |
| `first-frame` | the first frame was submitted (background only while fonts load) |
| `first-text` | the first frame with text |
| `theme` | the first frame with the account's own theme (`account`) or Slack's default when the account has none |
| `sidebar` | the first frame showing sidebar rows |
| `messages` | the first frame showing the open channel's messages |
| `cache-read` | the warm boot's disk reads resolved: `warm: N convs…` or `cold: <why>` |
| `sidebar-cached` | a warm start's sidebar from the boot data is applied (before Slack answered) |
| `core-ready` | core.Boot finished (conversations, counts, sections, prefs… answered) |
| `sidebar-live` | Slack's answers are in the sidebar |
| `js-runtime`, `taut-started`, `slick-started` | the plugins' JS engine is up; the plugins enabled at start have started |
| `data-ready` | boot data and the open channel's header are in |
| `images` | no image fetch or decode left after messages and sidebar were shown |
| `plugins` | the plugin runtimes attached (`none` without plugins) |
| `reveal` | Slack's boot placeholder gave way to the client (see below) |
| `settled` | the last change before 1.5 s with nothing new (or `[timeout]` at 60 s) |

`relayouts` counts frames where something already painted moved, by kind: `sidebar` (rows added, removed, reordered or relabelled), `unreads` (a row's unread or mention state), `messages` (a visible message row moved or resized), `theme` (the account theme changed after the first frame, the default-theme flash of a cold start).

The perf workflow (`.github/perf/startup.sh`) runs cold and warm starts of the base and HEAD against a Hack Club-sized fakeslackd answering every API call after 150 ms and every image after 40 ms, with a non-default account theme. `cold` starts have nothing on disk (no metadata cache, no boot data, no compiled JS engine, no saved theme or plugin slots), `warm` starts what a normal start has; every frame's layout and total time is logged too (`CLACK_FRAMELOG`). The table is on the run page ("Startup timeline"). `CLACK_NO_WARM_BOOT=1` starts cold even with boot data on disk (and reads no header cache).

## The cold path

Everything a start needs goes out at once (core.Boot and backend.start):

- **Boot batch:** users.conversations (then, straight away, users.info for the DM rows' people), client.counts, users.channelSections.list (truncated sections page in parallel), experiments, sidebar.dms, users.prefs, team.prefs, team.info, stars.list, saved, self + the rail icon, emoji.list. meta.json (users, emoji, groups) decodes alongside instead of before them. emoji.list (~7 MB, 65k entries on Hack Club) is scanned straight into the store's packed form (`slack.PackEmojiJSON`; encoding/json took ~300 ms) and written to disk off the critical path. Up to 16 calls are in flight (`slack.MaxConcurrent`).
- **The restored channel** (ui.json's route): its history and header (conversations.info tabs, bookmarks, member count and facepile: three reads at once in `backend.opened`), and the drafts' sidebar markers, start at t=0, not after boot.
- **Plugins:** the JS runtime, the bundles (QuickJS bytecode from the cache) and the enabled plugins start at t=0 too. Taut's `StartEnabled` no longer waits for core; the redux mirror reads `core.SnapshotNow` (never blocking the JS thread) and re-reads everything at the `booted` event.
- `{"ok":true` bodies skip the extra full parse that only read `ok`.

## Boot placeholder

Slack's page holds `div.p-client_container_placeholder` (a theme background, a sidebar block and a content panel) under `div.p-client_container`. client-boot's "Render Placeholder" hook (`renderBootPlaceholder`) colours it from what the last session stored for the team at renderComplete ("Save channel sidebar and team switcher colors"): the stored iaTheming's window gradient (the flat primary without window gradients), the sidebar block `rgba(255,255,255,.9)` (`.11` for an inverted light sidebar, `rgba(16,18,20,.6)` dark) as wide as the last sidebar, the content panel `#fff` (`rgb(26,29,33)` dark). With nothing stored it is page.html's `#bbbabb` (`#282828` dark). React mounts the client over it in renderApp ("Render React"), after bootDataFinished ("Finish waiting for boot data"), or from the persisted store on a warm boot; the channel then shows its own loading state until its history arrives.

Clack draws exactly that placeholder (`internal/ui/reveal.go`, from the first frame, before fonts are loaded) and keeps the account theme in ui.json (`acct_theme`) the way Slack keeps its colours per device. It renders the client once the sidebar's data is in (the boot data on a warm start, below; else core booted), the restored channel's header is in (Slack's boot data carries it in the channel object; on a warm start the header cache has it), and the plugins enabled at start registered and delivered their first renders (Taut's patches render synchronously with Slack's React; Clack's plugins render on their own thread, so it waits for their deliveries to go quiet, at most 100 ms after the data). Messages and images don't hold it. Re-layouts are counted only after it.

## Warm boot

Slack's client keeps its redux store in IndexedDB per team and user and, when one is there, renders from it before any boot call answers (client-boot, build 2c0d02ca130f): `getContextualInfoForTeamId` sets `isBootingWarm` from `maybeGetStoredState` → `getStoredStateIfValid` (IndexedDB keyed by team and user; skipped when the team expired, the store is older than the minimum data version, or holds no bootData), and in `runBootTeam` `renderApp` then waits only for the store hydrated from it (`dataSetupStore`, `setInitialState`), the translations and the render module: `getBlockingStatus(e,t){return t?"nonblocking":e}` makes every registered boot API nonblocking on a warm boot, and each one's answer is dispatched into the store when it comes. Its persisted stores include channels (tabs ride in `properties.tabs`), channelSections, unreadCounts, prefs, members, membershipCounts, files, messages and channelHistory; bookmarksStore is not persisted. In the channel the messages show from the persisted store, or `c-channel_loading_state` until the history arrives.

Clack does the same with the part the first frame shows (`internal/core/warmboot.go`, `internal/store/bootdata.go`):

- Boot reads `boot_data.json` (the team, the sidebar's conversations, sections, unread counts, open DMs, starred, the Activity badges) and `headers.json` while `meta.json` decodes. Once all three are in, the boot data is installed where Boot hasn't answered yet, a snapshot is built from it, `DataReady` closes and core emits `hydrated` (the plugins' state mirror re-reads, as at `booted`).
- The UI applies that snapshot (`core.WarmSnapshot`) and the restored channel's header from the header cache, and the placeholder lifts on them; a restored channel the header cache doesn't have doesn't hold it (Slack renders whatever its persisted store has), and its tabs come with their read.
- Each of Boot's sidebar answers is installed when it arrives (Slack dispatches every boot API's answer into its store on arrival) and announced, so the UI follows it: conversations (cached ones Slack no longer lists leave), counts, sections, sidebar.dms, stars. A call that fails keeps the cached part, and data that didn't change changes nothing on screen.
- The window is usable meanwhile, so an answer can land after a write it doesn't know about. Every change to the sidebar's state moves a journal (`internal/store/journal.go`): a mark or a new message per conversation, a leave, archive, join, open, rename or close per conversation, a DM's open flag, the section list, the Activity badges. An answer keeps whatever changed after its call went out (Boot's, and `refreshCounts`', `FetchSidebarDMs`', `refreshSections`' too). Counts merge per field: a mark made meanwhile keeps its cursor; a message alone takes Slack's cursor and mention count (it knows mentions below a partial mark the client can't count) and stays unread when it is newer than the answer. An answer that merged anything is partly older than Slack is: Boot reads the counts or the sections once more after it finishes, and `refreshCounts` reads once more (not again after that).
- Every channel opened shows its last header at once (tab bar, folder bookmarks, facepile, member count) while its reads revalidate it; a read that brings the same header draws nothing, and a failed read keeps the bar.
- Both files name their account and are read only for it (`ReadBootData`'s team and user check, besides the per-account directory). They are written 20 s after boot, every 5 minutes, and as the window closes (before plugins' stop hooks), only after a Boot that answered. Sign-out deletes them first, at once, and leaves a signed-out mark (`store.SignOutNow`; meta.json and the images follow in the background); a session that ended (auth_failed) deletes them, and no save already under way writes them again; a start finishes a sign-out whose process ended first (`store.SweepAccounts`, the marked directories; never the signed-in account's, whose stale mark it clears, as signing in again does). Other signed-in accounts' (the account switcher's) keep theirs, as Slack keeps a persisted store per signed-in team.
- Not copied: Slack boots cold again once its last cold boot is a week old (`hasBeenTooLongSinceLastColdBoot`), since its warm boot fetches conversations incrementally (`minChannelUpdated`) and can drift; Clack's refetches every list whole. Slack also persists messages; Clack never writes message content, so the restored channel shows its loading state until the history arrives.

The UI goroutine never waits for Boot: core's `Snapshot` (and `BootErr`) wait, so UI code reads the backend's model, `RecentSnapshot` or `SnapshotNow`, which don't. A wait longer than 50 ms is logged (`core: Snapshot waited Nms for boot (<caller>)`), a wait on the UI goroutine is a uiguard violation, and `TestNoBootWaitOnUIPaths` (a source scan), `TestUINeverWaitsForBoot`, `TestActivityNeverWaitsForBoot` and `TestStartupStatesNeverBlock` fail on one.

## What stays on disk

`<cache root>/v1/<team>-<user>/` (`internal/store/cachedir.go`); the cache root is the OS's user cache folder (`~/Library/Caches/Clack`, `~/.cache/clack`, `%LocalAppData%\Clack`), `<CLACK_HOME>/cache` when `CLACK_HOME` is set, or `CLACK_CACHE_DIR`. Directories are 0700, files 0600, written through temp files renamed into place. Nothing in it is message content.

| file | holds |
|---|---|
| meta.json | the user directory (always including the sidebar's DM people), all custom emoji, usergroups, prefs, channel names, bots (saved off boot's critical path) |
| boot_data.json | the sidebar a warm boot renders first (team, conversations without their latest message, sections, unread counts, open DMs, starred, Activity badges) |
| headers.json | the 100 most recently opened channels' tab bars (with the folder tabs' bookmarks), facepile ids and member counts |
| img/ | avatars and emoji; 384 MB, least recently used out |

Elsewhere: ui.json (per device) keeps the account theme for the boot placeholder (`acct_theme`) and the plugin slots (`plugin_slots`); `taut/cache` keeps the JS engine's compiled code and the bundles' bytecode. Sign-out deletes the account directory, and nothing is written after it. Older `vN` directories are deleted at start, and so is what alpha.28's boot snapshot left (`boot.json`, `history.json`).

## alpha.28's boot snapshot, and why the warm boot came back

alpha.28 painted warm starts from a persisted snapshot (sidebar, counts, the last channel's 50 messages), like Slack's IndexedDB store. On the perf workflow (150 ms per API call, ChatTabs and five Taut plugins) it lost: the same binary revealed at 932–937 ms without it and 981–988 ms with it (run 42765182591), because there the plugins, not the data, set the time, and the snapshot added its own read and reconcile. It was removed with its disk writes, and with it the last message text Clack wrote to disk.

The real network isn't 150 ms per call. Karim's clack.log (2026-11-15, Hack Club, 1,655 conversations) has users.conversations at 1.26–2.41 s and users.prefs at 1.0–1.5 s, the restored channel's header reads at 0.7–1.3 s, and the placeholder lifting at 2.1–2.9 s although the first frame was at ~0.7 s and the metadata cache loaded in 96–180 ms. The warm boot above takes the network off that path again, without any message on disk: the reveal waits for the disk reads, the first frame and at most 100 ms of plugin renders, about 0.8–0.9 s on that machine.

## Plugins at start

The JS engine starts at t=0. With its compile cache (`taut/cache`) it is up in ~170 ms on a CI runner; without one (the first start ever, or after a wazero or engine change) wazero compiles the 1 MB engine for ~1.3 s. Plugins start 30–150 ms after the engine. Their first renders mount component by component (ChatTabs, WhoReacted, LastSeen… ~70 roots for 50 messages), one layout per step: under the placeholder each step is a layout only, the placeholder alone goes to the GPU.

The placeholder waits for plugins at most 100 ms after the data is ready. A plugin that renders later doesn't move anything when it wraps a component it wrapped last session: the insets of the original inside the plugin's tree are kept in ui.json (`plugin_slots`), and until the plugin's first tree arrives the native component draws inside them (`internal/ui/slackc/reserve.go`). Slots are released once the plugins started and their renders were quiet for a second (10 s at most), and the learned ones are saved.

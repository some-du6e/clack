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
| `core-ready` | core.Boot finished (conversations, counts, sections, prefs… answered) |
| `sidebar-live` | Slack's answers are in the sidebar |
| `js-runtime`, `taut-started`, `slick-started` | the plugins' JS engine is up; the plugins enabled at start have started |
| `data-ready` | boot data and the open channel's header are in |
| `images` | no image fetch or decode left after messages and sidebar were shown |
| `plugins` | the plugin runtimes attached (`none` without plugins) |
| `reveal` | Slack's boot placeholder gave way to the client (see below) |
| `settled` | the last change before 1.5 s with nothing new (or `[timeout]` at 60 s) |

`relayouts` counts frames where something already painted moved, by kind: `sidebar` (rows added, removed, reordered or relabelled), `unreads` (a row's unread or mention state), `messages` (a visible message row moved or resized), `theme` (the account theme changed after the first frame, the default-theme flash of a cold start).

The perf workflow (`.github/perf/startup.sh`) runs cold and warm starts of the base and HEAD against a Hack Club-sized fakeslackd answering every API call after 150 ms and every image after 40 ms, with a non-default account theme. `cold` starts have nothing on disk (no metadata cache, no compiled JS engine, no saved theme or plugin slots), `warm` starts what a normal start has; every frame's layout and total time is logged too (`CLACK_FRAMELOG`). The table is on the run page ("Startup timeline").

## The cold path

Everything a start needs goes out at once (core.Boot and backend.start):

- **Boot batch:** users.conversations (then, straight away, users.info for the DM rows' people), client.counts, users.channelSections.list (truncated sections page in parallel), experiments, sidebar.dms, users.prefs, team.prefs, team.info, stars.list, saved, self + the rail icon, emoji.list. meta.json (users, emoji, groups) decodes alongside instead of before them. emoji.list (~7 MB, 65k entries on Hack Club) is scanned straight into the store's packed form (`slack.PackEmojiJSON`; encoding/json took ~300 ms) and written to disk off the critical path. Up to 16 calls are in flight (`slack.MaxConcurrent`).
- **The restored channel** (ui.json's route): its history and header (conversations.info tabs, bookmarks, member count and facepile: three reads at once in `backend.opened`), and the drafts' sidebar markers, start at t=0, not after boot.
- **Plugins:** the JS runtime, the bundles (QuickJS bytecode from the cache) and the enabled plugins start at t=0 too. Taut's `StartEnabled` no longer waits for core; the redux mirror reads `core.SnapshotNow` (never blocking the JS thread) and re-reads everything at the `booted` event.
- `{"ok":true` bodies skip the extra full parse that only read `ok`.

## Boot placeholder

Slack's page holds `div.p-client_container_placeholder` (a theme background, a sidebar block and a content panel) under `div.p-client_container`. client-boot's "Render Placeholder" hook (`renderBootPlaceholder`) colours it from what the last session stored for the team at renderComplete ("Save channel sidebar and team switcher colors"): the stored iaTheming's window gradient (the flat primary without window gradients), the sidebar block `rgba(255,255,255,.9)` (`.11` for an inverted light sidebar, `rgba(16,18,20,.6)` dark) as wide as the last sidebar, the content panel `#fff` (`rgb(26,29,33)` dark). With nothing stored it is page.html's `#bbbabb` (`#282828` dark). React mounts the client over it in renderApp ("Render React"), after bootDataFinished ("Finish waiting for boot data"), or from the persisted store on a warm boot; the channel then shows its own loading state until its history arrives.

Clack draws exactly that placeholder (`internal/ui/reveal.go`, from the first frame, before fonts are loaded) and keeps the account theme in ui.json (`acct_theme`) the way Slack keeps its colours per device. It renders the client once core booted (or a boot snapshot painted), the restored channel's header is in (Slack's boot data carries it in the channel object), and the plugins enabled at start registered and delivered their first renders (Taut's patches render synchronously with Slack's React; Clack's plugins render on their own thread, so it waits for their deliveries to go quiet, at most 2 s after boot). Messages and images don't hold it. Re-layouts are counted only after it.

## What stays on disk

`<cache root>/v1/<team>-<user>/` (`internal/store/cachedir.go`); the cache root is the OS's user cache folder (`~/Library/Caches/Clack`, `~/.cache/clack`, `%LocalAppData%\Clack`), `<CLACK_HOME>/cache` when `CLACK_HOME` is set, or `CLACK_CACHE_DIR`. Directories are 0700, files 0600, written through temp files renamed into place. Nothing in it is message content.

| file | holds |
|---|---|
| meta.json | the user directory, all custom emoji, usergroups, prefs, channel names, bots (saved off boot's critical path) |
| img/ | avatars and emoji; 384 MB, least recently used out |

Elsewhere: ui.json (per device) keeps the account theme for the boot placeholder (`acct_theme`) and the plugin slots (`plugin_slots`); `taut/cache` keeps the JS engine's compiled code and the bundles' bytecode. Sign-out deletes the account directory, and nothing is written after it. Older `vN` directories are deleted at start, and so is what alpha.28's boot snapshot left (`boot.json`, `history.json`).

## Why there's no boot snapshot

alpha.28 painted warm starts from a persisted snapshot (sidebar, counts, the last channel's 50 messages), like Slack's IndexedDB store. Once the cold path above was fast, it no longer won: on the perf workflow (150 ms per API call, ChatTabs and five Taut plugins), the same binary revealed at 932–937 ms without it and 981–988 ms with it (run 42765182591), because the plugins, not the data, set the time and the snapshot added its own read and reconcile. It was removed with its disk writes, and with it the last message text Clack wrote to disk.

## Plugins at start

The JS engine starts at t=0. With its compile cache (`taut/cache`) it is up in ~170 ms on a CI runner; without one (the first start ever, or after a wazero or engine change) wazero compiles the 1 MB engine for ~1.3 s. Plugins start 30–150 ms after the engine. Their first renders mount component by component (ChatTabs, WhoReacted, LastSeen… ~70 roots for 50 messages), one layout per step: under the placeholder each step is a layout only, the placeholder alone goes to the GPU.

The placeholder waits for plugins at most 100 ms after the data is ready. A plugin that renders later doesn't move anything when it wraps a component it wrapped last session: the insets of the original inside the plugin's tree are kept in ui.json (`plugin_slots`), and until the plugin's first tree arrives the native component draws inside them (`internal/ui/slackc/reserve.go`). Slots are released once the plugins started and their renders were quiet for a second (10 s at most), and the learned ones are saved.

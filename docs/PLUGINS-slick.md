# internal/slick: Slick plugins, natively

Requirement (Karim, 2026-11-07): Taut and Slick plugin support "with no mistakes as if it was native slack", importing plugins unmodified, natively, "with no ifs".

Slick (https://github.com/3kh0/slick, GPLv3) plugins are TypeScript classes extending `SlickPlugin` from `$slick`. They patch Slack's React components by name, read and patch Redux, listen to RTM, transform outgoing messages, store data, and may have a privileged `main.ts` half that Slick runs in Electron's main process. Clack runs the files Slick builds, unmodified. No Slick code is in this repo: the contract (src/shared/Plugin.ts, src/shared/settings.ts, src/shared/main.ts, src/plugins/README.md, src/app/pluginManager.ts) is implemented independently.

## Design: Slick runs on Taut's runtime

Slick's API grew out of Taut's (its messages.ts says "Ported from Taut"), so both map onto the same machinery. Slick plugins run **inside Taut's jsrt runtime**: one JS engine, one Redux mirror, one patchComponent registry, one thunk table, one set of composer/message hooks. Everything the UI owners wired for Taut (slackc patch points, `taut.Dispatch`, `TransformSendBlocks`, `SendCheck`, `MessageView`, `MemberView`, overlays, Preferences tabs) works for Slick plugins with no extra call sites.

```
 Slick plugin .js (Slick's build:plugin) [+ <id>.main.js, its main.ts]
        │ require('$slick'), React global, this.api.*
 ┌──────▼──────────────────────────────────────────────────────────┐
 │ internal/slick  js/10_plugin.js  $slick (SlickPlugin, PLUGIN_APIS),
 │                                  loader, lifecycle, SlickBridge
 │                 js/20_api.js     SlickAPI per plugin (scoped, requiredAPIs-gated)
 │                 js/30_main.js    main halves: MainCtx, Node/Electron shims
 │                 Go: manager, settings.json, schema coercion, themes,
 │                     Preferences > Slick model, host functions
 ├─────────────────────────────────────────────────────────────────┤
 │ internal/taut   TautAPI internals (__taut), Redux mirror, thunks,
 │                 composer/message hooks; taut.Extension shares the runtime
 ├─────────────────────────────────────────────────────────────────┤
 │ internal/jsrt   QuickJS on wazero, web APIs, React + host renderer
 │ internal/ui/slackc  patch points, layout, CSS
 └─────────────────────────────────────────────────────────────────┘
```

**Zero cost.** `slick.New` reads `settings.json`, `user.css`, `themes/*.json` and each plugin's cached `.meta.json`. With no plugin enabled, no JS engine starts (`TestZeroCostWithoutEnabledPlugins`). Themes and custom CSS are converted in Go and handed to the UI as stylesheets, so they never start JS either.

## Wiring (ui-shell)

```go
t, _ := taut.New(taut.Options{…})      // as today
s, _ := slick.New(slick.Options{
	Taut:     t,
	OpenURL:  openURL,                  // MainCtx.shell.openExternal
	OpenFile: nativeFilePicker,          // `file` settings, MainCtx.dialog.openFile ("" = cancelled)
})
defer t.Close()
defer s.Close()                         // close Slick first

s.SetRoute(teamID, channelID)           // on every navigation: location.pathname, getCurrentChannelId, workspace restrictions
s.SetDark(dark)                         // themes with a "mode" apply only in that mode
ch, stop := s.Subscribe()               // repaint Preferences > Slick on change
```

Styles arrive through the same `UIHooks.SetStyle` Taut uses: `plugin:<id>:<key>` from plugins, `slick:theme` and `slick:user` from Slick. That is the only new wiring. Notifications already go through Taut's `showNotification` thunk, which is where Slick's notification filters (ShutUpSlackbot, StreamerMode) sit.

What ui-shell has to draw: **Preferences > Slick** from `s.Settings()` (below), and the new component boundaries in [PLUGINS-components.md](PLUGINS-components.md) §9, owned by the UI packages listed there.

## Preferences > Slick: `s.Settings()`

Shaped like Slick's settings UI (src/app/settings.tsx), four tabs:

- **Slick** (overview): `Enabled`, the global switch (`SetGlobalEnabled`). `ConfigError` when settings.json is invalid (nothing is saved until it's fixed, like Slick).
- **Plugins**: `Filters` (All, On, Off, User plugins, Problems; the last two only when non-empty), a search box over id, name, description, authors and category, and `Sections`, the plugins grouped by category in Slick's order (`Categories`, then "User plugins", then "Other"), each with an icon name and an "N on" count. A row is a `PluginInfo`: name, mrkdwn description, `Authors` (Slack mentions), switch (`SetEnabled`), running/error state, and a detail view with:
  - **Settings**: `Options` in schema order. Controls by `Type`: `boolean` toggle, `number` field (`Min`/`Max`), `text` field (`MaxLength`), `color` picker (hex), `select` dropdown (`Options`), `file` picker (`Accept`, value is the path; `OpenFile`), `names` (Slack user id → name rows, see Nicknames), `schedule` (rules of `days`, `start`, `end`, `status` from `Statuses`, see OfficeHours). `SetOption` coerces like Slick, `ResetOption` removes a stored value (`Stored`). `Live` settings apply without a restart, `Relaunch` ones say "restart Clack".
  - **Workspaces**: `SetWorkspaces(id, nil | ids)`, hidden when `AppWideReason` is set.
  - **Source**: "Installed from a URL" (`Source`) or "from a file"; `HasMain` and `Capabilities` for a privileged half; **Remove plugin** (`Remove`, deletes its data) and `ClearData`.
- **Themes**: `Themes` cards (Slack, each bundled theme, imported ones, "Custom CSS only"), each with a `Preview` swatch and "Only in dark/light mode". `SetTheme`, `ImportTheme(json, filename)` (256 KiB max), `RemoveTheme`. **Custom CSS**: `UserCSS` / `SetUserCSS`. Slick edits it in Monaco; a plain multi-line editor is fine.
- **Install plugins**: install a file (`Import`, renderer or `.main.js`), **Install from URL** (`InstallURL`; https or localhost, GitHub file pages become raw URLs; re-downloaded 15 s after start and every 3 hours), **Check for updates** (`UpdateUserPlugins`), `AutoUpdates` / `SetAutoUpdates`.

Plugins' own `settings.addTab` tabs come through Taut's `UIHooks.PreferencesTabs` and render with `Bridge.RenderPreferencesTab`, like Taut plugins' tabs.

## Files

`~/.config/clack/slick/` (or `$CLACK_HOME/slick`):

| file | what |
|---|---|
| `settings.json` | Slick's own format: `{enabled?, theme?, importedThemes?, plugins: {id: {enabled, …}}, pluginWorkspaces?}`. A Slick desktop settings.json can be dropped in. Unknown keys are kept. |
| `user.css` | custom CSS |
| `themes/*.json` | bundled themes (copy Slick's `themes/`; Clack doesn't ship them) |
| `plugins/<id>.js` | a renderer as `build:plugin` wrote it |
| `plugins/<id>.main.js` | its privileged half, from `internal/slick/build/build.mjs` |
| `plugins/<id>.meta.json` | cached declarations (id, name, category, schema, liveSettings, requiredAPIs, main capabilities), keyed by a hash of both files |
| `plugins/sources.json`, `plugins/prefs.json` | install URLs, Slick's user-plugin-prefs (`autoUpdate`) |

Plugin data (Slick's `blobStore('plugin:<id>')`: `api.storage`, `api.Cache`, stored files, `MainCtx.storage`) lives in Taut's data store under `slick:plugin:<id>` (`taut/storage/slick~plugin~<id>.json`), and `MainCtx.secrets` under `slick:plugin:<id>:secrets` in Taut's 0600 secret store. One write-back and one flush cover both systems. Clack itself stores no message contents; what a plugin keeps (MessageLogger) is its own data, as in Slick.

`localStorage['localConfig_v2']` is served from Go (Slack's per-team `{id, name, domain, user_id, token, url}`, `lastActiveTeamId`), never written to disk and read-only. It includes the session token, as Slack's page does for Slick plugins.

## Building plugins: `internal/slick/build/build.mjs`

```
node internal/slick/build/build.mjs <slick checkout> <out dir> [plugin dirs…]
```

Runs Slick's own build from a Slick checkout (`bun install --ignore-scripts` first):

- Renderer: Slick's `buildUserPlugin` (scripts/build/plugin.ts, what `bun run build:plugin` runs). It refuses Slick's 18 first-party plugins that have a `main.ts` ("privileged halves only ship bundled into Slick"); those are built with Slick's page build options (`pageBuildOptions` + `pagePlugins` from scripts/build/app.ts) into the same file format, with `$slick` left to the host and Slick's shared code bundled in, as Slick itself bundles them.
- Main half: `main.ts` bundled for node (electron external), queued on `globalThis.__slickMainPlugins`, with Node's `process`/`Buffer` from the host.
- `manifest.json` records how each file was built.

Import order: the renderer first, then its `.main.js`.

## API coverage

Everything in `SlickAPI` (src/app/pluginManager.ts) and `$slick`:

| API | Clack |
|---|---|
| `$slick`: `SlickPlugin` (statics, `log`, `stop`, `onSettingsChange`), `PLUGIN_APIS`, default export | js/10_plugin.js |
| lifecycle: start/stop/onSettingsChange with 5 s timeouts, scope disposed before `stop()`, live settings (all changed keys in `liveSettings`) vs restart, failed start stays failed until its config changes, `isActive()`, `signal` | manager.go + js |
| `requiredAPIs` gating (`Plugin must declare requiredAPIs: x`; omitted = all) | js/20_api.js |
| `patchComponent` (string or `{displayName, filter, component}`), `getComponent`, `waitForComponent`, `getRenderedComponent`, `waitForRenderedComponent`, `getComponentSource`, `getExport`, `getByProps`, `waitForExport`, `getValueSource`, `findModuleId`, `moduleSources`, `getFiberFromNode` (null: no fibers) | Taut's registry and modules |
| `setStyle(css, key)` (namespaced `plugin:<id>:<key>`, null removes), `onDocument` (Clack has no pop-out documents: kept, never called), `deferResizeWork` | Taut |
| `redux`: getStore, getRawState, useReduxState, usePatchVersion, patchState, patchSlice, mapEntries, patchThunk, getThunkCreator, waitForThunkCreator, dispatchThunk, refresh | Taut's mirror |
| `rtm.on(type)` by type, subtype or `*` | Taut's RTM + subtype routing |
| `messages`: getRawMessage, asRawMessage, getMessageBotId (`slick_bot_id`), injectMessages, diagnostics, modifyMessageObject (`sentBy`), useActivityMessage, useMessageBot | js + Taut |
| `members`, `channels` (getCurrentChannelId from the route), `blocks` (makeDelta, fromDelta, toPlainText, toMarkdown), `files.upload`, `elements` (19 incl. MemberLink), `menu.Menu`, `modal` (openModal, confirm, alert), `react` | Taut |
| `onMessageSendDelta` | transforms run over the Delta of the outgoing blocks; in-place edits count; only a changed Delta is turned back into blocks |
| `onMessageSendBlocks` | Taut's send transforms (sends, edits, schedules) |
| `addSendCheck(blocks, {channelId, threadTs})` | Taut's send checks; `ComposerContext.Blocks` carries the message |
| `storage` (get/set/delete/clear/keys/entries), `Cache(name, ttl, max)`, `storedFileUrl` (stored blobs, or the `file` setting's path) | js over Taut's data store |
| `settings.set` (saved now, applied after), `settings.addTab` | manager.go, Taut's Preferences tabs |
| `userAPI`, `fetch` (`{status, body}`, no CORS), `loader` (`'electron'`), `Store`, `id` | Taut / jsrt |
| `main.call` / `main.on` | the main half in the same runtime, args and results cloned like IPC, Slick's error strings |
| Slick's `SlickBridge` (`getBridge()`) for plugins bundling Slick's shared code | js/10_plugin.js |

**MainCtx** (src/shared/main.ts), capabilities enforced (`used "x" without declaring it`): `settings`, `onSettingsChange`, `log`, `emit`/`emitTo`, `storage`, `secrets` (0600 store), `net.fetch` (Go HTTP, no CORS), `net.block`/`net.intercept` (applied to every fetch in the runtime; Clack loads no Slack web assets, so these see plugin traffic), `protocol.register` (boot only; `fetch('<scheme>://…')` reaches the handler), `notifications.filter` (one chain over the `showNotification` thunk, Electron-shaped options), `shell.openExternal`, `dialog.openFile`, `cookies` (Taut's cookie host), `sessions` (one session in Clack), `switches` (recorded; no Chromium), `frames.onFrame` and `media` (no frames or screen capture in Clack: accepted, never called). rpc handlers' `sender` is the window's WebContents: `id`, `getURL()` (the client URL of the current route), `session.fetch` (a `Response`, through the same path as `net.fetch`, no Slack cookie added), and `on`/`once`/`removeListener`, where `before-input-event` gets the window's keydown/keyup (`preventDefault` keeps the key from the page) and `destroyed`/`did-start-navigation` never fire (Clack's one window isn't destroyed or navigated while main halves run). Lifecycle: `boot` once per runtime, `ready` when enabled (its disposer runs on disable), `window` once, settings listeners on change; a renderer restart doesn't restart the main half. Node for main halves: `fs` (read-only, and only files a plugin's `file` setting points at), `path`, `os`, `stream.Readable.toWeb`, `crypto.createHash`, `util.promisify`, `events`, `url`, `process`, `Buffer`; other builtins load and throw `ENOSYS` when used. `electron` is a stub.

## Acceptance (`.github/workflows/slick.yml`)

Clones Slick, builds every plugin with build.mjs (artifact `slick-plugins`, never committed), then `go test ./internal/slick/...` with `SLICK_PLUGINS_DIR`. For each plugin: import the built file(s) as a user does, enable it (with the settings it needs to act), assert it starts with no error, list what it registered (components, thunks, slices, styles, RTM types, send hooks, net rules, protocols, notification filters, tabs), run its behaviour check, disable it and assert everything it registered is gone and stop() ran, then enable it again. Taut's workflow runs Taut's 40-plugin acceptance on every taut change.

Behaviour checks (synthetic workspace, fake Slack API, no network): HumanCount (header count), Censorship (message view masked), Nicknames (member view renamed), SilentTyping (typing thunk swallowed), NotShitMarkdown (`**x**` sent bold), ClearURLs (main half fetches rules, links cleaned on send), UserPronouns (pronouns rendered), IgnoreTime (stylesheet), OfficeHours (`users.setPresence` away inside a window), oneko (cat in the document, mousemove forwarded), NoTrack (telemetry fetch blocked), ShutUpSlackbot (slash-command notices dropped, others shown), Experiments (forced group in state, Preferences tab), HaikuWarning (a 5-7-5 message waits for the dialog), HcaStatus (main-half lookup marks the sender), CustomFonts (font stylesheet), StreamerMode (notifications silenced, main half's `setActive`), ChatTabs (Ctrl+W through the main half's `before-input-event`), InstalledApps (marketplace page through `sender.session.fetch`, no cookie), RestrictedChannelWarning ("Post anyway" in the roadblock). The others are checked for start, registration and clean teardown.

Results are in the latest `slick` run's "acceptance summary" step (`ACCEPT n/53 pass: …`).

## Known gaps

- Component boundaries: plugins patch ~58 names Taut's catalogue didn't have (PLUGINS-components.md §9). Patches register and render through slackc, but the UI owners still have to call `slackc.Render` at those boundaries for them to show in Clack's native views.
- Huddle plugins (BetterHuddles, CallTimer, LetMeSee, HuddleMuteDetector, CustomNameRecording) start and patch, but Clack has no huddle media, so their surfaces never render.
- Pop-out windows (`onDocument`), frames (QuietSpotify's Spotify embeds), Chromium switches and spellcheck (Snappy's main half) have no Clack equivalent: accepted, inert.
- AccountSwitcher's main half moves Slack session cookies between accounts; in Clack that maps to Taut's cookie host and one session, so switching accounts goes through Clack's own sign-in.

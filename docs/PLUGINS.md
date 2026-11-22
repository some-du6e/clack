# Taut plugin compatibility (native)

Requirement (Karim, 2026-11-07): "i should be able to import any taut plugin without any ifs", "it should not be ports, it should be native", "best one and fastest, make no mistakes".

Taut (https://github.com/jeremy46231/taut, GPLv3) plugins are JS/TSX classes extending `TautPlugin`. They use `this.api` to patch Slack's React components by name, patch Slack's Redux store (state, slices, thunks), listen to RTM, transform sent messages, add composer buttons, menus, modals and preferences tabs, inject CSS, and store data. Clack runs them **unmodified** with a React Native-style design:

```
 Taut plugin .js (user-built with taut's `npm run build:user-plugin`, or fetched)
        │ imports "$taut", React (JSX), uses this.api.*
 ┌──────▼─────────────────────────────────────────────────────────────┐
 │ internal/jsrt   JS engine (QuickJS on wazero: pure Go, ES2023)    │
 │                 event loop, timers, Promise jobs, fetch, Abort*,  │
 │                 URL, TextEncoder, localStorage, console, ESM      │
 │                 real React + react-reconciler (MIT, embedded)     │
 │                 host config → JSON host-tree ops to Go            │
 ├────────────────────────────────────────────────────────────────────┤
 │ internal/taut   "$taut" module: TautPlugin, opt, PLUGIN_CATEGORIES│
 │                 the full TautAPI (see the taut repo's app/pluginManager.ts, │
 │                 app/api/*, app/slack/*) over internal/core:        │
 │                 a Slack-shaped Redux mirror (channels, members,   │
 │                 messages, membershipCounts, readOnlyChannels…),   │
 │                 named thunks, rtm.on, messages/composer hooks,    │
 │                 storage/secrets/Cache/SharedStore/Store, modal,   │
 │                 menu, preferences.addTab, experiments, webpack    │
 │                 byName exports, plugin manager + settings model   │
 ├────────────────────────────────────────────────────────────────────┤
 │ internal/ui/slackc  Slack component registry for native widgets:  │
 │                 slackc.Render(gtx, "MessageSender", props, native)│
 │                 no patch → native fast path (zero JS, zero alloc) │
 │                 patched → JS renders, host tree laid out natively │
 │                 (flexbox subset, inline styles, Slack class CSS), │
 │                 <Original/> calls back into the native widget     │
 │                 CSS engine for api.setStyle; api.elements widgets │
 └────────────────────────────────────────────────────────────────────┘
```

## Non-negotiables
- **Fast:** with no plugins enabled, Clack must not get slower: no JS engine start, no per-frame work. With plugins, JS stays off the frame hot path. Patched renders are cached by props/state version, and the JS thread never blocks the UI goroutine.
- **Unmodified plugins:** the acceptance test is every plugin in taut's `plugins/` (39 + the disabled one), built with taut's own `build:user-plugin`, importing and behaving like it does in Taut on real Slack. Third-party plugins built against the same documented API must work too.
- **Policy:** plugin storage (`api.storage`, `localStorage`) may persist on disk, because that's the plugin's own data, like Taut. Clack itself still never persists message contents.
- **License:** Taut is GPLv3. Don't copy Taut source into Clack. Read it to learn the API contract and the behaviors, and implement them independently. Plugins are loaded by the user at runtime and aren't bundled in the repo.

## Contracts
### `internal/jsrt` (owner: jsrt agent)
Engine: QuickJS-NG v0.18 compiled to wasm32-wasip1 with our own ABI (`internal/jsrt/csrc/glue.c`, built by `.github/workflows/jsrt.yml`, committed as `internal/jsrt/jsrt.wasm`), run on wazero (pure Go, no cgo, all three platforms). Full ES2023+ (private fields, async generators, Proxy, WeakRef, BigInt, RegExp lookbehind / `\p{}` / `/v`, `Object.groupBy`, Set methods, iterator helpers…). Nothing is created until `New`.

```go
type KV interface { Get(k string) (string, bool); Set(k, v string); Delete(k string); Keys() []string; Clear() }
type Options struct {
	Fetch          func(ctx context.Context, req *http.Request) (*http.Response, error) // nil = http.DefaultClient
	Storage        func(namespace string) KV // "localStorage" / "sessionStorage"; nil = memory
	Log            func(level, msg string)   // console.*; level log|info|warn|error|debug
	CacheDir       string                    // wazero compiled-code cache (fast later starts)
	Timeout        time.Duration             // per JS task; runaway code is interrupted (default 5 s)
	MemoryLimit    int                       // JS heap (default 512 MiB)
	OpenURL        func(url string)          // window.open, <a href> clicks, location.assign
	ClipboardWrite func(text string) error; ClipboardRead func() (string, error)
	PickFiles      func(accept string, multiple bool) ([]PickedFile, error) // <input type=file> picker; nil files = cancelled
	Language, Platform string                // navigator.language / navigator.platform
}
func New(opts Options) (*Runtime, error)   // lazy: only when the first plugin is enabled
func (r *Runtime) Close(); Done() <-chan struct{}
func (r *Runtime) Module(name, src string)                 // ES module by bare name; react, react/jsx-runtime, react-dom(/client), scheduler are built in
func (r *Runtime) Import(name string) (Value, error)       // module namespace
func (r *Runtime) Eval(name, src string) (Value, error)    // classic sloppy script → completion value (a Taut plugin IIFE → its class)
func (r *Runtime) Call(ctx context.Context, path string, args ...any) (json.RawMessage, error) // "a.b.c" from globalThis, awaits Promises
func (r *Runtime) Expose(name string, fn HostFunc)         // globalThis.__clack[name]
func (r *Runtime) ExposeJSON(name string, fn func(args []json.RawMessage) (any, error))
func (r *Runtime) ExposeAsync(name string, fn func(ctx context.Context, args []json.RawMessage) (any, error)) // → Promise, own goroutine
func (r *Runtime) Do(f func(js *Ctx) error) error          // on the JS thread, waits (inline when already on it)
func (r *Runtime) Post(f func(js *Ctx))                    // async
func (r *Runtime) Stats() Stats                            // Boot duration + Phases, JS heap bytes
type HostFunc func(js *Ctx, this Value, args []Value) (any, error) // returned Go values converted by js.Value
```
`Value` (zero = undefined) is a GC-tracked handle; use its methods only on the JS thread: `Type, IsUndefined/IsNull/IsNullish/IsBool/IsNumber/IsString/IsObject/IsArray/IsFunction/IsPromise/IsError, Bool, Float, Int, String, Get, Index, Len, Set, SetIndex, Delete, Has, Keys, Call, CallThis, Method, New, JSON, Decode, Export, Bytes, Then(func(Value, error)), StrictEquals, Free`. `*Ctx`: `Global, Undefined, Null, Eval, Import, ParseJSON, NewObject, NewArray, NewError, Func(name, HostFunc), NewPromise() (p, resolve func(any), reject func(error))` (settle from any goroutine), `Value(any)` (nil→null, numbers, string, []byte→Uint8Array, json.RawMessage→parsed, error→Error, HostFunc→function, else encoding/json). JS exceptions come back as `*JSError{Name, Message, Stack}`; interrupted tasks as `ErrTimeout`.

Globals (env.js): `React` (real React 19.3, vendored), `ReactDOM` (`createPortal`, `createRoot`, `flushSync`, …), the event loop (`setTimeout/setInterval/setImmediate/queueMicrotask/requestAnimationFrame/requestIdleCallback`, `MessageChannel`), `fetch/Request/Response/Headers/FormData/Blob/File/FileReader/ReadableStream`, `XMLHttpRequest` (over fetch), `AbortController/AbortSignal(.timeout/.any/throwIfAborted)`, `EventTarget/Event/CustomEvent/DOMException`, `URL/URLSearchParams`, `TextEncoder/TextDecoder`, `atob/btoa`, `crypto.getRandomValues/randomUUID/subtle.digest`, `structuredClone`, `localStorage/sessionStorage`, `console`, `performance.now`, `navigator` (`language, platform, clipboard, locks, sendBeacon, userAgent, mediaDevices` stub), `location/history` stubs, `BroadcastChannel`, `Intl` (DateTimeFormat with timeZone and formatToParts, NumberFormat, RelativeTimeFormat, PluralRules, ListFormat, Collator, Segmenter, DisplayNames), `RegExp.escape`, and a DOM: `document/window`, Element/Text with `style` (CSSStyleDeclaration incl. `--vars`), `classList`, `dataset`, attributes, `querySelector(All)/closest/matches`, `addEventListener`, `getBoundingClientRect` (from `SetRects`), `MutationObserver`, `ResizeObserver`, `IntersectionObserver`, `getComputedStyle`, `matchMedia`, `OffscreenCanvas` text metrics. React host instances are these DOM elements, so refs and `addEventListener` on them work. `document.body` children are overlays; `<style>` in `document.head` is CSS.

**Host tree** (JS → Go), one JSON array of top-level nodes per root per commit:
- element `{"t":"div","k":17,"p":{…},"on":{"click":17,"keydowncapture":17},"c":[…]}`. `k` is the stable host-instance id (same across commits). `p` holds every non-function, non-children prop as React has it: `className`, `id`, `style` (raw React style object, camelCase, numbers unitless; DOM `style.*` writes land here too), `src`, `href`, `value`, `checked`, `disabled`, `placeholder`, `type`, `tabIndex`, `title`, `role`, `aria-*`, `data-*`, SVG attrs in React names (`viewBox`, `d`, `fill`, `strokeWidth`…), `dangerouslySetInnerHTML` is parsed into child nodes (no `__html` in `p`). Form state is in `p` too: `value`, `checked`, `selected`, `indeterminate`, `selectionStart/End` (focused input), `scrollTop/Left`. The focused node carries `"focus":true` (beside `k`, not in `p`), so `el.focus()` from JS reaches the UI. A `<dialog>` opened with `showModal()` carries `"modal":true` the same way (no attribute plugins could see): slackc puts it in the top layer, centred over the window above its `::backdrop`, which takes the clicks meant for the page under it (they go to the dialog, as in browsers), moves key focus to what JS focused in it, and the shell's shortcuts stand down while it is open. `show()` opens it in place (`position: absolute`); a closed one isn't drawn. Escape on a modal fires `cancel`, then (unless prevented) closes it and fires `close`. `on` lists handled events: React prop name lowercased without `on` (`click`, `mouseenter`, `doubleclick`, `change`, `clickcapture`…) plus DOM `addEventListener` types; the value is always the node's own `k`.
- text `{"t":"#","v":"…"}`.
- Slack component `{"t":"@","n":"BaseAvatarStack","k":18,"p":{…},"c":[…rendered children]}` from `__clack.native(name)`. In `@` props, functions become `{"$fn":id}` (call with `rt.CallFn(id, argsJSON)`), React elements become `{"$el":[nodes]}`, `{"$gofn":id}` objects pass through untouched.

**Roots:** `root := rt.NewRoot(func(tree []byte))`, `root.ID() uint64` (Go ids count from 1), `root.Render(js, element)` (JS thread), `root.Unmount()`. From JS: `__clack.createRoot()` → id with bit 52 set (`1<<52 | n`; JS numbers are float64), `__clack.render(id, el)`, `__clack.unmount(id)`; their commits go to `rt.OnCommit(func(id uint64, tree []byte))`. State/hook updates re-render asynchronously and deliver a new whole tree; at most one commit per root per JS task. A commit of JS's own updates (state, a store or patch-version subscription, a portal mounted under a native) delivers only when a host node changed: React commits a root whenever an update ran in it, and those commits used to send the same tree again. A native ('@') or slot node rendered again with the same props (the very values, compared with `Object.is`) is no change, and Slack components are memoized below what plugins see: a patch that hands its `<Original>` the props it had stops there. So a plugin changes what a Slack component draws by passing it new props; an object or array it mutates in place under the same reference shows only at the next real change (React's own rule). A new object equal to the old one still re-serializes, and Go drops the same tree. What Go renders always answers: `__clack.render` (`Root.Render`), and `__clack.portalInto` (`Root.RenderPortal`), an empty tree too, at once when the native it goes into is mounted and otherwise when it mounts. Callbacks run on the JS thread; the byte slice is the receiver's.

**Events** (Go → JS): `rt.DispatchEvent(target int, ev []byte)` (async) / `rt.DispatchEventSync(ctx, target, ev) (EventResult{DefaultPrevented, PropagationStopped}, error)`. `target` is the `k` of the innermost hit node (0 = document, for window-level keys). JS runs DOM capture → target → bubble with React handlers and DOM listeners interleaved like a browser (`stopPropagation`, `preventDefault`, `currentTarget`). Event JSON: `{"type":"click","clientX":…,"clientY":…,"button":0,"buttons":1,"key":…,"code":…,"altKey":…,"ctrlKey":…,"shiftKey":…,"metaKey":…,"repeat":…,"deltaX":…,"deltaY":…,"target":{"value":…,"checked":…},"rects":{"<k>":[x,y,w,h],…}}`. Types are DOM names (`click`, `dblclick`, `contextmenu`, `mousedown/up/move`, `mouseenter/leave` (not bubbling: send one per node whose hover changed), `pointer*`, `keydown/up`, `input` (text edits; drives React `onChange`), `change` (checkbox/select), `focus/blur` (React `onFocus/onBlur` bubble), `wheel`, `scroll`, `submit`). Default actions run in JS when not prevented (`<a href>` → OpenURL, submit buttons → form `submit`).
**Layout back:** `rt.SetRects(map[int]jsrt.Rect{X,Y,W,H})` after layout (changed nodes only; feeds `getBoundingClientRect`, `offsetWidth`…, fires `ResizeObserver`), `rt.SetViewport(w, h, dpr float32, dark bool)` (innerWidth/innerHeight, `resize`, `matchMedia('(prefers-color-scheme: dark)')`). `rt.WantsEvent(type) bool` (cheap, any goroutine): JS has a window/document listener for that type, so forward global input (e.g. `mousemove` for Oneko, `keydown` for StreamerMode's shortcuts) to target 0 only then. Keys go there only when no widget took them: the shell asks for them last in the frame (Gio gives a key to the first matching `Event` call, and an empty `key.Filter` name matches every key, so asking first took the focused composer's Enter). `rt.SetHidden(bool)` (document.hidden / visibilitychange / window focus+blur). **Document:** `rt.OnDocument(func(body []byte, css []string))`: `document.body`'s children as a host tree (overlays) and every `<style>` text, after each change. `rt.OnDocumentProps(func(html, body []byte))`: the `p` (attributes + inline style, e.g. `className`, `--css-vars` in `style`) of `<html>` and `<body>`, which global CSS selectors depend on (StreamerMode's `documentElement` class, FasterSlack's CSS variables).

### `internal/taut` (owner: taut-api agent)
- `Manager`: loads/enables/disables plugins (user-imported `.js` files under `~/.config/clack/taut/plugins/`, plus "install from URL"), config per plugin (`config.json` semantics like Taut: only set options stored, `migrateConfig`, secrets in a separate store), restart-on-config-change, and the 5 s start/stop timeout.
- The `$taut` module: everything a plugin imports.
- The TautAPI: implemented 1:1 against Taut's surface. For the names the published plugins use, see the catalogue below.
- The settings model for **Preferences > Taut** (plugin list by category, toggles, every `opt.*` control kind, custom `editor`s rendered via React, the plugin's `settingsData` component, plugin authors, Hack Club–only filtering). ui-shell draws the tab with native widgets plus host-tree rendering for React editors.
- Implemented: the Go API UI owners call (wiring, `taut.Dispatch` and the thunk catalogue, composer/message hooks, patched views, settings) is in [PLUGINS-taut.md](PLUGINS-taut.md). The Redux mirror's slices and shapes are in [PLUGINS-state.md](PLUGINS-state.md). Every member of the TautAPI and SlickAPI, how often their plugins use it and how Clack's compares (present, differs, missing), and the Slack thunks they name, are in [PLUGINS-api-compat.md](PLUGINS-api-compat.md). The `taut` workflow builds every Taut plugin with `build:user-plugin` and runs the acceptance suite against them.

### `internal/slick` (owner: slick-api agent)
Slick plugins (https://github.com/3kh0/slick) run unmodified on the same runtime through `taut.Extension`: the `$slick` module, each plugin's SlickAPI over the TautAPI internals, privileged `main.ts` halves against a Go-backed MainCtx, Slick's settings.json, themes and custom CSS (pure Go), and the Preferences > Slick model (`Slick.Settings()`). Design, wiring and coverage: [PLUGINS-slick.md](PLUGINS-slick.md). The `slick` workflow builds every Slick plugin with Slick's own build and runs the acceptance suite.

### `internal/ui/slackc` (owner: taut-ui agent)
```go
type Props map[string]any
func Render(gtx layout.Context, name string, props Props, native func(gtx layout.Context, p Props) layout.Dimensions) layout.Dimensions
func Patched(name string) bool   // cheap check
```
The UI package owners (ui-messages, ui-composer, ui-popups, ui-views, ui-shell) wrap their widgets at Slack's component boundaries with these names and **Slack-shaped props** (e.g. `BaseAvatarStack{channelId, memberCount}`), and their native funcs honour prop overrides coming back through `<Original {...props}>`. taut-ui owns the catalogue (name → props → owner) in `docs/PLUGINS-components.md`, and the host-tree layout + CSS engine.

## Catalogue used by taut's published plugins (from source, 2026-11-07)
- **patchComponent (≈60 names):** BaseAvatarStack, MessageSender, BaseMessageSender, ThreadSenderAndTimestampGeneric, BaseBroadcastPreamble, ConnectedBaseAvatar, Blocks, ViewBlocks, MessageListItem, MessageBackground, MessageActions, MessageActionButtonReaction, Reaction, ReactionBar, ReactionAddButton, UnstyledButton, ThreadSearchButton, ThreadTypingIndicator, ThreadFlexpaneAdminTools, MessagePaneInput, MessageInput, InputContainer, WysiwygContainer, TextyButtons, MessageInputRoadblock, ShortcutsMenuTriggerButton, EmojiPicker, EmojiPickerPopover, ActivityItem, ActivityPrivacyBlurredName, ActivityRowContentSenderAvatarLayout, ListChannelEntity, BaseMrkdwnChannel, BaseMessageAttachmentSlackMessage, MessageAttachmentHuddleLink, RimetoMemberProfileOverflowMenu, MemberProfileHoverCard, … (taut-ui completes the list, including names passed in variables)
- **patchThunk:** currentUserStartedTyping, markLastRead, setUserPrefByApi, deleteDraftApi, getChannelMembershipInfoForUsers, enqueueHuddleToast, maybeFetchBackgrounds, startEventLog, stopEventLog, stopHuddle (+ `waitForThunkCreator`)
- **patchSlice / patchState:** members, readOnlyChannels, threadOnlyChannels (+ getRawState, getStore, useReduxState, usePatchVersion, refresh)
- **webpack exports (byName / waitForExport):** I18n, getPrefCompliantMemberName, getBroadcastKeywordsForUser, getInputProxy, isInputRegistered (Slick's bChannel also finds `canUserAtChannelInChannelById` by name)

### @channel / @here / @everyone (NativeAtChannel, bChannel)
Slack decides who may use them from the permission system, not from team prefs or channel properties: edgeapi `permissions/info` `{user_id, permissions: ["CAN_AT_CHANNEL", "CAN_AT_EVERYONE"], channel_id}` → `{allowed: [...]}`, cached an hour (core/broadcast.go has the full port and evidence). On it sit `canUserAtChannelInChannelById`, `canUserAtEveryoneInChannelById` (only #general) and `getBroadcastKeywordsForUser`; Clack's JS versions (taut/js/05_slack.js) ask core through `taut_broadcast` and never block. The @ autocomplete offers exactly those keywords unless `includeAllBroadcastKeywords`; `prepareAndSendMessage` runs a slash command first (a plugin's rewrite, e.g. NativeAtChannel's `/channel …`) and otherwise refuses a keyword the viewer may not use with Slackbot's local "Only visible to you" message (`hasProhibitedKeywords`'s strings), keeping the draft. Deltas carry broadcasts as Slack's mention ops (`{insert: "@channel", attributes: {slackmention: {id: "BKchannel"}}}`) and reach JS as `Delta` instances, because both plugins read and rebuild them.

The send runs in Slack's order: (0) `channels.prefs.get` `enable_at_channel` (Slack's sendDraftMessage, before the thunk), (1) the "Notify N people?" confirmation when the options carry `includeBroadcastKeywordWarning` (the channel composer passes it, the thread composer doesn't), (2) the slash command, (3) `hasProhibitedKeywords`, (4) the post. Three inputs besides the permissions:
- **Team prefs** come from `team.prefs.get` at boot, Slack's own reload of `state.teamPrefs` (its boot response carries them; Clack doesn't call client.boot), and from `team_pref_change` (core/teamprefs.go).
- **The large-channel stop** (`shouldStopChannelMentions`, Grid only): a conversation other than a DM whose member count (edge `users/counts` `people`) is at least `loud_channel_mentions_limit` loses @channel/@here (and @everyone) for non-admins: the @ rows drop them and the send gets "Usage of *@channel* is restricted in this large channel." Hack Club's limit is 10000.
- **`enable_at_channel`** (`isBroadcastKeywordsEnabled`): `{}` or missing means enabled, else its `.enabled`. It covers both keywords ("*@here* has been disabled in this channel, so your message cannot be sent."); `enable_at_here` is only written alongside it by the channel settings dialog.
- **The confirmation** (`shouldWarnBeforeBroadcast`, `MessagePaneInputBroadcastWarning`): team pref `warn_before_at_channel` `always` / `once` (never seen) / `daily` (not seen today) / `never`, against the user pref `last_seen_at_channel_warning` that the dialog writes when it opens. It only applies to allowed keywords, in conversations of more than 5 people. The dialog has an illustrated header (Slack's is its rooster artwork; Clack, which commits no Slack assets, draws a megaphone on the same strip), "Notify {people − 1} people[ across {timezone_count} time zones]?" (`conversations.info` `timezone_count`, `display_counts`; @here: "Notify everyone currently online in #channel?"), "Edit Message" (the draft stays) and "Send" (or the scheduled variants and "Schedule Message"), and for admins "Pssst, admins… You can control this warning in Workspace settings." There is no "don't ask again". Cancelling rejects with Slack's "Shouty rooster in effect". The relay plugins only rewrite sends Slack refuses, so the confirmation never shows for them, and a plugin that hands the options back untouched keeps `includeBroadcastKeywordWarning`.
- **Other API:** within, signal, storage.store, secrets, Cache, SharedStore, Store, sharedFrom, modal.openModal/confirm, menu, elements (Button, Tooltip, SvgIcon, IconButtonBase, …), setStyle, experiments.set, members.useMember/getCurrentMemberId, channels.getCachedChannel, messages.getRawMessage/injectMessages/patchMessageText/patchActionableMessage, onMessageSendBlocks, composer.addButton/addSendCheck/addLinkTransform/patchInlineMarkup, rtm.on, huddles.*, workspace.onChange, fetch, cookies, accounts, preferences.addTab, scheduleEnd, deferResizeWork, plus some DOM (document/querySelector/MutationObserver in StreamerMode, FasterSlack, Oneko, CustomNameRecording)

## Plugin Library (`internal/pluginlib`)
Clack shows Taut and Slick as one plugin system, in Taut's own Preferences UI (`internal/ui/plugins_prefs.go`, `taut_prefs.go`; Karim 2026-11-10: Taut is the primary system). After Slack's Preferences tabs come **Plugins** and **Themes** (Slick's, while Slick runs), then the plugins' own tabs. Plugins is Taut's tab: rows to Appearance (custom CSS), Advanced (installing a plugin from a file or URL, where a file with Slick's plugin header goes to Slick; Taut's config directory; Clack's one row, Run Slick plugins; config.json) and About (credits), and a row to the **plugin library**; then Taut's "Search plugins", its pills (All, Enabled, Disabled, Hack Club only, User plugins) and every installed plugin of both systems under Taut's groups, each Taut's row (checkbox, name and description, a caret to that system's own page). A search nothing installed matches suggests the library's plugins that do, each with Install, and links to all results in the library. The library is every official Taut and Slick plugin, one row per plugin: a plugin both systems have (Clear URLs, Censorship, Oneko, …, and plugins with a name of their own in each, like Taut's No Tracking, Slick's No Track: the pair table below) is one row with both tags; Install installs the plugin bench's pick where the bench checked what both versions do, and the row says which version that is; without such a check (costs alone) or without bench data, Install asks which system while both run and neither has it. A row shows its description, authors (or upstream contributors) and the source commits it was built from; the chips are All, Taut, Slick, Installed and Updates; Install and Update are one click. The library keeps one shared taxonomy (`plugin_categories.go`); the Plugins list uses Taut's groups. The old tab ids (`taut`, `slick`, `slick_plugins`, `slick_install`, `library`, `plugin_settings`) open their new homes. (Until 2026-11-08, Taut, Slick's four tabs and the Library were separate tabs; until 2026-11-10 the list was Slick-styled, with a Plugin settings tab.)

- **Where the bundles come from.** Neither upstream publishes per-plugin builds (Taut ships them inside `taut.js`, Slick inside its app builds). `.github/workflows/plugin-library.yml` clones upstream `main`, builds every plugin with upstream's own build (Taut's `build:user-plugin`, Slick's build via `internal/slick/build/build.mjs`), runs Clack's Taut and Slick acceptance suites against exactly those bundles, and publishes them as the assets of the `plugins` **pre-release** of this repo (so the update banner's `releases/latest` never sees it). It runs weekly, on demand (`workflow_dispatch` with `taut_ref`/`slick_ref`), and when the workflow or `.github/scripts/plugin-index.mjs` changes on main, ui or live-taut. The build job has a read-only token and no caches; only the publish job can write releases, and it runs no third-party code.
- **GPLv3.** The bundles are third-party GPLv3 code and are never committed. The release carries `taut-LICENSE` and `slick-LICENSE`, and its notes and `index.json` give the upstream repo, commit and source path of every plugin (the corresponding source).
- **`index.json`** (schema 1): `sources.{taut,slick}` = `{name, repo, commit, license, licenseFile, build}`, and `plugins[]` = `{system, id, name, description, category, categoryLabel, authors[{name,url,slackId?}], contributors[], hackClubOnly, defaultEnabled, upstreamDisabled, source, file, sha256, size, main?{file, sha256, size}}`. Asset names are `taut-<Name>.js`, `slick-<Dir>.js` and `slick-<Dir>.main.js`.
- **Client.** Nothing is fetched until the Library tab opens. `pluginlib` uses a plain https-only client (no Slack cookies, no credentials, https-only redirects), caps sizes, checks every download's sha256 against the index, and caches `index.json` under `<config>/cache/plugin-library/` for offline listing. Installing calls `Taut.Import` / `Slick.Import` (renderer, then `.main.js`), so a Library plugin is an ordinary user plugin in that system's `plugins/` dir. An entry shows **Update** when the installed file's sha256 (`Taut.InstalledSHA256`, `Slick.InstalledSHA256`) differs from the index. `CLACK_PLUGIN_LIBRARY` overrides the index URL (`off` turns the Library off; tests use it).

### One plugin in both systems (`pluginlib.Pairs`, `libraryGroups`)
Many Library plugins exist in Taut and in Slick: the same plugin, written for each client. The Library lists each once, one row with both system tags. Where the plugin bench checked what both versions do (a behaviour check: `internal/pluginbench/bench.json`, docs/PLUGIN-BENCH.md), Install installs its pick and the row says which version that is ("Install adds Slick's version (recommended)."); the chevron beside Install ("Install for") offers both, the pick first with the bench's reason, so the other stays one click away. Where the bench only weighed costs (no behaviour check on either side: `pluginbench.Pick.Checked`), or hasn't measured the pair, nothing is recommended: costs can't tell a version that does nothing in Clack from one that works (Bring Back Slackbot did nothing and won on frame cost), so Install asks which ("Install…", "Install asks which: Taut's version or Slick's."). In "Install for" a version that doesn't fully work in Clack says how it works (its `pluginlib.CompatOf` line). Once either version is installed, the row shows that one, whichever it is (its name and description, Settings for it, "Slick's version is installed."), and nothing swaps it for the other; with both installed the chevron opens either one's settings, and an installed version that does nothing in Clack keeps the other in the chevron's "Install for". Where the other version has a name of its own, the row says it ("In Slick it's “No Track”."). Search finds the row under either version's name or description, and the Taut and Slick chips both list it. The plugins page's search suggestions ask which version of such a plugin ("Install…", the pick first): they have no chevron. A row is filed and sorted as the version it shows before either is installed (the pick, else Taut's), under that version's category by its name; installing the other version renames the row where it is rather than moving it away from the click (there's no upstream library of both systems' plugins to follow). A plugin in one system keeps its row as it was.

A pair is one plugin when both systems use its name (spaces and case aside: "Clear URLs" and "ClearURLs"), or, for plugins with names of their own, when both sources do the same thing: the same change to the same part of Slack, through the same outside services, whatever extras one of them adds. `pluginlib.Pairs` (pairs.go) is the one source of the second kind, and this table is its record (`TestPairsMatchDocs`); bench.json follows it and pairs nothing itself (`TestBenchPairsAreLibraryRows`, and `TestPluginBench` fails on a result for variants the Library doesn't list as one row). The sources were read at the Library's commits (taut 6fe5123, slick b582605). The bench measures every pair (`TestPluginBench` takes its pairs from `libraryGroups`), a run limited to some plugins (`only=`) keeps the rest of the committed data (`report -keep`), and results match by the variants' systems and ids, whatever a plugin's display name.

| Plugin (Taut / Slick) | Taut id | Slick id | Paired by | Why it's one plugin |
|---|---|---|---|---|
| Account Switcher | `AccountSwitcher` | `AccountSwitcher` | name | |
| Admin Backend | `AdminBackend` | `AdminBackend` | name | |
| Better Typing Indicator | `BetterTypingIndicator` | `BetterTypingIndicator` | name | |
| Censorship | `Censorship` | `Censorship` | name | |
| Clear URLs | `ClearURLs` | `ClearURLs` | name | |
| Click to Load | `ClickToLoad` | `Click2Load` | name | |
| Copy Reacted | `CopyReacted` | `CopyReacted` | name | |
| Custom Name Recording | `CustomNameRecording` | `CustomNameRecording` | name | |
| Experiments | `Experiments` | `Experiments` | name | |
| Haiku Warning | `HaikuWarning` | `HaikuWarning` | name | |
| Human Count | `HumanCount` | `HumanCount` | name | |
| Last Seen | `LastSeen` | `LastSeen` | name | |
| Nicknames | `Nicknames` | `Nicknames` | name | |
| Office Hours | `OfficeHours` | `OfficeHours` | name | |
| Oneko / oneko | `Oneko` | `oneko` | name | |
| Quick Join | `QuickJoin` | `QuickJoin` | name | |
| Restricted Channel Warning | `RestrictedChannelWarning` | `RestrictedChannelWarning` | name | |
| Show Real User | `ShowRealUser` | `ShowRealUser` | name | |
| Show Sending Bot | `ShowSendingBot` | `ShowSendingBot` | name | |
| Shut Up Slackbot | `ShutUpSlackbot` | `ShutUpSlackbot` | name | |
| Silent Typing | `SilentTyping` | `SilentTyping` | name | |
| Slim Message Box | `SlimMessageBox` | `SlimMessageBox` | name | |
| Streamer Mode | `StreamerMode` | `StreamerMode` | name | |
| User Pronouns | `UserPronouns` | `UserPronouns` | name | |
| Who Reacted | `WhoReacted` | `WhoReacted` | name | |
| No Tracking / No Track | `NoTrack` | `NoTrack` | sources | Both stop Slack's tracer and telemeter through their no-op paths and fail its tracking requests: fetch, XHR and beacons to slackb.com, `/beacon` and `/clog` (Taut's list adds a few API paths, such as `/metrics`). Karim reported the two rows on 2026-11-22. |
| Anonymize Filenames / Anonymise File Names | `AnonymizeFilenames` | `AnonymiseFileNames` | sources | Both rename each upload to 7 random letters and digits, keeping its extension. Slick's adds a per-file toggle on the upload and a setting to drop the extension. |
| Private Channel / Private Channel Mapper | `PrivateChannel` | `PrivateChannelMapper` | sources | Both name the private channels you can't see and let you mention the ones you aren't in, from the same flaron index (flaron.halceon.dev). Slick's says it's adapted from Taut's and keeps mentions behind a setting (off). |
| Real Markdown / Not Shit Markdown | `RealMarkdown` | `NotShitMarkdown` | sources | Both make the composer take standard Markdown instead of Slack's markup. Taut's formats as you type, Slick's converts when you send. |
| IDV Status / HCA Status | `IdvStatus` | `HcaStatus` | sources | Both underline senders by their Hack Club identity verification, unverified in red (`#e01e5a`) and over 18 in orange (`#d97706`), from the same check API (Taut's at identity.hackclub.com, Slick's at auth.hackclub.com). |
| Bring Back Slackbot / Custom Slackbot | `BringBackSlackbot` | `CustomSlackbot` | sources | Both show workspace custom responses as sent by Slackbot: Taut's turns Slack's `reskin_custom_responses` experiment off, Slick's restyles them, by default with Slackbot's name and avatar and no badge, and lets you change both. |

What Install does for the six with names of their own. Only Bring Back Slackbot / Custom Slackbot has a behaviour check; the other five have none, so Install asks which, and each is safe to leave to that choice:

| Pair | Install | Why |
|---|---|---|
| Bring Back Slackbot / Custom Slackbot | Slick's, recommended | The bench draws a workspace custom response: Taut's only turns off `reskin_custom_responses`, which neither Slack's client nor Clack reads any more, so the response stays the team's (`pluginlib.CompatOf`: doesn't work in Clack); Slick's puts Slackbot's avatar on it. The name stays the team's in both, as in Slack. |
| No Tracking / No Track | asks which | No check can see a difference: Clack draws Slack natively, so none of the telemetry both block (Slack's tracer, telemeter and beacons) runs in Clack, and both block the same requests a plugin might make. Both ✅ in the live docs (`taut` NoTrack, Slick's NoTrack check). |
| Anonymize Filenames / Anonymise File Names | asks which | Both rename uploads in Clack (`composer-taut` TestTautAnonymizeFilenames, `TestSlickViewAnonymiseFileNames`); the bench's client has no upload path to check. Slick's says it's partial in the menu (not in the edit-file dialog). |
| Private Channel / Private Channel Mapper | asks which | What both draw comes from the flaron index, an outside service a bench check would depend on; both are partial in Clack and the menu says how. |
| Real Markdown / Not Shit Markdown | asks which | Both work (`composer-taut` TestTautRealMarkdown; Slick's NotShitMarkdown check), at different moments: Taut's as you type, Slick's on send, a choice the user can make. |
| IDV Status / HCA Status | asks which | Both underline senders from the same Hack Club identity check, an outside service a bench check would depend on (`messages-taut` TestPluginIdvStatusSenderClick, `TestSlickViewHcaStatus`). |

Related, but not one plugin (each keeps its own row):

| Taut | Slick | Why not |
|---|---|---|
| Native At Channel (`NativeAtChannel`) | bChannel (`bChannel`) | Both send @channel and @here where Slack won't, through different relays: the at-channel app, or bChannel's `/bchannel` command and its own server, which also changes a private channel's posting permissions for its bot. |
| Faster Slack (`FasterSlack`) | Snappy (`Snappy`) | Faster Slack only pauses Slack's layout while the window resizes. Snappy does that and six more things (animations, duplicate stylesheets, selector rewrites, the GPU blocklist, the crash reporter, composer spellcheck). |
| Huddle Activity (`HuddleActivity`) | Huddle Mute Detector (`HuddleMuteDetector`) | Slick's is a port of Taut's old mute detector (taut 358eb74), which Taut has since grown into a log of who did what in a huddle: joins, mutes, settings changes and their toasts. |

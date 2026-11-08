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

**Roots:** `root := rt.NewRoot(func(tree []byte))`, `root.ID() uint64` (Go ids count from 1), `root.Render(js, element)` (JS thread), `root.Unmount()`. From JS: `__clack.createRoot()` → id with bit 52 set (`1<<52 | n`; JS numbers are float64), `__clack.render(id, el)`, `__clack.unmount(id)`; their commits go to `rt.OnCommit(func(id uint64, tree []byte))`. State/hook updates re-render asynchronously and deliver a new whole tree; at most one commit per root per JS task. Callbacks run on the JS thread; the byte slice is the receiver's.

**Events** (Go → JS): `rt.DispatchEvent(target int, ev []byte)` (async) / `rt.DispatchEventSync(ctx, target, ev) (EventResult{DefaultPrevented, PropagationStopped}, error)`. `target` is the `k` of the innermost hit node (0 = document, for window-level keys). JS runs DOM capture → target → bubble with React handlers and DOM listeners interleaved like a browser (`stopPropagation`, `preventDefault`, `currentTarget`). Event JSON: `{"type":"click","clientX":…,"clientY":…,"button":0,"buttons":1,"key":…,"code":…,"altKey":…,"ctrlKey":…,"shiftKey":…,"metaKey":…,"repeat":…,"deltaX":…,"deltaY":…,"target":{"value":…,"checked":…},"rects":{"<k>":[x,y,w,h],…}}`. Types are DOM names (`click`, `dblclick`, `contextmenu`, `mousedown/up/move`, `mouseenter/leave` (not bubbling: send one per node whose hover changed), `pointer*`, `keydown/up`, `input` (text edits; drives React `onChange`), `change` (checkbox/select), `focus/blur` (React `onFocus/onBlur` bubble), `wheel`, `scroll`, `submit`). Default actions run in JS when not prevented (`<a href>` → OpenURL, submit buttons → form `submit`).
**Layout back:** `rt.SetRects(map[int]jsrt.Rect{X,Y,W,H})` after layout (changed nodes only; feeds `getBoundingClientRect`, `offsetWidth`…, fires `ResizeObserver`), `rt.SetViewport(w, h, dpr float32, dark bool)` (innerWidth/innerHeight, `resize`, `matchMedia('(prefers-color-scheme: dark)')`). `rt.WantsEvent(type) bool` (cheap, any goroutine): JS has a window/document listener for that type, so forward global input (e.g. `mousemove` for Oneko, `keydown` for StreamerMode's shortcuts) to target 0 only then. Keys go there only when no widget took them: the shell asks for them last in the frame (Gio gives a key to the first matching `Event` call, and an empty `key.Filter` name matches every key, so asking first took the focused composer's Enter). `rt.SetHidden(bool)` (document.hidden / visibilitychange / window focus+blur). **Document:** `rt.OnDocument(func(body []byte, css []string))`: `document.body`'s children as a host tree (overlays) and every `<style>` text, after each change. `rt.OnDocumentProps(func(html, body []byte))`: the `p` (attributes + inline style, e.g. `className`, `--css-vars` in `style`) of `<html>` and `<body>`, which global CSS selectors depend on (StreamerMode's `documentElement` class, FasterSlack's CSS variables).

### `internal/taut` (owner: taut-api agent)
- `Manager`: loads/enables/disables plugins (user-imported `.js` files under `~/.config/clack/taut/plugins/`, plus "install from URL"), config per plugin (`config.json` semantics like Taut: only set options stored, `migrateConfig`, secrets in a separate store), restart-on-config-change, and the 5 s start/stop timeout.
- The `$taut` module: everything a plugin imports.
- The TautAPI: implemented 1:1 against Taut's surface. For the names the published plugins use, see the catalogue below.
- The settings model for **Preferences > Taut** (plugin list by category, toggles, every `opt.*` control kind, custom `editor`s rendered via React, the plugin's `settingsData` component, plugin authors, Hack Club–only filtering). ui-shell draws the tab with native widgets plus host-tree rendering for React editors.
- Implemented: the Go API UI owners call (wiring, `taut.Dispatch` and the thunk catalogue, composer/message hooks, patched views, settings) is in [PLUGINS-taut.md](PLUGINS-taut.md). The Redux mirror's slices and shapes are in [PLUGINS-state.md](PLUGINS-state.md). The `taut` workflow builds every Taut plugin with `build:user-plugin` and runs the acceptance suite against them.

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
- **Other API:** within, signal, storage.store, secrets, Cache, SharedStore, Store, sharedFrom, modal.openModal/confirm, menu, elements (Button, Tooltip, SvgIcon, IconButtonBase, …), setStyle, experiments.set, members.useMember/getCurrentMemberId, channels.getCachedChannel, messages.getRawMessage/injectMessages/patchMessageText/patchActionableMessage, onMessageSendBlocks, composer.addButton/addSendCheck/addLinkTransform/patchInlineMarkup, rtm.on, huddles.*, workspace.onChange, fetch, cookies, accounts, preferences.addTab, scheduleEnd, deferResizeWork, plus some DOM (document/querySelector/MutationObserver in StreamerMode, FasterSlack, Oneko, CustomNameRecording)

## Plugin Library (`internal/pluginlib`)
Preferences > **Plugin library** is one tab for both systems, after Slick's tabs (right after Taut's when Slick isn't running); Preferences > Taut's "Plugin library" row opens it. It lists every official Taut and Slick plugin in one search, grouped by category and tagged `Taut` or `Slick`, with its description, authors (or upstream contributors) and the source commits it was built from, filters (All, Taut, Slick, Installed, Updates), and installs or updates each in one click into its own system. Settings on an installed entry opens it in Preferences > Taut or Preferences > Slick > Plugins. (Until 2026-11-08 there were two Library screens, one under each system.)

- **Where the bundles come from.** Neither upstream publishes per-plugin builds (Taut ships them inside `taut.js`, Slick inside its app builds). `.github/workflows/plugin-library.yml` clones upstream `main`, builds every plugin with upstream's own build (Taut's `build:user-plugin`, Slick's build via `internal/slick/build/build.mjs`), runs Clack's Taut and Slick acceptance suites against exactly those bundles, and publishes them as the assets of the `plugins` **pre-release** of this repo (so the update banner's `releases/latest` never sees it). It runs weekly, on demand (`workflow_dispatch` with `taut_ref`/`slick_ref`), and when the workflow or `.github/scripts/plugin-index.mjs` changes on main, ui or live-taut. The build job has a read-only token and no caches; only the publish job can write releases, and it runs no third-party code.
- **GPLv3.** The bundles are third-party GPLv3 code and are never committed. The release carries `taut-LICENSE` and `slick-LICENSE`, and its notes and `index.json` give the upstream repo, commit and source path of every plugin (the corresponding source).
- **`index.json`** (schema 1): `sources.{taut,slick}` = `{name, repo, commit, license, licenseFile, build}`, and `plugins[]` = `{system, id, name, description, category, categoryLabel, authors[{name,url}], contributors[], hackClubOnly, defaultEnabled, upstreamDisabled, source, file, sha256, size, main?{file, sha256, size}}`. Asset names are `taut-<Name>.js`, `slick-<Dir>.js` and `slick-<Dir>.main.js`.
- **Client.** Nothing is fetched until the Library tab opens. `pluginlib` uses a plain https-only client (no Slack cookies, no credentials, https-only redirects), caps sizes, checks every download's sha256 against the index, and caches `index.json` under `<config>/cache/plugin-library/` for offline listing. Installing calls `Taut.Import` / `Slick.Import` (renderer, then `.main.js`), so a Library plugin is an ordinary user plugin in that system's `plugins/` dir. An entry shows **Update** when the installed file's sha256 (`Taut.InstalledSHA256`, `Slick.InstalledSHA256`) differs from the index. `CLACK_PLUGIN_LIBRARY` overrides the index URL (`off` turns the Library off; tests use it).

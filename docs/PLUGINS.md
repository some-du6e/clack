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
```go
type Runtime struct{ /* one JS thread (goroutine); all access serialized through it */ }
type Options struct {
	Fetch   func(ctx context.Context, req *http.Request) (*http.Response, error)
	Storage func(namespace string) KV // localStorage/sessionStorage backing
	Log     func(level, msg string)
}
func New(opts Options) (*Runtime, error)              // lazy: called only when the first plugin is enabled
func (r *Runtime) Module(name, src string)            // register an ES module ("$taut", "react", "react/jsx-runtime", …)
func (r *Runtime) Import(name string) (Value, error)  // import a module (user plugin bundles too)
func (r *Runtime) Expose(name string, fn HostFunc)    // Go functions callable from JS (globalThis.__clack.<name>)
func (r *Runtime) Do(f func(js *Ctx) error) error     // run on the JS thread (sync, with timeout)
func (r *Runtime) Post(f func(js *Ctx))               // async
func (r *Runtime) Close()
// React host renderer: Render(rootID, elementValue) → host tree; updates are pushed to a Go callback as a whole new tree (or patch ops)
```
Host tree (JS → Go), JSON:
`{"t":"div"|"span"|"button"|"img"|"svg"|"path"|…, "p":{"className":…, "style":{…}, "src":…, "on":{"click":cbId,…}, …}, "c":[…]}`, text nodes `{"t":"#","v":"text"}`, native Slack component `{"t":"@","n":"BaseAvatarStack","p":{…props}}`. Events go back as `__clack.dispatch(cbId, eventJSON)`.

### `internal/taut` (owner: taut-api agent)
- `Manager`: loads/enables/disables plugins (user-imported `.js` files under `~/.config/clack/taut/plugins/`, plus "install from URL"), config per plugin (`config.json` semantics like Taut: only set options stored, `migrateConfig`, secrets in a separate store), restart-on-config-change, and the 5 s start/stop timeout.
- The `$taut` module: everything a plugin imports.
- The TautAPI: implemented 1:1 against Taut's surface. For the names the published plugins use, see the catalogue below.
- The settings model for **Preferences > Taut** (plugin list by category, toggles, every `opt.*` control kind, custom `editor`s rendered via React, the plugin's `settingsData` component, plugin authors, Hack Club–only filtering). ui-shell draws the tab with native widgets plus host-tree rendering for React editors.

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
- **webpack exports (byName / waitForExport):** I18n, getPrefCompliantMemberName, getBroadcastKeywordsForUser, getInputProxy, isInputRegistered
- **Other API:** within, signal, storage.store, secrets, Cache, SharedStore, Store, sharedFrom, modal.openModal/confirm, menu, elements (Button, Tooltip, SvgIcon, IconButtonBase, …), setStyle, experiments.set, members.useMember/getCurrentMemberId, channels.getCachedChannel, messages.getRawMessage/injectMessages/patchMessageText/patchActionableMessage, onMessageSendBlocks, composer.addButton/addSendCheck/addLinkTransform/patchInlineMarkup, rtm.on, huddles.*, workspace.onChange, fetch, cookies, accounts, preferences.addTab, scheduleEnd, deferResizeWork, plus some DOM (document/querySelector/MutationObserver in StreamerMode, FasterSlack, Oneko, CustomNameRecording)

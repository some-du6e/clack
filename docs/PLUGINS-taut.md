# internal/taut: what UI owners call

`internal/taut` runs Taut plugins unmodified. It covers the `$taut` module, the plugin manager, the full TautAPI over `internal/core`, and the Preferences > Taut model. The TautAPI itself is JS (`internal/taut/js/*.js`, Clack's own implementation of Taut's contract) and runs on jsrt next to the plugins. Go owns files, config, secrets, lifecycle, the Redux mirror ([PLUGINS-state.md](PLUGINS-state.md)) and the APIs below. Without an enabled plugin, none of this starts a JS engine. Every check below is an atomic load.

## Wiring (ui root)

```go
t, _ := taut.New(taut.Options{App: app, UI: taut.UIHooks{
	Attached:    func(b *taut.Bridge) { slackc.SetBridge(b) },   // b also has RenderPreferencesTab / RenderEditor / RenderDataPanel
	Deliver:     func(root uint64, tree []byte) { slackc.Deliver(root, tree) },
	SetPatched:  slackc.SetPatched,
	CallGo:      slackc.CallGo,
	SetStyle:    slackc.SetStyle, RemoveStyle: slackc.RemoveStyle, // user CSS is key "user"
	Overlays:    func(o []taut.Overlay) { /* draw each o.Root (bit 52) as a modal layer */ },
	PreferencesTabs: …, Invalidate: w.Invalidate, Toast: …, OpenPreferences: …,
	ThreadPane:  …, Input: …, SetLocalDraft: …, SwitchAccount: …,
	OnDocument: slackc.SetDocument, OnDocumentProps: slackc.SetDocumentProps, // plugin DOM in body/head, <html>/<body> props
}, PickFiles: nativeFilePicker /* <input type=file>; nil = inert */})
defer t.Close()
```
`taut.New` reads `~/.config/clack/taut/` (config.json, user.css, plugins/*.js and their cached metadata). After `core.Boot` it starts the enabled plugins that run in this workspace. Hack Club–only plugins run only on the Hack Club grid (E09V59WQY1E / T0266FRGM).

If the JS engine traps, taut starts a fresh runtime on next use and restarts the plugins that were running.

Function ids: taut tags its own `{"$fn"}` / `{"$gofn"}` ids with bit 51 (`taut.IsTautFn`). `Bridge.Call` routes both kinds.

## Thunks: `taut.Dispatch`

Route Slack's named actions through `taut.Dispatch` where the UI would call core:

```go
_, err := taut.Dispatch(ctx, t, "currentUserStartedTyping", map[string]string{"channelId": ch, "threadTs": ts},
	func(ctx context.Context, a map[string]string) (any, error) { return nil, app.Typing(a["channelId"], a["threadTs"]) })
```
When no plugin patches the name, this is just `native(ctx, args)`. When a plugin patches it, the plugins' wrappers run in JS, and they can change the args, skip native, or change the result. Pass Slack's argument shape and call it off the UI goroutine. Thunks in `taut.SyncThunks` (menus, autocomplete) run their native on the JS thread, so keep those natives pure and fast. `t.RegisterThunk(name, fn)` sets the native that runs when a plugin dispatches the thunk itself. Defaults over core exist for the ones marked *taut*.

| thunk | args (Slack's) | who must call it | today |
|---|---|---|---|
| currentUserStartedTyping | `{channelId, threadTs?}` | ui/composer, typing send | native default: core.Typing |
| markLastRead | `{id: channelId, ts, threadTs?, reason?, immediate?}` | ui/messages, marking read | default: core.Mark / MarkThread |
| setUserPrefByApi, setUserPrefFetcher | `{pref, value}` | ui (prefs toggles) | default: users.prefs.set |
| initializeUserPrefs | `{prefsData}` | taut (prefs event) | identity |
| deleteDraftApi, clearDrafts | `{draft: {id,…}, reason?}` / `{ids, syncDelete?}` | ui/composer, ui/views drafts | default: core.DeleteDraft |
| syncDraftCreate, syncDraftUpdates | `{draftId, …}` | ui/composer | UI must dispatch |
| prepareAndSendMessage, sendMessage | `{channelId, replyToTs?, text, blocks, broadcast?}` / `{channel, blocks, thread_ts?, …}` | ui/composer send | UI must dispatch (after `TransformSendBlocks`) |
| commitMessageEdit (→ saveMessageEdit) | `{channelId, ts, blocks, text?}` | ui/messages edit | UI must dispatch commitMessageEdit; default native: saveMessageEdit → core.Edit with send transforms |
| chatDeleteFetcher | `{channel, ts}` | ui/messages delete | default: core.Delete |
| changeReactionFromUserAction, submitAddOrRemoveReaction | `{key: _rxn_key, name}` / `{reactionKey, name, add}` | ui/messages reactions, ui/popups emoji picker | UI must dispatch |
| getContextMenuTemplate | `{id: channelId}` → `[]MenuItem` | ui/popups channel menu | UI must dispatch (sync) |
| getMemberContextMenuTemplate | `{id: userId}` → `[]MenuItem` | ui/popups member menu | UI must dispatch (sync) |
| autocompleteChannels, autocompleteMembers | `{query, channelId?}` → results | ui/composer autocomplete | UI must dispatch (sync) |
| showNotification | `{message, channelId, title, …}` | ui (desktop notifications) | UI must dispatch |
| searchInline | `{query}` → `{items}` | ui/popups search dropdown | UI must dispatch |
| addAndUploadPendingFile, uploadFiles | `{file: taut.File}` / `{files: []taut.File, channelId}` | ui/composer attachments | UI must dispatch; addAndUploadPendingFile has a default (core.PrepareUpload → `{fileIds}`) for api.files.upload |
| maybePersistPendingFileData | `{pendingFileId}` | taut | native: `taut.PersistPendingFile` (ui/composer sets it); no-op unset |
| editFileApi | `{fileId, title}` | taut | native: `taut.EditFile` (ui/composer: its uploaded pending files); `files[fileId].title` reads the new title when it resolves; other ids fail |
| getChannelMembershipInfoForUsers | `{channelId, userIds}` | taut | default: conversations.members, fills `membership` |
| ensureMembersArePresent | `{memberIds}` | taut | default: core.EnsureUsers |
| fetchRawChannelsById | `{ids}` → `{missing}` | taut | default: core.ChannelInfo |
| fetchAndUpsertChannelsById | `{ids, reason?}` → `{channels, resolved, missing, failedIds}` | taut | default: dispatches fetchRawChannelsById (patchable), as Slack's |
| joinChannel | `{channelId}` | ui (join button) | default: core.Join |
| openPrefsModal | `{activeSection?}` | ui shell | default: UIHooks.OpenPreferences |
| enqueueHuddleToast | `{content, svgIconProps, duration}` | ui toasts | default: UIHooks.Toast |
| startEventLog, stopHuddle, stopEventLog | huddles | ui/huddleui | joining runs startEventLog around `huddle.Join`; leaving runs stopHuddle (native: the Chime LEAVE) then stopEventLog |
| maybeFetchBackgrounds, displayAlertMessage, processAwsTranscription, roomsStart/StopTranscriptionFetcher | huddles | ui/views huddles | no-op defaults (no huddle themes; captions come from Chime's transcription data messages) |
| logExperimentExposure, handleMessageImmediatelyWithoutPreprocessing | — | — | no-ops |
| apiCall, apiCallExpedited | `{method, args}` | taut | default: the Web API call |
| handleDeepLink | `{uri}` (slack:// links) | ui shell | default: `UIHooks.DeepLink(uri) bool` (false when unset) |
| currentUserEndedTyping, startHuddle | Slack's | ui/composer, ui/views | no-op defaults |
| openEditProfileModal, showSavedItemNotification | Slack's | ui/popups, ui notifications | UI must dispatch |
| sendActions, executeAppAction, submitView | Slack's (`{serviceId, appId, serviceTeamId, container, actions}`, `{appId, actionId, channelId, messageTs, applicableTeamId, clientToken}`, `{viewId, viewState: {values}}` → `{updatedView, responseAction, toastMessage}`) | ui/blockkit (block actions, shortcuts, modals) | dispatched by blockkit; natives registered (blocks.actions, apps.actions.v2.execute, views.submit). A plugin's own executeAppAction sends its clientToken as is (core `RunShortcutAs`), so the modal comes back to it in `view_opened` instead of opening |
| blocksFormatFetcher | `{blocks, reason}` | ui/composer markdown mode | native registered by blockkit (blocks.format) |

## Composer, messages and members

- `t.ComposerButtons()`: true when plugins added composer buttons. Render `slackc.Render(gtx, "TautComposerButtons", Props{"placement": "native"|"end", "context": taut.ComposerContext{…}}, nil)`. Each button is an `@ComposerButton` node with `{id, icon, label, pressed, onClick, tooltip}` (children: a custom icon). A thread composer's broadcast checkbox is `ComposerContext.Broadcast{Active, ChannelType, Set: t.Func(…)}`.
- `t.HasSendChecks()` / `t.SendCheck(ctx, composerCtx)`: run before sending. `false` keeps the text in the composer.
- `t.HasSendTransforms()` / `t.TransformSendBlocks(ctx, blocks)`: apply to the Block Kit of every send, edit and schedule.
- `t.HasLinkTransforms()` / `t.TransformLink(ctx, url, "paste"|"link-dialog")`: links pasted or added in the link dialog.
- `t.HasInlineMarkup()` / `ParseInlineMarkup` (offsets in runes), `SerializeInlineMarkup`, `EscapeInlineMarkup`: replace the composer's `*bold*` shortcuts (RealMarkdown). viewContext values are Slack's names: `MESSAGE_PANE`, `THREADS_FLEXPANE`, …
- Pending files (files.go): set `taut.PendingFiles` (list), `taut.RenamePendingFile` (the `updatePendingFile` action), `taut.PersistPendingFile` (maybePersistPendingFileData) and `taut.EditFile` (editFileApi) once, before the runtime starts, and call `t.PendingFilesChanged()` when attachments change. They serve `pendingFileUploads` and `files`. Unset, plugins see no pending files.
- Shaped natives: some Slack components have a JS shape beyond their name (04_react.js `SHAPES`). TextyInput is `memo(class TextyInput)` with Slack's `{instance: {quill}}` on its ref. Its owner feeds the Quill with `__taut.feedNative('TextyInput', props => ({state: {length, index, selected}, props: rest}))` from a prelude (ui/composer does).
- `t.SetComposerDraft(taut.ComposerDraft{DraftID, Channel, ThreadTs, Broadcast, Ops, FileIDs, Cursor})` while you type (Slack's fastLaneDraftUpdate: 150 ms debounce, leading and trailing) and `t.ClearComposerDraft(draftID)` on send: the composer's live draft in `drafts.unifiedDrafts[draftId]` (HaikuWarning's banner reads its ops). No-ops until the runtime runs.
- `t.RegisterInput(where, true/false)`: register composers with Slack's `{viewContext, channelId}`. Answer `UIHooks.Input` ops `isEmpty`, `getContents`, `setContents`, `focus` and `insertText` (`getInputProxy`).
- `t.SetEditing(windowID, ch, ts, open)`: call when an editor opens or closes. `patchMessageText` leaves the message being edited unchanged.
- `t.SlicePatched("messages")` → `t.MessageView(ch, ts)` (core.Message, ok) and `t.InjectedMessages(ch)`. `t.SlicePatched("members")` → `t.MemberView(id)`. `t.SlicePatched("channels")` → `t.ChannelView(id)`. `t.ChannelRestriction(ch)` → read-only / thread-only. These never block: they return ok=false until the patched value is computed, then `Invalidate` fires.
- `t.ActionablePatched()` / `t.ActionableMessage(ctx, ch, ts)`: edit/delete checks, the up arrow and the editor see this message.
- Components rendered inside a thread pane pass `$pane: {channelId, threadTs, windowId}` in their props (`api.threads.usePane`), and the pane answers `UIHooks.ThreadPane` ops `loaded`, `topReply` and `jumpToReply`.

## Settings (Preferences > Taut)

`t.Settings()` returns the categories (Messages, Message Box, People, Privacy, App, Fun, Other) with each plugin's name, mrkdwn description, authors, enabled/running state, error, option problems, options and data flags. It also carries the custom CSS box, a config.json error and plugin tabs. Option kinds map to controls: boolean → toggle, number → number field (Min/Max), string → text field, select → dropdown, list → rows, color → picker, json → code box, secret → password field (`t.Secret` / `t.SetSecret`). `HasEditor` means a React editor, rendered with `Bridge.RenderEditor`. `HasDataPanel` means the plugin's panel, rendered with `Bridge.RenderDataPanel` while it runs, and remounted when `RunID` changes. Actions: `SetEnabled`, `SetOption`, `ResetOption`, `SetSecret`, `Import(code, replacingID)`, `InstallURL`, `Remove`, `ClearData(id, "storage"|"cache")`, `SetUserCSS`, `ReloadConfig`. `t.Subscribe()` signals changes.

Files: `config.json` (Taut's format: only set options), `user.css`, `plugins/<id>.js` plus `.meta.json`, `storage/` (api.storage, api.Cache, localStorage), and the separate 0600 secret store (`secrets.json` for opt.secret values and accounts, `secrets/` for api.secrets).

## Extensions (other plugin systems on this runtime)

`t.Extend(taut.Extension{Name, Install, Prelude, Ready, LocalStorage, LocalStorageKeys})` adds a plugin system to Taut's runtime: `Install` registers host functions and `Prelude` runs after the TautAPI in every fresh runtime (the internals are on `globalThis.__taut`, the base API on `globalThis.TautAPI`), `Ready(rt, restarted)` runs once the runtime is attached (restart what ran after a crash), and `LocalStorage` serves read-only virtual keys that never reach disk. `t.JS()` starts the runtime on demand; registering an extension doesn't. `t.DataList/DataRead/DataWrite/DataDelete/DataClear` give extensions namespaces in Taut's data store. internal/slick is the user ([PLUGINS-slick.md](PLUGINS-slick.md)). `ComposerContext.Blocks`/`Text` carry the outgoing message to send checks.

## api.huddles (native)
`internal/ui/huddleui` feeds the TautAPI's huddles from the native session (`internal/taut/huddle.go`):
- `SetActiveHuddle(callID, channelID, selfAttendee, selfUser)` → `onActiveHuddle` / `getActiveHuddle` and the `getActiveHuddleCallId` / `getActiveHuddleChannelId` selectors.
- `HuddlePeerMessage(huddle.PeerEvent)` → `onPeerMessage` listeners get `{message, kind, senderAttendeeId, selfAttendeeId}`, with protobuf-es field names (`reacji`, `muteRequest`, `fromPeerIdString`…) for the `data-channel`, `awareness-sync-channel` and `transcript-catchup` topics.
- `SetHuddlePeers(attendee → user)` → the `peers` slice (`byId`, `leftById`), so `getAttendeeUserId` works.
- `SetHuddleReaction(f)` → `sendReaction` / `sendReactionMessage` send a reacji through Chime.
- `ENABLE_EVENT_LOG` / `DISABLE_EVENT_LOG` set `huddleEvents.enabled`.

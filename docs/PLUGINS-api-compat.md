# Taut and Slick plugin API compatibility

What Taut and Slick hand their plugins, member by member, and how Clack's
implementation compares (2026-11-20, branch plugin-api-compat). The sources:

- **Taut** at `TAUT_PIN` 6fe5123 (the workflows' pin; upstream `main` is now
  efe682d). Its API is `createScopedAPI`'s return type in
  `app/pluginManager.ts` (the scoped `TautAPI` over `makeBaseTautAPI`). Its
  plugins are the 40 files in `plugins/`.
- **Slick** at `main` f9f8f92 (v159), what ui-slick builds. Its API is
  `createScopedAPI` in `src/app/pluginManager.ts` (the `SlickAPI`). Its
  plugins are the 59 directories of `src/plugins/` (262 files, tests left
  out).
- **Slack's web client** build 35b42de8b870 (2026-11-19) for what the thunks
  do underneath.

How the table was made: the TypeScript checker reads each framework's API
type (members, call signatures, whether a function returns a Promise) and
counts every use of each member in the framework's own plugins, matched by
declaration, so aliases (`const api = this.api`) and destructuring count.
Clack's side is the same walk over Clack's own JS (`internal/taut/js`,
`internal/slick/js`), plus reading the code where the checker can't follow
it. The tools are in the agent scratch (`tools/surface.cjs`,
`tools/apidoc.py`), not in this repo.

Status:

- **present**: Clack has the member with the same name, kind and
  sync/async shape. Its behaviour was compared with upstream's source only
  where the notes say so. A "present" member can still differ in ways no
  test has caught.
- **differs**: Clack has the member but it behaves differently, as the
  note says.
- **missing**: Clack has no such member.

## Summary

| surface | members | used by plugins | present (used) | differs (used) | missing |
|---|---:|---:|---:|---:|---:|
| TautAPI | 198 | 142 | 133 | 9 | 0 |
| SlickAPI | 119 | 90 | 80 | 10 | 0 (1, `redux.thunkNames`, fixed here) |
| Slack thunks the plugins name | 46 | 46 | 38 (29 Clack defines, 9 its UI dispatches) | 1 | 7 (9 before this branch) |

By name nothing in either API object is missing now. What makes plugins
feel half-supported is behaviour: how calls fail (apiCall and userAPI, fixed
here), the Slack thunks Clack doesn't define (7 that plugins dispatch, 9 before this branch), and
the components Clack draws through the bridge (Taut's in
[PLUGINS-components.md](PLUGINS-components.md); Slick's 72 component names
aren't catalogued yet).

## apiCall: how Slack's request settles

Module ids are build 35b42de8b870's; line numbers are in the beautified
modules.

- **apiCall** is client-boot `0hZc` `M` (line 112, `meta.name` apiCall):
  `{method, args, defaultArgs = getDefaultArgsByMethod(method) (line 14),
  SET_ACTIVE, maxAttempts, abortSignal, responseParser, …}`. Methods in its
  no-retry list (line 52, `shouldRetryMethod`) get maxAttempts 1. The
  request is a SlackApiRequest enqueued on the team's API queue (`XG3c` →
  `BcxX` → `7quX` `ApiQ.q`, line 444), whose deferred the thunk returns.
  **apiCallExpedited** (`0hZc` `N`, line 213) dispatches apiCall's creator
  with `EXPEDITED: true`, so apiCall's patches see it too.
- **The response** (`QCxN` `resolve`, line 117): `JSON.parse` the body, or
  `{ok: false, error: "_parse_error", parse_error_message, payload_size}`
  when it throws. It resolves with `{request_id: X-Slack-Req-Id,
  client_request_id: id, args, method, ...answer}`.
- **`{"ok":false}`** (`QCxN` `_onNotOk`, line 151): `new SlackError(`<id>
  <method> not ok: <code>`, {name: code})` (`9BpC` line 3: class fields
  `args, data, apiErrorCode, id, method, subType, type, attempts,
  doNotFetchAuth`), then `apiErrorCode`, `data` (the parsed answer, whole),
  `id`, `method`, `status` 200, `args` (token `"[redacted]"`) and
  `attempts`. The id is `generateId` (`EVbb` line 4) over
  `bootData.version_uid`: `<first 8>-<Date.now()/1000 to 3 places>`.
  internal_error goes through `throwError({sourceError})` (status 0, tried
  again). A rate-limit code (`Umoo` `FJ`, line 285) goes through
  `throwError({sourceError, status: "RATELIMITED"})`, and the queue turns it
  into its dummy request and rejects at once (`7quX` line 276). Any other
  code rejects with the SlackError itself.
- **Everything else** goes through BaseRequest's `throwError` (`Umoo` line
  173), with SlackApiRequest's `throwError` (`QCxN` line 139) supplying the
  source error `Object.assign(Error(message), {method})`. The result is
  `Object.assign(Error(message || source.message || "Request did not
  complete."), source, {status, response: xhr.response, retryAfter,
  attempts})`, with no `data`. A non-200 status is "Request completed but is
  not OK" (`_onLoad`, line 135), a timeout "Request timed out after 60
  seconds." with status `TIMEOUT` (line 150), and no answer the default
  message with status 0 (`_onError`, line 162).
- **Retries**: `shouldRetry` (`Umoo` line 212; `QCxN` line 148) retries a
  429, 503 or 504, no answer, a timeout, or internal_error, while attempts <
  maxAttempts. The wait is `_getRetryAfterMs` (line 100): Retry-After in
  seconds, else `_getRetryAfterWithBackoff` (line 107), which is 60 s after a
  429 or 503 or for RATELIMITED, 15 s after a 504, 30 s after another 5xx
  and 1 s otherwise, plus `getBackoffJittered` (`ZI29` line 8). It is capped
  at 180 s. The queue rejects the caller with the request's last error
  (`7quX` line 329).

What Clack does now: Go sends the request and tries it as the queue does
(`slack.SlackRetriesUpTo`, internal/slack/attempts.go:121). `ObserveAttempts`
(attempts.go:39) reports each try's status, headers and body, and
`AttemptOf` (attempts.go:88) covers a repeat that the backoff guard answers.
The native hands the last try back whole (internal/taut/thunks.go:613), and
`apiCallSettle` (internal/taut/js/05_slack.js:707) builds Slack's response or
rejection from it. `apiCallThunk` (05_slack.js:753) builds the request the
way 0hZc does: `getFormData` (`L9gf`) fields (05_slack.js:651), the default
args, set_active, maxAttempts and the abort signal.

Still different from Slack's: Clack sends its own token whatever `args`
says, and none of Slack's request metadata (`_x_reason`, `_x_mode`,
`queryParams`, a per-call `timeout`). `args` in the response and in errors
shows the token as `"[redacted]"`, so a plugin logging a response can't
write the session token into Clack's log. Clack sends at most one file
part. Identical requests aren't merged the way the queue's
`_matchingRecord` (`7quX` line 129) merges them, and after a rate-limit code
the dummy request doesn't answer identical calls for its retry window
(`apiq_dummy_requests` is on for this account). A `_parse_error`'s
`parse_error_message` is QuickJS's JSON.parse text, not V8's.

## Fixed on this branch

| what | where | test |
|---|---|---|
| apiCall and apiCallExpedited settle as Slack's request does: `err.data`, Slack's messages, its retries and their errors, its response shape | internal/taut/js/05_slack.js:581-789, internal/taut/js/03_redux.js:599, internal/taut/thunks.go:613, internal/slack/attempts.go, internal/slack/client.go:744-790, internal/slack/apiq.go | TestAPICallNotOkLikeSlack, TestAPICallResolvesLikeSlack, TestAPICallRateLimitedLikeSlack, TestAPICallRetriesLikeSlack, TestSlickAPICallErrorsLikeSlack |
| userAPI fails as Taut's and Slick's own do: the answer's JSON whole in "failed:", `[slick]` for Slick, no Clack-side 429 retries, `HTTP <status>:`, `res.json()`'s SyntaxError, fetch's `Failed to fetch`, the abort | internal/taut/js/05_slack.js:514-563, internal/taut/host.go:278, internal/slick/js/20_api.js:528 | TestUserAPIErrorsLikeTaut, TestSlickAPICallErrorsLikeSlack |
| Slick's `redux.thunkNames` | internal/taut/js/03_redux.js:646, internal/slick/js/20_api.js:484 | TestSlickReduxThunkNames |
| `addMemberToPresenceSubscription` / `removeMemberFromPresenceSubscription` (LastSeen, OnlineCount): Slack's presence manager (client-boot 4hlf): a count per member; its first subscriber and its last leaving each send `presence_sub`, 100 ms debounced, with every counted member in Clack's own set | internal/core/presence_subs.go, internal/core/app.go presenceIDs, internal/taut/thunks.go | TestPresenceSubscriptionLikeSlack (over the booted socket), TestSlickPresenceThunks |

All of them run over internal/fakeslack, which can now answer a hook with any
status, headers and body (`fakeslack.Reply`, internal/fakeslack/fakeslack.go:1398),
in flake-check's `plugin-api` job.

## The tables

### TautAPI (Taut at TAUT_PIN 6fe5123)

| uses (plugins) | member | kind | signature | Clack | notes |
|---:|---|---|---|---|---|
| 110 (21) | `redux` | object | `` | present |  |
| 65 (24) | `patchComponent` | fn | `<P = object>(matcher: string \| { displayName?: string \| undefined; filter?: filter \| un…` | differs | patched renders reach only the components Clack draws through the bridge (PLUGINS-components.md) |
| 64 (20) | `signal` | value | `AbortSignal` | present |  |
| 32 (16) | `elements` | object | `` | present |  |
| 28 (11) | `redux.patchThunk` | fn | `(match: string \| ((value: any) => boolean), wrap: (original: (...args: any[]) => any) =…` | differs | only thunks Clack dispatches go through patches; see the thunk table |
| 25 (11) | `Store#.get` | fn | `(): T` | present |  |
| 23 (20) | `setStyle` | fn | `(css: string \| null, key?: string \| undefined): () => void` | differs | Clack applies the CSS its renderer supports (PLUGINS.md) |
| 21 (8) | `elements.Button` | value | `React.ComponentType<P>` | present |  |
| 18 (11) | `elements.SvgIcon` | value | `React.ComponentType<P>` | present |  |
| 18 (12) | `members` | object | `` | present |  |
| 18 (9) | `redux.getRawState` | fn | `(): any` | present |  |
| 18 (12) | `redux.useReduxState` | fn | `<T>(selector: (state: any) => T): T \| undefined` | present |  |
| 17 (8) | `Store#.use` | fn | `(): T` | present |  |
| 16 (5) | `messages` | object | `` | present |  |
| 16 (7) | `modal` | object | `` | present |  |
| 16 (8) | `within` | async fn | `<T>(work: Promise<T> \| ((signal: AbortSignal) => Promise<T>), ms: number, what?: string…` | present |  |
| 14 (9) | `storage` | object | `` | present |  |
| 13 (9) | `storage.store` | fn | `<T extends JsonValue>(key: string, defaultValue: T): StoredStore<Widen<T>>` | present |  |
| 11 (7) | `elements.Tooltip` | value | `React.ComponentType<P>` | present |  |
| 11 (5) | `waitForExport` | async fn | `<T>(matcher: ExportMatcher<T>): Promise<T>` | differs | Clack's module shims |
| 10 (6) | `redux.refresh` | fn | `(): void` | present |  |
| 9 (8) | `Cache` | class | `<T>(cacheKey: string, options?: CacheOptions \| undefined): BoundCache<T>` | present |  |
| 9 (8) | `Cache#.load` | async fn | `(): Promise<void>` | present |  |
| 9 (6) | `Store` | class | `<T>(initial: T): Store<T>` | present |  |
| 9 (6) | `redux.dispatchThunk` | async fn | `<T = any>(name: string, ...args: any[]): Promise<T>` | differs | thunks Clack has no native for reject at once ("Clack has no native X"); see the thunk table |
| 8 (4) | `Store#.set` | fn | `(next: T): void` | present |  |
| 7 (5) | `Cache#.get` | fn | `(key: string): T \| undefined` | present |  |
| 7 (6) | `composer` | object | `` | present |  |
| 7 (1) | `huddles` | object | `` | present |  |
| 7 (5) | `members.getCurrentMemberId` | fn | `(): string \| undefined` | present |  |
| 7 (6) | `members.useMember` | fn | `(userId: string): SlackMember \| undefined` | present |  |
| 7 (1) | `profile` | object | `` | present |  |
| 7 (5) | `redux.getStore` | fn | `(): SlackStore \| null` | present |  |
| 6 (5) | `Cache#.set` | fn | `(key: string, value: T): void` | present |  |
| 6 (1) | `accounts` | object | `` | present |  |
| 6 (3) | `byName` | fn | `(name: string): SimpleMatcher` | present |  |
| 6 (5) | `elements.MrkdwnElement` | value | `React.ComponentType<P>` | present |  |
| 6 (4) | `messages.getRawMessage` | fn | `(channel: string, ts: string): SlackMessage \| undefined` | present |  |
| 6 (5) | `rtm` | object | `` | present |  |
| 6 (5) | `rtm.on` | fn | `(type: string, listener: RtmListener): () => void` | present |  |
| 5 (5) | `Store#.subscribe` | fn | `(onChange: () => void): () => void` | present |  |
| 5 (3) | `byMeta` | fn | `(name: string): SimpleMatcher` | present |  |
| 5 (3) | `experiments` | object | `` | present |  |
| 5 (4) | `redux.patchSlice` | fn | `<T = any>(sliceName: string, mapEntry: MapEntry<T>, options?: SliceOptions \| (() => Ite…` | present |  |
| 5 (3) | `redux.waitForThunkCreator` | async fn | `(name: string): Promise<ThunkCreator>` | differs | waits forever for a name Clack never defines, as Taut does; Clack defines no thunk later than its natives |
| 5 (5) | `userAPI` | async fn | `<T = any>(method: string, params?: Record<string, string \| Blob>, options?: UserAPIOpti…` | present | fixed here: Taut's loop and messages, the answer's JSON whole (05_slack.js makeUserAPI) |
| 4 (4) | `Cache#.fetch` | async fn | `(key: string, fetcher: () => Promise<T>): Promise<T>` | present |  |
| 4 (3) | `blocks` | object | `` | present |  |
| 4 (1) | `channels` | object | `` | present |  |
| 4 (2) | `elements.FilterPill` | value | `React.ComponentType<P>` | present |  |
| 4 (3) | `elements.FormTextInput` | value | `React.ComponentType<P>` | present |  |
| 4 (3) | `experiments.set` | fn | `(name: string, group: ExperimentGroup): ForcedExperiments` | present |  |
| 4 (1) | `huddles.getAttendeeUserId` | fn | `(attendeeId: string): string \| undefined` | present |  |
| 4 (3) | `modal.Label` | value | `React.ComponentType<P>` | present |  |
| 4 (3) | `modal.alert` | async fn | `(options: AlertOptions): Promise<void>` | present |  |
| 4 (3) | `modal.confirm` | async fn | `(options: ConfirmOptions): Promise<boolean>` | present |  |
| 4 (3) | `modal.openModal` | fn | `(options: OpenModalOptions): ModalHandle \| null` | present |  |
| 4 (1) | `profile.getStatus` | fn | `(): SlackStatus \| undefined` | present |  |
| 3 (1) | `channels.getCachedChannel` | fn | `(channelId: string): SlackChannel \| undefined` | present |  |
| 3 (3) | `composer.addButton` | fn | `<K extends ComposerKind = MessageBoxKind>(added: ComposerButton<K>): () => void` | present |  |
| 3 (2) | `elements.InlineAlert` | value | `React.ComponentType<P>` | present |  |
| 3 (3) | `menu` | object | `` | present |  |
| 3 (3) | `modal.TextInput` | fn | `({ autoFocus, inputRef, ...props }: FormTextInputProps): any` | present |  |
| 3 (1) | `profile.setSnooze` | async fn | `(until: number \| null): Promise<void>` | present |  |
| 3 (3) | `redux.StateView` | fn | `({ patch, children, }: { patch: StatePatch; children?: any; }): any` | present |  |
| 3 (3) | `redux.mapEntries` | fn | `<T = any>(object: object, mapEntry: MapEntry<T>, addedKeys?: (() => Iterable<string>) \|…` | present |  |
| 3 (2) | `redux.usePatchVersion` | fn | `(): number` | present |  |
| 2 (1) | `RateLimit#.take` | async fn | `(): Promise<number>` | present |  |
| 2 (1) | `SharedStore#.update` | fn | `(updater: (value: T) => T): void` | present |  |
| 2 (2) | `Store#.update` | fn | `(updater: (value: T) => T): void` | present |  |
| 2 (2) | `blocks.fromDraft` | async fn | `(ops: unknown[], options?: FromDraftOptions \| undefined): Promise<Block[]>` | present |  |
| 2 (2) | `composer.addSendCheck` | fn | `(check: SendCheck): () => void` | present |  |
| 2 (2) | `elements.Avatar` | value | `React.ComponentType<P>` | present |  |
| 2 (1) | `elements.IconButtonBase` | value | `React.ComponentType<P>` | present |  |
| 2 (2) | `lazyComponent` | fn | `<P extends {}>(name: string, filter?: filter \| undefined): React.ComponentType<P>` | differs | Clack's bridge components only |
| 2 (1) | `members.getMember` | async fn | `(userId: string): Promise<SlackMember \| undefined>` | present |  |
| 2 (2) | `menu.Menu` | fn | `({ template, position, children }: MenuProps): any` | present |  |
| 2 (1) | `messages.asRawMessage` | fn | `(msg: SlackMessage \| undefined): SlackMessage \| undefined` | present |  |
| 2 (2) | `messages.getMessageMetadata` | fn | `(channel: string, ts: string, signal?: AbortSignal \| undefined): SlackMessageMetadata \|…` | present |  |
| 2 (2) | `messages.modifyMessageObject` | fn | `(message: SlackMessage, edits: { sentBy?: string \| undefined; typed?: boolean \| undefin…` | present |  |
| 2 (1) | `profile.getPresence` | fn | `(): "auto" \| "away" \| undefined` | present |  |
| 2 (1) | `profile.getSnooze` | fn | `(): number \| null` | present |  |
| 2 (1) | `profile.setPresence` | async fn | `(presence: "auto" \| "away"): Promise<void>` | present |  |
| 2 (1) | `profile.setStatus` | async fn | `(status: Partial<SlackStatus> & { oooMessage?: Block[] \| undefined; }): Promise<void>` | present |  |
| 2 (2) | `redux.waitForState` | async fn | `<T>(read: () => T \| undefined, signal?: AbortSignal \| undefined): Promise<T>` | present |  |
| 2 (2) | `storage.delete` | async fn | `(key: string): Promise<boolean>` | present |  |
| 2 (1) | `workspace` | object | `` | present |  |
| 1 (1) | `RateLimit` | class | `(key: string, options: RateLimitOptions): RateLimit` | present |  |
| 1 (1) | `SharedStore` | class | `<T>(key: string, initial: T): BoundSharedStore<T>` | present |  |
| 1 (1) | `accounts.addAccount` | async fn | `(): Promise<void>` | present |  |
| 1 (1) | `accounts.captureCurrent` | async fn | `(): Promise<StoredAccount \| null>` | present |  |
| 1 (1) | `accounts.forget` | async fn | `(userId: string): Promise<void>` | present |  |
| 1 (1) | `accounts.list` | async fn | `(): Promise<StoredAccount[]>` | present |  |
| 1 (1) | `accounts.supported` | value | `boolean` | present |  |
| 1 (1) | `accounts.switchTo` | async fn | `(userId: string): Promise<void>` | present |  |
| 1 (1) | `blocks.mapRichTextElements` | fn | `(blocks: Block[], map: (element: RichTextElement, context: RichTextElementContext) => R…` | present |  |
| 1 (1) | `blocks.mapRichTextSections` | fn | `(blocks: Block[], map: (elements: RichTextElement[], context: RichTextSectionContext) =…` | present |  |
| 1 (1) | `channels.makeChannelObject` | fn | `(channel: { id: string; name: string; isPrivate?: boolean \| undefined; isMember?: boole…` | present |  |
| 1 (1) | `composer.addLinkTransform` | fn | `(transform: LinkTransform): () => void` | present |  |
| 1 (1) | `composer.patchInlineMarkup` | fn | `(markup: InlineMarkup): () => void` | present |  |
| 1 (1) | `deferResizeWork` | fn | `(options: { quietMs: number; onHoldChange?: ((holding: boolean) => void) \| undefined; }…` | present |  |
| 1 (1) | `elements.BasicSelect` | value | `React.ComponentType<P>` | present |  |
| 1 (1) | `elements.DatePickerTrigger` | value | `React.ComponentType<P>` | present |  |
| 1 (1) | `elements.EmojiMenuTrigger` | value | `React.ComponentType<P>` | present |  |
| 1 (1) | `elements.Label` | value | `React.ComponentType<P>` | present |  |
| 1 (1) | `elements.Popover` | value | `React.ComponentType<P>` | present |  |
| 1 (1) | `elements.ProfileHoverTrigger` | value | `React.ComponentType<P>` | present |  |
| 1 (1) | `elements.RichTextInput` | fn | `({ value, onChange, onBlur, placeholder, ariaLabel, id, maxLength, numMinLines, disable…` | present |  |
| 1 (1) | `elements.TimePicker` | value | `React.ComponentType<P>` | present |  |
| 1 (1) | `experiments.delete` | fn | `(name: string): boolean` | present |  |
| 1 (1) | `experiments.keys` | fn | `(): IterableIterator<string>` | present |  |
| 1 (1) | `experiments.priority` | value | `number` | present |  |
| 1 (1) | `fetch` | async fn | `(input: RequestInfo \| URL, init?: RequestInit \| undefined): Promise<Response>` | present | no CORS, through Go (bridgeFetch) |
| 1 (1) | `files` | object | `` | present |  |
| 1 (1) | `files.upload` | async fn | `(file: File): Promise<string>` | present |  |
| 1 (1) | `findModuleId` | fn | `(value: any): string \| undefined` | differs | Clack's module shims |
| 1 (1) | `getModuleSources` | fn | `(): Generator<[string, string], any, any>` | differs | Clack's module shims, not Slack's webpack sources |
| 1 (1) | `huddles.getCall` | fn | `(roomId: string): HuddleCall \| undefined` | present |  |
| 1 (1) | `huddles.onActiveHuddle` | fn | `(listener: (huddle: ActiveHuddle \| undefined) => void): () => void` | present |  |
| 1 (1) | `huddles.onPeerMessage` | fn | `(listener: PeerMessageListener): () => void` | present |  |
| 1 (1) | `members.getCachedMember` | fn | `(userId: string): SlackMember \| undefined` | present |  |
| 1 (1) | `members.modifyMemberObject` | fn | `(member: SlackMember, edits: { name?: string \| undefined; displayName?: string \| undefi…` | present |  |
| 1 (1) | `menu.ContextMenu` | fn | `({ template, children }: ContextMenuProps): any` | present |  |
| 1 (1) | `messages.fetchMessageMetadata` | async fn | `(channel: string, ts: string, signal?: AbortSignal \| undefined): Promise<SlackMessageMe…` | present |  |
| 1 (1) | `messages.getMessageBotId` | fn | `(msg: SlackMessage \| undefined): string \| undefined` | present |  |
| 1 (1) | `messages.patchActionableMessage` | fn | `(transform: ActionableTransform): () => void` | present |  |
| 1 (1) | `messages.patchMessageText` | fn | `(transform: TextTransform): () => void` | present |  |
| 1 (1) | `messages.useActivityMessage` | fn | `(item: SlackActivityItem \| undefined): SlackMessage \| undefined` | present |  |
| 1 (1) | `messages.useMessageBot` | fn | `(msg: SlackMessage \| undefined): SlackBot \| undefined` | present |  |
| 1 (1) | `onMessageSendBlocks` | fn | `(transform: MessageSendTransform): () => void` | present |  |
| 1 (1) | `preferences` | object | `` | present |  |
| 1 (1) | `preferences.addTab` | fn | `(tab: PreferencesTab): () => void` | present |  |
| 1 (1) | `profile.getTimeZone` | fn | `(): string \| undefined` | present |  |
| 1 (1) | `redux.patchState` | fn | `(patch: StatePatch): () => void` | present |  |
| 1 (1) | `redux.waitForStore` | async fn | `(): Promise<SlackStore>` | present |  |
| 1 (1) | `scheduleEnd` | fn | `(windows: readonly ScheduleWindow[], timeZone?: string \| undefined): number \| null` | present |  |
| 1 (1) | `secrets` | object | `` | present |  |
| 1 (1) | `sharedFrom` | fn | `<T>(pluginId: string, key: string): { subscribe: (onChange: () => void) => () => void; …` | present |  |
| 1 (1) | `threads` | object | `` | present |  |
| 1 (1) | `threads.usePane` | fn | `(): ThreadPane \| null` | present |  |
| 1 (1) | `workspace.isHackClub` | fn | `(): boolean` | present |  |
| 1 (1) | `workspace.onChange` | fn | `(listener: () => void): () => void` | present |  |

Unused by the 40 plugins (56 members), all present unless marked: `Cache#.clear`, `RateLimit#.key`, `RateLimit#.options`, `RateLimit#.options.max`, `RateLimit#.options.window`, `RateLimit#.wait`, `SharedStore#.dispose`, `SharedStore#.get`, `SharedStore#.set`, `SharedStore#.use`, `accounts.validate`, `blocks.toMarkdown`, `blocks.toPlainText`, `commonModules`, `commonModules.react`, `cookies` (differs), `elements.Blocks`, `elements.Checkbox`, `elements.ConfirmationModal`, `elements.ContextMenuTrigger`, `elements.DateRangePicker`, `elements.FieldSet`, `elements.Hint`, `elements.Legend`, `elements.MenuFromTemplate`, `elements.MenuTrigger`, `elements.Tag`, `experiments.clear`, `experiments.dispose`, `experiments.entries`, `experiments.get`, `experiments.has`, `experiments.size`, `getByProps` (differs), `getComponent` (differs), `getExport` (differs), `getRenderedComponent` (differs), `huddles.getActiveHuddle`, `huddles.sendReaction`, `messages.injectMessages`, `redux.getThunkCreator`, `secrets.clear`, `secrets.delete`, `secrets.get`, `secrets.keys`, `secrets.set`, `secrets.store`, `secrets.update`, `storage.clear`, `storage.get`, `storage.keys`, `storage.set`, `storage.update`, `waitForComponent` (differs), `waitForRenderedComponent` (differs), `workspace.useIsHackClub`.

### SlickAPI (Slick main f9f8f92)

| uses (plugins) | member | kind | signature | Clack | notes |
|---:|---|---|---|---|---|
| 174 (29) | `redux` | object | `` | present |  |
| 110 (41) | `patchComponent` | fn | `<P = object>(matcher: PatchMatcher<P>, replacement: ComponentReplacer<P>): () => void` | differs | patched renders reach only the components Clack draws through the bridge |
| 94 (31) | `signal` | value | `AbortSignal` | present |  |
| 64 (13) | `Store#.set` | fn | `(next: T): void` | present |  |
| 43 (34) | `setStyle` | fn | `(css: string \| null, key?: unknown): () => void` | differs | Clack applies the CSS its renderer supports |
| 40 (13) | `Store#.use` | fn | `(): T` | present |  |
| 35 (15) | `Store` | class | `<T>(initial: T): Store<T>` | present |  |
| 32 (13) | `main` | object | `` | present |  |
| 30 (13) | `members` | object | `` | present |  |
| 29 (25) | `elements` | object | `` | present |  |
| 29 (9) | `redux.usePatchVersion` | fn | `(): number` | present |  |
| 27 (13) | `main.call` | async fn | `<T = unknown>(method: string, ...args: unknown[]): Promise<T>` | present |  |
| 27 (10) | `redux.getRawState` | fn | `(): any` | present |  |
| 27 (9) | `storage` | object | `` | present |  |
| 24 (7) | `isActive` | fn | `(): boolean` | differs | false once stopped; Clack has one workspace per window, so no workspace check |
| 24 (13) | `redux.refresh` | fn | `(): void` | present |  |
| 24 (14) | `redux.useReduxState` | fn | `<T>(selector: (state: any) => T): T \| undefined` | present |  |
| 23 (10) | `redux.dispatchThunk` | async fn | `<T = any>(name: string, ...args: any[]): Promise<T>` | differs | a name Clack doesn't define rejects at once ("[slick] Clack has no Slack thunk X") instead of after Slick's wait; see the thunk table |
| 23 (8) | `userAPI` | async fn | `<T = any>(method: string, params?: Record<string, string \| Blob>, options?: UserAPIOpti…` | present | fixed here: Slick's loop, [slick] messages, the answer's JSON whole (05_slack.js makeUserAPI) |
| 21 (11) | `loader` | value | `"electron" \| "extension" \| "userscript"` | present | "electron" |
| 14 (6) | `members.getCurrentMemberId` | fn | `(): string \| undefined` | present |  |
| 13 (10) | `redux.patchThunk` | fn | `(match: string \| Matcher, wrap: Wrapper): () => void` | differs | only thunks Clack dispatches go through patches; see the thunk table |
| 13 (9) | `settings` | object | `` | present |  |
| 11 (10) | `elements.SvgIcon` | value | `React.ComponentType<P>` | present |  |
| 11 (4) | `messages` | object | `` | present |  |
| 11 (3) | `react` | value | `any` | present |  |
| 11 (8) | `storage.get` | async fn | `<T>(key: string, fallback: T): Promise<T>` | present |  |
| 11 (8) | `storage.set` | async fn | `(key: string, value: unknown): Promise<boolean>` | present |  |
| 10 (3) | `Store#.get` | fn | `(): T` | present |  |
| 10 (10) | `elements.Tooltip` | value | `React.ComponentType<P>` | present |  |
| 10 (7) | `members.useMember` | fn | `(userId: string): SlackMember \| undefined` | present |  |
| 10 (7) | `waitForExport` | async fn | `<T>(matcher: ExportMatcher<T>): Promise<T>` | differs | Clack's module shims |
| 9 (3) | `redux.getStore` | fn | `(): SlackStore \| null` | present |  |
| 8 (6) | `channels` | object | `` | present |  |
| 8 (6) | `redux.patchSlice` | fn | `<T = any>(sliceName: string, mapEntry: MapEntry<T>, addedKeys?: (() => Iterable<string>…` | present |  |
| 8 (5) | `rtm` | object | `` | present |  |
| 8 (5) | `rtm.on` | fn | `(type: string, listener: RtmListener): () => void` | present |  |
| 8 (6) | `settings.set` | async fn | `(key: string, value: unknown): Promise<boolean>` | present |  |
| 7 (6) | `Cache` | fn | `<T>(name: string, ttlMs?: number \| ((value: T) => number) \| undefined, maxEntries?: num…` | present |  |
| 7 (5) | `channels.getCachedChannel` | fn | `(channelId: string): SlackChannel \| undefined` | present |  |
| 7 (7) | `getExport` | fn | `<T>(matcher: ExportMatcher<T>): T \| undefined` | differs | Clack's module shims |
| 7 (7) | `onDocument` | fn | `(cb: (doc: Document) => void): () => void` | present |  |
| 7 (5) | `redux.mapEntries` | fn | `<T = any>(object: object, mapEntry: MapEntry<T>, addedKeys?: (() => Iterable<string>) \|…` | present |  |
| 6 (5) | `modal` | object | `` | present |  |
| 5 (5) | `elements.Button` | value | `React.ComponentType<P>` | present |  |
| 5 (4) | `main.on` | fn | `(event: string, cb: (payload: unknown) => void): () => void` | present |  |
| 5 (4) | `members.getCachedMember` | fn | `(userId: string): SlackMember \| undefined` | present |  |
| 5 (4) | `messages.getRawMessage` | fn | `(channel: string, ts: string): SlackMessage \| undefined` | present |  |
| 4 (4) | `elements.Avatar` | value | `React.ComponentType<P>` | present |  |
| 4 (2) | `elements.MrkdwnElement` | value | `React.ComponentType<P>` | present |  |
| 4 (4) | `menu` | object | `` | present |  |
| 4 (3) | `modal.alert` | async fn | `(options: AlertOptions): Promise<void>` | present |  |
| 4 (3) | `redux.patchState` | fn | `(patch: StatePatch): () => void` | present |  |
| 3 (1) | `redux.getThunkCreator` | fn | `(name: string): ThunkCreator \| undefined` | present |  |
| 3 (3) | `settings.addTab` | fn | `(tab: Omit<SettingsTab, "id">): () => void` | present |  |
| 2 (2) | `addSendCheck` | fn | `(check: SendCheck): () => void` | present |  |
| 2 (2) | `menu.addMessageActions` | fn | `(provider: (context: MessageMenuContext) => MessageMenuAction[]): () => void` | present |  |
| 2 (1) | `messages.asRawMessage` | fn | `(msg: SlackMessage \| undefined): SlackMessage \| undefined` | present |  |
| 2 (2) | `messages.getMessageBotId` | fn | `(msg: SlackMessage \| undefined): string \| undefined` | present |  |
| 2 (2) | `modal.openModal` | fn | `(options: OpenModalOptions): ModalHandle \| null` | present |  |
| 2 (2) | `onMessageSendDelta` | fn | `(transform: DeltaTransform): () => void` | present |  |
| 2 (2) | `settings.panel` | fn | `(render: () => React.ReactNode): () => void` | present |  |
| 2 (1) | `storage.delete` | async fn | `(key: string): Promise<boolean>` | present |  |
| 2 (2) | `storedFileUrl` | async fn | `(setting: string): Promise<string \| null>` | present |  |
| 1 (1) | `blocks` | object | `` | present |  |
| 1 (1) | `blocks.fromDelta` | async fn | `(content: unknown[] \| Delta, options?: FromDeltaOptions \| undefined): Promise<Block[]>` | present |  |
| 1 (1) | `channels.getCurrentChannelId` | fn | `(): string \| undefined` | present |  |
| 1 (1) | `channels.makeChannelObject` | fn | `(channel: { id: string; name: string; isPrivate?: boolean \| undefined; isMember?: boole…` | present |  |
| 1 (1) | `deferResizeWork` | fn | `(options: { quietMs: number; onHoldChange?: ((holding: boolean) => void) \| undefined; }…` | present |  |
| 1 (1) | `elements.FormTextInput` | value | `React.ComponentType<P>` | present |  |
| 1 (1) | `elements.InlineAlert` | value | `React.ComponentType<P>` | present |  |
| 1 (1) | `elements.Label` | value | `React.ComponentType<P>` | present |  |
| 1 (1) | `elements.MemberLink` | value | `React.ComponentType<P>` | present |  |
| 1 (1) | `elements.ProfileHoverTrigger` | value | `React.ComponentType<P>` | present |  |
| 1 (1) | `files` | object | `` | present |  |
| 1 (1) | `files.upload` | async fn | `(file: File): Promise<string>` | present |  |
| 1 (1) | `findModuleId` | fn | `(value: any): string \| undefined` | differs | Clack's module shims |
| 1 (1) | `members.getMember` | async fn | `(userId: string): Promise<SlackMember \| undefined>` | present |  |
| 1 (1) | `members.modifyMemberObject` | fn | `(member: SlackMember, edits: { name?: string \| undefined; displayName?: string \| undefi…` | present |  |
| 1 (1) | `menu.Menu` | fn | `({ template, position, children }: MenuProps): any` | present |  |
| 1 (1) | `menu.getMessageActions` | fn | `(context: MessageMenuContext): MessageMenuAction[]` | present |  |
| 1 (1) | `messages.injectMessages` | fn | `(getMessages: () => Iterable<SlackMessage>): () => void` | present |  |
| 1 (1) | `messages.modifyMessageObject` | fn | `(message: SlackMessage, edits: { sentBy?: string \| undefined; }): SlackMessage` | present |  |
| 1 (1) | `messages.useActivityMessage` | fn | `(item: SlackActivityItem \| undefined): SlackMessage \| undefined` | present |  |
| 1 (1) | `messages.useMessageBot` | fn | `(msg: SlackMessage \| undefined): SlackBot \| undefined` | present |  |
| 1 (1) | `moduleSources` | fn | `(): Generator<[id: string, source: string], any, any>` | differs | Clack's module shims |
| 1 (1) | `onMessageSendBlocks` | fn | `(transform: BlocksTransform): () => void` | present |  |
| 1 (1) | `redux.thunkNames` | fn | `(): string[]` | present | fixed here (was missing): the creators Clack defines (03_redux.js thunkNames) |
| 1 (1) | `redux.waitForThunkCreator` | async fn | `(name: string): Promise<ThunkCreator>` | differs | as dispatchThunk |
| 1 (1) | `storage.entries` | async fn | `<T>(prefix?: string, { yieldBetweenBatches }?: { yieldBetweenBatches?: boolean \| undefi…` | present |  |

Unused by the 59 plugins (29 members), all present unless marked: `Store#.subscribe`, `Store#.update`, `blocks.makeDelta`, `blocks.toMarkdown`, `blocks.toPlainText`, `elements.BasicSelect`, `elements.Blocks`, `elements.ConfirmationModal`, `elements.FieldSet`, `elements.Hint`, `elements.IconButtonBase`, `elements.Legend`, `elements.MenuFromTemplate`, `elements.MenuTrigger`, `fetch`, `getByProps` (differs), `getComponent` (differs), `getComponentSource` (differs), `getFiberFromNode` (differs), `getRenderedComponent` (differs), `getValueSource` (differs), `id`, `messages.diagnostics`, `messages.diagnostics.historyShapeFailures` (differs), `modal.confirm`, `storage.clear`, `storage.keys`, `waitForComponent` (differs), `waitForRenderedComponent` (differs).

### Slack thunks plugins name

| thunk | Taut plugins | Slick plugins | Clack |
|---|---|---|---|
| `showNotification` | patch: ShutUpSlackbot, StreamerMode | patch: Censorship, ShutUpSlackbot | present (Clack's UI dispatches it) |
| `enqueueHuddleToast` | dispatch: HuddleActivity; patch: HuddleActivity | wait: HuddleMuteDetector | present (native) |
| `handleDeepLink` |  | dispatch: ChatTabs, InstalledApps, SlickMCP | present (native) |
| `addMemberToPresenceSubscription` |  | dispatch: LastSeen, OnlineCount | present (native; fixed here, was missing): Slack's presence manager, core/presence_subs.go |
| `apiCall` |  | patch: MessageLogger, Nicknames | present (native) |
| `autocompleteChannels` | patch: PrivateChannel | patch: PrivateChannelMapper | present (Clack's UI dispatches it) |
| `autocompleteMembers` | patch: NativeAtChannel | dispatch: BetterHuddles | differs: Clack's UI dispatches it, so patches run; a plugin's own dispatch rejects ("Clack has no native autocompleteMembers") |
| `currentUserStartedTyping` | patch: OwlMode, SilentTyping |  | present (native) |
| `displayAlertMessage` | patch: HuddleActivity | patch: HuddleMuteDetector | present (native) |
| `executeAppAction` | wait: NativeAtChannel, OwlMode |  | present (native) |
| `fetchRawChannelsById` | patch: PrivateChannel | patch: PrivateChannelMapper | present (native) |
| `getChannelMembershipInfoForUsers` | dispatch: HaikuWarning, NativeAtChannel |  | present (native) |
| `joinChannel` | wait: QuickJoin | dispatch: QuickJoin | present (native) |
| `markLastRead` | dispatch: ShutUpSlackbot | dispatch: BetterBlock | present (native) |
| `prepareAndSendMessage` | patch: NativeAtChannel, OwlMode |  | present (Clack's UI dispatches it) |
| `removeMemberFromPresenceSubscription` |  | dispatch: LastSeen, OnlineCount | present (native; fixed here, was missing): Slack's presence manager, core/presence_subs.go |
| `setUserPrefByApi` | dispatch: SlimMessageBox; patch: SlimMessageBox |  | present (native) |
| `submitView` | wait: NativeAtChannel, OwlMode |  | present (native) |
| `addAndUploadPendingFile` | patch: AnonymizeFilenames |  | present (native) |
| `changeReactionFromUserAction` | patch: OwlMode |  | present (Clack's UI dispatches it) |
| `chatDeleteFetcher` | patch: NativeAtChannel |  | present (native) |
| `clearDrafts` | patch: NativeAtChannel |  | present (native) |
| `closeFlexpane` |  | dispatch: ChatTabs | missing: never dispatched, patches never run; a dispatch rejects |
| `commitMessageEdit` | patch: NativeAtChannel |  | present (native) |
| `deleteDraftApi` | dispatch: NativeAtChannel |  | present (native) |
| `editFileApi` |  | dispatch: AnonymiseFileNames | present (native) |
| `getContextMenuTemplate` | patch: QuickJoin |  | present (Clack's UI dispatches it) |
| `getLink` |  | dispatch: BetterHuddles | missing: never dispatched, patches never run; a dispatch rejects |
| `getMemberContextMenuTemplate` | patch: Nicknames |  | present (Clack's UI dispatches it) |
| `initializeUserPrefs` | patch: SlimMessageBox |  | present (native) |
| `joinCodeChannel` |  | dispatch: QuickJoin | missing: never dispatched, patches never run; a dispatch rejects |
| `maybeFetchBackgrounds` | dispatch: HuddleActivity |  | present (native) |
| `maybePersistPendingFileData` |  | dispatch: AnonymiseFileNames | present (native) |
| `navigate` |  | dispatch: ChatTabs | present (native) |
| `notifyMember` |  | dispatch: BetterHuddles | missing: never dispatched, patches never run; a dispatch rejects |
| `openEditProfileModal` |  | patch: Nicknames | present (Clack's UI dispatches it) |
| `openInSplitView` |  | dispatch: ChatTabs | missing: never dispatched, patches never run; a dispatch rejects |
| `processAwsTranscription` | patch: HuddleActivity |  | present (native) |
| `requestReplies` |  | dispatch: BetterBlock | missing: never dispatched, patches never run; a dispatch rejects |
| `setUserPrefFetcher` | patch: SlimMessageBox |  | present (native) |
| `showThread` |  | dispatch: BetterHuddles | missing: never dispatched, patches never run; a dispatch rejects |
| `startEventLog` | patch: HuddleActivity |  | present (native) |
| `stopEventLog` | patch: HuddleActivity |  | present (native) |
| `stopHuddle` | patch: HuddleActivity |  | present (native) |
| `submitAddOrRemoveReaction` | patch: OwlMode |  | present (Clack's UI dispatches it) |
| `uploadFiles` | patch: AnonymizeFilenames |  | present (Clack's UI dispatches it) |


## Plan: what's left, most used first

1. **Thunks plugins dispatch that Clack doesn't define** (7; see the thunk
   table): `requestReplies` (BetterBlock),
   `getLink`, `notifyMember`, `showThread` (BetterHuddles),
   `openInSplitView`, `closeFlexpane` (ChatTabs) and `joinCodeChannel`
   (QuickJoin). Also `autocompleteMembers`, which a plugin can dispatch
   (BetterHuddles) but which has no native, and the presence manager's
   `getPresenceSubscriptionByTeamId` export, which OnlineCount looks up for
   its 950-member cap (without it the cap counts nothing already
   subscribed).
2. **patchComponent / elements for Slick**: 110 uses in 41 Slick plugins
   name 72 components. They need the catalogue PLUGINS-components.md keeps
   for Taut's 52.
3. **redux** (284 uses): `useReduxState`, `getRawState`, `patchSlice` and
   `mapEntries` read the mirror (PLUGINS-state.md). Each slice's shape
   against Slack's store is that doc's job; compare what Taut's and Slick's
   plugins read (5 and 6 slice names here) entry by entry.
4. **Store, storage, Cache, SharedStore, settings, main** (Taut 55, Slick
   150 uses): compare each method's semantics with upstream's (persistence,
   change events, errors).
5. **The ApiQ details above**: merging identical requests and the
   dummy-request window, apiCall's token and request metadata.
6. Everything else marked differs in the tables, then a behaviour pass over
   the "present" rows the notes don't vouch for.

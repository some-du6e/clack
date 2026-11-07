# Taut component, CSS and UI-API catalogue (source-derived)

Built from taut @ 6fe5123 (2026-11-06), `/tmp/taut/plugins/*.ts(x)` (40 files, including `ShinigamiEyes.disabled.tsx`) and `/tmp/taut/app/**`. It lists every Slack React component name, Slack class name, `data-qa`/attribute selector and UI API that taut plugins and taut's framework touch, and gives the Clack widget each one maps to. Slack's real props for these components, read live from the web client (shapes only), are in [PLUGINS-props-live.md](PLUGINS-props-live.md). Read it with [PLUGINS.md](PLUGINS.md) (the runtime design) and the "UI package ownership" table in [NATIVE.md](NATIVE.md). Taut is GPLv3: this file records names, prop shapes and behaviour only. Never copy its code.

Paths `P/X.tsx:N` mean `/tmp/taut/plugins/X.tsx` line N. `app/...` means `/tmp/taut/app/...`. Clack paths are relative to `internal/ui/`. Owners: **shell** = ui-shell (`internal/ui` root), **msgs** = ui-messages (`messages`, `richview`), **comp** = ui-composer, **pop** = ui-popups, **views** = ui-views, **taut-ui** = `slackc` + `internal/taut`.

## 0. What a "component name" is (app/slack/react.tsx)

Clack's `slackc.Render(gtx, name, props, native)` has to reproduce these rules exactly.

- **Lookup names** (`lazyComponent`, `waitForComponent`, `getComponent`, `api.elements.*`). Function or class components: `displayName` or `name`. `memo`: its own `displayName` or the inner type's name. `forwardRef`: `displayName`, `render.displayName` or `render.name`. Lookups search webpack exports only (`module.exports` and each own key), first match wins, and `getComponent(name, true)` returns all matches.
- **Render-time name** (`patchComponent`, `getRenderedComponent`). `memo` unwraps to the inner type's name, and the memo's own displayName is **ignored**. `forwardRef`: `displayName || render.displayName || render.name`. Function or class: `displayName || name`. Every element type goes through taut's `createElement`/`jsx`/`jsxs` hook, and the first type seen under a name is stored in `renderedComponents` (`getRenderedComponent`, `waitForRenderedComponent`).
- **Matchers.** `patchComponent` takes a `string` (exact render-time name) or `{displayName?, filter?, component?}`. `component` matches by identity. `filter(type)` must also return true when it is given. A spec with **no displayName** matches **any** type the filter accepts (two plugins rely on this, see §1.14). Patches compose in registration order, first registered innermost. `Original` is an opaque "original component" object, and the prop `__original: true` renders the raw type unpatched. Names are persisted as `patch_targets` so the wrapper exists before the plugin starts (bootstrap.ts:27-38).
- **Source markers** (`drawing(marker)`, elements.ts:366). `getComponentSource(type).includes(marker)` reads the whole webpack module factory source. Five elements depend on it: `Button` `"c-button"`, `Tooltip` `"data-sk":"tooltip"`, `Label` `"c-label"`, `Legend` `"c-legend"`, `Popover` `"c-popover"`. Two plugin patches also use source substrings: `canJoinByExternalHuddleLink` and `c-message_kit__gutter__left`. Clack must give each native component a source string (or an equivalent marker set) that contains these substrings.
- Wrapper names are literal strings: `Connect(EmojiMenuTrigger)`, `withKeyboardFocusContext(ContextMenuTrigger)`, `Serialized(ThreadsFlexpane)`. Lowercase names exist (`texty_input_with_autocomplete`).
- There is **no** `this.api.ui` and **no** `findComponent`. The elements object is `this.api.elements` (pluginManager.ts:160), and plugins often alias it as `ui`. Lookup API: `waitForComponent`, `waitForRenderedComponent`, `lazyComponent`, `getComponent`, `getRenderedComponent`, `patchComponent` (pluginManager.ts:126-140).

## Index

**92 names**: 89 named Slack components, 2 anonymous filter-matched memos and 1 name that is only compared, never patched (`ThreadCompactGutterGeneric`). "Status" says whether Clack draws the surface today and whether it can be wrapped at that boundary. Nothing calls `slackc.Render` yet, so every row still needs the owner to split the widget out at that boundary.

| # | Name | API | Users | Clack widget | Owner | Status |
|---|---|---|---|---|---|---|
| 1 | MessageBackground | patch | ShowSendingBot, StreamerMode | messages/row.go:`(*row).layout` (background states) | msgs | drawn, inline |
| 2 | MessageWrapper | patch | ShowRealUser | messages/row.go:`(*row).layout` | msgs | drawn, inline |
| 3 | MessageListItem | patch | ShowRealUser (search results) | popups/search_view.go:`(*SearchView).card` | pop | drawn, inline |
| 4 | SearchResult | patch | ShowSendingBot | popups/search_view.go:`(*SearchView).card` (+ search_popover.go `row` message cards) | pop | drawn, inline |
| 5 | MessageAvatar | patch | ShowSendingBot | messages/row.go:`(*row).layout` avatar (`drawAvatar`) | msgs | drawn, inline |
| 6 | ConnectedBaseAvatar | patch + elements.Avatar | ShowSendingBot, Nicknames | messages/style.go:`drawAvatar`; popups/util.go:`avatar`; views/kit.go:`(*base).avatar`; chrome.go:`(*App).avatar` | msgs/pop/views/shell | 4 private copies, no shared component |
| 7 | (memo, source has `c-message_kit__gutter__left`) | patch `{filter}` | ShowSendingBot | messages/row.go:`(*row).layout` gutter (avatar column + hover time) | msgs | drawn, inline |
| 8 | ThreadCompactGutterGeneric | displayName compare | ShowSendingBot | messages/row.go:`(*row).layout` grouped-row hover time (thread pane) | msgs | drawn, inline |
| 9 | MessageSender | patch | ShowSendingBot | messages/row.go:`(*row).nameLine` | msgs | drawn, inline |
| 10 | BaseMessageSender | patch | IdvStatus, ShinigamiEyes | messages/row.go:`(*row).nameLine` (`nameBtn`) | msgs | drawn, inline |
| 11 | BaseBroadcastPreamble | patch | UserPronouns | messages/row.go:`(*row).nameLine` (sender + timestamp children) | msgs | drawn, inline |
| 12 | ThreadSenderAndTimestampGeneric | patch | ShowSendingBot, UserPronouns | messages/row.go:`(*row).nameLine` in ThreadView; views/threads.go:`(*Threads).msg` | msgs/views | drawn, inline |
| 13 | ThreadRootGeneric | patch | ShowRealUser | messages/thread.go root row (`itThreadHead` in list.go `layoutItem`); views/threads.go:`(*Threads).card` | msgs/views | drawn, inline |
| 14 | MessageBlocks | patch | OwlMode | richview/view.go:`View.Layout` → blocks.go:`(*lay).blocks` | msgs | drawn |
| 15 | Blocks | patch + elements.Blocks | ShowRealUser, ShowSendingBot | richview/blocks.go:`(*lay).blocks` | msgs | drawn; interactive elements inert |
| 16 | ViewBlocks | patch | OwlMode | missing (no Block Kit modal/view surface) | pop | **missing** |
| 17 | MessageAttachment | patch | ShowSendingBot | richview/cards.go:`(*lay).attachment` | msgs | drawn |
| 18 | BaseMessageAttachmentSlackMessage | patch | PrivateChannel | richview/cards.go:`(*lay).attachment` (`IsShare` branch) | msgs | drawn |
| 19 | AttachmentMedia | patch | ClickToLoad | richview/cards.go:`(*lay).audio`/`video`/`videoCard`/`unfurl` | msgs | drawn, no iframe embeds |
| 20 | MessageAttachmentHuddleLink | patch | PrivateChannel | richview/cards.go:`(*lay).attachment` fed by richtext/message.go:`huddleBlock` | msgs | drawn, no join CTA |
| 21 | (memo, source has `canJoinByExternalHuddleLink`) | patch `{filter}` | PrivateChannel | same huddle card; join-gate logic missing | msgs | partial |
| 22 | ReactionBar | patch | CopyReacted, OwlMode | messages/row.go:`(*row).reactions` | msgs | drawn, inline |
| 23 | Reaction | patch | WhoReacted | messages/row.go:`(*row).reactions` (per pill) | msgs | drawn, inline |
| 24 | UnstyledButton (`data-qa="reactji"`) | patch | WhoReacted | messages/row.go:`(*row).reactions` pill button | msgs | drawn, inline |
| 25 | ReactionAddButton | patch | CopyReacted | messages/row.go:`(*row).reactions` add button | msgs | drawn, inline |
| 26 | MessageActions | patch | OwlMode | messages/list.go:`(*listState).toolbar` | msgs | drawn, inline |
| 27 | MessageActionButtonReaction | patch | OwlMode | messages/list.go:`(*listState).toolbar` (add-reaction button) | msgs | drawn, inline |
| 28 | EmojiPickerPopover | patch | OwlMode | popups/emoji_picker.go:`NewEmojiPicker`/`NewEmojiPickerAt` (overlay) | pop | drawn |
| 29 | EmojiPicker | patch | OwlMode | popups/emoji_picker.go:`(*EmojiPicker).Layout` | pop | drawn |
| 30 | MessagePaneInput | patch | BetterTypingIndicator; sendChecks infra | composer/composer.go:`(*Composer).Layout` (channel) | comp | drawn |
| 31 | InputContainer | patch | OwlMode, SlimMessageBox; sendChecks infra | composer/composer.go:`(*Composer).Layout` (thread) | comp | drawn |
| 32 | MessageInput | patch | OwlMode; buttons infra | composer/composer.go:`Composer` (+ `chromeState`) | comp | drawn |
| 33 | WysiwygContainer | patch + lazy (RichTextInput) | OwlMode; richTextInput | composer/chrome.go:`(*chrome).Layout` | comp | drawn |
| 34 | TextyButtons | patch | SlimMessageBox; buttons infra | composer/chrome.go:`(*chrome).layoutBar` + `layoutFooter` | comp | drawn |
| 35 | TextyButtonOverflow | patch | buttons infra (every `composer.addButton`) | composer/chrome.go:`(*chrome).layoutFooter` | comp | drawn, no plugin slot |
| 36 | ShortcutsMenuTriggerButton | patch | OwlMode | composer/chrome.go:`layoutFooter` (`crShortcut`) | comp | button only, no menu |
| 37 | BaseContextBarV2 | patch | HaikuWarning, OwlMode | missing (no bar between list and composer; conversation.go:`layoutConvo`) | comp | **missing** |
| 38 | MessageInputRoadblock | patch | RestrictedChannelWarning | missing (composer always shown) | comp | **missing** |
| 39 | ThreadTypingIndicator | patch | BetterTypingIndicator | composer/chrome.go:`(*chrome).layoutHint` (+ typing.go `typingText`) | comp | text only |
| 40 | TypingNames | patch | BetterTypingIndicator | composer/chrome.go:`layoutHint`; composer/typing.go:`typingText` | comp | text only |
| 41 | texty_input_with_autocomplete | lazy | richTextInput | composer/editor.go:`Editor` + autocomplete.go | comp | drawn |
| 42 | CharacterCount | lazy | richTextInput (OfficeHours) | missing | comp | **missing** |
| 43 | ComposerLinkModal | rendered registry, prototype `saveLink` | links infra (ClearURLs) | composer/linkdialog.go:`openLinkDialog` | comp | drawn |
| 44 | Serialized(ThreadsFlexpane) | patch | threads infra (ThreadJump) | messages/thread.go:`ThreadView`; mount.go:`threadPane.Layout` | msgs/shell | drawn |
| 45 | ThreadSearchButton | patch | ThreadJump | missing (thread.go:`(*ThreadView).header` has title + close only) | msgs | **missing** |
| 46 | ThreadFlexpaneAdminTools | patch | SlackFixes | missing | msgs | **missing** |
| 47 | BaseAvatarStack | patch | HumanCount | channel.go:`(*App).layoutChannelHeader` (facepile + count, `hdr.members`) | shell | drawn, inline |
| 48 | HelpButton | patch | StreamerMode; whatsNewButton | chrome.go:`(*App).layoutTopNav` (`nav.help`) | shell | drawn, inline |
| 49 | ListChannelEntity | patch | PrivateChannel | sidebar.go:`(*App).layoutSidebarRow` (`sbConv`) | shell | drawn, inline |
| 50 | BaseMrkdwnChannel | patch | PrivateChannel | richview/flow.go:`(*lay).addSpans` (channel pill) | msgs | drawn |
| 51 | MemberLink | lazy | BetterTypingIndicator | richview/flow.go:`(*lay).addSpans` (user pill) | msgs | drawn |
| 52 | ActivityItem | patch | ShowRealUser, StreamerMode | views/activity.go:`(*Activity).row` | views | drawn |
| 53 | ActivityRowContentSenderAvatarLayout | patch | ShowSendingBot | views/activity.go:`(*Activity).actAvatarOf` | views | drawn |
| 54 | ActivityPrivacyBlurredName | patch | StreamerMode | views/activity.go:`(*Activity).rowContent` (name) | views | no blur variant |
| 55 | RimetoProfilePresence | patch | LastSeen | popups/profile_view.go:`(*ProfileView).body` (presence) | pop | drawn |
| 56 | MemberProfileHoverCard | patch | LastSeen, ShinigamiEyes | popups/profile_card.go:`ProfileCard` (click-open, no hover) | pop | drawn |
| 57 | BasePresence | patch | LastSeen | popups/util.go:`presenceDot`; sidebar.go:`(*App).presenceDot`; views/kit.go:`avatarPresence` | pop/shell/views | private copies |
| 58 | LocalTime | patch | LastSeen | popups/util.go:`localTime` via profile_card.go `pfIconLine` | pop | drawn |
| 59 | RimetoMemberProfileOverflowMenu | patch | AdminBackend, Nicknames | popups/profile_card.go:`pfMoreItems` (+ profile_view.go `pfPaneMenu`) | pop | drawn |
| 60 | EditAudioButton | patch | CustomNameRecording | missing (profile_view.go only has an "Add name pronunciation" link row) | pop | **missing** |
| 61 | CaptionsSubmenu | patch | HuddleActivity | missing (no huddle call UI) | views | **missing** |
| 62 | CaptionsItemsMenu | patch | HuddleActivity | missing | views | **missing** |
| 63 | HuddleMetaEvent | patch | HuddleActivity | missing (no huddle event log) | views | **missing** |
| 64 | HuddleBaseEvent | patch | HuddleActivity | missing | views | **missing** |
| 65 | HuddlesAlertBannerBase | patch | HuddleActivity | missing | views | **missing** |
| 66 | Tabs | patch | settings (Preferences) | prefs_modal.go:`prefsModal` (+ `prefsNav`) | shell | drawn, fixed sections |
| 67 | MenuFromTemplate | patch + elements | AccountSwitcher, AdminBackend, Nicknames; menu.Menu | popups/menu.go:`Menu` (`NewMenu`, `MenuItem`) | pop | drawn |
| 68 | MenuTrigger | elements | menu.Menu (CopyReacted, Experiments) | popups/menu.go:`NewMenuOpts` (anchor) | pop | drawn |
| 69 | withKeyboardFocusContext(ContextMenuTrigger) | elements | menu.ContextMenu (PrivateChannel) | popups/menu.go:`NewMenu` at pointer | pop | drawn |
| 70 | SvgIcon | elements | 11 plugins + composer buttons + app | icons/`icons.Get(name)` | shell | drawn, icon names differ |
| 71 | ProfileHoverTrigger | elements | ShowSendingBot | Host `OpenProfile` (click), no hover | pop | partial |
| 72 | MrkdwnElement | elements | CopyReacted, HaikuWarning, OfficeHours, OwlMode, ShinigamiEyes + settings | richview/view.go:`View` (+ richtext parse) | msgs | drawn |
| 73 | Button | elements (`"c-button"`) | 8 plugins + settings | popups/style.go:`Button`; views/kit.go:`(*base).button` | pop | drawn |
| 74 | Tooltip | elements (`"data-sk":"tooltip"`) | 7 plugins + composer buttons | popups/popover.go:`Tooltip`; messages/list.go:`tooltip` | pop | drawn |
| 75 | KeyboardKeysTooltip | lazy | StreamerMode | popups/popover.go:`Tooltip` (shortcut arg) + style.go `kbd` | pop | drawn |
| 76 | IconButtonBase | elements | OwlMode, composer buttons | popups/style.go:`iconButton`; widgets.go:`iconButton` | pop/shell | drawn |
| 77 | ConfirmationModal | elements (+ every `api.modal.*`) | 7 plugins + settings | popups/modal.go:`Confirm` / `NewModal` | pop | drawn |
| 78 | InlineAlert | elements | CustomNameRecording, Experiments, settings | missing as a component (row.go `failedLine` only) | pop | **missing** |
| 79 | Label | elements (`"c-label"`) | AdminBackend, Nicknames, OwlMode, PrivateChannel, settings | missing as a component (labels inline in modals) | pop | **missing** |
| 80 | FormTextInput | elements | AdminBackend, Experiments, Nicknames, OwlMode, PrivateChannel, settings | popups/style.go:`field` | pop | drawn |
| 81 | Checkbox | elements | settings | private copies (composer `crCheckbox`, prefs `option`, views `actCheck`) | pop | **missing** as component |
| 82 | FilterPill | elements | Experiments, OfficeHours, settings | popups/search_view.go:`pill`; views/kit.go:`pillRow` | pop/views | private copies |
| 83 | Tag | elements | settings (changelog) | missing | pop | **missing** |
| 84 | DateRangePicker | elements | none (exposed only) | popups/datetime.go:`dtDate`×2 | pop | partial |
| 85 | DatePickerTrigger | elements | ThreadJump | popups/datetime.go:`dtCalendar` | pop | drawn, not triggerable |
| 86 | TimePicker | elements | OfficeHours | popups/datetime.go:`dtTime` + `dtTimeList` | pop | drawn |
| 87 | FieldSet | elements | settings, plugin data panels | missing | pop | **missing** |
| 88 | Legend | elements (`"c-legend"`) | settings | missing | pop | **missing** |
| 89 | Hint | elements | none (exposed only) | missing | pop | **missing** |
| 90 | BasicSelect | elements | Experiments, settings | views/directories.go:`dirDropdown.layoutSelect` | views | private |
| 91 | Connect(EmojiMenuTrigger) | elements | OfficeHours | Host `OpenEmojiPicker` | pop | drawn |
| 92 | Popover | elements (`"c-popover"`) | ThreadJump, settings, whatsNew | popups/popover.go:`NewPopover` | pop | drawn |

`RichTextInput` (`api.elements.RichTextInput`) is taut's own composite of #33 + #41 + #42 (app/slack/richTextInput.tsx). It is not a Slack name.

---

## 1. Per-name detail

Shared Slack shapes used below:
- `SlackMessage` = `{channel, ts, thread_ts?, user, bot_id?, app_id?, bot_profile?: {id, app_id, team_id, name, icons}, subtype?, text, blocks?, blocksProcessed?, attachments?: [{author_link, author_id, author_name, author_icon, author_subname, channel_id, ts}], files?, metadata?: {event_type, event_payload: {source_user_id?, poster?}}, reply_count?, latest_reply?, source_team_id?, edited?, taut_bot_id?}`.
- `MenuTemplateItem` = `{key: string, label?: ReactNode, description?: ReactNode, type?: 'submenu'|'separator'|'header'|'custom'|'radio'|'checkbox', checked?, template?: MenuTemplateItem[], icon?: string, click?(e?), disabled?, danger?}` (elements.ts:82-96).
- `Member` = `{id, name, real_name, profile: {display_name, real_name, image_24, image_48, pronouns, bot_id, api_app_id}}`.
- "Context wrap" means the patch renders `<Ctx.Provider value=…><Original {...props}/></Ctx.Provider>`. That is invisible, but it requires Clack to render the plugin subtree **inside** the native component's subtree, so that descendants patched later can read the context.

### 1.1 Message row

#### MessageBackground
- **ShowSendingBot** P/ShowSendingBot.tsx:334 (helper `provideMessage` :252-261). Reads `msg: SlackMessage`. Context wrap with `msg`.
- **StreamerMode** P/StreamerMode.tsx:216-228. Reads `msg.channel`, `className`. If `state.channels[ch]` is `is_im`/`is_mpim`/`is_private`, it appends class `taut-streamer-mode__private-thread`. CSS then blurs `.c-message_kit__gutter__left/right` beneath it inside `.p-threads_view`.
- **Clack**: messages/row.go:`(*row).layout` (hover/pinned/editing/highlight backgrounds), **msgs**. It must carry `className` (with its hover state `c-message_kit__background--hovered`) and contain the gutter (#7) and body as children.

#### MessageWrapper
- **ShowRealUser** P/ShowRealUser.tsx:185-201 (loop over `['MessageWrapper','ThreadRootGeneric','ActivityItem']`). Reads and overrides `msg` with a memoized `fixed(msg)` (keyed on `usePatchVersion()`). For a relay bot (bot_id in RELAY_BOTS L16-45, sender from `metadata.event_payload.source_user_id`, or `.poster` for B09QQ24JRL1) it sets `user` and drops `bot_id, app_id, username, icons, bot_profile, display_as_bot` and `subtype:'bot_message'`, keeping `taut_bot_id`. It rewrites forwarded attachments' `author_id/author_name/author_icon/author_link` and deletes `author_subname`.
- **Clack**: messages/row.go:`(*row).layout` (whole row, before `resolve`), **msgs**. The `msg` prop must be the input the row actually draws from (name, avatar, APP badge).

#### MessageListItem
- **ShowRealUser** P/ShowRealUser.tsx:212-226. Reads `result: {messages: SlackMessage[]}` (search results) and overrides it with each message fixed.
- **Clack**: popups/search_view.go:`(*SearchView).card`, **pop**.

#### SearchResult
- **ShowSendingBot** P/ShowSendingBot.tsx:336-339. Treats **the whole props object as a message** (`ts, user, bot_id, blocks…`) plus `channel: {id}|string`, and provides `{...props, channel: props.channel?.id ?? props.channel}`. Context wrap.
- **Clack**: popups/search_view.go:`(*SearchView).card` and search_popover.go:`(*SearchPopover).row` message cards, **pop**. Props must be message-shaped at the top level.

#### MessageAvatar
- **ShowSendingBot** P/ShowSendingBot.tsx:335. Reads `msg`. Context wrap.
- **Clack**: messages/row.go:`(*row).layout` avatar slot (`avatarBtn`, `drawAvatar`), **msgs**.

#### ConnectedBaseAvatar (also `api.elements.Avatar`)
- **ShowSendingBot patch** P/ShowSendingBot.tsx:355-367. Reads `className, size, userId`. It only acts when `className` contains `c-message_kit__avatar`, `c-search_message__avatar` or `c-message_attachment__author_icon` (:459-464) **and** there is a SenderContext message. It then wraps Original in `<span class="taut-sending-bot">{Original}<Badge/></span>`, badge 20px if size ≥ 32 else 14 (default size 36).
- **ShowSendingBot render** (:66-90) passes `botId, botProfile, userId, size: size-2, isInteractive`, wrapped in `ProfileHoverTrigger`.
- **Nicknames render** P/Nicknames.tsx:248,266 passes `userId, size: 36, isInteractive: false`.
- Declared props (elements.ts:195-214): `userId?, botId?, botProfile?, icons?, size?: number, className?, isInteractive?, showCard?, showTooltip?, messageTs?, ariaHidden?, tabIndex?, 'data-qa'?`.
- **Clack**: messages/style.go:`drawAvatar` (row), popups/util.go:`avatar`, views/kit.go:`(*base).avatar`, chrome.go:`(*App).avatar`, owned by **msgs/pop/views/shell**. Clack needs **one** `ConnectedBaseAvatar` native host component that each site renders with its Slack className (`c-message_kit__avatar` in rows, `c-search_message__avatar` in search, `c-message_attachment__author_icon` in attachments).

#### (anonymous memo whose module source includes `c-message_kit__gutter__left`)
- **ShowSendingBot** P/ShowSendingBot.tsx:393-422. Spec `{filter: memo && typeof type==='function' && String(type).includes('c-message_kit__gutter__left')}`, no displayName. Reads `light?: ReactNode, compact?: ReactNode` (GutterProps). A slot counts as a compact gutter if it is an element whose `props.className` includes `p-message_pane_message__compact_timestamp`, or whose `type.type.displayName === 'ThreadCompactGutterGeneric'`. It overrides that slot (compact first, then light) with `<>{slot}<SenderBadge size=18 corner="gutter"/></>`.
- **Clack**: messages/row.go:`(*row).layout`, the left gutter column (avatar on first rows, hover time "3:04" on grouped rows), **msgs**. Clack needs a gutter component whose "source" contains `c-message_kit__gutter__left`, with props `light`/`compact` as **element slots**. The compact one is an element with `className: 'p-message_pane_message__compact_timestamp'` in channels, or a memo whose inner displayName is `ThreadCompactGutterGeneric` in threads.

#### ThreadCompactGutterGeneric
- Never patched. ShowSendingBot (:477) compares `slot.type.type.displayName` with it, so the element type must be a memo object whose `.type.displayName` is this string.
- **Clack**: thread-pane grouped-row hover time inside `(*row).layout`, **msgs**.

#### MessageSender
- **ShowSendingBot** P/ShowSendingBot.tsx:432-441. Props pass through. Inside a ThreadSenderContext with `state.userPrefs.messages_theme === 'dense'`, it renders `<>{Original}<DenseSenderBadge size=18 corner="inline"/></>`. Otherwise it leaves Original unchanged.
- **Clack**: messages/row.go:`(*row).nameLine`, **msgs**.

#### BaseMessageSender
- **IdvStatus** P/IdvStatus.tsx:43-85. Reads `userId, botId, className`. It skips bots, ids not starting U/W, and USLACKBOT. Then it overrides `className` by appending `taut-idv-status--not-eligible` or `taut-idv-status--over-18`, whose CSS draws a wavy underline on itself and on `.c-message__sender_button`.
- **ShinigamiEyes** (disabled) P/ShinigamiEyes.disabled.tsx:69-105. Reads `botId, userId, className` and appends `taut-trust-level--0..4`. The CSS `::before { content: "<emoji> " }` prefixes the name with an emoji.
- **Clack**: messages/row.go:`(*row).nameLine` (`nameBtn`, name 15/900, APP badge, status emoji), **msgs**. The component must honour `className`, with the inner button carrying `c-message__sender_button`.

#### BaseBroadcastPreamble
- **UserPronouns** P/UserPronouns.tsx:82-104. Despite the name, this is the channel message **header** that receives the sender and the timestamp as `children` (comment at :80). Reads `msg: {user, bot_id}` and `children`. It finds the timestamp child (the first element whose props have a `clickable` key and `visible !== false`), otherwise the sender child (the first with an `isInteractive` key). It inserts `<Pronouns key="taut-pronouns" userId bulleted={timestamp found}/>` after it and renders `<Original {...props}>{children}</Original>`.
- **Clack**: messages/row.go:`(*row).nameLine`, **msgs**. The children must be real elements with props `isInteractive` (sender) and `clickable`/`visible` (timestamp), and must be re-orderable. Not the same as `(*row).broadcastLine` ("replied to a thread").

#### ThreadSenderAndTimestampGeneric
- **ShowSendingBot** P/ShowSendingBot.tsx:424-431. Reads `msg`. Context wrap (ThreadSenderContext).
- **UserPronouns** P/UserPronouns.tsx:107-124. Reads `msg, adjacent, visible, omitTimestamp, omitLinebreak`. Unless `adjacent`, `visible===false` or the sender isn't a person, it renders `<><Original {...props} omitLinebreak/><Pronouns bulleted={!omitTimestamp}/>{props.omitLinebreak ? null : <br/>}</>`.
- **Clack**: messages/row.go:`(*row).nameLine` as drawn in the ThreadView, and views/threads.go:`(*Threads).msg`, owned by **msgs/views**.

#### ThreadRootGeneric
- **ShowRealUser** (same loop as MessageWrapper, P/ShowRealUser.tsx:185-201). Overrides `msg` with the fixed message.
- **Clack**: thread root at the top of messages/thread.go:`ThreadView` (`itThreadHead` in list.go:`(*listState).layoutItem`), plus the root in views/threads.go:`(*Threads).card`, owned by **msgs/views**.

### 1.2 Message body, Block Kit, attachments

#### MessageBlocks
- **OwlMode** P/OwlMode.tsx:500-516. Reads `msg: {channel (starts with 'D'), bot_id, bot_profile.app_id, blocks}`. For DMs from the owl app (A0C5L27PB9P / A0CDBA8FXDL) whose blocks parse as a confirmation (`/(?:Post|Confession) #(\d+)/`, key `/Your private reply key:\n([0-9a-f]{64})/`), it renders `<>{Original}<SaveButton/></>`. SaveButton is `div.p-actions_block[data-stringify-ignore] > div.p-actions_block_elements > div.p-actions_block__action > Button(outline, small, className "p-block_kit_button_element taut-owl-save__button")`.
- **Clack**: richview/view.go:`View.Layout`, then blocks.go:`(*lay).blocks`, **msgs**. Needs a boundary *around* the message's blocks so siblings can be appended below them.

#### Blocks (also `api.elements.Blocks`)
- **ShowRealUser** P/ShowRealUser.tsx:204-209. Overrides `msg` with `withBot(msg)`, which restores `bot_id` and `bot_profile` from the raw store when `taut_bot_id` is set (so the bot's Block Kit buttons still work).
- **ShowSendingBot** P/ShowSendingBot.tsx:443-452 (config `hideMcpFooter`). Reads `msg.blocks` (last block `{type:'context', elements:[{type:'mrkdwn', text:'*Sent using* <@U…>'}]}`) and `blocksProcessed`. When the app matches, it passes `msg` with the last `blocks` and `blocksProcessed` entries removed.
- Declared element props (elements.ts:188-192): `msg: {blocks?, …}`, `blocksContainerContext?: 'message'|string`, `streaming?`.
- **Clack**: richview/blocks.go:`(*lay).blocks`/`block`/`section`/`context`/`button`/`image`/`table`, **msgs**. Selects, overflow menus, date pickers, inputs, checkboxes and radios render as inert placeholders (gap).

#### ViewBlocks
- **OwlMode** P/OwlMode.tsx:483-499. Reads `view: {appId, blocks}` (an app modal's blocks, scanning `block.text.text`). When `appId` is an owl app and the blocks parse as a confirmation, it renders `<>{Original}<SaveButton/></>`.
- **Clack**: **missing**. There are no Block Kit modals/views (`views.open` surfaces). The **pop** owner needs a Block Kit modal that renders richview blocks through a `ViewBlocks` boundary.

#### MessageAttachment
- **ShowSendingBot** P/ShowSendingBot.tsx:340-353. Reads `activeChannelId, messageTs, attachmentIndex (default 0), authorId, authorLink` and the raw stored `attachments[i].author_link`. It provides a synthesized message `{user: authorId, bot_id: /\/services\/(B…)/ match}`. Context wrap. Its inner author icon must be `ConnectedBaseAvatar className="c-message_attachment__author_icon"`.
- **Clack**: richview/cards.go:`(*lay).attachment` (author row), **msgs**.

#### BaseMessageAttachmentSlackMessage
- **PrivateChannel** P/PrivateChannel.tsx:689-706. Reads `channelId, channelName, isChannelNonExistent, isPrivate, isMessageNonExistent`. When `isChannelNonExistent && channelId`, it renders Original with `channelName = channelName || channelId`, `isPrivate: true`, `isMessageNonExistent: false`.
- **Clack**: richview/cards.go:`(*lay).attachment` `IsShare` branch ("Posted in #ch"), **msgs**.

#### AttachmentMedia
- **ClickToLoad** P/ClickToLoad.tsx:44-74. Reads `imageUrl?, videoHtml?, audioHtml? (HTML containing <iframe src>), audioHtmlHeight?`. When `audioHtml` is set and there's no image or video, and the provider (spotify.com, soundcloud.com, music.apple.com, other) is enabled and not yet clicked, it **replaces** Original with its placeholder. That placeholder is `div.c-message_attachment__audio.taut-click-to-load[style=height][data-stringify-ignore] > div.c-message_attachment__video_thumb > div.c-message_attachment__video_buttons > button.c-button-unstyled.c-message_attachment__video_play[aria-label] > i.c-deprecated-icon.c-icon--play.c-deprecated-icon--inherit` + `span.taut-click-to-load__label`. Clicking renders Original.
- **Clack**: richview/cards.go:`(*lay).audio`, `video`, `videoCard`, `unfurl`, **msgs**. Clack doesn't play iframe embeds, so Original is a static unfurl. The props must still carry `audioHtml`/`videoHtml`/`imageUrl` from the attachment.

#### MessageAttachmentHuddleLink
- **PrivateChannel** P/PrivateChannel.tsx:530-537. Reads `huddle.channel_id`. It wraps Original in `redux.StateView` with Slack's raw `channels[id]`, so "Request to join" appears for unknown channels.
- **Clack**: richview/cards.go:`(*lay).attachment` drawing richtext/message.go:`huddleBlock` ("Huddle in progress" / "A huddle happened"), **msgs**. There is no join button.

#### (anonymous memo whose module source includes `canJoinByExternalHuddleLink`)
- **PrivateChannel** P/PrivateChannel.tsx:538-551. Filter: `$$typeof === memo && typeof type==='function' && !type.name && String(type).includes('canJoinByExternalHuddleLink')`. Reads `channelId`, StateView wrap.
- **Clack**: the huddle card's join gate inside `(*lay).attachment`, **msgs**. It needs a component whose source contains `canJoinByExternalHuddleLink` and which has an empty function name. It only matters once Clack has the join CTA.

### 1.3 Reactions and hover actions

#### ReactionBar
- **CopyReacted** P/CopyReacted.tsx:191-205. Reads `reactionKey: 'message-<ts>-<channelId>'` (regex `^message-(\d+\.\d+)-([A-Z0-9]+)$`) and `reactions: {name, users: string[], count}[]`. Context wrap.
- **OwlMode** P/OwlMode.tsx:391-408. Reads `reactionKey, className, currentUser`. In owl threads it overrides `className += ' taut-owl-reactions[ taut-owl-demo]'` and `currentUser = <owl root message user>`, and adds a context wrap.
- **Clack**: messages/row.go:`(*row).reactions`, **msgs**. The props must be Slack-shaped: `reactionKey`, `reactions[]`, `currentUser`, `className` (carrying `c-reaction_bar`).

#### Reaction
- **WhoReacted** P/WhoReacted.tsx:92-99. Reads `users: string[]`. Context wrap.
- **Clack**: one pill inside `(*row).reactions`, **msgs**.

#### UnstyledButton
- **WhoReacted** P/WhoReacted.tsx:102-113. Reads `'data-qa'` and `children`. Only when `data-qa === 'reactji'`, it renders `<Original {...props}>{children}<Reactors/></Original>`, appending up to `maxAvatars` 16px `img.taut-reactors__avatar` (`profile.image_24 ?? image_48`) and `span.taut-reactors__overflow` "+N" inside the pill.
- This is generic Slack: every unstyled button passes through, so Clack must route **every** native unstyled button through `UnstyledButton` with its `data-qa`, or at least the reaction pill with `data-qa="reactji"`.
- **Clack**: the reaction pill button in `(*row).reactions`, **msgs**.

#### ReactionAddButton
- **CopyReacted** P/CopyReacted.tsx:208-213. Props pass through. It renders `<>{Original}<CopyButton/></>`, where CopyButton is `button.c-button-unstyled.c-reaction_add.taut-copy-reacted[data-qa=taut_copy_reacted]` with `SvgIcon copy 18` inside `api.menu.Menu`.
- **Clack**: the add-reaction button in `(*row).reactions` (34px, visible on hover), **msgs**. It has to draw `.c-reaction_add` so a plugin's sibling element styled with that class matches.

#### MessageActions
- **OwlMode** P/OwlMode.tsx:411-424. Reads `ts, channelId` and adds a context wrap. The OwlMode comment notes that Slack has **two** components with this name: the hover toolbar, and another that wraps the whole message. OwlMode targets the toolbar.
- **Clack**: messages/list.go:`(*listState).toolbar`, **msgs**.

#### MessageActionButtonReaction
- **OwlMode** P/OwlMode.tsx:425-437. No props read. Inside an owl context it wraps Original in `<span class="taut-owl-action[ taut-owl-demo]">` (`display: contents`), so `.c-message_actions__button` gets recoloured.
- **Clack**: the "Add reaction…" button in `listState.toolbar`, **msgs**. It must carry `c-message_actions__button`.

#### EmojiPickerPopover
- **OwlMode** P/OwlMode.tsx:439-451. Reads `ts, threadTs, channelId`. Context wrap.
- **Clack**: popups/emoji_picker.go:`NewEmojiPicker`/`NewEmojiPickerAt` (via Host `OpenEmojiPicker`), **pop**. The overlay root must carry the target message's `ts/threadTs/channelId`. Today `OpenEmojiPicker(anchor, onPick)` drops them.

#### EmojiPicker
- **OwlMode** P/OwlMode.tsx:452-461. Inside an owl context it wraps Original in `div.taut-owl-picker` (`display: contents`). CSS then adds a box-shadow on `.p-emoji_picker`.
- **Clack**: popups/emoji_picker.go:`(*EmojiPicker).Layout`, **pop**. Its root must carry class `p-emoji_picker`.

### 1.4 Composer

Taut's own infra patches these at module load, so its patches sit innermost (app/slack/composer/buttons.tsx and sendChecks.tsx).

#### MessagePaneInput
- **sendChecks infra** app/slack/composer/sendChecks.tsx:59-89 (loop over `['MessagePaneInput','InputContainer']`). Reads `channelId, threadTs, prepareAndSendMessage(options: {delta, channelId, replyToTs})`. Overrides `prepareAndSendMessage` with a checked version: it converts the Quill delta to blocks, runs every `addSendCheck`, and on a false result throws so the draft stays put. Channel composers send through this component.
- **BetterTypingIndicator** P/BetterTypingIndicator.tsx:127-139. Reads `channelId, threadTs`. Context wrap.
- Users via sendChecks: HaikuWarning, OwlMode.
- **Clack**: composer/composer.go:`(*Composer).Layout` for a channel composer, **comp**. `prepareAndSendMessage` must be a real Go func crossing as `$gofn`. A thrown error or rejected promise must keep the draft. `delta` must be Quill-delta-shaped `{ops:[…]}`, which needs a Doc→delta adapter in composer/blocks.go.

#### InputContainer
- **sendChecks infra**: same as MessagePaneInput. Thread composers send through this component.
- **OwlMode** P/OwlMode.tsx:206-227. Reads `channelId, threadTs`. When the thread is an owl thread, it sets `dontShowBroadcastControls: true` and registers the thread in SharedStore `composers`.
- **SlimMessageBox** P/SlimMessageBox.tsx:113-131 (`broadcast === 'hidden'`). Forces `dontShowBroadcastControls: true` and wraps `prepareAndSendMessage` to pass `shouldBroadcast: false`.
- **Clack**: composer/composer.go:`(*Composer).Layout` for a thread composer, **comp**. `dontShowBroadcastControls` must hide `chrome.layoutBroadcast`.

#### MessageInput
- **buttons infra** app/slack/composer/buttons.tsx:154-176. Reads `broadcastControls?: ReactElement<{broadcast?: boolean, channelType?: 'im'|'mpim'|'channel'…, onBroadcastChange?(e: {target:{checked}})}> | false` and provides a broadcast context `{active, channelType, set}`.
- **OwlMode** P/OwlMode.tsx:229-250. Reads `channelId, threadTs` and the `enable*` flags. In owl threads it sets `enableFileButton, enableStoryButton, enableAudioButton, enableGifPicker, enableScheduleSendButton, enableSlashCommandsButton, enableMentionButton` all to false, plus a context wrap.
- **Clack**: composer/composer.go:`Composer`, with `chromeState` deciding which buttons draw, **comp**. Every `enable*` flag must map to a `crBtn`: File→crAttach, Story→crVideo, Audio→crAudio, GifPicker (emoji picker GIF tab), ScheduleSend→crSchedule, SlashCommands→crShortcut, Mention→crMention. `broadcastControls` must be an element (or `false`) whose props hold the "Also send to" checkbox state.

#### WysiwygContainer
- **OwlMode patch** P/OwlMode.tsx:253-266. Inside an owl context it overrides `className += ' taut-owl-composer[ taut-owl-demo]'`. CSS sets `--TextyTheme-wysiwyg-border-color`, `--TextyTheme-send-bg`, `--TextyTheme-send-bg-hover` and `border-color` on `:focus-within`.
- **richTextInput lazy** app/slack/richTextInput.tsx:24. Passes `numMinLines (3), errors?: [{id:'character_limit_reached', message}], enableEmojiButton, enableComposerButton, isDisabled, children`.
- **Clack**: composer/chrome.go:`(*chrome).Layout` (bordered box with focus ring, formatting bar, editor, attachments, footer), **comp**. It must carry `c-wysiwyg_container` and honour the `--TextyTheme-*` custom properties for the border and send button colours.

#### TextyButtons
- **buttons infra** buttons.tsx:178-185. Reads `channelId, threadTs, draftId ('edit-' prefix means edit), viewContext (Slack's MESSAGE_PANE / COMPOSER_VIEW / thread contexts), enableSubmitButton, collapseToolbarControls, textyRef: {current: {element}}`. Context wrap.
- **SlimMessageBox** P/SlimMessageBox.tsx:104-109. For each option set false, it sets `enableComposerButton` (formatting toggle), `enableMentionButton`, `enableStoryButton`, `enableAudioButton`, `enableSlashCommandsButton` and `enableShortcutsButton` to false. It always sets `minButtonsForOverflow: 1` (Slack's default is 5).
- **Clack**: composer/chrome.go:`(*chrome).layoutBar` + `layoutFooter` (the button set), **comp**. It must honour `enable*Button` and `minButtonsForOverflow` (the footer's ⋯ overflow), and expose `viewContext`, `draftId` and `textyRef`.

#### TextyButtonOverflow
- **buttons infra** buttons.tsx:234-280. Reads `children` (Slack's toolbar buttons). It renders `<Original>{[...slack, ...native plugin buttons, ...submit (the last Slack child when enableSubmitButton), ...end plugin buttons]}</Original>`. It skips when `collapseToolbarControls` is set or the channel `isNonExistent`. Each plugin button is `Tooltip(tip, position top, offsetY -7, delay 500, zIndex above_fs) > IconButtonBase(className c-wysiwyg_container__button, size smedium, tabIndex -1, aria-label, aria-pressed) > SvgIcon(icon, 18)`.
- Users via `composer.addButton`: OwlMode, SilentTyping, SlimMessageBox.
- **Clack**: composer/chrome.go:`(*chrome).layoutFooter` button row, **comp**. Its children must be a **list of elements** that plugins can append to, laid out in the footer before the send button. `mousedown` must not steal editor focus.

#### ShortcutsMenuTriggerButton
- **OwlMode** P/OwlMode.tsx:312-318. Reads `channelId, threadTs` and **returns null** in owl threads.
- **Clack**: the composer/chrome.go:`layoutFooter` `crShortcut` ("Run shortcut") button, **comp**. Slack's shortcuts menu itself is missing.

#### BaseContextBarV2
- **HaikuWarning** P/HaikuWarning.tsx:107-141 (when `warning !== 'popup'`). Reads `channelId, threadTs, draftId` (for `state.drafts.unifiedDrafts[draftId].ops`). Overrides `onIsContextBarShowing(showing)`. It renders `<>{Original}{shown && <div.p-context_bar_v2.p-context_bar_v2--yellow.taut-haiku-context-bar data-qa="context_bar" data-context-bar>[div.p-context_bar_v2__icon > MrkdwnElement ":orpheus-woah:", div.p-context_bar_v2__content text, div.p-context_bar_v2__filler]}</>`. Its CSS hides the original's `__filler` and squares the top corners.
- **OwlMode** P/OwlMode.tsx:268-310. Reads `channelId, threadTs, ariaId, onIsContextBarShowing` (called with true). In owl threads it **replaces** Original with `div.p-context_bar_v2.taut-owl-context-bar[data-qa=context_bar][data-context-bar]` containing `__icon` (SvgIcon glasses-filled), `__content` (`span#ariaId` "Replying anonymously via Owl"), `Tooltip(tip "Details", top, delay 500) > IconButtonBase(className taut-owl-context-bar__details, size small)` (SvgIcon info), and `__filler`.
- **Clack**: **missing**. It belongs between the message list and the composer box, inside the composer: conversation.go:`(*App).layoutConvo` currently stacks list + composer with nothing between. **comp** must add a context-bar slot that renders `BaseContextBarV2` (natively empty), with props `channelId, threadTs, draftId, ariaId, onIsContextBarShowing`, and that grows to fit plugin content. When `showing` is true, the composer's top corners must square off (`border-top-*-radius: 0` via `.p-context_bar_v2 + …`).

#### MessageInputRoadblock
- **RestrictedChannelWarning** P/RestrictedChannelWarning.tsx:61-87. Reads `channelId, roadblockMessage: ReactNode, allowWrap`. When an admin would be restricted as a non-admin, it sets `allowWrap: true` and `roadblockMessage: <span.taut-restricted-channel><span>{orig}</span><Button size small type outline>Post anyway</Button></span>`. The plugin also patches redux slices `readOnlyChannels` (`isReadOnly`) and `threadOnlyChannels` (`isThreadOnly`).
- **Clack**: **missing**. **comp** has to draw the roadblock instead of the editor when `readOnlyChannels[ch].isReadOnly` (or thread-only/archived), with `roadblockMessage` as a ReactNode slot.

#### ThreadTypingIndicator
- **BetterTypingIndicator** P/BetterTypingIndicator.tsx:140. Reads `channelId, threadTs`. Context wrap.
- **Clack**: composer/chrome.go:`(*chrome).layoutHint` (hint bar text under the box) for a thread, **comp**.

#### TypingNames
- **BetterTypingIndicator** P/BetterTypingIndicator.tsx:142-258. Reads `firstTyper?: ReactNode, secondTyper?: ReactNode, severalPeopleAreTyping?: boolean`. It computes typers from `state.typing` (`{channelId, threadTs, memberId, memberIsSelf}`) and names from `getPrefCompliantMemberName`. Then it renders `<span.taut-typing-indicator>` with a hidden `span.taut-typing-indicator__sample` ("typing **typing**") and `<Original firstTyper={MemberLinks joined ', '} secondTyper={last} severalPeopleAreTyping={false}/>`. Names may be truncated, and an overflow becomes `Tooltip(tip names) > span "N others"`. It measures with `ResizeObserver`, `getComputedStyle` (fontFamily, fontSize, letterSpacing, fontWeight) and `OffscreenCanvas.measureText`, and uses the `I18n('notification_bar')` and `I18n('today')` phrases.
- **Clack**: composer/chrome.go:`layoutHint` with composer/typing.go:`typingText` (a plain string), **comp**. It must become a component rendering `firstTyper`/`secondTyper` ReactNode slots in a "<b>A</b> and <b>B</b> are typing" template. The plugin also needs the DOM measurement APIs to work on that node: `clientWidth`, computed font and `ResizeObserver` (taut-ui).

#### texty_input_with_autocomplete
- **richTextInput** :25 (used by OfficeHours `ReplyEditor`). Passes `ref` (instance `getContents() → {contents: ops}`), `windowRef, id, initialText: {contents: ops}, placeholder, ariaLabel, onTextChange, isDisabled, disableTags, size: "medium", useWysiwyg, enterCreatesNewlineOverride, completeOnEmoji, completeOnMembers, completeOnChannels`.
- **Clack**: composer/editor.go:`Editor` + autocomplete.go:`Autocomplete`, **comp**. It must be renderable standalone (outside `Composer`) and take/return Quill delta ops.

#### CharacterCount
- **richTextInput** :26 (OfficeHours, maxLength 200). Passes `maxCharacterLimit, currentCount, type: "multiline", children`.
- **Clack**: **missing**, **comp**.

#### ComposerLinkModal
- **links infra** app/slack/composer/links.ts:256-275 (ClearURLs via `composer.addLinkTransform`). Not patched. It uses `waitForRenderedComponent('ComposerLinkModal')`, then `(modal.type ?? modal).prototype.saveLink({didTextChange, text, url})` is wrapped. It needs a **class component with a prototype method**.
- **Clack**: composer/linkdialog.go:`openLinkDialog`/`crLinkDlg`, **comp**. The shim must route its save through `saveLink({didTextChange, text, url})` so prototype patches apply.

### 1.5 Thread pane

#### Serialized(ThreadsFlexpane)
- **threads infra** app/slack/threads.tsx:63-136 (ThreadJump via `api.threads.usePane()`). Reads `channelId, threadTs, windowId ('main'), timestamps: string[] (loaded replies), highlightKey, ref → {scrollToTs(ts): boolean, listRef.current.getScrollmark(): {mark}}`. Context wrap plus a ref merge. `jumpToReply` dispatches Slack's `jumpToReply` action `{windowId, threadKey: 'C-ts', ts}`.
- **Clack**: messages/thread.go:`ThreadView` inside mount.go:`threadPane.Layout`, **msgs/shell**. It must expose a ref-like instance with `scrollToTs` and `getScrollmark`, plus the loaded `timestamps`.

#### ThreadSearchButton
- **ThreadJump** P/ThreadJump.tsx:45-50. Props pass through. It renders `<><JumpButton/>{Original}</>`. JumpButton (`button.c-button-unstyled.c-icon_button.c-icon_button--size_medium.c-icon_button--default.p-flexpane_header__control`, aria-haspopup dialog) shows only when the root has `reply_count ≥ 50`, and it opens `Popover` holding `div.c-date_picker__dropdown.taut-jump-panel` with Buttons and a `DatePickerTrigger`. The plugin forces experiment `find_in_thread` on so that Slack draws ThreadSearchButton.
- **Clack**: **missing**. **msgs** must add a header-controls row in messages/thread.go:`(*ThreadView).header` that renders a native `ThreadSearchButton` (search in thread) beside the close X, with the controls carrying `p-flexpane_header__control`.

#### ThreadFlexpaneAdminTools
- **SlackFixes** P/SlackFixes.tsx:52-66 (`botThreadAdminTools`). Reads `channelId, threadTs`. It wraps Original in `redux.StateView`, filling `messages[ch][ts].source_team_id` from `bot_profile.team_id`.
- **Clack**: **missing** (admin moderation tools in the thread pane), **msgs**. It can render nothing natively, but it must still exist as a component so the wrap is harmless.

### 1.6 Channel header, top nav, sidebar, mentions

#### BaseAvatarStack
- **HumanCount** P/HumanCount.tsx:24-45. Reads `channelId, memberCount: number`. It overrides `memberCount = membershipCounts[ch].counts.member_count − (excludeGuests ? restricted_member_count : 0)`, skipping IMs and MPIMs.
- **Clack**: channel.go:`(*App).layoutChannelHeader` (facepile + count, `hdr.members`), **shell**. The count shown must come from the `memberCount` prop.

#### HelpButton
- **StreamerMode** P/StreamerMode.tsx:319-345 (unless `alwaysOn`). Renders `<><div.p-top_nav__windows_controls_container.taut-streamer-mode__container><Tooltip tip={KeyboardKeysTooltip|label} position bottom delay 500><button.c-button-unstyled.p-top_nav__button.p-top_nav__help.taut-streamer-mode__button aria-pressed><SvgIcon eye-closed|eye-open 20/></button></Tooltip></div>{Original}</>`.
- **whatsNewButton** app/whatsNewButton.tsx:28-40. Renders `<><WhatsNewButton/>{Original}</>` (same container and button classes, `SvgIcon megaphone 20`, `span.taut-whats-new__dot`, Popover).
- **Clack**: chrome.go:`(*App).layoutTopNav` (`nav.help`), **shell**. The top nav needs a children row where siblings render *before* Help, styled with `p-top_nav__button`.

#### ListChannelEntity
- **PrivateChannel** P/PrivateChannel.tsx:664-686. Reads `id` and redux `channels[id] {name, is_private, is_member, isNonExistent, isUnknown}`. For a stub or a private non-member it **replaces** Original with `renderMissing(name)` = `menu.ContextMenu > button.c-button-unstyled.c-missing_channel--private > SvgIcon lock inline + name`.
- **Clack**: sidebar.go:`(*App).layoutSidebarRow` (`sbConv` rows), **shell**. Where else Slack uses ListChannelEntity (channel lists in pickers) is unclear. See Ambiguities.

#### BaseMrkdwnChannel
- **PrivateChannel** P/PrivateChannel.tsx:639-662. Reads `id, channelName, isPrivate, isMember, isNonExistent, isUnknown`. If the channel is synthesized or unknown, it **replaces** Original with `UnnamedChannel` (a ContextMenu around `span/button.c-missing_channel--private` + lock). If it is private and the user isn't a member, it renders `renderMissing`.
- **Clack**: richview/flow.go:`(*lay).addSpans`, the channel mention pill, **msgs**. Inline pills must be able to host a plugin subtree (an inline host-tree box in the text flow).

#### MemberLink
- **BetterTypingIndicator** P/BetterTypingIndicator.tsx:97, rendered at :219-225. Passes `id: string, className, children` (name text).
- **Clack**: richview/flow.go:`addSpans` user pill (opens the profile), **msgs**. It needs a standalone `MemberLink` host component usable outside richview, in the typing line.

### 1.7 Activity

#### ActivityItem
- **ShowRealUser** (loop, :185-201). Overrides `msg`.
- **StreamerMode** P/StreamerMode.tsx:232-244. Reads `activityItem.channelId, className`. For non-public channels it appends `taut-streamer-mode__activity taut-streamer-mode__activity--{dm|group|private}`.
- **Clack**: views/activity.go:`(*Activity).row`, **views**. It must carry `className` and `data-qa="activity-item-container"`, with the preview carrying `data-qa="activity-item-message"`.

#### ActivityRowContentSenderAvatarLayout
- **ShowSendingBot** P/ShowSendingBot.tsx:370-390. Reads `activityItem {channelId, messageTs}, size: 'small'|'large', children`. Replaces children with `<span.taut-sending-bot>{children}<Badge size={large?36:28 → badge} corner="bottom-left" interactive=false/></span>`.
- **Clack**: views/activity.go:`(*Activity).actAvatarOf`, **views**.

#### ActivityPrivacyBlurredName
- **StreamerMode** P/StreamerMode.tsx:246-259. Reads `activityItem.channelId, children`. For non-public channels it replaces children with `<span.taut-streamer-mode__activity-name>{children}</span>`, which CSS then blurs.
- **Clack**: the channel/conversation name inside views/activity.go:`(*Activity).rowContent`, **views**. It must be a separate component even though Clack has no privacy blur of its own.

### 1.8 Profile, presence, people menus

#### RimetoProfilePresence
- **LastSeen** P/LastSeen.tsx:87-94. Reads `member.id`. Context wrap (Surface `{userId}`). Its descendant `BasePresence` shows "Last seen …".
- **Clack**: popups/profile_view.go:`(*ProfileView).body`, the full-profile presence line, **pop**.

#### MemberProfileHoverCard
- **LastSeen** P/LastSeen.tsx:95-102. Reads `memberId`. Context wrap (Surface `{userId, card: true}`).
- **ShinigamiEyes** (disabled) P/ShinigamiEyes.disabled.tsx:108-246. Reads `memberId, header: ReactNode` and overrides `header = <>{header}{trust-level div + audit log list of MrkdwnElement}</>`.
- **Clack**: popups/profile_card.go:`ProfileCard` (`NewProfileCard`, `content`), **pop**. It must take a `header` ReactNode slot. Clack opens the card on click only; Slack's opens on hover.

#### BasePresence
- **LastSeen** P/LastSeen.tsx:104-123. Reads `showText, isActive, isSelf, className`. Inside a non-card Surface with showText, not active and not self, it renders `<>{Original showText=false}<span.padding_left_50.taut-last-seen aria-hidden>Last seen {ago}</span></>`.
- **Clack**: popups/util.go:`presenceDot` (the profile card and view name lines), sidebar.go:`(*App).presenceDot` and views/kit.go:`(*base).avatarPresence`, owned by **pop/shell/views**. Clack needs one host component with `showText` (dot + "Active"/"Away" text).

#### LocalTime
- **LastSeen** P/LastSeen.tsx:126-138. Reads `member.id`. Inside a card Surface it renders `<>{Original}<span.taut-last-seen__card title={full date}>Last seen {ago}</span></>`.
- **Clack**: popups/profile_card.go `pfIconLine` with `localTime(u, now)`, **pop**.

#### RimetoMemberProfileOverflowMenu
- **AdminBackend** P/AdminBackend.tsx:120-127 and **Nicknames** P/Nicknames.tsx:58-65. Both read `memberId` and add a context wrap, which marks the member-profile "⋮" menu subtree for the `MenuFromTemplate` patches below.
- **Clack**: popups/profile_card.go:`pfMoreItems` (the menu it opens) and profile_view.go:`pfPaneMenu`, **pop**. The ⋮ trigger and its menu must render as `RimetoMemberProfileOverflowMenu{memberId}` **containing** the `MenuFromTemplate`, so the context reaches the menu.

#### EditAudioButton
- **CustomNameRecording** P/CustomNameRecording.tsx:25-33. Reads `onChangeFile(fileId|null), onFileUploadStart(), onFileUploadEnd()`. Renders `<>{Original}<UploadButton/></>`: a `Button(outline, medium, className taut-name-recording__upload)` with `SvgIcon file-upload 18` + `span.margin_left_25`, plus an `InlineAlert` on error. It uploads through `files.upload` (thunk `addAndUploadPendingFile`) with `subtype: 'slack_name_pronunciation'`.
- **Clack**: **missing**. The edit-profile name-recording control would sit near popups/profile_view.go's "Add name pronunciation" row, **pop**.

### 1.9 Huddles (HuddleActivity only)

Clack has no huddle call window, event log, captions menu, or alert banners. views/huddles.go:`Huddles` is a list page, and the header "Huddle" button toasts "not supported". Every name below is **missing**. The owner is **views**, or ui-shell for the call window.

- **CaptionsSubmenu**, **CaptionsItemsMenu** P/HuddleActivity.tsx:190-198 (loop `for name of ['CaptionsSubmenu','CaptionsItemsMenu']`). No props read. StateView wrap restoring the real `huddleEvents.isSideBySideEnabled`.
- **HuddleMetaEvent** :251-259. Reads `event: {type, timestampMs, tautNote?}` and adds a `tautNote` prop. Plugin entries `{type:'JOIN', memberId, timestampMs, tautNote}` are merged into `state.huddleEvents.events`.
- **HuddleBaseEvent** :260-273. Reads `tautNote` (stripped before rendering) and `children`. With a note, it replaces the children with `<span.p-huddle_event_log__meta_text>` holding the note parts.
- **HuddlesAlertBannerBase** :276-299. Reads `bannerName` (`'informational_user_was_muted'`) and `children`, plus `state.alert.alerts[{type:'peerMuteRequest'}]`. Replaces the children with `<b><Name/> muted you.</b>`.
- It also dispatches and patches the thunk `enqueueHuddleToast` with `{content: ReactNode, svgIconProps: {name}, duration: 5000, huddleToastType}`, so the huddle toast must accept **ReactNode content**.

### 1.10 Menus

#### MenuFromTemplate (also `api.elements.MenuFromTemplate`, and inside every `api.menu.Menu`)
- **AccountSwitcher** P/AccountSwitcher.tsx:49-73. Reads `template`. When it contains key `'sign-out'` or `'signout-submenu'` (the avatar/profile menu), it inserts before it `{key:'taut-account-switcher', label:'Switch account', type:'submenu', template:[{key:'taut-account-switcher__<uid>', label: <AccountRow/> (ReactNode), click?}, {type:'separator'}, {key:'…__add', label:'Add another account', click}]}`.
- **AdminBackend** P/AdminBackend.tsx:129-157. Inside a RimetoMemberProfileOverflowMenu context it appends `{key:'taut-admin-backend__separator', type:'separator'}` + `{key:'taut-admin-backend__tool-<i>', label:'Open in <x>', click: window.open(url)}`.
- **Nicknames** P/Nicknames.tsx:67-92. Inside the same context it inserts `{key:'taut-nicknames__set', label:'Set nickname…'|'Edit nickname…', click}` right after the item whose **string label** starts with `'Copy display name'`.
- **Rendered by** `api.menu.Menu` (CopyReacted, Experiments) with `template` + MenuTrigger's `menuProps`.
- **Clack**: popups/menu.go:`Menu` / `MenuItem`, **pop**. Every native menu has to be built from a Slack-shaped template with Slack's stable `key`s. Those include `sign-out`/`signout-submenu` in `NewUserMenu`/`NewTeamMenu`, and `Copy display name …` (exact label prefix) in `pfMoreItems`. The template is passed through `MenuFromTemplate` and converted back to `[]MenuItem`. Labels and descriptions can be ReactNodes (`MenuItem.Widget` covers this). `type: 'radio'|'checkbox'` maps to `Checked`, `submenu` to `Sub`, and `header` to `Header`.

#### MenuTrigger
- `api.menu.Menu` (app/api/menu.tsx:29-40) renders `<MenuTrigger position renderMenu={(menuProps) => <MenuFromTemplate {...menuProps} template/>}>{children}</MenuTrigger>`. Users: CopyReacted :131,160-169 (`position: 'bottom'`, children = its button); Experiments :464,496-514 (`position: 'bottom-right'|'bottom-left'`, children = FilterPill or text button, radio templates). Props: `position?: 'top'|'bottom'|'left'|'right'|'top-left'|'top-right'|'bottom-left'|'bottom-right', isDisabled?, renderMenu(menuProps): ReactNode, children`.
- **Clack**: popups/menu.go:`NewMenuOpts` (Anchor, Side, AlignRight), opened on the child's click, **pop**. `renderMenu` is a **render-prop**: Clack calls a JS function to get the menu subtree.

#### withKeyboardFocusContext(ContextMenuTrigger)
- `api.menu.ContextMenu` (menu.tsx:43-53) renders it with `getTemplate: () => template, desktopAppOnly: false, showSlackKitContextMenuOnDesktop: true, children`. Users: PrivateChannel :567,578-586 (`template: [{key:'taut-private-channel__name', label, click}]`). Declared props also include `width?, menuAriaLabel?, isDisabled?`.
- **Clack**: popups/menu.go:`NewMenu` at the pointer, on secondary click of the child, **pop**.

### 1.11 Preferences

#### Tabs
- **settings** app/settings/index.tsx:87-166. Reads `tabs: {label: ReactElement, content: ReactElement, svgIcon: {name}, id?, 'aria-labelledby'?, 'aria-label'?}[], onTabChange?(id, e), currentTabId?, collapsible?`. It only acts when `tabs[last].id === 'advanced'` (the Preferences dialog). It appends `{id:'taut', label:<>Taut</>, content:<TautSettings/>, svgIcon:{name:'code'}}` plus one `taut-tab-N` per `preferences.addTab` (Experiments: label 'Experiments', icon 'magic-wand'). It overrides `currentTabId` and `onTabChange` (ours map to 'advanced'), and sets `collapsible: false`. Opening uses the thunk `openPrefsModal({activeSection: 'advanced'})`. Its CSS targets `.p-prefs_dialog__tabs`, `.p-prefs_dialog__menu`, `.p-prefs_dialog__panel`.
- **Clack**: prefs_modal.go:`prefsModal` (`layout`, `prefsNav`, `page`), **shell**. Its sections must be a Slack-shaped `tabs[]` list whose **last id is `advanced`** (Clack has no Advanced section yet), rendered through `Tabs`, with `content`/`label` as ReactNode slots. `popups.NewPreferencesModal` (with its fuller nav) is unused.

### 1.12 api.elements (generic Slack components, app/api/elements.ts:369-413)

All are `lazyComponent` (they render null until resolved, and props pass through). "Users" lists plugin file:lines.

| Element key → Slack name | Props (declared) | Plugin users (props passed) | Clack | Owner |
|---|---|---|---|---|
| SvgIcon → `SvgIcon` | `name, size?, inline?` | AccountSwitcher :182,259 (check-filled, trash-filled 16 inline); AdminBackend :334,379 (minus/edit/plus/reorder 16); CopyReacted :167 (copy 18); CustomNameRecording :64 (file-upload 18); Experiments :365,506,511 (close 20, caret-down 16); Nicknames :291 (close 16); OfficeHours :438,530 (close 16, emoji 20); OwlMode :288,304,538 (glasses-filled, info, glasses 16); PrivateChannel :559,583 (lock inline); StreamerMode :334 (eye-closed/eye-open 20); ThreadJump :92,250 (caret-up-down 20, calendar 20); composer buttons (any name 18; glasses, glasses-filled, notifications, notifications-off, megaphone, megaphone-filled); app (code, paintbrush, settings, info, caret-right, arrow-left, warning, undo, close, megaphone, plus, plug, sparkles, bug) | icons/`icons.Get` | shell. **Missing from the Clack registry** (checked against `icons/*.go`): arrow-left, bug, caret-up-down, check-filled, eye-closed, eye-open, file-upload, glasses, glasses-filled, magic-wand, megaphone, megaphone-filled, notifications, notifications-off, paintbrush, plug, reorder, sparkles, trash-filled, undo. Present: calendar, caret-down, caret-right, close, code, copy, edit, emoji, info, lock, minus, plus, settings, warning. An alias/fallback map is needed |
| Avatar → `ConnectedBaseAvatar` | see §1.1 | ShowSendingBot :66-74; Nicknames :248,266 | see §1.1 | msgs/pop |
| ProfileHoverTrigger → `ProfileHoverTrigger` | `memberId?, serviceId?, botProfile?, messageTs?, position?, noStyling?, children` | ShowSendingBot :84-90 (`serviceId, memberId, botProfile`, child Avatar) | Host `OpenProfile` (click) | pop. Needs hover-intent |
| MrkdwnElement → `MrkdwnElement` | `text: string` | CopyReacted :151 (`:name: (n)` as menu label); HaikuWarning :126,171; OfficeHours :528 (emoji); OwlMode :678 (code block); ShinigamiEyes :212; settings (many) | richview/view.go:`View` | msgs. Root classes `p-mrkdwn_element`, `p-rich_text_section` |
| Button → `Button` (`"c-button"`) | `type?: primary\|ghost\|outline\|danger, size?: small\|medium\|large, icon?, href?, htmlType?, …button attrs` | AdminBackend :327,489,492; CustomNameRecording :57; Experiments :403,408,435,618; Nicknames :284; OfficeHours :431,445 (ghost); OwlMode :531,544 (className p-block_kit_button_element); RestrictedChannelWarning :75; ThreadJump :208-253; settings | popups/style.go:`Button` (BtnOutline/Primary/Danger, Small/Medium) | pop. Missing: `ghost`, `large`, `href`, `htmlType=submit`, className passthrough, `c-button--icon` square mode |
| Tooltip → `Tooltip` (`"data-sk":"tooltip"`) | `tip: ReactNode, position?, offsetY?, delay?, zIndex?, children` | AdminBackend :326,371; BetterTypingIndicator :230; Nicknames :283; OfficeHours :421,430; OwlMode :297; StreamerMode :326 (tip = KeyboardKeysTooltip node); ThreadJump :81; composer buttons | popups/popover.go:`Tooltip` | pop. `tip` must accept a ReactNode |
| IconButtonBase → `IconButtonBase` | `size? ('smedium','small'), className?, aria-pressed?, aria-label?, data-qa?, onClick?, onMouseDown?, tabIndex?, children` | OwlMode :298; composer buttons | popups/style.go:`iconButton`; widgets.go:`iconButton` | pop |
| ConfirmationModal → `ConfirmationModal` | `title?: ReactNode, children (body), onSubmit?, onCancel?, onClose?, submitButtonText?, cancelButtonText?, submitButtonType?, showCancelButton?, showSubmitButton?, disableSubmitButton?` | via api.modal: AccountSwitcher, CopyReacted, Experiments, HaikuWarning, Nicknames, OwlMode, PrivateChannel, settings | popups/modal.go:`NewModal`/`Confirm` | pop. Title must accept a ReactNode (HaikuWarning passes MrkdwnElement) |
| InlineAlert → `InlineAlert` | `children, className?, id?` | CustomNameRecording :68 (`c-inline_alert--level_error`); Experiments :370; settings | **missing** | pop |
| Label → `Label` (`"c-label"`) | `text: ReactNode, htmlFor?, subtext?, optional?, type?: block\|inline, isDisabled?, className?, id?, children?` | AdminBackend :450; Nicknames :216 (`optional`); OwlMode :677,728; PrivateChannel :600; settings | **missing** | pop |
| FormTextInput → `FormTextInput` | `id?, name?, type?: text\|password, value, onChange(value: string), onBlur?, onFocus?, onKeyDown?, placeholder?, hintText?, errorText?, isDisabled?, isInvalid?, isRequired?, size?, autoFocus?, inputRef?, autoComplete?, maxCharacterLimit?, className?` | AdminBackend :451; Experiments :348,609; Nicknames :217,272; OwlMode :729 (password, errorText); PrivateChannel :601; settings | popups/style.go:`field` | pop. Needs hint/error lines, password, `inputRef.focus()` |
| Checkbox → `Checkbox` | `checked?, onChange?(event: e.target.checked), indeterminate?, disabled?, id?, className?, aria-label?, aria-describedby?` | settings (plugin enable, boolean options) | **missing** as a component | pop |
| FilterPill → `FilterPill` | `isActive?, onClick?, isDisabled?, className?, id?, aria-pressed?, children` | Experiments :386,509; OfficeHours :396 (`taut-schedule__day`); settings | popups/search_view.go:`pill`; views/kit.go:`pillRow` | pop. Inner `.c-filter-pill__content` |
| Tag → `Tag` | `style? ('informative'), isMicro?, children` | settings changelog | **missing** | pop |
| DateRangePicker → `DateRangePicker` | 27 props (elements.ts:99-133) | none | popups/datetime.go:`dtDate` | pop |
| DatePickerTrigger → `DatePickerTrigger` | `onDateChange(date 'YYYY-MM-DD'), selectedDate?, dateFormat?, disabledDates?, disableDatesBefore?, disableDatesAfter?, children: one element` | ThreadJump :243-254 | popups/datetime.go:`dtCalendar` | pop. Must open from any child element |
| FieldSet → `FieldSet` | `id?, data-qa?, data-qa-section?, children` | settings; plugin dataPanel hosts | **missing** | pop |
| Legend → `Legend` (`"c-legend"`) | `className?, children` | settings | **missing** | pop |
| Hint → `Hint` | `children, className?` | none | **missing** | pop |
| BasicSelect → `BasicSelect` | `selectId, options: {label: ReactNode, value}[], selectedOption?, onSelectionChange(option), width?, size?, ariaLabel?, selectDataQa?, isDisabled?` | Experiments :582-601 (width 180, small); settings (width "100%") | views/directories.go:`dirDropdown` | views (should move to pop) |
| Blocks → `Blocks` | see §1.2 | none render it directly | richview | msgs |
| MenuTrigger / MenuFromTemplate / ContextMenuTrigger | see §1.10 | | popups/menu.go | pop |
| TimePicker → `TimePicker` | `value? 'HH:MM', onChange?(time), ariaLabel?, optionsHourIncrement?: five_minute\|ten_minute\|quarter\|half\|full, size?, width?, showTimeZone?, isDisabled?, isRequired?, min?, max?` | OfficeHours :373-384 (half, small, 116, showTimeZone false, isRequired) | popups/datetime.go:`dtTime` + `dtTimeList` | pop |
| EmojiMenuTrigger → `Connect(EmojiMenuTrigger)` | `onEmojiSelected(emoji, name), position?: top\|bottom\|top-left, offsetY?, children: one element` | OfficeHours :517-533 (bottom, 5) | Host `OpenEmojiPicker` | pop |
| Popover → `Popover` (`"c-popover"`) | `isOpen, targetBounds: DOMRect, windowRef: WeakRef<Window>, position?, offsetX?, offsetY?, onClose, ariaRole?, ariaLabel?, children` | ThreadJump :96-111 (bottom-right, 4, dialog); settings color picker; whatsNew | popups/popover.go:`NewPopover` | pop. `targetBounds` arrives in window CSS px |
| RichTextInput (taut) | `value: Block[], onChange(blocks, length), onBlur?, placeholder?, ariaLabel?, id?, maxLength?, numMinLines?, disabled?` | OfficeHours :621-636 | composer Editor standalone | comp |

#### KeyboardKeysTooltip (lazy, StreamerMode :295-314)
- Passes `title: string, modifiers?: string[] (['⌘'|'Ctrl', 'Option'|'Alt', 'Shift']), mainKey: string`. It is used as a `Tooltip.tip` ReactNode.
- **Clack**: popups/popover.go:`Tooltip(…, label, shortcut)` + style.go:`kbd`, **pop**. It needs a standalone host component.

---

## 2. CSS and DOM surface

Taut has **no `.css` files**. All CSS is template strings passed to `api.setStyle(css, key?)`, which becomes `<style data-taut-style="plugin:<id>:<key>">` in `document.head` of the main window and every child window (app/api/css.ts:40-60). User CSS is `user.css` under key `'user'` (bootstrap.ts:64). The example user CSS (`shared/userCss.ts:1`) is just a comment. `/tmp/taut/docs`, `README.md`, `AGENTS.md`, `CHANGELOG.md` and `assets/` contain **no** Slack class names. Taut's docs say: UI must be React patches, no `MutationObserver` with `subtree:true`, reuse Slack classes and tokens, and name injected classes `taut-<thing>` in BEM style.

### 2.1 Per plugin

**AccountSwitcher**: no classes. Inline styles on its own spans/img/button (flex, 20×20, `border-radius: 4px`, ellipsis, `color`/`background` from `var(--sk_error, #e01e5a)`, `rgba(var(--sk_foreground_low_solid,221,221,221),0.1)`, `transition`). DOM: `localStorage` (`taut:pendingSwitch`, `localConfig_v2`), `location.assign/reload`, `window.open`.

**AdminBackend**: setStyle :174-247, own classes `.taut-tool-list*` (`border: 1px solid var(--dt_color-otl-sec)`, `border-radius: 8px`, `background: var(--dt_color-base-pry)`, grid/flex, `cursor: grab`, `touch-action: none`). Slack classes: `.taut-tool-list__form .c-input_text {margin-bottom: 0}`, and on its own elements `c-button-unstyled`, `c-button--icon`, `offscreen` (aria-live). DOM: `children[].getBoundingClientRect()`, `window.addEventListener('pointermove'|'pointerup'|'pointercancel')`, `querySelectorAll('.taut-tool-list__handle')[i].focus()`.

**BetterTypingIndicator**: setStyle :107-125: `.taut-typing-indicator {display:block; flex:1 1 auto; min-width:0; overflow:hidden; text-overflow:ellipsis; white-space:nowrap}`, `__sample {position:absolute; visibility:hidden; pointer-events:none}`, `__member {color:inherit; letter-spacing:inherit}`. DOM: `getComputedStyle`, `getBoundingClientRect`, `ResizeObserver`, `OffscreenCanvas` 2d `font`/`letterSpacing`/`measureText`, `document.fonts.check`.

**BrowserPopouts**: patches `URLSearchParams.prototype.get('windowing')`.

**ClickToLoad**: setStyle :76-88: `.taut-click-to-load .c-message_attachment__video_thumb {min-height:72px; background-color: rgba(var(--sk_foreground_min_solid,248,248,248),1)}`, `.taut-click-to-load__label {display:block; padding-bottom:8px; color: var(--dt_color-constants-white); font-size:13px; font-weight:700}`. Its own markup uses Slack classes (§1.2 AttachmentMedia) and `data-stringify-ignore`.

**CopyReacted**: setStyle :174-189: `.taut-copy-reacted {color: var(--dt_color-content-pry)}`, `.sk-client-theme--dark .taut-copy-reacted {color: var(--dt_color-content-ter)}`, `… :is(:hover,:focus) {color: var(--dt_color-content-pry)}`. Own button classes: `c-button-unstyled c-reaction_add taut-copy-reacted`, `data-qa="taut_copy_reacted"`. DOM: `navigator.clipboard.writeText`.

**CustomNameRecording**: setStyle :35-38: `.taut-name-recording__upload {margin-left:8px}`, `.taut-name-recording__error {margin-top:4px}`. Slack classes `margin_left_25`, `c-inline_alert--level_error`. DOM: `document.createElement('input')` type=file accept `audio/*` with `style.display='none'`, `document.body.appendChild`, `.click()`, `change`/`cancel` events, `AudioContext`, `MediaRecorder`.

**Experiments**: setStyle :62-146: `.p-prefs_dialog__panel:has(.taut-experiments) {scrollbar-gutter: stable}`; `.taut-experiments .c-input_text {margin:0}`; `.taut-experiments__search .c-input_text {padding-right:36px}`; `.taut-experiments__menus .c-icon {margin-left:2px}`; plus own `.taut-experiments__*` classes (flex/grid layouts, `border-top: 1px solid var(--dt_color-otl-ter)`, `box-shadow: inset 3px 0 0 var(--dt_color-content-hig)`, monospace 12px, `overflow-wrap:anywhere`, `position:absolute; top:50%; transform:translateY(-50%)`). Own markup: `c-button-unstyled`, `c-inline_alert--level_error`, `<wbr/>`. DOM: `requestAnimationFrame(() => el.focus({preventScroll:true}))`, `location.reload()`.

**FasterSlack**: setStyle key `top-nav` :27-34: `.taut-resizing .p-ia4_top_nav__left_container {flex-basis: var(--taut-top-nav-left-basis) !important}`. DOM: `document.querySelector('.p-ia4_top_nav__left_container').style.flexBasis` (read), `document.documentElement.classList.add/remove('taut-resizing')`, `documentElement.style.setProperty('--taut-top-nav-left-basis', 'calc(…)')`. `api.deferResizeWork` gates `window` `resize` events (capture) and re-dispatches `new UIEvent('resize')`.

**HaikuWarning**: setStyle :89-105: `.p-context_bar_v2:has(+ .taut-haiku-context-bar) > .p-context_bar_v2__filler {display:none}`; `.p-context_bar_v2 + .taut-haiku-context-bar {border-top-left-radius:0; border-top-right-radius:0}`; `.taut-haiku-context-bar .p-context_bar_v2__icon .c-emoji, … .c-emoji img {display:block; width:16px; height:16px; margin:0; top:0}`. Own markup with `p-context_bar_v2`, `p-context_bar_v2--yellow`, `__icon`, `__content`, `__filler`, `data-qa="context_bar"`, `data-context-bar`. Modal body inline styles `marginTop:0`, `whiteSpace:'pre-line'`, `marginBottom:0`.

**HuddleActivity**: own `<span class="p-huddle_event_log__meta_text">`, plus `<b>`/`<strong>`.

**IdvStatus**: setStyle :88-100: `.taut-idv-status--not-eligible, .taut-idv-status--not-eligible .c-message__sender_button {text-decoration: underline wavy <color> !important; text-decoration-thickness: 1px !important}`, and the same for `--over-18`.

**LastSeen**: setStyle :140-147: `.taut-last-seen__card {display:block; margin-top:2px; font-size:13px; color: rgba(var(--sk_foreground_high_solid,134,134,134),1)}`. Own markup `padding_left_50 taut-last-seen`.

**Nicknames**: setStyle :112-166: `.taut-nicknames__*` (flex column, gap, ellipsis, `font-weight:700`, `color: var(--dt_color-content-pry|sec)`); `.taut-nicknames__field .c-input_text {margin-bottom:0}`; `.taut-nicknames__remove.c-button--icon {width:36px; height:36px}`. DOM: legacy `localStorage.getItem('taut_nicknames')`.

**NoTrack**: replaces `window.fetch`, `XMLHttpRequest.prototype.open/send` (fakes DONE, dispatches `readystatechange`/`error`/`loadend`) and `navigator.sendBeacon`. Patches webpack exports `getGenericTracer`, `getGenericTelemeter`, `getNoopTelemeter`.

**OfficeHours**: setStyle :317-362: `.taut-schedule*` (flex, `padding:12px`, `border-radius:8px`, `background: var(--dt_color-ctr-sec)`, 24×28 day pills); `.taut-schedule__day .c-filter-pill__content {width:100%; justify-content:center}`. setStyle :486-505: `.taut-status__text {flex:1; height:38px; padding:0 12px 0 0; border:0; outline:0; background:none; color:inherit; font:inherit}`, `::placeholder {color: var(--dt_color-content-ter)}`, `.taut-status__emoji > * {pointer-events:none}`. Own markup reuses `offscreen`, `c-button--icon`, `c-basic_container c-basic_container--bordered c-basic_container--prefixed p-custom_status_modal__input`, `c-basic_container__body`, `c-button-unstyled p-custom_status_modal__input_action`. DOM: `navigator.locks.request`.

**Oneko**: `document.createElement('div')` `#oneko` with `aria-hidden`, inline `width:32px; height:32px; position:fixed; pointer-events:none; image-rendering:pixelated; z-index:2147483647; background-image:url(https://taut.jer.app/oneko.webp)`. Per 100 ms it sets `style.left/top` and `backgroundPosition`. `document.body.appendChild`, `document.addEventListener('mousemove')` (`clientX/Y`), `window.addEventListener('beforeunload')`, `window.innerWidth/innerHeight`, `localStorage['oneko']`.

**OwlMode**: setStyle :102-174: `body {--taut-owl: 26,73,214}`, `body.sk-client-theme--dark {--taut-owl: 120,150,255}`, `.taut-owl-demo`/`body.sk-client-theme--dark .taut-owl-demo` (variable override); `.c-wysiwyg_container.taut-owl-composer {--TextyTheme-wysiwyg-border-color; --TextyTheme-send-bg; --TextyTheme-send-bg-hover}` and `:focus-within {border-color}`; `.p-context_bar_v2.taut-owl-context-bar {background: rgba(var(--taut-owl),0.1)}`; `.taut-owl-context-bar .p-context_bar_v2__icon {color}`; `.taut-owl-context-bar__details {margin:-4px -4px -4px 8px}`; `.c-reaction_bar.taut-owl-reactions .c-reaction, … [data-qa="add_reaction_button"] {box-shadow: 0 0 0 1px …}` + `:hover {background-color}`; `… .c-reaction--reacted (+:hover) {background-color; color: var(--dt_color-content-pry)}`; `.taut-owl-action, .taut-owl-picker {display:contents}`; `.taut-owl-action .c-message_actions__button {color}` + `:hover {background}`; `.taut-owl-picker .p-emoji_picker {box-shadow: 0 0 0 1px …, 0 4px 12px 0 #0000001f}`; `.taut-owl-save__button {display:flex; align-items:center; gap:6px}`; `.taut-owl-save__button.c-button--outline {border-color; color}`; `.taut-owl-save__button.c-button--disabled {border-color; color}`. Own markup: `p-context_bar_v2*`, `p-actions_block`, `p-actions_block_elements`, `p-actions_block__action`, `p-block_kit_button_element`, `data-stringify-ignore`.

**PrivateChannel**: own markup `c-button-unstyled c-missing_channel--private`, `span.c-missing_channel--private`.

**RestrictedChannelWarning**: setStyle :88-96: `.taut-restricted-channel {display:inline-flex; flex-wrap:wrap; align-items:center; justify-content:center; gap:4px 8px}`.

**ShinigamiEyes** (disabled): setStyle :248-262, a loop generating `.taut-trust-level--N::before {content: "<emoji> "}` for N = 0..4. Inline styles use `var(--sk_foreground_low)` and `var(--sk_primary_foreground)`.

**ShowSendingBot**: setStyle :267-331: `.taut-sending-bot {position:relative; display:flex; flex-shrink:0; align-self:flex-start}`; `.taut-sending-bot__badge {position:absolute; right:-3px; bottom:-3px; display:flex; align-items:center; justify-content:center; padding:1px; border-radius:4px; background-color: light-dark(var(--dt_color-base-sec), var(--dt_color-base-ter))}`; `--bottom-left {left:-3px; right:auto}`; `.c-message_kit__gutter__left {position:relative}`; `--gutter {right:calc(100% - 2px); bottom:auto; left:auto; top:8px; translate:0 -50%}`; `.p-thread_compact_gutter_generic + .taut-sending-bot__badge--gutter {right:calc(100% - 8px)}`; `--inline, .c-message_kit__background--hovered .taut-sending-bot__badge--inline {position:static; display:inline-flex; vertical-align:text-bottom; margin-left:4px; padding:0; background-color:transparent}`; `.taut-sending-bot__badge .c-base_icon {vertical-align:top}`; `.c-message_kit__background--hovered .taut-sending-bot__badge {background-color: rgba(var(--sk_foreground_min_solid,248,248,248),1)}`. It matches classNames in JS: `c-message_kit__avatar`, `c-search_message__avatar`, `c-message_attachment__author_icon`, `p-message_pane_message__compact_timestamp`.

**SlackFixes**: setStyle :26-33 (`halloweenSidebarRow`): `[data-qa="spooky_sidebar_candy_icon"] {width:16px; padding-right:0; justify-content:center}`, `[class*="spookySidebarLinkContainer__"] {border-radius:6px}`. setStyle :37-42 (`zalgoText`): `.p-rich_text_block, .p-section_block__text {overflow:clip; overflow-clip-margin:4px}`. StateView infra uses `document.querySelector('.p-client_container').firstElementChild`, then walks up the fibers.

**SlimMessageBox**: SCOPE `.p-message_input__input_container_unstyled`. ONE_LINE = `SCOPE:not(.c-wysiwyg_container--with_input_overlay):not(:has(.c-wysiwyg_container__attachments, .p-message_input__attachments, .c-pending_files, .c-message__editor__composer_attachments))`.
- Always: `SCOPE .c-wysiwyg_container__footer_divider {display:none !important}`.
- If the emoji button is hidden: `.c-texty_buttons .c-wysiwyg_container__button--emoji {position:absolute !important; visibility:hidden !important}`.
- If broadcast is `button`: `.p-threads_footer__input_container {min-height:0}`, `.p-threads_footer__input_container__broadcast_controls {display:none !important}`.
- One-line layout: `SCOPE {container: taut-composer / inline-size}`; `ONE_LINE .c-basic_container__body {display:flex !important; flex-direction:row !important; flex-wrap:wrap; align-items:flex-end !important; column-gap:6px}`; `… > * {order:0; flex:1 0 100%}`; `… > :empty {display:none}`; `.c-wysiwyg_container__formatting {order:1}`; `.c-wysiwyg_container__message_suggestions {order:2}`; `.c-texty_input_unstyled__container {order:3; flex:100 1 0% !important; min-width:min(450px,100%)}`; `.c-wysiwyg_container__draft {order:4}`; `.p-threads_footer__input_container__broadcast_controls {order:5; flex:1 0 100%}`; `.c-wysiwyg_container__footer {display:flex !important; order:6; flex:1 0 100% !important; flex-wrap:wrap; min-width:0}`; `.c-wysiwyg_container__suffix {margin-left:auto !important; flex:0 0 auto !important}`.
- `@container taut-composer (min-width: <790 − 32·hidden>px)`: `.c-wysiwyg_container__footer {flex:1 1 auto !important}`; with the broadcast checkbox, `ONE_LINE:has(BROADCAST) .c-texty_input_unstyled__container {min-width:0 !important}` and `ONE_LINE BROADCAST {order:7; padding-bottom:8px}`; `.c-wysiwyg_container__toolbar_buttons {flex:0 1 auto !important; min-width:0 !important}`; `.c-texty_buttons {display:contents !important}`.
- DOM: `localStorage['taut_slim_message_box_formatting']`.

**StreamerMode**: setStyle key `streamer`, all rules prefixed `html.taut-streamer-mode`.
- Blur rule `{filter: blur(<blur>px); transition: filter 0.15s}` applies to:
  - `.p-channel_sidebar__channel:is([data-qa-channel-sidebar-channel-type="private"]):not([data-qa-channel-sidebar-channel-is-selected="true"]) .p-channel_sidebar__name` (and the same with `="mpim"`);
  - `.c-inline_channel_entity:has([data-inline-channel-type-icon^="lock"]):not([data-qa="activity-item-container"] *) .c-channel_entity__name`;
  - `.taut-streamer-mode__activity--{private|group} .taut-streamer-mode__activity-name`;
  - `.c-search_autocomplete__suggestion_item:is(:has(.c-channel_icon svg[data-qa^="lock"])) .c-search_autocomplete__suggestion_item_left` (and the same with `[data-type="mpim"]`);
  - `[data-qa="dms_channel"]:not(:has(.p-activity_ia4_page__item--selected)):has(.c-base_icon_image_stacked) [data-qa="dms-channel-sender-name"]`;
  - `.taut-streamer-mode__activity [data-qa="activity-item-message"]` and `[data-qa="dms_channel"]:not(…) [data-qa="activity-item-message"]`;
  - `.p-threads_view .taut-streamer-mode__private-thread :is(.c-message_kit__gutter__left, .c-message_kit__gutter__right)`.
- Reveal rule `{filter:none}` applies to:
  - `.p-channel_sidebar:is(:hover, :has(:focus-visible)) <name>` (mode `list`);
  - `.p-channel_sidebar__static_list__item:is(:hover,:focus-visible) <name>`, sibling ranges `~` up to `[data-item-key^="sectionHeading-"]` (mode `section`), or the item itself (mode `channel`);
  - `.c-inline_channel_entity:hover .c-channel_entity__name`;
  - `[data-qa="activity-item-container"]:hover :is([data-qa="activity-item-message"], .taut-streamer-mode__activity-name, [data-qa="dms-channel-sender-name"])` and the same for `dms_channel`;
  - `.taut-streamer-mode__revealed …`;
  - `.c-search_autocomplete__suggestion_item:hover …`.
- Hide rule: `[data-qa="priority_vip_badge"], svg[data-qa="vip"], svg[data-qa="vip-filled"] {display:none}`.
- Own classes: `.taut-streamer-mode__container {margin-right:8px}`, `.taut-streamer-mode__button[aria-pressed="true"] {color: rgba(var(--sk_raspberry_red,224,30,90),1)}`.
- DOM: `document.documentElement.classList` (`taut-streamer-mode`); `window.addEventListener('keydown', …, true)` (`event.code` `KeyX`/`DigitN`, `metaKey`/`ctrlKey`/`altKey`/`shiftKey`); `window.addEventListener('pointerover'|'focusin', …, true)`; `document.querySelector('.p-threads_view')`; `target.closest('.c-virtual_list__item')`; `view.querySelectorAll('.c-virtual_list__item')` toggling `taut-streamer-mode__revealed` by `data-item-key` (strip `^(heading|root|footer)-`, then `<channel>-<ts>`); `navigator.platform`; monkeypatches `navigator.mediaDevices.getDisplayMedia/getUserMedia` (`constraints.video.mandatory.chromeMediaSource === 'desktop'`, track `ended`, stream `inactive`); `localStorage['taut_streamer_mode_active']`.

**ThreadJump**: setStyle :52-65: `.taut-jump-panel {display:grid; grid-template-columns:1fr 1fr; gap:8px; width:300px}`, `.taut-jump-panel__date {grid-column:1 / -1}`, `.taut-jump-panel__date > .c-button {width:100%}`. Own markup: `c-button-unstyled c-icon_button c-icon_button--size_medium c-icon_button--default p-flexpane_header__control`, `c-date_picker__dropdown`, `margin_right_50`. DOM: `getBoundingClientRect()`, `new WeakRef(window)`.

**UserPronouns**: setStyle :57-79: `.taut-pronouns {margin-left:6px; font-size:12px; font-weight:400; color: rgba(var(--sk_foreground_max_solid,97,96,97),1); white-space:nowrap; cursor:default; user-select:none; align-self:center}`, `--bulleted {margin-left:0}`, `--bulleted::before {content:'\2022'; margin:0 4px}`. Own `span[data-stringify-ignore]`.

**WhoReacted**: setStyle :60-90: `.taut-reactors {display:inline-flex; align-items:center; margin-left:4px}`, `__avatar {width:16px; height:16px; margin-left:-5px; border-radius:50%; object-fit:cover; border:1px solid var(--dt_color-surf-pry); background-color: var(--dt_color-surf-pry)}`, `__avatar:first-child {margin-left:0}`, `__overflow {margin-left:3px; font-size:11px; font-weight:700; line-height:1}`. It matches `data-qa="reactji"` in JS. Its `<img onError>` hides itself.

**No CSS or DOM at all**: AnonymizeFilenames (only `window.crypto`, `new File`), BringBackSlackbot, Censorship, ClearURLs, InvisibleForward, NativeAtChannel, QuickJoin, RealMarkdown, ShowRealUser, ShutUpSlackbot, SilentTyping (`localStorage.removeItem` only), HumanCount.

**app/** (framework):
- Settings CSS key `settings-ui`: `.taut-settings*`, `.taut-changes*`, `.taut-updates*`, `.taut-changelog__release`; Slack `.p-prefs_dialog__tabs {min-height:0}`, `.p-prefs_dialog__menu {min-height:0; overflow-y:auto}`, `.p-prefs_dialog__menu > * {flex-shrink:0}`, `.taut-settings__row .c-input_checkbox {flex:none; margin:0 0 0 4px}`, `.taut-settings__search/field .c-input_text {margin-bottom:0}`, `.taut-settings__field .c-input_textarea {margin-bottom:0}`, `.taut-settings__field .c-label {margin-bottom:4px}`, `.taut-changes__body .p-rich_text_section, .taut-changes__body .p-mrkdwn_element {display:inline}`.
- Own markup reuses `c-button-unstyled`, `c-link--button`, `c-link`, `c-input_textarea`, `c-sv_picker__gradient_box`, `c-sv_picker__thumb`, `c-color_slider`, `c-color_slider__outer_hue_track`, `c-color_slider__inner_hue_track`, `c-slider__input c-color_slider__slider`, `p-top_nav__windows_controls_container`, `p-top_nav__button`, `p-top_nav__help`.
- whatsNew CSS key `whats-new` (`.taut-whats-new*`).
- `.c-wysiwyg_container__button` on composer buttons.
- DOM: `document.querySelector('.taut-settings')` and `('.p-client_container')`; capture-phase `document` `focusin`, then `target.closest('.ql-container').__quill.getModule('clipboard'|'slackmarkdown')` (composer/links.ts:236, inlineMarkup.ts:356); a `MutationObserver` on `document.body` with `attributeFilter: ['class','style']` (cdn.ts:70, Monaco theme), which reads `getComputedStyle(body).colorScheme` and `--sk_primary_background`; `__reactFiber$*` keys; `<input type=file>`; `CSS.supports('color', …)`; canvas color conversion; `#taut-bridge`, `#taut-app`.

### 2.2 Slack CSS custom properties referenced

`--dt_color-content-pry`, `-sec`, `-ter`, `-hig`, `-imp`, `-hgl-1`; `--dt_color-otl-pry`, `-sec`, `-ter`, `-hgl-1`; `--dt_color-base-pry`, `-sec`, `-ter`; `--dt_color-surf-pry`, `-sec`, `-ter`; `--dt_color-ctr-sec`; `--dt_color-constants-white`; `--sk_foreground_min`, `_min_solid`, `_low`, `_low_solid`, `_high_solid`, `_max_solid`; `--sk_primary_foreground`, `--sk_primary_background`; `--sk_highlight`; `--sk_raspberry_red`; `--sk_error`; `--sk_input_styles-radius`; `--sk_focused-shadow-color`; `--TextyTheme-wysiwyg-border-color`, `--TextyTheme-send-bg`, `--TextyTheme-send-bg-hover` (consumed by the composer). The theme class `sk-client-theme--dark` goes on `body`. All of these must come from theme/palette.go via `css.SlackVars`, and `color-scheme` must be set so `light-dark()` resolves.

### 2.3 Deduplicated: Slack class / attribute → Clack native widget → properties to honour

"Also via markup" means plugins create their own elements with this class and expect Slack's stylesheet to style them. Clack's base stylesheet must define that class even where no native widget carries it.

| Slack class / attribute | Carried by (Clack) | Owner | Properties / behaviour that must be honoured |
|---|---|---|---|
| `html` root (+ `taut-streamer-mode`, `taut-resizing` classes, inline custom properties) | slackc document root | taut-ui | `classList` mutation, `style.setProperty` of custom properties, ancestor matching for every rule |
| `body`, `body.sk-client-theme--dark` | slackc document root | taut-ui | custom-property definitions (`--taut-owl`), dark-mode class |
| `.p-client_container` | app root element | shell/taut-ui | must exist with `firstElementChild` (StateView fiber walk) |
| `.p-ia4_top_nav__left_container` | chrome.go:`layoutTopNav` left section | shell | `flex-basis` (with `!important`, `var()`, `calc()`); `style.flexBasis` readable |
| `.p-top_nav__windows_controls_container` | top-nav container around Help (also via markup) | shell | `margin-right` |
| `.p-top_nav__button`, `.p-top_nav__help` | chrome.go `nav.help` button (also via markup) | shell | full button look (size, color, hover); `[aria-pressed="true"]` → `color` |
| `.p-channel_sidebar` | sidebar.go:`layoutSidebar` | shell | `:hover`, `:has(:focus-visible)` ancestor for descendant rules |
| `.p-channel_sidebar__channel` + `data-qa-channel-sidebar-channel-type` (`private`/`mpim`/…), `data-qa-channel-sidebar-channel-is-selected` | sidebar.go:`layoutSidebarRow` (`sbConv`) | shell | attribute selectors |
| `.p-channel_sidebar__name` | sidebar row name label | shell | `filter: blur(Npx)`, `transition: filter .15s`, `filter: none` |
| `.p-channel_sidebar__static_list__item` + `data-item-key` (`sectionHeading-…`) | every sidebar row (`sbRow`) | shell | `:hover`, `:focus-visible`, general sibling `~`, `:has(~ …)` |
| `[data-qa="spooky_sidebar_candy_icon"]`, `[class*="spookySidebarLinkContainer__"]` | sidebar top-link rows (seasonal, Slack only) | shell | `width`, `padding-right`, `justify-content`, `border-radius`; substring attribute match on class |
| `[data-qa="priority_vip_badge"]`, `svg[data-qa="vip"]`, `svg[data-qa="vip-filled"]` | VIP badges/icons (profile card `pfBtn` VIP, sidebar/DM rows if added) | pop/shell | `display: none` |
| `.c-inline_channel_entity` + `[data-inline-channel-type-icon^="lock"]`, `.c-channel_entity__name` | richview/flow.go channel mention pill | msgs | `filter: blur`, `:hover` reveal, `:has()` on the icon attribute |
| `.c-missing_channel--private` | richview channel pill for unknown/private channels (also via markup) | msgs | Slack's look (lock icon + muted name) |
| `.p-threads_view` | views/threads.go:`Threads` root | views | `querySelector` target; ancestor for blur rules |
| `.c-virtual_list__item` + `data-item-key` (`heading-`/`root-`/`footer-` + `<ch>-<ts>`) | each virtual-list item (messages/list.go items, views lists) | msgs/views | `closest()`, `querySelectorAll()`, plugin `classList.toggle` on native nodes |
| `.c-message_kit__background--hovered` | messages/row.go background in hover state | msgs | state class for descendant rules (`background-color` on children) |
| `.c-message_kit__gutter__left` | row.go avatar/time gutter | msgs | `position: relative` (abs-positioned badge children); `filter: blur` |
| `.c-message_kit__gutter__right` | row.go body column | msgs | `filter: blur` |
| `.c-message_kit__avatar` | row avatar (className prop of ConnectedBaseAvatar) | msgs | className match in JS |
| `.p-message_pane_message__compact_timestamp` | grouped-row hover time element (channel) | msgs | className match in JS |
| `.p-thread_compact_gutter_generic` | grouped-row hover time element (thread) | msgs | adjacent-sibling `+` |
| `.c-message__sender_button` | row.go `nameBtn` | msgs | `text-decoration: underline wavy <color>`, `text-decoration-thickness`, `!important` |
| `.c-base_icon` | every SvgIcon root | shell | `vertical-align` |
| `.c-icon` | every SvgIcon root (legacy name) | shell | `margin-left` |
| `.c-emoji` (+ `img`) | richview inline emoji / MrkdwnElement emoji | msgs | `display:block`, `width/height:16px`, `margin:0`, `top:0` |
| `.p-rich_text_block` | richview paragraph block | msgs | `overflow: clip`, `overflow-clip-margin: 4px` (zalgo clipping) |
| `.p-section_block__text` | richview/blocks.go:`section` text | msgs | same |
| `.p-rich_text_section`, `.p-mrkdwn_element` | richview inline section / MrkdwnElement root | msgs | `display: inline` |
| `.p-actions_block`, `.p-actions_block_elements`, `.p-actions_block__action`, `.p-block_kit_button_element` | richview/blocks.go actions row + `button` (also via markup) | msgs | Block Kit row layout (flex, gap, margins); button sizing |
| `data-stringify-ignore` | any plugin node | msgs | excluded from message copy / selection text |
| `.c-message_attachment__audio`, `__video_thumb`, `__video_buttons`, `__video_play` | richview/cards.go:`audio` / `video` / `videoCard` (also via markup) | msgs | `height` (inline), `min-height`, `background-color`, centred play button |
| `.c-deprecated-icon`, `.c-icon--play`, `.c-deprecated-icon--inherit` | play glyph (via markup) | msgs | icon-font glyph → native play icon, `color: inherit` |
| `.c-message_attachment__author_icon` | richview attachment author icon (ConnectedBaseAvatar className) | msgs | className match in JS |
| `.c-search_message__avatar` | search result avatar | pop | className match in JS |
| `.c-reaction_bar` | row.go:`reactions` container | msgs | ancestor for rules |
| `.c-reaction`, `.c-reaction--reacted` | reaction pill (self-reacted state) | msgs | `box-shadow` ring, `background-color`, `color`, `:hover` |
| `data-qa="reactji"` | reaction pill button (UnstyledButton prop) | msgs | JS prop match |
| `data-qa="add_reaction_button"`, `.c-reaction_add` (`__fg`) | add-reaction button (also via markup) | msgs | `box-shadow`, `:hover background`, `color`; dark-mode `color` |
| `.c-message_actions__button` | messages/list.go:`toolbar` buttons | msgs | `color`, `:hover background` |
| `.p-emoji_picker` | popups/emoji_picker.go root | pop | `box-shadow` (multiple) |
| `.p-message_input__input_container_unstyled` | composer root (`Composer.Layout`) | comp | `container: name / inline-size`; `@container (min-width)` queries |
| `.c-wysiwyg_container` (+ `--with_input_overlay`) | composer/chrome.go:`Layout` box | comp | `--TextyTheme-*` vars → border & send colors; `border-color` on `:focus-within`; `:has()` on attachment classes |
| `.c-basic_container`, `--bordered`, `--prefixed`, `.c-basic_container__body` | chrome box + its body (also via markup) | comp | body `display:flex; flex-direction:row; flex-wrap; align-items; column-gap`; child `order`/`flex`; `> :empty {display:none}` |
| `.c-wysiwyg_container__formatting` | chrome.go:`layoutBar` | comp | `order` |
| `.c-wysiwyg_container__message_suggestions` | (Slack AI suggestions; Clack none) | comp | `order` |
| `.c-texty_input_unstyled__container` | editor area | comp | `order`, `flex: 100 1 0%`, `min-width: min(450px,100%)` |
| `.c-wysiwyg_container__draft` | (draft status line; Clack none) | comp | `order` |
| `.c-wysiwyg_container__footer` | chrome.go:`layoutFooter` | comp | `display:flex`, `order`, `flex`, `flex-wrap`, `min-width` |
| `.c-wysiwyg_container__footer_divider` | divider between bar and footer | comp | `display: none` |
| `.c-wysiwyg_container__toolbar_buttons` | footer button group | comp | `flex`, `min-width` |
| `.c-texty_buttons` | footer button list | comp | `display: contents` |
| `.c-wysiwyg_container__suffix` | send button group (`layoutSend`) | comp | `margin-left: auto`, `flex` |
| `.c-wysiwyg_container__button` (+ `--emoji`) | each footer button; plugin buttons | comp | `position:absolute; visibility:hidden` (kept laid out but invisible) |
| `.c-wysiwyg_container__attachments`, `.p-message_input__attachments`, `.c-pending_files`, `.c-message__editor__composer_attachments` | composer/attach.go:`attachments.Layout` (channel / thread / edit) | comp | presence in `:has()` |
| `.p-threads_footer__input_container` | thread composer wrapper | comp | `min-height: 0` |
| `.p-threads_footer__input_container__broadcast_controls` | chrome.go:`layoutBroadcast` | comp | `display:none`, `order`, `flex`, `padding-bottom` |
| `.p-context_bar_v2`, `--yellow`, `__icon`, `__content`, `__filler`, `data-qa="context_bar"`, `data-context-bar` | **new** context-bar slot above the composer (also via markup) | comp | Slack's bar look (yellow variant), `:has(+ …)`, adjacent `+`, `border-top-*-radius`, `display:none` on filler, `background`, `color` |
| `.ql-container` (+ `__quill`) | composer/editor.go:`Editor` root | comp | `focusin` capture + `closest()`; a Quill-module shim (`getModule('clipboard'|'slackmarkdown')`) |
| `.p-flexpane_header__control`, `.c-icon_button`, `--size_medium`, `--default` | messages/thread.go:`header` close button (also via markup) | msgs | Slack icon-button look (28px square, hover bg) |
| `.c-date_picker__dropdown` | popups/datetime.go calendar popover (also via markup) | pop | dropdown card padding/background |
| `.p-prefs_dialog__tabs`, `.p-prefs_dialog__menu`, `.p-prefs_dialog__panel` | prefs_modal.go nav column / body | shell | `min-height:0`, `overflow-y:auto`, children `flex-shrink:0`, `scrollbar-gutter:stable`, `:has()` |
| `.p-huddle_event_log__meta_text` | (huddle event log; missing) (via markup) | views | Slack log-line text style |
| `[data-qa="activity-item-container"]`, `[data-qa="activity-item-message"]` | views/activity.go:`row` / preview body | views | blur, `:hover` reveal, `:not(X *)` |
| `[data-qa="dms_channel"]`, `[data-qa="dms-channel-sender-name"]`, `.p-activity_ia4_page__item--selected`, `.c-base_icon_image_stacked` | views/dms.go:`dmRow` / `nameLine` / selected state / group avatar | views | blur, `:has()`, `:hover` |
| `.c-search_autocomplete__suggestion_item` (+ `[data-type="mpim"]`), `__suggestion_item_left`, `.c-channel_icon` + `svg[data-qa^="lock"]` | popups/search_popover.go:`row` and its icon | pop | blur on left text, `:hover`, `:has()` |
| `.c-button` (+ `--outline`, `--disabled`, `--icon`) | popups/style.go:`Button` (also via markup, e.g. `> .c-button`) | pop | `width:100%`; `--icon` 36×36 square; `border-color`, `color` overrides |
| `.c-button-unstyled` | every bare plugin `<button>` (via markup) | taut-ui base CSS | reset: no border/background/padding, inherit font/color, `cursor:pointer` |
| `.c-input_text` | popups/style.go:`field` | pop | `margin`, `margin-bottom`, `padding-right` |
| `.c-input_textarea` | multiline field (settings JSON) | pop | `margin-bottom` |
| `.c-input_checkbox` | Checkbox host component | pop | `flex`, `margin` |
| `.c-label` | Label host component | pop | `margin-bottom` |
| `.c-filter-pill__content` | FilterPill inner content | pop | `width:100%`, `justify-content:center` |
| `.c-inline_alert--level_error` | InlineAlert (error level) | pop | red text + warning icon |
| `.c-link`, `.c-link--button` | link styles (via markup) | taut-ui base CSS | link colour, hover underline |
| `.c-sv_picker__gradient_box`, `__thumb`, `.c-color_slider`, `__outer_hue_track`, `__inner_hue_track`, `__slider`, `.c-slider__input` | settings colour picker (via markup) | taut-ui base CSS | Slack picker geometry; `input[type=range]` |
| `.p-custom_status_modal__input`, `__input_action` | status modal emoji+text input (via markup) | pop | bordered input with a leading emoji button |
| `.offscreen` | (via markup) | taut-ui base CSS | visually hidden, still in the a11y tree |
| `.margin_left_25`, `.margin_right_50`, `.padding_left_50` | (via markup) | taut-ui base CSS | Slack utility spacing (0.25/0.5 rem units → 4px/8px) |

**Count (excluding `taut-*` classes): 119 distinct Slack class names** (95 written out in full, 23 BEM modifiers/elements written as `--x`/`__x` next to their block, plus `sk-client-theme--dark`), **20 attribute selectors** (`data-qa` values, `data-qa-channel-sidebar-*`, `data-item-key`, `data-inline-channel-type-icon`, `data-type`, `data-context-bar`, `data-stringify-ignore`, `svg[data-qa…]`, `[class*="spookySidebarLinkContainer__"]`), and the `html`/`body` element hooks. Clack also needs a **base Slack stylesheet** for the classes marked "via markup", because plugins create elements with them and expect Slack's own CSS to style them.

---

## 3. Other API surface touching UI

### 3.1 composer.addButton (app/slack/composer/buttons.tsx:65-124)
Signature: `{id, placement: 'native'|'end', kinds?: ('channel'|'thread'|'edit'|'draft'|'other')[] (default channel+thread), render(ctx) → {icon: string|ReactNode, label: string, tooltip?: ReactNode, pressed?: boolean, onClick()} | null}`. `render` is a component, so hooks are allowed. The ctx is derived from TextyButtons props: `{kind:'other'}` / `{kind:'draft', channelId?}` / `{kind:'edit', channelId, threadTs?}` / `{kind:'thread', channelId, threadTs, broadcast?: {active, channelType, set}}` / `{kind:'channel', channelId}`.

| Plugin | id | placement / kinds | display |
|---|---|---|---|
| OwlMode :183-204 | `taut-owl-mode` | end / thread | icon `glasses-filled`/`glasses`; labels "Reply as yourself" / "Reply anonymously via Owl" / "Reply via (demo) Owl"; `pressed`; null unless the root is an owl post |
| SilentTyping :50-74 | `taut-silent-typing` | end / default | `notifications-off`/`notifications`; "Show/Hide typing in this chat"; `pressed`; null in owl threads (`api.sharedFrom('OwlMode','composers')`) |
| SlimMessageBox :138-164 | `taut-broadcast` | native / thread | `megaphone-filled`/`megaphone`; label "Also send to #name" / "…as direct message" / "…to the group"; tooltip = the opposite action; `pressed: broadcast.active`; `onClick: broadcast.set(!active)` |

Clack needs a plugin-button slot in composer/chrome.go:`layoutFooter` (after native buttons, and after send for `end`), the viewContext/kind derivation, and broadcast state from `layoutBroadcast` (**comp**).

### 3.2 composer.addSendCheck / addLinkTransform / patchInlineMarkup
- `addSendCheck((blocks, {channelId, threadTs}) => boolean|Promise<boolean>)`. Users: HaikuWarning :146 (modal confirm or a double-send hold), OwlMode :463-481 (warns about mentions). False keeps the message in the composer (§1.4).
- `addLinkTransform(url => url)`. User: ClearURLs :53. It applies on paste (Quill clipboard `preparePastedDelta`, `maybeInterceptMarkdownPaste`; it skips `code`, `code-block`, `link`, `unlink` attributes) and in the link dialog (`ComposerLinkModal.saveLink`). It uses Slack's `detectLinks({input, ignoreEmailMatches, ignorePhoneMatches})`.
- `patchInlineMarkup({parse, serialize, escape})`. User: RealMarkdown :17-21. It patches webpack exports `buildMarkdownChangeDelta` and `convertBlocksToMrkdwnDelta`, and the Quill module `slackmarkdown.buildTextChangeDelta` (only when `useWysiwyg && !formattingOnly`). Clack equivalent: composer/autoformat.go:`autoformatInline` and blocks.go:`DocFromRichtext` (**comp**).

### 3.3 menu
- `api.menu.Menu({template, position?, children})`. CopyReacted :131,160-169 (items `taut-copy-reacted__everyone`, `taut-copy-reacted__<name>` with `label: <MrkdwnElement text=":name: (n)"/>`); Experiments :464,496-514 (radio items).
- `api.menu.ContextMenu({template, children})`. PrivateChannel :567-586 (`taut-private-channel__name`).
- **Menu templates patched via thunks** (no component): Nicknames `patchThunk('getMemberContextMenuTemplate')` :95-110 appends a separator + `taut-nicknames__set` to the member right-click menu (`args.id`); QuickJoin `patchThunk('getContextMenuTemplate')` :29-58 appends a separator + `taut-quick-join__join` "Join channel" to the right-click menu of channel links, sidebar rows and the header (`args.id`). In Clack, popups/actions_menus.go:`NewChannelMenu` and the profile/member menus must be produced by Slack-named thunks returning `MenuTemplateItem[]`, so these patches apply (**pop**).

### 3.4 modal (app/api/modal.tsx)
`openModal({title: ReactNode, body: ReactNode, submitText?='Save', cancelText?='Cancel', danger?, showCancelButton?, showSubmitButton?, onSubmit?, onCancel?, onClose?}) → {close()}`; `confirm({title, body?, confirmText?='Confirm', cancelText?='Cancel', danger?}) → Promise<boolean>`; `alert({title, body?, closeText?='OK'}) → Promise<void>`; `modal.Label` (= Label); `modal.TextInput` (= FormTextInput, autoFocus applied after mount). Pressing Enter in a text-like `<input>` inside the body submits. It is dispatched through Slack's `openModal({element, name, isStackable: true})`.

| Plugin | calls |
|---|---|
| AccountSwitcher :139 | alert "Couldn't switch account" / "Couldn't add an account" |
| CopyReacted :99,120 | alert "Could not copy" |
| Experiments :336 | confirm "Reset all experiments?" (confirmText Reset) |
| HaikuWarning :169-190 | confirm, title = `<MrkdwnElement>` **(ReactNode title)**, body = `<p>`, `<blockquote>`, `<code>`; Send anyway / Keep editing |
| Nicknames :206-239 | openModal "Set nickname for X", body Label(optional) + TextInput(hintText, autoFocus) |
| OwlMode :475,673,691,748,907 | confirm (mentions), openModal details (danger Forget, Close), confirm forget (danger), openModal claim (TextInput password + errorText), alert errors |
| PrivateChannel :616-622 | openModal "Set local channel name", Label + TextInput |
| settings | confirm (danger) for Clear cache/data, Delete plugin |

Clack: popups/modal.go:`NewModal` (`ModalOpts{Title, Body, Buttons, EnterSubmits, OnClose}`) and `Confirm` (**pop**). Title and body must be ReactNode slots, and the plugin modal is stacked through `slackc.LayoutOverlays`.

### 3.5 preferences.addTab
`addTab({label: string, icon: string (SvgIcon name), render: () => ReactNode})`. Its tab ids are `taut-tab-N`, sorted by label, after taut's own "Taut" tab. User: Experiments :147-151 (`Experiments`, `magic-wand`). The Taut settings tab also hosts each plugin's `dataPanel()` (Nicknames :298-327; taut renders it under `FieldSet > Legend "Data"`, pluginDetail.tsx:268-276) and each `opt(…, {editor})` custom editor `<Editor value onChange label api/>` (AdminBackend ToolsEditor; OfficeHours ScheduleEditor, StatusEditor, ReplyEditor). Clack: prefs_modal.go needs a `Tabs` boundary and a "Taut" tab (**shell**, content **taut-ui**).

### 3.6 elements.* usage per plugin (summary; props in §1.12)
- AccountSwitcher: SvgIcon. menu labels are ReactNode (`AccountRow`).
- AdminBackend: Tooltip, Button, SvgIcon, Label, FormTextInput (settings editor).
- BetterTypingIndicator: Tooltip, plus lazy MemberLink.
- ClickToLoad: none (Slack markup only).
- CopyReacted: MrkdwnElement, SvgIcon (+ menu.Menu).
- CustomNameRecording: Button, SvgIcon, InlineAlert.
- Experiments: FormTextInput, SvgIcon, InlineAlert, FilterPill, Button, BasicSelect (+ menu.Menu, modal.confirm, preferences.addTab).
- HaikuWarning: MrkdwnElement (+ modal.confirm).
- Nicknames: Avatar, FormTextInput, Tooltip, Button, SvgIcon (+ modal.openModal/Label/TextInput).
- OfficeHours: FilterPill, TimePicker, Tooltip, Button, SvgIcon, EmojiMenuTrigger, MrkdwnElement, RichTextInput (settings editors).
- OwlMode: SvgIcon, Tooltip, IconButtonBase, Button, MrkdwnElement (+ modal.*, composer.addButton/addSendCheck).
- PrivateChannel: SvgIcon (+ menu.ContextMenu, modal.openModal/Label/TextInput).
- RestrictedChannelWarning: Button.
- ShinigamiEyes: MrkdwnElement.
- ShowSendingBot: Avatar, ProfileHoverTrigger.
- StreamerMode: Tooltip, SvgIcon, plus lazy KeyboardKeysTooltip.
- ThreadJump: Tooltip, SvgIcon, Popover, Button, DatePickerTrigger.
- app settings / whatsNew: SvgIcon, MrkdwnElement, Button, Tooltip, InlineAlert, Label, FormTextInput, Checkbox, FilterPill, Tag, FieldSet, Legend, BasicSelect, Popover.
- Exposed but unused anywhere: DateRangePicker, Hint, Blocks (as an element), TimePicker outside OfficeHours.

### 3.7 ReactNode-valued props ("$el"-style) Clack must accept as subtrees
- `Tooltip.tip`: KeyboardKeysTooltip node (StreamerMode), a string elsewhere.
- `composer.addButton` `icon` (string or node) and `tooltip`.
- `MenuTemplateItem.label` (AccountRow, `<MrkdwnElement/>`) and `description`.
- `MenuTrigger.renderMenu`: a render-prop function returning a node.
- `ConfirmationModal.title` (MrkdwnElement) and body children.
- `modal.openModal` title/body.
- `Label.text`/`subtext`; `BasicSelect.options[].label`.
- `MemberProfileHoverCard.header`; `MessageInputRoadblock.roadblockMessage`; `TypingNames.firstTyper`/`secondTyper`.
- Gutter memo `light`/`compact` (element slots inspected by `className` and `type.type.displayName`).
- `BaseBroadcastPreamble.children` (inspected by `isInteractive` / `clickable` / `visible` props).
- `HuddleBaseEvent.children`, `HuddlesAlertBannerBase.children`, `ActivityRowContentSenderAvatarLayout.children`, `ActivityPrivacyBlurredName.children`, `TextyButtonOverflow.children` (array, appended to).
- `MessageInput.broadcastControls` (element whose props are read).
- `Tabs.tabs[].label`/`content`; `enqueueHuddleToast` `options.content`.
- `preferences.addTab.render()`; `dataPanel()`; opt `editor`.

### 3.8 Thunks and redux paths that drive visible UI (no component)
- `showNotification` (args.message): suppressed by StreamerMode and ShutUpSlackbot, rewritten by Censorship (patchMessageText).
- `enqueueHuddleToast` (HuddleActivity).
- `currentUserStartedTyping`: no-op'd by SilentTyping and OwlMode.
- `autocompleteMembers` (NativeAtChannel adds `@channel`/`@here` entries `BKchannel`/`BKhere`) and `autocompleteChannels` (PrivateChannel merges stubs). These feed composer/autocomplete.go (**comp**).
- `prepareAndSendMessage` (NativeAtChannel, OwlMode).
- `getContextMenuTemplate` / `getMemberContextMenuTemplate` (§3.3).
- `setUserPrefFetcher` / `setUserPrefByApi` / `initializeUserPrefs` for `msg_input_sticky_composer` (SlimMessageBox, the formatting-bar toggle → composer `SetShowFormatting`).
- Experiments forced on/off: `reskin_custom_responses` (BringBackSlackbot), `find_in_thread` and `fe_long_threads_jump_to_latest` (ThreadJump). These gate whether Slack draws ThreadSearchButton and jump banners, so Clack must read them.
- Slices patched: `messages` (ShowRealUser, Censorship), `members` (Nicknames → every name label), `channels` (PrivateChannel → sidebar, mentions, switcher), `readOnlyChannels` and `threadOnlyChannels` (RestrictedChannelWarning → roadblock), `searchResults` (Censorship), `huddleEvents` (HuddleActivity). Every native widget must read names, channels and messages **through** the patched store view, never straight from core.

---

## 4. Ambiguities and open points

1. **Two anonymous filter matches** (#7, #21) match Slack module source, not a name. Clack's native gutter must report a "source" containing `c-message_kit__gutter__left`. The huddle join gate must be a memo with an unnamed inner function whose source contains `canJoinByExternalHuddleLink`. Either give native components synthetic source strings, or extend `getComponentSource` to read a per-component marker list.
2. **Source-marker elements** (`Button`, `Tooltip`, `Label`, `Legend`, `Popover`) are found by `getComponentSource(...).includes(marker)` over the *whole module*. Clack's exported components need those markers in whatever stands in for their module source.
3. **`MessageActions`** names two Slack components (the hover toolbar and the whole-message wrapper, per the OwlMode comment). OwlMode wants the toolbar. Clack should give the name only to the toolbar unless another plugin turns up that needs the wrapper.
4. **`BaseBroadcastPreamble`** is the channel message *header*, despite its name. The plugin inspects child element props (`isInteractive`, `clickable`, `visible`), so Clack's sender and timestamp must be real elements with those props.
5. **`ListChannelEntity`**: the plugin only shows it as a channel row. Whether Slack also uses it in pickers (forward modal, directories) isn't visible from taut source. Mapped to the sidebar row; views/directories.go:`chanRow` and popups/actions_forward.go targets are candidates.
6. **`UnstyledButton`** is generic: WhoReacted filters on `data-qa="reactji"`. To be exact, every native unstyled button would go through this name with its `data-qa`. At minimum the reaction pill must.
7. **`SearchResult`** spreads the whole props object as a message, so Clack's search card props must be the message fields at top level plus `channel`.
8. **`ComposerLinkModal`** needs a class with a patchable `prototype.saveLink`, found through the rendered registry (`waitForRenderedComponent`), and it is wrapped as a connect-memo (`modal.type ?? modal`).
9. **Typing indicator placement**: Clack draws typing as plain text in the composer hint bar. `TypingNames` needs ReactNode `firstTyper`/`secondTyper` slots, and BetterTypingIndicator measures the DOM node (`clientWidth`, computed font, `ResizeObserver`, `OffscreenCanvas`).
10. **Preferences `Tabs`** is only patched when the last tab id is `'advanced'`. Clack's prefs dialog has no Advanced section, so it must add one (or at least an `advanced` id last).
11. **Icon names**: plugins use Slack SvgIcon names that Clack's registry lacks (list in §1.12). They need an alias table and a fallback glyph.
12. **The class count is approximate at the edges.** `[class*="spookySidebarLinkContainer__"]` is a CSS-module hash prefix (Slack seasonal UI), and `c-reaction_add__fg` appears only in a comment.
13. **`ShinigamiEyes.disabled.tsx`** is included although taut ships it disabled (it broke when the Hackatime API changed).
14. **Process note**: the per-file extraction for this catalogue ran as a 6-agent workflow (4 plugin batches, `app/`, and a Clack widget index). Everything was then cross-checked by direct `rg` over every `patchComponent`/`lazyComponent`/`elements.*` call site. Raw notes are in `/tmp/pc/*.md` (not committed).

---

## 5. Wiring guide (per owner)

Every Slack boundary in §Index becomes one `slackc` call in the owner's package. The rules are the same everywhere.

### 5.1 The three calls
```go
// Hot widgets (rows, pills, sidebar items): allocation-free when unpatched.
// Pass method expressions, not closures or method values.
return slackc.Component(gtx, "MessageSender", r, (*row).senderProps, (*row).layoutSender)

// Cold widgets (headers, modals): a closure is fine.
return slackc.Render(gtx, "BaseAvatarStack", props, func(gtx layout.Context, p slackc.Props) layout.Dimensions { … })

// Props that are costly to build: built only when patched.
return slackc.RenderLazy(gtx, "MessageBlocks", func() slackc.Props { return r.blocksProps() }, r.layoutBlocks)
```
- **Props are Slack-shaped.** Use the names and shapes in §1 and in the live table (`message`/`msg` with Slack's message fields `ts`, `user`, `bot_id`, `subtype`, `blocks`, `reactions`, …, plus `channelId`, `memberId`, `userId`). Build them only inside the props func. Use `$key` (the message ts, the channel id) for list items so plugin hook state survives prop changes.
- **The native's `p` is nil when unpatched.** Under a patch it is what the plugin passed to `<Original {...props}>`. Honour the overrides plugins make (§1, "overrides"): read them with `p.StrOr`, `p.Bool`, `p.Map`, `p.List`, and fall back to your own state when a key is missing.
- **Children.** When Slack's component takes children, hand your sub-parts to JS as slots, so plugins can find and reorder them (UserPronouns, WhoReacted):
  ```go
  Props{"msg": m, "children": []any{slackc.Child("MessageSender", sp, r.layoutSender), slackc.Child("Timestamp", tp, r.layoutTS)}}
  ```
  In the native, `if e, ok := slackc.PropChildren(p); ok { return slackc.LayoutElement(gtx, e) }`. That lays the plugin's children out with the flow engine, and each slot calls back into its native.
- **Events and state** stay native. Clickables and editors live where they live today. A native called through a patch still gets real input.
- **Missing surfaces** (BaseContextBarV2, MessageInputRoadblock, CharacterCount, ThreadSearchButton, ThreadFlexpaneAdminTools, EditAudioButton): add the boundary with a native that draws nothing (or Slack's default), so a plugin that appends to it shows up.

### 5.2 Class tagging for CSS
```go
var msgClass = slackc.Class("c-virtual_list__item c-message_kit__message", "data-qa", "message_container").
	Mods("c-message_kit__message--hovered", "c-message--highlight")

st := msgClass.Enter(gtx, hoverState(r), modBits) // css.StateHover etc.
defer st.Leave()                                     // or call Leave on every return path
if st.Hidden() { return layout.Dimensions{} }
fg := st.Color(c.Text); pad := st.Padding(defPad); size := st.FontSize(15)
return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { … }) // opacity, blur, background, border, margin, transform
```
`Enter` costs one atomic load while no CSS is set. Declare `ClassDef`s at package level. The class names to use are in §2.3. Tag the ancestors too (`.p-client_container`, `.p-channel_sidebar`, `.c-message_list`, `.p-message_pane`), because descendant selectors only match when the whole chain is on the stack.

### 5.3 Per-owner checklist
Rows link to §Index numbers. ✔ = wrapped in `taut-ui`.
- **msgs** (`messages`, `richview`): 1 MessageBackground, 2 MessageWrapper, 5 MessageAvatar, 6 ConnectedBaseAvatar (register as host `ConnectedBaseAvatar`/`Avatar`), 7 gutter filter memo (name `MessageKitGutter`, source marker `c-message_kit__gutter__left`), 8, 9 MessageSender, 10 BaseMessageSender, 11 BaseBroadcastPreamble (children: sender + timestamp slots), 12, 13, 14 MessageBlocks, 15 Blocks (also host), 17–21 attachments, 22 ReactionBar (children: Reaction slots + ReactionAddButton slot), 23 Reaction, 24 UnstyledButton `data-qa=reactji`, 25 ReactionAddButton, 26 MessageActions, 27 MessageActionButtonReaction, 44, 45/46 (empty boundaries), 50 BaseMrkdwnChannel, 51 MemberLink, 72 MrkdwnElement (host).
- **comp** (`composer`): 30 MessagePaneInput, 31 InputContainer, 32 MessageInput, 33 WysiwygContainer, 34 TextyButtons, 35 TextyButtonOverflow (+ composer.addButton slot), 36, 37 BaseContextBarV2 (empty boundary above the composer), 38 MessageInputRoadblock (boundary around the composer), 39 ThreadTypingIndicator, 40 TypingNames, 41, 42 CharacterCount (empty), 43 ComposerLinkModal.
- **pop** (`popups`): 3 MessageListItem, 4 SearchResult, 28 EmojiPickerPopover, 29 EmojiPicker, 55–60 profile parts, 67–69 menus, plus the elements drawn by `slackc/kit`.
- **views** (`views`): 12/13 in Threads, 52 ActivityItem, 53, 54 ActivityPrivacyBlurredName, 61–65 huddle surfaces where they exist (Huddles view), 90 BasicSelect.
- **shell** (`internal/ui` root): 47 BaseAvatarStack, 48 HelpButton, 49 ListChannelEntity, 57 BasePresence (sidebar), 66 Tabs (prefs), 70 SvgIcon names, plus the ancestor classes (`p-client_container`, `p-ia`, `p-channel_sidebar`, `p-view_header`).

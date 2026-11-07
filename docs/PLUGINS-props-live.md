# Slack web client: live React component props (shapes only)

Captured from the signed-in web client, test channel only, by walking the React fiber tree (`__reactContainer$*` root → child/sibling) and reading `memoizedProps` of each composite fiber. Names are `type.displayName || type.name`, unwrapping memo (`type.type`) and forwardRef (`type.render`). Records names, types and shapes only, never values. The few string literals shown are UI enums (size/variant/type/position…).

**How to read it**

- `?` = optional: the key was missing or `undefined` in some sampled instances. `seen a/b` = instances with the key / sampled instances. Props were sampled from the first 400 instances per component, then every 10th.
- `fn` = function, `node` = React element, `ref` = `{current}`, `obj:Foo` = class instance, `map<string, T>` = id/ts-keyed object (keys dropped). Nesting goes 2 levels deep (3 for array elements). Deeper levels show as `object`.
- `fibers seen` is summed over every snapshot run (about 35 runs across the states below), so it is relative frequency, not the count on screen.
- outer host = first host (DOM) fiber found by following `.child` from the component. Classes with a `__xxxxx` hash suffix are CSS-module classes and change between builds. `<id>`/`<n>` mark scrubbed ids/timestamps.
- States: `activity`, `activity-menu-Filters`, `activity-menu-Settings`, `channel`, `channel-bottom`, `channel-details`, `channel-details-Agents & apps`, `channel-details-Automations`, `channel-details-Members`, `channel-details-Settings`, `channel-details-Tabs`, `channel-history`, `channel-scroll`, `channel2`, `composer-focused`, `composer-focused-thread`, `emoji-picker-composer`, `emoji-picker-reaction`, `message-more-menu`, `profile-hover`, `sidebar-context-menu`, `thread`, `thread-hover`

**Taut installed in this profile?** No. No `window.taut`, no `window.TautAPI`, no taut DOM/style elements, no extension scripts. When I opened my tab, it did have three `window.__taut*` globals (`__tautSlices` string, `__tautProbeStore` = a Redux store, `__tautReq` = webpack require). They were gone after a reload, so I think another agent ran a probe in this browser. That isn't a Taut install.

## Coverage

- Targets found live: 42/59
- Targets not rendered live: `ViewBlocks`, `MessageListItem`, `MessageInputRoadblock`, `ListChannelEntity`, `BaseMrkdwnChannel`, `BaseMessageAttachmentSlackMessage`, `MessageAttachmentHuddleLink`, `RimetoMemberProfileOverflowMenu`, `ConfirmationModal`, `InlineAlert`, `FormTextInput`, `FilterPill`, `Tag`, `FieldSet`, `Legend`, `Hint`, `TimePicker`
- Of those, found as webpack exports (prop names from source/defaultProps, no live shapes): `MessageListItem`, `MessageInputRoadblock`, `ConfirmationModal`, `InlineAlert`, `FormTextInput`, `FilterPill`, `Tag`, `FieldSet`, `Legend`, `Hint`, `TimePicker`

## Contents

1. Requested targets (live)
2. Requested targets (static only)
3. Top 150 other components by frequency
4. Other sidebar / header / message-list / divider / huddle / thread / composer / activity components (216)
5. Static-only extras (dividers etc.)

## 1. Requested targets (live)

### BaseAvatarStack

- type: memo · fibers seen: 62 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<button>` · classes: `c-button-unstyled` `p-avatar_stack--details` `p-avatar_stack--details--polish` `p-avatar_stack--no-outline`
- data-qa: `avatar_stack`

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 62/62 |
| `channelIsIm` | boolean | 62/62 |
| `experimentChannelIaMembershipGroupOn` | boolean | 62/62 |
| `hasHuddleParticipants` | boolean | 62/62 |
| `hideOutline` | boolean | 62/62 |
| `isQuipDoc` | boolean | 62/62 |
| `member1` | {_display_name_lc: string, _display_name_normalized_lc: string, _email_normalized_lc: string, _first_name_lc: string, _last_name_lc: string, _name_lc: string, _real_name_lc: string, _real_name_normalized_lc: string, activity: array, color: string, deleted: boolean, enterprise_id: string, enterprise_user: {enterprise_id: string, enterprise_name: string, id: string, is_admin: boolean, is_owner: boolean, is_primary_owner: boolean, teams: array}, files: array, id: string, isExternal: boolean, isNonExistent: boolean, isUnknown: boolean, is_admin: boolean, is_app_user: boolean, is_bot: boolean, is_email_confirmed: boolean, is_invited_user: boolean, is_owner: boolean, is_primary_owner: boolean, is_restricted: boolean, is_stranger: boolean, is_ultra_restricted: boolean, member_color: string, mentions: array, …+10} | 62/62 |
| `member2` | {_display_name_lc: string, _display_name_normalized_lc: string, _email_normalized_lc: string, _first_name_lc: string, _last_name_lc: string, _name_lc: string, _real_name_lc: string, _real_name_normalized_lc: string, activity: array, color: string, deleted: boolean, enterprise_id: string, enterprise_user: {enterprise_id: string, enterprise_name: string, id: string, is_admin: boolean, is_owner: boolean, is_primary_owner: boolean, teams: array}, files: array, id: string, isExternal: boolean, isNonExistent: boolean, isUnknown: boolean, is_admin: boolean, is_app_user: boolean, is_bot: boolean, is_email_confirmed: boolean, is_invited_user: boolean, is_owner: boolean, is_primary_owner: boolean, is_restricted: boolean, is_stranger: boolean, is_ultra_restricted: boolean, member_color: string, mentions: array, …+10} | 62/62 |
| `member3` | {_display_name_lc: string, _display_name_normalized_lc: string, _email_normalized_lc: string, _first_name_lc: string, _last_name_lc: string, _name_lc: string, _real_name_lc: string, _real_name_normalized_lc: string, activity: array, color: string, deleted: boolean, enterprise_id: string, enterprise_user: {enterprise_id: string, enterprise_name: string, id: string, is_admin: boolean, is_owner: boolean, is_primary_owner: boolean, teams: array}, files: array, id: string, isExternal: boolean, isNonExistent: boolean, isUnknown: boolean, is_admin: boolean, is_app_user: boolean, is_bot: boolean, is_email_confirmed: boolean, is_invited_user: boolean, is_owner: boolean, is_primary_owner: boolean, is_restricted: boolean, is_stranger: boolean, is_ultra_restricted: boolean, member_color: string, mentions: array, …+10} | 62/62 |
| `member4?` | undefined | 62/62 |
| `member5?` | undefined | 62/62 |
| `memberCount` | number | 62/62 |
| `memberIsActive1` | boolean | 62/62 |
| `memberIsActive2` | boolean | 62/62 |
| `memberIsActive3` | boolean | 62/62 |
| `memberIsActive4` | boolean | 62/62 |
| `memberIsActive5` | boolean | 62/62 |
| `memberName1` | string | 62/62 |
| `memberName2` | string | 62/62 |
| `memberName3` | string | 62/62 |
| `memberName4?` | undefined | 62/62 |
| `memberName5?` | undefined | 62/62 |
| `navigate` | fn | 62/62 |
| `openAboutChannelModal` | fn | 62/62 |
| `showFiveAvatars` | boolean | 62/62 |
| `showIconOnly` | boolean | 62/62 |
| `showPresence` | boolean | 62/62 |

### MessageSender

- type: plain · fibers seen: 396 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: `c-message__sender` `c-message_kit__sender`
- data-qa: `message_sender` `aria-labelledby-primary-<id>-<n>-sender`

| prop | shape | seen |
|---|---|---|
| `className?` | undefined | 30/396 |
| `hideAppBadge?` | boolean | 15/396 |
| `hideCustomStatus?` | boolean | 15/396 |
| `isInteractive?` | boolean | 366/396 |
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: array<object>, blocks?: array<object>, channel: string, crm_signal?: undefined, files?: array<string>, language?: undefined, latest_reply?: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users?: array<string>, reply_users_count?: number, slackbot_feels: null, subtype?: string ("reminder_add"|"bot_message"), text: string, thread_ts?: string, ts: string, type: string ("message"), user?: string, version?: string, client_msg_id?: string, upload?: boolean, bot_id?: string, edited?: {ts: string, user: string}, is_locked?: boolean, bot_profile?: {app_id: string, deleted: boolean, icons: object, id: string, name: string, team_id: string, updated: number, user_id: string}, parent_user_id?: string, …+6} | 396/396 |
| `suppressColoredNames?` | boolean | 15/396 |
| `viewContext` | string | 396/396 |
| `visible?` | boolean | 351/396 |

### BaseMessageSender

- type: plain · fibers seen: 396 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: `c-message__sender` `c-message_kit__sender`
- data-qa: `message_sender` `aria-labelledby-primary-<id>-<n>-sender`

_no props recorded_

### ThreadSenderAndTimestampGeneric

- type: plain · fibers seen: 30 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: `<span>` · classes: `c-message__sender` `c-message_kit__sender`
- data-qa: `message_sender`

| prop | shape | seen |
|---|---|---|
| `adjacent?` | boolean | 15/30 |
| `keepSenderAccessible` | boolean | 30/30 |
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: array<object>, blocks: array<object>, channel: string, crm_signal?: undefined, files?: array<string>, language?: undefined, latest_reply?: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users?: array<string>, reply_users_count?: number, slackbot_feels: null, subtype?: undefined, text: string, thread_ts: string, ts: string, type: string ("message"), user: string, bot_id?: string, client_msg_id?: string, edited?: {ts: string, user: string}, is_locked?: boolean, parent_user_id?: string, upload?: boolean, version?: string, x_files?: array<string>} | 30/30 |
| `omitLinebreak` | boolean | 30/30 |
| `omitTimestamp` | boolean | 30/30 |
| `timestampLabel` | string | 30/30 |
| `visible` | boolean | 30/30 |

### BaseBroadcastPreamble

- type: plain · fibers seen: 351 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: `c-message__sender` `c-message_kit__sender`
- data-qa: `message_sender` `aria-labelledby-primary-<id>-<n>-sender`

| prop | shape | seen |
|---|---|---|
| `dispatch` | fn | 351/351 |
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: array<object>, blocks?: array<object>, channel: string, crm_signal?: undefined, files?: array<string>, language?: undefined, latest_reply?: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users?: array<string>, reply_users_count?: number, slackbot_feels: null, subtype?: string ("reminder_add"), text: string, thread_ts?: string, ts: string, type: string ("message"), user: string, version?: string, client_msg_id?: string, upload?: boolean, edited?: {ts: string, user: string}, bot_id?: string, is_locked?: boolean, bot_profile?: {app_id: string, deleted: boolean, icons: object, id: string, name: string, team_id: string, updated: number, user_id: string}, metadata?: {event_type: string}} | 351/351 |
| `shouldRender` | boolean | 351/351 |
| `children` | array<node \| boolean> | 351/351 |

### ConnectedBaseAvatar

- type: plain · fibers seen: 706 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: `c-avatar` `p-member_profile_hover_card` `p-avatar_stack__avatar` `p-ia__nav__user__avatar` `c-message__reply_bar_avatar` `c-avatar--transparent_bg` `c-base_entity__avatar` `p-member_profile_base_entity__avatar` `p-member_profile_base_entity__avatar--cursor`
- data-qa: `channel-prefix-im-avatar` `member-entity__avatar`

| prop | shape | seen |
|---|---|---|
| `additionalOverlay?` | undefined | 25/430 |
| `ariaHidden?` | string | 152/430 |
| `botId?` | string | 184/430 |
| `botProfile?` | {app_id: string, deleted: boolean, icons: {image_36: string, image_48: string, image_72: string}, id: string, name: string, team_id: string, updated: number, user_id: string} | 159/430 |
| `channelId?` | string | 156/430 |
| `className?` | string | 270/430 |
| `clogLinkClick?` | fn | 152/430 |
| `data-qa?` | string | 264/430 |
| `emoji?` | undefined | 25/430 |
| `forceSlackbotNotification?` | boolean | 152/430 |
| `icons?` | undefined | 184/430 |
| `isAdHocMember?` | boolean | 152/430 |
| `isInteractive` | boolean | 430/430 |
| `isSlackbotAi?` | boolean | 152/430 |
| `isSlackbotAiThinking?` | boolean | 152/430 |
| `isSlackbotNotification?` | boolean | 152/430 |
| `isUnknown?` | boolean | 152/430 |
| `messageTs?` | string | 152/430 |
| `showCard?` | boolean | 177/430 |
| `showPresence?` | boolean | 133/430 |
| `showTooltip?` | boolean | 152/430 |
| `size` | number | 430/430 |
| `slackbotFeels?` | null | 152/430 |
| `tabIndex?` | number | 152/430 |
| `triggerId?` | undefined | 152/430 |
| `userId` | string | 430/430 |

### Blocks

- type: plain · fibers seen: 381 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `c-message_kit__blocks` `c-message_kit__blocks--rich_text`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `blocksContainerContext` | string | 381/381 |
| `inline?` | boolean | 15/381 |
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: array<object>, blocks?: array<object>, channel: string, crm_signal?: undefined, files?: array<string>, language?: undefined, latest_reply?: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users?: array<string>, reply_users_count?: number, slackbot_feels: null, subtype?: string ("reminder_add"), text: string, thread_ts?: string, ts: string, type: string ("message"), user: string, version?: string, client_msg_id?: string, upload?: boolean, edited?: {ts: string, user: string}, bot_id?: string, is_locked?: boolean, bot_profile?: {app_id: string, deleted: boolean, icons: object, id: string, name: string, team_id: string, updated: number, user_id: string}, parent_user_id?: string, …+2} | 381/381 |
| `streaming?` | boolean | 366/381 |

### MessageBackground

- type: plain · fibers seen: 381 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `c-message_kit__background` `c-message_kit__message` `p-message_pane_message__message` `c-message_kit__thread_message` `c-message_kit__thread_message--nested` `p-message_pane_message__active_thread` `c-message_kit__thread_message--root` `c-message_kit__background--hovered`
- data-qa: `message_container`

| prop | shape | seen |
|---|---|---|
| `className` | string | 381/381 |
| `clearHighlight` | fn | 381/381 |
| `disableDeleteAnimation?` | boolean | 15/381 |
| `experimentPrivacyModeGroupOn` | boolean | 381/381 |
| `getMessageMetaDataThunk` | fn | 381/381 |
| `highlight` | boolean | 381/381 |
| `hover` | boolean | 381/381 |
| `isActiveHuddle` | boolean | 381/381 |
| `isBlurredMsg?` | boolean | 351/381 |
| `isChildWindow` | boolean | 381/381 |
| `isDeleting` | boolean | 381/381 |
| `isMainWindow` | boolean | 381/381 |
| `isMessagingMaxWidthEnabled` | boolean | 381/381 |
| `isPrivacyModeBlurred` | boolean | 381/381 |
| `logMessage` | fn | 381/381 |
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: array<object>, blocks?: array<object>, channel: string, crm_signal?: undefined, files?: array<string>, language?: undefined, latest_reply?: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users?: array<string>, reply_users_count?: number, slackbot_feels: null, subtype?: string ("reminder_add"), text: string, thread_ts?: string, ts: string, type: string ("message"), user: string, version?: string, client_msg_id?: string, upload?: boolean, edited?: {ts: string, user: string}, bot_id?: string, is_locked?: boolean, bot_profile?: {app_id: string, deleted: boolean, icons: object, id: string, name: string, team_id: string, updated: number, user_id: string}, parent_user_id?: string, …+2} | 381/381 |
| `onClick` | fn | 381/381 |
| `ref` | null | 381/381 |
| `removeMessageAndTimestamp` | fn | 381/381 |
| `viewContext` | string | 381/381 |
| `windowId` | string | 381/381 |
| `windowRef` | WeakRef | 381/381 |
| `windowToken?` | undefined | 381/381 |
| `children` | node | 381/381 |

### MessageActions

- type: memo · fibers seen: 768 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `c-message_kit__actions` `c-message_kit__actions--default` `c-message_kit__actions--above` `c-message_kit__actions--inside` `c-message_actions__container` `c-message__actions`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `dispatch` | fn | 431/431 |
| `hover` | boolean | 431/431 |
| `isChildWindow` | boolean | 431/431 |
| `isEditing` | boolean | 431/431 |
| `isLast?` | boolean | 400/431 |
| `isMainWindow` | boolean | 431/431 |
| `isMessageHidden` | boolean | 431/431 |
| `messageContainerType` | string | 431/431 |
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: array<object>, blocks?: array<object>, channel: string, crm_signal?: undefined, files?: array<string>, language?: undefined, latest_reply?: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users?: array<string>, reply_users_count?: number, slackbot_feels: null, subtype?: string ("reminder_add"), text: string, thread_ts?: string, ts: string, type: string ("message"), user: string, version?: string, client_msg_id?: string, upload?: boolean, edited?: {ts: string, user: string}, is_locked?: boolean, bot_id?: string, bot_profile?: {app_id: string, deleted: boolean, icons: object, id: string, name: string, team_id: string, updated: number, user_id: string}, parent_user_id?: string, …+2} | 431/431 |
| `parentId` | string | 431/431 |
| `persistActions` | boolean | 431/431 |
| `position` | string ("default"|"above"|"inside") | 431/431 |
| `ref` | null | 431/431 |
| `renderOffscreen` | boolean | 431/431 |
| `rollup?` | undefined | 400/431 |
| `setHover` | fn | 431/431 |
| `showLaterQuickAction?` | boolean | 31/431 |
| `showShareButton?` | boolean | 31/431 |
| `suppressToolbar` | boolean | 431/431 |
| `viewContext` | string | 431/431 |
| `windowId` | string | 431/431 |
| `windowRef` | WeakRef | 431/431 |
| `windowToken?` | undefined | 431/431 |
| `children` | node | 431/431 |

### MessageActionButtonReaction

- type: plain · fibers seen: 6 · states: emoji-picker-reaction, message-more-menu, profile-hover, thread, thread-hover
- outer host: `<button>` · classes: `c-button-unstyled` `c-icon_button` `c-icon_button--size_smedium` `c-message_actions__button` `c-icon_button--default`
- data-qa: `add_reaction`

| prop | shape | seen |
|---|---|---|
| `actionId` | string | 6/6 |
| `aria-label` | string | 6/6 |
| `autoClogProps` | {elementName: number, onClick: {enableClogAction: boolean}, uiElement: number} | 6/6 |
| `btnRef` | ref | 6/6 |
| `columnIdentifier` | string | 6/6 |
| `columnWidth?` | undefined | 6/6 |
| `data-qa` | string | 6/6 |
| `fadeQuickReactionsCustomizationTip` | fn | 6/6 |
| `focusMetaData` | string | 6/6 |
| `hasKeyboardFocus` | boolean | 6/6 |
| `icon` | node | 6/6 |
| `label` | string | 6/6 |
| `messageContainerType` | string | 6/6 |
| `onSelectAction` | fn | 6/6 |
| `shortcut` | {key: array<string>, label: string} | 6/6 |
| `showLabel` | boolean | 6/6 |
| `uiElement` | number | 6/6 |

### Reaction

- type: plain · fibers seen: 9 · states: activity, activity-menu-Filters, activity-menu-Settings, channel-scroll
- outer host: `<button>` · classes: `c-button-unstyled` `c-reaction` `c-reaction--light`
- data-qa: `reactji`

| prop | shape | seen |
|---|---|---|
| `canReact` | boolean | 9/9 |
| `count` | number | 9/9 |
| `currentUser` | string | 9/9 |
| `name` | string | 9/9 |
| `onClick` | fn | 9/9 |
| `reactionKey` | string | 9/9 |
| `reactions` | array<{baseName: string, count: number, name: string, users: array}> | 9/9 |
| `shouldShowReacjiTriggers` | boolean | 9/9 |
| `tooltip?` | undefined | 9/9 |
| `users` | array<string> | 9/9 |

### ReactionBar

- type: plain · fibers seen: 388 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `c-message_kit__reaction_bar` `c-reaction_bar` `c-reaction_bar--collapsed` `p-activity_ia4_page__item__reactions_bar` `activity_multi_line_row_content__reactions_bar__cK4ca` `p-activity_ia4_page__item__reactions_bar--add_button_hidden`
- data-qa: `reaction_bar`

| prop | shape | seen |
|---|---|---|
| `addReactionButton?` | undefined | 388/388 |
| `alwaysExpand?` | boolean | 388/388 |
| `canReact` | boolean | 388/388 |
| `channelId` | string | 388/388 |
| `className` | string | 388/388 |
| `clogReactionClick?` | undefined | 388/388 |
| `currentUser` | string | 388/388 |
| `eligibleForRendering` | boolean | 388/388 |
| `handleReactionClick` | fn | 388/388 |
| `hasKeyboardFocus` | boolean | 388/388 |
| `isExpanded` | boolean | 388/388 |
| `reactionKey` | string | 388/388 |
| `reactions` | array<{baseName: string, count: number, name: string, users: array}> | 388/388 |
| `ref` | null | 388/388 |
| `setExpanded` | fn | 388/388 |
| `shouldShowReacjiTriggers` | boolean | 388/388 |
| `showWhenEmpty` | boolean | 388/388 |

### ReactionAddButton

- type: plain · fibers seen: 9 · states: activity, activity-menu-Filters, activity-menu-Settings, channel-scroll
- outer host: `<button>` · classes: `c-button-unstyled` `c-reaction_add`
- data-qa: `add_reaction_button`

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 9/9 |
| `onClick` | fn | 9/9 |
| `onEmojiSelected` | fn | 9/9 |
| `shouldPropagateOnClose?` | undefined | 9/9 |
| `shouldShowReacjiTriggers` | boolean | 9/9 |

### UnstyledButton

- type: plain · fibers seen: 3624 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<button>`, `<div>` · classes: `c-button-unstyled` `sectionbutton__CadG9` `p-tab_rail__button` `c-tabs__tab` `js-tab` `c-tabs__tab--full_width` `p-channel_sidebar__close` `c-icon_button` `c-icon_button--default` `ellipsis__Lwi05` `c-icon_button--size_medium` `p-ia4_history_buttons__button` `p-autoclog__hook` `p-home_header_controls__composer_button`
- data-qa: `channel_item_close` `unstyled-button` `history_back_button` `history_forward_button` `top-nav-history-menu` `top_nav_search` `top_nav_search_clear` `highlight-btn`

| prop | shape | seen |
|---|---|---|
| `aria-busy?` | undefined | 304/722 |
| `aria-checked?` | undefined | 90/722 |
| `aria-controls?` | string | 13/722 |
| `aria-describedby?` | string | 325/722 |
| `aria-disabled?` | boolean | 350/722 |
| `aria-expanded?` | boolean \| string | 44/722 |
| `aria-haspopup?` | string \| boolean | 291/722 |
| `aria-hidden?` | boolean \| string | 88/722 |
| `aria-keyshortcuts?` | string | 52/722 |
| `aria-label?` | string | 604/722 |
| `aria-labelledby?` | string | 10/722 |
| `aria-pressed?` | boolean \| string | 128/722 |
| `aria-selected?` | boolean | 102/722 |
| `autoClogProps?` | {onClick?: {enableClogAction: boolean, interaction?: number}, elementName?: string \| number, elementType?: number, eventId?: number, appId?: string, uiComponentName?: string, elementValue?: string, channelId?: string, uiComponentVariant?: string, uiStep?: string, uiPage?: string, stepVariant?: string, clogImpression?: boolean, botId?: string, disableAutoReferringUiContext?: boolean, isCopilot?: boolean, isPrimaryCTA?: boolean, action?: number, setElementValueOnAction?: boolean, view_type?: string, uiElement?: number, elementIndex?: number, interactionElement?: number} | 287/722 |
| `checked?` | undefined | 2/722 |
| `className?` | string | 721/722 |
| `data-feat?` | string | 186/722 |
| `data-focus-key?` | string | 3/722 |
| `data-focus-metadata?` | string | 3/722 |
| `data-format?` | string | 96/722 |
| `data-qa?` | string | 617/722 |
| `data-qa-expandable-trigger-is-expanded?` | boolean | 2/722 |
| `data-qa-expandable-trigger-key?` | string | 2/722 |
| `data-rbd-drag-handle-context-id?` | string | 6/722 |
| `data-rbd-drag-handle-draggable-id?` | string | 6/722 |
| `data-rbd-draggable-context-id?` | string | 6/722 |
| `data-rbd-draggable-id?` | string | 6/722 |
| `data-sk?` | string | 393/722 |
| `data-stringify-ignore?` | undefined | 39/722 |
| `data-team?` | string | 6/722 |
| `data-team-active?` | boolean | 6/722 |
| `disabled?` | boolean | 414/722 |
| `download?` | undefined | 2/722 |
| `draggable?` | boolean | 6/722 |
| `href?` | undefined | 32/722 |
| `id?` | string | 103/722 |
| `onBlur?` | fn | 439/722 |
| `onClick?` | fn | 694/722 |
| `onContextMenu?` | fn | 102/722 |
| `onDoubleClick?` | fn | 11/722 |
| `onDragStart?` | fn | 6/722 |
| `onFocus?` | fn | 404/722 |
| `onKeyDown?` | fn | 397/722 |
| `onKeyPress?` | fn | 8/722 |
| `onMouseDown?` | fn | 52/722 |
| `onMouseEnter?` | fn | 428/722 |
| `onMouseLeave?` | fn | 428/722 |
| `onMouseOut?` | fn | 101/722 |
| `onMouseOver?` | fn | 101/722 |
| `onTransitionEnd?` | null | 6/722 |
| `ref?` | null \| ref \| fn | 472/722 |
| `role?` | string ("tab"|"button"|"menuitem") | 197/722 |
| `style?` | {width?: string, height?: string, transform?: null, transition?: null, minWidth?: string} | 46/722 |
| `tabIndex?` | number | 417/722 |
| `target?` | undefined | 2/722 |
| `title?` | string | 2/722 |
| `children` | node \| array<node \| boolean \| array<node> \| null \| string> \| string | 722/722 |

### ThreadSearchButton

- type: plain · fibers seen: 15 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `isThreadTypeSupported` | boolean | 15/15 |
| `threadKey` | string | 15/15 |
| `windowId` | string | 15/15 |

### ThreadTypingIndicator

- type: plain · fibers seen: 15 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 15/15 |
| `suppressBotTyping` | boolean | 15/15 |
| `threadTs` | string | 15/15 |

### ThreadFlexpaneAdminTools

- type: plain · fibers seen: 15 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 15/15 |
| `isLockedThread` | boolean | 15/15 |
| `maxMarkableTs` | string | 15/15 |
| `threadTs` | string | 15/15 |

### MessagePaneInput

- type: plain · fibers seen: 31 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-message_pane_input`
- data-qa: `message_input_container`

_no props recorded_

### MessageInput

- type: plain · fibers seen: 46 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-message_input_unstyled` `p-message_input_unstyled--attachments-visible` `p-workspace__input` `p-threads_footer__input`
- data-qa: —

_no props recorded_

### InputContainer

- type: memo · fibers seen: 30 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: `<div>` · classes: `p-threads_footer__input_container` `p-threads_footer__input_container--sticky_composer`
- data-qa: `reply_container`

_no props recorded_

### WysiwygContainer

- type: plain · fibers seen: 46 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-message_input__input_container_unstyled` `c-wysiwyg_container` `c-wysiwyg_container--theme_light` `c-wysiwyg_container--with_footer` `c-wysiwyg_container--theme_light_bordered` `c-basic_container` `c-basic_container--size_medium` `c-wysiwyg_container--input_focused` `c-wysiwyg_container--double_decker`
- data-qa: —

_no props recorded_

### TextyButtons

- type: plain · fibers seen: 46 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `c-texty_buttons`
- data-qa: —

_no props recorded_

### ShortcutsMenuTriggerButton

- type: plain · fibers seen: 46 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-shortcuts_menu_trigger_button_container--composer_ia` `p-shortcuts_menu_trigger_button_container_unstyled` `p-shortcuts_menu_trigger_button_container_unstyled--composer_ia`
- data-qa: `shortcuts_menu_trigger_container`

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 46/46 |
| `draftId` | string | 46/46 |
| `forceFileMenuOpen` | boolean | 46/46 |
| `forceOpen` | boolean | 46/46 |
| `isComposerPage` | boolean | 46/46 |
| `isDisabled` | boolean | 46/46 |
| `isEditMessageShortcutsButton` | boolean | 46/46 |
| `limitShortcutsMenuToFilesOnly` | boolean | 46/46 |
| `limitShortcutsMenuToFileUpload` | boolean | 46/46 |
| `onFileMenuClose` | fn | 46/46 |
| `onFilesSelected` | fn | 46/46 |
| `onGifSelected` | fn | 46/46 |
| `refToForward` | null | 46/46 |
| `textyRef` | ref | 46/46 |
| `threadTs?` | string | 46/46 |
| `viewContext` | string | 46/46 |
| `windowId` | string | 46/46 |

### EmojiPicker

- type: plain · fibers seen: 2 · states: emoji-picker-composer, emoji-picker-reaction
- outer host: `<div>` · classes: `p-emoji_picker` `c-menu` `p-emoji_picker--style-v2` `p-emoji_picker--with_gif_picker`
- data-qa: `emoji-picker`

_no props recorded_

### EmojiPickerPopover

- type: plain · fibers seen: 1 · states: emoji-picker-reaction
- outer host: `<div>` · classes: `ReactModal__Overlay` `ReactModal__Overlay--after-open` `c-popover` `c-popover--z_above_fs`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 1/1 |
| `messageContainerType` | string | 1/1 |
| `onClose` | fn | 1/1 |
| `onOpen` | fn | 1/1 |
| `targetBounds` | {bottom: number, height: number, left: number, right: number, top: number, width: number} | 1/1 |
| `threadTs` | string | 1/1 |
| `ts` | string | 1/1 |

### ActivityItem

- type: plain · fibers seen: 64 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<div>` · classes: `c-message_kit__hover` `activity_row_content_margin__JI87w` `activity_row_content_margin_full_width__SYtD7` `c-message_kit__hover--hovered`
- data-qa: `activity-item-container`

| prop | shape | seen |
|---|---|---|
| `activityItem` | {aiPriorityRecommenderSession?: undefined, channelId: string, feedTs: string, isArchived: boolean, isPriority: boolean, isSalesforceChannel?: undefined, isUnread: boolean, key: string, messageTs: string, snoozedUntil?: undefined, threadTs?: string, ts: string, type: string ("at_user"|"thread_v2"|"message_reaction"|"bot_dm_bundle"), unreadCount?: number, reactionName?: string, reactionUser?: string, latestTs?: string, minUnreadTs?: undefined} | 64/64 |
| `activityItemIndex` | number | 64/64 |
| `className` | string | 64/64 |
| `density` | string ("detailed") | 64/64 |
| `inTriageMode` | boolean | 64/64 |
| `isRead` | boolean | 64/64 |
| `isSelected?` | undefined | 64/64 |
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: array<object>, blocks: array<object>, channel: string, crm_signal?: undefined, files?: undefined, language?: undefined, latest_reply?: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users?: array<string>, reply_users_count?: number, slackbot_feels: null, subtype?: string ("bot_message"), text: string, thread_ts?: string, ts: string, type: string ("message"), user?: string, client_msg_id?: string, bot_id?: string, bot_profile?: {app_id: string, deleted: boolean, icons: object, id: string, name: string, team_id: string, updated: number, user_id: string}, username?: string, id?: string, version?: string, icons?: {image_48: string}, source_team_id?: string, …+5} | 64/64 |
| `onClick` | fn | 64/64 |
| `parentId` | string | 64/64 |
| `showAsCard` | boolean | 64/64 |
| `userMessagesTheme` | string | 64/64 |
| `children` | node | 64/64 |

### ActivityPrivacyBlurredName

- type: plain · fibers seen: 100 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<span>` · classes: `activity_row_content__destination_channel__NpfGf` `p-activity_page__item__link` `p-activity_ia2022_page__item__link` `p-activity_page__item__link--no-underline` `c-inline_channel_entity` `c-base_inline_entity` `c-base_inline_entity--has_avatar` `c-message__sender` `c-message_kit__sender`
- data-qa: `inline_channel_entity` `message_sender`

| prop | shape | seen |
|---|---|---|
| `activityItem` | {aiPriorityRecommenderSession?: undefined, channelId: string, feedTs: string, isArchived: boolean, isPriority: boolean, isSalesforceChannel?: undefined, isUnread: boolean, key: string, messageTs: string, snoozedUntil?: undefined, threadTs?: string, ts: string, type: string ("at_user"|"thread_v2"|"message_reaction"|"bot_dm_bundle"), unreadCount?: number, reactionName?: string, reactionUser?: string, latestTs?: string, minUnreadTs?: undefined} | 100/100 |
| `children` | node | 100/100 |

### ActivityRowContentSenderAvatarLayout

- type: plain · fibers seen: 64 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<div>` · classes: `activity_row_content__avatar_and_icon__FvjNh` `activity_row_content__avatar_and_icon_small__G0gET`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `activityItem` | {aiPriorityRecommenderSession?: undefined, channelId: string, feedTs: string, isArchived: boolean, isPriority: boolean, isSalesforceChannel?: undefined, isUnread: boolean, key: string, messageTs: string, snoozedUntil?: undefined, threadTs?: string, ts: string, type: string ("at_user"|"thread_v2"|"message_reaction"|"bot_dm_bundle"), unreadCount?: number, reactionName?: string, reactionUser?: string, latestTs?: string, minUnreadTs?: undefined} | 64/64 |
| `density` | string ("dense") | 64/64 |
| `size` | string ("small") | 64/64 |
| `children` | node | 64/64 |

### MemberProfileHoverCard

- type: plain · fibers seen: 1 · states: profile-hover
- outer host: `<div>` · classes: `p-autoclog__hook`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `closeProfileCard` | fn | 1/1 |
| `disableMemberProfileLink` | boolean | 1/1 |
| `fileId?` | undefined | 1/1 |
| `hideButtons` | boolean | 1/1 |
| `isCanvasContext?` | undefined | 1/1 |
| `isListContext?` | undefined | 1/1 |
| `memberId` | string | 1/1 |
| `shouldStopPropagation` | boolean | 1/1 |

### SvgIcon

- type: memo · fibers seen: 4294 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<svg>` · classes: `is-inline`
- data-qa: `close` `caret-down` `channel-section-collapse` `ellipsis-vertical-filled` `arrow-left` `arrow-right` `clock` `search`

| prop | shape | seen |
|---|---|---|
| `aria-hidden?` | string \| boolean | 25/789 |
| `aria-label?` | string | 25/789 |
| `color?` | string | 1/789 |
| `data-qa?` | string | 88/789 |
| `data-qa-presence-active?` | boolean | 18/789 |
| `data-qa-presence-dnd?` | boolean | 18/789 |
| `data-qa-presence-self?` | boolean | 18/789 |
| `data-qa-type?` | string | 18/789 |
| `data-sidebar-channel-icon?` | string | 12/789 |
| `inline?` | boolean | 151/789 |
| `name` | string | 789/789 |
| `size?` | string ("inherit") | 459/789 |
| `title?` | string | 18/789 |

### Button

- type: forwardRef · fibers seen: 75 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<button>` · classes: `c-button` `c-button--outline` `c-button--small` `p-view_header_notifications_action_button` `p-view_header_notifications_action_button--polish` `p-activity_ia4_page__bulk_select_menu_container` `p-refine_button` `p-about_modal__section_picker` `p-about_modal__action_button--disabled` `p-member_profile_buttons__button` `p-emoji_picker__add_custom_emoji` `p-channel_details__slack_connect_ctas_button` `padding_top_25` `padding_bottom_25`
- data-qa: `control_button` `channel-header-star-button` `call` `customize_emoji_add_button` `member_profile_message_btn` `member_profile_huddle_btn` `member_profile_vip_btn` `apps-agents-section__learn_more_cta`

| prop | shape | seen |
|---|---|---|
| `aria-describedby?` | string | 43/75 |
| `aria-disabled?` | boolean | 6/75 |
| `aria-haspopup?` | string | 58/75 |
| `aria-label?` | string | 67/75 |
| `aria-pressed?` | undefined | 37/75 |
| `autoClogProps?` | {onClick: {enableClogAction: boolean, interaction?: number}, elementName?: string, eventId?: number, action?: number, entrypoint?: string, uiComponentVariant?: string, uiStep?: string, profileId?: string, uiComponentName?: string, entryPoint?: string} | 57/75 |
| `autoFocus?` | boolean | 7/75 |
| `className?` | string | 71/75 |
| `data-feat?` | string | 37/75 |
| `data-qa?` | string | 59/75 |
| `data-sk?` | string | 51/75 |
| `disabled?` | boolean | 10/75 |
| `emoji-type?` | string | 2/75 |
| `id?` | string | 7/75 |
| `onBlur?` | fn | 51/75 |
| `onClick?` | fn | 74/75 |
| `onFocus?` | fn | 51/75 |
| `onKeyDown?` | fn | 58/75 |
| `onMouseEnter?` | fn | 51/75 |
| `onMouseLeave?` | fn | 51/75 |
| `ref?` | null \| ref | 48/75 |
| `size` | string ("small"|"medium") | 75/75 |
| `type` | string ("outline"|"primary") | 75/75 |
| `visibleAutoFocus?` | boolean | 7/75 |
| `children` | array<node \| string \| boolean> \| node \| string | 75/75 |

### Tooltip

- type: plain · fibers seen: 3483 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<button>`, `<span>` · classes: `c-button-unstyled` `p-channel_sidebar__name` `sectionbutton__CadG9` `c-icon_button` `c-icon_button--size_medium` `c-icon_button--default` `overflowEllipsis__HJDrl` `ellipsis__Lwi05` `p-ia4_history_buttons__button` `p-home_header_controls__composer_button` `viewHeaderActionButton__d6GTR` `plusButton__W0iNT` `p-ia4_history_buttons__button-back` `p-ia4_history_buttons__button-forward`
- data-qa: `history_back_button` `history_forward_button` `top-nav-history-menu` `top_nav_search_clear` `unstyled-button` `highlight-btn` `team_sidebar_item` `add-workspace-btn`

| prop | shape | seen |
|---|---|---|
| `aria-haspopup?` | string | 117/708 |
| `className?` | string | 146/708 |
| `delay?` | number | 463/708 |
| `hideFromScreenReader?` | boolean | 132/708 |
| `maxWidth?` | string | 61/708 |
| `offsetX?` | number | 10/708 |
| `offsetY?` | number | 154/708 |
| `onClick?` | fn | 136/708 |
| `onContextMenu?` | fn | 10/708 |
| `onKeyDown?` | fn | 2/708 |
| `onShow?` | fn | 48/708 |
| `position?` | string ("bottom"|"bottom-left"|"bottom-right"|"right"|"top"|"top-right"|"top-left") | 585/708 |
| `ref?` | undefined | 8/708 |
| `shouldObserveTruncation?` | boolean | 17/708 |
| `shouldOnlyShowWhenTruncated?` | boolean | 31/708 |
| `tip?` | string \| node \| fn | 708/708 |
| `tipAnchorRef?` | undefined | 17/708 |
| `tipClassName?` | string | 110/708 |
| `zIndex?` | string | 199/708 |
| `children` | node | 708/708 |

### IconButtonBase

- type: plain · fibers seen: 1614 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<button>`, `<a>` · classes: `c-button-unstyled` `c-icon_button` `c-icon_button--default` `c-icon_button--size_smedium` `p-composer__button` `p-composer__button--composer_ia` `p-composer__selection_button` `p-composer__button--sticky` `c-icon_button--size_medium` `c-wysiwyg_container__button` `p-home_header_controls__composer_button` `viewHeaderActionButton__d6GTR` `c-icon_button--size_small` `p-team_sidebar__icon_button`
- data-qa: `add-workspace-btn` `sweeper_button` `composer_button` `sidebar_menu_onboarding_tasks_link_close_button` `huddle_channel_header_button__start_button` `huddle_channel_header_button` `search_in_channel_button` `unstyled-button`

| prop | shape | seen |
|---|---|---|
| `aria-busy?` | undefined | 2/521 |
| `aria-checked?` | undefined | 170/521 |
| `aria-describedby?` | string | 263/521 |
| `aria-disabled?` | boolean | 257/521 |
| `aria-expanded?` | boolean \| string | 56/521 |
| `aria-haspopup?` | string | 131/521 |
| `aria-hidden?` | boolean | 14/521 |
| `aria-keyshortcuts?` | string | 23/521 |
| `aria-label` | string | 521/521 |
| `aria-pressed?` | boolean \| string | 212/521 |
| `autoClogProps?` | {onClick: {enableClogAction: boolean, interaction?: number}, elementName?: string \| number, eventId?: number, elementType?: number, uiPage?: string, action?: number, setElementValueOnAction?: boolean, disableAutoReferringUiContext?: boolean, uiComponentName?: string, stepVariant?: string, interactionElement?: number, uiElement?: number} | 76/521 |
| `backgroundStyle?` | string | 20/521 |
| `className?` | string | 518/521 |
| `data-feat?` | string | 43/521 |
| `data-focus-key?` | string | 1/521 |
| `data-focus-metadata?` | string | 1/521 |
| `data-format?` | string | 184/521 |
| `data-qa?` | string | 492/521 |
| `data-sk?` | string | 434/521 |
| `disabled?` | boolean | 36/521 |
| `href?` | string | 25/521 |
| `icon?` | string | 189/521 |
| `onBlur?` | fn | 434/521 |
| `onClick?` | fn | 521/521 |
| `onDoubleClick?` | fn | 16/521 |
| `onFocus?` | fn | 434/521 |
| `onKeyDown?` | fn | 434/521 |
| `onMouseDown?` | fn | 17/521 |
| `onMouseEnter?` | fn | 434/521 |
| `onMouseLeave?` | fn | 434/521 |
| `ref?` | null \| ref | 256/521 |
| `role?` | string ("button") | 170/521 |
| `size?` | string ("medium"|"small"|"smedium") | 428/521 |
| `tabIndex?` | number | 338/521 |
| `children?` | array<node \| boolean> \| node | 332/521 |

### Label

- type: plain · fibers seen: 27 · states: activity, activity-menu-Filters, activity-menu-Settings, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: `<label>` · classes: `c-label` `c-label--inline` `c-label--pointer` `p-threads_footer__input_container__broadcast_controls__label` `c-label--with_formatted_text` `p-activity_ia4_page__checkbox_container` `c-label--block` `c-label--with_subtext`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `className?` | string | 22/27 |
| `cursor?` | string | 1/27 |
| `htmlFor?` | string | 16/27 |
| `onMouseDown?` | fn | 15/27 |
| `role?` | string ("none") | 7/27 |
| `subtext?` | null \| string | 4/27 |
| `text` | node \| string | 27/27 |
| `type?` | string ("inline") | 20/27 |
| `children` | node | 27/27 |

### Checkbox

- type: plain · fibers seen: 33 · states: activity, activity-menu-Filters, activity-menu-Settings, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: `<input>` · classes: `c-input_checkbox` `p-activity_ia4_page__item__checkbox`
- data-qa: `threads_footer_broadcast_checkbox`

| prop | shape | seen |
|---|---|---|
| `aria-describedby?` | string | 22/33 |
| `aria-hidden?` | boolean | 10/33 |
| `aria-label?` | string | 7/33 |
| `checked` | boolean | 33/33 |
| `className?` | string | 7/33 |
| `data-qa?` | string | 15/33 |
| `data-sk?` | string | 15/33 |
| `disabled?` | boolean | 10/33 |
| `id?` | string | 16/33 |
| `indeterminate?` | boolean | 17/33 |
| `name?` | string | 10/33 |
| `onBlur?` | fn | 15/33 |
| `onChange?` | fn | 23/33 |
| `onClick?` | undefined | 15/33 |
| `onFocus?` | fn | 15/33 |
| `onKeyDown?` | fn | 15/33 |
| `onMouseEnter?` | fn | 15/33 |
| `onMouseLeave?` | fn | 15/33 |
| `tabIndex?` | number | 10/33 |

### BasicSelect

- type: plain · fibers seen: 2 · states: channel-details-Members, channel-details-Settings
- outer host: `<div>` · classes: `c-basic-select` `margin_left_50` `p-ia_details_popover__filter` `p-ia_details_popover__filter--default` `manage_channel_tabs_select__sSpQj`
- data-qa: `ia-details-popover-members-filter-select` `basic-select-bea4n7ug`

| prop | shape | seen |
|---|---|---|
| `ariaLabel` | string | 2/2 |
| `autoClogProps?` | {elementName: string, elementValue: string, onSelectionChange: {enableClogAction: boolean}} | 1/2 |
| `className` | string | 2/2 |
| `isDisabled?` | boolean | 1/2 |
| `maxListHeight?` | number | 1/2 |
| `minOptionsListWidth?` | number | 1/2 |
| `onSelectionChange` | fn | 2/2 |
| `options` | array<{label: string, value: string, extras?: object, ariaLabel?: string}> | 2/2 |
| `overlayClassName?` | string | 1/2 |
| `renderOption?` | fn | 1/2 |
| `renderSelectedOption?` | fn | 1/2 |
| `selectDataQa?` | string | 1/2 |
| `selectedOption` | {label: string, value: string, ariaLabel?: string, extras?: {memberCount: number}} | 2/2 |
| `selectId?` | string | 1/2 |
| `width?` | number | 1/2 |

### MenuTrigger

- type: plain · fibers seen: 2361 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<button>`, `<div>` · classes: `c-button-unstyled` `p-channel_sidebar__channel` `p-channel_sidebar__channel--show-avatars` `p-tab_rail__button` `c-tabs__tab` `js-tab` `c-tabs__tab--full_width` `c-icon_button` `p-channel_sidebar__section_heading` `sectionbutton__CadG9` `ellipsis__Lwi05` `c-icon_button--size_medium` `c-icon_button--default` `p-ia4_history_buttons__button`
- data-qa: `channel-sidebar-channel` `canvas` `unstyled-button` `history_back_button` `history_forward_button` `top-nav-history-menu` `team_sidebar_item` `add-workspace-btn`

| prop | shape | seen |
|---|---|---|
| `allowanceX?` | number | 27/596 |
| `allowanceY?` | number | 10/596 |
| `closeOnSelect?` | boolean | 17/596 |
| `data-qa?` | string | 48/596 |
| `forceOpen?` | boolean | 398/596 |
| `isContextMenu?` | boolean | 396/596 |
| `isDisabled?` | boolean | 404/596 |
| `offsetX?` | number | 140/596 |
| `offsetY?` | number | 128/596 |
| `onClick?` | fn | 435/596 |
| `onClose?` | fn | 436/596 |
| `onMenuItemSelected?` | fn | 11/596 |
| `onPopoverClose?` | fn | 89/596 |
| `onPopoverOpen?` | fn | 447/596 |
| `onTriggerClose?` | fn | 5/596 |
| `openOnDownArrow?` | boolean | 1/596 |
| `overlayClassName?` | string | 10/596 |
| `position` | string ("right-bottom"|"bottom-left"|"bottom"|"left-bottom"|"top-left"|"top-right"|"right-top"|"bottom-right") | 596/596 |
| `preventScroll?` | boolean | 381/596 |
| `renderMenu` | fn | 596/596 |
| `shouldCloseOnOverlayClick?` | boolean | 34/596 |
| `shouldFade?` | boolean | 385/596 |
| `startsOpen?` | boolean | 10/596 |
| `suppressDragRegionsWhenOpen?` | boolean | 406/596 |
| `tryOppositePosition?` | boolean | 430/596 |
| `width?` | number | 15/596 |
| `children` | node \| fn | 596/596 |

### MenuFromTemplate

- type: plain · fibers seen: 1 · states: activity-menu-Settings
- outer host: `<div>` · classes: `c-menu`
- data-qa: `menu`

| prop | shape | seen |
|---|---|---|
| `experimentSimpleUnderlineLinkOn` | boolean | 1/1 |
| `experimentSlackKitCarouselGroupOn` | boolean | 1/1 |
| `isOpen` | boolean | 1/1 |
| `logger` | {action: fn, debug: fn, devHint: fn, error: fn, info: fn, teamOrEnterpriseId: string, timeStamp: fn, warn: fn} | 1/1 |
| `onClose` | fn | 1/1 |
| `onMenuItemSelected` | fn | 1/1 |
| `onMenuKeyDown?` | undefined | 1/1 |
| `onTriggerClose` | fn | 1/1 |
| `openedWithKeyboard` | boolean | 1/1 |
| `position` | string ("bottom-right") | 1/1 |
| `readKeyboardMode` | fn | 1/1 |
| `renderPopover` | fn | 1/1 |
| `shouldCloseOnOverlayMouseDown` | boolean | 1/1 |
| `shouldSetTargetBoundsForContextMenu` | boolean | 1/1 |
| `slackKitConfig` | {enable24hrTime: boolean, locale: string, managedFocusContainer: {Component: object, coachmarkSectionIndex: number, workspaceSwitcherTabListSectionIndex: number}, modalHooks: {closeModal: fn}, popoverHooks: {decrementOpenCount: fn, incrementOpenCount: fn}, restoreFocusFallback: fn, scrollmarkHooks: {addScrollmarks: fn, getScrollmark: fn, removeScrollmarks: fn}, telemeter: Telemeter {endpointUrl: string, enterpriseId: string, recordedMetrics: object, sessionId: string, subAppName: string, teamId: string, teamSize?: undefined, userId: string, versionTs: string}, timeZone: string} | 1/1 |
| `stripTerminalSeparators` | boolean | 1/1 |
| `targetBounds` | {bottom: number, height: number, left: number, right: number, top: number, width: number} | 1/1 |
| `template` | array<{key: string, type: string ("separator")}> | 1/1 |
| `triggerProps` | {aria-haspopup: string} | 1/1 |
| `useTargetBoundsAsPosition` | boolean | 1/1 |
| `children` | node | 1/1 |

### ContextMenuTrigger

- type: plain · fibers seen: 1495 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>`, `<button>` · classes: `c-button-unstyled` `p-member_profile_hover_card` `c-tabs__tab` `js-tab` `p-channel_sidebar__channel` `p-channel_sidebar__channel--show-avatars` `p-tab_rail__button` `c-tabs__tab--full_width` `channel_tab__B1pww` `channel_tab_draggable__blPuX` `p-channel_sidebar__section_heading` `wrapper__nq4fb` `p-team_sidebar__item` `p-team_sidebar__item--active`
- data-qa: `channel-sidebar-channel` `canvas` `team_sidebar_item` `tab_rail_dms_button` `tab_rail_activity_button` `tab_rail_files_button` `tab_rail_later_button` `tabs_item`

| prop | shape | seen |
|---|---|---|
| `desktopAppOnly?` | boolean | 501/509 |
| `forceOpen?` | boolean | 11/509 |
| `getTemplate?` | fn | 383/509 |
| `menuAriaLabel?` | string | 127/509 |
| `onClick?` | fn | 146/509 |
| `onClose?` | fn | 56/509 |
| `onContextMenuOpen?` | fn | 126/509 |
| `overrideDefaultMenu?` | boolean | 158/509 |
| `readKeyboardMode` | fn | 509/509 |
| `renderMenu?` | fn | 126/509 |
| `showSlackKitContextMenuOnDesktop?` | boolean | 489/509 |
| `width?` | string | 362/509 |
| `children` | node \| fn | 509/509 |

### Popover

- type: plain · fibers seen: 724 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `ReactModal__Overlay` `ReactModal__Overlay--after-open` `c-popover` `c-popover--z_above_fs` `c-popover--fade` `c-popover--no-pointer` `c-popover--z_menu`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `allowanceX?` | number | 53/432 |
| `allowanceY?` | number | 24/432 |
| `ariaHideApp?` | boolean | 408/432 |
| `ariaLabel?` | string | 400/432 |
| `ariaLabelledBy?` | undefined | 22/432 |
| `ariaModal?` | undefined | 378/432 |
| `ariaRole?` | string | 401/432 |
| `contentStyle?` | object | 30/432 |
| `customTrigger?` | boolean | 1/432 |
| `dataAttributes?` | {qa: string} | 1/432 |
| `experimentSimpleUnderlineLinkOn?` | boolean | 2/432 |
| `experimentSlackKitCarouselGroupOn?` | boolean | 2/432 |
| `forceOpen?` | boolean | 2/432 |
| `id?` | string | 1/432 |
| `isContentInteractive?` | boolean | 378/432 |
| `isOpen` | boolean | 432/432 |
| `isOverlayInteractive?` | boolean | 379/432 |
| `logger?` | {action: fn, debug: fn, devHint: fn, error: fn, info: fn, teamOrEnterpriseId: string, timeStamp: fn, warn: fn} | 2/432 |
| `offsetX?` | number | 31/432 |
| `offsetY?` | number | 410/432 |
| `onClick?` | fn | 2/432 |
| `onClose?` | fn | 431/432 |
| `onOpen?` | fn | 22/432 |
| `openedWithKeyboard?` | boolean | 2/432 |
| `openOnDownArrow?` | boolean | 1/432 |
| `overlayClassName?` | string | 50/432 |
| `overlayStyle?` | object | 379/432 |
| `position` | string ("top-left"|"top"|"bottom-left") | 432/432 |
| `readKeyboardMode?` | fn | 2/432 |
| `ref?` | undefined | 432/432 |
| `renderPopover?` | fn | 2/432 |
| `shouldCloseOnEsc?` | boolean | 21/432 |
| `shouldCloseOnOverlayClick?` | boolean | 1/432 |
| `shouldCloseOnOverlayMouseDown?` | boolean | 32/432 |
| `shouldFade?` | boolean | 431/432 |
| `shouldFocusAfterRender?` | boolean | 430/432 |
| `shouldObserveResize?` | boolean | 29/432 |
| `shouldPropagateOnClose?` | boolean | 29/432 |
| `shouldReturnFocusAfterClose?` | boolean | 24/432 |
| `shouldSetTargetBoundsForContextMenu?` | boolean | 2/432 |
| `shouldTrackOffsetChange?` | boolean | 1/432 |
| `slackKitConfig?` | {enable24hrTime: boolean, locale: string, managedFocusContainer: {Component: object, coachmarkSectionIndex: number, workspaceSwitcherTabListSectionIndex: number}, modalHooks: {closeModal: fn}, popoverHooks: {decrementOpenCount: fn, incrementOpenCount: fn}, restoreFocusFallback: fn, scrollmarkHooks: {addScrollmarks: fn, getScrollmark: fn, removeScrollmarks: fn}, telemeter: Telemeter {endpointUrl: string, enterpriseId: string, recordedMetrics: object, sessionId: string, subAppName: string, teamId: string, teamSize?: undefined, userId: string, versionTs: string}, timeZone: string} | 2/432 |
| `startsOpen?` | boolean | 2/432 |
| `stickyPosition?` | undefined | 29/432 |
| `suppressDragRegionsWhenOpen?` | boolean | 21/432 |
| `targetBounds?` | {bottom: number, height: number, left: number, right: number, top: number, width: number} | 411/432 |
| `tryOppositePosition?` | boolean | 410/432 |
| `useTargetBoundsAsPosition?` | boolean | 3/432 |
| `windowRef` | WeakRef | 432/432 |
| `zIndex?` | string | 408/432 |
| `children` | node \| fn | 432/432 |

### MrkdwnElement

- type: plain · fibers seen: 17 · states: channel, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: `<div>` · classes: `p-mrkdwn_element` `p-context_block__text`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `appId?` | undefined | 2/17 |
| `blockId` | string | 17/17 |
| `blockKitClogger` | fn | 17/17 |
| `blockKitLogPayload` | {blockKitAppId?: string, blockKitBotId?: string, surfaceId: string, surfaceType: number, uuid: string} | 17/17 |
| `blocksContainerContext?` | string | 2/17 |
| `channelId?` | string | 2/17 |
| `clogLinkClick?` | fn | 2/17 |
| `containerActionsPayload?` | {appId?: undefined, container: {channelId: string, isEphemeral: boolean, messageTs: string, type: string ("message")}, functionExecutionId?: undefined, serviceId?: undefined, serviceTeamId?: undefined} | 2/17 |
| `containerId` | string | 17/17 |
| `data-qa?` | string | 2/17 |
| `elementWidth?` | number | 2/17 |
| `focusOnLoad?` | undefined | 17/17 |
| `inline?` | undefined | 2/17 |
| `isDisabled?` | boolean | 2/17 |
| `isSlackbotMessage?` | boolean | 2/17 |
| `messageTs?` | string | 2/17 |
| `noTabbableContent?` | undefined | 2/17 |
| `onAction` | fn | 17/17 |
| `parent` | string | 17/17 |
| `size?` | string ("small") | 2/17 |
| `text` | string | 17/17 |
| `type` | string ("mrkdwn") | 17/17 |
| `verbatim` | boolean | 17/17 |

### ProfileHoverTrigger

- type: plain · fibers seen: 503 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: `p-member_profile_hover_card`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `appId?` | undefined | 16/410 |
| `botProfile?` | {app_id: string, deleted: boolean, icons: {image_36: string, image_48: string, image_72: string}, id: string, name: string, team_id: string, updated: number, user_id: string} | 378/410 |
| `memberId` | string | 410/410 |
| `messageTs?` | string | 186/410 |
| `noStyling?` | boolean | 16/410 |
| `onOpen?` | fn | 16/410 |
| `serviceId?` | string | 394/410 |
| `children` | node | 410/410 |

## 2. Requested targets not rendered live

### %s
### ViewBlocks
_not rendered in any state tried and not a top-level webpack export in loaded chunks_

### MessageListItem (static only, not rendered live)

- export: fn
- props destructured in source: `index`, `result`, `tabIndex`, `className`, `hideAttachments`, `forceExpanded`, `dangerouslyOverrideRouting`, `variant`, `secondaryViewType`, `listItemOverrideProps`

### MessageInputRoadblock (static only, not rendered live)

- export: fn
- props destructured in source: `viewContext`, `roadblockMessage`, `dataQa`, `className`, `roadblockMessageId`, `allowWrap`

### %s
### ListChannelEntity
_not rendered in any state tried and not a top-level webpack export in loaded chunks_

### %s
### BaseMrkdwnChannel
_not rendered in any state tried and not a top-level webpack export in loaded chunks_

### %s
### BaseMessageAttachmentSlackMessage
_not rendered in any state tried and not a top-level webpack export in loaded chunks_

### %s
### MessageAttachmentHuddleLink
_not rendered in any state tried and not a top-level webpack export in loaded chunks_

### %s
### RimetoMemberProfileOverflowMenu
_not rendered in any state tried and not a top-level webpack export in loaded chunks_

### ConfirmationModal (static only, not rendered live)

- export: fn
- props destructured in source: `title`, `titleAlign`, `headerImageSrc`, `headerImageAlt`, `children`, `footerText`, `submitButtonType`, `submitButtonText`, `showCancelButton`, `showSubmitButton`, `isSubmitButtonALoadingButton`, `isSubmitting`, `cancelButtonText`, `onSubmit`, `onCancel`, `onClose`, `className`, `overlayClassName`, `submitAutoClogProps`, `cancelAutoClogProps`, `shouldReturnFocusAfterClose`, `shouldAutoFocusCancel`, `headerImage`, `closeButtonTone`, `disableSubmitButton`

### InlineAlert (static only, not rendered live)

- export: fn
- props destructured in source: `children`, `className`, `id`, `...o`

### FormTextInput (static only, not rendered live)

- export: fn
- props read as `props.x`: `errorId`, `id`, `hintId`, `value`, `inputRef`, `focusOnMount`, `preventScrollOnFocus`, `emojisShouldCountAsSingleCharacter`, `useInlineAlert`, `showError`, `onMouseEnter`, `onMouseLeave`
- defaultProps: `className: null`, `errorId: undefined`, `errorLevel: string(error)`, `errorText: null`, `errorType: string(nested_box)`, `useInlineAlert: boolean`, `hintId: undefined`, `hintText: null`, `isDisabled: boolean`, `isInvalid: boolean`, `isRequired: boolean`, `maxCharacterLimit: null`, `minCharacterLimit: null`, `counterMinimumCharacters: number`, `emojisShouldCountAsSingleCharacter: boolean`, `onBlur: fn`, `onChange: fn`, `onCopy: fn`, `forceSelectAll: boolean`, `readOnly: boolean`, `onFocus: fn`, `placeholder: undefined`, `size: string(medium)`, `prefix: undefined`, `suffix: null`, `value: string`, `icon: undefined`, `focusOnMount: boolean`, `preventScrollOnFocus: boolean`, `showError: boolean`, `inputRef: undefined`, `ariaDescribedById: undefined`, `onMouseEnter: fn`, `onMouseLeave: fn`, `autoComplete: string(off)`, `type: undefined`, `spellCheck: boolean`, `min: number`, `max: number`, `autoCorrect: undefined`, `width: number`, `autoClogProps: undefined`, `autoFocus: boolean`, `role: undefined`, `onPaste: fn`, `onKeyDown: fn`, `onCompositionStart: fn`, `onCompositionEnd: fn`

### FilterPill (static only, not rendered live)

- export: fn
- props destructured in source: `ref`, `className`, `children`, `isActive`, `onClick`, `dataQA`, `id`, `isBold`, `isQuickFilter`, `autoClogProps`, `tabIndex`, `isDisabled`, `contentClassName`, `isThemed`, `isHighlightActive`, `isRounded`, `...b`

### Tag (static only, not rendered live)

- export: fn
- props destructured in source: `children`, `style`, `onClick`, `onKeyDown`, `dataQa`, `isMicro`, `autoClogProps`

### FieldSet (static only, not rendered live)

- export: fn
- props destructured in source: `className`, `children`, `...a`

### Legend (static only, not rendered live)

- export: fn
- props destructured in source: `ref`, `className`, `children`, `id`, `...s`

### Hint (static only, not rendered live)

- export: fn
- props destructured in source: `children`, `className`, `hideExternalSpacing`, `id`

### TimePicker (static only, not rendered live)

- export: fn
- props destructured in source: `allowCustomInput`, `autoClogProps`, `isDisabled`, `isInvalid`, `isRequired`, `max`, `min`, `onChange`, `onBlur`, `onEsc`, `renderOption`, `onOptionsListClose`, `optionsHourIncrement`, `placeholderText`, `selectId`, `showTimeZone`, `size`, `timeZone`, `value`, `timeZoneClassName`, `selectClassName`, `...O`

## 3. Top other components by frequency

### SvgIconPrimitive

- type: forwardRef · fibers seen: 4294 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<svg>` · classes: `is-inline`
- data-qa: `close` `caret-down` `channel-section-collapse` `ellipsis-vertical-filled` `arrow-left` `arrow-right` `clock` `search`

| prop | shape | seen |
|---|---|---|
| `aria-hidden?` | string \| boolean | 25/789 |
| `aria-label?` | string | 25/789 |
| `color?` | string | 1/789 |
| `data-qa?` | string | 88/789 |
| `data-qa-presence-active?` | boolean | 18/789 |
| `data-qa-presence-dnd?` | boolean | 18/789 |
| `data-qa-presence-self?` | boolean | 18/789 |
| `data-qa-type?` | string | 18/789 |
| `data-sidebar-channel-icon?` | string | 12/789 |
| `inline?` | boolean | 151/789 |
| `name` | string | 789/789 |
| `size?` | string ("inherit") | 459/789 |
| `title?` | string | 18/789 |

### BaseButton

- type: forwardRef · fibers seen: 3699 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<button>`, `<div>` · classes: `c-button-unstyled` `sectionbutton__CadG9` `p-tab_rail__button` `c-tabs__tab` `js-tab` `c-tabs__tab--full_width` `p-channel_sidebar__close` `c-icon_button` `c-icon_button--default` `ellipsis__Lwi05` `c-icon_button--size_medium` `p-ia4_history_buttons__button` `p-autoclog__hook` `p-home_header_controls__composer_button`
- data-qa: `channel_item_close` `unstyled-button` `history_back_button` `history_forward_button` `top-nav-history-menu` `top_nav_search` `top_nav_search_clear` `highlight-btn`

| prop | shape | seen |
|---|---|---|
| `aria-busy?` | undefined | 285/729 |
| `aria-checked?` | undefined | 92/729 |
| `aria-controls?` | string | 15/729 |
| `aria-describedby?` | string | 333/729 |
| `aria-disabled?` | boolean | 327/729 |
| `aria-expanded?` | boolean \| string | 51/729 |
| `aria-haspopup?` | string \| boolean | 305/729 |
| `aria-hidden?` | string \| boolean | 88/729 |
| `aria-keyshortcuts?` | string | 39/729 |
| `aria-label?` | string | 613/729 |
| `aria-labelledby?` | string | 8/729 |
| `aria-pressed?` | boolean \| string | 135/729 |
| `aria-selected?` | boolean | 108/729 |
| `autoClogProps?` | {onClick?: {enableClogAction: boolean, interaction?: number}, elementName?: string \| number, elementType?: number, eventId?: number, uiComponentName?: string, elementValue?: string, appId?: string, uiComponentVariant?: string, channelId?: string, uiStep?: string, uiPage?: string, action?: number, clogImpression?: boolean, disableAutoReferringUiContext?: boolean, stepVariant?: string, botId?: string, isCopilot?: boolean, isPrimaryCTA?: boolean, setElementValueOnAction?: boolean, entrypoint?: string, view_type?: string, elementIndex?: number, uiElement?: number, interactionElement?: number, profileId?: string} | 303/729 |
| `autoFocus?` | boolean | 1/729 |
| `checked?` | undefined | 1/729 |
| `className` | string | 729/729 |
| `data-feat?` | string | 201/729 |
| `data-focus-key?` | string | 1/729 |
| `data-focus-metadata?` | string | 1/729 |
| `data-format?` | string | 98/729 |
| `data-qa?` | string | 727/729 |
| `data-qa-expandable-trigger-is-expanded?` | boolean | 2/729 |
| `data-qa-expandable-trigger-key?` | string | 2/729 |
| `data-rbd-drag-handle-context-id?` | string | 9/729 |
| `data-rbd-drag-handle-draggable-id?` | string | 9/729 |
| `data-rbd-draggable-context-id?` | string | 9/729 |
| `data-rbd-draggable-id?` | string | 9/729 |
| `data-sk?` | string | 408/729 |
| `data-stringify-ignore?` | undefined | 43/729 |
| `data-team?` | string | 9/729 |
| `data-team-active?` | boolean | 9/729 |
| `disabled?` | boolean | 415/729 |
| `download?` | undefined | 1/729 |
| `draggable?` | boolean | 9/729 |
| `href?` | undefined | 26/729 |
| `id?` | string | 107/729 |
| `onBlur?` | fn | 458/729 |
| `onClick?` | fn | 707/729 |
| `onContextMenu?` | fn | 110/729 |
| `onDoubleClick?` | fn | 7/729 |
| `onDragStart?` | fn | 9/729 |
| `onFocus?` | fn | 415/729 |
| `onKeyDown?` | fn | 410/729 |
| `onKeyPress?` | fn | 7/729 |
| `onMouseDown?` | fn | 58/729 |
| `onMouseEnter?` | fn | 451/729 |
| `onMouseLeave?` | fn | 451/729 |
| `onMouseOut?` | fn | 105/729 |
| `onMouseOver?` | fn | 105/729 |
| `onTransitionEnd?` | null | 9/729 |
| `ref?` | null \| ref \| fn | 729/729 |
| `role?` | string ("tab"|"button"|"menuitem") | 206/729 |
| `size?` | string ("small") | 1/729 |
| `style?` | {width?: string, height?: string, transform?: null, transition?: null, minWidth?: string} | 54/729 |
| `tabIndex?` | number | 426/729 |
| `target?` | undefined | 1/729 |
| `title?` | string | 2/729 |
| `type?` | string ("outline") | 1/729 |
| `visibleAutoFocus?` | boolean | 1/729 |
| `children?` | node \| array<node \| boolean \| string \| null \| array<node>> \| string | 727/729 |

### BaseButtonInternal

- type: plain · fibers seen: 3699 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<button>`, `<div>` · classes: `c-button-unstyled` `sectionbutton__CadG9` `p-tab_rail__button` `c-tabs__tab` `js-tab` `c-tabs__tab--full_width` `p-channel_sidebar__close` `c-icon_button` `c-icon_button--default` `ellipsis__Lwi05` `c-icon_button--size_medium` `p-ia4_history_buttons__button` `p-autoclog__hook` `p-home_header_controls__composer_button`
- data-qa: `channel_item_close` `unstyled-button` `history_back_button` `history_forward_button` `top-nav-history-menu` `top_nav_search` `top_nav_search_clear` `highlight-btn`

| prop | shape | seen |
|---|---|---|
| `aria-busy?` | undefined | 285/729 |
| `aria-checked?` | undefined | 92/729 |
| `aria-controls?` | string | 15/729 |
| `aria-describedby?` | string | 333/729 |
| `aria-disabled?` | boolean | 327/729 |
| `aria-expanded?` | boolean \| string | 51/729 |
| `aria-haspopup?` | string \| boolean | 305/729 |
| `aria-hidden?` | string \| boolean | 88/729 |
| `aria-keyshortcuts?` | string | 39/729 |
| `aria-label?` | string | 613/729 |
| `aria-labelledby?` | string | 8/729 |
| `aria-pressed?` | boolean \| string | 135/729 |
| `aria-selected?` | boolean | 108/729 |
| `autoClogProps?` | {onClick?: {enableClogAction: boolean, interaction?: number}, elementName?: string \| number, elementType?: number, eventId?: number, uiComponentName?: string, elementValue?: string, appId?: string, uiComponentVariant?: string, channelId?: string, uiStep?: string, uiPage?: string, action?: number, clogImpression?: boolean, disableAutoReferringUiContext?: boolean, stepVariant?: string, botId?: string, isCopilot?: boolean, isPrimaryCTA?: boolean, setElementValueOnAction?: boolean, entrypoint?: string, view_type?: string, elementIndex?: number, uiElement?: number, interactionElement?: number, profileId?: string} | 303/729 |
| `autoFocus?` | boolean | 1/729 |
| `checked?` | undefined | 1/729 |
| `className` | string | 729/729 |
| `data-feat?` | string | 201/729 |
| `data-focus-key?` | string | 1/729 |
| `data-focus-metadata?` | string | 1/729 |
| `data-format?` | string | 98/729 |
| `data-qa?` | string | 727/729 |
| `data-qa-expandable-trigger-is-expanded?` | boolean | 2/729 |
| `data-qa-expandable-trigger-key?` | string | 2/729 |
| `data-rbd-drag-handle-context-id?` | string | 9/729 |
| `data-rbd-drag-handle-draggable-id?` | string | 9/729 |
| `data-rbd-draggable-context-id?` | string | 9/729 |
| `data-rbd-draggable-id?` | string | 9/729 |
| `data-sk?` | string | 408/729 |
| `data-stringify-ignore?` | undefined | 43/729 |
| `data-team?` | string | 9/729 |
| `data-team-active?` | boolean | 9/729 |
| `disabled?` | boolean | 415/729 |
| `download?` | undefined | 1/729 |
| `draggable?` | boolean | 9/729 |
| `hasKeyboardFocus` | boolean | 729/729 |
| `href?` | undefined | 26/729 |
| `id?` | string | 107/729 |
| `onBlur?` | fn | 458/729 |
| `onClick?` | fn | 707/729 |
| `onContextMenu?` | fn | 110/729 |
| `onDoubleClick?` | fn | 7/729 |
| `onDragStart?` | fn | 9/729 |
| `onFocus?` | fn | 415/729 |
| `onKeyDown?` | fn | 410/729 |
| `onKeyPress?` | fn | 7/729 |
| `onMouseDown?` | fn | 58/729 |
| `onMouseEnter?` | fn | 451/729 |
| `onMouseLeave?` | fn | 451/729 |
| `onMouseOut?` | fn | 105/729 |
| `onMouseOver?` | fn | 105/729 |
| `onTransitionEnd?` | null | 9/729 |
| `ref` | null \| ref \| fn | 729/729 |
| `role?` | string ("tab"|"button"|"menuitem") | 206/729 |
| `size?` | string ("small") | 1/729 |
| `style?` | {width?: string, height?: string, transform?: null, transition?: null, minWidth?: string} | 54/729 |
| `tabIndex?` | number | 426/729 |
| `target?` | undefined | 1/729 |
| `title?` | string | 2/729 |
| `type?` | string ("outline") | 1/729 |
| `visibleAutoFocus?` | boolean | 1/729 |
| `children?` | node \| array<node \| boolean \| string \| null \| array<node>> \| string | 727/729 |

### BaseAutoClog

- type: plain · fibers seen: 2613 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<button>`, `<div>` · classes: `c-button-unstyled` `p-autoclog__hook` `p-tab_rail__button` `c-tabs__tab` `js-tab` `c-tabs__tab--full_width` `p-channel_sidebar__link` `p-ia4_history_buttons__button` `p-top_nav__button` `p-top_nav__help` `c-icon_button` `c-icon_button--size_medium` `c-icon_button--default` `p-channel_sidebar__link--add-more-items`
- data-qa: `canvas` `unstyled-button` `message-text` `highlight-btn` `user-button` `history_back_button` `history_forward_button` `top-nav-history-menu`

| prop | shape | seen |
|---|---|---|
| `action?` | number | 612/621 |
| `activityLayoutFeedPanelWidth?` | number | 29/621 |
| `activityLayoutFeedWidthPercent?` | number | 29/621 |
| `activityLayoutTotalAppWidth?` | number | 29/621 |
| `activityV2?` | string | 29/621 |
| `appId?` | string | 75/621 |
| `archiveOnly?` | boolean | 29/621 |
| `articleId?` | number | 2/621 |
| `availableTasks?` | array<string> | 10/621 |
| `blockKitAppId?` | string | 14/621 |
| `blockKitBotId?` | string | 14/621 |
| `botId?` | string | 10/621 |
| `channel_id?` | string | 9/621 |
| `channelId?` | string | 214/621 |
| `channelIds?` | array | 29/621 |
| `channelSectionIds?` | array | 29/621 |
| `clogger` | Clogger {autoClogRecordedLogs: array<object>, beaconThrew: boolean, enterpriseId: string, experimentExposureRecordedLogs: array, i18nLocale: string, logURLs: array, options: {clogType: string}, processingLogURLs: boolean, recordedLogs: array, releasedHolds: Map, sendingLogURLs: Set, sessionIdFunc: fn, teamId: string, useBeacon: boolean, userId: string} | 621/621 |
| `clogImpression?` | boolean | 193/621 |
| `clogImpressionDuration?` | boolean | 13/621 |
| `completedTasks?` | array<string> | 10/621 |
| `density?` | string ("compact") | 29/621 |
| `disableAutoClog?` | boolean | 124/621 |
| `disableAutoReferringUiContext?` | boolean | 236/621 |
| `disableValidationWarnings?` | boolean | 9/621 |
| `elementIndex?` | number | 2/621 |
| `elementName?` | string \| number | 333/621 |
| `elementType?` | number | 336/621 |
| `elementValue?` | string | 68/621 |
| `enableClogImpressionOffScreen?` | boolean | 31/621 |
| `entryPoint?` | string | 9/621 |
| `entrypoint?` | string | 12/621 |
| `entryTypes?` | array | 29/621 |
| `eventId` | number | 621/621 |
| `interaction?` | number | 2/621 |
| `interactionElement?` | number | 3/621 |
| `internalNavigationStateActiveTab?` | undefined | 593/621 |
| `internalNavigationStateViews?` | undefined | 593/621 |
| `isActivityItemUnread?` | boolean | 25/621 |
| `isAutoClogAction?` | boolean | 326/621 |
| `isBadged?` | boolean | 20/621 |
| `isChildWindow` | boolean | 621/621 |
| `isCopilot?` | boolean | 10/621 |
| `isMainWindow` | boolean | 621/621 |
| `isPrimaryCTA?` | boolean | 10/621 |
| `isTopParentAutoClog` | boolean | 621/621 |
| `megaphoneEligibleNotifications?` | undefined | 13/621 |
| `megaphoneNotificationName?` | string | 23/621 |
| `megaphoneSpaceName?` | string | 23/621 |
| `msgThreadTs?` | undefined | 6/621 |
| `msgTs?` | string | 25/621 |
| `priorityOnly?` | boolean | 29/621 |
| `reclogImpression?` | number | 90/621 |
| `ref` | null | 621/621 |
| `requiredTagProps?` | array<string> | 612/621 |
| `screenTimeBeforeImpression?` | number | 90/621 |
| `setElementValueOnAction?` | boolean | 8/621 |
| `sort?` | string | 29/621 |
| `stepVariant?` | string | 37/621 |
| `struct` | {name: string, shape: map<string, string \| object>, tagConfigs: {activityLayoutFeedPanelWidth: object, activityLayoutFeedWidthPercent: object, activityLayoutTotalAppWidth: object, entryPoint: object, eventId: object, fetchDurationMs: object, referringEventId: object, referringUiContext: object}} | 621/621 |
| `surfaceId?` | string | 14/621 |
| `surfaceType?` | number | 14/621 |
| `tagPropsThatRetriggerImpression?` | Set | 612/621 |
| `totalUnreadCount?` | number | 29/621 |
| `trackClogRef?` | fn | 45/621 |
| `uiComponentName?` | string | 228/621 |
| `uiComponentVariant?` | string | 195/621 |
| `uiElement?` | number | 4/621 |
| `uiPage?` | string | 40/621 |
| `uiStep?` | string | 56/621 |
| `unreadsOnly?` | boolean | 29/621 |
| `uuid?` | string | 14/621 |
| `view_type?` | string | 7/621 |
| `viewId?` | string | 29/621 |
| `viewType?` | string | 29/621 |
| `windowId` | string | 621/621 |
| `windowRef` | WeakRef | 621/621 |
| `windowToken?` | undefined | 621/621 |
| `children` | node \| array<node \| boolean \| null> | 621/621 |

### PopoverTriggerWithKeyboardFocus

- type: plain · fibers seen: 2487 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<button>`, `<div>` · classes: `c-button-unstyled` `p-channel_sidebar__channel` `p-channel_sidebar__channel--show-avatars` `p-tab_rail__button` `c-tabs__tab` `js-tab` `c-tabs__tab--full_width` `c-icon_button` `p-channel_sidebar__section_heading` `sectionbutton__CadG9` `ellipsis__Lwi05` `c-icon_button--size_medium` `c-icon_button--default` `p-ia4_history_menu_button__button`
- data-qa: `channel-sidebar-channel` `unstyled-button` `canvas` `top-nav-history-menu` `team_sidebar_item` `add-workspace-btn` `tab_rail_dms_button` `tab_rail_activity_button`

| prop | shape | seen |
|---|---|---|
| `allowanceX?` | number | 31/608 |
| `allowanceY?` | number | 11/608 |
| `customTrigger?` | boolean | 24/608 |
| `data-qa?` | string | 56/608 |
| `forceOpen?` | boolean | 433/608 |
| `isContextMenu?` | boolean | 380/608 |
| `logger` | {action: fn, debug: fn, devHint: fn, error: fn, info: fn, teamOrEnterpriseId: string, timeStamp: fn, warn: fn} | 608/608 |
| `offsetX?` | number | 128/608 |
| `offsetY?` | number | 126/608 |
| `onClick?` | fn | 480/608 |
| `onClose?` | fn | 478/608 |
| `onOpen?` | fn | 2/608 |
| `openOnDownArrow?` | boolean | 2/608 |
| `overlayClassName?` | string | 11/608 |
| `position?` | string ("bottom-left"|"bottom"|"right-bottom"|"left-bottom"|"top-left"|"top-right"|"right-top"|"bottom-right") | 575/608 |
| `preventScroll?` | undefined | 380/608 |
| `ref` | null \| ref | 608/608 |
| `renderPopover` | fn | 608/608 |
| `shouldCloseOnOverlayClick?` | boolean | 36/608 |
| `shouldCloseOnOverlayMouseDown?` | boolean | 31/608 |
| `shouldFade?` | boolean | 391/608 |
| `slackKitConfig` | {enable24hrTime: boolean, locale: string, managedFocusContainer: {Component: object, coachmarkSectionIndex: number, workspaceSwitcherTabListSectionIndex: number}, modalHooks: {closeModal: fn}, popoverHooks: {decrementOpenCount: fn, incrementOpenCount: fn}, restoreFocusFallback: fn, scrollmarkHooks: {addScrollmarks: fn, getScrollmark: fn, removeScrollmarks: fn}, telemeter: Telemeter {endpointUrl: string, enterpriseId: string, recordedMetrics: object, sessionId: string, subAppName: string, teamId: string, teamSize?: undefined, userId: string, versionTs: string}, timeZone: string} | 608/608 |
| `startsOpen?` | boolean | 35/608 |
| `suppressDragRegionsWhenOpen?` | boolean | 393/608 |
| `targetBounds?` | undefined | 25/608 |
| `triggerProps?` | {aria-haspopup?: string} | 561/608 |
| `tryOppositePosition?` | boolean | 416/608 |
| `useRestoreFocus?` | boolean | 7/608 |
| `width?` | number | 8/608 |
| `children` | node \| fn | 608/608 |

### BasePopoverTrigger

- type: plain · fibers seen: 2487 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<button>`, `<div>` · classes: `c-button-unstyled` `p-channel_sidebar__channel` `p-channel_sidebar__channel--show-avatars` `p-tab_rail__button` `c-tabs__tab` `js-tab` `c-tabs__tab--full_width` `c-icon_button` `p-channel_sidebar__section_heading` `sectionbutton__CadG9` `ellipsis__Lwi05` `c-icon_button--size_medium` `c-icon_button--default` `p-ia4_history_menu_button__button`
- data-qa: `channel-sidebar-channel` `unstyled-button` `canvas` `top-nav-history-menu` `team_sidebar_item` `add-workspace-btn` `tab_rail_dms_button` `tab_rail_activity_button`

| prop | shape | seen |
|---|---|---|
| `allowanceX?` | number | 31/608 |
| `allowanceY?` | number | 11/608 |
| `customTrigger?` | boolean | 24/608 |
| `data-qa?` | string | 56/608 |
| `experimentSimpleUnderlineLinkOn` | boolean | 608/608 |
| `experimentSlackKitCarouselGroupOn` | boolean | 608/608 |
| `forceOpen?` | boolean | 433/608 |
| `isContextMenu?` | boolean | 380/608 |
| `logger` | {action: fn, debug: fn, devHint: fn, error: fn, info: fn, teamOrEnterpriseId: string, timeStamp: fn, warn: fn} | 608/608 |
| `offsetX?` | number | 128/608 |
| `offsetY?` | number | 126/608 |
| `onClick?` | fn | 480/608 |
| `onClose?` | fn | 478/608 |
| `onOpen?` | fn | 2/608 |
| `openOnDownArrow?` | boolean | 2/608 |
| `overlayClassName?` | string | 11/608 |
| `position?` | string ("bottom-left"|"bottom"|"right-bottom"|"left-bottom"|"top-left"|"top-right"|"right-top"|"bottom-right") | 575/608 |
| `preventScroll?` | undefined | 380/608 |
| `readKeyboardMode` | fn | 608/608 |
| `ref` | null \| ref | 608/608 |
| `renderPopover` | fn | 608/608 |
| `shouldCloseOnOverlayClick?` | boolean | 36/608 |
| `shouldCloseOnOverlayMouseDown?` | boolean | 31/608 |
| `shouldFade?` | boolean | 391/608 |
| `slackKitConfig` | {enable24hrTime: boolean, locale: string, managedFocusContainer: {Component: object, coachmarkSectionIndex: number, workspaceSwitcherTabListSectionIndex: number}, modalHooks: {closeModal: fn}, popoverHooks: {decrementOpenCount: fn, incrementOpenCount: fn}, restoreFocusFallback: fn, scrollmarkHooks: {addScrollmarks: fn, getScrollmark: fn, removeScrollmarks: fn}, telemeter: Telemeter {endpointUrl: string, enterpriseId: string, recordedMetrics: object, sessionId: string, subAppName: string, teamId: string, teamSize?: undefined, userId: string, versionTs: string}, timeZone: string} | 608/608 |
| `startsOpen?` | boolean | 35/608 |
| `suppressDragRegionsWhenOpen?` | boolean | 393/608 |
| `targetBounds?` | undefined | 25/608 |
| `triggerProps?` | {aria-haspopup?: string} | 561/608 |
| `tryOppositePosition?` | boolean | 416/608 |
| `useRestoreFocus?` | boolean | 7/608 |
| `width?` | number | 8/608 |
| `children` | node \| fn | 608/608 |

### ContextMenuTriggerWithKeyboardFocus

- type: plain · fibers seen: 1495 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>`, `<button>` · classes: `c-button-unstyled` `p-member_profile_hover_card` `c-tabs__tab` `js-tab` `p-channel_sidebar__channel` `p-channel_sidebar__channel--show-avatars` `p-tab_rail__button` `c-tabs__tab--full_width` `channel_tab__B1pww` `channel_tab_draggable__blPuX` `p-channel_sidebar__section_heading` `wrapper__nq4fb` `p-team_sidebar__item` `p-team_sidebar__item--active`
- data-qa: `channel-sidebar-channel` `canvas` `team_sidebar_item` `tab_rail_dms_button` `tab_rail_activity_button` `tab_rail_files_button` `tab_rail_later_button` `tabs_item`

| prop | shape | seen |
|---|---|---|
| `desktopAppOnly?` | boolean | 501/509 |
| `forceOpen?` | boolean | 11/509 |
| `getTemplate?` | fn | 383/509 |
| `menuAriaLabel?` | string | 127/509 |
| `onClick?` | fn | 146/509 |
| `onClose?` | fn | 56/509 |
| `onContextMenuOpen?` | fn | 126/509 |
| `overrideDefaultMenu?` | boolean | 158/509 |
| `renderMenu?` | fn | 126/509 |
| `showSlackKitContextMenuOnDesktop?` | boolean | 489/509 |
| `width?` | string | 362/509 |
| `children` | node \| fn | 509/509 |

### ListItem

- type: plain · fibers seen: 1377 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `c-virtual_list__item` `p-channel_sidebar__static_list__item` `p-channel_sidebar__static_list__item--contain` `p-channel_sidebar__static_list__item__row_container` `p-channel_sidebar__static_list__item--section_header_focus` `p-channel_sidebar__static_list__item--selected` `p-ia_details_popover__members_list_item` `p-ia_details_popover__members_list_item_member` `listitem__iBnNh` `c-virtual_list__item--sticky` `c-virtual_list__item--sticky-animated` `p-ia_details_popover__members_list_item_anchor` `p-ia_details_popover__members_list_item_add`
- data-qa: `virtual-list-item` `invite-members`

| prop | shape | seen |
|---|---|---|
| `aria-checked?` | boolean | 1/195 |
| `aria-label?` | string | 4/195 |
| `aria-labelledby?` | string | 2/195 |
| `aria-setsize?` | string | 194/195 |
| `ariaLabel?` | string | 2/195 |
| `className?` | string | 17/195 |
| `contextMenuVisible?` | boolean | 194/195 |
| `data-feat?` | string | 151/195 |
| `data-feat-tag?` | string | 147/195 |
| `data-feat-trace-end?` | string | 147/195 |
| `dataQA?` | null | 2/195 |
| `hasFocus?` | boolean | 194/195 |
| `hasFocusWithin?` | boolean | 194/195 |
| `hasKeyboardFocusVisual?` | boolean | 194/195 |
| `height?` | number | 194/195 |
| `id` | string | 195/195 |
| `isCachedHeightValid?` | boolean | 194/195 |
| `isChildWindow?` | boolean | 194/195 |
| `isInitialActiveItem?` | boolean | 194/195 |
| `isMainWindow?` | boolean | 194/195 |
| `itemKey?` | string | 194/195 |
| `keyboardFocusReader?` | undefined | 194/195 |
| `onClick?` | fn | 195/195 |
| `onContextMenu?` | fn | 194/195 |
| `onDoubleClick?` | fn | 13/195 |
| `onFocusEnter?` | fn | 194/195 |
| `onFocusLeave?` | fn | 194/195 |
| `onFocusWithin?` | fn | 194/195 |
| `onHeightChange?` | fn | 194/195 |
| `onKeyDown?` | fn | 194/195 |
| `onMouseDown?` | fn | 194/195 |
| `onTabWithin?` | fn | 194/195 |
| `readKeyboardMode?` | fn | 194/195 |
| `ref?` | null | 194/195 |
| `resizeObserver?` | ResizeObserver | 194/195 |
| `role?` | string ("presentation"|"checkbox") | 13/195 |
| `shouldHorizontallyScroll?` | boolean | 194/195 |
| `supportRovingTabIndex?` | boolean | 194/195 |
| `tabIndex?` | number | 194/195 |
| `top?` | number | 194/195 |
| `useStaticHeight?` | boolean | 194/195 |
| `windowId?` | string | 194/195 |
| `windowRef?` | WeakRef | 194/195 |
| `windowToken?` | undefined | 194/195 |
| `zIndex?` | number | 2/195 |
| `children` | node | 195/195 |

### WithKeyboardFocusListItem

- type: plain · fibers seen: 1367 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `c-virtual_list__item` `p-channel_sidebar__static_list__item` `p-channel_sidebar__static_list__item--contain` `p-channel_sidebar__static_list__item__row_container` `p-channel_sidebar__static_list__item--section_header_focus` `p-channel_sidebar__static_list__item--selected` `p-ia_details_popover__members_list_item` `p-ia_details_popover__members_list_item_member` `c-virtual_list__item--sticky` `c-virtual_list__item--sticky-animated` `p-ia_details_popover__members_list_item_anchor` `p-ia_details_popover__members_list_item_add`
- data-qa: `virtual-list-item` `invite-members`

| prop | shape | seen |
|---|---|---|
| `aria-checked?` | boolean | 1/194 |
| `aria-label?` | string | 3/194 |
| `aria-labelledby?` | string | 2/194 |
| `aria-setsize?` | string | 194/194 |
| `ariaLabel?` | string | 2/194 |
| `className?` | string | 16/194 |
| `contextMenuVisible` | boolean | 194/194 |
| `data-feat?` | string | 151/194 |
| `data-feat-tag?` | string | 147/194 |
| `data-feat-trace-end?` | string | 147/194 |
| `dataQA?` | null | 2/194 |
| `hasFocus` | boolean | 194/194 |
| `hasFocusWithin` | boolean | 194/194 |
| `height` | number | 194/194 |
| `id` | string | 194/194 |
| `isCachedHeightValid` | boolean | 194/194 |
| `isChildWindow` | boolean | 194/194 |
| `isInitialActiveItem` | boolean | 194/194 |
| `isMainWindow` | boolean | 194/194 |
| `itemKey` | string | 194/194 |
| `onClick` | fn | 194/194 |
| `onContextMenu` | fn | 194/194 |
| `onDoubleClick?` | fn | 13/194 |
| `onFocusEnter` | fn | 194/194 |
| `onFocusLeave` | fn | 194/194 |
| `onFocusWithin` | fn | 194/194 |
| `onHeightChange` | fn | 194/194 |
| `onKeyDown` | fn | 194/194 |
| `onMouseDown` | fn | 194/194 |
| `onTabWithin` | fn | 194/194 |
| `ref` | null | 194/194 |
| `resizeObserver?` | ResizeObserver | 194/194 |
| `role?` | string ("presentation"|"checkbox") | 13/194 |
| `shouldHorizontallyScroll` | boolean | 194/194 |
| `supportRovingTabIndex` | boolean | 194/194 |
| `tabIndex` | number | 194/194 |
| `top` | number | 194/194 |
| `useStaticHeight` | boolean | 194/194 |
| `windowId` | string | 194/194 |
| `windowRef` | WeakRef | 194/194 |
| `windowToken?` | undefined | 194/194 |
| `zIndex?` | number | 2/194 |
| `children` | node | 194/194 |

### Emoji

- type: memo · fibers seen: 1006 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<img>`, `<span>` · classes: `c-emoji` `c-emoji__medium` `c-emoji--inline` `c-emoji__small`
- data-qa: `emoji`

| prop | shape | seen |
|---|---|---|
| `alt?` | string | 321/460 |
| `ariaLabel?` | string | 321/460 |
| `dispatch?` | fn | 433/460 |
| `displayText?` | string | 433/460 |
| `element?` | {name: string, type: string ("emoji")} | 27/460 |
| `emojiNameFromText?` | null | 433/460 |
| `emojiSize?` | string | 460/460 |
| `fallbackUrl?` | string | 433/460 |
| `forceDisplay?` | boolean | 322/460 |
| `iconInTextMode?` | undefined | 10/460 |
| `isEmojiInlineWithText?` | boolean | 107/460 |
| `isEmojiTooltipHidden?` | boolean | 27/460 |
| `isImportAllowed?` | boolean | 80/460 |
| `isStandard?` | boolean | 433/460 |
| `renderAsText?` | boolean | 433/460 |
| `showTooltip?` | boolean | 67/460 |
| `sourceTeamId?` | undefined | 63/460 |
| `stopAnimations?` | boolean | 460/460 |
| `text?` | string | 433/460 |
| `unicode?` | undefined | 53/460 |
| `url?` | string | 433/460 |

### MemberContextMenu

- type: memo · fibers seen: 870 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: `p-member_profile_hover_card`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `getMemberContextMenuTemplate` | fn | 447/447 |
| `id` | string | 447/447 |
| `isClient` | boolean | 447/447 |
| `children` | node | 447/447 |

### RichTextRenderer

- type: plain · fibers seen: 812 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>`, `<span>` · classes: `p-rich_text_section`
- data-qa: `mrkdwn-link-url-hover-card`

| prop | shape | seen |
|---|---|---|
| `allowCheckList` | boolean | 441/441 |
| `alwaysDisplayMentionsAsLinks` | boolean | 441/441 |
| `blocksContainerContext` | string | 441/441 |
| `clogCitationImpression` | fn | 441/441 |
| `clogLinkClick` | fn | 441/441 |
| `elements` | array<{type: string ("rich_text_section"|"text"|"emoji"|"link"), text?: string, elements?: array, url?: string, name?: string, editedByUserId?: undefined, editedTs?: string, isArchived?: boolean, omitWhitespace?: boolean}> | 441/441 |
| `emojiSize?` | undefined | 441/441 |
| `hideTruncationExpandBtn?` | undefined | 441/441 |
| `isEmojiImportEnabled` | boolean | 441/441 |
| `isEmojiTooltipHidden` | boolean | 441/441 |
| `isFirst?` | boolean | 241/441 |
| `noInlineLinks` | boolean | 441/441 |
| `noTabbableContent` | boolean | 441/441 |
| `stopAnimations?` | boolean | 441/441 |
| `streaming` | boolean | 441/441 |
| `truncationExpandBtnCopy?` | undefined | 441/441 |

### BaseMessageReads

- type: memo · fibers seen: 762 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `dispatch` | fn | 436/436 |
| `experimentLogMessageReadsGroupTreatment` | boolean | 436/436 |
| `messageContainerType` | string | 436/436 |
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: array<object>, blocks?: array<object>, channel: string, crm_signal?: undefined, files?: array<string>, language?: undefined, latest_reply?: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users?: array<string>, reply_users_count?: number, slackbot_feels: null, subtype?: string ("reminder_add"), text: string, thread_ts?: string, ts: string, type: string ("message"), user: string, version?: string, client_msg_id?: string, upload?: boolean, is_locked?: boolean, edited?: {ts: string, user: string}, bot_id?: string, bot_profile?: {app_id: string, deleted: boolean, icons: object, id: string, name: string, team_id: string, updated: number, user_id: string}, parent_user_id?: string, …+2} | 436/436 |
| `children` | node | 436/436 |

### MessageLabels

- type: memo · fibers seen: 762 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `activeTab` | string | 436/436 |
| `className?` | string | 405/436 |
| `closeView` | fn | 436/436 |
| `container` | string | 436/436 |
| `currentUserId` | string | 436/436 |
| `dispatch` | fn | 436/436 |
| `experimentAutomaticReplyOooLabelGroupOn` | boolean | 436/436 |
| `experimentCrmSignalGroupOn` | boolean | 436/436 |
| `experimentFeRemindMenuHoverActionGroupOn` | boolean | 436/436 |
| `experimentInvitedUserGlowUpGroupOn` | boolean | 436/436 |
| `experimentReskinCustomResponsesGroupOn` | boolean | 436/436 |
| `forceTabWhenNavigating?` | undefined | 436/436 |
| `getPrimaryView` | fn | 436/436 |
| `getSecondaryView` | fn | 436/436 |
| `getSiblingView` | fn | 436/436 |
| `getSiblingViewWithUiState` | fn | 436/436 |
| `getViewSetWithUiState` | fn | 436/436 |
| `isIm` | boolean | 436/436 |
| `isLater` | boolean | 436/436 |
| `isMpim` | boolean | 436/436 |
| `isSelfIm` | boolean | 436/436 |
| `isSentViaEmail` | boolean | 436/436 |
| `isSlackbotAiEnabled` | boolean | 436/436 |
| `isSlackbotIm` | boolean | 436/436 |
| `isViewingChannel` | boolean | 436/436 |
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: array<object>, blocks?: array<object>, channel: string, crm_signal?: undefined, files?: array<string>, language?: undefined, latest_reply?: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users?: array<string>, reply_users_count?: number, slackbot_feels: null, subtype?: string ("reminder_add"), text: string, thread_ts?: string, ts: string, type: string ("message"), user: string, version?: string, client_msg_id?: string, upload?: boolean, is_locked?: boolean, edited?: {ts: string, user: string}, bot_id?: string, bot_profile?: {app_id: string, deleted: boolean, icons: object, id: string, name: string, team_id: string, updated: number, user_id: string}, parent_user_id?: string, …+2} | 436/436 |
| `navigationTraceId` | string | 436/436 |
| `ref` | null | 436/436 |
| `setBackground` | fn | 436/436 |
| `shouldNavigateInContext` | boolean | 436/436 |
| `showBroadcast?` | boolean | 15/436 |
| `theme` | string ("light") | 436/436 |
| `todosAutoOpenItemDetailsPref` | boolean | 436/436 |
| `view` | {id: string, params: {parentTab: string, tabId?: string, dangerouslyOverrideRouting?: boolean, highlightRoot?: boolean, threadId?: string, engagementEntryPoint?: string, engagementEntryPointLoad?: string, engagementEntryPointVisit?: string, sidebarSection?: string}, viewType: string} | 436/436 |
| `children` | node | 436/436 |

### BaseLabelsAriaLiveMessage

- type: memo · fibers seen: 762 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `currentUserId` | string | 436/436 |
| `dispatch` | fn | 436/436 |
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: array<object>, blocks?: array<object>, channel: string, crm_signal?: undefined, files?: array<string>, language?: undefined, latest_reply?: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users?: array<string>, reply_users_count?: number, slackbot_feels: null, subtype?: string ("reminder_add"), text: string, thread_ts?: string, ts: string, type: string ("message"), user: string, version?: string, client_msg_id?: string, upload?: boolean, is_locked?: boolean, edited?: {ts: string, user: string}, bot_id?: string, bot_profile?: {app_id: string, deleted: boolean, icons: object, id: string, name: string, team_id: string, updated: number, user_id: string}, parent_user_id?: string, …+2} | 436/436 |

### MessageEdit

- type: memo · fibers seen: 762 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: `c-message__sender` `c-message_kit__sender`
- data-qa: `message_sender` `aria-labelledby-primary-<id>-<n>-sender`

| prop | shape | seen |
|---|---|---|
| `dispatch` | fn | 436/436 |
| `isChildWindow` | boolean | 436/436 |
| `isEditing` | boolean | 436/436 |
| `isMainWindow` | boolean | 436/436 |
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: array<object>, blocks?: array<object>, channel: string, crm_signal?: undefined, files?: array<string>, language?: undefined, latest_reply?: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users?: array<string>, reply_users_count?: number, slackbot_feels: null, subtype?: string ("reminder_add"), text: string, thread_ts?: string, ts: string, type: string ("message"), user: string, version?: string, client_msg_id?: string, upload?: boolean, is_locked?: boolean, edited?: {ts: string, user: string}, bot_id?: string, bot_profile?: {app_id: string, deleted: boolean, icons: object, id: string, name: string, team_id: string, updated: number, user_id: string}, parent_user_id?: string, …+2} | 436/436 |
| `ref` | null | 436/436 |
| `setBackground` | fn | 436/436 |
| `viewContext` | string | 436/436 |
| `windowId` | string | 436/436 |
| `windowRef` | WeakRef | 436/436 |
| `windowToken?` | undefined | 436/436 |
| `children` | node \| array<node> | 436/436 |

### MessageText

- type: memo · fibers seen: 762 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: `c-message__body` `c-message__body--automated`
- data-qa: `aria-labelledby-primary-<id>-<n>-translated_state`

| prop | shape | seen |
|---|---|---|
| `clogMessageClick` | fn | 436/436 |
| `data-qa?` | string | 405/436 |
| `do24hrTime` | boolean | 436/436 |
| `experimentMeetingChannelLifecycleGroupOn` | boolean | 436/436 |
| `hideCanvasMsgInBroadcasts?` | boolean | 405/436 |
| `inline?` | boolean | 31/436 |
| `isSystemMessageRow?` | boolean | 421/436 |
| `isThreadRoot?` | boolean | 16/436 |
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: array<object>, blocks?: array<object>, channel: string, crm_signal?: undefined, files?: array<string>, language?: undefined, latest_reply?: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users?: array<string>, reply_users_count?: number, slackbot_feels: null, subtype?: string ("reminder_add"), text: string, thread_ts?: string, ts: string, type: string ("message"), user: string, version?: string, client_msg_id?: string, upload?: boolean, is_locked?: boolean, edited?: {ts: string, user: string}, bot_id?: string, bot_profile?: {app_id: string, deleted: boolean, icons: object, id: string, name: string, team_id: string, updated: number, user_id: string}, parent_user_id?: string, …+2} | 436/436 |
| `rollup?` | undefined | 405/436 |
| `shouldOptimizeLineLength` | boolean | 436/436 |
| `showMeetingChannelAssociatedMessage` | boolean | 436/436 |
| `showMeetingChannelRootCard` | boolean | 436/436 |

### BaseIcon

- type: plain · fibers seen: 744 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>`, `<img>` · classes: `c-base_icon__width_only_container` `c-base_icon` `c-base_icon--image`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `ariaHidden` | boolean | 434/434 |
| `image` | string | 434/434 |
| `onError?` | fn | 413/434 |
| `onLoad?` | fn | 413/434 |
| `resetIA?` | undefined | 413/434 |
| `size` | number | 434/434 |
| `srcSet?` | string | 413/434 |
| `widthOnly?` | boolean | 413/434 |

### WithFormattingStyles

- type: plain · fibers seen: 734 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>`, `<a>` · classes: `c-emoji` `c-emoji__medium` `c-emoji--inline` `c-link` `c-member_slug` `c-member_slug--light` `c-member_slug--link` `c-member_slug--mention`
- data-qa: `emoji` `mrkdwn-link-url-hover-card`

| prop | shape | seen |
|---|---|---|
| `style?` | undefined | 433/433 |
| `children` | string \| node | 433/433 |

### PopoverWithWindow

- type: plain · fibers seen: 724 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `ReactModal__Overlay` `ReactModal__Overlay--after-open` `c-popover` `c-popover--z_above_fs` `c-popover--fade` `c-popover--no-pointer` `c-popover--z_menu`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `allowanceX?` | number | 53/432 |
| `allowanceY?` | number | 24/432 |
| `ariaHideApp?` | boolean | 408/432 |
| `ariaLabel?` | string | 400/432 |
| `ariaLabelledBy?` | undefined | 22/432 |
| `ariaModal?` | undefined | 378/432 |
| `ariaRole?` | string | 401/432 |
| `contentStyle?` | object | 30/432 |
| `customTrigger?` | boolean | 1/432 |
| `dataAttributes?` | {qa: string} | 1/432 |
| `experimentSimpleUnderlineLinkOn?` | boolean | 2/432 |
| `experimentSlackKitCarouselGroupOn?` | boolean | 2/432 |
| `forceOpen?` | boolean | 2/432 |
| `id?` | string | 1/432 |
| `isContentInteractive?` | boolean | 378/432 |
| `isOpen` | boolean | 432/432 |
| `isOverlayInteractive?` | boolean | 379/432 |
| `logger?` | {action: fn, debug: fn, devHint: fn, error: fn, info: fn, teamOrEnterpriseId: string, timeStamp: fn, warn: fn} | 2/432 |
| `offsetX?` | number | 31/432 |
| `offsetY?` | number | 410/432 |
| `onClick?` | fn | 2/432 |
| `onClose?` | fn | 431/432 |
| `onOpen?` | fn | 22/432 |
| `openedWithKeyboard?` | boolean | 2/432 |
| `openOnDownArrow?` | boolean | 1/432 |
| `overlayClassName?` | string | 50/432 |
| `overlayStyle?` | object | 379/432 |
| `position` | string ("top-left"|"top"|"bottom-left") | 432/432 |
| `readKeyboardMode?` | fn | 2/432 |
| `renderPopover?` | fn | 2/432 |
| `shouldCloseOnEsc?` | boolean | 21/432 |
| `shouldCloseOnOverlayClick?` | boolean | 1/432 |
| `shouldCloseOnOverlayMouseDown?` | boolean | 32/432 |
| `shouldFade?` | boolean | 431/432 |
| `shouldFocusAfterRender?` | boolean | 430/432 |
| `shouldObserveResize?` | boolean | 29/432 |
| `shouldPropagateOnClose?` | boolean | 29/432 |
| `shouldReturnFocusAfterClose?` | boolean | 24/432 |
| `shouldSetTargetBoundsForContextMenu?` | boolean | 2/432 |
| `shouldTrackOffsetChange?` | boolean | 1/432 |
| `slackKitConfig?` | {enable24hrTime: boolean, locale: string, managedFocusContainer: {Component: object, coachmarkSectionIndex: number, workspaceSwitcherTabListSectionIndex: number}, modalHooks: {closeModal: fn}, popoverHooks: {decrementOpenCount: fn, incrementOpenCount: fn}, restoreFocusFallback: fn, scrollmarkHooks: {addScrollmarks: fn, getScrollmark: fn, removeScrollmarks: fn}, telemeter: Telemeter {endpointUrl: string, enterpriseId: string, recordedMetrics: object, sessionId: string, subAppName: string, teamId: string, teamSize?: undefined, userId: string, versionTs: string}, timeZone: string} | 2/432 |
| `startsOpen?` | boolean | 2/432 |
| `stickyPosition?` | undefined | 29/432 |
| `suppressDragRegionsWhenOpen?` | boolean | 21/432 |
| `targetBounds?` | {bottom: number, height: number, left: number, right: number, top: number, width: number} | 411/432 |
| `tryOppositePosition?` | boolean | 410/432 |
| `useTargetBoundsAsPosition?` | boolean | 3/432 |
| `zIndex?` | string | 408/432 |
| `children` | node \| fn | 432/432 |

### BaseAvatar

- type: plain · fibers seen: 706 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: `c-avatar` `p-member_profile_hover_card` `p-avatar_stack__avatar` `p-ia__nav__user__avatar` `c-message__reply_bar_avatar` `c-avatar--transparent_bg` `c-base_entity__avatar` `p-member_profile_base_entity__avatar` `p-member_profile_base_entity__avatar--cursor`
- data-qa: `channel-prefix-im-avatar` `member-entity__avatar`

| prop | shape | seen |
|---|---|---|
| `additionalOverlay?` | undefined | 25/278 |
| `botEmoji?` | undefined | 278/278 |
| `botId?` | string | 278/278 |
| `botProfile?` | undefined | 7/278 |
| `canRenderEmoji` | boolean | 278/278 |
| `channelId?` | string | 4/278 |
| `className?` | string | 118/278 |
| `data-qa?` | string | 112/278 |
| `disableLink` | boolean | 278/278 |
| `emoji?` | undefined | 25/278 |
| `experimentExternalHuddlesGroupOn` | boolean | 278/278 |
| `experimentFrontlineUsersGroupOn` | boolean | 278/278 |
| `hasNoAccess?` | undefined | 278/278 |
| `icons?` | undefined | 32/278 |
| `imageUrl` | string | 278/278 |
| `imageUrl2x` | string | 278/278 |
| `isChannelVisitor` | boolean | 278/278 |
| `isDeleted` | boolean | 278/278 |
| `isDnd` | boolean | 278/278 |
| `isExternal` | boolean | 278/278 |
| `isExternalHuddleGuest` | boolean | 278/278 |
| `isFrontlineMember` | boolean | 278/278 |
| `isInteractive` | boolean | 278/278 |
| `isRestricted` | boolean | 278/278 |
| `isSystemUser` | boolean | 278/278 |
| `isUltraRestricted` | boolean | 278/278 |
| `isUserHidden` | boolean | 278/278 |
| `maybeOpenInSurface` | fn | 278/278 |
| `memberOrBotName` | string | 278/278 |
| `navigate` | fn | 278/278 |
| `openProfile` | fn | 278/278 |
| `profileUrlForNewTab?` | undefined | 278/278 |
| `showCard?` | boolean | 25/278 |
| `showPresence?` | boolean | 278/278 |
| `size` | number | 278/278 |
| `teamId?` | undefined | 278/278 |
| `triggerId?` | undefined | 278/278 |
| `userId` | string | 278/278 |
| `userType?` | string | 278/278 |

### MessageWrapper

- type: memo · fibers seen: 702 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `c-message_kit__background` `p-message_pane_message__message` `c-message_kit__message` `p-message_pane_message__active_thread` `c-message_kit__background--hovered`
- data-qa: `message_container`

_no props recorded_

### ButtonLink

- type: plain · fibers seen: 655 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<button>` · classes: `c-link--button` `p-emoji_picker__list_item` `p-emoji_picker__list_item--a11y-not-selected` `c-message__sender_button` `c-link--on-neutral` `sk_foreground_max` `c-link--underline-inherit` `c-message__reply_count` `c-button-unstyled` `p-field__floating_hint` `p-field__edit_link` `p-field__button`
- data-qa: `emoji_list_item` `message_sender_name` `reply_bar_count` `about__topic_field-edit-button` `about__purpose_field-edit-button` `about__channel_manager_field__channel_manager_profile_link` `integrations__workflows_cta`

| prop | shape | seen |
|---|---|---|
| `aria-colindex?` | number | 239/425 |
| `aria-describedby?` | undefined | 21/425 |
| `aria-expanded?` | boolean | 18/425 |
| `autoClogProps?` | undefined | 239/425 |
| `className?` | string | 408/425 |
| `data-color-index?` | number | 239/425 |
| `data-message-sender?` | string | 130/425 |
| `data-name?` | string | 239/425 |
| `data-qa?` | string | 387/425 |
| `data-sk?` | string | 21/425 |
| `disabled?` | boolean | 239/425 |
| `id?` | string | 239/425 |
| `onBlur?` | fn | 21/425 |
| `onClick?` | fn | 407/425 |
| `onContextMenu?` | fn | 239/425 |
| `onFocus?` | fn | 21/425 |
| `onKeyDown?` | fn | 21/425 |
| `onMouseEnter?` | fn | 260/425 |
| `onMouseLeave?` | fn | 260/425 |
| `ref?` | ref | 239/425 |
| `role?` | string ("gridcell") | 239/425 |
| `style?` | undefined | 130/425 |
| `tabIndex?` | number | 260/425 |
| `underlineStyle?` | string | 18/425 |
| `children` | node \| string | 425/425 |

### HoverCardTrigger

- type: plain · fibers seen: 633 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: `p-member_profile_hover_card` `c-message_attachment_v3--hover_wrapper`
- data-qa: `mrkdwn-link-url-hover-card`

| prop | shape | seen |
|---|---|---|
| `className?` | string | 352/423 |
| `dataQa?` | string | 71/423 |
| `onContextMenu?` | fn | 295/423 |
| `onOpen?` | fn | 341/423 |
| `onOpenIntent?` | undefined | 340/423 |
| `popoverZIndex?` | string | 12/423 |
| `position` | string ("top-left"|"top") | 423/423 |
| `renderCard` | fn | 423/423 |
| `children` | node | 423/423 |

### BaseMrkdwn

- type: memo · fibers seen: 578 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: `p-classic_nav__model__title__info__topic__content` `p-classic_nav__model__title__info__topic__content--channel_tabs` `c-message__body` `c-message__body--automated`
- data-qa: `bk_markdown_element` `simple_message_preview`

| prop | shape | seen |
|---|---|---|
| `addDirectionAttr?` | boolean | 33/417 |
| `ariaAssertive?` | boolean | 33/417 |
| `className?` | string | 94/417 |
| `clogLinkClick?` | fn | 255/417 |
| `customContentHandler?` | fn | 33/417 |
| `customFormatHandler?` | fn | 60/417 |
| `customLineEnding?` | string | 62/417 |
| `data-qa?` | string | 62/417 |
| `do24hrTime` | boolean | 417/417 |
| `doInlineImages?` | boolean | 13/417 |
| `doSlackActionLinks?` | boolean | 130/417 |
| `emojiDisplayInfo?` | undefined | 27/417 |
| `emojiMode` | string | 417/417 |
| `emojiSize?` | string | 271/417 |
| `enableSlackActionLinks?` | boolean | 13/417 |
| `experimentTsfSnitchGroupOn` | boolean | 417/417 |
| `forceRealNames?` | undefined | 417/417 |
| `highlightWords` | array | 417/417 |
| `ignoredPropAttachments?` | undefined | 33/417 |
| `ignoredPropEdited?` | undefined | 33/417 |
| `isEmojiInlineWithText?` | boolean | 27/417 |
| `isExpandableStateKnown?` | boolean | 97/417 |
| `isExpanded?` | boolean | 97/417 |
| `isFromAppHome?` | boolean | 27/417 |
| `maxCharacters?` | number | 126/417 |
| `maxNewlines?` | number | 159/417 |
| `memberId?` | string | 33/417 |
| `noCode?` | boolean | 2/417 |
| `noHexColors` | boolean | 417/417 |
| `noHighlights?` | boolean | 85/417 |
| `noJumbomoji` | boolean | 417/417 |
| `noLinking?` | boolean | 62/417 |
| `noQuotes?` | boolean | 2/417 |
| `noSpecials?` | boolean | 130/417 |
| `onOpenSuspiciousLinkDialog` | fn | 417/417 |
| `onRender?` | fn | 124/417 |
| `onUserGroupClick` | fn | 417/417 |
| `showTooltips?` | boolean | 2/417 |
| `suppressBreaks?` | boolean | 196/417 |
| `suppressLinkWarningPref` | boolean | 417/417 |
| `text?` | string | 417/417 |

### Truncate

- type: plain · fibers seen: 576 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: `c-truncate` `c-truncate--break_words` `c-truncate--overflowWrap`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `breakWords?` | boolean | 218/417 |
| `customTooltipText?` | node | 27/417 |
| `lines` | number | 417/417 |
| `overflowWrap?` | boolean | 70/417 |
| `withTooltip?` | boolean | 248/417 |
| `children` | node \| array<node \| boolean> \| string | 417/417 |

### TabsItem

- type: plain · fibers seen: 538 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<button>`, `<div>` · classes: `c-button-unstyled` `c-tabs__tab` `js-tab` `draggable__eJoxt` `channel_tab__B1pww` `channel_tab_draggable__blPuX` `c-tabs__tab--active` `addTab__brBMy` `p-activity_ia4_page__tab_container__tab` `p-emoji_picker__group_tab` `p-emoji_picker__group_tab--active` `p-emoji_picker__group_tab--emoji-a11y` `c-tabs__tab--full_width`
- data-qa: `draggable_tab_item` `canvas` `unstyled-button` `channel` `list` `activity-saved-view-tab-all` `activity-saved-view-tab-dms` `activity-saved-view-tab-mentions`

| prop | shape | seen |
|---|---|---|
| `aria-describedby?` | string | 382/413 |
| `aria-haspopup?` | string | 246/413 |
| `aria-label?` | string | 251/413 |
| `ariaHaspopup?` | boolean | 189/413 |
| `ariaRole?` | string | 27/413 |
| `autoClogProps?` | {onClick: {enableClogAction: boolean}, elementType?: number, elementName?: string, elementValue?: string, appId?: undefined, channelId?: string, eventId?: number, uiComponentName?: string, uiComponentVariant?: string, uiStep?: string, clogImpression?: boolean, stepVariant?: string, elementIndex?: number} | 409/413 |
| `badge?` | undefined | 220/413 |
| `buttonWrapper?` | fn | 386/413 |
| `className?` | string | 412/413 |
| `customIcon?` | node | 220/413 |
| `data-feat?` | string | 220/413 |
| `data-qa?` | string | 386/413 |
| `focusOnMount?` | undefined | 220/413 |
| `formatCount?` | boolean | 220/413 |
| `fullWidth?` | boolean | 220/413 |
| `icon?` | undefined | 220/413 |
| `iconPosition?` | string | 220/413 |
| `id` | string | 413/413 |
| `isCountLoading?` | undefined | 220/413 |
| `isDisabled?` | boolean | 221/413 |
| `isFocused` | boolean | 413/413 |
| `isHidden?` | boolean | 220/413 |
| `isLoading?` | undefined | 220/413 |
| `isSelected` | boolean | 413/413 |
| `label` | node \| string | 413/413 |
| `maxTitleCount?` | undefined | 220/413 |
| `onBlur?` | fn | 139/413 |
| `onClick?` | fn | 413/413 |
| `onMouseDown?` | fn | 139/413 |
| `onMouseEnter?` | fn | 139/413 |
| `onMouseLeave?` | fn | 139/413 |
| `ref?` | fn \| ref | 413/413 |
| `svgIcon?` | undefined | 220/413 |
| `title?` | node | 4/413 |
| `titleCount?` | number | 220/413 |

### BaseLink

- type: plain · fibers seen: 536 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<a>` · classes: `c-link` `c-timestamp` `p-file_image_thumbnail__wrapper` `p-file_image_thumbnail__wrapper--gallery_v3` `c-message_kit__avatar` `c-avatar` `c-avatar--interactive` `c-avatar--transparent_bg`
- data-qa: `message_file_image_thumbnail`

| prop | shape | seen |
|---|---|---|
| `aria-hidden?` | string | 34/413 |
| `aria-label?` | string | 331/413 |
| `autoClogProps?` | undefined | 34/413 |
| `className` | string | 413/413 |
| `data-feat?` | string | 82/413 |
| `data-file-id?` | string | 82/413 |
| `data-qa?` | string | 413/413 |
| `data-sk?` | string | 297/413 |
| `data-stringify-ignore?` | undefined | 34/413 |
| `data-stringify-requires-siblings?` | boolean | 297/413 |
| `data-stringify-text?` | string | 297/413 |
| `data-stringify-type?` | string | 297/413 |
| `data-thumbnail-wrapper?` | boolean | 82/413 |
| `data-ts?` | string | 297/413 |
| `href` | string | 413/413 |
| `onBlur?` | fn | 297/413 |
| `onClick` | fn | 413/413 |
| `onFocus?` | fn | 297/413 |
| `onKeyDown?` | fn | 297/413 |
| `onMouseEnter?` | fn | 297/413 |
| `onMouseLeave?` | fn | 297/413 |
| `rel?` | string | 116/413 |
| `style?` | {height: string, width: string} | 34/413 |
| `tabIndex?` | number | 34/413 |
| `target?` | string | 34/413 |
| `children` | node \| array<node> | 413/413 |

### Timestamp

- type: plain · fibers seen: 528 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<a>`, `<span>` · classes: `c-link` `c-timestamp`
- data-qa: `aria-labelledby-primary-<id>-<n>-date_time`

| prop | shape | seen |
|---|---|---|
| `clickable` | boolean | 412/412 |
| `compact?` | undefined | 412/412 |
| `customTimestampFormat?` | string | 412/412 |
| `data-qa?` | undefined | 412/412 |
| `full?` | undefined | 412/412 |
| `link` | string | 412/412 |
| `meridiem?` | boolean | 412/412 |
| `onClick` | fn | 412/412 |
| `relative?` | boolean | 412/412 |
| `showWeekday?` | undefined | 412/412 |
| `timestamp` | string | 412/412 |
| `visible` | boolean | 412/412 |
| `children` | null \| string | 412/412 |

### VisuallyHiddenTimestamp

- type: plain · fibers seen: 528 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: —
- data-qa: `aria-labelledby-primary-<id>-<n>-date_time`

_no props recorded_

### Link

- type: plain · fibers seen: 515 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>`, `<a>` · classes: `p-channel_sidebar__link` `c-link` `p-channel_sidebar__link--page` `position_relative` `c-message_attachment_v3` `p-channel_sidebar__link--add-more-items` `c-member_slug` `c-member_slug--light` `c-member_slug--link` `c-message_attachment__image` `c-member_slug--mention` `p-channel_sidebar__link--all-threads` `p-channel_sidebar__link--page_pbrowse-huddles` `p-channel_sidebar__link--page_pdrafts`
- data-qa: `mrkdwn-link-url-hover-card` `message_attachment_default` `attachment_image` `channel_sidebar_vall_threads` `channel_sidebar_pbrowse-huddles` `channel_sidebar_pdrafts` `channel_sidebar_pbookmarks` `channel_sidebar_directories_link`

| prop | shape | seen |
|---|---|---|
| `aria-describedby?` | string | 39/411 |
| `aria-hidden?` | undefined | 23/411 |
| `aria-label?` | string | 8/411 |
| `autoClogProps?` | {onClick: {enableClogAction: boolean}, elementName?: string, entryPoint?: string, blockKitAppId?: undefined, blockKitBotId?: undefined, eventId?: number, interaction?: number, interactionElement?: number, surfaceId?: string, surfaceType?: number, uuid?: string} | 123/411 |
| `badgeCount?` | number | 109/411 |
| `className?` | string | 341/411 |
| `clogCitationImpression?` | fn | 68/411 |
| `clogLinkClick?` | fn | 68/411 |
| `data-member-id?` | string | 23/411 |
| `data-member-label?` | string | 23/411 |
| `data-pausable-image?` | boolean | 35/411 |
| `data-qa?` | string | 100/411 |
| `data-sk?` | string | 37/411 |
| `data-stringify-id?` | string | 23/411 |
| `data-stringify-label?` | string | 23/411 |
| `data-stringify-type?` | string | 23/411 |
| `disabled?` | boolean | 4/411 |
| `displayAsBold?` | boolean | 82/411 |
| `element?` | {type: string ("link"), url: string} | 68/411 |
| `emojiSize?` | undefined | 68/411 |
| `hasUnreads?` | boolean | 109/411 |
| `href?` | string | 122/411 |
| `icon?` | node | 164/411 |
| `id?` | string | 218/411 |
| `isBBModeEnabled?` | boolean | 136/411 |
| `isDisabled?` | boolean | 27/411 |
| `isEmojiTooltipHidden?` | boolean | 68/411 |
| `isFromAppHome?` | boolean | 68/411 |
| `isSelected?` | boolean | 136/411 |
| `isSilentSidebarActive?` | boolean | 109/411 |
| `label?` | string \| node | 218/411 |
| `noLinking?` | boolean | 68/411 |
| `noTabbableContent?` | boolean | 68/411 |
| `onBlur?` | fn | 37/411 |
| `onClick?` | fn | 315/411 |
| `onContextMenu?` | fn | 56/411 |
| `onFocus?` | fn | 37/411 |
| `onKeyDown?` | fn | 37/411 |
| `onMouseEnter?` | fn | 37/411 |
| `onMouseLeave?` | fn | 37/411 |
| `ref?` | null \| ref | 8/411 |
| `renderLinkContent?` | fn | 136/411 |
| `renderRichText?` | fn | 68/411 |
| `shouldUseStreamingAnimation?` | boolean | 68/411 |
| `stopAnimations?` | undefined | 68/411 |
| `style?` | {backgroundImage: string, backgroundSize: string, height?: number, width?: number} | 35/411 |
| `tabIndex?` | number | 23/411 |
| `target?` | string | 56/411 |
| `underlineStyle?` | undefined | 4/411 |
| `unstyled?` | boolean | 4/411 |
| `children?` | array<node> \| node \| string | 125/411 |

### AutoSizer

- type: forwardRef · fibers seen: 484 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `className?` | string | 2/408 |
| `disableHeight?` | boolean | 263/408 |
| `doNotBailOutOnEmptyChildren?` | boolean | 208/408 |
| `onResize?` | fn | 59/408 |
| `ref?` | null | 208/408 |
| `style?` | {width: string} | 84/408 |
| `children` | fn | 408/408 |

### BaseMessageAvatar

- type: memo · fibers seen: 468 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: `p-member_profile_hover_card`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `botId?` | string | 406/406 |
| `clogUserClick` | fn | 406/406 |
| `codeChannelLifecycleTile?` | undefined | 406/406 |
| `experimentChannelMeetingInviteGroupOn` | boolean | 406/406 |
| `experimentReskinCustomResponsesGroupOn` | boolean | 406/406 |
| `experimentSlackbotAiNextGroupOn` | boolean | 406/406 |
| `isAppOrBotJoiner` | boolean | 406/406 |
| `isMpdm` | boolean | 406/406 |
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: array<object>, blocks?: array<object>, channel: string, crm_signal?: undefined, files?: array<string>, language?: undefined, latest_reply?: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users?: array<string>, reply_users_count?: number, slackbot_feels: null, subtype?: string ("reminder_add"), text: string, thread_ts?: string, ts: string, type: string ("message"), user: string, version?: string, client_msg_id?: string, upload?: boolean, bot_id?: string, is_locked?: boolean, edited?: {ts: string, user: string}, bot_profile?: {app_id: string, deleted: boolean, icons: object, id: string, name: string, team_id: string, updated: number, user_id: string}, parent_user_id?: string, …+2} | 406/406 |
| `showCard?` | boolean | 349/406 |
| `showMeetingChannelAssociatedMessage` | boolean | 406/406 |
| `showMeetingChannelRootCard` | boolean | 406/406 |
| `teamId` | string | 406/406 |
| `userId` | string | 406/406 |

### MessageHover

- type: plain · fibers seen: 445 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `c-message_kit__hover` `activity_row_content_margin__JI87w` `activity_row_content_margin_full_width__SYtD7` `c-message_kit__hover--hovered`
- data-qa: `activity-item-container`

| prop | shape | seen |
|---|---|---|
| `className?` | string | 55/404 |
| `data-qa?` | string | 55/404 |
| `disabled?` | boolean | 349/404 |
| `hasContextMenu?` | boolean | 349/404 |
| `hasKeyboardFocus` | boolean | 404/404 |
| `hasKeyboardFocusWithin` | boolean | 404/404 |
| `hovered?` | boolean | 349/404 |
| `hoveredClassName?` | undefined | 349/404 |
| `onBlur` | fn | 404/404 |
| `onFocus` | fn | 404/404 |
| `ref` | null | 404/404 |
| `setBackground` | fn | 404/404 |
| `children` | node | 404/404 |

### StreamingRendererItem

- type: plain · fibers seen: 444 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-rich_text_block` `p-rich_text_block--inline` `block__G68co` `blockSlim__nnaQq` `p-context_block` `p-section_block` `p-image_block` `p-image_block--full_width`
- data-qa: `bk_context_block` `bk_section_block`

| prop | shape | seen |
|---|---|---|
| `fallback` | fn | 404/404 |
| `index` | number | 404/404 |
| `item` | {type: string ("rich_text"|"section"|"image"|"plan"|"context"), elements?: array<object>, block_id?: string, _isFirstBlock?: boolean, tasks?: array<object>, title?: string, alt_text?: string, fallback?: string, image_bytes?: number, image_height?: number, image_url?: string, image_width?: number, is_animated?: boolean, text?: {text: string, type: string ("mrkdwn"), verbatim: boolean}} | 404/404 |
| `renderables` | Map | 404/404 |
| `streaming?` | undefined | 404/404 |

### SUIRendererItem

- type: plain · fibers seen: 444 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-rich_text_block` `p-rich_text_block--inline` `block__G68co` `blockSlim__nnaQq` `p-context_block` `p-section_block` `p-image_block` `p-image_block--full_width`
- data-qa: `bk_context_block` `bk_section_block`

| prop | shape | seen |
|---|---|---|
| `fallback` | fn | 404/404 |
| `item` | {type: string ("rich_text"|"section"|"image"|"plan"|"context"), elements?: array<object>, block_id?: string, _isFirstBlock?: boolean, tasks?: array<object>, title?: string, alt_text?: string, fallback?: string, image_bytes?: number, image_height?: number, image_url?: string, image_width?: number, is_animated?: boolean, text?: {text: string, type: string ("mrkdwn"), verbatim: boolean}} | 404/404 |

### SUIRenderer

- type: plain · fibers seen: 442 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-block_kit_renderer__block_wrapper` `p-block_kit_renderer__block_wrapper--first` `p-rich_text_block` `p-rich_text_block--inline`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `errorFallback` | fn | 404/404 |
| `fallback` | fn | 404/404 |
| `payload` | array<{type: string ("rich_text"|"section"|"image"|"plan"|"context"), elements?: array, block_id?: string, _isFirstBlock?: boolean, tasks?: array, title?: string, alt_text?: string, fallback?: string, image_bytes?: number, image_height?: number, image_url?: string, image_width?: number, is_animated?: boolean, text?: object}> | 404/404 |
| `wrapTopLevelItem?` | fn | 319/404 |

### StreamingRenderer

- type: plain · fibers seen: 442 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-block_kit_renderer__block_wrapper` `p-block_kit_renderer__block_wrapper--first` `p-rich_text_block` `p-rich_text_block--inline`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `errorFallback` | fn | 404/404 |
| `fallback` | fn | 404/404 |
| `payload` | array<{type: string ("rich_text"|"section"|"image"|"plan"|"context"), elements?: array, block_id?: string, _isFirstBlock?: boolean, tasks?: array, title?: string, alt_text?: string, fallback?: string, image_bytes?: number, image_height?: number, image_url?: string, image_width?: number, is_animated?: boolean, text?: object}> | 404/404 |
| `streaming?` | undefined | 404/404 |
| `wrapTopLevelItem?` | fn | 404/404 |

### ManagedFocusContainer

- type: plain · fibers seen: 418 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `enabled-managed-focus-container`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `enableSectionNavigation?` | boolean | 365/401 |
| `focusFn?` | fn | 291/401 |
| `id` | string | 401/401 |
| `index` | number | 401/401 |
| `isChildWindow` | boolean | 401/401 |
| `isMainWindow` | boolean | 401/401 |
| `ref` | null | 401/401 |
| `restoreFocusFn?` | fn | 37/401 |
| `shouldFocus?` | fn | 37/401 |
| `tabCountToShowContextualHelp?` | number | 72/401 |
| `windowId` | string | 401/401 |
| `windowRef` | WeakRef | 401/401 |
| `windowToken?` | undefined | 401/401 |
| `children` | node \| array<node> | 401/401 |

### ManagedTabNavigationContainer

- type: plain · fibers seen: 418 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `enabled-managed-focus-container`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `className` | string | 401/401 |
| `hasKeyboardFocus` | boolean | 401/401 |
| `hasKeyboardFocusWithin` | boolean | 401/401 |
| `onBlur` | fn | 401/401 |
| `onFirstHiddenTabStopFocus` | fn | 401/401 |
| `onFocus` | fn | 401/401 |
| `onLastHiddenTabStopFocus` | fn | 401/401 |
| `refToForward` | ref | 401/401 |
| `style` | {display: string} | 401/401 |
| `children` | node \| array<node> | 401/401 |

### BaseText

- type: plain · fibers seen: 418 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `element` | {text: string, type: string ("text")} | 401/401 |
| `isFirstElementInFirstSection` | boolean | 401/401 |
| `isFirstElementInSection` | boolean | 401/401 |
| `shouldUseStreamingAnimation` | boolean | 401/401 |

### BaseCustomStatus

- type: plain · fibers seen: 415 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: `c-custom_status` `c-member__current-status` `c-member__current-status--medium`
- data-qa: `custom_status`

| prop | shape | seen |
|---|---|---|
| `animated?` | boolean | 26/401 |
| `className?` | string | 177/401 |
| `data-qa?` | string | 25/401 |
| `insertTextSpacing?` | boolean | 1/401 |
| `memberId` | string | 401/401 |
| `noText?` | boolean | 25/401 |
| `tooltip?` | boolean | 26/401 |

### RichTextComponent

- type: plain · fibers seen: 406 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-rich_text_block` `p-rich_text_block--inline`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `_isFirstBlock` | boolean | 400/400 |
| `addDirectionAttr` | boolean | 400/400 |
| `allowCheckList` | boolean | 400/400 |
| `blockOverrides` | {disableJumboEmoji?: undefined, highlightWords?: undefined, inline?: undefined, substringMatch?: undefined} | 400/400 |
| `blocksContainerContext` | string | 400/400 |
| `clogCitationImpression` | fn | 400/400 |
| `clogLinkClick` | fn | 400/400 |
| `collapsedChannelLimit?` | undefined | 400/400 |
| `disableHighlights` | boolean | 400/400 |
| `disableJumboEmoji` | boolean | 400/400 |
| `elements` | array<{elements: array, type: string ("rich_text_section")}> | 400/400 |
| `emojiSize?` | undefined | 400/400 |
| `findInThreadHighlightWords?` | undefined | 400/400 |
| `highlightWords?` | undefined | 400/400 |
| `inline` | boolean | 400/400 |
| `isEmojiImportEnabled` | boolean | 400/400 |
| `isEmojiTooltipHidden` | boolean | 400/400 |
| `noInlineLinks` | boolean | 400/400 |
| `noTabbableContent?` | boolean | 400/400 |
| `stopAnimations?` | boolean | 400/400 |
| `streaming?` | undefined | 400/400 |
| `substringMatch?` | boolean | 400/400 |
| `type` | string ("rich_text") | 400/400 |

### BaseRichText

- type: plain · fibers seen: 406 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-rich_text_block` `p-rich_text_block--inline`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `addDirectionAttr` | boolean | 400/400 |
| `allowCheckList` | boolean | 400/400 |
| `alwaysDisplayMentionsAsLinks` | boolean | 400/400 |
| `blocksContainerContext` | string | 400/400 |
| `clogCitationImpression` | fn | 400/400 |
| `clogLinkClick` | fn | 400/400 |
| `elements` | array<{elements: array, type: string ("rich_text_section")}> | 400/400 |
| `emojiSize?` | undefined | 400/400 |
| `findInThreadHighlightWords?` | undefined | 400/400 |
| `highlightWords` | array | 400/400 |
| `inline` | boolean | 400/400 |
| `isEmojiDisplayedAsText` | boolean | 400/400 |
| `isEmojiImportEnabled` | boolean | 400/400 |
| `isEmojiTooltipHidden` | boolean | 400/400 |
| `isJumbomojiEnabled` | boolean | 400/400 |
| `noInlineLinks` | boolean | 400/400 |
| `noTabbableContent?` | boolean | 400/400 |
| `stopAnimations?` | boolean | 400/400 |
| `streaming` | boolean | 400/400 |
| `substringMatch?` | boolean | 400/400 |
| `teamId` | string | 400/400 |

### RichTextSection

- type: plain · fibers seen: 406 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-rich_text_section`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `element` | {elements: array<object>, type: string ("rich_text_section")} | 400/400 |
| `isFirst` | boolean | 400/400 |
| `renderRichText` | fn | 400/400 |

### VisuallyHiddenSender

- type: plain · fibers seen: 396 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: —
- data-qa: `aria-labelledby-primary-<id>-<n>-sender`

_no props recorded_

### VisuallyHiddenMessageSummary

- type: plain · fibers seen: 381 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

_no props recorded_

### VisuallyHiddenCounts

- type: plain · fibers seen: 381 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

_no props recorded_

### VisuallyHiddenStates

- type: plain · fibers seen: 381 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

_no props recorded_

### AggregateMediaPlaybackInProgress

- type: plain · fibers seen: 381 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: `c-message__sender` `c-message_kit__sender`
- data-qa: `message_sender` `aria-labelledby-primary-<id>-<n>-sender`

| prop | shape | seen |
|---|---|---|
| `onMediaPlaybackInProgressChanged` | fn | 381/381 |
| `children` | node \| array<node> | 381/381 |

### MediaPlaybackInProgress

- type: plain · fibers seen: 381 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: `c-message__sender` `c-message_kit__sender`
- data-qa: `message_sender` `aria-labelledby-primary-<id>-<n>-sender`

| prop | shape | seen |
|---|---|---|
| `value` | fn | 381/381 |
| `children` | node \| array<node> | 381/381 |

### MeetingChannelRootTimeBadge

- type: plain · fibers seen: 381 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: array<object>, blocks?: array<object>, channel: string, crm_signal?: undefined, files?: array<string>, language?: undefined, latest_reply?: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users?: array<string>, reply_users_count?: number, slackbot_feels: null, subtype?: string ("reminder_add"), text: string, thread_ts?: string, ts: string, type: string ("message"), user: string, version?: string, client_msg_id?: string, upload?: boolean, edited?: {ts: string, user: string}, bot_id?: string, is_locked?: boolean, bot_profile?: {app_id: string, deleted: boolean, icons: object, id: string, name: string, team_id: string, updated: number, user_id: string}, parent_user_id?: string, …+2} | 381/381 |

### MessageFiles

- type: plain · fibers seen: 381 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `c-files_container`
- data-qa: `message_kit_files`

| prop | shape | seen |
|---|---|---|
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: array<object>, blocks?: array<object>, channel: string, crm_signal?: undefined, files?: array<string>, language?: undefined, latest_reply?: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users?: array<string>, reply_users_count?: number, slackbot_feels: null, subtype?: string ("reminder_add"), text: string, thread_ts?: string, ts: string, type: string ("message"), user: string, version?: string, client_msg_id?: string, upload?: boolean, edited?: {ts: string, user: string}, bot_id?: string, is_locked?: boolean, bot_profile?: {app_id: string, deleted: boolean, icons: object, id: string, name: string, team_id: string, updated: number, user_id: string}, parent_user_id?: string, …+2} | 381/381 |

### PendingFilesAtom

- type: plain · fibers seen: 381 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: array<object>, blocks?: array<object>, channel: string, crm_signal?: undefined, files?: array<string>, language?: undefined, latest_reply?: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users?: array<string>, reply_users_count?: number, slackbot_feels: null, subtype?: string ("reminder_add"), text: string, thread_ts?: string, ts: string, type: string ("message"), user: string, version?: string, client_msg_id?: string, upload?: boolean, edited?: {ts: string, user: string}, bot_id?: string, is_locked?: boolean, bot_profile?: {app_id: string, deleted: boolean, icons: object, id: string, name: string, team_id: string, updated: number, user_id: string}, parent_user_id?: string, …+2} | 381/381 |

### MessageKitAttachments

- type: plain · fibers seen: 381 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `c-message_kit__attachments`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `blocksContainerContext?` | string | 30/381 |
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: array<object>, blocks?: array<object>, channel: string, crm_signal?: undefined, files?: array<string>, language?: undefined, latest_reply?: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users?: array<string>, reply_users_count?: number, slackbot_feels: null, subtype?: string ("reminder_add"), text: string, thread_ts?: string, ts: string, type: string ("message"), user: string, version?: string, client_msg_id?: string, upload?: boolean, edited?: {ts: string, user: string}, bot_id?: string, is_locked?: boolean, bot_profile?: {app_id: string, deleted: boolean, icons: object, id: string, name: string, team_id: string, updated: number, user_id: string}, parent_user_id?: string, …+2} | 381/381 |
| `viewContext` | string | 381/381 |

### PeekTrigger

- type: plain · fibers seen: 376 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-peek_trigger`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `allowanceX?` | number | 38/376 |
| `allowanceY?` | number | 38/376 |
| `disableHover?` | undefined | 183/376 |
| `forceOpenOnClick?` | boolean | 38/376 |
| `isChildWindow` | boolean | 376/376 |
| `isMainWindow` | boolean | 376/376 |
| `isOpen?` | boolean | 221/376 |
| `offsetX` | number | 376/376 |
| `offsetY` | number | 376/376 |
| `onClose?` | fn | 376/376 |
| `onOpen?` | fn | 155/376 |
| `peekId` | string | 376/376 |
| `position` | string ("bottom-left"|"right-bottom") | 376/376 |
| `ref` | null | 376/376 |
| `renderContent` | fn | 376/376 |
| `triggerDelay?` | number | 193/376 |
| `windowId` | string | 376/376 |
| `windowRef` | WeakRef | 376/376 |
| `windowToken?` | undefined | 376/376 |
| `children` | node | 376/376 |

### MessageResend

- type: plain · fibers seen: 366 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: array<object>, blocks?: array<object>, channel: string, crm_signal?: undefined, files?: array<string>, language?: undefined, latest_reply?: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users?: array<string>, reply_users_count?: number, slackbot_feels: null, subtype?: string ("reminder_add"), text: string, thread_ts?: string, ts: string, type: string ("message"), user: string, version?: string, client_msg_id?: string, upload?: boolean, edited?: {ts: string, user: string}, bot_id?: string, is_locked?: boolean, bot_profile?: {app_id: string, deleted: boolean, icons: object, id: string, name: string, team_id: string, updated: number, user_id: string}, parent_user_id?: string, …+2} | 366/366 |
| `children` | array<node> | 366/366 |

### AgentPromptCollapsedBody

- type: plain · fibers seen: 366 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: array<object>, blocks?: array<object>, channel: string, crm_signal?: undefined, files?: array<string>, language?: undefined, latest_reply?: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users?: array<string>, reply_users_count?: number, slackbot_feels: null, subtype?: string ("reminder_add"), text: string, thread_ts?: string, ts: string, type: string ("message"), user: string, version?: string, client_msg_id?: string, upload?: boolean, is_locked?: boolean, edited?: {ts: string, user: string}, bot_id?: string, bot_profile?: {app_id: string, deleted: boolean, icons: object, id: string, name: string, team_id: string, updated: number, user_id: string}, metadata?: {event_type: string}} | 366/366 |
| `children` | array<node> | 366/366 |

### QuoteReplyHeader

- type: plain · fibers seen: 366 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: array<object>, blocks?: array<object>, channel: string, crm_signal?: undefined, files?: array<string>, language?: undefined, latest_reply?: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users?: array<string>, reply_users_count?: number, slackbot_feels: null, subtype?: string ("reminder_add"), text: string, thread_ts?: string, ts: string, type: string ("message"), user: string, version?: string, client_msg_id?: string, upload?: boolean, edited?: {ts: string, user: string}, bot_id?: string, is_locked?: boolean, bot_profile?: {app_id: string, deleted: boolean, icons: object, id: string, name: string, team_id: string, updated: number, user_id: string}, parent_user_id?: string, …+2} | 366/366 |

### Message

- type: plain · fibers seen: 351 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `c-message_kit__background` `p-message_pane_message__message` `c-message_kit__message` `p-message_pane_message__active_thread` `c-message_kit__background--hovered`
- data-qa: `message_container`

_no props recorded_

### MessageFileComment

- type: plain · fibers seen: 351 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: array<object>, blocks?: array<object>, channel: string, crm_signal?: undefined, files?: array<string>, language?: undefined, latest_reply?: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users?: array<string>, reply_users_count?: number, slackbot_feels: null, subtype?: string ("reminder_add"), text: string, thread_ts?: string, ts: string, type: string ("message"), user: string, version?: string, client_msg_id?: string, upload?: boolean, edited?: {ts: string, user: string}, bot_id?: string, is_locked?: boolean, bot_profile?: {app_id: string, deleted: boolean, icons: object, id: string, name: string, team_id: string, updated: number, user_id: string}, metadata?: {event_type: string}} | 351/351 |

### AgentCheckingDetailsLoader

- type: plain · fibers seen: 351 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: array<object>, blocks?: array<object>, channel: string, crm_signal?: undefined, files?: array<string>, language?: undefined, latest_reply?: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users?: array<string>, reply_users_count?: number, slackbot_feels: null, subtype?: string ("reminder_add"), text: string, thread_ts?: string, ts: string, type: string ("message"), user: string, version?: string, client_msg_id?: string, upload?: boolean, edited?: {ts: string, user: string}, bot_id?: string, is_locked?: boolean, bot_profile?: {app_id: string, deleted: boolean, icons: object, id: string, name: string, team_id: string, updated: number, user_id: string}, metadata?: {event_type: string}} | 351/351 |

### ThreadReplies

- type: plain · fibers seen: 351 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `c-message__reply_bar` `c-message_kit__thread_replies`
- data-qa: `reply_bar`

| prop | shape | seen |
|---|---|---|
| `messageContainerType` | string | 351/351 |
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: array<object>, blocks?: array<object>, channel: string, crm_signal?: undefined, files?: array<string>, language?: undefined, latest_reply?: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users?: array<string>, reply_users_count?: number, slackbot_feels: null, subtype?: string ("reminder_add"), text: string, thread_ts?: string, ts: string, type: string ("message"), user: string, version?: string, client_msg_id?: string, upload?: boolean, edited?: {ts: string, user: string}, bot_id?: string, is_locked?: boolean, bot_profile?: {app_id: string, deleted: boolean, icons: object, id: string, name: string, team_id: string, updated: number, user_id: string}, metadata?: {event_type: string}} | 351/351 |
| `unreadThreadsCount` | number | 351/351 |

### BroadcastFooter

- type: plain · fibers seen: 351 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: array<object>, blocks?: array<object>, channel: string, crm_signal?: undefined, files?: array<string>, language?: undefined, latest_reply?: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users?: array<string>, reply_users_count?: number, slackbot_feels: null, subtype?: string ("reminder_add"), text: string, thread_ts?: string, ts: string, type: string ("message"), user: string, version?: string, client_msg_id?: string, upload?: boolean, edited?: {ts: string, user: string}, bot_id?: string, is_locked?: boolean, bot_profile?: {app_id: string, deleted: boolean, icons: object, id: string, name: string, team_id: string, updated: number, user_id: string}, metadata?: {event_type: string}} | 351/351 |

### MessageBlocks

- type: plain · fibers seen: 346 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `c-message__message_blocks` `p-autoclog__hook`
- data-qa: `message-text`

| prop | shape | seen |
|---|---|---|
| `blockLimit?` | undefined | 346/346 |
| `blocksContainerContext` | string | 346/346 |
| `clogLinkClick` | fn | 346/346 |
| `emojiSize?` | undefined | 346/346 |
| `enableBlockDragSource` | boolean | 346/346 |
| `findInThreadHighlightWords?` | undefined | 346/346 |
| `hasFindInThreadMatch` | boolean | 346/346 |
| `inline?` | boolean | 346/346 |
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: array<object>, blocks: array<object>, channel: string, crm_signal?: undefined, files?: array<string>, language?: undefined, latest_reply?: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users?: array<string>, reply_users_count?: number, slackbot_feels: null, subtype?: undefined, text: string, thread_ts?: string, ts: string, type: string ("message"), user: string, version?: string, client_msg_id?: string, upload?: boolean, edited?: {ts: string, user: string}, bot_id?: string, is_locked?: boolean, bot_profile?: {app_id: string, deleted: boolean, icons: object, id: string, name: string, team_id: string, updated: number, user_id: string}, parent_user_id?: string, …+2} | 346/346 |
| `streaming` | boolean | 346/346 |

### EmojiListEmojiItem

- type: plain · fibers seen: 316 · states: emoji-picker-composer, emoji-picker-reaction
- outer host: `<button>` · classes: `c-link--button` `p-emoji_picker__list_item` `p-emoji_picker__list_item--a11y-not-selected`
- data-qa: `emoji_list_item`

| prop | shape | seen |
|---|---|---|
| `dispatch` | fn | 316/316 |
| `emoji` | string | 316/316 |
| `emojiIndex` | number | 316/316 |
| `index` | number | 316/316 |
| `isMultiSkintone` | boolean | 316/316 |
| `isProductivityEmojiGroup` | boolean | 316/316 |
| `isSelected` | boolean | 316/316 |
| `itemIndex` | number | 316/316 |
| `localizedEmoji` | string | 316/316 |
| `onEmojiClick` | fn | 316/316 |
| `onMouseEnter` | fn | 316/316 |
| `onMouseLeave` | fn | 316/316 |
| `onMultiSkinTonePopoverEscape` | fn | 316/316 |
| `usingKeyboard` | boolean | 316/316 |

### EmojiListEmojiButton

- type: plain · fibers seen: 316 · states: emoji-picker-composer, emoji-picker-reaction
- outer host: `<button>` · classes: `c-link--button` `p-emoji_picker__list_item` `p-emoji_picker__list_item--a11y-not-selected`
- data-qa: `emoji_list_item`

| prop | shape | seen |
|---|---|---|
| `dataColorIndex` | number | 316/316 |
| `emoji` | string | 316/316 |
| `isMultiSkintoneCapable` | boolean | 316/316 |
| `isProductivityEmojiGroup` | boolean | 316/316 |
| `isSelected` | boolean | 316/316 |
| `itemIndex` | number | 316/316 |
| `localizedEmoji` | string | 316/316 |
| `onClick` | fn | 316/316 |
| `onMouseEnter` | fn | 316/316 |
| `onMouseLeave` | fn | 316/316 |
| `refToForward` | null | 316/316 |
| `usingKeyboard` | boolean | 316/316 |

### WithKeyboardLocale

- type: memo · fibers seen: 310 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelSectionType?` | string | 248/310 |
| `dispatch` | fn | 310/310 |
| `floatingHeaderHeight?` | number | 62/310 |
| `keyboardLayout?` | undefined | 310/310 |
| `keyboardPref` | null | 310/310 |
| `logger` | {action: fn, debug: fn, devHint: fn, error: fn, info: fn, teamOrEnterpriseId: string, timeStamp: fn, warn: fn} | 310/310 |
| `onOverflow?` | fn | 62/310 |
| `onScrollFromTop?` | fn | 62/310 |
| `quickReadKey?` | undefined | 248/310 |
| `sidebarListRowKey?` | string | 248/310 |
| `WrappedComponent` | fn | 310/310 |

### Toolbar

- type: plain · fibers seen: 259 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `sk-client-theme--light-inverted-sidebar` `c-wysiwyg_container__formatting` `c-wysiwyg_container__footer` `c-wysiwyg_container__footer--with_formatting` `p-ia4_top_nav` `p-control_strip` `p-ia4_sidebar_header` `p-ia4_home_header` `p-ia4_home_header--with_upgrade` `p-ia4_home_header--with_divider` `p-ia4_sidebar_header--withControls` `p-ia4_sidebar_header--list_isnt_overflowing` `p-ia4_sidebar_header--at_scroll_top` `p-ia4_sidebar_header--text_filter_visible`
- data-qa: `wysiwyg-container_formatting-enabled` `top-nav` `view_header`

| prop | shape | seen |
|---|---|---|
| `activeItemIndex?` | number | 46/259 |
| `aria-label` | string | 259/259 |
| `className?` | string | 252/259 |
| `data-qa?` | string | 115/259 |
| `insertedButtons?` | number | 107/259 |
| `nodeRef?` | ref | 84/259 |
| `onClick?` | fn | 92/259 |
| `onFocusedElementRemoved?` | fn | 46/259 |
| `orientation?` | string | 52/259 |
| `ref?` | ref \| fn | 130/259 |
| `children` | node \| array<node \| boolean> | 259/259 |

### SectionHeading

- type: memo · fibers seen: 248 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-channel_sidebar__section_heading`
- data-qa: `stars` `channels` `direct_messages` `recent_apps`

_no props recorded_

### Channel

- type: plain · fibers seen: 248 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-channel_sidebar__channel` `p-channel_sidebar__channel--show-avatars` `p-channel_sidebar__channel--selected` `p-channel_sidebar__channel--im-you`
- data-qa: `channel-sidebar-channel`

| prop | shape | seen |
|---|---|---|
| `activeTab` | string | 31/31 |
| `channelId` | string | 31/31 |
| `container` | string | 31/31 |
| `highlightLazy?` | undefined | 31/31 |
| `highlightTs?` | undefined | 31/31 |
| `omitBookmarksBar` | boolean | 31/31 |
| `omitViewHeader` | boolean | 31/31 |
| `startTs?` | undefined | 31/31 |
| `startTsOffset?` | undefined | 31/31 |

### Name

- type: plain · fibers seen: 236 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: `c-file_name`
- data-qa: `file_name`

| prop | shape | seen |
|---|---|---|
| `disableTooltip?` | boolean | 76/236 |
| `file` | {id: string, isNonExistent: boolean} | 236/236 |
| `hideRichTextEmojiTooltips?` | boolean | 128/236 |
| `highlightQuery?` | undefined | 128/236 |
| `shouldShowHighlight?` | boolean | 160/236 |
| `useLeadingEmojiAsIcon?` | boolean | 128/236 |
| `useLiveTitleForChannelCanvas?` | undefined | 128/236 |

### DraggableTabItem

- type: plain · fibers seen: 228 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `draggable__eJoxt`
- data-qa: `draggable_tab_item`

| prop | shape | seen |
|---|---|---|
| `id` | string | 228/228 |
| `index` | number | 228/228 |
| `onReorder` | fn | 228/228 |
| `onReorderFromPeek` | fn | 228/228 |
| `children` | node | 228/228 |

### MessageActionButton

- type: plain · fibers seen: 225 · states: activity, activity-menu-Filters, activity-menu-Settings, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<button>`, `<a>` · classes: `c-button-unstyled` `c-icon_button` `c-message_actions__button` `c-icon_button--default` `c-icon_button--size_smedium` `c-message_actions__button--more` `c-icon_button--size_small` `c-message_actions__button--gallery_v3` `c-link` `c-message_actions__button--gallery_v3_margin` `active`
- data-qa: `unstyled-button` `activity-mark-as-unread-button` `add_reaction` `share_message` `add_to_later_todos` `add_to_list` `more_message_actions` `start_thread`

| prop | shape | seen |
|---|---|---|
| `actionId?` | string | 30/225 |
| `aria-haspopup?` | string | 72/225 |
| `aria-keyshortcuts?` | string | 28/225 |
| `aria-label` | string | 225/225 |
| `autoClogProps?` | {elementName: string \| number, onClick: {enableClogAction: boolean}, uiPage?: string, disableAutoReferringUiContext?: boolean, eventId?: number, uiComponentName?: string, elementType?: number, stepVariant?: string, uiElement?: number} | 217/225 |
| `btnRef?` | ref | 30/225 |
| `className?` | string | 86/225 |
| `data-focus-key?` | string | 28/225 |
| `data-focus-metadata?` | string | 30/225 |
| `data-qa?` | string | 97/225 |
| `fadeQuickReactionsCustomizationTip?` | fn | 36/225 |
| `href?` | string | 2/225 |
| `icon` | node | 225/225 |
| `inlineIcon?` | node | 10/225 |
| `inlineLabel?` | string | 10/225 |
| `isDisabled?` | undefined | 2/225 |
| `isSearchInChannel?` | boolean | 6/225 |
| `label` | string \| node | 225/225 |
| `onClick?` | fn | 201/225 |
| `onSelectAction?` | fn | 30/225 |
| `size?` | string ("smedium"|"small") | 10/225 |
| `subtitle?` | undefined | 6/225 |
| `tooltipPosition?` | string ("top"|"top-right") | 14/225 |
| `uiElement?` | number | 30/225 |

### FileGallerySpan

- type: memo · fibers seen: 218 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-message_gallery_image_file` `c-file_gallery_image_file` `c-file_gallery_image_file__smaller_than_container`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `className?` | undefined | 218/218 |
| `dispatch` | fn | 218/218 |
| `experimentAttachmentsMaxWidthGroupOn` | boolean | 218/218 |
| `fileId` | string | 218/218 |
| `galleryMode` | string | 218/218 |
| `galleryWidth` | number | 218/218 |
| `hasOverflowOverlay` | boolean | 218/218 |
| `onRemoveFile?` | undefined | 218/218 |
| `originalH` | number | 218/218 |
| `originalW` | number | 218/218 |
| `removeLabel?` | undefined | 218/218 |
| `renderedFilesDimensions` | array<{height: number, width: number}> | 218/218 |
| `rowHeight?` | undefined | 218/218 |
| `shouldUseMediaGallery` | boolean | 218/218 |
| `thumb360h` | number | 218/218 |
| `thumb360w` | number | 218/218 |
| `viewContext` | string | 218/218 |
| `children` | fn | 218/218 |

### FileImageThumbnail

- type: memo · fibers seen: 218 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `wrapper__nq4fb`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 218/218 |
| `clogLinkClick` | fn | 218/218 |
| `containerHeight` | number | 218/218 |
| `containerWidth` | number | 218/218 |
| `dimensionSetting` | number | 218/218 |
| `experimentAttachmentsMaxWidthGroupOn` | boolean | 218/218 |
| `file` | map<string, array> | 218/218 |
| `galleryWidth` | number | 218/218 |
| `hasOverflowOverlay` | boolean | 218/218 |
| `hasTinyThumb` | boolean | 218/218 |
| `isOnline` | boolean | 218/218 |
| `isUnderOverflowScrim` | boolean | 218/218 |
| `logger` | {action: fn, debug: fn, devHint: fn, error: fn, info: fn, teamOrEnterpriseId: string, timeStamp: fn, warn: fn} | 218/218 |
| `maxSize` | number | 218/218 |
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: undefined, blocks?: array<object>, channel: string, client_msg_id: string, crm_signal?: undefined, files: array<string>, language?: undefined, latest_reply?: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users?: array<string>, reply_users_count?: number, slackbot_feels: null, subtype?: undefined, text: string, thread_ts?: string, ts: string, type: string ("message"), upload: boolean, user: string, version?: string, is_locked?: boolean} | 218/218 |
| `onImageDescriptionClose` | fn | 218/218 |
| `openViewer` | fn | 218/218 |
| `refreshFile` | fn | 218/218 |
| `renderedFilesDimensions` | array<{height: number, width: number}> | 218/218 |
| `shouldUseMediaGallery` | boolean | 218/218 |
| `showActions` | boolean | 218/218 |
| `suppressAnimations` | boolean | 218/218 |
| `teamId` | string | 218/218 |
| `telemeter` | Telemeter {endpointUrl: string, enterpriseId: string, recordedMetrics: map<string, array>, sessionId: string, subAppName: string, teamId: string, teamSize?: undefined, userId: string, versionTs: string} | 218/218 |
| `threadTs?` | string | 218/218 |
| `ts` | string | 218/218 |
| `viewContext` | string | 218/218 |

### ConnectedChannel

- type: plain · fibers seen: 217 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-channel_sidebar__channel` `p-channel_sidebar__channel--show-avatars` `p-channel_sidebar__channel--selected` `p-channel_sidebar__channel--im-you`
- data-qa: `channel-sidebar-channel`

| prop | shape | seen |
|---|---|---|
| `channelSection` | {emoji: string, id: string, name: string, nextChannelSectionId: string, type: string ("channels"|"direct_messages"|"recent_apps")} | 217/217 |
| `finalDisplayedChannelSectionId` | string | 217/217 |
| `id` | string | 217/217 |
| `isHighlightedInAgentsTree?` | undefined | 217/217 |
| `onEditModeSelect` | fn | 217/217 |
| `onEnterEditModeClick` | fn | 217/217 |
| `pinnedIndex?` | undefined | 217/217 |
| `priorDisplayedChannelSectionId` | string | 217/217 |
| `rowKey` | string | 217/217 |
| `rowPosition` | {isFirstInSection: boolean, isLastInSection: boolean, position: string ("top-half"|"bottom-half"|"middle")} | 217/217 |
| `sectionChannelIndex` | number | 217/217 |

### BaseChannelPrefix

- type: plain · fibers seen: 217 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-channel_sidebar__user_avatar` `p-channel_sidebar__channel_icon_prefix`
- data-qa: `sidebar-channel-avatar-prefix`

| prop | shape | seen |
|---|---|---|
| `channelNameNormalized` | string | 217/217 |
| `data-qa` | string | 217/217 |
| `iconType` | string | 217/217 |
| `includePartyHat` | boolean | 217/217 |
| `isAppIm` | boolean | 217/217 |
| `isMuted` | boolean | 217/217 |
| `isPrivacyModeEnabled` | boolean | 217/217 |
| `isSelected` | boolean | 217/217 |
| `isSlackbotChannel` | boolean | 217/217 |
| `isSlackSecurityBotChannel` | boolean | 217/217 |
| `isTyping` | boolean | 217/217 |
| `isUnread` | boolean | 217/217 |
| `memberIdFromIm?` | string | 217/217 |
| `mpimMember?` | undefined | 217/217 |
| `showAvatars` | boolean | 217/217 |
| `showPresence` | boolean | 217/217 |
| `tooltipText?` | undefined | 217/217 |

### BaseChannelName

- type: plain · fibers seen: 217 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: `p-channel_sidebar__name` `p-channel_sidebar__name--away`
- data-qa: `channel_sidebar_name_announcements` `channel_sidebar_name_ᅟ` `channel_sidebar_name_clod` `channel_sidebar_name_teamline` `channel_sidebar_name_shroud` `channel_sidebar_name_slack` `channel_sidebar_name_welcomer`

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 217/217 |
| `channelIsIm` | boolean | 217/217 |
| `channelIsMpim` | boolean | 217/217 |
| `channelTeam?` | undefined | 217/217 |
| `channelTeamAtRowSize` | boolean | 217/217 |
| `codeChannelAgentTooltipText?` | undefined | 217/217 |
| `codeChannelTooltipText?` | undefined | 217/217 |
| `data-qa` | string | 217/217 |
| `disableTooltip` | boolean | 217/217 |
| `displayName` | string | 217/217 |
| `documentType?` | undefined | 217/217 |
| `externalOrgNameForMember?` | undefined | 217/217 |
| `hasSharedChannelTooltip` | boolean | 217/217 |
| `isCodeChannel` | boolean | 217/217 |
| `isFrontlineMember` | boolean | 217/217 |
| `isGuestMember` | boolean | 217/217 |
| `isPrivacyModeEnabled` | boolean | 217/217 |
| `isRecordChannel` | boolean | 217/217 |
| `isUnknown` | boolean | 217/217 |
| `isYou` | boolean | 217/217 |
| `memberIdFromIm?` | string | 217/217 |
| `members` | null | 217/217 |
| `mpdmMemberNames` | string | 217/217 |
| `qaChannelName` | string | 217/217 |
| `recordLabel?` | undefined | 217/217 |
| `shouldShowInviterText` | boolean | 217/217 |
| `userActive` | boolean | 217/217 |

### BaseChannelNameDisplayName

- type: plain · fibers seen: 217 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelIsIm` | boolean | 217/217 |
| `channelIsMpim` | boolean | 217/217 |
| `channelTeam?` | undefined | 217/217 |
| `channelTeamAtRowSize` | boolean | 217/217 |
| `displayName` | string | 217/217 |
| `documentType?` | undefined | 217/217 |
| `externalOrgNameForMember?` | undefined | 217/217 |
| `isFrontlineMember` | boolean | 217/217 |
| `isGuestMember` | boolean | 217/217 |
| `isUnknown` | boolean | 217/217 |
| `isYou` | boolean | 217/217 |
| `members?` | undefined | 217/217 |
| `mpdmMemberNames` | string | 217/217 |
| `shouldShowInviterText` | boolean | 217/217 |
| `userId?` | string | 217/217 |

### BaseChannelSuffix

- type: plain · fibers seen: 217 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: `p-channel_sidebar__channel_suffix`
- data-qa: `sidebar-channel-suffix`

| prop | shape | seen |
|---|---|---|
| `activityPeekHoverAreaRef?` | undefined | 217/217 |
| `badgeCount` | number | 217/217 |
| `channelId` | string | 217/217 |
| `className` | string | 217/217 |
| `data-qa` | string | 217/217 |
| `displayAsHovered` | boolean | 217/217 |
| `dualMentionCount` | number | 217/217 |
| `experimentHuddlesStatusUltraGroupOn` | boolean | 217/217 |
| `experimentMeetingsSidebarGroupOn` | boolean | 217/217 |
| `hasDraftText` | boolean | 217/217 |
| `hasFailedMessage` | boolean | 217/217 |
| `hasUnreadThreads` | boolean | 217/217 |
| `isHuddlesAllowed` | boolean | 217/217 |
| `isIm` | boolean | 217/217 |
| `isRowSelected` | boolean | 217/217 |
| `numDraftFiles` | number | 217/217 |
| `showDraft` | boolean | 217/217 |
| `showPending` | boolean | 217/217 |
| `sidebarBadgingVariant?` | undefined | 217/217 |
| `threadUnreadsVisibility` | string | 217/217 |

### DualActivityPeek

- type: plain · fibers seen: 217 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 217/217 |
| `disableHover` | boolean | 217/217 |
| `displayAsHovered` | boolean | 217/217 |
| `hoverAreaRef?` | undefined | 217/217 |
| `isDmOrApp` | boolean | 217/217 |
| `isRowSelected` | boolean | 217/217 |
| `mentionCount` | number | 217/217 |
| `peekId` | string | 217/217 |
| `sidebarBadgingVariant?` | undefined | 217/217 |
| `silentCollapse` | boolean | 217/217 |
| `threadUnreadsCount` | number | 217/217 |
| `threadUnreadsVisibility` | string | 217/217 |
| `totalCount` | number | 217/217 |

### SidebarRowHoverActions

- type: plain · fibers seen: 217 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-channel_sidebar__hover_actions`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `onClose` | fn | 217/217 |
| `showCloseButton` | boolean | 217/217 |

### RecordChannelIntroTabCoachmark

- type: plain · fibers seen: 214 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `draggable_tab__uEikv`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelId?` | string | 214/214 |
| `tabId` | string | 214/214 |
| `children` | node | 214/214 |

### DraggableDroppableTabItem

- type: plain · fibers seen: 214 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `draggable_tab__uEikv`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `className` | string | 214/214 |
| `draggingClassName` | string | 214/214 |
| `folderBookmarkId?` | undefined | 214/214 |
| `index` | number | 214/214 |
| `isFocused` | boolean | 214/214 |
| `isFolderTab` | boolean | 214/214 |
| `isHidden` | boolean | 214/214 |
| `isLastVisibleTab` | boolean | 214/214 |
| `isSelected` | boolean | 214/214 |
| `observer` | ResizeObserver | 214/214 |
| `onAttachmentDrop` | fn | 214/214 |
| `onClick` | fn | 214/214 |
| `onDrop` | fn | 214/214 |
| `onFolderDrop?` | undefined | 214/214 |
| `ref` | fn | 214/214 |
| `setIsDragging` | fn | 214/214 |
| `tab` | {buttonWrapper?: fn, contextMenuTemplate?: array<object>, data-qa: string, id: string, isDragDisabled: boolean, label: node, aria-describedby?: string, ariaHaspopup?: undefined, autoClogProps?: {appId?: undefined, channelId: string, elementName?: undefined, elementType: number, elementValue: string, eventId: number, onClick: object, uiComponentName: string, uiComponentVariant: string}, className?: string, renderWrapper?: undefined, aria-label?: string, title?: node} | 214/214 |
| `tabsId` | string | 214/214 |
| `children` | array<boolean \| node \| null> | 214/214 |

### EmojiMenuTrigger

- type: memo · fibers seen: 202 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<button>`, `<div>` · classes: `c-button-unstyled` `p-message_input_unstyled` `p-message_input_unstyled--attachments-visible` `c-icon_button` `c-icon_button--size_smedium` `c-wysiwyg_container__button` `c-wysiwyg_container__button--emoji` `skin-tone-` `c-icon_button--default` `p-workspace__input` `p-threads_footer__input` `c-reaction_add`
- data-qa: `emoji_toolbar_button` `add_reaction_button`

| prop | shape | seen |
|---|---|---|
| `channelId?` | string | 18/202 |
| `customTrigger?` | boolean | 92/202 |
| `enableGifPicker?` | boolean | 92/202 |
| `forceOpen?` | boolean | 92/202 |
| `offsetX?` | number | 92/202 |
| `offsetY?` | number | 92/202 |
| `onClick?` | fn | 110/202 |
| `onClose?` | fn | 184/202 |
| `onEmojiSelected` | fn | 202/202 |
| `onGifSelected?` | fn | 92/202 |
| `openedBy?` | string | 92/202 |
| `openNativeGifPicker?` | boolean | 92/202 |
| `position?` | string ("top-left") | 92/202 |
| `prefGifPickerEnabled` | boolean | 202/202 |
| `setUserPrefByApi` | fn | 202/202 |
| `shouldPropagateOnClose?` | undefined | 18/202 |
| `shouldShowReacjiTriggers?` | boolean | 18/202 |
| `showHandyReactions?` | boolean | 110/202 |
| `standardEmojiOnly?` | boolean | 92/202 |
| `targetBounds?` | undefined | 92/202 |
| `tryOppositePosition?` | boolean | 92/202 |
| `children` | node | 202/202 |

### FilesMetaTextWrapper

- type: memo · fibers seen: 200 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `c-message_kit__file`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `areSkeletonsLoading` | boolean | 200/200 |
| `blocksContainerContext?` | undefined | 200/200 |
| `canRemoveFiles` | boolean | 200/200 |
| `clogFileRemoved` | fn | 200/200 |
| `clogMessageClick` | fn | 200/200 |
| `collapseFromUnverifiedUser` | boolean | 200/200 |
| `downloadFiles` | fn | 200/200 |
| `enqueueEnablePopupsToast` | fn | 200/200 |
| `expandableKey` | string | 200/200 |
| `expandPref` | boolean | 200/200 |
| `experimentChannelMeetingInviteGroupOn` | boolean | 200/200 |
| `experimentNewEmailRenderingGroupOn` | boolean | 200/200 |
| `experimentPollsGroupOn` | boolean | 200/200 |
| `files` | array<map<string, array>> | 200/200 |
| `hasSkeletons` | boolean | 200/200 |
| `isExpanded?` | undefined | 200/200 |
| `isSkillFile` | boolean | 200/200 |
| `logger` | {action: fn, debug: fn, devHint: fn, error: fn, info: fn, teamOrEnterpriseId: string, timeStamp: fn, warn: fn} | 200/200 |
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: undefined, blocks?: array<object>, channel: string, crm_signal?: undefined, files: array<string>, language?: undefined, latest_reply?: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users?: array<string>, reply_users_count?: number, slackbot_feels: null, subtype?: undefined, text: string, thread_ts?: string, ts: string, type: string ("message"), upload: boolean, user: string, version?: string, client_msg_id?: string, bot_id?: string, edited?: {ts: string, user: string}, parent_user_id?: string, x_files?: array<string>, is_locked?: boolean} | 200/200 |
| `openDeleteMessageDialogCircular` | fn | 200/200 |
| `processingFileIds?` | undefined | 200/200 |
| `removeFile` | fn | 200/200 |
| `setExpanded` | fn | 200/200 |
| `setIsShowingNoCanvasDownloadsBanner` | fn | 200/200 |
| `skeletonAppId?` | undefined | 200/200 |
| `skeletonFile?` | undefined | 200/200 |
| `skeletonType?` | undefined | 200/200 |
| `userId` | string | 200/200 |
| `viewContext` | string | 200/200 |
| `workObjects?` | undefined | 200/200 |
| `children` | node | 200/200 |

### FileGallery

- type: memo · fibers seen: 200 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `areSkeletonsRemovable` | boolean | 200/200 |
| `channelId` | string | 200/200 |
| `clogMessageClick` | fn | 200/200 |
| `deletableFileIds` | array | 200/200 |
| `experimentAttachmentsMaxWidthGroupOn` | boolean | 200/200 |
| `experimentTab3pFilesFEAllowAddGroupOn` | boolean | 200/200 |
| `experimentUnfurlSkeletonsGroupOn` | boolean | 200/200 |
| `files` | array<map<string, array>> | 200/200 |
| `hideControls?` | undefined | 200/200 |
| `isMediaViewerStackable?` | undefined | 200/200 |
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: undefined, blocks?: array<object>, channel: string, crm_signal?: undefined, files: array<string>, language?: undefined, latest_reply?: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users?: array<string>, reply_users_count?: number, slackbot_feels: null, subtype?: undefined, text: string, thread_ts?: string, ts: string, type: string ("message"), upload: boolean, user: string, version?: string, client_msg_id?: string, bot_id?: string, edited?: {ts: string, user: string}, parent_user_id?: string, x_files?: array<string>, is_locked?: boolean} | 200/200 |
| `onRemoveFile` | fn | 200/200 |
| `onRemoveWorkObject` | fn | 200/200 |
| `removableFileIds` | array | 200/200 |
| `removableWorkObjectAttachmentIds` | array | 200/200 |
| `shouldUseCompactStack?` | undefined | 200/200 |
| `shouldUseMediaGallery` | boolean | 200/200 |
| `showActions` | boolean | 200/200 |
| `showFullscreenButton?` | undefined | 200/200 |
| `showPreview` | boolean | 200/200 |
| `showTranscriptButton?` | undefined | 200/200 |
| `skeletonMessageContainer` | string | 200/200 |
| `skeletons?` | undefined | 200/200 |
| `threadTs?` | string | 200/200 |
| `ts` | string | 200/200 |
| `viewContext` | string | 200/200 |
| `workObjects?` | undefined | 200/200 |

### ChannelTabLabelBaseComponent

- type: plain · fibers seen: 195 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 195/195 |
| `className?` | string | 9/195 |
| `hideBadge?` | boolean | 9/195 |
| `hideRichTextEmojiTooltips?` | boolean | 186/195 |
| `isFirstRelatedListTab?` | boolean | 186/195 |
| `isManageChannelTabsMenu?` | boolean | 9/195 |
| `labelClassName?` | undefined | 186/195 |
| `preventPeek?` | boolean | 9/195 |
| `renderPeek?` | fn | 186/195 |
| `tab` | {id: string, label: string, type: string ("channel"|"list"|"canvas"|"bookmarks"|"files"|"workflows"), data?: {fileId: string, sharedTs: string}, isDisabled?: boolean} | 195/195 |

### StyledChannelTabLabel

- type: plain · fibers seen: 195 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `className` | string | 195/195 |
| `fileId?` | string | 160/195 |
| `hasBadge` | boolean | 195/195 |
| `hasNewBadge` | boolean | 195/195 |
| `hasStaticNewBadge` | boolean | 195/195 |
| `hideRichTextEmojiTooltips` | boolean | 195/195 |
| `iconProps` | {name: string, size: string} | 195/195 |
| `isFirstRelatedListTab?` | boolean | 195/195 |
| `isManageChannelTabsMenu?` | boolean | 195/195 |
| `isSelected` | boolean | 195/195 |
| `label` | node \| string | 195/195 |
| `labelClassName` | string | 195/195 |
| `newBadgeName` | string | 195/195 |
| `peekIdPrefix` | string | 195/195 |
| `prState?` | undefined | 195/195 |
| `renderPeek?` | fn | 186/195 |
| `tab` | {id: string, label: string, type: string ("channel"|"list"|"canvas"|"bookmarks"|"files"|"workflows"), data?: {fileId: string, sharedTs: string}, isDisabled?: boolean} | 195/195 |

### TabPeekTrigger

- type: plain · fibers seen: 190 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-peek_trigger` `draggable__eJoxt`
- data-qa: `draggable_tab_item`

| prop | shape | seen |
|---|---|---|
| `disableHover?` | undefined | 38/190 |
| `forceTabWhenNavigating` | string | 190/190 |
| `offsetX?` | undefined | 38/190 |
| `peekId` | string | 190/190 |
| `position?` | undefined | 38/190 |
| `renderContent` | fn | 190/190 |
| `children` | node | 190/190 |

### BaseEntityText

- type: plain · fibers seen: 187 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: `c-base_entity__text` `c-file_entity__text` `c-member_name` `c-member__secondary-name` `p-member_profile_base_entity__name` `wrapping`
- data-qa: `member-entity__primary-name` `member-entity__secondary-name` `inline_member_entity__name`

| prop | shape | seen |
|---|---|---|
| `className?` | string | 180/187 |
| `data-qa?` | string | 51/187 |
| `inline?` | boolean | 27/187 |
| `multiline?` | boolean | 27/187 |
| `truncate?` | boolean | 7/187 |
| `withTruncationTooltip?` | boolean | 27/187 |
| `children` | node \| string | 187/187 |

### ChannelTabLabelWithPeek

- type: plain · fibers seen: 186 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 186/186 |
| `isFirstRelatedListTab` | boolean | 186/186 |
| `labelClassName?` | undefined | 186/186 |
| `tab` | {id: string, label: string, type: string ("channel"|"list"|"canvas"), data?: {fileId: string, sharedTs: string}} | 186/186 |

### MemberName

- type: plain · fibers seen: 182 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: `c-member_name` `c-base_entity__text` `p-member_profile_base_entity__name` `wrapping`
- data-qa: `member-entity__primary-name` `inline_member_entity__name`

| prop | shape | seen |
|---|---|---|
| `className?` | string | 1/182 |
| `data-qa?` | string | 26/182 |
| `highlightedDisplayName?` | undefined | 1/182 |
| `id?` | string | 155/182 |
| `inline?` | boolean | 2/182 |
| `invert?` | boolean | 25/182 |
| `isAgent?` | boolean | 27/182 |
| `isAgentforce?` | boolean | 25/182 |
| `isAppOrBot?` | boolean | 27/182 |
| `isBold?` | boolean | 1/182 |
| `isDeleted?` | boolean | 26/182 |
| `isDisabled?` | boolean | 25/182 |
| `isNonExistent?` | boolean | 27/182 |
| `isSelf?` | boolean | 26/182 |
| `isUnknown?` | boolean | 27/182 |
| `isUserHidden?` | boolean | 27/182 |
| `isWorkflowBot?` | boolean | 27/182 |
| `name?` | string | 27/182 |
| `showAppOrBotBadge?` | boolean | 27/182 |
| `showBadgeOnTextTruncation?` | undefined | 25/182 |
| `showDisabledText?` | boolean | 26/182 |
| `withTruncationTooltip?` | undefined | 25/182 |

### TruncationTooltip

- type: plain · fibers seen: 178 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: `c-truncate` `c-truncate--break_words`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `position?` | undefined | 178/178 |
| `tip` | node \| string | 178/178 |
| `tipAnchorRef?` | undefined | 178/178 |
| `tipClassName?` | undefined | 178/178 |
| `zIndex` | string | 178/178 |
| `children` | node | 178/178 |

### DraggableDroppable

- type: plain · fibers seen: 176 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `draggable_tab__uEikv`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `className` | string | 176/176 |
| `folderBookmarkId?` | undefined | 176/176 |
| `id` | string | 176/176 |
| `index` | number | 176/176 |
| `isFolderTab` | boolean | 176/176 |
| `isLastVisibleTab` | boolean | 176/176 |
| `label` | node | 176/176 |
| `onAttachmentDrop` | fn | 176/176 |
| `onDrop` | fn | 176/176 |
| `onFolderDrop?` | undefined | 176/176 |
| `setIsDragging` | fn | 176/176 |
| `type` | string ("tab") | 176/176 |
| `children` | node | 176/176 |

### BaseEntity

- type: plain · fibers seen: 161 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `c-base_entity` `c-base_entity--has-avatar` `c-base_entity--small` `c-file_entity` `c-base_entity--medium` `c-member` `c-member--medium` `c-member--with-presence` `p-rich_text_slug` `p-member_profile_base_entity`
- data-qa: `medium_member_entity` `rich_text_message_mention_element`

| prop | shape | seen |
|---|---|---|
| `avatar` | fn | 161/161 |
| `className` | string | 161/161 |
| `data-id?` | string | 135/161 |
| `data-qa?` | string | 160/161 |
| `data-sk?` | string | 153/161 |
| `data-stringify-channel-id?` | undefined | 7/161 |
| `data-stringify-tab-id?` | undefined | 7/161 |
| `data-stringify-text?` | string | 7/161 |
| `data-stringify-type?` | string | 7/161 |
| `forwardedRef?` | ref | 25/161 |
| `metadata?` | null \| node | 154/161 |
| `size` | string ("small"|"medium") | 161/161 |
| `style?` | undefined | 154/161 |
| `subtext?` | node | 26/161 |
| `text` | node | 161/161 |
| `useAvatarOutline?` | boolean | 128/161 |

### FormatTextWithEmoji

- type: plain · fibers seen: 159 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `emojiSize` | string | 159/159 |
| `inputIsEscaped?` | boolean | 127/159 |
| `showTooltips` | boolean | 159/159 |
| `stopAnimations?` | boolean | 127/159 |
| `text` | string | 159/159 |

### SidebarSimplificationIcon

- type: plain · fibers seen: 155 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: `p-channel_sidebar__link__icon` `c-icon--inherit`
- data-qa: `small-reply` `headphones` `paper-plane-alt` `bookmark` `unified-directory`

| prop | shape | seen |
|---|---|---|
| `iconLocation` | string | 155/155 |
| `name` | string | 155/155 |

### BaseChannelPrefixAvatar

- type: plain · fibers seen: 155 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-channel_sidebar__user_avatar`
- data-qa: `sidebar-channel-avatar-prefix`

| prop | shape | seen |
|---|---|---|
| `avatarSize?` | undefined | 155/155 |
| `className` | string | 155/155 |
| `includePartyHat` | boolean | 155/155 |
| `isAppIm` | boolean | 155/155 |
| `isSlackbotChannel` | boolean | 155/155 |
| `isSlackSecurityBotChannel` | boolean | 155/155 |
| `tooltipText?` | undefined | 155/155 |
| `userId` | string | 155/155 |

### BaseChannelNameImName

- type: plain · fibers seen: 155 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `externalOrgNameAtRowSize` | boolean | 155/155 |
| `externalOrgNameForMember?` | undefined | 155/155 |
| `isFrontlineMember` | boolean | 155/155 |
| `isGuestMember` | boolean | 155/155 |
| `isYou` | boolean | 155/155 |
| `shouldShowInviterText` | boolean | 155/155 |
| `userId` | string | 155/155 |

### BaseListEntity

- type: plain · fibers seen: 153 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `c-base_list_entity` `c-base_list_entity--small` `c-medium_member_list_entity` `c-base_list_entity--medium` `list_item_label__V2HbL`
- data-qa: `medium_member_list_entity`

| prop | shape | seen |
|---|---|---|
| `className` | string | 153/153 |
| `data-qa?` | string | 25/153 |
| `padding?` | undefined | 153/153 |
| `primaryContent` | node | 153/153 |
| `secondaryContent?` | boolean | 153/153 |
| `size` | string ("small"|"medium") | 153/153 |
| `style?` | undefined | 153/153 |

### FocusTransition

- type: plain · fibers seen: 152 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>`, `<style>` · classes: `p-team_sidebar__theme` `p-message_pane_input` `c-message_actions__container` `c-message__actions`
- data-qa: `message_input_container`

| prop | shape | seen |
|---|---|---|
| `listeners` | {workspace_switcher_tab_list?: fn, Channel_message_input?: fn, Channel_message_list?: fn, sidebar_current_channel?: fn, sidebar_display_rule_show_more_row?: fn, sidebar_edit_mode_key_offset?: fn, sidebar_exit_edit_mode_key_offset?: fn, sidebar_first_visible_row?: fn, sidebar_new_section?: fn, sidebar_previously_focused_item?: fn, sidebar_row_after_update?: fn, Thread_ai_app_chat_message_list?: fn, Thread_ai_app_split_message_list?: fn, Thread_message_input?: fn, Thread_message_list?: fn, huddle_threads_message_list?: fn, list-record-thread-flexpane_message_list?: fn, message_actions?: fn} | 152/152 |
| `children` | node \| array<node \| null> | 152/152 |

### BadgeAriaDescription

- type: plain · fibers seen: 152 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `badgeCount` | number | 152/152 |
| `tab` | string | 152/152 |

### ViewContainer

- type: plain · fibers seen: 152 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `enabled-managed-focus-container`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `dropTargetRef?` | fn | 114/152 |
| `shouldNavigateInContext` | boolean | 152/152 |
| `type` | string ("sidebar"|"primary"|"secondary"|"modal") | 152/152 |
| `windowId` | string | 152/152 |
| `children` | node \| array<node> | 152/152 |

### AutoTracing

- type: plain · fibers seen: 148 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-message_input_unstyled` `p-message_input_unstyled--attachments-visible` `p-channel_sidebar__list` `p-workspace__input` `p-threads_footer__input` `p-emoji_picker` `c-menu` `p-emoji_picker--style-v2` `p-emoji_picker--with_gif_picker`
- data-qa: `emoji-picker`

| prop | shape | seen |
|---|---|---|
| `name` | string | 148/148 |
| `children` | node | 148/148 |

### AttachmentFooter

- type: memo · fibers seen: 132 · states: channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: `c-message_attachment__footer`
- data-qa: `attachment-footer`

| prop | shape | seen |
|---|---|---|
| `allowPrivateMessageForwarding` | boolean | 132/132 |
| `channelId?` | undefined | 132/132 |
| `channelIsProbablyPrivate` | boolean | 132/132 |
| `clogLinkClick` | fn | 132/132 |
| `dispatch` | fn | 132/132 |
| `documentUnfurlTsText?` | undefined | 132/132 |
| `icon?` | string | 132/132 |
| `isThreadShareMessage` | boolean | 132/132 |
| `isUrlFromUnjoinable` | boolean | 132/132 |
| `ref` | null | 132/132 |
| `shouldHideUnfurlDate` | boolean | 132/132 |
| `shouldUseTruncate?` | boolean | 30/132 |
| `text` | string | 132/132 |
| `threadTs?` | undefined | 132/132 |
| `ts` | number \| string | 132/132 |
| `tsLink?` | string | 102/132 |

### AnimatedEditorAvatar

- type: plain · fibers seen: 128 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `avatar_toast__ILee5`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `fileId` | string | 128/128 |

### SmallFileListEntity

- type: plain · fibers seen: 128 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `c-base_list_entity` `c-base_list_entity--small` `list_item_label__V2HbL`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `className` | string | 128/128 |
| `hideRichTextEmojiTooltips` | boolean | 128/128 |
| `iconSize` | string | 128/128 |
| `id` | string | 128/128 |
| `labelOverride?` | undefined | 128/128 |
| `shouldShowDetailText` | boolean | 128/128 |
| `shouldShowHighlight` | boolean | 128/128 |
| `useFilledIcon` | boolean | 128/128 |
| `useLeadingEmojiAsIcon` | boolean | 128/128 |
| `useMissingIconIfFileUnknown` | boolean | 128/128 |
| `useMonochromeExternalFileIcon` | boolean | 128/128 |

### SmallFileEntity

- type: plain · fibers seen: 128 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `c-file_entity` `c-base_entity` `c-base_entity--small` `c-base_entity--has-avatar`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `data-id` | string | 128/128 |
| `data-qa?` | undefined | 128/128 |
| `data-sk` | string | 128/128 |
| `detailText` | string | 128/128 |
| `detailType?` | undefined | 128/128 |
| `dispatch` | fn | 128/128 |
| `file?` | undefined | 128/128 |
| `fileFromPropsOrState` | {id: string, isNonExistent: boolean} | 128/128 |
| `fontWeight?` | undefined | 128/128 |
| `hideRichTextEmojiTooltips` | boolean | 128/128 |
| `highlightQuery?` | undefined | 128/128 |
| `iconSize` | string | 128/128 |
| `id` | string | 128/128 |
| `labelOverride?` | undefined | 128/128 |
| `shouldCheckForTemporaryFiles?` | undefined | 128/128 |
| `shouldShowDetailText` | boolean | 128/128 |
| `shouldShowHighlight` | boolean | 128/128 |
| `textClassName?` | undefined | 128/128 |
| `useFilledIcon` | boolean | 128/128 |
| `useIconOutline` | boolean | 128/128 |
| `useLeadingEmojiAsIcon` | boolean | 128/128 |
| `useLiveTitleForChannelCanvas?` | undefined | 128/128 |
| `useMissingIconIfFileUnknown` | boolean | 128/128 |
| `useMonochromeExternalFileIcon` | boolean | 128/128 |
| `useSmallProductIcon?` | undefined | 128/128 |

### FormatTextWithHighlightMarkers

- type: plain · fibers seen: 127 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `inputIsEscaped?` | boolean | 76/127 |
| `showTooltips` | boolean | 127/127 |
| `text` | string | 127/127 |

### SectionHeadingRowElement

- type: plain · fibers seen: 124 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-channel_sidebar__section_heading`
- data-qa: `stars` `channels` `direct_messages` `recent_apps`

| prop | shape | seen |
|---|---|---|
| `addedUserGroupSectionCoachmarkSectionId?` | undefined | 124/124 |
| `ariaOwns` | string | 124/124 |
| `channelIds` | array<string> | 124/124 |
| `channelSection` | {emoji: string, id: string, name: string, nextChannelSectionId: string, type: string ("stars"|"channels"|"direct_messages"|"recent_apps")} | 124/124 |
| `channelSectionHasAllMutedHiddenChannels` | boolean | 124/124 |
| `channelSectionMutedHiddenChannels` | array | 124/124 |
| `channelSectionReadHiddenChannels` | array | 124/124 |
| `ensureItemInView` | fn | 124/124 |
| `finalDisplayedChannelSectionId` | string | 124/124 |
| `isCollapsed` | boolean | 124/124 |
| `isFirstVisibleSection` | boolean | 124/124 |
| `isFocused` | boolean | 124/124 |
| `isSelected` | boolean | 124/124 |
| `onChannelSectionCollapseToggle` | fn | 124/124 |
| `onEnterEditModeClick` | fn | 124/124 |
| `priorDisplayedChannelSectionId?` | string | 124/124 |
| `rowPosition` | {isFirstInSection: boolean, isLastInSection: boolean, position: string ("top-half")} | 124/124 |
| `sidebarListRowKey` | string | 124/124 |
| `sidebarSections` | map<string, {channelSection: object, channelSectionSidebarBehavior: string, channelsAssociatedOrdered: array, channelsOrdered: array, dormantHiddenChannelsOrdered: array, hasAllMutedHiddenChannels: boolean, isCollapsed: boolean, mutedHiddenChannelsOrdered: array, readHiddenChannels: array, readHiddenChannelsOrdered: array}> | 124/124 |
| `starredChannelSectionBadgeResult` | {onBadgeClick: fn, shouldForceSectionCollapsed: boolean, shouldShowBadge: boolean} | 124/124 |

### BaseChannelSectionHeading

- type: plain · fibers seen: 124 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-channel_sidebar__section_heading`
- data-qa: `stars` `channels` `direct_messages` `recent_apps`

| prop | shape | seen |
|---|---|---|
| `addedUserGroupSectionCoachmarkSectionId?` | undefined | 124/124 |
| `channelIds` | array<string> | 124/124 |
| `channelSection` | {emoji: string, id: string, name: string, nextChannelSectionId: string, type: string ("stars"|"channels"|"direct_messages"|"recent_apps")} | 124/124 |
| `channelSectionHasAllMutedHiddenChannels` | boolean | 124/124 |
| `channelSectionMutedHiddenChannels` | array | 124/124 |
| `channelSectionReadHiddenChannels` | array | 124/124 |
| `ensureItemInView` | fn | 124/124 |
| `finalDisplayedChannelSectionId` | string | 124/124 |
| `isCollapsed` | boolean | 124/124 |
| `isFirstVisibleSection` | boolean | 124/124 |
| `isFocused` | boolean | 124/124 |
| `isSelected` | boolean | 124/124 |
| `onChannelSectionCollapseToggle` | fn | 124/124 |
| `onEnterEditModeClick` | fn | 124/124 |
| `priorDisplayedChannelSectionId?` | string | 124/124 |
| `rowPosition` | {isFirstInSection: boolean, isLastInSection: boolean, position: string ("top-half")} | 124/124 |
| `sidebarListRowKey` | string | 124/124 |
| `sidebarSections` | map<string, {channelSection: object, channelSectionSidebarBehavior: string, channelsAssociatedOrdered: array, channelsOrdered: array, dormantHiddenChannelsOrdered: array, hasAllMutedHiddenChannels: boolean, isCollapsed: boolean, mutedHiddenChannelsOrdered: array, readHiddenChannels: array, readHiddenChannelsOrdered: array}> | 124/124 |
| `starredChannelSectionBadgeResult` | {onBadgeClick: fn, shouldForceSectionCollapsed: boolean, shouldShowBadge: boolean} | 124/124 |

### SectionHeadingOverlays

- type: plain · fibers seen: 124 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-channel_sidebar__section_heading`
- data-qa: `stars` `channels` `direct_messages` `recent_apps`

| prop | shape | seen |
|---|---|---|
| `addedUserGroupSectionCoachmarkSectionId?` | undefined | 124/124 |
| `channelIds` | array<string> | 124/124 |
| `channelSection` | {emoji: string, id: string, name: string, nextChannelSectionId: string, type: string ("stars"|"channels"|"direct_messages"|"recent_apps")} | 124/124 |
| `ensureItemInView` | fn | 124/124 |
| `sidebarListRowKey` | string | 124/124 |
| `children` | node | 124/124 |

### SectionMenuNewInternal

- type: plain · fibers seen: 124 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `container__bxot2`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `badgeCount` | number | 124/124 |
| `channelIds` | array<string> | 124/124 |
| `channelSection` | {emoji: string, id: string, name: string, nextChannelSectionId: string, type: string ("stars"|"channels"|"direct_messages"|"recent_apps")} | 124/124 |
| `channelSectionHasAllMutedHiddenChannels` | boolean | 124/124 |
| `channelSectionMutedHiddenChannels` | array | 124/124 |
| `channelSectionReadHiddenChannels` | array | 124/124 |
| `displayAsHovered` | boolean | 124/124 |
| `dormantHiddenChannelIds` | array | 124/124 |
| `emoji` | string | 124/124 |
| `getTemplate` | fn | 124/124 |
| `hasFocus` | boolean | 124/124 |
| `hasMoreSlackbotConversations` | boolean | 124/124 |
| `hasUnmutedUnreads` | boolean | 124/124 |
| `isBadgeForAllMuted` | boolean | 124/124 |
| `isCollapsed` | boolean | 124/124 |
| `isSelected` | boolean | 124/124 |
| `isVipAvailableForCurrentTeam` | boolean | 124/124 |
| `label` | string | 124/124 |
| `onCollapseToggleClick` | fn | 124/124 |
| `onPlusClick` | fn | 124/124 |
| `onSectionMenuClose` | fn | 124/124 |
| `onSectionMenuOpen` | fn | 124/124 |
| `peekChannelIds` | array | 124/124 |
| `peekMentionChannelIds` | array | 124/124 |
| `qaLabel` | string | 124/124 |
| `showBehavior` | string | 124/124 |
| `showSpinner` | boolean | 124/124 |
| `starredChannelSectionBadgeResult` | {onBadgeClick: fn, shouldForceSectionCollapsed: boolean, shouldShowBadge: boolean} | 124/124 |

### SectionMenuPlusButton

- type: plain · fibers seen: 124 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<button>` · classes: `c-button-unstyled` `sectionbutton__CadG9` `plusButton__W0iNT`
- data-qa: `section_heading_button_plus__channels` `section_heading_button_plus__direct_messages`

| prop | shape | seen |
|---|---|---|
| `channelSection` | {emoji: string, id: string, name: string, nextChannelSectionId: string, type: string ("stars"|"channels"|"direct_messages"|"recent_apps")} | 124/124 |
| `className` | string | 124/124 |
| `onClick` | fn | 124/124 |
| `qaLabel` | string | 124/124 |

### SectionMenuMarkAsReadButton

- type: plain · fibers seen: 124 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelIds` | array<string> | 124/124 |
| `channelSection` | {emoji: string, id: string, name: string, nextChannelSectionId: string, type: string ("stars"|"channels"|"direct_messages"|"recent_apps")} | 124/124 |
| `isSelected` | boolean | 124/124 |
| `qaLabel` | string | 124/124 |
| `shouldShowButtons` | boolean | 124/124 |

### SectionMenuEllipsisButton

- type: plain · fibers seen: 124 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<button>` · classes: `c-button-unstyled` `sectionbutton__CadG9` `ellipsis__Lwi05`
- data-qa: `section_heading_button_ellipsis__stars` `section_heading_button_ellipsis__channels` `section_heading_button_ellipsis__direct_messages` `section_heading_button_ellipsis__recent_apps`

| prop | shape | seen |
|---|---|---|
| `badgeCount` | number | 124/124 |
| `channelSection` | {emoji: string, id: string, name: string, nextChannelSectionId: string, type: string ("stars"|"channels"|"direct_messages"|"recent_apps")} | 124/124 |
| `getTemplate` | fn | 124/124 |
| `hasUnmutedUnreads` | boolean | 124/124 |
| `isCollapsed` | boolean | 124/124 |
| `isHiddenChannelsMenuOpen` | boolean | 124/124 |
| `isSelected` | boolean | 124/124 |
| `onSectionMenuClose` | fn | 124/124 |
| `onSectionMenuOpen` | fn | 124/124 |
| `qaLabel` | string | 124/124 |
| `shouldShowButtons` | boolean | 124/124 |

### FileContextMenuTrigger

- type: plain · fibers seen: 124 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `wrapper__nq4fb`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 124/124 |
| `disabled` | boolean | 124/124 |
| `file` | map<string, array> | 124/124 |
| `viewContext` | string | 124/124 |
| `children` | node | 124/124 |

### FileRemoveButton

- type: plain · fibers seen: 124 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `fileId` | string | 124/124 |

### ViewSuspense

- type: plain · fibers seen: 114 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-view_contents` `nested` `p-view_contents--primary-visible` `p-view_contents--secondary-visible` `p-view_contents--primary` `p-view_contents--sidebar` `sk-client-theme--light-inverted-sidebar` `p-view_contents--secondary` `p-view_contents--no-sidebar`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `className?` | string | 38/114 |
| `children` | array<node \| boolean> \| node | 114/114 |

### ViewContents

- type: plain · fibers seen: 114 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-view_contents` `nested` `p-view_contents--primary-visible` `p-view_contents--secondary-visible` `p-view_contents--primary` `p-view_contents--sidebar` `sk-client-theme--light-inverted-sidebar` `p-view_contents--secondary` `p-view_contents--no-sidebar`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `className` | string | 114/114 |
| `containerRef?` | fn | 38/114 |

### FileImageThumbnailImg

- type: plain · fibers seen: 109 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<img>` · classes: `p-file_image_thumbnail__image` `p-file_image_thumbnail__image--gallery_v3`
- data-qa: `file_image_thumbnail_img`

| prop | shape | seen |
|---|---|---|
| `alt` | string | 109/109 |
| `className` | string | 109/109 |
| `crossOrigin?` | undefined | 109/109 |
| `data-needs-load?` | undefined | 109/109 |
| `data-qa` | string | 109/109 |
| `height` | number | 109/109 |
| `onError` | fn | 109/109 |
| `onLoad` | fn | 109/109 |
| `src` | string | 109/109 |
| `width` | number | 109/109 |

### FileImageAltBadge

- type: plain · fibers seen: 109 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `dialogEntrypoint` | string | 109/109 |
| `file` | map<string, array> | 109/109 |
| `fileIds` | array<string> | 109/109 |
| `onImageDescriptionClose` | fn | 109/109 |
| `showActions` | boolean | 109/109 |

### FileImageAiGeneratedBadge

- type: plain · fibers seen: 109 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `file` | map<string, array> | 109/109 |

### BaseMrkdwnLink

- type: plain · fibers seen: 108 · states: channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: —
- data-qa: `mrkdwn-link-url-hover-card`

| prop | shape | seen |
|---|---|---|
| `appId?` | undefined | 108/108 |
| `botId?` | undefined | 108/108 |
| `botName?` | undefined | 108/108 |
| `clogCitationImpression?` | fn | 78/108 |
| `clogLinkClick` | fn | 108/108 |
| `doSlackAction` | fn | 108/108 |
| `doSlackActionLinks?` | boolean | 30/108 |
| `experimentCodeChannelsPrLinkTabsGroupOn` | boolean | 108/108 |
| `experimentTruncatedUrlHoverCardGroupOn` | boolean | 108/108 |
| `experimentWebIdentityLinksGroupTreatment` | boolean | 108/108 |
| `explicitlyRenameBotLink?` | boolean | 30/108 |
| `isFileListAndNotAccessible` | boolean | 108/108 |
| `isFileQuipAndNotAccessible` | boolean | 108/108 |
| `isFromAppHome` | boolean | 108/108 |
| `isShortcutLinkTrigger` | boolean | 108/108 |
| `isTruncatedLink?` | undefined | 78/108 |
| `maybeOpenLlmLinkDialog?` | fn | 78/108 |
| `maybeOpenSuspiciousLinkDialog` | fn | 108/108 |
| `memberId?` | undefined | 108/108 |
| `noLinking` | boolean | 108/108 |
| `noTabbableContent?` | boolean | 78/108 |
| `origin?` | undefined | 30/108 |
| `permalinkArticleId?` | undefined | 108/108 |
| `permalinkChannelId?` | undefined | 108/108 |
| `permalinkEntityId?` | undefined | 108/108 |
| `permalinkFileId?` | string | 108/108 |
| `permalinkMemberId?` | undefined | 108/108 |
| `permalinkRecordId?` | undefined | 108/108 |
| `permalinkRoomId?` | undefined | 108/108 |
| `permalinkTs?` | undefined | 108/108 |
| `permalinkType?` | string | 108/108 |
| `plainTextLabel?` | string | 78/108 |
| `requestAccessModalThunk` | fn | 108/108 |
| `showTooltip` | boolean | 108/108 |
| `suppressLinkWarningPref` | boolean | 108/108 |
| `triggerId?` | undefined | 108/108 |
| `url` | string | 108/108 |
| `urlIsSIWSLink` | boolean | 108/108 |
| `children` | string \| array<array<string> \| node> | 108/108 |

### UrlHoverCardTrigger

- type: plain · fibers seen: 108 · states: channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: —
- data-qa: `mrkdwn-link-url-hover-card`

| prop | shape | seen |
|---|---|---|
| `dataQa` | string | 108/108 |
| `url` | string | 108/108 |
| `children` | node | 108/108 |

### DismissOnClick

- type: plain · fibers seen: 108 · states: channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `children` | node | 108/108 |

### DraggableMrkdwnLink

- type: plain · fibers seen: 108 · states: channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: `c-mrkdwn__draggable-link`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `clogLinkClick` | fn | 108/108 |
| `data-stringify-link` | string | 108/108 |
| `data-truncated-link?` | undefined | 108/108 |
| `href` | string | 108/108 |
| `isFromAppHome` | boolean | 108/108 |
| `maybeOpenLlmLinkDialog` | fn | 108/108 |
| `maybeOpenSuspiciousLinkDialog` | fn | 108/108 |
| `onMouseEnter` | fn | 108/108 |
| `openThreadForRootMessage` | boolean | 108/108 |
| `plainTextLabel?` | string | 108/108 |
| `tabIndex?` | undefined | 108/108 |
| `url` | string | 108/108 |
| `children` | string \| array<array<string> \| node> | 108/108 |

### ChannelTeam

- type: memo · fibers seen: 102 · states: activity, activity-menu-Filters, activity-menu-Settings, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channel` | map<string, string \| boolean> | 102/102 |
| `className` | string | 102/102 |
| `data-qa` | string | 102/102 |
| `dispatch` | fn | 102/102 |
| `iconSize` | number | 102/102 |
| `shouldRender` | boolean | 102/102 |
| `showTeamName?` | undefined | 102/102 |
| `team?` | undefined | 102/102 |

### AvatarMasks

- type: plain · fibers seen: 100 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<svg>` · classes: `sr-only`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `hasNoAccess?` | undefined | 100/100 |
| `isDnd` | boolean | 100/100 |
| `size` | string ("small"|"base") | 100/100 |
| `userType` | string | 100/100 |

### QuipTitleParsedId

- type: plain · fibers seen: 96 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-sui-surfaces_readonly-blocks` `blocks__h3Y_k` `p-block_kit_renderer--inline` `p-quip_title__blocks_simple` `p-quip_title__blocks__undefined`
- data-qa: `block-kit-renderer`

| prop | shape | seen |
|---|---|---|
| `fileId` | string | 96/96 |
| `fileOverride` | map<string, array> | 96/96 |
| `hideRichTextEmojiTooltips` | boolean | 96/96 |
| `highlightQuery?` | undefined | 96/96 |
| `preferTitleBlocksFromFileObject` | boolean | 96/96 |
| `size?` | undefined | 96/96 |
| `stopAnimations` | boolean | 96/96 |
| `truncate` | boolean | 96/96 |
| `useLeadingEmojiAsIcon` | boolean | 96/96 |

### ReadonlyBlocks

- type: plain · fibers seen: 96 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-sui-surfaces_readonly-blocks` `blocks__h3Y_k` `p-block_kit_renderer--inline` `p-quip_title__blocks_simple` `p-quip_title__blocks__undefined`
- data-qa: `block-kit-renderer`

| prop | shape | seen |
|---|---|---|
| `blocks` | array<{elements: array, type: string ("rich_text"), block_id?: string}> | 96/96 |
| `className` | string | 96/96 |
| `containerContext` | string | 96/96 |
| `disableHighlights` | boolean | 96/96 |
| `disableJumboEmoji` | boolean | 96/96 |
| `hideRichTextEmojiTooltips` | boolean | 96/96 |
| `inline` | boolean | 96/96 |
| `noInlineLinks` | boolean | 96/96 |
| `stopAnimations` | boolean | 96/96 |

### PageRowElement

- type: plain · fibers seen: 93 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `glowClassName?` | undefined | 93/93 |
| `id` | string | 93/93 |
| `isDisabled` | boolean | 93/93 |

### Scrollbar

- type: plain · fibers seen: 92 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `c-scrollbar` `c-virtual_list` `c-virtual_list--scrollbar` `c-scrollbar--fade` `c-scrollbar--hidden` `p-channel_sidebar__static_list` `c-message_list` `p-activity_ia4_list` `p-activity_ia4_list--large` `p-activity_ia4_list--refreshed_separators` `p-activity_ia4_list--bordered_separators` `p-activity_ia4_list--compact` `p-activity_ia4_list--scrolled` `c-scrollbar--inherit_size`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `alwaysVisible?` | boolean | 87/92 |
| `anchor?` | string | 87/92 |
| `className?` | string | 87/92 |
| `contentHeight` | number | 92/92 |
| `fade?` | boolean | 87/92 |
| `forceDarkScrollbar?` | undefined | 87/92 |
| `height` | number | 92/92 |
| `id?` | string | 87/92 |
| `inheritSize?` | boolean | 5/92 |
| `inheritWidth?` | boolean | 87/92 |
| `initialScrollTop?` | number | 87/92 |
| `isChildWindow` | boolean | 92/92 |
| `isContentFullHeight?` | boolean | 5/92 |
| `isMainWindow` | boolean | 92/92 |
| `onScroll` | fn | 92/92 |
| `onTrackClick?` | fn | 87/92 |
| `ref` | fn \| null | 92/92 |
| `role?` | string ("presentation") | 87/92 |
| `slackKitConfig` | {enable24hrTime: boolean, locale: string, managedFocusContainer: {Component: object, coachmarkSectionIndex: number, workspaceSwitcherTabListSectionIndex: number}, modalHooks: {closeModal: fn}, popoverHooks: {decrementOpenCount: fn, incrementOpenCount: fn}, restoreFocusFallback: fn, scrollmarkHooks: {addScrollmarks: fn, getScrollmark: fn, removeScrollmarks: fn}, telemeter: Telemeter {endpointUrl: string, enterpriseId: string, recordedMetrics: object, sessionId: string, subAppName: string, teamId: string, teamSize?: undefined, userId: string, versionTs: string}, timeZone: string} | 92/92 |
| `trackTop?` | number | 87/92 |
| `useFixedTrackHeight?` | boolean | 87/92 |
| `useLegacyScrollbar?` | boolean | 87/92 |
| `width` | number | 92/92 |
| `windowId` | string | 92/92 |
| `windowRef` | WeakRef | 92/92 |
| `windowToken?` | undefined | 92/92 |
| `children` | array<node \| boolean> \| node | 92/92 |

### Content

- type: plain · fibers seen: 92 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `c-scrollbar__child`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `isContentFullHeight` | boolean | 92/92 |
| `width` | number | 92/92 |
| `children` | array<node \| boolean> \| node | 92/92 |

### FileDragDrop

- type: memo · fibers seen: 92 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-file_drag_drop__container` `p-threads_flexpane`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 92/92 |
| `className?` | string | 30/92 |
| `clogFileAttached` | fn | 92/92 |
| `disabled?` | boolean | 62/92 |
| `disableFileUploadsPref` | string | 92/92 |
| `draftId` | string | 92/92 |
| `fileAttachAfterSendDisabled` | boolean | 92/92 |
| `filterFilesForUpload` | fn | 92/92 |
| `goToChannel` | fn | 92/92 |
| `isMessageEdited` | boolean | 92/92 |
| `isRestrictedExternalHuddle` | boolean | 92/92 |
| `location` | string | 92/92 |
| `messageEditData?` | undefined | 92/92 |
| `messageEditUploadNewFiles` | fn | 92/92 |
| `onDrop?` | undefined | 30/92 |
| `sendBlockKitBlock` | fn | 92/92 |
| `slackConnectFileSharingBlocked` | boolean | 92/92 |
| `teamId` | string | 92/92 |
| `threadTs?` | string | 30/92 |
| `uploadDraftFiles` | fn | 92/92 |
| `uploadFiles` | fn | 92/92 |
| `uploadToNewDraft` | fn | 92/92 |
| `userCanPostInChannel` | boolean | 92/92 |
| `userCanPostInThread` | boolean | 92/92 |
| `viewContext` | string | 92/92 |
| `windowId` | string | 92/92 |
| `children` | node | 92/92 |

### TextyAutocomplete

- type: memo · fibers seen: 92 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `c-texty_input_unstyled__container` `c-texty_input_unstyled__container--size_medium` `c-texty_input_unstyled__container--multi_line` `c-texty_input_unstyled__container--no_actions`
- data-qa: —

_no props recorded_

### TextyInput

- type: memo · fibers seen: 92 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `c-texty_input_unstyled__container` `c-texty_input_unstyled__container--size_medium` `c-texty_input_unstyled__container--multi_line` `c-texty_input_unstyled__container--no_actions`
- data-qa: —

_no props recorded_

### ComposerAttachments

- type: memo · fibers seen: 92 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 92/92 |
| `dispatch` | fn | 92/92 |
| `draftBlockUnfurls` | array | 92/92 |
| `draftFiles` | array | 92/92 |
| `draftGifUnfurls` | array | 92/92 |
| `draftId` | string | 92/92 |
| `filesToSave?` | undefined | 92/92 |
| `focusInput` | fn | 92/92 |
| `hasForwardedMessages` | boolean | 92/92 |
| `onFilePermissionSelect` | fn | 92/92 |
| `onUnfurlRemoved` | fn | 92/92 |
| `shouldRenderCanvasUnfurls` | boolean | 92/92 |
| `shouldRenderDraftContactUnfurls` | boolean | 92/92 |
| `shouldRenderDraftUnfurls` | boolean | 92/92 |

### InlineChannelIcon

- type: plain · fibers seen: 88 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: `c-inline_channel_icon` `c-inline_channel_icon--space` `c-base_inline_entity__avatar` `p-view_header__channel_title_icon`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `bold` | boolean | 88/88 |
| `channel` | map<string, string \| boolean> | 88/88 |
| `className` | string | 88/88 |
| `data-qa?` | string | 51/88 |
| `disableArchived?` | boolean | 37/88 |
| `overrideHiddenIcon?` | undefined | 51/88 |
| `readChannelIcon?` | boolean | 51/88 |

### BasePresence

- type: plain · fibers seen: 85 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: `c-presence` `block` `c-presence--active` `c-avatar__presence` `c-member__presence` `c-presence--away` `is-inline`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `className?` | string | 84/85 |
| `data-qa?` | string | 15/85 |
| `hasNoAccess?` | undefined | 69/85 |
| `inheritIconSize?` | boolean | 69/85 |
| `inline?` | boolean | 84/85 |
| `invert?` | boolean | 15/85 |
| `isActive` | boolean | 85/85 |
| `isDnd` | boolean | 85/85 |
| `isSelf` | boolean | 85/85 |
| `isSystemUser` | boolean | 85/85 |
| `userType` | string | 85/85 |

### RenderedViewContents

- type: plain · fibers seen: 84 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-view_contents` `nested` `p-view_contents--primary-visible` `p-view_contents--secondary-visible` `p-view_contents--primary` `p-view_contents--sidebar` `sk-client-theme--light-inverted-sidebar` `p-view_contents--secondary` `p-view_contents--no-sidebar`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `className` | string | 84/84 |
| `containerRef?` | fn | 84/84 |
| `viewType` | string | 84/84 |
| `children` | node | 84/84 |

### AttachmentMedia

- type: memo · fibers seen: 78 · states: channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `c-message_attachment__media__inner_wrapper`
- data-qa: `attachment_media_inner_wrapper`

| prop | shape | seen |
|---|---|---|
| `activeChannelId` | string | 78/78 |
| `appUnfurlUrl?` | undefined | 78/78 |
| `audioHtml?` | undefined | 78/78 |
| `audioHtmlHeight?` | undefined | 78/78 |
| `backgroundSize` | string | 78/78 |
| `blocksContainerContext` | string | 78/78 |
| `clogLinkClick` | fn | 78/78 |
| `clogPlayButtonClick` | fn | 78/78 |
| `cropHeight` | boolean | 78/78 |
| `disableMediaActions` | boolean | 78/78 |
| `expandableKey` | string | 78/78 |
| `from_url?` | undefined | 78/78 |
| `fromUrl` | string | 78/78 |
| `imageBytes` | number | 78/78 |
| `imageHeight` | number | 78/78 |
| `imageUrl` | string | 78/78 |
| `imageWidth` | number | 78/78 |
| `messageTs` | string | 78/78 |
| `originalUrl` | string | 78/78 |
| `senderMemberId` | string | 78/78 |
| `thumbHeight?` | undefined | 78/78 |
| `thumbUrl?` | undefined | 78/78 |
| `thumbWidth?` | undefined | 78/78 |
| `title` | string | 78/78 |
| `titleLink` | string | 78/78 |
| `unsetImageWidthHeight` | boolean | 78/78 |
| `unsetWidthStyle?` | undefined | 78/78 |
| `useBackupBackground` | boolean | 78/78 |
| `videoHtml?` | undefined | 78/78 |
| `videoHtmlHeight?` | undefined | 78/78 |
| `videoHtmlWidth?` | undefined | 78/78 |

### BaseTeamIcon

- type: plain · fibers seen: 77 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<i>` · classes: `c-team_icon` `p-team_sidebar__image` `p-account_switcher__team_icon` `p-account_switcher__team_icon--current` `margin_right_25`
- data-qa: `team-icon`

| prop | shape | seen |
|---|---|---|
| `ariaHidden?` | boolean | 38/77 |
| `className` | string | 77/77 |
| `dispatch?` | fn | 1/77 |
| `icon` | {image_102: string, image_132: string, image_230: string, image_34: string, image_44: string, image_68: string, image_88: string, image_default: boolean} | 77/77 |
| `id?` | string | 1/77 |
| `size` | number | 77/77 |
| `teamName` | string | 77/77 |

### HistoryButtons

- type: plain · fibers seen: 76 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-ia4_history_buttons` `p-ia4_history_menu_button`
- data-qa: `history_menu_button`

_no props recorded_

### HistoryArrow

- type: plain · fibers seen: 76 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<button>` · classes: `c-button-unstyled` `p-ia4_history_buttons__button` `p-ia4_history_buttons__button-back` `p-ia4_history_buttons__button-forward`
- data-qa: `history_back_button` `history_forward_button`

| prop | shape | seen |
|---|---|---|
| `onContextMenuOpen` | fn | 76/76 |
| `onPeekOpen` | fn | 76/76 |
| `peekId` | string | 76/76 |
| `showHistoryMenuOnHover` | boolean | 76/76 |
| `showHistoryMenuOnRightClick` | boolean | 76/76 |
| `tooltip` | node | 76/76 |
| `children` | node | 76/76 |

### BaseSearch

- type: memo · fibers seen: 76 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-top_nav__search__container`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `clearSearchInput` | fn | 76/76 |
| `clogger` | Clogger {autoClogRecordedLogs: array, beaconThrew: boolean, enterpriseId: string, experimentExposureRecordedLogs: array, i18nLocale: string, logURLs: array, options: object, processingLogURLs: boolean, recordedLogs: array<object>, releasedHolds: Map, sendingLogURLs: Set, sessionIdFunc: fn, teamId: string, useBeacon: boolean, userId: string} | 76/76 |
| `currentRequestQuery` | string | 76/76 |
| `isGovSlackWorkspace` | boolean | 76/76 |
| `omniswitcherQuExperimentPlaceholderText?` | undefined | 76/76 |
| `openAdvancedSearchModal` | fn | 76/76 |
| `openFromClick` | fn | 76/76 |
| `plainTextQuery` | string | 76/76 |
| `ref` | null | 76/76 |
| `showTrialStartedCelebration` | boolean | 76/76 |
| `teamName` | string | 76/76 |

### WorkspaceDocumentTitle

- type: memo · fibers seen: 76 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `appBadgeCount` | number | 76/76 |
| `dispatch` | fn | 76/76 |
| `highlightCount` | number | 76/76 |
| `isWindowFocused` | boolean | 76/76 |
| `showMainWindowIcon` | boolean | 76/76 |
| `showMicrophoneIcon` | boolean | 76/76 |
| `showSpeakerIcon?` | undefined | 76/76 |
| `teamName` | string | 76/76 |
| `unreadCount` | number | 76/76 |
| `viewName` | string | 76/76 |

### Favicon

- type: memo · fibers seen: 76 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `dispatch` | fn | 76/76 |
| `href` | string | 76/76 |
| `logger` | {action: fn, debug: fn, devHint: fn, error: fn, info: fn, teamOrEnterpriseId: string, timeStamp: fn, warn: fn} | 76/76 |

## 4. Sidebar / header / message list / divider / huddle / thread / composer / activity components

### ActionsOverflowMenu

- type: plain · fibers seen: 64 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<button>` · classes: `c-button-unstyled` `c-icon_button` `c-icon_button--size_smedium` `c-message_actions__button` `c-icon_button--default`
- data-qa: `unstyled-button`

| prop | shape | seen |
|---|---|---|
| `activityItem` | {aiPriorityRecommenderSession?: undefined, channelId: string, feedTs: string, isArchived: boolean, isPriority: boolean, isSalesforceChannel?: undefined, isUnread: boolean, key: string, messageTs: string, snoozedUntil?: undefined, threadTs?: string, ts: string, type: string ("at_user"|"thread_v2"|"message_reaction"|"bot_dm_bundle"), unreadCount?: number, reactionName?: string, reactionUser?: string, latestTs?: string, minUnreadTs?: undefined} | 64/64 |
| `channelId` | string | 64/64 |
| `isDmTab` | boolean | 64/64 |
| `isPeekMode` | boolean | 64/64 |
| `setActionsVisible` | fn | 64/64 |
| `threadTs?` | string | 64/64 |
| `ts` | string | 64/64 |

### ActivityDecoupledTypeIcon

- type: plain · fibers seen: 37 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<span>` · classes: `activity_multi_line_row_content__entry_footer_type_icon__EJAWK`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `activityItem` | {aiPriorityRecommenderSession?: undefined, channelId: string, feedTs: string, isArchived: boolean, isPriority: boolean, isSalesforceChannel?: undefined, isUnread: boolean, key: string, messageTs: string, snoozedUntil?: undefined, threadTs?: string, ts: string, type: string ("at_user"|"thread_v2"), latestTs?: string, minUnreadTs?: undefined, unreadCount?: number} | 37/37 |
| `className` | string | 37/37 |
| `size` | string | 37/37 |

### ActivityEntryTypeIcon

- type: plain · fibers seen: 37 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<span>` · classes: `activity_multi_line_row_content__entry_footer_type_icon__EJAWK`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `activityItem` | {aiPriorityRecommenderSession?: undefined, channelId: string, feedTs: string, isArchived: boolean, isPriority: boolean, isSalesforceChannel?: undefined, isUnread: boolean, key: string, messageTs: string, snoozedUntil?: undefined, threadTs?: string, ts: string, type: string ("at_user"|"thread_v2"), latestTs?: string, minUnreadTs?: undefined, unreadCount?: number} | 37/37 |
| `className` | string | 37/37 |
| `decorative` | boolean | 37/37 |
| `restrictToStandaloneIconTypes?` | undefined | 37/37 |
| `size` | string | 37/37 |

### ActivityFilterActionButtons

- type: plain · fibers seen: 1 · states: activity-menu-Filters
- outer host: `<div>` · classes: `margin_50` `margin_right_100` `margin_left_100`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `hasUnsavedChanges` | boolean | 1/1 |
| `isCustomTab` | boolean | 1/1 |
| `onDelete` | fn | 1/1 |
| `onSave` | fn | 1/1 |
| `tabCount` | number | 1/1 |
| `tabId` | string | 1/1 |
| `viewName` | string | 1/1 |

### ActivityFilterBar

- type: plain · fibers seen: 7 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<div>` · classes: —
- data-qa: —

_no props recorded_

### ActivityFilterBulkAction

- type: plain · fibers seen: 7 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<span>` · classes: `p-activity_ia4_page__filter_bulk_action` `display_flex` `align_items_center` `margin_right_50`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `isArchiveSelected` | boolean | 7/7 |
| `isSmallWidth` | boolean | 7/7 |
| `isTriageModeSelected` | boolean | 7/7 |

### ActivityFilterByMenu

- type: plain · fibers seen: 7 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<div>` · classes: `c-coachmark-anchor`
- data-qa: `coachmark-anchor-wrapper`

| prop | shape | seen |
|---|---|---|
| `actionButtonsProps` | {hasUnsavedChanges: boolean, isCustomTab: boolean, onDelete: fn, onSave: fn, tabCount: number, tabId: string, viewName: string} | 7/7 |
| `activeFilters` | array | 7/7 |
| `hasTopLevelUnreads` | boolean | 7/7 |
| `hasUnsavedSelections` | boolean | 7/7 |
| `isAllTab` | boolean | 7/7 |
| `isSmallWidth` | boolean | 7/7 |
| `onFilterBySelected` | fn | 7/7 |
| `onFilterTypesChange?` | undefined | 7/7 |
| `onSourceChange` | fn | 7/7 |
| `showFilterIn` | boolean | 7/7 |

### ActivityFilterLayoutToggle

- type: plain · fibers seen: 7 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<div>` · classes: `c-coachmark-anchor`
- data-qa: `coachmark-anchor-wrapper`

| prop | shape | seen |
|---|---|---|
| `density` | string ("compact") | 7/7 |
| `entryPoint` | string | 7/7 |
| `onLayoutToggle` | fn | 7/7 |

### ActivityFilterSaveButton

- type: plain · fibers seen: 1 · states: activity-menu-Filters
- outer host: `<div>` · classes: `p-activity_ia4_page__filter_bar_menu_btn` `margin_right_50` `full_width`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `hasUnsavedChanges` | boolean | 1/1 |
| `isCustomTab` | boolean | 1/1 |
| `onSave` | fn | 1/1 |
| `tabCount` | number | 1/1 |

### ActivityFilterSearch

- type: plain · fibers seen: 7 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<div>` · classes: `p-search_button` `position_relative`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `isSearchOpen` | boolean | 7/7 |
| `onSearchQueryUpdated` | fn | 7/7 |
| `searchQuery` | string | 7/7 |
| `setIsSearchOpen` | fn | 7/7 |

### ActivityFilterSort

- type: plain · fibers seen: 7 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `entryPoint` | string | 7/7 |
| `hasVIPUsers` | boolean | 7/7 |
| `onSelectionChange` | fn | 7/7 |
| `selectedOptionType` | string | 7/7 |

### ActivityFilterUnreadsButton

- type: plain · fibers seen: 7 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<div>` · classes: `c-coachmark-anchor`
- data-qa: `coachmark-anchor-wrapper`

| prop | shape | seen |
|---|---|---|
| `activeFilters` | array | 7/7 |
| `filter` | string | 7/7 |
| `isSmallWidth` | boolean | 7/7 |
| `label?` | undefined | 7/7 |
| `onUnreadsFilter` | fn | 7/7 |

### ActivityInboxFirstClearedCoachmark

- type: plain · fibers seen: 7 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<div>` · classes: `c-coachmark-anchor`
- data-qa: `coachmark-anchor-wrapper`

| prop | shape | seen |
|---|---|---|
| `children` | node | 7/7 |

### ActivityInboxOnboardingCoachmarkAnchor

- type: plain · fibers seen: 14 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<div>` · classes: `c-coachmark-anchor`
- data-qa: `coachmark-anchor-wrapper`

| prop | shape | seen |
|---|---|---|
| `coachmarkConfig` | {arrowPosition: number, bodyText: string, isOpen: boolean, offsetX: number, offsetY: number, orientation: string, primaryCallToActionType: string, stepIndicatorProps: {currentStep: number, totalSteps: number}, title: string, type: string ("layoutToggle"|"unreadsToggle")} | 14/14 |
| `handleCoachmarkClose` | fn | 14/14 |
| `isOpen` | boolean | 14/14 |
| `onPrimaryButtonClick` | fn | 14/14 |
| `children` | node | 14/14 |

### ActivityInboxSavedViewTabs

- type: plain · fibers seen: 7 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<span>` · classes: `p-activity_ia4_page__resize_sentinel`
- data-qa: —

_no props recorded_

### ActivityInboxView

- type: plain · fibers seen: 7 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<div>` · classes: `primaryWrapper__q46PI`
- data-qa: —

_no props recorded_

### ActivityItemArchiveButton

- type: plain · fibers seen: 64 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<button>` · classes: `c-button-unstyled` `c-icon_button` `c-icon_button--size_smedium` `c-message_actions__button` `c-icon_button--default`
- data-qa: `unstyled-button`

| prop | shape | seen |
|---|---|---|
| `activityItem` | {aiPriorityRecommenderSession?: undefined, channelId: string, feedTs: string, isArchived: boolean, isPriority: boolean, isSalesforceChannel?: undefined, isUnread: boolean, key: string, messageTs: string, snoozedUntil?: undefined, threadTs?: string, ts: string, type: string ("at_user"|"thread_v2"|"message_reaction"|"bot_dm_bundle"), unreadCount?: number, reactionName?: string, reactionUser?: string, latestTs?: string, minUnreadTs?: undefined} | 64/64 |
| `experimentActivityInboxGroupOn` | boolean | 64/64 |
| `isPeekMode` | boolean | 64/64 |

### ActivityItemMarkUnreadButton

- type: plain · fibers seen: 57 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<button>` · classes: `c-button-unstyled` `c-icon_button` `c-icon_button--size_smedium` `c-message_actions__button` `c-icon_button--default`
- data-qa: `activity-mark-as-unread-button`

| prop | shape | seen |
|---|---|---|
| `activityItem` | {aiPriorityRecommenderSession?: undefined, channelId: string, feedTs: string, isArchived: boolean, isPriority: boolean, isSalesforceChannel?: undefined, isUnread: boolean, key: string, messageTs: string, snoozedUntil?: undefined, threadTs?: string, ts: string, type: string ("at_user"|"thread_v2"|"bot_dm_bundle"), unreadCount?: number, latestTs?: string, minUnreadTs?: undefined} | 57/57 |
| `channelId` | string | 57/57 |
| `experimentActivityInboxGroupOn` | boolean | 57/57 |
| `isDmTab` | boolean | 57/57 |
| `isPeekMode` | boolean | 57/57 |

### ActivityItemShortcuts

- type: plain · fibers seen: 64 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<div>` · classes: `activity_multi_line_row_content__f02c1`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `activityItemKey` | string | 64/64 |
| `rowElementId` | string | 64/64 |
| `children` | array<node \| null> | 64/64 |

### ActivityList

- type: plain · fibers seen: 7 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<div>` · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `addDateDividers` | boolean | 7/7 |
| `ariaLabel` | string | 7/7 |
| `density` | string ("compact") | 7/7 |
| `fetchActivity` | fn | 7/7 |
| `isLoading` | boolean | 7/7 |
| `isUnreadsFilterToggleSelected` | boolean | 7/7 |
| `items` | array<{aiPriorityRecommenderSession?: undefined, channelId: string, feedTs: string, isArchived: boolean, isPriority: boolean, isSalesforceChannel?: undefined, isUnread: boolean, key: string, messageTs: string, snoozedUntil?: undefined, threadTs?: string, ts: string, type: string ("at_user"|"thread_v2"), latestTs?: string, …+2}> | 7/7 |
| `openFeatTrace` | {emptyTag: string, rowTag?: undefined} | 7/7 |
| `preferredActivityFeedSortOrder` | string | 7/7 |
| `refocusOnActivityItems` | boolean | 7/7 |
| `selectedTab` | string | 7/7 |
| `shouldShowEmptyState` | boolean | 7/7 |
| `shouldTrackScroll?` | undefined | 7/7 |
| `showPageLoader` | boolean | 7/7 |

### ActivityMessage

- type: plain · fibers seen: 64 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<span>` · classes: —
- data-qa: `activity-item-message`

| prop | shape | seen |
|---|---|---|
| `destinationTag?` | undefined | 57/64 |
| `message` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: array<object>, blocks: array<object>, channel: string, crm_signal?: undefined, files?: undefined, language?: undefined, latest_reply?: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users?: array<string>, reply_users_count?: number, slackbot_feels: null, subtype?: string ("bot_message"), text: string, thread_ts?: string, ts: string, type: string ("message"), user?: string, client_msg_id?: string, bot_id?: string, bot_profile?: {app_id: string, deleted: boolean, icons: object, id: string, name: string, team_id: string, updated: number, user_id: string}, username?: string, id?: string, version?: string, icons?: {image_48: string}, source_team_id?: string, …+5} | 64/64 |
| `showSimpleFileNames?` | boolean | 7/64 |
| `subtext?` | node | 64/64 |
| `truncate` | number | 64/64 |

### ActivityPage

- type: plain · fibers seen: 7 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<div>` · classes: —
- data-qa: —

_no props recorded_

### ActivityPeekTrigger

- type: plain · fibers seen: 38 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-peek_trigger` `draggable__eJoxt`
- data-qa: `draggable_tab_item`

| prop | shape | seen |
|---|---|---|
| `children` | node | 38/38 |

### ActivityReactionTooltip

- type: plain · fibers seen: 7 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<span>` · classes: `reaction_emoji__XTXNb`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `reaction` | {baseName: string, count: number, name: string, users: array<string>} | 7/7 |
| `children` | node | 7/7 |

### ActivityRowContentActions

- type: plain · fibers seen: 64 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<button>` · classes: `c-button-unstyled` `c-icon_button` `c-icon_button--size_smedium` `c-message_actions__button` `c-icon_button--default`
- data-qa: `activity-mark-as-unread-button` `unstyled-button`

| prop | shape | seen |
|---|---|---|
| `activityItem` | {aiPriorityRecommenderSession?: undefined, channelId: string, feedTs: string, isArchived: boolean, isPriority: boolean, isSalesforceChannel?: undefined, isUnread: boolean, key: string, messageTs: string, snoozedUntil?: undefined, threadTs?: string, ts: string, type: string ("at_user"|"thread_v2"|"message_reaction"|"bot_dm_bundle"), unreadCount?: number, reactionName?: string, reactionUser?: string, latestTs?: string, minUnreadTs?: undefined} | 64/64 |
| `density` | string ("detailed") | 64/64 |
| `isReminderMessageDeleted` | boolean | 64/64 |
| `setActionsVisible` | fn | 64/64 |

### ActivityRowContentDetails

- type: plain · fibers seen: 64 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<div>` · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `activityItem` | {aiPriorityRecommenderSession?: undefined, channelId: string, feedTs: string, isArchived: boolean, isPriority: boolean, isSalesforceChannel?: undefined, isUnread: boolean, key: string, messageTs: string, snoozedUntil?: undefined, threadTs?: string, ts: string, type: string ("at_user"|"thread_v2"|"message_reaction"|"bot_dm_bundle"), unreadCount?: number, reactionName?: string, reactionUser?: string, latestTs?: string, minUnreadTs?: undefined} | 64/64 |
| `density` | string ("detailed") | 64/64 |

### ActivityRowContentMultiLine

- type: plain · fibers seen: 64 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<div>` · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `activityItem` | {aiPriorityRecommenderSession?: undefined, channelId: string, feedTs: string, isArchived: boolean, isPriority: boolean, isSalesforceChannel?: undefined, isUnread: boolean, key: string, messageTs: string, snoozedUntil?: undefined, threadTs?: string, ts: string, type: string ("at_user"|"thread_v2"|"message_reaction"|"bot_dm_bundle"), unreadCount?: number, reactionName?: string, reactionUser?: string, latestTs?: string, minUnreadTs?: undefined} | 64/64 |
| `hasFocus` | boolean | 64/64 |
| `index` | number | 64/64 |
| `isFirstEntry` | boolean | 64/64 |
| `itemId` | string | 64/64 |
| `precedesHeader` | boolean | 64/64 |
| `userMessagesTheme` | string | 64/64 |

### ActivityRowContentReactions

- type: plain · fibers seen: 7 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<div>` · classes: `c-message_kit__reaction_bar` `p-activity_ia4_page__item__reactions_bar` `activity_multi_line_row_content__reactions_bar__cK4ca` `p-activity_ia4_page__item__reactions_bar--add_button_hidden` `c-reaction_bar` `c-reaction_bar--collapsed`
- data-qa: `reaction_bar`

| prop | shape | seen |
|---|---|---|
| `activityItem` | {aiPriorityRecommenderSession?: undefined, channelId: string, feedTs: string, isArchived: boolean, isPriority: boolean, isSalesforceChannel?: undefined, isUnread: boolean, key: string, messageTs: string, reactionName: string, reactionUser: string, snoozedUntil?: undefined, threadTs?: undefined, ts: string, type: string ("message_reaction")} | 7/7 |
| `className` | string | 7/7 |

### ActivityRowContentSenderAvatar

- type: plain · fibers seen: 64 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<div>` · classes: `activity_row_content__avatar_and_icon__FvjNh` `activity_row_content__avatar_and_icon_small__G0gET`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `activityItem` | {aiPriorityRecommenderSession?: undefined, channelId: string, feedTs: string, isArchived: boolean, isPriority: boolean, isSalesforceChannel?: undefined, isUnread: boolean, key: string, messageTs: string, snoozedUntil?: undefined, threadTs?: string, ts: string, type: string ("at_user"|"thread_v2"|"message_reaction"|"bot_dm_bundle"), unreadCount?: number, reactionName?: string, reactionUser?: string, latestTs?: string, minUnreadTs?: undefined} | 64/64 |
| `density` | string ("dense") | 64/64 |
| `size` | string ("small") | 64/64 |

### ActivityRowContentSenderName

- type: plain · fibers seen: 64 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<span>` · classes: `c-truncate` `c-truncate--break_words`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `activityItem` | {aiPriorityRecommenderSession?: undefined, channelId: string, feedTs: string, isArchived: boolean, isPriority: boolean, isSalesforceChannel?: undefined, isUnread: boolean, key: string, messageTs: string, snoozedUntil?: undefined, threadTs?: string, ts: string, type: string ("at_user"|"thread_v2"|"message_reaction"|"bot_dm_bundle"), unreadCount?: number, reactionName?: string, reactionUser?: string, latestTs?: string, minUnreadTs?: undefined} | 64/64 |
| `density` | string ("dense") | 64/64 |

### ActivityRowContentStatusIndicatorWithResolvedReply

- type: plain · fibers seen: 64 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `activityItem` | {aiPriorityRecommenderSession?: undefined, channelId: string, feedTs: string, isArchived: boolean, isPriority: boolean, isSalesforceChannel?: undefined, isUnread: boolean, key: string, messageTs: string, snoozedUntil?: undefined, threadTs?: string, ts: string, type: string ("at_user"|"thread_v2"|"message_reaction"|"bot_dm_bundle"), unreadCount?: number, reactionName?: string, reactionUser?: string, latestTs?: string, minUnreadTs?: undefined} | 64/64 |
| `hasFocus` | boolean | 64/64 |
| `inline` | boolean | 64/64 |
| `openTooltipBelow` | boolean | 64/64 |
| `youRepliedMessageTs?` | undefined | 64/64 |

### ActivityRowContentTimestampAndActionsWithResolvedReply

- type: plain · fibers seen: 64 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<div>` · classes: `activity_detailed_row_content__actions_toggle__tW7YP`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `activityItem` | {aiPriorityRecommenderSession?: undefined, channelId: string, feedTs: string, isArchived: boolean, isPriority: boolean, isSalesforceChannel?: undefined, isUnread: boolean, key: string, messageTs: string, snoozedUntil?: undefined, threadTs?: string, ts: string, type: string ("at_user"|"thread_v2"|"message_reaction"|"bot_dm_bundle"), unreadCount?: number, reactionName?: string, reactionUser?: string, latestTs?: string, minUnreadTs?: undefined} | 64/64 |
| `density` | string ("detailed") | 64/64 |
| `hasFocus` | boolean | 64/64 |
| `hideStatusIndicator` | boolean | 64/64 |
| `hideTimestamp` | boolean | 64/64 |
| `youRepliedMessageTs?` | undefined | 64/64 |

### ActivityRowLoader

- type: plain · fibers seen: 6 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<div>` · classes: `p-activity_ia4_page__row_loader--hidden`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `hidden` | boolean | 6/6 |
| `offlineFeatTraceEnd` | string | 6/6 |

### ActivitySettingsMenu

- type: plain · fibers seen: 7 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<button>` · classes: `c-button-unstyled` `c-icon_button` `c-icon_button--size_medium` `viewHeaderActionButton__d6GTR` `c-icon_button--default` `activeActionButton__eyMmo`
- data-qa: `activity-more-actions-button`

_no props recorded_

### ActivityTabContent

- type: plain · fibers seen: 38 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-tab_rail__button__icon`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `badgeCounts` | {activity: number, activity-inbox: number, browse: number, dms: number, home: number, later: number, unified-files: number} | 38/38 |
| `hideMentions` | boolean | 38/38 |
| `isActive` | boolean | 38/38 |
| `isNarrowWindow` | boolean | 38/38 |
| `shouldHideInactiveLabels` | boolean | 38/38 |

### ActivityTimestamp

- type: plain · fibers seen: 64 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<span>` · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `activityTs` | string | 64/64 |

### ActivityToast

- type: memo · fibers seen: 76 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `clearQueue` | fn | 76/76 |
| `currentToast?` | undefined | 76/76 |
| `prefExcludeReactions` | boolean | 76/76 |
| `shiftToast` | fn | 76/76 |
| `stopAnimations` | boolean | 76/76 |
| `svgIconProps` | {name: string} | 76/76 |

### ActivityViewNameWithIcon

- type: plain · fibers seen: 21 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<span>` · classes: `p-activity_ia4_page__tab_label`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `className` | string | 21/21 |
| `iconName` | string | 21/21 |
| `name` | string | 21/21 |

### ActivityViewSidebar

- type: plain · fibers seen: 7 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<div>` · classes: `primaryWrapper__q46PI`
- data-qa: —

_no props recorded_

### ActivityViewSidebarHeader

- type: plain · fibers seen: 7 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<div>` · classes: `p-home_header--no_border` `p-home_header--activity_inbox` `p-home_header` `p-ia__sidebar_header--ia_details_popover` `p-ia__sidebar_header--ia4_details_popover`
- data-qa: —

_no props recorded_

### AddTabItem

- type: plain · fibers seen: 31 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<button>` · classes: `c-button-unstyled` `addTab__brBMy` `c-tabs__tab` `js-tab`
- data-qa: `unstyled-button`

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 31/31 |
| `isFocused` | boolean | 31/31 |
| `renderCoachmark` | fn | 31/31 |

### AddTabMenuTrigger

- type: plain · fibers seen: 1 · states: channel-details-Tabs
- outer host: `<button>` · classes: `c-button` `c-button--outline` `c-button--medium` `add_tab_menu_trigger__uKUXm` `manage_channel_tabs_add__Yg741`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 1/1 |
| `className` | string | 1/1 |

### AddWorkspacesMenuButton

- type: plain · fibers seen: 38 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<button>` · classes: `c-button-unstyled` `c-icon_button` `c-icon_button--size_medium` `p-team_sidebar__icon_button` `p-team_sidebar__icon_button--add_workspaces` `c-icon_button--default`
- data-qa: `add-workspace-btn`

| prop | shape | seen |
|---|---|---|
| `entryPoint` | string | 38/38 |
| `teams` | map<string, {channelSidebarBackground: string, customTheme: boolean, domain: string, iaTheming: object, icon: object, id: string, is_unified_user_client_enabled: boolean, lastActiveTab: string, lastViewState: object, name: string, teamSwitcherBackground: string, textColor: string, token: string, topNavBackground: string, …+6}> | 38/38 |

### AdminChannelModal

- type: plain · fibers seen: 1 · states: channel-details-Settings
- outer host: `<div>` · classes: `p-field_group`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `canUserCreatePublicFromPrivateChannel` | boolean | 1/1 |
| `channel` | map<string, string \| boolean> | 1/1 |
| `conversationHostId` | string | 1/1 |
| `dispatch` | fn | 1/1 |
| `id` | string | 1/1 |
| `isArchiveDisabled` | boolean | 1/1 |
| `isChannelPermissionsDisabled` | boolean | 1/1 |
| `isConvertToPublicDisabled` | boolean | 1/1 |
| `isDeleteDisabled` | boolean | 1/1 |
| `isHostedByAnotherTeamAndPrivate` | boolean | 1/1 |
| `isHuddlesPermissionsDisabled` | boolean | 1/1 |
| `isImOrMpim` | boolean | 1/1 |
| `isManageOrgAccessDisabled` | boolean | 1/1 |
| `isMemberExternalLimited` | boolean | 1/1 |
| `isMpim` | boolean | 1/1 |
| `isPostingPermissionsDisabled` | boolean | 1/1 |
| `isPrivateChannel` | boolean | 1/1 |
| `isPublicChannel` | boolean | 1/1 |
| `isRecordChannel` | boolean | 1/1 |
| `onClose` | fn | 1/1 |
| `settingsSections` | array<string> | 1/1 |
| `showArchive` | boolean | 1/1 |
| `showChangeLimitedAccessGroup` | boolean | 1/1 |
| `showChannelPermissions` | boolean | 1/1 |
| `showConvertChannelToPrivate` | boolean | 1/1 |
| `showConvertChannelToPublic` | boolean | 1/1 |
| `showConvertMpimToPrivate` | boolean | 1/1 |
| `showDataRetention` | boolean | 1/1 |
| `showDelete` | boolean | 1/1 |
| `showExport` | boolean | 1/1 |
| `showHuddlesPermissions` | boolean | 1/1 |
| `showManageOrgAccess` | boolean | 1/1 |
| `showPostingPermissions` | boolean | 1/1 |
| `showUnarchive` | boolean | 1/1 |
| `showUnlinkRecordChannel` | boolean | 1/1 |

### AgentConvertThreadButton

- type: plain · fibers seen: 15 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 15/15 |
| `threadTs` | string | 15/15 |

### AgentCopyThreadLinkButton

- type: plain · fibers seen: 15 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `backgroundStyle?` | undefined | 15/15 |
| `channelId` | string | 15/15 |
| `threadTs` | string | 15/15 |

### AgenticShowSidebarButton

- type: plain · fibers seen: 53 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

_no props recorded_

### AllThreadsRowElement

- type: plain · fibers seen: 31 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-channel_sidebar__link` `p-channel_sidebar__link--all-threads`
- data-qa: `channel_sidebar_vall_threads`

| prop | shape | seen |
|---|---|---|
| `isDisabled` | boolean | 31/31 |

### AppThreadNugget

- type: plain · fibers seen: 15 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 15/15 |
| `isInAgentControlsRow` | boolean | 15/15 |
| `threadTs` | string | 15/15 |

### AttachmentDeleteButton

- type: plain · fibers seen: 66 · states: channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `attachmentVersion` | string | 66/66 |
| `blocks?` | undefined | 66/66 |
| `deleteStyle?` | undefined | 66/66 |
| `experimentForwardMessageCardGroupOn` | boolean | 66/66 |
| `isMsgUnfurl` | boolean | 66/66 |
| `isOnlyMessageContent` | boolean | 66/66 |
| `onDelete` | fn | 66/66 |
| `showDelete` | boolean | 66/66 |

### AttachmentFooterIcon

- type: plain · fibers seen: 51 · states: channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: `c-message_attachment__footer_icon`
- data-qa: `message_attachment_footer_icon`

| prop | shape | seen |
|---|---|---|
| `altText` | string | 51/51 |
| `icon` | string | 51/51 |

### AttachmentFooterText

- type: plain · fibers seen: 66 · states: channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: `c-message_attachment__footer_text` `c-message_attachment__part`
- data-qa: `message_attachment_footer_text`

| prop | shape | seen |
|---|---|---|
| `clogLinkClick` | fn | 66/66 |
| `displayPrivateConversationTooltip` | boolean | 66/66 |
| `shouldUseTruncate?` | boolean | 66/66 |
| `text` | string | 66/66 |

### AttachmentImage

- type: plain · fibers seen: 41 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `c-aspect_box__outer` `c-message_attachment__image_container` `p-message_attachment__image_container--has-actions`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `activeChannelId` | string | 41/41 |
| `autoClogProps?` | {blockKitAppId?: undefined, blockKitBotId?: undefined, eventId: number, interaction: number, interactionElement: number, surfaceId: string, surfaceType: number, uuid: string} | 2/41 |
| `backgroundSize?` | string | 39/41 |
| `blockKitClogger?` | fn | 2/41 |
| `botId?` | undefined | 2/41 |
| `clogLinkClick?` | fn | 39/41 |
| `disableImageActions?` | boolean | 41/41 |
| `displayImageWithBorder?` | undefined | 2/41 |
| `displayImageWithoutLink?` | undefined | 2/41 |
| `expandableKey` | string | 41/41 |
| `externalSourceUrl?` | string | 39/41 |
| `height` | number | 41/41 |
| `messageTs` | string | 41/41 |
| `onLoadResult?` | fn | 39/41 |
| `openFileViewer?` | undefined | 2/41 |
| `originalHeight` | number | 41/41 |
| `originalWidth` | number | 41/41 |
| `rotation?` | number | 39/41 |
| `title` | string | 41/41 |
| `unsetImageWidthHeight?` | boolean | 39/41 |
| `unsetWidthStyle?` | undefined | 39/41 |
| `url` | string | 41/41 |
| `viewContext` | string | 41/41 |
| `width` | number | 41/41 |

### AttachmentImageActions

- type: plain · fibers seen: 2 · states: channel, channel2
- outer host: `<div>` · classes: `c-message_actions__container` `c-message_actions__group` `p-attachment_image_actions`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `downloadUrl?` | undefined | 2/2 |
| `expandableKey` | string | 2/2 |
| `fileName?` | undefined | 2/2 |
| `onExpand` | fn | 2/2 |
| `playPauseAnimationsOn` | boolean | 2/2 |
| `url` | string | 2/2 |

### AttachmentMediaTrigger

- type: memo · fibers seen: 4 · states: channel, channel2
- outer host: `<span>` · classes: `c-message_attachment__media_trigger` `c-message_attachment__media_trigger--caption`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `aria-label` | string | 4/4 |
| `expandableKey` | string | 4/4 |
| `imageBytes` | number | 4/4 |
| `imageUrl` | string | 4/4 |
| `onExpand` | fn | 4/4 |
| `senderMemberId` | string | 4/4 |
| `tooLarge` | boolean | 4/4 |
| `tooLargeWarning` | boolean | 4/4 |

### AttachmentText

- type: plain · fibers seen: 66 · states: channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: `c-message_attachment__text`
- data-qa: `message_attachment_text`

| prop | shape | seen |
|---|---|---|
| `activeChannelId` | string | 66/66 |
| `clogLinkClick` | fn | 66/66 |
| `emojiSize?` | string | 15/66 |
| `hasKeyboardFocus` | boolean | 66/66 |
| `id` | string | 66/66 |
| `messageTs` | string | 66/66 |
| `ref` | null | 66/66 |
| `shouldAllowExpand` | boolean | 66/66 |
| `shouldUseTruncate` | boolean | 66/66 |
| `suppressBreaks?` | boolean | 15/66 |
| `text` | string | 66/66 |
| `truncateLineLength` | number | 66/66 |
| `viewContext?` | string | 66/66 |

### AttachmentTitle

- type: plain · fibers seen: 66 · states: channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: `c-message_attachment__title`
- data-qa: `message_attachment_title`

| prop | shape | seen |
|---|---|---|
| `clogLinkClick?` | fn | 15/66 |
| `emojiSize?` | string | 15/66 |
| `link?` | string | 15/66 |
| `shouldLinkify?` | boolean | 15/66 |
| `suppressBreaks?` | boolean | 15/66 |
| `title` | string | 66/66 |
| `type?` | string ("default") | 15/66 |

### AudioComposerButton

- type: plain · fibers seen: 16 · states: channel, channel-bottom, channel-history, channel-scroll, channel2, message-more-menu, sidebar-context-menu
- outer host: `<button>` · classes: `c-button-unstyled` `c-icon_button` `c-icon_button--size_smedium` `p-audio_composer_button` `c-wysiwyg_container__button` `c-icon_button--default`
- data-qa: `audio_composer_button`

| prop | shape | seen |
|---|---|---|
| `draftId` | string | 16/16 |
| `enableClipsCoachmarkHighlight` | boolean | 16/16 |
| `enableScheduleSendButton` | boolean | 16/16 |
| `textyRef` | ref | 16/16 |

### BaseAboutChannelModal

- type: plain · fibers seen: 6 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs
- outer host: `<div>` · classes: `ReactModal__Overlay` `ReactModal__Overlay--after-open` `c-sk-overlay` `c-sk-overlay--no_animation`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `canEndConnectedDM` | boolean | 6/6 |
| `canHide` | boolean | 6/6 |
| `canJoinChannel` | boolean | 6/6 |
| `canToggleVip` | boolean | 6/6 |
| `channel` | map<string, string \| boolean> | 6/6 |
| `channelDisplayName` | string | 6/6 |
| `clogger` | Clogger {autoClogRecordedLogs: array, beaconThrew: boolean, enterpriseId: string, experimentExposureRecordedLogs: array, i18nLocale: string, logURLs: array, options: object, processingLogURLs: boolean, recordedLogs: array<object>, releasedHolds: Map, sendingLogURLs: Set, sessionIdFunc: fn, teamId: string, useBeacon: boolean, userId: string} | 6/6 |
| `fetchAndUpsertChannelManagers` | fn | 6/6 |
| `hidePhone` | boolean | 6/6 |
| `id` | string | 6/6 |
| `isAgentDmChannel` | boolean | 6/6 |
| `isOnEnterprise` | boolean | 6/6 |
| `isSelf` | boolean | 6/6 |
| `isSettingsVisible` | boolean | 6/6 |
| `isUserGuest` | boolean | 6/6 |
| `isUserHidden` | boolean | 6/6 |
| `isUserSingleChannelGuest` | boolean | 6/6 |
| `isUserVIP` | boolean | 6/6 |
| `isWorkflowBot` | boolean | 6/6 |
| `memberCount` | number | 6/6 |
| `memberId?` | undefined | 6/6 |
| `memberName?` | undefined | 6/6 |
| `teamId` | string | 6/6 |
| `viewType` | string | 6/6 |

### BaseChannelPrefixIcon

- type: plain · fibers seen: 62 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-channel_sidebar__channel_icon_prefix`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelIsUnread` | boolean | 62/62 |
| `className` | string | 62/62 |
| `iconType` | string | 62/62 |
| `isMuted` | boolean | 62/62 |
| `tooltipText?` | undefined | 62/62 |

### BaseChannelSectionSelectMenuTrigger

- type: memo · fibers seen: 74 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>`, `<button>` · classes: `c-button` `c-button--outline` `c-button--small` `p-about_modal__section_picker`
- data-qa: `channel-header-star-button`

| prop | shape | seen |
|---|---|---|
| `channel` | map<string, string \| boolean> | 74/74 |
| `channelId` | string | 74/74 |
| `clogger` | Clogger {autoClogRecordedLogs: array, beaconThrew: boolean, enterpriseId: string, experimentExposureRecordedLogs: array, i18nLocale: string, logURLs: array, options: object, processingLogURLs: boolean, recordedLogs: array<object>, releasedHolds: Map, sendingLogURLs: Set, sessionIdFunc: fn, teamId: string, useBeacon: boolean, userId: string} | 74/74 |
| `dispatch` | fn | 74/74 |
| `ignoreEditMode?` | boolean | 12/74 |
| `offsetX` | number | 74/74 |
| `offsetY?` | number | 12/74 |
| `position?` | string ("bottom-left") | 12/74 |
| `shouldTransitionFocusOnClose?` | boolean | 12/74 |
| `source?` | string | 62/74 |
| `children` | node | 74/74 |

### BaseCopyCodeBlockContextMenuTrigger

- type: plain · fibers seen: 8 · states: channel, channel2
- outer host: `<code>` · classes: `c-mrkdwn__code`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `blockType` | string | 8/8 |
| `children` | node | 8/8 |

### BaseMessageActionsContainer

- type: memo · fibers seen: 12 · states: emoji-picker-reaction, message-more-menu, profile-hover, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `fadeQuickReactionsCustomizationTip` | fn | 12/12 |
| `maxVisibleReactions?` | undefined | 12/12 |
| `notificationData` | object | 12/12 |
| `onEmojiSelected` | fn | 12/12 |
| `quickReactionsDisplayValue` | number | 12/12 |
| `quickReactionsNotificationName?` | undefined | 12/12 |
| `reacjiTriggers` | array | 12/12 |
| `reactionKey` | string | 12/12 |
| `reactions` | array | 12/12 |
| `setUsersQuickReactionsThunk` | fn | 12/12 |
| `shouldShowReacjiTriggers` | boolean | 12/12 |
| `usersQuickReactions` | array | 12/12 |

### BaseMessageList

- type: plain · fibers seen: 46 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `aiApps` | {hasShimmer: boolean, iconEmoji?: undefined, iconUrl?: undefined, loadingMessages?: undefined, username?: undefined} | 15/15 |
| `anchorThreadToStartTs` | boolean | 15/15 |
| `assistiveSummaryAnchorTs` | array | 15/15 |
| `comments` | array | 15/15 |
| `commentsExpanded` | boolean | 15/15 |
| `disableKeyboardShortcuts` | boolean | 15/15 |
| `findFlashReplay` | boolean | 15/15 |
| `findFlashTs?` | undefined | 15/15 |
| `hasThreadViewBottomNotification` | boolean | 15/15 |
| `highlightKey?` | undefined | 15/15 |
| `highlightTs?` | undefined | 15/15 |
| `initialLastRead` | null | 15/15 |
| `inputIsReady` | boolean | 15/15 |
| `isCaretBrowsingActive` | boolean | 15/15 |
| `isChildWindow` | boolean | 15/15 |
| `isFileExpanding` | boolean | 15/15 |
| `isMainWindow` | boolean | 15/15 |
| `lastRead?` | undefined | 15/15 |
| `latest` | string | 15/15 |
| `layout?` | undefined | 15/15 |
| `listRecordThreadFilters` | array | 15/15 |
| `listRenderer` | fn | 15/15 |
| `oldest` | string | 15/15 |
| `onContentHeightChange?` | undefined | 15/15 |
| `onScroll` | fn | 15/15 |
| `onVisibleRowsChanged` | fn | 15/15 |
| `persistentKeys` | array | 15/15 |
| `reachedEnd` | boolean | 15/15 |
| `reachedStart` | boolean | 15/15 |
| `ref` | ref | 15/15 |
| `requestHistory` | fn | 15/15 |
| `startTs` | string | 15/15 |
| `startTsOffset` | number | 15/15 |
| `timestamps` | array<string> | 15/15 |
| `windowId` | string | 15/15 |
| `windowRef` | WeakRef | 15/15 |
| `windowToken?` | undefined | 15/15 |

### BaseMessageText

- type: plain · fibers seen: 2 · states: channel, channel2
- outer host: `<span>` · classes: —
- data-qa: `aria-labelledby-primary-<id>-<n>-message_text`

_no props recorded_

### BaseModal

- type: plain · fibers seen: 6 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs
- outer host: `<div>` · classes: `ReactModal__Overlay` `ReactModal__Overlay--after-open` `c-sk-overlay` `c-sk-overlay--no_animation`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `className` | string | 6/6 |
| `closeModal` | fn | 6/6 |
| `contentLabel` | string | 6/6 |
| `dataQa` | string | 6/6 |
| `children` | node | 6/6 |

### BaseModalContainer

- type: memo · fibers seen: 76 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `ReactModal__Overlay` `ReactModal__Overlay--after-open` `c-sk-overlay` `c-sk-overlay--no_animation` `c-popover` `c-popover--z_above_fs`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `dispatch` | fn | 76/76 |
| `elements` | array<node> | 76/76 |

### BaseOnboardingTasksSidebarMenuLink

- type: plain · fibers seen: 31 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-autoclog__hook`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelsOnboardingTasksV3LargeIsEligible` | boolean | 31/31 |
| `dismissAndClearNotification` | fn | 31/31 |
| `hasSignedInMobileOtherTeam` | boolean | 31/31 |
| `isAnyModalOpen` | boolean | 31/31 |
| `joinerMobileTaskFixIsEligible` | boolean | 31/31 |
| `recordNotificationEventImpression` | fn | 31/31 |
| `setupSlackbotTaskJoinersIsEligible` | boolean | 31/31 |
| `setupSlackbotTaskJoinersThreadTs?` | undefined | 31/31 |
| `shouldSuppressAnimations` | boolean | 31/31 |
| `tasks` | array<{completed: boolean, hasSeenTaskCompletionAnimation: boolean, id: string}> | 31/31 |

### BaseVisuallyHiddenMessageText

- type: plain · fibers seen: 2 · states: channel, channel2
- outer host: `<span>` · classes: —
- data-qa: `aria-labelledby-primary-<id>-<n>-message_text`

_no props recorded_

### BookmarksSidebarLink

- type: plain · fibers seen: 31 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: `p-channel_sidebar__link__icon` `c-icon--inherit`
- data-qa: `bookmark`

| prop | shape | seen |
|---|---|---|
| `badge` | node | 31/31 |
| `icon` | node | 31/31 |
| `isSelected` | boolean | 31/31 |
| `name` | node | 31/31 |

### ChannelContextMenu

- type: plain · fibers seen: 31 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<button>` · classes: `c-button-unstyled` `p-view_header__big_button` `p-view_header__big_button--channel`
- data-qa: `channel_name_button`

| prop | shape | seen |
|---|---|---|
| `actionsToMoveToMenu` | array | 31/31 |
| `header` | boolean | 31/31 |
| `id` | string | 31/31 |
| `children` | node | 31/31 |

### ChannelHeaderStar

- type: plain · fibers seen: 31 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 31/31 |

### ChannelName

- type: plain · fibers seen: 51 · states: activity, activity-menu-Filters, activity-menu-Settings, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: `<span>` · classes: `c-channel_entity__name`
- data-qa: `inline_channel_entity__name`

| prop | shape | seen |
|---|---|---|
| `bestFormerName?` | undefined | 51/51 |
| `bold` | boolean | 51/51 |
| `channel` | map<string, string \| boolean> | 51/51 |
| `channelName` | string | 51/51 |
| `data-qa` | string | 51/51 |
| `dispatch` | fn | 51/51 |
| `displayFormerNames` | boolean | 51/51 |
| `isChannelFrozenAndInGracePeriod` | boolean | 51/51 |
| `overrideHiddenName?` | undefined | 51/51 |
| `showTeam` | boolean | 51/51 |
| `showTeamForLocalChannel` | boolean | 51/51 |
| `titleBlocks` | null | 51/51 |
| `width?` | undefined | 51/51 |

### ChannelSidebar

- type: plain · fibers seen: 31 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-channel_sidebar`
- data-qa: `channel-sidebar`

_no props recorded_

### ChannelTabBarBase

- type: plain · fibers seen: 31 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `channel_tab_bar__HY3uU`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 31/31 |

### ChannelTabFullView

- type: plain · fibers seen: 31 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 31/31 |
| `engagementEntryPoint?` | string | 1/31 |
| `engagementEntryPointLoad?` | string | 1/31 |
| `engagementEntryPointVisit?` | string | 1/31 |
| `isPreview` | boolean | 31/31 |
| `parentTab` | string | 31/31 |
| `sidebarSection?` | string | 1/31 |
| `tab` | {id: string, label: string, type: string ("channel")} | 31/31 |
| `tabId` | string | 31/31 |

### ChannelTabPanelBase

- type: plain · fibers seen: 31 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `channel_tab_panel__zJ5Bt` `c-tabs__tab_panel` `c-tabs__tab_panel--active` `c-tabs__tab_panel--full_height`
- data-qa: `tabs_content_container`

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 31/31 |

### ClientKeyboardShortcutsActivity

- type: plain · fibers seen: 7 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: — · classes: —
- data-qa: —

_no props recorded_

### ClientSidebarView

- type: plain · fibers seen: 38 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `enabled-managed-focus-container`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `isFullWidthSidebar` | boolean | 38/38 |
| `renderResizer` | fn | 38/38 |
| `windowId` | string | 38/38 |
| `windowWidth` | number | 38/38 |

### CompactToggleContextMenuTrigger

- type: plain · fibers seen: 76 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-workspace_switcher_prototype_context_target` `p-tab_rail__tab_container__context_target`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `children` | node | 76/76 |

### Composer

- type: plain · fibers seen: 46 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-composer__body` `p-composer__body--visible` `p-composer__body--unstyled`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `activeFormats` | array | 46/46 |
| `animateFormattingVisibility?` | undefined | 46/46 |
| `buttons` | array<array<string>> | 46/46 |
| `disabledFormats?` | undefined | 46/46 |
| `hasAnyPartialSlackFormatInRange` | boolean | 46/46 |
| `hasKeyboardFocus` | boolean | 46/46 |
| `isDisabled` | boolean | 46/46 |
| `isFormattingVisible?` | undefined | 46/46 |
| `isPortableContentMenu` | boolean | 46/46 |
| `isSticky` | boolean | 46/46 |
| `menuItems` | array<string> | 46/46 |
| `menuPosition` | string | 46/46 |
| `onEscape` | fn | 46/46 |
| `onFormatButtonClick` | fn | 46/46 |
| `overflowVisible` | boolean | 46/46 |
| `ref` | null | 46/46 |
| `textyHasFocus` | boolean | 46/46 |
| `width` | number | 46/46 |
| `children?` | undefined | 46/46 |

### ComposerButton

- type: plain · fibers seen: 46 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<button>` · classes: `c-button-unstyled` `c-icon_button` `c-icon_button--size_smedium` `c-wysiwyg_container__button` `c-wysiwyg_container__button--composer` `c-wysiwyg_container__button--composer_active` `c-icon_button--default`
- data-qa: `texty_composer_button`

| prop | shape | seen |
|---|---|---|
| `isDisabled` | boolean | 46/46 |
| `isFormattingEnabled` | boolean | 46/46 |
| `onClick` | fn | 46/46 |
| `textyRef` | ref | 46/46 |

### ComposerMenuTrigger

- type: plain · fibers seen: 46 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<button>` · classes: `c-button-unstyled` `c-icon_button` `c-icon_button--size_medium` `p-composer__menu-trigger` `p-composer__button` `p-composer__button--composer_ia` `p-composer__selection_button` `p-composer__button--sticky` `p-composer__menu-trigger--portable_content` `c-icon_button--default` `p-composer__button--no_pointer_events`
- data-qa: `paragraph-composer-button`

| prop | shape | seen |
|---|---|---|
| `activeFormats` | array | 46/46 |
| `buttonClassNames` | string | 46/46 |
| `getMenuItemShortcut?` | undefined | 46/46 |
| `iconSize?` | undefined | 46/46 |
| `isPortableContentMenu` | boolean | 46/46 |
| `isSticky` | boolean | 46/46 |
| `menuClassNames?` | undefined | 46/46 |
| `menuItems` | array<string> | 46/46 |
| `onItemClick` | fn | 46/46 |
| `onMenuClose` | fn | 46/46 |
| `position` | string ("top-left") | 46/46 |

### ConnectToAppsMessageActionsSubmenu

- type: plain · fibers seen: 1 · states: message-more-menu
- outer host: `<div>` · classes: `c-submenu`
- data-qa: `submenu_trigger_wrapper`

| prop | shape | seen |
|---|---|---|
| `activeMenuId` | string | 1/1 |
| `allowHighlightChange` | fn | 1/1 |
| `channelId` | string | 1/1 |
| `data-qa` | string | 1/1 |
| `highlighted` | boolean | 1/1 |
| `isKeyboardHighlight` | boolean | 1/1 |
| `isOpen` | boolean | 1/1 |
| `kbNavActive` | boolean | 1/1 |
| `label` | string | 1/1 |
| `menuCallback` | fn | 1/1 |
| `menuCallbackBefore` | fn | 1/1 |
| `menuCallbackId` | number | 1/1 |
| `messageContainerType` | string | 1/1 |
| `onMouseEnter` | fn | 1/1 |
| `onSelectAction` | fn | 1/1 |
| `preventHighlightChange` | fn | 1/1 |
| `returnKBNav` | fn | 1/1 |
| `rollup?` | undefined | 1/1 |
| `threadTs?` | undefined | 1/1 |
| `ts` | string | 1/1 |

### ConnectedHuddleSidebarFooter

- type: plain · fibers seen: 38 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `dispatchToCurrentTeam` | fn | 38/38 |
| `huddleToggleBtnRef` | ref | 38/38 |
| `primaryView` | string | 38/38 |
| `sidebarLayout` | {defaultWidth: number, isCollapsed: boolean, maxWidth: number, width: number, widthIfVisible: number} | 38/38 |

### ConnectedMessageAttachmentTwitterX

- type: plain · fibers seen: 51 · states: channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `c-message_attachment__body` `c-message_attachment__body--social_media_card` `c-message_attachment__body--fixed_layout` `c-message_attachment__body--twitter_x`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `activeChannelId` | string | 51/51 |
| `appUnfurlUrl?` | undefined | 51/51 |
| `audioHtml?` | undefined | 51/51 |
| `audioHtmlHeight?` | undefined | 51/51 |
| `authorIcon?` | undefined | 51/51 |
| `authorLink?` | undefined | 51/51 |
| `authorName?` | undefined | 51/51 |
| `authorSubname?` | undefined | 51/51 |
| `blocksContainerContext` | string | 51/51 |
| `clogLinkClick` | fn | 51/51 |
| `color?` | undefined | 51/51 |
| `documentUnfurlTsText?` | undefined | 51/51 |
| `expandableKey` | string | 51/51 |
| `from_url?` | undefined | 51/51 |
| `fromUrl` | string | 51/51 |
| `icon?` | undefined | 51/51 |
| `id` | string | 51/51 |
| `imageBytes?` | number | 51/51 |
| `imageHeight?` | number | 51/51 |
| `imageUrl?` | string | 51/51 |
| `imageWidth?` | number | 51/51 |
| `messageTs` | string | 51/51 |
| `originalUrl` | string | 51/51 |
| `senderMemberId` | string | 51/51 |
| `serviceIcon` | string | 51/51 |
| `serviceName` | string | 51/51 |
| `text` | string | 51/51 |
| `thumbHeight?` | number | 51/51 |
| `thumbUrl?` | string | 51/51 |
| `thumbWidth?` | number | 51/51 |
| `title` | string | 51/51 |
| `titleLink` | string | 51/51 |
| `ts` | number | 51/51 |
| `unsetWidthStyle?` | undefined | 51/51 |
| `videoHtml?` | undefined | 51/51 |
| `videoHtmlHeight?` | undefined | 51/51 |
| `videoHtmlWidth?` | undefined | 51/51 |
| `viewContext?` | undefined | 51/51 |

### CreateMenuTrigger

- type: plain · fibers seen: 38 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<button>` · classes: `c-button-unstyled` `p-control_strip__circle_button` `p-control_strip__create_button`
- data-qa: `unstyled-button`

_no props recorded_

### CustomAddTabItem

- type: plain · fibers seen: 7 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<div>` · classes: `addTab__brBMy` `p-activity_ia4_page__tab_container__add-tab`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `customAddTabProps` | {addTabType: string, aria-label: string, className: string, isAddTabDisabled: boolean, onMenuClickHandler: fn, onModalClickHandler: fn, tooltipText: string} | 7/7 |
| `isFocused` | boolean | 7/7 |

### DayDividerJumper

- type: plain · fibers seen: 22 · states: channel, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, message-more-menu, profile-hover, sidebar-context-menu
- outer host: `<div>` · classes: `c-message_list__day_divider__label` `c-message_list__day_divider__label--jump_to_date` `c-message_list__day_divider__label--non_sticky`
- data-qa: `day-divider-label`

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 22/22 |
| `date` | number | 22/22 |
| `getTimestampOfFirstMessageForDay` | fn | 22/22 |
| `hasYesterday` | boolean | 22/22 |
| `isBlurredMessageGroup` | boolean | 22/22 |
| `isDisabled` | boolean | 22/22 |
| `isUnread` | boolean | 22/22 |
| `jumpToTimestamp` | fn | 22/22 |
| `latestMessage` | string | 22/22 |
| `openModal` | fn | 22/22 |
| `sticky` | boolean | 22/22 |

### DividerRowElement

- type: plain · fibers seen: 31 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `divider__QhKlG`
- data-qa: —

_no props recorded_

### EmojiList

- type: memo · fibers seen: 4 · states: emoji-picker-composer, emoji-picker-reaction
- outer host: `<div>` · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `activeGroupName` | string | 4/4 |
| `clogger` | Clogger {autoClogRecordedLogs: array, beaconThrew: boolean, enterpriseId: string, experimentExposureRecordedLogs: array, i18nLocale: string, logURLs: array, options: object, processingLogURLs: boolean, recordedLogs: array<object>, releasedHolds: Map, sendingLogURLs: Set, sessionIdFunc: fn, teamId: string, useBeacon: boolean, userId: string} | 4/4 |
| `currentSelection?` | undefined | 4/4 |
| `cursorPosition` | array<number> | 4/4 |
| `groups` | array<{emoji_names: array, name: string, nameLocalized: string}> | 4/4 |
| `onAddEmojiButtonClick?` | fn | 4/4 |
| `onMouseEnter` | fn | 4/4 |
| `onMouseLeave` | fn | 4/4 |
| `onMultiSkinTonePopoverEscape` | fn | 4/4 |
| `onScroll` | fn | 4/4 |
| `onSelected` | fn | 4/4 |
| `openAddCustomEmojiModal` | fn | 4/4 |
| `pickerHasKeyboardFocusWithin` | boolean | 4/4 |
| `ref` | fn | 4/4 |
| `screenRows` | array<{ariaAttributes: object, group: object, items: array, type: string ("empty-header-row"|"group"|"emoji")}> | 4/4 |
| `scrollToIndex?` | undefined | 4/4 |
| `searchQuery` | string | 4/4 |
| `showAddEmojiButton` | boolean | 4/4 |
| `showProductivityReacji` | boolean | 4/4 |
| `throttledRecordNotificationEvent` | fn | 4/4 |

### EmojiListGroupItem

- type: plain · fibers seen: 4 · states: emoji-picker-composer, emoji-picker-reaction
- outer host: `<span>` · classes: `p-emoji_picker__heading` `p-emoji_picker__heading--emoji-14-no-bg`
- data-qa: `Frequently_Used_header` `Smileys_&_People_header`

| prop | shape | seen |
|---|---|---|
| `emojiGroup` | {emoji_names: array<string>, name: string, nameLocalized: string} | 4/4 |

### EmojiListRow

- type: plain · fibers seen: 44 · states: emoji-picker-composer, emoji-picker-reaction
- outer host: `<div>` · classes: `p-emoji_picker__row`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `activeGroupName` | string | 44/44 |
| `currentSelection?` | undefined | 44/44 |
| `cursorPosition` | array<number> | 44/44 |
| `index` | number | 44/44 |
| `locale` | string | 44/44 |
| `onEmojiClick` | fn | 44/44 |
| `onMouseEnter` | fn | 44/44 |
| `onMouseLeave` | fn | 44/44 |
| `onMultiSkinTonePopoverEscape` | fn | 44/44 |
| `role` | string ("gridcell") | 44/44 |
| `screenRows` | array<{ariaAttributes: object, group: object, items: array, type: string ("empty-header-row"|"group"|"emoji")}> | 44/44 |
| `showProductivityReacji` | boolean | 44/44 |
| `style` | {height: number, left: number, position: string ("absolute"), top: number, width: string} | 44/44 |
| `usingKeyboard` | boolean | 44/44 |

### EmojiPickerFooter

- type: plain · fibers seen: 2 · states: emoji-picker-composer, emoji-picker-reaction
- outer host: `<div>` · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `activeSkinToneId` | string | 2/2 |
| `channelId` | string | 2/2 |
| `currentEmojiSelection` | string | 2/2 |
| `hasZeroRows` | boolean | 2/2 |
| `isHuddlesEmojiPicker?` | undefined | 2/2 |
| `locale` | string | 2/2 |
| `noSearchResults` | boolean | 2/2 |
| `onAddEmojiButtonClick?` | fn | 2/2 |
| `onEmojiMouseEnter` | fn | 2/2 |
| `onEmojiMouseLeave` | fn | 2/2 |
| `onEmojiSelected` | fn | 2/2 |
| `onSkinToneChanged` | fn | 2/2 |
| `shouldShowReacjiTriggers?` | boolean | 2/2 |
| `showAddEmojiButton` | boolean | 2/2 |
| `showHandyReactions` | boolean | 2/2 |
| `showSkinTonePicker` | boolean | 2/2 |
| `teamOrEnterpriseId` | string | 2/2 |

### EmojiPickerInput

- type: plain · fibers seen: 2 · states: emoji-picker-composer, emoji-picker-reaction
- outer host: `<div>` · classes: `p-emoji_picker__input_container`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `onChange` | fn | 2/2 |
| `onClick` | fn | 2/2 |
| `placeholder` | string | 2/2 |
| `refToForward` | fn | 2/2 |

### EmojiPickerStickyHeader

- type: plain · fibers seen: 2 · states: emoji-picker-composer, emoji-picker-reaction
- outer host: `<div>` · classes: `p-emoji_picker__sticky_header`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `ariaAttributes` | {aria-label: string, aria-owns: string, aria-readonly: boolean, role: string ("grid")} | 2/2 |
| `header` | string | 2/2 |

### ExpandedThreadBreadcrumb

- type: plain · fibers seen: 15 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 15/15 |

### ForwardedShortcutsMenuTriggerButton

- type: plain · fibers seen: 46 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-shortcuts_menu_trigger_button_container--composer_ia` `p-shortcuts_menu_trigger_button_container_unstyled` `p-shortcuts_menu_trigger_button_container_unstyled--composer_ia`
- data-qa: `shortcuts_menu_trigger_container`

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 46/46 |
| `draftId` | string | 46/46 |
| `forceFileMenuOpen` | boolean | 46/46 |
| `forceOpen` | boolean | 46/46 |
| `isComposerPage` | boolean | 46/46 |
| `isDisabled` | boolean | 46/46 |
| `isEditMessageShortcutsButton` | boolean | 46/46 |
| `limitShortcutsMenuToFilesOnly` | boolean | 46/46 |
| `limitShortcutsMenuToFileUpload` | boolean | 46/46 |
| `onFileMenuClose` | fn | 46/46 |
| `onFilesSelected` | fn | 46/46 |
| `onGifSelected` | fn | 46/46 |
| `textyRef` | ref | 46/46 |
| `threadTs?` | string | 46/46 |
| `viewContext` | string | 46/46 |
| `windowId` | string | 46/46 |

### HomeHeaderMenuCoachmarkAnchor

- type: plain · fibers seen: 31 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `c-coachmark-anchor`
- data-qa: `coachmark-anchor-wrapper`

| prop | shape | seen |
|---|---|---|
| `closeCoachmark` | fn | 31/31 |
| `isOpen` | boolean | 31/31 |
| `children` | node | 31/31 |

### HomeHeaderMenuTrigger

- type: plain · fibers seen: 31 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<button>` · classes: `c-button-unstyled` `p-ia4_home_header_menu__button`
- data-qa: `workspace_actions_button`

_no props recorded_

### HuddleAudio

- type: plain · fibers seen: 38 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<audio>` · classes: `p-huddle_audio`
- data-qa: `p-huddle_audio`

| prop | shape | seen |
|---|---|---|
| `audioOutputRef` | ref | 38/38 |

### HuddleChannelHeaderButtonWrapper

- type: plain · fibers seen: 31 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-huddle_channel_header_button__container` `p-huddle_channel_header_button__container--polish`
- data-qa: `huddle_channel_header_button`

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 31/31 |
| `subtype` | string ("huddles_channel_header") | 31/31 |

### HuddleClientContainer

- type: plain · fibers seen: 38 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `children` | node | 38/38 |

### HuddleSidebarFooter

- type: memo · fibers seen: 76 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `canShowAiSummaryMenuButton` | boolean | 76/76 |
| `canvasThread?` | undefined | 76/76 |
| `channelDisplayName` | string \| null | 76/76 |
| `channelHasActiveHuddle` | boolean | 76/76 |
| `channelType` | string \| null | 76/76 |
| `clogContext` | {callsChannelId?: undefined, callsChannelType?: undefined, callsNumParticipants?: undefined, callsRoomId?: undefined} | 76/76 |
| `currentChannelId?` | string | 76/76 |
| `dismissTrialCard` | fn | 76/76 |
| `handleFileDrop` | fn | 76/76 |
| `hasPendingKnockingUsers` | boolean | 76/76 |
| `hideParticipantMenu` | fn | 76/76 |
| `hideTroubleshootingPanel` | fn | 76/76 |
| `huddleCallId?` | undefined | 76/76 |
| `huddleChannelId?` | undefined | 76/76 |
| `huddleState` | string | 76/76 |
| `huddleTeamId?` | undefined | 76/76 |
| `huddleToggleBtnRef` | ref | 76/76 |
| `isEligibleForPaidFeatureVhqTrial?` | undefined | 76/76 |
| `isHuddleLoungeMusicPlayerBanner` | boolean | 76/76 |
| `isHuddlesEnabledOrHasActiveHuddle` | boolean | 76/76 |
| `isHuddleSupportedInChannel` | boolean | 76/76 |
| `isHuddleWindowOpen` | boolean | 76/76 |
| `isInstantSlackInviteeSession` | boolean | 76/76 |
| `isLoading` | boolean | 76/76 |
| `isMiniPlayerHidden` | boolean | 76/76 |
| `isParticipantMenuVisible` | boolean | 76/76 |
| `isRequestingToJoinHuddle` | boolean | 76/76 |
| `isSidebarEditMode` | boolean | 76/76 |
| `isTeamOnProTrial` | boolean | 76/76 |
| `miniplayerWidth` | number | 76/76 |
| `primaryView` | string | 76/76 |
| `showHuddleSummaryPendingBanner` | boolean | 76/76 |
| `showTrialStartCard` | boolean | 76/76 |
| `silentSidebarIsActive` | boolean | 76/76 |
| `troubleshootingSource` | null | 76/76 |

### HuddleTrigger

- type: plain · fibers seen: 31 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-huddle_channel_header_button__container` `p-huddle_channel_header_button__container--polish`
- data-qa: `huddle_channel_header_button`

| prop | shape | seen |
|---|---|---|
| `channel` | map<string, string \| boolean> | 31/31 |
| `disabledReason?` | undefined | 31/31 |
| `children` | fn | 31/31 |

### HuddleWorkspace

- type: plain · fibers seen: 38 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `enabled-managed-focus-container`
- data-qa: —

_no props recorded_

### HuddlesLinkCTAs

- type: plain · fibers seen: 1 · states: channel-details-Settings
- outer host: `<div>` · classes: `p-channel_details__huddles_link_ctas_button_container`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channel` | map<string, string \| boolean> | 1/1 |
| `onClose` | fn | 1/1 |

### HuddlesPermissionSubtitle

- type: plain · fibers seen: 1 · states: channel-details-Settings
- outer host: `<span>` · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `dispatch` | fn | 1/1 |
| `featureHuddlesEnabled` | boolean | 1/1 |
| `id` | string | 1/1 |

### HuddlesSidebarLinkTooltipWrapper

- type: plain · fibers seen: 31 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `children` | node | 31/31 |

### IaDetailsPopoverMemberItemActionsMenu

- type: plain · fibers seen: 15 · states: channel-details-Members
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `addChannelManager` | fn | 15/15 |
| `canCurrentUserEditChannelManagers` | boolean | 15/15 |
| `canEditAgent` | boolean | 15/15 |
| `canRemoveAgent` | boolean | 15/15 |
| `canRemoveMember` | boolean | 15/15 |
| `channel` | map<string, string \| boolean> | 15/15 |
| `isMemberManager` | boolean | 15/15 |
| `isVisible` | boolean | 15/15 |
| `member` | {_display_name_lc: string, _display_name_normalized_lc: string, _email_normalized_lc: string, _first_name_lc: string, _last_name_lc: string, _name_lc: string, _real_name_lc: string, _real_name_normalized_lc: string, activity: array, color: string, deleted: boolean, enterprise_id: string, enterprise_user: {enterprise_id: string, enterprise_name: string, id: string, is_admin: boolean, is_owner: boolean, is_primary_owner: boolean, teams: array}, files: array, id: string, isExternal: boolean, isNonExistent: boolean, isUnknown: boolean, is_admin: boolean, is_app_user: boolean, is_bot: boolean, is_email_confirmed: boolean, is_invited_user: boolean, is_owner: boolean, is_primary_owner: boolean, is_restricted: boolean, is_ultra_restricted: boolean, member_color: string, mentions: array, name: string, …+10} | 15/15 |
| `onEditAgent` | fn | 15/15 |
| `onMemberRemoved` | fn | 15/15 |
| `onRemoveAgent` | fn | 15/15 |
| `openMakeChannelManagerModal` | fn | 15/15 |
| `openRemoveChannelManagerModal` | fn | 15/15 |
| `openRemoveMemberDialog` | fn | 15/15 |
| `shouldUseMembershipViewLayout` | boolean | 15/15 |

### IaDetailsPopoverMemberItemActionsMenuWrapper

- type: plain · fibers seen: 15 · states: channel-details-Members
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `canEditAgent` | boolean | 15/15 |
| `canRemoveAgent` | boolean | 15/15 |
| `canRemoveMember` | boolean | 15/15 |
| `channel` | map<string, string \| boolean> | 15/15 |
| `isVisible` | boolean | 15/15 |
| `member` | {_display_name_lc: string, _display_name_normalized_lc: string, _email_normalized_lc: string, _first_name_lc: string, _last_name_lc: string, _name_lc: string, _real_name_lc: string, _real_name_normalized_lc: string, activity: array, color: string, deleted: boolean, enterprise_id: string, enterprise_user: {enterprise_id: string, enterprise_name: string, id: string, is_admin: boolean, is_owner: boolean, is_primary_owner: boolean, teams: array}, files: array, id: string, isExternal: boolean, isNonExistent: boolean, isUnknown: boolean, is_admin: boolean, is_app_user: boolean, is_bot: boolean, is_email_confirmed: boolean, is_invited_user: boolean, is_owner: boolean, is_primary_owner: boolean, is_restricted: boolean, is_ultra_restricted: boolean, member_color: string, mentions: array, name: string, …+10} | 15/15 |
| `onEditAgent` | fn | 15/15 |
| `onMemberRemoved` | fn | 15/15 |
| `onRemoveAgent` | fn | 15/15 |
| `shouldUseMembershipViewLayout` | boolean | 15/15 |

### LazyModal

- type: plain · fibers seen: 6 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs
- outer host: `<div>` · classes: `ReactModal__Overlay` `ReactModal__Overlay--after-open` `c-sk-overlay` `c-sk-overlay--no_animation`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `errorSubtype` | string | 6/6 |
| `children` | node | 6/6 |

### LegacyDayDivider

- type: plain · fibers seen: 16 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, message-more-menu, profile-hover
- outer host: `<div>` · classes: `c-message_list__day_divider`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 16/16 |
| `date` | number | 16/16 |
| `ghost` | boolean | 16/16 |
| `isUnread` | boolean | 16/16 |
| `unreadMarkerKey` | string | 16/16 |
| `unreadTs?` | undefined | 16/16 |

### MAMenu

- type: plain · fibers seen: 1 · states: message-more-menu
- outer host: `<div>` · classes: `p-autoclog__hook`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `actions` | array<array<object>> | 1/1 |
| `agentAppId?` | undefined | 1/1 |
| `aiActionItemId?` | undefined | 1/1 |
| `aria-label` | string | 1/1 |
| `autoClogProps` | {uiComponentName: string, uiComponentVariant: string, uiPage: string} | 1/1 |
| `botId?` | undefined | 1/1 |
| `channelId` | string | 1/1 |
| `headerFor?` | undefined | 1/1 |
| `headerLabel?` | undefined | 1/1 |
| `iid?` | undefined | 1/1 |
| `index?` | undefined | 1/1 |
| `isOnSearchTab?` | undefined | 1/1 |
| `messageContainerType` | string | 1/1 |
| `onMenuItemSelected` | fn | 1/1 |
| `onMenuKeyDown` | fn | 1/1 |
| `onSelectAction` | fn | 1/1 |
| `onTriggerClose` | fn | 1/1 |
| `renderMenuItem` | fn | 1/1 |
| `rollup?` | undefined | 1/1 |
| `targetBounds` | {bottom: number, height: number, left: number, right: number, top: number, width: number} | 1/1 |
| `threadTs?` | undefined | 1/1 |
| `triggerId?` | undefined | 1/1 |
| `ts` | string | 1/1 |
| `userId` | string | 1/1 |
| `width?` | undefined | 1/1 |

### ManageChannelTabs

- type: plain · fibers seen: 1 · states: channel-details-Tabs
- outer host: `<div>` · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 1/1 |

### ManageChannelTabsListItem

- type: plain · fibers seen: 9 · states: channel-details-Tabs
- outer host: `<div>` · classes: `list_item_container__San37` `list_item_uneditable__SZMeo`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 9/9 |
| `hasFocus` | boolean | 9/9 |
| `index` | number | 9/9 |
| `isTabAtIndexDisabled` | fn | 9/9 |
| `lastIndex` | number | 9/9 |
| `lastStaticTabIndex` | number | 9/9 |
| `setHasErrored` | fn | 9/9 |
| `tab` | {id: string, label: string, type: string ("channel"|"list"|"canvas"|"bookmarks"|"files"|"workflows"), data?: {fileId: string, sharedTs: string}, isDisabled?: boolean} | 9/9 |
| `updateTabOrder` | fn | 9/9 |

### ManageChannelTabsPermissions

- type: plain · fibers seen: 1 · states: channel-details-Settings
- outer host: `<div>` · classes: `p-field_group`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 1/1 |

### MemberProfileBaseEntity

- type: plain · fibers seen: 1 · states: profile-hover
- outer host: `<div>` · classes: `p-member_profile_base_entity__primary`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `autoClogProps` | {eventId: number, onClick: {enableClogAction: boolean}, profileId: string, uiComponentName: string} | 1/1 |
| `channelId` | string | 1/1 |
| `deletedHoverCard` | boolean | 1/1 |
| `disableMemberProfileLink` | boolean | 1/1 |
| `memberId` | string | 1/1 |
| `onNavigate` | fn | 1/1 |

### MemberProfileButtons

- type: plain · fibers seen: 1 · states: profile-hover
- outer host: `<div>` · classes: `margin_top_75` `p-member_profile_buttons`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `autoFillSlashCommand` | fn | 1/1 |
| `callLogContext` | string | 1/1 |
| `canCallMember` | boolean | 1/1 |
| `canHuddle` | boolean | 1/1 |
| `canToggleVip` | boolean | 1/1 |
| `className` | string | 1/1 |
| `enqueueToast` | fn | 1/1 |
| `experimentInvitedUserGlowUpGroupOn` | boolean | 1/1 |
| `hoverCard` | boolean | 1/1 |
| `huddleEntryPoint` | string | 1/1 |
| `isListContext` | boolean | 1/1 |
| `isOffline` | boolean | 1/1 |
| `isUserHidden` | boolean | 1/1 |
| `isUserVIP` | boolean | 1/1 |
| `member` | {_display_name_lc: string, _display_name_normalized_lc: string, _email_normalized_lc: string, _first_name_lc: string, _last_name_lc: string, _name_lc: string, _real_name_lc: string, _real_name_normalized_lc: string, activity: array, color: string, deleted: boolean, enterprise_id: string, enterprise_user: {enterprise_id: string, enterprise_name: string, id: string, is_admin: boolean, is_owner: boolean, is_primary_owner: boolean, teams: array}, files: array, id: string, isExternal: boolean, isNonExistent: boolean, isUnknown: boolean, is_admin: boolean, is_app_user: boolean, is_bot: boolean, is_email_confirmed: boolean, is_invited_user: boolean, is_owner: boolean, is_primary_owner: boolean, is_restricted: boolean, is_stranger: boolean, is_ultra_restricted: boolean, member_color: string, mentions: array, …+10} | 1/1 |
| `navigateToCleanFileBrowser` | fn | 1/1 |
| `openUploadPhotoModal` | fn | 1/1 |
| `prefCompliantMemberName` | string | 1/1 |
| `refToForward?` | undefined | 1/1 |
| `showCallButton` | boolean | 1/1 |
| `showMemberProfile` | fn | 1/1 |
| `showUnhideButton` | boolean | 1/1 |
| `showUploadPhotoCta` | boolean | 1/1 |
| `startCallByMemberId` | fn | 1/1 |
| `startHuddleWithMember` | fn | 1/1 |
| `startImByMemberId` | fn | 1/1 |
| `togglePriorityUserById` | fn | 1/1 |
| `unhideUser` | fn | 1/1 |
| `useSmallButtons` | boolean | 1/1 |

### MemberProfileHoverBanner

- type: plain · fibers seen: 1 · states: profile-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `isAdmin` | boolean | 1/1 |
| `isDeactivated` | boolean | 1/1 |
| `isOwner` | boolean | 1/1 |

### MentionContextMenuTrigger

- type: plain · fibers seen: 7 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<span>` · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `onOpenInNewWindowClick` | fn | 7/7 |
| `onOpenInSplitViewClick` | fn | 7/7 |
| `url` | string | 7/7 |
| `children` | node | 7/7 |

### Menu

- type: plain · fibers seen: 2 · states: activity-menu-Settings, message-more-menu
- outer host: `<div>` · classes: `c-menu` `p-autoclog__hook`
- data-qa: `menu`

| prop | shape | seen |
|---|---|---|
| `aria-label?` | string | 1/2 |
| `autoClogProps?` | {clogImpression: boolean, elementName: number, eventId: number, uiComponentName: string, uiComponentVariant: string, uiPage: string, uiStep: string} | 1/2 |
| `experimentSimpleUnderlineLinkOn?` | boolean | 1/2 |
| `experimentSlackKitCarouselGroupOn?` | boolean | 1/2 |
| `isChildWindow` | boolean | 2/2 |
| `isMainWindow` | boolean | 2/2 |
| `isOpen?` | boolean | 1/2 |
| `logger` | {action: fn, debug: fn, devHint: fn, error: fn, info: fn, teamOrEnterpriseId: string, timeStamp: fn, warn: fn} | 2/2 |
| `menuClassNames?` | string | 1/2 |
| `onClose?` | fn | 1/2 |
| `onMenuItemSelected` | fn | 2/2 |
| `onMenuKeyDown?` | fn | 2/2 |
| `onTriggerClose` | fn | 2/2 |
| `openedWithKeyboard?` | boolean | 1/2 |
| `position?` | string ("bottom-right") | 1/2 |
| `readKeyboardMode?` | fn | 1/2 |
| `ref` | null | 2/2 |
| `renderPopover?` | fn | 1/2 |
| `shouldCloseOnOverlayMouseDown?` | boolean | 1/2 |
| `shouldSetTargetBoundsForContextMenu?` | boolean | 1/2 |
| `slackKitConfig?` | {enable24hrTime: boolean, locale: string, managedFocusContainer: {Component: object, coachmarkSectionIndex: number, workspaceSwitcherTabListSectionIndex: number}, modalHooks: {closeModal: fn}, popoverHooks: {decrementOpenCount: fn, incrementOpenCount: fn}, restoreFocusFallback: fn, scrollmarkHooks: {addScrollmarks: fn, getScrollmark: fn, removeScrollmarks: fn}, telemeter: Telemeter {endpointUrl: string, enterpriseId: string, recordedMetrics: object, sessionId: string, subAppName: string, teamId: string, teamSize?: undefined, userId: string, versionTs: string}, timeZone: string} | 1/2 |
| `stripTerminalSeparators` | boolean | 2/2 |
| `targetBounds?` | {bottom: number, height: number, left: number, right: number, top: number, width: number} | 1/2 |
| `triggerProps?` | {aria-haspopup: string} | 1/2 |
| `useTargetBoundsAsPosition?` | boolean | 1/2 |
| `width?` | undefined | 1/2 |
| `windowId` | string | 2/2 |
| `windowRef` | WeakRef | 2/2 |
| `windowToken?` | undefined | 2/2 |
| `children` | array<node \| array<array>> | 2/2 |

### MenuItem

- type: memo · fibers seen: 24 · states: activity-menu-Settings, message-more-menu
- outer host: `<div>` · classes: `c-menu_item__li`
- data-qa: `activity-show-dms-wrapper` `activity-show-channels-wrapper` `activity-show-reminders-wrapper` `activity-notification-preferences-wrapper` `mark_unread-wrapper` `later-wrapper` `remind_me-wrapper` `follow_message-wrapper`

| prop | shape | seen |
|---|---|---|
| `activeMenuId` | string | 24/24 |
| `additionalIcon?` | undefined | 8/24 |
| `allowHighlightChange?` | fn | 6/24 |
| `ariaDescribedBy?` | undefined | 8/24 |
| `ariaLabel?` | undefined | 8/24 |
| `ariaRole?` | undefined | 8/24 |
| `autoClogProps?` | {elementName?: string, eventId?: number, elementType?: number, onChange?: {enableClogAction: boolean}, uiStep?: string, channel_id?: string, message_author_id?: string, message_ts?: string, onClick?: {enableClogAction: boolean}, step?: string, uiPage?: string, user_id?: string} | 18/24 |
| `badgeCount?` | undefined | 8/24 |
| `badgeStyle?` | undefined | 8/24 |
| `checked?` | boolean | 8/24 |
| `className?` | string | 18/24 |
| `classNameListItem?` | undefined | 8/24 |
| `danger?` | boolean | 18/24 |
| `data-qa` | string | 24/24 |
| `defaultAppIcon?` | string | 10/24 |
| `description?` | undefined | 18/24 |
| `disabled?` | boolean | 24/24 |
| `dispatch?` | fn | 10/24 |
| `download?` | undefined | 8/24 |
| `forceLeft?` | boolean | 6/24 |
| `group?` | undefined | 10/24 |
| `highlighted` | boolean | 24/24 |
| `href?` | undefined | 18/24 |
| `icon?` | node \| null | 24/24 |
| `iconBorder?` | undefined | 8/24 |
| `iconClassName?` | undefined | 10/24 |
| `indeterminate?` | undefined | 8/24 |
| `isKeyboardHighlight` | boolean | 24/24 |
| `isOpen?` | boolean | 6/24 |
| `kbNavActive?` | boolean | 6/24 |
| `label` | string \| node | 24/24 |
| `menuCallback?` | fn | 18/24 |
| `menuCallbackBefore` | fn | 24/24 |
| `menuCallbackId?` | number | 18/24 |
| `onMount?` | undefined | 8/24 |
| `onMouseEnter` | fn | 24/24 |
| `onSelected?` | fn | 18/24 |
| `onUnmount?` | undefined | 8/24 |
| `preventHighlightChange?` | fn | 6/24 |
| `renderLegacySubmenu?` | boolean | 6/24 |
| `renderSubmenu?` | fn | 6/24 |
| `returnKBNav?` | fn | 6/24 |
| `shortcut?` | {key: array<string>, label: string} | 18/24 |
| `showEnter?` | undefined | 8/24 |
| `showLinkIndicatorIcon?` | undefined | 18/24 |
| `showOfflineIcon?` | boolean | 20/24 |
| `submenuStyle?` | undefined | 6/24 |
| `tag?` | undefined | 8/24 |
| `target?` | undefined | 8/24 |
| `type?` | string ("checkbox"|"compact_item") | 18/24 |
| `withSubmenu?` | boolean | 6/24 |
| `children?` | undefined | 8/24 |

### MenuItemHeaderComponent

- type: plain · fibers seen: 1 · states: activity-menu-Settings
- outer host: `<div>` · classes: `c-menu_item__header`
- data-qa: `menu-item-header`

| prop | shape | seen |
|---|---|---|
| `className?` | undefined | 1/1 |
| `label` | string | 1/1 |
| `onMouseEnter` | fn | 1/1 |

### MenuItemIconComponent

- type: plain · fibers seen: 12 · states: activity-menu-Settings, message-more-menu
- outer host: `<div>` · classes: `c-menu_item__icon` `c-menu_item__icon--border`
- data-qa: `menu_item_icon`

| prop | shape | seen |
|---|---|---|
| `icon` | null \| node | 12/12 |
| `iconBorder` | boolean | 12/12 |

### MenuSeparatorComponent

- type: plain · fibers seen: 7 · states: activity-menu-Filters, activity-menu-Settings, message-more-menu
- outer host: `<div>` · classes: `c-menu_separator__li` `c-menu_separator__li--no_first_child` `c-menu_separator__li--no_last_child`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `noFirstChild?` | boolean | 6/7 |
| `noLastChild?` | boolean | 6/7 |
| `onMouseEnter?` | fn | 6/7 |
| `type?` | string ("separator") | 2/7 |

### MessageActionButtonReactionDiscoveryDot

- type: plain · fibers seen: 6 · states: emoji-picker-reaction, message-more-menu, profile-hover, thread, thread-hover
- outer host: `<button>` · classes: `c-button-unstyled` `c-icon_button` `c-icon_button--size_smedium` `c-message_actions__button` `c-icon_button--default`
- data-qa: `add_reaction`

| prop | shape | seen |
|---|---|---|
| `messageContainerType` | string | 6/6 |
| `children` | node | 6/6 |

### MessageActionMenuItem

- type: memo · fibers seen: 10 · states: message-more-menu
- outer host: `<div>` · classes: `c-menu_item__li`
- data-qa: `mark_unread-wrapper` `later-wrapper` `follow_message-wrapper` `copy_link-wrapper` `copy_text-wrapper`

| prop | shape | seen |
|---|---|---|
| `action` | {data-qa: string, disabled: boolean, frecencyId: string, icon: node, id: string, name: string, showOfflineIcon: boolean, uiElement: number, shortcut?: {key: array, label: string}, autoClogProps?: {channel_id: string, message_author_id: string, message_ts: string, step: string, user_id: string}, className?: string, description?: undefined, isLoading?: boolean, isSaved?: boolean, multiLineDescription?: boolean} | 10/10 |
| `activeMenuId` | string | 10/10 |
| `app?` | undefined | 10/10 |
| `className` | string | 10/10 |
| `defaultAppIcon` | string | 10/10 |
| `disabled` | boolean | 10/10 |
| `dispatch` | fn | 10/10 |
| `highlighted` | boolean | 10/10 |
| `iconClassName?` | undefined | 10/10 |
| `isKeyboardHighlight` | boolean | 10/10 |
| `menuCallback` | fn | 10/10 |
| `menuCallbackBefore` | fn | 10/10 |
| `menuCallbackId` | number | 10/10 |
| `onMouseEnter` | fn | 10/10 |
| `onSelected` | fn | 10/10 |
| `shortcut?` | {key: array<string>, label: string} | 10/10 |
| `showOfflineIcon` | boolean | 10/10 |

### MessageActionsMenu

- type: plain · fibers seen: 1 · states: message-more-menu
- outer host: `<div>` · classes: `p-autoclog__hook`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `aiActionItemId?` | undefined | 1/1 |
| `autoClogProps` | {uiComponentName: string, uiComponentVariant: string, uiPage: string} | 1/1 |
| `botId?` | undefined | 1/1 |
| `channelId` | string | 1/1 |
| `iid?` | undefined | 1/1 |
| `index?` | undefined | 1/1 |
| `isOnSearchTab?` | undefined | 1/1 |
| `menuUsageLocation` | string | 1/1 |
| `messageContainerType` | string | 1/1 |
| `onMenuItemSelected` | fn | 1/1 |
| `onSelectAction?` | undefined | 1/1 |
| `onTriggerClose` | fn | 1/1 |
| `rollup?` | undefined | 1/1 |
| `showJumpActionInOverflow` | boolean | 1/1 |
| `targetBounds` | {bottom: number, height: number, left: number, right: number, top: number, width: number} | 1/1 |
| `threadTs?` | undefined | 1/1 |
| `triggerId?` | undefined | 1/1 |
| `ts` | string | 1/1 |
| `userId` | string | 1/1 |

### MessageActionsOverflowMenu

- type: plain · fibers seen: 1 · states: message-more-menu
- outer host: `<div>` · classes: `p-autoclog__hook`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `aiActionItemId?` | undefined | 1/1 |
| `botId?` | undefined | 1/1 |
| `channelId` | string | 1/1 |
| `iid?` | undefined | 1/1 |
| `index?` | undefined | 1/1 |
| `isOnSearchTab?` | undefined | 1/1 |
| `messageContainerType` | string | 1/1 |
| `onMenuItemSelected` | fn | 1/1 |
| `onSelectAction?` | undefined | 1/1 |
| `onTriggerClose` | fn | 1/1 |
| `rollup?` | undefined | 1/1 |
| `showJumpActionInOverflow` | boolean | 1/1 |
| `targetBounds` | {bottom: number, height: number, left: number, right: number, top: number, width: number} | 1/1 |
| `threadTs?` | undefined | 1/1 |
| `triggerId?` | undefined | 1/1 |
| `ts` | string | 1/1 |
| `userId` | string | 1/1 |

### MessageAttachment

- type: plain · fibers seen: 66 · states: channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>`, `<span>` · classes: `c-message_attachment_v3--hover_wrapper`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `actions?` | undefined | 66/66 |
| `activeChannelId` | string | 66/66 |
| `attachmentIndex` | number | 66/66 |
| `blocksContainerContext` | string | 66/66 |
| `botId?` | undefined | 66/66 |
| `className` | string | 66/66 |
| `clogLinkClick` | fn | 66/66 |
| `fallback` | string | 66/66 |
| `fromUrl` | string | 66/66 |
| `id` | string | 66/66 |
| `imageBytes?` | number | 39/66 |
| `imageHeight?` | number | 39/66 |
| `imageUrl?` | string | 39/66 |
| `imageWidth?` | number | 39/66 |
| `mediaType` | string | 66/66 |
| `messageTs` | string | 66/66 |
| `originalUrl` | string | 66/66 |
| `senderMemberId` | string | 66/66 |
| `service?` | undefined | 66/66 |
| `serviceIcon?` | string | 51/66 |
| `serviceName` | string | 66/66 |
| `sourceTeamId` | string | 66/66 |
| `text` | string | 66/66 |
| `threadTs?` | string | 66/66 |
| `thumbHeight?` | number | 12/66 |
| `thumbUrl?` | string | 12/66 |
| `thumbWidth?` | number | 12/66 |
| `title` | string | 66/66 |
| `titleLink` | string | 66/66 |
| `ts?` | number | 51/66 |

### MessageAttachmentDefault

- type: plain · fibers seen: 66 · states: channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>`, `<span>` · classes: `c-message_attachment_v3--hover_wrapper`
- data-qa: —

_no props recorded_

### MessageAttachmentGeneric

- type: plain · fibers seen: 15 · states: channel-bottom, channel-history, channel-scroll, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `c-message_attachment__body`
- data-qa: —

_no props recorded_

### MessageAttachmentTwitterX

- type: plain · fibers seen: 51 · states: channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `c-message_attachment__body` `c-message_attachment__body--social_media_card` `c-message_attachment__body--fixed_layout` `c-message_attachment__body--twitter_x`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `activeChannelId` | string | 51/51 |
| `appUnfurlUrl?` | undefined | 51/51 |
| `audioHtml?` | undefined | 51/51 |
| `audioHtmlHeight?` | undefined | 51/51 |
| `authorIcon?` | undefined | 51/51 |
| `authorLink?` | undefined | 51/51 |
| `authorName?` | undefined | 51/51 |
| `authorSubname?` | undefined | 51/51 |
| `blocksContainerContext` | string | 51/51 |
| `childAttachments` | array<{fallback: string, from_url: string, id: string, image_bytes: number, image_height: number, image_url: string, image_width: number, media_type: string, original_url: string, service_icon: string, service_name: string, text: string, title: string, title_link: string, …+1}> | 51/51 |
| `clogLinkClick` | fn | 51/51 |
| `color?` | undefined | 51/51 |
| `documentUnfurlTsText?` | undefined | 51/51 |
| `expandableKey` | string | 51/51 |
| `from_url?` | undefined | 51/51 |
| `fromUrl` | string | 51/51 |
| `icon?` | undefined | 51/51 |
| `id` | string | 51/51 |
| `imageBytes?` | number | 51/51 |
| `imageHeight?` | number | 51/51 |
| `imageUrl?` | string | 51/51 |
| `imageWidth?` | number | 51/51 |
| `messageTs` | string | 51/51 |
| `originalUrl` | string | 51/51 |
| `senderMemberId` | string | 51/51 |
| `serviceIcon` | string | 51/51 |
| `serviceName` | string | 51/51 |
| `text` | string | 51/51 |
| `thumbHeight?` | number | 51/51 |
| `thumbUrl?` | string | 51/51 |
| `thumbWidth?` | number | 51/51 |
| `title` | string | 51/51 |
| `titleLink` | string | 51/51 |
| `ts` | number | 51/51 |
| `unsetWidthStyle?` | undefined | 51/51 |
| `videoHtml?` | undefined | 51/51 |
| `videoHtmlHeight?` | undefined | 51/51 |
| `videoHtmlWidth?` | undefined | 51/51 |
| `viewContext?` | undefined | 51/51 |

### MessageHoverTrigger

- type: plain · fibers seen: 7 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<span>` · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 7/7 |
| `ts` | string | 7/7 |
| `children` | node | 7/7 |

### MessageInputReactionPopover

- type: plain · fibers seen: 46 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-message_input_unstyled` `p-message_input_unstyled--attachments-visible` `p-workspace__input` `p-threads_footer__input`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `addFocusTransitionListener` | fn | 46/46 |
| `cancelFocusTransitionFallback` | fn | 46/46 |
| `changeReactionFromUserAction` | fn | 46/46 |
| `channelId` | string | 46/46 |
| `focusCanTransition` | fn | 46/46 |
| `getLastMessageInChannel` | fn | 46/46 |
| `getLastMessageInThread` | fn | 46/46 |
| `isChildWindow` | boolean | 46/46 |
| `isFocusTransitioning` | fn | 46/46 |
| `isMainWindow` | boolean | 46/46 |
| `logger` | {action: fn, debug: fn, devHint: fn, error: fn, info: fn, teamOrEnterpriseId: string, timeStamp: fn, warn: fn} | 46/46 |
| `ref` | ref | 46/46 |
| `removeFocusTransitionListener` | fn | 46/46 |
| `startFocusTransitionFallbackTimeout` | fn | 46/46 |
| `threadTs?` | string | 15/46 |
| `transitionFocusTo` | fn | 46/46 |
| `windowId` | string | 46/46 |
| `windowRef` | WeakRef | 46/46 |
| `windowToken?` | undefined | 46/46 |
| `children` | node | 46/46 |

### MessageInputWithSkills

- type: plain · fibers seen: 46 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-message_input_unstyled` `p-message_input_unstyled--attachments-visible` `p-workspace__input` `p-threads_footer__input`
- data-qa: —

_no props recorded_

### MessagePaneInputPlaceholder

- type: plain · fibers seen: 31 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `c-texty_input__placeholder`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 31/31 |
| `channelName` | string | 31/31 |
| `channelType` | string | 31/31 |
| `destinationNamesString` | string | 31/31 |
| `dispatch` | fn | 31/31 |
| `hasAnyDestinationNames` | boolean | 31/31 |
| `inputRef` | map<string, {sendMessageToAnnouncerAPI?: fn}> | 31/31 |
| `isAgentforceView` | boolean | 31/31 |
| `isGovSlackWorkspace` | boolean | 31/31 |
| `isPrivacyBlurred` | boolean | 31/31 |
| `isSelfDm` | boolean | 31/31 |
| `isSlackbotAiConversation` | boolean | 31/31 |
| `memberId?` | undefined | 31/31 |
| `renderFriendlyPlaceholder` | boolean | 31/31 |
| `showStatus` | boolean | 31/31 |

### MessageSlugRichTextLabel

- type: plain · fibers seen: 7 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `labelResolverParams` | {channelId: string, isFileChannel: boolean, isFromCurrentUser: boolean, messageAuthorName: string, plainTextChannelName: string, variant: string ("dm")} | 7/7 |
| `plainTextLabel` | string | 7/7 |

### ModalContent

- type: plain · fibers seen: 5 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Settings, channel-details-Tabs
- outer host: `<div>` · classes: `c-sk-modal_content` `p-about_modal__contents` `c-sk-modal_content--indicateBottom` `p-about_modal__contents--agents`
- data-qa: `sk-modal-content`

| prop | shape | seen |
|---|---|---|
| `className` | string | 5/5 |
| `isContentFullHeight?` | boolean | 1/5 |
| `children` | node \| array<boolean \| node> | 5/5 |

### ModalHeader

- type: plain · fibers seen: 6 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs
- outer host: `<div>` · classes: `c-sk-modal_header` `p-about_modal__header`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `className` | string | 6/6 |
| `children` | array<node \| boolean> | 6/6 |

### ModalTitleBar

- type: plain · fibers seen: 6 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs
- outer host: `<div>` · classes: `c-sk-modal_title_bar` `c-sk-modal_title_bar--close_button_visible` `p-about_modal__title`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `className` | string | 6/6 |
| `title` | node | 6/6 |

### MultiThreadBackground

- type: plain · fibers seen: 30 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: `<div>` · classes: `c-message_kit__background` `c-message_kit__message` `c-message_kit__thread_message` `c-message_kit__thread_message--nested` `c-message_kit__thread_message--root` `c-message_kit__background--hovered`
- data-qa: `message_container`

| prop | shape | seen |
|---|---|---|
| `currentDragTarget?` | undefined | 30/30 |
| `first?` | boolean | 15/30 |
| `isMessagingMaxWidthEnabled?` | undefined | 30/30 |
| `isSingleThread` | boolean | 30/30 |
| `threadKey` | string | 30/30 |
| `children` | node | 30/30 |

### NestedActivityView

- type: plain · fibers seen: 7 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<div>` · classes: `viewContainerHeader__bttGf`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `children` | node | 7/7 |

### NestedActivityViewContents

- type: plain · fibers seen: 7 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<div>` · classes: `viewContainerContents__asASw` `p-view_sidebar` `p-view_sidebar--list` `p-view_sidebar--list--flex-column` `p-view_sidebar--list--activity_inbox` `p-view_sidebar--list--activity_inbox_design_v3` `nested__IAsoY`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `children` | node | 7/7 |

### NestedActivityViewHeader

- type: plain · fibers seen: 7 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<div>` · classes: `viewContainerHeader__bttGf`
- data-qa: —

_no props recorded_

### OnboardingTasksSidebarMenuLink

- type: plain · fibers seen: 31 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-autoclog__hook`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelScopedId?` | undefined | 31/31 |
| `channelsOnboardingTasksV3LargeIsEligible` | boolean | 31/31 |
| `className?` | undefined | 31/31 |
| `dismissAndClearNotification` | fn | 31/31 |
| `eligibleNotifications` | array | 31/31 |
| `hasSignedInMobileOtherTeam` | boolean | 31/31 |
| `joinerMobileTaskFixIsEligible` | boolean | 31/31 |
| `notificationName` | string | 31/31 |
| `recordNotificationEventImpression` | fn | 31/31 |
| `setupSlackbotTaskJoinersIsEligible` | boolean | 31/31 |
| `tasks` | array<{completed: boolean, hasSeenTaskCompletionAnimation: boolean, id: string}> | 31/31 |
| `children?` | undefined | 31/31 |

### OrganizeMessageActionsSubmenu

- type: plain · fibers seen: 1 · states: message-more-menu
- outer host: `<div>` · classes: `c-submenu`
- data-qa: `submenu_trigger_wrapper`

| prop | shape | seen |
|---|---|---|
| `activeMenuId` | string | 1/1 |
| `allowHighlightChange` | fn | 1/1 |
| `channelId` | string | 1/1 |
| `data-qa` | string | 1/1 |
| `highlighted` | boolean | 1/1 |
| `isKeyboardHighlight` | boolean | 1/1 |
| `isOpen` | boolean | 1/1 |
| `kbNavActive` | boolean | 1/1 |
| `label` | string | 1/1 |
| `menuCallback` | fn | 1/1 |
| `menuCallbackBefore` | fn | 1/1 |
| `menuCallbackId` | number | 1/1 |
| `messageContainerType` | string | 1/1 |
| `onMouseEnter` | fn | 1/1 |
| `onSelectAction` | fn | 1/1 |
| `preventHighlightChange` | fn | 1/1 |
| `returnKBNav` | fn | 1/1 |
| `rollup?` | undefined | 1/1 |
| `threadTs?` | undefined | 1/1 |
| `ts` | string | 1/1 |

### OrganizeModeMenu

- type: plain · fibers seen: 31 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

_no props recorded_

### PortableReactModal

- type: plain · fibers seen: 13 · states: activity-menu-Filters, activity-menu-Settings, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, thread-hover
- outer host: `<div>` · classes: `ReactModal__Overlay` `ReactModal__Overlay--after-open` `c-popover` `c-popover--z_above_fs` `c-sk-overlay` `c-sk-overlay--no_animation` `c-popover--fade` `c-popover--no-pointer` `c-popover--z_menu`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `aria` | {modal?: boolean, label?: string, describedby?: undefined, hidden?: boolean, labelledby?: undefined} | 13/13 |
| `ariaHideApp?` | boolean | 13/13 |
| `bodyOpenClassName?` | undefined | 13/13 |
| `className` | string | 13/13 |
| `closeTimeoutMS` | number | 13/13 |
| `contentElement?` | fn | 6/13 |
| `contentLabel` | string | 13/13 |
| `contentRef` | fn | 13/13 |
| `data?` | {qa: string} | 13/13 |
| `id` | string | 13/13 |
| `isOpen` | boolean | 13/13 |
| `onAfterClose` | fn | 13/13 |
| `onAfterOpen` | fn | 13/13 |
| `onRequestClose` | fn | 13/13 |
| `overlayClassName` | string | 13/13 |
| `overlayRef` | fn | 13/13 |
| `parentSelector?` | undefined | 13/13 |
| `portalClassName?` | string | 6/13 |
| `preventScroll?` | boolean | 7/13 |
| `reactRootSelector?` | undefined | 7/13 |
| `refToForward?` | undefined | 13/13 |
| `role` | string ("tooltip"|"presentation"|"dialog") | 13/13 |
| `shouldCloseOnEsc?` | boolean | 7/13 |
| `shouldCloseOnOverlayClick` | boolean | 13/13 |
| `shouldFocusAfterRender?` | boolean | 7/13 |
| `shouldReturnFocusAfterClose` | boolean | 13/13 |
| `style` | {content: {left?: number, outline?: string, position?: string ("absolute"), top?: number, transitionDuration?: string, maxWidth?: undefined}, overlay: {animationDuration?: string, padding?: string, paddingTop?: string}} | 13/13 |
| `children` | node | 13/13 |

### PortableTextyInput

- type: plain · fibers seen: 46 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `c-texty_input_unstyled__container` `c-texty_input_unstyled__container--size_medium` `c-texty_input_unstyled__container--multi_line` `c-texty_input_unstyled__container--no_actions`
- data-qa: —

_no props recorded_

### ProfileHoverCard

- type: plain · fibers seen: 1 · states: profile-hover
- outer host: `<div>` · classes: `p-autoclog__hook`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `botProfile?` | undefined | 1/1 |
| `disableMemberProfileLink?` | undefined | 1/1 |
| `fileId?` | undefined | 1/1 |
| `hideButtons?` | undefined | 1/1 |
| `isCanvasContext?` | undefined | 1/1 |
| `isListContext?` | undefined | 1/1 |
| `memberId` | string | 1/1 |
| `messageTs` | string | 1/1 |
| `serviceId?` | undefined | 1/1 |
| `shouldStopPropagation` | boolean | 1/1 |

### QuoteReplyComposerStrip

- type: plain · fibers seen: 46 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `draftId` | string | 46/46 |
| `onRemove` | fn | 46/46 |

### ReactionContent

- type: plain · fibers seen: 7 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<div>` · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `activityItem` | {aiPriorityRecommenderSession?: undefined, channelId: string, feedTs: string, isArchived: boolean, isPriority: boolean, isSalesforceChannel?: undefined, isUnread: boolean, key: string, messageTs: string, reactionName: string, reactionUser: string, snoozedUntil?: undefined, threadTs?: undefined, ts: string, type: string ("message_reaction")} | 7/7 |
| `density` | string ("detailed") | 7/7 |
| `isPeekMode` | boolean | 7/7 |
| `message` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _last_read: string, _rxn_key: string, _subscribed: boolean, attachments: array<object>, blocks: array<object>, channel: string, client_msg_id: string, crm_signal?: undefined, files?: undefined, is_locked: boolean, language?: undefined, latest_reply: string, no_display: boolean, replies?: undefined, reply_count: number, reply_user_icons: array<object>, reply_users: array<string>, reply_users_count: number, slackbot_feels: null, subtype?: undefined, text: string, thread_ts: string, ts: string, type: string ("message"), user: string} | 7/7 |

### ReactionSender

- type: plain · fibers seen: 7 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `activityItem` | {aiPriorityRecommenderSession?: undefined, channelId: string, feedTs: string, isArchived: boolean, isPriority: boolean, isSalesforceChannel?: undefined, isUnread: boolean, key: string, messageTs: string, reactionName: string, reactionUser: string, snoozedUntil?: undefined, threadTs?: undefined, ts: string, type: string ("message_reaction")} | 7/7 |
| `density` | string ("dense") | 7/7 |
| `inPrivacyMode` | boolean | 7/7 |
| `message` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _last_read: string, _rxn_key: string, _subscribed: boolean, attachments: array<object>, blocks: array<object>, channel: string, client_msg_id: string, crm_signal?: undefined, files?: undefined, is_locked: boolean, language?: undefined, latest_reply: string, no_display: boolean, replies?: undefined, reply_count: number, reply_user_icons: array<object>, reply_users: array<string>, reply_users_count: number, slackbot_feels: null, subtype?: undefined, text: string, thread_ts: string, ts: string, type: string ("message"), user: string} | 7/7 |

### ReplyBarAvatar

- type: plain · fibers seen: 34 · states: channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<span>` · classes: `c-message__reply_bar_avatar` `c-avatar`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `additionalOverlay?` | undefined | 34/34 |
| `channelId` | string | 34/34 |
| `threadTs` | string | 34/34 |
| `userId` | string | 34/34 |

### RichTextSlug

- type: plain · fibers seen: 7 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<a>` · classes: `c-link` `c-link--underline-inherit`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `dataQa` | string | 7/7 |
| `iconName` | string | 7/7 |
| `id` | string | 7/7 |
| `isDisabled` | boolean | 7/7 |
| `label` | node | 7/7 |
| `noLinking` | boolean | 7/7 |
| `onClick` | fn | 7/7 |
| `style?` | undefined | 7/7 |
| `url` | string | 7/7 |

### ShortcutsMenuTrigger

- type: plain · fibers seen: 46 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<button>` · classes: `c-button-unstyled` `c-icon_button` `c-icon_button--size_smedium` `c-wysiwyg_container__button` `c-wysiwyg_container__button--shortcuts` `p-shortcuts_menu_trigger_button--composer_ia` `c-icon_button--default`
- data-qa: `shortcuts_menu_trigger__Channel` `shortcuts_menu_trigger__Thread`

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 46/46 |
| `draftId` | string | 46/46 |
| `forceOpen` | boolean | 46/46 |
| `hiddenFileInputRef` | ref | 46/46 |
| `isComposerPage` | boolean | 46/46 |
| `isEditMessageShortcutsButton` | boolean | 46/46 |
| `limitShortcutsMenuToFilesOnly` | boolean | 46/46 |
| `limitShortcutsMenuToFileUpload` | boolean | 46/46 |
| `onClick` | fn | 46/46 |
| `onGifSelected` | fn | 46/46 |
| `onPopoverClose` | fn | 46/46 |
| `onPopoverOpen` | fn | 46/46 |
| `overlayClassName` | string | 46/46 |
| `textyRef` | ref | 46/46 |
| `threadTs?` | string | 46/46 |
| `viewContext` | string | 46/46 |
| `windowId` | string | 46/46 |
| `children` | node | 46/46 |

### SidebarHeader

- type: plain · fibers seen: 31 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-ia4_sidebar_header` `p-ia4_home_header` `p-ia4_home_header--with_upgrade` `p-ia4_home_header--with_divider` `p-ia4_sidebar_header--withControls` `p-ia4_sidebar_header--list_isnt_overflowing` `p-ia4_sidebar_header--at_scroll_top` `p-ia4_sidebar_header--text_filter_visible`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `isChannelListAtTop` | boolean | 31/31 |
| `isChannelListOverflowing` | boolean | 31/31 |

### SidebarPopout

- type: plain · fibers seen: 31 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `container__gVCBn`
- data-qa: `sidebar-popout`

| prop | shape | seen |
|---|---|---|
| `sidebarLayout` | {defaultWidth: number, isCollapsed: boolean, maxWidth: number, width: number, widthIfVisible: number} | 31/31 |

### SidebarTextFilterInput

- type: plain · fibers seen: 31 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-sidebar_text_filter_input_header` `p-home_header--no_border` `headerWrapper__pdaIJ` `p-home_header` `p-ia__sidebar_header--ia_details_popover` `p-ia__sidebar_header--ia4_details_popover`
- data-qa: —

_no props recorded_

### SidebarTextFilterResultAnnouncer

- type: plain · fibers seen: 31 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `sidebarSections` | map<string, {channelSection: object, channelSectionSidebarBehavior: string, channelsAssociatedOrdered: array, channelsOrdered: array, dormantHiddenChannelsOrdered: array, hasAllMutedHiddenChannels: boolean, isCollapsed: boolean, mutedHiddenChannelsOrdered: array, readHiddenChannels: array, readHiddenChannelsOrdered: array}> | 31/31 |

### SidebarViewContents

- type: plain · fibers seen: 38 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-view_contents` `p-view_contents--sidebar` `nested` `p-view_contents--primary-visible` `sk-client-theme--light-inverted-sidebar` `p-view_contents--secondary-visible`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `invertedSidebarClassname` | string | 38/38 |

### SilentSidebarTrigger

- type: plain · fibers seen: 38 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: —
- data-qa: —

_no props recorded_

### SilentSidebarTriggerButton

- type: plain · fibers seen: 38 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `buttonContainer__BXOiH` `idle__FcMfs`
- data-qa: —

_no props recorded_

### SimpleRichTextPreview

- type: plain · fibers seen: 57 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<a>`, `<span>` · classes: `c-link` `c-member_slug` `c-member_slug--light` `c-member_slug--link` `c-member_slug--mention`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `emojiSize?` | undefined | 57/57 |
| `hideTooltips?` | undefined | 57/57 |
| `maxNewlines` | number | 57/57 |
| `richTextBlock` | {block_id: string, elements: array<object>, type: string ("rich_text")} | 57/57 |
| `truncateLength?` | undefined | 57/57 |
| `withFormattingStyles` | boolean | 57/57 |
| `withHighlights` | boolean | 57/57 |
| `withHyperlinks` | boolean | 57/57 |
| `withQuotes` | boolean | 57/57 |
| `withSlackLinks` | boolean | 57/57 |

### SlackbotThreadFooter

- type: plain · fibers seen: 30 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: array<object>, blocks: array<object>, channel: string, crm_signal?: undefined, files?: array<string>, language?: undefined, latest_reply?: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users?: array<string>, reply_users_count?: number, slackbot_feels: null, subtype?: undefined, text: string, thread_ts: string, ts: string, type: string ("message"), user: string, bot_id?: string, client_msg_id?: string, edited?: {ts: string, user: string}, is_locked?: boolean, parent_user_id?: string, upload?: boolean, version?: string, x_files?: array<string>} | 30/30 |

### StyledChannelTabBar

- type: plain · fibers seen: 31 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `channel_tab_bar__HY3uU`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 31/31 |
| `currentTabId` | string | 31/31 |
| `defaultTabId` | string | 31/31 |
| `isTabEditingEnabled` | boolean | 31/31 |
| `onTabChange` | fn | 31/31 |
| `tabs` | array<{aria-describedby?: string, ariaHaspopup?: undefined, autoClogProps: object, buttonWrapper: fn, className?: string, contextMenuTemplate: array, data-qa: string, id: string, isDragDisabled: boolean, label: node, renderWrapper?: undefined}> | 31/31 |

### StyledChannelTabPanel

- type: plain · fibers seen: 31 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `channel_tab_panel__zJ5Bt` `c-tabs__tab_panel` `c-tabs__tab_panel--active` `c-tabs__tab_panel--full_height`
- data-qa: `tabs_content_container`

| prop | shape | seen |
|---|---|---|
| `onKeyDown` | fn | 31/31 |
| `ref` | ref | 31/31 |
| `tabContent` | node | 31/31 |

### TabsItemIcon

- type: plain · fibers seen: 2 · states: emoji-picker-composer
- outer host: `<div>` · classes: `c-tabs__tab_icon--left`
- data-qa: `tabs_item_render_icon`

| prop | shape | seen |
|---|---|---|
| `className` | string | 2/2 |
| `customIcon` | node | 2/2 |
| `icon?` | undefined | 2/2 |
| `svgIcon?` | undefined | 2/2 |

### TeamSidebar

- type: plain · fibers seen: 76 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `allWorkspaces?` | array<string> | 38/76 |
| `bootedWorkspaces?` | array<string> | 38/76 |
| `bootingTeamId?` | undefined | 38/76 |
| `className` | string | 76/76 |
| `clogger?` | Clogger {autoClogRecordedLogs: array, beaconThrew: boolean, enterpriseId: string, experimentExposureRecordedLogs: array, i18nLocale: string, logURLs: array, options: object, processingLogURLs: boolean, recordedLogs: array<object>, releasedHolds: Map, sendingLogURLs: Set, sessionIdFunc: fn, teamId: string, useBeacon: boolean, userId: string} | 38/76 |
| `entryPoint?` | string | 38/76 |
| `failedToBootWorkspaces?` | array | 38/76 |
| `isNarrowWindow` | boolean | 76/76 |
| `isOverlayOpen?` | boolean | 38/76 |
| `isWorkspaceSwitcherVisible` | boolean | 76/76 |
| `selectedTeamId` | string | 76/76 |
| `setNavigateToUnbootedWorkspace?` | fn | 38/76 |
| `setTeamOrder?` | fn | 38/76 |
| `shouldShowMegaphoneNotification?` | boolean | 38/76 |
| `signOutFromTeam?` | fn | 38/76 |
| `teams?` | map<string, {channelSidebarBackground: string, customTheme: boolean, domain: string, iaTheming: object, icon: object, id: string, is_unified_user_client_enabled: boolean, lastActiveTab: string, lastViewState: object, name: string, teamSwitcherBackground: string, textColor: string, token: string, topNavBackground: string, …+6}> | 38/76 |

### TeamSidebarItem

- type: memo · fibers seen: 76 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<button>` · classes: `c-button-unstyled` `p-team_sidebar__item` `p-team_sidebar__item--active`
- data-qa: `team_sidebar_item`

| prop | shape | seen |
|---|---|---|
| `activeItemIndex` | number | 76/76 |
| `draggableProps` | {data-rbd-draggable-context-id: string, data-rbd-draggable-id: string, onTransitionEnd: null, style: {transform: null, transition: null}} | 76/76 |
| `dragHandleProps` | {aria-describedby: string, data-rbd-drag-handle-context-id: string, data-rbd-drag-handle-draggable-id: string, draggable: boolean, onDragStart: fn, role: string ("button"), tabIndex: number} | 76/76 |
| `failedToBoot` | boolean | 76/76 |
| `hasSessionExpired` | boolean | 76/76 |
| `hasTeamBooted` | boolean | 76/76 |
| `hasUnreads` | boolean | 76/76 |
| `icon` | {image_102: string, image_132: string, image_230: string, image_34: string, image_44: string, image_68: string, image_88: string, image_default: boolean} | 76/76 |
| `isActive` | boolean | 76/76 |
| `isBooted` | boolean | 76/76 |
| `isDragging` | boolean | 76/76 |
| `isGovSlackWorkspace` | boolean | 76/76 |
| `isNarrowWindow` | boolean | 76/76 |
| `isPendingAuth?` | undefined | 76/76 |
| `isWorkspaceSwitcherVisible` | boolean | 76/76 |
| `mentionCount` | number | 76/76 |
| `onClick` | fn | 76/76 |
| `onItemActive` | fn | 76/76 |
| `openLeaveWorkspaceDialog` | fn | 76/76 |
| `order` | number | 76/76 |
| `refToForward` | fn | 76/76 |
| `shouldShowKeyboardShortcut` | boolean | 76/76 |
| `signOutFromTeam` | fn | 76/76 |
| `teamId` | string | 76/76 |
| `teamIsEnterprise` | boolean | 76/76 |
| `teamName` | string | 76/76 |
| `userCanLeaveWorkspace` | boolean | 76/76 |

### TeamSidebarTheme

- type: plain · fibers seen: 38 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<style>` · classes: `p-team_sidebar__theme`
- data-qa: —

_no props recorded_

### TextyButtonOverflow

- type: plain · fibers seen: 46 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>`, `<span>` · classes: `c-texty_buttons__overflow_container` `c-wysiwyg_container__footer_divider`
- data-qa: `texty_buttons_divider`

| prop | shape | seen |
|---|---|---|
| `containerOffsetWidth` | number | 46/46 |
| `minButtonsForOverflow?` | undefined | 46/46 |
| `ref` | ref | 46/46 |
| `textyHasFocus` | boolean | 46/46 |
| `children` | array<boolean> | 46/46 |

### TextyButtonsDivider

- type: plain · fibers seen: 32 · states: channel, channel-bottom, channel-history, channel-scroll, channel2, message-more-menu, sidebar-context-menu
- outer host: `<span>` · classes: `c-wysiwyg_container__footer_divider`
- data-qa: `texty_buttons_divider`

_no props recorded_

### TextyFormattingBar

- type: plain · fibers seen: 46 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-texty_sticky_formatting_bar`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `activeFormats` | array | 46/46 |
| `allowBlockquoteLayout` | boolean | 46/46 |
| `enableStickyComposer` | boolean | 46/46 |
| `formatsOnAnySelectedLine` | array | 46/46 |
| `hasAnyPartialSlackFormatInRange` | boolean | 46/46 |
| `isDisabled` | boolean | 46/46 |
| `isMarkdownMode` | boolean | 46/46 |
| `onEscape` | fn | 46/46 |
| `setToolbarOverflowVisible` | fn | 46/46 |
| `showLinkForm` | fn | 46/46 |
| `supportsPortableContent` | boolean | 46/46 |
| `textyHasFocus` | boolean | 46/46 |
| `textyRef` | ref | 46/46 |
| `toolbarOverflowVisible` | boolean | 46/46 |
| `toolbarPositionData?` | undefined | 46/46 |
| `useExpandedRichText` | boolean | 46/46 |
| `useSfdcTokens` | boolean | 46/46 |
| `useStickyComposer` | boolean | 46/46 |
| `useWorkflowTokens` | boolean | 46/46 |

### TextyInputWithAutocomplete

- type: plain · fibers seen: 46 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `c-texty_input_unstyled__container` `c-texty_input_unstyled__container--size_medium` `c-texty_input_unstyled__container--multi_line` `c-texty_input_unstyled__container--no_actions`
- data-qa: —

_no props recorded_

### TextyStickyFormattingBar

- type: plain · fibers seen: 46 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-texty_sticky_formatting_bar`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `activeFormats` | array | 46/46 |
| `animateFormattingVisibility?` | undefined | 46/46 |
| `buttons` | array<array<string>> | 46/46 |
| `disabledFormats?` | undefined | 46/46 |
| `hasAnyPartialSlackFormatInRange` | boolean | 46/46 |
| `isDisabled` | boolean | 46/46 |
| `isFormattingVisible?` | undefined | 46/46 |
| `isPortableContentMenu` | boolean | 46/46 |
| `menuItems` | array<string> | 46/46 |
| `menuPosition` | string | 46/46 |
| `onEscape` | fn | 46/46 |
| `onFormatButtonClick` | fn | 46/46 |
| `onMouseDown` | fn | 46/46 |
| `overflowVisible` | boolean | 46/46 |
| `textyHasFocus` | boolean | 46/46 |
| `children?` | undefined | 46/46 |

### ThreadAgentControlsRow

- type: plain · fibers seen: 15 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: `<div>` · classes: `controls__OsGFM`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `children` | array<node> | 15/15 |

### ThreadAgentResponseModeRow

- type: plain · fibers seen: 15 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 15/15 |
| `threadTs` | string | 15/15 |

### ThreadBannerContainerGeneric

- type: plain · fibers seen: 30 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: `<div>` · classes: `p-threads_view__top_banners` `p-threads_view__bottom_banners`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `direction` | string | 30/30 |
| `children` | array<node> | 30/30 |

### ThreadFloatingBanners

- type: plain · fibers seen: 30 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: `<div>` · classes: `p-threads_view__top_banners` `p-threads_view__bottom_banners`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 30/30 |
| `direction` | string | 30/30 |
| `isQuipCommentThread` | boolean | 30/30 |
| `jumpToLatestBannerOnClickHandler` | fn | 30/30 |
| `shouldShowJumpToLatestBanner` | boolean | 30/30 |
| `shouldShowUnreadBanner` | boolean | 30/30 |
| `threadTs` | string | 30/30 |
| `timestamps` | array<string> | 30/30 |
| `unreadBannerOnClickHandler` | fn | 30/30 |
| `unreadRepliesCount` | number | 30/30 |
| `viewContext?` | undefined | 30/30 |

### ThreadFooter

- type: memo · fibers seen: 30 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

_no props recorded_

### ThreadJumpToLatestBanner

- type: plain · fibers seen: 30 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `direction` | string | 30/30 |
| `onClick` | fn | 30/30 |
| `shouldShowBanner` | boolean | 30/30 |

### ThreadReply

- type: plain · fibers seen: 15 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: `<div>` · classes: `c-message_kit__background` `c-message_kit__message` `c-message_kit__thread_message` `c-message_kit__thread_message--nested`
- data-qa: `message_container`

| prop | shape | seen |
|---|---|---|
| `adjacent` | boolean | 15/15 |
| `channelId` | string | 15/15 |
| `className` | string | 15/15 |
| `clearHighlight` | fn | 15/15 |
| `clogUnreadThreadEvent` | fn | 15/15 |
| `currentUserId` | string | 15/15 |
| `dispatch` | fn | 15/15 |
| `focused` | boolean | 15/15 |
| `hasContextMenu` | boolean | 15/15 |
| `highlight` | boolean | 15/15 |
| `isInView` | boolean | 15/15 |
| `isLastReplyInThread` | boolean | 15/15 |
| `markAsUnread` | fn | 15/15 |
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: undefined, blocks: array<object>, bot_id: string, channel: string, crm_signal?: undefined, edited: {ts: string, user: string}, files: array<string>, language?: undefined, latest_reply?: undefined, no_display: boolean, parent_user_id: string, replies?: undefined, reply_count: number, reply_users?: undefined, reply_users_count?: undefined, slackbot_feels: null, subtype?: undefined, text: string, thread_ts: string, ts: string, type: string ("message"), upload: boolean, user: string, version: string, x_files: array<string>} | 15/15 |
| `onMediaPlaybackInProgressChanged` | fn | 15/15 |
| `parentId` | string | 15/15 |
| `prevTs?` | undefined | 15/15 |
| `renderActionsOffscreen` | boolean | 15/15 |
| `ts` | string | 15/15 |
| `useStandaloneMessagePresentation?` | undefined | 15/15 |
| `viewContext` | string | 15/15 |

### ThreadReplyBody

- type: plain · fibers seen: 15 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `blocksContainerContext` | string | 15/15 |
| `footer?` | undefined | 15/15 |
| `hideAgentforceMessageActionButtons` | boolean | 15/15 |
| `isCompact` | boolean | 15/15 |
| `isSystemMessageRow` | boolean | 15/15 |
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: undefined, blocks: array<object>, bot_id: string, channel: string, crm_signal?: undefined, edited: {ts: string, user: string}, files: array<string>, language?: undefined, latest_reply?: undefined, no_display: boolean, parent_user_id: string, replies?: undefined, reply_count: number, reply_users?: undefined, reply_users_count?: undefined, slackbot_feels: null, subtype?: undefined, text: string, thread_ts: string, ts: string, type: string ("message"), upload: boolean, user: string, version: string, x_files: array<string>} | 15/15 |
| `onMediaPlaybackInProgressChanged` | fn | 15/15 |
| `shouldShowReacjiTriggers` | boolean | 15/15 |

### ThreadReplyBodyGeneric

- type: plain · fibers seen: 15 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `blocksContainerContext` | string | 15/15 |
| `footer?` | undefined | 15/15 |
| `hideAgentforceMessageActionButtons` | boolean | 15/15 |
| `isCompact` | boolean | 15/15 |
| `isSystemMessageRow` | boolean | 15/15 |
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: undefined, blocks: array<object>, bot_id: string, channel: string, crm_signal?: undefined, edited: {ts: string, user: string}, files: array<string>, language?: undefined, latest_reply?: undefined, no_display: boolean, parent_user_id: string, replies?: undefined, reply_count: number, reply_users?: undefined, reply_users_count?: undefined, slackbot_feels: null, subtype?: undefined, text: string, thread_ts: string, ts: string, type: string ("message"), upload: boolean, user: string, version: string, x_files: array<string>} | 15/15 |
| `onMediaPlaybackInProgressChanged` | fn | 15/15 |
| `shouldShowReacjiTriggers` | boolean | 15/15 |

### ThreadReplyGeneric

- type: plain · fibers seen: 15 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: `<div>` · classes: `c-message_kit__background` `c-message_kit__message` `c-message_kit__thread_message` `c-message_kit__thread_message--nested`
- data-qa: `message_container`

| prop | shape | seen |
|---|---|---|
| `adjacent` | boolean | 15/15 |
| `blocksContainerContext` | string | 15/15 |
| `className` | string | 15/15 |
| `clearHighlight` | fn | 15/15 |
| `currentDragTarget?` | undefined | 15/15 |
| `focused` | boolean | 15/15 |
| `groupSenderAndBodyInIndent` | boolean | 15/15 |
| `hasContextMenu` | boolean | 15/15 |
| `hideAgentforceMessageActionButtons` | boolean | 15/15 |
| `highlight` | boolean | 15/15 |
| `isCompact` | boolean | 15/15 |
| `isInView` | boolean | 15/15 |
| `isMessagingMaxWidthEnabled?` | undefined | 15/15 |
| `isSingleThread` | boolean | 15/15 |
| `messageContainerType` | string | 15/15 |
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments?: undefined, blocks: array<object>, bot_id: string, channel: string, crm_signal?: undefined, edited: {ts: string, user: string}, files: array<string>, language?: undefined, latest_reply?: undefined, no_display: boolean, parent_user_id: string, replies?: undefined, reply_count: number, reply_users?: undefined, reply_users_count?: undefined, slackbot_feels: null, subtype?: undefined, text: string, thread_ts: string, ts: string, type: string ("message"), upload: boolean, user: string, version: string, x_files: array<string>} | 15/15 |
| `onBackgroundClick` | fn | 15/15 |
| `onMediaPlaybackInProgressChanged` | fn | 15/15 |
| `parentId` | string | 15/15 |
| `renderActionsOffscreen` | boolean | 15/15 |
| `showShareButton` | boolean | 15/15 |
| `useCurrentUserMessageBubble` | boolean | 15/15 |
| `useStandaloneMessagePresentation` | boolean | 15/15 |

### ThreadRoot

- type: plain · fibers seen: 15 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: `<div>` · classes: `c-message_kit__background` `c-message_kit__message` `c-message_kit__thread_message` `c-message_kit__thread_message--root` `c-message_kit__thread_message--nested` `c-message_kit__background--hovered`
- data-qa: `message_container`

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 15/15 |
| `className` | string | 15/15 |
| `clearHighlight` | fn | 15/15 |
| `clogUnreadThreadEvent` | fn | 15/15 |
| `currentUserId` | string | 15/15 |
| `dispatch` | fn | 15/15 |
| `focused` | boolean | 15/15 |
| `hasContextMenu` | boolean | 15/15 |
| `highlight` | boolean | 15/15 |
| `markAsUnread` | fn | 15/15 |
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments: array<object>, blocks: array<object>, channel: string, client_msg_id: string, crm_signal?: undefined, files?: undefined, is_locked: boolean, language?: undefined, latest_reply: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users: array<string>, reply_users_count: number, slackbot_feels: null, subtype?: undefined, text: string, thread_ts: string, ts: string, type: string ("message"), user: string} | 15/15 |
| `onMediaPlaybackInProgressChanged` | fn | 15/15 |
| `parentId` | string | 15/15 |
| `renderActionsOffscreen` | boolean | 15/15 |
| `ts` | string | 15/15 |
| `useStandaloneMessagePresentation?` | undefined | 15/15 |

### ThreadRootBody

- type: plain · fibers seen: 15 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `footer?` | undefined | 15/15 |
| `isCompact` | boolean | 15/15 |
| `isSystemMessageRow` | boolean | 15/15 |
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments: array<object>, blocks: array<object>, channel: string, client_msg_id: string, crm_signal?: undefined, files?: undefined, is_locked: boolean, language?: undefined, latest_reply: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users: array<string>, reply_users_count: number, slackbot_feels: null, subtype?: undefined, text: string, thread_ts: string, ts: string, type: string ("message"), user: string} | 15/15 |
| `onMediaPlaybackInProgressChanged` | fn | 15/15 |

### ThreadRootBodyGeneric

- type: plain · fibers seen: 15 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `blocksContainerContext` | string | 15/15 |
| `footer?` | undefined | 15/15 |
| `isCompact` | boolean | 15/15 |
| `isSystemMessageRow` | boolean | 15/15 |
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments: array<object>, blocks: array<object>, channel: string, client_msg_id: string, crm_signal?: undefined, files?: undefined, is_locked: boolean, language?: undefined, latest_reply: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users: array<string>, reply_users_count: number, slackbot_feels: null, subtype?: undefined, text: string, thread_ts: string, ts: string, type: string ("message"), user: string} | 15/15 |
| `onMediaPlaybackInProgressChanged` | fn | 15/15 |
| `viewContext` | string | 15/15 |

### ThreadRootGeneric

- type: plain · fibers seen: 15 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: `<div>` · classes: `c-message_kit__background` `c-message_kit__message` `c-message_kit__thread_message` `c-message_kit__thread_message--root` `c-message_kit__thread_message--nested` `c-message_kit__background--hovered`
- data-qa: `message_container`

| prop | shape | seen |
|---|---|---|
| `actionPosition` | string | 15/15 |
| `className` | string | 15/15 |
| `clearHighlight` | fn | 15/15 |
| `currentDragTarget?` | undefined | 15/15 |
| `focused` | boolean | 15/15 |
| `groupSenderAndBodyInIndent` | boolean | 15/15 |
| `hasContextMenu` | boolean | 15/15 |
| `highlight` | boolean | 15/15 |
| `includeTombstone` | boolean | 15/15 |
| `isCompact` | boolean | 15/15 |
| `isMessagingMaxWidthEnabled?` | undefined | 15/15 |
| `isSingleThread` | boolean | 15/15 |
| `messageContainerType` | string | 15/15 |
| `msg` | {__meta__: {lastUpdatedTs: string}, _hidden_reply: boolean, _rxn_key: string, attachments: array<object>, blocks: array<object>, channel: string, client_msg_id: string, crm_signal?: undefined, files?: undefined, is_locked: boolean, language?: undefined, latest_reply: string, no_display: boolean, replies?: undefined, reply_count: number, reply_users: array<string>, reply_users_count: number, slackbot_feels: null, subtype?: undefined, text: string, thread_ts: string, ts: string, type: string ("message"), user: string} | 15/15 |
| `omitGutter` | boolean | 15/15 |
| `omitSenderAndTimestamp` | boolean | 15/15 |
| `onBackgroundClick` | fn | 15/15 |
| `onMediaPlaybackInProgressChanged` | fn | 15/15 |
| `parentId` | string | 15/15 |
| `renderActionsOffscreen` | boolean | 15/15 |
| `useCurrentUserMessageBubble` | boolean | 15/15 |
| `useStandaloneMessagePresentation` | boolean | 15/15 |

### ThreadSeparatorRow

- type: plain · fibers seen: 15 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: `<div>` · classes: `p-thread_separator_row_generic`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 15/15 |
| `ghost` | boolean | 15/15 |
| `isCanvas` | boolean | 15/15 |
| `messageContainerType` | string | 15/15 |
| `separatorType` | string | 15/15 |
| `threadsUnreadDataQa` | string | 15/15 |
| `threadTs` | string | 15/15 |
| `unreadMarkerKey` | string | 15/15 |

### ThreadSeparatorRowGeneric

- type: plain · fibers seen: 15 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: `<div>` · classes: `p-thread_separator_row_generic`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `clientTheme` | string | 15/15 |
| `ghost` | boolean | 15/15 |
| `messageContainerType` | string | 15/15 |
| `replyCount` | number | 15/15 |
| `separatorType` | string | 15/15 |
| `threadsUnreadDataQa` | string | 15/15 |
| `unreadMarkerKey` | string | 15/15 |
| `willPulse?` | undefined | 15/15 |

### ThreadSubtitle

- type: plain · fibers seen: 15 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 15/15 |
| `threadTs` | string | 15/15 |

### ThreadUnreadBannerGeneric

- type: plain · fibers seen: 30 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `direction` | string | 30/30 |
| `onClick` | fn | 30/30 |
| `onClose` | fn | 30/30 |
| `replyCount` | number | 30/30 |
| `showBanner` | boolean | 30/30 |

### ThreadView

- type: plain · fibers seen: 15 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: `<div>` · classes: `p-flexpane` `p-flexpane--iap1` `p-flexpane--ia_details_popover`
- data-qa: `threads_flexpane`

_no props recorded_

### VideoComposerButton

- type: plain · fibers seen: 16 · states: channel, channel-bottom, channel-history, channel-scroll, channel2, message-more-menu, sidebar-context-menu
- outer host: `<button>` · classes: `c-button-unstyled` `c-icon_button` `c-icon_button--size_smedium` `p-video_composer_button` `c-wysiwyg_container__button` `c-wysiwyg_container__button--story_meeting` `c-icon_button--default`
- data-qa: `video_composer_button`

| prop | shape | seen |
|---|---|---|
| `draftId` | string | 16/16 |
| `enableClipsCoachmarkHighlight` | boolean | 16/16 |
| `textyRef` | ref | 16/16 |

### ViewHeader

- type: plain · fibers seen: 31 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-view_header` `p-view_header--primary-background` `p-view_header--with-channel-tabs` `p-view_header--inset-box-shadow`
- data-qa: `view_header`

| prop | shape | seen |
|---|---|---|
| `channelSpaceLayout` | string | 31/31 |
| `columnIdentifier` | string | 31/31 |
| `columnLayout` | {isCollapsed: boolean, width: number} | 31/31 |
| `dispatch` | fn | 31/31 |
| `experimentMpdmNamingGroupOn` | boolean | 31/31 |
| `experimentUnifiedGlobalTriageGroupOn` | boolean | 31/31 |
| `fileId?` | undefined | 31/31 |
| `id` | string | 31/31 |
| `isInstantSlackInviteeSession` | boolean | 31/31 |
| `otherColumnLayouts` | {secondary: {defaultWidth: number, isCollapsed: boolean, maxWidth: number, width: number}, sidebar: {defaultWidth: number, isCollapsed: boolean, maxWidth: number, width: number, widthIfVisible: number}, split: {defaultWidth: number, isCollapsed: boolean, maxWidth: number, potentialWidth: number, width: number}} | 31/31 |
| `showQuipActivityHistoryHeader` | boolean | 31/31 |
| `showSlackbotPreviewBadge` | boolean | 31/31 |
| `viewType` | string | 31/31 |
| `children` | node | 31/31 |

### ViewHeaderBigButton

- type: plain · fibers seen: 62 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<button>`, `<div>` · classes: `c-button-unstyled` `p-view_header__big_button` `p-view_header__big_button--channel` `p-view_header__actions` `p-view_header__actions--gap-6px`
- data-qa: `channel_name_button`

| prop | shape | seen |
|---|---|---|
| `channel?` | map<string, string \| boolean> | 31/62 |
| `connectionAppId?` | undefined | 31/62 |
| `connectionAppUserId?` | undefined | 31/62 |
| `dispatch?` | fn | 31/62 |
| `id` | string | 62/62 |
| `isChannelThreadSummariesEnabled?` | boolean | 31/62 |
| `isColumnNarrow?` | boolean | 31/62 |
| `isUserExternalLimited?` | boolean | 31/62 |
| `showDecorateDefaultChannelsCoachmark?` | boolean | 31/62 |
| `showFiveAvatarsInstack?` | boolean | 31/62 |
| `showPresenceInAvatarStack?` | boolean | 31/62 |
| `showsContextTeam?` | boolean | 31/62 |
| `showSolutionsOnboardingHuddlesCoachmark?` | boolean | 31/62 |
| `titleAccessory?` | undefined | 31/62 |

### ViewHeaderChannelSectionSelector

- type: plain · fibers seen: 6 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs
- outer host: `<button>` · classes: `c-button` `c-button--outline` `c-button--small` `p-about_modal__section_picker`
- data-qa: `channel-header-star-button`

| prop | shape | seen |
|---|---|---|
| `allChannelSectionsById` | map<string, {emoji: string, id: string, name: string, nextChannelSectionId: string, type: string ("stars")}> | 6/6 |
| `channelId` | string | 6/6 |
| `channelName` | string | 6/6 |
| `channelSectionIds` | array | 6/6 |
| `clogger` | Clogger {autoClogRecordedLogs: array, beaconThrew: boolean, enterpriseId: string, experimentExposureRecordedLogs: array, i18nLocale: string, logURLs: array, options: object, processingLogURLs: boolean, recordedLogs: array<object>, releasedHolds: Map, sendingLogURLs: Set, sessionIdFunc: fn, teamId: string, useBeacon: boolean, userId: string} | 6/6 |
| `currentUserId` | string | 6/6 |
| `dispatch` | fn | 6/6 |
| `isAppChannel` | boolean | 6/6 |
| `isChannelImOrMpim` | boolean | 6/6 |
| `isChannelInExternalWorkspace` | boolean | 6/6 |
| `isChannelStarred` | boolean | 6/6 |
| `isRecordChannel` | boolean | 6/6 |
| `isTemporaryChannel` | boolean | 6/6 |
| `renderButton` | fn | 6/6 |
| `shouldStarDirectly` | boolean | 6/6 |
| `showAddToSection` | boolean | 6/6 |
| `showMoveSection` | boolean | 6/6 |
| `starredChannelSection` | {emoji: string, id: string, name: string, nextChannelSectionId: string, type: string ("stars")} | 6/6 |
| `triggerProps` | {offsetX: number, offsetY: number} | 6/6 |

### ViewHeaderMore

- type: plain · fibers seen: 31 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<button>` · classes: `c-button-unstyled` `c-icon_button` `c-icon_button--size_small` `interactionButton__hAY0B` `c-icon_button--default`
- data-qa: `unstyled-button`

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 31/31 |

### ViewHeaderSearch

- type: plain · fibers seen: 31 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<button>` · classes: `c-button-unstyled` `c-icon_button` `c-icon_button--size_medium` `p-view_header_search_action_button` `p-view_header_search_action_button--no-border` `c-icon_button--default`
- data-qa: `search_in_channel_button`

| prop | shape | seen |
|---|---|---|
| `channel` | map<string, string \| boolean> | 31/31 |

### ViewHeaderStar

- type: plain · fibers seen: 31 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `channelId` | string | 31/31 |

### ViewHeaderStarButton

- type: plain · fibers seen: 31 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `ariaLabel` | string | 31/31 |
| `channelSectionEmoji?` | undefined | 31/31 |
| `channelSectionIcon` | {name: string, size: string} | 31/31 |
| `channelSectionIconAutoClogProps` | {elementName: string, eventId: number, onClick: {enableClogAction: boolean}, view_type: string} | 31/31 |
| `isStarred` | boolean | 31/31 |
| `toolTipText` | string | 31/31 |

### ViewHeaderTopic

- type: plain · fibers seen: 31 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-view_header__topic_container`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `id` | string | 31/31 |

### ViewSidebarHeader

- type: plain · fibers seen: 38 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-home_header--no_border` `p-home_header` `p-ia__sidebar_header--ia_details_popover` `p-ia__sidebar_header--ia4_details_popover` `p-sidebar_text_filter_input_header` `headerWrapper__pdaIJ` `p-home_header--activity_inbox`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `headerClassName` | string | 38/38 |
| `role` | string ("none") | 38/38 |
| `useToolbar` | boolean | 38/38 |
| `children` | node | 38/38 |

### ViewSidebarHeaderButtons

- type: plain · fibers seen: 7 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<div>` · classes: `header__BaLem` `headerSmallGap__lWJoq`
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `children` | array<boolean \| node> | 7/7 |

### ViewSidebarHeaderTitle

- type: plain · fibers seen: 7 · states: activity, activity-menu-Filters, activity-menu-Settings
- outer host: `<div>` · classes: `p-dms_page_header__title`
- data-qa: `activity-inbox-sidebar-header-title`

| prop | shape | seen |
|---|---|---|
| `dataQa` | string | 7/7 |
| `children` | node | 7/7 |

### VisibleModalContentChecker

- type: plain · fibers seen: 7 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, emoji-picker-reaction
- outer host: — · classes: —
- data-qa: —

_no props recorded_

### WithCallMenu

- type: plain · fibers seen: 7 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, profile-hover
- outer host: `<button>` · classes: `c-button` `c-button--outline` `c-button--small` `p-about_modal__action_button--disabled` `p-member_profile_buttons__button` `p-member_profile_buttons__button--huddle`
- data-qa: `call` `member_profile_huddle_btn`

| prop | shape | seen |
|---|---|---|
| `channelId?` | string | 6/7 |
| `childFunctionComponent` | fn | 7/7 |
| `cloggerContext?` | {entryPoint: string} | 1/7 |
| `includeHuddle` | boolean | 7/7 |
| `includeHuddleDiscoveryLink` | boolean | 7/7 |
| `member` | {files: array, id: string, isNonExistent: boolean, is_app_user: boolean, is_bot: boolean, name: string, profile: {avatar_hash: string, display_name: string, display_name_normalized: string, image_1024: string, image_192: string, image_24: string, image_32: string, image_48: string, image_512: string, image_72: string, real_name: string, real_name_normalized: string, always_active?: boolean, fields?: object, …+19}, updated: number, _display_name_lc?: string, _display_name_normalized_lc?: string, _email_normalized_lc?: string, _first_name_lc?: string, _last_name_lc?: string, _name_lc?: string, _real_name_lc?: string, _real_name_normalized_lc?: string, activity?: array, color?: string, deleted?: boolean, enterprise_id?: string, enterprise_user?: {enterprise_id: string, enterprise_name: string, id: string, is_admin: boolean, is_owner: boolean, is_primary_owner: boolean, teams: array}, isExternal?: boolean, isUnknown?: boolean, is_admin?: boolean, is_email_confirmed?: boolean, is_invited_user?: boolean, is_owner?: boolean, is_primary_owner?: boolean, is_restricted?: boolean, is_stranger?: boolean, …+10} | 7/7 |
| `onMenuItemSelected` | fn | 7/7 |
| `onRenderMenuCallback?` | fn | 1/7 |
| `useBookmarksStyle?` | boolean | 6/7 |
| `useIaDetailsStyle?` | boolean | 6/7 |
| `useIAP2InspectorStyle?` | boolean | 6/7 |

### WithMediaThreadPlayback

- type: plain · fibers seen: 15 · states: channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, profile-hover, thread, thread-hover
- outer host: `<div>` · classes: `p-flexpane` `p-flexpane--iap1` `p-flexpane--ia_details_popover`
- data-qa: `threads_flexpane`

| prop | shape | seen |
|---|---|---|
| `threadKey` | string | 15/15 |
| `children` | node | 15/15 |

### XWSHuddleInvite

- type: plain · fibers seen: 38 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

_no props recorded_

### XWSHuddleLocker

- type: plain · fibers seen: 38 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

_no props recorded_

### XWSHuddleSidebarFooter

- type: plain · fibers seen: 38 · states: activity, activity-menu-Filters, activity-menu-Settings, channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: — · classes: —
- data-qa: —

| prop | shape | seen |
|---|---|---|
| `primaryView` | string | 38/38 |
| `sidebarLayout` | {defaultWidth: number, isCollapsed: boolean, maxWidth: number, width: number, widthIfVisible: number} | 38/38 |

### XWSHuddleTrigger

- type: plain · fibers seen: 31 · states: channel, channel-bottom, channel-details, channel-details-Agents & apps, channel-details-Automations, channel-details-Members, channel-details-Settings, channel-details-Tabs, channel-history, channel-scroll, channel2, composer-focused, composer-focused-thread, emoji-picker-composer, emoji-picker-reaction, message-more-menu, profile-hover, sidebar-context-menu, thread, thread-hover
- outer host: `<div>` · classes: `p-huddle_channel_header_button__container` `p-huddle_channel_header_button__container--polish`
- data-qa: `huddle_channel_header_button`

| prop | shape | seen |
|---|---|---|
| `channel` | map<string, string \| boolean> | 31/31 |
| `forceProvider` | boolean | 31/31 |
| `children` | fn | 31/31 |

## 5. Static-only extras

### MessageDivider (static only, not rendered live)

- export: fn
- props destructured in source: `className`, `dataQa`

### MessageAttachmentSlackMessageCard (static only, not rendered live)

- export: fn
- props destructured in source: `avatar`, `authorName`, `timestamp`, `context`, `children`, `media`, `footer`, `className`, `onClick`, `isBare`

### UnreadDivider (static only, not rendered live)

- export: fn
- props destructured in source: `ghost`, `className`, `unreadMarkerKey`, `threadsUnreadDataQa`, `messageContainerType`

### DayDivider (static only, not rendered live)

- export: Symbol(react.memo)
- props destructured in source: `isUnread`, `ghost`, `unreadMarkerKey`, `fullWidth`

### DayDividerLabel (static only, not rendered live)

- export: Symbol(react.memo)
- props destructured in source: `date`, `sticky`

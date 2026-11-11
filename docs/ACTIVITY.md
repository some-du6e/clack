# Activity: Slack's model and Clack's port (2026-11-09, branch `activity-parity`)

Source: Slack's web client, build `a84662d2` (`_x_id` prefix), public CDN
chunks fetched without a cookie (`gantry-v2-async-client-activity-*` and the
webapp build's `gantry-v2-shared-boot-async`, which carries the Activity
components; `application.js` and `client-boot` for the fetchers and RTM
handlers). Module ids are the webapp build's unless marked "gantry". Nothing
was opened in a live Slack client for this pass; the layout measurements in
docs/FIDELITY.md (round 2) and docs/VIEWS-AUDIT.md still stand.

## Page

- **Route.** The rail's Activity tab is `ActivityInbox` when `activity_inbox`
  is on (gantry `5Ns/ Lh`); `client-activity-inbox-view` mounts it, and
  without the experiment falls back to the old `ActivityList` sidebar.
- **Header, tabs, filter bar, empty states, settings menu, "+" menu, bulk
  select.** Unchanged in this pass; see docs/VIEWS-AUDIT.md (ActivityPage,
  ActivityPageTabs, ActivityFilterBar, ActivityFilterByMenu,
  ActivityFilterBulkAction, getActivityEmptyStateData).
- **Row layout.** `ActivityRowContentMultiLine` (Detailed) and
  `ActivityRowContentSingleLine` (Dense) from the gantry build: see "Which
  build" below (an earlier pass of this doc described the webapp build's
  grouped `ActivityRowContentDetailed`, which Slack's CSS no longer
  styles).

## Feed

- **activity.feed** (fetcher params, unchanged): `limit 20`, `types`,
  `mode chrono_v1`, `archive_only`, `priority_only`, `is_activity_inbox`,
  `unread_only`, `only_salesforce_channels`, `exclude_automations`,
  `automations_only`, `cursor`. The "all" types are hgTi `j`: mentions
  (+ `unjoined_channel_mention` behind an experiment), threads, reactions,
  apps, dms (+ `prejoin_dm_welcome_party_alert`), invitations
  (+ `team_joiner_invite`, `quietly_added_to_channel` behind experiments),
  channel, reminders (when `global_reminders_in_activity`), and
  `list_record_edited`.
- **Writes.** `activity.markRead`, `activity.markUnread {entries}`,
  `activity.archive` / `activity.unarchive` (Clear / Restore),
  `activity.markAllRead`, and **`activity.clearBefore {before_ts, filters}`**
  (`activityClearBeforeFetcher`; `createApiMethodFetcher` snake-cases the
  args; the filters are uby+ `o`: `entry_types`, `channel_ids`,
  `unread_only`, `archive_only`, …). New in core: `ClearActivityBefore`.
- **Realtime** (gantry boot-render `handleActivityRTMEvent`): `activity`
  with subtype `activity_updated` (patch the entry), `activity_deleted`
  (drop it), `activity_invalidated` (refetch); also
  `activity_clear_all_completed` (an undo toast) and
  `activity_views_updated` (saved views). Clack now drops deleted entries;
  updated and invalidated were already handled.

## Each row (activity_slack.go)

**Destination** (hliv `gA`): list entries → List; `generic_system_alert` →
System; `saved_reminder` → by `item_type` (reminder → Later, file/canvas →
File, list_record → List, message → the channel's type); an app or bot DM →
App; channel invites (`internal_channel_invite`, `quietly_added_to_channel`,
external invites) → Invitation; `team_joiner_invite`,
`prejoin_dm_welcome_party_alert` → Workspace invitation; else channel /
DM / MPDM from the conversation.

**Which build.** The previous pass read the webapp build's components
(`gantry-v2-shared-boot-async`, SLUr/aJri). The rows Slack ships come from
the gantry build's `client-boot-render` (module U5s4:
`ActivityRowContentMultiLine`, `ActivityRowContentSingleLine`; Gjwd, VeYL,
QTZf, laKt), which is what `client-boot-styles.cf726c4f2eaecc74.min.css`
styles: that CSS has the multi-line and single-line rows' classes
(`activity_multi_line_row_content__entry_footerWithTypeIcon`, `__status`,
`activity_single_line_row_content__*`) and none of the grouped grid the
webapp build's `ActivityRowContentDetailed` uses. Both rows render when
`activity_entry_layout_revisions_2` (5Ns/ `xi`) is on, which also switches
the footer strings below. The live measurements (FIDELITY round 2: name at
+14, avatar at +18, footer 8px under the body, 97px for a one-line body;
the 5px/2px status column) are the multi-line row's CSS exactly.

**Detailed row** (`ActivityRowContentMultiLine`): container
`--grouped --detailed --multi-line`, padding 12px 20px 12px 16px in the
bordered-separators list, 1px `otl-ter` bottom border; read rows
`--read-fill` (`base-sec`), unread `base-pry`, selected `base-hgl-3`,
hovered `base-pry` under `base-pry-hover`. Grid gap 8px: the avatar
(padding-top 6px, opacity .8 until unread), the stack (padding-top 2px):
the name row (22px, gap 8px: the sender, `content-sec` 400 / unread
`content-pry` 900; the timestamp, 13px, 54px wide, `content-ter`), the
body (15px/22px, `content-sec` / unread `content-pry`, 2 lines), the
footer (padding-top 8px, 13px/18px `content-ter`); then the status column
(padding 5px 0 2px).

**Footer** (VeYL `getFooterContent`, revisions on): the 16px type icon
when the item's icon kind is a standalone one (Gjwd `PJ`: thread,
at_mention, invite, list, canvas, file, reminder, later), 4px, then the
text, shown for the destinations in 0S4w `bi` (channel, mpdm, dm, app,
list, invite, workspace_invite, file). The destination is bold
(`activity_row_content__footer_name`, 700):

| item | footer |
|---|---|
| mentions (keyword too) | Mention in **#channel** (+ " thread" in a thread) |
| thread_v2 | Thread in **#channel** |
| message_reaction | **#channel** |
| channel post | **#channel** |
| dm / mpdm / app | **Direct message** / **the group's names** / **App** |
| invitations / joiner, prejoin | Invitation / Workspace invitation |
| saved_reminder | Reminder from **#channel** (message, file, canvas, list item), else Reminder |
| list items with an item | Mention in **▤ {list}** |

Icon kinds (Gjwd `getActivityIconType`): mentions and code review →
at_mention (`mentions`), bot DMs → app, dm/mpdm → dm, threads → thread,
invitations and joiners → invite (`paper-plane`), reactions → reaction,
reminders → their item (`file`, `lists`, `canvas`, else later:
`bookmark`), channel posts → channel, alerts and prejoin → notification,
anything else at_mention.

**Dense row** (`ActivityRowContentSingleLine`): container `--dense
--design-v3 --grouped`, 50px (padding 10px, the sides 20px), the same
backgrounds and border. One line, gap 8px: the leading block (200px;
100px in a container 300px or narrower) with the user (the 20px avatar,
opacity .8 until unread; the sender, 15px, `content-sec` 400 / unread
`content-pry` 700) and the status (the unread badge, then the 20px type
icon in `content-ter`, the reaction's emoji for reactions, for any item
whose destination is known); then the centre (gap 16px, 4px at 625px or
narrower): the message (15px, one line), the location (13px, 100px, 224px
from 800px; the destination's label, bold when unread) and the timestamp
(13px, 54px, right-aligned, hidden while the row is hovered). The hover
actions sit centred on the row's right edge (-4px).

**Unread badge** (`ActivityRowContentStatusIndicatorWithResolvedReply`):
`c-badge--themed --circle` with the standalone override: 20px tall, min
20px, padding 0 8px, 10px 700, `theme-base-imp` / `theme-content-imp`.

**Sender** (laKt `ActivityRowContentSenderName`; both rows pass density
"dense"): an external invite's
"{user} from {team}" (the team name after the user in Detailed; "Slack
Connect Invitation" before the invite loads); "Slack" for system alerts;
"Reminder" for reminders; the app for app messages; reactions
(`ReactionSender`): the reactor, "{name} +{n}" when other people reacted
too; threads (`ThreadSender`): the reply's author, "+{n}" for the root's
other participants (not you, not Slackbot); "Slackbot"; else the person.

**Body** (w4rX `ActivityRowContentDetails`): the message (2 lines Detailed,
1 Dense) with "You: " when you wrote it; reactions (QTZf `ReactionContent`): "Reacted {emoji} to:" (Detailed) or
"Reacted to:" (Dense) and your message on one line; reminders: "{author}: " and the message,
the reminder's text, or "You set a reminder about this file"; internal
invites: "invited you to #channel"; external: "invited you to #channel" /
"invited you to direct message" / "You received an invitation that has been
ignored, expired, or is no longer available for another reason."; joiner:
"Accepted your invitation to join Slack — take a second to say hello." or,
for the reminder, "Joined your workspace. A welcome from you will help them
get started."; prejoin: "Was invited to join {team} — take a second to say
hello."; lists (`ListContent`, for entries with an item; approvals have none):
"@mention • {column} • {item}", "Edited {item}: {column}", "Assigned to
you: {item}", "List update: {item}", where {item} is Hi+d `ListItemName`:
the list-item icon (not in "List update:") and the item's title, "Untitled
item" until the list loads; an unknown message is Slack's loading shimmer.
The to-do and approval entries have no sender (hliv `EM` falls back to the
message author, which they don't have): Slack's shimmer.

**Status** (Gh4i `getStatusIndicator`, hliv `g_`): unread rows show a count
badge; the count is the bundle's for dm, mpdm, bot DMs, channel posts and
threads (sGb0 `oY`), else 1.

**Hover actions** (Gh4i `ActivityRowContentActions`): Mark as read
(`mark-as-read`) while unread; Mark as unread (`mark-as-unread`) once read,
never for reactions; Clear (`eraser`), or in Cleared items Restore
notification (`eraser-filled`); More actions (`ellipsis-vertical-filled`).
The bar is as wide as its buttons.

**Item menu** (HGmO `getActivityItemMenuTemplate`, the same template for
More actions and the row's context menu): Mark as read / Mark as unread
(U), Add to-do / Remove to-do (A; Save for later / Remove from Later without
`later_todos`); Remind me about this ▸ In 20 minutes, In 1 hour, In 3 hours,
Tomorrow, Next week; Get notified about new replies / Turn off notifications
for replies (threads); Mute channel / Mute conversation (at_user,
at_user_group, channel invites); Copy link (the message's; see "Row
actions" for the rows added since); Clear all
before… (`ClearAllBeforeDialog`: "Clear all before this?", "This will clear
all notifications older than this one from your activity feed." or "…
matching your current filters …", Cancel / Clear); Open in home.

**Click** (DKeS `handleOnClickActivityItem`): marks read
(`activity.markRead`), selects the row and opens the conversation, a thread
reply in its thread; lists open the item, invites the invite. Unchanged.

**Loading.** First page: shimmer rows (`ActivityItemLoading`). Next pages:
`ActivityRowLoader`, a spinner (Clack drew two skeleton rows: now a
spinner).

## Layout setting (Dense / Detailed)

- **Where Slack keeps it.** Not in localStorage or users.prefs: the
  Activity store is created with `persistenceEnabled: !1`. Slack's
  `fetchActivityViews` reads `activity.views`, which answers the saved
  views (each with `density`) and global `prefs {sort, density}`;
  `formatFilterViewState` lays `prefs.density` over every view when
  `activity_global_settings` (5Ns/ `XW`) is on, and maps the API value
  with `getDensityFromApiDensity` ("dense", else Detailed/"compact").
- **Where it writes.** With `XW` on the toggle sits in the page header
  (`ActivityFilterLayoutToggle` with entry point HEADER, as on the
  audited account) and calls `updateAllActivityViewsByApi`:
  `activity.prefs.update {density: "dense"|"compact"}`. With `XW` off the
  toggle is in the filter bar and `updateActivityViewsByApi` saves a
  custom view's density through `activity.views.update`, but for the All
  view only changes the page's state, so it doesn't survive a reload.
- **Clack.** core `ActivityDensity` / `SetActivityDensity` do the same;
  the view loads it on first open and saves on every toggle (a failed
  write switches back). The toggle's icons are Slack's `table` (Detailed)
  and `menu` (Dense).

## Colours

The row, page and text colours above are Slack's tokens, light and dark,
as Clack already carries them (internal/ui/slackc/css/slackvars.go, read
from the web client): `base-pry` #fff / #1a1d21, `base-sec` #f8f8f8 /
#1a1d21, `base-hgl-3` #fffae0 / #2f1e00, `base-pry-hover` #4544470f /
#f8f8f814, `otl-ter` #5e5d6021 / #797c814d, `content-pry` #1d1c1d /
#f8f8f8, `content-sec` #454447 / #b9babd, `content-ter` #5e5d60 /
#9a9b9e. Fixed this round:

- The pressed Unreads filter (`p-activity_ia4_page__filter_bar--active`)
  is `light-dark()`: light `base-hgl-1` #e3f8ff with an `otl-hgl-1-sec`
  border and `content-hgl-1` #1264a3 text; dark `base-hgl-2` #05241b with
  a `plt-jade-10` #083f2e border and `plt-jade-80` #8dd2b5 text (green,
  not blue); hovered, `base-hgl-1` with a `content-hgl-1` border and text.
- The `theme-*` tokens follow the account's Slack theme (ia_theme), as in
  Slack: the selected tab's underline (`theme-base-pry`), the unread dots
  and count badges (`theme-base-imp` / `theme-content-imp`). Clack's views
  had Aubergine's fixed; they now take the theme's when it isn't the
  default.

## Tests and fixtures

- `internal/core/testdata/activity_feed.json`: one entry per type (38),
  now with reactions by several people, a busy thread, a group DM, a
  joiner reminder, a revoked Slack Connect invite, a file reminder and a
  message of your own.
- `internal/fakeslack/activity.go` (Size.Views): `activity.feed` with one
  entry per rendered type about ViewsChannel's messages, filtered by
  `types`, `unread_only`, `archive_only`, with read and cleared state;
  `messages.list` for them; every write recorded.
- `TestActivityFakeslackFeed`, `TestActivityFakeslackFiltersAndReads`,
  `TestActivityClearBefore` (core); `TestActivityEveryItemTypeRenders`
  (each row's sender, footer, icons, body, subtext), `TestActivityHoverButtonsLikeSlack`,
  `TestActivityItemMenuLikeSlack`, `TestActivityClicksMarkRead`,
  `TestActivityRowContextMenu`, `TestActivityRealtimeDelete`,
  `TestActivityParityShots` (views).
- Screenshots: the activity-shots workflow's artifact (Detailed and Dense,
  light and dark; hover bar, item menu, Clear all before, light and dark),
  and `side-{detailed,dense}-{light,dark}.png`: Clack's rows next to the
  same rows in Slack's markup under Slack's shipping CSS and tokens
  (.github/scripts/activity_ref.py; avatars are grey squares and messages
  plain text on the Slack side).
- `TestActivityDensityRoundTrip` (core, fakeslack's `activity.views` /
  `activity.prefs.update`), `TestActivityDensityPersists`,
  `TestActivityDenseRowLikeSlack` (views).

## Row actions (2026-11-10, branch `activity-rows`)

Read from the same gantry build (`client-boot-render.777e711847af270a`,
`client-boot-shared.4f8632cd9602d1fa`, `client-boot.bac7c893b49156a7`,
`modern.vendor.99445d7c5cf41877`; public CDN, no cookie). Nothing was
opened in a live Slack client and no write reached the real account: every
call below is tested against internal/fakeslack (`ActivityWrites`).

**Read rows' marks** (U5s4 `ActivityRowContentStatusIndicatorWithResolvedReply`,
inline, as both shipping rows use it). After the unread badge, in order:
the completed to-do (a saved reminder whose saved item isn't incomplete,
rZx3 E8: `check-filled`, "You completed this to-do" + "On {date_slash}"),
Draft (a draft in the conversation or thread, for thread_v2 / dm / channel
/ bot_dm_bundle: `edit-filled`, 'Draft: "{60 chars}"'), You replied (rZx3
`getYouRepliedMessageTs`: a thread whose latest_ts isn't the feed_ts; a
mention whose thread or channel's latest message is yours and newer; a DM,
group DM, app DM or channel post whose latest message is yours and newer
than the entry; svza `RepliedBadge` compact, `check-filled`), You reacted
(rZx3 jW: your reactions on the feed_ts message, svza `ReactedBadge`
compact, `check-filled`, "You reacted: :a: :b: :c: +n more"); none
otherwise. All are `status_icon --circle --compact`: a 20px `plt-gray-10`
circle, the 12px glyph in `content-pry`, in the 24px status slot, the
tooltip below. Typing and agent status aren't in Clack's Activity.

**Badges.** The text badges (Draft `lagoon`, Replied `grass`, Incomplete
`honeycomb` / Completed `lagoon`) are Pck2
`ActivityRowContentTimestampAndActionsWithResolvedReply`'s, drawn only
with `activity_inbox` off (`!A &&`) and only by the old
`ActivityRowContentDense` / `ActivityRowContentDetailed`; the shipping
multi-line and single-line rows pass `hideTimestamp` and
`hideStatusIndicator`, so they never show. Clack draws the inbox rows, so
they're left out. There is no "Needs reply" string anywhere in the build,
and no VIP badge on a row (VIP is a filter view, `filterVipSelected`).

**Reminders.** Mark Complete (xSv9 `ActivityItemMarkCompleteButton`,
shown while E8 says incomplete): in Detailed a small button (28px,
`margin-top: 12px`, the `check` icon then "Mark Complete") under
ReminderMessage / ReminderText's body (not under the file reminder's "You
set a reminder about this file"); in Dense the hover bar's first button
(`markCompleteIconButton`: `base-inv-hgl-2` under `content-inv-pry`, 12px
and a rule after). It calls CCch `onToggleComplete` →
`saved.update {item_type, item_id, [ts], mark: completed, todo_state:
completed, date_due: 0}` (mheF toggle*Complete). Reschedule (Pck2
`ActivityItemRescheduleButton`, `clock`, after Mark Complete; hidden when
the saved item doesn't exist or is completed, disabled while it loads)
opens SP97 `RemindMenu` / ccoC `ReminderTimeMenu` (240px): In 30 mins, In
1 hour, In 3 hours (under "Snooze…" as "30 mins"… when overdue), Tomorrow
at {reminder_notification_time}, Monday at {…} (2a23 calculateDueDateTs:
9:00, the DND end when later, the pref), Custom… (the Reminder dialog),
then Clear reminder with a due date; each `saved.update {date_due}` (mheF
updateSavedMessage / File / Reminder). The saved items come from
`saved.get {items: [{item_id, item_type, ts, item_detail}]}` (YeRq
`getMaybeUnknownSavedItemById` → 2ar2, modern.vendor `savedGetFetcher`),
keyed by Gjwd qy. Not in Clack: the "{date} at {time}" recent custom time
(`lastSavedCustomDateReminderTs`, Later's store).

**Slack Connect invites** (Qt0R `ActivityRowContentInviteActions`, Detailed
only, in `entry_extras`; only external invites, Gjwd bL: an internal
channel invite has no buttons). 3Oc+ `SlackConnectInviteActions` lists
v+N2 FI's text actions for a received invite: Ignore (small outline) then
Accept (small primary), `margin-left: 4px`, 4px apart. Ignore opens rPCk
`IgnoreForkedModal` ("What would you like to do?": "Ignore this
invitation" and "Block invitations from this person", the icon font's
times-circle-medium / disable / chevron-medium-right, extracted into
iconfont.go); the first opens `IgnoreThisModal` (Cancel / Ignore
Invitation, danger) → `conversations.ignoreInvite {invite}` (channel) or
`sharedInvites.ignoreInvite` (DM); the second hCGE `IgnoreAllModal`
(Cancel / Block Invitations) → `conversations.ignoreUser` /
`sharedInvites.ignoreUser`, then "You won’t hear from {name} again".
Accept on a DM invite (`SlackConnectAcceptDmInviteAction`):
`sharedInvites.canAccept {sig}`; eligible and from a verified organisation
(Cfiv), `sharedInvites.accept {sig, invite}` and the DM opens; eligible
otherwise, `SlackConnectConfirmTrustModal` ("Before you accept this
invitation…", the inviter, "{name} from {team}.", the email, "This
organization has used Slack since {MMM YYYY}", Ignore / Accept
Invitation). **Left out:** Accept on a channel invite: it opens Slack's
multi-step accept modal (workspace, channel name, privacy, "Accept and
Join Channel"), which Clack doesn't have; the trust modal's 48px header
illustration (a CDN image Clack doesn't carry) and its "new account"
variant (the invite's trust level isn't in the feed).

**Sender, footer.** laKt `BaseInviteSenderName`: the inviter, the
inviting organisation's TeamIcon (16px, `margin_right_25`; the invite's
`inviting_team.icon`), and in Detailed the team's name. VeYL: an app
destination that's your Slackbot DM reads "Slackbot" when
`slackbot_unread_v2` is on (rZx3 AN), else "App"; a file destination is
`FileName` (the `file` icon and the file's title from files.info; nothing
until it loads), so a file reminder's footer is "Reminder from [file]
**title**".

**Item menu** (gantry wrqs `getActivityItemMenuTemplate`, activity inbox,
not the DMs tab): mark_as_read / mark_as_unread, save_for_later,
change_notifications | remind_submenu, notify_broadcast_mentions,
toggle_notifications_replies, mute_conversation, move_to_section,
toggle_vip_user, copy_link | summarize_thread | clear_all_before |
open_in_home, open_in_split_view, open_in_new_window. New in Clack:
- Remind ▸ Custom… (`onAddReminder` "Custom" → the Reminder dialog).
- Edit notifications (channel posts and channel invites): the
  ChannelNotificationPrefMenu as its submenu, its description the channel's
  setting ("Muted", "All new posts", "Just mentions", "Nothing").
- Move channel ▸ the sections (with a custom section), else "Move to new
  section" (Create a section with the channel).
- "Don’t notify for @here and @channel" / "Notify for @here and
  @channel" (at_channel, not muted): the channel's `suppress_at_channel`
  through `users.prefs.setNotifications`.
- Add to VIP / Remove from VIP (a DM with someone who can be one, on a
  paid team): `users.priority.add` / `remove`, "Added {name} to VIPs".
- Copy link: in the inbox it's the message's link. Clack showed the DMs
  tab's Copy ▸ Copy name / Copy link submenu; that and its Copy huddle
  link are the DMs tab's, which Clack's Activity isn't.
- Open in split view (after Open in home, not in Simplified layout
  mode): the thread or the channel in the split view, with the "Ctrl Alt
  Click" / "⌘ Opt Click" hint; that modifier click on a row does the same
  (GMkG maybeOpenInSurface). See docs/SPLIT-VIEW.md.
- **Left out:** Summarize thread (needs
  `ACCESS_SLACK_AI_CHANNEL_THREAD_SUMMARIES`, Slack AI, which the account
  lacks); Open in new window (Clack has no second window).

**Clear all, offline, saved views.** `activity_clear_all_completed` with
an undo_key raises `ActivityClearAllToast` (bottom right):
"Notifications cleared. Undo"; Undo calls `activity.clearAll {undo_key}`
(Wy3r) and says "We brought back your notifications", or "There was an
issue clearing some notifications". `ActivityRowLoader` offline shows DXqe
`InlineOfflineIndicator` (96px, 13px: `cloud-offline`, **No
connection.** "Go online to load more activity."); Slack's signal is
`connectivity.clientOnline`, Clack's a next page that failed for want of
a network. With `activity_global_settings` off the layout toggle sits in
the filter bar and each tab is its saved view with its own density
(`activity.views`); 9Gp `updateActivityViewsByApi {density, isAutoSave}`
writes `activity.views.update {view_id, sort, density}` for any view but
All, which only changes the page.

**Tests and screenshots.** `TestActivityInviteActionsLikeSlack`,
`TestActivityInviteWrites`, `TestActivityReminderWrites`,
`TestActivityClearAllViewsAndSuppress`, `TestActivityRepliedReacted`
(core, fakeslack); `TestActivityReadMarksLikeSlack`,
`TestActivityReminderActionsLikeSlack`, `TestActivityInviteButtonsLikeSlack`,
`TestActivityFooterLabelsLikeSlack`, `TestActivityItemMenuNewRows`,
`TestActivityClearAllToast`, `TestActivityOfflineFooter`,
`TestActivitySavedViewDensity` (views). The activity-shots artifact adds
`side-rows-{detailed,dense}-{light,dark}.png` (these rows next to Slack's
markup under its shipping CSS) and the toast, offline footer, invite
modals and new menu rows.

## Bulk select, saved views, sort, keys, live updates (2026-11-10, branch `activity-rest`)

Read from the gantry build (`client-boot-render.777e711847af270a`,
`client-boot-shared.4f8632cd9602d1fa`, `client-boot-vendors.a7070832025a6dd7`,
`client-boot.bac7c893b49156a7`; public CDN, no cookie) and the shipping CSS
(`client-boot-styles`, `helper-gantry-v2-styles` for the spacing helpers).
Nothing was opened in a live Slack client; every write below is tested
against internal/fakeslack only.

**Triage mode** (3x6A `$Y`: bulk action mode, P0K2 `m1`, or anything in
`bulkItems`). In it the filter bar is ActivityFilterBulkAction alone
(m+ho: Unreads, the filters, search and the right side are `!L &&`);
under it ActivityBulkSelectionBanner (i9bp: margin 0 16px 8px, padding
8px, radius 8, `light-dark(base-sec, base-ter)`, 13px/22px): "{n} items
selected on page." and, with no search, at least one row and more
loadable or not all picked, "Select everything in **{view}**" (or
"Select everything matching these filters." when the type filters
differ from the view's saved ones). That link picks every loaded row and
turns on server-side select-all (`Ro`): "Everything in **{view}** is
selected.", rows loaded later join it (eP2o), "Mark selected as read" /
"as unread" go and Clear is "Clear everything": ClearAllFilteredDialog
("Are you sure you want to clear everything matching this filter?",
'Use the "Cleared items" filter to access these once cleared.', Cancel /
Yes) → `activity.clearAll {reason: server-side-select-all-clear,
filters}` (FtTX `oe` with `read_state: reads_and_unreads`; `PK` drops
`saved_reminder` while reminders are off in Activity). Each row leads
with its checkbox (O6z2 RowCheckbox: the triage grid's 22px, the 16px
checkbox centred, the row card 10px after it and 16px short of the
right, `activity_row_content_margin_triage`); a checkbox toggles the row,
Shift toggles the range from the last pick (0lkN onSelectRow; unpicking
hands "last" to the next picked row below); a click on the row still
opens it. A picked unread row is `--bulk-selected` (base-hgl-1); a read
one keeps `--read-fill` (Slack's cascade: `.grouped.read-fill` is the
more specific rule). The select checkbox is indeterminate while only
some rows are picked. The buttons write once (`activity.markRead` /
`archive` with `{reason: bulk_mark_read | bulk_archive, entries: [{type,
key, feed_ts}]}`, `markUnread` with `bulk_mark_unread` and `ts`), are
icon-only under 415px (isSmallWidth), 8px apart (`margin_left_50`), and
disabled base-ter under content-sec without a border. "Mark selected as
unread" needs `activity_bulk_unread_tool` (5Ns/ Yw); Clear has no toast
with the activity inbox (`_ ||`); Custom select (BulkActionsSelect,
inbox only) is bulk action mode with nothing picked.

**Saved views** (GuYW ActivityInboxSavedViewTabs, rsjQ fetchActivityViews,
birT / +9Gp / r23F / dWbk, RY04). activity.views gives the views, the
global prefs and, by omission, the hidden default types. The tabs: All,
the defaults not hidden (DMs while DMs show in Activity, Vn), then the
custom views by position, each "{emoji or BGxe icon} {name}" (QIkC, 16px,
4px gap). Each view keeps its own filters while the page is open (P0K2
views[]); a custom view starts from its saved ones (formatFilterViewState:
entry_types back to the rows, unread_only, archive_only, priority_only,
channel_section_ids, channel_ids), and the feed gets them
(`priority_only`, `channel_ids`, `channel_section_ids`). Switching views
ends the bulk selection and closes the detail pane (handleViewChange). A
custom tab's context menu is Rename view / Delete view
(ActivityFilterSaveViewConfirmationModal: "Are you sure you want to delete
“{view}”?", "This view will be removed from Activity.", Delete), a
default one's (not All) Hide view (ActivityHideViewConfirmationModal,
"This tab will be hidden from Activity.", Hide). The "+"
(ActivitySuggestedTabsMenu 9av4, 280px) lists Hidden views (unhide),
Default suggestions (vzS1 cv: Unreads, Mentions, Threads, Reactions,
VIP with VIPs, minus any a view has by name or filters), Suggestions from
your sidebar (Starred, the custom sections, up to ten), Create a custom
view (`plus-filled`); with nothing to suggest it opens the modal straight
away, and past 15 views it's off. A suggestion is a view
(`activity.views.create {name, sort: newest, density: compact, filters}`)
shown at once; the view the user was on loses its unsaved filters
(`Lr("clearAllTab")`). ActivityFilterSaveViewModal (fNIs, 520px): "Create
a new view" ("View name (required)", the input container: 38px, 4px
radius, the emoji picker, placeholder "New view", "Remove emoji"; then
ActivityAddTabContent: Layout's two 76px cards without
activity_global_settings, "Only include unreads" with its switch, "Filter
by" and the rows in two columns, Sort by with VIPs), "Save this view"
(the filter menu's primary "Create Custom View", enabled with unsaved
filters; in a custom view "Save as …" ▸ New view / Current view and
"Delete View") and "Rename this view"; Slack's errors (RY04 `U_`: "There’s
already a view with this name.", "View name is required.", …) under the
input; '"{name}" has been saved successfully.' after. Realtime
`activity_views_updated {views}` replaces the views (sD/F
activityViewsUpdated).

**VIP and sort.** The filter menu's VIP row is filterVipSelected →
`priority_only=true`. Sort (1p6b G3): "Newest" and, with VIPs, "VIP
unreads on top": the header's sort button (KJvs HEADER, with
activity_global_settings on and the prototype off) and ActivitySettingsMenu's
"Sort by" and "Density" sections (vzfm, behind `activity_in_home_prototype`,
6rqO j; Density's "Multi-line rows" / "Single-line rows"). Both write
`activity.prefs.update {sort | density}` (UykM updateAllActivityViewsByApi,
every view takes it); VIP unreads on top asks for
`priority_unreads_first_v1` (TfjX w) and lists the unread VIP rows
(`entry.priority`, /29I isPriority) under "VIP unreads", then "Everything
else" (U5s4 ActivityList rows, oDbF ActivitySectionHeader: 42px, the 20px
tiles mark-as-important-filled on campfire-0/30 and notifications-filled
on base-ter, 13px/18px bold content-sec), without day dividers in the
first group or on the first row after the second header.

**Keyboard** (U5s4 ActivityList's virtual list, WbG1
useActivityItemShortcuts, N8Zu ClientKeyboardShortcutsActivity): a press
on the list focuses it; Up / Down move the focused row (items only), Enter
or Right opens it; the focused row shows its actions and, after a key, the
bordered list's focus ring (4px in, square; the Dense row's bottom 0).
U read/unread, C clear (restore in Cleared items), X pick, Space pick in
triage mode, A Save for later / Add to-do, M the custom reminder, F
forward; in triage mode Alt+U marks the picked rows read and Alt+C marks
them read and clears them. Alt+Up / Down open the previous / next row,
Shift+Alt the previous / next unread, ⌘. closes the detail pane, ⌘Z undoes
the last clear or read change.

**Live updates** (sD/F handleActivityRTMEvent with the inbox,
activityUpdatedV2 → 46Vh pushActivityItemToStore): `activity_updated`
replaces a listed entry, or inserts a new one at its feed_ts unless the
view can't show it (reminders / DMs off in Activity, cleared vs Cleared
items, a reaction under Unreads, a non-VIP under VIP, another type than
the view's, a conversation outside its channels and sections); a cleared
one leaves. `activity_invalidated` fetches the feed again;
`activity_deleted` drops the entry. A new unread entry while scrolled
down raises "New activity ↑" (ActivityUnreadsBanner: 28px `#1264a3` pill
12px from the top). Clack no longer reloads the feed on mention and count
events (Slack doesn't).

**Tests and screenshots.** `TestActivityViewsRoundTrip`,
`TestActivityViewsEvent`, `TestActivityBulkAndFilters` (core, fakeslack);
`TestActivityTriageLikeSlack`, `TestActivityBulkWritesLikeSlack`,
`TestActivityCustomViewsLikeSlack`, `TestActivityVIPAndSortLikeSlack`,
`TestActivityKeyboardLikeSlack`, `TestActivityLiveUpdatesLikeSlack`
(views). The activity-shots artifact adds activity-triage(-dense),
activity-select-everything, activity-clear-everything,
activity-plus-menu, activity-custom-tab, activity-create-view,
activity-filter-create-view, activity-settings-sections,
activity-vip-sort, activity-keyboard-focus and activity-new-activity.
The `section` and `mark-as-important-filled` glyphs are Slack's own paths
(internal/ui/icons/activity.go).

## Not done

- Reaction bundles ("reaction_bundle" rows), behind an experiment.
- The typing indicator and agent status in the status column; the
  importance icon; Block Kit action buttons under app messages.
- Accept on a Slack Connect channel invite (Slack's multi-step accept
  modal), the trust modal's illustration.
- Summarize thread, Open in new window (see above); the priority /
  importance rows.
- The subtext is drawn as a column before the message, so a 2-line
  message's second line is indented under it (Slack runs it inline).
- Specific channel or section ("In" in Create a new view, the filter
  menu's row): Slack's channel and section picker (IKpm) isn't in Clack;
  saved views that name sections or channels are applied, not edited.
- The mention sub-filters (@you, @channel, @user-group, keyword) in the
  filter menu and the new view's rows; "Discard changes?" when closing
  Create a new view with changes; dragging tabs to reorder
  (`next_view_id`); the tab strip's icon-only mode when narrow; the
  sort button's "new" badge; tooltips on the bulk buttons and the "+".
- `s` (snooze) and the other keys that act on snoozable items: Clack has
  no snooze.
- The rest of activity_in_home_prototype (Activity moved into Home, the
  settings menu in the filter bar): only its Sort and Density sections.

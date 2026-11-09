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
at_user_group, channel invites); Copy ▸ Copy name, Copy link; Clear all
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

## Not done

- The menu rows Clack can't carry out yet are left out rather than stubbed:
  Remind ▸ Custom…, Edit notifications ▸ (channel posts and invites), Move
  channel / Move to new section, Don't notify for @here and @channel, VIP
  toggle, Summarize thread, Open in split view, Open in new window, Copy
  huddle link, and the priority / importance rows.
- Reminder rows' Mark Complete (Dense) and Reschedule hover buttons, and the
  reminder state badges (Incomplete / Completed).
- Reaction bundles ("reaction_bundle" rows), behind an experiment.
- The MultiLine (layout revisions) row and its ActivityEntryTypePill.
- The "Needs reply" / "Replied" / Draft badges, typing indicator, VIP badge
  and importance icon in the row.
- Invite rows' Accept / Decline buttons (`ActivityRowContentInviteActions`)
  and Block Kit action buttons under app messages.
- The external inviter's team icon; file names in "Reminder from {file}".
- `activity_clear_all_completed`'s undo toast; saved views
  (`activity.views`) and their realtime updates.
- The offline "Go online to load more activity." footer.
- The subtext is drawn as a column before the message, so a 2-line
  message's second line is indented under it (Slack runs it inline).

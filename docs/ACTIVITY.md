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
- **Row layout.** With `activity_entry_layout_revisions_2` off and the
  grouped flag on (the account measured in FIDELITY round 2), rows are
  `ActivityRowContentDetailed` (Gh4i `TN`) in its grouped grid: avatar,
  name + timestamp, status column, the *context pill*, body, extras
  (reactions, invite actions, Block Kit buttons). Dense is
  `ActivityRowContentDense` (Gh4i `O4`). The layout-revisions variant is
  `ActivityRowContentMultiLine` (SLUr), with `ActivityEntryTypePill` on the
  right; Clack doesn't draw that variant.

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

**Context pill** (aJri `withPrefixes`, variant `pill`, grouped): a 16px type
icon (`PillTypeIcon`: sGb0 `Z1` → `_F`; none for reactions and channel
posts) then the text:

| item | pill |
|---|---|
| at_user, at_user_group, at_channel, at_everyone, unjoined mention | Mention in {destination} |
| keyword | Keyword in {destination} |
| thread_v2 | Thread in {destination} |
| message_reaction | Reacted {emoji} (no image for the emoji: its `:name:` alt text, clipped to the 16px box, from vbPY's `renderEmojiFallback`) |
| saved_reminder | Reminder from {source} (message, file, canvas, list item), else Reminder |
| channel post | {#channel} (no "Post in": that is the non-grouped footer) |
| dm | Direct Message |
| mpdm | the group's names |
| app | App |
| invitations | Invitation |
| joiner / prejoin | Workspace invitation |
| generic_system_alert | Channel / User group / External channel / Workflow / Invitation / Notification |
| list items with an item (assigned, edited, mentioned, to-do) | Mention in ▤ {list title} (the lists icon; the title is empty until the list loads) |
| list approvals | the type icon alone (no item, so ActivityListName renders nothing) |

Icons: mentions `mentions`, DMs `direct-messages`, apps `apps`, threads
`threads`, invitations `paper-plane`, reminders `reminder`, alerts and
prejoin `notifications`, anything else `mentions`.

**Sender** (wM7g `ActivityRowContentSenderName`): an external invite's
"{user} from {team}" (the team name after the user in Detailed; "Slack
Connect Invitation" before the invite loads); "Slack" for system alerts;
"Reminder" for reminders; the app for app messages; reactions
(`ReactionSender`): the reactor, "{name} & others" when other people reacted
too ("{name} +{n}" in Dense); threads (`ThreadSender`): the reply's author
and the root's other participants (not you, not Slackbot): "{a} and {b}",
"{a} & {n} others", Dense "{a} +{n}"; "Slackbot"; else the person.

**Body** (w4rX `ActivityRowContentDetails`): the message (2 lines Detailed,
1 Dense) with "You: " when you wrote it; reactions: "You: " and your message
on one line (`ReactionContent`); reminders: "{author}: " and the message,
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
  (each row's sender, pill, icon, body, subtext), `TestActivityHoverButtonsLikeSlack`,
  `TestActivityItemMenuLikeSlack`, `TestActivityClicksMarkRead`,
  `TestActivityRowContextMenu`, `TestActivityRealtimeDelete`,
  `TestActivityParityShots` (views).
- Screenshots: the activity-shots workflow's artifact (Detailed and Dense,
  light and dark; hover bar, item menu, Clear all before, light and dark).

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

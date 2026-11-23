# Notification rules: Slack's client and Clack

Every kind of desktop notification Slack's client shows, what decides it,
and what it does under each condition, read from Slack's own code. Clack's
port is noted per cell. The decision log (`notify: show|skip <reason>
ch:…/ts:…` in clack.log, docs/INSPECT.md) uses Slack's reason numbers, so a
"didn't get a notification" report reads against this file.

Source: Slack's web client build **a09b625d90aa** (2026-11-21:
client-boot `83aff18a1847b8e9`, boot-render `3c277d3805090044`, boot-shared
`2c4b2e38fc703b98`; cached in clack-ops/scratch/directories-usergroups/cdn).
Module ids are that build's. `Fpyc` holds the rules (rr, RM, gA, HW, x6, pK,
the on-screen checks, the sounds); `maybeShowNotification` (boot-render) runs
them; `Eley` (boot-shared) shows the notification.

Status marks: **✓** Clack does what Slack does. **fixed (n)** it didn't until
notify-relatives part n (1: the rules, 2: sounds and huddles, 3: the
sidebar's marks, 4: Windows, 5: the stall watchdogs). **gap (n)** it still
differs; part n of notify-relatives takes it. **n/a** a feature Clack
doesn't have (named).

## 1. The paths

| Kind | What arrives | Gate | Clack |
|---|---|---|---|
| DM: a person's, an app's, Slackbot's | socket `message` | maybeShowNotification → RM `shouldShowNotificationForDirectMessage` | ✓ ui/notify_dm.go `noteDMReason` |
| Group DM | socket `message` | → rr `shouldShowNotificationForChannelMessage`, level `global_mpdm_desktop` | ✓ `noteChannelReason` |
| Channel message: the viewer's mention, @here/@channel/@everyone, a user group, a keyword | socket `message` | → rr | fixed (1), see §3 |
| A followed thread's reply | the server's `desktop_notification` | → gA `shouldShowNotificationForDesktopNotification` | ✓ ui/notify_threads.go `desktopNotification` |
| Thread broadcast ("Also send to #channel") | `message` subtype `thread_broadcast` | → rr or RM: a reply (isReply, SPMg), 6 without a mention | fixed (1) |
| Bot's, app's, Workflow Builder's post | `message` (`bot_message`, `bot_id`) | → rr: 9, 10 | fixed (1) |
| Slackbot | `message` in the Slackbot DM | → RM: 22 for a forwarded email | fixed (1) |
| Reminder (/remind) | Slackbot DM `message` ("Reminder: …"); `reminder_fired` only updates the list | → RM | ✓ |
| Later reminder | `saved_due` | `handleSavedDue` (boot-render) loads the item → x6 `shouldShowNotificationForSavedItem` → `showSavedItemNotification`: "Reminder due" with the item's content (a message, a list record, a canvas section, a reminder, a file), a click opens it in Later | gap (6): not handled. A path of its own (the socket event, x6, the content per item type, the click into Later, fakeslack's `saved_due`), left for part 6 |
| Calendar reminder | `calendar_reminder` | `showJustInTimeCalendarNotification` (silent) | n/a: Clack has no calendar integration |
| Huddle invite | `huddle_invite` | HW `shouldShowNotificationForHuddle` → `showHuddleNotification`, plus the ring | fixed (2): see §6 |
| Huddle started in a channel | `message` subtype `huddle_thread` | → rr: never as a mention (canCountAsUnreadForCurrentUser), passes at Everything | fixed (1) |
| "Join the huddle" suggestion | `huddle_recommend` | HW → `showHuddleRecommendationNotification` | gap (6): not handled. Clack draws no huddle suggestion at all, so the toast comes with that feature |
| Slack Connect or channel invite | `desktop_notification` with `is_channel_invite` | gA: shown (0) before any reply check | ✓ |
| Canvas mention | `message` subtype `document_mention` in the canvas's file channel | maybeShowNotification lets it past the member check; → rr: 1 without the viewer's mention | fixed (1) |
| List assignment | Activity item `list_record_assigned` | none: Activity only. A `list_mention` message → rr (1 without a mention) | ✓ |
| File comments | a canvas's or list's comment in its file channel (`document_comment_root`, `list_record_comment`, `surface_card_comment`) | → rr: 25; `canvas_section_stub` 26; their replies as replies | fixed (1) |
| Approval request | `approval_response_requested` | pK `shouldShowNotificationForApprovals` | n/a: no workflow approvals |

Nothing in Activity toasts: `showNotification`'s only other callers are a
send-failed notice and a debug log. Activity items come from the server
(`activity_updated`) whatever the notification prefs.

## 2. maybeShowNotification (every message and desktop_notification)

In order:

1. **Shown already**: the shown keys (`channel:ts`, a desktop_notification's
   `channel:msg`, the last 100). Clack ✓ (21).
2. **Push switch** (`u0`): the conversation's `desktop_push_enabled`, else
   `global_desktop_push_enabled` (on when unset); "Skip pref-disabled push
   notification". This is the prefs page's "Nothing" and a channel's
   mobile-only setting: no level is ever `nothing` (it reads as
   `mentions_dms`). Clack ✓ (`pref-disabled`).
3. **Streaming** (`defer_notification_3p_streaming`): an in-progress chat
   stream, 33; the update that settles it notifies (messageChanged hands
   over the store's copy). Clack ✓.
4. **Member**: the viewer is in the conversation, or the message is a canvas
   mention (`document_mention` in a file channel); else 2. Clack fixed (1)
   (the canvas mention).
5. **Which rule**: a desktop_notification → gA; an IM → RM; an ephemeral
   message in a file channel → 28; anything else → rr. Clack fixed (1): it
   held every ephemeral message; only its own local notices (core
   LocalEphemeral, Slack's addEphemeralMessage) never reach the rules.
6. **Agent turns** (`agent_turn_notifications`): 36. n/a: no AI agents' turns.
7. **Not shown**: if the message is in the focused window's active view,
   `maybePlayMessageSentOrArrivedNoise` plays the arrival sound (or the sent
   sound for the viewer's own). n/a: both sounds are Accessibility settings
   (`conversationIncomingMessageSound`, `conversationSentMessageSound`, F25J),
   kept per device in local storage, which Clack's preferences don't have.
8. **Shown**: showNotification (Eley): permission (15), not from a secondary
   window, `ensureModelsArePresent` (5 s), the dock bounce
   (`mac_ssb_bounce`), `maybePlayNewMsgSound`, then the OS notification.

## 3. rr: a channel's or group DM's message

`shouldShowNotificationForChannelMessage` (Fpyc), in order:

| # | Rule | Clack |
|---|---|---|
| 4 | `no_display` | ✓ |
| 25, 26 | file channel: `document_comment_root`, `list_record_comment`, `surface_card_comment`; `canvas_section_stub` | fixed (1) |
| 6 | a reply (isReply: thread_ts ≠ ts, a broadcast too) that doesn't mention the viewer: their mention, a user group of theirs or @here (checkForMentionInMessage with ignoreHighlightWords; @channel and @everyone never count in a reply) | fixed (1): broadcasts |
| 8 | `channel_join`, `channel_leave`, `group_join`, `group_leave` | fixed (1): Clack held every join, leave, topic, purpose, rename, pin and bookmark without deciding |
| 9 | `bot_message` at Mentions without a mention | fixed (1) (Clack: 1, or shown for a keyword) |
| 10 | an app's message (`bot_id`, sender `is_app_user`: isAppMessage) at Mentions without a mention | fixed (1) |
| 1 | `document_mention` or `list_mention` without a mention, any level | fixed (1) |
| 1 | the level: Everything passes; Mentions needs a mention that counts as unread (canCountAsUnreadForCurrentUser: not the viewer's own unless a workflow posted it, not a join, a huddle's post, an app's join/leave/created, an `_unread_ignore` subtype, `dnd_suppressed`) | fixed (1) |
| 16 | on screen (shouldSuppressNotificationInActiveChannel): the focused window's Primary view shows the conversation's messages (no tab past Messages), unless the text holds an at-command (@channel, @everyone, @group, the Salesforce teams; not @here) | ✓ |
| 11 | the viewer's own (`user` or `comment.user`) | ✓ |
| 5 | `no_notifications` | ✓ |
| 12 | muted, unless the viewer's own mention or a keyword is in it (checkMessageAndUpdateMentions without at-channel mentions or user groups) | fixed (1): Clack held all |
| 13 | Do Not Disturb, unless `_ignore_dnd` (dnd_override) or a VIP's message (Mvkl Wh) | ✓ |
| 24, 17 | another workspace's channel (XWS); an org channel another signed-in workspace of the org shows (shouldSuppressOrgChannel) | n/a: one workspace |
| 18 | an @channel in a large channel by someone the loud-mentions limit stops | gap (later): the LoudChannelMentionsLimitAccess feature |
| 29 | a sender the viewer hid | ✓ |
| 30, 31, 34, 35 | a channel canvas's update, a tab canvas's, a tab added, a folder's item | ✓ |

**What mentions the viewer** (checkMessageAndUpdateMentions, uSWp
checkForMentionInMessage): blocks if the message has any, else its text,
then its attachments (not link previews or forwards). The viewer's own
mention always counts. Where the channel takes at-channel mentions
(can_channel_have_at_channel_mentions, QWNA: Everything and not muted, or
no `suppress_at_channel`; a #general the viewer can't post in always), a
user group of theirs, @channel, @everyone and @group (not in a reply) and
@here (not while the viewer is away) count; elsewhere none of them do,
user groups included. Highlight words count except in a `bot_message`
without a workflow bot's profile and in an app's message
(shouldIgnoreHighlightWords). Clack fixed (1): it read any `<!here`,
`<!channel` or `<!everyone` in the text, never a user group, never an
attachment, and a keyword in any bot's message. Not ported: the #general
branch (Clack keeps no posting permission for it).

**Levels** (getCalculatedDesktopNotifySetting): a channel's desktop
override, else `global_desktop`; a group DM's override, else
`global_mpdm_desktop` (unset: Everything). `nothing` reads as
`mentions_dms`. Clack ✓.

## 4. RM: a DM's message

`shouldShowNotificationForDirectMessage`: 4 `no_display`; 6 any reply (a
broadcast too; the newest build lets a settled Slackbot answer's reply
through to its thread check); 16 on screen; 11 own; 12 muted; 5
`no_notifications`; 33/23 a Slackbot AI answer with `suppress_notification`
(n/a); 13 DND unless `_ignore_dnd` or a VIP; 17 (n/a); 22 an email
forwarded to Slackbot (the Slackbot DM, `upload` with a file of mode
`email`); 29 hidden sender; 30, 31, 34, 35 and 32 (an assistant thread's
root or first message). A DM has no level: every message notifies. Clack
✓, fixed (1) for broadcasts (6) and 22.

## 5. gA: the server's desktop_notification

3 no known conversation; 24, 17 (n/a); 0 a channel invite
(`is_channel_invite`) before any other check; 7 not a reply ({ts,
thread_ts}); 16 the thread open in the focused window's side pane (or a
huddle's canvas thread, or an agent's or Slackbot AI thread in the main or
split view: n/a); 29 hidden sender; 19 newer than `notifications.maxTs`; 20
the thread read to it. No level, mute or DND check: the server decides
whom it sends one to. Clack ✓ (thread-reply-toast rounds 1–3).

## 6. The other gates

- **HW (huddles)**: permission 15, DND 13 (no VIP override), the channel
  muted 12, the level 14, no channel 3, 24, 17. The level never holds one:
  getCalculatedNotifySetting reads a stored `nothing` (the conversation's or
  the global) as `mentions_dms`. `huddle_invite` also ignores an invite to
  the huddle the viewer is in. Clack (huddleui `mayNotifyHuddle`): DND,
  mute and the known channel ✓; fixed (2): it held an invite in a
  conversation, or a workspace, set to Nothing
  (TestIncomingInviteNotifiesAtNothing). `huddle_recommend`: gap (6), §1.
- **x6 (Later reminders)**: permission 15, `global_desktop` stored as
  `nothing` 14 (areGlobalDesktopNotificationsDisabled reads the stored
  value), a message's channel must be known (3), 24, 17. No DND check.
  gap (6), §1.
- **pK (approvals)**, activities summary, onboarding, seasonal, welcome
  party: permission 15, `global_desktop` `nothing` 14. n/a.

## 7. Sound, dock, badge, Activity, sidebar

- **Sound with a notification** (maybePlayNewMsgSound): canPlaySound says no
  for the OS's sound setting (the desktop app), `mute_sounds`, Do Not
  Disturb unless the message is a VIP's (a dnd_override's toast is silent
  unless it's a VIP's), and, under `message_sound_debounce`, a second sound
  for the same thread or channel within 5 s; nor while a call window or a
  huddle is active (getWindowForActiveCallAcrossAllTeams,
  getActiveHuddleCallId). The sound is
  `desktop_sound`, or `priority_desktop_sound` for a VIP. Clack
  (ui/notify.go `notifyMessage`): the OS setting (SHOULD_PLAY_SOUND), DND
  with the VIP exception and the sound names ✓; fixed (2): the debounce
  (`soundDebounced`, TestMessageSoundDebounceLikeSlack) and no sound in a
  huddle (TestNoMessageSoundInAHuddle). Clack has no call windows apart
  from huddles.
- **Sound without one**: the arrival sound (§2.7). n/a.
- **Dock**: macOS bounces the icon on a shown notification per
  `mac_ssb_bounce` (short, long, off). Clack ✓ (ui/dock.go `bounceDock`:
  maybeBounceDockIcon, and the shell's Do Not Disturb check before
  dock.bounce).
- **Dock and taskbar badge**: getDesktopUnreads / getAppBadgeCount over the
  sidebar's counts. Clack ✓: the count moves with the message event that
  makes a row unread, in the same `handle`.
- **Threads link**: bold while the threads have unread replies, the badge
  `channel_badges.thread_mentions`. Clack ✓ (thread-reply-toast round 3).
- **Activity's unread**: the server's `activity_updated` items. Clack ✓.
- **Sidebar bold and mention badge** for a message the socket brings
  (countUnreadMessages, EJMC; core/badge_rules.go): bold when
  canCountAsUnreadForCurrentUser (qxvW) says so, with
  `system_messages_redesign_m1`'s system messages: not a join or leave, a
  huddle's post, a topic, purpose or canvas update, a conversion, a
  retention notice, an AI app's thread root; the viewer's own join counts
  when someone added them, and a workflow's post as them; a broadcast
  counts until its thread is read past it (B95B). The mention badge when
  shouldMessageBadgeChannel (eqVu) says so: every message where the
  conversation badges all unreads; in a DM, or a conversation that takes
  at-channel mentions (QWNA) and isn't muted with muted items hidden
  (`improve_mute_pref`), the viewer's mention, a user group of theirs,
  @channel, @everyone and @group (not in a reply) and @here (not while
  they're away, nor from before the session's start: `start_ms`; the build
  never sets `_presence_last_changed`); elsewhere the viewer's mention and
  their user groups. Their keywords count except in a bot's or an app's
  message. Fixed (3): Clack bolded the row for every message, counted
  @here, @channel and @everyone anywhere and while away, never a user
  group's or a mention in an attachment, and a keyword in any bot's
  message, and read the viewer's join as their own message
  (TestSidebarMarksLikeSlack). Cleared when read anywhere: the server's
  `channel_marked` ✓. Not ported: a file comment of the viewer's and their
  `mpdm_move` (Clack decodes neither; file comments are gone since 2018),
  and the streamed messages of an agent's session channel behind
  `session_channels_streaming_unread` (off in Clack's experiments).

## 8. Windows: why a toast Clack sent doesn't show

The decision line's `notify: show` is the rules'. What Windows does with
the toast after that is logged by ui/notify_windows.go with the same tags,
read from Windows itself (internal/desktop focusassist_windows.go,
notifystate_windows.go). Slack desktop asks the toast notifier's setting
before a toast and Focus Assist before a sound (checkNotificationState,
main.bundle.cjs 4.53.276 win32: "Suppressing notification display due to
Windows toast notifications being disabled or FocusAssist being active").

| Windows' reason | What Windows does | clack.log |
|---|---|---|
| Clack's notifications off (Settings > System > Notifications > Clack) | takes the toast, never shows it | `notify: Windows has Clack's notifications off (…): the toast for … won't show`, from the registry's `Notifications\Settings\<app id>` `Enabled` 0, since `ToastNotifier.Setting` fails for Clack's unpackaged app id (0x80070490) |
| Every app's notifications off | the same | `notify: Windows has every app's notifications off (…)`, from `PushNotifications` `ToastEnabled` 0 |
| A group policy, the manifest | the same | `notify: Windows has Clack's toasts off (off by a group policy)`, when `ToastNotifier.Setting` answers |
| Focus Assist or Do not disturb: alarms only, or priority only without Clack on its list, by hand or by its automatic rules | sends the toast to the notification center without a banner | `notify: Focus Assist is on (alarms only): …` |
| The shell holding banners: an app or a game full screen, presentation mode, the screen locked, the quiet hour after a first sign-in, a Store app full screen | the same | `notify: Windows holds notification banners now (an app is full screen): …`, from SHQueryUserNotificationState |
| The app in focus | shows the app its own toasts | nothing from Windows; Clack's rules hold them (`notify: skip 16`) |
| A burst | shows the banners one after another | `notify: N toasts in 10s, the one for … last: …` |
| Shown, then gone | | `notify: the toast for … left the screen after 6s: its banner timed out (…)`, or closed, or taken back by Clack |
| Refused | | `notify: Windows didn't show the toast for …: <HRESULT>` (its Failed event), `notify: the toast for … didn't go to Windows: …` |
| Clack's app id not registered | keeps the toast in its history only | `notify: registering Clack's app id for toasts: …` |

Clack's own notification settings, as Windows keeps them, are listed
whenever they change: `notify: Windows' notification settings for Clack:
Enabled=1 ShowInActionCenter=1 …`.

## 9. When nothing comes: the stall watchdogs (part 5)

Karim's α.136 wrote nothing to clack.log from 01:57Z to 02:42Z on
2026-11-22 while its socket answered pings, and a reply's toast never
came. Slack's socket manager keeps a socket only while it hears from it:
a `reconnect_url` comes every 285 s at most, and a missing pong closes it
after 90 s (memory: slack-socket-manager-rules). Clack's watchdogs
(internal/slack/rtm.go, internal/core/pump_watch.go):

- **A socket that only answers pings**: no frame of Slack's own (an event, a
  `reconnect_url`) for twice 285 s while the pongs come. clack.log says
  `rtm: nothing from Slack for … but answers to its pings: closing the
  socket to connect again (4998 …)`, then every goroutine's stack, and the
  socket closes with 4998 to connect again, as a slow connect.
- **A pong check that stops running** (a ping stuck in its write): `rtm:
  the pong check hasn't run for …`, the stacks, and the same reconnect.
  A look long after the last one (the machine slept) leaves the socket to
  the pong check.
- **A handler that blocks**: the event pump, or a queue behind it (the
  lookups', the Activity events', a held conversation's), on one task for
  30 s: `core: <the queue> has been on <a TYPE/subtype event of ch:TAG>
  for …, N waiting behind it`, the stalled goroutine's stack and every
  other's, and `core: … took …` once it's done.
- **A full event buffer** is not silence: `rtm: event buffer full (N events
  wait for the handler): dropping … and what follows until it has room;
  the socket still brings them`, a count every 10 s, and `rtm: the event
  buffer has room again, after N events dropped`.

Dumps come at most every 10 minutes. A healthy idle socket, which hears
a `reconnect_url` now and then, logs none of it (TestRTMKeepsAnIdleSocket,
TestIdleSessionLogsNoStall).

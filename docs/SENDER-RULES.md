# Message sender rules

How Slack's web client draws who sent a message: avatar, name, app badge,
grouping into continuation rows, and the pinned label. Read from the public
CDN build a84662d2d135 (2026-11-08). Module ids are webpack ids in
`client-boot`, `gantry-v2-async-client-boot-shared` (boot-shared) and
`modern.vendor`. Code: `internal/core/sender.go`, `internal/core/badge.go`,
`internal/ui/messages/list.go` (`groupable`), `internal/ui/messages/pin_icon.go`.

## Who the sender is

`getMsgSenderEntity` (33X/): a message with `user` is that member's
(`{userId, botId, appId}`, with no username). A message without one is
`{botId, username}`. Special cases: huddles, list edits, canvas updates and
the welcome party, plus Slackbot file messages, which use the file's bot.

So an app that posts with a person's token (`user` plus `bot_id`, `app_id`,
`bot_profile`) is drawn as the person, with no badge. An agent app's reply
(its bot user plus `bot_profile`) is drawn as the bot user. Nothing names
the app. The only attribution line is `SendOnBehalfAttribution`, "Sent by
{delegateName}", which needs `delegate_sender_user_id` and
`show_delegate_attribution` (a delegated send, a different feature).

`display_as_bot` appears nowhere in the client.

## Name

`getMessageSenderNameFromState` (4ubN): member ? the member's name :
`botId && username` ? username : bot ? `bot.name` : … username. `username`
only names user-less messages.

## Avatar

`ConnectedBaseAvatar` (ztjr), `getMemberProfileImage` (yEEQ):

1. With a `userId`: the member's picture,
   `ca.slack-edge.com/{team}-{user}-{avatar_hash}-{size}`. `msg.icons` and
   `bot_profile.icons` are not read. A member without a custom photo has a
   server-picked default (a gravatar `g…` hash that falls back to
   `ava_00NN`, e.g. kyto's green `ava_0009`).
2. Without one: `msg.icons || bot_profile.icons`, then the store bot's
   icons. Workflow Builder's bot (apps `A0112L5RZ5E`, `A01126R8Y2X`) uses its
   own icons over the message's.
3. `icons.emoji` (`icon_emoji`) draws that emoji.
4. Otherwise `image_<n>`, where n is the smallest available size at or
   above the drawn size (`getIconSizeToFit`, wqqh). A 36 px avatar takes
   `image_36` at 1x and `image_72` at 2x.
5. No images anywhere: `ava_0002`, a white person on blue
   (`ava_0002-{24,32,48,72,192}` bundled; byte-identical to
   `a.slack-edge.com/df10d/img/avatars/ava_0002-72.png`).

`bot_profile` for an app without its own icon carries Slack's generic
plugin icons (`plugins/app/bot_36.png`, `bot_48.png`, `service_72.png`).
`service_72` is the blue notebook. Slack only shows it for user-less
messages.

## Badge

`BaseMessageSender.renderBotLabel` and `BaseAppBadge`:

- No badge when the member is human. `memberIsHuman` means the member
  exists and none of `is_bot`, `is_app_user` or id `USLACK` applies.
  Slackbot is served with `is_bot` false, so it gets no badge either.
  There's also no badge on huddle, canvas-update, welcome, custom-response
  and AI huddle-summary messages, or when the caller hides it. A user-less
  message always gets one.
- Label precedence: EXPERIMENTAL (sidekick, never passed by messages) >
  AGENTFORCE (`is_agentforce_bot`) > AGENT > WORKFLOW (`is_workflow_bot`
  on the member or the bot) > APP.
- AGENT (`isAgentApp`, /ly8) needs the experiments `show_as_agent` and
  `agent_badge` to be on. On Hack Club `support_lots_of_agents` is off,
  so the rule is `hasAgentBadgeByAppId`: the app's full profile from
  `apps.profile.get` (stored as `legacy_app`) must have
  `is_shown_as_agent`. The member's `agent_status` isn't read. The store
  usually holds the thin Flannel `apps/info` row, which has no
  `legacy_app`, so agent apps mostly show APP until their card is opened.
  With `support_lots_of_agents` on, AGENT means the member is an Agentforce
  or sidekick bot, or has `agent_status` `consistently_an_agent`.
- LEGACY (`SlackbotLegacyBadge`): a Slackbot notification in the Slackbot
  DM, on plans where the legacy Slackbot rebranding shows. Not ported.

## Grouping

`isAdjacentMessage` (boot-shared Ag38). The next message continues the
previous one's group unless one of these breaks it:

- either message is a system, huddle or tombstone-type message, or a
  thread broadcast;
- either message has `reply_count`;
- the gap is over 300 s or the day changes;
- two different users (a message from Slackbot is exempt).

Senders then compare like this:

- Same `userId`: grouped, whatever the username, icons or `bot_id`.
- A user-less bot: same `bot_id`, the same `username` (if it has one) and
  identical `msg.icons` (`isSameBotIcon`: emoji and image_36/48/64/72).
- Otherwise: the same non-empty `username`.

Pinning doesn't break a group. `Message.shouldHideSenderAndTimestamp`
hides the sender of any adjacent message, and the pinned label still
shows above it.

## Pinned label

`LabelsText` and the pinned label (GSoE):

- "Pinned" when nobody is recorded as the pinner;
- "Pinned by you" for the viewer;
- otherwise "Pinned by {member}", where the member is a name link
  (`c-message_kit__labels__link`).

The label combines with a broadcast ("{broadcast} ∙ {pinned}") and,
without the reskin, with a custom response ("{pinned} ∙ {custom}").
`pinnedBy` is `pinned_info.pinned_by` (`getMessagePinnedInfo`). A
`pin_added` event sets it (`extendMessageWithPinnedInfo`) and
`pin_removed` drops it.

The icon is the legacy font Icon `thumb-tack-filled`:
`.c-icon--thumb-tack-filled:before{content:""}` in font "Slack v2"
(`slack-icons-v2-6509901.woff`), glyph `icon_pin_filled`. It's drawn at
the 13 px label size in `#e8912d`, right-aligned in the gutter with 8 px
before the text, and it leans with the head up-left and the needle
down-right.

# Channels audit: Clack vs Slack, side by side

How this was done (2026-11-08, overnight):

- **Slack:** the real web client, signed in as the test account (meclod) in the agent-browser `whatever` profile, in its own tab at 1440×900, driven over CDP by target id (the shared profile switches its active tab underneath you). Strictly read-only: menus and dialogs were opened and closed, nothing was chosen. Only #clack (C0CFT52NEGM), #bot-tests (C08E083LZ9A, not a member) and the channel browser were viewed. Geometry and colours come from `getBoundingClientRect` and `getComputedStyle`; API method names come from the tab's own resource timing.
- **Clack:** the CI build of `ui`@6d4e8de (`clack-linux-amd64`), on a private Xvfb, signed in as the same account with realtime off.
- **Source:** Slack's web-client chunks from the public CDN (boot-shared 4f8632cd9602d1fa, boot-render 777e711847af270a, gantry-v2-shared-boot-async 080032942d9a3cd0, client-boot bac7c893b49156a7, modern.vendor 99445d7c5cf41877). The module names cited below are from those builds.
- **Writes** (join, leave, temp join, star, sections, topic) were only exercised against `internal/fakeslack`, in `internal/ui/channels_audit_test.go` and the existing preview tests. The `channels-audit` workflow renders those states (artifact `channels-audit-shots`).
- **Screenshots:** none committed. Real-Slack screenshots of anything except #clack, the browser's public list and UI chrome were deleted right after comparing. No message text was saved.

Status: **matched** (no change needed), **fixed** (on branch `channels-e2e`), **open** (still different; reason given).

## Channel header

| Feature | Slack | Clack before | Status |
|---|---|---|---|
| Star button | `ViewHeaderEntitySectionSelector`: a section picker. Icon is the section's emoji, else `section` (custom section), `star-filled` (starred) or `star`. A click opens `ChannelSectionMoveToMenu` (280px, under the star): "Move to…", Starred plus the custom sections as radios, a divider, then "Create your first section" (no custom sections) or "Create new section" and "Remove from section". | toggle; backend toast "Starring from Clack isn't supported yet" | **fixed**: `channel_move.go`; choosing a section is Slack's `performBulkChannelMove` (in there, out of where it was) |
| Star icon in a custom section without an emoji | `section` glyph (the one on custom section headings) | n/a | **fixed**: the sidebar's custom-section heading glyph |
| Move to… position | 8px under the star, left edge 66px left of it | 10px higher | **fixed** (live check) |
| Radio rows in Move to… and the bell menu | ✓ in a left gutter, checked row link blue, regular labels, descriptions wrap to 2 lines | ✓ at the right, bold labels, one line | **fixed**: `popups.MenuItem.Radio` |
| Header for a channel you aren't in | no star (title at +18), no bell; facepile, Huddle, search and ⋮ stay | star and bell shown | **fixed** |
| "+" add-tab button for a channel you aren't in | none | shown | **fixed** |
| Tab labels | by tab id (boot-shared ChannelTabLabel): `bookmarks` → "Pins" (pin icon), `files` → "Files & links", a folder without a label → "Bookmarks" | "Bookmarks", "Files" | **fixed** (core `builtinTabIDTitles`, pin icon) |
| Canvas tab title with `:emoji:` | rendered as emoji ("clack testing 🐱") | raw `:cat-slackbot:` | **fixed**: `tabLabelSpans` |
| Unread dot on a canvas tab, channel you aren't in | none | blue dot | **fixed** |
| ⋮ menu (member) | Channel details ›, Copy ›, Star channel, Move channel / Add a workflow, Edit settings / Open in split view / Leave channel | same rows | **matched** (rows) |
| ⋮ "Move channel" | opens the Move to… choice; with no custom section, straight to "Create a section" | toast "isn't supported yet" | **fixed** |
| ⋮ "Edit settings" | opens channel details on Settings | toast | **fixed** (`OpenDetailsTab`) |
| ⋮ "Add a workflow" | Workflow Builder | toast naming "yoloslack" | **open**: no workflows in Clack; toast now says Clack |
| ⋮ menu (not a member) | Channel details, Copy / Add a workflow, Edit settings / Open in split view | also Star, Move, Leave | **fixed** |
| Topic | inline after the title (view-header chunk `W58+` ViewHeaderTopic / ModelInfoTopic): the topic is a button that opens `openChannelTopicDialog`, and an "Edit" link (aria-label "Edit channel topic", "Edit topic" in DMs; the 22×18 box is the word "Edit" at 13px, link blue, 8px after the topic, shown while the pointer is over the topic container, underlined on its own hover). Neither shows when `canUserSetTopicForChannelWithReasonById` (boot-shared `xjwB`) says no. A topic too wide for the header is capped at min(70%, 540px) so Edit stays beside it | text only | **fixed**: `layoutHeaderTopic`; the dialog is Slack's `ChannelTopicDialog` (client-channel-details-view `ulnE`: "Edit topic", Save, `useChannelTopicInput`'s hint, `getChannelTopicError` on a refusal), `popups.NewTopicDialog`. Clack's permission check covers archived, not a member, can't post and guests, not Slack's role grants |
| Bell | `ChannelNotificationPrefMenu` (300px, right-aligned): "Notify you about…", All new posts / Just mentions / Mute and hide (with descriptions; "Mute channel" without `improve_mute_pref`), More options › Temporarily mute… Until tomorrow / 1 hour, Edit default preferences | a two-row stub at a fixed spot | **fixed**: `popups.NewChannelNotifyMenu`; temporary mute is `temp_mute_expiration_ts` (`core.TempMute`) |
| More options › Advanced settings | `MoreOptionsSubmenu` (boot-shared `laNq`) ends with "Advanced settings" (`channel_ctx_menu_advanced_options`; hidden in group DMs without `mpdm_notifications_advanced_option`). It opens `ChannelNotificationsPrefsModal` (boot-shared `RemI`): 600px, a 40px icon tile, "Advanced settings", "Notifications set to …"; Desktop notifications, Mobile notifications, Show/Hide advanced mobile overrides (All new posts / Just mentions, "Same as desktop"), Badge every new message (All new posts only), Follow every thread, Get broadcast messages (Just mentions only); the warning when All new posts has both pushes off; the error alert after a refused write. Each switch is one `users.prefs.setNotifications` {name, value, channel_id, global: false, sync: false} (`AvPY` → `nfg+` setNotificationsChannelOverride) | missing | **fixed**: `popups.NewChannelNotifyModal`, `core.SetChannelNotifyPref`; writes checked against fakeslack (`TestChannelNotifyAdvancedSettings`). Not drawn: the speedbump for browser/macOS-blocked notifications, which needs the OS notification permission |
| Bell icons | `n9iP` Km / Nl: "everything" is `notifications-all-new-posts` (the ringing bell), mentions `notifications`, muted or nothing `notifications-off`; the header's `NotificationsControl` (boot-shared `vpVS`, source channel_header) and the menu rows use them. A 1:1 DM's bell is a plain mute toggle | plain bell; a DM's bell opened a two-row stub | **fixed**: `popups.NotifyIcon`, `headerBellIcon`; a DM's bell toggles mute; group DMs get the menu |
| Facepile, Huddle, search | as measured in docs/FIDELITY.md | | **matched** |

## Not-a-member preview and temporary join

| Feature | Slack | Clack before | Status |
|---|---|---|---|
| Join pane | `# name`, Details, Join Channel split button | same | **matched** |
| Join caret | 40×36, chevron-down | 32 wide, filled caret | **fixed** |
| "Join…" menu | 300px, right edge on the caret's right edge, directly above it: Join… / Just for today / For 48 hours / For 1 week | 220px | **fixed** |
| Preview row in the sidebar | the previewed channel shows in Channels while selected, then disappears | same | **matched** |
| Viewing the preview | read-only; no join (checked with `conversations.info` `is_member` false afterwards for #bot-tests and #test) | same | **matched** |

## Channel details

| Feature | Slack | Clack before | Status |
|---|---|---|---|
| Header: name, star ▾, notifications button ("Just mentions"), Start a Call ▾ | | same | **matched** (caret glyphs are filled triangles in Clack: icons) |
| Notifications dropdown rows | All new posts, Just mentions, Mute and hide | All new posts, Just mentions, Off, Mute channel | **fixed** |
| Field row height | 77px (16 + 22 + 22 + 17) | 71–74px (Gio measures a 15/22 line at ~19px) | **fixed**: `cdLine22` |
| Edit topic / Edit description | stacked 520px modal: title 22/30 Black, input with focus ring, hint ("Let people know what **#clack** is focused on right now (ex. a project milestone). Topics are always visible in the header." / "Let people know what this channel is for."), Cancel / Save 80×36, Enter saves, max 250; a refused topic shows `getChannelTopicError`'s text in place of the hint and stays open | inline editor with raw `<https://…>` markup, closes before Slack answers, toast on error | **fixed** |
| Tab strip | About, Members 4, Agents & apps 7, Automations, Tabs, Settings | no Agents & apps count | **fixed** (users/counts `apps`) |
| About: Topic, Description with Edit | | same | **matched** |
| About: "Workspaces with access to this channel" | `ManageOrgAccess` (on Enterprise Grid, channel not shared outside the org): team icon and name, "Learn more", help icon; greyed (`#1d1c1d@0.7`) when you can't change it | missing | **fixed** (`internal_team_ids`, `App.Team`) |
| About: Managed by | name plus a blue ⓘ | no ⓘ | **fixed** |
| About: Created by, Leave channel, Send emails, Channel ID | | same | **matched** |
| Members list | people only (`MEMBER_LIST` filter `everyone AND NOT bots AND NOT apps`), you first, then by display name | everyone, bots included; present people first | **fixed** |
| Member names | `MediumMemberEntity`: bold display name, presence, then the real name | real name first ("karim karim") | **fixed** (`DisplaySet`) |
| "Channel Manager" tag | pill: 13/18, padding 4×8, radius 16, `#1d1c1d@0.06` | missing | **fixed** |
| Presence dots in Members | ● / ○ | none with realtime off | **open**: presence only arrives over the socket |
| Agents & apps tab | card "Agents and apps" + "Add agents and apps"; a row per bot (edge `users/list` `filter:"apps"`, `users_by_realname`), AGENT/APP badge (`agent_status`), tagline (`apps.profile.get`) | static "Bring the tools…" card | **fixed** |
| Agents & apps: Agentforce promo card | shown | none | **open**: Salesforce ad, not data |
| Automations tab | "Let workflows take tasks off your plate", Go to Your Workflows, Learn More, illustration | "Workflows / Add a Workflow" | **open** |
| Tabs tab | "Manage tabs" card: Messages, the canvas, Files & links, Pins, Workflows (Hidden), New Tab | per-type list plus an empty Bookmarks card | **fixed** (New Tab itself: open, toasts) |
| Settings: Channel name (Edit), Posting permissions | hidden from a plain member | shown to everyone | **fixed** (creator, manager or admin) |
| Settings: AI use, Guest access, Member/Guest permissions, Channel member suggestions, Copy member names/emails | shown | missing | **open**: needs `conversations.permissions.*` reads |
| Settings: Huddles, tabs permission | | same | **matched** |

## Channel browser (Directories → Channels)

| Feature | Slack | Clack before | Status |
|---|---|---|---|
| Search, Create Channel, All channels, Any channel type, Workspaces, Organizations, Most recommended | | same | **matched** (layout) |
| Purpose in the rows | rich text: emoji, `#channel` names, links | plain text: raw `:emoji:` and `#C0EA9S0A0` ids | **fixed** (`spanLine`, names resolved by core) |
| "Reset" | 13px link after the filters, always shown | missing | **fixed** |
| Row height | 75px between hairlines | 71–73px | **fixed** |
| "Browse channels" (Add channels, ⊕) | opens the Channels tab | opened People: `directories?tab=channels` was ignored | **fixed** |
| Workspaces / Organizations filters | menus | toast "isn't supported yet" | **open** |
| Filter carets | chevrons | filled triangles | **open** (icons) |
| Tab icons (Channels, External, Invitations) | `#+person`, building, envelope | `#`, globe, send | **open** (icons) |

## Leaving and rejoining

| Feature | Slack | Clack before | Status |
|---|---|---|---|
| Leave a public channel (⋮, sidebar, details) | at once, no confirmation (`maybeLeaveChannel`: not private → leave) | "Leave #name? You can rejoin…" confirmation | **fixed** |
| Leave a private channel | `LeaveChannel`: "Leave <name>", "When you leave a private channel, you’ll no longer be able to see any of its messages. To rejoin this channel later, you’ll need to be invited.", Cancel / Leave Channel | own wording | **fixed** |
| Leave #general | `LeaveChannelError` CANT_LEAVE_GENERAL: "Membership is required in #general", OK | confirmation, then a server error | **fixed** |
| Last member leaving | the archive dialog (`confirm_leave_and_archive`) | plain leave | **open** |
| Rejoin | the channel is a preview again with the join pane; Join brings the composer back | same | **matched** (fake: `TestChannelLeaveRejoin`) |
| Browser row confirmation for a private channel | the `LeaveChannel` modal | inline "Leave #x? …" | **open** |

## Sidebar (seen in passing)

| Feature | Slack | Clack | Status |
|---|---|---|---|
| A channel whose name is only an emoji/symbol glyph | shows the glyph (tofu on Linux Chrome) | empty name after "#" | **open**: font fallback, not channel code |
| Draft pencil on a channel row | pencil before the badge | none | **open** (sidebar) |

## Code channels (for the code-channels agent)

#test (C0BUVSNLEA3) is a Slack Code channel (`properties.code_channel`, `agent_session`, `record_channel`). Viewed read-only on 2026-11-08 as a non-member:

- Header: a `</>` tile where the star would be, the name, **no topic, no tab bar, no Huddle, no search**: only the facepile (9) and ⋮. Clack draws the normal channel header (now without star/bell/+ since it's a preview, but still Huddle, search and a Messages tab).
- Sidebar: a "Code channels" section above Starred with the channel as "— test  Hack Club" (a dash glyph, then the workspace name in grey) while it's previewed. Clack shows no such section.
- The join pane is the ordinary one (Details, Join Channel ▾).
- Slack's tab label switch has cases Clack doesn't know: a PR-link tab (`prUrl`), "Resources" instead of "Files & links" (record channels), "Summary", "Record details", "Skills". Channel details render `RecordChannelDetails` first on About for record channels and hide "Created by".

## Real-Slack side effects during the audit (meclod)

- My first DOM click looking for the browser's Channels tab hit the sidebar's Channels section heading and collapsed it; I expanded it again at once.
- A click aimed at the browser's tab bar landed on #clack's "clack testing" canvas tab while the page was still on #clack, which opened the canvas (its unread dot cleared).
- Nothing else was chosen, typed, joined, starred or saved; every menu and dialog was closed with Cancel/Escape.

## Not compared

- The Automations tab beyond its copy ("Let workflows take tasks off your plate" card: **open**).
- Settings' AI use / Guest access / Member and Guest permissions / member suggestions / Copy member names (**open**, need the `conversations.permissions.*` reads).

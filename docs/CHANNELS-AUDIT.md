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
| Bell | `ChannelNotificationPrefMenu` (300px, right-aligned): "Notify you about…", All new posts / Just mentions / Mute and hide (with descriptions; "Mute channel" without `improve_mute_pref`), More options › Temporarily mute… Until tomorrow / 1 hour, Edit default preferences. The setting in effect carries a "Customized" tag (`informative`) when its advanced settings depart from it (n9iP `Xq`: follow every thread, @channel suppressed under Just mentions, badge every message under All new posts, or a mobile level of its own), never while muted | a two-row stub at a fixed spot | **fixed**: `popups.NewChannelNotifyMenu`; temporary mute is `temp_mute_expiration_ts` (`core.TempMute`); the Customized tag (popups `notifyCustomized`, the menu rows' tag slot; branch exp-menus) |
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
| Presence dots in Members | ● / ○. Slack's `PRESENCE-MANAGER` (client-boot `8pMo`) ref-counts every rendered presence and sends one `presence_sub` with the whole set (100 ms debounce); the gateway answers with `presence_change`. The 2026-11 client never sends `presence_query`. | none with realtime off; with it on, the rows kept the page's snapshot and never redrew | **fixed** (branch `ch-details`): rows read `core.UserPresence` every frame and the dialog redraws on `presence_change` for loaded members (`Members` already subscribes). With realtime off there are still no dots, since Slack has no other source. |
| Agents & apps tab | card "Agents and apps" + "Add agents and apps"; a row per bot (edge `users/list` `filter:"apps"`, `users_by_realname`), AGENT/APP badge (`agent_status`), tagline (`apps.profile.get`) | static "Bring the tools…" card | **fixed** |
| Agents & apps: Agentforce promo card | shown | none | **open**: Salesforce ad, not data |
| Agents & apps: "Add agents and apps" / "Add an App" | `openAddPeopleModal` with `entityScope` AgentsApps (the channel invite modal for apps) or the apps browser | toast "Not supported yet" | **open**: Clack has neither modal |
| DM / group DM: Add people | `openAddPeopleModal` → `openMpdmAddPeopleModal` (boot-shared `TWss`, `MpdmAddPeopleModal`), from About's "Add people to this conversation" in a 1:1 DM and the Members tab's Add people in a group DM. Step 1 picks people (up to 9 including you, "Already in this conversation" greyed, "ex. Matt or @mbrewer"). Step 2 is "Include conversation history?": Don’t include / From today (the default unless `mpdm_default_exclude_history_clients_webapp`) / From the beginning / Custom. "From the beginning" asks for confirmation, and "Once included, it can’t be undone." Done calls `createChildChannel` → `conversations.createChild {channel, user_ids, migrate_history_start_ts}`, then opens the new group DM | toast "Adding people to a direct message isn’t supported yet" (Members) or an inline search (About) | **fixed**. Custom (a date picker plus a preview of the included messages) is **open** |
| Group DM About | conversation name field, Topic, Send emails, Channel ID (no Add people row) | had an Add people row | **fixed** (the conversation name field is **open**) |
| 1:1 header: VIP, Hide | VIP: `users.priority.add/remove` and the "Added/Removed **x** to/from VIPs" toast (`77my`). Hide: `hideOrUnhideUserById` (`N0zR`). "Hide x?" (three icon lines, Cancel / Hide This Person) leads to `hideUserById`, which closes the DM and saves `hidden_users`; Unhide happens at once | VIP toasted "Not supported yet"; Hide closed the DM with no confirmation and no `hidden_users` | **fixed** (Slack's `user-hide` glyph is missing from Clack's icon set, so VIP-on and Hide show no leading icon) |
| Header: Start a Call ▾ | `WithCallMenu` / `CallMenu` (boot-shared): the team's call apps, phone, and the huddle | toast "Not supported yet" | **open**: needs the call-apps list (`WithCallMenu`'s module isn't in the chunks fetched) |
| About: Send emails to this channel | `SendEmailsToChannelButton` → the channel email modal (`fRXX`, `conversations.emailaddresses.*`) | toast "Not supported yet" | **open** |
| Automations tab | "Let workflows take tasks off your plate", Go to Your Workflows, Learn More, illustration | "Workflows / Add a Workflow" | **open** |
| Tabs tab | "Manage tabs" card: Messages, the canvas, Files & links, Pins, Workflows (Hidden), New Tab | per-type list plus an empty Bookmarks card | **fixed** |
| Tabs: New Tab | `AddTabMenuTrigger` (manage-channel-tabs chunk) renders the tab bar's `AddTabMenu` (boot-shared `/wV5`, `isManageTabsMenu`) | toast "Not supported yet" | **fixed**: opens the same add-tab menu as the header "+" (`App.AddTabMenu`); that menu's items belong to the header code |
| Settings tab itself | `AdminChannelModal` (shared admin-channel-modal chunk, `oWlM`); the sections come from `jf5b` in this order: rename, permissions, tab-permissions, channel-permissions, add-member-suggestions, export, copy-members, managing-this-channel. The tab shows only when there is a section (`isSettingsVisible`), never to guests, so a 1:1 DM has none | DMs: invented "Notifications" and "Convert to a private channel" rows | **fixed**: `details_more.go` draws those sections in that order; a DM has no Settings tab |
| Settings: Channel name | `rename` section: `canUserRenameChannelWithReason` (`T3ou`). On Grid that means `permissions.info` `RENAME_CHANNEL` | creator, manager or admin (a guess) | **fixed**: `permissions.info {user_id, permissions, channel_id}` |
| Settings: Posting permissions | `ViewPostingPermissions` with its subtitle, editable in `PostingPermissionsModal`; hidden from a plain member | a row whose Edit toasted | **open**: the row is gone until Clack has the modal and the subtitle (posting restrictions) |
| Settings: AI use | `AiExclusionsField`: needs experiment `ai_channel_exclusions` and the feature access `slack_ai_exclude_from_slack_ai_property__access`. Reads `properties.exclude_from_slack_ai`: a green check-filled "…can be used…" with **Restrict**, or a red close-filled "…cannot…" with **Allow**. Without `EDIT_CHANNEL_AI_EXCLUSIONS` (or when another org hosts the channel) the row is read-only: text at 70%, a help icon with "You don’t have permission to change the AI use setting.". Edit opens `AiExclusionsModal`, which calls `conversations.setProperties {exclude_from_slack_ai}` | missing | **fixed** (the feature-access flag isn't readable by Clack, so the row follows the experiment alone) |
| Settings: Guest access | `GuestRestrictionsField`: needs experiment `enable_new_channel_settings` and `conversations.canSetProperties [restrict_guests, …]`. Settable: Restrict / Allow. Not settable: hidden on `disallowed_action`, otherwise read-only (`scg_present` → "You cannot change this setting when the channel contains single-channel guests."). `GuestRestrictionsModal` → `setProperties {restrict_guests}` | missing | **fixed** |
| Settings: Huddles | EditableField: "Members can start and join huddles in this channel." plus Learn more, then `HuddlesLinkCTAs` (Start Huddle, Copy Huddle Link, each closing the dialog). Non-admins see it read-only ("You don’t have permission to change this setting for this channel") | text and two buttons that toasted | **fixed**: Start Huddle is `startHuddle`, Copy Huddle Link is `copyHuddleLinkByChannelId` (`rooms.getLink`). The admin Edit (`HuddlesPermissionModal`) is **open**, so admins get the row without Edit |
| Settings: tabs permission | `ManageChannelTabsPermissions` (`z92f`, experiment `move_channel_tabs_settings`): Everyone / Channel managers from `properties.who_can_manage_tabs`, disabled unless you may change it, `setProperties` on change | a select with "Everyone" only, always enabled | **fixed** (who may change it is approximated as channel manager or admin) |
| Settings: Member / Guest permissions | `ViewChannelPermissions` ×2 from `conversations.permissions.accountTypes.list` (`zTmD` HE): the allowed permissions with green checks, or "Members/Guests don’t have any extra permissions yet." Edit, for an admin or a member with `MANAGE_CHANNEL_PERMISSIONS`, opens `EditChannelPermissionsModal` (checkboxes, locked ones greyed, "Some permissions are locked…"), which saves the changes with `…accountTypes.set` and fetches again | missing | **fixed** |
| Settings: Channel member suggestions | `AddMemberSuggestionsField`: checkbox "Get suggestions to add people who aren’t in the channel when you @mention them.", the pref `non_member_mention_invite_opt_out` (`{channel: true}`) | missing | **fixed** |
| Settings: Copy member names / emails | `CopyMembers` (paid team): names joined with ", " (real names); emails only when every address is visible, else disabled with "You don’t have permission to see everyone’s email addresses…". "Copied ✓" shows for 2 s; past 1,000 members there's one disabled row | missing | **fixed** (Gio's clipboard is plain text, so Slack's HTML mention form is dropped) |
| Settings: Message history, Export, Archive / Delete / Convert (managing-this-channel), Slack Connect sections | shown only with admin or retention permissions | missing | **open**: hidden from a plain member; Clack draws none of them yet |

## Channel browser (Directories → Channels)

| Feature | Slack | Clack before | Status |
|---|---|---|---|
| Search, Create Channel, All channels, Any channel type, Workspaces, Organizations, Most recommended | | same | **matched** (layout) |
| Purpose in the rows | rich text: emoji, `#channel` names, links | plain text: raw `:emoji:` and `#C0EA9S0A0` ids | **fixed** (`spanLine`, names resolved by core) |
| "Reset" | 13px link after the filters, always shown | missing | **fixed** |
| Row height | 75px between hairlines | 71–73px | **fixed** |
| "Browse channels" (Add channels, ⊕) | opens the Channels tab | opened People: `directories?tab=channels` was ignored | **fixed** |
| Workspaces / Organizations filters | `SearchFilterWorkspace` (kGy0): a `SearchFilterSearchSelect` (1Vs3, 280px, search input "Ex. Acme Inc") with "All workspaces", the chosen one, then "Your workspaces" (the org's teams the viewer is in, 6/pc n1); the pill reads "In <team>" with its 16px icon; sends `search_only_team`. `SearchFilterOrgs` (xlY+): multi-select of the orgs `users.slackConnect.orgList` (connection_type member) returns under "Suggestions", "Clear all" after two; "With <org>" / "With N organizations"; sends `external_shared_team_ids` and `channel_type` external_shared(_archived/_private) (XjMX getSearchAPIParams) | toast "isn't supported yet" | **fixed** (`dir_filters.go`, core `browse_filters.go`); the Organizations search filters the connected orgs by name, where Slack runs its team searcher |
| Filter pill when a filter is set | `c-filter-pill--active`: content-hgl-1 fill, white text | unchanged look | **fixed** |
| Filter carets | `FilterDropdownIcon` (DDoZ): SvgIcon `caret-down` (a chevron) 16px, 4px after the text, in the text's colour, turned over while the menu is open; the sort select (`SearchFilterSingleSelect`) the same glyph | filled triangles | **fixed** |
| Tab icons | `UnifiedDirectoryTabs` (unified-directory-view tBgf): `user`, `channel-search`, `user-groups`, `buildings`, `email-invite`; the `-filled` one (`email-filled` for Invitations) on the selected tab | `user`, `#`, `people`, globe, send | **fixed** (`icons/directory.go`: Slack's outlines) |
| People: Title, Location, Workspaces | `SearchFilterTitle` / `SearchFilterLocation`: multi-selects of `search.modules.people`'s `filter_suggestions` (5 shown, locations by count, kept 4 minutes: ujfX), the typed text echoed as an option; "Title: X" / "Multiple titles"; send `profile_title` / `profile_location`. User Groups' Workspaces sends `usergroups.search` `search_only_team` | toast "isn't supported yet" | **fixed** |
| External and Invitations tabs | the Slack Connect directory (q2q9) and invitations explorer (ebxv) | "isn't available in yoloslack yet" empty states | **open**: needs the Slack Connect reads |

## Leaving and rejoining

| Feature | Slack | Clack before | Status |
|---|---|---|---|
| Leave a public channel (⋮, sidebar, details) | at once, no confirmation (`maybeLeaveChannel`: not private → leave) | "Leave #name? You can rejoin…" confirmation | **fixed** |
| Leave a private channel | `LeaveChannel`: "Leave <name>", "When you leave a private channel, you’ll no longer be able to see any of its messages. To rejoin this channel later, you’ll need to be invited.", Cancel / Leave Channel | own wording | **fixed** |
| Leave #general | `LeaveChannelError` CANT_LEAVE_GENERAL: "Membership is required in #general", OK | confirmation, then a server error | **fixed** |
| Last member leaving | `maybeLeaveChannel` (boot-shared `Wlp6`): a public channel is left at once. A private one (not archived, not Slack Connect) first reads fresh `users/counts`, and with `people` = 1 opens `BaseArchiveDialog` with `andLeave`: "Leave and archive #x", the archive bullets, "Yes, Leave & Archive the Channel" → `conversations.archive`. With errors it shows `renderError` ("Since you’re the last one here, you’ll need to invite an admin…") | plain leave | **fixed** (details' Leave channel) |
| Leave a private channel from details | `LeaveChannel` modal | an inline confirmation inside the card | **fixed**: a stacked `LeaveChannel` dialog |
| Rejoin | the channel is a preview again with the join pane; Join brings the composer back | same | **matched** (fake: `TestChannelLeaveRejoin`) |
| Browser row confirmation for a private channel | `onClickLeave` is `maybeLeaveChannel` (Wlp6 B): the `LeaveChannel` modal | inline "Leave #x? …" | **fixed** (`popups.MaybeLeaveChannel`) |

## Sidebar (seen in passing)

| Feature | Slack | Clack | Status |
|---|---|---|---|
| A channel whose name is only an emoji/symbol glyph | shows the glyph (tofu on Linux Chrome): a name like "ㅤ" (U+3164 HANGUL FILLER, a letter, so allowed) is default-ignorable, but HarfBuzz shapes the four Hangul fillers as spacing glyphs | empty name after "#": `theme.CleanText` dropped the fillers | **fixed**: kept; the system-font fallback draws a font's blank glyph, else Lato's .notdef box |
| Draft pencil on a channel row | `BaseChannelSuffixDraftIcon` (boot-render PZNh): the icon font's pencil (U+E024) 14px in a 20px `suffix_generic` slot, before the badge, in the row's colour (.64 when muted); shown when the channel has a draft (Ex/u cF), isn't open and can be posted in, with `has_drafted_message`; thread and DM-by-user drafts count only under `draft_management_in_composer` / `sidebar_draft_indicator_menu_and_thread_inclusion` | 13px `edit` SVG, only without a badge, also on the open channel | **fixed**; the experiments' hover menu ("Jump to draft…") is **open** |

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
- Settings' AI use / Guest access / Member and Guest permissions / member suggestions / Copy member names: built from Slack's source on 2026-11-10 (branch `ch-details`) and checked against `internal/fakeslack` only. Never exercised on the real account (they write settings and prefs).

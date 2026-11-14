# Which conversations the sidebar shows, and in what order

Clack ports these rules from Slack's own web client, not from guesses. The implementation lives in `internal/core/sidebar_rules.go`, `dmsidebar.go` and `app.go` (`sections`, `orderSections`), and `sidebar_rules_test.go` / `sidebar_dms_test.go` have one case per branch.

## Evidence

- **Source:** Slack web client build `a84662d2d135` (2026-11-08). The chunks are public files on `a.slack-edge.com/bv1-13-br/`; the chunk list comes from IzieStratt/slack-datamine-plus `build/chunks.txt`.
- **Tooling:** REA 6.8.0 `analyze-javascript-application` produced Evidence `ev_48e09d88348a45efeb130db2c553a50e226b0d6c946e2de8f432616266f028f9` (boot-shared chunk). The module ids below are webpack ids.
- **Live checks** used the test account and read-only calls only.
  - `sidebar.dms` and `client.userBoot` both report `is_open`. Their open sets agree: 5 of 8 IMs are open.
  - `users.conversations` returns all 8 and carries no `is_open`.
  - `experiments.getByUser` returns the experiment groups the rules depend on.

## Rules

- **Candidates** (`gantry-v2-async-client-boot-render` lJBI = `application` XkwK)
  - Channels must be ones the user is a member of, and not archived.
  - DMs and group DMs must be open (`is_open`, QzJY.isOpen), unread (s2nj → 0jMB.OM unread count), have a draft (`hasDraftByChannelId`) or have a huddle on (`huddleChannelIds`). Build 2c0d02ca130f, `core.dmCandidate`:
    - The draft set is Slack's `drafts.channelIds` (client-boot `YFjR`), read from the draft cache that `DraftFor` uses (`drafts.go` `sidebarDrafts`). Slack keys an attached draft (`is_from_composer` false) by its first destination (`HOmS` getDraftClientIdFromServerDraft: `channel` or `channel-thread_ts`) and lists the channel only for a draft that isn't a reply. A composer draft is keyed by its `client_msg_id`, and a draft addressed to user ids alone has no key, so neither keeps a DM. A change to the set emits `sidebar_changed`.
    - The huddle set is `b0mi` jg (through `YovN`): every active channel whose call has someone in it, yours included. The huddle UI keeps those rooms (`sh_room_*`, edge `huddles/list`). Core reads `huddleui.HuddleChannels` through `SetHuddleChannels` each time it builds the sections, and the sidebar asks for new sections when the set changes (`huddle_wire.go` `sbHuddleSync`). Slack also gates the set on `allow_huddles` (`zq6z` pt); Clack's huddle UI has no such gate, so the rule reads what the rows' huddle suffix draws.
  - Never shown: archived DMs, DMs with deleted users, DMs with `USEARCH`/`UCHANNELEXPERT`, and Slackbot's DM while Slackbot's tab or sidebar link shows (below).
  - Closed DMs drop out of every section, custom ones included.
- **Section of a row** (lJBI)
  - Starred conversations always go to Starred.
  - A conversation listed in one section goes there.
  - A conversation listed in several goes to the highest rank among the leading sections: stars 3 > standard 2 > user_group 1.
  - Anything else goes to the default Channels / Direct messages / Apps buckets.
  - The Apps section exists only when there are app DMs (`shouldSeparateApps`).
- **Section order** (modern.vendor `updatedOrderedChannelSectionList`)
  1. Start a chain at every section nothing points to through `next_channel_section_id`. If every section is pointed to, start at the first one.
  2. Put the longest chains first and keep each section's first position.
  3. Sections outside every chain get position 0.
- **Section filter** (boot-render 0Eub, boot-shared aQqV/ij26)
  - The `channel_sections` pref's `"sidebar"` value counts only under `sidebar_behavior=hide_read_channels_unless_starred` ("Custom by section").
  - Otherwise it comes from `sidebar_behavior`: `hide_inactive_channels`→`active`, `""`→`all`, `hide_read_channels`→`hid`, `show_mentions_only`→`mentions` (DMs: `hid`).
- **Row filter** (lJBI)
  - `active` hides rows whose `properties.is_dormant` is set (QzJY.isDormant; Slack's server sets the flag) when `filter_dormant_channels` is on.
  - `hid` hides read rows. `mentions` keeps only rows with a badge.
  - Under `improve_mute_pref`, muted rows go unless `show_muted_items_outside_sidebar_menus` is on, and a badge always keeps a row.
  - In an expanded section, a draft keeps any row, a dormant one under `active` or a muted one included. A huddle keeps a read row, but not a dormant one under `active` or a muted one that is hidden.
  - A collapsed section keeps its unread rows, muted ones included.
- **Sort** (boot-shared O/sM + s0VO, lJBI)
  - The per-section `"sort"` pref wins, else `channel_sort` (`default` = alphabetical with `Intl.Collator("en-US",{numeric:true})`, `recent`, `priority`, `recent_dms`).
  - Under `dm_recency_sort_rollout` (`without_edu`/`with_edu`), DM sections default to most recent first.
  - Rows go in groups:
    - `boost_mentions` puts rows with a badge first.
    - Slackbot comes next.
    - In the DM section the self-DM goes last (`youNewTeam`).
  - Muted rows come after the rest.
  - A group DM sorts by the other members' names joined with `,`.

- **The open conversation** (lJBI `currentlySelectedId`, `internal/core/preview.go`)
  - It is always a candidate, even when the viewer isn't a member (a previewed channel) or it is archived, so it gets a row while it's open. It lands in its section or the default bucket.
  - Its row is never filtered out, in a collapsed section too.
  - The UI reports it with `core.SetSelected` on every route change.
- **Temporary channels** (`temp_join_channel`; client-boot `+7BQ`, `26kz` temporaryChannelStore)
  - A "Join…" membership (conversations.join `expiration_ts`, or `channel_joined.temp_membership_expiration_ts`) goes to the Temporary channels section, after Starred, or to the default bucket when there is no such section.
  - The section shows only while the viewer has a temporary channel.

## The DMs tab (karim-bugs-2-chrome, 2026-11-10)

- **Fetch** (gantry dms-list-view `eLDn` DmsListAll): `client.dms {count: 250, include_closed, include_channel, exclude_bots: !agents_in_dm_sidebar}`.
- **Rows** (`uV3r` useChannelIds, `DUy0` isBotDm, `upCu` useSortedChannels), `core.DMs` / `dmsTabShows`:
  - An IM goes when its member is deleted, when it is frozen or a pending Slack Connect IM, when the member is `USLACKSECURITY`, `USEARCH` or `UCHANNELEXPERT`, and when it is a bot DM: Slackbot's IM, `USLACK`, or a member with `is_bot` / `is_slackbot` (`is_app_user` alone isn't a bot there), unless the bot is an agent shown with the DMs (`uXg5` isAgentInDmSidebar, by member).
  - A DM without a latest message goes, except the self-DM and while onboarding isn't complete (shouldShowAllDms).
  - Newest first.
- The sidebar's Direct messages already sends app DMs to Apps (`uSoP` getDefaultSectionType with `qSRE` isAppOrBot).
- **Slackbot's DM** (`OpEF` AZ shouldHideSlackbotDmFromSidebar, `core.slackbotDMHidden`):
  - Slack drops the DM while Slackbot big mode is on (`w7YO` Yc, the rail's Slackbot tab) or the sidebar shows the Slackbot link (`OpEF` gW shouldShowSlackbotLink: `slackbot_ia_prototype`, Slack AI's Slackbot with its sidebar education, onboarding, sampling or joiner onboarding, unless `enable_slackbot_view` is false).
  - lJBI then leaves it out of the candidates, and out of every default section even while it's the open conversation.
  - The tab and the link both open Slack AI's Slackbot view, which Clack doesn't have (the rail's Slackbot tab is off for the same reason). Clack shows neither, so the DM stays in Apps. Hiding it would leave no way to reach Slackbot. The gate is wired, and tests turn it on. Once Clack has the view, big mode would follow the account's `slackbot_big_mode_ga` (treatment) and its Slack AI Slackbot permission (`ACCESS_SLACK_AI_SLACKBOT`).

## Canvases and lists (file channels)

Implementation: `internal/core/sidebar_docs.go`, `internal/ui/sidebar_docs.go`. Tests: `internal/core/sidebar_docs_test.go`, `internal/ui/sidebar_docs_test.go` (fakeslack `Size.FileChannels`). Build fa938a086569 (2026-11-12).

- **What they are** (read-only `conversations.info` on the real account, flags only): every standalone canvas and list has a hidden conversation for its comments, the file's id with `F` swapped for `C` (`I3+E`). It is `is_file`, `is_channel`, `is_private`, `is_thread_only`, `is_org_shared`, named after the file's title (spaces, capitals, `:emoji:`). Commenting makes the viewer a member, so `users.conversations` lists it among the private channels.
- **Candidates** (`lJBI`): a file channel (`EK7S` jT: `is_file`) stays only when `O2By` GH is on (`lists_pin_to_sidebar`, `programmable_sidebar` or `sidebar_ia6`) and a section lists it, it is starred or it is temporary; the open conversation always stays. Otherwise it isn't in the sidebar, its unread totals, or the snapshot the switcher, pickers and `#` autocomplete read (they also skip `is_file`, `I3y6` isValidViewableChannel).
- **Row** (`PZNh`, `m7We` getSidebarDocumentType): a list is a document row; a canvas is one only with `programmable_sidebar`. Other file channels draw as private channels. Icons (`UGOO`): canvas `canvas`; list `lists` with `programmable_sidebar`, else the font's `bullet-list`. A document row's name goes through FormatTextWithEmoji (`Un00`, `G6jU`), so `:shortcodes:` draw as emoji. The name (`W+Ke`) is the canvas's title (a channel canvas: "#channel canvas" / "Canvas for DM with …"); a list's is the channel name.
- **Click** (`r5i1` openFileChannelFromSidebar, GH on): a list opens the list view; a canvas opens the canvas view with `programmable_sidebar` or `sidebar_ia6`; anything else opens the conversation. The row is selected while its document is the main view (`x6Wj`).

## The Starred section when nothing is starred

Implementation: `core` `emptyStarredShown` and `starredBanner` (`sidebar_rules.go`), `internal/ui/sidebar_starred.go`. Tests: `internal/core/sidebar_keep_test.go`, `internal/ui/sidebar_starred_test.go` over fakeslack `Size.FewSections`, with screenshots from the `sidebar-experiments` workflow. Build 2c0d02ca130f.

- **Shown or not** (lJBI visibleChannelSections, while no candidate is starred and the section has no sidebar entities):
  - Hidden in simplified onboarding (`m01E` l; Clack never is) and for frontline members (`u+N9` W).
  - Otherwise shown, empty, unless it is client-made (Clack makes none), the viewer has VIPs (`U8c3` MY), or there are three custom sections or more.
- **The banner** (boot-shared `m1KW` l showEmptyStarredSectionBanner, `Section.StarredBanner`):
  - It needs no VIPs, fewer than three custom sections, and Starred as the first section the server sent (Code channels and VIP unreads, made on the client, are skipped).
  - No starred conversation may be one that would show: unarchived, the viewer in it, open if it's a DM or group DM, its DM member not deleted.
  - Slack's other inputs are always off here: the Activity's own star (`STARRED_ACTIVITY_KEY`) and Starred's sidebar entities.
- **The row** (boot-render `FmLP` EmptyStarredSectionRow, type and id `emptyStarredSectionBanner`):
  - It shows under an open Starred heading with no rows, outside organize mode and the text filter. It is 44px: a 4px spacer, then the 40px banner.
  - "Drag and drop important stuff here" is 13px regular in the sidebar's colour, 24px from the list's inset with 12px on the right. It wraps at most two lines of 13px × 1.46668 from the top, and the second line ellipsizes.
- **The "New" badge** (boot-render `MGQu`, `iNkd` SectionMenuNewInternal):
  - It needs the banner's condition and the megaphone's `generic_new_badge` listing `starred_section_new_badge`.
  - The Starred heading then carries EducationTag's micro "New" (`7ux/`, style education): 8px after the label, 1px lower, at most 70px. It is hidden when the sidebar is under 220px.
  - `0Eub` reads the section as collapsed. Clicking the heading toggles it open and ends the hold at once. 500ms later `recordAndClearNewBadge` sends `megaphone.setNotificationAsSeen` dismiss with the badge as its step.

## External connections

Implementation: `core` `sections` (app.go). Tests: `TestConnectSectionPref`, `TestConnectSectionShowsForAnyShared`, and `internal/ui` `TestSidebarConnectPlacement` over fakeslack `Size.ConnectSection`.

- **Where it is.** It is the user's `slack_connect` section from `users.channelSections.list`, wherever the linked list puts it. Slack makes no client-side one.
- **What goes there** (`uSoP`). With `should_show_connect_section` on, an externally shared conversation (`is_ext_shared`) that no section lists goes to External connections, unless it is starred, temporary, an agent's channel (Code channels) or a Salesforce record's. That check comes before agents, apps, Direct messages and Channels.
- **Shown or not** (lJBI showConnectSection && hasSharedChannels). It shows while any candidate is externally shared, even when custom sections or Starred hold all of them and it has no rows.
- **Name** (`p4Ef` getChannelSectionNameIfStatic). It is always "External connections", whatever the server calls it.

## Not ported

- Programmable-sidebar experiments (`programmable_sidebar*`), which are off for the test account.
- Starred's banner as a drop target, with its drag looks. Clack's sidebar has no drag and drop.
- The banner's tooltip, which shows only on a cut label; the label fits two lines at every sidebar width.
- The hidden-row lists for the heading menus are worked out after the filter. Slack fills them inside it, so a row that is both muted and dormant goes only to Inactive channels there, and a muted row hidden for being read also goes to Muted conversations.

## Rail (the left tab column)

Implementation: `internal/core/rail.go` (tabs, order, More), `internal/core/team_icon.go` (workspace button), `internal/ui/chrome.go` / `host.go` (drawing, More menu). Tests: `internal/core/rail_test.go`. Same build and chunks as above; module ids are webpack ids.

- **Tab ids and default order** (client-boot `pl6F`): `slackbot, today, home, dms, activity, activity-inbox, activity-inbox-sidebar-prototype, search, sales-home, unified-files, later, agents, platform, slack-kit-playground`. More is `browse`, and "Agents & tools" is `platform`.
- **User order** (boot-shared `Ozeq`, pref `tab_rail_customization_order`): keep the valid ids once each, then splice every default tab the pref lacks back in at its default index.
- **Which tabs the user has** (client-boot `dk8L` get_tab_visibility):
  - Home always.
  - Activity is `activity-inbox` under `activity_inbox` (`activity-inbox-sidebar-prototype` with `activity_with_sidebar`). It is no tab when `unified_global_triage` or `activity_in_home_prototype` is on.
  - DMs, Files and Agents & tools always.
  - Later only with `has_starred_item` or overdue to-dos (OpEF shouldShowLaterLink).
  - Agentforce, Slackbot, Today, Sales and Search depend on features Clack doesn't have, so they are off.
- **Which of those sit in the rail** (client-boot `5ydT`, `Y2EP`, `cR2m`):
  - `tab_rail_customization` (tab → bool) is merged over the defaults.
  - Defaults: all on, except Files (off only during simplified onboarding without `has_uploaded`) and Agents & tools (on only under `agent_tasks`).
  - Home and Activity can't be turned off.
- **Rail vs More** (boot-shared `G1Mx`, boot-render `4v2a` useTabRailOrganizer):
  - `visibleTabs` = order ∩ available; `tabRailList` = the visible tabs kept in the rail.
  - Rail tabs are placed in order while they fit (68 px each, 40 px in `ia4_compact_mode`).
  - Every visible tab not placed goes under More. When More has anything, it is appended, and the last tab is dropped if less than one tab's height is left.
  - An active tab that is under More makes More the active button.
- **More menu** (boot-render `Zxh6` BrowsePeekMenu):
  - A "More" header.
  - One row per overflow tab: icon, label, subtitle ("Canvases, lists and attachments", "A place to manage your to-dos", "Manage agents, apps, and workflows", …), and a Ctrl Shift N hint when the tab is among the first 9 rail tabs.
  - Then "Customize navigation bar", which opens Preferences > Navigation.
- **Tab peeks** (web client build f132f0e78468; boot-render `Zxh6` renderWrappers, boot-shared `v5c+` TabPeekTrigger over `20Sq` PeekTrigger). Implementation: `internal/ui/rail_tab_peek.go` (the trigger), `rail_peek_agents.go`, `rail_peek_pages.go` and `internal/ui/views/*_peek.go` (the cards). Tests: `internal/ui/rail_tab_peek_test.go`, `rail_peek_pages_test.go`; shots from the `status-shots` workflow.
  - Trigger: 300 ms hover delay, right-bottom of the button (offsetX 6, offsetY −44), no scrim, no keyboard focus, a 30 ms throttled safe area, Escape (two moves on the button to reopen), a press on the button closes it and the click goes to the tab. No peek on the active tab. One peek at a time with More's and focus mode's.
  - Card (`9eDG` PeekView, `.p-peek_card--as-card`): ctr-pry, 360 px wide, 65vh tall (at most 100vh − 8 px; 100vh − 20 px at most 584 px tall), radius 12. Header 44 px, padding 0 8 0 16, 15 px black, a 1 px `sk_foreground_low` .13 rule; header buttons at its right. The card is a root Popover, so the lists use the client theme's tokens (858H / 7uI9), not the inverted sidebar's (jmNE).
  - Toggles in the header are `XrTy` UnreadsToggleButton under `.p-ia4_client`: 12 px bold label in theme-content-inv-sec (inv-pry when on), a 28 × 18 switch with a 12 px handle; on, theme-base-imp fill and theme-content-imp border and handle.
  - Every list is the tab's own view instance (the same filter state as the tab, as Slack's redux), with its own scroll position. A row closes the peek (maybeClosePeek) before it navigates, except Files, which opens the file first.
  - **Agents & tools** (`AutomationsPeekView`): see `rail_peek_agents.go`.
  - **DMs** (`DmsPeekView`, `eLDn` DmsListAll compact, isPeekView): "Direct messages" (ia4) and the DMs Unreads toggle (`Tbey`). No find query, no VIP sections, no active row, no preselected DM, no bottom padding. Rows have the `sk_foreground_low` .13 rule (`.p-peek_card--dms`) and a one-line preview. Skeleton rows until the first load; the Unreads empty state with "Show all DMs". A row selects the DM in the DMs tab.
  - **Activity** (`gOQ8` ActivityPeekTrigger): on the Activity tab (Activity, the inbox, the inbox-with-sidebar prototype), and on Home instead while `activity_in_home_prototype` is on (`6rqO` j) and Home isn't active. "Activity" with the Unreads toggle (`x96h`) over the Activity page's list (`5465`, `p-activity_ia4_page--peek`); row hover is base-pry under base-pry-hover. A row opens the item in Activity.
  - **Later** (`s1p9` LaterPeekView): the tab's name ("To-dos" or "Later") and "{count} in progress" (13 px foreground_max_solid, 8 px after). LaterPeekContent reloads on mount, shows the medium spinner floating over the list while loading, and the saved_for_later empty state when empty. Rows are the page's (`.p-peek_card__later_list`: square, no timestamps, hover actions at 12 / 12). A row shows the to-do in the To-dos tab (ViewTodo).
  - **Files** (`j33M` UnifiedFilesPeekView): "Files" (quip-files-browser) with the Starred toggle. Its own browser (`UNIFIED_FILES_PEEK`: default types, Recently viewed) or the starred files. 13 skeleton rows while loading. Rows use list styling: padding 12 16, an otl-ter line between rows, hover sk_foreground_min_solid. Empty: "📁 No files yet" / "Recently viewed canvases and lists will show up here.", or with Starred "⭐ No starred files yet" / "Click the star on any canvas or list to add it here for later". A row opens the file (handleFileContainerClick), then closes the peek.
  - **Activity badges** (`ActivityPeekViewTabs`): under the inbox (`5Ns/ Lh`) the header buttons are the badges in a `.margin_left_50` div (8 px after the title) and the Unreads toggle, spread by `.activityPeekView` (flex 1, space-between). With the inbox on the component always draws the badges: DMs, Mentions, Threads (`3x6A cR`), each only above 0, a 16 px glyph 4 px before a 13 px bold count in content-ter, 1 × 18 otl-sec separators (margin 0 8), tooltips "# unread DM(s)" / mention(s) / thread(s). Its All | DMs tab menu is the `!Lh && Vn` branch, which the peek never renders, so Clack has no All | DMs tabs.
  - **Row menus** (`views/dms_menu.go`, `views/later_menu.go`, `rail_peek_agents_menu.go`; Activity's is the page's `itemMenu`): DMs rows get the activity row's hover actions with `isDmTab` (Mark as read while unread, the to-do button, ⋮) and `getActivityItemMenuTemplate` with `isDmTab` from the ⋮ (300 px, bottom-right, offsetY 6) and a right-click. Activity rows keep the page's ⋮ and right-click menu; Open in home closes the peek (maybeClosePeek). Later rows' ⋮ is `SavedItemActionsMenu` (240 px; no split view or new window in the peek); saved items have no context menu. Files rows have only their ⋮ (FileContainer gets no onContextMenu). Agents & tools session rows have a hover ⋮ (`AgentSessionMenuButton`, 220 px): Pin, Mute, Rename session, Open in split view, Copy link, Close conversation (`agents.threads.pin/unpin/archive/rename`); workflow rows have none.
  - The card stays open while the pointer is in a menu it opened (PeekTrigger's `eu`: the menu is a portal in the card's React tree), closes once the pointer leaves the menu for the window, and keeps `eu` when the menu closes under the pointer (no mouseleave) until the pointer re-enters and leaves the card. Closing the peek closes its menus.
  - Not ported:
    - The Activity-in-Home triage view (`ActivityPeekTriageView`: Activity, Sent, Drafts and To-dos tabs). It needs `activity_in_home_prototype` (`6rqO j`), and the test account isn't assigned it (`internal/core/testdata/account_experiments.json` has no entry).
    - Files' "Created by agents" (`AgentCanvasPeekSection`). It shows only with `unviewed_agent_created_canvas_shelf` on (`7MA4 wM`), which the account isn't assigned either. It would need `canvases.agents.list` (fetched when the Files tab mounts), the `agent_canvas_created` RTM event, `canvases.agents.markRead` on open, the files' `canvas_agent_creator_id`, and the AgentCanvasShelf view that "See all" opens.
    - The DMs menu's agents_in_dm_list (agent DM eligibility) and every Open in new window.
- **Labels** (`mBDz`): Later is "To-dos" under `later_todos`. Platform is "Agents & tools" under `agents_landing_page`, else "Tools" (frontline users always get "Tools").
- **Home badge** (boot-render `InjD` useGetMentions; `rHEx` homeBadgeCount = unread − highlight counts of non-DM conversations):
  - With the activity inbox, Home shows a dot when channels have unreads, even while Home is open.
  - It shows a number (DM mentions) only when the DMs tab isn't in the rail and, with the inbox, DMs are opted out (`activity_dms_opt_out`).
  - More's badge sums the badges of the tabs under it (`yEMG`).
- **Workspace button** (boot-render `RmBj` TeamOrEnterpriseIcon, `qxQQ`, client-boot `qVVL`, `wqqh`):
  - On Grid the current team is the org. Slack shows the workspace's icon instead when the workspace team pref `use_workspace_icon_for_single_workspace_users` is on and the user is in exactly one workspace (`users.info` `enterprise_user.teams`).
  - The image is the smallest `image_N` ≥ 2 × 36 px, so `image_88`. When `icon.image_default` is set, it draws the team's initials (up to 2).
  - Live check, read-only: Hack Club has one workspace and the pref on, so the rail shows the workspace icon. `team.info` with `team=` returns either the org or the workspace.
- **Icon freshness** (boot-render `teamIconChanged`):
  - `team_icon_change` carries the new `icon` object for `team_id` (else `source_team`, else the current team). Slack stores it, and the new URL loads as a new image.
  - Hack Club's icon rotates about every 30 minutes (an app changes it). Clack applies the event live. With the socket off, the poll loop re-reads `team.info` every 10 minutes (`TeamIconRefresh`; Slack itself never polls).
  - The image cache is keyed by URL, and the rail keeps drawing the previous icon until the new URL has loaded.

## Section heading menus and the Create menu

Implementation: `internal/ui/section_menu.go` (templates, actions, toasts), `internal/ui/create_channel.go` (`openCreateMenu`), `internal/ui/popups/section_modals.go` (Rename, Create a section / Manage conversations, Delete, Start a Huddle), `internal/core/section_menu.go` (API calls). Fake: `internal/fakeslack/sections.go`. Tests: `internal/core/section_menu_test.go`, `internal/ui/section_menu_test.go`; screenshots from the `section-menus` workflow. Same build and chunks as above.

- **⋯ menu** (boot-render `o7` getSectionHeadingEllipsisMenuTemplate). It has five groups, joined by `assembleMenuGroups`, which drops empty groups and puts separators between the rest:
  1. Group 1:
     - The section name as a heading.
     - "Search in this section" (custom and Starred sections, `sidebar_section_search`).
     - "Mark all as read" (only when something in the section is unread).
     - "Start direct message" (DMs) or "Create a channel" (Channels, custom, Starred).
     - "Share" ▸ "Share directly" / "Copy section link" (custom sections with conversations).
     - "Browse channels" / "Recent direct messages" / "Browse apps".
  2. "Manage section":
     - "Rename" (custom sections).
     - "Create a new section". It needs the `ChannelSectionsCreate` permission; without it the row is "Create sidebar section" PAID.
     - "Manage conversations" (custom and Starred, `add_channels_channel_sections_slackathon`).
  3. "Section settings": "Filter" ▸ and "Sort" ▸, each showing its current value.
  4. "Notifications": "Mute all conversations" / "Unmute all conversations" (custom, user-group and temporary sections).
  5. "Delete section" (custom and Starred, danger), and "Leave all temporary channels".
- **Right-click** (`o2` getSectionHeadingContextMenuTemplate):
  - Group 1 again, except the browse row, which appears only for Channels, Apps, Slack Connect and Agents.
  - "Manage section" ▸ (300px) holds groups 2–5. Its Section settings starts with "Manage VIPs" in the DM section.
  - Then the rows the filter hides: "Unread conversations" (filter Mentions, or a collapsed section), "Read conversations", "Muted conversations", and "Inactive channels" ▸ ("No messages in at least 30 days", `filter_dormant_channels`).
- **Filter** (`getFilterPrefItems`):
  - With `filter_dormant_channels`: "Active only" (with `mute_channel_edu` its description is "New activity within the last 30 days"), "Unreads", "Mentions", "All".
  - Without it: "All", "Unreads only", "Mentions only".
  - DM sections never offer Mentions. Under `programmable_sidebar_dm_history`, DM sections offer "1 day", "Last 7 days", "Last 30 days" and "Unreads".
- **Sort** (`getSortPrefItems`):
  - Items: "A–Z" ("Alphabetically" without `filter_dormant_channels`), "Recency" ("By most recent", or "By most recent (Default)" for DMs under `dm_recency_sort_rollout`), "Priority".
  - The submenu row's value reads "A-Z", "Recency" or "Priority".
- **Persistence** (boot-shared `OHTN` setChannelSectionPrefByApi):
  - Filter and Sort write `{section: {sidebar|sort: value}}` into the `channel_sections` pref (users.prefs.set).
  - If a section's filter now differs from what `sidebar_behavior` gives it, and `sidebar_behavior` isn't "hide_read_channels_unless_starred" yet, Slack first pins every section's current filter into the pref, then switches `sidebar_behavior` to that value ("Custom by section").
  - Entries for sections that no longer exist are dropped on write.
- **API calls** (client-boot `2A1t`, boot-shared `hW84`, `HKiO`, `IDOy`, `nEGd`, `YvWE`):

  | Item | Call |
  |---|---|
  | Create a new section | `users.channelSections.create {name, emoji, next_channel_section_id}` |
  | Rename | `users.channelSections.set {channel_section_id, name, emoji}` |
  | Delete section | `users.channelSections.delete {channel_section_id}` (`section_not_found` counts as done) |
  | Manage conversations | `users.channelSections.channels.bulkUpdate {insert, remove}` (Starred stars or unstars instead) |
  | Mute all / Unmute all | `users.prefs.setNotifications {name: muted, value, channel_ids, global: false}` |
  | Mark all as read | `conversations.mark` per unread conversation |
  | Copy section link | `users.channelSections.createLink` → `{url}` |

- **Toasts**: "Marked N channels as read.", "Muted N channels.", "Deleted X.", "Renamed to X.", each with Undo; also "Section link copied". The plural forms are Slack's ICU strings ("Muted channel." for one).
- **Create menu** (boot-render `CreateMenu`, `nx8t` useCreateMenuActions): 360px, header "Create".
  - Rows: "Message", "Channel", "Huddle" ("Start a video or audio chat"), "Canvas" ("Create and share content", Ctrl+Shift+N), "List" ("Track and manage projects"), "Workflow" ("Automate everyday tasks"), "Notes" (`meeting_assistant`), then a separator and "Invite people". "Workspace" appears only on SPF non-work workspaces.
  - Huddle opens "Start a Huddle" (`HuddleInviteStartModal`) and then Clack's normal huddle start.
  - Canvas makes a standalone canvas.
  - Clack has no screens for List, Workflow, Notes or Invite people, so those rows show a toast instead of doing nothing.
- **Live check** (read-only `experiments.getByUser`, 2026-11-08): on for the test account: `filter_dormant_channels`, `sidebar_section_search`, `add_channels_channel_sections_slackathon`, `mute_channel_edu`, `improve_mute_pref`; `dm_recency_sort_rollout` is `without_edu`; `meeting_assistant` and the `programmable_sidebar*` experiments are unset.

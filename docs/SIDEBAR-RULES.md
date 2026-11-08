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
  - DMs and group DMs must be open (`is_open`, QzJY.isOpen) or unread (s2nj → 0jMB.OM unread count). Slack also keeps them for a draft or a huddle.
  - Never shown: archived DMs, DMs with deleted users, DMs with `USEARCH`/`UCHANNELEXPERT`.
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

## Not ported

- Drafts, huddles and the currently open conversation keeping a row visible (core has no such state).
- Slack Connect section placement.
- Hiding the Slackbot DM when the Slackbot link shows.
- Starred section empty states.
- Programmable-sidebar experiments (`programmable_sidebar*`), which are off for the test account.

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

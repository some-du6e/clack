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

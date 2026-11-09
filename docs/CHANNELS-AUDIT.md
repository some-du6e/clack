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
| Star icon in a custom section without an emoji | `section` glyph | n/a | **open**: Clack has no `section` icon yet (icons are icons-redraw's). Uses `star-filled` until one exists. |
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
| Topic | inline after the title; a pencil "Edit channel topic" (22×18) after it | text only | **open**: the hover pencil isn't drawn |
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
| Tab strip | About, Members 4, Agents & apps 7, Automations, Tabs, Settings | no Agents & apps count | **fixed** (users/counts `apps`) |
| About: Topic, Description with Edit | | same | **matched** |
| About: "Workspaces with access to this channel" | `ManageOrgAccess` (on Enterprise Grid, channel not shared outside the org): team icon and name, "Learn more", help icon; greyed (`#1d1c1d@0.7`) when you can't change it | missing | **fixed** (`internal_team_ids`, `App.Team`) |
| About: Managed by | name plus a blue ⓘ | no ⓘ | **fixed** |
| About: Created by, Leave channel, Send emails, Channel ID | | same | **matched** |
| Members list | people only (`MEMBER_LIST` filter `everyone AND NOT bots AND NOT apps`), you first | everyone, bots included; karim first | **fixed** |
| Member names | `MediumMemberEntity`: bold display name, presence, then the real name | real name first ("karim karim") | **fixed** (`DisplaySet`) |
| "Channel Manager" tag | pill: 13/18, padding 4×8, radius 16, `#1d1c1d@0.06` | missing | **fixed** |
| Presence dots in Members | ● / ○ | none with realtime off | **open**: presence only arrives over the socket |
| Agents & apps tab | card "Agents and apps" + "Add agents and apps"; a row per bot (edge `users/list` `filter:"apps"`, `users_by_realname`), AGENT/APP badge (`agent_status`), tagline (`apps.profile.get`) | static "Bring the tools…" card | **fixed** |
| Agents & apps: Agentforce promo card | shown | none | **open**: Salesforce ad, not data |
| Automations tab | "Let workflows take tasks off your plate", Go to Your Workflows, Learn More, illustration | "Workflows / Add a Workflow" | **open** |
| Tabs tab | "Manage tabs" card: Messages, the canvas, Files & links, Pins, Workflows (Hidden), New Tab | per-type list plus an empty Bookmarks card | **open** |
| Settings: Channel name (Edit), Posting permissions | hidden from a plain member | shown to everyone | **fixed** (creator, manager or admin) |
| Settings: AI use, Guest access, Member/Guest permissions, Channel member suggestions, Copy member names/emails | shown | missing | **open**: needs `conversations.permissions.*` reads |
| Settings: Huddles, tabs permission | | same | **matched** |

## Channel browser (Directories → Channels)

| Feature | Slack | Clack before | Status |
|---|---|---|---|
| Search, Create Channel, All channels, Any channel type, Workspaces, Organizations, Most recommended | | same | **matched** (layout) |
| Purpose in the rows | rich text: emoji, `#channel` names, links | plain text: raw `:emoji:` and `#C0EA9S0A0` ids | **fixed** (`spanLine`, names resolved by core) |
| "Reset" | 13px link after the filters, always shown | missing | **fixed** |
| Workspaces / Organizations filters | menus | toast "isn't supported yet" | **open** |
| Filter carets | chevrons | filled triangles | **open** (icons) |
| Tab icons (Channels, External, Invitations) | `#+person`, building, envelope | `#`, globe, send | **open** (icons) |

## Sidebar (seen in passing)

| Feature | Slack | Clack | Status |
|---|---|---|---|
| A channel whose name is only an emoji/symbol glyph | shows the glyph (tofu on Linux Chrome) | empty name after "#" | **open**: font fallback, not channel code |
| Draft pencil on a channel row | pencil before the badge | none | **open** (sidebar) |

## Code channels (for the code-channels agent)

#test (C0BUVSNLEA3) is a Slack Code channel (`properties.code_channel`, `agent_session`, `record_channel`). Slack's tab label switch has cases Clack doesn't know: a PR-link tab (`prUrl`), "Resources" instead of "Files & links" (record channels), "Summary", "Record details", "Skills". Channel details render `RecordChannelDetails` first on About for record channels and hide "Created by". These belong to the code-channels work.

## Not compared yet

- The notifications button / modal ("Just mentions") field by field.
- The edit topic / description modals and their validation messages.
- Leave → rejoin round trip in the UI (covered on the fake by the preview tests; not compared visually).

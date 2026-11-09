# Code channels (Slack Code)

An AI agent (Claude, Devin, Copilot, Vercel…) opens a dedicated channel for a longer task. Slack calls these code channels, and in older strings "session channels". This page records what Slack's web client does with them and what Clack ports.

The sources are Slack's public CDN chunks for build `a84662d2d135` (2026-11-08): `client-boot`, `gantry-v2-async-client-boot-shared`, `gantry-v2-async-client-boot-render`, `modern.vendor`, the `*code-channel*` async chunks and `client-boot-styles.cf726c4f2eaecc74.min.css`. Module ids below (`aK5v`, `j9If`, …) are webpack ids in those files. Chunk names came from slack-datamine-plus `build/chunks.txt`. The live sample is Hack Club's #test (C0BUVSNLEA3), read once with `conversations.info`, `bookmarks.list`, `conversations.history` (block types only) and `experiments.getByUser`, from inside the signed-in browser. The account isn't a member of #test.

Code: `internal/core/codechannel.go` (model, status, calls), `internal/core/app.go` `sections` (sidebar), `internal/ui/codechannel.go` (dot, header tile, context bar), `internal/fakeslack/codechannels.go` (fixtures).

## Data model

Everything sits on `channel.properties`. #test had exactly these keys:

```
record_channel  {record_id: "AC:<n>:<n>:<channel id>", record_type: "agent_channel",
                 record_label: "Code Channel", record_label_plural: "Code Channels"}
code_channel    {}
agent_session   {status: "active", title: <string>, agent_bot_user_ids: [],
                 encoded_agent_bot_user_ids: [<bot user id>]}
```

A normal channel (#bot-tests) has only `tabs`/`tabz`. #test had no bookmarks and only plain `rich_text` messages, so the live sample shows no artifacts.

**Which channels are code channels.** `aK5v.$n`: a record channel (`record_channel.record_id` set) whose `record_type` is `"agent_channel"` or whose `record_id` starts with `"AC:"`. Nothing else counts; an `agent_session` alone doesn't.

**agent_session** (read in `j9If`, `iR00`, `TDkr`, `0AIR`):

| field | use |
| --- | --- |
| `status` | `active`, `processing`, `suspended`, `closed` (enum `jZ43`; `stale` is computed, never sent) |
| `date_status_processing_expire` | unix seconds; after it, `processing` falls back to active |
| `title` | session title (thread sessions, rename modal) |
| `encoded_agent_bot_user_ids`, `agent_bot_user_ids` | the agent; the first id names it (`useAgentName`, fallback "Agent") |
| `agent_statuses[]` | per-agent `{agent_bot_user_id, status, loading_messages, agent_response_mode}` (multiplayer, behind `agent_multiplayer`) |
| `origin_link` | `{channel_id, ts}` of the message the channel was started from (channel menu "go to origin message") |

**code_channel** (read in `07q3`, `96Xs`, `PntH`, `ICrU`, `pzMB`):

| field | use |
| --- | --- |
| `repo`, `branch` | the context bar's aria label "{agent} — {repo}, {branch}" |
| `context_bar_items[]` | `{key, item_type, label, icon, url, bot_user_id}`; `item_type: "action"` is a button, anything else a link or plain text |
| `pr_status` | `open`, `draft`, `blocked`, `closed`, `merged` (status icon when idle) |
| `ci_state`, `pr_title` | PR tab and unfurl |
| `agent_session_views[]` | `{view_id, type, name, file_id, canvas_id, pr_url, content_version}` |
| `agent_views[]` | html views `{view_id, view_key, file_id, content_version, date_added}` |
| `diff_file_id`, `diff_file_version` | the "Code" diff view |
| `plan_canvases[]` | `{canvas_id, date_tabbed}` |
| `summary_message.message_ts` | the AI summary's anchor |

View types are `diff`, `html`, `canvas`, `block_kit`, `pull_request` (enum `SS3I`). `getCodeChannelViews` (`PntH`) merges the lists, drops PR views unless PR views are on, and sorts the Code diff first.

## Agent status

`getAgentChannelStatus` (`j9If.RO`) and `getAgentChannelStatusAt` (`j9If.kB`), in order:

1. `closed` → Closed; `suspended` → Suspended.
2. `processing`, or a live busy signal → Processing. The busy signal is an assistant status of `typing`/`banner`, or a message whose `streaming_state` is `in_progress`.
3. With `session_channels` on, a channel whose latest message is older than 259200 s (3 days) → Stale.
4. Otherwise Active.

A Processing status whose `date_status_processing_expire` has passed, with no live signal, resolves to Active.

Labels (`UaSM`): Done, Working, Needs attention, else Inactive. The sidebar dot (`getAgentStatusDisplay`, `WbzC`):

| status | icon | tooltip |
| --- | --- | --- |
| Closed | `check-circle` | Done |
| Stale | `minus` | Inactive |
| Suspended | `needs-attention` | Needs attention |
| Processing | spinner (InfiniteSpinner medium, slow, inherit), pulsing | Working |
| Active + PR open/draft | `pr-open` | PR open |
| Active + PR blocked | `needs-attention` | PR blocked |
| Active + PR closed | `pr-closed` | PR closed |
| Active + PR merged | `branch` | PR merged |
| Active | `status-member` | Idle |

The live #test row showed the `minus` icon with aria-label "Inactive". Its last message was 2026-11-04, past the 3-day threshold, which matches the table.

Clack ports rules 1, 3 and 4 and the expiry. It has neither live busy signal yet, so a streaming reply doesn't turn the dot into a spinner.

## Experiments

`experiments.getByUser` for the test account (2026-11-08), code-channel ones only: `session_channels`, `session_channel_views`, `session_channels_create`, `session_channels_inline_diff`, `session_channels_slug_peek`, `session_channels_translations`, `code_channels_naming`, `code_channels_auto_close`, `code_channels_client_create_api`, `code_channels_ghes_provider`, `code_channels_set_canvas_content`, `agent_channel_header`, `channel_details_agents_apps_tab`, `record_channels` are on. `code_channel_commands` is off. Absent, so off: `session_channels_code_tab_pr` (artifact tabs and PR views), `agent_sessions_synth_view`, `code_channels_browse`, `code_channels_internal`, `session_channels_mute_button`, `agent_multiplayer`, `code_channels_pr_link_tabs`. Clack's offline defaults include the first three.

## Sidebar

**The section.** With `session_channels` on and no persisted `coding_channels` section, the client makes one: `{type: coding_channels, name: "Code channels"}` with id `local_coding_channels` (`client-boot` selectors `z`/`WHwP`). Selector `Q` splices the client-made sections in at the front of the ordered list (Salesforce ones go before Channels, canvases after), so Code channels lands first. The live sidebar showed it right under the top links, above Starred, with the `code` glyph as its heading icon (`data-qa-channel-sidebar-section-heading="coding_channels"`).

**Membership.** `getDefaultSectionType` (`client-boot`): a channel in a custom section stays there; otherwise starred → Starred, temporary → Temporary channels, then any code channel → Code channels, before Salesforce, Slack Connect, agents, apps, DMs and Channels.

**Visibility.** `lJBI`'s visible-sections filter doesn't special-case `coding_channels`, so the section shows whenever it exists, empty included. Its default sort is recent (`O/sM`) and its default filter is the "active" dormant filter (`aQqV.I`); Clack already had both in `sidebar_rules.go`. Archived channels leave the sidebar like any archived channel.

**Rows.** `Jbto`: a code channel's `#` becomes `AgentChannelStatusDot` (`p-channel_sidebar__presence_icon`, 16 px, the row's color; table above). Hover shows "Code channel" plus the status label.

Clack ports the section, its position and icon, the membership rule and the dot. It leaves out the "More code channels" overflow row (`iNkd`), the "Mine" filter (behind `agent_sessions_synth_view`), the `code_channels_sidebar_peeks` hover peek and the dot's tooltip.

## Header

`ViewHeaderCodeIcon` (in the `img_api_automation_landing…` merged chunk): a code channel's header shows a 28 px tile before the title, `background: rgb(--dt_color-plt-aubergine-0)`, `color: rgb(--dt_color-plt-aubergine-60)`, radius 8, 1 px below center, holding the 20 px `code` glyph (`user-groups` in the Slackbot standalone app). The same code drops the star button for code channels. The title keeps the normal `channel-filled` icon (`InlineChannelIcon` without `showCodeIcon`), as the live header did. There is no status chip in the header. Status lives in the sidebar dot and the context bar's working chip.

Clack ports the tile and the missing star. The pencil (`EditCodeChannelNameButton`) isn't ported.

## Foreword

`CodeChannelForewordDescription` (`Bj51`) replaces the channel intro and its buttons: "This is a code channel — @{agent} can see and respond to every message and file." then a 12 px `content-sec` disclaimer 4 px below: "Content shared here is also processed by {agent} according to its terms. {agent} uses AI and can make mistakes." Clack ports the text; the agent is plain text, not a member slug.

## The links bar (context bar)

The bar above the composer is not bookmarks. It is the composer's context bar (`BaseContextBarV2`) in its `agentChannel` variant, `ContextBarAgentChannelContent` in `07q3`.

**When.** `shouldDisplayAgentChannelBanner`: `session_channels` on, a code channel, not in a thread, not the share dialog, not presented as a Slackbot thread.

**Look.** `.p-context_bar_v2--agent-channel`: `--dt_color-base-pry` background, padding 6px 8px, top corners 8px, no icon, a 6 px filler into the composer. Content `.container__BAERT`: flex, gap 16, 12 px `content-sec`, space-between.

**Content, left to right.**

- Mute button: behind `session_channels_mute_button`, off.
- The working chip (`AgentWorkingNugget`), only while Processing. `.chip__U2UDT` is `base-sec`, radius 8, padding 6px 8px. It holds a 14 px slot with a small spinner (on hover a 14 px `base-inv-hgl-1` dot with `stop-filled`) and "Stop agent" ("Stopping…" while stopping). A click calls `agents.sessions.stop {channel, reason: "stopped-by-user"}`.
- Archive button: Stale only, behind `code_channels_internal`, off. It calls `conversations.archive` with reason `archive_stale_agent_channel`.
- A spacer, then the `context_bar_items`. An info item is `.context_link__bdTEu`, `content-ter` (`content-pry` on hover), a 16 px icon 4 px before the label. The icon must be one of branch, folder, hierarchy, life-ring, link, globe, terminal, code, search, lock, else `link`. With a url it's a link with the url as tooltip, without one plain text. An action item is the same look as a button: `client.codeChannels.action {channel_id, action_key, client_token}`, a spinner while pending, toast "Couldn’t reach {agent}. Try again." on failure.
- The response mode button: `agent_multiplayer`, off.

An empty `code_channel` (like #test) shows "No branch info" (`content-ter`, margin-right 4) instead of items. A preview (not a member) gets no mute or archive, and actions are disabled with "Join this channel to interact".

Clack ports all of this except the mute, archive and response-mode buttons (off for the account), tooltips, and the screen-reader announcements. The live check couldn't show the bar: the account isn't in #test, so the join pane replaced the composer. The DOM did contain `p-context_bar_v2 p-context_bar_v2--agent-channel` with `container__BAERT`.

## Status updates and archiving

No dedicated event exists. Status, title, items and views arrive in `channel_updated {channel | channels, updates}`; the handler merges `updates` into the stored channel. `is_archived` in `updates` routes to `channelArchived`. The only code-channel RTM event is `code_channel_pull_request_updated`, which refreshes PR data. Clack now deep-merges `channel_updated` updates and re-emits the conversation, so dots and the bar change live. `channel_archive` already dropped the row.

Archiving when the work is done is server-side (`code_channels_auto_close`, error key `archive_stale_code_channels`). Workflow Builder has a "When a code channel is archived" trigger (`wfb_code_channel_archived_trigger`).

## Artifacts

Agents publish two kinds.

**Messages.** Plain Block Kit. Plans and task cards (`plan`, `task_card`) already render (`internal/ui/blockkit/agent*.go`). The client knows no `diff` or `html_preview` message block; its block type enum (`client-boot`, 47 types from `callout` to `fallback_canary`) has none.

A block whose type isn't in that enum goes through `SUIRendererItem`: with no renderer registered, `resolveFallback` draws the block's own `fallback` payload (one block or a list) if it has one, else the message renderer's `UnsupportedBlockFallback` (`l2Ua`). That is a full-width entity card, max 426 px, margin 8px 0, 1px border, radius 12, with "This message can’t be fully displayed." in 13 px `content-sec`, padding 12. Clack did something else before: it drew the block's text, or the message's `text` when nothing rendered. It now follows Slack (`richtext.UnknownBlock`, `bkFallback`), and the two richtext tests that pinned the old behavior now pin Slack's. The fixtures include an unknown `code_diff` block.

**Views** (`agent_session_views` and friends). `CodeChannelViewRenderer` draws one view:

| type | rendering | API |
| --- | --- | --- |
| `diff` | the Code tab's diff viewer (`lu3W`) | `client.codeChannels.getDiff`, `getFileLines` |
| `html` | an agent view (`vxvv`, sandboxed html) | the file `file_id` |
| `canvas` | the canvas `canvas_id ?? file_id` | canvases |
| `block_kit` | `CodeChannelBlockKitView` with container `{type: code_channel_view, view_id, channelId}` | `client.codeChannels.getBlockKitView {channel_id, view_id}` → `{blocks, service_id, app_id}` |
| `pull_request` | the PR view | `client.codeChannels.getPullRequest`, … |

With artifact tabs off (`session_channels_code_tab_pr`, off for the account), views open in the "Code channel details" secondary pane (`toCodeChannelDetails`: `{id: "code_channel_details", viewType: CodeChannelDetails, params: {channelId, viewId}}`). The pane has a tab per view, named by `name`, else "Pull request", "Canvas", "Details" or "View", plus "Files and links". Navigation comes from channel-tab slugs and links (`navigateToCodeChannelView`).

Clack has the model (`CodeChannel.Views`, the same merge as `PntH`) and `CodeChannelBlockKitView`. It doesn't have the details pane or any view renderer yet. That's the biggest remaining gap. The html view needs a sandboxed web view, which a native client doesn't have.

## Agents & tools

The rail's "Agents & tools" and the sidebar's "Agent sessions" link open the `CodeChannelsBrowser` page ("Agent sessions" / "Code Channels") only with `agent_sessions_synth_view` (and `code_channels_browse` for the public list). Both are off for the test account, so its Agents & tools tab shows no sessions. `channel_details_agents_apps_tab` (on) adds an "Agents & apps" tab to channel details. Not ported.

## Record channels

Code channels are record channels with `record_type: "agent_channel"`. The other record-channel UI (Salesforce: the Salesforce records section, `RecordChannelIntroOverview` in the header, the related-list tab, mention tooltips "{recordLabel} channel") is keyed on Salesforce record types. `aK5v.U_` excludes code channels from it, so none of it shows for a code channel.

## Not known

- The partner API docs (api.slack.com/partners/code-channels) weren't readable; field names here come from the client and the live sample.
- What `client.codeChannels.getDiff` takes and returns. The fetcher snake-cases its args, but the diff viewer chunk wasn't read.
- Whether `context_bar_items` carries more item types than info and action.
- The real look of the bar's links and the working chip. No live code channel the account belongs to had either.

## Tests

- `internal/core/codechannel_test.go`: detection, parsing, the status rules (stale, expiry), the status display table, section placement, the `channel_updated` merge, the three API calls' arguments.
- `internal/ui/codechannel_shots_test.go`: `TestCodeChannelsShell` (section, statuses, context bar heights, the experiment gate), `TestCodeChannelShots` (screenshots over an offline core), `TestCodeChannelsFakeslack` (the fixtures through fakeslack: the section, the preview's join pane, the archived bar and the row leaving the sidebar).
- `internal/richtext/blocks_test.go`: the unknown-block card and the block-level `fallback` payload.
- `.github/workflows/code-channels.yml` uploads `code-channels-shots`: `code-<status>-<name>-{light,dark}.png` for processing, suspended, closed, active, stale and the empty `code_channel`, plus `code-fakeslack-{working,preview,archived}.png`.

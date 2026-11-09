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

With artifact tabs off (`session_channels_code_tab_pr`, off for the account), views open in the "Code channel details" secondary pane. With it on, the same views become channel tabs instead (`96Xs`), and the pane isn't used.

## The details pane

Sources, all in build `a84662d2d135`: `client-code-channel-details-view` (`ECTe`, `CodeChannelDetails`), `shared-js_modern_components_code-channel-details_code-channel-view-renderer_tsx` (`iuvb`, `CodeChannelViewRenderer`; `4shT`, `CodeChannelPrView`), `shared-js_modern_components_WIP_tabbed-channels_views_agent-view-tab-view…807617` (`lu3W`, `CodeTabView`; `vxvv`, `AgentViewTabView`; `V/cF`, the tab helpers), `client-boot-shared` (`Afoo`, the patch renderer; `qi+/` and `OH3S`, the highlighter and its theme; `RzYr`, `HtmlSurfaceIframe`; `rlA3`, `navigateToChannelTabSlug`; `lp6D`, `navigateToCodeChannelView`), `client-boot` (`PntH`, `pX9p`, `96Xs`, `jpad.it` = `toCodeChannelDetails`, `biaa`, the experiment gates) and the CSS in `client-boot-styles` and `client-code-channel-details-view.css`. The `lu3W` chunk isn't in the boot list; the details view's lazy import in `client-boot-render` names it.

**How it opens.** `toCodeChannelDetails(channelId, viewId)` is `{id: "code_channel_details", viewType: CodeChannelDetails, params: {channelId, viewId}}`, pushed into the secondary view. Three things reach it, all through `navigateToChannelTabSlug` (`rlA3`): a channel-tab slug (a rich_text `channel` element with a `tab_id`; the entity collector files it under `channelTabIds`), a channel-tab permalink (`/archives/<C>/tabs/<id>`, `getChannelTabPermalink`) in a message or a deep link, and the inline cards of an `agent_view_added` message (see below). `resolveCodeChannelViewIdForTab` (`pX9p`) maps the tab to a view: the tab must be in the channel's `tabz`/`tabs`; a `code` or `agent_view` tab is its own view id; a `canvas` tab counts when its `data.file_id` is one of `plan_canvases`. It opens the pane only when `biaa.PH` holds (`session_channels` on and artifact tabs off); anything else navigates to the channel tab. The header has no button for it and the context bar has none either. Its only button is the pane's own expand toggle.

**`agent_view_added` messages.** The message renderer (`Nwo1` in `client-boot-shared`) reads the `channelTabIds` of an `agent_view_added` message's blocks. With `session_channels_inline_diff` on (`biaa._5`, on for the account), a link to `code` adds `InlineCodeDiffBlock` (a card: "{n} files changed", +/−, the first 3 files capped at 12 lines, "View Full Diff"; it calls `getDiff` with reason `inline_code_diff_block`), and a link to an html or canvas view adds `InlineAgentViewHtmlBlock` / `InlineAgentViewCanvasBlock` (a file card, "Open {title}"). Each card opens its tab through `navigateToChannelTabSlug`.

**The view list.** `getCodeChannelViews` (`PntH`): `agent_session_views` (a `pull_request` view only with `prViewsEnabled`, which is `biaa.jt` = `session_channels_gh_wo`, off for the account, and a `pr_url`), then `agent_views` as html views, `diff_file_id` as the `code` diff, `plan_canvases` as canvases. A plan canvas's view id is the id of its canvas tab when `tabz` has one, else the canvas id. The Code diff sorts first.

**The pane.** A flexpane (`.body___uSCk`, the header `.header__KXeRZ` with a 48px primary row). Its title is a `TabMenu` (`.tabs__RFqbz`, aria "Code channel views") with one tab per view: `.tab__Fz4BC` is 49px tall with 8px side padding and 4px apart, a 16px icon left of a title cut at 100px. Labels come from `labelForView`: a canvas's title once loaded, else `name`, else "Code" / "Canvas" / "Details" / "Pull request" / "View". Icons come from `svgIconForView`: `code`, `eye-open`, `canvas-content`, `info`, `branch`. When the channel's `tabz` has a `files` tab, a last tab "Files and links" (`canvas-browser`) shows the channel's files. With no tabs the title is "Code". The selected tab is `viewId` if it's in the list, else the first. Next to close is `artifacts_expand_toggle` ("Expand view" / "Collapse view", `expand-diagonal` / `reduce-diagonal`), which switches the window to single-column mode. Above the body, `GitHubEnterpriseConnectAlert` asks to "Connect GitHub Enterprise ({host})" when a PR's host isn't connected.

**The renderer.** `CodeChannelViewRenderer` by type:

- `diff`: `CodeTabView` (`lu3W`) for the channel.
- `pull_request`: in artifact-tab mode, `CodeTabView` with the PR's url. In the pane, `CodeChannelPrView`: for github.com, the GitHub work object `W:<pn>:<repo>#<n>` (looked up with `github_cloud_get_pr`); for other hosts, `client.codeChannels.getPrWorkObject {channel_id, pr_url, reason: "code-channel-pr-view"}` → `{entity_id, message_ts}`. Either way it draws the work-object flexpane. On failure it shows "Post the pull request link in this channel to load its details."
- `html`: `AgentViewTabView`. It calls `client.codeChannels.getView {channel_id, view_id, reason: "agent_view_tab"}` → `{html_content, content_version, csp: {resource_domains}}` and refetches when `content_version` changes. The header (`.header__EZe0R`, padding 12, gap 12, flex-end) has the caption "HTML file created by @{agent}" (13px `content-ter`) and the outline buttons "Download" and "Share". The content box (`.content__COVya`, 1px `otl-ter`, radius 12, margin 0 12px 12px) holds `HtmlSurfaceIframe` (variant `agent_view`): a sandboxed iframe (`allow-scripts allow-same-origin`) on a per-file sandbox origin, its CSP built from `resource_domains`. When the sandbox URL can't be built, it shows "View is not available in this environment."
- `canvas`: `CodeChannelCanvasView`, the canvas (`canvas_id ?? file_id`) with its menu floating at the top right.
- `block_kit`: `CodeChannelBlockKitView`. `client.codeChannels.getBlockKitView {channel_id, view_id, reason: "code_channel_block_kit_view"}` → `{blocks, service_id, app_id}`, refetched on `content_version`. While loading it shows a jumbo spinner ("Loading view"); on error, "Unable to load this view. Please try again later."; then the blocks in `.scroller__dGvZk` (padding 16) with the container `{type: "code_channel_view", view_id, channelId}` and actions through `sendActions`.

**`CodeTabView`.** In branch mode it fetches `client.codeChannels.getDiff {channel_id, reason: "code_tab_view"}` → `{diff: {files[], truncated, total_commits, base_branch, head_branch}}` and refetches when `code_channel.diff_file_version` changes. A file is `{filename, status (added|removed|renamed|else modified), additions, deletions, patch, is_generated}`. With a PR (and `session_channels_code_tab_pr`) it fetches `client.codeChannels.getPullRequest {channel_id, pr_url, reason}` → `{pull_request: {title, body, state, review_decision, has_conflicts, pending_review, body_source, reactions}, diff, comments[], revision, more_comment_kinds, response_metadata}`. Expanding context lines calls `client.codeChannels.getFileLines {channel_id, pr_url, revision, path, ranges}` (`code_channels_expand_diff_context`, PR only).

- Loading shows a spinner ("Loading diff"). An error shows `.authPrompt` with "Unable to load diff" (15px bold) over "Something went wrong fetching the diff. Please try again later." (13px `content-sec`), or a provider connect prompt.
- The summary line (`.summaryLine__ku0Xz`, padding 0 16px 16px, 16px on top without a header): "Comparing {base} ← {head}" (or "Branch {base}") with the branches as micro aubergine pills in monospace, then the stats (`.stats__FkD_0`, 13px `content-sec`, gap 16): "# commits" (branch mode), "# files changed" and +additions −deletions in Slack green and red (hidden when truncated).
- Files (`orderFiles`): generated files last, or the file tree's order in the PR layout. Each file is `.fileGroup__Ne7g2` (1px `otl-ter`, radius 8) with a sticky header `.fileItem__Tg0hG` (`base-sec`, padding 4px 8px, 13px): the caret toggle, the path (12px mono, ellipsized), "Copy filepath" (then "Copied" for 2s), a neutral "Generated" pill, then +/−. `getAutoCollapsedFiles` collapses generated files and files over 500 changed lines, and everything after 15 open files or 500 rendered lines (each file counts at most 100). Up to 5 commented files open anyway.
- The patch (`Afoo`, `.patchTable__UELw3`, 12px, line-height 1.25, mono): rows are 20px. Line numbers are `content-ter`, min-width 48px, padding 0 8px. Code has padding 0 16px and `white-space: pre`. Added rows are `#2ea04326` (number `#2ea04340`), removed rows `#f8514926` (number `#f8514940`). A hunk header row is `base-hgl-1` / `content-sec` with padding 4px 16px. A patch shows 100 lines, then `.expandBar__CZ8f6` with "Show N more lines" (12px `content-hgl-1`). Split layout (`buildSplitRows`): 80px number columns, `table-layout: fixed`, `pre-wrap`, an empty side in `base-sec`, removed lines paired with the added lines after them. "Hide whitespace" (`hideWhitespaceChanges`) turns a removed/added pair that differs only in whitespace into a context line.
- Syntax colours: shiki, one line at a time, the language from the extension (`R` in `Afoo`; js, ts, json, html, css, md and diff are preloaded, the rest load on demand). The theme is "slalabaster" (`OH3S`), which colours only comments (tomato 70), strings (cilantro 70), escapes and punctuation (`content-ter`), constants and numbers (indigo 70) and entity names (aubergine 70). Keywords keep the text colour.
- The PR layout (`nu`: `session_channels_code_tab_pr` and PR data): state pills (Open success, Draft neutral, Merged indigo, Closed flamingo), "Approved" / "Changes requested", "Merge conflicts". It has the subtabs "Conversation" and "Files Changed {n}" (`.prSubtab__MYTjc`, 26px, radius 5, 13px bold, selected `base-inv-hgl-1`). Conversation holds the description card and the unanchored comments. Files Changed adds a sticky 40px toolbar: "Show/Hide file tree" (`sidebar-left`, only with more than one file and a container ≥ 720px), the commit range picker, "Jump to file", "Conversations", "Diff display options" (`filters`: Layout Unified / Split, then Hide whitespace) and "Find in diff". The file tree (`.fileTreePane__agnE3`, 240px, resizable) compacts single-child folder chains, puts folders first and marks each file with a coloured change dot (green added, red removed, yellow modified) or an arrow for renames. Review comments sit under their line (`anchorGitHubPrComments`: `review_anchor {path, line, side}`; LEFT is the old side). The layout, whitespace and tree choices persist (`code-channels-diff-layout`, `…-hide-whitespace`, `…-file-tree`, `…-file-tree-width`). Outside the PR layout the patch is always unified, with no toolbar or tree.

**What Clack builds.** `internal/core/codechannel_views.go` has the gates (`CodeChannelViewsOn`, `CodeChannelPRViewsOn`, `CodeChannelInlineDiffOn`), `CodeChannelViews` (`PntH` with the experiments, plan canvases keyed by their tab), `CodeViewLabel` / `CodeViewIcon`, `CodeViewForTab` (`pX9p`), and the calls `getDiff`, `getPullRequest`, `getView`, `getBlockKitView` with Slack's args. `internal/core/codediff.go` ports the patch model: `ParsePatch`, `HideWhitespace`, `SplitRows`, `FileTree`, `OrderFiles`, `AutoCollapsed`, `CodeLang`. `internal/core/codehighlight.go` is a per-line tokenizer for the slalabaster scopes. Clack has no TextMate engine, so the token rules are its own. `internal/ui/codechannel_pane.go` has the pane (tabs, expand toggle, close), the slug and permalink routes (`OpenChannelTabLink`, `slack://channel?id=…&tab=…`), the Block Kit view (`blockkit.NewCodeChannelSurface`, container `code_channel_view`) and the canvas view (canvasview, Tab mode). `internal/ui/codechannel_diff.go` is `CodeTabView` (summary line, file groups with sticky headers, unified and split, colours, the 100-line page, auto-collapse, the PR pills, subtabs, toolbar, display options menu, file tree, comments). `internal/ui/codechannel_html.go` is the html view.

The html view is the only one Clack can't draw. Clack has no web view, and rendering untrusted HTML in-process is out. It fetches `getView` (keeping only whether there is content) and shows the caption, "Download", and Slack's own state for an agent view it can't sandbox, "View is not available in this environment.", with the file's "Open in browser" (Slack's file action wording; it opens `url_private`).

**Where Clack differs.** The pane's PR view uses `CodeTabView` with `getPullRequest` (Slack's artifact-tab PR layout) instead of the GitHub work-object flexpane, because Clack has no work-object renderer. Both are hidden for the test account, since PR views are off. Clack shows the subtabs, file tree and layout menu whenever a PR view loads, without also requiring `session_channels_code_tab_pr`. The file tree is a full-height column next to the list rather than sticky inside it, and its width isn't resizable. Long lines clip instead of scrolling sideways, and split mode doesn't wrap. The PR comment composer, reviews, reactions, commit picker, find in diff, Jump to file, Conversations and expanding context lines are not ported, nor is the "Files and links" tab, the GitHub Enterprise connect alert, the html view's "Share", or the `agent_view_added` inline cards. Messages with tab slugs link to the pane instead.

## Agents & tools

The rail's "Agents & tools" and the sidebar's "Agent sessions" link open the `CodeChannelsBrowser` page ("Agent sessions" / "Code Channels") only with `agent_sessions_synth_view` (and `code_channels_browse` for the public list). Both are off for the test account, so its Agents & tools tab shows no sessions. `channel_details_agents_apps_tab` (on) adds an "Agents & apps" tab to channel details. Not ported.

## Record channels

Code channels are record channels with `record_type: "agent_channel"`. The other record-channel UI (Salesforce: the Salesforce records section, `RecordChannelIntroOverview` in the header, the related-list tab, mention tooltips "{recordLabel} channel") is keyed on Salesforce record types. `aK5v.U_` excludes code channels from it, so none of it shows for a code channel.

## Not known

- The partner API docs (api.slack.com/partners/code-channels) weren't readable; field names here come from the client and the live sample.
- The real response shapes of `getDiff`, `getPullRequest` and `getView`. The field names above are what the client reads. A read-only `getDiff` on #test answered `team_is_restricted`, so no live sample.
- The PR comment `sender` shape. `G.d_` names the author; Clack reads `{login, name, user_id, avatar_url}`.
- What the pane looks like live. No code channel the account belongs to has views.
- Whether `context_bar_items` carries more item types than info and action.
- The real look of the bar's links and the working chip. No live code channel the account belongs to had either.

## Tests

- `internal/core/codechannel_test.go`: detection, parsing, the status rules (stale, expiry), the status display table, section placement, the `channel_updated` merge, the three API calls' arguments.
- `internal/ui/codechannel_shots_test.go`: `TestCodeChannelsShell` (section, statuses, context bar heights, the experiment gate), `TestCodeChannelShots` (screenshots over an offline core), `TestCodeChannelsFakeslack` (the fixtures through fakeslack: the section, the preview's join pane, the archived bar and the row leaving the sidebar).
- `internal/richtext/blocks_test.go`: the unknown-block card and the block-level `fallback` payload.
- `internal/core/codediff_test.go`: the patch parser and line keys, split rows, hide whitespace, the file tree, auto-collapse, the highlighter, the view list and its experiments, `CodeViewForTab`, comment anchoring, and the four calls' arguments.
- `internal/ui/codechannel_pane_test.go`: `TestCodeChannelPaneLinks` (the tab-link parser), `TestCodeChannelPane` (an `agent_view_added` slug opens the pane on its view, the tab list and labels, offline error state, the non-view and artifact-tab cases), and `TestCodeChannelPaneFakeslack` (every view over fakeslack, with screenshots).
- `.github/workflows/code-channels.yml` uploads `code-channels-shots`: `code-<status>-<name>-{light,dark}.png` for processing, suspended, closed, active, stale and the empty `code_channel`, plus `code-fakeslack-{working,preview,archived}.png` and `code-pane-{diff,blockkit,html,canvas,pr-conversation,pr-split,pr-unified}-{light,dark}.png`.

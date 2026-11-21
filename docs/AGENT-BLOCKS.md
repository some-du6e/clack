# Thinking blocks: plan and task_card

Karim, 2026-11-15: "thinking blcoks are finicky". This is what Slack's web client does with the blocks agents use to show their work (build `9685e6ce144b`, public CDN chunks fetched without a cookie), what the account's apps send, and what Clack does. Slack's i18n namespace for them is `thinking_steps`. Module ids are the bundle's. Class names are from `client-boot-styles`, whose hashes don't change between builds.

## What the account's apps send (2026-11-19)

The probe reads shapes only (key names, counts, length buckets, never text) as the orchestrator's account.

- 34 messages with a `plan` block, all from one plain app (not `is_agent_app`, not `is_ai_app`), all top level in channels. Half of them are thread roots, and no thread reply holds a plan.
- Each message has exactly one block, the plan, with `block_id`, `title` and `tasks`. 30 plans have one task; the rest have 3, 6, 10 and 20.
- Each task has `task_id`, `title`, `status` (complete or error) and `details` (one rich_text section). All details are 250 characters or less and at most three lines, so Slack's content cut never applies to them.
- Every message has `edited`: the app posts and then updates with chat.update. None has `streaming_state`. Slack shows no "(edited)" on them, because `shouldDisplayAsEdited` (iub5) skips anything with a `bot_id`. Clack already matches that (`core.Message.DisplaysEdited`).
- The account's three bot DMs aren't agent or AI apps (every `apps.profile.get` flag is false), so no agent DM can be probed from it.

So the shape that matters most is a top-level plan that changes in place under chat.update.

## Slack's components

The account has `agent_streaming_slimmer_task_cards` on, so plans and task cards draw slim.

- **PlanBlock** (5mjF): the status is in progress while any task is pending or in progress, then error if any task failed, else complete. Without `plan_block_sidebar` (the account doesn't have it) the plan is an ExpandableContent (cQd1), collapsed until clicked. Its ToggleBar (3mCJ) is a fit-content button (`.toggleBarButtonSlim`: no hover background) holding the inline status icon, the title (13px, `content-ter`) and a caret. Closing the plan unmounts its PlanTaskList, so reopening it shows every task open and its text cut again.
- **ExpandableContent** (cQd1): sets the block's `height` to its content's measured height (slim, or open; cards use 40px collapsed), and `.block` has `transition: height .25s`. So any change of that height animates, not only a toggle: an update that adds a task or details, or a title that wraps onto a new line. `.blockSlim` clips with `overflow: clip; overflow-clip-margin: 8px`, and cards with `overflow: hidden`.
- **TaskCard** (1+vf): one component for both looks. It starts open only for an MCP task (block id `mcp_task_…`), and under `slackbot_mcp_tasks_card_auto_collapse_when_complete` (on for the account) only while the task isn't already done. A card the viewer hasn't toggled closes when its task completes or fails.
- **TaskItem** (3fJq): plan tasks start open. A slim task's details and output go through TruncatedBlocks (ZJNP) with 3 lines, 250 characters and a block-level expand button. That cut is BLFT `truncateRichTextElements`:
  - a block boundary costs a line, and so does each list item but the last;
  - a run past the lines is cut at the line, with no ellipsis;
  - a run past the characters is cut there, trimmed, and ends in "…";
  - **Show more** (`c-rich_text_expand_button`, 13px link) shows the whole text, and **Show less** cuts it again.

  More than five sources become a Sources button (SourcesModal). A task can carry an `icon`.
- **Icons** (ihFq): a primary icon is `check-circle-filled` when complete and `warning-filled` on error. Secondary icons are `hourglass` (pending), `ai-sparkle-filled` (in progress), `check-filled` (complete) and `warning-filled` (error). In progress draws the small blue spinner.

## What Clack does

`internal/ui/blockkit/agent.go` (slim) and `agent_card.go` (cards) draw both looks.

Fixed in round 2:

- **Content cut**: `agent_truncate.go` ports BLFT and is checked against Slack's own module run in node (`testdata/agent_truncate.json`, 17 cases). `slimRich` draws the cut text with Show more, or the whole text with Show less, and closing a plan or card starts its tasks over.
- **MCP cards**: in both looks, an MCP card starts open, starts closed when it is already done, and closes once when its task finishes, unless the viewer toggled it.
- **Height**: `animated` is one rule for every change of a block's height once it has been drawn: a toggle, an MCP card closing, or an update that grows the plan or wraps its title. Each runs the .25s transition from the height on screen. A change during a transition starts again from where it got to, and a change back runs for the share already done (CSS's reversing shortening factor). Before this, only a toggle animated, and an update made an open plan jump (61 → 112px in the test) or a collapsed one jump when its title wrapped (24 → 44px).

Tests are in `agent_slim_test.go`:

- `TestAgentTaskShowMore` clicks Show more and Show less through Gio's router and checks a plan that reopens.
- `TestAgentMCPCardAutoCollapse` covers the experiment on and off, a card the viewer toggled, and a card that is done when first drawn.
- `TestAgentPlanUpdateTransition` replays chat.update on an open plan and on a collapsed one.
- `TestAgentTaskTruncation` compares the cut with Slack's module.

The fixtures are `plan-long-details`, `task-card-mcp-running` and `-done`, and `plan-grow-1`, `-2` and `-title` (fakeslack/agents.go). On e93a5e70 the three behaviour tests fail and the guards pass (red runs 44480752516 and 44487568322). The shots are `agent-slim-content-{light,dark}.png` in bk-agent-shots.

Round 3 added:

- **Task icons** (TaskIcon, IDPB). A task's `icon` (`{"name": …}`) draws instead of its status glyph when the task is neither in progress nor failed, and its name is one of the 54 Block Kit names Qw/u `deriveIconName` maps (`lightbulb` → `emoji-objects`, `gear` → `settings`, …). Slim draws it at 13px in the step indicator, `content-sec` when complete. Cards draw it at 15px in the header's 20px box. An unknown name, or an icon that isn't an object, keeps the glyph, and the block still parses. Fixture `plan-icons`, test `TestAgentTaskIcons`.
- **The stopped label** (StoppedLabel, J825). `ai_context.result_status` `stopped_by_user` (vshb `getMessageLabelValues`) puts "(stopped)" where "(edited)" goes, in `c-message__stopped_label` (13px, the same colour, no tooltip). Under `agents_streaming_stop_button_fe` it reads "(stopped by {name})" when someone other than the viewer stopped it. A message showing "(edited)" shows that alone (insertMessageLabelIntoBlocks), and an app's message never shows "(edited)". Fixture `AgentStoppedReply`, tests `TestAgentStoppedLabelValues` (core), `TestAgentStoppedLabel` (richview), `TestAgentStoppedRow` (messages).

Not built yet:

- **The Sources button and its modal.** In slim mode, more than five sources become one "Sources" link opening SourcesModal (+sSl, `openPlanSourcesModalCircular`). The modal sorts them into Slack messages, files and web links. A message source is fetched by its permalink and drawn with the sender's avatar, the channel, a timestamp that jumps to it, and its text cut to 165 characters and 3 lines. A file source is a Slack file URL. A web source has a title and domain.
- `plan_block_sidebar` ("Working…" opening PlanDetails in the sidebar; the account doesn't have it).
- **The streaming text reveal.** StreamingMessageRenderer (u33y, under `sui_stable_streaming_mount`) and StreamingRichTextBlock (0gd9) reveal a streaming message's text at 250 characters a second in 30ms ticks. When it completes with 250 or more characters still hidden, the rest fades in: opacity 0→1 and 20px up over .4s, staggered over 1s. A remount doesn't animate again. Clack draws each update's text at once. The account's plan app doesn't stream, but agents that use chat.startStream do.

A hazard, not proven to be the bug: the spinner and the height transition run `gtx.Execute(op.InvalidateCmd)` in the middle of layout. That is the pattern that lost the composer's + clicks (composer-plus-dblclick, not merged into ui). In Gio's router (`io/input/router.go` `execute`), such a command collapses the frame's pending state changes and requeues their events. I couldn't build a plan sequence that loses a click on paper, so it is left as it is, with no red test.

## Agent DMs

The full map from Slack's code (the new agent DM, its header and tab model, the AI app tabs, suggested prompts and status rows) is in docs/AGENT-DMS.md.

Slack's agent DM changes are gated on the app and on experiments:

- `feat_agents_dms` (the new agent DM, with the header's Tasks button and peek);
- `support_lots_of_agents`;
- the "sunroof" agent resolution: jVZf `resolveIsActiveAgentApp` and the member's `agent_status`, where `is_ai_app` members never open as agents.

The test account has none of them, and none of its DMs is with an agent or AI app. What Clack has is the Tasks tab (docs/APP-TASKS.md), the assistant status line and the AI app thread titles. The rest is not built:

- suggested prompts (`assistant.threads.setSuggestedPrompts`);
- "Suggested from your recent prompts";
- New chat;
- Chat and History;
- the split view.

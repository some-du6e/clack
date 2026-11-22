# Agent DMs and AI app DMs

Karim, 2026-11-15: "the agents thingy like a dm with an agent". This is how Slack's web client draws a DM with an agent app or an AI app, read from build `9685e6ce144b` (public CDN chunks fetched without a cookie). Module ids are the bundle's. None of it is in Clack yet: the experiments below gate a different header and tab model, and building one piece of it alone would draw a DM that is neither Slack's old one nor its new one.

## Who sees what

On 2026-11-19 the orchestrator's account had none of `feat_agents_dms`, `agents_dms_header_status`, `support_lots_of_agents`, `wfb_3p_agent_tasks_tab`, `wfb_agent_tasks_recent_prompts`, `channel_resources_remove_tabs`, or any `agentic_ia_*` experiment. None of its DMs is with an agent or AI app: every `apps.profile.get` flag (`is_agent_app`, `is_shown_as_agent`, `is_ai_app`) was false. New since 11-13: `wfb_agents_can_create_simple_automations_tab_ms3` on (the task details view), `wfb_3p_agent_tasks_dev_controls` on, `agent_observability` on.

Karim sees the Tasks tab, which needs an agent app shown as an agent and `wfb_3p_agent_tasks_tab`. The tab list (L1V2) doesn't read `feat_agents_dms`, so the tab alone doesn't show whether Karim has the new agent DM. If they do, their agent DM has no tab bar (below).

## The two kinds of app DM

L1V2 `getAppChannelTabsState` builds the tabs:

- **Agent app** (`is_agent_app`, OQHR `dy`): Home, Messages, Sessions (`agentic_ia_agent_dm_sessions_tab`), About, Tasks (docs/APP-TASKS.md). The Messages tab is the DM's channel.
- **AI app** (`is_ai_app`, R `yk`; not Agentforce, not an agent app): Home, **Chat**, Code (a code channel), **History**, Memories and Tasks (Sidekick only), About. No experiment gates this: every AI app DM has Chat and History.

## The new agent DM (`feat_agents_dms`, V0bV)

`isAgentDmsEnabledForTypeAgentDm`: the experiment is on, and OQHR `Xf` says the DM's type is AgentApp. With it:

- **No tab bar.** jKaV `isChannelTabsListEnabledForChannel` is true for such a DM, as it is for every channel under `channel_resources_remove_tabs`. The tabs then live in the channel resources list (ChannelTabsList), opened from the header.
- **Header actions** (3C61, for an app DM):
  - `agent_dm_history`: AgentDmHistoryButton, a 20px `history` icon button with the tooltip "Sessions", toggling the sessions panel (HFT3, section `activity`, entry point `history_button`), shown only while that hook is enabled;
  - `channel_resources`: the resources list;
  - `agent_dm_tasks`: AgentDmTasksButton, when V0bV `isAgentDmTasksEnabledForTypeAgentDm` (the same gates as the Tasks tab, without the plan check);
  - search, only for some app DMs;
  - no bell: notifications go for an app DM under the experiment.
- **AgentDmTasksButton** (8Lr+): a small icon button labelled "Tasks", `ai-workflow` (20px), drawn `ai-workflow-filled` while the Tasks view is open or its peek is showing. A click opens the DM's ChannelTabsList view on the Tasks tab (3Uqs `toAgentDmTasks`), or goes back when it is already open. Hovering opens **AgentDmTasksPeek** after 150ms, below the button: the Tasks tab view (I66T) with `isPreview` and no Add Task. The hover is off while the view is open, and the tooltip "Tasks" is held back while the peek shows.
- **Status in the header** (`agents_dms_header_status`): StatusLine in the view header.
- **The conversation** renders in the AGENT_DM_CONVERSATION container (Zhyr), a thread-like list: broadcast controls hidden, expired agent messages marked (VCb1). `handleAiAssistantStatus` opens a thread in the agent DM view when its root is your own message with no replies yet.

## AI app threads (Chat)

The Thread view (A23K) adds rows for an AI app thread from its `aiApps` props:

- `ai_apps_overview` at the top (AssistantOverview: the app's icon, name and badge, its description, and "How to use {appName}?" opening About);
- after the last reply, `ai_apps_suggested_prompts` (AssistantPrompts): `div.p-ai_app_view__prompts` with an h4 of the title, or "Try these prompts:" (`ai_apps`), then one small outline PromptButton per prompt, sending its `message`;
- `ai_apps_message_shimmer` while the app's status is showing (the loading messages).

The store (Ks/P, `aiApps`) keeps prompts per thread (`suggestedPromptsObjectByThreadTs`, keyed `channel-thread_ts`), and per channel for agents (`suggestedPromptsObjectByAgentChannelId`). They are replaced, never cleared: an empty list draws nothing.

The socket handlers are in boot-render:

- `handleAiAssistantThreadsSuggestedPrompts` sets the thread's prompts, or the channel's for an Agentforce app without a thread, and for an active agent DM the channel-level prompts (QF `s`).
- `handleAiAssistantStatus` sets the status and the shimmer, keyed by thread (or by channel under `ai_apps_animated_streaming_roots_fe` without a thread). An empty status clears both, and `is_using_sessions` clears the shimmer only.

Clack's core already keeps both (core/agent.go, `SuggestedPromptsFor`, `AssistantStatusFor`), and the composer draws the status as the typing line. No view draws the prompts yet, because only the Chat view (AI_APP_CHAT) and the agent views pass `aiApps` to the thread. The regular thread pane never shows them.

## What building it takes

1. The ChannelTabsList (channel resources) for agent DMs under `feat_agents_dms` (and `channel_resources_remove_tabs`).
2. The agent DM header: Sessions, channel resources, Tasks with its peek, no bell.
3. The AI app tabs: Chat (the current assistant thread with the overview, prompts and shimmer rows, and New chat), and History (its threads).
4. The split view (AI_APP_SPLIT) and the header status.

Each needs fakeslack to grant the experiment (core follows the person's own `experiments.getByUser`, as the Tasks tab's tests do), and an agent or AI app fixture.

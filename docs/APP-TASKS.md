# The agent DM's Tasks tab

Karim asked for "the agentic task tab" (2026-11-18). An app that runs as an agent can switch on a Tasks tab in its App Home settings: "Show a Tasks tab where people can manage the tasks this app runs as an agent." Slack then shows **Tasks** in the app's DM, after About.

This file is what Slack's own web client does, read from its public CDN bundle (build `9685e6ce144b`, 2026-11-18, fetched without a cookie), and what Clack does with it. Module ids are the bundle's. Boot modules are in `client-boot`, `gantry-v2-async-client-boot-shared`, `-render` and `-vendors`; the tab itself is in the `shared-js_modern_components_slackbot-ai_views_agent-tasks-tab-view_tsx` chunk; the API fetchers are in `gantry-v2-vendors`. CSS classes are from `client-boot-styles` (their hashes don't change between builds).

## What a task is

A task is a Workflow Builder workflow created by an agent: `creation_source_type: AGENT`, with the app as `creation_source_id`. It has one step, `send_prompt_to_3p_agent` (`inputs.prompt`, `inputs.agent_id`), and a trigger, usually a scheduled one. The prompt runs as the person who made the task, on the trigger's schedule. Slackbot's own tasks are the same thing with `send_prompt_to_agent` and creation source `SLACKBOT`.

## When the tab is there

`getAppChannelTabsState` and `P` in client-boot **L1V2** build an app DM's tabs. For an **agent app** (`isAgentApp`) the list is `[Home, Messages, Sessions, About, Tasks]`, sorted by **3HCD**'s `s3` order (`…AppHome, Channel, AppSessions, AppAbout, App3pAgentTasks`). The Tasks entry needs all of these:

| Input | Where Slack reads it | Clack |
|---|---|---|
| `isAgentApp` | OQHR `dy`: the app profile's `legacy_app.is_agent_app` (FVzo `Mk`). With `support_lots_of_agents` on, the member's agent identity comes first. | `AppCard.AgentApp` (apps.profile.get) |
| `isShownAsAgent` | OQHR `pT`: `legacy_app.is_shown_as_agent` (FVzo `aH`) | `AppCard.ShownAsAgent` |
| `experimentWfb3pAgentTasksTabGroupOn` | YwqI `qC`: `wfb_3p_agent_tasks_tab` is `on` | `core.ExpAgentTasksTab` |
| `!isAgentTasksDisabled` | o1P2 `sQ`: apps `A0AT1U0QRNH` and `A019W997N66` never get it; otherwise the `ai_apps_settings` pref's entry for the app must not have `is_agent_tasks_enabled: false` | team pref `ai_apps_settings` |
| `isAgentTasksAllowedByPlan` | YwqI `ro`: with the experiment on, featureAccess `agent_tasks__write` `is_enabled` (D47q `G2`) | `FeatureAccess("agent_tasks__write")` |
| `isAgentTasksEnabledByDeveloper` | SUqq `getAppHomeFormatted`: the app home meta's `agent_tasks_enabled`, on unless `false`; the meta comes from `conversations.info {channel, return_app_home: true}` (g5os `NP`, quL6) | `AppHome.AgentTasks` |

Slack's client never checks `messages_tab_enabled` for Tasks. The settings page is what couples them: turning off "Allow users to send Slash commands and messages from the messages tab" also turns off the Tasks tab, "which needs messaging turned on", so the app home then reports `agent_tasks_enabled: false`. The fake Slack models that server side.

The tab is labelled "Tasks" (`tabbed-channel`). Its icon is `ai-workflow`, and `ai-workflow-filled` while the tab is open (X2Ko `useTabIconProps`). Clack draws them as `workflow` and `workflow-filled`.

On 2026-11-13 the test account had none of `wfb_3p_agent_tasks_tab`, `wfb_agent_tasks_recent_prompts`, `feat_agents_dms` or `support_lots_of_agents`. Karim sees the tab, so their account has the first one. Clack reads the signed-in person's own `experiments.getByUser`, so it follows whatever Karim has.

### The other entry point: the agent DM header (not in Clack)

Under `feat_agents_dms` (the new agent DM), V0bV `isAgentDmTasksEnabledForTypeAgentDm` puts **AgentDmTasksButton** in the view header. It is an `ai-workflow` icon button labelled "Tasks", with a hover peek (**AgentDmTasksPeek**: the same tab view with `isPreview` and no Add Task), and it opens the tab as a `ChannelTabsList` view (3Uqs `toAgentDmTasks`). It needs the same gates. The account doesn't have `feat_agents_dms`, so Clack has no header button or peek.

## The tab (AgentTasksTabViewContent, I66T)

For a third-party agent the creation source is the DM person's app id (DIay `PV`). The browser instance is `permanent_browser_instance_agent_tasks_<appId>` (VpzY `fT`). Until the app id is known, the tab shows its twelve-row skeleton.

- **Header** (ExplorerHeader with `explorerHeader__JyK7k`): 16px padding above and below, a container at most 1000px wide with 16px insets. The title "Tasks" is an `h1.tabTitle__tTPP1` (18px Bold, line-height 1.2), pulled 8px left by `tabHeader__aG_ot`'s margin. It is Black when `wfb_agent_tasks_recent_prompts` is on (`tabTitleHeavy`). Slack also puts **Add Task** there (a primary button with `plus-filled`, opening the create task modal). Clack doesn't have it; see below.
- **Search** (the explorer's sticky results header, only when the tab isn't in its empty state): SearchInput `tasks-search-input`, placeholder "Search your tasks", medium, with 16px below it, in `p-explorer_search__content` (at most 1000px wide, 16px insets). Typing filters the loaded tasks by title at once (useAgentTasksExplorer's `z`). Only after the list has had more than one page (`cursorMark`) does it also ask the server, 300ms after typing (`filter_options.name`).
- **Rows** (`agent-task` rows, rendered as **AgentTaskItem** in NQBk): `p-search_workflows_item p-search_workflows_agent_task_item`, padding 12, 1px `rgba(fg, .1)` borders shared between rows, the first and last rounded by 12, at most 968px wide (`p-explorer_results__contained-result`). On hover the border goes to `rgba(fg, .15)` and the row gets a `0 1px 1px #0000000f` shadow. Rows have `--no-click`, so the row itself isn't a button. Each row has:
  - a 36px icon box, radius 8, `indigo-5` background with the 24px `ai-workflow` glyph in `indigo-60`. A paused task's box is `light-dark(gray-5, gray-20)` with the glyph in `gray-60`, plus a 16px `gray-30` circle (1px `light-dark(white, gray-5)` ring) at bottom −2px, right −6px, holding `pause-filled` at 10px;
  - the title (bold, one line) followed by **PAUSED** (`--unpublished_badge`: 10px Bold, 14px tall, padding 1px 3px, radius 2, 8px after the title, `rgba(fg_max, .7)` on `rgba(fg_low, .13)`);
  - the schedule line (13px/1.38463, `content-sec`), described below;
  - on hover, an ActionsMenu (`showOnlyOnHover`) at the right: **Run task now** (`play`) when the task can run and isn't paused; **Pause task** (`pause`) or **Restart task** (`rotate`); **Edit task** (`edit`); **More actions** (`ellipsis-vertical-filled`).
- **Order** (useAgentTasksExplorer `U`): published tasks first, then by lowercased title. `is_empty` workflows are dropped.
- **States**: while the first page loads, the explorer's twelve SearchWorkflowSkeleton rows (hOG1: a 36px placeholder and two random-width bars). A failed load gives the empty-state row "There was a problem loading tasks." / "Give it another try in a few minutes." with **Retry** (resetQueryAndFetch). No tasks and no search gives **ThirdPartyAgentTasksEmptyState**. A search that matches nothing gives "No tasks found" / "Try a different search term." More pages load as the end of the list comes into view.
- **Empty state** (ThirdPartyAgentTasksEmptyState) without `wfb_agent_tasks_recent_prompts`: `container__eB2B7` (16px padding) around `card__F5pMa` (1px `otl-ter`, radius 8). The card's header is padded 48/24/48, has a `:before` glow (a radial `base-inv-hgl-1` gradient at 30% opacity, blurred 30px, centred on the card's top edge), and centres "Set repetitive tasks on autopilot" (22px Black) over "Add a task to have this agent run a prompt on a schedule." (`content-sec`). With the experiment on, Slack instead shows **AgentTasksEmptyStateHero** (a sample task card, "Put repetitive work on autopilot", Create a Task, How Tasks Work) and "Suggested from your recent prompts". Clack doesn't have that version.
- **Daily run limits** (DailyTaskRunProgressBar, DailyTaskRunLimitAlert) are Slackbot's tasks only; the 3P tab never shows them.

### The schedule line (ZgZs, VVpd, znID, g5+0)

AgentTaskItem calls `n1._(automationInfo, time24, wfb_scheduled_trigger_time_buckets, wfb_schedule_on_last_day_of_month, ms3)`. The output is en-US. Times use toTime (`h:mm A`, or `HH:mm` under the time24 pref). Dates use toCalendarDate (`MMMM Do`, with the year only for a one-off in another year). The zone is dayjs's `z` short name, in the trigger's own time zone.

| Frequency | Line |
|---|---|
| once | `Once on December 1st at 2:00 PM PST` |
| daily | `Daily at 5:30 PM PST`, `Every 3 days at …` |
| weekly | `Mondays at 9:00 AM PST`, `Mondays and Fridays at …`, `Mondays, Tuesdays, Wednesdays, Thursdays, and Fridays at …` (listify), `Mondays every 2 weeks at …` |
| monthly | `Monthly at …`, `First Monday of every month at …`, `Last day of every month at …` |
| yearly | `Yearly on November 23rd at …` |
| time bucket | `Starts tomorrow in the morning and repeats daily` (the bucket, a relative day, then znID's repeat clause) |
| ms3 window | `Daily between 5:30 PM and 5:45 PM PST` |
| not scheduled | the trigger's description |

Event-triggered tasks draw an EventTriggerLabel (`nX.l`) instead. Clack shows the description for those.

## Calls

All go through `createApiMethodFetcher` (G8am), which snake-cases arguments deeply (LYxw `VA`) and sends objects as JSON.

| What | Request | Slack module |
|---|---|---|
| list | `functions.workflows.list {limit: 50, cursor?, filter_options: {name, creation_source_type: "AGENT", creation_source_id: <app>}, sort_options: {column: "date_updated", direction: "desc"}, workflow_builder_only: true}`, reason `automations_gallery` | wBUp `fetchWorkflowsForUser`, /XFr |
| triggers | `workflows.triggers.list {app_ids: <the workflows' app_ids, comma-joined>}`, reason `automations_gallery`; a task's trigger is the first one whose `workflow.app.id` is the workflow's `app_id` | HDp+, evA5, wBUp `formatWorkflowsAndTriggersManagedByYouAPIResonse` |
| Pause task | `functions.workflows.unpublish {workflow_id}`, reason `slackbot-automation-item-pause` | vRD3 `setTaskPublished`, 4HXV |
| Restart task | `functions.workflows.publish {workflow_id}`, reason `slackbot-automation-item-restart` | vRD3, 8b/l |
| Delete task | `workflows.triggers.delete {trigger_id}`, then `functions.workflows.delete {workflow_id}`, both with reason `slackbot-automation-item-delete` | 8ftf, VV9g, 9S8A |
| Run task now | the prompt sent into the agent's DM as the person's own message, through the normal send path (`chat.postMessage`). The open IM is reused; `conversations.open` happens only without one (Cdn9). | EAAO `runPromptFor3pAgent` |

- **Pause/Restart** is optimistic: the store's `is_published` flips at once and is held for 30s after success. A failure puts the old state back with the toast "Something went wrong. Please try again." Only one request per task can be in flight.
- **Run task now** is offered when the task has a prompt that doesn't read `{{inputs.…}}` (WdYh `promptReadsWorkflowInputs`) and isn't paused. Slack then opens the DM's Messages tab with the message highlighted. A failure toasts "Something went wrong. Please try again."
- **More actions** for a 3P task has only **Delete task** (danger, `trash`). It shows when the viewer is one of the workflow's collaborators and the task has no trigger or its app `is_workflow_app`. The confirm is ConfirmDeleteWorkflowModal (variant `automation`): "Permanently delete this task?" / "When you delete this task, it’ll stop working and you won’t be able to use it again." with Cancel and **Confirm**. A failure toasts "Something went wrong. Please try again."; success is announced to screen readers only ("Task deleted"). "Edit task with Slackbot" is for Slackbot's tasks.
- **Edit task** opens `edit-task-modal`. **Add Task** (coQm `useAddTaskMenu`, o6XF) opens `create-task-modal` (zNS2 `handleCreateTask`): `functions.workflows.create {title, creation_source_type: AGENT, creation_source_id}`, a `send_prompt_to_3p_agent` step, `functions.workflows.publish`, then `workflows.triggers.create`. Both modals are **TaskModal**, the full Workflow Builder trigger editor (prompt, ScheduledTriggerForm, FrequencySelect, WeekdaysPicker, TimeBucketSelect, SlackEventTriggerForm), about 190 KB of Slack code in the `slackbot-tasks_prompt-blocks` chunk. Neither is in Clack yet.
- **Task details** (`wfb_agents_can_create_simple_automations_tab_ms3`) makes the row a button into `agent-task-details-view`, with run history from `workflows.taskRuns.list`. The account doesn't have that experiment, so the row isn't clickable there.

## Add Task's requests (handleCreateTask, zNS2; not built)

CreateTaskModal (`gantry-v2-async-create-task-modal`) saves through the `handleCreateTask` thunk. Every call has reason `create-task-modal`. Arguments are snake-cased by `pctx nf` to a set depth, leaving the keys of a step's `input_parameters` and of a trigger's `service.inputs` as they are.

1. `functions.workflows.create {title: <name>, description: "", creation_source_type: "AGENT", creation_source_id: <app id>}` (gCWs `H6`) returns the workflow (`id`, `app_id`).
2. `functions.workflows.steps.add {workflow_id, step: {id: <new uuid>, function_id: send_prompt_to_3p_agent, inputs: {prompt: {value}, agent_id: {value: <app id>}}}, input_parameters?}` (gOZ5 `eD`). Slackbot's own tasks use `send_prompt_to_agent` with the prompt alone. The prompt value is built by fEP6 `E` from the text, the prompt blocks and the enabled skills.
3. `functions.workflows.publish {workflow_id}` (8b/l `Yk`).
4. `workflows.triggers.create {...<trigger config>, name, description: "", workflow: <id>, workflow_app_id, inputs: {}}` (gOZ5 `Ii`). The config is the trigger definition's `toCreateConfig(form)` (75zy `hX`), for the schedule form or the event form. With neither, Slack logs "Cannot create task: no schedule or event provided" and toasts "Something went wrong. Please try again."
5. Then the tab's browser fetches again, and the toast is "Task created successfully.".

When the prompt reads trigger variables and the experiment YwqI `i4` is on, the order changes. The trigger comes first, then WdYh `Hn` resolves the variables against it, then steps 2 and 3 run with the resolved prompt and `input_parameters`. Last, `workflows.triggers.update {trigger: {...definition.toUpdatePayload(trigger), inputs}, name, description: "", workflow}` (1x2R `Y`) maps the inputs. If that update fails, the toast is "Task created, but some of its prompt variables couldn’t be connected.".

Any failure deletes what was made: `workflows.triggers.delete` (VV9g) for a trigger that was created, then `functions.workflows.delete {workflow_id}` (9S8A). EditTaskModal is the same TaskModal (`gantry-v2-async-edit-task-modal`). Its form (prompt, ScheduledTriggerForm with FrequencySelect, WeekdaysPicker and TimeBucketSelect, and SlackEventTriggerForm) is the next slice to read.

## Live updates (boot-render)

- `workflow {workflow_id, subtype}` (**workflowChanged**): for every `agent-tasks`, managed-workflows and workflows browser, a `deleted` subtype removes the item at once; anything else marks the browser stale and it fetches again.
- `hermes_trigger_updated {trigger_id, is_deleted, is_workflow_published}` (**workflowTriggerUpdated**): a deleted or unpublished trigger is dropped from the store, and the trigger, its preview and its workflow are marked stale.
- `feature_usage_updated` feeds the daily run counter (Slackbot only).

Clack's core emits both events as `Event{Type: "agent_task"}`. The tab drops a deleted task on the next frame and reads the list again for everything else.

- `feature_access_policies_updated {feature_access_policies}` (**featureAccessUpdate**): updateFeatureAccessStore (zxoU) merges the event into the stored policies (`{...state, ...event}`), so a policy the event leaves out keeps its value. Clack merges too and leaves the hourly `features.access.policies.list` read on its time.

### The app's profile (apps.profile.get)

The tab rule needs the app's profile (`is_agent_app`, `is_shown_as_agent`). Slack fetches an app it doesn't have through UnknownFetchManager (0mwJ). Each attempt gets 65 s, and only a timeout is tried again, three attempts in all. Any other error, or the last timeout, stores the app as non-existent. The apps refetcher (FftE over p7oS) then marks it fresh with no max age, so every reader gets the non-existent app without a request. That lasts until something marks it stale:

- a slow reconnect: `_maybeResetState` sees the slow connection count move ("Connection count a -> n: marking all as stale"), which marks every app stale;
- `apps_installed` or `apps_changed` (sharedUpsertAppFromImsg), which marks `app.id` stale;
- `apps_uninstalled` (sharedAppUninstalled), which marks `app_id` stale;
- `bot_changed` (botChanged), which marks the bot's `app_id` stale.

A refetch of an app Slack already has swallows its error, and the stored profile stays, fresh again.

Clack's `AppCardOf` (core/app_card.go) follows the same rule. Its fetch runs on the App's context, so one caller's deadline ends only that caller's wait. A failure is remembered for every caller (the tab bar, the app hover card, App details, About) until one of the four events or a slow reconnect forgets it. Both emit `Event{Type: "app_card"}`, so the open DM draws a frame and the tab bar asks again. A failed refetch keeps the last card. The 10-minute refetch of a card that did load is Clack's own; Slack keeps a loaded profile until the same resets.

## Where else tasks show

- The agent DM header button and peek (`feat_agents_dms`), described above.
- **Suggested from your recent prompts** (AgentTasksRecentPrompts, `wfb_agent_tasks_recent_prompts`): prompts asked more than once in the DM, each with "Make it a task". It appears below the task list and in the hero empty state. Not in Clack.
- A task's runs are messages in the agent's DM, sent by the person (Run task now) or by the app's replies. Q0Hh `ju` collects the bot users of AGENT-created workflows. Nothing task-specific shows in Activity or notifications in the cached client code; the runs notify like any DM.

## What Clack builds

- `internal/core/agent_tasks.go`: the tab rule (`AgentTasksTab`, `AgentTasksTabRule`), `AgentTasks` (list, triggers, merge, `SortAgentTasks`), `SetAgentTaskPublished`, `DeleteAgentTask`, `RunAgentTaskNow`, and the `workflow` and `hermes_trigger_updated` handlers. `agent_task_schedule.go` is the schedule line.
- `internal/ui/blockkit/apphome.go`: Tasks in the DM's tab bar in Slack's order. The open tab's icon is now filled for every tab, as Slack's `useTabIconProps` and Clack's channel tab bar already did.
- `internal/ui/views/agent_tasks.go`: the tab body (header, search, rows, hover actions, menu, confirm, skeleton, error, empty card with its glow, paging, live events). `internal/ui/agent_tasks_tab.go` hosts it, and Run task now ends on the DM's Messages tab at the sent prompt.
- `internal/ui/icons`: `workflow-filled`, `play-line` (Slack's outline `play`) and `pause-line` (Slack's outline `pause`), all checked by TestIconsMatchSlack.
- `internal/fakeslack/agent_tasks.go` (`Size.AgentTasks`): Atlas, an agent app with tasks in every state (weekdays, weekly, paused daily, monthly first Monday, once, a prompt that reads `{{inputs.channel_id}}`, and an empty draft), plus every method above, `features.access.policies.list`, `team.prefs.get` `ai_apps_settings`, and the two events.

Not built: Add Task and Edit task (TaskModal), the task details view, the recent prompts and hero empty state, the agent DM header button and peek, Automation Groups (`slackbot_ai_automation_groups`), and event-trigger labels.

## Tests

- core (`agent_tasks_test.go`): the rule with each input off; each gate through the fake (experiment, developer switch, messaging off, not shown as an agent, plan, admin); the list's request shape, order and per-task state; paging; each action's request; the live events; the schedule lines.
- ui (`agent_tasks_tab_test.go`): the tab shows and goes away with the experiment, and an open Tasks tab falls back to Messages; the developer switch, messaging and agent-badge cases; rows, PAUSED and schedule lines as drawn; each state's actions; search and "No tasks found"; Pause and Restart (optimistic, re-sorted), a failed pause put back with the toast, Run task now, Delete task through its confirm; a deleted task gone on the next frame and an unpublished one PAUSED after the refetch; the empty and error states. Shots go to the `bk-agent-shots` artifact (`AGENT_TASKS_SHOTS_DIR`): `agent-tasks-tab-bar`, `-list`, `-row-hover`, `-row-paused-hover`, `-no-match`, `-paused`, `-delete-confirm`, `-empty` and `-error`, each in light and dark.

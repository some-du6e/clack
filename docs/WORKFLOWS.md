# Workflows

How Slack's web client runs and lists workflows, and what Clack does. Source: Slack's web client, build a84662d2d135 (2026-11-08), fetched without cookies from the public CDN (`https://a.slack-edge.com/bv1-13-br/`, chunk names from slack-datamine-plus `build/chunks.txt`). Modules are named by chunk and rspack module id. Clack was tested against `internal/fakeslack` only (`Size.Workflows`, `internal/fakeslack/workflows.go`). No workflow was run, created or edited on a real account.

## Running a workflow

Every way a link trigger starts goes through one thunk, `workflowsTriggersTrip` (gantry-v2-async-client-boot-shared, module `0/Om`).

1. `workflows.triggers.trip {url, context, client_token, run_precheck: true}`. The fetcher snake_cases its arguments deeply (`WH55` → `9AQx.snakeCaseDeep`), so `context` is the caller's `workflowLinkTriggerUIContext` with snake_case keys. The client token is enqueued like a modal's (`h5pZ.generateAndEnqueue`), so a form the workflow opens arrives as `view_opened` with it.
2. On `ok`, a toast (module `Izws`), unless the trip came from the composer or the `workflow_execution_window` experiment is on:
   - "You successfully started a workflow" (`SlackFunctionFormModalConfirmToast`, 2 s);
   - with `is_slow_workflow`: "**You started {trigger name}.** This workflow might take a little longer to run." (`SlowWorkflowStartedToast`), or, with `contains_ai_steps` and `enable_wfb_ai_answer_step` on, "You started a workflow. This workflow has an AI step and may take longer to run." (`AiStepWorkflowStartedToast`).
3. On `pretrip_ack_required` with `pre_trip_info`, the speedbump opens if `shouldShowPrecheckSpeedbump` says so: third-party accounts not connected, missing context or input parameters, risky scopes to acknowledge, AI steps, or a trip from search. Otherwise, and for every other error, the error toast (`SlackFunctionTriggerErrorToast`) shows its text for `error` and `error_messages[0]`: "You don’t have permissions to run this workflow", "Unfortunately you can’t run new workflows in private channels", "This workflow no longer exists.", "Workflow currently unavailable. Contact a workspace owner for help.", and so on, falling back to "Oops, there was an error running that workflow".
4. The speedbump's Start Workflow trips again without `run_precheck` (the modal calls `tripTrigger()` with no arguments).

Contexts, by caller:

| where | context | Slack module |
|---|---|---|
| a `slack.com/shortcuts/Ft…` link in a message | `{location: message, channel_id, message_ts}`, or `{location: markdown_link}` outside a message | `JQmE` (built in `kQRT`'s link) |
| a `workflow_button` element | `{location: button, action_id, block_id, channel_id, message_ts}`; an ephemeral message's button adds the trigger's `customizable_input_parameters` | `C7fj` WorkflowButtonElement |
| a bookmarked workflow | `{location: bookmark, channel_id, bookmark_id}` | boot-render `Jf9W` useChannelWorkflowsExplorer |
| channel details' Automations list | `{location: channel_details, channel_id}` | `2Lul` WorkflowAppItem |
| the `/` command menu | `{location: slash_command, channel_id}` | boot-render `26v2` (autocompleteCommands execute) |

### The speedbump

Chunk `gantry-v2-async-workflow-trigger-speedbump-modal` (module `GcYO`, WorkflowTriggerSpeedbumpModal) chooses one of:

- **Default:** the workflow's title (`trigger.name`, else the workflow's title) and no body. The footer is a right-aligned column (`.p-workflow_trigger_speedbump_modal_footer`: column, `align-items: flex-end`, 80px): "Copy workflow link" when the trigger has a `share_url` (copies it, toast "Workflow link copied", `Zvd/`), then Cancel and ▶ Start Workflow.
- **Scopes:** titled "Review workflow access". "What will the workflow be able to do?" (or "…in {team}?" when another workspace owns it), then up to three `bot_scopes` that have a description, each with a pencil icon (`action_type: do`) or an eye (`view`).
- Third-party accounts ("Connect accounts"), missing context or inputs (conversation, canvas and user pickers), AI resource access, and the multi-step version when several apply.

### Workflow step messages

A workflow step's message (a form's Continue button, approvals) carries `function_execution_id`. Slack's message containers pass it to `blocks.actions` and `blocks.suggestions` (`o9iI`, `Em/L`; the `blocks.actions` error list `RfeG` has the workflow ones: `function_execution_not_running`, `wf_button_expired`, `workflow_run_permission_denied`, `workflow_version_changed`, `single_click_button_in_use`). Slack's client has no copy of its own for those errors.

### The composer's + menu

`useComposerIAFormatter` (boot-render `26v2`) has one workflow row: "Workflow" (`attach-workflow`, the play icon; `bolt` under the composer simplification experiment), hidden in the edit-message composer and without the `hermes__read` feature access. It opens the workflow picker with `source: shortcuts_menu` ("Attach a workflow", Insert). Insert is `attachWorkflowToMessage`: the trigger's name, linked to its `share_url`, at the caret. The menu lists no triggers itself.

## What Clack does

| | Slack (evidence) | Clack before | Clack now |
|---|---|---|---|
| workflow link in a message | trip with message context; Slack's toasts (`0/Om`, `JQmE`, `Izws`) | tripped with message context; invented toasts "Workflow started" / "Couldn't start the workflow: …" | Slack's context, toasts and error texts (`core.TripTrigger`, `blockkit.TripWorkflow`) |
| `workflow_button` | `{location: button, action_id, block_id, channel_id, message_ts}`, inputs only when ephemeral (`C7fj`) | sent `location: message` and always sent inputs as a separate argument | Slack's button context (`core.ButtonTripContext`) |
| speedbump | default or scopes modal, Start without `run_precheck` (`GcYO`) | an invented "Start workflow?" / "This workflow will run with your permissions." confirm; tripped again with `run_precheck=false` | Slack's default and "Review workflow access" modals, Copy workflow link, second trip without `run_precheck`; the other variants open the link in Slack's web client (below) |
| bookmarked workflow | trip with bookmark context (`Jf9W`) | opened the link in the browser | trips with `{location: bookmark}` (`ui.tripBookmark`; bookmarks now keep `shortcut_id`) |
| workflow step messages | `function_execution_id` in `blocks.actions` / `blocks.suggestions` (`o9iI`, `Em/L`) | dropped: messages never kept it | kept on `slack.Message` / `core.Message` and sent by `MessageContainer` |
| forms a workflow opens | `view_opened` with the trip's client token | already shown (blockkit's view stack); the token is issued by `Interactive.ClientToken` | unchanged |
| + menu | "Workflow" opens the picker ("Attach a workflow" / Insert) (`26v2`, `MUtn`) | no row | the row and the picker; Insert puts the name linked to the share URL at the caret |

Tests: `TestTripContexts`, `TestTripTrigger`, `TestTripErrorText`, `TestNeedsSpeedbump`, `TestTriggerInfoAndSearch`, `TestWorkflowStepActions` (internal/core); `TestTripWorkflow`, `TestWorkflowSpeedbump`, `TestWorkflowPicker`, `TestWorkflowShots` (internal/ui/blockkit). The `workflows-shots` workflow uploads `workflow-speedbump-scopes-{light,dark}.png` and `workflow-picker-attach-{light,dark}.png`.

## Not done

- **Third-party auth, missing inputs, AI access and multi-step speedbumps.** These need Slack's OAuth window and its conversation, canvas and user pickers. When Slack would show one, Clack opens the workflow link in the browser, where Slack's web client trips it and shows the speedbump.
- **Toast styling.** Clack's toast is one bold line with no icon, shown for 3.5 s. Slack's success toast lasts 2 s, and the slow-workflow toast has a rocket icon and a bold first sentence.
- **The `hermes__read` gate on the + menu row.** Slack reads it from the `featureAccess` store (`X+JB.G2`), which Clack doesn't load (`features.access.policies.list`). Clack always shows the row.
- **The `/` menu's workflow triggers** (`autocompleteCommands` in `26v2` searches `workflowTriggers` and trips with `location: slash_command`) belong to the composer's autocomplete, which doesn't list them yet (docs/AUTOCOMPLETE-RULES.md).
- **The workflow execution window and status toasts** ("Running workflow step 1 of 3: …", `WorkflowStatusToast` / `WorkflowOutcomeToast` in `Izws`), which follow a run's progress when `workflow_execution_window` is on.
- **Workflow mentions** (rich text `workflow_mention`, `jp/6`: clicking one opens the workflow details view) and **workflow unfurl cards** (`NLQV` WorkflowEntity). Clack has no workflow details view.

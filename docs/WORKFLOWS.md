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

### The channel tab bar's +

`AddTabMenu` (boot-shared `/wV5`) has "Workflow" (the `bolt` icon) after Canvas, List and Link. It is listed only while the channel has no Workflows tab and the viewer may post in the channel (`o/IU`), and it gets an upgrade badge on free plans. It runs `useCreateWorkflow` (`OAZD`): the same picker as the channel menu's, but `addWorkflowBookmarkForChannel` (`Tyab.VV`), which rejects a workflow already bookmarked and sends `link: shortcut_url`. Then the Workflows tab opens.

## "Add a workflow" and the workflow picker

The channel menu's "Add a workflow" (`getWorkflowTemplate`, boot-render module `w7ek`) doesn't open Workflow Builder. It opens the **workflow picker** (`MUtn` WorkflowPicker): `source: channel_workflows`, `frecency: true`, `includeAutomatedTriggers: true`. The row is missing in DMs and group DMs (`aK5v.JM`), in private channels the viewer isn't in (`Zp && !bl`), and for frontline workers. When a workflow is picked, `onInsertWorkflow` calls `addBookmarkForChannel` (`Tyab.eV`, fetcher `YQEj`): `bookmarks.add {channel, title: trigger name or id, type: shortcut, shortcut_id}`. Then it opens the channel on its Workflows tab (`jpad` toChannel with `tabId: workflows`). A failed add shows nothing: the promise has no catch.

The picker, by source (`shortcuts_menu` from the composer, `channel_workflows`, `channel-primary-action`):

| | title | primary |
|---|---|---|
| `shortcuts_menu` | Attach a workflow | Insert |
| `channel-primary-action` | Feature workflow | Feature |
| others | Add workflow | Add |

- 650 wide (`p-workflow_picker__modal--new_style`): a 1px rule under the header and over the footer.
- The search box "Search by workflow name or keyword" (`WorkflowPickerFilter`, 16px margin, search icon, autofocus).
- A 300px list of `WorkflowListItem` rows (`whkB`): 12px 25px padding, the app icon (36px) 12px before the title (15px) over the description (13px at 70%); the picked row white on #1264a3, a hover row on the min-solid background.
- The footer: "Create a Workflow" at the left (launchWorkflowBuilder `create_empty`: Workflow Builder in its own 1024×700 window at `/workflow-builder/{team}/workflow/create-empty`, `ThpL`/`CrVT`), Cancel, and the primary button, enabled once a row is picked.
- The search is the client searcher over `workflowTriggers`. Its remote part is `search.autocomplete.triggers {query, current_channel_id, strict_exposure_rules: true, trigger_types}` (QcwJ fetchAndUpsertTriggersWithQuery, debounced 500 ms leading and trailing): every trigger type with automated triggers, `["shortcut"]` without.
- With no workflows at all, the empty state: no search box, the illustration (`workflow-picker-empty-state.svg`) or, where templates may be browsed from `channel_workflows`, the workflow templates; "Create From Scratch" in the footer.

## The channel's Workflows tab

`useChannelWorkflowsExplorer` (boot-render `Jf9W`) builds the tab's explorer rows, drawn in `.workflows_tab_view`: the base-sec background, 20px top padding, results at most 968px wide (`.p-explorer_results__contained-result`).

- **Featured workflows:** the channel's `properties.channel_workflows` (`TPuo`), each entry's trigger from the trigger previews store (`EXgk`: `workflows.triggers.preview {trigger_ids: "Ft1,Ft2"}`). A preview whose error is `access_denied` stays and takes the entry's title (`MUnG`); other errors are dropped (`nRAK` isTriggerWithNonAccessDeniedError). With rows: the "Featured workflows" heading (`.heading`, 13/18 bold at 70%, 24px above), "Feature up to 15 workflows, so people can use them instead of sending a message.", and "Feature workflow" for those who may manage the channel's workflows, disabled at 15 (`DE+5`). It opens the picker as `channel-primary-action` (link triggers, the featured ones excluded), and the pick goes to `setChannelWorkflowActions` (`uJYJ`): `workflows.featured.add {trigger_ids, channel_id}`. Without rows: the "Feature a workflow" card.
- **Bookmarked workflows:** the channel's shortcut bookmarks (`HR6W`, `ADp7` isShortcutBookmark: `shortcut`, `auto_gen_shortcut`) through the same previews. Heading "Bookmarked workflows" / "Help people find and use workflows relevant to this channel." with "Add workflow" (the picker as `channel_workflows`, the bookmarked ones excluded, then `addWorkflowBookmarkForChannel` with the shortcut URL). Without rows: the "Bookmark a workflow" card.
- **Who may manage** (`HRod` canUserManageChannelWorkflows): a member who may post (`o/IU`), not a guest (`wY11`), in a channel no other org hosts (`2ES1`), with the channel permission `MANAGE_CHANNEL_WORKFLOWS` (edge `permissions/info`; the enum `tzxv` maps each name to itself).
- **Rows** (the explorer's `WorkflowItem`, boot-shared `31Gm`): a 1px-bordered card, rows 16px padded, 12px corners at the ends (`.p-search_workflows_item`, `--list--first/--last`). Each has the 36px icon (radius 12), the bold name over the 13px description (`--sk_foreground_max_solid`), and on hover ▶ "Start workflow" for a published link trigger. The Start button runs onExecute with the explorer's context `{location: bookmark, channel_id, bookmark_id: the trigger id}` and uiPage `channel_workflows`. `AccessDeniedWorkflowItem` is the lock in a 36px grey box, the title (or "Private workflow") and "Only certain people can use this workflow".
- **Cards** (`WorkflowTabCTACard`): 24px above, bordered, radius 12, padding 16; the star or bookmark icon in a 36px base-ter box; the bold title over the 13px subtitle; the outline button, disabled without the permission.
- **Footer** (`WorkflowsTabFooterContent`): "Got repetitive tasks? Try workflows." then Browse templates (with template access), Build from scratch (for those who may create workflows) and Get more info, with "·" between them.

## Channel details > Automations

The tab's body is `AutomationsSettings` (chunk `gantry-v2-async-client-channel-settings-automations-view`, module `2MVc`). For a channel (not a DM: `shvH`, get-integrations-sections) it renders `WorkflowsSettings`, which merges two stores:

1. **workflowApps** (`A3kQ` fetchAndUpsertFirstNWorkflowAppsForChannel, `FQkZ` selectors): edge `users/list {channels: [C], present_first: true, filter: "workflows NOT deactivated", count: 100}` finds the channel's workflow bots. `functions.workflows.findWorkflowForBotUsers {user_ids, channel_id}` returns their `workflow_apps` (`app_id`, `workflow_id`, `share_url`, `app_details.icons`), and `functions.workflows.list {workflow_ids, access_level: all, limit}` upserts those workflows. Each row's title and description come from the share URL's trigger (`workflows.triggers.info {trigger_id, include_workflow_details}`), else the workflow's (`jsMl` createShortcutFromTriggerAndWorkflow).
2. **channelActions** (`s4p6`, `TtuD`): `apps.actions.listForResource {resource_type: channel, resource_id}`, the channel's legacy workflows.

The list is the workflow apps, then the channel actions sorted by name, at most 100. With rows: a ListField (`Z8ln`, `p-field--list`) titled "Published Automations". Its title link "Manage" shows when there are channel actions and the viewer is a member. Rows (`2Lul` WorkflowsSectionItemV2) are:

- WorkflowAppItem → `ARyf` WorkflowItem: 18px top and bottom padding, the app icon (36px), the bold title over the description (13px, tertiary). A row without both draws nothing. Clicking a link trigger trips it with `{location: channel_details, channel_id}` (reason `trip-trigger-from-channel-details-integrations-click`); other triggers open the workflow details modal. On hover: a ▶ "Start workflow" icon button and a ⋮ menu (View details, Copy workflow link, Forward workflow, Edit workflow for collaborators).
- AppActionItem: the app's `image_64`, the bold name with the WORKFLOW badge (`2C9J` BaseAppBadge `isWorkflow`), the description, and ▶ "Start workflow", which runs `runChannelAction` (`4c9s`): `apps.actions.run {action_id, type: channel_action, app_id, channel, client_token}`, then "You used the workflow **{name}**", or "Sorry, that didn’t work. Try again?" on failure.

The sectioned footer holds the members-only "Add Automation" (it opens Workflows › Managed by you), "Add Automation as Member" (with `wfb_add_workflow_to_channel_via_channel_details_modal`) and Learn More (help article 16583775096083).

Without rows, the empty state: `/img/ia-details/workflows-empty.png`, the title and description, then the same buttons with "Go to Your Workflows" in place of "Add Automation". The `channel_details_agents_apps_tab` experiment picks the copy: "Let workflows take tasks off your plate" / "Workflow Builder lets you automate your team’s daily routines and everyday processes, all without writing a single line of code." Without it: "Supercharge your channel" / "There’s a few automations that will make your life easier. Set them up in a flash." A team below Workflow Builder's plan sees an upsell instead.

## The workflow details view

"View workflow details" on a workflow's message (2rcC t7, a workflow
bot's message with a `trigger_id`) opens Slack's WorkflowDetails view
(client-workflow-details `HRfd`, the viewType WorkflowDetails route of
openWorkflowDetailsModalCircular) in the flexpane (`ui/workflow_details.go`,
secondary kind `secWorkflow`):

- Data: the triggers store's workflows.triggers.info with
  include_workflow_details (core `TriggerInfo`: `Steps`, `Collaborators`,
  `IsWorkflowApp`), then the workflows store's functions.workflows.get
  (core `Workflow`), merged as createShortcutFromTriggerAndWorkflow (jsMl)
  does. A trigger error draws "Workflow not found" (trigger_not_found),
  "Permission denied" (access_denied) or "Something’s gone wrong".
- Flexpane "Workflow" with its close button; WorkflowDetailsInfo (padding
  24, rows 8px apart, an otl-ter line under it): the icon (36), the title
  (18px bold), the description, an automated trigger's type line
  (getTriggerTypeIcon / getTriggerTypeLabel, hermes_shortcuts), ▶ Start
  Workflow across the panel for a link trigger (tripped with
  `FlexpaneTripContext`, uiPage workflow_details_view), the VIP button for
  the workflow's bot user ("Add to VIP" / "Remove from VIP", core
  `AddVIP` / `RemoveVIP`) and the ⋮ (MoreActionsMenu isDetailsView: Copy
  workflow link with "Workflow link copied", Copy workflow ID).
- WorkflowDetailsTab: Information ("Managed by" ShortcutByline's
  LinkifyMembers within 30 characters, "Created by" the owning team when
  it isn't the viewer's) and Steps: the trigger step (aquarium; "When
  someone clicks the workflow link" with right-click for a link), an arrow
  down, then renderStepsWithBranches: a level's steps but loops as one
  entity list (8uOE icons, Eu1t themes: the 32px tile's -0 fill and -60
  icon, charcoal plain), each switch's cases after it as tinted boxes
  ("Branch A"… / "Fallback", workflow_builder; the case's display_config
  colour, gray by default, tomato as paprika) and loops in an outlined
  "Repeat" box with the campfire tile.
- Tests and shots: `TestWorkflowDetailsPaneLikeSlack`,
  `TestMessageActionsHooksWired` (ui), `TestWorkflowDetailsData` (core),
  `TestMessageMenuWorkflowRowsLikeSlack` (popups); menu-keys-shots
  `workflow-details-{light,dark}.png`, `workflow-details-error-light.png`.
- Left out: Share and Edit (Slack's share-workflow dialog and Workflow
  Builder), the ⋮ menu's builder rows (edit, unpublish, copy, export, add
  to channel), Connections (third-party steps' accounts), the VIP badge
  after the title, the "and N others" managers dialog, and an event
  trigger's type line (Clack keeps no event type). The other ways in
  (the Workflows tab's rows and ⋮ View details, Automations' non-link
  triggers and ⋮, workflow mentions) still don't open it.

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
| tab bar + | "Workflow" while there's no Workflows tab; picker, bookmark with the shortcut URL, no duplicates (`/wV5`, `OAZD`) | Canvas and List only | the row with Slack's rule (`ui.addTabWorkflowItems`); the bookmark sends the link and skips one already there |
| channel ⋮ "Add a workflow" | the workflow picker; the pick is bookmarked (`bookmarks.add` type shortcut) and the channel's Workflows tab opens (`w7ek`, `MUtn`, `Tyab`) | toast "Workflows aren't supported in Clack yet" | the picker (`channel_workflows`, automated triggers), `bookmarks.add` with Slack's arguments, then the channel's Workflows tab opens once the reloaded tab bar has it; the row is gone in DMs, group DMs and private channels the viewer isn't in (`popups/workflows.go`, `ui.addWorkflow`) |
| the channel's Workflows tab | featured and bookmarked workflows, cards, footer, ▶ Start (`Jf9W`, `31Gm`, `MUnG`, `HR6W`, `EXgk`, `HRod`, `uJYJ`) | the tab bar toasted "This tab opens in Slack" | the tab opens in the channel's body with Slack's sections, rows, placeholder, cards and footer; ▶ trips with the explorer's context; Feature workflow / Add workflow run Slack's calls (`ui/workflows_tab.go`, `core.ChannelWorkflowsTab`, `core.FeatureWorkflow`) |
| channel details > Automations | WorkflowsSettings: workflow apps and channel actions as above (`2MVc`, `2Lul`, `ARyf`, `A3kQ`, `s4p6`) | an invented "Workflows / Automate routine tasks… / Add a Workflow" card whose button toasted "Not supported yet" | the same calls and merge, "Published Automations" rows (icon, title, description, WORKFLOW badge, ▶ on hover), a click trips (`channel_details`) or runs the legacy workflow with Slack's toasts, Learn More, Slack's empty state copy (`core.ChannelWorkflowApps`, `core.ChannelActions`, `views/details_automations.go`) |

Tests: `TestTripContexts`, `TestTripTrigger`, `TestTripErrorText`, `TestNeedsSpeedbump`, `TestTriggerInfoAndSearch`, `TestWorkflowStepActions`, `TestChannelWorkflows` (internal/core); `TestTripWorkflow`, `TestWorkflowSpeedbump`, `TestWorkflowPicker`, `TestWorkflowShots` (internal/ui/blockkit); `TestAutomationsTab`, `TestAutomationsEmpty` (internal/ui/views); `TestAddWorkflowChannelMenu`, `TestWorkflowBookmarkTrips`, `TestWorkflowsTab` (internal/ui); `TestAddWorkflowBookmark`, `TestChannelWorkflowsTab` (internal/core). The `workflows-shots` workflow uploads `workflow-speedbump-scopes-{light,dark}.png`, `workflow-picker-attach-{light,dark}.png`, `automations-tab-{light,dark}.png`, `automations-empty-{light,dark}.png` and `workflows-tab-light.png`.

## Not done

- **Workflow Builder.** "Create a Workflow" opens Slack's Builder page in the browser, the page Slack's own window loads. The Builder's editor (chunk `client-workflow-editor-view`, 13,000 lines beautified) and template gallery (`client-workflow-templates-view`, `workflows.templates.*`) aren't ported: "Add a workflow" doesn't lead to them.
- **The Workflows tab's education banner** (megaphone notifications), **its rows' ⋮ menu** (View details, Copy workflow link, Forward, Remove…) and **row clicks** (Slack opens the workflow details view, which Clack now has from the message menu but doesn't open from here), **"Browse templates"** (Slack's template gallery), and **trigger summaries** in a row's description (a scheduled or event trigger's "Runs every Monday…", `iN.WL`).
- **Picker details.** Frecency sorting (Slack's searcher ranks by local frecency; Clack keeps the server's order); the empty state's illustration (an SVG, which Clack can't draw: its space stays empty) and templates list; the "Add automation as member" mode.
- **Third-party auth, missing inputs, AI access and multi-step speedbumps.** These need Slack's OAuth window and its conversation, canvas and user pickers. When Slack would show one, Clack opens the workflow link in the browser, where Slack's web client trips it and shows the speedbump.
- **Toast styling.** Clack's toast is one bold line with no icon, shown for 3.5 s. Slack's success toast lasts 2 s, and the slow-workflow toast has a rocket icon and a bold first sentence.
- **The `hermes__read` gate on the + menu row.** Slack reads it from its `featureAccess` store (`X+JB.G2`, filled by `updateFeatureAccessStore`, `HhLp`), which Clack doesn't load. Clack always shows the row.
- **The `/` menu's workflow triggers** (`autocompleteCommands` in `26v2` searches `workflowTriggers` and trips with `location: slash_command`) belong to the composer's autocomplete, which doesn't list them yet (docs/AUTOCOMPLETE-RULES.md).
- **The workflow execution window and status toasts** ("Running workflow step 1 of 3: …", `WorkflowStatusToast` / `WorkflowOutcomeToast` in `Izws`), which follow a run's progress when `workflow_execution_window` is on.
- **Automations: the buttons and menus that lead to views Clack lacks.** These are "Go to Your Workflows" / "Add Automation" / "Manage" (Slack's Workflows › Managed by you view), the ⋮ menu's View details / Forward workflow / Edit workflow, a non-link trigger's click (the workflow details modal), "Add Automation as Member" (a second picker mode, `searchWorkflowsToAddAsChannelMember` / `addWorkflowAsChannelMember`, behind `wfb_add_workflow_to_channel_via_channel_details_modal`), and the empty state's illustration (served from app.slack.com only, which Clack doesn't fetch with the session). The free-plan upsell isn't drawn either: Clack doesn't know the team's plan.
- **Workflow mentions** (rich text `workflow_mention`, `jp/6`: clicking one opens the workflow details view, which Clack has but doesn't open from a mention yet) and **workflow unfurl cards** (`NLQV` WorkflowEntity).

package ui

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"log"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/f32"
	"gioui.org/gesture"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/logtag"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/blockkit"
	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/views"
)

// Workflow wiring (docs/WORKFLOWS.md). Running a workflow is blockkit's
// (TripWorkflow: Slack's workflowsTriggersTrip, its toasts and speedbump);
// this file hands it the shell's workflow entry points.

func init() {
	composer.WorkflowItem = composerWorkflowItem
	views.TripWorkflow = func(h views.Host, link string, tc core.TripContext, uiPage string) {
		if bh, ok := h.(blockkit.Host); ok { // channel details > Automations
			blockkit.TripWorkflow(bh, link, tc, uiPage)
		}
	}
	popups.AddWorkflow = addWorkflow
	blockkit.TripToasts = func(h blockkit.Host, id string, res core.TripResult, err error) bool {
		a, ok := h.(*App)
		if ok {
			a.tripToast(id, res, err)
		}
		return ok
	}
}

// addWorkflow is the channel menu's "Add a workflow" (getWorkflowTemplate,
// w7ek): the workflow picker with source channel_workflows and automated
// triggers; the picked one is bookmarked in the channel (bookmarks.add,
// type shortcut) and the channel opens.
func addWorkflow(h popups.Host, ch string) {
	a, _ := h.(*App)
	blockkit.OpenWorkflowPicker(h, blockkit.WorkflowPickerOpts{Source: blockkit.PickerChannelWorkflows, ChannelID: ch, IncludeAutomated: true,
		OnInsert: func(t core.WorkflowTrigger) {
			if a != nil {
				a.addWorkflowBookmark(ch, t, "")
			}
		}})
}

// addTabWorkflowItems is the tab bar "+" menu's "Workflow" (Slack's
// AddTabMenu, boot-shared /wV5: the bolt icon), listed while the channel
// has no Workflows tab and the viewer may post in it. Its picker is
// useCreateWorkflow's (OAZD): the same as the channel menu's, but the
// bookmark carries the trigger's shortcut URL and a workflow already
// bookmarked isn't added again (addWorkflowBookmarkForChannel).
func (a *App) addTabWorkflowItems(ch string) []MenuItem {
	c := a.Core()
	if c == nil || !c.PostingIn(ch).CanPost() {
		return nil
	}
	for _, t := range a.channelTabs(ch) {
		if t.icon == "workflow" {
			return nil
		}
	}
	return []MenuItem{{Label: "Workflow", Icon: "lightning", Do: func() {
		blockkit.OpenWorkflowPicker(a, blockkit.WorkflowPickerOpts{Source: blockkit.PickerChannelWorkflows, ChannelID: ch, IncludeAutomated: true,
			OnInsert: func(t core.WorkflowTrigger) {
				if a.data != nil {
					for _, bm := range a.data.bookmarks[ch] {
						if bm.ShortcutID == t.ID {
							return // "Cannot add already bookmarked workflow to bookmarks"
						}
					}
				}
				a.addWorkflowBookmark(ch, t, t.ShortcutURL)
			}})
	}}}
}

// addWorkflowBookmark bookmarks t in ch (link: the tab bar's shortcut URL,
// "" for the channel menu's), then opens the channel's Workflows tab with
// the tabs reloaded (Slack's toChannel with tabId workflows). Slack shows
// nothing when bookmarks.add fails (the promise has no catch); Clack logs
// it.
func (a *App) addWorkflowBookmark(ch string, t core.WorkflowTrigger, link string) {
	if a.data == nil || a.data.core == nil {
		return
	}
	c := a.data.core
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, err := c.AddWorkflowBookmark(ctx, ch, t, link)
		a.Post(func() {
			if err != nil {
				log.Printf("workflows: add %s to %s: %v", t.ID, logtag.ID(ch), err)
				return
			}
			a.Navigate("c/" + ch)
			delete(a.data.fetched, ch)
			a.data.opened(ch)
			a.openWorkflowsTab(ch) // workflows_tab.go: selected once the tab bar has it
		})
	}()
}

// composerWorkflowItem is the + menu's "Workflow" row (Slack's
// useComposerIAFormatter: attach-workflow, the play icon): the workflow
// picker titled "Attach a workflow", whose Insert puts the workflow's
// name linked to its share URL at the caret (attachWorkflowToMessage).
func composerWorkflowItem(h popups.Host, ch string, insert func(text, url string)) *popups.MenuItem {
	return &popups.MenuItem{Label: "Workflow", Icon: "play", Do: func() {
		blockkit.OpenWorkflowPicker(h, blockkit.WorkflowPickerOpts{Source: blockkit.PickerShortcutsMenu, ChannelID: ch,
			OnInsert: func(t core.WorkflowTrigger) {
				if t.Name == "" || t.ShareURL == "" {
					return // Slack inserts nothing without both
				}
				insert(t.Name, t.ShareURL)
			}})
	}}
}

// tripBookmark runs a workflow bookmark (type shortcut, or any bookmark
// whose link is a workflow link) the way Slack's bookmarked workflows do:
// its trigger with location bookmark. It reports whether bm was one.
func (a *App) tripBookmark(ch string, bm core.Bookmark) bool {
	if blockkit.IsWorkflowLink(bm.Link) {
		blockkit.TripWorkflow(a, bm.Link, core.BookmarkTripContext(ch, bm.ID), "channel_workflows")
		return true
	}
	if bm.Type != "shortcut" || bm.ShortcutID == "" || a.data == nil || a.data.core == nil {
		return false
	}
	// a shortcut bookmark without its link: the trigger has it
	c := a.data.core
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		t, err := c.Interactive().TriggerInfo(ctx, bm.ShortcutID)
		a.Post(func() {
			if err != nil || t.ShortcutURL == "" {
				a.workflowErrorToast(core.ErrCode(err), "")
				return
			}
			blockkit.TripWorkflow(a, t.ShortcutURL, core.BookmarkTripContext(ch, bm.ID), "channel_workflows")
		})
	}()
	return true
}

// ---- workflow mentions (richtext workflow_mention.go) ----

// wfMentions is the triggers store a workflow mention reads (SzL7 bn
// with fetch): a trigger already looked up, else workflows.triggers.info
// once per trigger; a failed lookup is kept as its error, as Slack stores
// {id, error}. When one lands the visible messages are read again.
type wfMentions struct {
	a *App
	c *core.App

	mu        sync.Mutex
	got       map[string]richtext.WorkflowSlug
	asked     map[string]bool
	refreshAt bool // a refresh of the visible messages is queued
}

func (m *wfMentions) slug(id string) (richtext.WorkflowSlug, bool) {
	m.mu.Lock()
	s, ok := m.got[id]
	ask := !ok && !m.asked[id]
	if ask {
		m.asked[id] = true
	}
	m.mu.Unlock()
	if ok || !ask {
		return s, ok
	}
	if t, ok := m.c.Interactive().Trigger(id); ok && t.Error == "" {
		s = richtext.WorkflowSlug{Title: t.WorkflowTitle, Icon: t.Icon}
		m.mu.Lock()
		m.got[id] = s
		m.mu.Unlock()
		return s, true
	}
	go m.fetch(id)
	return s, false
}

func (m *wfMentions) fetch(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	t, err := m.c.Interactive().TriggerInfo(ctx, id)
	s := richtext.WorkflowSlug{Title: t.WorkflowTitle, Icon: t.Icon}
	if err != nil {
		if s.Error = core.ErrCode(err); s.Error == "" {
			s.Error = "error"
		}
	}
	m.mu.Lock()
	m.got[id] = s
	queue := !m.refreshAt
	m.refreshAt = true
	m.mu.Unlock()
	if !queue {
		return
	}
	go func() {
		time.Sleep(150 * time.Millisecond) // one refresh for the mentions landing together
		m.mu.Lock()
		m.refreshAt = false
		m.mu.Unlock()
		m.a.Post(func() {
			m.a.refreshVisible()
			m.a.Invalidate()
		})
	}()
}

// OpenWorkflowMention is messages.WorkflowMentionOpener: a click on a
// workflow mention in m opens the workflow details view
// (openWorkflowDetailsModalCircular) for its trigger. Disabled mentions
// (hidden or deleted workflows) draw no link, so they never get here.
func (a *App) OpenWorkflowMention(m *core.Message, u string) bool {
	if m == nil {
		return false
	}
	trig := richtext.WorkflowMentionTrigger(m.Blocks, u)
	if trig == "" {
		return false
	}
	a.OpenWorkflowDetails(trig, m.Channel)
	return true
}

// wfMentionsCur is the resolver installed in richtext (the last App up).
var wfMentionsCur atomic.Pointer[wfMentions]

// startWorkflowWatch starts what follows the workspace's workflows while
// c is up: the mentions' triggers and the realtime events behind the run
// status toasts. It returns the stop.
func (a *App) startWorkflowWatch(c *core.App) func() {
	if c == nil {
		return func() {}
	}
	m := &wfMentions{a: a, c: c, got: map[string]richtext.WorkflowSlug{}, asked: map[string]bool{}}
	wfMentionsCur.Store(m)
	richtext.SetWorkflowSlugs(m.slug)
	evs, stopRaw := c.SubscribeRaw()
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case ev, ok := <-evs:
				if !ok {
					return
				}
				switch ev.Type {
				case "workflow_execution_step_started", "workflow_execution_outcome_changed":
					typ, raw := ev.Type, ev.Raw
					a.Post(func() { a.workflowRunEvent(typ, raw) })
				}
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			close(done)
			stopRaw()
			if wfMentionsCur.CompareAndSwap(m, nil) {
				richtext.SetWorkflowSlugs(nil)
			}
		})
	}
}

// ---- Slack's workflow toasts (Izws) ----

// wfFnNS is the trip toasts' namespace (slack_functions).
var wfFnNS = i18n.NS("slack_functions")

// tripToast is workflowsTriggersTrip's toast (0/Om) in Slack's Toast
// (c5oy / 4HH1: bottom right, 6 s unless it says): the error toast, else,
// while the workflow execution window is off, "You successfully started
// a workflow" (SlackFunctionFormModalConfirmToast, 2 s), the bolt
// AiStepWorkflowStartedToast for a slow workflow with AI steps
// (enable_wfb_ai_answer_step) or the rocket SlowWorkflowStartedToast.
// With the window on (workflow_execution_window, on for the account on
// 2026-11-15) the run's status toasts follow it instead.
func (a *App) tripToast(id string, res core.TripResult, err error) {
	if err != nil {
		code, sub := "", ""
		var te *core.TripError
		if errors.As(err, &te) {
			code, sub = te.Code, te.SubType
		}
		a.workflowErrorToast(code, sub)
		return
	}
	c := a.Core()
	if c == nil || c.Experiment("workflow_execution_window") == "on" {
		return
	}
	switch {
	case !res.Slow:
		a.ToastParts(wfFnNS.RT("You successfully started a workflow"), "", color.NRGBA{}, 2*time.Second)
	case res.AISteps && c.Experiment("enable_wfb_ai_answer_step") == "on":
		// two <div>s: the bold line over the other
		a.ToastParts([]i18n.Part{{Text: wfShortNS.T("You started a workflow."), Tag: "strong"},
			{Text: "\n" + wfShortNS.T("This workflow has an AI step and may take longer to run.")}}, "bolt", color.NRGBA{}, 6*time.Second)
	default:
		var parts []i18n.Part
		if t, ok := c.Interactive().Trigger(id); ok && t.Error == "" && t.Name != "" {
			parts = wfShortNS.RT("<strong>You started {workflowTitle}.</strong> This workflow might take a little longer to run.", "workflowTitle", t.Name)
		} else {
			// Slack's t(), not rt(): the tags show as typed
			parts = []i18n.Part{{Text: wfShortNS.T("<strong>You started a workflow.</strong> This workflow might take a little longer to run.")}}
		}
		a.ToastParts(parts, "rocket", color.NRGBA{}, 6*time.Second)
	}
}

// workflowErrorToast is SlackFunctionTriggerErrorToast (6 s): its text
// for the error and subtype (core.TripErrorText), the restrictions help
// article linked for away_user_home_only_workflow, and the bold line over
// "Contact a workspace owner for help." for the plan errors.
func (a *App) workflowErrorToast(code, sub string) {
	const d = 6 * time.Second
	switch {
	case code == "access_denied" && sub == "away_user_home_only_workflow":
		parts := wfFnNS.RT("People from external orgs can’t use this workflow. <a>Learn more about workflow restrictions</a>.")
		for i := range parts {
			if parts[i].Tag == "a" {
				parts[i].Tag = "link" // HelpLink articleId 0xd8057515213
			}
		}
		a.ToastLink(parts, slackHelp("14844871922195"), d)
	case code == "not_opted_into_overages" || code == "reached_upper_spend_limit" || code == "free_team_not_allowed":
		parts := wfFnNS.RT("<strong>Workflow currently unavailable.</strong> <br></br>Contact a workspace owner for help.")
		for i := range parts {
			if parts[i].Tag != "strong" { // rt drops the empty <br>: the line break
				parts[i].Text = "\n" + strings.TrimLeft(parts[i].Text, " ")
				break
			}
		}
		a.ToastParts(parts, "", color.NRGBA{}, d)
	default:
		a.ToastParts([]i18n.Part{{Text: core.TripErrorText(code, sub)}}, "", color.NRGBA{}, d)
	}
}

// ---- the workflow execution window's toasts ----
//
// With workflow_execution_window on, Slack follows a run from realtime
// events (boot-render's RTM handlers, Izws):
//
//   - workflow_execution_step_started (unless the run's toast was
//     dismissed): WorkflowStatusToast, replacing the last toast: the
//     workflow's icon (24, a #0000004d scrim and a small white spinner)
//     when the workflows store has the workflow, else a large blue
//     spinner; "{n}/{total}" (content-inv-sec) unless the workflow
//     branches; the step's display title (getWorkflowStepDisplayTitle:
//     a builtin step's sentence, else "In progress: {stepTitle}"); the
//     caret-up-down button ("Open workflow details": the details view);
//     and the close button ("Dismiss and continue running workflow").
//     .workflow_status_toast pads it 16px top and bottom; its row is 8px
//     apart, at most 300 wide, on 24px lines. It fades after Toast's 6 s
//     unless the step is an AI answer or a channel summary.
//   - workflow_execution_outcome_changed for a run whose step came and
//     whose toast wasn't dismissed, with a final status (SUCCESS, ERROR,
//     EXPIRED, CANCELED, ABANDONED): WorkflowOutcomeToast, the workflow's
//     icon (24) or check-circle / warning, "Workflow complete", "Workflow
//     failed" or "Workflow canceled", and the close button ("Dismiss
//     notification").
//
// Both sit 60px up from the window's bottom right (offsetY 60).
//
// Left out: the step titles' two experiment variants ("Summarizing
// channels", wfb_back_to_basics_jalapeno_release; "Sending summary to
// channel", slackforce_scheduled_summary_to_channel), which core doesn't
// read yet, and the screen reader's announcement.

// wfStepTitles is getWorkflowStepDisplayTitle's table: builtin step
// callback ids (M3bu oW) to their sentences (workflow_builder).
var wfStepTitles = map[string]string{
	"add_pin":                           "Pinning a message",
	"remove_pin":                        "Unpinning a message",
	"canvas_create":                     "Creating a channel canvas",
	"channel_canvas_create":             "Creating a channel canvas",
	"create_channel":                    "Creating a channel",
	"test_user_permission":              "Testing user permission",
	"add_user_to_usergroup":             "Adding people to a user group",
	"remove_user_from_usergroup":        "Removing someone from a user group",
	"archive_channel":                   "Archiving a channel",
	"unarchive_channel":                 "Unarchiving a channel",
	"change_user_status":                "Changing status",
	"change_client_color_scheme":        "Setting client color scheme",
	"create_usergroup":                  "Creating a user group",
	"delay":                             "Delaying this workflow",
	"invite_user_to_channel":            "Adding people to a channel",
	"placeholder":                       "Running placeholder step",
	"rename_channel":                    "Renaming a channel",
	"send_ephemeral_message":            "Sending an \"only visible to you\" message",
	"send_message":                      "Sending a message to a channel",
	"turnoff_dnd":                       "Turning off DND",
	"turnon_dnd":                        "Turning on DND",
	"canvas_update_content":             "Updating a canvas",
	"canvas_update_content_v2":          "Updating a canvas",
	"canvas_update_variables":           "Updating canvas variables",
	"update_channel_topic":              "Updating the channel topic",
	"open_form":                         "Collecting info in a form",
	"send_dm":                           "Sending a message to a person",
	"send_to_my_sales_home":             "Sending to My Sales Home",
	"send_to_my_salesforce_app":         "Sending to My Salesforce App",
	"send_to_record_channel":            "Sending a message to a Salesforce channel",
	"send_to_sales_home":                "Sending to Sales Home",
	"send_to_salesforce_app":            "Sending to Salesforce App",
	"send_summary_to_my_sales_home":     "Sending summary to My Sales Home",
	"send_summary_to_sales_home":        "Sending summary to Sales Home",
	"reply_in_thread":                   "Replying to a message in thread",
	"share_canvas":                      "Sharing a canvas",
	"list_add_record":                   "Adding an item to a list",
	"summarize_channel":                 "Summarizing public channels",
	"remove_reaction":                   "Removing a reaction from a message",
	"search_summary":                    "Summarizing search results",
	"lookup_record_channel":             "Finding or creating a Salesforce channel",
	"create_record_channel":             "Creating a Salesforce channel",
	"ask_agentforce":                    "Asking Agentforce",
	"ai_answer":                         "Generating AI response",
	"add_agent_to_channel":              "Adding agent to channel",
	"lookup_entities":                   "Creating object collection",
	"add_bookmark":                      "Adding a bookmark",
	"add_channel_manager":               "Adding a channel manager",
	"add_channel_to_channel_section":    "Adding a channel to a section",
	"add_channel_list_to_section":       "Adding channels to a section",
	"channel start message":             "Sending a channel start message",
	"create_channel_section":            "Creating a channel section",
	"find_channels_by_keywords":         "Finding channels by keywords",
	"conditionally_move_channels":       "Moving channels conditionally",
	"move_channels_by_channel_metadata": "Moving channels by metadata",
	"select_channels":                   "Selecting channels",
	"add_reaction":                      "Adding a reaction to a message",
	"copy_list":                         "Copying a list",
	"create_list":                       "Creating a list",
	"delete_list_record":                "Deleting a list item",
	"read_list_record":                  "Reading a list item",
	"read_all_list_records":             "Reading list items",
	"update_list_record":                "Updating a list item",
	"lists_activity_feed":               "Generating list activity feed",
	"share_list_channels":               "Sharing a list to channels",
	"share_list_users":                  "Sharing a list with people",
	"share_list":                        "Sharing a list",
	"send_task_list_alert":              "Sending a task list alert",
	"create_items_due_summary":          "Creating items due summary",
	"share_canvas_in_thread":            "Sharing a canvas in thread",
	"add_canvas_to_channel_tab":         "Adding a canvas tab to a channel",
	"start_huddle":                      "Starting a huddle",
	"switch_step":                       "Branching workflow",
	"case_step":                         "Evaluating conditions",
	"loop_step":                         "Repeating for each item",
	"lookup_channel_record":             "Looking up a Salesforce channel record",
	"send_summary_to_salesforce_app":    "Sending summary to Salesforce App",
	"send_summary_to_my_salesforce_app": "Sending summary to My Salesforce App",
}

// wfStepTitle is getWorkflowStepDisplayTitle.
func wfStepTitle(callback, title string) string {
	if t, ok := wfStepTitles[callback]; ok {
		return wfBuilderNS.T(t)
	}
	return wfBuilderNS.T("In progress: {stepTitle}", "stepTitle", title)
}

// wfFinal are the outcome statuses that end a run's toasts (Qn).
var wfFinal = map[string]bool{"SUCCESS": true, "ERROR": true, "EXPIRED": true, "CANCELED": true, "ABANDONED": true}

// wfRunToast is the toast a run shows: WorkflowStatusToast, or with
// outcome set WorkflowOutcomeToast.
type wfRunToast struct {
	exec, trigger string
	outcome       string // a final status: the outcome toast
	count, text   string // "2/3" ("" when it branches) and the step's title
	icon, title   string // the workflow's, from the workflows store ("" without)
	until         time.Time
	close, expand widget.Clickable
	closeTip      popups.TipState
	hover         gesture.Hover
}

// wfRunState is the window's run toasts (UI goroutine).
type wfRunState struct {
	seen, dismissed map[string]bool // Slack's started and dismissed trigger executions
	cur             *wfRunToast
}

var wfRunStates sync.Map // *App -> *wfRunState

func (a *App) wfRuns() *wfRunState {
	v, _ := wfRunStates.LoadOrStore(a, &wfRunState{seen: map[string]bool{}, dismissed: map[string]bool{}})
	return v.(*wfRunState)
}

// workflowRunEvent is the two realtime handlers (UI goroutine).
func (a *App) workflowRunEvent(typ string, raw json.RawMessage) {
	c := a.Core()
	if c == nil || c.Experiment("workflow_execution_window") != "on" {
		return
	}
	var ev struct {
		WorkflowID string `json:"workflow_id"`
		Exec       string `json:"trigger_execution_id"`
		StepNumber *int   `json:"step_number"`
		StepTitle  string `json:"step_title"`
		TotalSteps int    `json:"total_steps"`
		TriggerID  string `json:"trigger_id"`
		Branching  bool   `json:"has_conditional_branching"`
		CallbackID string `json:"callback_id"`
		Status     string `json:"workflow_instance_status"`
	}
	if json.Unmarshal(raw, &ev) != nil || ev.Exec == "" {
		return
	}
	st := a.wfRuns()
	if st.dismissed[ev.Exec] {
		return
	}
	t := &wfRunToast{exec: ev.Exec, trigger: ev.TriggerID}
	if v, ok := wfWorkflows.Load(ev.WorkflowID); ok {
		w := v.(core.WorkflowInfo)
		if w.Icon != "" && w.Title != "" {
			t.icon, t.title = w.Icon, w.Title
		}
	}
	now := time.Now()
	switch typ {
	case "workflow_execution_step_started":
		st.seen[ev.Exec] = true
		if ev.StepTitle == "" || ev.TriggerID == "" {
			return // the toast renders nothing
		}
		if !ev.Branching && ev.TotalSteps > 0 && ev.StepNumber != nil {
			t.count = wfBuilderNS.T("{stepNumber}/{totalSteps}", "stepNumber", *ev.StepNumber+1, "totalSteps", ev.TotalSteps)
		}
		t.text = wfStepTitle(ev.CallbackID, ev.StepTitle)
		if ev.CallbackID != "ai_answer" && ev.CallbackID != "summarize_channel" {
			t.until = now.Add(6 * time.Second) // shouldAutoFade
		}
	case "workflow_execution_outcome_changed":
		if !st.seen[ev.Exec] || !wfFinal[ev.Status] {
			return
		}
		t.outcome = ev.Status
		switch {
		case ev.Status == "SUCCESS":
			t.text = wfBuilderNS.T("Workflow complete")
		case ev.Status == "ERROR":
			t.text = wfBuilderNS.T("Workflow failed")
		default:
			t.text = wfBuilderNS.T("Workflow canceled")
		}
		t.until = now.Add(6 * time.Second)
	default:
		return
	}
	st.cur = t
	a.Invalidate()
}

// layoutWorkflowRunToast draws the run's toast (App.frame, over the other
// toast).
func (a *App) layoutWorkflowRunToast(gtx layout.Context) {
	v, ok := wfRunStates.Load(a)
	if !ok {
		return
	}
	st := v.(*wfRunState)
	t := st.cur
	if t == nil {
		return
	}
	if t.close.Clicked(gtx) {
		st.dismissed[t.exec] = true // dismissWorkflowStatusToast: later steps stay quiet
		st.cur = nil
		return
	}
	if t.expand.Clicked(gtx) {
		a.OpenWorkflowDetails(t.trigger, "") // openWorkflowDetailsModalCircular {triggerId}
	}
	now := gtx.Now
	if !t.until.IsZero() {
		if t.hover.Update(gtx.Source) {
			t.until = now.Add(6 * time.Second) // .c-toast onMouseEnter holds the fade
		}
		if now.After(t.until) {
			st.cur = nil
			return
		}
		gtx.Execute(op.InvalidateCmd{At: t.until})
	}
	th := a.th
	c := &th.C
	sz := gtx.Constraints.Max
	padX, padY := gtx.Dp(16), gtx.Dp(12)
	if t.outcome == "" {
		padY = gtx.Dp(16) // .workflow_status_toast
	}
	gap, lh, is := gtx.Dp(8), gtx.Dp(24), gtx.Dp(24)
	// the close button: the status toast's 20x24, Toast's 36 square
	// (IconButton medium, margin-top 2) on the outcome
	cw, ch := gtx.Dp(20), gtx.Dp(24)
	if t.outcome != "" {
		cw, ch = gtx.Dp(36), gtx.Dp(36)
	}
	rowMax := min(gtx.Dp(300), gtx.Dp(350)-2*padX-gap-cw)
	x := is + gap
	countW := 0
	if t.count != "" {
		countW = th.Measure(gtx, 15, theme.Regular, t.count)
		x += countW + gap
	}
	expandW := 0
	if t.outcome == "" {
		expandW = gap + gtx.Dp(20)
	}
	textMax := max(1, rowMax-x-expandW)
	textCall, td := measured(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(textMax, gtx.Dp(200))}
		return th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 24, Weight: theme.Regular, Color: c.TooltipText}, t.text)
	})
	rowW := x + td.Size.X + expandW
	rowH := max(lh, td.Size.Y)
	innerH := rowH
	if t.outcome != "" {
		innerH = max(rowH, ch+gtx.Dp(2))
	}
	w, h := 2*padX+rowW+gap+cw, 2*padY+innerH
	right, bottom := gtx.Dp(20), gtx.Dp(60) // offsetX 20, offsetY 60
	r := image.Rect(sz.X-right-w, sz.Y-bottom-h, sz.X-right, sz.Y-bottom)
	theme.Shadow(gtx, r.Add(image.Pt(0, gtx.Dp(4))), gtx.Dp(8), gtx.Dp(12), theme.WithAlpha(c.PanelShadow, 0x14))
	theme.RRect(gtx, r.Inset(-max(1, gtx.Dp(1))), gtx.Dp(8)+1, theme.WithAlpha(c.Fg, 0x21))
	theme.RRect(gtx, r, gtx.Dp(8), c.Tooltip)
	func() {
		defer clip.Rect(r).Push(gtx.Ops).Pop()
		t.hover.Add(gtx.Ops)
	}()
	// the row: aligned to the top (align-items flex-start); the outcome's
	// centred with its close button
	ry := r.Min.Y + padY
	if t.outcome != "" {
		ry = r.Min.Y + padY + (innerH-rowH)/2
	}
	ox := r.Min.X + padX
	at(gtx, image.Pt(ox, ry), image.Pt(is, is), func(gtx layout.Context) layout.Dimensions {
		return a.wfRunIcon(gtx, t, is)
	})
	if t.count != "" {
		at(gtx, image.Pt(ox+is+gap, ry), image.Pt(countW+gtx.Dp(2), lh), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return th.Label(gtx, 15, theme.Regular, c.TooltipSub, t.count)
			})
		})
	}
	place(gtx, textCall, image.Pt(ox+x, ry))
	if t.outcome == "" {
		ex := ox + x + td.Size.X + gap
		at(gtx, image.Pt(ex, ry), image.Pt(gtx.Dp(20), lh), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, &t.expand, nil, func(gtx layout.Context) layout.Dimensions {
				at(gtx, image.Pt(0, (lh-gtx.Dp(20))/2), image.Pt(gtx.Dp(20), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
					return icons.Get("caret-up-down")(gtx, 20, c.TooltipText)
				})
				return layout.Dimensions{Size: image.Pt(gtx.Dp(20), lh)}
			})
		})
	}
	cx := r.Max.X - padX - cw
	cy := r.Min.Y + padY // .workflow_status_close_button: align-self flex-start
	if t.outcome != "" {
		cy = r.Min.Y + padY + (innerH-ch)/2 + gtx.Dp(1)
	}
	tip := wfBuilderNS.T("Dismiss and continue running workflow")
	if t.outcome != "" {
		tip = wfBuilderNS.T("Dismiss notification")
	}
	at(gtx, image.Pt(cx, cy), image.Pt(cw, ch), func(gtx layout.Context) layout.Dimensions {
		d := clickArea(gtx, &t.close, nil, func(gtx layout.Context) layout.Dimensions {
			at(gtx, image.Pt((cw-gtx.Dp(20))/2, (ch-gtx.Dp(20))/2), image.Pt(gtx.Dp(20), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
				return icons.Get("close")(gtx, 20, c.TooltipText)
			})
			return layout.Dimensions{Size: image.Pt(cw, ch)}
		})
		popups.Tooltip(gtx, th, &t.closeTip, t.close.Hovered(), d.Size, popups.Above, tip, "")
		return d
	})
}

// wfRunIcon is the toast's 24px icon: the workflow's image (radius
// clamp(4px, 22.222%, 12px)), on the status toast under a #0000004d scrim
// with a small (12px) white spinner; without one, a large (24px) blue
// spinner, or the outcome's check-circle / warning.
func (a *App) wfRunIcon(gtx layout.Context, t *wfRunToast, is int) layout.Dimensions {
	c := &a.th.C
	sq := image.Pt(is, is)
	if t.icon != "" {
		rad := max(gtx.Dp(4), min(is*2/9, gtx.Dp(12)))
		if img, ok := a.Images().Get(t.icon, is); ok {
			theme.Image(gtx, img, sq, rad)
		} else {
			a.th.Placeholder(gtx, sq, rad)
		}
		if t.outcome == "" {
			theme.RRect(gtx, image.Rectangle{Max: sq}, rad, color.NRGBA{A: 0x4d})
			s := gtx.Dp(12)
			at(gtx, image.Pt((is-s)/2, (is-s)/2), image.Pt(s, s), func(gtx layout.Context) layout.Dimensions {
				wfSpinner(gtx, s, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, c.FgSubtle)
				return layout.Dimensions{Size: image.Pt(s, s)}
			})
		}
		return layout.Dimensions{Size: sq}
	}
	switch t.outcome {
	case "":
		wfSpinner(gtx, is, c.Link, c.FgSubtle)
	case "SUCCESS":
		icons.Get("check-circle")(gtx, 24, c.TooltipText)
	default:
		icons.Get("warning")(gtx, 24, c.TooltipText)
	}
	return layout.Dimensions{Size: sq}
}

// wfSpinner is InfiniteSpinner (s6So) at the default speed: the track
// (content-ter at .25) and two 55-unit arcs of r 35 in a 100 box, the
// ring turning in 1.2s on cubic-bezier(.25,.29,.54,.86) from -45°, the
// tail in 1.2s on cubic-bezier(.41,.24,.64,.69).
func wfSpinner(gtx layout.Context, d int, col, track color.NRGBA) {
	sc := float32(d) / 100
	r := 35 * sc
	sw := 8 * sc
	ctr := f32.Pt(float32(d)/2, float32(d)/2)
	arc := func(a0, sweep float32, c color.NRGBA) {
		var p clip.Path
		p.Begin(gtx.Ops)
		p.MoveTo(f32.Pt(ctr.X+r*float32(math.Cos(float64(a0))), ctr.Y+r*float32(math.Sin(float64(a0)))))
		p.ArcTo(ctr, ctr, sweep)
		st := clip.Stroke{Path: p.End(), Width: sw}.Op().Push(gtx.Ops)
		paint.ColorOp{Color: c}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		st.Pop()
	}
	track.A = uint8(float32(track.A)*.25 + .5)
	arc(0, 2*math.Pi-0.001, track)
	t := float64(gtx.Now.UnixMilli()%1200) / 1200
	dash := float32(55.0 / 219.9 * 2 * math.Pi)
	base := float32(-90.0/219.9*2*math.Pi) - float32(math.Pi/4) // dashoffset 90, ring rotate(-45deg)
	arc(base+float32(2*math.Pi*ccBezier(.25, .29, .54, .86, t)), dash, col)
	arc(base+float32(2*math.Pi*ccBezier(.41, .24, .64, .69, t)), dash, col)
	indicatorFrame(gtx, 33*time.Millisecond)
}

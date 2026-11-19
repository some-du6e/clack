package core

// Agent tasks: the Tasks tab Slack shows in an agent app's DM, the one an
// app's settings page turns on under App Home ("Show a Tasks tab where
// people can manage the tasks this app runs as an agent"). A task is a
// Workflow Builder workflow the app is the creation source of (an
// AGENT-created workflow whose step is send_prompt_to_3p_agent: the
// prompt, run as the person who made the task, on the trigger's
// schedule). Every rule and call shape is from Slack's web client, build
// 9685e6ce144b (2026-11-18); docs/APP-TASKS.md names the modules.
//
//   - The tab (client-boot L1V2 getAppChannelTabsState): an app DM whose
//     app is an agent app (OQHR dy: the app profile's legacy_app
//     is_agent_app) shows [Home, Messages, (Sessions), About, Tasks]; Tasks
//     needs wfb_3p_agent_tasks_tab on (YwqI qC), the app not turned off by
//     an admin (o1P2 sQ: the team's ai_apps_settings app entry with
//     is_agent_tasks_enabled false, or one of two built-in app ids), the
//     plan (YwqI ro: featureAccess agent_tasks__write while the experiment
//     is on), the developer's switch (SUqq: the app home's
//     agent_tasks_enabled, on unless false) and the app shown as an agent
//     (OQHR pT: legacy_app.is_shown_as_agent). Slack's client never reads
//     messages_tab_enabled for it: the settings page turns the Tasks tab
//     off with messaging, and the app home answers agent_tasks_enabled
//     false then.
//   - The list (wBUp fetchWorkflowsForUser for entity agent-tasks):
//     functions.workflows.list {limit 50, cursor, filter_options {name:
//     query, creation_source_type AGENT, creation_source_id: the app},
//     sort_options {column date_updated, direction desc},
//     workflow_builder_only true, reason automations_gallery}, then
//     workflows.triggers.list {app_ids: the workflows' app ids, comma
//     joined}; a task's trigger is the first whose workflow.app.id is the
//     workflow's app_id. useAgentTasksExplorer drops is_empty workflows and
//     sorts the published ones first, then by lowercased title.
//   - The row (boot-shared NQBk AgentTaskItem): Run task now posts the
//     task's prompt into the agent's DM (EAAO runPromptFor3pAgent, when the
//     prompt reads no {{inputs.…}}); Pause task / Restart task is
//     functions.workflows.unpublish / publish {workflow_id, reason
//     slackbot-automation-item-pause|restart} (vRD3 setTaskPublished:
//     optimistic, reverted with "Something went wrong. Please try again."
//     on a failure); Delete task (8ftf) is workflows.triggers.delete
//     {trigger_id} then functions.workflows.delete {workflow_id}, reason
//     slackbot-automation-item-delete.
//   - Live (boot-render): workflow {workflow_id, subtype} drops a deleted
//     task from every agent-tasks list and marks the lists stale
//     otherwise (workflowChanged); hermes_trigger_updated {trigger_id,
//     is_deleted, is_workflow_published} marks the trigger and its
//     workflow stale (workflowTriggerUpdated). Core emits both as
//     Event{Type: "agent_task"}.
//
// Scraping policy: tasks live in memory only.

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// ExpAgentTasksTab is wfb_3p_agent_tasks_tab (YwqI qC,
// isAgentTasksTabFor3pAgentsEnabled).
const ExpAgentTasksTab = "wfb_3p_agent_tasks_tab"

// FeatureAgentTasksWrite is YwqI ro's featureAccess name
// (isAgentTasksAllowedByPlan).
const FeatureAgentTasksWrite = "agent_tasks__write"

// The experiments the Tasks tab reads (TestExperimentReadsListed): the
// tab itself, the title's weight (tabTitleHeavy under recent prompts) and
// the schedule line's switches (AgentTaskItem's n1._ arguments).
func init() {
	for _, n := range []string{ExpAgentTasksTab, "wfb_agent_tasks_recent_prompts", "wfb_scheduled_trigger_time_buckets",
		"wfb_schedule_on_last_day_of_month", "wfb_agents_can_create_simple_automations_tab_ms3",
		// a task card's auto-collapse (ui/blockkit/agent.go)
		"slackbot_mcp_tasks_card_auto_collapse_when_complete", "agents_streaming_stop_button_fe"} {
		experimentReads[n] = on
	}
}

// agentTasksBlockedApps is o1P2's u: apps Slack never gives a Tasks tab.
var agentTasksBlockedApps = map[string]bool{"A0AT1U0QRNH": true, "A019W997N66": true}

// AgentTasksTabInputs are getAppChannelTabsState's fields the Tasks tab
// reads (L1V2 P: p && !m && h && l && a, inside the isAgentApp branch).
type AgentTasksTabInputs struct {
	AgentApp           bool // isAgentApp (OQHR dy)
	ShownAsAgent       bool // isShownAsAgent (OQHR pT)
	Experiment         bool // experimentWfb3pAgentTasksTabGroupOn
	Disabled           bool // isAgentTasksDisabled (o1P2 sQ)
	AllowedByPlan      bool // isAgentTasksAllowedByPlan (YwqI ro)
	EnabledByDeveloper bool // isAgentTasksEnabledByDeveloper (SUqq)
}

// AgentTasksTabRule is L1V2's Tasks entry: an agent app DM shows
// App3pAgentTasks when all of it holds.
func AgentTasksTabRule(in AgentTasksTabInputs) bool {
	return in.AgentApp && in.Experiment && !in.Disabled && in.AllowedByPlan && in.EnabledByDeveloper && in.ShownAsAgent
}

// AgentTasksTab reports whether the app DM ch, whose app home is home,
// shows the Tasks tab, and whether that can still change because the
// app's profile isn't loaded yet (the caller loads it: AppCardOf).
func (a *App) AgentTasksTab(ch string, home AppHome) (shown, pending bool) {
	in, pending := a.AgentTasksTabInputsOf(ch, home)
	return AgentTasksTabRule(in), pending
}

// AgentTasksTabInputsOf reads the rule's inputs for the app DM ch.
func (a *App) AgentTasksTabInputsOf(ch string, home AppHome) (in AgentTasksTabInputs, pending bool) {
	c, ok := a.S.Conv(ch)
	if !ok || !c.IsIM || c.User == "" {
		return in, false
	}
	var appOfUser, status string
	if u, ok := a.S.User(c.User); ok && u != nil {
		appOfUser, status = u.Profile.APIAppID, u.AgentStatus
	}
	// a fetch that failed counts as known (no card) until its retry, so the
	// tab bar asks for the profile once, not every frame
	card, _, known := a.AppCardSettled(c.User, "")
	pending = !known
	in.AgentApp, in.ShownAsAgent = card.AgentApp, card.ShownAsAgent
	if a.experiment("support_lots_of_agents") == "on" && status == "consistently_an_agent" {
		// getMemberAgentIdentity / resolveShownAsAgent: a member whose
		// agent_status is consistently_an_agent is an agent shown as one
		in.AgentApp, in.ShownAsAgent = true, true
	}
	in.Experiment = a.experiment(ExpAgentTasksTab) == "on"
	appID := firstNonEmpty(card.AppID, appOfUser, home.AppID)
	in.Disabled = a.agentTasksDisabled(appID)
	in.AllowedByPlan = !in.Experiment || a.FeatureAccess(FeatureAgentTasksWrite)
	in.EnabledByDeveloper = home.AgentTasks
	return in, pending
}

// AgentTasksAgent is the agent DM ch's agent: its bot user and app id
// (the app profile's, else the bot user's api_app_id; "" until known).
func (a *App) AgentTasksAgent(ch string) (user, app string) {
	c, ok := a.S.Conv(ch)
	if !ok || !c.IsIM || c.User == "" {
		return "", ""
	}
	user = c.User
	if card, ok := a.CachedAppCard(user, ""); ok && card.AppID != "" {
		return user, card.AppID
	}
	if u, ok := a.S.User(user); ok && u != nil {
		return user, u.Profile.APIAppID
	}
	return user, ""
}

// agentTasksDisabled is o1P2 sQ: the two blocked apps, or the team's
// ai_apps_settings entry for the app with is_agent_tasks_enabled false.
func (a *App) agentTasksDisabled(appID string) bool {
	if appID == "" {
		return false
	}
	if agentTasksBlockedApps[appID] {
		return true
	}
	// o1P2's ai_apps_settings: on Enterprise Grid the org's locked pref
	// alone (TI4y getLockedOrgPref; THpq isEnterprise), a workspace's
	// entry ignored; elsewhere the team pref
	var raw json.RawMessage
	if a.C.Creds.EnterpriseID != "" {
		raw = a.EnterpriseLockedPref("ai_apps_settings")
	} else if r, ok := a.TeamPref("ai_apps_settings"); ok {
		raw = r
	}
	if len(raw) == 0 {
		return false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil { // a pref sent as a JSON string
		raw = json.RawMessage(s)
	}
	var v struct {
		Apps []struct {
			AppID   string `json:"app_id"`
			Enabled *bool  `json:"is_agent_tasks_enabled"`
		} `json:"apps"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return false
	}
	for _, app := range v.Apps {
		if app.AppID == appID {
			return app.Enabled != nil && !*app.Enabled
		}
	}
	return false
}

// ---- tasks ----

// AgentTask is one task as AgentTaskItem draws it: the workflow (title,
// is_published, its send-prompt step, collaborators) and its trigger
// (type, schedule), merged the way createShortcutFromTriggerAndWorkflow
// (wcWH) does.
type AgentTask struct {
	WorkflowID, TriggerID string
	Title, Description    string
	// Published is the workflow's is_published; Known false when neither
	// the workflow nor the trigger said (then the row is never PAUSED).
	Published, Known bool
	// Prompt is the send-prompt step's inputs.prompt.value as text, and
	// PromptRaw the value itself (a string, or rich text).
	Prompt    string
	PromptRaw json.RawMessage
	// AgentAppID is a send_prompt_to_3p_agent step's inputs.agent_id;
	// ThirdParty says the step is that function (else
	// send_prompt_to_agent, Slackbot's).
	AgentAppID       string
	ThirdParty       bool
	HasPromptStep    bool
	Collaborators    []string
	CreationSourceID string
	AppID            string
	IsWorkflowApp    bool
	DateUpdated      int64
	Empty            bool   // is_empty: never listed
	TriggerType      string // scheduled | event | shortcut | webhook | external | service
	Schedule         *TaskSchedule
}

// TaskSchedule is a scheduled trigger's schedule, camel-cased the way
// Slack's ijf8 reads it.
type TaskSchedule struct {
	StartTime, EndTime string
	Timezone           string
	OccurrenceCount    int
	TimeBucket         string // morning | afternoon | evening
	Frequency          *TaskFrequency
}

// TaskFrequency is schedule.frequency.
type TaskFrequency struct {
	Type             string // once | hourly | daily | weekly | monthly | yearly
	RepeatsEvery     int
	OnDays           []string // Monday … Sunday
	OnWeekNum        *int     // 1…4, -1 for the last
	OnLastDayOfMonth bool
	// keys are the keys the trigger sent (nil for one built in Go): TGBB
	// matches a menu option with lodash's deep isEqual, so a key past the
	// option's own (repeats_every 1, on_last_day_of_month false) makes it
	// custom
	keys map[string]bool
}

// exactly reports whether the frequency, as sent, has only these keys.
// A frequency built in Go has no key record and passes.
func (f *TaskFrequency) exactly(keys ...string) bool {
	if f.keys == nil {
		return true
	}
	if len(f.keys) != len(keys) {
		return false
	}
	for _, k := range keys {
		if !f.keys[k] {
			return false
		}
	}
	return true
}

// Paused is AgentTaskItem's F (showUnpublished, as the Tasks tab sets
// it): the workflow is known to be unpublished.
func (t AgentTask) Paused() bool { return t.Known && !t.Published }

// inputsRef is WdYh's l: a prompt that reads the trigger's inputs.
var inputsRef = regexp.MustCompile(`\{\{inputs\.([\w.-]+?)\}\}`)

// CanRunNow is useRunTaskNow's canRunNow for a third-party agent's task
// ($: a prompt that reads no {{inputs.…}}), and the row shows Run task
// now only while the task isn't paused (D: is_published false).
func (t AgentTask) CanRunNow() bool {
	if !t.HasPromptStep || len(t.PromptRaw) == 0 || string(t.PromptRaw) == `""` || string(t.PromptRaw) == "null" {
		return false
	}
	return !inputsRef.Match(t.PromptRaw) && !(t.Known && !t.Published)
}

// CanDelete is useDeleteAutomationMenuItem's show: the viewer is one of
// the workflow's collaborators, and the task has no trigger or its
// workflow app is a workflow app.
func (t AgentTask) CanDelete(self string) bool {
	if t.WorkflowID == "" || self == "" {
		return false
	}
	in := false
	for _, c := range t.Collaborators {
		if c == self {
			in = true
		}
	}
	return in && (t.TriggerID == "" || t.IsWorkflowApp)
}

// AgentTasksPage is one page of an agent's tasks.
type AgentTasksPage struct {
	Tasks  []AgentTask
	Cursor string // response_metadata.next_cursor: there is another page
}

type rawTaskStep struct {
	Function struct {
		CallbackID string `json:"callback_id"`
	} `json:"function"`
	Inputs map[string]struct {
		Value json.RawMessage `json:"value"`
	} `json:"inputs"`
}

type rawTaskWorkflow struct {
	ID                 string          `json:"id"`
	Title              string          `json:"title"`
	Description        string          `json:"description"`
	IsPublished        *bool           `json:"is_published"`
	AppID              string          `json:"app_id"`
	Collaborators      []string        `json:"collaborators"`
	Steps              []rawTaskStep   `json:"steps"`
	CreationSourceID   string          `json:"creation_source_id"`
	CreationSourceType string          `json:"creation_source_type"`
	DateUpdated        json.RawMessage `json:"date_updated"`
	IsEmpty            bool            `json:"is_empty"`
}

type rawTaskTrigger struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Workflow    struct {
		ID          string `json:"id"`
		WorkflowID  string `json:"workflow_id"`
		AppID       string `json:"app_id"`
		Title       string `json:"title"`
		Description string `json:"description"`
		App         struct {
			ID            string `json:"id"`
			IsWorkflowApp bool   `json:"is_workflow_app"`
		} `json:"app"`
	} `json:"workflow"`
	WorkflowDetails *struct {
		IsPublished   *bool    `json:"is_published"`
		Collaborators []string `json:"collaborators"`
	} `json:"workflow_details"`
	Schedule *struct {
		StartTime       json.RawMessage `json:"start_time"`
		EndTime         json.RawMessage `json:"end_time"`
		Timezone        string          `json:"timezone"`
		OccurrenceCount int             `json:"occurrence_count"`
		TimeBucket      string          `json:"time_bucket"`
		Frequency       json.RawMessage `json:"frequency"`
	} `json:"schedule"`
}

// The send-prompt functions (ck4f oW): SendPromptTo3pAgent is a
// third-party agent's task, SendPromptToAgent Slackbot's.
const (
	fnSendPromptTo3pAgent = "send_prompt_to_3p_agent"
	fnSendPromptToAgent   = "send_prompt_to_agent"
)

// jsonTime reads a timestamp that may come as an ISO string or a number of
// seconds.
func jsonTime(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var f float64
	if json.Unmarshal(raw, &f) == nil && f > 0 {
		return time.Unix(int64(f), 0).UTC().Format(time.RFC3339)
	}
	return ""
}

func jsonInt64(raw json.RawMessage) int64 {
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		return int64(f)
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		n, _ := strconv.ParseInt(s, 10, 64)
		return n
	}
	return 0
}

// promptText is a prompt value as the text Run task now sends: a string
// as is, rich text as its text runs.
func promptText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var walk func(v any) string
	walk = func(v any) string {
		switch x := v.(type) {
		case map[string]any:
			if t, ok := x["text"].(string); ok && x["type"] == "text" {
				return t
			}
			out := ""
			if el, ok := x["elements"]; ok {
				out += walk(el)
			}
			if x["type"] == "rich_text_section" || x["type"] == "rich_text_preformatted" || x["type"] == "rich_text_quote" {
				out += "\n"
			}
			return out
		case []any:
			out := ""
			for _, e := range x {
				out += walk(e)
			}
			return out
		}
		return ""
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	return strings.TrimRight(walk(v), "\n")
}

// agentTaskOf merges a workflow with its trigger (nil for none).
// taskFrequencyOf is a trigger's schedule.frequency, with the keys it
// came with (nil for none).
func taskFrequencyOf(raw json.RawMessage) *TaskFrequency {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var f struct {
		Type             string   `json:"type"`
		RepeatsEvery     int      `json:"repeats_every"`
		OnDays           []string `json:"on_days"`
		OnWeekNum        *int     `json:"on_week_num"`
		OnLastDayOfMonth bool     `json:"on_last_day_of_month"`
	}
	var keys map[string]json.RawMessage
	if json.Unmarshal(raw, &f) != nil || json.Unmarshal(raw, &keys) != nil {
		return nil
	}
	out := &TaskFrequency{Type: f.Type, RepeatsEvery: f.RepeatsEvery, OnDays: f.OnDays, OnWeekNum: f.OnWeekNum,
		OnLastDayOfMonth: f.OnLastDayOfMonth, keys: map[string]bool{}}
	for k := range keys {
		out.keys[k] = true
	}
	return out
}

func agentTaskOf(w rawTaskWorkflow, tr *rawTaskTrigger) AgentTask {
	t := AgentTask{WorkflowID: w.ID, Title: w.Title, Description: w.Description, AppID: w.AppID,
		Collaborators: w.Collaborators, CreationSourceID: w.CreationSourceID, DateUpdated: jsonInt64(w.DateUpdated), Empty: w.IsEmpty}
	if w.IsPublished != nil {
		t.Published, t.Known = *w.IsPublished, true
	}
	for _, st := range w.Steps {
		cb := st.Function.CallbackID
		if cb != fnSendPromptTo3pAgent && cb != fnSendPromptToAgent {
			continue
		}
		t.HasPromptStep, t.ThirdParty = true, cb == fnSendPromptTo3pAgent
		if p, ok := st.Inputs["prompt"]; ok {
			t.PromptRaw = p.Value
			t.Prompt = promptText(p.Value)
		}
		if ag, ok := st.Inputs["agent_id"]; ok {
			var id string
			_ = json.Unmarshal(ag.Value, &id)
			t.AgentAppID = id
		}
		break
	}
	if tr == nil {
		return t
	}
	t.TriggerID, t.TriggerType = tr.ID, tr.Type
	t.Title = firstNonEmpty(tr.Name, tr.Workflow.Title, w.Title)
	t.Description = firstNonEmpty(tr.Description, tr.Workflow.Description, w.Description)
	t.IsWorkflowApp = tr.Workflow.App.IsWorkflowApp
	if !t.Known && tr.WorkflowDetails != nil && tr.WorkflowDetails.IsPublished != nil {
		t.Published, t.Known = *tr.WorkflowDetails.IsPublished, true
	}
	if len(t.Collaborators) == 0 && tr.WorkflowDetails != nil {
		t.Collaborators = tr.WorkflowDetails.Collaborators
	}
	if s := tr.Schedule; s != nil && tr.Type == "scheduled" {
		ts := &TaskSchedule{StartTime: jsonTime(s.StartTime), EndTime: jsonTime(s.EndTime), Timezone: s.Timezone,
			OccurrenceCount: s.OccurrenceCount, TimeBucket: s.TimeBucket}
		ts.Frequency = taskFrequencyOf(s.Frequency)
		t.Schedule = ts
	}
	return t
}

// agentTaskFilter is filter_options as fetchWorkflowsForUser sends it,
// in its key order (is_published is undefined there, so JSON.stringify
// leaves it out).
type agentTaskFilter struct {
	Name               string `json:"name"`
	CreationSourceType string `json:"creation_source_type"`
	CreationSourceID   string `json:"creation_source_id"`
}

// AgentTasksReason is the reason fetchWorkflowsForUser sends.
const AgentTasksReason = "automations_gallery"

// AgentTasks reads one page of the tasks the agent app appID is the
// creation source of, those whose title has query, from cursor ("" the
// first page): useAgentTasksExplorer's browser.
func (a *App) AgentTasks(ctx context.Context, appID, query, cursor string) (AgentTasksPage, error) {
	if appID == "" {
		return AgentTasksPage{}, errors.New("core: AgentTasks needs an app")
	}
	fb, _ := json.Marshal(agentTaskFilter{Name: query, CreationSourceType: "AGENT", CreationSourceID: appID})
	p := url.Values{"limit": {"50"}, "filter_options": {string(fb)},
		"sort_options": {`{"column":"date_updated","direction":"desc"}`}, "workflow_builder_only": {"true"},
		"reason": {AgentTasksReason}}
	if cursor != "" {
		p.Set("cursor", cursor)
	}
	var wl struct {
		Workflows []rawTaskWorkflow `json:"workflows"`
		Meta      struct {
			NextCursor string `json:"next_cursor"`
		} `json:"response_metadata"`
	}
	err := a.C.Call(ctx, "functions.workflows.list", p, &wl)
	logCall("functions.workflows.list", "agent tasks app="+appID, err)
	if err != nil {
		return AgentTasksPage{}, err
	}
	var trs []rawTaskTrigger
	if len(wl.Workflows) > 0 {
		var ids []string
		for _, w := range wl.Workflows {
			ids = append(ids, w.AppID)
		}
		var tl struct {
			Triggers []rawTaskTrigger `json:"triggers"`
		}
		err := a.C.Call(ctx, "workflows.triggers.list", url.Values{"app_ids": {strings.Join(ids, ",")}, "reason": {AgentTasksReason}}, &tl)
		logCall("workflows.triggers.list", "agent tasks n="+strconv.Itoa(len(ids)), err)
		if err != nil {
			return AgentTasksPage{}, err
		}
		trs = tl.Triggers
	}
	page := AgentTasksPage{Cursor: wl.Meta.NextCursor}
	seen := map[string]bool{}
	for _, w := range wl.Workflows {
		if seen[w.ID] { // uniqBy id
			continue
		}
		seen[w.ID] = true
		var tr *rawTaskTrigger
		for i := range trs {
			if w.AppID != "" && trs[i].Workflow.App.ID == w.AppID {
				tr = &trs[i]
				break
			}
		}
		t := agentTaskOf(w, tr)
		if t.Empty {
			continue
		}
		page.Tasks = append(page.Tasks, t)
	}
	a.rememberAgentTasks(page.Tasks)
	return page, nil
}

// AgentTasksRetry is AgentTasks for the error row's Retry: a click, so
// the request skips the client's backoff for the identical one that just
// failed (resetQueryAndFetch is a fresh request in Slack).
func (a *App) AgentTasksRetry(ctx context.Context, appID, query string) (AgentTasksPage, error) {
	return a.AgentTasks(slack.NoBackoff(ctx), appID, query, "")
}

// SortAgentTasks is useAgentTasksExplorer's U: published tasks first
// (is_published not false), then by lowercased title.
func SortAgentTasks(ts []AgentTask) {
	sort.SliceStable(ts, func(i, j int) bool {
		pi, pj := !ts[i].Paused(), !ts[j].Paused()
		if pi != pj {
			return pi
		}
		return strings.ToLower(ts[i].Title) < strings.ToLower(ts[j].Title)
	})
}

// agentTaskIndex remembers the listed tasks by workflow and trigger, for
// the live events (which name only ids).
type agentTaskIndex struct {
	mu        sync.Mutex
	byTrigger map[string]string // trigger id → workflow id
}

var agentTaskIndexes sync.Map // *App → *agentTaskIndex

func (a *App) agentTaskIndex() *agentTaskIndex {
	v, _ := agentTaskIndexes.LoadOrStore(a, &agentTaskIndex{byTrigger: map[string]string{}})
	return v.(*agentTaskIndex)
}

func (a *App) rememberAgentTasks(ts []AgentTask) {
	ix := a.agentTaskIndex()
	ix.mu.Lock()
	for _, t := range ts {
		if t.TriggerID != "" {
			ix.byTrigger[t.TriggerID] = t.WorkflowID
		}
	}
	ix.mu.Unlock()
}

// SetAgentTaskPublished is setTaskPublished's call: Restart task
// (functions.workflows.publish) or Pause task (functions.workflows.
// unpublish) with reason slackbot-automation-item-restart|pause. The
// caller shows the new state first and puts the old one back on an
// error.
func (a *App) SetAgentTaskPublished(ctx context.Context, workflowID string, publish bool) error {
	m, reason := "functions.workflows.unpublish", "slackbot-automation-item-pause"
	if publish {
		m, reason = "functions.workflows.publish", "slackbot-automation-item-restart"
	}
	err := a.C.Call(ctx, m, url.Values{"workflow_id": {workflowID}, "reason": {reason}}, nil)
	logCall(m, "workflow="+workflowID, err)
	return err
}

// DeleteAgentTask is the Delete task confirm's onConfirm: the trigger's
// workflows.triggers.delete, then functions.workflows.delete, both with
// reason slackbot-automation-item-delete; the first error stops it.
func (a *App) DeleteAgentTask(ctx context.Context, triggerID, workflowID string) error {
	const reason = "slackbot-automation-item-delete"
	if triggerID != "" {
		err := a.C.Call(ctx, "workflows.triggers.delete", url.Values{"trigger_id": {triggerID}, "reason": {reason}}, nil)
		logCall("workflows.triggers.delete", "trigger="+triggerID, err)
		if err != nil {
			return err
		}
	}
	if workflowID != "" {
		err := a.C.Call(ctx, "functions.workflows.delete", url.Values{"workflow_id": {workflowID}, "reason": {reason}}, nil)
		logCall("functions.workflows.delete", "workflow="+workflowID, err)
		if err != nil {
			return err
		}
	}
	return nil
}

// RunAgentTaskNow is runPromptFor3pAgent: the agent's DM (the open IM
// with agentUserID, else conversations.open, as startImByMemberId does),
// and the task's prompt sent there as the viewer's message. It returns
// the DM and the sent message's ts, which the tab highlights in Messages.
func (a *App) RunAgentTaskNow(ctx context.Context, agentUserID string, t AgentTask) (ch, ts string, err error) {
	if agentUserID == "" || !t.CanRunNow() {
		return "", "", errors.New("core: this task can't run now")
	}
	ch = a.openIMWith(agentUserID)
	if ch == "" {
		cv, err := a.OpenDM(ctx, agentUserID)
		if err != nil {
			return "", "", err
		}
		ch = cv.ID
	}
	blocks, text := runPromptBlocks(t.PromptRaw)
	// sendMessage's arguments: the blocks alone (s0Qa sets text only
	// without blocks); text draws the optimistic copy
	m, err := a.Send(ctx, SendOpts{Channel: ch, Text: text, Blocks: blocks, BlocksOnly: true})
	if err != nil {
		return ch, "", err
	}
	return ch, m.TS, nil
}

// runPromptBlocks is what Run task now posts for a prompt value (EAAO
// runPromptFor3pAgent: a string becomes a Delta, rich text goes through
// convertBlocksToDelta, and prepareAndSendMessage posts the
// convertDeltaToBlocks rich_text blocks): a string as one rich_text block
// holding it as literal text, a rich text prompt's blocks as they are;
// text is the plain fallback.
func runPromptBlocks(raw json.RawMessage) (json.RawMessage, string) {
	text := promptText(raw)
	var s string
	if json.Unmarshal(raw, &s) == nil {
		b, _ := json.Marshal([]any{map[string]any{"type": "rich_text", "elements": []any{
			map[string]any{"type": "rich_text_section", "elements": []any{map[string]any{"type": "text", "text": s}}}}}})
		return b, text
	}
	var blocks []json.RawMessage
	if json.Unmarshal(raw, &blocks) == nil && len(blocks) > 0 {
		return raw, text
	}
	b, _ := json.Marshal([]any{map[string]any{"type": "rich_text", "elements": []any{
		map[string]any{"type": "rich_text_section", "elements": []any{map[string]any{"type": "text", "text": text}}}}}})
	return b, text
}

// openIMWith is the open IM with user, "" when none is known.
func (a *App) openIMWith(user string) string {
	a.S.RLock()
	defer a.S.RUnlock()
	for id, c := range a.S.Convs {
		if c != nil && c.IsIM && c.User == user && !c.IsArchived {
			return id
		}
	}
	return ""
}

// onAgentTaskEvent applies workflow and hermes_trigger_updated (Slack's
// workflowChanged and workflowTriggerUpdated): the Tasks tab drops a
// deleted task at once and reads the list again otherwise.
func (a *App) onAgentTaskEvent(typ string, raw json.RawMessage) {
	switch typ {
	case "workflow":
		var e struct {
			WorkflowID string `json:"workflow_id"`
			Subtype    string `json:"subtype"`
		}
		_ = json.Unmarshal(raw, &e)
		a.emit(Event{Type: "agent_task", Name: e.Subtype, TS: e.WorkflowID})
	case "hermes_trigger_updated":
		var e struct {
			TriggerID   string `json:"trigger_id"`
			IsDeleted   bool   `json:"is_deleted"`
			IsPublished bool   `json:"is_workflow_published"`
		}
		_ = json.Unmarshal(raw, &e)
		ix := a.agentTaskIndex()
		ix.mu.Lock()
		wf := ix.byTrigger[e.TriggerID]
		ix.mu.Unlock()
		name := "trigger_updated"
		if e.IsDeleted {
			name = "trigger_deleted"
		}
		a.emit(Event{Type: "agent_task", Name: name, TS: wf, ThreadTS: e.TriggerID})
	}
}

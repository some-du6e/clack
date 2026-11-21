package fakeslack

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/some-du6e/clack/internal/slack"
)

// The Tasks tab fixture (Size.AgentTasks): Atlas, a third-party agent app
// (apps.profile.get is_agent_app, is_shown_as_agent) the viewer has an
// open DM with, whose App Home turns the Tasks tab on, and its tasks:
// AGENT-created workflows with Atlas as their creation source and a
// send_prompt_to_3p_agent step, each with a scheduled trigger, one per
// way Slack's AgentTaskItem draws a task (docs/APP-TASKS.md). Synthetic
// only. The server side the Tasks tab calls is here too:
// functions.workflows.list (filter_options creation_source_id Atlas),
// workflows.triggers.list (app_ids), functions.workflows.publish /
// unpublish / delete and workflows.triggers.delete, the features policy
// agent_tasks__write and the ai_apps_settings team pref; WorkflowEvent and
// TriggerUpdatedEvent are the realtime events the server sends when a
// task changes elsewhere.
const (
	AgentTasksDMID     = "D0FAKETASK"
	AgentTasksUserID   = "U0FAKETASK" // Atlas's bot user
	AgentTasksBotID    = "B0FAKETASK"
	AgentTasksAppID    = "A0FAKETASK"
	AgentTasksAppName  = "Atlas"
	AgentTasksTimezone = "America/Los_Angeles"
)

// The fixture's tasks (workflow ids; triggers are "Ft" + the same tail).
const (
	TaskBriefing  = "Wf0TASK001" // weekdays at 9:00 AM, published
	TaskDigest    = "Wf0TASK002" // Mondays at 9:00 AM, published
	TaskChecklist = "Wf0TASK003" // daily at 5:30 PM, paused
	TaskReview    = "Wf0TASK004" // the first Monday of every month, published
	TaskStandup   = "Wf0TASK005" // once, published
	TaskTriage    = "Wf0TASK006" // reads {{inputs.channel_id}}: no Run task now
	TaskDraft     = "Wf0TASK007" // is_empty: never listed
)

// TaskTriggerID is a fixture task's trigger.
func TaskTriggerID(workflowID string) string { return "Ft" + strings.TrimPrefix(workflowID, "Wf") }

// TaskAppID is a fixture task's workflow app (each workflow is its own
// app, as Workflow Builder makes them).
func TaskAppID(workflowID string) string { return "A0" + strings.TrimPrefix(workflowID, "Wf0") }

// FakeTask is one of Atlas's tasks.
type FakeTask struct {
	ID, Title string
	Prompt    string
	// PromptRich, when set, is the prompt as rich text blocks (the
	// step's inputs.prompt.value; Prompt is then unused)
	PromptRich  []any
	Published   bool
	Empty       bool
	DateUpdated int64
	StartTime   string // ISO, UTC
	Frequency   map[string]any
	Deleted     bool
}

// AgentTaskCall is one call the Tasks tab made.
type AgentTaskCall struct {
	Method string
	Form   url.Values
}

// AgentTasksState is the fixture's server side; tests change it to
// set the tab's conditions up.
type AgentTasksState struct {
	mu    sync.Mutex
	Tasks []*FakeTask
	// MessagesTab is the app home's messages_tab_enabled; TasksTab the
	// developer's "Show a Tasks tab". The settings page turns the Tasks
	// tab off with messaging: agent_tasks_enabled answers false while
	// MessagesTab is off.
	MessagesTab, TasksTab bool
	ShownAsAgent          bool // apps.profile.get is_shown_as_agent
	Plan                  bool // features.access.policies.list agent_tasks__write
	// AdminOff is an org admin's is_agent_tasks_enabled false for Atlas in
	// ai_apps_settings, as Grid holds it (enterprise.prefs.get's
	// locked_prefs); TeamAdminOff the same entry in the workspace's
	// team.prefs.get, which Slack ignores on Grid
	AdminOff, TeamAdminOff bool
	// ListError makes functions.workflows.list fail (the tab's error row).
	ListError bool
	Calls     []AgentTaskCall
}

var agentTaskStates = newPerServer() // *Server → *AgentTasksState

// AgentTasks is the fixture's state (Size.AgentTasks).
func (s *Server) AgentTasks() *AgentTasksState {
	v, _ := agentTaskStates.LoadOrStore(s, newAgentTasksState())
	return v.(*AgentTasksState)
}

func newAgentTasksState() *AgentTasksState {
	weekdays := []string{"Monday", "Tuesday", "Wednesday", "Thursday", "Friday"}
	one := 1
	return &AgentTasksState{MessagesTab: true, TasksTab: true, ShownAsAgent: true, Plan: true, Tasks: []*FakeTask{
		{ID: TaskBriefing, Title: "Morning briefing", Prompt: "Summarize what happened in my channels yesterday and send me a briefing.",
			Published: true, DateUpdated: 1763400000, StartTime: "2026-11-23T17:00:00Z",
			Frequency: map[string]any{"type": "weekly", "repeats_every": 1, "on_days": weekdays}},
		{ID: TaskDigest, Title: "Weekly support digest", Prompt: "Round up last week's support questions and how they were answered.",
			Published: true, DateUpdated: 1763300000, StartTime: "2026-11-23T17:00:00Z",
			Frequency: map[string]any{"type": "weekly", "repeats_every": 1, "on_days": []string{"Monday"}}},
		{ID: TaskChecklist, Title: "Release checklist", Prompt: "Check the release checklist and tell me what's still open.",
			Published: false, DateUpdated: 1763200000, StartTime: "2026-11-23T01:30:00Z",
			Frequency: map[string]any{"type": "daily", "repeats_every": 1}},
		{ID: TaskReview, Title: "Quarterly review prep", Prompt: "Collect the metrics I need for the monthly review.",
			Published: true, DateUpdated: 1763100000, StartTime: "2026-12-07T18:00:00Z",
			Frequency: map[string]any{"type": "monthly", "repeats_every": 1, "on_days": []string{"Monday"}, "on_week_num": one}},
		{ID: TaskStandup, Title: "Standup notes", Prompt: "Draft my standup notes from yesterday's threads.",
			Published: true, DateUpdated: 1763000000, StartTime: "2026-12-01T22:00:00Z"},
		{ID: TaskTriage, Title: "Inbox triage", Prompt: "Triage the new messages in {{inputs.channel_id}}.",
			Published: true, DateUpdated: 1762900000, StartTime: "2026-11-23T16:00:00Z",
			Frequency: map[string]any{"type": "daily", "repeats_every": 1}},
		{ID: TaskDraft, Title: "Untitled task", Empty: true, DateUpdated: 1762800000},
	}}
}

// AgentTaskCalls returns the calls so far.
func (st *AgentTasksState) AgentTaskCalls() []AgentTaskCall {
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]AgentTaskCall(nil), st.Calls...)
}

// Set runs f with the state locked (a test's change).
func (st *AgentTasksState) Set(f func(st *AgentTasksState)) {
	st.mu.Lock()
	defer st.mu.Unlock()
	f(st)
}

// Task returns the task with workflow id (nil when there is none).
func (st *AgentTasksState) Task(id string) *FakeTask {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.task(id)
}

func (st *AgentTasksState) task(id string) *FakeTask {
	for _, t := range st.Tasks {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// WorkflowEvent is the realtime workflow event (Slack's workflowChanged):
// subtype deleted, published, unpublished, updated or created.
func WorkflowEvent(workflowID, subtype string) map[string]any {
	return map[string]any{"type": "workflow", "subtype": subtype, "workflow_id": workflowID}
}

// TriggerUpdatedEvent is hermes_trigger_updated (workflowTriggerUpdated).
func TriggerUpdatedEvent(triggerID string, deleted, published bool) map[string]any {
	return map[string]any{"type": "hermes_trigger_updated", "trigger_id": triggerID, "is_deleted": deleted,
		"is_workflow_published": published}
}

// agentTasksUser is Atlas's bot user (users.list and users.info).
func (s *Server) agentTasksUser() slack.User {
	return slack.User{ID: AgentTasksUserID, TeamID: TeamID, Name: "atlas", RealName: AgentTasksAppName, IsBot: true,
		Profile: slack.Profile{RealName: AgentTasksAppName, Team: TeamID, BotID: AgentTasksBotID, APIAppID: AgentTasksAppID,
			Image48: s.URL + "img/48x48/app.png", Image72: s.URL + "img/72x72/app.png", Image192: s.URL + "img/192x192/app.png"},
		Presence: "active"}
}

// addAgentTasks adds Atlas's bot user and the viewer's DM with it.
func (s *Server) addAgentTasks() {
	s.users = append(s.users, s.agentTasksUser())
	s.convs = append(s.convs, slack.Conversation{ID: AgentTasksDMID, IsIM: true, User: AgentTasksUserID, IsOpen: true,
		Created: 1762500000, Priority: 0.8})
	s.hist[AgentTasksDMID] = []slack.Message{{Type: "message", TS: "1763400000.000100", User: AgentTasksUserID, BotID: AgentTasksBotID,
		Text: "Hi! Give me a prompt and a schedule and I'll run it for you."}}
}

// taskJSON is a workflow as functions.workflows.list sends it.
func (t *FakeTask) taskJSON() map[string]any {
	w := map[string]any{"id": t.ID, "title": t.Title, "description": "", "is_published": t.Published, "app_id": TaskAppID(t.ID),
		"collaborators": []string{SelfID}, "creation_source_type": "AGENT", "creation_source_id": AgentTasksAppID,
		"date_updated": t.DateUpdated, "is_empty": t.Empty, "bot_user_id": "U" + strings.TrimPrefix(t.ID, "Wf")}
	if !t.Empty {
		w["steps"] = []any{map[string]any{"id": "st-" + t.ID, "function_id": "Fn00SP3PAGT",
			"function": map[string]any{"callback_id": "send_prompt_to_3p_agent", "title": "Send a prompt to an agent", "type": "builtin"},
			"inputs":   map[string]any{"prompt": map[string]any{"value": t.promptValue()}, "agent_id": map[string]any{"value": AgentTasksAppID}}}}
	} else {
		w["steps"] = []any{}
	}
	return w
}

// promptValue is the step's inputs.prompt.value: the rich text, or the
// string.
func (t *FakeTask) promptValue() any {
	if t.PromptRich != nil {
		return t.PromptRich
	}
	return t.Prompt
}

// triggerJSON is the task's scheduled trigger as workflows.triggers.list
// sends it.
func (t *FakeTask) triggerJSON() map[string]any {
	sch := map[string]any{"start_time": t.StartTime, "timezone": AgentTasksTimezone}
	if t.Frequency != nil {
		sch["frequency"] = t.Frequency
	}
	return map[string]any{"id": TaskTriggerID(t.ID), "type": "scheduled", "name": t.Title, "description": "",
		"workflow": map[string]any{"id": t.ID, "workflow_id": t.ID, "app_id": TaskAppID(t.ID), "title": t.Title,
			"app": map[string]any{"id": TaskAppID(t.ID), "is_workflow_app": true}},
		"workflow_details": map[string]any{"is_published": t.Published, "collaborators": []string{SelfID}},
		"schedule":         sch, "owning_team_id": TeamID}
}

// agentTasksProfile is apps.profile.get for Atlas.
func (s *Server) agentTasksProfile() map[string]any {
	st := s.AgentTasks()
	st.mu.Lock()
	shown := st.ShownAsAgent
	st.mu.Unlock()
	icons := map[string]any{}
	for _, px := range []int{32, 48, 64, 72, 192} {
		icons["image_"+strconv.Itoa(px)] = s.URL + "img/" + strconv.Itoa(px) + "x" + strconv.Itoa(px) + "/app.png"
	}
	return map[string]any{"id": AgentTasksAppID, "name": AgentTasksAppName, "desc": "Runs your prompts on a schedule",
		"app_user_id": AgentTasksUserID, "team_id": TeamID, "is_installed": true, "is_agent_app": true, "is_shown_as_agent": shown,
		"icons": icons, "bot_user": map[string]any{"id": AgentTasksUserID, "username": "atlas"},
		"app_home_metadata": map[string]any{"home_tab_enabled": false, "messages_tab_enabled": true}}
}

// handleAgentTasks answers the Tasks tab's calls (Size.AgentTasks).
func (s *Server) handleAgentTasks(method string, f url.Values) (any, bool) {
	st := s.AgentTasks()
	record := func() {
		st.mu.Lock()
		st.Calls = append(st.Calls, AgentTaskCall{Method: method, Form: f})
		st.mu.Unlock()
	}
	switch method {
	case "apps.profile.get":
		if f.Get("bot") != AgentTasksBotID && f.Get("app") != AgentTasksAppID {
			return nil, false
		}
		return ok("app_profile", s.agentTasksProfile()), true
	case "bots.info":
		if !strings.Contains(","+f.Get("bots")+","+f.Get("bot")+",", ","+AgentTasksBotID+",") {
			return nil, false
		}
		return ok("bots", []slack.BotProfile{{ID: AgentTasksBotID, Name: "atlas", AppID: AgentTasksAppID, UserID: AgentTasksUserID, TeamID: TeamID}}), true
	case "conversations.info":
		if f.Get("channel") != AgentTasksDMID {
			return nil, false
		}
		c, _ := s.conv(AgentTasksDMID)
		out := ok("channel", c)
		if f.Get("return_app_home") == "true" {
			st.mu.Lock()
			msgs, tasks := st.MessagesTab, st.TasksTab && st.MessagesTab
			st.mu.Unlock()
			out["app_home"] = map[string]any{"app_id": AgentTasksAppID, "app_installed_team_id": TeamID, "conversation_id": AgentTasksDMID,
				"home_tab_enabled": false, "messages_tab_enabled": msgs, "messages_tab_read_only_enabled": false,
				"agent_tasks_enabled": tasks}
		}
		return out, true
	case "features.access.policies.list":
		st.mu.Lock()
		plan := st.Plan
		st.mu.Unlock()
		return ok("feature_policies", map[string]any{"user_groups__manage": map[string]any{"is_enabled": true},
			"agent_tasks__write": map[string]any{"is_enabled": plan}}), true
	case "team.prefs.get":
		prefs := map[string]any{}
		for k, v := range s.TeamPrefs {
			prefs[k] = v
		}
		st.mu.Lock()
		off := st.TeamAdminOff
		st.mu.Unlock()
		prefs["ai_apps_settings"] = map[string]any{"apps": []any{map[string]any{"app_id": AgentTasksAppID,
			"is_agent_tasks_enabled": !off, "is_ai_enabled": true}}, "allow_user_pinning": true}
		return ok("prefs", prefs), true
	case "enterprise.prefs.get":
		st.mu.Lock()
		off := st.AdminOff
		st.mu.Unlock()
		locked := map[string]any{"ai_apps_settings": map[string]any{"apps": []any{map[string]any{"app_id": AgentTasksAppID,
			"is_agent_tasks_enabled": !off, "is_ai_enabled": true}}, "allow_user_pinning": true}}
		return ok("org_prefs", map[string]any{}, "locked_prefs", locked, "unlocked_prefs", map[string]any{}), true
	case "functions.workflows.list":
		var fo struct {
			Name               string `json:"name"`
			CreationSourceType string `json:"creation_source_type"`
			CreationSourceID   string `json:"creation_source_id"`
		}
		_ = json.Unmarshal([]byte(f.Get("filter_options")), &fo)
		if fo.CreationSourceType != "AGENT" || fo.CreationSourceID != AgentTasksAppID {
			return nil, false
		}
		record()
		st.mu.Lock()
		defer st.mu.Unlock()
		if st.ListError {
			return map[string]any{"ok": false, "error": "internal_error"}, true
		}
		var out []map[string]any
		q := strings.ToLower(fo.Name)
		for _, t := range st.Tasks {
			if t.Deleted || q != "" && !strings.Contains(strings.ToLower(t.Title), q) {
				continue
			}
			out = append(out, t.taskJSON())
		}
		// sort_options {column date_updated, direction desc}
		for i := 1; i < len(out); i++ {
			for j := i; j > 0 && out[j]["date_updated"].(int64) > out[j-1]["date_updated"].(int64); j-- {
				out[j], out[j-1] = out[j-1], out[j]
			}
		}
		limit, _ := strconv.Atoi(f.Get("limit"))
		if limit <= 0 {
			limit = 50
		}
		start, _ := strconv.Atoi(strings.TrimPrefix(f.Get("cursor"), "page:"))
		if start > len(out) {
			start = len(out)
		}
		end := min(start+limit, len(out))
		next := ""
		if end < len(out) {
			next = "page:" + strconv.Itoa(end)
		}
		page := out[start:end]
		if page == nil {
			page = []map[string]any{}
		}
		return ok("workflows", page, "response_metadata", map[string]any{"next_cursor": next}), true
	case "workflows.triggers.list":
		ids := map[string]bool{}
		for _, id := range strings.Split(f.Get("app_ids"), ",") {
			ids[id] = true
		}
		st.mu.Lock()
		defer st.mu.Unlock()
		out, owned := []map[string]any{}, false
		for _, t := range st.Tasks {
			if !ids[TaskAppID(t.ID)] {
				continue
			}
			owned = true // one of Atlas's workflows (a draft has no trigger)
			if !t.Deleted && !t.Empty {
				out = append(out, t.triggerJSON())
			}
		}
		if !owned {
			return nil, false
		}
		st.Calls = append(st.Calls, AgentTaskCall{Method: method, Form: f})
		return ok("triggers", out), true
	case "functions.workflows.publish", "functions.workflows.unpublish", "functions.workflows.delete":
		st.mu.Lock()
		t := st.task(f.Get("workflow_id"))
		st.mu.Unlock()
		if t == nil {
			return nil, false
		}
		record()
		st.mu.Lock()
		switch method {
		case "functions.workflows.publish":
			t.Published = true
		case "functions.workflows.unpublish":
			t.Published = false
		default:
			t.Deleted = true
		}
		st.mu.Unlock()
		return ok(), true
	case "workflows.triggers.delete":
		id := strings.TrimPrefix(f.Get("trigger_id"), "Ft")
		st.mu.Lock()
		t := st.task("Wf" + id)
		st.mu.Unlock()
		if t == nil {
			return nil, false
		}
		record()
		return ok(), true
	case "chat.postMessage":
		if f.Get("channel") == AgentTasksDMID {
			record() // the generic handler answers it
		}
		return nil, false
	}
	return nil, false
}

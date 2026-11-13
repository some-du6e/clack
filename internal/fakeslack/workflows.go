package fakeslack

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/some-du6e/clack/internal/slack"
)

// Workflow fixtures (docs/WORKFLOWS.md): Size.Workflows adds
// #workflows-gallery with each way a workflow starts from a message (a
// workflow link, a workflow_button, a workflow step's message whose
// button carries function_execution_id, a link to a workflow the viewer
// may not run), a folder tab bookmarking a workflow, the workflow tab, and
// the Workflow Builder methods Slack's client calls for them:
// workflows.triggers.trip (recorded), workflows.triggers.info and
// search.autocomplete.triggers. Every name and text is made up.

const (
	WorkflowsChannelID = "C0WORKFLOW1"
	workflowsBaseTS    = 1762800000

	// WorkflowBuilderBotID is the fixtures' Workflow Builder bot (the
	// app id is Workflow Builder's own, so the sender rules apply).
	WorkflowBuilderBotID  = "B0FAKEWFB01"
	WorkflowBuilderAppID  = "A0112L5RZ5E"
	WorkflowStepExecution = "Fx0FAKESTEP1" // the step message's function_execution_id

	// The triggers: a link trigger that runs, one that wants its risky
	// scopes acknowledged first (then runs slowly), one the viewer may
	// not run, and a scheduled one (search lists it with automated
	// triggers only).
	TriggerDeploy  = "Ft0FAKEWF01"
	TriggerTimeOff = "Ft0FAKEWF02"
	TriggerBug     = "Ft0FAKEWF04"
	TriggerStandup = "Ft0FAKEWF03"

	WorkflowsBookmarkID = "Bk0FAKEWF01" // the folder tab's bookmarked workflow

	// TriggerHidden is a featured workflow the viewer may not see
	// (workflows.triggers.preview answers access_denied).
	TriggerHidden = "Ft0FAKEWF99"
)

// WorkflowLink is a fixture trigger's shortcut URL.
func WorkflowLink(id string) string {
	return "https://slack.com/shortcuts/" + id + "/" + strings.ToLower(id) + "0abc"
}

// WorkflowShareURL is a fixture trigger's share URL.
func WorkflowShareURL(id string) string {
	return "https://slack.com/shortcuts/" + id + "/" + strings.ToLower(id) + "0share"
}

type fakeTrigger struct {
	id, typ, name, desc, wf, title string
}

var fakeTriggers = []fakeTrigger{
	{TriggerDeploy, "shortcut", "Run deploy", "Deploy the main branch to staging", "Wf0FAKE01", "Deploy"},
	{TriggerTimeOff, "shortcut", "Request time off", "Ask your lead for days off", "Wf0FAKE02", "Time off"},
	{TriggerBug, "shortcut", "Report a bug", "File a bug with the platform team", "Wf0FAKE04", "Bug report"},
	{TriggerStandup, "scheduled", "Weekly standup", "Posts the standup thread every Monday", "Wf0FAKE03", "Standup"},
}

// WorkflowManagers are the fixture workflows' collaborators (the
// workflow details view's "Managed by").
var WorkflowManagers = []string{ProfileID, AdminID}

// workflowSteps are Run deploy's steps (workflow_details.steps and
// functions.workflows.get): a form, a branch on its answer (a coloured
// branch and the fallback) and a pin; made-up titles.
func workflowSteps() []map[string]any {
	fn := func(cb, title string) map[string]any {
		return map[string]any{"callback_id": cb, "title": title, "type": "builtin"}
	}
	return []map[string]any{
		{"id": "s1", "function": fn("open_form", "Collect deploy details")},
		{"id": "s2", "type": "switch", "function": fn("switch_step", "Check the environment"), "steps": []map[string]any{
			{"id": "s2a", "expression": map[string]any{"op": "eq"}, "display_config": map[string]any{"color": "jade"},
				"steps": []map[string]any{{"id": "s2a1", "function": fn("send_message", "Announce the deploy")}}},
			{"id": "s2b", "steps": []map[string]any{{"id": "s2b1", "function": fn("send_dm", "Ask for approval")}}},
		}},
		{"id": "s3", "function": fn("add_pin", "Pin the deploy note")},
	}
}

func (t fakeTrigger) json(base string) map[string]any {
	details := map[string]any{"is_published": true, "bot_user_id": "U" + t.wf[2:]}
	if t.id == TriggerDeploy {
		details["collaborators"], details["steps"] = WorkflowManagers, workflowSteps()
	}
	m := map[string]any{"id": t.id, "type": t.typ, "name": t.name, "description": t.desc, "owning_team_id": TeamID,
		"workflow": map[string]any{"id": "Fn" + t.wf[2:], "workflow_id": t.wf, "app_id": "A" + t.wf[2:], "title": t.title,
			"app": map[string]any{"id": "A" + t.wf[2:], "name": t.title, "is_workflow_app": true,
				"icons": map[string]string{"image_36": base + "img/wf-" + t.id + "-36.png", "image_72": base + "img/wf-" + t.id + "-72.png"}}},
		"workflow_details": details}
	if t.typ == "shortcut" {
		m["shortcut_url"] = WorkflowLink(t.id)
		m["share_url"] = WorkflowShareURL(t.id)
	}
	return m
}

// TripCall is one workflows.triggers.trip call.
type TripCall struct {
	URL, ClientToken string
	Precheck         string // run_precheck as sent ("" when left out)
	Context          map[string]any
}

type wfState struct {
	mu       sync.Mutex
	trips    []TripCall
	runs     []url.Values     // apps.actions.run
	added    []map[string]any // bookmarks.add
	featured []url.Values     // workflows.featured.add
}

// FeaturedAdds returns the workflows.featured.add calls so far.
func (s *Server) FeaturedAdds() []url.Values {
	st := s.wf()
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]url.Values(nil), st.featured...)
}

// AddedBookmarks returns the bookmarks bookmarks.add created.
func (s *Server) AddedBookmarks() []map[string]any {
	st := s.wf()
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]map[string]any(nil), st.added...)
}

var wfStates sync.Map // *Server -> *wfState

func (s *Server) wf() *wfState {
	v, _ := wfStates.LoadOrStore(s, &wfState{})
	return v.(*wfState)
}

// TripCalls returns the workflows.triggers.trip calls so far.
func (s *Server) TripCalls() []TripCall {
	st := s.wf()
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]TripCall(nil), st.trips...)
}

func workflowsTS(i int) string { return strconv.Itoa(workflowsBaseTS+i*60) + ".000300" }

// WorkflowsTS is the ts of #workflows-gallery's i-th message (0: the
// link, 1: the workflow button, 2: the step message, 3: the bug link).
func WorkflowsTS(i int) string { return workflowsTS(i) }

func (s *Server) addWorkflowsChannel() {
	tabs, _ := json.Marshal(map[string]any{"tabs": []map[string]any{
		{"id": "Ct0FAKEWFB", "type": "folder", "label": ""},
		{"id": "workflows", "type": "workflows", "label": ""},
	}, "tabz": []map[string]any{ // the store's copy a channel-tab slug resolves against (findTabInChannelProperties)
		{"id": "Ct0FAKEWFB", "type": "folder", "label": ""},
		{"id": "workflows", "type": "workflows", "label": ""},
		// slugs only (not on the tab bar): a list and a canvas the
		// fixtures serve, for their tab peeks
		{"id": "Ct" + ListTodos, "type": "list", "label": "", "data": map[string]any{"file_id": ListTodos}},
		{"id": "Ct" + CanvasChannel, "type": "canvas", "label": "", "data": map[string]any{"file_id": CanvasChannel}},
	}, "channel_workflows": []map[string]any{ // the Workflows tab's featured workflows
		{"workflow_trigger_id": TriggerDeploy, "title": "Run deploy"},
		{"workflow_trigger_id": TriggerHidden, "title": "Payroll export"},
	}})
	s.convs = append(s.convs, slack.Conversation{ID: WorkflowsChannelID, Name: "workflows-gallery", IsChannel: true, IsMember: true,
		NumMembers: 5, ContextTeamID: TeamID, Purpose: &slack.Purpose{Value: "Workflow fixtures."}, Properties: tabs})
	bot := &slack.BotProfile{ID: WorkflowBuilderBotID, Name: "Workflow Builder", AppID: WorkflowBuilderAppID, TeamID: TeamID}
	sec := func(text string) map[string]any {
		return map[string]any{"type": "section", "block_id": "wf_sec_" + strconv.Itoa(len(text)), "text": map[string]any{"type": "mrkdwn", "text": text}}
	}
	wfButton, _ := json.Marshal([]map[string]any{
		sec("Ship it when the checks are green."),
		{"type": "actions", "block_id": "wf_actions", "elements": []map[string]any{{
			"type": "workflow_button", "action_id": "wf_deploy", "text": map[string]any{"type": "plain_text", "text": "Run deploy"}, "style": "primary",
			"workflow": map[string]any{"trigger": map[string]any{"url": WorkflowLink(TriggerDeploy),
				"customizable_input_parameters": []map[string]any{{"name": "env", "value": "staging"}}}}}}},
	})
	step, _ := json.Marshal([]map[string]any{
		sec("A deploy to staging is waiting for your approval."),
		{"type": "actions", "block_id": "wf_step", "elements": []map[string]any{
			{"type": "button", "action_id": "wf_approve", "text": map[string]any{"type": "plain_text", "text": "Approve"}, "style": "primary", "value": "approve"},
			{"type": "button", "action_id": "wf_deny", "text": map[string]any{"type": "plain_text", "text": "Deny"}, "value": "deny"},
		}},
	})
	user := s.users[1%len(s.users)].ID
	msgs := []slack.Message{
		{Type: "message", TS: workflowsTS(0), User: user, Text: "Need a day off? Use <" + WorkflowLink(TriggerTimeOff) + "|Request time off>"},
		{Type: "message", TS: workflowsTS(1), BotID: WorkflowBuilderBotID, BotProfile: bot, Text: "Ship it when the checks are green.", Blocks: wfButton},
		{Type: "message", TS: workflowsTS(2), BotID: WorkflowBuilderBotID, BotProfile: bot, Text: "A deploy to staging is waiting for your approval.",
			Blocks: step, FunctionExecutionID: WorkflowStepExecution},
		{Type: "message", TS: workflowsTS(3), User: user, Text: "Found one? <" + WorkflowLink(TriggerBug) + "|Report a bug>"},
	}
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 { // history is newest first
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	s.hist[WorkflowsChannelID] = msgs
}

// The channel's automations (channel details > Automations): two
// workflows whose bots are members (Run deploy, Request time off) and a
// legacy workflow (a channel action).
const (
	LegacyActionID   = "Ac0FAKELEG1"
	LegacyActionName = "Office hours sign-up"
)

// workflowBots are the gallery channel's workflow bot members, with their
// workflow apps (functions.workflows.findWorkflowForBotUsers).
var workflowBots = []struct{ user, app, workflow, trigger string }{
	{"U0FAKE01", "A0FAKE01", "Wf0FAKE01", TriggerDeploy},
	{"U0FAKE02", "A0FAKE02", "Wf0FAKE02", TriggerTimeOff},
}

// ActionRuns returns the apps.actions.run calls so far.
func (s *Server) ActionRuns() []url.Values {
	st := s.wf()
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]url.Values(nil), st.runs...)
}

// handleWorkflows answers the Workflow Builder methods.
func (s *Server) handleWorkflows(method string, f url.Values, edge map[string]json.RawMessage) (any, bool) {
	switch method {
	case "edge/users/list":
		var filter string
		_ = json.Unmarshal(edge["filter"], &filter)
		var chans []string
		_ = json.Unmarshal(edge["channels"], &chans)
		if filter != "workflows NOT deactivated" {
			return nil, false
		}
		s.convMu.Lock()
		s.listFilters = append(s.listFilters, filter) // UserListFilters
		s.convMu.Unlock()
		out := []map[string]any{}
		if len(chans) == 1 && chans[0] == WorkflowsChannelID {
			for _, b := range workflowBots {
				out = append(out, map[string]any{"id": b.user, "team_id": TeamID, "is_bot": true, "is_workflow_bot": true})
			}
		}
		return ok("results", out, "next_marker", ""), true
	case "functions.workflows.findWorkflowForBotUsers":
		apps := []map[string]any{}
		if f.Get("channel_id") == WorkflowsChannelID {
			for _, b := range workflowBots {
				if strings.Contains(","+f.Get("user_ids")+",", ","+b.user+",") {
					apps = append(apps, map[string]any{"app_id": b.app, "workflow_id": b.workflow, "share_url": WorkflowShareURL(b.trigger),
						"app_details": map[string]any{"icons": map[string]string{"image_36": s.URL + "img/wfapp-" + b.app + "-36.png"}}})
				}
			}
		}
		return ok("workflow_apps", apps), true
	case "functions.workflows.list":
		var ids []string
		_ = json.Unmarshal([]byte(f.Get("workflow_ids")), &ids)
		out := []map[string]any{}
		for _, t := range fakeTriggers {
			for _, id := range ids {
				if id == t.wf {
					out = append(out, map[string]any{"id": t.wf, "title": t.title, "description": t.desc, "is_published": true})
				}
			}
		}
		return ok("workflows", out), true
	case "apps.actions.listForResource":
		if f.Get("resource_type") != "channel" || f.Get("resource_id") != WorkflowsChannelID {
			return ok("actions", []any{}), true
		}
		return ok("actions", []map[string]any{{"action_id": LegacyActionID, "name": LegacyActionName, "description": "Pick a slot for this week",
			"type": "channel_action", "install_team_id": TeamID, "install_team_name": "Fake Club",
			"app": map[string]any{"app_id": "A0FAKELEG1", "icons": map[string]string{"image_64": s.URL + "img/legacy-64.png"}}}}), true
	case "workflows.triggers.preview":
		out := []map[string]any{}
		for _, id := range strings.Split(f.Get("trigger_ids"), ",") {
			found := false
			for _, t := range fakeTriggers {
				if t.id == id {
					out, found = append(out, t.json(s.URL)), true
				}
			}
			if !found && id != "" {
				out = append(out, map[string]any{"id": id, "error": "access_denied"})
			}
		}
		return ok("triggers", out), true
	case "edge/permissions/info":
		var perms []string
		_ = json.Unmarshal(edge["permissions"], &perms)
		var ch string
		_ = json.Unmarshal(edge["channel_id"], &ch)
		for _, p := range perms {
			if p == "MANAGE_CHANNEL_WORKFLOWS" {
				allowed := []string{}
				if ch == WorkflowsChannelID {
					allowed = append(allowed, p)
				}
				return ok("allowed", allowed, "allowed_per_team", map[string]any{}), true
			}
		}
		return nil, false
	case "workflows.featured.add":
		st := s.wf()
		st.mu.Lock()
		st.featured = append(st.featured, f)
		st.mu.Unlock()
		return ok(), true
	case "bookmarks.add":
		if f.Get("channel") != WorkflowsChannelID {
			return nil, false
		}
		st := s.wf()
		st.mu.Lock()
		b := map[string]any{"id": "Bk0FAKEADD" + strconv.Itoa(len(st.added)+1), "channel_id": WorkflowsChannelID, "title": f.Get("title"),
			"link": f.Get("link"), "type": f.Get("type"), "shortcut_id": f.Get("shortcut_id"), "rank": "z" + strconv.Itoa(len(st.added)),
			"date_created": workflowsBaseTS + 3600}
		st.added = append(st.added, b)
		st.mu.Unlock()
		return ok("bookmark", b), true
	case "apps.actions.run":
		st := s.wf()
		st.mu.Lock()
		st.runs = append(st.runs, f)
		st.mu.Unlock()
		return ok(), true
	case "workflows.triggers.trip":
		c := TripCall{URL: f.Get("url"), ClientToken: f.Get("client_token"), Precheck: f.Get("run_precheck")}
		_ = json.Unmarshal([]byte(f.Get("context")), &c.Context)
		st := s.wf()
		st.mu.Lock()
		st.trips = append(st.trips, c)
		st.mu.Unlock()
		switch id := tripTriggerID(c.URL); {
		case id == TriggerTimeOff && c.Precheck == "true":
			return map[string]any{"ok": false, "error": "pretrip_ack_required", "pre_trip_info": map[string]any{
				"scopes_acknowledgements": map[string]any{"is_shown": true, "bot_scopes": []map[string]any{
					{"name": "chat:write", "description": "Send messages to channels and people", "action_type": "do"},
					{"name": "users:read", "description": "View people in the workspace", "action_type": "view"},
				}}}}, true
		case id == TriggerTimeOff:
			return ok("is_slow_workflow", true), true
		case id == TriggerBug:
			return map[string]any{"ok": false, "error": "access_denied", "error_messages": []string{"unsupported_private_channel"}}, true
		case id == "":
			return map[string]any{"ok": false, "error": "invalid_url"}, true
		}
		return ok(), true
	case "functions.workflows.get":
		for _, t := range fakeTriggers {
			if t.wf != f.Get("workflow_id") {
				continue
			}
			w := map[string]any{"id": t.wf, "title": t.title, "description": t.desc, "is_published": true, "bot_user_id": "U" + t.wf[2:],
				"icons": map[string]string{"image_96": s.URL + "img/wf-" + t.id + "-96.png"}}
			if t.id == TriggerDeploy {
				w["collaborators"], w["steps"] = WorkflowManagers, workflowSteps()
			}
			return ok("workflow", w), true
		}
		return map[string]any{"ok": false, "error": "workflow_not_found"}, true
	case "workflows.triggers.info":
		for _, t := range fakeTriggers {
			if t.id == f.Get("trigger_id") {
				return ok("trigger", t.json(s.URL)), true
			}
		}
		return map[string]any{"ok": false, "error": "trigger_not_found"}, true
	case "search.autocomplete.triggers":
		var types []string
		_ = json.Unmarshal([]byte(f.Get("trigger_types")), &types)
		q := strings.ToLower(f.Get("query"))
		out := []map[string]any{}
		for _, t := range fakeTriggers {
			kind := false
			for _, ty := range types {
				kind = kind || ty == t.typ
			}
			if kind && (q == "" || strings.Contains(strings.ToLower(t.name), q)) {
				out = append(out, t.json(s.URL))
			}
		}
		return ok("triggers", out), true
	case "bookmarks.list":
		if f.Get("channel_id") != WorkflowsChannelID && f.Get("channel") != WorkflowsChannelID {
			return nil, false
		}
		st := s.wf()
		st.mu.Lock()
		out := append([]map[string]any{{"id": WorkflowsBookmarkID, "channel_id": WorkflowsChannelID, "title": "Request time off",
			"link": WorkflowLink(TriggerTimeOff), "type": "shortcut", "shortcut_id": TriggerTimeOff, "rank": "a", "parent_id": "Ct0FAKEWFB",
			"date_created": workflowsBaseTS}}, st.added...)
		st.mu.Unlock()
		return ok("bookmarks", out), true
	}
	return nil, false
}

func tripTriggerID(link string) string {
	for _, part := range strings.Split(link, "/") {
		if strings.HasPrefix(part, "Ft") {
			return part
		}
	}
	return ""
}

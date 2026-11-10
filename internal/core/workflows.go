package core

// Workflows (Slack's Workflow Builder platform): tripping link and shortcut
// triggers the way Slack's web client does, the trigger lookups its
// speedbump, toasts and lists need, and the trigger search behind the
// workflow picker. Every call shape is from Slack's web client (gantry-v2
// build a84662d2d135, 2026-11-08); docs/WORKFLOWS.md names the modules.
//
// Scraping policy: triggers are kept in memory only.

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/some-du6e/clack/internal/slack"
)

// ---- link triggers ----

// WorkflowTriggerID returns the trigger id of a workflow link
// (https://slack.com/shortcuts/Ft…/…), or "" (Slack's
// getTriggerIdFromShortcutUrl).
func WorkflowTriggerID(link string) string {
	u, err := url.Parse(link)
	if err != nil || !strings.HasSuffix(u.Hostname(), "slack.com") || !strings.Contains(u.Path, "/shortcuts/") {
		return ""
	}
	for _, part := range strings.Split(u.Path, "/") {
		if len(part) >= 10 && strings.HasPrefix(part, "Ft") {
			return part
		}
	}
	return ""
}

// TripContext says where a trigger was tripped from: Slack's
// workflowLinkTriggerUIContext, snake_cased the way its API fetcher
// sends it (snakeCaseDeep), JSON in the trip's context argument.
type TripContext map[string]any

// MessageTripContext is a workflow link's context: the message it is in
// (location message), or markdown_link when it isn't in one (JQmE).
func MessageTripContext(ch, ts string) TripContext {
	if ch == "" {
		return TripContext{"location": "markdown_link"}
	}
	return TripContext{"location": "message", "channel_id": ch, "message_ts": ts}
}

// ButtonTripContext is a workflow_button's (WorkflowButtonElement, C7fj):
// location button with the element's action and block ids, the channel
// and the message ts; an ephemeral message's button also sends the
// trigger's customizable_input_parameters.
func ButtonTripContext(actionID, blockID, ch, ts string, ephemeral bool, inputs json.RawMessage) TripContext {
	c := TripContext{"location": "button", "action_id": actionID, "block_id": blockID, "channel_id": ch, "message_ts": ts}
	if ephemeral && len(inputs) > 0 && string(inputs) != "null" {
		c["customizable_input_parameters"] = inputs
	}
	return c
}

// BookmarkTripContext is a channel's bookmarked workflow
// (useChannelWorkflowsExplorer): location bookmark.
func BookmarkTripContext(ch, bookmarkID string) TripContext {
	return TripContext{"location": "bookmark", "channel_id": ch, "bookmark_id": bookmarkID}
}

// ChannelDetailsTripContext is a workflow started from channel details
// (WorkflowAppItem): location channel_details.
func ChannelDetailsTripContext(ch string) TripContext {
	return TripContext{"location": "channel_details", "channel_id": ch}
}

// Location is the context's location ("" when unset).
func (c TripContext) Location() string {
	s, _ := c["location"].(string)
	return s
}

// TripResult is what a successful workflows.triggers.trip answered.
type TripResult struct {
	Slow    bool // is_slow_workflow: Slack's "might take a little longer" toast
	AISteps bool // contains_ai_steps
}

// TripError is a trip Slack refused: error, error_messages[0] (the
// subtype Slack's error toast reads) and, for pretrip_ack_required, the
// pre_trip_info its speedbump is built from.
type TripError struct {
	Code, SubType string
	PreTrip       *PreTripInfo
}

func (e *TripError) Error() string { return e.Code }

// PreTripInfo is pre_trip_info: what the workflow wants acknowledged or
// filled in before it runs (Slack's WorkflowTriggerSpeedbumpModal).
type PreTripInfo struct {
	ShowSpeedbump   bool `json:"show_speedbump"`
	ThirdPartyAuths struct {
		IsShown     bool            `json:"is_shown"`
		IsSatisfied bool            `json:"is_satisfied"`
		Auths       json.RawMessage `json:"auths"`
	} `json:"third_party_auths"`
	MissingContextParameters struct {
		IsShown bool `json:"is_shown"`
	} `json:"missing_context_parameters"`
	ScopesAcknowledgements struct {
		IsShown   bool       `json:"is_shown"`
		BotScopes []BotScope `json:"bot_scopes"`
	} `json:"scopes_acknowledgements"`
	MissingCustomizableInputParameters struct {
		IsShown bool `json:"is_shown"`
	} `json:"missing_customizable_input_parameters"`
	ContainsAISteps           bool              `json:"contains_ai_steps"`
	RequiredContextParameters []json.RawMessage `json:"required_context_parameters"`
}

// BotScope is one thing a workflow will be able to do (the speedbump's
// "Review workflow access" list): action_type do (pencil) or view (eye).
type BotScope struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ActionType  string `json:"action_type"`
}

// NeedsSpeedbump is Slack's shouldShowPrecheckSpeedbump: whether the
// precheck's answer opens the speedbump modal rather than the error
// toast. location is the trip's; uiPage the surface it ran from.
func (p *PreTripInfo) NeedsSpeedbump(location, uiPage string) bool {
	if p == nil {
		return false
	}
	show := location == "search" // showSpeedbump: I?.location === "search"
	switch uiPage {
	case "workflow_gallery", "workflows_recently_viewed", "workflow_details_modal", "workflow_details_view":
		show = false
	}
	return show || p.ThirdPartyAuths.IsShown && !p.ThirdPartyAuths.IsSatisfied || p.MissingContextParameters.IsShown ||
		p.ScopesAcknowledgements.IsShown || p.MissingCustomizableInputParameters.IsShown || p.ContainsAISteps
}

// TripTrigger runs a link trigger (Slack's workflowsTriggersTrip, 0/Om):
// workflows.triggers.trip with the link, the context, a client_token this
// client issued (the forms the workflow opens arrive as view_opened with
// it) and, on the first try, run_precheck. A workflow that wants a
// speedbump answers pretrip_ack_required with pre_trip_info; the second
// try leaves run_precheck out, as Slack's speedbump does.
func (ix *Interactive) TripTrigger(ctx context.Context, link string, tc TripContext, precheck bool) (TripResult, error) {
	cb, _ := json.Marshal(tc)
	p := url.Values{"url": {link}, "context": {string(cb)}, "client_token": {ix.ClientToken()}}
	if precheck {
		p.Set("run_precheck", "true")
	}
	var res struct {
		Slow bool `json:"is_slow_workflow"`
		AI   bool `json:"contains_ai_steps"`
	}
	err := ix.call(ctx, "workflows.triggers.trip", p, &res)
	logCall("workflows.triggers.trip", "trigger="+WorkflowTriggerID(link)+" location="+tc.Location(), err)
	if err != nil {
		var se *slack.Error
		if !errors.As(err, &se) {
			return TripResult{}, err
		}
		te := &TripError{Code: se.Code}
		var body struct {
			Messages []string     `json:"error_messages"`
			PreTrip  *PreTripInfo `json:"pre_trip_info"`
		}
		if json.Unmarshal(se.Body, &body) == nil {
			if len(body.Messages) > 0 {
				te.SubType = body.Messages[0]
			}
			te.PreTrip = body.PreTrip
		}
		return TripResult{}, te
	}
	return TripResult{Slow: res.Slow, AISteps: res.AI}, nil
}

// TripErrorText is Slack's SlackFunctionTriggerErrorToast text for a
// failed trip (Izws), markup removed.
func TripErrorText(code, sub string) string {
	switch code {
	case "access_denied":
		switch sub {
		case "unsupported_private_channel":
			return "Unfortunately you can’t run new workflows in private channels"
		case "unsupported_slack_connect_channel":
			return "Unfortunately you can’t run new workflows in Slack Connect"
		case "away_user_home_only_workflow":
			return "People from external orgs can’t use this workflow. Learn more about workflow restrictions."
		case "frontline_user_ai_workflow":
			return "Frontline members can’t use workflows with AI steps."
		case "":
			return "You don’t have permissions to run this workflow"
		}
		// another subtype: Slack's switch breaks out with the default text
	case "trigger_not_found":
		return "This workflow no longer exists."
	case "not_opted_into_overages", "reached_upper_spend_limit", "free_team_not_allowed":
		return "Workflow currently unavailable. Contact a workspace owner for help."
	}
	return "Oops, there was an error running that workflow"
}

// ---- triggers ----

// WorkflowTrigger is a trigger as workflows.triggers.info and
// search.autocomplete.triggers return it: the fields Slack's
// createShortcutFromTriggerAndWorkflow (jsMl) reads.
type WorkflowTrigger struct {
	ID          string // Ft…
	Type        string // shortcut | event | scheduled | webhook | external | service
	Name        string
	Description string
	ShortcutURL string // shortcut triggers: what tripping uses
	ShareURL    string // what "Copy workflow link" and attachments use
	OwningTeam  string
	WorkflowID  string
	AppID       string
	AppName     string
	// WorkflowTitle / WorkflowDescription stand in for an unnamed trigger.
	WorkflowTitle, WorkflowDescription string
	Icon                               string // the workflow app's icon
	Published                          bool   // workflow_details.is_published (true without details)
	BotUserID                          string
	Error                              string // a lookup that failed (access_denied…)
}

// Title is the shortcut's title: the trigger's name, else its workflow's.
func (t WorkflowTrigger) Title() string { return firstNonEmpty(t.Name, t.WorkflowTitle) }

// Desc is the shortcut's description, the same way.
func (t WorkflowTrigger) Desc() string { return firstNonEmpty(t.Description, t.WorkflowDescription) }

// IsShortcut reports a link (shortcut) trigger, the kind a click runs.
func (t WorkflowTrigger) IsShortcut() bool {
	return t.Type == "shortcut" || t.Type == "" && t.ShortcutURL != ""
}

type rawTrigger struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	ShortcutURL  string `json:"shortcut_url"`
	ShareURL     string `json:"share_url"`
	OwningTeamID string `json:"owning_team_id"`
	Workflow     struct {
		ID          string `json:"id"`
		WorkflowID  string `json:"workflow_id"`
		AppID       string `json:"app_id"`
		Title       string `json:"title"`
		Description string `json:"description"`
		App         struct {
			ID    string            `json:"id"`
			Name  string            `json:"name"`
			Icons map[string]string `json:"icons"`
		} `json:"app"`
	} `json:"workflow"`
	Details *struct {
		IsPublished *bool  `json:"is_published"`
		BotUserID   string `json:"bot_user_id"`
	} `json:"workflow_details"`
}

func (r rawTrigger) trigger() WorkflowTrigger {
	t := WorkflowTrigger{ID: r.ID, Type: r.Type, Name: r.Name, Description: r.Description, ShortcutURL: r.ShortcutURL,
		ShareURL: r.ShareURL, OwningTeam: r.OwningTeamID, WorkflowID: firstNonEmpty(r.Workflow.WorkflowID, r.Workflow.ID),
		AppID: firstNonEmpty(r.Workflow.AppID, r.Workflow.App.ID), AppName: r.Workflow.App.Name,
		WorkflowTitle: r.Workflow.Title, WorkflowDescription: r.Workflow.Description, Published: true}
	for _, k := range []string{"image_36", "image_48", "image_72", "image_64", "image_32", "image_96", "image_192"} {
		if s := r.Workflow.App.Icons[k]; s != "" {
			t.Icon = s
			break
		}
	}
	if d := r.Details; d != nil {
		if d.IsPublished != nil {
			t.Published = *d.IsPublished
		}
		t.BotUserID = d.BotUserID
	}
	return t
}

// wfState is an App's workflow memory: triggers by id.
type wfState struct {
	mu       sync.Mutex
	triggers map[string]WorkflowTrigger
}

var wfStates sync.Map // *App -> *wfState

func (ix *Interactive) wf() *wfState {
	v, _ := wfStates.LoadOrStore(ix.a, &wfState{triggers: map[string]WorkflowTrigger{}})
	return v.(*wfState)
}

func (ix *Interactive) putTriggers(ts ...WorkflowTrigger) {
	st := ix.wf()
	st.mu.Lock()
	for _, t := range ts {
		if t.ID != "" {
			st.triggers[t.ID] = t
		}
	}
	st.mu.Unlock()
}

// Trigger returns a trigger already looked up or found by a search.
func (ix *Interactive) Trigger(id string) (WorkflowTrigger, bool) {
	st := ix.wf()
	st.mu.Lock()
	defer st.mu.Unlock()
	t, ok := st.triggers[id]
	return t, ok
}

// TeamID is the viewer's workspace (Workflow Builder's URL names it).
func (ix *Interactive) TeamID() string { return ix.teamID() }

// OwningTeamName is the workspace that owns t when it isn't the viewer's
// (Slack's speedbump names it), once EnsureTeam has read it; else "".
func (ix *Interactive) OwningTeamName(t WorkflowTrigger) string {
	if t.OwningTeam == "" || t.OwningTeam == ix.teamID() {
		return ""
	}
	name, _, _ := ix.a.Team(t.OwningTeam, 20)
	return name
}

// TriggerInfo looks a trigger up (Slack's trigger store fetch,
// fetchTriggersForTeamId: workflows.triggers.info with
// include_workflow_details) and keeps it in memory.
func (ix *Interactive) TriggerInfo(ctx context.Context, id string) (WorkflowTrigger, error) {
	var res struct {
		Trigger rawTrigger `json:"trigger"`
	}
	err := ix.call(ctx, "workflows.triggers.info", url.Values{"trigger_id": {id}, "include_workflow_details": {"true"}}, &res)
	logCall("workflows.triggers.info", "trigger="+id, err)
	if err != nil {
		return WorkflowTrigger{ID: id, Error: ErrCode(err)}, err
	}
	t := res.Trigger.trigger()
	if t.ID == "" {
		t.ID = id
	}
	ix.putTriggers(t)
	return t, nil
}

// SearchTriggers is the workflow picker's remote search (Slack's
// fetchAndUpsertTriggersWithQuery): search.autocomplete.triggers with the
// query (empty lists them all), the current channel, strict exposure
// rules and the trigger types: every kind with automated triggers,
// shortcuts only without.
func (ix *Interactive) SearchTriggers(ctx context.Context, query, channelID string, automated bool) ([]WorkflowTrigger, error) {
	types := `["shortcut"]`
	if automated {
		types = `["shortcut","event","webhook","scheduled","service"]`
	}
	p := url.Values{"query": {query}, "strict_exposure_rules": {"true"}, "trigger_types": {types}}
	if channelID != "" {
		p.Set("current_channel_id", channelID)
	}
	var res struct {
		Triggers []rawTrigger `json:"triggers"`
	}
	err := ix.call(ctx, "search.autocomplete.triggers", p, &res)
	logCall("search.autocomplete.triggers", "n="+strconv.Itoa(len(res.Triggers)), err)
	if err != nil {
		return nil, err
	}
	out := make([]WorkflowTrigger, 0, len(res.Triggers))
	for _, r := range res.Triggers {
		out = append(out, r.trigger())
	}
	ix.putTriggers(out...)
	return out, nil
}

// ErrCode is a Slack error's code, else the error text.
func ErrCode(err error) string {
	var se *slack.Error
	if errors.As(err, &se) {
		return se.Code
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

// ---- a channel's workflows (channel details > Automations) ----

// ChannelAction is a channel's legacy workflow (Slack's channelActions
// store: apps.actions.listForResource for the channel), drawn with its
// app's icon and the WORKFLOW badge.
type ChannelAction struct {
	ID, AppID, Name, Description, Type string
	Icon                               string // the app's image_64
	InstallTeamID, InstallTeamName     string
}

// ChannelActions lists a channel's legacy workflows (Slack's channel
// actions refetcher s4p6: apps.actions.listForResource with
// resource_type channel).
func (ix *Interactive) ChannelActions(ctx context.Context, ch string) ([]ChannelAction, error) {
	var res struct {
		Actions []struct {
			ActionID        string `json:"action_id"`
			Name            string `json:"name"`
			Description     string `json:"description"`
			Type            string `json:"type"`
			InstallTeamID   string `json:"install_team_id"`
			InstallTeamName string `json:"install_team_name"`
			App             struct {
				AppID string            `json:"app_id"`
				Icons map[string]string `json:"icons"`
			} `json:"app"`
		} `json:"actions"`
	}
	err := ix.call(ctx, "apps.actions.listForResource", url.Values{"resource_type": {"channel"}, "resource_id": {ch}}, &res)
	logCall("apps.actions.listForResource", "channel="+ch, err)
	if err != nil {
		return nil, err
	}
	out := make([]ChannelAction, 0, len(res.Actions))
	for _, r := range res.Actions {
		out = append(out, ChannelAction{ID: r.ActionID, AppID: r.App.AppID, Name: r.Name, Description: r.Description, Type: r.Type,
			Icon:          firstNonEmpty(r.App.Icons["image_64"], r.App.Icons["image_72"], r.App.Icons["image_48"], r.App.Icons["image_36"]),
			InstallTeamID: r.InstallTeamID, InstallTeamName: r.InstallTeamName})
	}
	return out, nil
}

// RunChannelAction runs a legacy workflow (Slack's runChannelAction:
// apps.actions.run with type channel_action and a client_token this
// client issued, for the form it may open).
func (ix *Interactive) RunChannelAction(ctx context.Context, a ChannelAction, ch string) error {
	p := url.Values{"action_id": {a.ID}, "type": {"channel_action"}, "app_id": {a.AppID}, "channel": {ch}, "client_token": {ix.ClientToken()}}
	err := ix.call(ctx, "apps.actions.run", p, nil)
	logCall("apps.actions.run", "channel="+ch+" action="+a.ID, err)
	return err
}

// WorkflowApp is a workflow whose bot is a member of the channel (Slack's
// workflowApps store): its app, its share URL's trigger and its icon.
type WorkflowApp struct {
	AppID, WorkflowID, ShareURL, Icon string
	Trigger                           WorkflowTrigger // looked up from the share URL; zero until found
	// WorkflowTitle / WorkflowDescription come from functions.workflows.list.
	WorkflowTitle, WorkflowDescription string
}

// Title and Desc are the row's text (createShortcutFromTriggerAndWorkflow:
// the trigger's, else the workflow's).
func (w WorkflowApp) Title() string { return firstNonEmpty(w.Trigger.Title(), w.WorkflowTitle) }
func (w WorkflowApp) Desc() string  { return firstNonEmpty(w.Trigger.Desc(), w.WorkflowDescription) }

// ChannelWorkflowApps finds the workflows in a channel the way Slack's
// fetchAndUpsertFirstNWorkflowAppsForChannel (A3kQ) does: the first 100
// workflow bots among the channel's members (edge users/list, filter
// "workflows NOT deactivated", present first), their workflows
// (functions.workflows.findWorkflowForBotUsers), those workflows'
// details (functions.workflows.list, access level all) and each share
// URL's trigger (workflows.triggers.info).
func (ix *Interactive) ChannelWorkflowApps(ctx context.Context, ch string) ([]WorkflowApp, error) {
	var members struct {
		Results []struct {
			ID string `json:"id"`
		} `json:"results"`
	}
	body := map[string]any{"channels": []string{ch}, "present_first": true, "filter": "workflows NOT deactivated", "count": 100}
	if err := ix.a.C.Edge(ctx, "users/list", body, &members); err != nil {
		logCall("edge users/list", "workflows channel="+ch, err)
		return nil, err
	}
	ids := make([]string, 0, len(members.Results))
	for _, m := range members.Results {
		ids = append(ids, m.ID)
	}
	var found struct {
		WorkflowApps []struct {
			AppID      string `json:"app_id"`
			WorkflowID string `json:"workflow_id"`
			ShareURL   string `json:"share_url"`
			AppDetails struct {
				Icons map[string]string `json:"icons"`
			} `json:"app_details"`
		} `json:"workflow_apps"`
	}
	err := ix.call(ctx, "functions.workflows.findWorkflowForBotUsers", url.Values{"user_ids": {strings.Join(ids, ",")}, "channel_id": {ch}}, &found)
	logCall("functions.workflows.findWorkflowForBotUsers", "channel="+ch+" bots="+strconv.Itoa(len(ids)), err)
	if err != nil {
		return nil, err
	}
	out := make([]WorkflowApp, 0, len(found.WorkflowApps))
	var wfIDs []string
	for _, w := range found.WorkflowApps {
		ic := w.AppDetails.Icons
		out = append(out, WorkflowApp{AppID: w.AppID, WorkflowID: w.WorkflowID, ShareURL: w.ShareURL,
			Icon: firstNonEmpty(ic["image_36"], ic["image_48"], ic["image_72"])})
		if w.WorkflowID != "" {
			wfIDs = append(wfIDs, w.WorkflowID)
		}
	}
	if len(wfIDs) > 0 {
		wb, _ := json.Marshal(wfIDs)
		var wfs struct {
			Workflows []struct {
				ID          string `json:"id"`
				Title       string `json:"title"`
				Description string `json:"description"`
			} `json:"workflows"`
		}
		p := url.Values{"workflow_ids": {string(wb)}, "access_level": {"all"}, "limit": {strconv.Itoa(len(wfIDs))}}
		if err := ix.call(ctx, "functions.workflows.list", p, &wfs); err == nil { // Slack ignores a failure here
			for _, w := range wfs.Workflows {
				for i := range out {
					if out[i].WorkflowID == w.ID {
						out[i].WorkflowTitle, out[i].WorkflowDescription = w.Title, w.Description
					}
				}
			}
		} else {
			logCall("functions.workflows.list", "n="+strconv.Itoa(len(wfIDs)), err)
		}
	}
	for i := range out {
		id := WorkflowTriggerID(out[i].ShareURL)
		if id == "" {
			continue
		}
		t, ok := ix.Trigger(id)
		if !ok {
			t, _ = ix.TriggerInfo(ctx, id)
		}
		out[i].Trigger = t
	}
	return out, nil
}

// ---- adding a workflow to a channel ----

// AddWorkflowBookmark bookmarks a workflow in a channel, as the channel
// menu's "Add a workflow" does (Slack's addBookmarkForChannel, Tyab eV;
// the fetcher YQEj): bookmarks.add with type shortcut, the trigger as
// shortcut_id and its name as the title (the id without one).
func (a *App) AddWorkflowBookmark(ctx context.Context, ch string, t WorkflowTrigger) (Bookmark, error) {
	p := url.Values{"channel": {ch}, "title": {firstNonEmpty(t.Name, t.ID)}, "type": {"shortcut"}, "shortcut_id": {t.ID}}
	var res struct {
		Bookmark struct {
			ID         string `json:"id"`
			Title      string `json:"title"`
			Link       string `json:"link"`
			Type       string `json:"type"`
			Rank       string `json:"rank"`
			ParentID   string `json:"parent_id"`
			ShortcutID string `json:"shortcut_id"`
			Created    int64  `json:"date_created"`
		} `json:"bookmark"`
	}
	err := a.C.Call(ctx, "bookmarks.add", p, &res)
	logCall("bookmarks.add", "channel="+ch+" shortcut="+t.ID, err)
	if err != nil {
		return Bookmark{}, err
	}
	b := res.Bookmark
	return Bookmark{ID: b.ID, Title: b.Title, Link: b.Link, Type: b.Type, Rank: b.Rank, ParentID: b.ParentID,
		ShortcutID: b.ShortcutID, Created: b.Created}, nil
}

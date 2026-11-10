package core

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// Synthetic fixtures only (scraping policy).

func TestWorkflowTriggerID(t *testing.T) {
	if id := WorkflowTriggerID("https://slack.com/shortcuts/Ft0123ABCDEF/abcdef0123"); id != "Ft0123ABCDEF" {
		t.Errorf("id %q", id)
	}
	if WorkflowTriggerID("https://example.com/shortcuts/Ft0123ABCDEF/x") != "" || WorkflowTriggerID("https://slack.com/archives/C1") != "" {
		t.Error("non-workflow links")
	}
}

// TestTripContexts: each place a trigger runs from sends Slack's
// snake_cased workflowLinkTriggerUIContext (JQmE, C7fj, Jf9W, 2Lul).
func TestTripContexts(t *testing.T) {
	inputs := json.RawMessage(`[{"name":"env","value":"staging"}]`)
	cases := []struct {
		c    TripContext
		want string
	}{
		{MessageTripContext("C1", "1.2"), `{"channel_id":"C1","location":"message","message_ts":"1.2"}`},
		{MessageTripContext("", ""), `{"location":"markdown_link"}`},
		{ButtonTripContext("go", "b1", "C1", "1.2", false, inputs), `{"action_id":"go","block_id":"b1","channel_id":"C1","location":"button","message_ts":"1.2"}`},
		{ButtonTripContext("go", "b1", "C1", "1.2", true, inputs),
			`{"action_id":"go","block_id":"b1","channel_id":"C1","customizable_input_parameters":[{"name":"env","value":"staging"}],"location":"button","message_ts":"1.2"}`},
		{BookmarkTripContext("C1", "Bk1"), `{"bookmark_id":"Bk1","channel_id":"C1","location":"bookmark"}`},
		{ChannelDetailsTripContext("C1"), `{"channel_id":"C1","location":"channel_details"}`},
	}
	for _, c := range cases {
		b, _ := json.Marshal(c.c)
		if string(b) != c.want {
			t.Errorf("context\n got %s\nwant %s", b, c.want)
		}
	}
}

func TestTripTrigger(t *testing.T) {
	a, f := testApp(t)
	ix := a.Interactive()
	link := "https://slack.com/shortcuts/Ft0123ABCDEF/abcdef0123"
	f.reply["workflows.triggers.trip"] = `{"ok":false,"error":"pretrip_ack_required","pre_trip_info":{"scopes_acknowledgements":{"is_shown":true,` +
		`"bot_scopes":[{"name":"chat:write","description":"Send messages as the workflow","action_type":"do"}]}}}`
	_, err := ix.TripTrigger(context.Background(), link, MessageTripContext("C1", "1.2"), true)
	var te *TripError
	if !errors.As(err, &te) || te.Code != "pretrip_ack_required" || te.PreTrip == nil {
		t.Fatalf("precheck: %v", err)
	}
	if s := te.PreTrip.ScopesAcknowledgements; !s.IsShown || len(s.BotScopes) != 1 || s.BotScopes[0].ActionType != "do" {
		t.Errorf("scopes %+v", s)
	}
	if !te.PreTrip.NeedsSpeedbump("message", "") {
		t.Error("risky scopes need the speedbump")
	}
	p := f.last("workflows.triggers.trip")
	if p.Get("url") != link || p.Get("run_precheck") != "true" || p.Get("context") != `{"channel_id":"C1","location":"message","message_ts":"1.2"}` {
		t.Errorf("params: %v", p)
	}
	if tok := p.Get("client_token"); !strings.HasPrefix(tok, "web-") || !ix.ours(tok, 0) {
		t.Errorf("client_token %q not issued here (the form's view_opened would be dropped)", tok)
	}

	// the speedbump's Start: run_precheck left out, as Slack's tripTrigger() sends it
	f.reply["workflows.triggers.trip"] = `{"ok":true,"is_slow_workflow":true}`
	res, err := ix.TripTrigger(context.Background(), link, MessageTripContext("", ""), false)
	if err != nil || !res.Slow || res.AISteps {
		t.Fatalf("trip: %+v %v", res, err)
	}
	if p := f.last("workflows.triggers.trip"); p.Get("context") != `{"location":"markdown_link"}` || p.Has("run_precheck") {
		t.Errorf("params: %v", p)
	}

	f.reply["workflows.triggers.trip"] = `{"ok":false,"error":"access_denied","error_messages":["unsupported_private_channel"]}`
	_, err = ix.TripTrigger(context.Background(), link, MessageTripContext("C1", "1.2"), true)
	if !errors.As(err, &te) || te.SubType != "unsupported_private_channel" {
		t.Fatalf("access_denied: %v", err)
	}
	if got := TripErrorText(te.Code, te.SubType); got != "Unfortunately you can’t run new workflows in private channels" {
		t.Errorf("text %q", got)
	}
}

func TestTripErrorText(t *testing.T) {
	cases := map[[2]string]string{
		{"access_denied", ""}:                           "You don’t have permissions to run this workflow",
		{"access_denied", "something_new"}:              "Oops, there was an error running that workflow",
		{"trigger_not_found", ""}:                       "This workflow no longer exists.",
		{"reached_upper_spend_limit", ""}:               "Workflow currently unavailable. Contact a workspace owner for help.",
		{"invalid_url", ""}:                             "Oops, there was an error running that workflow",
		{"access_denied", "frontline_user_ai_workflow"}: "Frontline members can’t use workflows with AI steps.",
	}
	for k, want := range cases {
		if got := TripErrorText(k[0], k[1]); got != want {
			t.Errorf("%v: %q, want %q", k, got, want)
		}
	}
}

func TestNeedsSpeedbump(t *testing.T) {
	var nothing PreTripInfo
	if nothing.NeedsSpeedbump("message", "") {
		t.Error("nothing to acknowledge outside search")
	}
	if !nothing.NeedsSpeedbump("search", "") || nothing.NeedsSpeedbump("search", "workflow_details_modal") {
		t.Error("search shows the default speedbump, except from workflow details")
	}
	var auth PreTripInfo
	auth.ThirdPartyAuths.IsShown = true
	if !auth.NeedsSpeedbump("button", "") {
		t.Error("an unsatisfied connection needs the speedbump")
	}
	auth.ThirdPartyAuths.IsSatisfied = true
	if auth.NeedsSpeedbump("button", "") {
		t.Error("satisfied connections don't")
	}
	if (*PreTripInfo)(nil).NeedsSpeedbump("search", "") {
		t.Error("no pre_trip_info, no speedbump")
	}
}

func TestTriggerInfoAndSearch(t *testing.T) {
	a, f := testApp(t)
	ix := a.Interactive()
	f.reply["workflows.triggers.info"] = `{"ok":true,"trigger":{"id":"Ft1","type":"shortcut","name":"","description":"",` +
		`"shortcut_url":"https://slack.com/shortcuts/Ft1/aa","share_url":"https://slack.com/shortcuts/Ft1/bb",` +
		`"workflow":{"workflow_id":"Wf1","app_id":"A1","title":"Request PTO","description":"Ask for time off",` +
		`"app":{"id":"A1","name":"Request PTO","icons":{"image_36":"https://img.example/36.png","image_72":"https://img.example/72.png"}}},` +
		`"workflow_details":{"is_published":false,"bot_user_id":"UWF1"}}}`
	tr, err := ix.TriggerInfo(context.Background(), "Ft1")
	if err != nil {
		t.Fatal(err)
	}
	if tr.Title() != "Request PTO" || tr.Desc() != "Ask for time off" || !tr.IsShortcut() || tr.Published || tr.BotUserID != "UWF1" ||
		tr.Icon != "https://img.example/36.png" || tr.WorkflowID != "Wf1" || tr.AppID != "A1" {
		t.Errorf("trigger %+v", tr)
	}
	if p := f.last("workflows.triggers.info"); p.Get("trigger_id") != "Ft1" || p.Get("include_workflow_details") != "true" {
		t.Errorf("params %v", p)
	}
	if got, ok := ix.Trigger("Ft1"); !ok || got.Title() != "Request PTO" {
		t.Error("not kept in memory")
	}

	f.reply["search.autocomplete.triggers"] = `{"ok":true,"triggers":[{"id":"Ft2","type":"shortcut","name":"Standup","shortcut_url":"https://slack.com/shortcuts/Ft2/cc","workflow":{"workflow_id":"Wf2"}},` +
		`{"id":"Ft3","type":"scheduled","name":"Weekly report","workflow":{"workflow_id":"Wf3"}}]}`
	ts, err := ix.SearchTriggers(context.Background(), "", "C1", true)
	if err != nil || len(ts) != 2 || ts[0].ID != "Ft2" || ts[1].IsShortcut() {
		t.Fatalf("search %+v %v", ts, err)
	}
	p := f.last("search.autocomplete.triggers")
	if !p.Has("query") || p.Get("current_channel_id") != "C1" || p.Get("strict_exposure_rules") != "true" ||
		p.Get("trigger_types") != `["shortcut","event","webhook","scheduled","service"]` {
		t.Errorf("params %v", p)
	}
	if _, err := ix.SearchTriggers(context.Background(), "stand", "", false); err != nil {
		t.Fatal(err)
	}
	if p := f.last("search.autocomplete.triggers"); p.Get("trigger_types") != `["shortcut"]` || p.Has("current_channel_id") {
		t.Errorf("params %v", p)
	}
}

// TestWorkflowStepActions: a workflow step's message carries
// function_execution_id, and its block actions send it (o9iI, Em/L).
func TestWorkflowStepActions(t *testing.T) {
	a, f := testApp(t)
	f.reply["blocks.actions"] = `{"ok":true}`
	msg := slack.Message{TS: "1700000000.000200", User: "UWF", BotID: "BWF", Text: "Approve?", FunctionExecutionID: "Fx0STEP1",
		BotProfile: &slack.BotProfile{ID: "BWF", AppID: "AWF", TeamID: "T1"}}
	m := a.message("C1", &msg)
	if m.FunctionExecutionID != "Fx0STEP1" {
		t.Fatalf("message lost function_execution_id: %+v", m)
	}
	ix := a.Interactive()
	b := ix.MessageContainer(&m, 0)
	b.Action = json.RawMessage(`{"type":"button","block_id":"b1","action_id":"approve","value":"yes"}`)
	if _, err := ix.BlockActionSend(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	if p := f.last("blocks.actions"); p.Get("function_execution_id") != "Fx0STEP1" {
		t.Errorf("blocks.actions without function_execution_id: %v", p)
	}
}

// TestChannelWorkflows: channel details > Automations reads Slack's two
// stores against fakeslack's #workflows-gallery: the workflow bots among
// the members (edge users/list with Slack's filter), their workflows and
// each share URL's trigger, and the legacy channel actions.
func TestChannelWorkflows(t *testing.T) {
	sz := fakeslack.Small
	sz.Workflows = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := New(slack.New(fs.Creds()), store.New(""))
	ix := a.Interactive()
	ctx := context.Background()
	apps, err := ix.ChannelWorkflowApps(ctx, fakeslack.WorkflowsChannelID)
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 2 || apps[0].Title() != "Run deploy" || apps[1].Title() != "Request time off" ||
		apps[1].Desc() != "Ask your lead for days off" || apps[1].Trigger.ShortcutURL != fakeslack.WorkflowLink(fakeslack.TriggerTimeOff) ||
		apps[0].Icon == "" || apps[0].WorkflowTitle != "Deploy" {
		t.Errorf("workflow apps %+v", apps)
	}
	found := false
	for _, f := range fs.UserListFilters() {
		found = found || f == "workflows NOT deactivated"
	}
	if !found {
		t.Errorf("no users/list with Slack's workflow filter: %v", fs.UserListFilters())
	}
	acts, err := ix.ChannelActions(ctx, fakeslack.WorkflowsChannelID)
	if err != nil || len(acts) != 1 || acts[0].Name != fakeslack.LegacyActionName || acts[0].AppID != "A0FAKELEG1" || acts[0].Icon == "" {
		t.Fatalf("channel actions %+v %v", acts, err)
	}
	if err := ix.RunChannelAction(ctx, acts[0], fakeslack.WorkflowsChannelID); err != nil {
		t.Fatal(err)
	}
	runs := fs.ActionRuns()
	if len(runs) != 1 || runs[0].Get("action_id") != fakeslack.LegacyActionID || runs[0].Get("type") != "channel_action" ||
		runs[0].Get("channel") != fakeslack.WorkflowsChannelID || !strings.HasPrefix(runs[0].Get("client_token"), "web-") {
		t.Errorf("apps.actions.run %v", runs)
	}
	// the picker's search reaches the workflow fixtures (not search.go's
	// empty answer for the search popover)
	if ts, err := ix.SearchTriggers(ctx, "", fakeslack.WorkflowsChannelID, false); err != nil || len(ts) != 3 {
		t.Errorf("search: %d triggers, %v", len(ts), err)
	}
	// another channel: nothing
	if apps, _ := ix.ChannelWorkflowApps(ctx, "C0NOTHERE1"); len(apps) != 0 {
		t.Errorf("apps elsewhere %+v", apps)
	}
}

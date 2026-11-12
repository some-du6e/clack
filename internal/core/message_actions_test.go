package core

import (
	"context"
	"testing"
)

// FlagMessageDialog's submit is moderation.flags.create: a message flag
// (type message) or a channel detail's (type channel_details with
// channel_details_flagged, JSON-encoded).
func TestFlagContentCall(t *testing.T) {
	a, f := testApp(t)
	f.reply["moderation.flags.create"] = `{"ok":true}`
	if err := a.FlagContent(context.Background(), "C1", "1700000000.000100", "", "spam in a synthetic test"); err != nil {
		t.Fatal(err)
	}
	p := f.last("moderation.flags.create")
	if p.Get("channel_id") != "C1" || p.Get("item_id") != "1700000000.000100" || p.Get("type") != "message" ||
		p.Get("reason") != "spam in a synthetic test" || p.Has("channel_details_flagged") {
		t.Errorf("message flag %v", p)
	}
	if err := a.FlagContent(context.Background(), "C1", "1700000000.000200", "channel_topic", "a synthetic reason"); err != nil {
		t.Fatal(err)
	}
	p = f.last("moderation.flags.create")
	if p.Get("type") != "channel_details" || p.Get("channel_details_flagged") != `["channel_topic"]` {
		t.Errorf("channel details flag %v", p)
	}
	f.reply["moderation.flags.create"] = `{"ok":false,"error":"invalid_arguments"}`
	if err := a.FlagContent(context.Background(), "C1", "1700000000.000300", "", "x"); err == nil {
		t.Error("an error answer isn't an error")
	}
}

// The flag row's gate: the org's locked content_review_enabled (0vTf
// isContentReviewEnabled: truthy), on an Enterprise team or workspace.
func TestContentReviewEnabled(t *testing.T) {
	a, f := testApp(t)
	if a.ContentReviewEnabled() {
		t.Error("on before enterprise.prefs.get")
	}
	for _, c := range []struct {
		locked string
		want   bool
	}{{`{"content_review_enabled":true}`, true}, {`{"content_review_enabled":false}`, false}, {`{}`, false},
		{`{"content_review_enabled":null}`, false}, {`{"content_review_enabled":0}`, false}} {
		f.reply["enterprise.prefs.get"] = `{"ok":true,"org_prefs":{},"locked_prefs":` + c.locked + `}`
		if err := a.LoadEnterprisePrefs(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got := a.ContentReviewEnabled(); got != c.want {
			t.Errorf("%s: %v", c.locked, got)
		}
	}
	f.reply["enterprise.prefs.get"] = `{"ok":true,"locked_prefs":{"flag_message_custom_link":"https://example.com/policy"}}`
	if err := a.LoadEnterprisePrefs(context.Background()); err != nil {
		t.Fatal(err)
	}
	if a.FlagCustomLink() != "https://example.com/policy" {
		t.Errorf("custom link %q", a.FlagCustomLink())
	}
	if a.EnterpriseTeam() {
		t.Error("a workspace with no enterprise is no Enterprise team")
	}
	a.C.Creds.EnterpriseID = "E1"
	if !a.EnterpriseTeam() || a.EnterpriseID() != "E1" {
		t.Error("a Grid workspace is")
	}
}

// The trigger previews store (EXgk): workflows.triggers.preview with the
// ids comma-joined; previews and errors kept for TriggerPreview.
func TestTriggerPreviewsStore(t *testing.T) {
	a, f := testApp(t)
	ix := a.Interactive()
	if _, ok := ix.TriggerPreview("Ft1"); ok {
		t.Fatal("a preview before the fetch")
	}
	f.reply["workflows.triggers.preview"] = `{"ok":true,"triggers":[{"id":"Ft1","type":"shortcut","name":"Run deploy",` +
		`"workflow":{"id":"Fn1","workflow_id":"Wf1","title":"Deploy"}},{"id":"Ft9","error":"access_denied"}]}`
	ts, err := ix.TriggerPreviews(context.Background(), "Ft1", "Ft9")
	if err != nil || len(ts) != 2 {
		t.Fatalf("previews %+v %v", ts, err)
	}
	if p := f.last("workflows.triggers.preview"); p.Get("trigger_ids") != "Ft1,Ft9" {
		t.Errorf("params %v", p)
	}
	if p, ok := ix.TriggerPreview("Ft1"); !ok || p.WorkflowID != "Wf1" || p.Error != "" {
		t.Errorf("Ft1 %+v", p)
	}
	if p, ok := ix.TriggerPreview("Ft9"); !ok || p.Error != "access_denied" {
		t.Errorf("Ft9 %+v", p)
	}
	if _, ok := ix.Trigger("Ft9"); ok {
		t.Error("an access_denied preview joined the triggers")
	}
}

// The workflow details view's data: workflow_details' steps and
// collaborators from workflows.triggers.info, the workflow from
// functions.workflows.get (switch cases with their expression and
// colour, loops' steps).
func TestWorkflowDetailsData(t *testing.T) {
	a, f := testApp(t)
	ix := a.Interactive()
	steps := `[{"id":"s1","function":{"callback_id":"open_form","title":"Collect","type":"builtin"}},` +
		`{"id":"s2","type":"switch","function":{"callback_id":"switch_step","title":"Check"},"steps":[` +
		`{"id":"s2a","expression":{"op":"eq"},"display_config":{"color":"jade"},"steps":[{"id":"s2a1","function":{"callback_id":"send_message","title":"Announce"}}]},` +
		`{"id":"s2b","steps":[{"id":"s2b1","function":{"callback_id":"send_dm","title":"Ask"}}]}]},` +
		`{"id":"s3","type":"loop","steps":[{"id":"s3a","function":{"callback_id":"add_pin","title":"Pin","type":"app","app_id":"A9"}}]}]`
	f.reply["workflows.triggers.info"] = `{"ok":true,"trigger":{"id":"Ft1","type":"shortcut","name":"Run deploy","shortcut_url":"https://slack.com/shortcuts/Ft1/aa",` +
		`"workflow":{"workflow_id":"Wf1","app":{"id":"A1","is_workflow_app":true}},` +
		`"workflow_details":{"is_published":true,"bot_user_id":"UWF1","collaborators":["U1","U2"],"steps":` + steps + `}}}`
	tr, err := ix.TriggerInfo(context.Background(), "Ft1")
	if err != nil {
		t.Fatal(err)
	}
	if !tr.IsWorkflowApp || len(tr.Collaborators) != 2 || len(tr.Steps) != 3 {
		t.Fatalf("trigger %+v", tr)
	}
	sw := tr.Steps[1]
	if sw.Type != "switch" || len(sw.Steps) != 2 || !sw.Steps[0].Expression || sw.Steps[0].Color != "jade" || sw.Steps[1].Expression ||
		sw.Steps[0].Steps[0].CallbackID != "send_message" || sw.Steps[0].Steps[0].Title != "Announce" {
		t.Errorf("switch %+v", sw)
	}
	if lp := tr.Steps[2]; lp.Type != "loop" || len(lp.Steps) != 1 || lp.Steps[0].AppID != "A9" || lp.Steps[0].FuncType != "app" {
		t.Errorf("loop %+v", lp)
	}
	f.reply["functions.workflows.get"] = `{"ok":true,"workflow":{"id":"Wf1","title":"Deploy","description":"Ship it","is_published":false,` +
		`"bot_user_id":"UWF1","collaborators":["U1"],"icons":{"image_96":"https://img.example/96.png"},"steps":` + steps + `}}`
	w, err := ix.Workflow(context.Background(), "Wf1")
	if err != nil {
		t.Fatal(err)
	}
	if p := f.last("functions.workflows.get"); p.Get("workflow_id") != "Wf1" {
		t.Errorf("params %v", p)
	}
	if w.Title != "Deploy" || w.Description != "Ship it" || w.Published || len(w.Collaborators) != 1 || len(w.Steps) != 3 || w.Icon != "https://img.example/96.png" {
		t.Errorf("workflow %+v", w)
	}
	if c := FlexpaneTripContext(""); c.Location() != "flexpane" || c["channel_id"] != nil {
		t.Errorf("trip context %v", c)
	}
}

// The share dialog's Save Draft before a pick: a draft with destinations
// [] (no channel needed), and still none without NoDestination.
func TestSaveDraftNoDestination(t *testing.T) {
	a, f := testApp(t)
	f.reply["drafts.create"] = `{"ok":true,"draft":{"id":"Dr1","destinations":[]}}`
	if _, err := a.SaveDraft(context.Background(), DraftOpts{Text: "x"}); err == nil {
		t.Error("a draft without a destination or NoDestination")
	}
	d, err := a.SaveDraft(context.Background(), DraftOpts{NoDestination: true, Text: "synthetic forward"})
	if err != nil {
		t.Fatal(err)
	}
	if p := f.last("drafts.create"); p.Get("destinations") != "[]" {
		t.Errorf("destinations %q", p.Get("destinations"))
	}
	if d.ID != "Dr1" || d.Channel != "" {
		t.Errorf("draft %+v", d)
	}
}

package core

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"
)

// Synthetic fixtures only: the posting-permissions derivations follow
// PostingPermissionsSubtitle and PostingPreferences (oWlM, build
// d3de3a487b1c) case by case.

func pv(v string) ChannelPrefValue { return ChannelPrefValue{OK: true, Raw: json.RawMessage(v)} }

func TestPostingSubtitle(t *testing.T) {
	member := PostingViewer{SelfID: "USELF", Enterprise: true}
	admin := PostingViewer{SelfID: "USELF", Enterprise: true, EnterpriseAdmin: true}
	ws := PostingViewer{SelfID: "USELF", WorkspaceAdmin: true}
	on := pv(`{"enabled":true}`)
	off := pv(`{"enabled":false}`)
	for _, c := range []struct {
		name               string
		p                  PostingPrefs
		v                  PostingViewer
		who                string
		thread, here, okay bool
	}{
		{"no type: everyone, replies on", PostingPrefs{WhoCanPost: pv(`{}`), AtHere: pv(`{}`), AtChannel: pv(`{}`)}, member, "ra", true, true, true},
		{"failed read: nothing", PostingPrefs{}, member, "", false, false, false},
		{"null value: nothing", PostingPrefs{WhoCanPost: pv(`null`)}, member, "", false, false, false},
		{"regular", PostingPrefs{WhoCanPost: pv(`{"type":["regular"]}`), CanThread: pv(`{"type":["ra"]}`), AtHere: off, AtChannel: off}, member, "regular", true, false, true},
		{"admin for a plain member reads multi", PostingPrefs{WhoCanPost: pv(`{"type":["admin"],"user":["USELF"]}`), AtHere: on, AtChannel: off}, member, "multi", false, true, true},
		{"admin with others", PostingPrefs{WhoCanPost: pv(`{"type":["admin"],"user":["U1"]}`), AtHere: on, AtChannel: on}, member, "admin_multi", false, true, true},
		{"owner with others", PostingPrefs{WhoCanPost: pv(`{"type":["owner"],"user":["U1"]}`), AtHere: on, AtChannel: on}, member, "owner_multi", false, true, true},
		{"org admin seen by an org admin", PostingPrefs{WhoCanPost: pv(`{"type":["org_admin"]}`), AtHere: on, AtChannel: on}, admin, "org_admin", false, true, true},
		{"owner on Grid reads org_admin", PostingPrefs{WhoCanPost: pv(`{"type":["owner"]}`), AtHere: on, AtChannel: on}, admin, "org_admin", false, true, true},
		{"org_admin off Grid reads admin", PostingPrefs{WhoCanPost: pv(`{"type":["org_admin"]}`), AtHere: on, AtChannel: on}, ws, "admin", false, true, true},
		{"can_thread all", PostingPrefs{WhoCanPost: pv(`{"type":["regular"]}`), CanThread: pv(`{"type":["all"]}`), AtHere: on, AtChannel: on}, member, "regular", true, true, true},
		{"can_thread admin", PostingPrefs{WhoCanPost: pv(`{}`), CanThread: pv(`{"type":["admin"]}`), AtHere: on, AtChannel: on}, member, "ra", false, true, true},
		{"mention read failed: on", PostingPrefs{WhoCanPost: pv(`{"type":["regular"]}`), AtHere: off}, member, "regular", false, true, true},
		{"enabled as a string isn't true", PostingPrefs{WhoCanPost: pv(`{"type":["regular"]}`), AtHere: pv(`{"enabled":"true"}`), AtChannel: off}, member, "regular", false, false, true},
	} {
		who, thread, here, ok := PostingSubtitle(c.p, c.v)
		if who != c.who || thread != c.thread || here != c.here || ok != c.okay {
			t.Errorf("%s: got %q thread=%v here=%v ok=%v, want %q %v %v %v", c.name, who, thread, here, ok, c.who, c.thread, c.here, c.okay)
		}
	}
	org := member
	org.OrgDefault = true
	if who, _, _, _ := PostingSubtitle(PostingPrefs{WhoCanPost: pv(`{}`)}, org); who != "org_admin" {
		t.Errorf("an org default channel without a type reads %q, want org_admin", who)
	}
}

func TestPostingFormState(t *testing.T) {
	member := PostingViewer{SelfID: "USELF", Enterprise: true}
	f := PostingFormState(PostingPrefs{WhoCanPost: pv(`{"type":["admin"],"user":["USELF","U1"]}`), CanThread: pv(`{"type":["ra"]}`),
		AtHere: pv(`{}`), AtChannel: pv(`{"enabled":false}`)}, member)
	if !f.OK || f.Who != "multi" || !slices.Equal(f.Users, []string{"USELF", "U1"}) || !f.CanThread || !f.AtHere {
		t.Errorf("admin plus people: %+v", f)
	}
	if f := PostingFormState(PostingPrefs{WhoCanPost: pv(`{"type":["admin"]}`)}, member); f.Who != "multi" || len(f.Users) != 0 {
		t.Errorf("admins only for a member who can't manage: %+v", f)
	}
	mgr := member
	mgr.ManagePerms = true
	if f := PostingFormState(PostingPrefs{WhoCanPost: pv(`{"type":["admin"]}`)}, mgr); f.Who != "admin" {
		t.Errorf("admins only for a manager: %+v", f)
	}
	if f := PostingFormState(PostingPrefs{WhoCanPost: pv(`{}`)}, member); f.Who != "ra" || !f.CanThread || !f.AtHere {
		t.Errorf("no type: %+v", f)
	}
	if f := PostingFormState(PostingPrefs{}, member); f.OK {
		t.Errorf("a failed read loaded the form: %+v", f)
	}
}

func TestPostingSavePrefs(t *testing.T) {
	member := PostingViewer{SelfID: "USELF", Enterprise: true}
	wsAdmin := PostingViewer{SelfID: "USELF", Enterprise: true, WorkspaceAdmin: true}
	orgShared := PostingViewer{SelfID: "USELF", Enterprise: true, OrgShared: true, EnterpriseAdmin: true}
	for _, c := range []struct {
		who          string
		picked       []string
		thread, here bool
		v            PostingViewer
		want         string
	}{
		{"ra", nil, true, true, member, `{"who_can_post":"type:ra","can_thread":"type:ra","enable_at_here":"true","enable_at_channel":"true"}`},
		{"regular", nil, false, false, member, `{"who_can_post":"type:regular","can_thread":"type:regular","enable_at_here":"false","enable_at_channel":"false"}`},
		{"admin", nil, false, true, wsAdmin, `{"who_can_post":"type:admin","can_thread":"type:admin","enable_at_here":"true","enable_at_channel":"true"}`},
		{"multi", []string{"U1", "U2"}, true, false, member, `{"who_can_post":"type:admin,user:U1,user:U2,user:USELF","can_thread":"type:ra","enable_at_here":"false","enable_at_channel":"false"}`},
		{"multi", []string{"USELF", "U1"}, false, false, member, `{"who_can_post":"type:admin,user:USELF,user:U1","can_thread":"type:admin,user:USELF,user:U1","enable_at_here":"false","enable_at_channel":"false"}`},
		{"org_admin", nil, false, true, orgShared, `{"who_can_post":"type:org_admin","can_thread":"type:org_admin","enable_at_here":"true","enable_at_channel":"true"}`},
	} {
		if got := PostingSavePrefs(c.who, c.picked, c.thread, c.here, c.v); got != c.want {
			t.Errorf("%s %v:\n got %s\nwant %s", c.who, c.picked, got, c.want)
		}
	}
}

func TestPostingPermsVisibility(t *testing.T) {
	base := PostingVisIn{Channel: true, Member: true}
	for _, c := range []struct {
		name     string
		mod      func(*PostingVisIn)
		show, ds bool
	}{
		{"a plain member", func(*PostingVisIn) {}, false, false},
		{"#general with MANAGE_CHANNEL_PERMISSIONS", func(i *PostingVisIn) { i.General, i.ManagePerms = true, true }, true, false},
		{"the feature and the posting grant", func(i *PostingVisIn) { i.FeatureAccess, i.ManagePosting, i.CanPost = true, true, true }, true, false},
		{"the posting grant but can't post", func(i *PostingVisIn) { i.FeatureAccess, i.ManagePosting = true, true }, false, false},
		{"org default needs an org admin", func(i *PostingVisIn) {
			i.FeatureAccess, i.ManagePosting, i.CanPost, i.OrgDefault = true, true, true, true
		}, false, false},
		{"hosted by another org", func(i *PostingVisIn) { i.General, i.ManagePerms, i.ExtSharedHosted = true, true, true }, false, false},
		{"a DM", func(i *PostingVisIn) { i.Channel, i.General, i.ManagePerms = false, true, true }, false, false},
		{"not a member, not an admin", func(i *PostingVisIn) { i.General, i.ManagePerms, i.Member = true, true, false }, true, true},
		{"Standard-plan upsell", func(i *PostingVisIn) { i.StandardPlan, i.UpsellTreatment = true, true }, true, true},
		{"upsell off during a trial", func(i *PostingVisIn) { i.StandardPlan, i.UpsellTreatment, i.TrialActive = true, true, true }, false, false},
	} {
		in := base
		c.mod(&in)
		got := PostingPermsVisibility(in)
		if got.Show != c.show || got.Show && got.Disabled != c.ds {
			t.Errorf("%s: %+v, want show=%v disabled=%v", c.name, got, c.show, c.ds)
		}
	}
}

// The four reads and the write go out as Slack sends them.
func TestChannelPostingPrefsCalls(t *testing.T) {
	a, f := testApp(t)
	f.reply["channels.prefs.get"] = `{"ok":true,"pref_value":{"type":["regular"]}}`
	p := a.ChannelPostingPrefs(context.Background(), "C1")
	if !p.WhoCanPost.OK || !p.CanThread.OK || !p.AtHere.OK || !p.AtChannel.OK {
		t.Fatalf("reads %+v", p)
	}
	f.mu.Lock()
	var names []string
	for i, m := range f.calls {
		if m == "channels.prefs.get" {
			if f.forms[i].Get("channel_id") != "C1" {
				t.Errorf("channel_id %v", f.forms[i])
			}
			names = append(names, f.forms[i].Get("pref_name"))
		}
	}
	f.mu.Unlock()
	slices.Sort(names)
	if want := []string{"can_thread", "enable_at_channel", "enable_at_here", "who_can_post"}; !slices.Equal(names, want) {
		t.Errorf("pref names %v, want %v", names, want)
	}
	f.reply["channels.prefs.set"] = `{"ok":true,"prefs":{}}`
	prefs := PostingSavePrefs("regular", nil, true, true, PostingViewer{SelfID: "USELF"})
	if err := a.SetChannelPrefs(context.Background(), "C1", prefs); err != nil {
		t.Fatal(err)
	}
	if s := f.last("channels.prefs.set"); s.Get("channel_id") != "C1" || s.Get("prefs") != prefs {
		t.Errorf("channels.prefs.set %v", s)
	}
}

func TestFeatureAccess(t *testing.T) {
	a, f := testApp(t)
	f.reply["features.access.policies.list"] = `{"ok":true,"feature_policies":{"manage_channel_posting__access":{"is_enabled":true},"frontline__access":{"is_enabled":false}}}`
	evs, stop := a.Subscribe()
	defer stop()
	if a.FeatureAccess(FeatureManageChannelPosting) {
		t.Error("on before the read answered")
	}
	next(t, evs, "features")
	if !a.FeatureAccess(FeatureManageChannelPosting) || a.FeatureAccess(FeatureFrontline) {
		t.Error("features.access.policies.list not applied")
	}
}

func TestAppsResultRows(t *testing.T) {
	cats := []AppCategory{
		{ID: "Ai01", DisplayName: "Installed", Apps: []AppIndexEntry{{AppID: "A1", Name: "Polly"}, {AppID: "A2", Name: "Giphy"}}, NextCursor: "c2"},
		{ID: "Ai02", DisplayName: "Empty"},
		{ID: "Ai03", DisplayName: "Recommended", Apps: []AppIndexEntry{{AppID: "A3", Name: "Jira"}}},
	}
	var got []string
	for _, r := range AppsResultRows(cats) {
		switch r.Kind {
		case "category":
			got = append(got, "#"+r.Category.DisplayName)
		case "app":
			got = append(got, r.App.Name)
		default:
			got = append(got, r.Kind)
		}
	}
	if want := []string{"#Installed", "Polly", "Giphy", "#Recommended", "Jira", "loading"}; !slices.Equal(got, want) {
		t.Errorf("rows %v, want %v", got, want)
	}
	if e := (AppIndexEntry{Icons: map[string]string{"image_48": "u48"}}); e.Icon(72) != "u48" {
		t.Errorf("icon fallback %q", e.Icon(72))
	}
}

func TestAppsAddable(t *testing.T) {
	a, f := testApp(t)
	rows := []AppsBrowserRow{{Kind: "app", App: AppIndexEntry{AppID: "A1", BotUserID: "UB", Installed: true}},
		{Kind: "app", App: AppIndexEntry{AppID: "A2", BotUserID: "UC"}}}
	f.reply["membership"] = `{"ok":true,"members":[],"non_members":["UB"]}`
	got := a.AppsAddable(context.Background(), "C1", rows)
	if !got["UB"] || got["UC"] {
		t.Errorf("addable %v", got)
	}
}

func TestHistoryPreviewRows(t *testing.T) {
	loc := time.UTC
	day1 := time.Date(2026, 11, 10, 9, 0, 0, 0, loc).Unix()
	day2 := time.Date(2026, 11, 11, 9, 0, 0, 0, loc).Unix()
	ts := func(sec int64, n int) string { return itoa(sec+int64(n)) + ".000100" }
	msgs := []Message{
		{TS: ts(day1, 0), Text: "a"},
		{TS: ts(day1, 1), Text: "reply", ThreadTS: ts(day1, 0)},
		{TS: ts(day1, 2), Text: "broadcast", ThreadTS: ts(day1, 0), Broadcast: true},
		{TS: ts(day2, 0), Text: "b"},
		{TS: ts(day2, 1), Text: "orphan broadcast", ThreadTS: ts(day1-100, 0), Broadcast: true},
	}
	var got []string
	for _, r := range HistoryPreviewRows(msgs, "", loc) {
		if r.Kind == "message" {
			got = append(got, r.Msg.Text)
		} else {
			got = append(got, r.Kind)
		}
	}
	if want := []string{"day_divider", "a", "broadcast", "day_divider", "b"}; !slices.Equal(got, want) {
		t.Errorf("rows %v, want %v", got, want)
	}
	if rs := HistoryPreviewRows(msgs, itoa(day2), loc); len(rs) != 2 || rs[1].Msg.Text != "b" {
		t.Errorf("from day 2: %+v", rs)
	}
	var many []Message
	for i := 0; i < 250; i++ {
		many = append(many, Message{TS: itoa(day1+int64(i)) + ".000000"})
	}
	rs := HistoryPreviewRows(many, "", loc)
	if len(rs) != HistoryPreviewLimit+1 || rs[len(rs)-1].Kind != "more_messages" {
		t.Errorf("%d rows, last %q; want the cut at %d and more_messages", len(rs), rs[len(rs)-1].Kind, HistoryPreviewLimit)
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

// HistoryOldest and HistoryPreview send fetchPreviewHistory's params.
func TestHistoryPreviewCalls(t *testing.T) {
	a, f := testApp(t)
	f.reply["conversations.history"] = `{"ok":true,"messages":[{"type":"message","ts":"1762790400.000200","user":"U1","text":"two"},` +
		`{"type":"message","ts":"1762790400.000100","user":"U1","text":"one"},{"type":"message","ts":"1762790400.000300","hidden":true}]}`
	old, err := a.HistoryOldest(context.Background(), "D1")
	if err != nil || old != "1762790400.000100" {
		t.Errorf("oldest %q %v", old, err)
	}
	if p := f.last("conversations.history"); p.Get("oldest") != HistoryFromBeginning || p.Get("limit") != "1" {
		t.Errorf("oldest params %v", p)
	}
	ms, err := a.HistoryPreview(context.Background(), "D1", "1762790400", HistoryPreviewLimit)
	if err != nil || len(ms) != 2 || ms[0].Text != "one" || ms[1].Text != "two" {
		t.Errorf("preview %+v %v", ms, err)
	}
	if p := f.last("conversations.history"); p.Get("oldest") != "1762790400" || p.Get("limit") != "200" || p.Get("inclusive") != "true" {
		t.Errorf("preview params %v", p)
	}
}

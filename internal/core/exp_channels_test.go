package core

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// Synthetic fixtures only: the experiments the channel header and details
// follow (chperms.go, mpdm_name.go, enterprise_prefs.go), each in every
// group Slack's code tells apart.

// waitGrants waits for permissions/info's answer for ch to be cached.
func waitGrants(t *testing.T, a *App, ch string) {
	t.Helper()
	dl := time.Now().Add(5 * time.Second)
	for {
		pc := a.chPerms()
		pc.mu.Lock()
		_, ok := pc.m[ch]
		pc.mu.Unlock()
		if ok {
			return
		}
		if time.Now().After(dl) {
			t.Fatalf("no permissions/info answer for %s", ch)
		}
		a.ChannelGrant(ch, PermSetChannelTopic)
		time.Sleep(5 * time.Millisecond)
	}
}

// forgetGrants drops the cached grants (a new permissions/info answer).
func forgetGrants(a *App) {
	pc := a.chPerms()
	pc.mu.Lock()
	pc.m = map[string]chPermEntry{}
	pc.mu.Unlock()
}

// canUserSetTopicForChannelWithReasonById / canUserSetPurpose: without
// the experiment a member who can post edits topic and description;
// under channel_topic_purpose_setting_restrict_to_channel_managers only
// the SET_CHANNEL_TOPIC / SET_CHANNEL_PURPOSE grants do, 1:1 DMs aside.
func TestTopicPurposeRestrictedToGrants(t *testing.T) {
	a, f := testApp(t)
	a.S.PutConv(slack.Conversation{ID: "C2", Name: "random", IsChannel: true, IsMember: true})
	a.S.PutConv(slack.Conversation{ID: "C3", Name: "elsewhere", IsChannel: true})
	a.S.PutConv(slack.Conversation{ID: "C4", Name: "old", IsChannel: true, IsMember: true, IsArchived: true})
	a.S.PutConv(slack.Conversation{ID: "G1", Name: "mpdm-me--alice--bob-1", IsMPIM: true, IsMember: true, Members: []string{"USELF", "U1", "U2"}})
	a.S.PutConv(slack.Conversation{ID: "G2", Name: "partners", IsMPIM: true, IsMember: true, IsExtShared: true})
	f.reply["info"] = `{"ok":true,"allowed":["CAN_AT_CHANNEL"]}`
	evs, stop := a.Subscribe()
	defer stop()
	for _, ch := range []string{"C2", "G1", "D1"} {
		waitGrants(t, a, ch)
	}
	next(t, evs, "permissions")

	// unassigned (Slack's null): the member rule
	if !a.CanSetTopic("C2") || !a.CanSetPurpose("C2") || !a.CanSetTopic("G1") || !a.CanSetTopic("D1") {
		t.Error("a member can't set topic or description without the experiment")
	}
	if a.CanSetTopic("C3") || a.CanSetPurpose("C3") {
		t.Error("a non-member can set the topic")
	}
	if a.CanSetTopic("C4") || a.CanSetPurpose("C4") || a.CanSetTopic("G2") || a.CanSetPurpose("G1") {
		t.Error("archived channel, Slack Connect group DM or a group DM's purpose is editable")
	}
	for _, group := range []string{"off", "control"} {
		a.SetExperimentGroup(ExpRestrictTopicPurpose, group)
		if !a.CanSetTopic("C2") {
			t.Errorf("%s: the member rule should hold", group)
		}
	}

	a.SetExperimentGroup(ExpRestrictTopicPurpose, "on")
	if a.CanSetTopic("C2") || a.CanSetPurpose("C2") || a.CanSetTopic("G1") {
		t.Error("on: editable without the grants")
	}
	if !a.CanSetTopic("D1") {
		t.Error("on: a 1:1 DM's topic needs no grant")
	}

	forgetGrants(a)
	f.reply["info"] = `{"ok":true,"allowed":["SET_CHANNEL_TOPIC","SET_CHANNEL_PURPOSE"]}`
	waitGrants(t, a, "C2")
	waitGrants(t, a, "C3")
	if !a.CanSetTopic("C2") || !a.CanSetPurpose("C2") || !a.CanSetTopic("C3") {
		t.Error("on: the grants don't allow it")
	}
	if a.CanSetTopic("C4") {
		t.Error("an archived channel's topic is editable with the grant")
	}

	// a guest gets only what the grants give
	a.ClearExperimentGroup(ExpRestrictTopicPurpose)
	forgetGrants(a)
	f.reply["info"] = `{"ok":true,"allowed":[]}`
	waitGrants(t, a, "C2")
	a.S.Self.IsRestricted = true
	if u, ok := a.S.User("USELF"); ok {
		u.IsRestricted = true
	}
	if a.CanSetTopic("C2") || a.CanSetPurpose("C2") {
		t.Error("a guest edits the topic without a grant")
	}
}

// The grants are one edge permissions/info call per conversation:
// {user_id, permissions, channel_id}, no channel_id for a 1:1 DM.
func TestChannelGrantRequest(t *testing.T) {
	a, f := testApp(t)
	f.reply["info"] = `{"ok":true,"allowed":["SET_CHANNEL_TOPIC"]}`
	got, err := a.fetchChannelGrants(context.Background(), "C1")
	if err != nil || !got[PermSetChannelTopic] || got[PermSetChannelPurpose] {
		t.Fatalf("fetchChannelGrants = %v, %v", got, err)
	}
	if _, err := a.fetchChannelGrants(context.Background(), "D1"); err != nil {
		t.Fatal(err)
	}
}

// mpdm_naming (tpcx kH) offers naming a group DM, sc_mpdm_naming (QW) a
// Slack Connect one; a frontline worker can't (Fpkn q).
func TestCanNameMpdmFollowsExperiments(t *testing.T) {
	a, _ := testApp(t)
	a.S.PutConv(slack.Conversation{ID: "G1", Name: "mpdm-me--alice--bob-1", IsMPIM: true})
	a.S.PutConv(slack.Conversation{ID: "G2", Name: "Partners", IsMPIM: true, IsExtShared: true,
		Properties: json.RawMessage(`{"has_custom_mpdm_name":true}`)})
	cases := []struct {
		naming, sc string
		g1, g2     bool
	}{
		{"on", "on", true, true},
		{"on", "", true, false},
		{"", "on", false, false},
		{"off", "off", false, false},
	}
	for _, c := range cases {
		a.SetExperimentGroups(map[string]*string{"mpdm_naming": &c.naming, "sc_mpdm_naming": &c.sc})
		if a.CanNameMpdm("G1") != c.g1 || a.CanNameMpdm("G2") != c.g2 {
			t.Errorf("mpdm_naming %q sc %q: G1 %v G2 %v", c.naming, c.sc, a.CanNameMpdm("G1"), a.CanNameMpdm("G2"))
		}
	}
	if a.CanNameMpdm("C1") || a.CanNameMpdm("D1") {
		t.Error("a channel or a DM can be named")
	}
	on := "on"
	a.SetExperimentGroups(map[string]*string{"mpdm_naming": &on, "sc_mpdm_naming": &on})
	if u, ok := a.S.User("USELF"); ok {
		u.RestrictedUserType = "frontline_worker"
	}
	if a.CanNameMpdm("G1") {
		t.Error("a frontline worker can name a group DM")
	}
}

// RenameMpdm is renameChannelApi: conversations.rename {channel, name,
// validate}; under mpdm_naming the answer gets has_custom_mpdm_name (an
// empty name clears it), without it the answer is stored as it came.
func TestRenameMpdm(t *testing.T) {
	a, f := testApp(t)
	a.S.PutConv(slack.Conversation{ID: "G1", Name: "mpdm-me--alice--bob-1", IsMPIM: true, Members: []string{"USELF", "U1", "U2"}})
	a.SetExperimentGroup("mpdm_naming", "on")
	f.reply["conversations.rename"] = `{"ok":true,"channel":{"id":"G1","name":"Design crew","is_mpim":true,"members":["USELF","U1","U2"]}}`
	if err := a.RenameMpdm(context.Background(), "G1", "Design crew"); err != nil {
		t.Fatal(err)
	}
	if p := f.last("conversations.rename"); p.Get("channel") != "G1" || p.Get("name") != "Design crew" || p.Get("validate") != "true" {
		t.Errorf("conversations.rename params %v", p)
	}
	if n, ok := a.MpdmCustomName("G1"); !ok || n != "Design crew" || !a.MpdmHasCustomName("G1") {
		t.Errorf("after the rename: %q %v", n, ok)
	}
	f.reply["conversations.rename"] = `{"ok":true,"channel":{"id":"G1","name":"mpdm-me--alice--bob-1","is_mpim":true,"properties":{"has_custom_mpdm_name":true}}}`
	if err := a.RenameMpdm(context.Background(), "G1", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.MpdmCustomName("G1"); ok || a.MpdmHasCustomName("G1") {
		t.Error("an empty name kept the custom name")
	}
	// without mpdm_naming Slack leaves the answer's properties alone
	a.SetExperimentGroup("mpdm_naming", "")
	f.reply["conversations.rename"] = `{"ok":true,"channel":{"id":"G1","name":"Crew","is_mpim":true}}`
	if err := a.RenameMpdm(context.Background(), "G1", "Crew"); err != nil {
		t.Fatal(err)
	}
	if a.MpdmHasCustomName("G1") {
		t.Error("has_custom_mpdm_name set without mpdm_naming")
	}
	f.reply["conversations.rename"] = `{"ok":false,"error":"invalid_name_specials"}`
	var se *slack.Error
	if err := a.RenameMpdm(context.Background(), "G1", "a/b"); !errors.As(err, &se) || se.Code != "invalid_name_specials" {
		t.Errorf("a refusal: %v", err)
	}
}

// LHz7 hasMpdmWithDuplicateName: another named group DM with the name,
// trimmed and case-folded, under mpdm_naming only.
func TestHasMpdmWithDuplicateName(t *testing.T) {
	a, _ := testApp(t)
	named := json.RawMessage(`{"has_custom_mpdm_name":true}`)
	a.S.PutConv(slack.Conversation{ID: "G1", Name: "Design crew", IsMPIM: true, Properties: named})
	a.S.PutConv(slack.Conversation{ID: "G2", Name: "mpdm-me--alice-1", IsMPIM: true})
	a.SetExperimentGroup("mpdm_naming", "on")
	if !a.HasMpdmWithDuplicateName("  design CREW ", "G2") {
		t.Error("the duplicate isn't found")
	}
	if a.HasMpdmWithDuplicateName("Design crew", "G1") || a.HasMpdmWithDuplicateName("mpdm-me--alice-1", "G1") || a.HasMpdmWithDuplicateName(" ", "G2") {
		t.Error("the group DM itself, an unnamed one or a blank name counts")
	}
	a.SetExperimentGroup("mpdm_naming", "off")
	if a.HasMpdmWithDuplicateName("Design crew", "G2") {
		t.Error("duplicates without mpdm_naming")
	}
}

// enterprise.prefs.get fills getOrgPref / getLockedOrgPref, at the top
// level or under prefs.
func TestEnterprisePrefs(t *testing.T) {
	a, f := testApp(t)
	if a.EnterpriseOrgPrefTrue("mpdm_add_people_default_exclude_history") {
		t.Error("an org pref before enterprise.prefs.get")
	}
	f.reply["enterprise.prefs.get"] = `{"ok":true,"org_prefs":{"mpdm_add_people_default_exclude_history":true},"locked_prefs":{"content_review_enabled":true}}`
	if err := a.LoadEnterprisePrefs(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !a.EnterpriseOrgPrefTrue("mpdm_add_people_default_exclude_history") || string(a.EnterpriseLockedPref("content_review_enabled")) != "true" {
		t.Error("the org prefs aren't read")
	}
	f.reply["enterprise.prefs.get"] = `{"ok":true,"prefs":{"org_prefs":{"mpdm_add_people_default_exclude_history":false}}}`
	if err := a.LoadEnterprisePrefs(context.Background()); err != nil {
		t.Fatal(err)
	}
	if a.EnterpriseOrgPrefTrue("mpdm_add_people_default_exclude_history") {
		t.Error("prefs nested under prefs aren't read")
	}
}

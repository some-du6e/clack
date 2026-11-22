package core

// Settings' Message history and Export rules (channel_retention.go), with
// the answers Slack's code gives (boot-shared uUSj additionalOptionsTo
// Show: dataRetention, exportHistory; admin-channel-modal oWlM tU and
// isChannelRetentionOverrideEnforced), and the calls' forms. The rules
// and calls are reached through channel_retention_api_test.go.

import (
	"encoding/json"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

type retViewer struct{ owner, admin, guest, entAdmin bool }

// retApp is testApp on a Grid workspace with a plan (isPaidTeam) and the
// viewer v.
func retApp(t *testing.T, v retViewer) (*App, *fakeSlack) {
	t.Helper()
	a, f := testApp(t)
	a.C.Creds.EnterpriseID = "E1"
	a.setTeamPlan("enterprise", "E1")
	u := *mustUser(a.S, "USELF")
	u.IsOwner, u.IsAdmin, u.IsRestricted = v.owner, v.admin, v.guest
	if v.entAdmin {
		u.EnterpriseUser = &slack.EnterpriseUser{IsAdmin: true}
	}
	a.S.PutUsers([]slack.User{u})
	a.S.Self = u
	return a, f
}

func retInf(kind string, archived, orgShared bool) ChannelDetails {
	return ChannelDetails{Conv: Conv{ID: "C5", Kind: kind, IsArchived: archived}, IsOrgShared: orgShared}
}

func TestMessageHistoryShownLikeSlack(t *testing.T) {
	member, owner := retViewer{}, retViewer{owner: true}
	for _, tc := range []struct {
		name      string
		v         retViewer
		free      bool
		notGrid   bool
		team, org string // team.prefs / org_prefs allow_retention_override ("" unset)
		unlocked  string // unlocked_prefs allow_retention_override
		orgLevel  bool   // enterprise_retention_allow_override_on_org_level_channels
		inf       ChannelDetails
		want      bool
	}{
		// DZwJ isPaidTeam first
		{name: "free team", v: owner, free: true, inf: retInf("private", false, false), want: false},
		// t1L8 JF: unset counts as allowed; rW && !yG: a public channel
		// needs a workspace owner or org admin
		{name: "member, private", v: member, inf: retInf("private", false, false), want: true},
		{name: "member, public", v: member, inf: retInf("channel", false, false), want: false},
		{name: "admin (not owner), public", v: retViewer{admin: true}, inf: retInf("channel", false, false), want: false},
		{name: "owner, public", v: owner, inf: retInf("channel", false, false), want: true},
		{name: "org admin, public", v: retViewer{entAdmin: true}, inf: retInf("channel", false, false), want: true},
		{name: "team forbids overrides", v: owner, team: "false", inf: retInf("private", false, false), want: false},
		// oMux p: on Grid the org pref comes before the team's
		{name: "org pref over team pref", v: member, team: "false", org: "true", inf: retInf("private", false, false), want: true},
		{name: "org pref false", v: owner, team: "true", org: "false", inf: retInf("channel", false, false), want: false},
		// HkVr DM, +NpH F
		{name: "guest", v: retViewer{guest: true}, inf: retInf("private", false, false), want: false},
		{name: "archived", v: owner, inf: retInf("private", true, false), want: false},
		// DMs, group DMs and org-shared channels on Grid: the unlocked org
		// pref decides when set (even for a guest: it returns first)
		{name: "DM, unlocked false", v: member, team: "true", unlocked: "false", inf: retInf("im", false, false), want: false},
		{name: "DM, unlocked true, guest", v: retViewer{guest: true}, unlocked: "true", inf: retInf("im", false, false), want: true},
		{name: "group DM, unlocked unset", v: member, inf: retInf("mpim", false, false), want: true},
		{name: "org-shared private, unlocked false", v: member, unlocked: "false", inf: retInf("private", false, true), want: false},
		{name: "org-shared public, unlocked true", v: member, unlocked: "true", inf: retInf("channel", false, true), want: true},
		{name: "org-level experiment on", v: member, unlocked: "false", orgLevel: true, inf: retInf("im", false, false), want: true},
		{name: "not Grid: unlocked ignored", v: member, notGrid: true, unlocked: "false", inf: retInf("im", false, false), want: true},
		{name: "plain private ignores unlocked", v: member, unlocked: "false", inf: retInf("private", false, false), want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := retApp(t, tc.v)
			if tc.free {
				a.setTeamPlan("", "E1")
			}
			if tc.notGrid {
				a.C.Creds.EnterpriseID = ""
			}
			if tc.team != "" {
				a.SetTeamPrefs(map[string]json.RawMessage{"allow_retention_override": json.RawMessage(tc.team)})
			}
			ent := map[string]json.RawMessage{}
			if tc.org != "" {
				ent["org_prefs"] = json.RawMessage(`{"allow_retention_override":` + tc.org + `}`)
			}
			if tc.unlocked != "" {
				ent["unlocked_prefs"] = json.RawMessage(`{"allow_retention_override":` + tc.unlocked + `}`)
			}
			a.setEnterprisePrefs(ent)
			if tc.orgLevel {
				a.SetExperimentGroup(ExpRetentionOrgLevel, "on")
			}
			if got := retHistoryShown(a, tc.inf); got != tc.want {
				t.Errorf("Message history shown = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestExportHistoryShownLikeSlack(t *testing.T) {
	for _, tc := range []struct {
		name string
		v    retViewer
		exp  bool // channel_exports
		kind string
		want bool
	}{
		{"experiment off, admin", retViewer{admin: true}, false, "private", false},
		{"member, public", retViewer{}, true, "channel", false},
		{"owner (not admin), public", retViewer{owner: true}, true, "channel", false},
		{"admin, public", retViewer{admin: true}, true, "channel", true},
		{"member, private", retViewer{}, true, "private", true},
		{"member, DM", retViewer{}, true, "im", true},
		{"member, group DM", retViewer{}, true, "mpim", true},
		{"guest admin, private", retViewer{guest: true, admin: true}, true, "private", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := retApp(t, tc.v)
			if tc.exp {
				a.SetExperimentGroup(ExpChannelExports, "on")
			}
			if got := retExportShown(a, retInf(tc.kind, false, false)); got != tc.want {
				t.Errorf("Export shown = %v, want %v", got, tc.want)
			}
		})
	}
}

// The row's setting: the channel's own when it has one and overrides are
// enforced, else the team's for the conversation type (useRetentionPrefs).
func TestShownRetentionLikeSlack(t *testing.T) {
	a, _ := retApp(t, retViewer{})
	a.SetTeamPrefs(map[string]json.RawMessage{
		"retention_type": json.RawMessage(`2`), "retention_duration": json.RawMessage(`400`),
		"group_retention_type": json.RawMessage(`1`), "group_retention_duration": json.RawMessage(`0`),
		"dm_retention_type": json.RawMessage(`2`), "dm_retention_duration": json.RawMessage(`"90"`),
	})
	own := Retention{Type: 1, Duration: 30}
	for _, tc := range []struct {
		name string
		inf  ChannelDetails
		own  Retention
		want Retention
	}{
		{"channel override", retInf("channel", false, false), own, own},
		{"no override: the team's", retInf("channel", false, false), Retention{}, Retention{Type: 2, Duration: 400}},
		{"private: group_", retInf("private", false, false), Retention{}, Retention{Type: 1}},
		{"DM: dm_", retInf("im", false, false), Retention{}, Retention{Type: 2, Duration: 90}},
		{"group DM: dm_", retInf("mpim", false, false), Retention{}, Retention{Type: 2, Duration: 90}},
	} {
		if got := retShown(a, tc.inf, tc.own); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
	// isChannelRetentionOverrideEnforced: a team that forbids overrides
	// shows its own setting on a channel, not on a DM or an org-shared
	// channel; allow_admin_retention_override === 1 enforces it again
	a.SetTeamPrefs(map[string]json.RawMessage{"allow_retention_override": json.RawMessage(`false`),
		"retention_type": json.RawMessage(`1`), "dm_retention_type": json.RawMessage(`0`)})
	if got := retShown(a, retInf("channel", false, false), own); got != (Retention{Type: 1}) {
		t.Errorf("not enforced: %+v, want the team's", got)
	}
	if got := retShown(a, retInf("im", false, false), own); got != own {
		t.Errorf("a DM's override is always enforced: %+v", got)
	}
	if got := retShown(a, retInf("channel", false, true), own); got != own {
		t.Errorf("an org-shared channel's override is always enforced: %+v", got)
	}
	a.SetTeamPrefs(map[string]json.RawMessage{"allow_retention_override": json.RawMessage(`false`),
		"allow_admin_retention_override": json.RawMessage(`1`)})
	if got := retShown(a, retInf("channel", false, false), own); got != own {
		t.Errorf("allow_admin_retention_override 1: %+v, want the channel's", got)
	}
}

// getDataRetention / setDataRetention / submitChannelExport forms and
// parsing (wqjU ex, eB, ek).
func TestRetentionCalls(t *testing.T) {
	a, f := retApp(t, retViewer{})
	f.reply["conversations.getRetention"] = `{"ok":true,"retention":{"retention_type":"1","retention_duration":45}}`
	f.reply["conversations.setRetention"] = `{"ok":true}`
	f.reply["conversations.export"] = `{"ok":true}`
	r, err := retGet(a, "C5")
	if err != nil || r != (Retention{Type: 1, Duration: 45}) {
		t.Fatalf("getRetention: %+v %v", r, err)
	}
	if got := f.last("conversations.getRetention"); got.Get("channel") != "C5" {
		t.Errorf("getRetention form %v", got)
	}
	f.reply["conversations.getRetention"] = `{"ok":true}`
	if r, err := retGet(a, "C5"); err != nil || r != (Retention{}) {
		t.Errorf("no retention: %+v %v, want 0, 0", r, err)
	}
	if err := retSet(a, "C5", 1, 730); err != nil {
		t.Fatal(err)
	}
	if got := f.last("conversations.setRetention"); got.Get("channel") != "C5" || got.Get("retention_type") != "1" || got.Get("retention_duration") != "730" {
		t.Errorf("setRetention custom form %v", got)
	}
	if err := retSet(a, "C5", 0, 730); err != nil {
		t.Fatal(err)
	}
	if got := f.last("conversations.setRetention"); got.Get("retention_type") != "0" || got.Has("retention_duration") {
		t.Errorf("setRetention default form %v (no duration for type 0)", got)
	}
	if err := retExport(a, "C5"); err != nil {
		t.Fatal(err)
	}
	got := f.last("conversations.export")
	got.Del("token") // the session's, in every form
	if got.Get("channel") != "C5" || len(got) != 1 {
		t.Errorf("export form %v, want {channel}", got)
	}
}

// convertChannelToPrivateApi's xulU convertToPrivate: the stored channel
// is private once the call answers.
func TestConvertToPrivateMarksStoredChannel(t *testing.T) {
	a, f := testApp(t)
	a.S.PutConv(slack.Conversation{ID: "C6", Name: "plans", IsChannel: true, IsMember: true})
	f.reply["conversations.convertToPrivate"] = `{"ok":true}`
	if err := a.ConvertToPrivate(t.Context(), "C6"); err != nil {
		t.Fatal(err)
	}
	if c, ok := a.Conv("C6"); !ok || c.Kind != "private" {
		t.Errorf("after convertToPrivate the channel is %+v, want private", c)
	}
}

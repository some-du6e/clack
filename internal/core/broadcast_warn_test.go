package core

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
)

// The rest of Slack's broadcast rules (broadcast.go, teamprefs.go): the
// large-channel stop, the channel's enable_at_channel pref and the
// "Notify N people?" warning. Prefs are fixtures shaped like Hack Club's
// real answers (2026-11-08, read-only): team.prefs.get has
// loud_channel_mentions_limit 10000 and warn_before_at_channel "always";
// channels.prefs.get enable_at_channel answers pref_value {}; users.prefs
// has last_seen_at_channel_warning as a millisecond number.

func TestStopChannelMentions(t *testing.T) {
	grid := MentionLimit{Grid: true, Limit: 10000}
	for _, tc := range []struct {
		name string
		c    Conv
		l    MentionLimit
		want bool
	}{
		{"big channel on Grid", cvPublic, MentionLimit{Grid: true, Limit: 10000, Members: 10000}, true},
		{"just under the limit", cvPublic, MentionLimit{Grid: true, Limit: 10000, Members: 9999}, false},
		{"admins are exempt", cvPublic, MentionLimit{Grid: true, Limit: 10000, Members: 50000, Admin: true}, false},
		{"not Grid", cvPublic, MentionLimit{Limit: 10000, Members: 50000}, false},
		{"no limit pref", cvPublic, MentionLimit{Grid: true, Members: 50000}, false},
		{"group DM counts", cvMPIM, MentionLimit{Grid: true, Limit: 3, Members: 4}, true},
		{"never a DM", cvIM, MentionLimit{Grid: true, Limit: 1, Members: 2}, false},
		{"unknown channel", Conv{}, grid, false},
	} {
		if got := StopChannelMentions(tc.c, tc.l); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	stopped := BroadcastPerms{Known: true, AtChannel: true, AtEveryone: true, Stopped: true}
	if got := BroadcastKeywords(cvGeneral, stopped); len(got) != 0 {
		t.Errorf("stopped #general offers %v", got)
	}
	msg, no := ProhibitedBroadcast(BroadcastSend{Conv: cvPublic, Perms: stopped, Used: []string{KeywordChannel}})
	if !no || msg != "Usage of *@channel* is restricted in this large channel." {
		t.Errorf("stopped @channel: %q %v", msg, no)
	}
	msg, _ = ProhibitedBroadcast(BroadcastSend{Conv: cvGeneral, Perms: stopped, Used: []string{KeywordEveryone}})
	if msg != "Usage of *@everyone* is restricted in the large general channel." {
		t.Errorf("stopped @everyone: %q", msg)
	}
}

func TestShouldWarnBroadcast(t *testing.T) {
	now := time.Date(2026, 11, 8, 15, 0, 0, 0, time.Local)
	today := time.Date(2026, 11, 8, 9, 0, 0, 0, time.Local).UnixMilli()
	yesterday := time.Date(2026, 11, 7, 23, 0, 0, 0, time.Local).UnixMilli()
	base := BroadcastWarn{Conv: cvPublic, Used: []string{KeywordChannel}, CanAtChannel: true, Enabled: true,
		Pref: "always", Members: 40, Now: now}
	with := func(f func(w *BroadcastWarn)) BroadcastWarn {
		w := base
		f(&w)
		return w
	}
	for _, tc := range []struct {
		name string
		w    BroadcastWarn
		want bool
	}{
		{"always", base, true},
		{"no keyword", with(func(w *BroadcastWarn) { w.Used = nil }), false},
		{"never", with(func(w *BroadcastWarn) { w.Pref = "never" }), false},
		{"pref missing", with(func(w *BroadcastWarn) { w.Pref = "" }), false},
		{"once, never seen", with(func(w *BroadcastWarn) { w.Pref = "once" }), true},
		{"once, seen", with(func(w *BroadcastWarn) { w.Pref = "once"; w.LastSeen = yesterday }), false},
		{"daily, seen yesterday", with(func(w *BroadcastWarn) { w.Pref = "daily"; w.LastSeen = yesterday }), true},
		{"daily, seen today", with(func(w *BroadcastWarn) { w.Pref = "daily"; w.LastSeen = today }), false},
		{"always, seen today", with(func(w *BroadcastWarn) { w.LastSeen = today }), true},
		{"5 members", with(func(w *BroadcastWarn) { w.Members = 5 }), false},
		{"6 members", with(func(w *BroadcastWarn) { w.Members = 6 }), true},
		{"count unknown", with(func(w *BroadcastWarn) { w.Members = 0 }), false},
		{"@here not allowed", with(func(w *BroadcastWarn) { w.Used = []string{KeywordHere}; w.CanAtChannel = false }), false},
		{"disabled in the channel", with(func(w *BroadcastWarn) { w.Enabled = false }), false},
		{"DM", with(func(w *BroadcastWarn) { w.Conv = cvIM }), false},
		{"group DM", with(func(w *BroadcastWarn) { w.Conv = cvMPIM }), true},
		{"@everyone in #general", with(func(w *BroadcastWarn) {
			w.Conv, w.Used, w.CanAtChannel, w.CanAtEveryone = cvGeneral, []string{KeywordEveryone}, false, true
		}), true},
		{"@everyone in #general, not allowed", with(func(w *BroadcastWarn) {
			w.Conv, w.Used, w.CanAtEveryone = cvGeneral, []string{KeywordEveryone}, false
		}), false},
		// Slack doesn't check @everyone outside #general here: it warns,
		// and hasProhibitedKeywords refuses after Send
		{"@everyone elsewhere", with(func(w *BroadcastWarn) { w.Used = []string{KeywordEveryone}; w.CanAtChannel = false }), true},
	} {
		if got := ShouldWarnBroadcast(tc.w); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestWarnKeyword(t *testing.T) {
	for _, tc := range []struct {
		used []string
		want string
	}{
		{[]string{"here"}, "here"},
		{[]string{"here", "channel"}, "channel"},
		{[]string{"channel", "everyone", "here"}, "everyone"},
		{nil, ""},
	} {
		if got := WarnKeyword(tc.used); got != tc.want {
			t.Errorf("%v: %q, want %q", tc.used, got, tc.want)
		}
	}
}

func TestBroadcastPrefEnabled(t *testing.T) {
	for v, want := range map[string]bool{
		`{}`: true, `{"enabled":true}`: true, `{"enabled":false}`: false, `{"other":1}`: false,
		`null`: true, ``: true, `true`: true, `false`: true, `0`: true, `""`: true, `"false"`: false, `[]`: true,
	} {
		if got := BroadcastPrefEnabled(json.RawMessage(v)); got != want {
			t.Errorf("%s: %v, want %v", v, got, want)
		}
	}
}

// fakeBroadcastApp is an App against fakeslack with its first channel in
// the store.
func fakeBroadcastApp(t *testing.T, fs *fakeslack.Server, self slack.User) (*App, string) {
	t.Helper()
	ch := fs.ChannelIDs()[0]
	s := store.New("")
	s.PutConv(slack.Conversation{ID: ch, Name: "big", IsChannel: true, IsMember: true, NumMembers: 1100})
	self.ID = fakeslack.SelfID
	s.PutUsers([]slack.User{self})
	s.Self = self
	return New(slack.New(fs.Creds()), s), ch
}

func TestTeamPrefsAndStop(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	fs.TeamPrefs = map[string]any{"loud_channel_mentions_limit": 1000, "warn_before_at_channel": "always"}
	a, ch := fakeBroadcastApp(t, fs, slack.User{})
	ctx := context.Background()
	if err := a.LoadTeamPrefs(ctx); err != nil {
		t.Fatal(err)
	}
	if n, ok := a.TeamPrefInt("loud_channel_mentions_limit"); !ok || n != 1000 {
		t.Errorf("limit %d %v", n, ok)
	}
	if got := a.TeamPrefString("warn_before_at_channel"); got != "always" {
		t.Errorf("warn pref %q", got)
	}
	// fakeslack's users/counts: 1100 people ≥ 1000
	p, err := a.LoadBroadcastPerms(ctx, ch)
	if err != nil || !p.AtChannel || !p.Stopped {
		t.Fatalf("perms %+v %v, want CAN_AT_CHANNEL and stopped", p, err)
	}
	if got := a.BroadcastKeywordsFor(ch); len(got) != 0 {
		t.Errorf("keywords in a stopped channel: %v", got)
	}
	if a.BroadcastWarning(ctx, ch, []string{KeywordChannel}, true) {
		t.Error("warning for a keyword the stop takes away")
	}

	// team_pref_change lifts the limit
	a.onTeamPrefChange(json.RawMessage(`{"type":"team_pref_change","name":"loud_channel_mentions_limit","value":0,"team_id":"` + fakeslack.TeamID + `"}`))
	if p := a.BroadcastPerms(ch); p.Stopped {
		t.Errorf("still stopped after the pref changed: %+v", p)
	}

	// admins are exempt
	fs2, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	defer fs2.Close()
	fs2.TeamPrefs = fs.TeamPrefs
	admin, ch2 := fakeBroadcastApp(t, fs2, slack.User{IsAdmin: true})
	_ = admin.LoadTeamPrefs(ctx)
	if p, _ := admin.LoadBroadcastPerms(ctx, ch2); p.Stopped {
		t.Errorf("admin stopped: %+v", p)
	}
}

func TestTeamPrefChangeOtherTeam(t *testing.T) {
	a := New(slack.New(slack.Creds{Token: "xoxc-test", UserID: "U1", Team: "T1"}), store.New(""))
	a.onTeamPrefChange(json.RawMessage(`{"name":"warn_before_at_channel","value":"daily","team_id":"T2"}`))
	if got := a.TeamPrefString("warn_before_at_channel"); got != "" {
		t.Errorf("another team's change applied off Grid: %q", got)
	}
	a.onTeamPrefChange(json.RawMessage(`{"name":"warn_before_at_channel","value":"daily","team_id":"T1"}`))
	if got := a.TeamPrefString("warn_before_at_channel"); got != "daily" {
		t.Errorf("own change: %q", got)
	}
}

func TestBroadcastEnabledAndWarning(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	fs.TeamPrefs = map[string]any{"warn_before_at_channel": "once"}
	a, ch := fakeBroadcastApp(t, fs, slack.User{IsAdmin: true})
	ctx := context.Background()
	_ = a.LoadTeamPrefs(ctx)

	if !a.LoadBroadcastEnabled(ctx, ch) {
		t.Error("pref_value {} read as disabled")
	}
	fs.ChannelPrefs = map[string]map[string]any{ch: {"enable_at_channel": map[string]any{"enabled": false}}}
	if a.LoadBroadcastEnabled(ctx, ch) {
		t.Error("enabled false read as enabled")
	}
	fs.ChannelPrefs = nil

	if !a.BroadcastWarning(ctx, ch, []string{KeywordHere}, true) {
		t.Fatal("no warning with warn_before_at_channel once, never seen")
	}
	if a.BroadcastWarning(ctx, ch, []string{KeywordHere}, false) {
		t.Error("warning in a channel with keywords disabled")
	}
	members, tz, err := a.BroadcastAudience(ctx, ch)
	if err != nil || members < 10 || tz < 1 { // fakeslack's channel: its num_members, 1–12 zones
		t.Errorf("audience %d people, %d time zones, %v", members, tz, err)
	}
	if err := a.MarkBroadcastWarningSeen(ctx); err != nil {
		t.Fatal(err)
	}
	if v, ok := fs.Pref("last_seen_at_channel_warning"); !ok || v == "" {
		t.Errorf("last_seen_at_channel_warning not written: %v %v", v, ok)
	}
	if a.PrefInt("last_seen_at_channel_warning", 0) == 0 {
		t.Error("last seen not kept locally")
	}
	if a.BroadcastWarning(ctx, ch, []string{KeywordHere}, true) {
		t.Error("\"once\" warned again")
	}
	admin, url := a.BroadcastWarningAdmin()
	if !admin || url == "" || url[len(url)-len("admin/settings#permissions"):] != "admin/settings#permissions" {
		t.Errorf("admin %v %q", admin, url)
	}
}

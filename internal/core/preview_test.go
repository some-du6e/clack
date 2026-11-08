package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// previewApp has C1 (member) plus C9, a channel the viewer isn't in, known
// from ResolveConv, and C8, an archived non-member channel.
func previewApp(t *testing.T) (*App, *fakeSlack) {
	a, f := testApp(t)
	a.S.PutExtConv(slack.Conversation{ID: "C9", Name: "lounge", IsChannel: true, NumMembers: 40,
		Purpose: &slack.Purpose{Value: "hang out"}})
	a.S.PutExtConv(slack.Conversation{ID: "C8", Name: "old", IsChannel: true, IsArchived: true})
	close(a.ready)
	return a, f
}

func TestPreviewIsSlacksIsPreview(t *testing.T) {
	a, _ := previewApp(t)
	for id, want := range map[string]bool{"C1": false, "C9": true, "C8": false, "D1": false} {
		c, ok := a.Conv(id)
		if !ok {
			t.Fatalf("%s unknown", id)
		}
		if c.Preview != want {
			t.Errorf("%s Preview = %v, want %v", id, c.Preview, want)
		}
	}
	// conversations.info saying is_member (e.g. just joined elsewhere) wins
	a.S.PutExtConv(slack.Conversation{ID: "C7", Name: "joined", IsChannel: true, IsMember: true})
	if c, _ := a.Conv("C7"); c.Preview {
		t.Error("is_member channel shown as a preview")
	}
}

func TestSelectedPreviewGetsASidebarRow(t *testing.T) {
	a, _ := previewApp(t)
	evs, stop := a.Subscribe()
	defer stop()
	if strings.Contains(sectionsByType(a)["channels"], "C9") {
		t.Fatal("unselected preview in the sidebar")
	}
	a.SetSelected("C9")
	next(t, evs, "sidebar_changed")
	if got := sectionsByType(a)["channels"]; !strings.Contains(got, "C9") || !strings.Contains(got, "C1") {
		t.Errorf("selected preview missing from Channels: %q", got)
	}
	var found bool
	for _, c := range a.Snapshot().Convs {
		if c.ID == "C9" {
			found = c.Preview
		}
	}
	if !found {
		t.Error("snapshot lacks the previewed conv (or it isn't marked Preview)")
	}
	a.SetSelected("C1")
	next(t, evs, "sidebar_changed")
	if strings.Contains(sectionsByType(a)["channels"], "C9") {
		t.Error("preview row stayed after leaving it")
	}
}

func TestSelectedRowSurvivesHideRead(t *testing.T) {
	a, _ := previewApp(t)
	p := a.S.Prefs
	p.SidebarBehavior = "hide_read_channels"
	a.S.SetPrefs(p, nil)
	a.S.PutConv(slack.Conversation{ID: "C2", Name: "quiet", IsChannel: true})
	if strings.Contains(sectionsByType(a)["channels"], "C2") {
		t.Skip("hide_read did not hide a read row in this fixture")
	}
	a.SetSelected("C2")
	if !strings.Contains(sectionsByType(a)["channels"], "C2") {
		t.Error("the open conversation's row was filtered out (lJBI keeps it)")
	}
}

func TestJoinForSendsExpirationAndFilesTemporary(t *testing.T) {
	a, f := previewApp(t)
	a.SetExperimentGroup("temp_join_channel", "on")
	a.S.Sections = []slack.Section{{ID: "LCH", Type: "channels"}, {ID: "LTMP", Type: "temporary_channels"}}
	f.reply["conversations.join"] = `{"ok":true,"channel":{"id":"C9","name":"lounge","is_channel":true,"is_member":true}}`
	f.reply["conversations.info"] = `{"ok":true,"channel":{"id":"C9","name":"lounge","is_channel":true,"is_member":true}}`
	before := time.Now()
	if _, err := a.JoinFor(context.Background(), "C9", TempTwoDays); err != nil {
		t.Fatal(err)
	}
	form := f.last("conversations.join")
	if form.Get("channel") != "C9" || form.Get("expiration_ts") == "" {
		t.Fatalf("join form %v", form)
	}
	if exp := a.TempExpiry("C9"); exp < before.Add(47*time.Hour).Unix() || exp > time.Now().Add(49*time.Hour).Unix() {
		t.Errorf("expiry %d not ~48h out", exp)
	}
	got := sectionsByType(a)
	if !strings.Contains(got["temporary_channels"], "C9") || strings.Contains(got["channels"], "C9") {
		t.Errorf("temporary join not in Temporary channels: %v", got)
	}
	if c, _ := a.Conv("C9"); c.Preview || c.TempUntil == 0 {
		t.Errorf("after join: %+v", c)
	}
	// a plain Join carries no expiration_ts and is permanent
	if _, err := a.Join(context.Background(), "C9"); err != nil {
		t.Fatal(err)
	}
	if f.last("conversations.join").Has("expiration_ts") {
		t.Error("plain join sent expiration_ts")
	}
	if a.TempExpiry("C9") != 0 || strings.Contains(sectionsByType(a)["temporary_channels"], "C9") {
		t.Error("plain join kept the temporary membership")
	}
}

func TestTempSectionHiddenWithoutTempChannels(t *testing.T) {
	a, _ := previewApp(t)
	a.SetExperimentGroup("temp_join_channel", "on")
	a.S.Sections = []slack.Section{{ID: "LCH", Type: "channels"}, {ID: "LTMP", Type: "temporary_channels"}}
	if _, ok := sectionsByType(a)["temporary_channels"]; ok {
		t.Error("empty Temporary channels section shown")
	}
	// off: a temporary membership is an ordinary channel
	a.SetExperimentGroup("temp_join_channel", "control")
	a.setTemp("C1", time.Now().Add(time.Hour).Unix())
	if got := sectionsByType(a); !strings.Contains(got["channels"], "C1") {
		t.Errorf("experiment off: %v", got)
	}
}

func TestChannelJoinedTempExpiry(t *testing.T) {
	a, f := previewApp(t)
	f.reply["conversations.info"] = `{"ok":true,"channel":{"id":"C9","name":"lounge","is_channel":true,"is_member":true}}`
	exp := time.Now().Add(24 * time.Hour).Unix()
	a.handle(rtm("channel_joined", map[string]any{"channel": map[string]any{"id": "C9", "name": "lounge"}, "temp_membership_expiration_ts": exp}))
	if a.TempExpiry("C9") != exp {
		t.Errorf("temp expiry %d, want %d", a.TempExpiry("C9"), exp)
	}
	a.handle(rtm("channel_joined", map[string]any{"channel": map[string]any{"id": "C9"}, "temp_membership_expiration_ts": 0}))
	if a.TempExpiry("C9") != 0 {
		t.Error("expiration 0 kept the temporary membership")
	}
}

func TestLeaveTurnsTheOpenChannelIntoAPreview(t *testing.T) {
	a, f := previewApp(t)
	f.reply["conversations.leave"] = `{"ok":true}`
	a.SetSelected("C1")
	if err := a.Leave(context.Background(), "C1"); err != nil {
		t.Fatal(err)
	}
	c, ok := a.Conv("C1")
	if !ok || !c.Preview {
		t.Fatalf("left channel: ok=%v %+v", ok, c)
	}
	if !strings.Contains(sectionsByType(a)["channels"], "C1") {
		t.Error("the open, just-left channel lost its row")
	}
}

func TestTempJoinExpiry(t *testing.T) {
	loc := time.FixedZone("x", -5*3600)
	now := time.Date(2026, 11, 8, 10, 30, 0, 0, loc)
	if got := TempJoinExpiry(TempToday, now); got != time.Date(2026, 11, 8, 23, 59, 59, 0, loc).Unix() {
		t.Errorf("today = %d", got)
	}
	if got := TempJoinExpiry(TempTwoDays, now); got != now.Add(48*time.Hour).Unix() {
		t.Errorf("two_days = %d", got)
	}
	if got := TempJoinExpiry(TempWeek, now); got != now.AddDate(0, 0, 7).Unix() {
		t.Errorf("one_week = %d", got)
	}
	if TempJoinExpiry("", now) != 0 {
		t.Error("no duration should be permanent")
	}
}

// ---- posting permissions (core/posting.go) ----

func postingConv(props string) *slack.Conversation {
	return &slack.Conversation{ID: "CANN", Name: "ann", IsChannel: true, Properties: json.RawMessage(props)}
}

func TestPostingFollowsComputeChannelPostingPermissions(t *testing.T) {
	bt := func(b bool) *bool { return &b }
	cases := []struct {
		name string
		self slack.User
		conv *slack.Conversation
		want Posting
	}{
		{"no prefs", slack.User{}, postingConv(``), Posting{}},
		{"admin-only posting, member", slack.User{}, postingConv(`{"posting_restricted_to":{"type":["admin"]}}`), Posting{ThreadOnly: true}},
		{"admin-only posting, admin", slack.User{IsAdmin: true}, postingConv(`{"posting_restricted_to":{"type":["admin"]}}`), Posting{}},
		{"org admin counts as admin", slack.User{EnterpriseUser: &slack.EnterpriseUser{IsAdmin: true}}, postingConv(`{"posting_restricted_to":{"type":["admin"]}}`), Posting{}},
		{"admin posts and replies: read-only", slack.User{}, postingConv(`{"posting_restricted_to":{"type":["admin"]},"threads_restricted_to":{"type":["admin"]}}`), Posting{ReadOnly: true}},
		{"admin posts, everyone replies", slack.User{}, postingConv(`{"posting_restricted_to":{"type":["admin"]},"threads_restricted_to":{"type":["ra"]}}`), Posting{ThreadOnly: true}},
		{"listed user may post", slack.User{}, postingConv(`{"posting_restricted_to":{"type":["admin"],"user":["USELF"]}}`), Posting{}},
		{"regular: members yes", slack.User{}, postingConv(`{"posting_restricted_to":{"type":["regular"]}}`), Posting{}},
		{"regular: guests no", slack.User{IsRestricted: true}, postingConv(`{"posting_restricted_to":{"type":["regular"]}}`), Posting{ThreadOnly: true}},
		{"org_admin: workspace admin no", slack.User{IsAdmin: true}, postingConv(`{"posting_restricted_to":{"type":["org_admin"]}}`), Posting{ThreadOnly: true}},
		{"empty pref blocks (isEmpty)", slack.User{}, postingConv(`{"posting_restricted_to":{}}`), Posting{ThreadOnly: true}},
		{"type list without a type: everyone", slack.User{}, postingConv(`{"posting_restricted_to":{"type":[],"user":[]}}`), Posting{}},
		{"role wins over type", slack.User{}, postingConv(`{"posting_restricted_to":{"role":"ra","type":["admin"]}}`), Posting{}},
		{"server flag wins", slack.User{}, &slack.Conversation{ID: "CANN", IsChannel: true, IsThreadOnly: bt(true),
			Properties: json.RawMessage(`{"posting_restricted_to":{"type":["admin"]}}`)}, Posting{ThreadOnly: true}},
		{"non-threadable", slack.User{}, &slack.Conversation{ID: "CANN", IsChannel: true, IsNonThreadable: true}, Posting{NoThreads: true}},
		{"shared channel hosted elsewhere: admin no", slack.User{IsAdmin: true}, &slack.Conversation{ID: "CANN", IsChannel: true,
			IsExtShared: true, ConversationHostID: "TOTHER", Properties: json.RawMessage(`{"posting_restricted_to":{"type":["admin"]}}`)}, Posting{ThreadOnly: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := testApp(t)
			u := tc.self
			u.ID, u.Name = "USELF", "me"
			a.S.PutUsers([]slack.User{u})
			if got := a.PostingFor(tc.conv); got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
	if (Posting{ThreadOnly: true}).CanPost() || !(Posting{ThreadOnly: true}).CanReply() || (Posting{ReadOnly: true}).CanReply() {
		t.Error("CanPost/CanReply")
	}
}

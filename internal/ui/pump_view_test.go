package ui

import (
	"context"
	"testing"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// The shell's pump posts (and so draws a frame for) only the events that
// change what the window draws or that it acts on (wants, pump_view.go):
// a message in a channel that is already unread, a DM partner's status
// text, presence that didn't move, unchanged badges, a lookup of a
// channel the viewer isn't in and the events the views follow on their
// own draw nothing; a new unread, a mention, a DM, a rename, a picture,
// a status emoji, a presence change and the viewer's own profile do.
func TestPumpPostsOnlyWhatDraws(t *testing.T) {
	st := store.New("")
	st.PutUsers([]slack.User{
		{ID: "USELF", Name: "me", Profile: slack.Profile{DisplayName: "Me"}},
		{ID: "UDM", Name: "ada", Profile: slack.Profile{DisplayName: "Ada", StatusEmoji: ":wave:"}},
		{ID: "UGDM", Name: "grace", Profile: slack.Profile{DisplayName: "Grace"}},
	})
	st.Self = slack.User{ID: "USELF", Name: "me"}
	c := core.New(slack.New(slack.Creds{Token: "xoxc-test", UserID: "USELF", Team: "T1"}), st)
	t.Cleanup(c.Shutdown)
	b := &backend{core: c, selfID: "USELF"}
	dm, _ := c.RawUser("UDM")
	b.m = model{SelfID: "USELF", SelfName: "Me", SelfActive: true, Convs: map[string]*convInfo{
		"C1":  {ID: "C1", Name: "general", Kind: "channel", Unread: true},
		"C2":  {ID: "C2", Name: "random", Kind: "channel"},
		"D1":  {ID: "D1", Name: dm.Name, Kind: "im", User: "UDM", Avatar: dm.Avatar, StatusEmoji: dm.StatusEmojiURL, Active: true},
		"G1":  {ID: "G1", Name: "grace", Kind: "mpim", Members: []string{"USELF", "UGDM"}},
		"DMB": {ID: "DMB", Name: "bot", Kind: "app", User: "UBOT"},
	}}
	b.syncView()

	msg := func(ch string, unread bool, mentions int, mention bool, kind string) core.Event {
		return core.Event{Type: "message", Channel: ch, Count: &core.Count{Unread: unread, Mentions: mentions, Latest: "1700000000.000200"},
			Msg: &core.Message{User: "UOTHER", Text: "hello"}, Mention: mention, ConvKind: kind}
	}
	user := func(id string, edit func(*core.User)) core.Event {
		u, _ := c.RawUser(id)
		prev := u
		edit(&u)
		return core.Event{Type: "user", UserID: id, User: &u, Prev: &prev}
	}
	for _, tc := range []struct {
		name string
		ev   core.Event
		want bool
	}{
		{"a message in a channel that is already unread", msg("C1", true, 0, false, "channel"), false},
		{"a message that makes a channel unread", msg("C2", true, 0, false, "channel"), true},
		{"a mention in an unread channel", msg("C1", true, 1, true, "channel"), true},
		{"a message in a conversation the model doesn't hold", msg("C9", true, 0, false, "channel"), false},
		{"the viewer's own message", core.Event{Type: "message", Channel: "C1", Count: &core.Count{Unread: true}, Msg: &core.Message{User: "USELF"}}, false},
		{"a DM partner's status text", user("UDM", func(u *core.User) { u.StatusText = "on a train" }), false},
		{"a DM partner's rename", user("UDM", func(u *core.User) { u.Name = "Ada Renamed" }), true},
		{"a DM partner's picture", user("UDM", func(u *core.User) { u.Avatar = "https://avatars.invalid/new.png" }), true},
		{"a DM partner's status emoji", user("UDM", func(u *core.User) { u.StatusEmojiURL = "https://emoji.invalid/train.png" }), true},
		{"a group DM member's status text", user("UGDM", func(u *core.User) { u.StatusText = "lunch" }), false},
		{"a group DM member's rename (the group's name lists them)", user("UGDM", func(u *core.User) { u.Name = "Grace H" }), true},
		{"a stranger's profile", core.Event{Type: "user", UserID: "U9", User: &core.User{ID: "U9", Name: "x"}}, false},
		{"the viewer's own profile", user("USELF", func(u *core.User) { u.StatusText = "focus" }), true},
		{"a DM partner who stays active", core.Event{Type: "presence", UserID: "UDM", Presence: "active"}, false},
		{"a DM partner who goes away", core.Event{Type: "presence", UserID: "UDM", Presence: "away"}, true},
		{"a stranger's presence", core.Event{Type: "presence", Users: []string{"U9", "U8"}, Presence: "away"}, false},
		{"the viewer going away", core.Event{Type: "presence", UserID: "USELF", Presence: "away"}, true},
		{"a channel the viewer isn't in, looked up", core.Event{Type: "conv", Channel: "C0ELSE", Conv: &core.Conv{ID: "C0ELSE", Preview: true}}, false},
		{"a sidebar conversation changed", core.Event{Type: "conv", Channel: "C2", Conv: &core.Conv{ID: "C2"}}, true},
		{"a mark that changes nothing", core.Event{Type: "marked", Channel: "C1", Count: &core.Count{Unread: true}}, false},
		{"a mark that reads a channel", core.Event{Type: "marked", Channel: "C1", Count: &core.Count{LastRead: "1700000000.000200"}}, true},
		{"badge_counts_updated (raw)", core.Event{Type: "badge_counts_updated"}, false},
		{"update_thread_state (raw)", core.Event{Type: "update_thread_state"}, false},
		{"thread_marked (raw)", core.Event{Type: "thread_marked"}, false},
		{"pref_change (raw; its prefs event follows)", core.Event{Type: "pref_change"}, false},
		{"another member's DND", core.Event{Type: "dnd_updated_user"}, false},
		{"a delete (the lists follow it)", core.Event{Type: "delete", Channel: "C1", TS: "1.1"}, false},
		{"an Activity item (the page follows it)", core.Event{Type: "activity"}, false},
		{"typing", core.Event{Type: "typing", Channel: "C1", UserID: "UDM"}, false},
		{"new custom emoji", core.Event{Type: "emoji_changed", Name: "partyparrot"}, false},
		{"a lookup of people the shell doesn't draw", core.Event{Type: "names", Users: []string{"U9", "B9"}}, false},
		{"a lookup of a DM partner", core.Event{Type: "names", Users: []string{"U9", "UDM"}}, true},
		{"a lookup of a sidebar conversation", core.Event{Type: "names", Users: []string{"C2"}}, true},
		{"the user groups' names (no ids)", core.Event{Type: "names"}, true},
		{"the connection", core.Event{Type: "connection", Connected: true}, true},
		{"a sidebar change", core.Event{Type: "sidebar_changed"}, true},
	} {
		if got := b.wants(tc.ev); got != tc.want {
			t.Errorf("%s: wants %v, want %v", tc.name, got, tc.want)
		}
	}

	// the view follows the model: once handle took the rename, its echo
	// draws nothing new
	ren := user("UDM", func(u *core.User) { u.Name = "Ada Renamed" })
	b.handle(&App{}, ren)
	if b.m.Convs["D1"].Name != "Ada Renamed" {
		t.Fatalf("DM row %q after the rename", b.m.Convs["D1"].Name)
	}
	if b.wants(ren) {
		t.Error("the rename's echo still posts")
	}
	// marking a channel read takes the newest message core holds
	c.S.NoteMessage("C1", &slack.Message{Type: "message", TS: "1700000000.000300", User: "UOTHER", Text: "newer"}, false)
	b.m.Convs["C1"].Latest = "1700000000.000100"
	b.ctx = context.Background()
	b.markRead("C1")
	if c1 := b.m.Convs["C1"]; c1.Unread || c1.LastRead != "1700000000.000300" {
		t.Fatalf("C1 after markRead: unread %v, last read %q, want core's newest 1700000000.000300", c1.Unread, c1.LastRead)
	}
	read := core.Event{Type: "marked", Channel: "C1", Count: &core.Count{LastRead: b.m.Convs["C1"].LastRead}}
	if b.wants(read) {
		t.Error("the mark's echo, which the row already shows, posts")
	}
}

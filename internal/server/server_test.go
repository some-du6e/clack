package server

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
)

// Synthetic fixtures only.

func testApp() *App {
	return NewApp(slack.New(slack.Creds{Token: "xoxc-test", UserID: "USELF", Team: "T1"}), store.New(""))
}

func TestSectionsView(t *testing.T) {
	a := testApp()
	s := a.S
	s.PutUsers([]slack.User{{ID: "U1", Name: "human"}, {ID: "UB", Name: "bot", IsBot: true}})
	for _, c := range []slack.Conversation{
		{ID: "C1", Name: "zeta", IsChannel: true}, {ID: "C4", Name: "alpha", IsChannel: true},
		{ID: "C2", Name: "starred", IsChannel: true}, {ID: "G3", Name: "proj", IsPrivate: true},
		{ID: "D1", IsIM: true, User: "U1"}, {ID: "D2", IsIM: true, User: "UB"},
	} {
		s.PutConv(c)
	}
	sec := func(id, typ, name, next string, chans ...string) slack.Section {
		x := slack.Section{ID: id, Type: typ, Name: name, Next: next}
		x.ChannelIDsPage.ChannelIDs = chans
		return x
	}
	// Deliberately out of order: display order comes from the linked list.
	s.Sections = []slack.Section{
		sec("L4", "direct_messages", "", ""),
		sec("L2", "standard", "Proj", "L3", "G3", "CGONE"),
		sec("L1", "stars", "", "L2", "C2"),
		sec("L3", "channels", "", "L4"),
		sec("L5", "slack_connect", "", ""), // empty special section: hidden
	}
	var convs []ConvView
	for _, c := range s.Convs {
		convs = append(convs, a.convView(c))
	}
	out := a.sectionsView(convs)
	var got []string
	for _, v := range out {
		got = append(got, v.Type+":"+v.Name+"="+strings.Join(v.Channels, ","))
	}
	want := "stars:Starred=C2|standard:Proj=G3|channels:Channels=C4,C1|direct_messages:Direct messages=D1|apps:Apps=D2"
	if strings.Join(got, "|") != want {
		t.Errorf("sections:\n got %s\nwant %s", strings.Join(got, "|"), want)
	}
}

func TestApplyReaction(t *testing.T) {
	m := &slack.Message{}
	applyReaction(m, "+1", "U1", true)
	applyReaction(m, "+1", "U1", true) // duplicate ignored
	applyReaction(m, "+1", "U2", true)
	if len(m.Reactions) != 1 || m.Reactions[0].Count != 2 {
		t.Fatalf("add: %+v", m.Reactions)
	}
	applyReaction(m, "+1", "U1", false)
	applyReaction(m, "+1", "U2", false)
	if len(m.Reactions) != 0 {
		t.Errorf("remove: %+v", m.Reactions)
	}
	applyReaction(m, "x", "U1", false) // removing a missing reaction is a no-op
}

func TestMentionsSelf(t *testing.T) {
	for text, want := range map[string]bool{
		"hi <@USELF>": true, "hi <@USELF|me>": true, "<!here> all": true, "<!channel>": true,
		"hi <@U2>": false, "USELF plain": false, "": false,
	} {
		if got := mentionsSelf(&slack.Message{Text: text}, "USELF"); got != want {
			t.Errorf("mentionsSelf(%q) = %v", text, got)
		}
	}
}

func TestHostAllowed(t *testing.T) {
	for host, want := range map[string]bool{
		"127.0.0.1:7777": true, "localhost:7777": true, "[::1]:7777": true, "localhost": true,
		"evil.example:7777": false, "evil.example": false, "127.0.0.1.evil.example": false,
	} {
		r := httptest.NewRequest("GET", "/api/boot", nil)
		r.Host = host
		if got := hostAllowed(r); got != want {
			t.Errorf("hostAllowed(%q) = %v", host, got)
		}
	}
}

func TestNeedsFileInfo(t *testing.T) {
	if needsFileInfo(&slack.Message{Files: []slack.File{{ID: "F1", URLPrivate: "https://files.slack.com/x"}}}) {
		t.Error("complete file flagged")
	}
	if !needsFileInfo(&slack.Message{Files: []slack.File{{ID: "F1", FileAccess: "check_file_info"}}}) {
		t.Error("stub not flagged")
	}
	if needsFileInfo(&slack.Message{Files: []slack.File{{ID: "F1", Mode: "tombstone"}}}) {
		t.Error("tombstone flagged")
	}
}

func TestPresenceIDs(t *testing.T) {
	a := testApp()
	a.S.PutConv(slack.Conversation{ID: "D1", IsIM: true, User: "U1"})
	ids := a.presenceIDs([]string{"U9", "B1", "C1", "U9"})
	joined := strings.Join(ids, ",")
	if !strings.Contains(joined, "USELF") || !strings.Contains(joined, "U1") || !strings.Contains(joined, "U9") || strings.Contains(joined, "B1") || strings.Contains(joined, "C1") {
		t.Errorf("presenceIDs = %v", ids)
	}
	if strings.Count(joined, "U9") != 1 {
		t.Errorf("duplicate ids: %v", ids)
	}
}

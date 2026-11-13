package core

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// Synthetic fixtures only: ids, names and timestamps are made up.

func tsAgo(d time.Duration) string {
	return strconv.FormatInt(time.Now().Add(-d).Unix(), 10) + ".000100"
}

func sectionsByType(a *App) map[string]string {
	got := map[string]string{}
	for _, s := range a.Snapshot().Sections {
		got[s.Type] = strings.Join(s.Channels, ",")
	}
	return got
}

// dmApp has five DMs; sidebar.dms says two are open.
func dmApp(t *testing.T) (*App, *fakeSlack) {
	a, f := testApp(t)
	a.S.PutUsers([]slack.User{{ID: "U3", Name: "carol"}, {ID: "U4", Name: "dave"}, {ID: "U5", Name: "erin"}})
	for _, c := range []slack.Conversation{
		{ID: "D2", IsIM: true, User: "U2"}, {ID: "D3", IsIM: true, User: "U3"},
		{ID: "D4", IsIM: true, User: "U4"}, {ID: "D5", IsIM: true, User: "U5"},
	} {
		a.S.PutConv(c)
	}
	a.S.SetCounts(&slack.Counts{IMs: []slack.CountEntry{{ID: "D1", Latest: tsAgo(time.Hour)}, {ID: "D3", Latest: tsAgo(2 * time.Hour)}}})
	f.reply["sidebar.dms"] = `{"ok":true,"ims":[
		{"id":"D1","user":"U1","is_open":true,"is_im":true},
		{"id":"D2","user":"U2","is_open":false,"is_im":true},
		{"id":"D3","user":"U3","is_open":true,"is_im":true},
		{"id":"D4","user":"U4","is_open":false,"is_im":true},
		{"id":"D5","user":"U5","is_open":false,"is_im":true}],"mpdms":[]}`
	list, err := a.C.SidebarDMs(context.Background())
	if err != nil || len(list) != 5 || !list[0].IsIM || list[1].IsOpen {
		t.Fatalf("SidebarDMs = %+v, %v", list, err)
	}
	a.setSidebarDMs(list)
	close(a.ready)
	return a, f
}

func TestSidebarShowsOnlyOpenDMs(t *testing.T) {
	a, _ := dmApp(t)
	if got := sectionsByType(a)["direct_messages"]; got != "D1,D3" { // Alice, carol
		t.Fatalf("direct messages = %q, want the open D1,D3", got)
	}
	// closed DMs are still known conversations (switcher, search, history)
	if c, ok := a.Conv("D2"); !ok || c.Kind != "im" {
		t.Errorf("closed DM D2 not resolvable: %+v %v", c, ok)
	}
	if n := len(a.Snapshot().Convs); n != 6 {
		t.Errorf("Snapshot.Convs = %d, want all 6 known conversations", n)
	}
}

func TestUnknownOpenStateShowsEveryDM(t *testing.T) {
	a, _ := testApp(t) // sidebar.dms never answered: Clack's old behaviour
	a.S.PutConv(slack.Conversation{ID: "D2", IsIM: true, User: "U2"})
	close(a.ready)
	if got := sectionsByType(a)["direct_messages"]; got != "D1,D2" {
		t.Errorf("direct messages = %q", got)
	}
}

func TestClosedDMReappearsOnNewMessage(t *testing.T) {
	a, _ := dmApp(t)
	evs, cancel := a.Subscribe()
	defer cancel()
	// realtime: a new message in closed D2 reopens it
	a.handle(rtm("message", map[string]any{"channel": "D2", "user": "U2", "ts": tsAgo(-time.Minute), "text": "hi"}))
	next(t, evs, "sidebar_changed")
	if got := sectionsByType(a)["direct_messages"]; got != "D2,D1,D3" { // newest first (dm_recency_sort_rollout)
		t.Fatalf("after a message: %q", got)
	}
	// polling: counts show a newer message in closed D4
	a.mu.Lock()
	mark := a.dms.closedAt["D4"]
	a.mu.Unlock()
	if mark == "" {
		t.Fatal("no reopen mark for D4")
	}
	if a.reopenActive() {
		t.Error("reopened without a new message")
	}
	a.S.SetCounts(&slack.Counts{IMs: []slack.CountEntry{{ID: "D4", Latest: tsAgo(-2 * time.Minute), HasUnreads: true}}})
	if !a.reopenActive() {
		t.Fatal("counts with a new D4 message did not reopen it")
	}
	if got := sectionsByType(a)["direct_messages"]; !strings.Contains(got, "D4") {
		t.Errorf("after polled counts: %q", got)
	}
	// an old message (before it was closed) does not reopen D5
	a.S.SetCounts(&slack.Counts{IMs: []slack.CountEntry{{ID: "D5", Latest: tsAgo(48 * time.Hour)}}})
	if a.reopenActive() || strings.Contains(sectionsByType(a)["direct_messages"], "D5") {
		t.Error("an old message reopened D5")
	}
}

func TestDMOpenCloseEvents(t *testing.T) {
	a, f := dmApp(t)
	f.reply["conversations.info"] = `{"ok":true,"channel":{"id":"D5","is_im":true,"user":"U5"}}`
	a.handle(rtm("im_open", map[string]any{"channel": "D5", "user": "U5"}))
	if got := sectionsByType(a)["direct_messages"]; got != "D1,D3,D5" {
		t.Fatalf("after im_open: %q", got)
	}
	a.handle(rtm("im_close", map[string]any{"channel": "D1", "user": "U1"}))
	if got := sectionsByType(a)["direct_messages"]; got != "D3,D5" {
		t.Fatalf("after im_close: %q", got)
	}
	if _, ok := a.Conv("D1"); !ok {
		t.Error("im_close forgot the conversation")
	}
}

// Slack keeps each user's section order as a linked list
// (next_channel_section_id); built-in sections sit wherever it puts them.
func TestSectionOrderFollowsLinkedList(t *testing.T) {
	a, _ := testApp(t)
	a.S.PutUsers([]slack.User{{ID: "UAPP", Name: "deploy", IsBot: true}})
	a.S.PutConv(slack.Conversation{ID: "DAPP", IsIM: true, User: "UAPP"})
	a.S.PutConv(slack.Conversation{ID: "C2", Name: "zebra", IsChannel: true})
	a.S.PutConv(slack.Conversation{ID: "C3", Name: "apple", IsChannel: true})
	a.S.PutConv(slack.Conversation{ID: "C4", Name: "mango", IsChannel: true})
	a.S.SetCounts(&slack.Counts{IMs: []slack.CountEntry{{ID: "DAPP", Latest: tsAgo(time.Hour)}}})
	a.S.CustomEmoji.Set(map[string]string{"orpheus": "https://e.example/orpheus.png"})
	sec := func(id, typ, name, emoji, next string, chans ...string) slack.Section {
		x := slack.Section{ID: id, Type: typ, Name: name, Emoji: emoji, Next: next}
		x.ChannelIDsPage.ChannelIDs = chans
		return x
	}
	// API order differs from the linked order on purpose:
	// Apps -> Projects -> Direct messages -> Channels -> Starred -> (empty agents)
	a.S.Sections = []slack.Section{
		sec("L5", "stars", "", "", "L6"),
		sec("L4", "channels", "Channels", "", "L5"),
		sec("L1", "recent_apps", "Recent Apps", "", "L2"),
		sec("L6", "agents", "Agents", "", ""),
		sec("L3", "direct_messages", "Direct Messages", "", "L4"),
		sec("L2", "standard", "Projects", ":orpheus:", "L3", "C2", "C3"),
	}
	close(a.ready)
	var order, chans []string
	for _, s := range a.Snapshot().Sections {
		order = append(order, s.Type)
		chans = append(chans, s.Name+"="+strings.Join(s.Channels, ","))
		if s.Type == "standard" && (s.Emoji != ":orpheus:" || s.EmojiURL != "https://e.example/orpheus.png") {
			t.Errorf("section emoji: %q %q", s.Emoji, s.EmojiURL)
		}
		if s.Type == "channels" && s.EmojiURL != "" {
			t.Errorf("built-in section got an emoji: %q", s.EmojiURL)
		}
	}
	// coding_channels: Slack's client-made Code channels section, spliced in first
	if got := strings.Join(order, ","); got != "coding_channels,recent_apps,standard,direct_messages,channels,stars" {
		t.Fatalf("section order = %s", got)
	}
	// alphabetical within each section; app DMs in the Apps slot
	if got := strings.Join(chans, " "); got != "Code channels= Apps=DAPP Projects=C3,C2 Direct messages=D1 Channels=C1,C4 Starred=" {
		t.Errorf("section contents = %s", got)
	}
}

func TestStandardSectionEmoji(t *testing.T) {
	a, _ := testApp(t)
	x := slack.Section{ID: "L1", Type: "standard", Name: "Fun", Emoji: "tada"}
	x.ChannelIDsPage.ChannelIDs = []string{"C1"}
	y := slack.Section{ID: "L2", Type: "standard", Name: "Plain"}
	x.Next = "L2"
	a.S.Sections = []slack.Section{x, y}
	close(a.ready)
	for _, s := range a.Snapshot().Sections {
		switch s.Name {
		case "Fun":
			if s.EmojiURL == "" {
				t.Error("standard emoji :tada: did not resolve")
			}
		case "Plain":
			if s.Emoji != "" || s.EmojiURL != "" {
				t.Errorf("section without emoji: %q %q", s.Emoji, s.EmojiURL)
			}
		}
	}
}

// TestAgentsSectionFollowsExperiment: client-boot Q drops Agents sections
// from the ordered list under agents_in_channels_improvements; what one
// listed goes back to its default section.
func TestAgentsSectionFollowsExperiment(t *testing.T) {
	a, _ := testApp(t)
	a.S.PutConv(slack.Conversation{ID: "C2", Name: "helper", IsChannel: true})
	ag := slack.Section{ID: "L2", Type: "agents", Name: "Agents"}
	ag.ChannelIDsPage.ChannelIDs = []string{"C2"}
	a.S.Sections = []slack.Section{{ID: "L1", Type: "channels", Name: "Channels", Next: "L2"}, ag}
	close(a.ready)
	layout := func() string {
		var out []string
		for _, s := range a.Snapshot().Sections {
			if s.Type == "channels" || s.Type == "agents" {
				out = append(out, s.Type+"="+strings.Join(s.Channels, ","))
			}
		}
		return strings.Join(out, " ")
	}
	a.setExperiments(map[string]string{"agents_in_channels_improvements": "off"})
	if got := layout(); got != "channels=C1 agents=C2" {
		t.Errorf("off: %s", got)
	}
	a.setExperiments(map[string]string{"agents_in_channels_improvements": "on"})
	if got := layout(); got != "channels=C1,C2" {
		t.Errorf("on: %s", got)
	}
}

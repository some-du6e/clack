package core

import (
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// The DM badges (rail.go), synthetic ids only. Karim, 2026-11-16: "i had
// two ghost pings in the dms tab/pill thingy ... it turns out it was a
// slack dm": the DMs tab counted the Slack system user's DM (USLACK,
// served is_bot false), which Slack's rHEx dmsBadgeCount skips as a bot
// DM (DUy0 G) and the DMs page never lists.

func TestDMBadgeSkips(t *testing.T) {
	for _, c := range []struct {
		name             string
		user             string
		member           *slack.User
		noBadge, noCount bool
	}{
		{"a person", "U1", &slack.User{ID: "U1"}, false, false},
		{"a person the store doesn't hold yet", "U1", nil, false, false},
		{"Slackbot's IM (s0AY D), which 6Z4k Sm still counts", "USLACKBOT", &slack.User{ID: "USLACKBOT"}, true, false},
		{"Slackbot's IM, member unknown", "USLACKBOT", nil, true, false},
		{"the Slack system user (DUy0 G's USLACK; o6sH _ for Sm)", "USLACK", &slack.User{ID: "USLACK", RealName: "Slack"}, true, true},
		{"the Slack system user, member unknown (Gx04's stub carries the id, o6sH _)", "USLACK", nil, true, true},
		{"an app's bot user (is_bot)", "UB", &slack.User{ID: "UB", IsBot: true}, true, true},
		{"an app's bot user the store doesn't hold yet", "UB", nil, false, false},
		{"is_slackbot (gmjA U)", "UX", &slack.User{ID: "UX", IsSlackbot: true}, true, true},
		{"is_app_user alone isn't a bot (nMbN po)", "UA", &slack.User{ID: "UA", IsAppUser: true}, false, false},
		{"the search user (Opi6 df)", "USEARCH", &slack.User{ID: "USEARCH"}, true, false},
		{"the security bot (Opi6 Cx; w7aZ C)", "USLACKSECURITY", &slack.User{ID: "USLACKSECURITY"}, true, true},
		{"the security bot, member unknown (w7aZ C on the stub's id)", "USLACKSECURITY", nil, true, true},
	} {
		noBadge, noCount := dmBadgeSkips(c.user, c.member)
		if noBadge != c.noBadge || noCount != c.noCount {
			t.Errorf("%s: noBadge %v noCount %v, want %v %v", c.name, noBadge, noCount, c.noBadge, c.noCount)
		}
	}
}

func TestDMsBadgeRule(t *testing.T) {
	inbox := RailState{ActivityInbox: true, DMsInRail: true}
	if n := inbox.DMsBadge(3, 2); n != 2 {
		t.Errorf("inbox, DMs tab in the rail: %d, want the 2 unread DMs (6Z4k Sm)", n)
	}
	// only DMs rHEx leaves out are unread: no badge, whatever Sm counts
	// (Slackbot's IM)
	if n := inbox.DMsBadge(0, 1); n != 0 {
		t.Errorf("no highlights: %d, want none", n)
	}
	underMore := RailState{ActivityInbox: true}
	if n := underMore.DMsBadge(3, 2); n != 3 {
		t.Errorf("inbox, DMs tab under More: %d, want the 3 highlights", n)
	}
	classic := RailState{DMsInRail: true}
	if n := classic.DMsBadge(3, 2); n != 3 {
		t.Errorf("no inbox: %d, want the 3 highlights (rHEx dmsBadgeCount)", n)
	}
	if n := classic.DMsBadge(0, 2); n != 0 {
		t.Errorf("no inbox, no highlights: %d", n)
	}
}

// The snapshot's IMs carry the skips from the members the store holds.
func TestSnapshotDMBadgeSkips(t *testing.T) {
	a, _ := testApp(t)
	a.S.PutUsers([]slack.User{{ID: "USLACK", Name: "slack", RealName: "Slack"}, {ID: "USLACKBOT", Name: "slackbot"}})
	for _, c := range []slack.Conversation{
		{ID: "DSLACK", IsIM: true, User: "USLACK"},
		{ID: "DSLACKBOT", IsIM: true, User: "USLACKBOT"},
		{ID: "DBOT", IsIM: true, User: "UB"},
		{ID: "GGROUP", IsMPIM: true, Members: []string{"USELF", "U1", "U2"}},
	} {
		a.S.PutConv(c)
	}
	close(a.ready)
	want := map[string][2]bool{"D1": {false, false}, "DSLACK": {true, true}, "DSLACKBOT": {true, false},
		"DBOT": {true, true}, "GGROUP": {false, false}}
	seen := 0
	for _, c := range a.Snapshot().Convs {
		w, ok := want[c.ID]
		if !ok {
			continue
		}
		seen++
		if c.NoDMsBadge != w[0] || c.NoDMsCount != w[1] {
			t.Errorf("%s: NoDMsBadge %v NoDMsCount %v, want %v %v", c.ID, c.NoDMsBadge, c.NoDMsCount, w[0], w[1])
		}
	}
	if seen != len(want) {
		t.Errorf("the snapshot has %d of the %d conversations", seen, len(want))
	}
}

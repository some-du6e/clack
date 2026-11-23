package store

import (
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// Synthetic fixtures only.

// What a contact card reads about its member: their name and the
// viewer's team for a member of it, the loading bar's team for an
// external one; canInteract from is_stranger; canUserShareContactById's
// rules (not the viewer, an active member, a viewer who isn't a guest,
// and the member's who_can_share_contact_card); unknown and non-existent
// members not at all.
func TestContactMember(t *testing.T) {
	s := New("")
	s.Self = slack.User{ID: "U0SELF", TeamID: "T1"}
	s.TeamID, s.TeamName = "T1", "Fake Club"
	s.PutUsers([]slack.User{
		{ID: "U0SELF", TeamID: "T1", Name: "me"},
		{ID: "U0MEMBER", TeamID: "T1", Name: "robin", RealName: "Robin Example"},
		{ID: "U0XEVERY", TeamID: "T2", Name: "sam", IsStranger: true, WhoCanShareContactCard: "EVERYONE"},
		{ID: "U0XTEAM", TeamID: "T2", Name: "kim", WhoCanShareContactCard: "TEAM_MEMBERS"},
		{ID: "U0NOONE", TeamID: "T1", Name: "lee", WhoCanShareContactCard: "NO_ONE"},
		{ID: "U0BOT", TeamID: "T1", Name: "bot", IsBot: true},
		{ID: "U0GUEST", TeamID: "T1", Name: "guest", IsRestricted: true},
		{ID: "U0PROF", TeamID: "T1", Name: "prof", IsProfileOnlyUser: true},
		{ID: "U0GONE", TeamID: "T1", Name: "gone", Deleted: true},
	})
	s.PutNonexistent([]string{"U0NOSUCH"})
	for _, c := range []struct {
		id                           string
		team                         string
		teamUnknown, interact, share bool
		profileOnly, deleted         bool
	}{
		{"U0SELF", "Fake Club", false, true, false, false, false},
		{"U0MEMBER", "Fake Club", false, true, true, false, false},
		{"U0XEVERY", "", true, false, true, false, false},
		{"U0XTEAM", "", true, true, false, false, false},
		{"U0NOONE", "Fake Club", false, true, false, false, false},
		{"U0BOT", "Fake Club", false, true, false, false, false},
		{"U0GUEST", "Fake Club", false, true, false, false, false},
		{"U0PROF", "Fake Club", false, true, true, true, false},
		{"U0GONE", "Fake Club", false, true, false, false, true},
	} {
		m, ok := s.ContactMember(c.id)
		if !ok {
			t.Errorf("%s: unknown", c.id)
			continue
		}
		if m.Team != c.team || m.TeamUnknown != c.teamUnknown || m.CanInteract != c.interact || m.CanShare != c.share || m.ProfileOnly != c.profileOnly || m.Deleted != c.deleted {
			t.Errorf("%s: %+v, want team %q (unknown %v), interact %v, share %v, profile only %v, deleted %v", c.id, m, c.team, c.teamUnknown, c.interact, c.share, c.profileOnly, c.deleted)
		}
	}
	if m, _ := s.ContactMember("U0MEMBER"); m.Name == "" || m.Avatar == "" {
		t.Errorf("the member's name %q and avatar %q", m.Name, m.Avatar)
	}
	// a guest viewer shares nobody's contact
	s.Self.IsRestricted = true
	if m, _ := s.ContactMember("U0MEMBER"); m.CanShare {
		t.Errorf("a guest viewer may share contacts")
	}
	for _, id := range []string{"U0NOSUCH", "U0NEVERSEEN"} {
		if _, ok := s.ContactMember(id); ok {
			t.Errorf("%s: known", id)
		}
	}
}

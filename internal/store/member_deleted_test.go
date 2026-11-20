package store

import (
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// TestMemberDeletedStates: MemberDeleted (richtext's MemberStater, a
// share_shortcut card's bot) tells the three states Slack's
// getMemberById gives a card apart: a member loaded (deleted or not), one
// users.info didn't find (the non-existent member: known, not deleted),
// and one still being fetched (unknown).
func TestMemberDeletedStates(t *testing.T) {
	s := New("")
	s.PutUsers([]slack.User{{ID: "U0BOT00001", Name: "bot"}, {ID: "U0BOTDELE1", Name: "gone", Deleted: true}})
	s.PutNonexistent([]string{"U0GONE0001"})
	for _, c := range []struct {
		id             string
		deleted, known bool
	}{
		{"U0BOT00001", false, true},
		{"U0BOTDELE1", true, true},
		{"U0GONE0001", false, true},
		{"U0BOT00009", false, false},
	} {
		if d, k := s.MemberDeleted(c.id); d != c.deleted || k != c.known {
			t.Errorf("%s: deleted %v known %v, want %v %v", c.id, d, k, c.deleted, c.known)
		}
	}
}

package store

// A refetch's answer is stored only if the store doesn't hold a later
// copy, decided under the store's lock (PutUserNotOlder): a user_change
// stored while the answer was out stays. Synthetic member.

import (
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

func TestPutUserNotOlder(t *testing.T) {
	s := New(t.TempDir())
	s.PutUsers([]slack.User{{ID: "U0NEWER001", Name: "live", Updated: 200}})
	if s.PutUserNotOlder(slack.User{ID: "U0NEWER001", Name: "older answer", Updated: 100}) {
		t.Error("an answer older than the held copy was stored")
	}
	if u, _ := s.RawUser("U0NEWER001"); u.Name != "live" {
		t.Errorf("the store holds %q, want the later copy", u.Name)
	}
	if !s.PutUserNotOlder(slack.User{ID: "U0NEWER001", Name: "same stamp", Updated: 200}) {
		t.Error("an answer as new as the held copy wasn't stored")
	}
	if !s.PutUserNotOlder(slack.User{ID: "U0NEWER002", Name: "unheld", Updated: 1}) {
		t.Error("a member the store doesn't hold wasn't stored")
	}
}

package store

// AvatarAt's table of a member's larger pictures holds the members the
// store holds, as they are now: a member's previous picture and a member
// the members pass let go leave it (it grew by ~350 B for every member a
// session ever saw). Synthetic ids and URLs.

import (
	"fmt"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

func altPic(id string, v, px int) string {
	return fmt.Sprintf("https://avatars.invalid/%s-v%d-%d.png", id, v, px)
}

func altMember(id string, v int) slack.User {
	return slack.User{ID: id, Name: "m" + id, Profile: slack.Profile{
		Image72: altPic(id, v, 72), Image192: altPic(id, v, 192), Image512: altPic(id, v, 512)}}
}

func TestAvatarAltsFollowTheStore(t *testing.T) {
	s := New(t.TempDir())
	var us []slack.User
	for i := range 40 {
		us = append(us, altMember(fmt.Sprintf("U0ALT%05d", i), 1))
	}
	s.PutUsers(us)
	a, b := us[0].ID, us[1].ID
	if got := s.AvatarAt(altPic(a, 1, 72), 144); got != altPic(a, 1, 192) {
		t.Fatalf("a stored member's 72px picture at 144 px draws %q, want their 192px one: the case tests nothing", got)
	}

	// a member's new picture: the old one's sizes go, the new one's come
	s.PutUsers([]slack.User{altMember(b, 2)})
	if got := s.AvatarAt(altPic(b, 1, 72), 144); got != altPic(b, 1, 72) {
		t.Errorf("the picture a member had before still maps to %q", got)
	}
	if got := s.AvatarAt(altPic(b, 2, 72), 300); got != altPic(b, 2, 512) {
		t.Errorf("the member's new picture at 300 px draws %q, want %q", got, altPic(b, 2, 512))
	}

	// the members pass lets them go: so do their pictures' sizes
	s.SetMembersSizeForDebug(1, 1)
	gone := s.EvictMembers()
	s.SetMembersSizeForDebug(-1, -1)
	if len(gone) < 30 {
		t.Fatalf("the pass let %d members go: the case tests nothing", len(gone))
	}
	for _, id := range gone {
		v := 1
		if id == b {
			v = 2
		}
		if got := s.AvatarAt(altPic(id, v, 72), 144); got != altPic(id, v, 72) {
			t.Errorf("member %s, let go, still maps their picture to %q", id, got)
			break
		}
	}
	s.avatarMu.RLock()
	n := len(s.avatarAlts)
	s.avatarMu.RUnlock()
	if held, _ := s.MembersStats(); n > held {
		t.Errorf("avatarAlts holds %d pictures for the %d members the store holds", n, held)
	}
}

package messages

import (
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
)

// A change only a profile card shows (a status text, a title) redraws no
// message list, even for a member a list drew in its latest frame: the
// sidebar reads its DM people every frame, so every DM partner counts as
// drawn, and a busy workspace's status changes redrew every list. A
// change a row shows (a name) still does, and so does one about a member
// the store didn't hold before.
func TestHubSkipsCardOnlyProfileChanges(t *testing.T) {
	hb := &hub{chans: map[string]*model{}, thread: map[string]*model{}}
	hb.frame = time.Now().Add(-time.Second) // a list drew a second ago
	prev := core.User{ID: "U0FAKE00001", Name: "ada", Avatar: "https://avatars.invalid/ada-72.png"}
	seen := time.Now() // read since: drawn
	u := prev
	u.StatusText, u.Title, u.TZ = "on a train", "Synthetic engineer", "Europe/Lisbon"
	if hb.apply(core.Event{Type: "user", UserID: prev.ID, User: &u, Prev: &prev, Seen: seen}) {
		t.Error("a status text change redrew the lists")
	}
	renamed := prev
	renamed.Name = "ada lovelace"
	if !hb.apply(core.Event{Type: "user", UserID: prev.ID, User: &renamed, Prev: &prev, Seen: seen}) {
		t.Error("a rename of a member a list drew didn't redraw it")
	}
	emoji := prev
	emoji.StatusEmoji, emoji.StatusEmojiURL = ":train:", "https://emoji.invalid/train.png"
	if !hb.apply(core.Event{Type: "user", UserID: prev.ID, User: &emoji, Prev: &prev, Seen: seen}) {
		t.Error("a status emoji change (rows draw it after the name) didn't redraw")
	}
	if !hb.apply(core.Event{Type: "user", UserID: prev.ID, User: &u, Seen: seen}) {
		t.Error("a member the store didn't hold before (no Prev) didn't redraw")
	}
}

// A lookup ("names": the member fetcher's answer, for a sender in any
// channel) redraws the lists only when one on screen shows one of its
// people: a sender, the reply bar's people, a member its text names.
// Without ids (the user groups' names) every list redraws.
func TestHubNamesRedrawOnlyWhatIsOnScreen(t *testing.T) {
	m := &model{ch: "C1", msgs: []entry{{m: core.Message{TS: "1700000000.000100", User: "U0FAKE00001", ReplyUsers: []string{"U0FAKE00002"}}}}}
	gone := &model{ch: "C2", msgs: []entry{{m: core.Message{TS: "1700000000.000200", User: "U0FAKE00003"}}}}
	hb := &hub{chans: map[string]*model{"C1": m, "C2": gone}, thread: map[string]*model{}}
	hb.markDrawn(gone, time.Now().Add(-time.Minute)) // a list left a minute ago
	hb.markDrawn(m, time.Now())
	for _, tc := range []struct {
		ids  []string
		want bool
	}{
		{[]string{"U0FAKE00999"}, false},
		{[]string{"U0FAKE00003"}, false}, // only a list off screen shows them
		{[]string{"U0FAKE00001"}, true},
		{[]string{"U0FAKE00002"}, true},
		{nil, true},
	} {
		if got := hb.apply(core.Event{Type: "names", Users: tc.ids}); got != tc.want {
			t.Errorf("names %v: redraws %v, want %v", tc.ids, got, tc.want)
		}
	}
}

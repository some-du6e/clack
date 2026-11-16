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

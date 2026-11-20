package views

// A message fetched by id that the store held no copy of (an Activity
// entry the socket pushes, in a conversation whose messages the plugins
// read already) draws its plugins' view a JS round trip after it lands, as
// Slack's patched selector reads what fetchObjectWithId put in its one
// store in the same render. No change announces a message new to the
// store (a new revision of one it held goes out as an "update"), so
// nothing marked it for the plugins' mirror, and its row drew core's copy
// (a word Censorship masks, a relay bot's name) until the mirror's next
// sweep, up to 750 ms (the reviews). The sweep is an hour here: only the
// mark brings the view. Synthetic data only.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/taut"
)

func TestActivityFetchedViewWithoutTheSweep(t *testing.T) {
	defer taut.SetSweepEvery(time.Hour)()
	e := newLmEnv(t, lmOpts{texts: true})
	e.e.until(t, "the plugin's patch on the fetched vote", func() bool { return strings.Contains(e.drawn(lmVote), "Ups: 1") })

	// a new entry: its message, in the channel the plugins read, is held
	// nowhere until Activity fetches it by id
	e.f.mu.Lock()
	e.f.mention2 = "<@" + fakeslack.SelfID + "> Upvotes for the synthetic retro, please"
	e.f.mu.Unlock()
	if _, held := e.app.S.FindHeld(lmChan, lmMention2); held {
		t.Fatal("the store holds the new mention already: the test checks nothing")
	}
	raw, _ := json.Marshal(map[string]any{"type": "activity", "subtype": "activity_updated", "key": lmMention2Key,
		"entry": map[string]any{"key": lmMention2Key, "feed_ts": lmMention2, "is_unread": true, "item": map[string]any{
			"type": "at_user", "message": map[string]any{"ts": lmMention2, "channel": lmChan, "author_user_id": "U0FAKE00002"}}}})
	e.app.Inject(slack.Event{Type: "activity", Subtype: "activity_updated", Raw: raw})
	e.e.until(t, "the new entry drawn", func() bool { return e.drawn(lmMention2) != "" })
	e.e.until(t, "the plugin's patch on the new entry, with no sweep", func() bool {
		return strings.Contains(e.drawn(lmMention2), "Ups for the synthetic retro")
	})
}

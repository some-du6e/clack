package core

// The events Activity orders against its entries (activity_later.go): the
// badges' counts, zero or not, and a clear-all's undo toast come out after
// an entry the socket sent before them, however long that entry's lookups
// take, and in socket order; with no entry pending they go out at once,
// before handle returns. The busy paths were reached only by scheduling
// luck (α.110's review). Synthetic data only.

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

func TestActivityOrderedEventsWaitForPendingEntry(t *testing.T) {
	a, fs := actFake(t)
	a.SetExperimentGroup("activity_inbox", "on") // the badges are activity_v2's
	t.Cleanup(func() { a.ClearExperimentGroup("activity_inbox") })
	evs, stop := a.Subscribe()
	defer stop()
	// the entry's users/info is held until released
	release := make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	t.Cleanup(free)
	fs.SetEdgeHook(func(method string, _ map[string]json.RawMessage) (any, bool) {
		if method == "edge/users/info" {
			<-release
		}
		return nil, false
	})
	t.Cleanup(func() { fs.SetEdgeHook(nil) })

	js := func(v map[string]any) json.RawMessage {
		b, _ := json.Marshal(v)
		return b
	}
	entry := js(map[string]any{"type": "activity", "subtype": "activity_updated", "entry": map[string]any{
		"key": "fa-held", "feed_ts": "1762702100.000100", "is_unread": true,
		"item": map[string]any{"type": "at_user", "message": map[string]any{"ts": "1762700100.000100",
			"channel": fakeslack.ViewsChannel, "author_user_id": "U0HELDAUTH1"}}}})
	badges := func(n int) slack.Event {
		return slack.Event{Type: "badge_counts_updated", Raw: js(map[string]any{"type": "badge_counts_updated",
			"activity_v2": map[string]int{"at_user": n, "thread_v2": 0}})}
	}
	undo := func(key string) slack.Event {
		return slack.Event{Type: "activity_clear_all_completed", Raw: js(map[string]any{"type": "activity_clear_all_completed", "undo_key": key})}
	}
	label := func(ev Event) string {
		switch {
		case ev.Type == "activity" && ev.Name == "activity_clear_all_completed":
			return "undo " + ev.ID
		case ev.Type == "activity" && ev.ID == "fa-held":
			return "entry"
		case ev.Type == "activity_badges" && ev.Name == ActivityBadgesZero:
			return "badges zero"
		case ev.Type == "activity_badges":
			return "badges"
		}
		return ""
	}
	next := func(d time.Duration) (string, bool) {
		dl := time.After(d)
		for {
			select {
			case ev := <-evs:
				if l := label(ev); l != "" {
					return l, true
				}
			case <-dl:
				return "", false
			}
		}
	}

	a.handle(slack.Event{Type: "activity", Subtype: "activity_updated", Raw: entry})
	a.handle(badges(2))
	a.handle(badges(0))
	a.handle(undo("uk-held"))
	if l, ok := next(500 * time.Millisecond); ok {
		if l == "entry" {
			t.Fatal("the entry came out with its users/info held: the test holds nothing")
		}
		t.Fatalf("%q came out while the entry before it waited for its lookups", l)
	}
	free()
	for _, want := range []string{"entry", "badges", "badges zero", "undo uk-held"} {
		l, ok := next(10 * time.Second)
		if !ok {
			t.Fatalf("%q never came out", want)
		}
		if l != want {
			t.Fatalf("%q came out where %q should: after the entry, in socket order", l, want)
		}
	}

	// the queue idle: each goes out before handle returns
	for dl := time.Now().Add(10 * time.Second); !a.ActivitySettled(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(dl) {
			t.Fatal("the activity queue never settled")
		}
	}
	now := func() string {
		for {
			select {
			case ev := <-evs:
				if l := label(ev); l != "" {
					return l
				}
			default:
				return ""
			}
		}
	}
	a.handle(badges(0))
	if l := now(); l != "badges zero" {
		t.Errorf("with no entry pending the badges' zero didn't go out at once (got %q)", l)
	}
	a.handle(undo("uk-idle"))
	if l := now(); l != "undo uk-idle" {
		t.Errorf("with no entry pending the clear-all's undo didn't go out at once (got %q)", l)
	}
}

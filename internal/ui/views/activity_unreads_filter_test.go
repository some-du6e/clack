package views

// Karim, α.85: Activity's Unreads filter listed two of his own thread
// replies, both read. Slack's ActivityList selector Q (3x6A, build
// c07ab895506f) keeps, with filterUnreadsSelected, only the unread rows
// and the opened one; a refresh keeps only getItemsRetainedOnRefresh's
// rows. Synthetic feed (internal/core/testdata), no network.

import (
	"encoding/json"
	"image"
	"slices"
	"testing"
	"time"

	"gioui.org/op"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// actShownKeys is the rows the list draws, in order.
func actShownKeys(v *Activity) []string {
	p := actPage(v)
	var keys []string
	for _, i := range v.vis {
		if i >= 0 && i < len(p.Items) {
			keys = append(keys, actKey(&p.Items[i]))
		}
	}
	return keys
}

// The feed answers its read rows too (the fake ignores unread_only, as
// a stale page would hold rows read since): under Unreads the list
// draws only the unread ones; a row read here leaves at once, the opened
// one stays until it's closed, a realtime read takes its row out, and
// the All list keeps every row.
func TestActivityUnreadsShowsOnlyUnread(t *testing.T) {
	app := actFeedApp(t)
	v := NewActivity(&slickHost{app: app, th: theme.New(false)})
	v.unread.Value = true
	var ops op.Ops
	sz := image.Pt(1200, 800)
	frame := func() { v.Layout(actFrame(&ops, sz)) }
	until := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for {
			frame()
			if cond() {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s (rows %v)", what, actShownKeys(v))
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	until("the feed", func() bool {
		_, loaded, _ := v.data.get()
		return loaded && !v.data.busy() && len(actPage(v).Items) >= actFeedLen
	})
	frame()
	shown := func(key string) bool { return slices.Contains(actShownKeys(v), key) }

	// your read thread reply, your read mention and a read channel post
	for _, k := range []string{"k-thread-many", "k-mine", "k-channel-gone"} {
		if actItem(t, v, k).Unread {
			t.Fatalf("fixture: %s should be read", k)
		}
		if shown(k) {
			t.Errorf("Unreads draws the read row %s", k)
		}
	}
	for _, k := range actShownKeys(v) {
		if !v.isUnread(actItem(t, v, k), k) {
			t.Errorf("Unreads draws the read row %s", k)
		}
	}
	for _, k := range []string{"k-thread", "k-at-user", "k-keyword"} {
		if !shown(k) {
			t.Fatalf("Unreads hides the unread row %s (rows %v)", k, actShownKeys(v))
		}
	}

	// Mark as read: the row leaves Unreads at once
	v.markRead(actItem(t, v, "k-thread"), "k-thread")
	until("k-thread to leave Unreads", func() bool { return !shown("k-thread") })

	// opening a row reads it; it stays while it's open
	r := v.rowFor("k-at-user")
	v.choose(actItem(t, v, "k-at-user"), "k-at-user", r)
	frame()
	if v.isUnread(actItem(t, v, "k-at-user"), "k-at-user") {
		t.Fatal("opening k-at-user didn't read it")
	}
	if !shown("k-at-user") {
		t.Error("the opened row left Unreads while open")
	}
	v.selOK = false // closed
	until("the closed read row to leave Unreads", func() bool { return !shown("k-at-user") })

	// a realtime activity_updated that reads a row (read elsewhere)
	raw, _ := json.Marshal(map[string]any{"type": "activity", "subtype": "activity_updated", "key": "k-keyword",
		"entry": map[string]any{"key": "k-keyword", "feed_ts": "1700000109.000100", "is_unread": false,
			"item": map[string]any{"type": "keyword", "message": map[string]any{"ts": "1700000109.000100", "channel": "C1", "author_user_id": "U2"}}}})
	app.Inject(slack.Event{Type: "activity", Subtype: "activity_updated", Raw: raw})
	until("the row read elsewhere to leave Unreads", func() bool { return !shown("k-keyword") })

	// All: every row, read ones included
	v.unread.Value = false
	v.dirty = true
	until("the All list", func() bool {
		_, loaded, _ := v.data.get()
		return loaded && !v.data.busy() && len(actPage(v).Items) >= actFeedLen &&
			shown("k-thread-many") && shown("k-mine") && shown("k-thread") && shown("k-at-user") && shown("k-keyword")
	})
}

// The Unreads refresh (P0K2 refreshItems, getItemsRetainedOnRefresh):
// the fresh page, plus the old rows newer than its newest row or marked
// unread here; old rows read since (or just loaded before) go.
func TestActivityUnreadsRefreshRetains(t *testing.T) {
	it := func(key, ts string, unread bool) core.ActivityItem {
		return core.ActivityItem{Key: key, Type: "at_user", FeedTS: ts, Unread: unread}
	}
	old := core.ActivityPage{Next: "old-cursor", Items: []core.ActivityItem{
		it("newer", "1700000009.000100", true),
		it("kept-unread", "1700000005.000100", true),
		it("read-since", "1700000004.000100", false),
		it("marked-unread", "1700000003.000100", false),
		it("older-unread", "1700000002.000100", true),
	}}
	fresh := core.ActivityPage{Next: "fresh-cursor", Items: []core.ActivityItem{
		it("new-unread", "1700000006.000100", true),
		it("kept-unread", "1700000005.000100", true),
	}}
	got := actRetained(fresh, old, map[string]bool{"marked-unread": true})
	var keys []string
	for i := range got.Items {
		keys = append(keys, got.Items[i].Key)
	}
	want := []string{"newer", "new-unread", "kept-unread", "marked-unread"}
	if !slices.Equal(keys, want) {
		t.Errorf("refreshed Unreads %v, want %v", keys, want)
	}
	if got.Next != "fresh-cursor" {
		t.Errorf("cursor %q, want the fresh page's", got.Next)
	}
	// an empty page keeps only the rows marked unread here
	got = actRetained(core.ActivityPage{}, old, map[string]bool{"marked-unread": true})
	if len(got.Items) != 1 || got.Items[0].Key != "marked-unread" {
		t.Errorf("empty refresh kept %v", got.Items)
	}
}

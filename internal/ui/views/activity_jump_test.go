package views

import (
	"image"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// Opening an Activity item lands on its message, as GMkG
// handleOnClickActivityItem does (Karim, α.89: "the thing to hop to the
// desired message when clicking a notif doesn't work"): with
// activity_inbox_remove_persistent_detail_panel beside the inbox at the
// message (toMessage: startTs and highlightTs) or in its thread at the
// reply (toThread's replyTs), the conversation itself for a channel item;
// without it the view's own pane jumps there (jumpToMessage in the
// Primary). Synthetic feed (core/testdata/activity_feed.json).

// jumpHost is slickHost with the ui root's secondary column, recording
// what each item opened.
type jumpHost struct {
	*slickHost
	got              string
	secCh, secThread string
}

func (h *jumpHost) OpenChannelSecondary(ch string) {
	h.got, h.secCh, h.secThread = "channel "+ch, ch, ""
}
func (h *jumpHost) SecondaryShows(ch, ts string) bool {
	return ch != "" && ch == h.secCh && ts == h.secThread
}
func (h *jumpHost) OpenDetailSecondary(key string, _ layout.Widget) { h.got = "detail " + key }
func (h *jumpHost) SecondaryShowsDetail(string) bool                { return false }
func (h *jumpHost) OpenMessageSecondary(ch, ts string) {
	h.got, h.secCh, h.secThread = "message "+ch+" "+ts, ch, ""
}
func (h *jumpHost) OpenThreadAt(ch, thread, ts string) {
	h.got, h.secCh, h.secThread = "thread "+ch+" "+thread+" "+ts, ch, thread
}
func (h *jumpHost) OpenThread(ch, thread string) {
	h.got, h.secCh, h.secThread = "thread "+ch+" "+thread, ch, thread
}
func (h *jumpHost) SecondaryAt() (string, string) { return "", "" }
func (h *jumpHost) JumpPane(ch, thread, ts string) {
	h.got = "pane " + ch + " " + thread + " " + ts
}

func TestActivityItemOpensAtItsMessage(t *testing.T) {
	app := actFeedApp(t)
	for _, exp := range []string{"activity_inbox", "nested_secondary_view", "activity_inbox_remove_persistent_detail_panel"} {
		app.SetExperimentGroup(exp, "on")
	}
	h := &jumpHost{slickHost: &slickHost{app: app, th: theme.New(false)}}
	v := NewActivity(h)
	var ops op.Ops
	sz := image.Pt(1200, 800)
	deadline := time.Now().Add(10 * time.Second)
	for len(actPage(v).Items) < actFeedLen {
		if time.Now().After(deadline) {
			t.Fatalf("feed never loaded (%d items)", len(actPage(v).Items))
		}
		v.Layout(actFrame(&ops, sz))
		time.Sleep(10 * time.Millisecond)
	}
	open := func(key string) string {
		t.Helper()
		h.got = ""
		v.selOK = false
		v.choose(actItem(t, v, key), key, v.rowFor(key))
		v.Layout(actFrame(&ops, sz))
		return h.got
	}

	// beside the inbox
	for _, c := range []struct{ key, want string }{
		{"k-at-user", "message C1 1700000105.000100"},                      // a mention: R9 at the message
		{"k-keyword", "message C1 1700000109.000100"},                      // a keyword match
		{"k-dm", "message D1 1700000104.000100"},                           // an unread DM: its latest message
		{"k-mine", "message C1 1700000142.000100"},                         // your own, read: still at it (not a DM)
		{"k-thread", "thread C1 1700000110.000100 1700000111.000100"},      // unread thread_v2: no min_unread_ts, its reply
		{"k-thread-many", "thread C1 1700000134.000100 1700000136.000100"}, // read thread_v2 under the inbox: feedTs
		{"k-channel", "channel C1"},                                        // a channel item: toChannel
	} {
		if got := open(c.key); got != c.want {
			t.Errorf("%s beside the inbox opened %q, want %q", c.key, got, c.want)
		}
	}

	// the persistent detail panel: the view's own pane, jumped
	app.SetExperimentGroup("activity_inbox_remove_persistent_detail_panel", "off")
	v.Layout(actFrame(&ops, sz))
	for _, c := range []struct{ key, want string }{
		{"k-at-user", "pane C1  1700000105.000100"},
		{"k-thread", "pane C1 1700000110.000100 1700000111.000100"},
		{"k-channel", ""}, // the conversation as it is
	} {
		if got := open(c.key); got != c.want {
			t.Errorf("%s in the detail panel: %q, want %q", c.key, got, c.want)
		}
		if !v.paneInView() {
			t.Errorf("%s: the item isn't in the view's own pane", c.key)
		}
	}
}

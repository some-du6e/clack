package views

// An activity entry whose message is deleted, or edited to drop the
// viewer's mention, while the entry waits behind another's lookups never
// shows (α.110's review; Karim's 2026-11-17 ghost row: "a deleted reply's
// thread row stayed"). Core runs an entry's lookups (its messages, its
// people) off the event pump, one entry after another; Activity drops an
// entry whose message changed after the entry's stamp (landMsgs: RemoveSeq
// > MsgSeq), so the stamp must be the socket's order, not the moment the
// entry's turn came. Here users/info answers after 1.5 s, a first entry
// names someone the store doesn't hold, the second is about a message
// whose removal comes right after it, and a third about a message nobody
// touched shows the queue drained. Synthetic data only.

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

func TestActivityEntryLeavesWhenItsMessageGoesWhileItWaits(t *testing.T) {
	for _, how := range []string{"deleted", "mention dropped"} {
		t.Run(how, func(t *testing.T) { activityEntryWaits(t, how) })
	}
}

func activityEntryWaits(t *testing.T, how string) {
	v, h, e, fs := vaFakeActivity(t, false)
	app := h.Core()
	ch := fakeslack.ViewsChannel
	const stranger = "U0WAITNEW01" // nobody the store holds: the first entry's users/info
	const author = "U0FAKE00002"
	mention := "<@" + fakeslack.SelfID + "> "
	first, _ := fs.PostQuiet(ch, stranger, mention+"a synthetic question first")
	gone, _ := fs.PostQuiet(ch, author, mention+"a synthetic question that goes")
	kept, _ := fs.PostQuiet(ch, author, mention+"a synthetic question that stays")

	// users/info answers after 1.5 s, as on a busy Grid
	fs.SetEdgeHook(func(method string, _ map[string]json.RawMessage) (any, bool) {
		if method == "edge/users/info" {
			time.Sleep(1500 * time.Millisecond)
		}
		return nil, false
	})
	t.Cleanup(func() { fs.SetEdgeHook(nil) })

	evs, stop := app.Subscribe()
	defer stop()
	entry := func(key, ts, by string) {
		raw, _ := json.Marshal(map[string]any{"type": "activity", "subtype": "activity_updated", "entry": map[string]any{
			"key": key, "feed_ts": ts, "is_unread": true,
			"item": map[string]any{"type": "at_user", "message": map[string]any{"ts": ts, "channel": ch, "author_user_id": by}}}})
		app.Inject(slack.Event{Type: "activity", Subtype: "activity_updated", Raw: raw})
	}
	entry("fa-wait-first", first, stranger)
	entry("fa-wait-gone", gone, author)
	prev := slack.Message{Type: "message", TS: gone, User: author, Text: mention + "a synthetic question that goes"}
	switch how {
	case "deleted":
		lvInject(app, ch, map[string]any{"type": "message", "subtype": "message_deleted", "hidden": true,
			"deleted_ts": gone, "previous_message": prev})
	default:
		now := prev
		now.Text = "a synthetic question that goes, for the team"
		now.Edited = &slack.Edited{User: author, TS: fmt.Sprintf("%d.000001", time.Now().Unix())}
		lvInject(app, ch, map[string]any{"type": "message", "subtype": "message_changed", "hidden": true,
			"message": now, "previous_message": prev})
	}
	entry("fa-wait-kept", kept, author)

	// core's announcements of the three
	seen := map[string]bool{}
	for dl := time.After(20 * time.Second); !seen["fa-wait-first"] || !seen["fa-wait-gone"] || !seen["fa-wait-kept"]; {
		select {
		case ev := <-evs:
			if ev.Type == "activity" {
				seen[ev.ID] = true
			}
		case <-dl:
			t.Fatalf("core announced %v of the three entries", seen)
		}
	}
	ghost := false
	e.until(t, "the untouched message's entry", func() bool {
		ghost = ghost || actListed(v, "fa-wait-gone")
		return actListed(v, "fa-wait-kept")
	})
	if ghost {
		t.Fatalf("%s: Activity listed the entry whose message was %s while it waited", how, how)
	}
	for i := 0; i < 10; i++ {
		e.frame()
		if actListed(v, "fa-wait-gone") {
			t.Fatalf("%s: frame %d lists the entry whose message was %s while it waited", how, i, how)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

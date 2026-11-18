package views

// What a followed thread's reply deleted or edited by its author does to
// the Activity entry about it, and what the server zeroing the badges
// does to the rows (Karim's report, 2026-11-17: a deleted reply's thread
// row stayed, drawing the deleted text). On internal/fakeslack's
// activity fixture.

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

const (
	faReply = "1762600200.000100" // fa-thread's latest_ts: a reply in ViewsThreadUnread
	faAtTS  = "1762700100.000100" // fa-at-user's message, in ViewsChannel's window
)

func actListed(v *Activity, key string) bool {
	for _, it := range actPage(v).Items {
		if actKey(&it) == key {
			return true
		}
	}
	return false
}

// A reply deleted by its author: the thread_v2 entry whose messageTs it
// was (min_unread_ts || latest_ts: Slack's /29I) leaves Activity at once,
// as Slack's messageDeleted dispatches "Remove activity item"
// {channelId, messageTs: deleted_ts} (P0K2), with no feed read.
func TestActivityThreadLeavesWithItsDeletedReply(t *testing.T) {
	v, h, e, fs := vaFakeActivity(t, false)
	actItem(t, v, "fa-thread")
	feeds := fs.CallCount("activity.feed")
	prev := slack.Message{Type: "message", TS: faReply, ThreadTS: fakeslack.ViewsThreadUnread, User: "U0FAKE00002", Text: "a synthetic follow-up"}
	lvInject(h.Core(), fakeslack.ViewsChannel, map[string]any{"type": "message", "subtype": "message_deleted", "hidden": true,
		"deleted_ts": faReply, "previous_message": prev})
	e.until(t, "the thread's row gone", func() bool { return !actListed(v, "fa-thread") })
	for i := 0; i < 10; i++ {
		e.frame()
		if actListed(v, "fa-thread") {
			t.Fatalf("frame %d: the thread's row came back", i)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if n := fs.CallCount("activity.feed") - feeds; n != 0 {
		t.Errorf("%d activity.feed calls for the deletion; Slack's reducer removes the entry", n)
	}
}

// A reply edited to take the viewer's mention out: Slack's messageChanged
// removes the entries whose messageTs it is (VxbD's checkForMentionInMessage
// of previous_message, and not of the new copy), the thread's row here.
func TestActivityThreadLeavesWhenItsReplyDropsTheMention(t *testing.T) {
	v, h, e, _ := vaFakeActivity(t, false)
	actItem(t, v, "fa-thread")
	prev := slack.Message{Type: "message", TS: faReply, ThreadTS: fakeslack.ViewsThreadUnread, User: "U0FAKE00002",
		Text: "<@" + fakeslack.SelfID + "> a synthetic follow-up"}
	now := prev
	now.Text = "a synthetic follow-up, for the team"
	now.Edited = &slack.Edited{User: "U0FAKE00002", TS: fmt.Sprintf("%d.000001", time.Now().Unix())}
	lvInject(h.Core(), fakeslack.ViewsChannel, map[string]any{"type": "message", "subtype": "message_changed", "hidden": true,
		"message": now, "previous_message": prev})
	e.until(t, "the thread's row gone", func() bool { return !actListed(v, "fa-thread") })
	if !actListed(v, "fa-at-user") {
		t.Error("another entry went with it")
	}
}

// badge_counts_updated with every activity_v2 count at 0 marks each
// unread row read here alone (VxbD handleBadgeCountsUpdatedRTMEvent → Japw
// m$ isLocalOnlyUpdate): no activity.markRead goes out, and a row whose
// message the store doesn't hold stays as it is.
func TestActivityBadgesZeroReadLocally(t *testing.T) {
	v, h, e, fs := vaFakeActivity(t, false)
	at := actItem(t, v, "fa-at-user")
	if !v.isUnread(at, "fa-at-user") {
		t.Fatal("fa-at-user is read before the event")
	}
	if _, ok := h.Core().S.FindHeld(fakeslack.ViewsChannel, faAtTS); !ok {
		t.Fatal("fa-at-user's message isn't stored")
	}
	// the unread rows the rule leaves: a reaction's, and those with no
	// message the store holds (an invite, a reminder, a system alert)
	var keep []string
	for _, it := range actPage(v).Items {
		k := actKey(&it)
		if !v.isUnread(&it, k) {
			continue
		}
		ts := firstNonEmpty(it.LatestTS, actMessageTS(&it))
		_, held := h.Core().S.FindHeld(it.Channel, ts)
		if it.Type == "message_reaction" || it.Channel == "" || ts == "" || !held {
			keep = append(keep, k)
		}
	}
	if len(keep) == 0 {
		t.Fatal("the feed has no unread row without a stored message")
	}
	writes := len(fs.ActivityWrites())
	zeros := map[string]int{}
	for _, k := range []string{"at_user", "at_user_group", "at_channel", "thread_v2", "dm", "keyword", "internal_channel_invite"} {
		zeros[k] = 0
	}
	raw, _ := json.Marshal(map[string]any{"type": "badge_counts_updated", "activity_v2": zeros})
	h.Core().Inject(slack.Event{Type: "badge_counts_updated", Raw: raw})
	e.until(t, "fa-at-user read", func() bool { return !v.isUnread(actItem(t, v, "fa-at-user"), "fa-at-user") })
	for _, k := range keep {
		if !v.isUnread(actItem(t, v, k), k) {
			t.Errorf("%s, a reaction's or with no message stored, was marked read", k)
		}
	}
	for i := 0; i < 5; i++ {
		e.frame()
	}
	for _, w := range fs.ActivityWrites()[writes:] {
		t.Errorf("%s went out for the badges' zero; Slack's update is local only", w.Method)
	}
}

// badgesZero is badge_counts_updated with every activity_v2 count at 0.
func badgesZero(t *testing.T, v *Activity) {
	t.Helper()
	zeros := map[string]int{}
	for _, k := range []string{"at_user", "at_user_group", "at_channel", "thread_v2", "dm", "keyword", "internal_channel_invite"} {
		zeros[k] = 0
	}
	raw, _ := json.Marshal(map[string]any{"type": "badge_counts_updated", "activity_v2": zeros})
	v.h.Core().Inject(slack.Event{Type: "badge_counts_updated", Raw: raw})
}

// The badges' zero marks the rows read in the page Activity holds, as
// Slack's local-only write marks its store's items: a load that went out
// after it brings the server's read state, which wins (a row the server
// says is unread again draws unread). A load already out when the zero
// came, read before it, doesn't undo the marks.
func TestActivityBadgesZeroThenLoad(t *testing.T) {
	v, _, e, fs := vaFakeActivity(t, false)
	unread := func() bool { return v.isUnread(actItem(t, v, "fa-at-user"), "fa-at-user") }
	// a load out when the zero comes keeps the mark
	fs.SetFault("activity.feed", fakeslack.Fault{ReplyDelay: 600 * time.Millisecond, Times: 1})
	feeds := fs.CallCount("activity.feed")
	v.load(actRefresh)
	e.until(t, "the slow load out", func() bool { return fs.CallCount("activity.feed") > feeds })
	badgesZero(t, v)
	e.until(t, "fa-at-user read", func() bool { return !unread() })
	e.until(t, "the slow load landed", func() bool { return !v.data.busy() && !v.wasBusy })
	if unread() {
		t.Fatal("a load read before the zero made the row unread again")
	}
	// the server still says unread (no markRead went out): a load after
	// the zero brings that back
	v.load(actRefresh)
	e.until(t, "the load after the zero landed, the row unread", func() bool { return !v.data.busy() && !v.wasBusy && unread() })
}

// Realtime changes and the badges' zero apply in the order the socket sent
// them, however many came while Activity drew nothing: a row an update made
// unread before the zero is read after it, and one made unread after the
// zero stays unread.
func TestActivityBadgesZeroInSocketOrder(t *testing.T) {
	v, _, e, _ := vaFakeActivity(t, false)
	app := v.h.Core()
	entry := func(key, ts string, unread bool) {
		raw, _ := json.Marshal(map[string]any{"type": "activity", "subtype": "activity_updated", "entry": map[string]any{
			"key": key, "feed_ts": ts, "is_unread": unread,
			"item": map[string]any{"type": "keyword", "message": map[string]any{"ts": ts, "channel": fakeslack.ViewsChannel, "author_user_id": "U0FAKE00002"}}}})
		app.Inject(slack.Event{Type: "activity", Subtype: "activity_updated", Raw: raw})
	}
	const kw = "1762700400.000100" // fa-keyword's message, in ViewsChannel's window
	entry("fa-keyword", kw, true)
	for i := 0; i < 3; i++ {
		badgesZero(t, v)
	}
	time.Sleep(150 * time.Millisecond) // the event goroutine queues them; no frame drawn meanwhile
	e.frame()
	e.frame()
	if v.isUnread(actItem(t, v, "fa-keyword"), "fa-keyword") {
		t.Error("an entry made unread before the zero is unread after it")
	}
	badgesZero(t, v)
	entry("fa-keyword", kw, true)
	time.Sleep(150 * time.Millisecond)
	e.frame()
	e.frame()
	if !v.isUnread(actItem(t, v, "fa-keyword"), "fa-keyword") {
		t.Error("an entry made unread after the zero is read")
	}
}

// Without the inbox, Slack marks the All list's unread rows read locally
// when every count and alert badge_counts_updated carries is 0 (VxbD
// handleBadgeCountsUpdatedRTMEvent's classic branch), no activity.markRead.
func TestActivityBadgesZeroClassic(t *testing.T) {
	v, _, e, fs := vaFakeActivity(t, false)
	app := v.h.Core()
	app.SetExperimentGroup("activity_inbox", "off")
	t.Cleanup(func() { app.ClearExperimentGroup("activity_inbox") })
	e.frame()
	if !v.isUnread(actItem(t, v, "fa-at-user"), "fa-at-user") {
		t.Fatal("fa-at-user is read before the event")
	}
	writes := len(fs.ActivityWrites())
	raw, _ := json.Marshal(map[string]any{"type": "badge_counts_updated",
		"counts": map[string]int{"channels": 0, "dms": 0, "thread_unreads": 0, "thread_mentions": 0, "app_dms": 0},
		"alerts": map[string]int{"external_channel_invite": 0, "quietly_added_to_channel": 0}})
	app.Inject(slack.Event{Type: "badge_counts_updated", Raw: raw})
	e.until(t, "fa-at-user read", func() bool { return !v.isUnread(actItem(t, v, "fa-at-user"), "fa-at-user") })
	for _, w := range fs.ActivityWrites()[writes:] {
		t.Errorf("%s went out for the badges' zero; Slack's update is local only", w.Method)
	}
}

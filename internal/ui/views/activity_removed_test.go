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

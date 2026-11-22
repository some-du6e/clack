package core

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// A message the event buffer had no room for still raises maxTs (the
// socket's OnDrop); a dropped edit's event ts (the edit's time) doesn't.
func TestDroppedMessageRaisesMaxTS(t *testing.T) {
	a, _ := testApp(t)
	if a.RTM == nil || a.RTM.OnDrop == nil {
		t.Fatal("the socket has no OnDrop: a message the buffer drops never reaches maxTs")
	}
	const ts = "1762600100.000100"
	a.RTM.OnDrop(slack.Event{Type: "message", Raw: raw(map[string]any{"type": "message", "channel": "C1", "ts": ts, "user": "U1"})})
	if got := a.S.NotificationsMaxTS(); got != ts {
		t.Errorf("after a dropped message maxTs is %q, want its ts %s", got, ts)
	}
	a.RTM.OnDrop(slack.Event{Type: "message", Raw: raw(map[string]any{"type": "message", "subtype": "message_changed", "channel": "C1", "ts": "1762600200.000100"})})
	if got := a.S.NotificationsMaxTS(); got != ts {
		t.Errorf("a dropped edit's event ts raised maxTs to %q", got)
	}
}

// The reply bar's channel lists as Slack's activityIA4 keeps them: a
// channel's list is there once a fetch answered; the socket's items go in
// at once after that (none before), a read one comes out under
// replybar_unread_reliability_fix only, a thread read in another client
// takes its items out, and a fetch requested before a live item keeps it.
func TestChannelThreadActivityLikeSlack(t *testing.T) {
	a, f := testApp(t)
	a.SetExperimentGroup("unread_thread_indication_in_replybar", "on")
	ctx := context.Background()
	const r1, r2, r3 = "1700000001.000100", "1700000002.000100", "1700000003.000100"
	bundle := func(root, latest string, unread bool) map[string]any {
		return map[string]any{"key": "thread_v2-C1-" + root, "feed_ts": latest, "is_unread": unread,
			"item": map[string]any{"type": "thread_v2", "bundle_info": map[string]any{"payload": map[string]any{
				"thread_entry": map[string]any{"channel_id": "C1", "thread_ts": root, "latest_ts": latest, "unread_msg_count": 1}}}}}
	}
	live := func(e map[string]any) {
		a.handle(rtm("activity", map[string]any{"subtype": "activity_updated", "entry": e}))
	}
	feed := func(items ...map[string]any) {
		b, _ := json.Marshal(map[string]any{"ok": true, "items": items, "response_metadata": map[string]any{"next_cursor": ""}})
		f.mu.Lock()
		f.reply["activity.feed"] = string(b)
		f.reply["messages.list"] = `{"ok":true,"messages":{}}`
		f.mu.Unlock()
	}
	feeds := func() int {
		f.mu.Lock()
		defer f.mu.Unlock()
		n := 0
		for _, c := range f.calls {
			if c == "activity.feed" {
				n++
			}
		}
		return n
	}
	live(bundle(r1, "1700000001.000200", true))
	if n := a.UnreadThreadsFor("C1", r1); n != 0 || a.ThreadActivityLoaded("C1") {
		t.Errorf("before any fetch the socket's item went in (%d, loaded %v): Slack's has no cursor for it", n, a.ThreadActivityLoaded("C1"))
	}
	feed(bundle(r1, "1700000001.000200", true))
	if _, err := a.ChannelUnreadThreads(ctx, "C1"); err != nil {
		t.Fatal(err)
	}
	if n := a.UnreadThreadsFor("C1", r1); n != 1 || !a.ThreadActivityLoaded("C1") {
		t.Fatalf("after the fetch %s has %d unread items (loaded %v), want 1", r1, n, a.ThreadActivityLoaded("C1"))
	}
	before := feeds()
	live(bundle(r2, "1700000002.000200", true))
	if n := a.UnreadThreadsFor("C1", r2); n != 1 {
		t.Errorf("a live item for %s gave %d unread items, want 1 at once", r2, n)
	}
	if n := feeds(); n != before {
		t.Errorf("a live item fetched activity.feed again (%d calls, was %d)", n, before)
	}
	live(bundle(r2, "1700000002.000200", false))
	if n := a.UnreadThreadsFor("C1", r2); n != 1 {
		t.Errorf("a read item without replybar_unread_reliability_fix changed the list (%d), Slack's leaves it", n)
	}
	a.SetExperimentGroup("replybar_unread_reliability_fix", "on")
	live(bundle(r2, "1700000002.000200", false))
	if n := a.UnreadThreadsFor("C1", r2); n != 0 {
		t.Errorf("a read item under replybar_unread_reliability_fix left %d unread items", n)
	}
	a.handle(rtm("thread_marked", map[string]any{"subscription": map[string]any{"channel": "C1", "thread_ts": r1, "last_read": "1700000001.000200"}}))
	if n := a.UnreadThreadsFor("C1", r1); n != 0 {
		t.Errorf("a thread read in another client kept %d unread items", n)
	}
	release, started := gateOnce(f, "activity.feed")
	defer release()
	feed() // the answer is as the server was when the request went out
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = a.ChannelUnreadThreads(ctx, "C1")
	}()
	waitFor(t, "the fetch out", 3*time.Second, started)
	live(bundle(r3, "1700000003.000200", true))
	release()
	<-done
	if n := a.UnreadThreadsFor("C1", r3); n != 1 {
		t.Errorf("a fetch requested before %s's live item dropped it (%d)", r3, n)
	}
}

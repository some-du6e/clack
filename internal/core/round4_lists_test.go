package core

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// replyBarFeed makes activity.feed answer items and messages.list nothing
// (the items' messages aren't needed here).
func replyBarFeed(f *fakeSlack, items ...map[string]any) {
	b, _ := json.Marshal(map[string]any{"ok": true, "items": items, "response_metadata": map[string]any{"next_cursor": ""}})
	f.mu.Lock()
	f.reply["activity.feed"] = string(b)
	f.reply["messages.list"] = `{"ok":true,"messages":{}}`
	f.mu.Unlock()
}

// replyBarBundle is the thread_v2 item of root in C1 whose latest reply is
// latest.
func replyBarBundle(root, latest string, unread bool) map[string]any {
	return map[string]any{"key": "thread_v2-C1-" + root, "feed_ts": latest, "is_unread": unread,
		"item": map[string]any{"type": "thread_v2", "bundle_info": map[string]any{"payload": map[string]any{
			"thread_entry": map[string]any{"channel_id": "C1", "thread_ts": root, "latest_ts": latest, "unread_msg_count": 1}}}}}
}

// The socket missed a thread being read in another client (a sleep, a
// slow reconnect): the counts read after the gap says C1 has no unread
// threads, and C1's reply bar list goes with it. Kept, the thread read as
// unread for the rest of the session.
func TestThreadActivityClearedWithItsCount(t *testing.T) {
	a, f := testApp(t)
	a.SetExperimentGroup("unread_thread_indication_in_replybar", "on")
	ctx := context.Background()
	const root, latest = "1700000001.000100", "1700000001.000200"
	replyBarFeed(f, replyBarBundle(root, latest, true))
	if _, err := a.ChannelUnreadThreads(ctx, "C1"); err != nil {
		t.Fatal(err)
	}
	if n := a.UnreadThreadsFor("C1", root); n != 1 {
		t.Fatalf("setup: the fetch left %d unread items for %s, want 1", n, root)
	}
	f.mu.Lock()
	f.reply["client.counts"] = `{"ok":true,"threads":{"has_unreads":false,"mention_count":0,"unread_count_by_channel":{}},"channels":[],"ims":[],"mpims":[]}`
	f.mu.Unlock()
	a.refreshCountsAfterGap(ctx)
	if n := a.UnreadThreadsFor("C1", root); n != 0 {
		t.Errorf("after a counts read with no unread threads in C1, its reply bar list kept %d unread items for %s", n, root)
	}
}

// The viewer reads a thread while the first fetch of its channel's thread
// activity is out (a toast's click opens the channel and the thread at
// once): the mark and its thread_marked echo come before the answer,
// which the server made before the mark. The answer doesn't put the
// thread back as unread.
func TestThreadActivityKeepsAMarkFromBeforeItsList(t *testing.T) {
	a, f := testApp(t)
	a.SetExperimentGroup("unread_thread_indication_in_replybar", "on")
	ctx := context.Background()
	const root, latest = "1700000001.000100", "1700000001.000200"
	replyBarFeed(f, replyBarBundle(root, latest, true))
	f.mu.Lock()
	f.reply["subscriptions.thread.mark"] = `{"ok":true}`
	f.mu.Unlock()
	release, started := gateOnce(f, "activity.feed")
	defer release()
	done := make(chan error, 1)
	go func() {
		_, err := a.ChannelUnreadThreads(ctx, "C1")
		done <- err
	}()
	waitFor(t, "the fetch out", 3*time.Second, started)
	if err := a.MarkThread(ctx, "C1", root, latest); err != nil {
		t.Fatal(err)
	}
	a.handle(rtm("thread_marked", map[string]any{"subscription": map[string]any{"channel": "C1", "thread_ts": root, "last_read": latest}}))
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if n := a.UnreadThreadsFor("C1", root); n != 0 {
		t.Errorf("a fetch answered after the thread was read put %s back as unread (%d items): the mark came before the list", root, n)
	}
}

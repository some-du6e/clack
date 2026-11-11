package core

import (
	"context"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// client.counts' unread_count_by_channel, and fetchActivitiesForChannel's
// request and getUnreadThreadsCountForMessage's counting (thread_v2 items
// per thread ts; read items and reactions don't count).
func TestChannelUnreadThreads(t *testing.T) {
	a, f := testApp(t)
	c := &slack.Counts{}
	c.Threads.UnreadCountByChannel = map[string]int{"C1": 2}
	a.S.SetCounts(c)
	if n := a.ThreadsUnreadIn("C1"); n != 2 {
		t.Errorf("ThreadsUnreadIn(C1) = %d, want 2", n)
	}
	if n := a.ThreadsUnreadIn("C9"); n != 0 {
		t.Errorf("ThreadsUnreadIn(C9) = %d, want 0", n)
	}
	thread := func(key, ts string, unread bool) string {
		u := "false"
		if unread {
			u = "true"
		}
		return `{"key":"` + key + `","feed_ts":"1700000100.000000","is_unread":` + u + `,"item":{"type":"thread_v2","bundle_info":{"payload":{"thread_entry":{"channel_id":"C1","thread_ts":"` + ts + `","latest_ts":"1700000099.000100","unread_msg_count":3}}}}}`
	}
	f.reply["activity.feed"] = `{"ok":true,"items":[` + thread("t1", "1700000001.000100", true) + `,` + thread("t2", "1700000002.000100", false) + `,` +
		`{"key":"r1","is_unread":true,"item":{"type":"message_reaction","message":{"channel":"C1","ts":"1700000003.000100","thread_ts":"1700000003.000100"},"reaction":{"user":"U1","name":"tada"}}}],"response_metadata":{"next_cursor":""}}`
	f.reply["messages.list"] = `{"ok":true,"messages":{}}`
	got, err := a.ChannelUnreadThreads(context.Background(), "C1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["1700000001.000100"] != 1 {
		t.Errorf("unread threads %v, want only 1700000001.000100 once", got)
	}
	p := f.last("activity.feed")
	for k, want := range map[string]string{"channel_ids": "C1", "types": "thread_v2", "unread_only": "true", "mode": "chrono_unreads", "limit": "10"} {
		if v := p.Get(k); v != want {
			t.Errorf("activity.feed %s = %q, want %q", k, v, want)
		}
	}
}

package core

import (
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// broadcastEvent is a thread_broadcast's message event with its root (the
// parent, n replies, the latest at latest).
func broadcastEvent(ch, ts string, p slack.Message, n int, latest string) map[string]any {
	return map[string]any{"type": "message", "subtype": "thread_broadcast", "channel": ch, "ts": ts, "thread_ts": p.TS, "event_ts": ts,
		"user": "U0FAKE00003", "text": "also sent to the channel",
		"root": map[string]any{"type": "message", "ts": p.TS, "thread_ts": p.TS, "user": p.User, "text": p.Text,
			"reply_count": n, "reply_users": []string{"U0FAKE00002", "U0FAKE00003"}, "reply_users_count": 2, "latest_reply": latest}}
}

// newerOf is the broadcast's NewerReplies as core gives it to the UI now.
func newerOf(t *testing.T, a *App, ch, ts string) bool {
	t.Helper()
	for _, m := range timelineOf(a, ch) {
		if m.TS == ts {
			return a.message(ch, &m).NewerReplies
		}
	}
	t.Fatalf("broadcast %s not in the timeline", ts)
	return false
}

// waitNewer waits for an "update" of broadcast ts with NewerReplies want.
func waitNewer(t *testing.T, log *eventLog, ts string, want bool) {
	t.Helper()
	log.wait(t, "the broadcast's update", func(evs []Event) bool {
		for i := len(evs) - 1; i >= 0; i-- {
			if ev := evs[i]; ev.Type == "update" && ev.TS == ts && ev.Msg != nil {
				return ev.Msg.NewerReplies == want
			}
		}
		return false
	})
}

// ThreadBroadcastFooter (KYIV k): "View newer replies" shows once the
// stored root's latest_reply is past the broadcast, and goes when the
// root's message_changed brings it back (the newer reply deleted).
func TestBroadcastNewerRepliesParentCached(t *testing.T) {
	a, fs, log := rtApp(t)
	ch := fixtureCh
	p, _, _ := threadFixture(a, ch)
	b := fmtTS(time.Now().Unix() - 30)
	fs.Push(broadcastEvent(ch, b, p, 3, b))
	flush(t, fs, log)
	if newerOf(t, a, ch, b) {
		t.Fatal("the thread's latest reply is the broadcast: no newer replies")
	}
	r := slack.Message{TS: fmtTS(time.Now().Unix() - 20), ThreadTS: p.TS, User: "U0FAKE00002", Text: "after"}
	fs.Push(replyEvent(ch, r))
	fs.Push(repliedEvent(ch, p, 4, []string{"U0FAKE00002", "U0FAKE00003"}, r.TS))
	waitNewer(t, log, b, true)
	if !newerOf(t, a, ch, b) {
		t.Error("a reply after the broadcast: no newer replies")
	}
	// the newer reply deleted: the parent's message_changed has the
	// broadcast as its latest reply again
	fs.Push(map[string]any{"type": "message", "subtype": "message_changed", "hidden": true, "channel": ch, "event_ts": r.TS,
		"message": map[string]any{"type": "message", "ts": p.TS, "thread_ts": p.TS, "user": p.User, "text": p.Text,
			"reply_count": 3, "reply_users": []string{"U0FAKE00002", "U0FAKE00003"}, "reply_users_count": 2, "latest_reply": b}})
	waitNewer(t, log, b, false)
	if newerOf(t, a, ch, b) {
		t.Error("newer replies after the newer reply went")
	}
}

// Without the parent cached, the broadcast's own root is Slack's stored
// root (makeBroadcastRootObject): it decides at load, and message_replied
// for the thread moves it on. A broadcast without a root object never
// shows the footer (KYIV reads msg.root.ts).
func TestBroadcastNewerRepliesRootOnly(t *testing.T) {
	a, fs, log := rtApp(t)
	ch := fixtureCh
	now := time.Now().Unix() - 60
	root := &slack.Message{Type: "message", TS: fmtTS(now - 100), ThreadTS: fmtTS(now - 100), User: "U0FAKE00001", Text: "root",
		ReplyCount: 2, LatestReply: fmtTS(now)}
	old := slack.Message{Type: "message", TS: fmtTS(now - 50), ThreadTS: root.TS, Subtype: "thread_broadcast", User: "U0FAKE00002", Text: "old", Root: root}
	cur := slack.Message{Type: "message", TS: fmtTS(now), ThreadTS: root.TS, Subtype: "thread_broadcast", User: "U0FAKE00002", Text: "cur", Root: root}
	bare := slack.Message{Type: "message", TS: fmtTS(now - 40), ThreadTS: root.TS, Subtype: "thread_broadcast", User: "U0FAKE00002", Text: "bare"}
	a.S.SetLatest(ch, []slack.Message{old, bare, cur}, false)
	if !newerOf(t, a, ch, old.TS) {
		t.Error("a broadcast older than its root's latest reply: no footer")
	}
	if newerOf(t, a, ch, cur.TS) {
		t.Error("the thread's latest reply shows the footer")
	}
	if newerOf(t, a, ch, bare.TS) {
		t.Error("a broadcast without a root object shows the footer")
	}
	p := slack.Message{TS: root.TS, User: root.User, Text: root.Text}
	latest := fmtTS(now + 10)
	fs.Push(repliedEvent(ch, p, 3, []string{"U0FAKE00002"}, latest))
	waitNewer(t, log, cur.TS, true)
	if !newerOf(t, a, ch, cur.TS) {
		t.Error("message_replied with a newer reply: no footer")
	}
	// an older message_replied than the root copy has is dropped
	fs.Push(repliedEvent(ch, p, 1, []string{"U0FAKE00002"}, fmtTS(now-50)))
	flush(t, fs, log)
	if !newerOf(t, a, ch, cur.TS) {
		t.Error("a stale message_replied took the footer away")
	}
}

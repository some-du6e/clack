package core

import (
	"context"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// A followed thread's last read (Slack's thread subscription lastRead,
// docs/THREADS-AUDIT.md): conversations.replies' root carries last_read,
// thread_marked and thread_subscribed move it, and a successful
// subscriptions.thread.mark sets it at once; each tells the views.
func TestThreadLastRead(t *testing.T) {
	a, f := testApp(t)
	const root, r1, r2 = "1.000001", "1.000002", "1.000003"
	a.S.SetThread("C1", root, []slack.Message{
		{TS: root, ThreadTS: root, User: "U1", Text: "a synthetic root", ReplyCount: 2, ReplyUsersN: 2, Subscribed: true, LastRead: r1},
		{TS: r1, ThreadTS: root, User: "U2", Text: "one"},
		{TS: r2, ThreadTS: root, User: "U1", Text: "two"},
	})
	msgs, err := a.Replies(context.Background(), "C1", root)
	if err != nil || len(msgs) != 3 {
		t.Fatalf("replies: %v %v", msgs, err)
	}
	if m := msgs[0]; !m.FollowingThread || m.ThreadLastRead != r1 || m.ReplyUsersN != 2 {
		t.Fatalf("root: following %v, last read %q, reply users %d", m.FollowingThread, m.ThreadLastRead, m.ReplyUsersN)
	}
	evs, stop := a.Subscribe()
	defer stop()
	a.handle(rtm("thread_marked", map[string]any{"subscription": map[string]any{"type": "thread", "channel": "C1", "thread_ts": root, "last_read": r2}}))
	if e := next(t, evs, "update"); e.Msg.TS != root || e.Msg.ThreadLastRead != r2 {
		t.Errorf("thread_marked: update %s with last read %q, want %q", e.Msg.TS, e.Msg.ThreadLastRead, r2)
	}
	a.handle(rtm("thread_subscribed", map[string]any{"subscription": map[string]any{"channel": "C1", "thread_ts": root, "last_read": r1}}))
	if e := next(t, evs, "update"); !e.Msg.FollowingThread || e.Msg.ThreadLastRead != r1 {
		t.Errorf("thread_subscribed: following %v, last read %q", e.Msg.FollowingThread, e.Msg.ThreadLastRead)
	}
	f.reply["subscriptions.thread.mark"] = `{"ok":true}`
	if err := a.MarkThread(context.Background(), "C1", root, r2); err != nil {
		t.Fatal(err)
	}
	if e := next(t, evs, "update"); e.Msg.ThreadLastRead != r2 {
		t.Errorf("after subscriptions.thread.mark the root reads %q, want %q", e.Msg.ThreadLastRead, r2)
	}
	if m, _ := a.S.Find("C1", root); m.LastRead != r2 {
		t.Errorf("the cached root's last_read is %q", m.LastRead)
	}
}

// The server's notification for a followed thread's reply reaches the UI.
func TestDesktopNotificationForwarded(t *testing.T) {
	a, _ := testApp(t)
	evs, stop := a.Subscribe()
	defer stop()
	a.handle(rtm("desktop_notification", map[string]any{"channel": "C1", "ts": "1.000002", "thread_ts": "1.000001", "content": "synthetic"}))
	if e := next(t, evs, "desktop_notification"); e.Channel != "C1" || len(e.Raw) == 0 {
		t.Errorf("forwarded %+v", e)
	}
}

package core

import (
	"context"
	"testing"
)

// getView's threads are followed ones: core.Threads seeds their
// subscriptions from the roots (processThreads runs every root through
// slurpThreadSubscriptions, udbq), so the viewer's own mark (MarkThread)
// and a thread_marked move only the last read, and the thread stays
// followed whether its root is cached or not. A last read alone for a
// thread core knows nothing of records nothing (readers fall back to the
// root's copy, as Slack's do on its unset subscribed), a thread_marked's
// too: Slack's threadMarked reads no active.
func TestThreadsSeedSubscriptionsLikeSlack(t *testing.T) {
	a, f := testApp(t)
	ctx := context.Background()
	const root, reply, other, late = "1700000000.000100", "1700000060.000100", "1700000100.000100", "1700000160.000100"
	f.reply["subscriptions.thread.getView"] = `{"ok":true,"has_more":false,"threads":[{"root_msg":` +
		`{"type":"message","channel":"C1","ts":"` + root + `","thread_ts":"` + root + `","user":"U1","text":"root","reply_count":1,` +
		`"reply_users":["U2"],"latest_reply":"` + reply + `","subscribed":true,"last_read":"` + root + `"},` +
		`"latest_replies":[{"type":"message","channel":"C1","ts":"` + reply + `","thread_ts":"` + root + `","user":"U2","text":"one"}],` +
		`"unread_replies":[{"type":"message","channel":"C1","ts":"` + reply + `","thread_ts":"` + root + `","user":"U2","text":"one"}]}]}`
	p, err := a.Threads(ctx, "", 8)
	if err != nil {
		t.Fatal(err)
	}
	if sub, ok := a.S.ThreadSub("C1", root); !ok || !sub.Subscribed || sub.LastRead != root {
		t.Fatalf("after getView the subscription is %+v (known %v), want followed, read to the root", sub, ok)
	}
	if r := p.Threads[0].Root; !r.FollowingThread || r.ThreadLastRead != root {
		t.Errorf("the listed root: following %v, last read %q", r.FollowingThread, r.ThreadLastRead)
	}
	f.reply["subscriptions.thread.mark"] = `{"ok":true}`
	if err := a.MarkThread(ctx, "C1", root, reply); err != nil {
		t.Fatal(err)
	}
	if sub, _ := a.S.ThreadSub("C1", root); !sub.Subscribed || sub.LastRead != reply {
		t.Errorf("after the viewer's mark: %+v, want followed, read to the reply", sub)
	}
	a.handle(rtm("thread_marked", map[string]any{"subscription": map[string]any{"type": "thread", "channel": "C1",
		"thread_ts": root, "last_read": reply, "active": false}}))
	if sub, _ := a.S.ThreadSub("C1", root); !sub.Subscribed || sub.LastRead != reply {
		t.Errorf("after thread_marked (active false): %+v, want followed, read to the reply", sub)
	}
	// a thread nothing seeded and no cached root
	if err := a.MarkThread(ctx, "C1", other, late); err != nil {
		t.Fatal(err)
	}
	if sub, ok := a.S.ThreadSub("C1", other); ok {
		t.Errorf("a mark recorded an unknown thread as %+v", sub)
	}
	a.handle(rtm("thread_marked", map[string]any{"subscription": map[string]any{"type": "thread", "channel": "C1",
		"thread_ts": other, "last_read": late, "active": true}}))
	if sub, ok := a.S.ThreadSub("C1", other); ok {
		t.Errorf("thread_marked recorded an unknown thread as %+v: Slack's threadMarked sets only its last read", sub)
	}
}

// threadsManuallyMarkedUnread's origins (markOrigin): Mark unread records
// where it came from; a failed one puts back the thread's earlier mark
// (v4te's previousManualMark) or takes the thread out when it had none.
// Activity's Mark as unread on a thread's item marks its thread
// (markActivityItemAsUnread, 54V6: ACTIVITY_PAGE), not on a reaction's,
// and a failure leaves it.
func TestThreadsMarkedUnreadOriginsLikeSlack(t *testing.T) {
	a, f := testApp(t)
	ctx := context.Background()
	const root, r1, other = "1700000000.000100", "1700000060.000100", "1700000300.000100"
	f.reply["subscriptions.thread.mark"] = `{"ok":true}`
	if err := a.MarkThreadUnreadFrom(ctx, "C1", root, r1, MarkedInThreadsView); err != nil {
		t.Fatal(err)
	}
	if o := a.ThreadMarkedUnread("C1", root); o != MarkedInThreadsView {
		t.Fatalf("marked from %q, want %q", o, MarkedInThreadsView)
	}
	f.reply["subscriptions.thread.mark"] = `{"ok":false,"error":"internal_error"}`
	if err := a.MarkThreadUnreadFrom(ctx, "C1", root, r1, MarkedInThread); err == nil {
		t.Fatal("the failed mark returned no error")
	}
	if o := a.ThreadMarkedUnread("C1", root); o != MarkedInThreadsView {
		t.Errorf("after a failed mark the thread is marked from %q, want the earlier mark's %q", o, MarkedInThreadsView)
	}
	if err := a.MarkThreadUnreadFrom(ctx, "C1", other, other, MarkedInThread); err == nil {
		t.Fatal("the failed mark returned no error")
	}
	if o := a.ThreadMarkedUnread("C1", other); o != "" {
		t.Errorf("a failed first mark left the thread marked from %q", o)
	}
	f.reply["activity.markUnread"] = `{"ok":false,"error":"internal_error"}`
	_ = a.MarkActivityUnread(ctx, ActivityItem{Type: "thread_v2", Key: "k1", Channel: "C1", TS: r1, ThreadTS: other},
		ActivityItem{Type: "message_reaction", Key: "k2", Channel: "D1", TS: r1, ThreadTS: root})
	if o := a.ThreadMarkedUnread("C1", other); o != MarkedInActivity {
		t.Errorf("Activity's Mark as unread on a thread's item: marked from %q, want %q", o, MarkedInActivity)
	}
	if o := a.ThreadMarkedUnread("D1", root); o != "" {
		t.Errorf("a reaction's item marked its thread (%q)", o)
	}
	a.ForgetThreadMarkedUnread("C1", root)
	if o := a.ThreadMarkedUnread("C1", root); o != "" {
		t.Errorf("forgotten, the thread is still marked from %q", o)
	}
	a.ForgetThreadMarkedUnread("", "")
	if m := a.ThreadsMarkedUnread(); m != nil {
		t.Errorf("after clearing: %v", m)
	}
}

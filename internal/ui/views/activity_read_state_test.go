package views

// Activity's read state as Slack's (activity_marks.go), over the socket:
// a thread read elsewhere (thread_marked) or here (subscriptions.thread.mark)
// reads its rows at once, before any activity_updated, and takes them off
// the badge; a mark whose activity.markRead is still out wins over an
// activity_updated from before it. Synthetic data on internal/fakeslack.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
)

// actRSEnv is the views fixtures with two unread threads you follow and
// an unread mention, the badges counting them, booted with realtime on.
type actRSEnv struct {
	fs               *fakeslack.Server
	app              *core.App
	v                *Activity
	e                *vaEnv
	ch               string
	rootA, replyA    string
	rootB, replyB    string
	mention          string
	keyA, keyB, keyM string
}

func newActRSEnv(t *testing.T) *actRSEnv {
	t.Helper()
	sz := fakeslack.Small
	sz.Views = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	r := &actRSEnv{fs: fs, ch: fakeslack.ViewsChannel}
	start := time.Now()
	at := func(ago time.Duration) string { return fmt.Sprintf("%d.000100", start.Add(-ago).Unix()) }
	self, ada := fakeslack.SelfID, "U0FAKE00001"
	r.rootA, r.replyA = at(2*time.Hour), at(10*time.Minute)
	fs.PostAtQuiet(r.ch, r.rootA, self, "a synthetic plan")
	fs.ReplyAtQuiet(r.ch, r.rootA, r.replyA, ada, "a synthetic reply to the plan")
	fs.UpsertActivity(fakeslack.ActivityThreadEntry(r.ch, r.rootA, r.replyA, r.replyA, 1))
	r.rootB, r.replyB = at(3*time.Hour), at(20*time.Minute)
	fs.PostAtQuiet(r.ch, r.rootB, self, "a synthetic agenda")
	fs.ReplyAtQuiet(r.ch, r.rootB, r.replyB, ada, "a synthetic reply to the agenda")
	fs.UpsertActivity(fakeslack.ActivityThreadEntry(r.ch, r.rootB, r.replyB, r.replyB, 1))
	r.mention = at(30 * time.Minute)
	fs.PostAtQuiet(r.ch, r.mention, ada, "<@"+self+"> a synthetic question")
	r.keyM = "at_user-" + r.ch + "-" + r.mention
	fs.UpsertActivity(r.mentionEntry(true))
	fs.SetActivityBadges(map[string]int{"thread_v2": 2, "at_user": 1})
	r.keyA, r.keyB = fakeslack.ActivityThreadKey(r.ch, r.rootA), fakeslack.ActivityThreadKey(r.ch, r.rootB)
	r.app = actOrderBoot(t, fs, t.TempDir())
	r.v, r.e = actOrderView(r.app)
	r.e.until(t, "the feed", func() bool {
		return r.item(r.keyA) != nil && r.item(r.keyB) != nil && r.item(r.keyM) != nil && !r.v.data.busy()
	})
	r.e.until(t, "the badges", func() bool {
		c, ok := r.app.ActivityTabCounts()
		return ok && c[3] == 2
	})
	return r
}

func (r *actRSEnv) mentionEntry(unread bool) map[string]any {
	return map[string]any{"key": r.keyM, "feed_ts": r.mention, "is_unread": unread, "is_bot": false,
		"item": map[string]any{"type": "at_user", "message": map[string]any{"ts": r.mention, "channel": r.ch, "author_user_id": "U0FAKE00001"}}}
}

// threadMarked pushes thread_marked for root, read up to lastRead.
func (r *actRSEnv) threadMarked(root, lastRead string) {
	r.fs.Push(map[string]any{"type": "thread_marked", "subscription": map[string]any{
		"channel": r.ch, "thread_ts": root, "last_read": lastRead, "active": true}})
}

// queuedRead reports whether the view queued a read of thread (""
// for the conversation) for its next draw.
func (r *actRSEnv) queuedRead(thread string) bool {
	r.v.notes.mu.Lock()
	defer r.v.notes.mu.Unlock()
	for _, p := range r.v.notes.items {
		if p.read != nil && p.read.ch == r.ch && p.read.thread == thread {
			return true
		}
	}
	return false
}

// waitNoDraw polls ok without drawing (10 s).
func waitNoDraw(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for dl := time.Now().Add(10 * time.Second); !ok(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(dl) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func (r *actRSEnv) threadsBadge() int {
	c, _ := r.app.ActivityTabCounts()
	return c[3]
}

// item is the loaded page's item key (nil: not listed).
func (r *actRSEnv) item(key string) *core.ActivityItem {
	p := actPage(r.v)
	for i := range p.Items {
		if actKey(&p.Items[i]) == key {
			it := p.Items[i]
			return &it
		}
	}
	return nil
}

func (r *actRSEnv) unread(key string) bool {
	it := r.item(key)
	return it != nil && r.v.isUnread(it, key)
}

// A thread read in another client (thread_marked) reads its row now, in
// the frame after the event, with no activity_updated; one read here
// (subscriptions.thread.mark) the same. Each takes one off the Threads
// badge (decrementBadgeCounts).
func TestActivityThreadReadMarksRows(t *testing.T) {
	r := newActRSEnv(t)
	if !r.unread(r.keyA) || !r.unread(r.keyB) {
		t.Fatal("control: the threads' rows aren't unread")
	}
	updates := r.fs.CallCount("activity.feed")
	// the server's counts after each read, so a client.counts the read
	// sets off agrees with the decrement
	r.fs.SetActivityBadges(map[string]int{"thread_v2": 1, "at_user": 1})
	r.threadMarked(r.rootA, r.replyA)
	r.e.until(t, "thread_marked's row read", func() bool { return !r.unread(r.keyA) })
	if n := r.threadsBadge(); n != 1 {
		t.Errorf("after thread_marked the Threads badge is %d, want 1", n)
	}
	if !r.unread(r.keyB) {
		t.Error("thread_marked read the other thread's row too")
	}
	r.fs.SetActivityBadges(map[string]int{"thread_v2": 0, "at_user": 1})
	if err := r.app.MarkThread(context.Background(), r.ch, r.rootB, r.replyB); err != nil {
		t.Fatal(err)
	}
	r.e.until(t, "subscriptions.thread.mark's row read", func() bool { return !r.unread(r.keyB) })
	if n := r.threadsBadge(); n != 0 {
		t.Errorf("after subscriptions.thread.mark the Threads badge is %d, want 0", n)
	}
	if n := r.fs.CallCount("activity.feed"); n != updates {
		t.Errorf("the reads read the feed again (%d activity.feed): they're local", n-updates)
	}
}

// A row marked read whose activity.markRead is still out: an
// activity_updated from before the mark (is_unread true, the same
// feed_ts) doesn't put it back to unread (applyPendingMarkToActivityUpdate).
func TestActivityPendingMarkBeatsStaleUpdate(t *testing.T) {
	r := newActRSEnv(t)
	it := r.item(r.keyM)
	if it == nil || !r.v.isUnread(it, r.keyM) {
		t.Fatal("control: the mention's row isn't unread")
	}
	r.fs.SetFault("activity.markRead", fakeslack.Fault{Delay: 3 * time.Second})
	r.v.markRead(it, r.keyM)
	if r.unread(r.keyM) {
		t.Fatal("control: the row isn't read right after the mark")
	}
	r.v.live.mu.Lock()
	delete(r.v.live.keys, r.keyM)
	r.v.live.mu.Unlock()
	r.fs.Push(r.fs.UpsertActivity(r.mentionEntry(true)))
	r.e.until(t, "the stale activity_updated applied", func() bool {
		r.v.live.mu.Lock()
		defer r.v.live.mu.Unlock()
		return r.v.live.keys[r.keyM]
	})
	for range 3 {
		r.e.frame()
	}
	if r.unread(r.keyM) {
		t.Error("an activity_updated from before the mark put the row back to unread while activity.markRead was out")
	}
	// the held activity.markRead answers: the mark ends, the row stays read
	r.e.until(t, "activity.markRead's answer", func() bool { return r.v.pending.marks[r.keyM] == nil })
	for range 3 {
		r.e.frame()
	}
	if r.unread(r.keyM) {
		t.Error("once activity.markRead answered the row went back to unread")
	}
}

// A read applied when Activity draws again, after the server's badge
// set that counts it came in (badge_counts_updated): the badge is the
// server's, not one less. Activity undrawn while it all comes.
func TestActivityUndrawnReadKeepsServerBadges(t *testing.T) {
	r := newActRSEnv(t)
	if !r.unread(r.keyA) {
		t.Fatal("control: thread A's row isn't unread")
	}
	server := map[string]int{"thread_v2": 1, "at_user": 1}
	r.fs.SetActivityBadges(server)
	r.threadMarked(r.rootA, r.replyA)
	waitNoDraw(t, "the read queued for the next draw", func() bool { return r.queuedRead(r.rootA) })
	r.fs.Push(map[string]any{"type": "badge_counts_updated", "activity_v2": server})
	waitNoDraw(t, "the server's set", func() bool { return r.threadsBadge() == 1 })
	r.e.until(t, "the row read on the next draws", func() bool { return !r.unread(r.keyA) })
	for range 3 {
		r.e.frame()
	}
	if n := r.threadsBadge(); n != 1 {
		t.Errorf("the Threads badge is %d, want the server's 1: the read came off a set that counted it already", n)
	}
	if !r.unread(r.keyB) {
		t.Error("thread B's row was read too")
	}
}

// A conversation's read (conversations.mark) reads its rows outside
// threads at or before the cursor, and leaves its threads' rows: a
// cursor before the mention leaves it unread, one at it reads it.
func TestActivityChannelReadMarksRows(t *testing.T) {
	r := newActRSEnv(t)
	if !r.unread(r.keyM) {
		t.Fatal("control: the mention's row isn't unread")
	}
	before := fmt.Sprintf("%d.000100", parseTSSec(r.mention)-60)
	if _, err := r.app.Mark(context.Background(), r.ch, before); err != nil {
		t.Fatal(err)
	}
	// thread A's read comes after it on the socket: once its row is read,
	// the conversation's read was applied too
	r.threadMarked(r.rootA, r.replyA)
	r.e.until(t, "thread A's row read", func() bool { return !r.unread(r.keyA) })
	if !r.unread(r.keyM) {
		t.Error("a read before the mention read its row")
	}
	r.fs.SetActivityBadges(map[string]int{"thread_v2": 1, "at_user": 0})
	if _, err := r.app.Mark(context.Background(), r.ch, r.mention); err != nil {
		t.Fatal(err)
	}
	r.e.until(t, "the mention's row read", func() bool { return !r.unread(r.keyM) })
	if !r.unread(r.keyB) {
		t.Error("the conversation's read read a thread's row")
	}
	if c, _ := r.app.ActivityTabCounts(); c[2] != 0 {
		t.Errorf("after the conversation's read the Mentions badge is %d, want 0", c[2])
	}
}

// parseTSSec is a message ts's seconds.
func parseTSSec(ts string) int64 {
	var s int64
	fmt.Sscanf(ts, "%d.", &s)
	return s
}

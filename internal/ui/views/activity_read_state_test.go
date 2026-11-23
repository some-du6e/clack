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
	r.fs.Push(map[string]any{"type": "thread_marked", "subscription": map[string]any{
		"channel": r.ch, "thread_ts": r.rootA, "last_read": r.replyA, "active": true}})
	r.e.until(t, "thread_marked's row read", func() bool { return !r.unread(r.keyA) })
	if c, _ := r.app.ActivityTabCounts(); c[3] != 1 {
		t.Errorf("after thread_marked the Threads badge is %d, want 1", c[3])
	}
	if !r.unread(r.keyB) {
		t.Error("thread_marked read the other thread's row too")
	}
	if err := r.app.MarkThread(context.Background(), r.ch, r.rootB, r.replyB); err != nil {
		t.Fatal(err)
	}
	r.e.until(t, "subscriptions.thread.mark's row read", func() bool { return !r.unread(r.keyB) })
	if c, _ := r.app.ActivityTabCounts(); c[3] != 0 {
		t.Errorf("after subscriptions.thread.mark the Threads badge is %d, want 0", c[3])
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
}

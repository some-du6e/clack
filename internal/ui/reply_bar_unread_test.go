package ui

// The reply bar's "Unread" mark stays true to the server's thread state
// when the socket can't keep it current (thread-reply-toast round 3's
// review): after a slow reconnect the channel's thread activity is
// fetched again, and a fetch that failed is tried again. Synthetic people
// and text only.

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/messages"
)

// replyBarEnv is a live client of a fakeslack whose activity.feed answers
// (Views), with the reply bar's Unread mark on and the window in the
// background.
func replyBarEnv(t *testing.T) (*perfEnv, *fakeslack.Server) {
	t.Helper()
	t.Setenv("CLACK_HOME", t.TempDir())
	sz := fakeslack.Small
	sz.Views = true // activity.feed answers from the feed, the live entries with it
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	c := e.a.data.core
	c.SetRealtime(true)
	e.until("the socket", 20*time.Second, func() bool { return c.RTM != nil && c.RTM.Connected() })
	e.a.notify = &timedNotes{}
	c.SetExperimentGroup("unread_thread_indication_in_replybar", "on")
	e.a.focused = false
	return e, fs
}

// nextSecond waits for the clock's second after ts's: fakeslack numbers a
// thread's quiet replies from its root's second, so two roots of one
// second would give their first replies one ts.
func nextSecond(ts string) {
	sec, _ := strconv.ParseInt(strings.Split(ts, ".")[0], 10, 64)
	for time.Now().Unix() <= sec {
		time.Sleep(20 * time.Millisecond)
	}
}

func replyBarRows(e *perfEnv, ch string, roots ...string) *messages.ChannelView {
	e.until("the roots' rows", 20*time.Second, func() bool {
		cv := e.channelView(ch)
		if cv == nil {
			return false
		}
		for _, r := range roots {
			if _, _, ok := cv.ReplyBarState(r); !ok {
				return false
			}
		}
		return true
	})
	return e.channelView(ch)
}

// The socket drops; meanwhile the first thread is read in another client
// and the second gets an unread reply, so the server's count for the
// channel stays at 1 and nothing the socket would have said comes. After
// the slow reconnect the channel's thread activity is fetched again: the
// first thread's mark goes and the second's comes. Clack fetched it once
// per list, so the first thread read as unread for the rest of the
// session.
func TestReplyBarRefetchesAfterASlowReconnect(t *testing.T) {
	e, fs := replyBarEnv(t)
	c := e.a.data.core
	ch := fs.ChannelIDs()[2]
	fs.SetThreadsByChannel(map[string]int{ch: 1}) // client.counts: one unread thread in ch throughout
	replier := fs.UserIDs()[3]
	t1, _ := fs.PostQuiet(ch, fakeslack.SelfID, "a synthetic question, the first")
	nextSecond(t1)
	t2, _ := fs.PostQuiet(ch, fakeslack.SelfID, "a synthetic question, the second")
	fs.PostReplyQuiet(ch, t2, replier, "an earlier synthetic answer to the second, read") // its reply bar, no mark
	e.openChannel(ch)
	cv := replyBarRows(e, ch, t1, t2)
	r1 := fs.PushFollowedReplyLive(ch, t1, replier, "a synthetic answer to the first", false)
	e.until("the first thread's Unread mark", 20*time.Second, func() bool { _, u, _ := cv.ReplyBarState(t1); return u })

	fs.DropSockets()
	e.until("the socket down", 10*time.Second, func() bool { return !c.RTM.Connected() })
	r2 := fs.PostReplyQuiet(ch, t2, replier, "a synthetic answer to the second, unread")
	fs.UpsertActivity(fakeslack.ActivityThreadEntry(ch, t1, r1, "", 0)) // read in another client
	fs.UpsertActivity(fakeslack.ActivityThreadEntry(ch, t2, r2, r2, 1))
	e.until("the socket back", 30*time.Second, func() bool { return c.RTM.Connected() })
	if !e.untilSoft(20*time.Second, func() bool {
		_, u1, _ := cv.ReplyBarState(t1)
		_, u2, _ := cv.ReplyBarState(t2)
		return !u1 && u2
	}) {
		_, u1, _ := cv.ReplyBarState(t1)
		_, u2, _ := cv.ReplyBarState(t2)
		t.Errorf("after a slow reconnect the first (read) thread's mark is %v and the second (unread) thread's %v; want only the second's (the list fetched again)", u1, u2)
	}
}

// The first fetch of the channel's thread activity fails: once the
// channel's unread thread count moves, it is fetched again and the mark
// comes. Clack never fetched it again, so the channel showed no marks for
// the session.
func TestReplyBarRetriesAFailedFetch(t *testing.T) {
	e, fs := replyBarEnv(t)
	c := e.a.data.core
	ch := fs.ChannelIDs()[2]
	fs.SetThreadsByChannel(map[string]int{ch: 1})
	replier := fs.UserIDs()[3]
	root, _ := fs.PostQuiet(ch, fakeslack.SelfID, "a synthetic question the viewer asked")
	// failing from before the pane opens: the counts read at the socket's
	// first connect may answer after SetThreadsByChannel (the pane fetches
	// as it opens) or before it (at the reply's thread state)
	before := fs.CallCount("activity.feed")
	fs.SetFault("activity.feed", fakeslack.Fault{Error: "ratelimited"})
	e.openChannel(ch)
	cv := replyBarRows(e, ch, root)
	fs.PushFollowedReplyLive(ch, root, replier, "a synthetic answer", false)
	e.until("the failed fetch", 15*time.Second, func() bool { return fs.CallCount("activity.feed") > before })
	e.settle(300 * time.Millisecond)
	fs.SetFault("activity.feed", fakeslack.Fault{}) // the server answers again
	// another thread of ch gets an unread reply: the count moves to 2
	fs.SetThreadsByChannel(map[string]int{ch: 2})
	fs.Push(map[string]any{"type": "update_thread_state", "has_unreads": true, "mention_count": 0,
		"unread_count_by_channel": map[string]int{ch: 2}, "mention_count_by_channel": map[string]int{}, "org_wide": c.IsEnterprise()})
	if !e.untilSoft(20*time.Second, func() bool { _, u, _ := cv.ReplyBarState(root); return u }) {
		t.Errorf("after a failed fetch and a new count the reply bar has no Unread mark (activity.feed calls %d, %d before the reply)", fs.CallCount("activity.feed"), before)
	}
}

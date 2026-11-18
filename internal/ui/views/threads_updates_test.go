package views

// The Threads view between loads, against Slack's ThreadsView
// (client-threads-view 9Ra5) and the thunks around it (boot-shared k0ZD
// maybeRefreshAllThreads, client-boot 181D, boot-render VxbD): a thread
// marked unread and the divider, a slow socket reconnect, a refresh that
// can't join a card's replies, a load landing just before the next one
// starts, and the threads a reply names.

import (
	"context"
	"image"
	"net/url"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// thrReplyEvent is the socket's message event for a reply from grace.
func thrReplyEvent(ch, root, ts string) map[string]any {
	return map[string]any{"type": "message", "channel": ch, "user": "U0FAKE00002", "text": "a synthetic reply",
		"ts": ts, "thread_ts": root, "event_ts": ts}
}

// thrRefreshAfter pushes ev and waits for the realtime refresh it brings
// (the 5 s between refreshes waived) and the frame after it.
func thrRefreshAfter(t *testing.T, e *vaEnv, v *Threads, fs *fakeslack.Server, what string, ev map[string]any) {
	t.Helper()
	v.w.mu.Lock()
	v.w.last = time.Time{}
	v.w.mu.Unlock()
	n := fs.CallCount("subscriptions.thread.getView")
	fs.Push(ev)
	e.until(t, what, func() bool { return fs.CallCount("subscriptions.thread.getView") > n && !v.data.busy() })
	e.frame()
}

// Mark unread on the read thread under the divider (its message menu:
// core's MarkThreadUnread, Slack's threadsManuallyMarkedUnread) moves that
// thread's arrival last read to its new last read
// (getDerivedStateFromProps), so its reply from someone else is new and
// the divider goes. A reply that lands in it then leaves it unread, though
// it shows (maybeMarkNewMessageOnUpdate skips a thread marked unread).
func TestThreadsMarkedUnreadDividerLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	const before = "1762698000.000100" // the last read Mark unread on thrCReply leaves
	var replied atomic.Bool
	var marks thrMarkLog
	app, fs := vkFake(t, func(method string, f url.Values) (any, bool) {
		marks.note(method, f)
		if method != "subscriptions.thread.getView" {
			return nil, false
		}
		c := thrFixRead(ch, thrC, thrCReply, "U0FAKE00002", false, thrCReply)
		if replied.Load() {
			c = thrCount(thrFixRead(ch, thrC, thrLate, "U0FAKE00002", true, before), 2)
		}
		return thrOrderAnswer(thrFixRead(ch, thrA, thrAReply, "U0FAKE00002", true, thrA), c), true
	})
	v := NewThreads(newKBHost(app, false))
	e := thrViewEnv(t, v, 2)
	e.frame()
	if v.divAt != 1 {
		t.Fatalf("divider at %d after the load, want 1: before the read thread", v.divAt)
	}
	if err := app.MarkThreadUnread(context.Background(), ch, thrC, thrCReply); err != nil {
		t.Fatal(err)
	}
	// the server's thread_marked for the mark, as it sends every client
	fs.Push(map[string]any{"type": "thread_marked", "subscription": map[string]any{"type": "thread", "channel": ch,
		"thread_ts": thrC, "last_read": before, "active": true}, "event_ts": "1762699500.000100"})
	e.until(t, "the divider gone after Mark unread", func() bool { return v.divAt == -1 })
	replied.Store(true)
	thrRefreshAfter(t, e, v, fs, "the refresh after a reply in it", thrReplyEvent(ch, thrC, thrLate))
	e.frame()
	if marks.has(thrC + "@" + thrLate + " read=1") {
		t.Errorf("a reply in the thread marked unread marked it read (%v)", marks.all())
	}
	if v.divAt != -1 {
		t.Errorf("divider at %d after the reply, want -1", v.divAt)
	}
}

// A slow socket connect while the Threads view shows reloads it (k0ZD
// maybeRefreshAllThreads, then 181D incrementAllThreadsRefreshCounter and
// ThreadsView's reloadThreads): a followed thread the list didn't have
// that got a reply while the socket was down comes in with no event about
// it. A refresh alone puts on top only a thread an event named.
func TestThreadsSlowReconnectLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	var gap atomic.Bool
	app, fs := vkFake(t, func(method string, _ url.Values) (any, bool) {
		if method != "subscriptions.thread.getView" {
			return nil, false
		}
		ths := []map[string]any{thrFixRead(ch, thrA, thrAReply, "U0FAKE00002", true, thrA),
			thrFixRead(ch, thrC, thrCReply, fakeslack.SelfID, false, thrCReply)}
		if gap.Load() {
			ths = append(ths, thrFixRead(ch, thrD, thrDReply, "U0FAKE00002", true, thrD))
		}
		return thrOrderAnswer(ths...), true
	})
	v := NewThreads(newKBHost(app, false))
	e := thrViewEnv(t, v, 2)
	n, ws := fs.CallCount("subscriptions.thread.getView"), fs.CallCount("ws")
	gap.Store(true) // the reply in d while the socket is down: no event reaches the client
	fs.DropSockets()
	e.until(t, "the slow reconnect", func() bool { return fs.CallCount("ws") > ws })
	e.until(t, "the reload after it", func() bool { return fs.CallCount("subscriptions.thread.getView") > n && v.Settled() })
	e.frame()
	if got := thrRoots(v); len(got) != 3 || got[0] != thrD {
		t.Errorf("after the slow reconnect: %v, want %s first (the reload sorted afresh)", got, thrD)
	}
}

// A refresh whose window can't join a card's replies: three replies in
// one refresh's gap over getView's 2-reply window. The card shows the
// window alone with "Show 3 more replies", which brings the rest in order:
// every reply is drawn. A refresh whose window then moves back over the
// replies loaded (the newest deleted) draws each of them once.
func TestThreadsRefreshGapLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	const root = thrC
	rs := []string{"1762695100.000100", "1762695200.000100", "1762695300.000100", "1762695400.000100", "1762695500.000100"}
	reply := func(ts string) slack.Message {
		return slack.Message{Type: "message", Channel: ch, TS: ts, ThreadTS: root, User: "U0FAKE00002", Text: "a synthetic reply"}
	}
	rootMsg := func(count int, latest string) slack.Message {
		return slack.Message{Type: "message", Channel: ch, TS: root, ThreadTS: root, User: "U0FAKE00001", Text: "a synthetic root",
			ReplyCount: count, ReplyUsers: []string{"U0FAKE00002"}, LatestReply: latest, Subscribed: true, LastRead: latest}
	}
	thread := func(window []string, count int) map[string]any {
		var w []slack.Message
		for _, ts := range window {
			w = append(w, reply(ts))
		}
		return map[string]any{"root_msg": rootMsg(count, window[len(window)-1]), "latest_replies": w, "unread_replies": []slack.Message{}}
	}
	var state atomic.Int32 // 0: two replies, 1: five, 2: the fifth deleted
	app, fs := vkFake(t, func(method string, f url.Values) (any, bool) {
		switch method {
		case "subscriptions.thread.getView":
			c := thread(rs[:2], 2)
			switch state.Load() {
			case 1:
				c = thread(rs[3:5], 5)
			case 2:
				c = thread(rs[2:4], 4)
			}
			return thrOrderAnswer(thrFixRead(ch, thrA, thrAReply, "U0FAKE00002", true, thrA), c), true
		case "conversations.replies":
			if f.Get("ts") != root {
				return nil, false
			}
			msgs := []slack.Message{rootMsg(5, rs[4])}
			for _, ts := range rs {
				if !actTSLess(f.Get("latest"), ts) { // up to latest, inclusive
					msgs = append(msgs, reply(ts))
				}
			}
			return map[string]any{"ok": true, "messages": msgs, "has_more": false}, true
		}
		return nil, false
	})
	v := NewThreads(newKBHost(app, false))
	e := thrViewEnv(t, v, 2)
	key := ch + "/" + root
	shown := func() []string {
		p, _, _ := v.data.get()
		for i := range p.items {
			if p.items[i].key == key {
				return thrTSs(thrShown(&p.items[i], v.cards[key]))
			}
		}
		return nil
	}
	state.Store(1)
	thrRefreshAfter(t, e, v, fs, "the refresh after three replies", thrReplyEvent(ch, thrA, "1762693500.000100"))
	e.frame()
	if got := shown(); !slices.Equal(got, rs[3:]) {
		t.Fatalf("after three replies in one refresh the card shows %v, want the window alone %v (it can't join %v)", got, rs[3:], rs[:2])
	}
	c := v.cards[key]
	if c == nil || c.moreText == "" {
		t.Fatal("no \"Show N more replies\" row")
	}
	c.more.Click()
	e.until(t, "the older replies", func() bool { return slices.Equal(shown(), rs) })
	e.frame()
	for _, ts := range rs {
		if _, ok, _ := v.kb.top(key + "-" + ts); !ok {
			t.Errorf("reply %s isn't drawn", ts)
		}
	}
	state.Store(2)
	thrRefreshAfter(t, e, v, fs, "the refresh after a delete", thrReplyEvent(ch, thrA, "1762693600.000100"))
	if got := shown(); !slices.Equal(got, rs[:4]) {
		t.Errorf("after the window moved back the card shows %v, want %v, each once", got, rs[:4])
	}
}

// A load that sorts the list lands, and before the next frame settles it
// another load starts (the frame's staleness check or the next page come
// first). The landed load is settled first, its own mode: the mount's
// floating loader stops.
func TestThreadsSettleBeforeNextLoad(t *testing.T) {
	ch := fakeslack.ViewsChannel
	app, fs := vaFake(t)
	fs.SetHook(func(method string, _ url.Values) (any, bool) {
		if method != "subscriptions.thread.getView" {
			return nil, false
		}
		return thrOrderAnswer(thrFixRead(ch, thrA, thrAReply, "U0FAKE00002", true, thrA),
			thrFixRead(ch, thrC, thrCReply, fakeslack.SelfID, false, thrCReply)), true
	})
	v := NewThreads(newKBHost(app, false))
	e := thrViewEnv(t, v, 2)
	v.mounting = true
	v.reload()
	deadline := time.Now().Add(10 * time.Second)
	for v.data.busy() { // landed, no frame since
		if time.Now().After(deadline) {
			t.Fatal("the reload never landed")
		}
		time.Sleep(5 * time.Millisecond)
	}
	v.load(actRefresh)
	e.until(t, "the refresh", v.Settled)
	e.frame()
	if v.mounting {
		t.Error("the reload that landed before the refresh started was never settled: the floating loader stays")
	}
}

// A thread a reply names waits for the refresh that brings it, however
// many come first (Slack keeps the fetched thread and prepends it while
// the viewer has unread threads), until a load that sorts the list.
func TestThreadsNamedThreadWaitsLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	var calls atomic.Int32
	app, fs := vkFake(t, func(method string, _ url.Values) (any, bool) {
		if method != "subscriptions.thread.getView" {
			return nil, false
		}
		ths := []map[string]any{thrFixRead(ch, thrA, thrAReply, "U0FAKE00002", true, thrA),
			thrFixRead(ch, thrC, thrCReply, fakeslack.SelfID, false, thrCReply)}
		if calls.Add(1) > 5 { // the first load, then four refreshes without it
			ths = append(ths, thrFixRead(ch, thrD, thrDReply, "U0FAKE00002", true, thrD))
		}
		return thrOrderAnswer(ths...), true
	})
	v := NewThreads(newKBHost(app, false))
	e := thrViewEnv(t, v, 2)
	thrRefreshAfter(t, e, v, fs, "the refresh after the reply in d", thrReplyEvent(ch, thrD, thrDReply))
	for i := 0; i < 4; i++ {
		thrRefreshAfter(t, e, v, fs, "a refresh after a reply in a", thrReplyEvent(ch, thrA, "1762693500.000100"))
	}
	if got := thrRoots(v); len(got) != 3 || got[0] != thrD {
		t.Errorf("after the fifth refresh: %v, want %s on top", got, thrD)
	}
}

// thrFrameHost is kbHost with the window's frame count: the view shows
// only while it draws in the frames (shownTracker).
type thrFrameHost struct {
	*kbHost
	seq atomic.Uint64
}

func (h *thrFrameHost) FrameSeq() uint64 { return h.seq.Load() }

// While another page shows, a reply names no thread and doesn't make the
// view stale: Slack's ThreadsView is unmounted there, and the next visit
// loads afresh. thread_marked still reaches the view's read state.
func TestThreadsAwayIgnoresRepliesLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	app, fs := vkFake(t, func(method string, _ url.Values) (any, bool) {
		if method != "subscriptions.thread.getView" {
			return nil, false
		}
		return thrOrderAnswer(thrFixRead(ch, thrA, thrAReply, "U0FAKE00002", true, thrA),
			thrFixRead(ch, thrC, thrCReply, fakeslack.SelfID, false, thrCReply)), true
	})
	h := &thrFrameHost{kbHost: newKBHost(app, false)}
	v := NewThreads(h)
	e := &vaEnv{size: image.Pt(1000, 1100)}
	e.draw = func(gtx layout.Context) { h.seq.Add(1); v.Layout(gtx) }
	e.until(t, "the threads", func() bool { return len(thrRoots(v)) == 2 && !v.data.busy() })
	e.draw = func(layout.Context) { h.seq.Add(1) } // another page
	e.frame()
	e.frame()
	fs.Push(thrReplyEvent(ch, thrD, thrDReply))
	// a later event on the same socket: once it is in, the reply was seen
	fs.Push(map[string]any{"type": "thread_marked", "subscription": map[string]any{"type": "thread", "channel": ch,
		"thread_ts": thrA, "last_read": thrAReply, "active": true}, "event_ts": "1762699500.000100"})
	e.until(t, "the thread_marked after the reply", func() bool {
		v.mu.Lock()
		defer v.mu.Unlock()
		return v.marks[ch+"/"+thrA] == thrAReply
	})
	v.mu.Lock()
	_, named := v.named[ch+"/"+thrD]
	v.mu.Unlock()
	v.w.mu.Lock()
	stale := v.w.stale
	v.w.mu.Unlock()
	if named || stale {
		t.Errorf("a reply while another page shows: named %v, stale %v; want neither", named, stale)
	}
}

package views

// How Slack's Threads view clears the unreads it shows
// (client-threads-view 9Ra5): maybeMarkVisibleThreadAsRead (1946-1968)
// over get_rows_and_keys' unread rows (749-770, the ghost of 2337-2345),
// and maybeMarkNewMessageOnUpdate's guard for a reply that lands on
// screen.

import (
	"context"
	"image"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// thrUnreadRowOf is unreadRow for thread key as the view holds it.
func thrUnreadRowOf(v *Threads, key string) (int, bool) {
	p, _, _ := v.data.get()
	for i := range p.items {
		if p.items[i].key == key {
			c := v.cards[key]
			return v.unreadRow(&p.items[i], c, thrShown(&p.items[i], c))
		}
	}
	return thrNoUnreadRow, false
}

// count is how many times s was marked.
func (m *thrMarkLog) count(s string) (n int) {
	for _, x := range m.all() {
		if x == s {
			n++
		}
	}
	return n
}

// 350 ms after a load that sorts the list (loadThreads' setTimeout), a
// followed unread thread whose "unread" row is on screen is marked read
// at its newest reply, with the viewer's attention on the view; its row
// turns ghost ("unread_marked_read"). With the window unfocused nothing is
// marked, and the attention coming back marks it at once
// (componentDidUpdate). A thread never read (no last read) has no unread
// row and isn't marked.
func TestThreadsMarkVisibleLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	for _, tc := range []struct {
		name string
		away bool
	}{{"focused", false}, {"unfocused", true}} {
		t.Run(tc.name, func(t *testing.T) {
			var marks thrMarkLog
			var answered atomic.Pointer[time.Time] // the load's answer leaving the server: the view settles it later
			app, _ := vkFake(t, func(method string, f url.Values) (any, bool) {
				marks.note(method, f)
				if method != "subscriptions.thread.getView" {
					return nil, false
				}
				now := time.Now()
				answered.CompareAndSwap(nil, &now)
				return thrOrderAnswer(thrFixRead(ch, thrA, thrAReply, "U0FAKE00002", true, thrA), // read before: an unread row
					thrFixRead(ch, thrB, thrBReply, "U0FAKE00002", true, ""), // never read: none
					thrFixRead(ch, thrC, thrCReply, fakeslack.SelfID, false, thrCReply)), true
			})
			h := newKBHost(app, false)
			h.away.Store(tc.away)
			v := NewThreads(h)
			e := thrViewEnv(t, v, 3)
			landed := *answered.Load()
			e.frame() // settled: loadThreads' 350 ms start, in this frame or one thrViewEnv ran
			ka, kb := ch+"/"+thrA, ch+"/"+thrB
			markA, markB := thrA+"@"+thrAReply+" read=1", thrB+"@"+thrBReply+" read=1"
			if at, ghost := thrUnreadRowOf(v, ka); at < 0 || ghost {
				t.Fatalf("a thread read before: unread row %d (ghost %v), want one", at, ghost)
			}
			if at, _ := thrUnreadRowOf(v, kb); at != thrNoUnreadRow {
				t.Errorf("a thread never read has an unread row (%d)", at)
			}
			for time.Since(landed) < 330*time.Millisecond {
				e.frame()
				if v.read[ka] != "" {
					t.Fatalf("marked %v after the load; Slack waits 350 ms", time.Since(landed))
				}
				time.Sleep(10 * time.Millisecond)
			}
			if tc.away {
				if thrBounded(e, 600*time.Millisecond, func() bool { return marks.has(markA) }) {
					t.Fatal("marked with the window unfocused")
				}
				h.away.Store(false)
				e.frame() // the attention comes back
			}
			e.until(t, "the thread marked read", func() bool { return marks.has(markA) })
			e.frame()
			if at, ghost := thrUnreadRowOf(v, ka); at < 0 || !ghost {
				t.Errorf("after the mark: unread row %d (ghost %v), want the ghost", at, ghost)
			}
			if thrBounded(e, 400*time.Millisecond, func() bool { return marks.has(markB) }) || marks.count(markA) != 1 {
				t.Errorf("marks %v: want A's once and B's never", marks.all())
			}
		})
	}
}

// A scroll that brings an unread row on screen marks its thread read once
// the scroll is over (onListScroll, debounced 150 ms, sets the 350 ms
// timer); a thread whose row is off screen at the load isn't marked.
func TestThreadsMarkVisibleOnScrollLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	var marks thrMarkLog
	app, _ := vkFake(t, func(method string, f url.Values) (any, bool) {
		marks.note(method, f)
		if method != "subscriptions.thread.getView" {
			return nil, false
		}
		return thrOrderAnswer(thrFixRead(ch, thrA, thrAReply, "U0FAKE00002", true, thrA),
			thrFixRead(ch, thrB, thrBReply, "U0FAKE00002", true, thrB),
			thrFixRead(ch, thrC, thrCReply, fakeslack.SelfID, false, thrCReply)), true
	})
	v := NewThreads(newKBHost(app, false))
	e := thrViewEnv(t, v, 3)
	e.size = image.Pt(1000, 300) // a's card fills the list
	e.frame()
	markA, markB := thrA+"@"+thrAReply+" read=1", thrB+"@"+thrBReply+" read=1"
	e.until(t, "the thread on screen marked read", func() bool { return marks.has(markA) })
	if thrBounded(e, 600*time.Millisecond, func() bool { return marks.has(markB) }) {
		t.Fatal("a thread whose unread row is off screen was marked read")
	}
	v.list.Position.First, v.list.Position.Offset = 1, 0 // b's card at the top
	start := time.Now()
	e.until(t, "the thread scrolled on screen marked read", func() bool { return marks.has(markB) })
	if el := time.Since(start); el < 300*time.Millisecond {
		t.Errorf("marked %v after the scroll; Slack waits 350 ms after it", el)
	}
}

// A thread the viewer marked unread from the Threads view stays unread
// on screen (maybeMarkVisibleThreadAsRead skips THREADS_VIEW's marks); one
// marked unread in the thread pane is marked read again when the
// attention comes back.
func TestThreadsMarkVisibleSkipsOwnMarksLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	var marks thrMarkLog
	app, fs := vkFake(t, func(method string, f url.Values) (any, bool) {
		marks.note(method, f)
		if method != "subscriptions.thread.getView" {
			return nil, false
		}
		return thrOrderAnswer(thrFixRead(ch, thrA, thrAReply, "U0FAKE00002", true, thrA),
			thrFixRead(ch, thrC, thrCReply, fakeslack.SelfID, false, thrCReply)), true
	})
	h := newKBHost(app, false)
	v := NewThreads(h)
	e := thrViewEnv(t, v, 2)
	e.frame()
	key, markA := ch+"/"+thrA, thrA+"@"+thrAReply+" read=1"
	e.until(t, "the thread marked read after the load", func() bool { return marks.has(markA) })
	for _, tc := range []struct {
		origin core.ThreadMarkOrigin
		marked bool
	}{{core.MarkedInThreadsView, false}, {core.MarkedInThread, true}} {
		n := marks.count(markA)
		if err := app.MarkThreadUnreadFrom(context.Background(), ch, thrA, thrAReply, tc.origin); err != nil {
			t.Fatal(err)
		}
		fs.Push(map[string]any{"type": "thread_marked", "subscription": map[string]any{"type": "thread", "channel": ch,
			"thread_ts": thrA, "last_read": thrA, "active": true}, "event_ts": "1762699500.000100"})
		e.until(t, string(tc.origin)+": the thread unread again", func() bool { return v.read[key] == thrA })
		h.away.Store(true)
		e.frame()
		h.away.Store(false) // the attention comes back
		got := thrBounded(e, 400*time.Millisecond, func() bool { return marks.count(markA) > n })
		if got != tc.marked {
			t.Errorf("marked unread from %s: marked read again %v, want %v", tc.origin, got, tc.marked)
		}
	}
}

// A reply that lands on screen in a thread that came in unread marks it
// read only with its "unread" row on screen too, or its last read up to
// the replies before (maybeMarkNewMessageOnUpdate's guard): with that row
// scrolled above the list, the thread stays unread.
func TestThreadsSeenReplyGuardLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	const root = thrA
	rs := []string{"1762693100.000100", "1762693200.000100", "1762693300.000100", "1762693400.000100",
		"1762693500.000100", "1762693600.000100"}
	var n atomic.Int32
	n.Store(5)
	var marks thrMarkLog
	app, fs := vkFake(t, func(method string, f url.Values) (any, bool) {
		marks.note(method, f)
		if method != "subscriptions.thread.getView" {
			return nil, false
		}
		var win []slack.Message
		for _, ts := range rs[:n.Load()] {
			win = append(win, thrReplyBy(ts, "U0FAKE00002"))
		}
		return thrOrderAnswer(thrFixReplies(ch, root, len(win), root, win...)), true // every reply unread
	})
	h := newKBHost(app, false)
	h.away.Store(true) // markVisible doesn't take the thread first
	v := NewThreads(h)
	e := thrViewEnv(t, v, 1)
	e.size = image.Pt(1000, 400)
	e.frame()
	key := ch + "/" + root
	first := key + "-" + rs[0] // the unread row goes before it
	y, ok, _ := v.kb.top(first)
	if !ok {
		t.Fatal("the first unread reply isn't laid out")
	}
	v.list.Position.Offset = y + v.kb.height(first) + 1 // its row above the list
	e.frame()
	if v.kb.overlaps(first) {
		t.Fatal("the unread row is still on screen")
	}
	h.away.Store(false)
	e.frame()
	n.Store(6)
	thrRefreshAfter(t, e, v, fs, "the refresh after a reply", thrReplyEvent(ch, root, rs[5]))
	if !v.kb.overlaps(key + "-" + rs[5]) {
		t.Fatal("the new reply isn't on screen")
	}
	if thrBounded(e, 300*time.Millisecond, func() bool {
		for _, m := range marks.all() {
			if strings.HasPrefix(m, root+"@") && strings.HasSuffix(m, "read=1") {
				return true
			}
		}
		return false
	}) {
		t.Errorf("a reply on screen marked the thread read with its unread row above the list (%v)", marks.all())
	}
}

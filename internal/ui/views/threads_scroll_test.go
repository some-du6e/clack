package views

// When Slack's Threads view checks what it shows (client-threads-view
// 9Ra5, maybeMarkVisibleThreadAsRead 1946-1968): onListScrolledDebounce is
// jp8I debounceRAF(onListScroll, 150), which calls only once the list has
// been still 150 ms, and onListScroll's setTimeout(…, 350) then marks what
// shows; the unread row is a divider of no height at its reply's top; a
// remount's new list scrolled nowhere; every change of the viewer's
// attention is a render, drawn or not; and the ghost divider mounts with
// --ghost on it, at half opacity.

import (
	"context"
	"image"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
)

// thrPointIn is getVisibleRange's test for the unread divider before the
// row key: a row of no height at its top, in Layout.getBounds' range from
// just past the viewport's top to its bottom.
func thrPointIn(v *Threads, key string) bool {
	y, ok, _ := v.kb.top(key)
	return ok && y > 0 && y <= v.kb.viewH
}

// thrFling moves the list dy px a frame, a frame about every 16 ms, while
// more holds, and returns when the last frame that moved it began.
func thrFling(t *testing.T, e *vaEnv, v *Threads, dy int, more func() bool) time.Time {
	t.Helper()
	var last time.Time
	for deadline := time.Now().Add(10 * time.Second); more(); {
		if time.Now().After(deadline) {
			t.Fatal("the fling never got where it was going")
		}
		p := v.list.Position
		v.list.Position.Offset += dy
		at := time.Now()
		e.frame()
		if q := v.list.Position; q.First != p.First || q.Offset != p.Offset {
			last = at
		}
		time.Sleep(16 * time.Millisecond)
	}
	return last
}

// A fling marks nothing while it moves: Slack's scroll check runs only
// once the list has been still 150 ms (jp8I debounceRAF is trailing only),
// then 350 ms on, over what shows then. A thread whose unread row the
// fling carried through the list and on past stays unread, however long
// the row showed; one the list stops on is marked 500 ms or more after the
// last move.
func TestThreadsMarkVisibleFlingLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	var marks thrMarkLog
	app, _ := vkFake(t, func(method string, f url.Values) (any, bool) {
		marks.note(method, f)
		if method != "subscriptions.thread.getView" {
			return nil, false
		}
		return thrOrderAnswer(thrFixRead(ch, thrA, thrAReply, "U0FAKE00002", true, thrA),
			thrFixRead(ch, thrB, thrBReply, "U0FAKE00002", true, thrB),
			thrFixRead(ch, thrC, thrCReply, fakeslack.SelfID, false, thrCReply),
			thrFixRead(ch, thrE, thrEReply, "U0FAKE00002", false, thrEReply),
			thrFixRead(ch, thrX, thrXReply, "U0FAKE00002", false, thrXReply)), true
	})
	v := NewThreads(newKBHost(app, false))
	e := thrViewEnv(t, v, 5)
	e.size = image.Pt(1000, 300) // a's card fills the list
	e.frame()
	markA, markB := thrA+"@"+thrAReply+" read=1", thrB+"@"+thrBReply+" read=1"
	e.until(t, "the thread on screen marked read after the load", func() bool { return marks.has(markA) })
	thrBounded(e, 400*time.Millisecond, func() bool { return false })
	rowB := ch + "/" + thrB + "-" + thrBReply // b's unread row goes before its reply
	if thrPointIn(v, rowB) || marks.has(markB) {
		t.Fatal("b's unread row is on screen before the fling")
	}
	n := len(marks.all())
	// down to b's unread row, over it slowly for half a second, then on
	// past it: never 150 ms still
	thrFling(t, e, v, 40, func() bool { return !thrPointIn(v, rowB) })
	crept := time.Now()
	thrFling(t, e, v, 1, func() bool { return time.Since(crept) < 500*time.Millisecond })
	if !thrPointIn(v, rowB) {
		t.Fatal("b's unread row left the list while the fling crept over it")
	}
	past := 0
	last := thrFling(t, e, v, 40, func() bool {
		if !thrPointIn(v, rowB) {
			past++
		}
		return past < 4
	})
	if y, ok, above := v.kb.top(rowB); ok && y >= 0 || !ok && !above {
		t.Fatalf("the fling stopped with b's unread row at %d (laid out %v), want it above the list", y, ok)
	}
	if got := marks.all(); len(got) != n {
		t.Fatalf("marked %v while the list moved; Slack checks only once it has been still 150 ms", got[n:])
	}
	if thrBounded(e, time.Until(last.Add(900*time.Millisecond)), func() bool { return marks.has(markB) }) {
		t.Fatal("b marked read: its unread row showed only while the fling went by")
	}
	// the list stops on b
	v.list.Position = layout.Position{First: 1}
	moved := time.Now()
	e.until(t, "the thread the list stopped on marked read", func() bool { return marks.has(markB) })
	if el := time.Since(moved); el < 490*time.Millisecond {
		t.Errorf("marked %v after the list stopped; Slack waits 150 ms still, then 350 ms", el)
	}
}

// The unread row is the divider itself, of no height at the top of its
// reply (.c-message_list__unread_divider{height:0}): with a tall unread
// reply's top scrolled above the list, its thread isn't marked however
// much of the reply shows, and it is once the reply's top is back on the
// list.
func TestThreadsUnreadRowPointLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	const root, tall = thrA, thrAReply
	long := strings.Repeat("a synthetic line of a long reply\n", 40)
	var marks thrMarkLog
	app, _ := vkFake(t, func(method string, f url.Values) (any, bool) {
		marks.note(method, f)
		if method != "subscriptions.thread.getView" {
			return nil, false
		}
		r := thrReplyBy(tall, "U0FAKE00002")
		r.Text = long
		return thrOrderAnswer(thrFixReplies(ch, root, 1, root, r)), true
	})
	h := newKBHost(app, false)
	h.away.Store(true) // nothing marked while the list is put in place
	v := NewThreads(h)
	e := thrViewEnv(t, v, 1)
	e.size = image.Pt(1000, 400)
	e.frame()
	row := ch + "/" + root + "-" + tall
	y, ok, _ := v.kb.top(row)
	if !ok || v.kb.height(row) < 600 {
		t.Fatalf("the tall reply: laid out %v, %dpx high; want over 600", ok, v.kb.height(row))
	}
	v.list.Position.Offset += y + 60 // its top 60px above the list
	e.frame()
	if y, ok, _ := v.kb.top(row); !ok || y >= 0 || !v.kb.overlaps(row) {
		t.Fatalf("the reply at %d (laid out %v): want its top above the list and the rest on it", y, ok)
	}
	thrBounded(e, 600*time.Millisecond, func() bool { return false }) // the scroll's check, unattended
	h.away.Store(false)                                               // the attention back: a check over what shows
	mark := root + "@" + tall + " read=1"
	if thrBounded(e, 900*time.Millisecond, func() bool { return marks.has(mark) }) {
		t.Fatal("marked read with its unread row (the reply's top) above the list")
	}
	v.list.Position.Offset -= 100 // the reply's top 40px down the list
	e.frame()
	e.until(t, "the thread marked once its reply's top shows", func() bool { return marks.has(mark) })
}

// A remount's new list is at its top and scrolled nowhere
// (componentDidMount builds it with no scroll event): its first check is
// loadThreads' 350 ms after the answer, so with the mount's load slow the
// store's threads it shows meanwhile aren't marked.
func TestThreadsRemountNoScrollLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	var marks thrMarkLog
	var g thrGate
	app, fs := vkFake(t, func(method string, f url.Values) (any, bool) {
		marks.note(method, f)
		if method != "subscriptions.thread.getView" {
			return nil, false
		}
		g.wait()
		return thrOrderAnswer(thrFixRead(ch, thrA, thrAReply, "U0FAKE00002", true, thrA),
			thrFixRead(ch, thrB, thrBReply, "U0FAKE00002", true, thrB),
			thrFixRead(ch, thrC, thrCReply, fakeslack.SelfID, false, thrCReply)), true
	})
	t.Cleanup(g.open) // before the fake's Close, which waits for a held answer
	h := newKBHost(app, false)
	h.away.Store(true) // nothing marked before the remount
	v := NewThreads(h)
	e := thrViewEnv(t, v, 3)
	e.size = image.Pt(1000, 300)
	e.frame()
	v.list.Position = layout.Position{First: 1}
	e.frame()
	thrBounded(e, 600*time.Millisecond, func() bool { return false }) // the scroll's check, unattended
	v.PageLeft()
	// back from another page, the window attended, the mount's load slow
	h.away.Store(false)
	g.hold()
	n := fs.CallCount("subscriptions.thread.getView")
	e.frame() // the remount
	e.until(t, "the mount's load", func() bool { return fs.CallCount("subscriptions.thread.getView") > n })
	if p := v.list.Position; p.First != 0 || p.Offset != 0 {
		t.Fatalf("the new list at %+v, want its top", p)
	}
	markA := thrA + "@" + thrAReply + " read=1"
	if thrBounded(e, 900*time.Millisecond, func() bool { return marks.has(markA) }) {
		t.Fatal("marked while the mount's load was out: the new list's top counted as a scroll")
	}
	g.open()
	e.until(t, "the thread marked after the mount's answer", func() bool { return marks.has(markA) })
}

// thrAttHost is kbHost counting the times the viewer's attention went
// (AttentionLost), as the App does when the window loses the focus or an
// overlay opens.
type thrAttHost struct {
	*kbHost
	lost atomic.Uint64
}

func (h *thrAttHost) AttentionLost() uint64 { return h.lost.Load() }

// Every change of the viewer's attention is a render in Slack
// (componentDidUpdate compares each with the one before): a window
// minimized and restored with no frame drawn between still marks the
// unread rows on screen when the attention comes back.
func TestThreadsAttentionAfterMinimizeLikeSlack(t *testing.T) {
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
	h := &thrAttHost{kbHost: newKBHost(app, false)}
	v := NewThreads(h)
	e := thrViewEnv(t, v, 2)
	e.frame()
	key, markA := ch+"/"+thrA, thrA+"@"+thrAReply+" read=1"
	e.until(t, "the thread marked read after the load", func() bool { return marks.has(markA) })
	// marked unread in the thread pane: nothing checks the view again
	n := marks.count(markA)
	if err := app.MarkThreadUnreadFrom(context.Background(), ch, thrA, thrAReply, core.MarkedInThread); err != nil {
		t.Fatal(err)
	}
	fs.Push(map[string]any{"type": "thread_marked", "subscription": map[string]any{"type": "thread", "channel": ch,
		"thread_ts": thrA, "last_read": thrA, "active": true}, "event_ts": "1762699500.000100"})
	e.until(t, "the thread unread again", func() bool { return v.read[key] == thrA })
	if thrBounded(e, 400*time.Millisecond, func() bool { return marks.count(markA) > n }) {
		t.Fatal("marked read again with nothing to check the view")
	}
	// minimized and restored: the attention went and came back, no frame
	h.away.Store(true)
	h.lost.Add(1)
	h.away.Store(false)
	if !thrBounded(e, 600*time.Millisecond, func() bool { return marks.count(markA) > n }) {
		t.Error("the attention back after a minimize marked nothing; Slack's componentDidUpdate saw it go")
	}
}

// The divider of a thread marked read ("unread_marked_read") mounts with
// --ghost on it, so its line is at half opacity from its first frame (the
// rule's opacity .1s 3s transition never runs on a new element) and never
// asks for frames; it keeps its "New". A thread never read draws no line.
func TestThreadsGhostLineDrawnLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	var marks thrMarkLog
	app, _ := vkFake(t, func(method string, f url.Values) (any, bool) {
		marks.note(method, f)
		if method != "subscriptions.thread.getView" {
			return nil, false
		}
		return thrOrderAnswer(thrFixRead(ch, thrA, thrAReply, "U0FAKE00002", true, thrA),
			thrFixRead(ch, thrB, thrBReply, "U0FAKE00002", true, ""),
			thrFixRead(ch, thrC, thrCReply, fakeslack.SelfID, false, thrCReply)), true
	})
	v := NewThreads(newKBHost(app, false))
	e := thrViewEnv(t, v, 3)
	e.frame()
	ka, kb := ch+"/"+thrA, ch+"/"+thrB
	red := v.th.C.NewLine
	if l, ok := v.lines[ka]; !ok || l.ghost || l.col != red {
		t.Fatalf("a's line before the mark: %+v (drawn %v), want %v", l, ok, red)
	}
	e.until(t, "the thread marked read", func() bool { return marks.has(thrA + "@" + thrAReply + " read=1") })
	strs := drawnStrings(e) // the frame after the mark
	half := uint8(float32(red.A) * 0.5)
	l, ok := v.lines[ka]
	if !ok || !l.ghost || l.col.A != half || l.col.R != red.R || l.col.G != red.G || l.col.B != red.B {
		t.Fatalf("the frame after the mark drew a's line %+v (drawn %v), want the ghost at alpha %d", l, ok, half)
	}
	if !slices.Contains(strs, nsMessages.T("New")) {
		t.Error("the ghost divider lost its New")
	}
	if _, ok := v.lines[kb]; ok {
		t.Error("a thread never read drew a New line")
	}
	for end := time.Now().Add(3300 * time.Millisecond); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		e.frame()
	}
	if l := v.lines[ka]; l.col.A != half {
		t.Errorf("3.3 s on, the ghost's line has alpha %d, want %d", l.col.A, half)
	}
	if thrAnimating(e) {
		t.Error("the ghost divider still asks for frames")
	}
}

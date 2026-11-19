package views

// A card's reply box and the keyboard in Slack's Threads view
// (client-threads-view 9Ra5): a footer row's focus hands the focus to its
// reply box (onItemFocusEnter, focusInput), whose onFocus is
// onFooterInputFocus (1521-1542). That marks a thread read only when the
// viewer marked it unread by hand and follows it. The threads the
// keyboard passes are marked by the check over what shows once the list
// is still, whose unread row counts in Layout.getBounds' range at
// 0 < y <= viewH; a check that falls due while no list is drawn is spent.

import (
	"context"
	"image"
	"net/url"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/store"
)

// ↓ held through the list, a key every ~30 ms from the first heading past
// the third footer: each footer's reply box opens and hands the focus on
// (focusItemBelowInput), and nothing is marked while the list moves.
// Once it has been still 500 ms, the threads whose unread row shows are
// marked, and only those.
func TestThreadsKeysHeldDownLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	var marks thrMarkLog
	app, _ := vkFake(t, func(method string, f url.Values) (any, bool) {
		marks.note(method, f)
		if method != "subscriptions.thread.getView" {
			return nil, false
		}
		return thrOrderAnswer(thrFixRead(ch, thrA, thrAReply, "U0FAKE00002", true, thrA),
			thrFixRead(ch, thrB, thrBReply, "U0FAKE00002", true, thrB),
			thrFixRead(ch, thrE, thrEReply, "U0FAKE00002", true, thrE),
			thrFixRead(ch, thrC, thrCReply, fakeslack.SelfID, false, thrCReply),
			thrFixRead(ch, thrX, thrXReply, "U0FAKE00002", false, thrXReply)), true
	})
	v := NewThreads(newKBHost(app, false))
	e := thrViewEnv(t, v, 5)
	e.size = image.Pt(1000, 400)
	e.frame()
	ka, kc := ch+"/"+thrA, ch+"/"+thrC
	e.until(t, "the thread on screen marked read after the load", func() bool { return marks.has(thrA + "@" + thrAReply + " read=1") })
	thrBounded(e, 300*time.Millisecond, func() bool { return false })
	n := len(marks.all())
	kbClickRow(t, e, v.kb, "heading-"+ka, 49)
	if v.kb.active != "heading-"+ka || !v.kb.focused {
		t.Fatalf("after a press on a's heading: active %q, focused %v", v.kb.active, v.kb.focused)
	}
	var last time.Time
	for deadline := time.Now().Add(15 * time.Second); v.kb.active != "heading-"+kc; {
		if time.Now().After(deadline) {
			t.Fatalf("↓ held never got past the third footer (active %q)", v.kb.active)
		}
		p := v.list.Position
		at := time.Now()
		e.r.Queue(key.Event{Name: key.NameDownArrow, State: key.Press}, key.Event{Name: key.NameDownArrow, State: key.Release})
		e.frame()
		e.frame()
		if q := v.list.Position; q.First != p.First || q.Offset != p.Offset {
			last = at
		}
		if got := marks.all(); len(got) != n {
			t.Fatalf("marked %v while ↓ moved through the list; Slack's footer focus marks only a thread marked unread by hand", got[n:])
		}
		time.Sleep(25 * time.Millisecond)
	}
	// each footer handed the focus to its reply box (the list's active row
	// goes while the box has it)
	for _, k := range []string{ka, ch + "/" + thrB, ch + "/" + thrE} {
		if c := v.cards[k]; c == nil || c.comp == nil {
			t.Fatalf("↓ through %s's footer opened no reply box", k)
		}
	}
	if last.IsZero() {
		t.Fatal("the list never moved")
	}
	if thrBounded(e, time.Until(last.Add(480*time.Millisecond)), func() bool { return len(marks.all()) != n }) {
		t.Fatalf("marked %v less than 500 ms after the list last moved", marks.all()[n:])
	}
	thrBounded(e, time.Until(last.Add(900*time.Millisecond)), func() bool { return false })
	var want []string
	p, _, _ := v.data.get()
	for i := range p.items {
		it := &p.items[i]
		if it.key == ka || !v.followed(it) {
			continue
		}
		c := v.cards[it.key]
		replies := thrShown(it, c)
		if at, ghost := v.unreadRow(it, c, replies); at >= 0 && !ghost && thrPointIn(v, it.key+"-"+replies[at].TS) {
			want = append(want, it.Root.TS+"@"+it.latest()+" read=1")
		}
	}
	got := marks.all()[n:]
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("once the list was still: marked %v, want %v (the threads whose unread row shows)", got, want)
	}
}

// A card's reply box taking the focus marks its thread read only when the
// viewer marked it unread by hand and follows it (onFooterInputFocus), at
// its newest reply, and the hand mark goes; an open box does so on its
// next focus. Any other thread's box marks nothing, and a hand-marked
// thread nobody follows keeps its mark.
func TestThreadsFooterFocusLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	ka, kb := ch+"/"+thrA, ch+"/"+thrB
	markA, markB := thrA+"@"+thrAReply+" read=1", thrB+"@"+thrBReply+" read=1"
	env := func(t *testing.T) (*core.App, *Threads, *vaEnv, *thrMarkLog, func(key string) *thrCard) {
		t.Helper()
		marks := &thrMarkLog{}
		app, _ := vkFake(t, func(method string, f url.Values) (any, bool) {
			marks.note(method, f)
			if method != "subscriptions.thread.getView" {
				return nil, false
			}
			return thrOrderAnswer(thrFixRead(ch, thrA, thrAReply, "U0FAKE00002", true, thrA),
				thrFixRead(ch, thrB, thrBReply, "U0FAKE00002", true, thrB),
				thrFixRead(ch, thrC, thrCReply, fakeslack.SelfID, false, thrCReply)), true
		})
		h := newKBHost(app, false)
		h.away.Store(true) // the visible threads' check marks nothing: only the boxes do
		v := NewThreads(h)
		e := thrViewEnv(t, v, 3)
		e.frame()
		open := func(key string) *thrCard {
			t.Helper()
			c := v.cards[key]
			c.reply.Click()
			e.until(t, "the reply box of "+key, func() bool { return c.comp != nil && c.comp.Focused() })
			return c
		}
		return app, v, e, marks, open
	}
	ctx := context.Background()
	t.Run("not marked by hand", func(t *testing.T) {
		_, _, e, marks, open := env(t)
		open(ka)
		if thrBounded(e, 400*time.Millisecond, func() bool { return marks.has(markA) }) {
			t.Error("the reply box of a thread nobody marked unread marked it read")
		}
	})
	t.Run("marked by hand, its open box focused again", func(t *testing.T) {
		app, v, e, marks, open := env(t)
		c := open(ka)
		thrBounded(e, 300*time.Millisecond, func() bool { return false })
		n := marks.count(markA)
		if err := app.MarkThreadUnreadFrom(ctx, ch, thrA, thrAReply, core.MarkedInThreadsView); err != nil {
			t.Fatal(err)
		}
		v.kb.focusOn("heading-" + ka)
		e.until(t, "the list's focus back", func() bool { return v.kb.focused && !c.comp.Focused() })
		c.comp.Focus()
		e.until(t, "the hand-marked thread marked read on its box's focus", func() bool { return marks.count(markA) > n })
		if o := app.ThreadMarkedUnread(ch, thrA); o != "" {
			t.Errorf("after its box's focus the thread is still marked unread from %q", o)
		}
		if got := marks.count(markA) - n; got != 1 {
			t.Errorf("marked %d times on one focus, want once", got)
		}
	})
	t.Run("marked by hand, nobody follows", func(t *testing.T) {
		app, _, e, marks, open := env(t)
		app.S.SetThreadSub(ch, thrB, func(s *store.ThreadSub) { s.Subscribed = false })
		if err := app.MarkThreadUnreadFrom(ctx, ch, thrB, thrBReply, core.MarkedInThread); err != nil {
			t.Fatal(err)
		}
		open(kb)
		if thrBounded(e, 400*time.Millisecond, func() bool { return marks.has(markB) }) {
			t.Error("the box of a thread nobody follows marked it read")
		}
		if o := app.ThreadMarkedUnread(ch, thrB); o != core.MarkedInThread {
			t.Errorf("the box of a thread nobody follows took its hand mark (%q)", o)
		}
	})
}

// The unread row's point counts in Layout.getBounds' range from just past
// the viewport's top to its bottom (0 < y <= viewH): at the top, where
// keyboard focus scrolls a row (scrollToKey), the divider sits before the
// range's first row; at the bottom it is the last.
func TestThreadsUnreadRowEdgesLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	for _, tc := range []struct {
		name   string
		y      func(viewH int) int
		marked bool
	}{
		{"at the top", func(int) int { return 0 }, false},
		{"a pixel down", func(int) int { return 1 }, true},
		{"at the bottom", func(h int) int { return h }, true},
		{"a pixel below", func(h int) int { return h + 1 }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			h := newKBHost(app, false)
			h.away.Store(true) // nothing marked while the row is put in place
			v := NewThreads(h)
			e := thrViewEnv(t, v, 3)
			e.size = image.Pt(1000, 400)
			e.frame()
			row := ch + "/" + thrB + "-" + thrBReply    // b's unread row goes before its reply
			v.list.Position = layout.Position{First: 1} // b's card laid out
			e.frame()
			want := tc.y(v.kb.viewH)
			for range 4 {
				y, ok, _ := v.kb.top(row)
				if !ok {
					t.Fatalf("b's reply isn't laid out (position %+v)", v.list.Position)
				}
				if y == want {
					break
				}
				v.list.Position.Offset += y - want
				e.frame()
			}
			if y, _, _ := v.kb.top(row); y != want {
				t.Fatalf("b's reply at %d, want %d (the list %dpx high)", y, want, v.kb.viewH)
			}
			thrBounded(e, 600*time.Millisecond, func() bool { return false }) // the scroll's check, unattended
			h.away.Store(false)                                               // the attention back: a check over what shows
			mark := thrB + "@" + thrBReply + " read=1"
			if got := thrBounded(e, 900*time.Millisecond, func() bool { return marks.has(mark) }); got != tc.marked {
				t.Errorf("the unread row at y=%d of %d: marked %v, want %v", want, v.kb.viewH, got, tc.marked)
			}
		})
	}
}

// A mark check that falls due while no list is drawn finds none and is
// spent, as maybeMarkVisibleThreadAsRead returns without a listRef: a
// scroll's check that comes due under a failed reload's error view doesn't
// run when Try Again brings the list back. The next check is loadThreads'
// 350 ms after its answer.
func TestThreadsChecksSpentWithoutListLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	var marks thrMarkLog
	var g thrGate
	var answered atomic.Pointer[time.Time]
	app, fs := vkFake(t, func(method string, f url.Values) (any, bool) {
		marks.note(method, f)
		if method != "subscriptions.thread.getView" {
			return nil, false
		}
		g.wait()
		now := time.Now()
		answered.Store(&now)
		return thrOrderAnswer(thrFixRead(ch, thrA, thrAReply, "U0FAKE00002", true, thrA),
			thrFixRead(ch, thrC, thrCReply, fakeslack.SelfID, false, thrCReply)), true
	})
	t.Cleanup(g.open) // before the fake's Close, which waits for a held answer
	h := newKBHost(app, false)
	h.away.Store(true)
	v := NewThreads(h)
	e := thrViewEnv(t, v, 2)
	e.size = image.Pt(1000, 300)
	e.frame()
	v.list.Position = layout.Position{First: 1} // a's unread row above the list
	e.frame()
	thrBounded(e, 600*time.Millisecond, func() bool { return false })
	h.away.Store(false)
	markA := thrA + "@" + thrAReply + " read=1"
	if thrBounded(e, 400*time.Millisecond, func() bool { return marks.has(markA) }) {
		t.Fatal("marked with its unread row off the list")
	}
	// the list brings a's unread row on, and before the scroll's check is
	// due a reload fails: the error view shows past the check's time
	v.list.Position = layout.Position{}
	e.frame()
	fs.SetFault("subscriptions.thread.getView", fakeslack.Fault{Error: "fatal_error", Times: 1})
	v.Reselect()
	errText := nsThreads.T(thrErrText)
	e.until(t, "the error view", func() bool { return v.Settled() && slices.Contains(drawnStrings(e), errText) })
	thrBounded(e, 700*time.Millisecond, func() bool { return false })
	if marks.has(markA) {
		t.Fatal("marked while the error view showed")
	}
	// Try Again brings the list back with its load held
	g.hold()
	e.click(thrFind(t, e, &v.retry))
	if thrBounded(e, 600*time.Millisecond, func() bool { return marks.has(markA) }) {
		t.Fatal("marked before Try Again's answer: a check that fell due with no list ran once the list was back")
	}
	g.open()
	e.until(t, "Try Again's answer", func() bool { return v.Settled() && answered.Load() != nil })
	e.until(t, "the mark after Try Again's answer", func() bool { return marks.has(markA) })
	if el := time.Since(*answered.Load()); el < 330*time.Millisecond {
		t.Errorf("marked %v after Try Again's answer; Slack waits 350 ms", el)
	}
}

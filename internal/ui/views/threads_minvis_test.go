package views

// A card's first shown reply on a new refreshId, Slack's
// getDefaultMinimumVisibleTs (iPpN) through getDerivedStateFromProps'
// minimumVisibleTsByThread.

import (
	"fmt"
	"image"
	"net/url"
	"slices"
	"sync/atomic"
	"testing"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// Three threads of seven replies, getView's window the three newest: one
// read to its newest shows its two newest and "Show 5 more replies"; one
// read to its second newest shows from that reply on; one never read
// shows every reply the card has until it is marked read, then its two
// newest (iPpN picked again while it was "").
func TestThreadsDefaultFirstReplyLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	type th struct{ root, r5, r6, r7 string }
	p := th{"1762620000.000100", "1762620500.000100", "1762620600.000100", "1762620700.000100"}
	q := th{"1762610000.000100", "1762610500.000100", "1762610600.000100", "1762610700.000100"}
	r := th{"1762600000.000100", "1762600500.000100", "1762600600.000100", "1762600700.000100"}
	var marks thrMarkLog
	app, _ := vkFake(t, func(method string, f url.Values) (any, bool) {
		marks.note(method, f)
		if method != "subscriptions.thread.getView" {
			return nil, false
		}
		fix := func(x th, lastRead string) map[string]any {
			return thrFixReplies(ch, x.root, 7, lastRead, thrReplyBy(x.r5, "U0FAKE00002"), thrReplyBy(x.r6, "U0FAKE00001"), thrReplyBy(x.r7, "U0FAKE00002"))
		}
		return thrOrderAnswer(fix(p, p.r7), fix(q, q.r6), fix(r, r.root)), true
	})
	h := newKBHost(app, false)
	h.away.Store(true) // nothing marked read until the last part
	v := NewThreads(h)
	e := thrViewEnv(t, v, 3)
	e.frame()
	e.frame()
	shown := func(x th) []string {
		key := ch + "/" + x.root
		d, _, _ := v.data.get()
		return thrTSs(thrShown(thrByKey(d.items, key), v.cards[key]))
	}
	if got, want := shown(p), []string{p.r6, p.r7}; !slices.Equal(got, want) {
		t.Errorf("a thread read to its newest reply shows %v, want its two newest %v", got, want)
	}
	c := v.cards[ch+"/"+p.root]
	if c == nil {
		t.Fatal("the thread read to its newest reply has no card")
	}
	if c.moreText != "Show 5 more replies" {
		t.Errorf("its row says %q, want Show 5 more replies", c.moreText)
	}
	if got, want := shown(q), []string{q.r6, q.r7}; !slices.Equal(got, want) {
		t.Errorf("a thread read to its second newest shows %v, want from it on %v", got, want)
	}
	if got, want := shown(r), []string{r.r5, r.r6, r.r7}; !slices.Equal(got, want) {
		t.Errorf("a thread never read shows %v, want every reply it has %v", got, want)
	}
	h.away.Store(false) // the attention back: r, on screen and unread, is marked read
	e.until(t, "r marked read", func() bool { return marks.has(r.root + "@" + r.r7 + " read=1") })
	e.frame()
	if got, want := shown(r), []string{r.r6, r.r7}; !slices.Equal(got, want) {
		t.Errorf("once read, the thread shows %v, want its two newest %v", got, want)
	}
	if got, want := shown(q), []string{q.r6, q.r7}; !slices.Equal(got, want) {
		t.Errorf("q's first shown reply moved after its mark: %v, want %v", got, want)
	}
}

// thrViewEnvAt is thrViewEnv opened straight at size, so no card below the
// fold there is ever laid out. thrViewEnv opens at 1100px, where more of
// them may be, but promises none: its until returns once the answer is
// in, maybe before a frame draws it. A test that needs a card laid out
// scrolls to it.
func thrViewEnvAt(t *testing.T, v *Threads, n int, size image.Point) *vaEnv {
	t.Helper()
	e := &vaEnv{size: size}
	e.draw = func(gtx layout.Context) { v.Layout(gtx) }
	e.until(t, fmt.Sprintf("%d threads", n), func() bool { return len(thrRoots(v)) == n && !v.data.busy() })
	return e
}

// Each thread's first shown reply is picked when the threads load, for
// the threads below the fold too (getDerivedStateFromProps runs iPpN over
// props.threads, laid out or not), so what lands on a thread before the
// viewer scrolls to it doesn't move it. A read thread of ten replies (r8,
// r9 and r10 held) starts at r9; a reply r11 that lands before its card is
// laid out shows with r9 and r10. A thread unread at load from r6 starts
// at r6; read elsewhere before its card is laid out, it still shows r6 to
// r10, not its two newest.
func TestThreadsBelowFoldFirstReplyLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	// not the views fixture's own unread thread (1762600000.000100), whose
	// subscription the store holds
	const root = "1762610000.000100"
	r := func(n int) string { return fmt.Sprintf("176261%02d00.000100", n) } // r(6) is 1762610600.000100
	key := ch + "/" + root
	fillers := func(unread bool) []map[string]any {
		var ths []map[string]any
		for _, th := range thrBannerThreads[:3] {
			lr := th[1]
			if unread {
				lr = th[0]
			}
			ths = append(ths, thrFixRead(ch, th[0], th[1], "U0FAKE00002", unread, lr))
		}
		return ths
	}
	scrollTo := func(t *testing.T, e *vaEnv, v *Threads) []string {
		t.Helper()
		if v.cards[key] != nil {
			t.Fatal("the thread's card was laid out before it was scrolled to")
		}
		v.list.Position = layout.Position{First: 3}
		e.frame()
		e.frame()
		if v.cards[key] == nil {
			t.Fatal("the thread's card isn't laid out once scrolled to")
		}
		return thrTSs(thrShown(thrItemOf(v, key), v.cards[key]))
	}
	t.Run("a reply", func(t *testing.T) {
		var late atomic.Bool
		app, fs := vkFake(t, func(method string, f url.Values) (any, bool) {
			if method != "subscriptions.thread.getView" {
				return nil, false
			}
			th := thrFixReplies(ch, root, 10, r(10), thrReplyBy(r(8), "U0FAKE00002"), thrReplyBy(r(9), "U0FAKE00001"), thrReplyBy(r(10), "U0FAKE00002"))
			if late.Load() {
				th = thrFixReplies(ch, root, 11, r(10), thrReplyBy(r(9), "U0FAKE00001"), thrReplyBy(r(10), "U0FAKE00002"), thrReplyBy(r(11), "U0FAKE00002"))
			}
			return thrOrderAnswer(append(fillers(false), th)...), true
		})
		h := newKBHost(app, false)
		h.away.Store(true) // nothing marked read by what shows
		v := NewThreads(h)
		e := thrViewEnvAt(t, v, 4, image.Pt(1000, 300))
		e.frame()
		late.Store(true)
		thrRefreshAfter(t, e, v, fs, "the refresh after r11", thrReplyEvent(ch, root, r(11)))
		e.until(t, "r11 held", func() bool {
			it := thrItemOf(v, key)
			return it != nil && it.latest() == r(11)
		})
		if got, want := scrollTo(t, e, v), []string{r(9), r(10), r(11)}; !slices.Equal(got, want) {
			t.Errorf("the thread shows %v once scrolled to, want %v from r9, its first shown reply at the load", got, want)
		}
	})
	t.Run("a read", func(t *testing.T) {
		app, fs := vkFake(t, func(method string, f url.Values) (any, bool) {
			if method != "subscriptions.thread.getView" {
				return nil, false
			}
			var rs []slack.Message
			for n := 6; n <= 10; n++ {
				rs = append(rs, thrReplyBy(r(n), "U0FAKE00002"))
			}
			return thrOrderAnswer(append(fillers(true), thrFixReplies(ch, root, 10, r(6), rs...))...), true
		})
		h := newKBHost(app, false)
		h.away.Store(true)
		v := NewThreads(h)
		e := thrViewEnvAt(t, v, 4, image.Pt(1000, 300))
		e.frame()
		if got := thrRoots(v); len(got) != 4 || got[3] != root {
			t.Fatalf("the threads are in the order %v, want the thread last", got)
		}
		fs.Push(map[string]any{"type": "thread_marked", "subscription": map[string]any{"type": "thread", "channel": ch,
			"thread_ts": root, "last_read": r(10), "active": true}, "event_ts": "1762699500.000100"})
		e.until(t, "the thread read elsewhere", func() bool { return v.lastRead(thrItemOf(v, key)) == r(10) })
		if got, want := scrollTo(t, e, v), []string{r(6), r(7), r(8), r(9), r(10)}; !slices.Equal(got, want) {
			t.Errorf("the thread shows %v once scrolled to, want %v from r6, its first shown reply at the load", got, want)
		}
	})
}

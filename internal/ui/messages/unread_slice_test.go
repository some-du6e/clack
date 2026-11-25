package messages

// The "New" line, the unread banner and paging back with more unread
// messages than the newest page holds (Slack's 28, determineDefaultHistoryLimit):
//
//   - canShowUnreadDivider (boot-shared AEjl) draws the line only where the
//     slice reaches the last read (its first message at or before it, or
//     the conversation's start), so a newest page that is all unread has
//     none, and jumpToUnread goes to the last read instead
//     (jumpToTimestamp({ts: lastReadTs})), landing on the first unread.
//   - The banner's count is known only when the slice holding the last
//     read reaches the newest (unreadCountKnown): "20+ new messages" until
//     the older pages reach it.
//   - MessageList.requestOlder asks again as soon as a page landed; only the
//     pane's HistoryRateLimiter holds a page asked for again.
//
// Synthetic history on the fake (guardEnv: 240 messages 90 s apart, the
// newest page messages 212 to 239).

import (
	"net/url"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// unreadSliceEnv is guardEnv's channel read up to message last, the list
// open at its newest messages. With hold, the page before the newest one
// (latest = the newest page's first message) waits at the fake until the
// returned func opens it.
func unreadSliceEnv(t *testing.T, ch string, last int, hold bool) (*winEnv, *fakeslack.Server, func()) {
	t.Helper()
	gate := make(chan struct{})
	var once sync.Once
	open := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(open)
	var fake *fakeslack.Server
	e, fs := guardEnv(t, ch, nil, func(app *core.App, fs *fakeslack.Server, tss []string) {
		fake = fs
		app.S.SetCounts(&slack.Counts{Channels: []slack.CountEntry{{ID: ch, LastRead: tss[last], Latest: tss[len(tss)-1], HasUnreads: true}}})
		if !hold {
			return
		}
		first := tss[len(tss)-slack.DefaultHistoryLimit(ch)]
		fs.SetHook(func(method string, f url.Values) (any, bool) {
			if method == "conversations.history" && f.Get("channel") == ch && f.Get("latest") == first && f.Get("oldest") == "" {
				<-gate
			}
			return nil, false
		})
	})
	t.Cleanup(func() { open(); fake.SetHook(nil) }) // first: before the fake closes
	return e, fs, open
}

// unreadLineBefore is the message the "New" line sits right above ("" for
// no line).
func (e *winEnv) unreadLineBefore() string {
	s := e.v.s
	ui := s.unreadIndex()
	if ui < 0 {
		return ""
	}
	for i := ui + 1; i < len(s.items); i++ {
		if it := s.items[i]; it.kind == itMsg && it.idx < len(s.snap.msgs) {
			return s.snap.msgs[it.idx].m.TS
		}
	}
	return "(none after it)"
}

// 40 unread (messages 200 to 239), the newest page 212 to 239 and the page
// before it held: no "New" line, since the slice doesn't reach the last
// read and its first message isn't the first unread (the old rule drew it
// above message 212, where the banner's jump went, and the next press
// marked the channel read with 12 unread messages never shown). The
// banner's press jumps to the last read, the window around it, and lands
// on message 200 under the line, marking nothing.
func TestUnreadLineWaitsForTheLastRead(t *testing.T) {
	const ch = "C0UNRDPG01"
	e, fs, _ := unreadSliceEnv(t, ch, 199, true)
	if at := e.unreadLineBefore(); at != "" {
		t.Errorf("the newest page (all unread, the last read before it) draws the \"New\" line above message %d; Slack's canShowUnreadDivider draws none", e.index(at))
	}
	ub := &e.v.s.ub
	e.await(t, "the banner", func() bool { return ub.text != "" })
	if ub.icon != "arrow-up" {
		t.Errorf("the banner's arrow is %s, want arrow-up (no line: getUnreadLinePlacement 1)", ub.icon)
	}
	marks := func() int64 { return fs.CallCount("conversations.mark") }
	e.rt.press(centre(ub.msgR), true)
	want := e.tss[200]
	e.await(t, "the first unread under the line, in view", func() bool {
		return e.v.s.pendingJump == "" && e.unreadLineBefore() == want && e.drawn(want)
	})
	e.frames(300 * time.Millisecond)
	if !e.drawn(want) || e.unreadLineBefore() != want {
		t.Errorf("after the jump: the line above message %d, message 200 drawn %v (%s)", e.index(e.unreadLineBefore()), e.drawn(want), e.state())
	}
	if ub.icon != "arrow-down" {
		t.Errorf("after the jump the banner's arrow is %s, want arrow-down (the line in view)", ub.icon)
	}
	if n := marks(); n != 0 {
		t.Errorf("the banner's jump marked the channel read (%d conversations.mark)", n)
	}
}

// The banner's "N new messages" (getUnreadBannerDisplayCount with
// unreadCountKnown): exact when the slice holding the last read reaches
// the newest, else floored to tens with a "+".
func TestUnreadBannerCountLikeSlack(t *testing.T) {
	t.Run("the newest page holds the last read", func(t *testing.T) {
		e, _, _ := unreadSliceEnv(t, "C0UNRDPG02", 219, false)
		ub := &e.v.s.ub
		e.await(t, "the banner", func() bool { return ub.text != "" })
		if ub.text != "20 new messages" {
			t.Errorf("20 unread in the newest page: the banner reads %q, want \"20 new messages\"", ub.text)
		}
		if at := e.unreadLineBefore(); at != e.tss[220] {
			t.Errorf("the line is above message %d, want 220", e.index(at))
		}
	})
	t.Run("more unread than the page", func(t *testing.T) {
		e, _, open := unreadSliceEnv(t, "C0UNRDPG03", 199, true)
		ub := &e.v.s.ub
		e.await(t, "the banner", func() bool { return ub.text != "" })
		if ub.text != "20+ new messages" {
			t.Errorf("40 unread, the newest page of 28 loaded: the banner reads %q, want \"20+ new messages\" (the count isn't known)", ub.text)
		}
		open() // the page before the newest reaches the last read
		e.await(t, "the page before the newest", func() bool { return e.unreadLineBefore() == e.tss[200] })
		e.frames(100 * time.Millisecond)
		if ub.text != "40 new messages" {
			t.Errorf("40 unread, the slice reaching the last read: the banner reads %q, want \"40 new messages\"", ub.text)
		}
	})
}

// Paging back at the list's top: each older page goes out on the frame
// after the one before it landed, as MessageList.requestOlder asks again
// on the update that brings a page (only isLoading refuses it). A second
// between pages let a reader scrolling back get 27 messages a second
// once the pages were 28.
func TestOlderPagesChainLikeSlack(t *testing.T) {
	const ch = "C0OLDCHN01"
	e, _ := guardEnv(t, ch, nil, nil)
	m := e.v.s.m
	state := func() (older bool, n int) {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.older, len(m.msgs)
	}
	for k := 0; k < 3; k++ {
		e.await(t, "no older page out", func() bool { o, _ := state(); return !o })
		// the list takes the page in (its sync keeps the row it showed
		// where it was, the page above it) before the reader goes to the top
		e.await(t, "the list's rows of the page", func() bool { _, n := state(); return len(e.v.s.snap.msgs) == n && !e.v.s.snap.older })
		e.rt.frame()
		_, n0 := state()
		e.v.s.list.Position = layout.Position{BeforeEnd: true} // the reader at the top of what's loaded
		e.rt.frame()
		e.rt.frame()
		if o, n := state(); !o && n == n0 {
			t.Fatalf("page %d: at the top of %d loaded messages, two frames after the last page landed, no older page went out", k+1, n0)
		}
	}
}

package views

// A card's gaps (fillGap): the replies a refresh's 2-reply window skipped,
// fetched with conversations.replies {oldest, latest}. Slack's card draws
// every reply the socket put in its store; Clack's fetches what a refresh
// couldn't join, one fetch per card at a time, and falls back to the
// card's latest window and "Show N more replies" when one fails, so no
// reply is ever lost between the two.

import (
	"image"
	"net/url"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// thrGapRoot is the gap tests' thread, read and followed; thrGapReplies
// its replies, oldest first.
const thrGapRoot = "1762694000.000100"

var thrGapReplies = []string{"1762694100.000100", "1762694200.000100", "1762694300.000100", "1762694400.000100",
	"1762694500.000100", "1762694600.000100", "1762694700.000100", "1762694800.000100", "1762694900.000100",
	"1762694950.000100", "1762694970.000100"}

// thrGapFake is a server whose gap thread has the first n replies less
// the deleted ones (gone), its getView window the latest two. views holds
// getView's answers (taken as the request arrives) and gaps the gap
// fetches' (oldest set); a gap fetch whose oldest is fail fails, once.
type thrGapFake struct {
	ch          string
	n           atomic.Int32
	views, gaps thrGate
	mu          sync.Mutex
	fail        string
	gone        map[string]bool
	others      []map[string]any // threads listed above it
}

// live is the thread's replies of the first n the server still has.
func (f *thrGapFake) live(n int) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, ts := range thrGapReplies[:n] {
		if !f.gone[ts] {
			out = append(out, ts)
		}
	}
	return out
}

// del deletes replies on the server.
func (f *thrGapFake) del(tss ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.gone == nil {
		f.gone = map[string]bool{}
	}
	for _, ts := range tss {
		f.gone[ts] = true
	}
}

func (f *thrGapFake) reply(ts string) slack.Message {
	return slack.Message{Type: "message", Channel: f.ch, TS: ts, ThreadTS: thrGapRoot, User: "U0FAKE00002", Text: "a synthetic reply"}
}

func (f *thrGapFake) root(n int) slack.Message {
	rs := f.live(n)
	last := rs[len(rs)-1]
	return slack.Message{Type: "message", Channel: f.ch, TS: thrGapRoot, ThreadTS: thrGapRoot, User: "U0FAKE00001", Text: "a synthetic root",
		ReplyCount: len(rs), ReplyUsers: []string{"U0FAKE00002"}, LatestReply: last, Subscribed: true, LastRead: last}
}

func (f *thrGapFake) failGap(oldest string) {
	f.mu.Lock()
	f.fail = oldest
	f.mu.Unlock()
}

func (f *thrGapFake) hook(method string, v url.Values) (any, bool) {
	switch method {
	case "subscriptions.thread.getView":
		n := int(f.n.Load()) // the server's state as the request arrives
		rs, root := f.live(n), f.root(n)
		f.views.wait()
		th := map[string]any{"root_msg": root, "latest_replies": []slack.Message{f.reply(rs[len(rs)-2]), f.reply(rs[len(rs)-1])},
			"unread_replies": []slack.Message{}}
		return thrOrderAnswer(append(slices.Clone(f.others), th)...), true
	case "conversations.replies":
		if v.Get("ts") != thrGapRoot {
			return nil, false
		}
		oldest, latest, incl := v.Get("oldest"), v.Get("latest"), v.Get("inclusive") == "true"
		if oldest != "" {
			f.gaps.wait()
			f.mu.Lock()
			fail := f.fail != "" && f.fail == oldest
			if fail {
				f.fail = ""
			}
			f.mu.Unlock()
			if fail {
				return map[string]any{"ok": false, "error": "internal_error"}, true
			}
		}
		n := int(f.n.Load())
		msgs := []slack.Message{f.root(n)}
		for _, ts := range f.live(n) {
			if oldest != "" && !actTSLess(oldest, ts) && (!incl || ts != oldest) {
				continue
			}
			if latest != "" && !actTSLess(ts, latest) && (!incl || ts != latest) {
				continue
			}
			msgs = append(msgs, f.reply(ts))
		}
		return map[string]any{"ok": true, "messages": msgs, "has_more": false}, true
	}
	return nil, false
}

// thrGapView is the Threads view over f, its gap thread at two replies.
func thrGapView(t *testing.T, f *thrGapFake) (*Threads, *vaEnv, *fakeslack.Server) {
	t.Helper()
	f.ch = fakeslack.ViewsChannel
	f.n.Store(2)
	app, fs := vkFake(t, f.hook)
	// after vkFake's: cleanups run last first, and the fake's Close waits
	// for every answer a gate holds
	t.Cleanup(f.views.open)
	t.Cleanup(f.gaps.open)
	v := NewThreads(newKBHost(app, false))
	e := thrViewEnv(t, v, len(f.others)+1)
	e.size = image.Pt(1000, 2400)
	e.frame()
	return v, e, fs
}

// thrGapShown is the gap thread's replies as its card shows them.
func thrGapShown(v *Threads) []string {
	key := fakeslack.ViewsChannel + "/" + thrGapRoot
	p, _, _ := v.data.get()
	for i := range p.items {
		if p.items[i].key == key {
			return thrTSs(thrShown(&p.items[i], v.cards[key]))
		}
	}
	return nil
}

// thrGapMore clicks the gap thread's "Show N more replies" until its card
// shows every one of the n replies, and fails if a click brings no more.
func thrGapMore(t *testing.T, e *vaEnv, v *Threads, n int) {
	t.Helper()
	key := fakeslack.ViewsChannel + "/" + thrGapRoot
	want := thrGapReplies[:n]
	for range 3 {
		if slices.Equal(thrGapShown(v), want) {
			break
		}
		c := v.cards[key]
		if c == nil || c.moreText == "" {
			t.Fatalf("the card shows %v with no \"Show N more replies\" row; want %v", thrGapShown(v), want)
		}
		had := len(thrGapShown(v))
		c.more.Click()
		e.until(t, "the older replies", func() bool { return len(thrGapShown(v)) > had || v.cards[key].olderStart })
		e.frame()
	}
	if got := thrGapShown(v); !slices.Equal(got, want) {
		t.Fatalf("after \"Show N more replies\" the card shows %v; want %v, none missing", got, want)
	}
	if c := v.cards[key]; c.moreText != "" {
		t.Errorf("a %q row with every reply shown", c.moreText)
	}
}

// One gap fetch per card: a refresh's gap while one is out waits, widened.
// When the one out fills, the waiting one goes. When it fails, the card
// goes to its latest window (not the failed gap's) with the waiting gap,
// and "Show N more replies" loads every reply before it; for a while
// after, a gap takes the card there at once, fetching nothing.
func TestThreadsGapOnePerCardLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	rs := thrGapReplies
	for _, fail := range []bool{false, true} {
		name := map[bool]string{false: "fills", true: "fails"}[fail]
		t.Run(name, func(t *testing.T) {
			f := &thrGapFake{}
			v, e, fs := thrGapView(t, f)
			if fail {
				f.failGap(rs[1])
			}
			// r2-r4 within one refresh: the gap after r1 is fetched, and held
			f.gaps.hold()
			f.n.Store(5)
			thrRefreshAfter(t, e, v, fs, "the refresh after r2-r4", thrReplyEvent(ch, thrGapRoot, rs[4]))
			e.until(t, "the gap's fetch", func() bool { return fs.CallCount("conversations.replies") >= 1 })
			// r5-r7 within the next, while that fetch is out
			f.n.Store(8)
			thrRefreshAfter(t, e, v, fs, "the refresh after r5-r7", thrReplyEvent(ch, thrGapRoot, rs[7]))
			thrBounded(e, 300*time.Millisecond, func() bool { return false })
			if got := fs.CallCount("conversations.replies"); got != 1 {
				t.Errorf("%d gap fetches out for one card, want one at a time", got)
			}
			f.gaps.open()
			if fail {
				e.until(t, "the card at its latest window", func() bool { return slices.Equal(thrGapShown(v), rs[6:8]) })
				e.frame()
				thrGapMore(t, e, v, 8)
				if got := fs.CallCount("conversations.replies"); got != 2 {
					t.Errorf("%d replies fetches: want the failed gap's and the older replies'", got)
				}
				// r8-r10 within the next refresh, soon after the failure
				f.n.Store(11)
				thrRefreshAfter(t, e, v, fs, "the refresh after r8-r10", thrReplyEvent(ch, thrGapRoot, rs[10]))
				e.until(t, "the card at its new window", func() bool { return slices.Equal(thrGapShown(v), rs[9:11]) })
				thrBounded(e, 300*time.Millisecond, func() bool { return false })
				if got := fs.CallCount("conversations.replies"); got != 2 {
					t.Errorf("a gap right after a failed fetch was fetched (%d replies fetches)", got-2)
				}
				thrGapMore(t, e, v, 11)
				return
			}
			e.until(t, "every reply in the card", func() bool { return slices.Equal(thrGapShown(v), rs[:8]) })
			if got := fs.CallCount("conversations.replies"); got != 2 {
				t.Errorf("%d gap fetches, want two: one, then the one that waited", got)
			}
			if c := v.cards[ch+"/"+thrGapRoot]; c == nil || c.moreText != "" {
				t.Errorf("a \"Show N more replies\" row with every reply shown")
			}
		})
	}
}

// A refresh already out when a gap fetch fails read the card's replies
// before the failure: what it carries over from before the card's new
// window stays out (the card's floor), so "Show N more replies" loads
// them back in order and no reply falls in a hole.
func TestThreadsGapCarryFloorLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	rs := thrGapReplies
	f := &thrGapFake{}
	v, e, fs := thrGapView(t, f)
	f.failGap(rs[1])
	f.gaps.hold()
	f.n.Store(5)
	thrRefreshAfter(t, e, v, fs, "the refresh after r2-r4", thrReplyEvent(ch, thrGapRoot, rs[4]))
	e.until(t, "the gap's fetch", func() bool { return fs.CallCount("conversations.replies") >= 1 })
	// a refresh goes out over the card as it is, and is held
	f.views.hold()
	f.n.Store(6)
	v.w.mu.Lock()
	v.w.last = time.Time{}
	v.w.mu.Unlock()
	n := fs.CallCount("subscriptions.thread.getView")
	fs.Push(thrReplyEvent(ch, thrGapRoot, rs[5]))
	e.until(t, "the refresh's request", func() bool { return fs.CallCount("subscriptions.thread.getView") > n })
	f.gaps.open() // the gap's fetch fails
	e.until(t, "the card at its window", func() bool { return slices.Equal(thrGapShown(v), rs[3:5]) })
	f.views.open()
	e.until(t, "the refresh in", func() bool { return !v.data.busy() && slices.Contains(thrGapShown(v), rs[5]) })
	e.frame()
	if got := thrGapShown(v); !slices.Equal(got, rs[3:6]) {
		t.Errorf("the refresh that was out brought back %v; want %v, from the card's window on", got, rs[3:6])
	}
	thrGapMore(t, e, v, 6)
}

// A gap fetch from before a remount lands nowhere in the new list's card
// (no card's fold stamp repeats, across remounts too): with the thread
// below the fold until the mount's answer is in, its card is new after it,
// shows its own window, and "Show N more replies" loads every reply.
func TestThreadsGapRemountLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	rs := thrGapReplies
	f := &thrGapFake{others: []map[string]any{
		thrFixRead(ch, thrA, thrAReply, "U0FAKE00002", true, thrA),
		thrFixRead(ch, thrB, thrBReply, "U0FAKE00002", true, thrB),
		thrFixRead(ch, thrC, thrCReply, fakeslack.SelfID, false, thrCReply),
	}}
	v, e, fs := thrGapView(t, f)
	e.size = image.Pt(1000, 300) // the gap thread, last, below the fold
	e.frame()
	f.gaps.hold()
	f.n.Store(5)
	thrRefreshAfter(t, e, v, fs, "the refresh after r2-r4", thrReplyEvent(ch, thrGapRoot, rs[4]))
	e.until(t, "the gap's fetch", func() bool { return fs.CallCount("conversations.replies") >= 1 })
	// another page, then back: a new list over the server's later state
	v.PageLeft()
	f.n.Store(8)
	n := fs.CallCount("subscriptions.thread.getView")
	e.frame() // the remount
	e.until(t, "the mount's answer", func() bool { return fs.CallCount("subscriptions.thread.getView") > n && v.Settled() && !v.mounting })
	e.frame()
	if _, made := v.cards[ch+"/"+thrGapRoot]; made {
		t.Fatal("the gap thread's card was made before the mount's answer: it must be below the fold")
	}
	e.size = image.Pt(1000, 2400) // the gap thread drawn: a new card
	e.frame()
	f.gaps.open() // the old fetch lands
	thrBounded(e, 300*time.Millisecond, func() bool { return false })
	if got := thrGapShown(v); !slices.Equal(got, rs[6:8]) {
		t.Errorf("the new card shows %v; want its window %v, the old fetch landing nowhere", got, rs[6:8])
	}
	thrGapMore(t, e, v, 8)
}

// A refresh carries over the card's replies before its window as the copies
// the card had when it went out: one a change reached before the request
// that no frame took yet (a load that starts before the frame after the
// change) takes it as the refresh lands, so the card draws the edit, or
// leaves the reply out.
func TestThreadsCarriedChangeLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	rs := thrGapReplies
	f := &thrGapFake{}
	v, e, fs := thrGapView(t, f)
	f.n.Store(3)
	thrRefreshAfter(t, e, v, fs, "the refresh after r2", thrReplyEvent(ch, thrGapRoot, rs[2]))
	f.n.Store(4)
	thrRefreshAfter(t, e, v, fs, "the refresh after r3", thrReplyEvent(ch, thrGapRoot, rs[3]))
	e.until(t, "r0 and r1 carried over", func() bool { return slices.Equal(thrGapShown(v), rs[:4]) })
	text := func(ts string) string {
		key := ch + "/" + thrGapRoot
		p, _, _ := v.data.get()
		for i := range p.items {
			if p.items[i].key == key {
				for _, m := range thrShown(&p.items[i], v.cards[key]) {
					if m.TS == ts {
						return m.Text
					}
				}
			}
		}
		return ""
	}
	edited := f.reply(rs[0])
	edited.Text = "an edited carried reply"
	edited.Edited = &slack.Edited{User: "U0FAKE00002", TS: "1762695990.000100"}
	lvInject(v.h.Core(), ch, map[string]any{"type": "message", "subtype": "message_changed", "hidden": true, "message": edited, "previous_message": f.reply(rs[0])})
	v.load(actRefresh) // before any frame took the edit
	e.until(t, "the refresh", v.Settled)
	e.frame()
	if got := text(rs[0]); got != "an edited carried reply" {
		t.Fatalf("the refresh carried r0 over as %q, want its edit", got)
	}
	lvInject(v.h.Core(), ch, map[string]any{"type": "message", "subtype": "message_deleted", "hidden": true, "deleted_ts": rs[1], "previous_message": f.reply(rs[1])})
	v.load(actRefresh)
	e.until(t, "the refresh", v.Settled)
	e.frame()
	if got, want := thrGapShown(v), []string{rs[0], rs[2], rs[3]}; !slices.Equal(got, want) {
		t.Errorf("the refresh after r1's deletion shows %v, want %v", got, want)
	}
}

// thrGapDeleted is the socket's message_deleted for reply ts.
func thrGapDeleted(ch, ts string) map[string]any {
	return map[string]any{"type": "message", "subtype": "message_deleted", "channel": ch, "hidden": true, "deleted_ts": ts,
		"ts": "1762695990.000100", "event_ts": "1762695990.000100",
		"previous_message": map[string]any{"type": "message", "ts": ts, "thread_ts": thrGapRoot, "user": "U0FAKE00002", "text": "a synthetic reply"}}
}

// A failed gap fetch's floor holds only for the answers asked for before
// the cut. A refresh asked for after it brings the card's window wherever
// that starts: with the two newest replies deleted, getView's window moves
// back before the floor, and the card draws it.
func TestThreadsGapFloorLeavesLaterWindowsLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	rs := thrGapReplies
	f := &thrGapFake{}
	v, e, fs := thrGapView(t, f)
	f.failGap(rs[1])
	f.n.Store(5)
	thrRefreshAfter(t, e, v, fs, "the refresh after r2-r4", thrReplyEvent(ch, thrGapRoot, rs[4]))
	e.until(t, "the card cut to its window", func() bool { return slices.Equal(thrGapShown(v), rs[3:5]) })
	f.del(rs[3], rs[4])
	fs.Push(thrGapDeleted(ch, rs[3]))
	fs.Push(thrGapDeleted(ch, rs[4]))
	e.until(t, "the deletions drawn", func() bool { return len(thrGapShown(v)) == 0 })
	v.load(actRefresh)
	e.until(t, "the refresh after the deletions", v.Settled)
	e.frame()
	if got := thrGapShown(v); !slices.Equal(got, rs[1:3]) {
		t.Errorf("after the refresh the card shows %v; want getView's window %v (the floor was the failed gap's)", got, rs[1:3])
	}
	thrGapMore(t, e, v, 3)
}

// A refresh with a gap that lands while another page shows fetches nothing
// when the view comes back: its first frame remounts, and the old cards
// the gap was for go (Slack's unmounted view sends nothing).
func TestThreadsGapAwayLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	rs := thrGapReplies
	f := &thrGapFake{}
	v, e, fs := thrGapView(t, f)
	f.views.hold()
	f.n.Store(5)
	v.w.mu.Lock()
	v.w.last = time.Time{}
	v.w.mu.Unlock()
	n := fs.CallCount("subscriptions.thread.getView")
	fs.Push(thrReplyEvent(ch, thrGapRoot, rs[4]))
	e.until(t, "the refresh's request", func() bool { return fs.CallCount("subscriptions.thread.getView") > n })
	v.PageLeft() // another page
	f.views.open()
	for deadline := time.Now().Add(10 * time.Second); v.data.busy(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the refresh never answered")
		}
	}
	e.frame() // back: the refresh with its gap settles, and the view remounts
	e.until(t, "the mount's load", func() bool { return v.Settled() && !v.mounting })
	thrBounded(e, 300*time.Millisecond, func() bool { return false })
	if got := fs.CallCount("conversations.replies"); got != 0 {
		t.Errorf("%d conversations.replies on the way back: the gap was for the cards the remount let go", got)
	}
}

// reloadThreads (the sidebar row clicked again) starts each card again at
// its getView window, getDefaultMinimumVisibleTs on the new refreshId: until
// the reload answers, the card is its window with "Show N more replies",
// not the replies refreshes carried over with a hole where the filled gap
// was.
func TestThreadsReloadCutsToWindowLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	rs := thrGapReplies
	f := &thrGapFake{}
	v, e, fs := thrGapView(t, f)
	f.n.Store(5)
	thrRefreshAfter(t, e, v, fs, "the refresh after r2-r4", thrReplyEvent(ch, thrGapRoot, rs[4]))
	e.until(t, "every reply in the card", func() bool { return slices.Equal(thrGapShown(v), rs[:5]) })
	f.views.hold()
	v.Reselect()
	e.frame()
	if got := thrGapShown(v); !slices.Equal(got, rs[3:5]) {
		t.Errorf("while the reload is out the card shows %v; want its window %v", got, rs[3:5])
	}
	if c := v.cards[ch+"/"+thrGapRoot]; c == nil || c.moreText == "" {
		t.Error("no \"Show N more replies\" row over the window while the reload is out")
	}
	f.views.open()
	e.until(t, "the reload", func() bool { return v.Settled() && !v.data.busy() })
	thrGapMore(t, e, v, 5)
}

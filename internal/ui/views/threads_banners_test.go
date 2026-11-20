package views

// Slack's Threads view banners (client-threads-view 9Ra5,
// renderTopBanners and renderBottomBanners with ThreadUnreadBannerGeneric
// ntqA over JwHt), and markThread's failures (2wCo).

import (
	"context"
	"fmt"
	"image"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// thrBannerThreads are six followed unread threads, newest first: at
// 1000x300 with the third card at the top, the first two are above the
// list and the last three below it.
var thrBannerThreads = [][2]string{
	{"1762690000.000100", "1762690500.000100"},
	{"1762680000.000100", "1762680500.000100"},
	{"1762670000.000100", "1762670500.000100"},
	{"1762660000.000100", "1762660500.000100"},
	{"1762650000.000100", "1762650500.000100"},
	{"1762640000.000100", "1762640500.000100"},
}

// thrBannerLog records the subscriptions.thread.mark and clearAll calls.
type thrBannerLog struct {
	thrMarkLog
	mu     sync.Mutex
	clears int
}

func (l *thrBannerLog) note(method string, f url.Values) {
	l.thrMarkLog.note(method, f)
	if method == "subscriptions.thread.clearAll" {
		l.mu.Lock()
		l.clears++
		l.mu.Unlock()
	}
}

func (l *thrBannerLog) clearsN() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.clears
}

// thrBannerEnv is the Threads view over thrBannerThreads at 1000x300,
// unattended (nothing marked read by what shows), with its third card at
// the top once the scroll's check has run.
func thrBannerEnv(t *testing.T, hasMore bool, counts map[string]int) (*core.App, *Threads, *vaEnv, *thrBannerLog) {
	t.Helper()
	ch := fakeslack.ViewsChannel
	log := &thrBannerLog{}
	app, _ := vkFake(t, func(method string, f url.Values) (any, bool) {
		log.note(method, f)
		switch method {
		case "subscriptions.thread.getView":
			var ths []map[string]any
			for _, th := range thrBannerThreads {
				ths = append(ths, thrFixRead(ch, th[0], th[1], "U0FAKE00002", true, th[0]))
			}
			a := thrOrderAnswer(ths...)
			a["has_more"] = hasMore
			return a, true
		case "subscriptions.thread.clearAll":
			return map[string]any{"ok": true}, true
		case "client.counts":
			if counts != nil {
				return map[string]any{"ok": true, "channels": []any{}, "ims": []any{}, "mpims": []any{},
					"threads": map[string]any{"has_unreads": true, "mention_count": 0, "unread_count_by_channel": counts}}, true
			}
		}
		return nil, false
	})
	h := newKBHost(app, false)
	h.away.Store(true)
	v := NewThreads(h)
	e := thrViewEnv(t, v, len(thrBannerThreads))
	e.size = image.Pt(1000, 300)
	e.frame()
	v.list.Position = layout.Position{First: 2}
	thrBounded(e, 400*time.Millisecond, func() bool { return false }) // the scroll's check
	return app, v, e, log
}

// thrBannerKey is thread i's key and its reply's row.
func thrBannerKey(i int) (key, reply string) {
	key = fakeslack.ViewsChannel + "/" + thrBannerThreads[i][0]
	return key, key + "-" + thrBannerThreads[i][1]
}

// thrHoverAt finds where button c is drawn, scanning row y from x across.
func thrHoverAt(t *testing.T, e *vaEnv, c *widget.Clickable, y int) image.Point {
	t.Helper()
	for x := 0; x < e.size.X; x += 2 {
		p := image.Pt(x, y)
		e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(p.X), float32(p.Y))})
		e.frame()
		if c.Hovered() {
			return p
		}
	}
	t.Fatalf("no button drawn along y=%d", y)
	return image.Point{}
}

// The banners count the unread threads whose "unread" row is above and
// below the visible range, once the scroll has stopped
// (getCurrentUnreadThreads): "↑ 2 unread threads" and "↓ 3 unread
// threads". The bottom label scrolls the first thread below to its divider
// 100px down the list and marks it read, its hand mark gone; the top one
// does so for the last above. The close calls subscriptions.thread.clearAll,
// lets go of every hand mark and hides both banners for 5 s.
func TestThreadsUnreadBannersLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	app, v, e, log := thrBannerEnv(t, false, nil)
	drawn := func() []string {
		var out []string
		for _, s := range drawnStrings(e) {
			if strings.Contains(s, "unread thread") {
				out = append(out, strings.TrimSpace(s)) // the label's run starts with the button's " "
			}
		}
		slices.Sort(out)
		return out
	}
	if got, want := drawn(), []string{"2 unread threads", "3 unread threads"}; !slices.Equal(got, want) {
		t.Fatalf("with the third card at the top the banners say %v, want %v", got, want)
	}
	vaShot(t, e, "threads-unread-banners", false)
	// the bottom label: the first thread below, marked unread here by hand
	_, r4 := thrBannerKey(3)
	if err := app.MarkThreadUnreadFrom(context.Background(), ch, thrBannerThreads[3][0], thrBannerThreads[3][1], core.MarkedInThreadsView); err != nil {
		t.Fatal(err)
	}
	e.click(thrFind(t, e, &v.ban.down.jump))
	e.frame()
	if y, ok, _ := v.kb.top(r4); !ok || y < 99 || y > 101 {
		t.Errorf("the bottom banner scrolled the first thread below to y=%d (laid out %v), want its divider 100px down", y, ok)
	}
	if mark := thrBannerThreads[3][0] + "@" + thrBannerThreads[3][1] + " read=1"; !log.has(mark) {
		t.Errorf("the bottom banner marked %v, want %s", log.all(), mark)
	}
	if o := app.ThreadMarkedUnread(ch, thrBannerThreads[3][0]); o != "" {
		t.Errorf("the thread it marked read keeps its hand mark from %q", o)
	}
	// the top label, once the scroll has stopped: the last thread above
	thrBounded(e, 400*time.Millisecond, func() bool { return false })
	if !slices.ContainsFunc(drawn(), func(s string) bool { return s == "3 unread threads" }) {
		t.Fatalf("after the jump the banners say %v, want 3 above", drawn())
	}
	e.click(thrFind(t, e, &v.ban.up.jump))
	e.frame()
	_, r3 := thrBannerKey(2)
	if y, ok, _ := v.kb.top(r3); !ok || y < 99 || y > 101 {
		t.Errorf("the top banner scrolled the last thread above to y=%d (laid out %v), want its divider 100px down", y, ok)
	}
	if mark := thrBannerThreads[2][0] + "@" + thrBannerThreads[2][1] + " read=1"; !log.has(mark) {
		t.Errorf("the top banner marked %v, want %s", log.all(), mark)
	}
	// the close: every thread read on the server, every hand mark gone,
	// both banners hidden 5 s
	thrBounded(e, 400*time.Millisecond, func() bool { return false })
	if err := app.MarkThreadUnreadFrom(context.Background(), ch, thrBannerThreads[5][0], thrBannerThreads[5][1], core.MarkedInThread); err != nil {
		t.Fatal(err)
	}
	labels := drawn()
	if len(labels) == 0 {
		t.Fatal("no banner to close")
	}
	var bb *thrBannerButtons
	if len(v.ban.below) > 0 {
		bb = &v.ban.down
	} else {
		bb = &v.ban.up
	}
	at := thrFind(t, e, &bb.jump)
	e.click(thrHoverAt(t, e, &bb.close, at.Y))
	closed := time.Now()
	e.until(t, "subscriptions.thread.clearAll", func() bool { return log.clearsN() == 1 })
	if m := app.ThreadsMarkedUnread(); m != nil {
		t.Errorf("after the close the threads marked unread are %v, want none", m)
	}
	// the close hides them from the next determineUnreadThreadBannerVisibility
	// (render reads only the lists): the server's thread_marked events, or
	// here a scroll; with unread threads both above and below it lasts 5 s
	v.list.Position.Offset += 40
	thrBounded(e, 400*time.Millisecond, func() bool { return false })
	if got := drawn(); len(got) != 0 {
		t.Errorf("once the scroll after the close stopped the banners say %v", got)
	}
	v.list.Position.Offset += 20 // another scroll while hidden: still hidden
	thrBounded(e, time.Until(closed.Add(4*time.Second)), func() bool { return false })
	if got := drawn(); len(got) != 0 {
		t.Errorf("4 s after the close the banners say %v", got)
	}
	thrBounded(e, time.Until(closed.Add(5300*time.Millisecond)), func() bool { return false })
	v.list.Position.Offset -= 40 // the next check after the 5 s: the threads still unread show again
	e.until(t, "the banners back after 5 s", func() bool { return len(drawn()) > 0 })
}

// The close's own path over the views fixture, with no clearAll override:
// subscriptions.thread.clearAll, then the server's thread_marked for the
// thread it read. The thread reads, and the check its scan inputs bring
// leaves no banner, with no scroll. The 5 s hide asks for no frame at its
// end: Slack's timer only clears hideUnreadBanners, and render reads only
// the lists.
func TestThreadsBannerCloseLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	app, fs := vkFake(t, nil) // the views fixture's own handlers, over a live socket (vaFake serves offline)
	// clearAll answered (and the server's thread_marked sent) 300ms after
	// the click, so the frames asked for during the click's are its own
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "subscriptions.thread.clearAll" {
			time.Sleep(300 * time.Millisecond)
		}
		return nil, false
	})
	h := newKBHost(app, false)
	h.away.Store(true)
	v := NewThreads(h)
	e := thrViewEnv(t, v, 2)
	e.size = image.Pt(1000, 150)
	e.frame()
	key := ch + "/" + fakeslack.ViewsThreadUnread
	v.list.Position = layout.Position{First: 1} // the read thread at the top, the unread one above the list
	e.until(t, "↑ 1 unread thread", func() bool { return slices.Equal(v.ban.above, []string{key}) })
	at := thrFind(t, e, &v.ban.up.jump)
	p := thrHoverAt(t, e, &v.ban.up.close, at.Y)
	clicked := time.Now()
	e.click(p)
	if h.framesAsked(clicked, time.Now()) == 0 {
		t.Error("the close asked the window for no frame (the router mid-layout instead?)")
	}
	e.until(t, "subscriptions.thread.clearAll", func() bool { return fs.ThreadClears() == 1 })
	e.until(t, "the thread read by the server's thread_marked", func() bool {
		return v.lastRead(thrItemOf(v, key)) == "1762600200.000100"
	})
	e.frame()
	if len(v.ban.above) != 0 || len(v.ban.below) != 0 {
		t.Errorf("after the close's thread_marked the banners list %v above and %v below", v.ban.above, v.ban.below)
	}
	if got := drawnStrings(e); slices.ContainsFunc(got, func(s string) bool { return strings.Contains(s, "unread thread") }) {
		t.Errorf("a banner is drawn after the close's thread_marked: %v", got)
	}
	end := v.ban.unhideAt
	if end.IsZero() {
		t.Fatal("the close set no end to its hide")
	}
	for stop := time.Now().Add(time.Second); time.Now().Before(stop); time.Sleep(50 * time.Millisecond) {
		e.r.WakeupTime()
		e.frame()
		if w, ok := e.r.WakeupTime(); ok && !w.IsZero() && w.Sub(end).Abs() < 50*time.Millisecond {
			t.Fatalf("the hide asks for a frame at its end (%v)", w)
		}
	}
	// nor through the window (a timer for the 5 s): nothing else runs then
	// (the counts refresh came 1.5 s after the close)
	time.Sleep(time.Until(end.Add(150 * time.Millisecond)))
	if n := h.framesAsked(end.Add(-20*time.Millisecond), end.Add(120*time.Millisecond)); n != 0 {
		t.Errorf("the view asked the window for %d frames at the hide's end", n)
	}
}

// With more pages to load and the server's unread threads count, the
// bottom banner counts at least that count less the unread threads not
// below (getUnreadsBelowCount): 9 unread in all, 3 at or above the list's
// end, so 6.
func TestThreadsUnreadBannerCountLikeSlack(t *testing.T) {
	_, _, e, _ := thrBannerEnv(t, true, map[string]int{fakeslack.ViewsChannel: 9})
	e.until(t, "the bottom banner's count from the server's", func() bool {
		return slices.ContainsFunc(drawnStrings(e), func(s string) bool { return strings.TrimSpace(s) == "6 unread threads" })
	})
}

// The jump goes to the divider keyed with the thread's first timestamp
// past its last read over every reply held for the thread (Slack's
// timestamps: the thread's slice in the one messages store): the replies
// the card holds and hides, and those the thread pane fetched. A thread
// read at load to the newest of its ten replies shows its two newest (r9,
// r10), its card laid out. Marked unread from r5 in the thread pane, its
// unread row is at r9 and "↓ 1 unread thread" counts it, but no divider is
// keyed with the first reply past r4: the click does nothing. No scroll,
// no mark, and the hand mark stays. Either r8 is held but hidden (a
// three-reply getView window), or the pane fetched r1 to r10 (a two-reply
// window, like the views fixture's).
func TestThreadsBannerJumpHiddenUnreadLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	const root = "1762610000.000100"
	r := func(n int) string { return fmt.Sprintf("176261%02d00.000100", n) } // r(5) is 1762610500.000100
	for _, tc := range []struct {
		name   string
		window []int // getView's replies
		pane   bool  // the thread pane fetched the thread
	}{
		{"r8 held behind Show more replies", []int{8, 9, 10}, false},
		{"the thread pane fetched r1 to r10", []int{9, 10}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var win []slack.Message
			for _, n := range tc.window {
				win = append(win, thrReplyBy(r(n), "U0FAKE00002"))
			}
			fix := thrFixReplies(ch, root, 10, r(10), win...)
			var marks thrMarkLog
			app, fs := vkFake(t, func(method string, f url.Values) (any, bool) {
				marks.note(method, f)
				switch {
				case method == "conversations.replies" && f.Get("ts") == root:
					msgs := []slack.Message{fix["root_msg"].(slack.Message)}
					for n := 1; n <= 10; n++ {
						m := thrReplyBy(r(n), "U0FAKE00002")
						m.Channel, m.ThreadTS = ch, root
						msgs = append(msgs, m)
					}
					return map[string]any{"ok": true, "has_more": false, "messages": msgs}, true
				case method != "subscriptions.thread.getView":
					return nil, false
				}
				var ths []map[string]any
				for _, th := range thrBannerThreads[:3] {
					ths = append(ths, thrFixRead(ch, th[0], th[1], "U0FAKE00002", false, th[1]))
				}
				return thrOrderAnswer(append(ths, fix)...), true
			})
			h := newKBHost(app, false)
			h.away.Store(true)
			v := NewThreads(h)
			e := thrViewEnv(t, v, 4)
			e.size = image.Pt(1000, 300)
			e.frame()
			key := ch + "/" + root
			// the thread's card laid out (it is fourth, below the fold),
			// then the list back at its top
			v.list.Position = layout.Position{First: 3}
			e.frame()
			e.frame()
			v.list.Position = layout.Position{}
			e.frame()
			c := v.cards[key]
			if c == nil {
				t.Fatal("the thread's card isn't laid out once scrolled to")
			}
			if got, want := thrTSs(thrShown(thrItemOf(v, key), c)), []string{r(9), r(10)}; !slices.Equal(got, want) {
				t.Fatalf("the read thread shows %v, want its two newest %v", got, want)
			}
			if tc.pane {
				if _, err := app.Replies(context.Background(), ch, root); err != nil {
					t.Fatal(err)
				}
				if got, _ := app.S.Thread(ch, root); len(got) != 11 {
					t.Fatalf("core holds %d of the thread's messages after the pane's fetch, want the root and its 10 replies", len(got))
				}
			}
			if err := app.MarkThreadUnreadFrom(context.Background(), ch, root, r(5), core.MarkedInThread); err != nil {
				t.Fatal(err)
			}
			fs.Push(map[string]any{"type": "thread_marked", "subscription": map[string]any{"type": "thread", "channel": ch,
				"thread_ts": root, "last_read": r(4), "active": true}, "event_ts": "1762699500.000100"})
			e.until(t, "↓ 1 unread thread", func() bool { return slices.Equal(v.ban.below, []string{key}) })
			if row, _ := v.unreadRowKey(thrItemOf(v, key)); row != key+"-"+r(9) {
				t.Fatalf("its unread row is %s, want the divider at r9 (%s)", row, r(9))
			}
			at, n := v.list.Position, len(marks.all())
			e.click(thrFind(t, e, &v.ban.down.jump))
			e.frame()
			if p := v.list.Position; p.First != at.First || p.Offset != at.Offset {
				t.Errorf("the click moved the list from %+v to %+v; Slack's list has no divider keyed with the first reply past the last read to go to", at, p)
			}
			if got := marks.all()[n:]; len(got) != 0 {
				t.Errorf("the click marked %v", got)
			}
			if o := app.ThreadMarkedUnread(ch, root); o != core.MarkedInThread {
				t.Errorf("the click took the pane's hand mark (now %q)", o)
			}
		})
	}
}

// The jump puts the divider itself 100px down the list (scrollToKey on
// the divider's key, offset -100), the usual ↓ target included: a card
// below the fold that was never laid out, whose rows' places aren't known
// when the jump goes. The view opens straight at 300px, so the fourth of
// seven read threads has never been drawn when the pane marks it unread.
func TestThreadsBannerJumpUnlaidCardLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	const root, reply = "1762665000.000100", "1762665500.000100" // after thrBannerThreads[2]'s activity, before [3]'s
	var marks thrMarkLog
	app, fs := vkFake(t, func(method string, f url.Values) (any, bool) {
		marks.note(method, f)
		if method != "subscriptions.thread.getView" {
			return nil, false
		}
		ths := []map[string]any{thrFixRead(ch, root, reply, "U0FAKE00002", false, reply)}
		for _, th := range thrBannerThreads {
			ths = append(ths, thrFixRead(ch, th[0], th[1], "U0FAKE00002", false, th[1]))
		}
		return thrOrderAnswer(ths...), true
	})
	h := newKBHost(app, false)
	h.away.Store(true)
	v := NewThreads(h)
	e := thrViewEnvAt(t, v, 7, image.Pt(1000, 300))
	e.frame()
	key := ch + "/" + root
	if got := thrRoots(v); len(got) != 7 || got[3] != root {
		t.Fatalf("the threads are in the order %v, want the thread fourth", got)
	}
	if v.cards[key] != nil {
		t.Fatal("the fourth thread's card was laid out at 300px")
	}
	if err := app.MarkThreadUnreadFrom(context.Background(), ch, root, reply, core.MarkedInThread); err != nil {
		t.Fatal(err)
	}
	fs.Push(map[string]any{"type": "thread_marked", "subscription": map[string]any{"type": "thread", "channel": ch,
		"thread_ts": root, "last_read": root, "active": true}, "event_ts": "1762699500.000100"})
	e.until(t, "↓ 1 unread thread", func() bool { return slices.Equal(v.ban.below, []string{key}) })
	if v.cards[key] != nil {
		t.Fatal("the fourth thread's card was laid out before the jump")
	}
	// the click's frame (the jump to the card's top), then the frame that
	// lays the card out and jumps again, asking the window for the next
	pos := thrFind(t, e, &v.ban.down.jump)
	pt := f32.Pt(float32(pos.X), float32(pos.Y))
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt})
	e.frame()
	if v.ban.jumpRow == "" {
		t.Fatal("the jump to a card never laid out left nothing to jump to again")
	}
	before := time.Now()
	e.frame()
	if v.ban.jumpRow != "" {
		t.Error("the card's layout didn't bring the jump again")
	}
	if h.framesAsked(before, time.Now()) == 0 {
		t.Error("the jump again asked the window for no frame to draw it")
	}
	e.frame()
	row := key + "-" + reply
	if y, ok, _ := v.kb.top(row); !ok || y < 99 || y > 101 {
		t.Errorf("the jump left the divider at y=%d (laid out %v), want 100: Slack scrolls to the divider's key, not the card's top", y, ok)
	}
	if !marks.has(root + "@" + reply + " read=1") {
		t.Errorf("the jump marked %v, want the thread read", marks.all())
	}
}

// A thread's read mark that fails puts its last read and its hand mark
// back, with no toast (markThread only logs), except a message_not_found,
// which Slack's markThread counts as done; one already read to its newest
// reply sends no request, and its hand mark still goes.
func TestThreadsMarkFailureLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	ctx := context.Background()
	t.Run("fails", func(t *testing.T) {
		app, fs := vkFake(t, func(method string, f url.Values) (any, bool) {
			if method != "subscriptions.thread.getView" {
				return nil, false
			}
			return thrOrderAnswer(thrFixRead(ch, thrA, thrAReply, "U0FAKE00002", true, thrA),
				thrFixRead(ch, thrC, thrCReply, fakeslack.SelfID, false, thrCReply)), true
		})
		h := newKBHost(app, false)
		h.away.Store(true)
		v := NewThreads(h)
		e := thrViewEnv(t, v, 2)
		e.frame()
		key := ch + "/" + thrA
		if err := app.MarkThreadUnreadFrom(ctx, ch, thrA, thrAReply, core.MarkedInThread); err != nil {
			t.Fatal(err)
		}
		n := fs.CallCount("subscriptions.thread.mark")
		fs.SetFault("subscriptions.thread.mark", fakeslack.Fault{Error: "internal_error", Times: 1})
		h.away.Store(false) // the attention back: markVisible marks a, and the mark fails (before the hook sees it)
		e.until(t, "the mark's request", func() bool { return fs.CallCount("subscriptions.thread.mark") > n })
		e.until(t, "the failed mark undone", func() bool { return v.lastRead(thrItemOf(v, key)) != thrAReply })
		if o := app.ThreadMarkedUnread(ch, thrA); o != core.MarkedInThread {
			t.Errorf("after the failed mark the hand mark is %q, want %q back", o, core.MarkedInThread)
		}
		thrBounded(e, 200*time.Millisecond, func() bool { return false })
		if ts := h.toasts(); len(ts) != 0 {
			t.Errorf("a failed thread mark toasted %q; Slack's markThread only logs", ts)
		}
	})
	t.Run("message not found", func(t *testing.T) {
		app, fs := vkFake(t, func(method string, f url.Values) (any, bool) {
			if method != "subscriptions.thread.getView" {
				return nil, false
			}
			return thrOrderAnswer(thrFixRead(ch, thrA, thrAReply, "U0FAKE00002", true, thrA),
				thrFixRead(ch, thrC, thrCReply, fakeslack.SelfID, false, thrCReply)), true
		})
		h := newKBHost(app, false)
		h.away.Store(true)
		v := NewThreads(h)
		e := thrViewEnv(t, v, 2)
		e.frame()
		key := ch + "/" + thrA
		if err := app.MarkThreadUnreadFrom(ctx, ch, thrA, thrAReply, core.MarkedInThread); err != nil {
			t.Fatal(err)
		}
		n := fs.CallCount("subscriptions.thread.mark")
		fs.SetFault("subscriptions.thread.mark", fakeslack.Fault{Error: "message_not_found", Times: 1})
		h.away.Store(false) // the attention back: markVisible marks a, and the server can't find the message
		e.until(t, "the mark's request", func() bool { return fs.CallCount("subscriptions.thread.mark") > n })
		if thrBounded(e, 600*time.Millisecond, func() bool { return v.lastRead(thrItemOf(v, key)) != thrAReply }) {
			t.Errorf("the read mark Slack counts done (message_not_found) was undone: last read %s", v.lastRead(thrItemOf(v, key)))
		}
		if o := app.ThreadMarkedUnread(ch, thrA); o != "" {
			t.Errorf("the hand mark came back (%q) after a read mark Slack counts done", o)
		}
	})
	t.Run("read again while it was out", func(t *testing.T) {
		var g thrGate
		var asked atomic.Int32
		app, fs := vkFake(t, func(method string, f url.Values) (any, bool) {
			switch {
			case method == "subscriptions.thread.mark" && f.Get("read") == "1":
				asked.Add(1)
				g.wait()
				return map[string]any{"ok": false, "error": "internal_error"}, true
			case method != "subscriptions.thread.getView":
				return nil, false
			}
			return thrOrderAnswer(thrFixRead(ch, thrA, thrAReply, "U0FAKE00002", true, thrA),
				thrFixRead(ch, thrC, thrCReply, fakeslack.SelfID, false, thrCReply)), true
		})
		t.Cleanup(g.open) // before the fake's Close, which waits for a held answer
		h := newKBHost(app, false)
		h.away.Store(true)
		v := NewThreads(h)
		e := thrViewEnv(t, v, 2)
		e.frame()
		key := ch + "/" + thrA
		if err := app.MarkThreadUnreadFrom(ctx, ch, thrA, thrAReply, core.MarkedInThreadsView); err != nil {
			t.Fatal(err)
		}
		g.hold()
		c := v.cards[key]
		c.reply.Click() // its box's focus marks the hand-marked thread read (onFooterInputFocus), the request held
		e.until(t, "the read mark's request", func() bool { return asked.Load() > 0 })
		if o := app.ThreadMarkedUnread(ch, thrA); o != "" {
			t.Fatalf("the read mark left the hand mark from %q", o)
		}
		// another client reads it to its root before the mark fails
		fs.Push(map[string]any{"type": "thread_marked", "subscription": map[string]any{"type": "thread", "channel": ch,
			"thread_ts": thrA, "last_read": thrA, "active": true}, "event_ts": "1762699500.000100"})
		e.until(t, "the other client's read", func() bool { return v.lastRead(thrItemOf(v, key)) == thrA })
		g.open() // the read mark fails
		thrBounded(e, 600*time.Millisecond, func() bool { return app.ThreadMarkedUnread(ch, thrA) != "" })
		if o := app.ThreadMarkedUnread(ch, thrA); o != "" {
			t.Errorf("the failed read mark put the hand mark back (%q) after the thread was read since; Slack's markThread puts nothing back then", o)
		}
	})
	t.Run("already read", func(t *testing.T) {
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
		h.away.Store(true)
		v := NewThreads(h)
		e := thrViewEnv(t, v, 2)
		e.frame()
		key := ch + "/" + thrA
		if err := app.MarkThreadUnreadFrom(ctx, ch, thrA, thrAReply, core.MarkedInThreadsView); err != nil {
			t.Fatal(err)
		}
		// read elsewhere to its newest reply, the hand mark still here
		fs.Push(map[string]any{"type": "thread_marked", "subscription": map[string]any{"type": "thread", "channel": ch,
			"thread_ts": thrA, "last_read": thrAReply}, "event_ts": "1762699500.000100"})
		e.until(t, "the thread read elsewhere", func() bool { return v.lastRead(thrItemOf(v, key)) == thrAReply })
		n := marks.count(thrA + "@" + thrAReply + " read=1")
		c := v.cards[key]
		c.reply.Click()
		e.until(t, "its reply box", func() bool { return c.comp != nil && c.comp.Focused() })
		if o := app.ThreadMarkedUnread(ch, thrA); o != "" {
			t.Errorf("its box's focus left the hand mark from %q", o)
		}
		if thrBounded(e, 400*time.Millisecond, func() bool { return marks.count(thrA+"@"+thrAReply+" read=1") > n }) {
			t.Error("a thread already read to its newest reply was marked read again")
		}
	})
}

// thrItemOf is the view's thread key as it holds it now.
func thrItemOf(v *Threads, key string) *thrItem {
	p, _, _ := v.data.get()
	return thrByKey(p.items, key)
}

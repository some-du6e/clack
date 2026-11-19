package views

// Slack's Threads view banners (client-threads-view 9Ra5,
// renderTopBanners and renderBottomBanners with ThreadUnreadBannerGeneric
// ntqA over JwHt), and markThread's failures (2wCo).

import (
	"context"
	"image"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
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
				out = append(out, s)
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

// With more pages to load and the server's unread threads count, the
// bottom banner counts at least that count less the unread threads not
// below (getUnreadsBelowCount): 9 unread in all, 3 at or above the list's
// end, so 6.
func TestThreadsUnreadBannerCountLikeSlack(t *testing.T) {
	_, _, e, _ := thrBannerEnv(t, true, map[string]int{fakeslack.ViewsChannel: 9})
	e.until(t, "the bottom banner's count from the server's", func() bool {
		return slices.Contains(drawnStrings(e), "6 unread threads")
	})
}

// A thread's read mark that fails puts its last read and its hand mark
// back, with no toast (markThread only logs); one already read to its
// newest reply sends no request, and its hand mark still goes.
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

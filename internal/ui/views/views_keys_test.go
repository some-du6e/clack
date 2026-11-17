package views

// The rail list views' keyboard (vlist_keys.go, Slack's virtual list and
// the To-dos InteractiveList) and live updates, on internal/fakeslack's
// views fixtures (synthetic data only). Realtime events go over the fake's
// socket to a booted core, as Slack's RTM sends them.

import (
	"context"
	"fmt"
	"image"
	"maps"
	"net/url"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// kbHost records the views' navigation and Undo toasts.
type kbHost struct {
	*vaHost
	mu      sync.Mutex
	navs    []string
	threads []string
	undos   []string
	undo    func()
}

func (h *kbHost) Navigate(r string) {
	h.mu.Lock()
	h.navs = append(h.navs, r)
	h.mu.Unlock()
}

func (h *kbHost) OpenThread(ch, ts string) {
	h.mu.Lock()
	h.threads = append(h.threads, ch+"/"+ts)
	h.mu.Unlock()
}

func (h *kbHost) ToastUndo(text string, undo func()) {
	h.mu.Lock()
	h.undos = append(h.undos, text)
	h.undo = undo
	h.mu.Unlock()
}

func (h *kbHost) last() (nav, thread string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if n := len(h.navs); n > 0 {
		nav = h.navs[n-1]
	}
	if n := len(h.threads); n > 0 {
		thread = h.threads[n-1]
	}
	return
}

func newKBHost(app *core.App, dark bool) *kbHost {
	return &kbHost{vaHost: &vaHost{slickHost: &slickHost{app: app, th: theme.New(dark)}}}
}

// kbPress presses and releases a key, then runs two frames.
func kbPress(e *vaEnv, n key.Name, m key.Modifiers) {
	e.r.Queue(key.Event{Name: n, Modifiers: m, State: key.Press}, key.Event{Name: n, Modifiers: m, State: key.Release})
	e.frame()
	e.frame()
}

// kbClickRow clicks the middle of a list row (top from the list's last
// layout, listY the list's top in the view).
func kbClickRow(t *testing.T, e *vaEnv, kb *vkList, k string, listY int) {
	t.Helper()
	y, ok, _ := kb.top(k)
	if !ok {
		t.Fatalf("row %s isn't laid out", k)
	}
	e.click(image.Pt(e.size.X/2, listY+y+kb.height(k)/2))
}

// vkFake boots a core on the views fixtures with realtime on.
func vkFake(t *testing.T, hook func(method string, f url.Values) (any, bool)) (*core.App, *fakeslack.Server) {
	t.Helper()
	sz := fakeslack.Small
	sz.Views = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	if hook != nil {
		fs.SetHook(hook)
	}
	app := core.New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(app.Shutdown)
	evs, stop := app.Subscribe()
	defer stop()
	if err := app.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	app.S.PutUsers([]slack.User{
		{ID: "U0FAKE00001", Name: "ada", RealName: "Ada Example"},
		{ID: "U0FAKE00002", Name: "grace", RealName: "Grace Example"},
	})
	app.S.PutConv(slack.Conversation{ID: fakeslack.ViewsChannel, Name: "views-fixtures", IsChannel: true, IsMember: true, NumMembers: 3})
	app.SetRealtime(true)
	deadline := time.After(10 * time.Second)
	for {
		select {
		case ev := <-evs:
			if ev.Type == "connection" && ev.Connected {
				return app, fs
			}
		case <-deadline:
			t.Fatal("realtime didn't connect")
		}
	}
}

// Threads: Slack's rows (heading, root, replies, footer) under the
// virtual list's keys. A press makes a row active without the ring; ↓/↑
// move through every row with the ring; → opens the active message's
// thread; Enter on a root or reply jumps to the root in the channel;
// Home goes back to the first row.
func TestThreadsKeysLikeSlack(t *testing.T) {
	for _, dark := range []bool{false, true} {
		app, _ := vaFake(t)
		h := newKBHost(app, dark)
		v := NewThreads(h)
		e := &vaEnv{size: image.Pt(1000, 1100)}
		e.draw = func(gtx layout.Context) { v.Layout(gtx) }
		e.until(t, "the followed threads", func() bool { p, ok, _ := v.data.get(); return ok && len(p.items) == 2 })
		e.frame()
		e.frame()
		p, _, _ := v.data.get()
		k0 := p.items[0].key
		root := "root-" + k0
		kbClickRow(t, e, v.kb, root, 49)
		if v.kb.active != root || v.kb.ring || !v.kb.focused {
			t.Fatalf("after a press: active %q ring %v focused %v", v.kb.active, v.kb.ring, v.kb.focused)
		}
		kbPress(e, key.NameDownArrow, 0)
		if want := k0 + "-" + p.items[0].Replies[0].TS; v.kb.active != want || !v.kb.ring {
			t.Fatalf("↓: active %q (ring %v), want %q", v.kb.active, v.kb.ring, want)
		}
		vaShot(t, e, "threads-keyboard-focus", dark)
		kbPress(e, key.NameUpArrow, 0)
		kbPress(e, key.NameUpArrow, 0)
		if v.kb.active != "heading-"+k0 {
			t.Fatalf("↑↑: active %q, want the heading", v.kb.active)
		}
		kbPress(e, key.NameUpArrow, 0)
		if v.kb.active != "heading-"+k0 {
			t.Fatalf("↑ at the top moved to %q", v.kb.active)
		}
		kbPress(e, key.NameDownArrow, 0)
		kbPress(e, key.NameRightArrow, 0)
		if _, th := h.last(); th != fakeslack.ViewsChannel+"/"+fakeslack.ViewsThreadUnread {
			t.Errorf("→ on the root opened %q", th)
		}
		kbPress(e, key.NameReturn, 0)
		if nav, _ := h.last(); nav != "c/"+fakeslack.ViewsChannel+"/m/"+fakeslack.ViewsThreadUnread {
			t.Errorf("Enter on the root went to %q", nav)
		}
		// ↓ onto the footer: its reply box takes the focus (onItemFocusEnter)
		for i := 0; i < 4 && (v.cards[k0] == nil || v.cards[k0].comp == nil); i++ {
			kbPress(e, key.NameDownArrow, 0)
		}
		c := v.cards[k0]
		if c == nil || c.comp == nil {
			t.Fatalf("↓ to the footer didn't open the reply box (active %q)", v.kb.active)
		}
		e.frame()
		if v.kb.focused {
			t.Error("the list kept the focus after the footer opened its reply box")
		}
		// ↑ in the empty reply box: focusItemAboveInput
		c.comp.OnFocusList()
		e.frame()
		e.frame()
		last := p.items[0].Replies[len(p.items[0].Replies)-1].TS
		if v.kb.active != k0+"-"+last || !v.kb.focused || !v.kb.ring {
			t.Fatalf("↑ in the reply box: active %q focused %v", v.kb.active, v.kb.focused)
		}
		// ↓ in the empty reply box (gap-w10): onDownArrowInEmptyInput's
		// focusItemBelowInput, the key after the footer
		var below string
		for i, it := range v.kb.items {
			if it.key == "footer-"+k0 && i+1 < len(v.kb.items) {
				below = v.kb.items[i+1].key
			}
		}
		if below != "" {
			c.comp.OnDownWhenEmpty()
			e.frame()
			e.frame()
			if v.kb.active != below {
				t.Errorf("↓ in the reply box: active %q, want %q", v.kb.active, below)
			}
		}
		kbPress(e, key.NameHome, 0)
		if v.kb.active != "heading-"+k0 {
			t.Errorf("Home: active %q", v.kb.active)
		}
		// a printable key on a row: the reply box takes it (after any
		// draft the composer restored for this thread)
		before := c.comp.DocText()
		kbType(e, "A", 0, "a")
		if got := c.comp.DocText(); got != before+"a" {
			t.Errorf("typing on a row: reply box %q, was %q", got, before)
		}
		c.comp.Close()
	}
}

// Threads live: thread_marked patches the thread's last read in place
// (no getView), thread_subscribed brings the newly followed thread in
// (a refresh), as Slack's RTM-THREADS handlers do.
func TestThreadsRealtimeLikeSlack(t *testing.T) {
	app, fs := vkFake(t, nil)
	v := NewThreads(newKBHost(app, false))
	e := &vaEnv{size: image.Pt(1000, 1100)}
	e.draw = func(gtx layout.Context) { v.Layout(gtx) }
	e.until(t, "the followed threads", func() bool { p, ok, _ := v.data.get(); return ok && len(p.items) == 2 })
	p, _, _ := v.data.get()
	if v.unread(&p.items[0]) != 1 {
		t.Fatalf("unread %d, want 1", v.unread(&p.items[0]))
	}
	views := fs.CallCount("subscriptions.thread.getView")
	fs.Push(map[string]any{"type": "thread_marked", "subscription": map[string]any{
		"type": "thread", "channel": fakeslack.ViewsChannel, "thread_ts": fakeslack.ViewsThreadUnread,
		"last_read": "1762600200.000100", "active": true}, "event_ts": "1762600300.000100"})
	e.until(t, "the thread marked read in place", func() bool {
		p, _, _ := v.data.get()
		return v.unread(&p.items[0]) == 0
	})
	for i := 0; i < 20; i++ {
		e.frame()
		time.Sleep(10 * time.Millisecond)
	}
	if n := fs.CallCount("subscriptions.thread.getView"); n != views {
		t.Errorf("thread_marked reloaded the view (%d getView calls, was %d)", n, views)
	}
	fs.Push(map[string]any{"type": "thread_subscribed", "subscription": map[string]any{
		"type": "thread", "channel": fakeslack.ViewsChannel, "thread_ts": fakeslack.ViewsThreadRead,
		"last_read": "1762500100.000100", "active": true}, "event_ts": "1762600400.000100"})
	e.until(t, "the refresh after thread_subscribed", func() bool {
		return fs.CallCount("subscriptions.thread.getView") > views
	})
}

// The Threads view's order (dogfood, α.104). Slack's client sorts the
// threads itself on every load: processThreads (boot-shared zoZ+) and
// ThreadsView's sortThreads (8orQ) put threads with unread replies first,
// then each group newest activity first, whatever order the server sent.
// Between loads the order holds (threadKeysOrdered, per refreshId): a
// reply in a listed thread leaves it where it is
// (maybeUpdateAllThreadsOnNewMessage), a followed thread the list doesn't
// have goes on top (prependThread), the next page goes after the loaded
// ones sorted only within itself (appendMoreThreads), and mounting the
// view again or clicking its sidebar row again loads and sorts afresh.
const (
	thrA, thrAReply = "1762690000.000100", "1762693000.000100" // today's unread thread
	thrB, thrBReply = "1762590000.000100", "1762593000.000100" // yesterday's unread thread
	thrC, thrCReply = "1762695000.000100", "1762699000.000100" // read, the newest activity: the viewer's own reply
	thrD, thrDReply = "1762790000.000100", "1762790500.000100" // a followed thread the list doesn't have yet
	thrLate         = "1762800000.000100"                      // a new reply in thrC
)

// thrOrderFix is one getView thread: its root, one reply from user, unread
// or not.
func thrOrderFix(ch, root, reply, user string, unread bool) map[string]any {
	r := slack.Message{Type: "message", Channel: ch, TS: root, ThreadTS: root, User: "U0FAKE00001", Text: "a synthetic root"}
	r.ReplyCount, r.ReplyUsers, r.LatestReply = 1, []string{user}, reply
	x := slack.Message{Type: "message", Channel: ch, TS: reply, ThreadTS: root, User: user, Text: "a synthetic reply"}
	u := []slack.Message{}
	if unread {
		u = []slack.Message{x}
	}
	return map[string]any{"root_msg": r, "latest_replies": []slack.Message{x}, "unread_replies": u}
}

func thrOrderAnswer(ths ...map[string]any) map[string]any {
	return map[string]any{"ok": true, "threads": ths, "has_more": false, "total_unread_replies": 2,
		"threads_state": map[string]any{"has_unreads": true, "mention_count": 0}}
}

// thrRoots is the view's threads by root, in its order.
func thrRoots(v *Threads) (out []string) {
	p, _, _ := v.data.get()
	for _, it := range p.items {
		out = append(out, it.Root.TS)
	}
	return
}

// thrViewEnv draws v and waits for n threads.
func thrViewEnv(t *testing.T, v *Threads, n int) *vaEnv {
	t.Helper()
	e := &vaEnv{size: image.Pt(1000, 1100)}
	e.draw = func(gtx layout.Context) { v.Layout(gtx) }
	e.until(t, fmt.Sprintf("%d threads", n), func() bool { return len(thrRoots(v)) == n && !v.data.busy() })
	return e
}

// The α.104 case: the unread threads (today's, then yesterday's), then the
// read one with the newest activity (the viewer's own reply), whatever
// order the answer has.
func TestThreadsOrderUnreadFirstLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	a := thrOrderFix(ch, thrA, thrAReply, "U0FAKE00002", true)
	b := thrOrderFix(ch, thrB, thrBReply, "U0FAKE00002", true)
	c := thrOrderFix(ch, thrC, thrCReply, fakeslack.SelfID, false)
	for _, tc := range []struct {
		name   string
		answer []map[string]any
	}{
		{"newest first", []map[string]any{c, a, b}},
		{"any order", []map[string]any{b, c, a}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, fs := vaFake(t)
			fs.SetHook(func(method string, _ url.Values) (any, bool) {
				if method == "subscriptions.thread.getView" {
					return thrOrderAnswer(tc.answer...), true
				}
				return nil, false
			})
			v := NewThreads(newKBHost(app, false))
			thrViewEnv(t, v, 3)
			if got, want := thrRoots(v), []string{thrA, thrB, thrC}; !slices.Equal(got, want) {
				t.Errorf("order %v, Slack's %v (unread first, then the newest activity)", got, want)
			}
		})
	}
}

// A reply in a listed read thread leaves it where it is, its card gaining
// the reply; a followed thread the list doesn't have comes in on top.
func TestThreadsOrderHeldOnReplyLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	var replied, followed atomic.Bool
	app, fs := vkFake(t, func(method string, _ url.Values) (any, bool) {
		if method != "subscriptions.thread.getView" {
			return nil, false
		}
		c := thrOrderFix(ch, thrC, thrCReply, fakeslack.SelfID, false)
		if replied.Load() {
			c = thrOrderFix(ch, thrC, thrLate, "U0FAKE00002", true)
		}
		ths := []map[string]any{c, thrOrderFix(ch, thrA, thrAReply, "U0FAKE00002", true)}
		if followed.Load() {
			ths = append([]map[string]any{thrOrderFix(ch, thrD, thrDReply, "U0FAKE00002", true)}, ths...)
		}
		return thrOrderAnswer(ths...), true
	})
	v := NewThreads(newKBHost(app, false))
	e := thrViewEnv(t, v, 2)
	if got := thrRoots(v); !slices.Equal(got, []string{thrA, thrC}) {
		t.Fatalf("order %v, want the unread thread first", got)
	}
	refresh := func(what string, set *atomic.Bool, ev map[string]any) {
		t.Helper()
		n := fs.CallCount("subscriptions.thread.getView")
		set.Store(true)
		fs.Push(ev)
		e.until(t, what, func() bool { return fs.CallCount("subscriptions.thread.getView") > n && !v.data.busy() })
		e.frame()
	}
	refresh("the refresh after a reply", &replied, map[string]any{"type": "message", "channel": ch, "user": "U0FAKE00002",
		"text": "a synthetic late reply", "ts": thrLate, "thread_ts": thrC, "event_ts": thrLate})
	if got, want := thrRoots(v), []string{thrA, thrC}; !slices.Equal(got, want) {
		t.Errorf("order %v after a reply in the read thread; Slack keeps a listed thread where it is: %v", got, want)
	}
	p, _, _ := v.data.get()
	// the card gains the reply and keeps the one it had (getView's window
	// moved past it): Slack's card draws from a first reply fixed per load
	if r := p.items[len(p.items)-1].Replies; len(r) != 2 || r[0].TS != thrCReply || r[1].TS != thrLate {
		t.Errorf("the read thread's card has replies %v, want the one it had and the new one", thrTSs(r))
	}
	// the view refreshes at most every 5 s: the next one at once
	v.w.mu.Lock()
	v.w.last = time.Time{}
	v.w.mu.Unlock()
	refresh("the refresh after a new thread's reply", &followed, map[string]any{"type": "message", "channel": ch, "user": "U0FAKE00002",
		"text": "a synthetic reply", "ts": thrDReply, "thread_ts": thrD, "event_ts": thrDReply})
	if got, want := thrRoots(v), []string{thrD, thrA, thrC}; !slices.Equal(got, want) {
		t.Errorf("order %v after a reply in a thread the list didn't have; Slack puts it first: %v", got, want)
	}
}

// The next page goes after the threads loaded, sorted within itself: its
// unread thread doesn't jump above page 1's read one.
func TestThreadsPageAppendedLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	app, fs := vaFake(t)
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method != "subscriptions.thread.getView" {
			return nil, false
		}
		if f.Get("max_ts") == "" {
			ans := thrOrderAnswer(thrOrderFix(ch, thrC, thrCReply, fakeslack.SelfID, false))
			ans["has_more"], ans["max_ts"] = true, thrCReply
			return ans, true
		}
		return thrOrderAnswer(thrOrderFix(ch, thrB, thrBReply, "U0FAKE00002", true)), true
	})
	v := NewThreads(newKBHost(app, false))
	thrViewEnv(t, v, 2)
	if got, want := thrRoots(v), []string{thrC, thrB}; !slices.Equal(got, want) {
		t.Errorf("order %v after the second page, Slack's %v (the page appended)", got, want)
	}
}

// Mounting the view again (the route back on it) and clicking its sidebar
// row while it shows load the threads afresh and sort them again; until
// the answer is in, the threads loaded before stay on screen. The click
// scrolls to the top first.
func TestThreadsReloadLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	var phase atomic.Int32
	app, fs := vaFake(t)
	fs.SetHook(func(method string, _ url.Values) (any, bool) {
		if method != "subscriptions.thread.getView" {
			return nil, false
		}
		read := phase.Load() == 1 // a read, c has a new reply
		return thrOrderAnswer(thrOrderFix(ch, thrA, thrAReply, "U0FAKE00002", !read),
			thrOrderFix(ch, thrC, thrLate, "U0FAKE00002", read)), true
	})
	v := NewThreads(newKBHost(app, false))
	e := thrViewEnv(t, v, 2)
	if got := thrRoots(v); !slices.Equal(got, []string{thrA, thrC}) {
		t.Fatalf("order %v, want the unread thread first", got)
	}
	e.size = image.Pt(1000, 300) // the list scrolls
	e.frame()
	for _, tc := range []struct {
		name   string
		phase  int32
		reopen func()
		want   []string
	}{
		{"mounted again", 1, v.PageLeft, []string{thrC, thrA}},
		{"its row clicked again", 2, v.Reselect, []string{thrA, thrC}},
	} {
		n := fs.CallCount("subscriptions.thread.getView")
		phase.Store(tc.phase)
		v.list.Position.First, v.list.Position.Offset = 1, 40
		for _, c := range v.cards { // older replies a card loaded fold away with the new refreshId
			c.older, c.olderStart = []core.Message{{TS: "1762500000.000100"}}, true
		}
		tc.reopen()
		for k, c := range v.cards {
			if len(c.older) > 0 || c.olderStart {
				t.Errorf("%s: %s keeps its older replies", tc.name, k)
			}
		}
		e.frame()
		if p := v.list.Position; p.First != 0 || p.Offset != 0 {
			t.Errorf("%s: the list at %+v, want its top", tc.name, p)
		}
		if len(thrRoots(v)) != 2 {
			t.Errorf("%s: the threads went while the view loads (%v)", tc.name, thrRoots(v))
		}
		e.until(t, tc.name+": the reload", func() bool { return fs.CallCount("subscriptions.thread.getView") > n && !v.data.busy() })
		e.frame()
		if got := thrRoots(v); !slices.Equal(got, tc.want) {
			t.Errorf("%s: order %v, Slack's %v", tc.name, got, tc.want)
		}
	}
}

// thrTSs is the messages' timestamps.
func thrTSs(ms []core.Message) (out []string) {
	for _, m := range ms {
		out = append(out, m.TS)
	}
	return
}

// thrFixRead is thrOrderFix for a followed thread its viewer last read at
// lastRead ("" never).
func thrFixRead(ch, root, reply, user string, unread bool, lastRead string) map[string]any {
	f := thrOrderFix(ch, root, reply, user, unread)
	r := f["root_msg"].(slack.Message)
	r.Subscribed, r.LastRead = true, lastRead
	f["root_msg"] = r
	return f
}

// thrGate holds the fake's getView answers while it is shut: a load the
// view sent stays out until open.
type thrGate struct {
	mu   sync.Mutex
	shut chan struct{}
}

func (g *thrGate) hold() {
	g.mu.Lock()
	g.shut = make(chan struct{})
	g.mu.Unlock()
}

func (g *thrGate) open() {
	g.mu.Lock()
	if g.shut != nil {
		close(g.shut)
		g.shut = nil
	}
	g.mu.Unlock()
}

func (g *thrGate) wait() {
	g.mu.Lock()
	c := g.shut
	g.mu.Unlock()
	if c != nil {
		<-c
	}
}

const (
	thrE, thrEReply = "1762490000.000100", "1762490500.000100" // read before, unread now
	thrX, thrXReply = "1762300000.000100", "1762300500.000100" // an old read thread
)

// The "You’re up to date" divider as get_rows_and_keys draws it
// (client-threads-view 9Ra5): before firstReadThreadKey as soon as the
// load lands, with an unread thread above it. It hides only for a thread
// from it down (lodash dropWhile, NbLH) with a reply from someone else
// newer than the last read it came into the list with
// (initialLastReadByThread). So a reply in the read thread keeps it (that
// thread came in read), and so does a page with an unread thread never
// read; a page with an unread thread read before hides it.
func TestThreadsUpToDateDividerLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	const page2, page3 = "1762500000.000100", "1762400000.000100" // the cursors
	var replied atomic.Bool
	app, fs := vkFake(t, func(method string, f url.Values) (any, bool) {
		if method != "subscriptions.thread.getView" {
			return nil, false
		}
		switch f.Get("max_ts") {
		case page2:
			ans := thrOrderAnswer(thrFixRead(ch, thrB, thrBReply, "U0FAKE00002", true, ""))
			ans["max_ts"] = page3
			return ans, true
		case page3:
			return thrOrderAnswer(thrFixRead(ch, thrE, thrEReply, "U0FAKE00002", true, thrE)), true
		}
		c := thrFixRead(ch, thrC, thrCReply, fakeslack.SelfID, false, thrCReply)
		if replied.Load() {
			c = thrFixRead(ch, thrC, thrLate, "U0FAKE00002", true, thrCReply)
		}
		ans := thrOrderAnswer(thrFixRead(ch, thrA, thrAReply, "U0FAKE00002", true, thrA), c)
		ans["max_ts"] = page2
		return ans, true
	})
	v := NewThreads(newKBHost(app, false))
	e := thrViewEnv(t, v, 2)
	e.frame()
	if v.divAt != 1 {
		t.Fatalf("divider at %d after the load, want 1: before the read thread, with the unread one above it", v.divAt)
	}
	n := fs.CallCount("subscriptions.thread.getView")
	replied.Store(true)
	fs.Push(map[string]any{"type": "message", "channel": ch, "user": "U0FAKE00002", "text": "a synthetic late reply",
		"ts": thrLate, "thread_ts": thrC, "event_ts": thrLate})
	e.until(t, "the refresh after a reply", func() bool { return fs.CallCount("subscriptions.thread.getView") > n && !v.data.busy() })
	e.frame()
	if p, _, _ := v.data.get(); v.unread(&p.items[1]) == 0 {
		t.Fatal("the read thread's new reply didn't come in")
	}
	if v.divAt != 1 {
		t.Errorf("divider at %d after a reply in the thread under it, want 1 (that thread came in read)", v.divAt)
	}
	for _, st := range []struct {
		what    string
		n, want int
	}{
		{"a page with an unread thread never read", 3, 1},
		{"a page with an unread thread read before", 4, -1},
	} {
		v.load(actMore)
		e.until(t, st.what, func() bool { return len(thrRoots(v)) == st.n && !v.data.busy() })
		e.frame()
		if v.divAt != st.want {
			t.Errorf("%s: divider at %d, want %d", st.what, v.divAt, st.want)
		}
	}
}

// A refresh puts on top only a thread a reply or thread_subscribed names
// (maybeUpdateAllThreadsOnNewMessage, prependThread). An old thread that
// only moved into page 1 because a listed one left it (unfollowed, its
// root deleted, its channel left) has no event: it stays out until the
// next page brings it, after the list (appendMoreThreads).
func TestThreadsRefreshKeepsOldThreadsOffTopLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	type th struct{ root, reply string }
	a := th{"1762610000.000100", "1762619000.000100"} // unread
	b := th{"1762611000.000100", "1762618000.000100"} // leaves page 1
	rest := []th{{"1762612000.000100", "1762617000.000100"}, {"1762613000.000100", "1762616000.000100"},
		{"1762614000.000100", "1762615000.000100"}, {"1762614500.000100", "1762614800.000100"}}
	d := th{"1762500000.000100", "1762500500.000100"} // on page 2 until b leaves page 1
	const page2 = "1762600000.000100"
	for _, tc := range []struct {
		name  string
		other bool // b in another channel
		event func(fs *fakeslack.Server, bch string)
	}{
		{"unfollowed", false, func(fs *fakeslack.Server, bch string) {
			fs.Push(map[string]any{"type": "thread_unsubscribed", "subscription": map[string]any{"type": "thread", "channel": bch,
				"thread_ts": b.root, "last_read": b.reply, "active": false}, "event_ts": "1762620000.000100"})
		}},
		{"root deleted", false, func(fs *fakeslack.Server, bch string) {
			fs.Push(map[string]any{"type": "message", "subtype": "message_deleted", "channel": bch, "deleted_ts": b.root,
				"hidden": true, "ts": "1762620000.000100", "event_ts": "1762620000.000100"})
		}},
		{"channel left", true, func(fs *fakeslack.Server, bch string) {
			fs.Push(map[string]any{"type": "channel_left", "channel": bch, "event_ts": "1762620000.000100"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gone atomic.Bool
			bch := ch
			app, fs := vkFake(t, func(method string, f url.Values) (any, bool) {
				if method != "subscriptions.thread.getView" {
					return nil, false
				}
				if f.Get("max_ts") == page2 {
					return thrOrderAnswer(thrFixRead(ch, d.root, d.reply, "U0FAKE00002", false, d.reply)), true
				}
				ths := []map[string]any{thrFixRead(ch, a.root, a.reply, "U0FAKE00002", true, a.root)}
				if !gone.Load() {
					ths = append(ths, thrFixRead(bch, b.root, b.reply, "U0FAKE00002", false, b.reply))
				}
				for _, x := range rest {
					ths = append(ths, thrFixRead(ch, x.root, x.reply, "U0FAKE00002", false, x.reply))
				}
				if gone.Load() {
					ths = append(ths, thrFixRead(ch, d.root, d.reply, "U0FAKE00002", false, d.reply))
				}
				ans := thrOrderAnswer(ths...)
				ans["has_more"], ans["max_ts"] = true, page2
				return ans, true
			})
			if tc.other {
				for _, id := range fs.ChannelIDs() {
					if id != ch {
						bch = id
						break
					}
				}
			}
			v := NewThreads(newKBHost(app, false))
			e := &vaEnv{size: image.Pt(1000, 300)} // page 2 waits until the list's end shows
			e.draw = func(gtx layout.Context) { v.Layout(gtx) }
			e.until(t, "page 1", func() bool { return len(thrRoots(v)) == 6 && !v.data.busy() })
			gone.Store(true)
			tc.event(fs, bch)
			n := fs.CallCount("subscriptions.thread.getView")
			fs.Push(map[string]any{"type": "message", "channel": ch, "user": "U0FAKE00002", "text": "a synthetic reply",
				"ts": "1762619500.000100", "thread_ts": a.root, "event_ts": "1762619500.000100"})
			e.until(t, "the refresh", func() bool { return fs.CallCount("subscriptions.thread.getView") > n && !v.data.busy() })
			e.frame()
			roots := thrRoots(v)
			if roots[0] != a.root || slices.Contains(roots, d.root) {
				t.Fatalf("after the refresh: %v; want %s first and the old thread %s out until page 2", roots, a.root, d.root)
			}
			v.list.Position.First = len(roots) - 1 // the list's end on screen: page 2 brings it, after the list
			e.until(t, "page 2", func() bool { return len(thrRoots(v)) > len(roots) && !v.data.busy() })
			if r := thrRoots(v); r[len(r)-1] != d.root {
				t.Errorf("after page 2: %v, want %s last", r, d.root)
			}
		})
	}
}

// Mounting the view again, as Slack's new ThreadsView instance: until
// loadThreads' answer lands, the threads it holds are sorted afresh on
// what the view has read and without the ones unfollowed meanwhile
// (sortThreads over the "subscribed" filter), with the floating loader
// over them, no divider (isDoingGlobalRefresh) and no active row; the
// answer then moves nothing.
func TestThreadsMountLoadLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	var g thrGate
	t.Cleanup(g.open)
	var after atomic.Bool // the server after the visit: a read, x unfollowed
	app, fs := vaFake(t)
	fs.SetHook(func(method string, _ url.Values) (any, bool) {
		if method != "subscriptions.thread.getView" {
			return nil, false
		}
		g.wait()
		if after.Load() {
			return thrOrderAnswer(thrFixRead(ch, thrA, thrAReply, "U0FAKE00002", false, thrAReply),
				thrFixRead(ch, thrC, thrCReply, fakeslack.SelfID, false, thrCReply)), true
		}
		return thrOrderAnswer(thrFixRead(ch, thrA, thrAReply, "U0FAKE00002", true, thrA),
			thrFixRead(ch, thrC, thrCReply, fakeslack.SelfID, false, thrCReply),
			thrFixRead(ch, thrX, thrXReply, "U0FAKE00002", false, thrXReply)), true
	})
	v := NewThreads(newKBHost(app, false))
	e := thrViewEnv(t, v, 3)
	e.frame()
	if got := thrRoots(v); !slices.Equal(got, []string{thrA, thrC, thrX}) || v.divAt != 1 {
		t.Fatalf("order %v, divider at %d; want [a c x], the divider before c", got, v.divAt)
	}
	// while the view is away: a read from its reply box, x unfollowed in
	// its thread pane, a row active
	p, _, _ := v.data.get()
	v.markRead(&p.items[0])
	app.S.SetThreadSub(ch, thrX, func(s *store.ThreadSub) { s.Subscribed = false })
	v.kb.active, v.kb.initial = "heading-"+p.items[1].key, "heading-"+p.items[1].key
	after.Store(true)
	g.hold()
	v.PageLeft()
	e.frame()
	want := []string{thrC, thrA} // both read now: the newest activity first
	if got := thrRoots(v); !slices.Equal(got, want) {
		t.Errorf("while the mount loads: %v, want %v", got, want)
	}
	if !v.mounting || v.divAt != -1 {
		t.Errorf("while the mount loads: mounting %v, divider at %d; want the floating loader and no divider", v.mounting, v.divAt)
	}
	if v.kb.active != "" || v.kb.initial != "" {
		t.Errorf("the new list's active row %q (initial %q), want none", v.kb.active, v.kb.initial)
	}
	vaShot(t, e, "threads-mount-loading", false)
	g.open()
	e.until(t, "the mount's load", v.Settled)
	e.frame()
	if got := thrRoots(v); !slices.Equal(got, want) || v.mounting {
		t.Errorf("after the mount's load: %v (mounting %v), want %v and no loader", got, v.mounting, want)
	}
}

// A reload that fails, on a revisit or the sidebar row clicked again,
// shows Slack's error view (loadThreads' errored) whatever threads the
// view holds; Try Again loads the list again.
func TestThreadsFailedReloadLikeSlack(t *testing.T) {
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
	for _, tc := range []struct {
		name   string
		reload func()
	}{
		{"mounted again", v.PageLeft},
		{"its row clicked again", v.Reselect},
	} {
		fs.SetFault("subscriptions.thread.getView", fakeslack.Fault{Error: "fatal_error", Times: 1})
		tc.reload()
		e.until(t, tc.name+": the failed reload", func() bool { return v.loadErr != nil && v.Settled() })
		e.frame()
		if len(thrRoots(v)) == 0 {
			t.Fatalf("%s: the threads went", tc.name)
		}
		vaShot(t, e, "threads-reload-error", false)
		v.load(actFresh) // Try Again
		e.until(t, tc.name+": Try Again", func() bool { return v.loadErr == nil && v.Settled() && len(thrRoots(v)) == 2 })
	}
}

// A reply that comes while a reload is out isn't lost. The reload's answer
// didn't have it, so a refresh follows the reload and brings the newly
// followed thread in on top.
func TestThreadsReplyDuringReloadLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	var g thrGate
	t.Cleanup(g.open)
	var withD atomic.Bool
	arrived := make(chan struct{}, 8)
	app, fs := vkFake(t, func(method string, _ url.Values) (any, bool) {
		if method != "subscriptions.thread.getView" {
			return nil, false
		}
		ths := []map[string]any{thrFixRead(ch, thrA, thrAReply, "U0FAKE00002", true, thrA),
			thrFixRead(ch, thrC, thrCReply, fakeslack.SelfID, false, thrCReply)}
		if withD.Load() {
			ths = append(ths, thrFixRead(ch, thrD, thrDReply, "U0FAKE00002", true, thrD))
		}
		ans := thrOrderAnswer(ths...) // the server's state when the request came
		select {
		case arrived <- struct{}{}:
		default:
		}
		g.wait()
		return ans, true
	})
	v := NewThreads(newKBHost(app, false))
	e := thrViewEnv(t, v, 2)
	for len(arrived) > 0 {
		<-arrived
	}
	n := fs.CallCount("subscriptions.thread.getView")
	g.hold()
	v.Reselect()
	select {
	case <-arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("the reload's request never came")
	}
	withD.Store(true)
	fs.Push(map[string]any{"type": "message", "channel": ch, "user": "U0FAKE00002", "text": "a synthetic reply",
		"ts": thrDReply, "thread_ts": thrD, "event_ts": thrDReply})
	for i := 0; i < 40; i++ { // the reply makes the view stale while the reload is out
		e.frame()
		time.Sleep(5 * time.Millisecond)
	}
	g.open()
	e.until(t, "the refresh after the reload", func() bool {
		return fs.CallCount("subscriptions.thread.getView") >= n+2 && v.Settled()
	})
	e.frame()
	if got := thrRoots(v); len(got) != 3 || got[0] != thrD {
		t.Errorf("after the reload and its refresh: %v, want %s first", got, thrD)
	}
}

// Drafts & sent: the DraftList's keys (every row focusable, Enter is the
// row's click: a draft opens in its conversation) and the list taking a
// draft written on another device (draft_create).
func TestDraftsKeysAndRealtimeLikeSlack(t *testing.T) {
	app, fs := vkFake(t, nil)
	h := newKBHost(app, false)
	d := NewDrafts(h)
	e := &vaEnv{size: image.Pt(1000, 900)}
	e.draw = func(gtx layout.Context) {
		d.Layout(gtx)
		for _, o := range h.open() {
			o.Layout(gtx)
		}
	}
	e.until(t, "drafts", func() bool { return len(d.drafts) == 1 && len(d.scheduled) == 2 })
	e.frame()
	e.frame()
	kb := d.kb[draftsTabDrafts]
	k := "d:" + d.drafts[0].ID
	listY := e.size.Y - kb.viewH
	kbClickRow(t, e, kb, k, listY)
	if kb.active != k || kb.ring {
		t.Fatalf("after a press: active %q ring %v", kb.active, kb.ring)
	}
	h.mu.Lock()
	h.navs = nil
	h.mu.Unlock()
	kbPress(e, key.NameUpArrow, 0)
	if kb.active == k && d.view[0].kind == draftsEntryCard {
		t.Errorf("↑ didn't reach the education card")
	}
	kbPress(e, key.NameEnd, 0)
	if kb.active != k || !kb.ring {
		t.Fatalf("End: active %q ring %v, want the draft with the ring", kb.active, kb.ring)
	}
	vaShot(t, e, "drafts-keyboard-focus", false)
	kbPress(e, key.NameReturn, 0)
	if nav, _ := h.last(); nav != "c/"+fakeslack.ViewsChannel {
		t.Errorf("Enter on the draft went to %q", nav)
	}

	id := fs.DraftElsewhere("a synthetic draft from another device")
	e.until(t, "the other device's draft", func() bool {
		for _, x := range d.drafts {
			if x.ID == id {
				return true
			}
		}
		return false
	})
}

// To-dos: InteractiveList (arrows step, PageDown jumps a page of 10,
// Home/End), E completes the active to-do, Delete clears it behind an
// undoable "To-do cleared" toast, and a saved_added event reloads.
func TestTodosKeysAndRealtimeLikeSlack(t *testing.T) {
	var mu sync.Mutex
	items := []map[string]any{}
	for i := 0; i < 12; i++ {
		items = append(items, map[string]any{"item_id": fakeslack.ViewsChannel, "item_type": "message",
			"ts": fmt.Sprintf("%d.000100", 1762601000+i), "todo_state": "to_do", "date_created": 1762600000 + i})
	}
	// E's write reloads the feeds; CI once saw that reload land between
	// the test picking the row Delete clears and the keys that reach it.
	// The reload is held at the server until the row is picked, then
	// applied before the keys, every run.
	var completed atomic.Bool
	listHeld, release := make(chan struct{}), make(chan struct{})
	var heldOnce sync.Once
	log := &callLog{}
	app, fs := vkFake(t, func(method string, f url.Values) (any, bool) {
		log.rec(method, f)
		switch method {
		case "saved.update": // as the server: the completed to-do leaves the to-do list
			if f.Get("mark") != "completed" {
				break
			}
			mu.Lock()
			for i, it := range items {
				if it["ts"] == f.Get("ts") {
					done := maps.Clone(it) // a list reply may still be encoding the old one
					done["todo_state"], done["date_completed"] = "completed", time.Now().Unix()
					items[i] = done
				}
			}
			mu.Unlock()
			completed.Store(true)
		case "saved.delete":
			mu.Lock()
			items = slices.DeleteFunc(slices.Clone(items), func(it map[string]any) bool { return it["ts"] == f.Get("ts") })
			mu.Unlock()
		case "saved.list":
			if completed.Load() {
				heldOnce.Do(func() { close(listHeld) })
				select {
				case <-release:
				case <-time.After(10 * time.Second):
				}
			}
			mu.Lock()
			defer mu.Unlock()
			return map[string]any{"ok": true, "saved_items": append([]map[string]any(nil), items...)}, true
		}
		return nil, false
	})
	h := newKBHost(app, false)
	l := NewLater(h)
	e := &vaEnv{size: image.Pt(1000, 900)}
	e.draw = func(gtx layout.Context) { l.Layout(gtx) }
	e.until(t, "the to-dos", func() bool { return len(l.view) == 12 })
	e.frame()
	e.frame()
	first := laterKey(&l.view[0])
	kbClickRow(t, e, l.kb, first, e.size.Y-l.kb.viewH)
	if l.kb.active != first || l.kb.ring {
		t.Fatalf("after a press: active %q ring %v", l.kb.active, l.kb.ring)
	}
	kbPress(e, key.NameDownArrow, 0)
	if want := laterKey(&l.view[1]); l.kb.active != want || !l.kb.ring {
		t.Fatalf("↓: %q ring %v, want %q", l.kb.active, l.kb.ring, want)
	}
	kbPress(e, key.NameRightArrow, 0)
	if want := laterKey(&l.view[2]); l.kb.active != want {
		t.Fatalf("→: %q, want %q", l.kb.active, want)
	}
	vaShot(t, e, "todos-keyboard-focus", false)
	kbPress(e, key.NamePageDown, 0)
	if want := laterKey(&l.view[10]); l.kb.active != want {
		t.Fatalf("PageDown from row 2: %q, want row 10", l.kb.active)
	}
	kbPress(e, key.NameHome, 0)
	if l.kb.active != first {
		t.Fatalf("Home: %q", l.kb.active)
	}
	kbPress(e, "E", 0)
	e.until(t, "saved.update mark=completed", func() bool {
		return log.count("saved.update", func(f url.Values) bool { return f.Get("mark") == "completed" || f.Get("todo_state") == "completed" }) > 0
	})
	todoUndoFor = 50 * time.Millisecond
	t.Cleanup(func() { todoUndoFor = 6 * time.Second })
	e.until(t, "the reload after E at the server", func() bool {
		select {
		case <-listHeld:
			return true
		default:
			return false
		}
	})
	gone := laterKey(&l.view[1])
	close(release)
	e.until(t, "the reload after E applied", func() bool { return laterSettled(l) })
	kbPress(e, key.NameHome, 0)
	kbPress(e, key.NameDownArrow, 0)
	kbPress(e, key.NameDeleteForward, 0)
	h.mu.Lock()
	undos := append([]string(nil), h.undos...)
	h.mu.Unlock()
	if len(undos) != 1 || undos[0] != "To-do cleared" {
		t.Fatalf("Delete: toasts %q", undos)
	}
	for _, it := range l.view {
		if laterKey(&it) == gone {
			t.Fatal("Delete left the to-do in the list")
		}
	}
	e.until(t, "saved.delete once the toast is gone", func() bool { return log.count("saved.delete", nil) == 1 })

	lists := log.count("saved.list", nil)
	mu.Lock()
	items = append(items, map[string]any{"item_id": fakeslack.ViewsChannel, "item_type": "message", "ts": "1762700000.000100", "todo_state": "to_do", "date_created": 1762700000})
	mu.Unlock()
	fs.Push(map[string]any{"type": "saved_added", "saved": map[string]any{"item_id": fakeslack.ViewsChannel, "item_type": "message",
		"ts": "1762700000.000100", "todo_state": "to_do", "date_created": 1762700000}, "event_ts": "1762700000.000200"})
	e.until(t, "the reload after saved_added", func() bool { return log.count("saved.list", nil) > lists })
}

// laterSettled: every feed's first page that was asked for is in.
func laterSettled(l *Later) bool {
	for _, f := range l.feeds {
		if !f.first.started() {
			continue
		}
		v, ok, _ := f.first.get()
		if f.first.busy() || !ok || v.seq != f.firstApplied {
			return false
		}
	}
	return true
}

// Unreads: the virtual list's own Home/End under UnreadsList's commands,
// and the ring only in keyboard mode.
func TestUnreadsHomeEndLikeSlack(t *testing.T) {
	v, _, e, _ := unrEnv(t, false, nil)
	e.click(image.Pt(500, 300))
	if !v.focused || v.kbMode {
		t.Fatalf("after a press: focused %v keyboard mode %v", v.focused, v.kbMode)
	}
	kbPress(e, key.NameEnd, 0)
	if v.active < 0 || v.rows[v.active].kind != unrFooter || !v.kbMode {
		t.Fatalf("End: row %d (keyboard mode %v), want the footer", v.active, v.kbMode)
	}
	kbPress(e, key.NameHome, 0)
	if v.active < 0 || v.rows[v.active].kind != unrHeader {
		t.Fatalf("Home: row %d, want the first header", v.active)
	}
}

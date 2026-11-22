package ui

// The threads-ship review's blockers in the thread pane (wf_15e6e96d-4f5;
// /root/clack-ops/next-briefs/threads-ship-findings.md): a pane closed or
// hidden while it pages, reopened after a jump, refreshed by a poll with
// the socket away, and Mod+Z and Up reading the slice the reply box sits
// on. Each is red on e89cc02a. Synthetic data only.

import (
	"context"
	"fmt"
	"image"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/messages"
)

// shipFake is a fake whose threads answer a page at a time, as live
// Slack does.
func shipFake(t *testing.T) *fakeslack.Server {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	fs.PagedReplies = true
	return fs
}

// readChannel is a channel the viewer is in with nothing unread.
func readChannel(t *testing.T, app *core.App, fs *fakeslack.Server) string {
	t.Helper()
	for _, id := range fs.ChannelIDs()[1:] {
		if c := app.Count(id); !c.Unread && c.Mentions == 0 {
			return id
		}
	}
	t.Fatal("no read channel in the fixture")
	return ""
}

// shipThread posts a synthetic thread of n replies in ch, unpushed: the
// root two hours old, a reply a second after it, by author(i) (the
// directory's people in turn when nil); reply(i) is reply i's ts.
func shipThread(fs *fakeslack.Server, ch string, n int, author func(int) string) (root string, reply func(int) string) {
	base := time.Now().Add(-2 * time.Hour).Unix()
	root = fmt.Sprintf("%d.000100", base)
	us := fs.UserIDs()
	if author == nil {
		author = func(i int) string { return us[1+i%3] }
	}
	fs.PostAtQuiet(ch, root, us[1], "synthetic: a thread for the threads-ship tests")
	reply = func(i int) string { return fmt.Sprintf("%d.000100", base+int64(i)) }
	for i := 1; i <= n; i++ {
		fs.ReplyAtQuiet(ch, root, reply(i), author(i), fmt.Sprintf("synthetic reply %d", i))
	}
	return root, reply
}

func paneHas(tv *messages.ThreadView, ts string) bool {
	if tv == nil {
		return false
	}
	for _, r := range tv.RowRects() {
		if r.TS == ts {
			return true
		}
	}
	return false
}

// paneMid is a point in the middle of the pane's list.
func paneMid(tv *messages.ThreadView) image.Point {
	r := tv.ListRect()
	return image.Pt((r.Min.X+r.Max.X)/2, (r.Min.Y+r.Max.Y)/2)
}

// newerPages counts a thread's loadPost reads ({oldest} past the root),
// from the fake's hook.
type newerPages struct {
	mu       sync.Mutex
	ch, root string
	n        atomic.Int32
}

func (p *newerPages) set(ch, root string) {
	p.mu.Lock()
	p.ch, p.root = ch, root
	p.mu.Unlock()
}

func (p *newerPages) see(method string, form url.Values) {
	p.mu.Lock()
	ch, root := p.ch, p.root
	p.mu.Unlock()
	if method == "conversations.replies" && ch != "" && form.Get("channel") == ch && form.Get("ts") == root &&
		form.Get("oldest") != "" && form.Get("oldest") != root && form.Get("latest") == "" {
		p.n.Add(1)
	}
}

// waitPoll waits for core's next poll (pollLoop, every 20 s with the
// socket off, as in these tests): its "poll" event.
func waitPoll(t *testing.T, app *core.App) {
	t.Helper()
	evs, stop := app.Subscribe()
	defer stop()
	dl := time.After(45 * time.Second)
	for {
		select {
		case ev := <-evs:
			if ev.Type == "poll" {
				return
			}
		case <-dl:
			t.Fatal("no poll within 45 s")
		}
	}
}

// TestThreadPaneClosedWhilePageLoads (finding 3): a thread in a public
// channel the viewer isn't in (Karim's huge thread is one) whose pane
// closes while its next page is out: closing lets the thread's history go
// (truncateThreadHistory's removeAllTimestamps), and the page lands after
// that. Reopened from the reply bar at once, the pane draws the thread
// again, root and replies, as a pane mounted afresh. The code before kept
// the model "loaded" through the close, and the late page made its
// messages the empty slice at the pane's start in a history holding only
// that page: a blank pane for the next 30 s, and a reply sent there not
// drawn.
func TestThreadPaneClosedWhilePageLoads(t *testing.T) {
	fs := shipFake(t)
	th := fs.AddHugeThread("closed-while-paging", 100000, 40)
	ch, root := th.Channel, th.Root
	reply := fs.HugeThreadReplyTS
	// the first loadPost waits in the fake until released
	release := make(chan struct{})
	var held atomic.Bool
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		if method == "conversations.replies" && form.Get("channel") == ch && form.Get("oldest") != "" && form.Get("oldest") != root &&
			held.CompareAndSwap(false, true) {
			<-release
		}
		return nil, false
	})
	t.Cleanup(func() { free(); fs.SetHook(nil) })
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	app := e.a.data.core
	e.a.OpenThread(ch, root)
	e.until("the root's page", 20*time.Second, func() bool { return paneHas(e.threadView(ch, root), reply(1)) })
	tv := e.threadView(ch, root)
	pt := paneMid(tv)
	for i := 0; !held.Load(); i++ {
		if i > 1500 {
			t.Fatal("no newer page asked for after 1500 wheel steps")
		}
		e.scroll(pt, 80)
		e.frame()
		time.Sleep(2 * time.Millisecond)
	}
	e.until("the channel known", 10*time.Second, func() bool {
		c, ok := app.Conv(ch)
		return ok && c.Kind == "channel" && !c.Member
	})
	e.a.CloseSecondary()
	e.frame()
	e.frame()
	free() // the page lands after the close
	e.settle(500 * time.Millisecond)
	e.a.OpenThread(ch, root) // the reply bar, within 30 s
	e.until("the reopened pane's root and first reply", 15*time.Second, func() bool {
		tv := e.threadView(ch, root)
		return paneHas(tv, root) && paneHas(tv, reply(1))
	})
}

// TestThreadPaneHiddenAndShownKeepsPaging (the review's "pane unmount
// clears th.start but leaves the model loaded"): a 200-reply thread in a
// channel the viewer is in, read down a few pages, its pane hidden and
// shown again (Mod+. twice: the pane unmounts), then scrolled on: the
// next page lands in the slice the pane draws, which stays drawn. The
// code before cleared the model's start at the unmount and kept the model
// loaded; the next page landed as the slice at no start, which is empty
// short of the thread's end, and the pane went blank for 30 s.
func TestThreadPaneHiddenAndShownKeepsPaging(t *testing.T) {
	fs := shipFake(t)
	pages := &newerPages{}
	fs.SetHook(func(method string, form url.Values) (any, bool) { pages.see(method, form); return nil, false })
	t.Cleanup(func() { fs.SetHook(nil) })
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	ch := readChannel(t, e.a.data.core, fs)
	root, reply := shipThread(fs, ch, 200, nil)
	pages.set(ch, root)
	e.a.OpenThread(ch, root)
	e.until("the root's page", 20*time.Second, func() bool { return paneHas(e.threadView(ch, root), reply(1)) })
	scrollPages := func(n int32) {
		t.Helper()
		want := pages.n.Load() + n
		pt := paneMid(e.threadView(ch, root))
		for i := 0; pages.n.Load() < want; i++ {
			if i > 1500 {
				t.Fatalf("%d of %d newer pages after 1500 wheel steps", pages.n.Load(), want)
			}
			e.scroll(pt, 80)
			e.frame()
			time.Sleep(2 * time.Millisecond)
		}
		e.settle(500 * time.Millisecond)
	}
	scrollPages(2)
	e.a.toggleSecondary() // Mod+.: hidden
	e.frame()
	e.frame()
	e.a.toggleSecondary() // and shown
	e.frame()
	e.until("the shown pane's rows", 10*time.Second, func() bool { return len(e.threadView(ch, root).RowRects()) > 0 })
	scrollPages(1)
	if rows := e.threadView(ch, root).RowRects(); len(rows) == 0 {
		t.Fatal("the pane went blank at the page after it was hidden and shown")
	}
}

// TestThreadReopenAfterJumpStartsAtItsStart (finding 4): a pane that
// jumped into the middle of a long thread the viewer doesn't follow (a
// Later item, Activity), closed, and reopened from the reply bar once its
// model is stale (a poll), opens as a pane mounted afresh does: at
// getThreadStartPosition's message, the root, its saved scrollmark being
// a message the slice it draws now doesn't hold. The code before kept the
// list's row index from the jump's slice and laid the root's page out at
// it: near the bottom of that page, paging on, neither the root nor the
// place left.
func TestThreadReopenAfterJumpStartsAtItsStart(t *testing.T) {
	fs := shipFake(t)
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	app := e.a.data.core
	ch := readChannel(t, app, fs)
	root, reply := shipThread(fs, ch, 200, nil)
	e.a.jumpToMessage(ch, reply(100), root, false)
	e.until("the jump's reply in the pane", 20*time.Second, func() bool { return paneHas(e.threadView(ch, root), reply(100)) })
	e.settle(300 * time.Millisecond)
	e.a.CloseSecondary()
	e.frame()
	e.frame()
	waitPoll(t, app) // every model stale, as 30 s would make it
	e.a.OpenThread(ch, root)
	e.until("the reopened pane's rows", 20*time.Second, func() bool { return len(e.threadView(ch, root).RowRects()) > 0 })
	e.settle(500 * time.Millisecond)
	tv := e.threadView(ch, root)
	rows := tv.RowRects()
	if len(rows) == 0 || rows[0].TS != root || rows[0].R.Min.Y > tv.ListRect().Min.Y+40 {
		first := "none"
		if len(rows) > 0 {
			first = rows[0].TS
		}
		t.Errorf("the reopened pane's top row is %s, want the root %s at the top (reply 100 is %s)", first, root, reply(100))
	}
}

// TestThreadPaneRefreshesAtPoll (finding 2, the pane's side): with the
// socket off, the pane tells core which replies it shows, and the next
// poll reads them again: an edit, a deletion and a reaction made
// meanwhile show in the pane. The code before read nothing of a thread
// whose history hadn't reached its end.
func TestThreadPaneRefreshesAtPoll(t *testing.T) {
	fs := shipFake(t)
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	app := e.a.data.core
	ch := readChannel(t, app, fs)
	root, reply := shipThread(fs, ch, 60, nil)
	e.a.OpenThread(ch, root)
	e.until("the replies on screen", 20*time.Second, func() bool { return paneHas(e.threadView(ch, root), reply(5)) })
	e.settle(300 * time.Millisecond)
	us := fs.UserIDs()
	const edited = "synthetic reply 3, edited while the socket was away"
	if !fs.EditReplyQuiet(ch, root, reply(3), edited) || !fs.DeleteReplyQuiet(ch, root, reply(5)) ||
		!fs.ReactReplyQuiet(ch, root, reply(4), "eyes", us[2]) {
		t.Fatal("the fake didn't take the changes")
	}
	e.until("the edit and the deletion in the pane", 45*time.Second, func() bool {
		tv := e.threadView(ch, root)
		txt, _, ok := tv.RowText(reply(3))
		return ok && strings.Contains(txt, "edited while the socket was away") && !paneHas(tv, reply(5))
	})
	if m, ok := app.S.Find(ch, reply(4)); !ok || len(m.Reactions) == 0 {
		t.Error("reply 4 has no reaction after the poll")
	}
}

// TestUndoAndEditLastReadTheNewestSlice (findings 6 and 9): Mod+Z and Up
// in a thread's empty reply box find the viewer's last message in the
// thread's newest slice, as Slack's undo send and edit-last selectors
// read dTxY tp(threadKey, null) (boot-shared P5Eq Rw, qszY W4): a long
// thread the viewer doesn't follow, opened near its end, the viewer's
// reply sent there. Replies, which both read before, is the slice at the
// thread's default start (the root's page, where the viewer's reply 5 is),
// so Mod+Z offered, or with no confirmation deleted, reply 5.
func TestUndoAndEditLastReadTheNewestSlice(t *testing.T) {
	fs := shipFake(t)
	app := core.New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(app.Shutdown)
	ctx := context.Background()
	if err := app.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	ch := readChannel(t, app, fs)
	self := app.S.SelfID()
	us := fs.UserIDs()
	root, reply := shipThread(fs, ch, 100, func(i int) string {
		if i == 5 {
			return self
		}
		return us[1+i%3]
	})
	if _, err := app.ThreadAt(ctx, ch, root, reply(95)); err != nil { // opened at a reply near the end
		t.Fatal(err)
	}
	sent, err := app.Send(ctx, core.SendOpts{Channel: ch, ThreadTS: root, Text: "synthetic: mine, just sent"})
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := lastOwnMessage(ctx, app, ch, root, self); !ok || m.TS != sent.TS {
		t.Errorf("Mod+Z found %q (%v), want the reply just sent %s (reply 5, the viewer's too, is %s)", m.TS, ok, sent.TS, reply(5))
	}
	if m, ok := lastEditable(ctx, app, ch, root, self); !ok || m.TS != sent.TS {
		t.Errorf("Up found %q (%v), want the reply just sent %s (reply 5, the viewer's too, is %s)", m.TS, ok, sent.TS, reply(5))
	}
}

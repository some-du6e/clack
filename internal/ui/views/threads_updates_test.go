package views

// The Threads view between loads, against Slack's ThreadsView
// (client-threads-view 9Ra5) and the thunks around it (boot-shared k0ZD
// maybeRefreshAllThreads, client-boot 181D, boot-render VxbD): a thread
// marked unread and the divider, socket reconnects, a refresh that can't
// join a card's replies, a load landing just before the next one starts,
// and the threads a reply names.

import (
	"context"
	"fmt"
	"image"
	"net/url"
	"slices"
	"strings"
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

// thrReplyEvent is the socket's message event for a reply from grace.
func thrReplyEvent(ch, root, ts string) map[string]any {
	return map[string]any{"type": "message", "channel": ch, "user": "U0FAKE00002", "text": "a synthetic reply",
		"ts": ts, "thread_ts": root, "event_ts": ts}
}

// thrRepliedEvent is the socket's message_replied for root after reply
// ts, its reply_count n: a live layer patches the root's count with it
// before the refresh reads it.
func thrRepliedEvent(ch, root, ts string, n int) map[string]any {
	return map[string]any{"type": "message", "subtype": "message_replied", "channel": ch, "hidden": true, "ts": ts, "event_ts": ts,
		"message": map[string]any{"type": "message", "ts": root, "thread_ts": root, "user": "U0FAKE00001", "text": "a synthetic root",
			"reply_count": n, "latest_reply": ts, "reply_users": []string{"U0FAKE00002"}}}
}

// thrRefreshAfter pushes ev and waits for the realtime refresh it brings
// (the 5 s between refreshes waived) and the frame after it.
func thrRefreshAfter(t *testing.T, e *vaEnv, v *Threads, fs *fakeslack.Server, what string, ev map[string]any) {
	t.Helper()
	v.w.mu.Lock()
	v.w.last = time.Time{}
	v.w.mu.Unlock()
	n := fs.CallCount("subscriptions.thread.getView")
	fs.Push(ev)
	e.until(t, what, func() bool { return fs.CallCount("subscriptions.thread.getView") > n && !v.data.busy() })
	e.frame()
}

// thrFind is where the drawn button c is: the first point down the
// view's middle column that hovers it.
func thrFind(t *testing.T, e *vaEnv, c *widget.Clickable) image.Point {
	t.Helper()
	for y := 0; y < e.size.Y; y += 2 {
		p := image.Pt(e.size.X/2, y)
		e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(p.X), float32(p.Y))})
		e.frame()
		if c.Hovered() {
			return p
		}
	}
	t.Fatal("the button isn't drawn")
	return image.Point{}
}

// thrBounded runs frames for d and reports whether ok held in any.
func thrBounded(e *vaEnv, d time.Duration, ok func() bool) bool {
	for end := time.Now().Add(d); time.Now().Before(end); {
		e.frame()
		if ok() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// Mark unread from the Threads view on the read thread under the divider
// (its message menu: core's MarkThreadUnreadFrom, Slack's
// threadsManuallyMarkedUnread) moves that thread's arrival last read to
// its new last read (getDerivedStateFromProps), so its reply from someone
// else is new and the divider goes. The thread stays followed. A reply
// that lands in it on screen then leaves it unread
// (maybeMarkNewMessageOnUpdate skips a thread marked unread).
func TestThreadsMarkedUnreadDividerLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	const before = "1762698000.000100" // the last read Mark unread on thrCReply leaves
	var replied atomic.Bool
	var marks thrMarkLog
	app, fs := vkFake(t, func(method string, f url.Values) (any, bool) {
		marks.note(method, f)
		if method != "subscriptions.thread.getView" {
			return nil, false
		}
		c := thrFixRead(ch, thrC, thrCReply, "U0FAKE00002", false, thrCReply)
		if replied.Load() {
			c = thrFixReplies(ch, thrC, 2, before, thrReplyBy(thrCReply, "U0FAKE00002"), thrReplyBy(thrLate, "U0FAKE00002"))
		}
		return thrOrderAnswer(thrFixRead(ch, thrA, thrAReply, "U0FAKE00002", true, thrA), c), true
	})
	v := NewThreads(newKBHost(app, false))
	e := thrViewEnv(t, v, 2)
	e.frame()
	if v.divAt != 1 {
		t.Fatalf("divider at %d after the load, want 1: before the read thread", v.divAt)
	}
	if err := app.MarkThreadUnreadFrom(context.Background(), ch, thrC, thrCReply, core.MarkedInThreadsView); err != nil {
		t.Fatal(err)
	}
	// the server's thread_marked for the mark, as it sends every client
	fs.Push(map[string]any{"type": "thread_marked", "subscription": map[string]any{"type": "thread", "channel": ch,
		"thread_ts": thrC, "last_read": before, "active": true}, "event_ts": "1762699500.000100"})
	e.until(t, "the divider gone after Mark unread", func() bool { return v.divAt == -1 })
	p, _, _ := v.data.get()
	if !v.followed(&p.items[1]) {
		t.Fatal("the thread marked unread isn't followed")
	}
	key := ch + "/" + thrC
	replied.Store(true)
	thrRefreshAfter(t, e, v, fs, "the refresh after a reply in it", thrReplyEvent(ch, thrC, thrLate))
	e.frame()
	p, _, _ = v.data.get()
	if v.unread(&p.items[1]) == 0 || v.read[key] != before {
		t.Errorf("after a reply in the thread marked unread: %d unread, read to %q; want it unread, read to %s", v.unread(&p.items[1]), v.read[key], before)
	}
	if thrBounded(e, 300*time.Millisecond, func() bool {
		return slices.ContainsFunc(marks.all(), func(m string) bool { return strings.HasPrefix(m, thrC+"@") && strings.HasSuffix(m, "read=1") })
	}) {
		t.Errorf("a reply in the thread marked unread marked it read (%v)", marks.all())
	}
	if v.divAt != -1 {
		t.Errorf("divider at %d after the reply, want -1", v.divAt)
	}
}

// A slow socket connect while the Threads view shows reloads it (k0ZD
// maybeRefreshAllThreads, then 181D incrementAllThreadsRefreshCounter and
// ThreadsView's reloadThreads): once, with the list at its top and every
// card at its default first reply, and a followed thread the list didn't
// have that got a reply while the socket was down comes in with no event
// about it. A refresh alone puts on top only a thread an event named.
func TestThreadsSlowReconnectLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	var gap atomic.Bool
	app, fs := vkFake(t, func(method string, _ url.Values) (any, bool) {
		if method != "subscriptions.thread.getView" {
			return nil, false
		}
		ths := []map[string]any{thrFixRead(ch, thrA, thrAReply, "U0FAKE00002", true, thrA),
			thrFixRead(ch, thrC, thrCReply, fakeslack.SelfID, false, thrCReply)}
		if gap.Load() {
			ths = append(ths, thrFixRead(ch, thrD, thrDReply, "U0FAKE00002", true, thrD))
		}
		return thrOrderAnswer(ths...), true
	})
	v := NewThreads(newKBHost(app, false))
	e := thrViewEnv(t, v, 2)
	e.size = image.Pt(1000, 300) // the list scrolls
	e.frame()
	v.list.Position.First, v.list.Position.Offset = 1, 40
	e.frame()
	folds := map[string]uint64{}
	for k, c := range v.cards {
		folds[k] = c.folds
	}
	n, ws := fs.CallCount("subscriptions.thread.getView"), fs.CallCount("ws")
	gap.Store(true) // the reply in d while the socket is down: no event reaches the client
	fs.DropSockets()
	e.until(t, "the slow reconnect", func() bool { return fs.CallCount("ws") > ws })
	e.until(t, "the reload after it", func() bool { return fs.CallCount("subscriptions.thread.getView") > n && v.Settled() })
	e.frame()
	if got := thrRoots(v); len(got) != 3 || got[0] != thrD {
		t.Errorf("after the slow reconnect: %v, want %s first (the reload sorted afresh)", got, thrD)
	}
	if p := v.list.Position; p.First != 0 || p.Offset != 0 {
		t.Errorf("the list at %+v after the reload, want its top", p)
	}
	for k, f := range folds {
		if c := v.cards[k]; c == nil || c.folds <= f {
			t.Errorf("%s didn't go back to its default first reply", k)
		}
	}
	thrBounded(e, 300*time.Millisecond, func() bool { return false })
	if got := fs.CallCount("subscriptions.thread.getView"); got != n+1 {
		t.Errorf("%d getView calls after the reconnect, want one reload", got-n)
	}
}

// A fast reconnect (at the socket's reconnect_url: Slack's
// FAST_RECONNECTING, lastConnectWasFast) reloads nothing:
// maybeRefreshAllThreads returns on its payload, so the list keeps its
// place and its cards their replies.
func TestThreadsFastReconnectLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	app, fs := vkFake(t, func(method string, _ url.Values) (any, bool) {
		if method != "subscriptions.thread.getView" {
			return nil, false
		}
		return thrOrderAnswer(thrFixRead(ch, thrA, thrAReply, "U0FAKE00002", true, thrA),
			thrFixRead(ch, thrC, thrCReply, fakeslack.SelfID, false, thrCReply)), true
	})
	evs, stop := app.Subscribe()
	defer stop()
	v := NewThreads(newKBHost(app, false))
	e := thrViewEnv(t, v, 2)
	e.size = image.Pt(1000, 300)
	e.frame()
	v.list.Position.First, v.list.Position.Offset = 1, 40
	e.frame()
	for _, c := range v.cards {
		c.older = []core.Message{{TS: "1762500000.000100"}}
	}
	fs.Push(map[string]any{"type": "reconnect_url", "url": "ws" + strings.TrimPrefix(fs.URL, "http") + "ws?fast=1"})
	// the RTM reads its frames in order: a typing after the reconnect_url
	// comes out once the URL is kept
	fs.Push(map[string]any{"type": "user_typing", "channel": ch, "user": "U0FAKE00002"})
	for typed, deadline := false, time.After(10*time.Second); !typed; {
		select {
		case ev := <-evs:
			typed = ev.Type == "typing" && ev.UserID == "U0FAKE00002"
		case <-deadline:
			t.Fatal("the socket's typing after the reconnect_url never came")
		}
	}
	n := fs.CallCount("subscriptions.thread.getView")
	fs.DropSockets()
	deadline := time.After(10 * time.Second)
	for fast := false; !fast; {
		select {
		case ev := <-evs:
			if ev.Type == "connection" && ev.Connected {
				if !ev.Fast {
					t.Fatal("the reconnect at the reconnect_url came as a slow one")
				}
				fast = true
			}
		case <-deadline:
			t.Fatal("no reconnect")
		case <-time.After(10 * time.Millisecond):
			e.frame()
		}
	}
	thrBounded(e, 400*time.Millisecond, func() bool { return false })
	if got := fs.CallCount("subscriptions.thread.getView"); got != n {
		t.Errorf("a fast reconnect reloaded the view (%d getView calls)", got-n)
	}
	if p := v.list.Position; p.First != 1 {
		t.Errorf("the list at %+v after a fast reconnect, want where it was", p)
	}
	for k, c := range v.cards {
		if len(c.older) == 0 {
			t.Errorf("%s folded on a fast reconnect", k)
		}
	}
}

// A refresh whose window starts after a card's newest reply: replies may
// have come between the two that neither list has (three in one refresh's
// gap over getView's 2-reply window). The card keeps both and fetches the
// replies between (conversations.replies after its newest and before the
// window's first), so every reply is drawn in order with no "Show N more
// replies", as Slack's card grows by each reply. It holds with the
// socket's message_replied for each reply, which a live layer turns into
// a reply_count already counting them, and when the replies straddle the
// refresh's request. A failed fetch takes the card back to its window
// alone, and its row then loads the rest in order. Each gap is one
// conversations.replies. Every reply the card draws, carried over, in
// the window or brought by the gap's fetch, takes an edit, a reaction and
// a deletion at once, and keeps them across refreshes.
func TestThreadsRefreshGapLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	const root = thrC
	rs := []string{"1762695100.000100", "1762695200.000100", "1762695300.000100", "1762695400.000100", "1762695500.000100", "1762695600.000100",
		"1762695700.000100"}
	reply := func(ts string) slack.Message {
		return slack.Message{Type: "message", Channel: ch, TS: ts, ThreadTS: root, User: "U0FAKE00002", Text: "a synthetic reply"}
	}
	for _, tc := range []struct {
		name     string
		straddle bool // the first reply comes before the refresh's request, the others while it is out
		fail     bool // the gap's fetch fails
	}{
		{"burst", false, false},
		{"straddle", true, false},
		{"fetch fails", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var state atomic.Int32 // the replies the server has
			state.Store(2)
			// read is how many getView answers have read state: the fake
			// counts a call (CallCount) before the hook runs, so a wait on
			// the count let the test move state before the answer read it
			// (the straddle then had no gap: full-test, 2026-11-21)
			var read atomic.Int32
			var g thrGate
			rootMsg := func(n int) slack.Message {
				return slack.Message{Type: "message", Channel: ch, TS: root, ThreadTS: root, User: "U0FAKE00001", Text: "a synthetic root",
					ReplyCount: n, ReplyUsers: []string{"U0FAKE00002"}, LatestReply: rs[n-1], Subscribed: true, LastRead: rs[n-1]}
			}
			app, fs := vkFake(t, func(method string, f url.Values) (any, bool) {
				switch method {
				case "subscriptions.thread.getView":
					n := int(state.Load()) // the answer as of the request's arrival
					read.Add(1)
					g.wait()
					th := map[string]any{"root_msg": rootMsg(n), "latest_replies": []slack.Message{reply(rs[n-2]), reply(rs[n-1])}, "unread_replies": []slack.Message{}}
					return thrOrderAnswer(thrFixRead(ch, thrA, thrAReply, "U0FAKE00002", true, thrA), th), true
				case "conversations.replies":
					if f.Get("ts") != root {
						return nil, false
					}
					oldest, latest, incl := f.Get("oldest"), f.Get("latest"), f.Get("inclusive") == "true"
					n := int(state.Load())
					msgs := []slack.Message{rootMsg(n)}
					for _, ts := range rs[:n] {
						if oldest != "" && !actTSLess(oldest, ts) && (!incl || ts != oldest) {
							continue
						}
						if latest != "" && !actTSLess(ts, latest) && (!incl || ts != latest) {
							continue
						}
						msgs = append(msgs, reply(ts))
					}
					return map[string]any{"ok": true, "messages": msgs, "has_more": false}, true
				}
				return nil, false
			})
			t.Cleanup(g.open) // before the fake's Close, which waits for a held answer
			v := NewThreads(newKBHost(app, false))
			e := thrViewEnv(t, v, 2)
			e.size = image.Pt(1000, 1600)
			key := ch + "/" + root
			shown := func() []string {
				p, _, _ := v.data.get()
				for i := range p.items {
					if p.items[i].key == key {
						return thrTSs(thrShown(&p.items[i], v.cards[key]))
					}
				}
				return nil
			}
			drawn := func(want []string) {
				t.Helper()
				e.frame()
				for _, ts := range want {
					if _, ok, _ := v.kb.top(key + "-" + ts); !ok {
						t.Errorf("reply %s isn't drawn", ts)
					}
				}
			}
			if tc.fail {
				fs.SetFault("conversations.replies", fakeslack.Fault{Error: "internal_error", Times: 1})
			}
			// three replies come within one refresh's gap
			v.w.mu.Lock()
			v.w.last = time.Time{}
			v.w.mu.Unlock()
			n := fs.CallCount("subscriptions.thread.getView")
			reads := read.Load()
			if tc.straddle {
				g.hold()
			}
			state.Store(3)
			fs.Push(thrReplyEvent(ch, root, rs[2]))
			fs.Push(thrRepliedEvent(ch, root, rs[2], 3))
			if tc.straddle {
				e.until(t, "the refresh's request", func() bool { return read.Load() > reads })
			}
			state.Store(5)
			for _, i := range []int{3, 4} {
				fs.Push(thrReplyEvent(ch, root, rs[i]))
				fs.Push(thrRepliedEvent(ch, root, rs[i], i+1))
			}
			g.open()
			e.until(t, "the refresh after three replies", func() bool { return fs.CallCount("subscriptions.thread.getView") > n && !v.data.busy() })
			e.frame()
			if tc.fail {
				e.until(t, "the card back to its window", func() bool { return slices.Equal(shown(), rs[3:5]) })
				e.frame()
				c := v.cards[key]
				if c == nil || c.moreText == "" {
					t.Fatal("no \"Show N more replies\" row after the failed fetch")
				}
				c.more.Click()
			}
			e.until(t, "every reply in the card", func() bool { return slices.Equal(shown(), rs[:5]) })
			drawn(rs[:5])
			if c := v.cards[key]; c == nil || c.moreText != "" {
				t.Errorf("a \"Show N more replies\" row (%q) with every reply shown", c.moreText)
			}
			// the gap's fetch, and the failed card's older replies. Straddled,
			// the replies sent while the refresh is out race its answer to the
			// view: seen first, they make the answer stale, the next window
			// starts after a hole and the gap is fetched; the answer first,
			// it brings the third reply and the next window joins it, no gap
			// and nothing to fetch (1 run in 20 under load: flakes-1122). The
			// card ends the same either way; a gap is one fetch, never two.
			got, want := fs.CallCount("conversations.replies"), map[bool]int64{false: 1, true: 2}[tc.fail]
			switch {
			case tc.straddle && got == 0:
				t.Logf("straddle: the refresh's answer reached the view before its straddling replies: no gap to fetch")
			case got != want:
				t.Errorf("%d conversations.replies for one gap, want %d", got, want)
			}
			if tc.name != "burst" {
				return
			}
			// the socket deletes r0 and edits r1, then r5 brings a refresh
			fs.Push(map[string]any{"type": "message", "subtype": "message_deleted", "channel": ch, "hidden": true, "deleted_ts": rs[0],
				"ts": "1762695650.000100", "event_ts": "1762695650.000100",
				"previous_message": map[string]any{"type": "message", "ts": rs[0], "thread_ts": root, "user": "U0FAKE00002", "text": "a synthetic reply"}})
			fs.Push(map[string]any{"type": "message", "subtype": "message_changed", "channel": ch, "hidden": true,
				"ts": "1762695660.000100", "event_ts": "1762695660.000100",
				"message": map[string]any{"type": "message", "ts": rs[1], "thread_ts": root, "user": "U0FAKE00002", "text": "an edited reply",
					"edited": map[string]any{"user": "U0FAKE00002", "ts": "1762695660.000100"}}})
			text := func(ts string) string {
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
			e.until(t, "the delete and the edit drawn", func() bool { return slices.Equal(shown(), rs[1:5]) && text(rs[1]) == "an edited reply" })
			// r2 came in the gap's fetch: an edit of it and a reaction to
			// it are drawn in the frame after each
			gap := rs[2]
			edited := reply(gap)
			edited.Text = "an edited gap reply"
			edited.Edited = &slack.Edited{User: "U0FAKE00002", TS: fmt.Sprintf("%d.000002", time.Now().Unix())}
			lvInject(app, ch, map[string]any{"type": "message", "subtype": "message_changed", "hidden": true, "message": edited, "previous_message": reply(gap)})
			e.frame()
			if got := lvDrawn(&v.base, ch, gap); !strings.Contains(got, "an edited gap reply") || text(gap) != "an edited gap reply" {
				t.Fatalf("the frame after the gap reply's edit drew %q (the card's copy %q)", got, text(gap))
			}
			lvInject(app, ch, map[string]any{"type": "reaction_added", "user": "U0FAKE00001", "reaction": "tada",
				"item": map[string]any{"type": "message", "channel": ch, "ts": gap}})
			e.frame()
			tadas := func() int {
				p, _, _ := v.data.get()
				for i := range p.items {
					if p.items[i].key == key {
						for _, m := range thrShown(&p.items[i], v.cards[key]) {
							for _, rx := range m.Reactions {
								if m.TS == gap && rx.Name == "tada" {
									return rx.Count
								}
							}
						}
					}
				}
				return 0
			}
			if n := tadas(); n != 1 || text(gap) != "an edited gap reply" {
				t.Fatalf("the frame after a reaction to the gap reply: %d tada on it (its text %q)", n, text(gap))
			}
			state.Store(6)
			thrRefreshAfter(t, e, v, fs, "the refresh after r5", thrReplyEvent(ch, root, rs[5]))
			e.frame()
			if got := shown(); !slices.Equal(got, rs[1:6]) || text(rs[1]) != "an edited reply" || text(gap) != "an edited gap reply" || tadas() != 1 {
				t.Errorf("after the next refresh the card shows %v (r1 %q, r2 %q with %d tada); want %v with the edits and the reaction",
					got, text(rs[1]), text(gap), tadas(), rs[1:6])
			}
			if got := lvDrawn(&v.base, ch, gap); !strings.Contains(got, "an edited gap reply") {
				t.Errorf("after the next refresh the gap reply is drawn %q", got)
			}
			// its deletion takes it out of the card in the frame after,
			// and it stays out across the next refresh
			lvInject(app, ch, map[string]any{"type": "message", "subtype": "message_deleted", "hidden": true, "deleted_ts": gap, "previous_message": edited})
			e.frame()
			e.frame() // the keyboard's rows are the last frame's
			if got := shown(); slices.Contains(got, gap) {
				t.Fatalf("the frame after the gap reply's deletion the card shows %v", got)
			}
			if _, ok, _ := v.kb.top(key + "-" + gap); ok {
				t.Error("the deleted gap reply is still drawn")
			}
			state.Store(7)
			thrRefreshAfter(t, e, v, fs, "the refresh after r6", thrReplyEvent(ch, root, rs[6]))
			e.frame()
			if got, want := shown(), []string{rs[1], rs[3], rs[4], rs[5], rs[6]}; !slices.Equal(got, want) {
				t.Errorf("after the refresh past the deletion the card shows %v; want %v", got, want)
			}
			if got := fs.CallCount("conversations.replies"); got != 1 {
				t.Errorf("%d conversations.replies after refreshes with no gap, want the one gap's", got)
			}
		})
	}
}

// A load that sorts the list lands, and before the next frame settles it
// another load starts (the frame's staleness check or the next page come
// first). The landed load is settled first, its own mode: the mount's
// floating loader stops asking for frames. Checked at once and 150 ms on
// (a slow runner: markVisible's check, 350 ms after each load that sorts
// the list, is then a frame asked for within 250 ms, which
// thrAnimating took for the loader).
func TestThreadsSettleBeforeNextLoad(t *testing.T) {
	for _, late := range []time.Duration{0, 150 * time.Millisecond} {
		t.Run(fmt.Sprintf("checked %v on", late), func(t *testing.T) { threadsSettleBeforeNextLoad(t, late) })
	}
}

func threadsSettleBeforeNextLoad(t *testing.T, late time.Duration) {
	ch := fakeslack.ViewsChannel
	app, fs := vaFake(t)
	fs.SetHook(func(method string, _ url.Values) (any, bool) {
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
	v.mounting = true
	v.reload()
	deadline := time.Now().Add(10 * time.Second)
	for v.data.busy() { // landed, no frame since
		if time.Now().After(deadline) {
			t.Fatal("the reload never landed")
		}
		time.Sleep(5 * time.Millisecond)
	}
	v.load(actRefresh)
	e.until(t, "the refresh", v.Settled)
	e.frame()
	time.Sleep(late)
	if v.mounting || thrAnimating(e) {
		t.Errorf("the reload that landed before the refresh started was never settled: mounting %v, the floating loader still drawn", v.mounting)
	}
}

// firstReadThreadKey is picked among the threads mapStateToProps shows:
// with the load's first read thread in a channel the viewer left, the
// divider goes before the next read thread shown.
func TestThreadsHiddenFirstReadLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	app, fs := vkFake(t, nil)
	// a channel left through the API, so the fake's own answers agree
	// afterwards (#general can't be left)
	other := ""
	for _, id := range fs.ChannelIDs() {
		if id != ch && app.Leave(context.Background(), id) == nil && thrLeftChannel(app, id) {
			other = id
			break
		}
	}
	if other == "" {
		t.Fatal("no channel left")
	}
	fs.SetHook(func(method string, _ url.Values) (any, bool) {
		if method != "subscriptions.thread.getView" {
			return nil, false
		}
		return thrOrderAnswer(thrFixRead(ch, thrA, thrAReply, "U0FAKE00002", true, thrA),
			thrFixRead(other, thrE, thrEReply, "U0FAKE00002", false, thrEReply), // the newest read one, in the channel left
			thrFixRead(ch, thrX, thrXReply, "U0FAKE00002", false, thrXReply)), true
	})
	h := newKBHost(app, false)
	h.away.Store(true)
	v := NewThreads(h)
	e := thrViewEnv(t, v, 3)
	e.frame()
	if _, drawn := v.kb.idx["heading-"+other+"/"+thrE]; drawn {
		t.Fatal("the left channel's thread is drawn")
	}
	if v.divAt != 1 {
		t.Errorf("divider at %d, want 1: before %s, the first read thread shown", v.divAt, thrX)
	}
}

// A thread a reply names waits for the refresh that brings it, however
// many come first (Slack keeps the fetched thread and prepends it while
// the viewer has unread threads), until a load that sorts the list.
func TestThreadsNamedThreadWaitsLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	var calls atomic.Int32
	app, fs := vkFake(t, func(method string, _ url.Values) (any, bool) {
		if method != "subscriptions.thread.getView" {
			return nil, false
		}
		ths := []map[string]any{thrFixRead(ch, thrA, thrAReply, "U0FAKE00002", true, thrA),
			thrFixRead(ch, thrC, thrCReply, fakeslack.SelfID, false, thrCReply)}
		if calls.Add(1) > 5 { // the first load, then four refreshes without it
			ths = append(ths, thrFixRead(ch, thrD, thrDReply, "U0FAKE00002", true, thrD))
		}
		return thrOrderAnswer(ths...), true
	})
	v := NewThreads(newKBHost(app, false))
	e := thrViewEnv(t, v, 2)
	thrRefreshAfter(t, e, v, fs, "the refresh after the reply in d", thrReplyEvent(ch, thrD, thrDReply))
	for i := 0; i < 4; i++ {
		thrRefreshAfter(t, e, v, fs, "a refresh after a reply in a", thrReplyEvent(ch, thrA, "1762693500.000100"))
	}
	if got := thrRoots(v); len(got) != 3 || got[0] != thrD {
		t.Errorf("after the fifth refresh: %v, want %s on top", got, thrD)
	}
}

// thrFrameHost is kbHost with the window's frame count: the view shows
// only while it draws in the frames (shownTracker).
type thrFrameHost struct {
	*kbHost
	seq atomic.Uint64
}

func (h *thrFrameHost) FrameSeq() uint64 { return h.seq.Load() }

// While another page shows, a reply names no thread and doesn't make the
// view stale: Slack's ThreadsView is unmounted there, and the next visit
// loads afresh. thread_marked still reaches the view's read state.
func TestThreadsAwayIgnoresRepliesLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	app, fs := vkFake(t, func(method string, _ url.Values) (any, bool) {
		if method != "subscriptions.thread.getView" {
			return nil, false
		}
		return thrOrderAnswer(thrFixRead(ch, thrA, thrAReply, "U0FAKE00002", true, thrA),
			thrFixRead(ch, thrC, thrCReply, fakeslack.SelfID, false, thrCReply)), true
	})
	h := &thrFrameHost{kbHost: newKBHost(app, false)}
	v := NewThreads(h)
	e := &vaEnv{size: image.Pt(1000, 1100)}
	e.draw = func(gtx layout.Context) { h.seq.Add(1); v.Layout(gtx) }
	e.until(t, "the threads", func() bool { return len(thrRoots(v)) == 2 && !v.data.busy() })
	e.draw = func(layout.Context) { h.seq.Add(1) } // another page
	e.frame()
	e.frame()
	fs.Push(thrReplyEvent(ch, thrD, thrDReply))
	// a later event on the same socket: once it is in, the reply was seen
	fs.Push(map[string]any{"type": "thread_marked", "subscription": map[string]any{"type": "thread", "channel": ch,
		"thread_ts": thrA, "last_read": thrAReply, "active": true}, "event_ts": "1762699500.000100"})
	e.until(t, "the thread_marked after the reply", func() bool {
		v.mu.Lock()
		defer v.mu.Unlock()
		return v.marks[ch+"/"+thrA] == thrAReply
	})
	v.mu.Lock()
	_, named := v.named[ch+"/"+thrD]
	v.mu.Unlock()
	v.w.mu.Lock()
	stale := v.w.stale
	v.w.mu.Unlock()
	if named || stale {
		t.Errorf("a reply while another page shows: named %v, stale %v; want neither", named, stale)
	}
}

package ui

// A reply posted to the thread open in the pane is drawn there in the
// frame after the socket brings it, and no later than Activity has its
// entry (Karim, 2026-11-18: "threads are a shit show rn, they dont get
// replies quickly and i see it like in my activity tab first"). The server
// tells a thread's follower of a reply with the reply's message event and
// the root's message_replied, and with two activity entries (thread_reply,
// which Slack's client drops, and the thread's thread_v2 bundle), in
// either order. Core fetched the people a message names before announcing
// it, on the event pump: a reply from someone the client never saw waited
// for users/info, and every event behind it with it; and an entry that
// came first held the reply behind the entry's own lookups (its messages,
// its people). Slack's messageReceived stores the message at once, and its
// activity store holds up no other event. Here users/info answers after
// 1.5 s, as on a busy Grid, every reply is from someone new, someone else
// new types in the thread 100 ms before each (core asked users/info for a
// typer on the pump, and the reply waited behind it: α.110's review), and
// the window shows Activity with the thread open beside it, as Karim's
// did.
// Frames are drawn as a window draws them (when the app asks for one).
// A reply counts once the pane's row paints its body, within two frames
// and 250 ms of the socket. Natively, under a plugin patching the messages
// slice, and under one patching the components a message draws through.
// Synthetic data only.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

func TestThreadReplyLatency(t *testing.T) {
	if os.Getenv("CLACK_LATENCY_TESTS") == "" {
		t.Skip("CLACK_LATENCY_TESTS not set: flake-check's thread-replies job runs this")
	}
	for _, mode := range []string{"native", "slice", "components"} {
		t.Run(mode, func(t *testing.T) { threadReplyLatency(t, mode) })
	}
}

func threadReplyLatency(t *testing.T, mode string) {
	sz := fakeslack.Small
	sz.AppPlans = true
	sz.Views = true // Activity's feed, and messages.list for an entry's messages
	sz.Authors = 20 // the generated messages' people; the others are no one the client fetched
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	ch := fakeslack.AppPlanChannelID
	root := fs.PostUptimeQuiet("10router blipped · 0m 0s")
	fs.PostReplyQuiet(ch, root, fakeslack.SelfID, "on it, checking 10router") // the viewer follows the thread
	ids := fs.UserIDs()
	replier := func(k int) string { return ids[len(ids)-1-k] } // someone the client never fetched
	// typer(k) types in the thread just before reply k: someone else the
	// client never fetched (the typing line asks for them; on the pump,
	// users/info for them held the reply behind it: α.110's review)
	typer := func(k int) string { return ids[len(ids)-31-k] }

	var e *perfEnv
	switch mode {
	case "native":
		e = startEnv(t, fs, t.TempDir())
	default:
		e = benchStart(t, fs).perfEnv
	}
	e.waitReady()
	c := e.a.data.core
	c.SetRealtime(true)
	e.until("the socket", 20*time.Second, func() bool { return c.RTM != nil && c.RTM.Connected() })
	if mode != "native" {
		e.until("Taut", 60*time.Second, func() bool { return e.a.Taut() != nil })
		tt := e.a.Taut()
		code, id := editPassComponents, "EditPassComponents"
		if mode == "slice" {
			code, id = editPassSlice, "EditPassSlice"
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if _, err := tt.Import(ctx, []byte(code), ""); err != nil {
			t.Fatal(err)
		}
		if err := tt.SetEnabled(ctx, id, true); err != nil {
			t.Fatal(err)
		}
		e.until("the plugin's patches", 30*time.Second, func() bool {
			if mode == "slice" {
				return tt.SlicePatched("messages")
			}
			return slackc.Patched("MessageText") && slackc.Patched("MessageWrapper")
		})
	}

	// Activity, with the thread open beside it
	e.a.Navigate("activity")
	e.settle(2 * time.Second)
	e.a.OpenThread(ch, root)
	paneDrew := func(ts string) bool {
		tv := e.threadView(ch, root)
		if tv == nil {
			return false
		}
		_, _, ok := tv.RowText(ts)
		return ok
	}
	e.until("the thread pane", 20*time.Second, func() bool { return paneDrew(root) })
	e.settle(500 * time.Millisecond)
	// a reply counts once the pane's own row paints its body (Activity,
	// beside it, paints the same text): the row's richview, which a
	// component patch's MessageText reaches through MessageBlocks
	painted := map[*richview.View]bool{}
	richview.PaintedHook = func(v *richview.View) { painted[v] = true }
	t.Cleanup(func() { richview.PaintedHook = nil })
	panePainted := func(ts string) bool {
		tv := e.threadView(ch, root)
		if tv == nil {
			return false
		}
		b := tv.RowBody(ts)
		return b != nil && painted[b]
	}

	// core's announcements: a reply's message, its thread_v2 entry
	evs, stop := c.Subscribe()
	defer stop()
	var mu sync.Mutex
	msgAt, actAt := map[string]time.Time{}, map[string]time.Time{}
	go func() {
		for ev := range evs {
			mu.Lock()
			switch {
			case ev.Type == "message" && ev.ThreadTS == root && ev.TS != root:
				if _, ok := msgAt[ev.TS]; !ok {
					msgAt[ev.TS] = time.Now()
				}
			case ev.Type == "activity" && ev.Activity != nil && ev.Activity.Type == "thread_v2":
				for _, k := range []string{ev.Activity.LatestTS, ev.Activity.TS} {
					if _, ok := actAt[k]; !ok && k != "" {
						actAt[k] = time.Now()
					}
				}
			}
			mu.Unlock()
		}
	}()

	// users/info slow from now on, as on a busy Grid
	fs.SetEdgeHook(func(method string, _ map[string]json.RawMessage) (any, bool) {
		if method == "edge/users/info" {
			time.Sleep(1500 * time.Millisecond)
		}
		return nil, false
	})
	t.Cleanup(func() { fs.SetEdgeHook(nil) })

	// frames as the window asks for them; the window watched, not used
	var asked atomic.Bool
	prevHook := invalidateHook
	invalidateHook = func(*App) { asked.Store(true) }
	t.Cleanup(func() { invalidateHook = prevHook })
	e.blur()
	asked.Store(false)
	var frames int64
	var lastFrame time.Time
	const vsync = time.Second / 60

	type reply struct {
		ts            string
		activityFirst bool
	}
	var rs []reply
	var ds []latDraw
	const typingLead = 100 * time.Millisecond
	next := time.Now().Add(typingLead)
	end := next.Add(8 * time.Second)
	typed := 0 // the replies whose typer has typed
	for {
		now := time.Now()
		if !now.Before(end) && (len(ds) == 0 || !ds[len(ds)-1].drawn.IsZero() || now.Sub(ds[len(ds)-1].pushed) > 4*time.Second) {
			break
		}
		if now.Before(end) && typed == len(rs) && !now.Before(next.Add(-typingLead)) {
			fs.Push(map[string]any{"type": "user_typing", "channel": ch, "thread_ts": root, "user": typer(len(rs) + 1)})
			typed++
		}
		if now.Before(end) && !now.Before(next) {
			k := len(rs) + 1
			text := fmt.Sprintf("synthetic reply %d to the outage", k)
			first := k%2 == 0 // every other reply's entries come first
			pn := frames
			ts := fs.PushReplyLive(ch, root, replier(k), text, first)
			rs = append(rs, reply{ts, first})
			ds = append(ds, latDraw{text: text, pushed: now, pushedN: pn})
			next = next.Add(time.Second)
		}
		wake, ok := e.r.WakeupTime()
		if !asked.Swap(false) && !(ok && !wake.After(now)) {
			time.Sleep(time.Millisecond)
			continue
		}
		if w := time.Until(lastFrame.Add(vsync)); w > 0 {
			time.Sleep(w)
		}
		lastFrame = time.Now()
		clear(painted)
		e.frame()
		frames++
		mu.Lock()
		for i := range ds {
			d := &ds[i]
			if at, ok := msgAt[rs[i].ts]; ok && d.core.IsZero() {
				d.core = at
			}
			if d.drawn.IsZero() && panePainted(rs[i].ts) {
				d.drawn, d.frames = lastFrame, frames-d.pushedN
			}
		}
		mu.Unlock()
	}
	latStats(t, mode, "a reply in the open thread pane", ds)
	mu.Lock()
	defer mu.Unlock()
	for i := range ds {
		d := &ds[i]
		at, ok := actAt[rs[i].ts]
		switch {
		case !ok:
			t.Logf("%s: reply %d's activity entry never came (entries first: %v)", mode, i+1, rs[i].activityFirst)
		case !d.drawn.IsZero() && d.drawn.After(at.Add(vsync)): // never drawn: latStats said so
			t.Errorf("%s: reply %d (entries first: %v) reached Activity %v after the socket brought it, the pane %v: Activity first",
				mode, i+1, rs[i].activityFirst, at.Sub(d.pushed).Round(time.Millisecond), d.drawn.Sub(d.pushed).Round(time.Millisecond))
		}
	}
}

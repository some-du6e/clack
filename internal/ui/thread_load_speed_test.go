package ui

// Karim, 2026-11-20: "https://hackclub.slack.com/archives/C06QV2T1P4G/p1710818631730789
// make this thread load really fucking fast make no mistakes". That
// thread has 1,078,423 replies from 233 people. Clack read every one of
// them (conversations.replies, 200 a page, about 5,400 requests) before
// the pane drew a reply; Slack's thread pane reads one page of 28, or two
// around a reply, and pages as it scrolls (core thread_pages.go, messages
// thread_load.go). The fake serves a thread of the same size, every reply
// made up on request (fakeslack huge_thread.go: nothing of the real
// thread is read or kept), in a public channel the viewer isn't in, and
// the pane opens on it the three ways Karim reaches it:
//
//   - through its permalink (the channel at the root, then the root's
//     reply bar);
//   - from the channel (the reply bar);
//   - from Activity, at a reply in the middle that mentions the viewer.
//
// Each open must draw its first replies within a few seconds, with
// Slack's requests and page size: one {oldest: root, inclusive, limit 28}
// from the root, two {latest: reply} and {oldest: reply} around a reply.
// Scrolled, the pane pages with {oldest: the slice's end} (down) and
// {latest: its start} (up), one page at a time, the rows on screen not
// moving as a page lands above them; it never sits at a slice's end that
// stops short of the thread's, and shows no reply box there. Memory stays
// with the pages read (the store holds the root and those replies, and
// lets the thread go when the pane closes: truncateThreadHistory in a
// channel the viewer isn't in), and frames stay short while scrolling up
// through older pages. The measurements are logged (time to the first
// replies, requests, frames, heap). Synthetic data only.

import (
	"image"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/views"
)

func TestHugeThreadLoadsLikeSlack(t *testing.T) {
	const n = 1078423
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	th := fs.AddHugeThread("largest-thread-fixture", n, 233)
	mid := n / 2
	midTS := fs.SetHugeThreadMention(mid)
	ch, root := th.Channel, th.Root
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	app := e.a.data.core
	reply := fs.HugeThreadReplyTS
	reqs := func() []string {
		var out []string
		for _, r := range fs.HugeThreadRequests() {
			var b []string
			for _, k := range []string{"oldest", "latest", "limit", "inclusive", "cursor"} {
				if v := r.Get(k); v != "" {
					b = append(b, k+"="+v)
				}
			}
			out = append(out, strings.Join(b, " "))
		}
		return out
	}
	page := func(bound, ts string) string { return bound + "=" + ts + " limit=28 inclusive=true" }
	heap0 := liveHeapMB()
	t.Logf("heap before any open: %.1f MB", heap0)

	// opened runs frames from the open until the pane lays ts's row out
	// on screen (a frame after the open: a closed pane keeps its rows),
	// and logs how long that took and in how many frames
	opened := func(t *testing.T, what, ts string, open func()) {
		t.Helper()
		t0 := time.Now()
		open()
		frames := 1
		e.frame()
		drawn := e.untilSoft(20*time.Second, func() bool {
			frames++
			tv := e.threadView(ch, root)
			if tv == nil {
				return false
			}
			for _, r := range tv.RowRects() {
				if r.TS == ts {
					return true
				}
			}
			return false
		})
		el := time.Since(t0)
		if !drawn {
			rows := 0
			if tv := e.threadView(ch, root); tv != nil {
				rows = len(tv.RowRects())
			}
			held, _ := app.S.Thread(ch, root)
			t.Fatalf("%s: no first replies drawn %v after the open (%d frames): %d replies requests so far, %d rows on screen, the store holds %d of the thread",
				what, el.Round(time.Millisecond), frames, len(fs.HugeThreadRequests()), rows, len(held))
		}
		t.Logf("%s: first replies drawn %v after the open, in %d frames; %d replies requests so far", what, el.Round(time.Millisecond), frames, len(fs.HugeThreadRequests()))
		if el > 5*time.Second {
			t.Errorf("%s: the first replies took %v (a 10-reply thread's open draws in well under a second)", what, el)
		}
	}
	// closed closes the pane and checks the store let the thread go
	closed := func(t *testing.T, what string) {
		t.Helper()
		// truncateThreadHistory reads the channel (conversations.info,
		// which opening it asked for)
		e.until("the channel known", 10*time.Second, func() bool {
			c, ok := app.Conv(ch)
			return ok && c.Kind == "channel" && !c.Member
		})
		e.a.CloseSecondary()
		e.frame()
		e.frame()
		if threadHistoryHeld(app, ch, root) {
			t.Errorf("%s: the closed thread's history stayed (a public channel the viewer isn't in: removeAllTimestamps)", what)
		}
		if held, ok := app.S.Thread(ch, root); ok && len(held) > 1 {
			t.Errorf("%s: the store kept %d of the closed thread's messages", what, len(held))
		}
	}
	checkHeld := func(t *testing.T, what string, max int) {
		t.Helper()
		held, _ := app.S.Thread(ch, root)
		t.Logf("%s: the store holds %d of the thread's messages", what, len(held))
		if len(held) > max {
			t.Errorf("%s: the store holds %d of the thread's messages, more than the %d the pages read", what, len(held), max)
		}
	}

	// 1. the permalink: the channel at the root, then its reply bar
	t.Run("permalink", func(t *testing.T) {
		u := "https://fake.slack.com/archives/" + ch + "/p" + strings.ReplaceAll(root, ".", "")
		pl, ok := core.ParsePermalink(u)
		if !ok || pl.Kind != "message" || pl.ChannelID != ch || pl.TS != root {
			t.Fatalf("the permalink parses as %+v", pl)
		}
		e.a.Navigate("c/" + pl.ChannelID + "/m/" + pl.TS) // what following it in the client navigates to (slackc slackRoute)
		e.until("the channel at the root", 20*time.Second, func() bool {
			c := e.a.convos[ch+"/"]
			if c == nil {
				return false
			}
			l, ok := c.list.(channelList)
			return ok && l.ChannelView.RowText(root) != ""
		})
		opened(t, "permalink", reply(1), func() {
			e.a.OpenThread(ch, root) // the reply bar's click
			e.a.FocusThreadComposer(ch, root)
		})
		got := reqs()
		if len(got) == 0 || got[0] != page("oldest", root) {
			t.Fatalf("the open from the root asked %v; want one %q first", got, page("oldest", root))
		}
		e.settle(time.Second)
		for i, r := range reqs()[1:] {
			if !strings.HasPrefix(r, "oldest=") || !strings.HasSuffix(r, " limit=28 inclusive=true") {
				t.Errorf("request %d after the open: %q, not loadPost's", i+1, r)
			}
		}
		if got := len(reqs()); got > 3 {
			t.Errorf("%d replies requests a second after the open", got)
		}
		tv := e.threadView(ch, root)
		if !tv.HidesComposer() {
			t.Error("the reply box shows over a slice short of the thread's end (getThreadRowsAndKeys: loading-end, no input)")
		}
		checkHeld(t, "permalink", 1+28*len(reqs()))
		closed(t, "permalink")
	})

	// 2. the channel's reply bar, then three pages down
	t.Run("channel", func(t *testing.T) {
		e.a.Navigate("c/" + ch)
		e.until("the channel", 20*time.Second, func() bool {
			c := e.a.convos[ch+"/"]
			if c == nil {
				return false
			}
			l, ok := c.list.(channelList)
			return ok && l.ChannelView.RowText(root) != ""
		})
		before := len(reqs())
		opened(t, "channel", reply(1), func() {
			e.a.OpenThread(ch, root)
			e.a.FocusThreadComposer(ch, root)
		})
		if got := reqs()[before:]; len(got) == 0 || got[0] != page("oldest", root) {
			t.Fatalf("the open from the channel asked %v", got)
		}
		e.settle(300 * time.Millisecond)
		tv := e.threadView(ch, root)
		r := tv.ListRect()
		pt := image.Pt((r.Min.X+r.Max.X)/2, (r.Min.Y+r.Max.Y)/2)
		start := len(reqs())
		// wheel down until three more loadPost pages came in, in order
		for i := 0; len(reqs()) < start+3; i++ {
			if i > 1500 {
				t.Fatalf("no third newer page after %d wheel steps; requests %v", i, reqs()[before:])
			}
			e.scroll(pt, 80)
			e.frame()
			time.Sleep(2 * time.Millisecond)
		}
		e.settle(500 * time.Millisecond)
		got := reqs()[before:]
		end := 28
		for i, q := range got[1:] {
			if want := page("oldest", reply(end)); q != want {
				t.Errorf("newer page %d: %q, want %q (loadPost from the slice's end)", i+1, q, want)
				break
			}
			end += 27
		}
		tv = e.threadView(ch, root)
		if !tv.HidesComposer() {
			t.Error("the reply box shows over a slice short of the thread's end")
		}
		checkHeld(t, "channel", 1+28*len(got))
		closed(t, "channel")
	})

	// 3. Activity: the mention in the middle of the thread, then older
	// pages scrolled up through
	t.Run("activity", func(t *testing.T) {
		on := "on"
		inbox := map[string]*string{"activity_inbox": &on, "nested_secondary_view": &on, "secondary_view_default_width": &on,
			"activity_inbox_remove_persistent_detail_panel": &on, "activity_entry_layout_revisions_2": &on, "activity_global_settings": &on}
		app.SetExperimentGroups(inbox)
		e.a.Navigate("activity")
		e.frame()
		e.until("the activity feed", 20*time.Second, func() bool { return fs.CallCount("activity.feed") > 0 })
		e.settle(time.Second)
		act, _ := e.a.views[Route("activity")].(*views.Activity)
		if act == nil {
			t.Fatal("no Activity view")
		}
		before := len(reqs())
		opened(t, "activity", midTS, func() {
			e.until("the mention open beside the feed", 20*time.Second, func() bool {
				if !e.a.secondary.open() {
					act.StepItem(1, false) // Alt+Down: the item opens as its click opens it
				}
				return e.a.secondary.open() && e.a.secondary.kind == secThread
			})
		})
		if h := e.threadView(ch, root).Highlighted(); h != midTS {
			t.Errorf("the mention's reply isn't flashing (highlighted %q): jumpToThreadReply's highlightTs from Activity", h)
		}
		got := reqs()[before:]
		sorted := append([]string(nil), got...)
		sort.Strings(sorted)
		want := []string{page("latest", midTS), page("oldest", midTS)}
		if len(sorted) != 2 || sorted[0] != want[0] || sorted[1] != want[1] {
			t.Fatalf("the open at reply %d asked %v; want the two reads around it, %v", mid, got, want)
		}
		e.settle(300 * time.Millisecond)
		if got := len(reqs()) - before; got != 2 {
			t.Errorf("%d replies requests after the open settled, want the 2 around the reply", got)
		}
		tv := e.threadView(ch, root)
		r := tv.ListRect()
		pt := image.Pt((r.Min.X+r.Max.X)/2, (r.Min.Y+r.Max.Y)/2)
		var frames []time.Duration
		frame := func() {
			t0 := time.Now()
			e.frame()
			frames = append(frames, time.Since(t0))
		}
		start := mid - 27 // the slice's start: the older read's
		const pages = 8
		for p := 1; p <= pages; p++ {
			n0 := len(reqs())
			// the page's answer waits (HoldLoad) until the wheel has
			// stopped, so the rows' places before it lands are the
			// reader's
			release := messages.HoldLoad(ch, root)
			for i := 0; len(reqs()) == n0; i++ {
				if i > 1500 {
					release()
					t.Fatalf("older page %d: no loadPre after %d wheel steps", p, i)
				}
				e.scroll(pt, -80)
				frame()
				time.Sleep(2 * time.Millisecond)
			}
			if q, want := reqs()[n0], page("latest", reply(start)); q != want {
				release()
				t.Fatalf("older page %d: %q, want %q (loadPre from the slice's start)", p, q, want)
			}
			// no wheel while it lands: the rows on screen stay where they are
			var ys map[string]int
			for stable := 0; stable < 3; {
				frame()
				now := map[string]int{}
				for _, rr := range e.threadView(ch, root).RowRects() {
					now[rr.TS] = rr.R.Min.Y
				}
				if len(now) > 0 && len(now) == len(ys) && sameYs(now, ys) {
					stable++
				} else {
					stable = 0
				}
				ys = now
				time.Sleep(5 * time.Millisecond)
			}
			release()
			e.until("older page landed", 10*time.Second, func() bool {
				frame()
				first := paneSliceFirst(e.threadView(ch, root))
				return first == reply(start-27)
			})
			for i := 0; i < 3; i++ {
				frame()
			}
			moved := 0
			for _, rr := range e.threadView(ch, root).RowRects() {
				if y, ok := ys[rr.TS]; ok && y != rr.R.Min.Y {
					moved++
					if moved == 1 {
						t.Errorf("older page %d: reply %s moved from y %d to %d as the page landed above it", p, rr.TS, y, rr.R.Min.Y)
					}
				}
			}
			start -= 27
		}
		e.settle(500 * time.Millisecond)
		got = reqs()[before:]
		if len(got) != 2+pages {
			t.Errorf("%d replies requests for the open and %d older pages, want %d", len(got), pages, 2+pages)
		}
		ds := append([]time.Duration(nil), frames...)
		sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
		p95 := ds[len(ds)*95/100]
		t.Logf("scrolling up through %d older pages: %d frames, median %v, p95 %v, longest %v", pages, len(ds),
			ds[len(ds)/2].Round(time.Microsecond), p95.Round(time.Microsecond), ds[len(ds)-1].Round(time.Microsecond))
		if p95 > 150*time.Millisecond || ds[len(ds)-1] > time.Second {
			t.Errorf("frames while scrolling up: p95 %v, longest %v", p95, ds[len(ds)-1])
		}
		checkHeld(t, "activity", 1+28*len(got))
		heap := liveHeapMB()
		t.Logf("heap with %d older pages read: %.1f MB (%.1f MB over before any open)", pages, heap, heap-heap0)
		if heap-heap0 > 96 {
			t.Errorf("the heap grew %.1f MB for a few pages of one thread", heap-heap0)
		}
		closed(t, "activity")
		t.Logf("replies requests over the three opens: %d (all of the thread would be about %d at 200 a page)", len(reqs()), n/200+1)
		for k := range inbox {
			inbox[k] = nil // the experiments as the account has them again
		}
		app.SetExperimentGroups(inbox)
	})

	// 4. a jump to a reply deep in the thread while the pane shows it from
	// its root (a notification's click, a reply's permalink, a shared
	// message: jumpToMessage, then openThreadAt's JumpTo): the pane loads
	// the page that holds the reply (the two reads around it, as Slack's
	// jumpToThreadReply sets the start the slice is read around) and
	// lands on it, flashing it, instead of dropping a jump its slice
	// doesn't hold or showing the newest page
	t.Run("jump", func(t *testing.T) {
		// without the nested secondary view a jump from the thread's own
		// channel highlights its reply (jumpToThreadReply's highlight rule,
		// openThreadAt); under it, only one from elsewhere does
		app.SetExperimentGroup("nested_secondary_view", "")
		defer app.ClearExperimentGroup("nested_secondary_view")
		e.a.Navigate("c/" + ch)
		e.until("the channel", 20*time.Second, func() bool {
			c := e.a.convos[ch+"/"]
			if c == nil {
				return false
			}
			l, ok := c.list.(channelList)
			return ok && l.ChannelView.RowText(root) != ""
		})
		opened(t, "jump: the thread from its root", reply(1), func() { e.a.OpenThread(ch, root) })
		e.settle(300 * time.Millisecond)
		deep := n / 3
		deepTS := reply(deep)
		before := len(reqs())
		t0 := time.Now()
		e.a.jumpToMessage(ch, deepTS, root, false)
		landed := e.untilSoft(20*time.Second, func() bool {
			tv := e.threadView(ch, root)
			if tv == nil {
				return false
			}
			for _, r := range tv.RowRects() {
				if r.TS == deepTS {
					return true
				}
			}
			return false
		})
		el := time.Since(t0)
		tv := e.threadView(ch, root)
		first, last, _, _ := tv.SliceRange()
		if !landed {
			t.Fatalf("the jump to reply %d never landed on it (%v; %d requests since: %v; the pane draws %s..%s)",
				deep, el.Round(time.Millisecond), len(reqs())-before, reqs()[before:], first, last)
		}
		t.Logf("jump to reply %d: on screen %v after the jump, %d requests", deep, el.Round(time.Millisecond), len(reqs())-before)
		if h := tv.Highlighted(); h != deepTS {
			t.Errorf("the reply jumped to isn't flashing (highlighted %q)", h)
		}
		got := append([]string(nil), reqs()[before:]...)
		sort.Strings(got)
		if want := []string{page("latest", deepTS), page("oldest", deepTS)}; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
			t.Errorf("the jump asked %v; want the two reads around the reply, %v", got, want)
		}
		r := tv.ListRect()
		for _, rr := range tv.RowRects() {
			if rr.TS == deepTS && rr.R.Min.Y > r.Min.Y+r.Dy()/2 {
				t.Errorf("the reply landed at y %d, below the middle of the list (%v)", rr.R.Min.Y, r)
			}
		}
		closed(t, "jump")
	})
}

func sameYs(a, b map[string]int) bool {
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

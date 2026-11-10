package views

// The Unreads view against internal/fakeslack's Unreads fixtures
// (Size.Unreads, synthetic data only): Slack's frames, rows, marks,
// collapse, menus and realtime updates (unreads.go). VIEWS_SHOTS_DIR set
// (the unreads-shots workflow) also saves light and dark screenshots.

import (
	"context"
	"encoding/json"
	"image"
	"net/url"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// unrFake boots a core.App on the Unreads fixtures with realtime on.
func unrFake(t *testing.T, log *callLog) (*core.App, *fakeslack.Server) {
	t.Helper()
	sz := fakeslack.Small
	sz.Unreads = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	if log != nil {
		fs.SetHook(func(method string, f url.Values) (any, bool) {
			log.rec(method, f)
			return nil, false
		})
	}
	app := core.New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(app.Shutdown)
	evs, stop := app.Subscribe()
	defer stop()
	if err := app.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
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

func unrEnv(t *testing.T, dark bool, log *callLog) (*Unreads, *vaHost, *vaEnv, *fakeslack.Server) {
	app, fs := unrFake(t, log)
	h := &vaHost{slickHost: &slickHost{app: app, th: theme.New(dark)}}
	v := NewUnreads(h)
	e := &vaEnv{size: image.Pt(1100, 900)}
	e.draw = func(gtx layout.Context) {
		v.Layout(gtx)
		for _, o := range h.open() {
			o.Layout(gtx)
		}
	}
	deadline := time.Now().Add(15 * time.Second)
	for !(v.built && len(v.rows) > 0 && v.rows[len(v.rows)-1].kind == unrFooter) {
		if time.Now().After(deadline) {
			var st []string
			for _, f := range v.frames {
				st = append(st, f.ch+":"+formatInt(int64(len(f.msgs)))+":"+map[bool]string{true: "end", false: "more"}[f.reachedEnd]+":"+map[bool]string{true: "loading", false: ""}[f.loading])
			}
			t.Fatalf("frames not loaded: built %v, %d rows, %v, toasts %q", v.built, len(v.rows), st, h.toasts())
		}
		v.list.ScrollTo(len(v.rows)) // pages load as the list nears its end
		e.frame()
		time.Sleep(5 * time.Millisecond)
	}
	v.list.Position = layout.Position{}
	e.frame()
	return v, h, e, fs
}

func unrHead(v *Unreads, ch string) (unrRow, bool) {
	for _, r := range v.rows {
		if r.kind == unrHeader && r.f.ch == ch {
			return r, true
		}
	}
	return unrRow{}, false
}

// Slack's frames: the unread, unmuted channels the viewer is in, in
// "priority" order on a paid team (channels with mentions first), each
// fetched with fetchHistory's params, paged past 28.
func TestUnreadsFramesLikeSlack(t *testing.T) {
	log := &callLog{}
	for _, dark := range []bool{false, true} {
		v, _, e, fs := unrEnv(t, dark, log)
		got := strings.Join(v.Frames(), ",")
		want := fakeslack.UnreadsLaunchID + "," + fakeslack.UnreadsDesignID
		if got != want {
			t.Fatalf("frames %s, want %s (muted and read channels left out, mentions first)", got, want)
		}
		launch, _ := unrHead(v, fakeslack.UnreadsLaunchID)
		design, _ := unrHead(v, fakeslack.UnreadsDesignID)
		if launch.count != 31 || !launch.f.reachedEnd || unrMsgCount(launch.count, true) != "31 messages" {
			t.Errorf("launch: %d messages, end %v", launch.count, launch.f.reachedEnd)
		}
		if design.count != 3 || unrMsgCount(design.count, true) != "3 messages" {
			t.Errorf("design: %d messages", design.count)
		}
		// two pages for launch (28 + 3), from its last read
		n := log.count("conversations.history", func(f url.Values) bool {
			return f.Get("channel") == fakeslack.UnreadsLaunchID && f.Get("limit") == "28" && f.Get("inclusive") == "true" &&
				f.Get("no_user_profile") == "true"
		})
		if n < 2 {
			t.Errorf("launch fetched %d times with Slack's params, want 2 pages", n)
		}
		if log.count("conversations.history", func(f url.Values) bool {
			return f.Get("channel") == fakeslack.UnreadsLaunchID && f.Get("oldest") == fs.UnreadsLastRead(fakeslack.UnreadsLaunchID)
		}) == 0 {
			t.Error("the first page didn't start at the last read")
		}
		// grouping: design's first two unread messages are one sender within 5 minutes
		var hdr []bool
		for _, r := range v.rows {
			if r.kind == unrMsgRow && r.f.ch == fakeslack.UnreadsDesignID {
				hdr = append(hdr, r.header)
			}
		}
		if len(hdr) != 3 || !hdr[0] || hdr[1] || !hdr[2] {
			t.Errorf("design sender rows %v, want [true false true]", hdr)
		}
		vaShot(t, e, "unreads", dark)
	}
}

// Mark as Read (conversations.mark at the frame's end), its Undo (back to
// the initial cursor), Esc on the topmost frame, then Mark All Messages
// Read (unread.markRead markers) and the empty state's Undo.
func TestUnreadsMarkRead(t *testing.T) {
	for _, dark := range []bool{false, true} {
		v, _, e, fs := unrEnv(t, dark, nil)
		launch, _ := unrHead(v, fakeslack.UnreadsLaunchID)
		f := launch.f
		end, initial := f.end, f.initial
		f.markBtn.Click()
		e.frame()
		e.until(t, "conversations.mark", func() bool { return fs.Marked(fakeslack.UnreadsLaunchID) == end })
		e.frame()
		if r, _ := unrHead(v, fakeslack.UnreadsLaunchID); f.headerState(r.count, f.after()) != 2 || f.marked != 31 {
			t.Fatalf("after Mark as Read: state %d, marked %d", f.headerState(r.count, f.after()), f.marked)
		}
		vaShot(t, e, "unreads-marked", dark)
		f.undo.Click()
		e.frame()
		e.until(t, "the undo mark", func() bool { return fs.Marked(fakeslack.UnreadsLaunchID) == initial })
		e.frame()
		if r, _ := unrHead(v, fakeslack.UnreadsLaunchID); r.count != 31 {
			t.Fatalf("after Undo: %d messages shown", r.count)
		}
		// Esc marks the topmost frame (launch) and moves on
		if !v.MarkActiveRead() {
			t.Fatal("Esc marked nothing")
		}
		e.until(t, "Esc's mark", func() bool { return fs.Marked(fakeslack.UnreadsLaunchID) == end })
		e.frame()
		// Mark All Messages Read
		v.markAll.Click()
		e.frame()
		e.until(t, "unread.markRead", func() bool {
			for _, c := range fs.UnreadsCalls() {
				if c.Method == "unread.markRead" {
					return true
				}
			}
			return false
		})
		var markers map[string]string
		for _, c := range fs.UnreadsCalls() {
			if c.Method == "unread.markRead" {
				_ = json.Unmarshal([]byte(c.Form.Get("markers")), &markers)
			}
		}
		if markers[fakeslack.UnreadsDesignID] == "" {
			t.Errorf("markers %v lack the design channel", markers)
		}
		e.frame()
		if unrAnyUnread(v.frames) || len(v.marked) == 0 {
			t.Fatal("not the empty state with Undo after Mark All")
		}
		vaShot(t, e, "unreads-empty", dark)
		v.undoAll.Click()
		e.frame()
		e.until(t, "the undo markers", func() bool {
			n := 0
			for _, c := range fs.UnreadsCalls() {
				if c.Method == "unread.markRead" {
					n++
				}
			}
			return n == 2
		})
		e.frame()
		if !unrAnyUnread(v.frames) {
			t.Error("Undo didn't bring the conversations back")
		}
	}
}

// Realtime: a message in a frame's channel shows "1 new message" under
// it; one in a channel outside the frames counts toward the header's
// refresh button, which rebuilds the frames.
func TestUnreadsRealtime(t *testing.T) {
	for _, dark := range []bool{false, true} {
		v, _, e, fs := unrEnv(t, dark, nil)
		fs.PostUnread(fakeslack.UnreadsDesignID, "U0FAKE00002", "a synthetic follow-up")
		e.until(t, "the show-newer row", func() bool {
			for _, r := range v.rows {
				if r.kind == unrShowNewer && r.f.ch == fakeslack.UnreadsDesignID && r.count == 1 {
					return true
				}
			}
			return false
		})
		fs.PostUnread(fakeslack.JoinBannerID, "U0FAKE00003", "hello newcomers")
		e.until(t, "the refresh button", func() bool { return v.newUnread() == 1 })
		vaShot(t, e, "unreads-realtime", dark)
		v.refreshBtn.Click()
		e.frame()
		e.until(t, "the rebuilt frames", func() bool {
			return strings.Contains(strings.Join(v.Frames(), ","), fakeslack.JoinBannerID) && v.newUnread() == 0
		})
	}
}

// Collapse and expand: unread.collapse / unread.expand with the
// unread_collapsed_channels pref.
func TestUnreadsCollapse(t *testing.T) {
	v, _, e, fs := unrEnv(t, false, nil)
	r, _ := unrHead(v, fakeslack.UnreadsDesignID)
	v.list.ScrollTo(v.headerRow(r.f)) // its header on screen
	e.frame()
	r.f.toggle.Click()
	e.frame()
	e.until(t, "unread.collapse", func() bool {
		for _, c := range fs.UnreadsCalls() {
			if c.Method == "unread.collapse" && c.Form.Get("channel") == fakeslack.UnreadsDesignID {
				return true
			}
		}
		return false
	})
	if got := v.h.Core().UnreadCollapsed(); len(got) != 1 || got[0] != fakeslack.UnreadsDesignID {
		t.Errorf("unread_collapsed_channels %v", got)
	}
	e.frame()
	if r, _ := unrHead(v, fakeslack.UnreadsDesignID); r.f.headerState(r.count, 0) != 1 || unrNewText(r.count, true) != "3 new messages" {
		t.Errorf("collapsed header state %d", r.f.headerState(r.count, 0))
	}
	vaShot(t, e, "unreads-collapsed", false)
	r.f.toggle.Click()
	e.frame()
	e.until(t, "unread.expand", func() bool {
		for _, c := range fs.UnreadsCalls() {
			if c.Method == "unread.expand" {
				return true
			}
		}
		return false
	})
}

// The sort menu's rows and its write; the new order rebuilds the frames.
func TestUnreadsSortMenu(t *testing.T) {
	log := &callLog{}
	v, h, e, _ := unrEnv(t, false, log)
	if v.sortLabel() != "Sorted by recommended order" {
		t.Errorf("default sort label %q (a paid team sorts by priority)", v.sortLabel())
	}
	v.openSortMenu()
	e.frame()
	if len(h.open()) != 1 {
		t.Fatalf("%d menus open", len(h.open()))
	}
	vaShot(t, e, "unreads-sort-menu", false)
	h.open()[0].(interface{ Dismiss() }).Dismiss()
	if err := v.h.Core().SetPref(context.Background(), "all_unreads_sort_order", "alphabetical"); err != nil {
		t.Fatal(err)
	}
	e.until(t, "the alphabetical frames", func() bool {
		return strings.Join(v.Frames(), ",") == fakeslack.UnreadsDesignID+","+fakeslack.UnreadsLaunchID
	})
	if log.count("users.prefs.set", func(f url.Values) bool {
		return f.Get("name") == "all_unreads_sort_order" && f.Get("value") == "alphabetical"
	}) != 1 {
		t.Error("no users.prefs.set all_unreads_sort_order")
	}
}

// Slack's sorts on fixed candidates.
func TestUnreadsSortOrders(t *testing.T) {
	cs := []unrCand{
		{id: "C1", name: "beta", kind: "channel", lastRead: "100.0", latest: "300.0", priority: 1},
		{id: "C2", name: "alpha", kind: "channel", lastRead: "200.0", latest: "250.0", mentions: 1},
		{id: "D1", name: "zed", kind: "im", lastRead: "50.0", latest: "400.0", priority: 5},
		{id: "C3", name: "gamma", kind: "private", lastRead: "10.0", latest: "20.0", starred: true},
	}
	ids := func(cs []unrCand) string {
		var s []string
		for _, c := range cs {
			s = append(s, c.id)
		}
		return strings.Join(s, ",")
	}
	for order, want := range map[string]string{
		"alphabetical": "C3,C2,C1,D1", // starred, then channels A–Z, then DMs
		"priority":     "C3,C2,C1,D1", // starred, mentions, priority
		"newest":       "D1,C1,C2,C3",
		"oldest":       "C3,D1,C1,C2",
	} {
		if got := ids(unrSort(cs, order, nil)); got != want {
			t.Errorf("%s: %s, want %s", order, got, want)
		}
	}
	secs := []core.Section{{ID: "S1", All: []string{"C2", "D1"}}, {ID: "S2", All: []string{"C1"}}}
	if got := ids(unrSort(cs, "sidebar", secs)); got != "C2,D1,C1,C3" {
		t.Errorf("sidebar: %s", got)
	}
}

// The empty state's copy is Slack's en-US list; the octopus line swaps.
func TestUnreadsEmptyCopy(t *testing.T) {
	if len(unrEmptyMsgs) != 22 {
		t.Errorf("%d messages, Slack's en-US list has 22", len(unrEmptyMsgs))
	}
	e, ok := unrSwap(unrEmpty{"octopus", "All done.", "The world is your oyster."})
	if !ok || e.emoji != "oyster" || e.two != "The world is your octopus." {
		t.Errorf("swap: %+v", e)
	}
	if _, ok := unrSwap(unrEmpty{"rocket", "All done.", "The future is yours."}); ok {
		t.Error("the rocket swapped")
	}
	if unrMsgCount(28, false) != "28+ messages" || unrMsgCount(1, true) != "1 message" || unrNewText(2, false) != "New messages" {
		t.Error("header counts")
	}
}

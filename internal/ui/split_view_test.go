package ui

// Slack's split view (split.go) against internal/fakeslack (synthetic
// workspace; nothing talks to Slack): opening a channel from its ⋮ menu,
// the sidebar's row menu and the split modifier click, the column's width
// and resizer, the header's close and back buttons, a thread and the
// composer inside it, Escape, and the window's minimum width. With
// PREVIEW_SHOTS_DIR set (the split-view-shots workflow) each state is
// saved as a screenshot.

import (
	"image"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// splitChannels are two channels the viewer is in (sidebar order).
func splitChannels(t *testing.T, e *perfEnv) (string, string) {
	t.Helper()
	var ids []string
	e.until("two member channels", 20*time.Second, func() bool {
		ids = ids[:0]
		for _, s := range e.a.model().Sections {
			for _, id := range s.Rows {
				if c := e.a.model().conv(id); c != nil && c.Kind == "channel" && !c.Preview {
					ids = append(ids, id)
				}
			}
		}
		return len(ids) >= 2
	})
	return ids[0], ids[1]
}

// splitPosts records chat.postMessage's channels.
func splitPosts(e *perfEnv) func() []string {
	var mu sync.Mutex
	var chans []string
	e.fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "chat.postMessage" {
			mu.Lock()
			chans = append(chans, f.Get("channel"))
			mu.Unlock()
		}
		return nil, false
	})
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), chans...)
	}
}

// splitDrag queues a press-drag-release on the split resizer, from the
// card's left edge to x (window px).
func (e *perfEnv) splitDrag(x int) {
	c := e.a.split.card
	from := f32.Pt(float32(c.Min.X), float32(c.Min.Y+c.Dy()/2))
	to := f32.Pt(float32(x), from.Y)
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: from},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: from},
	)
	e.frame()
	// a move with the button held is a drag (Gio's router makes the Drag)
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: from.Add(f32.Pt((to.X-from.X)/2, 0))},
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: to},
	)
	e.frame()
	e.r.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: to})
	e.frame()
	e.frame()
	e.a.split.lastDown = time.Time{} // the next press is no double click
}

// The channel ⋮ menu's "Open in split view" puts the channel in the split
// column (Slack's width: 30% of the workspace in a window over 1200px),
// focused, with its composer posting to it; a thread opened inside it
// stays in it with a back button; Escape closes it.
func TestSplitViewChannelMenu(t *testing.T) {
	e, _ := previewEnv(t)
	chA, chB := splitChannels(t, e)
	posts := splitPosts(e)
	e.openChannel(chA)
	e.settle(300 * time.Millisecond)

	m := popups.NewChannelMenu(e.a, image.Pt(900, 60), chB)
	var split popups.MenuItem
	for _, it := range m.Items() {
		if it.Key == popups.SplitKey {
			split = it
		}
	}
	if split.Label != "Open in split view" || split.Icon != "move-to-split-view" || split.Hint != popups.SplitHint() || split.Do == nil {
		t.Fatalf("the channel menu's split row: %+v", split)
	}
	if want := "Ctrl Alt Click"; shortcut.GOOS != "darwin" && split.Hint != want {
		t.Errorf("hint %q, want %q", split.Hint, want)
	}
	split.Do()
	e.settle(400 * time.Millisecond)
	if got := e.a.splitTop(); got != Route("c/"+chB) {
		t.Fatalf("split view shows %q, want c/%s", got, chB)
	}
	if !e.a.split.focus {
		t.Error("the split view didn't get the focus")
	}
	// 1440 wide: the workspace is 1440-70-4 = 1366, so 30% (409.8) → 410
	if w := e.a.split.col.Dx(); w < 409 || w > 411 {
		t.Errorf("split column %dpx, want 410 (30%% of the workspace)", w)
	}
	if c := e.a.split.card; c.Max.X != e.size.X-int(theme.PanelInset) || c.Min.X != e.a.split.col.Min.X+splitGap || c.Min.Y != int(theme.TopNavH) {
		t.Errorf("card %v (column %v)", c, e.a.split.col)
	}
	if e.a.split.minW != 994 {
		t.Errorf("window minimum %d, want Slack's 994 (70+320+300+300+4)", e.a.split.minW)
	}
	if e.a.split.mode != "sticky" || e.a.prefs.SplitW != e.a.split.width || e.a.prefs.SplitWMode != "sticky" {
		t.Errorf("width %d mode %q, prefs %d %q", e.a.split.width, e.a.split.mode, e.a.prefs.SplitW, e.a.prefs.SplitWMode)
	}
	if e.a.split.pane.convos[chB+"/"] == nil || e.a.convos[chA+"/"] == nil {
		t.Fatal("each pane should have its own conversation")
	}
	e.shot("split-view-channel")

	// the split composer has the focus: typing and Enter post to chB
	e.typeText("hello from the split view")
	e.settle(100 * time.Millisecond)
	e.key(key.NameReturn, 0)
	e.until("chat.postMessage", 10*time.Second, func() bool { return len(posts()) > 0 })
	if got := posts(); got[0] != chB {
		t.Errorf("the split composer posted to %v, want %s", got, chB)
	}
	e.settle(300 * time.Millisecond)

	// a thread opened inside the split view replaces its channel there,
	// with the channel as its back stack; one from the main view goes to
	// the main view's thread pane
	// someone else's root (not the send above), one with replies if any
	var ts string
	self := e.a.model().SelfID
	if tl := e.a.data.core.S.Timeline(chB); tl != nil {
		for i := len(tl.Msgs) - 1; i >= 0; i-- {
			m := tl.Msgs[i]
			if m.User == self || (m.ThreadTS != "" && m.ThreadTS != m.TS) {
				continue
			}
			if ts == "" || m.ReplyCount > 0 {
				ts = m.TS
			}
			if m.ReplyCount > 0 {
				break
			}
		}
	}
	if ts == "" {
		t.Fatal("no message in the split channel")
	}
	replies := e.fs.CallCount("conversations.replies")
	e.a.inPane(paneSplit, func() { e.a.OpenThread(chB, ts) })
	e.settle(300 * time.Millisecond)
	if got := e.a.splitTop(); got != Route("c/"+chB+"/t/"+ts) || len(e.a.split.stack) != 2 {
		t.Fatalf("thread in the split view: %v", e.a.split.stack)
	}
	if e.a.split.pane.convos[chB+"/"+ts] == nil {
		t.Fatal("the thread didn't draw in the split view")
	}
	// its replies load (a cached thread wouldn't ask again: no failure)
	for dl := time.Now().Add(5 * time.Second); time.Now().Before(dl) && e.fs.CallCount("conversations.replies") <= replies; {
		e.frame()
		time.Sleep(5 * time.Millisecond)
	}
	e.settle(500 * time.Millisecond)
	if e.a.secondary.kind == secThread {
		t.Error("the split view's thread opened in the main view's pane")
	}
	e.a.inPane(paneSplit, func() {
		if !e.a.PaneBack() {
			t.Error("no back button for the split view's thread")
		}
	})
	e.shot("split-view-thread")
	e.a.inPane(paneSplit, e.a.GoPaneBack)
	e.frame()
	if got := e.a.splitTop(); got != Route("c/"+chB) {
		t.Errorf("back: %q", got)
	}

	// Escape with the focus in the split view closes it (a first one may
	// mark the channel read)
	e.a.split.focus = true
	for i := 0; i < 3 && e.a.splitOpen(); i++ {
		e.key(key.NameEscape, 0)
		e.settle(200 * time.Millisecond)
	}
	if e.a.splitOpen() {
		t.Fatal("Escape didn't close the split view")
	}
	if e.a.split.minW != 0 {
		t.Errorf("window minimum still %d after closing", e.a.split.minW)
	}
	if e.a.router.Current() != Route("c/"+chA) {
		t.Errorf("the main view moved to %q", e.a.router.Current())
	}
}

// The resizer: Slack's 300–1000 limits, 300 left for the primary view, a
// snap to the default within 15px, a dragged width kept as "manual", a
// double click back to the default.
func TestSplitViewResize(t *testing.T) {
	e, _ := previewEnv(t)
	chA, chB := splitChannels(t, e)
	e.openChannel(chA)
	e.a.OpenSplit("c/" + chB)
	e.settle(300 * time.Millisecond)
	s := &e.a.split
	def := s.col.Dx()

	e.splitDrag(s.card.Min.X - 200)
	if w := s.col.Dx(); w != def+200 || s.mode != "manual" {
		t.Errorf("dragged 200px left: %dpx (%q), want %d manual", w, s.mode, def+200)
	}
	if e.a.prefs.SplitW != s.width || e.a.prefs.SplitWMode != "manual" {
		t.Errorf("prefs %d %q", e.a.prefs.SplitW, e.a.prefs.SplitWMode)
	}

	// past the maximum: the primary view keeps 300 (1366-232-300 = 834)
	e.splitDrag(s.card.Min.X - 600)
	if w := s.col.Dx(); w != 834 {
		t.Errorf("dragged far left: %dpx, want 834", w)
	}
	// past the minimum
	e.splitDrag(s.card.Max.X - 50)
	if w := s.col.Dx(); w != splitMinW {
		t.Errorf("dragged far right: %dpx, want %d", w, splitMinW)
	}
	// near the default: snaps to it ("auto", sticky once released)
	e.splitDrag(e.size.X - int(theme.PanelInset) - def - 10 + splitGap)
	if w := s.col.Dx(); w != def || s.mode != "sticky" {
		t.Errorf("dragged near the default: %dpx (%q), want %d sticky", w, s.mode, def)
	}
	// a double click resets a manual width
	e.splitDrag(s.card.Min.X - 150)
	if s.mode != "manual" {
		t.Fatalf("mode %q after a drag", s.mode)
	}
	c := s.card
	p := f32.Pt(float32(c.Min.X), float32(c.Min.Y+c.Dy()/2))
	for i := 0; i < 2; i++ {
		e.r.Queue(
			pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: p},
			pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p},
		)
		e.frame()
	}
	e.frame()
	if w := s.col.Dx(); w != def || s.mode != "sticky" {
		t.Errorf("double click: %dpx (%q), want %d sticky", w, s.mode, def)
	}
	e.splitDrag(s.card.Min.X - 200)
	e.settle(200 * time.Millisecond)
	e.shot("split-view-wide")

	// the width outlives the split view, as Slack keeps it on the device
	e.a.CloseSplit()
	e.settle(100 * time.Millisecond)
	e.a.OpenSplit("c/" + chB)
	e.settle(200 * time.Millisecond)
	if w := s.col.Dx(); w != def+200 {
		t.Errorf("reopened at %dpx, want the dragged %d", w, def+200)
	}
}

// The sidebar's row menu has "Open in split view" in its own group before
// Leave; the split modifier click (Ctrl+Alt, ⌘⌥ on macOS) on a row opens
// the conversation in the split view without leaving the open one; the
// thread menu offers it outside the split view only; Simplified layout
// mode has none of it.
func TestSplitViewSidebarAndModifierClick(t *testing.T) {
	e, _ := previewEnv(t)
	chA, chB := splitChannels(t, e)
	e.openChannel(chA)
	e.settle(300 * time.Millisecond)

	var got []popups.MenuItem
	sbMenuTap = func(ch string, items []popups.MenuItem) { got = items }
	t.Cleanup(func() { sbMenuTap = nil })
	e.a.openSidebarMenu(chB, image.Pt(200, 200), slackc.JSFunc{})
	var labels []string
	for _, it := range got {
		l := it.Label
		if it.Separator {
			l = "---"
		}
		labels = append(labels, l)
	}
	if s := strings.Join(labels, " | "); !strings.Contains(s, "--- | Open in split view | --- | Leave channel") {
		t.Errorf("sidebar menu: %s", s)
	}
	e.key(key.NameEscape, 0)
	e.frame()

	// the modifier click on a sidebar row
	p, ok := e.splitRowPos(chB)
	if !ok {
		t.Fatal("no sidebar row for the second channel")
	}
	mods := shortcut.Mod() | key.ModAlt
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Modifiers: mods, Position: p},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Modifiers: mods, Position: p},
	)
	e.settle(300 * time.Millisecond)
	if got := e.a.splitTop(); got != Route("c/"+chB) {
		t.Fatalf("the modifier click put %q in the split view, want c/%s", got, chB)
	}
	if e.a.router.Current() != Route("c/"+chA) {
		t.Errorf("the modifier click navigated the main view to %q", e.a.router.Current())
	}
	e.shot("split-view-sidebar-click")

	// the thread header's ⋮: Open in split view in the main view only
	tl := e.a.data.core.S.Timeline(chA)
	if tl == nil || len(tl.Msgs) == 0 {
		t.Fatal("no message in the main channel")
	}
	msg := core.Message{Channel: chA, TS: tl.Msgs[len(tl.Msgs)-1].TS}
	hasSplit := func(m *popups.Menu) bool {
		for _, it := range m.Items() {
			if it.Key == popups.SplitKey {
				return true
			}
		}
		return false
	}
	if !hasSplit(popups.NewThreadHeaderMenu(e.a, image.Rect(0, 0, 10, 10), msg, false, false, func() {})) {
		t.Error("the thread menu has no Open in split view")
	}
	e.a.inPane(paneSplit, func() {
		if hasSplit(popups.NewThreadHeaderMenu(e.a, image.Rect(0, 0, 10, 10), msg, false, false, func() {})) {
			t.Error("the split view's thread menu offers Open in split view")
		}
	})

	// the channel and its split view in the same container show the same
	// conversation twice, each with its own list
	e.a.OpenSplit("c/" + chA)
	e.settle(300 * time.Millisecond)
	if e.a.split.pane.convos[chA+"/"] == nil || e.a.split.pane.convos[chA+"/"] == e.a.convos[chA+"/"] {
		t.Error("the same channel in both views shares one conversation")
	}
	e.shot("split-view-same-channel")

	// Simplified layout mode: no split rows, no split view
	e.a.th.SetDark(true)
	e.settle(200 * time.Millisecond)
	e.shot("split-view-dark")
	e.a.th.SetDark(false)
	m := popups.NewChannelMenu(e.a, image.Pt(900, 60), chB)
	popups.StripSplitView(m, true)
	for _, l := range m.Labels() {
		if l == "Open in split view" {
			t.Error("StripSplitView left the split row")
		}
	}
	if ls := strings.Join(m.Labels(), "|"); strings.Contains(ls, "---|---") || strings.HasSuffix(ls, "---") {
		t.Errorf("separators left doubled: %s", ls)
	}
}

// splitRowPos is the window position of conversation ch's sidebar row
// (dp = px here), from the rows the sidebar laid out (svEnv.rowPos's
// arithmetic: the header, the jump box and its gap above the rows).
func (e *perfEnv) splitRowPos(ch string) (f32.Point, bool) {
	s := &e.a.sidebar
	y := e.a.primaryOrigin.Y + 49 + 28 + 10
	idx := s.shown
	n := len(s.rows)
	if idx != nil {
		n = len(idx)
	}
	for k := 0; k < n; k++ {
		i := k
		if idx != nil {
			i = idx[k]
		}
		r := &s.rows[i]
		h := int(theme.SidebarRowH)
		switch r.kind {
		case sbGap, sbDivider:
			h = r.gap
		case sbNote:
			h = 44
		}
		if r.kind == sbConv && r.key == ch {
			return f32.Pt(float32(int(theme.RailW)+60), float32(y+h/2)), true
		}
		y += h
	}
	return f32.Point{}, false
}

// A narrow column moves header buttons out as Slack's
// useActionsToMoveToMenu does (view-header 1DLG): search under 550px, the
// bell under 500px, the Huddle button once floor((w-340)/30) is 0; the
// Slackbot DM only ever loses its bell.
func TestSplitViewHeaderFit(t *testing.T) {
	a := &App{}
	gtx := layout.Context{Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	ch := &convInfo{Kind: "channel"}
	bot := &convInfo{Kind: "im", User: "USLACKBOT"}
	for _, c := range []struct {
		w      int
		conv   *convInfo
		btns   string
		huddle bool
	}{
		{1000, ch, "kebab search bell", true},
		{550, ch, "kebab search bell", true},
		{549, ch, "kebab bell", true},
		{500, ch, "kebab bell", true},
		{499, ch, "kebab", true},
		{370, ch, "kebab", true},
		{369, ch, "kebab", false},
		{499, bot, "kebab search", true},
		{520, bot, "kebab search bell", true},
	} {
		btns, hud := a.headerFit(gtx, c.w, c.conv, []string{"kebab", "search", "bell"}, true)
		if got := strings.Join(btns, " "); got != c.btns || hud != c.huddle {
			t.Errorf("%dpx %s: %q huddle=%v, want %q huddle=%v", c.w, c.conv.Kind, got, hud, c.btns, c.huddle)
		}
	}
}

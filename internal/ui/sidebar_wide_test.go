package ui

import (
	"encoding/json"
	"image"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// W2CY's wide sizes: isWideWidth is .38 of the space beside the rail
// within 320 … 720; isExtraWideWidth is .5 (.24 beside the split view)
// within 320 … the space less 300 for primary, 260 for a secondary view
// and 300 for the split view. A manual width is clamped to the same
// range; an intent counts only in manual mode.
func TestSidebarWideBoundsLikeSlack(t *testing.T) {
	avail := func(win float32) float32 { return win - theme.RailW - sbRightPad }
	for _, c := range []struct {
		win    float32
		intent int
		mode   string
		want   int
	}{
		{1440, 0, "auto", 519}, // 1366 * .38 = 519.08
		{1920, 0, "auto", 701}, // 1846 * .38 = 701.48
		{1366, 0, "auto", 491}, // 1292 * .38 = 490.96
		{800, 0, "auto", 320},
		{2600, 0, "auto", 720},
		{1440, 600, "manual", 600},
		{1440, 900, "manual", 720},
		{1440, 100, "manual", 320},
		{1440, 600, "auto", 519},
	} {
		s := sbWideState{intent: c.intent, mode: c.mode}
		if got := s.widthFor(sidebarWideBounds(avail(c.win))); got != c.want {
			t.Errorf("wide, window %v, %d %q: %d, want %d", c.win, c.intent, c.mode, got, c.want)
		}
	}
	for _, c := range []struct {
		win              float32
		secondary, split bool
		intent           int
		mode             string
		want, hi         int
	}{
		{1440, false, false, 0, "auto", 683, 1066}, // 1366 * .5; 1366 - 300
		{1440, true, false, 0, "auto", 683, 806},   // less 260 for the secondary view
		{1440, false, true, 0, "auto", 328, 766},   // .24 beside the split view
		{1440, false, false, 2000, "manual", 1066, 1066},
		{1440, true, true, 900, "manual", 506, 506}, // 1366 - 300 - 260 - 300
		{800, false, false, 0, "auto", 363, 426},
		{600, false, false, 0, "auto", 320, 320}, // the minimum wins (SwVo q)
		{1440, false, false, 100, "manual", 320, 1066},
	} {
		b := sidebarXWideBounds(avail(c.win), c.secondary, c.split)
		s := sbWideState{intent: c.intent, mode: c.mode}
		if got := s.widthFor(b); got != c.want || int(b.hi+.5) != c.hi {
			t.Errorf("extra wide, window %v, secondary %v split %v, %d %q: %d (max %v), want %d (max %d)", c.win, c.secondary, c.split, c.intent, c.mode, got, b.hi, c.want, c.hi)
		}
	}
	for _, c := range []struct {
		w          int
		mode       string
		wantIntent int
		wantMode   string
	}{
		{600, "manual", 600, "manual"},
		{600, "auto", 0, "auto"},
		{600, "", 0, "auto"},
		{0, "manual", 0, "auto"},
	} {
		var s sbWideState
		s.load(c.w, c.mode)
		if s.intent != c.wantIntent || s.mode != c.wantMode {
			t.Errorf("load(%d, %q) = %d %q, want %d %q", c.w, c.mode, s.intent, s.mode, c.wantIntent, c.wantMode)
		}
	}
}

// The DMs list's and Activity's column is Slack's wide sidebar with the
// Home sidebar's BaseResizer: a drag holds the pointer and draws in the
// frame it arrives in, clamps to 320 … 720, snaps to the default within
// 15px ("auto"), a double click resets it, and the width is the
// column's own (viewLayoutSidebarWideWidthIntent / Mode), kept in
// ui.json without touching the Home sidebar's. Under activity_inbox
// Activity's column is the extra wide one, with its own width again.
func TestWideSidebarResizeLikeSlack(t *testing.T) {
	e, _ := previewEnv(t)
	e.frame()
	a := e.a
	c := a.Core()
	off := "off"
	c.SetExperimentGroups(map[string]*string{"activity_inbox": &off, "activity_inbox_remove_persistent_detail_panel": &off})
	rail := int(theme.RailW)
	home := a.sidebar.width

	a.Navigate("dms")
	e.settle(300 * time.Millisecond)
	s := &a.sbWide
	if s.rz.edge != 519 || s.mode == "manual" {
		t.Fatalf("1440 window: the DMs list is %d (%q), want 519 auto", s.rz.edge, s.mode)
	}
	e.shot("wide-sidebar-dms-auto")
	y := a.primaryOrigin.Y + 300
	drawn := func(st *sbWideState, want int, what string) {
		t.Helper()
		if st.rz.edge != want {
			t.Errorf("%s: column %d, want %d in the same frame", what, st.rz.edge, want)
		}
	}
	drag := func(st *sbWideState, to int) {
		t.Helper()
		from := rail + st.rz.edge
		e.rzPress(image.Pt(from, y))
		if !st.rz.dragging() {
			t.Fatalf("the press at %d didn't start a drag", from)
		}
		e.rzMove(image.Pt((from+to)/2, y+100))
		e.rzMove(image.Pt(to, y))
		e.rzRelease(image.Pt(to, y))
		e.frame()
		st.rz.lastPress = time.Time{} // the next press is no double click
	}

	edge := rail + 519
	e.rzPress(image.Pt(edge, y))
	e.rzMove(image.Pt(edge+100, y+200)) // a jerk into the conversation
	drawn(s, 619, "100px right")
	e.rzMove(image.Pt(e.size.X+300, y)) // out of the window
	drawn(s, sbWideMax, "out of the window on the right")
	e.rzMove(image.Pt(-300, y))
	drawn(s, sbWideMin, "out of the window on the left")
	e.rzMove(image.Pt(edge+100, y))
	e.rzRelease(image.Pt(edge+100, y))
	s.rz.lastPress = time.Time{}
	if s.rz.dragging() || s.intent != 619 || s.mode != "manual" {
		t.Errorf("released 100px right: dragging %v, %d %q; want a manual 619", s.rz.dragging(), s.intent, s.mode)
	}
	e.shot("wide-sidebar-dms-dragged")
	if a.sidebar.width != home || a.sidebar.mode == "manual" {
		t.Errorf("the Home sidebar moved with it: %d (%q), want %d auto", a.sidebar.width, a.sidebar.mode, home)
	}
	a.flushPrefs()
	b, err := os.ReadFile(filepath.Join(e.home, "ui.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		W        int    `json:"sidebar_w"`
		WideW    int    `json:"sidebar_wide_w"`
		WideMode string `json:"sidebar_wide_w_mode"`
	}
	if err := json.Unmarshal(b, &saved); err != nil || saved.WideW != 619 || saved.WideMode != "manual" || saved.W != 0 {
		t.Errorf("ui.json sidebar_wide_w %d %q, sidebar_w %d (%v); want 619 manual, no sidebar_w", saved.WideW, saved.WideMode, saved.W, err)
	}

	drag(s, edge+10) // within 15px of the default
	if s.rz.edge != 519 || s.mode != "auto" {
		t.Errorf("dragged near the default: %d (%q), want 519 auto", s.rz.edge, s.mode)
	}
	drag(s, edge-120)
	if s.rz.edge != 399 || s.mode != "manual" {
		t.Fatalf("dragged 120px left: %d (%q), want 399 manual", s.rz.edge, s.mode)
	}

	// Activity (no activity_inbox) shares the column's width
	a.Navigate("activity")
	e.settle(300 * time.Millisecond)
	drawn(s, 399, "Activity's column")
	e.shot("wide-sidebar-activity")

	// a double click resets it to the default
	at := image.Pt(rail+s.rz.edge, y)
	for range 2 {
		e.rzPress(at)
		e.rzRelease(at)
	}
	e.frame()
	if s.rz.edge != 519 || s.mode != "auto" {
		t.Errorf("double click: %d (%q), want 519 auto", s.rz.edge, s.mode)
	}

	// the extra wide column (ActivityInbox): its own width
	on := "on"
	c.SetExperimentGroups(map[string]*string{"activity_inbox": &on})
	e.settle(300 * time.Millisecond)
	x := &a.sbXWide
	drawn(x, 683, "the inbox's extra wide column")
	drag(x, rail+800)
	drawn(x, 800, "the inbox dragged to 800")
	if x.mode != "manual" || s.mode != "auto" || s.rz.edge != 519 {
		t.Errorf("extra wide %q, wide %d %q; want manual and the wide one's 519 auto untouched", x.mode, s.rz.edge, s.mode)
	}
	e.shot("wide-sidebar-activity-inbox")

	// the window squishes it: primary keeps 300, down to the 320 minimum
	e.size = image.Pt(1000, 900)
	e.frame()
	e.frame()
	drawn(x, 1000-rail-int(theme.PanelInset)-300, "a 1000 window (primary keeps 300)")
	e.size = image.Pt(600, 900)
	e.frame()
	e.frame()
	drawn(x, sbWideMin, "a 600 window (the minimum)")
}

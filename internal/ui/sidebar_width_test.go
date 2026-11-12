package ui

import (
	"encoding/json"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// Slack's Home sidebar column (W2CY): auto is .24 of the window less the
// rail and the 4px padding, within 180 … 450; a manual width is clamped
// to the same range.
func TestSidebarWidthLikeSlack(t *testing.T) {
	for _, c := range []struct {
		win    float32
		intent int
		mode   string
		want   int
	}{
		{1440, 0, "auto", 328}, // (1440-74)*.24 = 327.84
		{1920, 0, "auto", 443}, // Karim's ~420 at ~1820
		{1820, 0, "auto", 419},
		{800, 0, "auto", 180},
		{2600, 0, "auto", 450},
		{1440, 300, "manual", 300},
		{1440, 600, "manual", 450},
		{1440, 100, "manual", 180},
		{1440, 300, "auto", 328}, // an intent counts only in manual mode
	} {
		s := sidebarState{}
		s.intent, s.mode = c.intent, c.mode
		if got := s.widthFor(c.win); got != c.want {
			t.Errorf("window %v, %d %q: %d, want %d", c.win, c.intent, c.mode, got, c.want)
		}
	}
	// ui.json from before auto saved the fixed 232 for everyone
	for _, c := range []struct {
		w          int
		mode       string
		wantIntent int
		wantMode   string
	}{
		{232, "", 0, "auto"},
		{300, "", 300, "manual"},
		{0, "", 0, "auto"},
		{-5, "", 0, "auto"},
		{232, "manual", 232, "manual"},
		{400, "auto", 0, "auto"},
	} {
		s := sidebarState{}
		s.loadWidth(c.w, c.mode)
		if s.intent != c.wantIntent || s.mode != c.wantMode {
			t.Errorf("loadWidth(%d, %q) = %d %q, want %d %q", c.w, c.mode, s.intent, s.mode, c.wantIntent, c.wantMode)
		}
	}
}

// sidebarDrag drags the sidebar's resizer from its edge to x.
func (e *perfEnv) sidebarDrag(x int) {
	a := e.a
	y := float32(a.primaryOrigin.Y + 200)
	from := f32.Pt(float32(int(theme.RailW)+a.sidebar.width), y)
	to := f32.Pt(float32(x), y)
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: from},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: from},
	)
	e.frame()
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: from.Add(f32.Pt((to.X-from.X)/2, 0))},
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: to},
	)
	e.frame()
	e.r.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: to})
	e.frame()
	e.frame()
	a.sidebar.lastPress = time.Time{} // the next press is no double click
}

// The resizer: a dragged width is "manual" and kept in ui.json, a drag
// near the default snaps back to "auto", a double click resets it, and
// auto follows the window.
func TestSidebarResizeLikeSlack(t *testing.T) {
	e, _ := previewEnv(t)
	e.frame()
	s := &e.a.sidebar
	if s.width != 328 || s.mode == "manual" {
		t.Fatalf("1440 window: sidebar %d (%q), want 328 auto", s.width, s.mode)
	}
	edge := int(theme.RailW) + 328

	e.sidebarDrag(edge + 100)
	if s.width != 428 || s.mode != "manual" {
		t.Errorf("dragged 100px right: %d (%q), want 428 manual", s.width, s.mode)
	}
	e.a.flushPrefs()
	b, err := os.ReadFile(filepath.Join(e.home, "ui.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		W         int             `json:"sidebar_w"`
		Mode      string          `json:"sidebar_w_mode"`
		Collapsed json.RawMessage `json:"collapsed"`
	}
	if err := json.Unmarshal(b, &saved); err != nil || saved.W != 428 || saved.Mode != "manual" {
		t.Errorf("ui.json sidebar_w %d %q (%v), want 428 manual", saved.W, saved.Mode, err)
	}
	if saved.Collapsed != nil {
		t.Errorf("ui.json still keeps collapsed sections: %s (they're the account's)", saved.Collapsed)
	}

	e.sidebarDrag(int(theme.RailW) + 900)
	if s.width != sbWidthMax {
		t.Errorf("dragged far right: %d, want %d", s.width, sbWidthMax)
	}
	e.sidebarDrag(int(theme.RailW) + 20)
	if s.width != sbWidthMin {
		t.Errorf("dragged far left: %d, want %d", s.width, sbWidthMin)
	}
	e.sidebarDrag(edge + 10) // within 15px of the default
	if s.width != 328 || s.mode != "auto" {
		t.Errorf("dragged near the default: %d (%q), want 328 auto", s.width, s.mode)
	}

	// a double click resets a manual width
	e.sidebarDrag(edge - 60)
	if s.mode != "manual" {
		t.Fatalf("dragged 60px left: %q, want manual", s.mode)
	}
	y := float32(e.a.primaryOrigin.Y + 200)
	at := f32.Pt(float32(int(theme.RailW)+s.width), y)
	for range 2 {
		e.r.Queue(
			pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at},
			pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: at},
		)
		e.frame()
		e.r.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: at})
		e.frame()
	}
	e.frame()
	if s.width != 328 || s.mode != "auto" {
		t.Errorf("double click: %d (%q), want 328 auto", s.width, s.mode)
	}

	// auto follows the window
	e.size = image.Pt(1920, 1000)
	e.frame()
	if s.width != 443 {
		t.Errorf("1920 window: %d, want 443", s.width)
	}
}

// A heading's toggle goes to the account (channel_sections "c"), and the
// account's change elsewhere (pref_change) redraws the sidebar.
func TestSectionCollapseSyncsInSidebar(t *testing.T) {
	e, fs := previewEnv(t)
	e.frame()
	a := e.a
	c := a.Core()
	var head *sbRow
	for i := range a.sidebar.rows {
		if r := &a.sidebar.rows[i]; r.kind == sbHeading && strings.HasPrefix(r.key, "L") {
			head = r
			break
		}
	}
	if head == nil {
		t.Fatal("no section heading")
	}
	id := head.key
	sectionC := func() string {
		v, _ := fs.Pref("channel_sections")
		str, _ := v.(string)
		var l map[string]map[string]any
		_ = json.Unmarshal([]byte(str), &l)
		cv, _ := l[id]["c"].(string)
		return cv
	}

	a.sbSetCollapsed(head, true)
	e.frame()
	if !a.sidebar.collapsed[id] {
		t.Fatal("the toggle didn't collapse the heading")
	}
	c.FlushSectionCollapsed() // the 3 s debounce
	if got := sectionC(); got != "1" {
		t.Errorf("account channel_sections[%s].c = %q, want \"1\"", id, got)
	}
	e.settle(200 * time.Millisecond) // our write's echo keeps it collapsed
	if !a.sidebar.collapsed[id] {
		t.Error("the heading opened again after the write")
	}

	// another client opens it: pref_change
	evs, stop := c.Subscribe()
	defer stop()
	c.SetRealtime(true)
	dl := time.After(10 * time.Second)
	for up := false; !up; {
		select {
		case ev := <-evs:
			up = ev.Type == "connection" && ev.Connected
		case <-dl:
			t.Fatal("no realtime connection")
		}
	}
	fs.SetPrefs(map[string]any{"channel_sections": `{"` + id + `":{"c":"0"}}`})
	fs.Push(map[string]any{"type": "pref_change", "name": "channel_sections", "value": `{"` + id + `":{"c":"0"}}`})
	e.until("the heading open after pref_change", 10*time.Second, func() bool { return !a.sidebar.collapsed[id] })
}

// Activity and To-dos keep their own secondary pane width, as Slack keeps
// secondaryWidthIntentForActivity / …ForLater beside the channel views'.
func TestSecondaryWidthPerTab(t *testing.T) {
	a, _ := expNavApp(t, nil)
	a.router.Push("c/C1")
	a.syncSecondaryTab()
	a.secondary.width = 500 // dragged in a channel
	a.router.Push("activity")
	a.syncSecondaryTab()
	if a.secondary.width != 0 {
		t.Errorf("Activity took the channel's %d, want its own auto (0)", a.secondary.width)
	}
	a.secondary.width = 700 // dragged in Activity
	a.router.Push("later")
	a.syncSecondaryTab()
	if a.secondary.width != 0 {
		t.Errorf("To-dos: %d, want auto", a.secondary.width)
	}
	a.router.Push("c/C1")
	a.syncSecondaryTab()
	if a.secondary.width != 500 {
		t.Errorf("back in the channel: %d, want 500", a.secondary.width)
	}
	a.router.Push("activity")
	a.syncSecondaryTab()
	if a.secondary.width != 700 {
		t.Errorf("back in Activity: %d, want 700", a.secondary.width)
	}
	if a.secTabWidth("") != 500 || a.secTabWidth("activity") != 700 || a.secTabWidth("later") != 0 {
		t.Errorf("saved widths %d %d %d, want 500 700 0", a.secTabWidth(""), a.secTabWidth("activity"), a.secTabWidth("later"))
	}
}

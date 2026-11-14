package ui

import (
	"image"
	"image/color"
	"math"
	"runtime"
	"testing"
	"time"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/io/system"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// Electron 45's caption buttons at 1x (win_caption_button.cc,
// win_caption_button_container.cc, win_frame_view.cc): a 137 DIP container
// at the right edge, minimize 45 wide, maximize and close 46 with a 1 DIP
// gap before their backgrounds, from y 1 (restored) or 0 (maximized) to
// the overlay's height; 10 DIP symbols centred in the buttons' bounds
// (ClampToCenteredSize's integer halves), stroked 1 px.
func TestWcoGeometry(t *testing.T) {
	type rects [3]image.Rectangle
	cases := []struct {
		name      string
		w         int
		s, height float32
		maximized bool
		bounds    rects
		bg        rects
		sym       rects
		stroke    int
	}{
		{"1x, the client's overlay (39)", 1440, 1, 39, false,
			rects{image.Rect(1303, 1, 1348, 39), image.Rect(1348, 1, 1394, 39), image.Rect(1394, 1, 1440, 39)},
			rects{image.Rect(1303, 1, 1348, 39), image.Rect(1349, 1, 1394, 39), image.Rect(1395, 1, 1440, 39)},
			rects{image.Rect(1320, 15, 1330, 25), image.Rect(1366, 15, 1376, 25), image.Rect(1412, 15, 1422, 25)}, 1},
		{"1x maximized", 1440, 1, 39, true,
			rects{image.Rect(1303, 0, 1348, 39), image.Rect(1348, 0, 1394, 39), image.Rect(1394, 0, 1440, 39)},
			rects{image.Rect(1303, 0, 1348, 39), image.Rect(1349, 0, 1394, 39), image.Rect(1395, 0, 1440, 39)},
			rects{image.Rect(1320, 14, 1330, 24), image.Rect(1366, 14, 1376, 24), image.Rect(1412, 14, 1422, 24)}, 1},
		{"1x, the BrowserWindow's overlay (33)", 1000, 1, 33, false,
			rects{image.Rect(863, 1, 908, 33), image.Rect(908, 1, 954, 33), image.Rect(954, 1, 1000, 33)},
			rects{image.Rect(863, 1, 908, 33), image.Rect(909, 1, 954, 33), image.Rect(955, 1, 1000, 33)},
			rects{image.Rect(880, 12, 890, 22), image.Rect(926, 12, 936, 22), image.Rect(972, 12, 982, 22)}, 1},
		{"2x", 2880, 2, 39, false,
			rects{image.Rect(2606, 2, 2696, 78), image.Rect(2696, 2, 2788, 78), image.Rect(2788, 2, 2880, 78)},
			rects{image.Rect(2606, 2, 2696, 78), image.Rect(2698, 2, 2788, 78), image.Rect(2790, 2, 2880, 78)},
			rects{image.Rect(2641, 30, 2661, 50), image.Rect(2732, 30, 2752, 50), image.Rect(2824, 30, 2844, 50)}, 2},
	}
	for _, c := range cases {
		g := wcoGeometry(c.w, c.s, c.height, c.maximized)
		if g.bounds != c.bounds || g.bg != c.bg || g.sym != c.sym || g.stroke != c.stroke {
			t.Errorf("%s:\n bounds %v\n want   %v\n bg     %v\n want   %v\n sym    %v\n want   %v\n stroke %d want %d",
				c.name, g.bounds, c.bounds, g.bg, c.bg, g.sym, c.sym, g.stroke, c.stroke)
		}
	}
	// 1.5x: contiguous buttons, 15 px symbols, 2 px strokes
	g := wcoGeometry(2160, 1.5, 39, false)
	if g.bounds[0].Max.X != g.bounds[1].Min.X || g.bounds[1].Max.X != g.bounds[2].Min.X || g.bounds[2].Max.X != 2160 {
		t.Errorf("1.5x buttons not contiguous to the edge: %v", g.bounds)
	}
	if w := g.bounds[0].Min.X; 2160-w < 205 || 2160-w > 206 {
		t.Errorf("1.5x container %d px wide, want 137 DIPs", 2160-w)
	}
	if g.sym[0].Dx() != 15 || g.stroke != 2 {
		t.Errorf("1.5x symbol %d px, stroke %d; want 15, 2", g.sym[0].Dx(), g.stroke)
	}
}

// views::Button's hover_animation_: 150 ms ease-out, a partial fade
// taking its share of it; a press drops it, a release over the button
// brings it back at once.
func TestWcoHoverFade(t *testing.T) {
	t0 := time.Unix(1000, 0)
	at := func(d time.Duration) time.Time { return t0.Add(d) }
	near := func(got, want float64) bool { return got > want-1e-6 && got < want+1e-6 }
	var b wcoButton
	b.setState(wcoHovered, t0)
	if !b.hover.step(at(75*time.Millisecond)) || !near(b.hover.value, 0.75) {
		t.Fatalf("75 ms into the fade in: %v, want 0.75 (ease-out)", b.hover.value)
	}
	if b.hover.step(at(150*time.Millisecond)) || b.hover.value != 1 {
		t.Fatalf("150 ms: %v (still fading?), want 1", b.hover.value)
	}
	b.setState(wcoNormal, at(200*time.Millisecond))
	b.hover.step(at(275 * time.Millisecond))
	if !near(b.hover.value, 0.25) {
		t.Fatalf("75 ms into the fade out: %v, want 0.25", b.hover.value)
	}
	b.hover.step(at(350 * time.Millisecond))
	if b.hover.value != 0 {
		t.Fatalf("faded out: %v", b.hover.value)
	}
	// in for 75 ms (0.75), then out: 150 × 0.75 = 112.5 ms
	b.setState(wcoHovered, at(time.Second))
	b.hover.step(at(time.Second + 75*time.Millisecond))
	b.setState(wcoNormal, at(time.Second+75*time.Millisecond))
	if !b.hover.step(at(time.Second+75*time.Millisecond+112*time.Millisecond)) || b.hover.step(at(time.Second+75*time.Millisecond+113*time.Millisecond)) {
		t.Fatalf("a fade out from 0.75 doesn't take 112.5 ms (value %v)", b.hover.value)
	}
	// a press: no hover; the release over it: hovered at once
	b.setState(wcoHovered, at(2*time.Second))
	b.setState(wcoPressed, at(2*time.Second+10*time.Millisecond))
	if b.hover.value != 0 || b.hover.dir != 0 {
		t.Fatalf("pressed: hover %v", b.hover.value)
	}
	b.setState(wcoHovered, at(2*time.Second+20*time.Millisecond))
	if b.hover.value != 1 || b.hover.dir != 0 {
		t.Fatalf("released over the button: hover %v, want 1 at once", b.hover.value)
	}
}

// The symbol colour: the legacy sidebar theme's top_nav_text
// (useWindowThemedTitlebar's A.top_nav_text, OpEF ze).
func TestWcoThemeText(t *testing.T) {
	custom := `{"column_bg":"#111111","menu_bg":"#222222","active_item":"#333333","active_item_text":"#444444","hover_item":"#555555","text_color":"#666666","active_presence":"#777777","badge":"#888888"`
	withNav := custom + `,"top_nav_bg":"#999999","top_nav_text":"#ABCDEF"}`
	plain := custom + `}`
	noBadge := `{"column_bg":"#111111","menu_bg":"#222222","active_item":"#333333","active_item_text":"#444444","hover_item":"#555555","text_color":"#666666","active_presence":"#777777"}`
	ia := `{"top_nav_bg":"#000000","top_nav_text":"#123456"}`
	cases := []struct {
		dark                bool
		name, custom, iaTop string
		want                string
	}{
		{false, "", "", "", "#FFFFFF"},
		{true, "", "", "", "#D1D2D3"},
		{false, "default", "", "", "#FFFFFF"},
		{true, "default_theme", "", "", "#D1D2D3"},
		{false, "hoth_theme", "", "", "#383F45"},
		{true, "hoth_theme", "", "", "#F8F8FA"},
		{true, "cmyk_theme", "", "", "#0E0B01"},
		{false, "custom_theme", withNav, "", "#ABCDEF"},
		{false, "custom_theme", plain, ia, "#123456"},
		{false, "custom_theme", plain, "", "#666666"},
		{false, "custom_theme", noBadge, "", "#FFFFFF"},
		{true, "custom_theme", noBadge, "", "#D1D2D3"},
		{false, "hoth_theme", withNav, "", "#383F45"},
		{false, "cotton_theme", withNav, "", "#ABCDEF"},
		{false, "no_such_theme", "", "", "#FFFFFF"},
		{false, "", withNav, "", "#ABCDEF"},
	}
	for _, c := range cases {
		if got := wcoThemeText(c.dark, c.name, c.custom, c.iaTop); got != c.want {
			t.Errorf("dark %v, %q, custom %.20q, ia %q: %s, want %s", c.dark, c.name, c.custom, c.iaTop, got, c.want)
		}
	}
	for s, want := range map[string]color.NRGBA{
		"#D1D2D3":             {R: 0xd1, G: 0xd2, B: 0xd3, A: 0xff},
		"rgba(1,2,3,0.5)":     {R: 1, G: 2, B: 3, A: 0x80},
		"ABCDEF":              wcoBootSymbol, // Electron can't parse it: the overlay keeps #FFFFFF
		"not a colour at all": wcoBootSymbol,
	} {
		if got := wcoSymbolColor(s); got != want {
			t.Errorf("symbol colour of %q: %v, want %v", s, got, want)
		}
	}
}

// Slack's window minimum: 668×400 on Windows and macOS (the top nav is the
// caption there); Clack's 800×500 elsewhere, which keeps its title bar.
// The frame options: HiddenTitleBar on Windows; on macOS HiddenTitleBar
// and the traffic lights at Slack's {12, 12} (titlebar_darwin.go).
func TestMainWindowOptions(t *testing.T) {
	var cnf app.Config
	for _, o := range mainWindowOptions() {
		o(unit.Metric{PxPerDp: 1, PxPerSp: 1}, &cnf)
	}
	wantMin, chrome := image.Pt(800, 500), 0
	switch runtime.GOOS {
	case "windows":
		wantMin, chrome = image.Pt(668, 400), 1
	case "darwin":
		wantMin, chrome = image.Pt(668, 400), 2
	}
	if cnf.MinSize != wantMin || cnf.Size != image.Pt(1440, 900) || cnf.Title != AppName {
		t.Errorf("main window: min %v size %v title %q; want min %v, 1440x900, %q", cnf.MinSize, cnf.Size, cnf.Title, wantMin, AppName)
	}
	if n := len(mainWindowChrome()); n != chrome {
		t.Errorf("%d frame options on %s, want %d (HiddenTitleBar on Windows; and TrafficLightPosition on macOS)", n, runtime.GOOS, chrome)
	}
	if runtime.GOOS == "darwin" && (!macTitlebarOn || !closeHidesWithoutTray || !hideLeavesFullscreen) {
		t.Errorf("macOS: nav caption %v, close hides without a tray %v, Hide leaves full screen %v; want all", macTitlebarOn, closeHidesWithoutTray, hideLeavesFullscreen)
	}
}

// macOS (W35): with the title bar hidden (a.macNav) and no caption
// buttons, the top nav is the drag area under its widgets, the app menu's
// spacer included (on macOS the traffic lights over it are AppKit's
// buttons, and only .p-ia4_top_nav--windows makes it no-drag), with no
// space kept for windows controls; the search, history and help buttons
// are no-drag; full screen ends it.
func TestMacNavCaption(t *testing.T) {
	e := newPerfEnv(t, false)
	e.waitReady()
	e.settle(200 * time.Millisecond)
	if p := e.searchPillLeftOf(); e.actionOK(p) {
		t.Fatalf("the nav has a system action at %v with a title bar", p)
	}
	e.a.macNav = true
	t.Cleanup(func() { e.a.macNav = false })
	e.settle(200 * time.Millisecond)
	if e.a.wcoOn() || e.a.wcoNavReserve(ttContext(new(op.Ops), e.size)) != 0 {
		t.Fatal("caption buttons with the macOS title bar")
	}
	pill := e.a.searchPill
	hist := pill.Min.X - 9 - 3*26 - 2*6
	helpR := e.size.X - 8 // the right container's end: no windows controls
	checks := []struct {
		what string
		p    image.Point
		move bool
	}{
		{"the spacer, past the app menu button (Linux's)", image.Pt(50, 20), true},
		{"the spacer's right end", image.Pt(70, 30), true},
		{"the nav between the spacer and history", image.Pt((76+hist)/2, 20), true},
		{"the nav right of the search", image.Pt((pill.Max.X+helpR-26)/2, 20), true},
		{"the nav's top row", image.Pt((76+hist)/2, 0), true},
		{"the search box", wcoCentre(pill), false},
		{"the history buttons", image.Pt(pill.Min.X-9-13, 20), false},
		{"the help button", image.Pt(helpR-13, 20), false},
		{"under the nav", image.Pt(600, 300), false},
	}
	for _, c := range checks {
		got, ok := e.actionAt(c.p)
		if move := ok && got == system.ActionMove; move != c.move {
			t.Errorf("%s at %v: action %v (%v), want drag %v", c.what, c.p, got, ok, c.move)
		}
	}
	e.movePtr(image.Pt(helpR-13, 20))
	if !e.a.clicks.get("nav.help").Hovered() {
		t.Error("the help button isn't at the right container's end")
	}

	// full screen: no drag area
	e.a.winMode = app.Fullscreen
	e.settle(100 * time.Millisecond)
	if _, ok := e.actionAt(image.Pt((76+hist)/2, 20)); ok {
		t.Error("full screen: the nav is still a caption")
	}
	e.a.winMode = app.Windowed
}

func wcoCentre(r image.Rectangle) image.Point { return r.Min.Add(r.Max).Div(2) }

// searchPillLeftOf is the nav's empty space between the app menu's spacer
// (76 px) and the history buttons left of the search.
func (e *perfEnv) searchPillLeftOf() image.Point {
	return image.Pt((76+e.a.searchPill.Min.X-9-3*26-2*6)/2, 20)
}

func (e *perfEnv) actionOK(p image.Point) bool { _, ok := e.actionAt(p); return ok }

// actionAt is the router's system action at window point p.
func (e *perfEnv) actionAt(p image.Point) (system.Action, bool) {
	return e.r.ActionAt(f32.Pt(float32(p.X)+.5, float32(p.Y)+.5))
}

// The caption buttons in the signed-in window (as on Windows, a.wco.on):
// Electron's places, their action areas over the nav's drag area, the
// nav's widgets and spacers no-drag, the help button clear of the
// buttons, a click a WM_SYSCOMMAND (restore when maximized), the hover
// fading in; full screen hides them; without them the nav is no caption.
func TestWcoTopNav(t *testing.T) {
	e := newPerfEnv(t, false)
	var sent []uint32
	orig := wcoPost
	wcoPost = func(_ *App, sc uint32) { sent = append(sent, sc) }
	t.Cleanup(func() { wcoPost = orig })
	e.waitReady()
	e.settle(200 * time.Millisecond)
	if p := e.searchPillLeftOf(); e.actionOK(p) {
		t.Fatalf("the nav has a system action at %v without the caption buttons", p)
	}
	e.a.wco.on = true
	e.settle(200 * time.Millisecond)

	g := e.a.wco.geo
	if want := wcoGeometry(e.size.X, 1, 39, false); g != want {
		t.Fatalf("geometry %+v, want %+v", g, want)
	}
	if g.bounds[0] != image.Rect(1303, 1, 1348, 39) || g.bounds[2] != image.Rect(1394, 1, 1440, 39) {
		t.Fatalf("buttons %v: not Electron's at 1440 px", g.bounds)
	}
	for i, want := range []system.Action{system.ActionMinimize, system.ActionMaximize, system.ActionClose} {
		if got, ok := e.actionAt(wcoCentre(g.bounds[i])); !ok || got != want {
			t.Errorf("button %d: action %v (%v), want %v", i, got, ok, want)
		}
	}
	pill := e.a.searchPill
	hist := pill.Min.X - 9 - 3*26 - 2*6
	helpR := e.size.X - 8 - wcoReserveW // the right container's end
	checks := []struct {
		what string
		p    image.Point
		move bool
	}{
		{"the nav between the app menu and history", image.Pt((76+hist)/2, 20), true},
		{"the nav right of the search", image.Pt((pill.Max.X+helpR-26)/2, 20), true},
		{"the nav's top row", image.Pt((76+hist)/2, 0), true},
		{"the search box", wcoCentre(pill), false},
		{"the app menu button's spacer", image.Pt(60, 30), false},
		{"the history buttons", image.Pt(pill.Min.X-9-13, 20), false},
		{"the windows controls spacer", image.Pt(1440-8-wcoReserveW+3, 20), false},
		{"the help button", image.Pt(helpR-13, 20), false},
		{"under the nav", image.Pt(600, 300), false},
	}
	for _, c := range checks {
		got, ok := e.actionAt(c.p)
		if move := ok && got == system.ActionMove; move != c.move {
			t.Errorf("%s at %v: action %v (%v), want drag %v", c.what, c.p, got, ok, c.move)
		}
	}
	e.movePtr(image.Pt(helpR-13, 20))
	if !e.a.clicks.get("nav.help").Hovered() {
		t.Error("the help button isn't 138 px clear of the caption buttons")
	}

	for i, want := range []uint32{scMinimize, scMaximize, scClose} {
		sent = nil
		e.click(wcoCentre(g.bounds[i]))
		e.frame()
		e.frame()
		if len(sent) != 1 || sent[0] != want {
			t.Errorf("button %d's click sent %#x, want %#x", i, sent, want)
		}
	}
	// the hover fades in on close: 150 ms
	e.movePtr(wcoCentre(g.bounds[wcoClose]))
	e.settle(250 * time.Millisecond)
	if b := e.a.wco.btns[wcoClose]; b.state != wcoHovered || b.hover.value != 1 {
		t.Errorf("close hovered: state %d hover %v", b.state, b.hover.value)
	}

	// maximized: the restore button, from the top
	e.a.winMode = app.Maximized
	e.settle(100 * time.Millisecond)
	if e.a.wco.geo.bounds[1].Min.Y != 0 {
		t.Errorf("maximized buttons start at y %d, want 0", e.a.wco.geo.bounds[1].Min.Y)
	}
	if got, ok := e.actionAt(wcoCentre(e.a.wco.geo.bounds[1])); !ok || got != system.ActionUnmaximize {
		t.Errorf("maximized: the middle button's action %v, want ActionUnmaximize", got)
	}
	sent = nil
	e.click(wcoCentre(e.a.wco.geo.bounds[1]))
	e.frame()
	e.frame()
	if len(sent) != 1 || sent[0] != scRestore {
		t.Errorf("restore sent %#x, want SC_RESTORE", sent)
	}

	// full screen: no buttons, no caption, the help button at the edge
	e.a.winMode = app.Fullscreen
	e.settle(100 * time.Millisecond)
	if got, ok := e.actionAt(wcoCentre(g.bounds[wcoClose])); ok {
		t.Errorf("full screen: action %v over the close button", got)
	}
	if _, ok := e.actionAt(image.Pt((76+hist)/2, 20)); ok {
		t.Error("full screen: the nav is still a caption")
	}
	e.a.winMode = app.Windowed
	e.a.wco.on = false
}

// The top nav's narrow breakpoint moves out by the 138px the caption
// buttons take (668 + 138/zoom): under it the left container grows with
// the search instead of keeping the sidebar's width, and the search
// stays clear of the help button and the buttons.
func TestWcoNarrowNav(t *testing.T) {
	e := newPerfEnv(t, false)
	e.waitReady()
	e.a.wco.on = true
	t.Cleanup(func() { e.a.wco.on = false })
	for _, c := range []struct {
		w      int
		narrow bool
	}{{760, true}, {805, true}, {900, false}, {1440, false}} {
		e.size = image.Pt(c.w, 800)
		e.settle(150 * time.Millisecond)
		pill := e.a.searchPill
		navX := int(theme.RailW) + int(math.Round(float64(e.a.sidebar.width)))
		helpL := c.w - 8 - wcoReserveW - 26
		if c.narrow == (pill.Min.X == navX) {
			t.Errorf("%d px: the search starts at %d, the sidebar's edge %d: narrow %v", c.w, pill.Min.X, navX, c.narrow)
		}
		if pill.Max.X+8 > helpL {
			t.Errorf("%d px: the search (%v) runs into the help button at %d", c.w, pill, helpL)
		}
		if hist := pill.Min.X - 9 - 3*26 - 2*6; hist < 76 {
			t.Errorf("%d px: the history buttons start at %d, over the app menu's spacer", c.w, hist)
		}
	}
	// without the caption buttons the breakpoint is 668: 760 isn't narrow
	e.a.wco.on = false
	e.size = image.Pt(760, 800)
	e.settle(150 * time.Millisecond)
	if navX := int(theme.RailW) + int(math.Round(float64(e.a.sidebar.width))); e.a.searchPill.Min.X != navX {
		t.Errorf("760 px without the buttons: the search at %d, want the sidebar's edge %d", e.a.searchPill.Min.X, navX)
	}
}

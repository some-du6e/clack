package ui

import (
	"encoding/json"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/app"
	"gioui.org/unit"
)

// The displays the cases use, in physical pixels at 96 DPI (scale 1)
// unless they say otherwise.
var (
	wbPrimary = app.Display{Bounds: image.Rect(0, 0, 1920, 1080), WorkArea: image.Rect(0, 0, 1920, 1040), Scale: 1, Primary: true}
	wbRight   = app.Display{Bounds: image.Rect(1920, 0, 4480, 1440), WorkArea: image.Rect(1920, 0, 4480, 1400), Scale: 1}
	wbLeft    = app.Display{Bounds: image.Rect(-1920, 0, 0, 1080), WorkArea: image.Rect(-1920, 0, 0, 1040), Scale: 1}
	wbRunner  = app.Display{Bounds: image.Rect(0, 0, 1024, 768), WorkArea: image.Rect(0, 0, 1024, 728), Scale: 1, Primary: true}
	wbMac     = app.Display{Bounds: image.Rect(0, 0, 1512, 982), WorkArea: image.Rect(0, 33, 1512, 902), Scale: 1, Primary: true}
	wbSmall   = app.Display{Bounds: image.Rect(0, 0, 800, 600), WorkArea: image.Rect(0, 0, 800, 560), Scale: 1, Primary: true}
)

// wbRect is Slack's {x, y, width, height}.
func wbRect(x, y, w, h int) image.Rectangle { return image.Rect(x, y, x+w, y+h) }

// Slack's rV, the bounds of a window it has none saved for: the expected
// values are Slack's own functions (win32 main.bundle.cjs 4.53.276
// @806363..@807089) run in node on the same displays
// (/root/clack-ops/scratch/window-bounds/tools/bounds_truth.cjs), and at
// 150% and 200% the 1200×800 DIP floor in pixels.
func TestDefaultWindowBoundsLikeSlack(t *testing.T) {
	hidpi := app.Display{Bounds: image.Rect(0, 0, 3840, 2160), WorkArea: image.Rect(0, 0, 3840, 2080), Scale: 2, Primary: true}
	laptop := app.Display{Bounds: image.Rect(0, 0, 1920, 1080), WorkArea: image.Rect(0, 0, 1920, 1032), Scale: 1.5, Primary: true}
	for _, c := range []struct {
		name string
		d    app.Display
		want image.Rectangle
	}{
		{"1920x1080, taskbar at the bottom", wbPrimary, wbRect(360, 120, 1200, 800)},
		{"the windows-latest runner's 1024x768", wbRunner, wbRect(0, 23, 1024, 683)},
		{"2560x1440 right of the primary", wbRight, wbRect(2600, 280, 1200, 840)},
		{"a MacBook's 1512x982 points, menu bar and Dock", wbMac, wbRect(156, 68, 1200, 800)},
		{"800x600", wbSmall, wbRect(0, 14, 800, 533)},
		// 200%: 2400x1600 px, centred
		{"3840x2160 at 200%", hidpi, wbRect(720, 240, 2400, 1600)},
		// 150%: 1800x1200 px doesn't fit 1032 px high: 1548x1032
		{"1920x1080 at 150%", laptop, wbRect(186, 0, 1548, 1032)},
	} {
		if got := defaultWindowBounds(c.d); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// validateBoundsForDisplay and the minimum size (Slack's own code run in
// node on the same cases, as above): bounds on a connected display, or
// less than 10% past its work area, stay; others move onto the display
// they overlap most (none: the nearest on Windows, the primary on macOS)
// and shrink into it; a size under 668x400 DIPs grows.
func TestOpenWindowBoundsLikeSlack(t *testing.T) {
	nearest := displayNearest
	t.Cleanup(func() { displayNearest = nearest })
	saved := func(r image.Rectangle) *mainWindowSettings { return &mainWindowSettings{Bounds: winRectOf(r)} }
	for _, c := range []struct {
		name    string
		saved   *mainWindowSettings
		ds      []app.Display
		nearest bool
		want    image.Rectangle
	}{
		{"inside the work area", saved(wbRect(100, 100, 1200, 800)), []app.Display{wbPrimary}, true, wbRect(100, 100, 1200, 800)},
		{"less than 10% past it", saved(wbRect(-150, 50, 1200, 800)), []app.Display{wbPrimary}, true, wbRect(-150, 50, 1200, 800)},
		{"where a display was", saved(wbRect(2500, 200, 1200, 800)), []app.Display{wbPrimary}, true, wbRect(720, 200, 1200, 800)},
		{"bigger than the display", saved(wbRect(0, 0, 3000, 2000)), []app.Display{wbPrimary}, true, wbRect(0, 0, 1560, 1040)},
		{"on the second display", saved(wbRect(2500, 200, 1200, 800)), []app.Display{wbPrimary, wbRight}, true, wbRect(2500, 200, 1200, 800)},
		{"across two: the one it overlaps most", saved(wbRect(1500, 100, 1200, 800)), []app.Display{wbPrimary, wbRight}, true, wbRect(1920, 100, 1200, 800)},
		{"nothing saved: the primary's default", nil, []app.Display{wbRight, wbPrimary}, true, wbRect(360, 120, 1200, 800)},
		{"zero size saved: the default", saved(image.Rectangle{}), []app.Display{wbPrimary}, true, wbRect(360, 120, 1200, 800)},
		{"under the minimum", saved(wbRect(100, 100, 500, 300)), []app.Display{wbPrimary}, true, wbRect(100, 100, 668, 400)},
		{"far left of every display: the nearest", saved(wbRect(-5000, 100, 1200, 800)), []app.Display{wbPrimary, wbLeft}, true, wbRect(-1920, 100, 1200, 800)},
		{"far left, macOS: the primary", saved(wbRect(-5000, 100, 1200, 800)), []app.Display{wbPrimary, wbLeft}, false, wbRect(0, 100, 1200, 800)},
		{"the runner, saved off it", saved(wbRect(5000, 5000, 900, 600)), []app.Display{wbRunner}, true, wbRect(124, 128, 900, 600)},
		{"below the bottom", saved(wbRect(100, 900, 1200, 800)), []app.Display{wbPrimary}, true, wbRect(100, 240, 1200, 800)},
		{"macOS, nothing saved", nil, []app.Display{wbMac}, false, wbRect(156, 68, 1200, 800)},
	} {
		displayNearest = c.nearest
		if got := openWindowBounds(c.saved, c.ds, 668, 400); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	// the minimum is DIPs: 668x400 at 150% is 1002x600 px
	big := app.Display{Bounds: image.Rect(0, 0, 2880, 1800), WorkArea: image.Rect(0, 0, 2880, 1752), Scale: 1.5, Primary: true}
	if got, want := openWindowBounds(saved(wbRect(50, 60, 700, 500)), []app.Display{big}, 668, 400), wbRect(50, 60, 1002, 600); got != want {
		t.Errorf("under the minimum at 150%%: %v, want %v", got, want)
	}
}

// MAIN_WINDOW_ACTIONS: the window's bounds, maximized and full screen
// state; a maximized window's bounds are the default ones on its display
// (Slack's rV(getDisplayMatching(getBounds())), node: 1200x840 at
// (2600, 280) on the right display); off the screen nothing is kept.
func TestWindowSettingsLikeSlack(t *testing.T) {
	frame := wbRect(300, 200, 1000, 700)
	s, ok := windowSettingsOf(app.Windowed, frame, wbPrimary)
	if !ok || s.Maximized || s.FullScreen || s.Bounds == nil || s.Bounds.rect() != frame {
		t.Errorf("windowed: %+v %v, want its frame", s, ok)
	}
	s, ok = windowSettingsOf(app.Maximized, wbRect(1912, -8, 2576, 1416), wbRight)
	if !ok || !s.Maximized || s.FullScreen || s.Bounds == nil || s.Bounds.rect() != wbRect(2600, 280, 1200, 840) {
		t.Errorf("maximized on the right display: %+v %v, want the default bounds there", s, ok)
	}
	s, ok = windowSettingsOf(app.Fullscreen, wbRight.Bounds, wbRight)
	if !ok || !s.FullScreen || s.Maximized || s.Bounds == nil || s.Bounds.rect() != wbRight.Bounds {
		t.Errorf("full screen: %+v %v, want the screen's frame (Slack's getBounds)", s, ok)
	}
	if s, ok = windowSettingsOf(app.Windowed, image.Rectangle{}, app.Display{}); ok {
		t.Errorf("off the screen: %+v kept", s)
	}
	// Linux: the size in dp, windowed only
	if s, ok = windowSizeOf(app.Windowed, image.Pt(1500, 1050), 1.5); !ok || s.Size == nil || *s.Size != (winSize{1000, 700}) || s.Bounds != nil {
		t.Errorf("Linux windowed: %+v %v, want 1000x700 dp", s, ok)
	}
	if _, ok = windowSizeOf(app.Maximized, image.Pt(1920, 1080), 1); ok {
		t.Error("Linux maximized: size kept")
	}
}

// wbOptions applies the main window's placement options to a Config.
func wbOptions(opts []app.Option) app.Config {
	var c app.Config
	for _, o := range opts {
		o(unit.Metric{PxPerDp: 1, PxPerSp: 1}, &c)
	}
	return c
}

// mainWindowPlace's options: the validated bounds, then the saved mode;
// none to place where Gio lists no displays, only a saved size.
func TestMainWindowPlace(t *testing.T) {
	t.Cleanup(func() { displaysHook = nil })
	a := New(Options{ConfigDir: t.TempDir(), NoRealtime: true}, nil)
	displaysHook = func() []app.Display { return []app.Display{wbPrimary} }

	c := wbOptions(a.mainWindowPlace())
	if !a.winPlace || c.Bounds() != wbRect(360, 120, 1200, 800) || c.Mode != app.Windowed {
		t.Errorf("first launch: bounds %v mode %v (placed %v), want Slack's default on the primary, windowed", c.Bounds(), c.Mode, a.winPlace)
	}
	a.prefs.MainWindow = &mainWindowSettings{Bounds: winRectOf(wbRect(2500, 200, 1200, 800)), Maximized: true}
	if c = wbOptions(a.mainWindowPlace()); c.Bounds() != wbRect(720, 200, 1200, 800) || c.Mode != app.Maximized {
		t.Errorf("maximized on a display that's gone: bounds %v mode %v, want (720,200) 1200x800 maximized", c.Bounds(), c.Mode)
	}
	a.prefs.MainWindow = &mainWindowSettings{Bounds: winRectOf(wbPrimary.Bounds), FullScreen: true, Maximized: true}
	if c = wbOptions(a.mainWindowPlace()); c.Bounds() != wbPrimary.Bounds || c.Mode != app.Fullscreen {
		t.Errorf("full screen: bounds %v mode %v, want the screen, full screen first", c.Bounds(), c.Mode)
	}

	// Linux
	displaysHook = func() []app.Display { return nil }
	a.prefs.MainWindow = nil
	if opts := a.mainWindowPlace(); a.winPlace || len(opts) != 0 {
		t.Errorf("no displays, nothing saved: %d options (placed %v)", len(opts), a.winPlace)
	}
	a.prefs.MainWindow = &mainWindowSettings{Size: &winSize{1000, 700}}
	if c = wbOptions(a.mainWindowPlace()); c.Size != image.Pt(1000, 700) || !c.Bounds().Empty() {
		t.Errorf("no displays: size %v bounds %v, want the saved 1000x700 dp only", c.Size, c.Bounds())
	}
	a.prefs.MainWindow = &mainWindowSettings{Size: &winSize{300, 200}}
	if c = wbOptions(a.mainWindowPlace()); c.Size != image.Pt(mainMinW, mainMinH) {
		t.Errorf("no displays, a size under the minimum: %v, want %dx%d", c.Size, mainMinW, mainMinH)
	}
}

// The settings a ConfigEvent brings are written to ui.json (soon, or at
// the window's DestroyEvent), and the next launch opens there: written and
// read back. A minimized or hidden window changes nothing.
func TestWindowPlaceRoundTrip(t *testing.T) {
	t.Cleanup(func() { displaysHook = nil })
	displaysHook = func() []app.Display { return []app.Display{wbPrimary, wbRight} }
	dir := t.TempDir()
	a := New(Options{ConfigDir: dir, NoRealtime: true}, nil)
	a.mainWindowPlace()

	frame := wbRect(2200, 150, 1300, 900)
	a.noteWindowAt(app.Windowed, frame, wbRight, image.Pt(1284, 892))
	if !a.winSaveDue || a.prefs.MainWindow == nil || a.prefs.MainWindow.Bounds.rect() != frame {
		t.Fatalf("after a move: %+v (save due %v)", a.prefs.MainWindow, a.winSaveDue)
	}
	path := filepath.Join(dir, "ui.json")
	deadline := time.Now().Add(10 * time.Second)
	for {
		a.drain()
		if b, err := os.ReadFile(path); err == nil && strings.Contains(string(b), `"main_window_settings"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("ui.json never got main_window_settings")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if a.winSaveDue {
		t.Error("written, still due")
	}
	b, _ := os.ReadFile(path)
	var file struct {
		W map[string]any `json:"main_window_settings"`
	}
	if err := json.Unmarshal(b, &file); err != nil {
		t.Fatal(err)
	}
	if bo, _ := file.W["bounds"].(map[string]any); bo["x"] != 2200.0 || bo["y"] != 150.0 || bo["width"] != 1300.0 || bo["height"] != 900.0 {
		t.Errorf("ui.json main_window_settings: %v, want Slack's {x, y, width, height}", file.W)
	}

	// maximized there, then minimized: the minimized window has no bounds
	// and the maximized state stays
	a.noteWindowAt(app.Maximized, wbRect(1912, -8, 2576, 1416), wbRight, image.Pt(2560, 1400))
	a.noteWindowAt(app.Windowed, image.Rectangle{}, app.Display{}, image.Pt(2560, 1400))
	if s := a.prefs.MainWindow; s == nil || !s.Maximized || s.Bounds.rect() != wbRect(2600, 280, 1200, 840) {
		t.Fatalf("maximized, then minimized: %+v", s)
	}
	a.savePrefs() // what DestroyEvent does when a save is due
	a.flushPrefs()

	a2 := New(Options{ConfigDir: dir, NoRealtime: true}, nil)
	if !a2.prefs.MainWindow.same(a.prefs.MainWindow) {
		t.Fatalf("read back %+v, want %+v", a2.prefs.MainWindow, a.prefs.MainWindow)
	}
	if c := wbOptions(a2.mainWindowPlace()); c.Bounds() != wbRect(2600, 280, 1200, 840) || c.Mode != app.Maximized {
		t.Errorf("the next launch: bounds %v mode %v, want maximized from (2600,280) 1200x840", c.Bounds(), c.Mode)
	}
	// the right display gone: maximized on the primary instead
	displaysHook = func() []app.Display { return []app.Display{wbPrimary} }
	if c := wbOptions(a2.mainWindowPlace()); !wbPrimary.WorkArea.Intersect(c.Bounds()).Eq(c.Bounds()) || c.Mode != app.Maximized {
		t.Errorf("launched without that display: bounds %v mode %v, want inside the primary's work area, maximized", c.Bounds(), c.Mode)
	}
}

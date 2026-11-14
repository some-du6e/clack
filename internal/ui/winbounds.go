package ui

import (
	"image"
	"log"
	"math"
	"runtime"

	"gioui.org/app"
	"gioui.org/unit"
)

// The main window opens where it was (gap 9, W40). Slack keeps
// settings.mainWindowSettings: the window's bounds, maximized and
// fullScreen (and on macOS restorableState, the window's Space, through
// its own native module; Clack has no equivalent). MAIN_WINDOW_ACTIONS
// (win32 main.bundle.cjs 4.53.276 @1146194, mac @1147790) saves them a
// second after the window is maximized, unmaximized, restored, resized,
// moved, shown, focused or blurred, with a maximized window's bounds
// replaced by the default bounds on its display. At launch the window is
// created at those bounds (@1143722: {...bounds, fullscreen}) and, once
// it exists, validateBoundsForDisplay (@1146420; mac `Ko` @788450) keeps
// them on a display that is still connected, the window's minimum size
// applies, and INITIALIZE_MAIN_WINDOW (gC @1151072) maximizes it or makes
// it full screen before showing it. Without saved bounds the window gets
// the default bounds on the primary display.
//
// Gio patch 25 (app.Bounds, app.Displays, Config.Bounds and
// Config.Display) does the placing on Windows and macOS, in their screen
// coordinates: physical pixels on Windows, points on macOS. Slack's
// numbers are DIPs; the DIP constants here (1200×800, the minimum) are
// scaled by the display's app.Display.Scale, and the rest of Slack's
// geometry is the same in either unit. Linux has neither (Gio can't
// place a window or list the displays there, and doesn't see a window
// manager's maximize on X11), so Clack keeps the window's size in dp
// there and nothing else.

// winRect is a window frame as Slack keeps one, {x, y, width, height},
// in app.Bounds' screen coordinates.
type winRect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

func winRectOf(r image.Rectangle) *winRect {
	return &winRect{X: r.Min.X, Y: r.Min.Y, Width: r.Dx(), Height: r.Dy()}
}

func (r winRect) rect() image.Rectangle {
	return image.Rect(r.X, r.Y, r.X+r.Width, r.Y+r.Height)
}

// winSize is the window's size in dp, kept where it can't be placed
// (Linux).
type winSize struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// mainWindowSettings is Slack's mainWindowSettings in ui.json.
type mainWindowSettings struct {
	Bounds     *winRect `json:"bounds,omitempty"`
	Size       *winSize `json:"size,omitempty"` // Linux
	Maximized  bool     `json:"maximized,omitempty"`
	FullScreen bool     `json:"full_screen,omitempty"`
}

func (s *mainWindowSettings) same(o *mainWindowSettings) bool {
	if s == nil || o == nil {
		return s == o
	}
	return s.Maximized == o.Maximized && s.FullScreen == o.FullScreen &&
		sameWinPtr(s.Bounds, o.Bounds) && sameWinPtr(s.Size, o.Size)
}

func sameWinPtr[T comparable](a, b *T) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

// displaysHook replaces app.Displays in tests.
var displaysHook func() []app.Display

func listDisplays() []app.Display {
	if displaysHook != nil {
		return displaysHook()
	}
	return app.Displays()
}

// displayNearest: a window on no display goes to the nearest one, as
// Windows' MonitorFromRect(MONITOR_DEFAULTTONEAREST) picks it for
// Chromium's ScreenWin::GetDisplayMatching; macOS's ScreenMac takes
// NSScreen.mainScreen, which at launch (no key window yet) is the primary
// screen.
var displayNearest = runtime.GOOS != "darwin"

// jsRect is a rectangle in floating point, as Slack's JS computes them.
type jsRect struct{ x, y, w, h float64 }

func jsRectOf(r image.Rectangle) jsRect {
	return jsRect{float64(r.Min.X), float64(r.Min.Y), float64(r.Dx()), float64(r.Dy())}
}

// jsRound is Math.round: halves go up (-2.5 is -2).
func jsRound(v float64) int { return int(math.Floor(v + .5)) }

// scaleAbout is Slack's rW (@806363): r scaled by (sw, sh) about its
// centre.
func scaleAbout(r jsRect, sw, sh float64) jsRect {
	w, h := r.w*sw, r.h*sh
	return jsRect{x: r.x - (w-r.w)/2, y: r.y - (h-r.h)/2, w: w, h: h}
}

// fitInside is rH (@806501): a w×h size shrunk into area keeping its
// aspect ratio (the height first, then the width), rounded.
func fitInside(w, h float64, area jsRect) (int, int) {
	r := w / h
	if h > area.h {
		w, h = area.h*r, area.h
	}
	if w > area.w {
		w, h = area.w, area.w/r
	}
	return jsRound(w), jsRound(h)
}

// containsRect is rB (@806717): a lies within b, edges included.
func containsRect(a, b jsRect) bool {
	return a.x >= b.x && a.x+a.w <= b.x+b.w && a.y >= b.y && a.y+a.h <= b.y+b.h
}

// centerIn is rG (@806848): a w×h frame centred in d's work area, but not
// above or left of it.
func centerIn(w, h int, d app.Display) (int, int) {
	wa := d.WorkArea
	cx, cy := float64(wa.Min.X)+float64(wa.Dx())/2, float64(wa.Min.Y)+float64(wa.Dy())/2
	x, y := jsRound(cx-float64(w)/2), jsRound(cy-float64(h)/2)
	return max(x, wa.Min.X), max(y, wa.Min.Y)
}

// displayScale is d's screen units per DIP (1 when the platform didn't
// say).
func displayScale(d app.Display) float64 {
	if d.Scale > 0 {
		return float64(d.Scale)
	}
	return 1
}

// defaultWindowBounds is rV (@807089): 40% of the work area's width and
// 60% of its height, at least 1200×800 DIPs, fitted into the work area
// and centred in it. Slack opens a window it has no bounds for there, on
// the primary display, and saves a maximized window's bounds as this on
// its display.
func defaultWindowBounds(d app.Display) image.Rectangle {
	wa := jsRectOf(d.WorkArea)
	t := scaleAbout(wa, .4, .6)
	s := displayScale(d)
	w, h := fitInside(math.Max(1200*s, t.w), math.Max(800*s, t.h), wa)
	x, y := centerIn(w, h, d)
	return image.Rect(x, y, x+w, y+h)
}

// primaryDisplay is getPrimaryDisplay: the primary, or the first.
func primaryDisplay(ds []app.Display) app.Display {
	for _, d := range ds {
		if d.Primary {
			return d
		}
	}
	return ds[0]
}

// displayMatching is screen.getDisplayMatching(r): the display with the
// biggest part of r (Chromium's FindDisplayWithBiggestIntersection over
// the displays' bounds); with none, the nearest (Windows) or the primary
// (macOS).
func displayMatching(r image.Rectangle, ds []app.Display) app.Display {
	best, area := -1, 0
	for i, d := range ds {
		in := d.Bounds.Intersect(r)
		if a := in.Dx() * in.Dy(); a > area {
			best, area = i, a
		}
	}
	if best >= 0 {
		return ds[best]
	}
	if !displayNearest {
		return primaryDisplay(ds)
	}
	best, dist := 0, math.MaxInt
	for i, d := range ds {
		dx := max(0, d.Bounds.Min.X-r.Max.X, r.Min.X-d.Bounds.Max.X)
		dy := max(0, d.Bounds.Min.Y-r.Max.Y, r.Min.Y-d.Bounds.Max.Y)
		if dx+dy < dist {
			best, dist = i, dx+dy
		}
	}
	return ds[best]
}

// validateWindowBounds is validateBoundsForDisplay (@1146420): bounds
// within some display's work area grown by 20% (10% past each edge) stay
// as they are; others move onto the work area of the display that
// matches them best, right and bottom edges first, and shrink into it
// keeping their aspect ratio.
func validateWindowBounds(b image.Rectangle, ds []app.Display) image.Rectangle {
	n := jsRectOf(b)
	for _, d := range ds {
		if containsRect(n, scaleAbout(jsRectOf(d.WorkArea), 1.2, 1.2)) {
			return b
		}
	}
	i := displayMatching(b, ds).WorkArea
	x := max(min(b.Max.X, i.Max.X)-b.Dx(), i.Min.X)
	y := max(min(b.Max.Y, i.Max.Y)-b.Dy(), i.Min.Y)
	w, h := fitInside(float64(b.Dx()), float64(b.Dy()), jsRectOf(i))
	return image.Rect(x, y, x+w, y+h)
}

// openWindowBounds is where the window opens (@1146420's caller): the
// saved bounds, or the default ones on the primary display, validated,
// then at least minW×minH DIPs (getMinimumSize) on the display they land
// on. ds is not empty.
func openWindowBounds(s *mainWindowSettings, ds []app.Display, minW, minH float64) image.Rectangle {
	var b image.Rectangle
	if s != nil && s.Bounds != nil && s.Bounds.Width > 0 && s.Bounds.Height > 0 {
		b = s.Bounds.rect()
	} else {
		b = defaultWindowBounds(primaryDisplay(ds))
	}
	b = validateWindowBounds(b, ds)
	sc := displayScale(displayMatching(b, ds))
	w, h := max(b.Dx(), jsRound(minW*sc)), max(b.Dy(), jsRound(minH*sc))
	return image.Rect(b.Min.X, b.Min.Y, b.Min.X+w, b.Min.Y+h)
}

// windowSettingsOf is what MAIN_WINDOW_ACTIONS saves for a window in
// mode with frame b on display d (a ConfigEvent's Mode, Bounds and
// Display): fullScreen, maximized, and bounds, the default bounds on its
// display while maximized. ok is false while the window is off the screen
// (b is empty: minimized, hidden, not shown yet), when there is nothing
// new to keep.
func windowSettingsOf(mode app.WindowMode, b image.Rectangle, d app.Display) (s mainWindowSettings, ok bool) {
	if b.Empty() {
		return s, false
	}
	s.FullScreen = mode == app.Fullscreen
	s.Maximized = mode == app.Maximized
	if s.Maximized && !d.WorkArea.Empty() {
		b = defaultWindowBounds(d)
	}
	s.Bounds = winRectOf(b)
	return s, true
}

// windowSizeOf is Linux's: the size (px) of a window that is neither
// maximized nor full screen (as far as Gio knows), in dp at pxPerDp.
func windowSizeOf(mode app.WindowMode, size image.Point, pxPerDp float32) (s mainWindowSettings, ok bool) {
	if mode != app.Windowed || pxPerDp <= 0 || size.X <= 0 || size.Y <= 0 {
		return s, false
	}
	s.Size = &winSize{Width: jsRound(float64(float32(size.X) / pxPerDp)), Height: jsRound(float64(float32(size.Y) / pxPerDp))}
	return s, true
}

// mainWindowPlace is the main window's place at launch, as options after
// mainWindowOptions: app.Bounds at openWindowBounds, then full screen or
// maximized as it was saved (Slack makes a saved full-screen window full
// screen, else maximizes a maximized one). Where Gio lists no displays
// (Linux) only a saved size comes back. Sets a.winPlace.
func (a *App) mainWindowPlace() []app.Option {
	s := a.prefs.MainWindow
	ds := listDisplays()
	a.winPlace = len(ds) > 0
	if !a.winPlace {
		if s != nil && s.Size != nil && s.Size.Width > 0 && s.Size.Height > 0 {
			return []app.Option{app.Size(unit.Dp(max(s.Size.Width, mainMinW)), unit.Dp(max(s.Size.Height, mainMinH)))}
		}
		return nil
	}
	b := openWindowBounds(s, ds, mainMinW, mainMinH)
	a.winOpened = b
	opts := []app.Option{app.Bounds(b)}
	mode := "windowed"
	switch {
	case s != nil && s.FullScreen:
		opts, mode = append(opts, app.Fullscreen.Option()), "full screen"
	case s != nil && s.Maximized:
		opts, mode = append(opts, app.Maximized.Option()), "maximized"
	}
	saved := "none saved"
	if s != nil && s.Bounds != nil {
		saved = "saved " + s.Bounds.rect().String()
	}
	log.Printf("ui: the main window opens at %v, %s (%s; %d displays)", b, mode, saved, len(ds))
	return opts
}

// noteWindowPlace is MAIN_WINDOW_ACTIONS for the ConfigEvent c: a change
// of the window's place or mode updates mainWindowSettings, and ui.json
// is written soon after (Slack's settings a second later). Nothing changes
// while the window is off the screen, so one maximized and then minimized
// or closed to the tray reopens maximized. UI goroutine.
func (a *App) noteWindowPlace(c app.Config) {
	a.noteWindowAt(c.Mode, c.Bounds(), c.Display(), c.Size)
}

// noteWindowAt is noteWindowPlace for a window in mode with frame b on
// display d, size px big.
func (a *App) noteWindowAt(mode app.WindowMode, b image.Rectangle, d app.Display, size image.Point) {
	var s mainWindowSettings
	var ok bool
	switch {
	case a.winPlace:
		s, ok = windowSettingsOf(mode, b, d)
	case a.pxPerDp > 0:
		s, ok = windowSizeOf(mode, size, a.pxPerDp/a.zoomFactor())
	}
	if !ok || a.prefs.MainWindow.same(&s) {
		return
	}
	a.prefs.MainWindow = &s
	a.winSaveDue = true
	a.savePrefsSoon()
}

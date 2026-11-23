// SPDX-License-Identifier: Unlicense OR MIT

//go:build darwin && !ios

package app

import (
	"image"
	"slices"
	"testing"
	"time"

	"gioui.org/app/cocoatest"
	"gioui.org/io/system"
)

// Clack patch (CLACK_PATCHES.md §25) on a real macOS desktop
// (CLACK_MACOS_WINDOW_TESTS, ci's macos job; TestMain runs Main).

// macRect is a cocoatest frame in whole points.
func macRect(r cocoatest.Rect) image.Rectangle {
	x, y := int(r.X+.5), int(r.Y+.5)
	return image.Rect(x, y, x+int(r.W+.5), y+int(r.H+.5))
}

// sameScreen: d is the screen p is. Not the whole Display: the runner's
// Dock comes and goes, and the visible frame (the work area) with it
// (705, then 708 points down, between two reads a second apart).
func sameScreen(d, p Display) bool {
	return d.Bounds == p.Bounds && d.Primary == p.Primary && d.Scale == p.Scale && d.WorkArea.In(d.Bounds)
}

// macAfterLaunch waits until NSApplication finished launching: its
// applicationDidFinishLaunching ran (launchOpened) and returned (a
// main-thread round trip after it). A test can start while the launch is
// still going on, and a Displays call then takes the launch's screens, as
// it should (CLACK_PATCHES.md §56): TestDisplaysMacAfterLaunchReadsLive
// did once in 10 processes (flakes-1122c's fl1122c-s-displays
// 45056626963).
func macAfterLaunch(t *testing.T) {
	t.Helper()
	macWaitUntil(t, "the launch's end", func() bool {
		launchMu.Lock()
		defer launchMu.Unlock()
		return launchOpened
	})
	cocoatest.OnMain(func() {})
}

func (h *macTestWindow) config() Config {
	if c := h.cfg.Load(); c != nil {
		return *c
	}
	return Config{}
}

// Displays is NSScreen.screens, the primary first, flipped against the
// primary's height as Electron flips them: each screen's frame and its
// visible frame (the work area), in points.
func TestDisplaysMac(t *testing.T) {
	if !macWindowTestsEnabled() {
		t.Skip("CLACK_MACOS_WINDOW_TESTS not set")
	}
	macAfterLaunch(t) // this tests the reads after it
	// Displays and NSScreen are two reads, and the runner's Dock may move
	// the visible frame between them: compare a read of Displays taken
	// between two NSScreen reads that agree. The steady misses (a work
	// area 706 points down against NSScreen's 705, image-context-menu's
	// push ci 44990293041; 708 against 706, train-169's 45045823814) were
	// the launch's screens, which this first call of the process got
	// (TestDisplaysMacAfterLaunchReadsLive).
	screens := func() []cocoatest.Screen {
		var ss []cocoatest.Screen
		cocoatest.OnMain(func() { ss = cocoatest.Screens() })
		return ss
	}
	var ds []Display
	var ss []cocoatest.Screen
	for try := 0; ; try++ {
		before := screens()
		ds = Displays()
		ss = screens()
		if slices.Equal(before, ss) {
			break
		}
		if try == 50 {
			t.Fatalf("NSScreen never held still across a Displays read (50 tries): %+v, then %+v", before, ss)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Logf("displays %+v; NSScreen %+v", ds, ss)
	if len(ds) == 0 || len(ds) != len(ss) {
		t.Fatalf("%d displays, NSScreen has %d", len(ds), len(ss))
	}
	for i, d := range ds {
		if d.Bounds != macRect(ss[i].Frame) || d.WorkArea != macRect(ss[i].Visible) || d.Scale != 1 || d.Primary != (i == 0) {
			t.Errorf("display %d: %+v, want frame %v visible %v, scale 1, primary %v", i, d, ss[i].Frame, ss[i].Visible, i == 0)
		}
	}
	if p := ds[0].Bounds.Min; p != (image.Point{}) {
		t.Errorf("the primary screen's top left is %v, want the origin", p)
	}
}

// After the launch Displays reads NSScreen afresh, on the main thread,
// whenever it's called: held, the main thread holds the call, which then
// gives the screens as they are. The launch's own screens (CLACK_PATCHES.md
// §56) went to the process's first call however late it came, without the
// main thread: TestDisplaysMac read the runner's Dock as it was at the
// launch (a work area to 708 against NSScreen's 706, train-169's push ci
// 45045823814). Run alone, this is the process's first call.
func TestDisplaysMacAfterLaunchReadsLive(t *testing.T) {
	if !macWindowTestsEnabled() {
		t.Skip("CLACK_MACOS_WINDOW_TESTS not set")
	}
	macAfterLaunch(t)
	held, release := make(chan struct{}), make(chan struct{})
	go cocoatest.OnMain(func() {
		close(held)
		<-release
	})
	<-held
	got := make(chan []Display, 1)
	go func() { got <- Displays() }()
	select {
	case ds := <-got:
		close(release)
		t.Fatalf("Displays answered %+v with the main thread held: not NSScreen now (the launch's screens)", ds)
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	var ds []Display
	select {
	case ds = <-got:
	case <-time.After(10 * time.Second):
		t.Fatal("Displays never answered once the main thread was free")
	}
	var ss []cocoatest.Screen
	cocoatest.OnMain(func() { ss = cocoatest.Screens() })
	if len(ds) == 0 || len(ds) != len(ss) {
		t.Fatalf("%d displays, NSScreen has %d", len(ds), len(ss))
	}
	t.Logf("displays %+v; NSScreen %+v", ds, ss)
}

// Bounds on macOS: the window opens at the frame asked for (points from
// the primary screen's top left), which its ConfigEvents report with the
// screen it is on; a move AppKit makes brings a ConfigEvent; a later
// Bounds moves and sizes it, no smaller than its minimum. Opened with
// Maximized it zooms on that screen and unzooms to the frame; opened full
// screen it goes full screen once it is on the screen (last: the runner
// never takes a window out of full screen again).
func TestBoundsMac(t *testing.T) {
	if !macWindowTestsEnabled() {
		t.Skip("CLACK_MACOS_WINDOW_TESTS not set")
	}
	ds := Displays()
	if len(ds) == 0 {
		t.Fatal("no displays")
	}
	prim := ds[0]
	wa := prim.WorkArea
	r := image.Rect(wa.Min.X+50, wa.Min.Y+40, wa.Min.X+50+640, wa.Min.Y+40+420)
	never := func() bool { return false }

	// 1. opened at r, as its ConfigEvents say
	h := newMacTestWindowWith(t, never, HiddenTitleBar(true), MinSize(400, 300), Bounds(r))
	if s := h.state(); macRect(s.ScreenFrame) != r {
		t.Errorf("opened at %v (%+v), want %v", macRect(s.ScreenFrame), s.ScreenFrame, r)
	}
	macWaitUntil(t, "the ConfigEvent's bounds", func() bool { return h.config().Bounds() == r })
	if d := h.config().Display(); !sameScreen(d, prim) {
		t.Errorf("the window's display %+v, want the primary %+v", d, prim)
	}

	// 2. a move AppKit makes (as a drag does): windowDidMove: brings the new bounds
	moved := r.Add(image.Pt(30, 20))
	cocoatest.OnMain(func() { cocoatest.SetTopLeft(h.view, float64(moved.Min.X), float64(moved.Min.Y)) })
	macWaitUntil(t, "the bounds after a move", func() bool { return h.config().Bounds() == moved })

	// 3. a later Bounds: moved and sized; under the minimum it is the minimum
	r3 := image.Rect(wa.Min.X+80, wa.Min.Y+60, wa.Min.X+80+560, wa.Min.Y+60+380)
	h.w.Option(Bounds(r3))
	macWaitUntil(t, "the frame Bounds gave", func() bool { return macRect(h.state().ScreenFrame) == r3 && h.config().Bounds() == r3 })
	small := image.Rect(r3.Min.X, r3.Min.Y, r3.Min.X+100, r3.Min.Y+80)
	h.w.Option(Bounds(small))
	macWaitUntil(t, "the minimum frame", func() bool {
		f := macRect(h.state().ScreenFrame)
		return f.Min == small.Min && f.Dx() == 400 && f.Dy() == 300
	})
	h.destroy(t)

	// 4. opened maximized: zoomed on the screen, Mode Maximized, and
	// Window > Zoom takes it back to r
	h = newMacTestWindowWith(t, never, HiddenTitleBar(true), MinSize(400, 300), Bounds(r), Maximized.Option())
	macWaitUntil(t, "zoomed", func() bool { return h.state().Zoomed && h.config().Mode == Maximized })
	if b := h.config().Bounds(); !b.In(prim.Bounds) || b.Dx() < r.Dx() || !sameScreen(h.config().Display(), prim) {
		t.Errorf("maximized: bounds %v on %+v, want a frame bigger than %v on the primary", b, h.config().Display(), r)
	}
	cocoatest.OnMain(func() { cocoatest.ZoomWindow(h.view) })
	macWaitUntil(t, "unzoomed to r", func() bool { s := h.state(); return !s.Zoomed && macRect(s.ScreenFrame) == r })
	h.destroy(t)

	// 5. opened full screen
	h = newMacTestWindowWith(t, never, HiddenTitleBar(true), MinSize(400, 300), Bounds(r), Fullscreen.Option())
	deadline := time.Now().Add(30 * time.Second)
	for !(h.state().FullScreen && h.config().Mode == Fullscreen) {
		if time.Now().After(deadline) {
			t.Fatalf("not full screen 30 s after opening: %+v, mode %v", h.state(), h.config().Mode)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Logf("full screen: bounds %v, display %+v", h.config().Bounds(), h.config().Display())
	if b := h.config().Bounds(); !b.In(prim.Bounds) || !sameScreen(h.config().Display(), prim) {
		t.Errorf("full screen: bounds %v on %+v, want on the primary %+v", b, h.config().Display(), prim)
	}
	// the runner may never take the window out of its full-screen Space:
	// a close that doesn't finish is logged, not failed
	h.w.Perform(system.ActionClose)
	select {
	case <-h.done:
	case <-time.After(15 * time.Second):
		t.Logf("the full-screen window didn't close within 15 s: %+v", h.state())
	}
}

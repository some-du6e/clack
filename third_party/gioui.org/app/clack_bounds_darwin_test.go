// SPDX-License-Identifier: Unlicense OR MIT

//go:build darwin && !ios

package app

import (
	"image"
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
	ds := Displays()
	var ss []cocoatest.Screen
	cocoatest.OnMain(func() { ss = cocoatest.Screens() })
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

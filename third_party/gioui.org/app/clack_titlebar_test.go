// SPDX-License-Identifier: Unlicense OR MIT

package app

import (
	"image"
	"testing"

	"gioui.org/io/system"
	"gioui.org/unit"
)

// Clack patch (CLACK_PATCHES.md §22): what each point of a window with a
// hidden title bar is, in WinFrameView::NonClientHitTest's order (ci's
// linux-gio job; the Windows codes on a real window are
// clack_titlebar_windows_test.go's).
func TestCaptionHit(t *testing.T) {
	size := image.Pt(1000, 700)
	const band, corner = 8, 12
	none := system.Action(0)
	cases := []struct {
		name       string
		p          image.Point
		fullscreen bool
		band       int
		act        system.Action
		hasAct     bool
		want       captionPart
	}{
		{"the caption", image.Pt(500, 20), false, band, system.ActionMove, true, captionMove},
		{"a button in the caption", image.Pt(500, 20), false, band, none, false, captionClient},
		{"below the caption", image.Pt(500, 300), false, band, none, false, captionClient},
		{"minimize", image.Pt(880, 20), false, band, system.ActionMinimize, true, captionMinimize},
		{"maximize", image.Pt(930, 20), false, band, system.ActionMaximize, true, captionMaximize},
		{"restore", image.Pt(930, 20), false, band, system.ActionUnmaximize, true, captionMaximize},
		{"close", image.Pt(980, 20), false, band, system.ActionClose, true, captionClose},
		// the caption buttons come before the top resize band
		{"close in the top band", image.Pt(995, 2), false, band, system.ActionClose, true, captionClose},
		{"maximize in the top band", image.Pt(930, 2), false, band, system.ActionMaximize, true, captionMaximize},
		// the band before the caption and the client
		{"the top edge over the caption", image.Pt(500, 2), false, band, system.ActionMove, true, captionTop},
		{"the top edge over a widget", image.Pt(500, 7), false, band, none, false, captionTop},
		{"just below the band", image.Pt(500, 8), false, band, system.ActionMove, true, captionMove},
		{"the top-left corner", image.Pt(11, 3), false, band, system.ActionMove, true, captionTopLeft},
		{"past the top-left corner", image.Pt(12, 3), false, band, system.ActionMove, true, captionTop},
		{"the top-right corner", image.Pt(988, 3), false, band, none, false, captionTopRight},
		// maximized or not resizable: no band (the caller passes 0)
		{"maximized: the top edge is the caption", image.Pt(500, 2), false, 0, system.ActionMove, true, captionMove},
		{"maximized: the top-left corner", image.Pt(3, 3), false, 0, system.ActionMove, true, captionMove},
		// full screen: everything is the program's
		{"full screen: the caption", image.Pt(500, 20), true, band, system.ActionMove, true, captionClient},
		{"full screen: close", image.Pt(980, 20), true, band, system.ActionClose, true, captionClient},
		// an area declared for more than one action: the button wins
		{"maximize or restore", image.Pt(930, 20), false, band, system.ActionMaximize | system.ActionUnmaximize, true, captionMaximize},
	}
	for _, c := range cases {
		if got := captionHit(c.p, size, c.fullscreen, c.band, corner, c.act, c.hasAct); got != c.want {
			t.Errorf("%s at %v: %d, want %d", c.name, c.p, got, c.want)
		}
	}
}

// HiddenTitleBar is a plain option: off unless asked for, and Decorated
// keeps its own value.
func TestHiddenTitleBarOption(t *testing.T) {
	var cnf Config
	cnf.apply(unit.Metric{PxPerDp: 1}, []Option{Decorated(true)})
	if cnf.hiddenTitleBar {
		t.Fatal("on by default")
	}
	cnf.apply(unit.Metric{PxPerDp: 1}, []Option{HiddenTitleBar(true)})
	if !cnf.hiddenTitleBar || !cnf.Decorated {
		t.Fatalf("HiddenTitleBar(true): hidden %v decorated %v", cnf.hiddenTitleBar, cnf.Decorated)
	}
	cnf.apply(unit.Metric{PxPerDp: 1}, []Option{HiddenTitleBar(false)})
	if cnf.hiddenTitleBar {
		t.Fatal("HiddenTitleBar(false) left it on")
	}
	// Clack patch (CLACK_PATCHES.md §23): the buttons' place, unset until
	// asked for, in DIPs whatever the metric
	if cnf.trafficLight.set {
		t.Fatal("TrafficLightPosition set by default")
	}
	cnf.apply(unit.Metric{PxPerDp: 2}, []Option{TrafficLightPosition(12, 12)})
	if want := (trafficLight{set: true, x: 12, y: 12}); cnf.trafficLight != want {
		t.Fatalf("TrafficLightPosition(12, 12): %+v, want %+v", cnf.trafficLight, want)
	}
}

// Clack patch (CLACK_PATCHES.md §23): a double-click on a drag area does
// what Chromium's NativeWidgetMacNSWindow does for its custom draggable
// areas with each AppleActionOnDoubleClick.
func TestTitlebarDoubleClickAction(t *testing.T) {
	for _, c := range []struct {
		pref         string
		set, canFill bool
		want         titlebarDoubleClick
	}{
		{"", false, false, titlebarDoubleClickZoom}, // unset: the default, Zoom
		{"", false, true, titlebarDoubleClickZoom},
		{"Maximize", true, false, titlebarDoubleClickZoom},
		{"Minimize", true, false, titlebarDoubleClickMinimize},
		{"Fill", true, true, titlebarDoubleClickFill},
		{"Fill", true, false, titlebarDoubleClickNone}, // before macOS 15: nothing, as in Chromium
		{"None", true, true, titlebarDoubleClickNone},
		{"", true, false, titlebarDoubleClickNone},
		{"maximize", true, false, titlebarDoubleClickNone}, // the values are case-sensitive
	} {
		if got := titlebarDoubleClickAction(c.pref, c.set, c.canFill); got != c.want {
			t.Errorf("AppleActionOnDoubleClick %q (set %v, Fill %v): %d, want %d", c.pref, c.set, c.canFill, got, c.want)
		}
	}
}

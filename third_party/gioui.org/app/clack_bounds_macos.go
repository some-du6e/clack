// SPDX-License-Identifier: Unlicense OR MIT

//go:build darwin && !ios

package app

/*
#cgo CFLAGS: -Werror -xobjective-c -fobjc-arc

#import <AppKit/AppKit.h>

// Clack patch (CLACK_PATCHES.md §25): window frames and screens in
// Electron's coordinates, points from the top left of the primary screen
// (the menu bar's, NSScreen.screens[0]) with y down: NativeWindowMac's
// GetBounds and SetBounds flip an NSWindow's frame against that screen's
// height, and Chromium's ScreenMac its screens' frames and visible frames
// (the work areas).

typedef struct {
	CGFloat x, y, w, h;
} gio_place_rect;

typedef struct {
	gio_place_rect frame, visible;
	int primary;
} gio_place_screen;

static CGFloat gio_placePrimaryHeight(void) {
	NSScreen *s = [NSScreen screens].firstObject;
	return s != nil ? NSHeight(s.frame) : 0;
}

static gio_place_rect gio_placeFlip(NSRect r, CGFloat h0) {
	gio_place_rect o = {r.origin.x, h0 - NSMaxY(r), r.size.width, r.size.height};
	return o;
}

static void gio_placeScreen(NSScreen *s, CGFloat h0, int primary, gio_place_screen *out) {
	out->frame = gio_placeFlip(s.frame, h0);
	out->visible = gio_placeFlip(s.visibleFrame, h0);
	out->primary = primary;
}

// gio_placeScreens fills out with up to limit screens, the primary
// first, and returns how many.
static int gio_placeScreens(gio_place_screen *out, int limit) {
	@autoreleasepool {
		CGFloat h0 = gio_placePrimaryHeight();
		int n = 0;
		for (NSScreen *s in [NSScreen screens]) {
			if (n == limit) {
				break;
			}
			gio_placeScreen(s, h0, n == 0, &out[n]);
			n++;
		}
		return n;
	}
}

// gio_placeWindow reads where the window is: 0 when it isn't on the
// screen (ordered out or miniaturized), 1 with its frame, 2 with its frame
// and the screen with the biggest part of it (NSWindow.screen).
static int gio_placeWindow(CFTypeRef windowRef, gio_place_rect *frame, gio_place_screen *screen) {
	@autoreleasepool {
		NSWindow *window = (__bridge NSWindow *)windowRef;
		if (!window.visible || window.miniaturized) {
			return 0;
		}
		CGFloat h0 = gio_placePrimaryHeight();
		*frame = gio_placeFlip(window.frame, h0);
		NSScreen *s = window.screen;
		if (s == nil) {
			return 1;
		}
		NSScreen *primary = [NSScreen screens].firstObject;
		BOOL isPrimary = primary != nil && [s.deviceDescription[@"NSScreenNumber"] isEqual:primary.deviceDescription[@"NSScreenNumber"]];
		gio_placeScreen(s, h0, isPrimary ? 1 : 0, screen);
		return 2;
	}
}

// gio_placeSetFrame is NativeWindowMac::SetBounds: the size raised to the
// window's minimum (setFrame:display: doesn't check it), the frame flipped
// against the primary screen.
static void gio_placeSetFrame(CFTypeRef windowRef, CGFloat x, CGFloat y, CGFloat w, CGFloat h) {
	@autoreleasepool {
		NSWindow *window = (__bridge NSWindow *)windowRef;
		NSSize cmin = window.contentMinSize;
		NSSize least = [window frameRectForContentRect:NSMakeRect(0, 0, cmin.width, cmin.height)].size;
		w = MAX(w, least.width);
		h = MAX(h, least.height);
		[window setFrame:NSMakeRect(x, gio_placePrimaryHeight() - y - h, w, h) display:YES];
	}
}
*/
import "C"

import (
	"image"
	"math"
)

// displays is Displays on macOS: NSScreen.screens, read on the main
// thread.
func displays() []Display {
	var ds []Display
	done := make(chan struct{})
	runOnMain(func() {
		defer close(done)
		var buf [32]C.gio_place_screen
		n := int(C.gio_placeScreens(&buf[0], C.int(len(buf))))
		for i := 0; i < n; i++ {
			ds = append(ds, placeDisplay(buf[i]))
		}
	})
	<-done
	return ds
}

func placeDisplay(s C.gio_place_screen) Display {
	return Display{Bounds: placeRect(s.frame), WorkArea: placeRect(s.visible), Scale: 1, Primary: s.primary != 0}
}

// placeRect is r in whole points (AppKit's frames are whole points but
// for a window centred on an odd size).
func placeRect(r C.gio_place_rect) image.Rectangle {
	round := func(v C.CGFloat) int { return int(math.Floor(float64(v) + .5)) }
	x, y := round(r.x), round(r.y)
	return image.Rect(x, y, x+round(r.w), y+round(r.h))
}

// windowPlacement is Config.Bounds and Config.Display for window: its
// frame and its screen while it is on screen, nothing otherwise. Main
// thread.
func windowPlacement(window C.CFTypeRef) (image.Rectangle, Display) {
	if window == 0 {
		return image.Rectangle{}, Display{}
	}
	var f C.gio_place_rect
	var s C.gio_place_screen
	switch C.gio_placeWindow(window, &f, &s) {
	case 1:
		return placeRect(f), Display{}
	case 2:
		return placeRect(f), placeDisplay(s)
	}
	return image.Rectangle{}, Display{}
}

// setWindowBounds moves window's frame to r (Bounds). Main thread.
func setWindowBounds(window C.CFTypeRef, r image.Rectangle) {
	C.gio_placeSetFrame(window, C.CGFloat(r.Min.X), C.CGFloat(r.Min.Y), C.CGFloat(r.Dx()), C.CGFloat(r.Dy()))
}

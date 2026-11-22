// SPDX-License-Identifier: Unlicense OR MIT

//go:build darwin && !ios

package app

/*
#include <AppKit/AppKit.h>

static double gio_doubleClickInterval(void) {
	return [NSEvent doubleClickInterval];
}
*/
import "C"

import (
	"image"
	"time"

	"gioui.org/internal/clickrule"
)

// Clack patch 48 (CLACK_PATCHES.md §48): on macOS Chromium takes a click's
// count from AppKit (components/input/web_input_event_builders_mac.mm:
// [event clickCount]), and so does gesture.Click (pointer.Event.Clicks,
// gio_onMouse). A press without AppKit's count (a test's) counts on within
// NSEvent.doubleClickInterval, the System Settings double-click speed,
// read on every press, and 2 px of the last press.
func init() {
	clickrule.Base = clickrule.Rule{}
	clickrule.Read = macDoubleClick
}

func macDoubleClick() (time.Duration, image.Point) {
	return time.Duration(float64(C.gio_doubleClickInterval()) * float64(time.Second)), image.Pt(2, 2)
}

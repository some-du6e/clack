// SPDX-License-Identifier: Unlicense OR MIT

//go:build windows

package app

import (
	"image"
	"time"

	"gioui.org/app/internal/windows"
	"gioui.org/internal/clickrule"
)

// Clack patch 48 (CLACK_PATCHES.md §48): Windows counts clicks as
// Chromium's WebMouseEventBuilder::Build does
// (ui/events/blink/web_input_event_builders_win.cc), which Chrome's pages
// get their click counts from there: a press no later than
// GetDoubleClickTime() after the last one, no further than half of
// SM_CXDOUBLECLK and SM_CYDOUBLECLK from the run's first press, counts on,
// and a move out of that box ends the run. Chromium asks for both on
// every event, so they're read on every press: a change in the Mouse
// settings applies to the next click.
func init() {
	clickrule.Base = clickrule.Rule{FromFirst: true, MoveResets: true}
	clickrule.Read = windowsDoubleClick
}

func windowsDoubleClick() (time.Duration, image.Point) {
	return time.Duration(windows.GetDoubleClickTime()) * time.Millisecond,
		image.Pt(windows.GetSystemMetrics(windows.SM_CXDOUBLECLK)/2, windows.GetSystemMetrics(windows.SM_CYDOUBLECLK)/2)
}

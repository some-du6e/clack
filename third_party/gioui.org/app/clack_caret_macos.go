// SPDX-License-Identifier: Unlicense OR MIT

//go:build darwin && !ios

package app

/*
#cgo LDFLAGS: -framework Accessibility

#include <stdbool.h>
#include <stdint.h>

__attribute__ ((visibility ("hidden"))) int64_t gio_caretBlinkPeriodMs(void);
__attribute__ ((visibility ("hidden"))) void gio_observeCaretBlink(void);
__attribute__ ((visibility ("hidden"))) void gio_clackCaretDefaults(bool setOn, int64_t on, bool setOff, int64_t off, bool post);
*/
import "C"

import (
	"time"

	"gioui.org/widget"
)

// caretObserved: the period is read again on its notifications (main
// thread only).
var caretObserved bool

// setCaretBlinkFromOS sets widget.CaretBlinkInterval to macOS's caret
// blink period as Chromium reads it (Clack patch, CLACK_PATCHES.md §47:
// gio_caretBlinkPeriodMs), as a window opens and on the notifications
// Chromium reads it again on. Main thread.
func setCaretBlinkFromOS() {
	switch ms := int64(C.gio_caretBlinkPeriodMs()); {
	case ms < 0:
		widget.SetCaretBlinkInterval(-1) // Chromium's 500 ms
	case ms == 0:
		widget.SetCaretBlinkInterval(0) // no blinking
	default:
		widget.SetCaretBlinkInterval(time.Duration(ms) * time.Millisecond)
	}
	if !caretObserved {
		caretObserved = true
		C.gio_observeCaretBlink()
	}
}

//export gio_onCaretBlinkChanged
func gio_onCaretBlinkChanged() {
	setCaretBlinkFromOS()
}

// clackCaretDefaults sets NSTextInsertionPointBlinkPeriodOn and ...Off in
// the process's own user defaults (nil: removes it), then, if post, posts
// NSApplicationWillBecomeActiveNotification as AppKit does when the app
// comes to the front (tests: TestCaretBlinkFromOSMac). Main thread.
func clackCaretDefaults(on, off *int64, post bool) {
	var vOn, vOff int64
	if on != nil {
		vOn = *on
	}
	if off != nil {
		vOff = *off
	}
	C.gio_clackCaretDefaults(C.bool(on != nil), C.int64_t(vOn), C.bool(off != nil), C.int64_t(vOff), C.bool(post))
}

// clackCaretPeriodMs is gio_caretBlinkPeriodMs: the period in ms, 0 for no
// blinking, -1 for Chromium's default (tests).
func clackCaretPeriodMs() int64 { return int64(C.gio_caretBlinkPeriodMs()) }

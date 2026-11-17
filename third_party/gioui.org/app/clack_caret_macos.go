// SPDX-License-Identifier: Unlicense OR MIT

//go:build darwin && !ios

package app

/*
#cgo LDFLAGS: -framework Accessibility

#include <stdint.h>

__attribute__ ((visibility ("hidden"))) int64_t gio_caretBlinkPeriodMs(void);
__attribute__ ((visibility ("hidden"))) void gio_observeCaretBlink(void);
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
// blink period as Chromium reads it (Clack patch, CLACK_PATCHES.md §46:
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

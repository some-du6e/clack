//go:build darwin && cgo

package ui

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit
#include <stdint.h>
void clack_window_install(void);
void clack_window_set_main(uintptr_t view);
*/
import "C"

import (
	"sync/atomic"

	"gioui.org/app"
	"gioui.org/io/event"
)

// The main window after a close hid it (W35, runfromtray.go): the Dock
// icon's click and the app coming to the front show it again, as Slack's
// MAIN_WINDOW_ACTIONS epic does (window_darwin.m).

// windowApp is the App whose window the glue shows (window_darwin.m asks
// it whether Clack is quitting).
var windowApp atomic.Pointer[App]

func init() {
	startMacWindow = func(a *App) {
		windowApp.Store(a)
		C.clack_window_install()
	}
	macViewEvent = func(ev event.Event) {
		if e, ok := ev.(app.AppKitViewEvent); ok && e.View != 0 {
			C.clack_window_set_main(C.uintptr_t(e.View))
		}
	}
}

// clackWindowQuitting is Slack's app.isQuitting for window_darwin.m's
// handlers (Quit Clack, File > Quit, an update's restart). Main thread:
// it only reads.
//
//export clackWindowQuitting
func clackWindowQuitting() C.int {
	if a := windowApp.Load(); a != nil && a.quitting.Load() {
		return 1
	}
	return 0
}

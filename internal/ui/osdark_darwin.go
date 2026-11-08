//go:build darwin && cgo

package ui

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit
void clack_osdark_install(void);
void clack_osdark_refresh(void);
int clack_osdark_prefs(void);
*/
import "C"

import (
	"sync/atomic"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/uiguard"
)

// macOS: osdark_darwin.m observes NSApp's effectiveAppearance (KVO) and
// the AppleInterfaceThemeChangedNotification distributed notification on
// the main thread and hands the value over with clackOSAppearance, which
// only stores an atomic. Nothing here waits for the main thread: requests
// go to it with dispatch_async.

// osAppKitSeen: AppKit reported at least once, so the CFPreferences
// fallback stays quiet.
var osAppKitSeen atomic.Bool

//export clackOSAppearance
func clackOSAppearance(dark C.int) {
	osAppKitSeen.Store(true)
	setOSScheme(dark != 0)
}

// osPrefersDark reads the global AppleInterfaceStyle in-process with
// CFPreferences ("Dark" in dark mode, absent in light mode), no exec. It
// may wait on cfprefsd, so only the watcher goroutine calls it.
func osPrefersDark() (dark, ok bool) {
	uiguard.OffUI("osPrefersDark")
	return C.clack_osdark_prefs() != 0, true
}

func startOSSchemeWatcher() {
	C.clack_osdark_install()
	go func() {
		// Give AppKit a moment to answer before the fallback guesses (an
		// "Auto" appearance can differ from the stored key).
		time.Sleep(time.Second)
		for {
			if !osAppKitSeen.Load() {
				if dark, ok := osPrefersDark(); ok {
					setOSScheme(dark)
				}
			}
			C.clack_osdark_refresh() // also catches a missed notification
			time.Sleep(osSchemePoll)
		}
	}()
}

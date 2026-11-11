//go:build darwin && cgo

package ui

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit
void clack_osmotion_install(void);
*/
import "C"

// macOS: osmotion_darwin.m reads NSWorkspace's
// accessibilityDisplayShouldReduceMotion on the main thread and again on
// NSWorkspaceAccessibilityDisplayOptionsDidChangeNotification, and hands
// it over with clackOSReduceMotion (an atomic store). Nothing waits for
// the main thread.

//export clackOSReduceMotion
func clackOSReduceMotion(reduce C.int) {
	setOSReduceMotion(reduce != 0)
}

func startOSMotionWatcher() { C.clack_osmotion_install() }

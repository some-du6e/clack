// SPDX-License-Identifier: Unlicense OR MIT

//go:build darwin && !ios

package app

/*
__attribute__ ((visibility ("hidden"))) void gio_replyToTerminate(int quit);
*/
import "C"

import "time"

// terminateWait is how long AppKit waits on a program that took a
// TerminateEvent and neither ended nor answered (one ranging over Events
// that doesn't handle it): then the process is terminated, as before.
const terminateWait = 15 * time.Second

// terminateTimer ends that wait. Main thread.
var terminateTimer *time.Timer

// gio_onShouldTerminate is the app delegate's applicationShouldTerminate:
// (Clack patch, CLACK_PATCHES.md §58), on the main thread: a program
// ranging over Events gets a TerminateEvent and AppKit waits for it
// (NSTerminateLater, 1); without one, AppKit terminates now (0).
//
//export gio_onShouldTerminate
func gio_onShouldTerminate() C.int {
	if yieldGlobalEvent == nil {
		return 0
	}
	processGlobalEvent(TerminateEvent{})
	if terminateTimer != nil {
		terminateTimer.Stop()
	}
	terminateTimer = time.AfterFunc(terminateWait, func() { runOnMain(func() { replyToTerminate(true) }) })
	return 1
}

// replyToTerminate answers the terminate: AppKit is waiting on
// (replyToApplicationShouldTerminate:): quit or carry on. Main thread.
func replyToTerminate(quit bool) {
	if terminateTimer != nil {
		terminateTimer.Stop()
		terminateTimer = nil
	}
	q := C.int(0)
	if quit {
		q = 1
	}
	C.gio_replyToTerminate(q)
}

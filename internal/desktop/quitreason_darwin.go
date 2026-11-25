//go:build darwin && cgo

package desktop

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation -framework CoreServices
#import <Foundation/Foundation.h>
#import <CoreServices/CoreServices.h>

// clackQuitEndsSession reads the quit Apple Event AppKit is handling (its
// kAEQuitReason, which loginwindow sets when it logs out, restarts or
// shuts down; the Dock's Quit and a scripted quit carry none). Nothing is
// made, so nothing is released (the package's Objective-C is MRC).
static int clackQuitEndsSession(void) {
	NSAppleEventDescriptor *e = [[NSAppleEventManager sharedAppleEventManager] currentAppleEvent];
	if (e == nil || [e eventClass] != kCoreEventClass || [e eventID] != kAEQuitApplication) {
		return 0;
	}
	NSAppleEventDescriptor *why = [e attributeDescriptorForKeyword:kAEQuitReason];
	if (why == nil) {
		return 0;
	}
	OSType r = [why enumCodeValue];
	if (r == 0) {
		r = [why typeCodeValue];
	}
	switch (r) {
	case kAELogOut:
	case kAEReallyLogOut:
	case kAEShowRestartDialog:
	case kAERestart:
	case kAEShowShutdownDialog:
	case kAEShutDown:
		return 1;
	}
	return 0;
}
*/
import "C"

// QuitEndsSession reports whether the quit AppKit is asking about is the
// session's end (log out, restart or shut down): the quit Apple Event's
// kAEQuitReason. Call it on the main thread while AppKit asks, in the
// app's TerminateEvent (applicationShouldTerminate:).
func QuitEndsSession() bool { return C.clackQuitEndsSession() != 0 }

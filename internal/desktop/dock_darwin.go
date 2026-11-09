//go:build darwin && cgo

package desktop

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>
#include <stdlib.h>

static void clack_dock_badge(const char *s) {
	NSString *label = [[NSString alloc] initWithUTF8String:s]; // released on the main queue
	dispatch_async(dispatch_get_main_queue(), ^{
		[[NSApp dockTile] setBadgeLabel:(label.length ? label : nil)];
		[label release];
	});
}

static void clack_dock_bounce(int critical) {
	dispatch_async(dispatch_get_main_queue(), ^{
		[NSApp requestUserAttention:(critical ? NSCriticalRequest : NSInformationalRequest)];
	});
}
*/
import "C"

import "unsafe"

// SetDockBadge sets the Dock icon's badge ("" clears it), like the Slack
// desktop app's app.dock.setBadge. Any goroutine; returns at once.
func SetDockBadge(label string) {
	cs := C.CString(label)
	defer C.free(unsafe.Pointer(cs))
	C.clack_dock_badge(cs)
}

// BounceDock bounces the Dock icon (Slack's app.dock.bounce): once
// (NSInformationalRequest) or, critical, until Clack is brought forward.
// AppKit ignores it while Clack is the active app.
func BounceDock(critical bool) {
	c := C.int(0)
	if critical {
		c = 1
	}
	C.clack_dock_bounce(c)
}

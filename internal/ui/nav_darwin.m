//go:build darwin && cgo

#import <AppKit/AppKit.h>
#include "_cgo_export.h"

static id clackNavMonitor;

// clack_nav_install adds the NSEvent monitor nav_darwin.go describes.
void clack_nav_install(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		if (clackNavMonitor != nil) {
			return;
		}
		NSEventMask mask = NSEventMaskScrollWheel | NSEventMaskOtherMouseDown;
		clackNavMonitor = [NSEvent addLocalMonitorForEventsMatchingMask:mask handler:^NSEvent *(NSEvent *e) {
			if (e.type == NSEventTypeScrollWheel) {
				// trackpad gestures only: no wheel, no momentum
				if (e.hasPreciseScrollingDeltas && e.phase != NSEventPhaseNone && e.momentumPhase == NSEventPhaseNone) {
					clackNavScroll((int)e.phase, e.scrollingDeltaX, e.scrollingDeltaY, e.timestamp,
						e.directionInvertedFromDevice ? 1 : 0,
						[NSEvent isSwipeTrackingFromScrollEventsEnabled] ? 1 : 0);
				}
			} else if (e.type == NSEventTypeOtherMouseDown) {
				if (e.buttonNumber == 3) {
					clackNavButton(-1);
				} else if (e.buttonNumber == 4) {
					clackNavButton(1);
				}
			}
			return e;
		}];
	});
}

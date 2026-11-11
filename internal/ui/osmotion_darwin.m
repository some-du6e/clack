//go:build darwin && cgo

#import <AppKit/AppKit.h>
#include "_cgo_export.h"

// Reduce motion for osmotion_darwin.go. AppKit is touched only on the main
// thread (dispatch_async blocks and the notification's callback there);
// the value goes to Go with clackOSReduceMotion, an atomic store. Nothing
// waits on the main thread.

static void clack_osmotion_report(void) {
	BOOL reduce = NO;
	if (@available(macOS 10.12, *)) {
		reduce = [[NSWorkspace sharedWorkspace] accessibilityDisplayShouldReduceMotion];
	}
	clackOSReduceMotion(reduce ? 1 : 0);
}

@interface ClackMotionObserver : NSObject
@end

@implementation ClackMotionObserver
- (void)displayOptionsChanged:(NSNotification *)n {
	clack_osmotion_report();
}
@end

static ClackMotionObserver *clackMotionObs;

void clack_osmotion_install(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		if (clackMotionObs != nil) {
			return;
		}
		clackMotionObs = [ClackMotionObserver new];
		[[[NSWorkspace sharedWorkspace] notificationCenter] addObserver:clackMotionObs
			selector:@selector(displayOptionsChanged:)
			name:NSWorkspaceAccessibilityDisplayOptionsDidChangeNotification
			object:nil];
		clack_osmotion_report();
	});
}

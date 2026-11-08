//go:build darwin && cgo

#import <AppKit/AppKit.h>
#include "_cgo_export.h"

// The OS appearance for osdark_darwin.go. Everything that touches AppKit
// runs on the main thread from dispatch_async blocks or AppKit's own
// callbacks there; the result goes to Go with clackOSAppearance (an atomic
// store). Nothing waits on the main thread (no dispatch_sync): Gio lays
// out frames synchronously on it, so a wait there could deadlock.

static void clack_osdark_report(void) {
	NSApplication *app = NSApp;
	if (app == nil) {
		return;
	}
	int dark = 0;
	if (@available(macOS 10.14, *)) {
		NSAppearanceName n = [app.effectiveAppearance bestMatchFromAppearancesWithNames:@[NSAppearanceNameAqua, NSAppearanceNameDarkAqua]];
		dark = [n isEqualToString:NSAppearanceNameDarkAqua] ? 1 : 0;
	}
	clackOSAppearance(dark);
}

static void clack_osdark_report_soon(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		clack_osdark_report();
	});
}

@interface ClackAppearanceObserver : NSObject
@end

@implementation ClackAppearanceObserver
- (void)observeValueForKeyPath:(NSString *)keyPath ofObject:(id)object change:(NSDictionary *)change context:(void *)context {
	clack_osdark_report_soon();
}
- (void)themeChanged:(NSNotification *)n {
	// effectiveAppearance can lag the notification: read now and again
	// a moment later (the KVO callback covers it too).
	clack_osdark_report_soon();
	dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(300 * NSEC_PER_MSEC)), dispatch_get_main_queue(), ^{
		clack_osdark_report();
	});
}
@end

static ClackAppearanceObserver *clackAppearanceObs;

static void clack_osdark_install_main(void) {
	if (clackAppearanceObs != nil) {
		return;
	}
	if (NSApp == nil) { // before Gio made the application: try again
		dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(500 * NSEC_PER_MSEC)), dispatch_get_main_queue(), ^{
			clack_osdark_install_main();
		});
		return;
	}
	clackAppearanceObs = [ClackAppearanceObserver new];
	if (@available(macOS 10.14, *)) {
		[NSApp addObserver:clackAppearanceObs forKeyPath:@"effectiveAppearance" options:0 context:NULL];
	}
	[[NSDistributedNotificationCenter defaultCenter] addObserver:clackAppearanceObs
		selector:@selector(themeChanged:)
		name:@"AppleInterfaceThemeChangedNotification"
		object:nil];
	clack_osdark_report();
}

void clack_osdark_install(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		clack_osdark_install_main();
	});
}

void clack_osdark_refresh(void) {
	clack_osdark_report_soon();
}

// clack_osdark_prefs reads AppleInterfaceStyle from the global domain in
// process (CFPreferences is thread-safe; no exec): 1 dark, 0 light.
int clack_osdark_prefs(void) {
	CFPreferencesAppSynchronize(kCFPreferencesAnyApplication);
	CFPropertyListRef v = CFPreferencesCopyAppValue(CFSTR("AppleInterfaceStyle"), kCFPreferencesAnyApplication);
	int dark = 0;
	if (v != NULL) {
		if (CFGetTypeID(v) == CFStringGetTypeID() &&
			CFStringCompare((CFStringRef)v, CFSTR("Dark"), kCFCompareCaseInsensitive) == kCFCompareEqualTo) {
			dark = 1;
		}
		CFRelease(v);
	}
	return dark;
}

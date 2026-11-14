//go:build darwin && cgo

#import <AppKit/AppKit.h>
#import <objc/runtime.h>
#include "_cgo_export.h"

// The main window after a close hid it (W35; runfromtray.go, Gio patch 24).
// Slack's MAIN_WINDOW_ACTIONS epic (mac main.bundle.cjs 4.53.276 @1148718)
// listens to Electron's app "activate", which is the app delegate's
// applicationShouldHandleReopen:hasVisibleWindows: (a click on the Dock
// icon), and "did-become-active", which is applicationDidBecomeActive: (the
// app coming to the front: ⌘Tab, the Dock, a click on one of its windows).
// Everything here runs on the main thread and calls into Go only to read
// whether Clack is quitting. ARC.

static __weak NSView *mainView; // the main window's Gio view (AppKitViewEvent)

void clack_window_set_main(uintptr_t view) {
	dispatch_async(dispatch_get_main_queue(), ^{
		mainView = (__bridge NSView *)(void *)view;
	});
}

// focusedWindow is BrowserWindow.getFocusedWindow(): the key window when it
// is one of Clack's (Gio's) windows, not a panel such as the open dialog.
static NSWindow *focusedWindow(void) {
	NSWindow *k = [NSApp keyWindow];
	Class g = NSClassFromString(@"GioView");
	if (k == nil || g == nil || ![k.contentView isKindOfClass:g]) {
		return nil;
	}
	return k;
}

// isShown is BrowserWindow.isVisible(): ordered in and not minimized.
static BOOL isShown(NSWindow *w) { return w.visible && !w.miniaturized; }

// show is BrowserWindow.show() (NativeWindowMac::Show): the app activates
// and the window becomes key, in front.
static void show(NSWindow *w) {
	[NSApp activateIgnoringOtherApps:YES];
	[w makeKeyAndOrderFront:nil];
}

// showInactive is BrowserWindow.showInactive() (ShowInactive): in front,
// not key.
static void showInactive(NSWindow *w) { [w orderFrontRegardless]; }

// focus is BrowserWindow.focus() (NativeWindowMac::Focus).
static void focus(NSWindow *w) {
	[NSApp activateIgnoringOtherApps:NO];
	[w makeKeyAndOrderFront:nil];
}

// reopen is app's "activate" handler (@1149631):
//
//	if (isQuitting) return;
//	let o = getFocusedWindow();
//	o ? (main.isVisible() || main.showInactive(), recentSweep || o.focus())
//	  : hasVisibleWindows && !main.isVisible() ? main.showInactive() : main.show()
//
// recentSweep is a deferred-show window sweep in the last second, which
// Clack has none of. Electron's delegate returns hasVisibleWindows.
static BOOL reopen(id self, SEL cmd, NSApplication *app, BOOL visible) {
	NSWindow *main = mainView.window;
	if (main == nil || clackWindowQuitting()) {
		return visible;
	}
	NSWindow *o = focusedWindow();
	if (o != nil) {
		if (!isShown(main)) {
			showInactive(main);
		}
		focus(o);
	} else if (visible && !isShown(main)) {
		showInactive(main);
	} else {
		show(main);
	}
	return visible;
}

// becameActive is "did-become-active"'s handler: once a window took the
// focus or 50 ms went by, a main window that isn't minimized is shown
// unless one of the app's windows has the focus:
//
//	await Promise.race([once(app, "browser-window-focus"), setTimeout(50)]);
//	isQuitting || getFocusedWindow() || main.isDestroyed() || main.isMinimized() || main.show()
static void becameActive(void) {
	dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 50 * NSEC_PER_MSEC), dispatch_get_main_queue(), ^{
		NSWindow *main = mainView.window;
		if (main == nil || clackWindowQuitting() || focusedWindow() != nil || main.miniaturized) {
			return;
		}
		show(main);
	});
}

void clack_window_install(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		static BOOL installed;
		if (installed) {
			return;
		}
		installed = YES;
		// Gio's app delegate has no reopen handler: add Slack's to its class
		id del = [NSApp delegate];
		SEL r = @selector(applicationShouldHandleReopen:hasVisibleWindows:);
		if (del != nil && ![del respondsToSelector:r]) {
			struct objc_method_description md = protocol_getMethodDescription(@protocol(NSApplicationDelegate), r, NO, YES);
			if (md.types != NULL) {
				class_addMethod([del class], r, (IMP)reopen, md.types);
			}
		}
		[[NSNotificationCenter defaultCenter] addObserverForName:NSApplicationDidBecomeActiveNotification
														  object:nil
														   queue:nil
													  usingBlock:^(NSNotification *n) {
														  becameActive();
													  }];
	});
}

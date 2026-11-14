// SPDX-License-Identifier: Unlicense OR MIT

//go:build darwin && !ios

// Clack patch (CLACK_PATCHES.md §23): AppKit helpers for the macOS window
// tests (cocoatest_darwin.go). ARC.

#import <AppKit/AppKit.h>

#include "_cgo_export.h"

@interface NSView (CocoaTest)
- (NSRect)_opaqueRectForWindowMoveWhenInTitlebar;
@end

static NSWindow *windowOf(uintptr_t view) {
	return ((__bridge NSView *)(void *)view).window;
}

// topLeft is r (window coordinates, origin at the bottom left) with its
// origin at the window's top left.
static cocoatest_rect topLeft(NSWindow *w, NSRect r) {
	cocoatest_rect o = {r.origin.x, NSHeight(w.frame) - NSMaxY(r), r.size.width, r.size.height};
	return o;
}

void cocoatest_state(uintptr_t viewRef, cocoatest_window *s) {
	@autoreleasepool {
		memset(s, 0, sizeof(*s));
		NSView *view = (__bridge NSView *)(void *)viewRef;
		NSWindow *w = view.window;
		if (w == nil) {
			return;
		}
		NSWindowStyleMask m = w.styleMask;
		s->fullSizeContent = (m & NSWindowStyleMaskFullSizeContentView) != 0;
		s->fullScreen = (m & NSWindowStyleMaskFullScreen) != 0;
		s->titlebarTransparent = w.titlebarAppearsTransparent;
		s->titleHidden = w.titleVisibility == NSWindowTitleHidden;
		s->tabbingDisallowed = w.tabbingMode == NSWindowTabbingModeDisallowed;
		NSWindowButton kinds[3] = {NSWindowCloseButton, NSWindowMiniaturizeButton, NSWindowZoomButton};
		for (int i = 0; i < 3; i++) {
			NSButton *b = [w standardWindowButton:kinds[i]];
			if (b == nil) {
				s->buttonsHidden[i] = 1;
				continue;
			}
			s->buttonsHidden[i] = b.hidden;
			s->buttons[i] = topLeft(w, [b convertRect:b.bounds toView:nil]);
		}
		NSView *container = [w standardWindowButton:NSWindowCloseButton].superview.superview;
		if (container != nil) {
			s->container = topLeft(w, [container convertRect:container.bounds toView:nil]);
		}
		cocoatest_rect f = {0, 0, NSWidth(w.frame), NSHeight(w.frame)};
		s->frame = f;
		NSView *content = w.contentView;
		s->content = topLeft(w, [content convertRect:content.bounds toView:nil]);
		if ([content respondsToSelector:@selector(_opaqueRectForWindowMoveWhenInTitlebar)]) {
			NSRect r = [content _opaqueRectForWindowMoveWhenInTitlebar];
			CGFloat y = content.isFlipped ? r.origin.y : NSHeight(content.bounds) - NSMaxY(r);
			cocoatest_rect o = {r.origin.x, y, r.size.width, r.size.height};
			s->opaqueForMove = o;
			s->hasOpaqueForMove = 1;
		}
		s->visible = w.visible;
		s->zoomed = w.zoomed;
		s->miniaturized = w.miniaturized;
		s->key = w.keyWindow;
		s->appActive = NSApp.active;
		const char *t = w.title.UTF8String;
		if (t != NULL) {
			strlcpy(s->title, t, sizeof(s->title));
		}
	}
}

int cocoatest_hit(uintptr_t viewRef, CGFloat x, CGFloat y, char *cls, int n) {
	@autoreleasepool {
		cls[0] = 0;
		NSView *view = (__bridge NSView *)(void *)viewRef;
		NSWindow *w = view.window;
		NSView *frame = w.contentView.superview;
		if (frame == nil) {
			return 0;
		}
		// hitTest: takes a point in the receiver's superview; the frame view
		// has none, so window coordinates
		NSView *hit = [frame hitTest:NSMakePoint(x, NSHeight(w.frame) - y)];
		if (hit == nil) {
			return 0;
		}
		strlcpy(cls, NSStringFromClass([hit class]).UTF8String, n);
		if (hit == w.contentView) {
			return 1;
		}
		NSWindowButton kinds[3] = {NSWindowCloseButton, NSWindowMiniaturizeButton, NSWindowZoomButton};
		for (int i = 0; i < 3; i++) {
			NSButton *b = [w standardWindowButton:kinds[i]];
			if (b != nil && (hit == b || [hit isDescendantOf:b])) {
				return 2;
			}
		}
		return 3;
	}
}

void cocoatest_post_mouse(uintptr_t view, int down, CGFloat x, CGFloat y, int clicks) {
	@autoreleasepool {
		NSWindow *w = windowOf(view);
		if (w == nil) {
			return;
		}
		NSEvent *e = [NSEvent mouseEventWithType:down ? NSEventTypeLeftMouseDown : NSEventTypeLeftMouseUp
										location:NSMakePoint(x, NSHeight(w.frame) - y)
								   modifierFlags:0
									   timestamp:[[NSProcessInfo processInfo] systemUptime]
									windowNumber:w.windowNumber
										 context:nil
									 eventNumber:0
									  clickCount:clicks
										pressure:down ? 1.0 : 0.0];
		[NSApp postEvent:e atStart:NO];
	}
}

void cocoatest_press(uintptr_t view, int button) {
	@autoreleasepool {
		NSWindowButton kinds[3] = {NSWindowCloseButton, NSWindowMiniaturizeButton, NSWindowZoomButton};
		if (button < 0 || button > 2) {
			return;
		}
		[[windowOf(view) standardWindowButton:kinds[button]] performClick:nil];
	}
}

void cocoatest_order_front(uintptr_t view) {
	@autoreleasepool {
		[NSApp activateIgnoringOtherApps:YES];
		[windowOf(view) makeKeyAndOrderFront:nil];
	}
}

void cocoatest_set_double_click(const char *v) {
	@autoreleasepool {
		NSUserDefaults *d = [NSUserDefaults standardUserDefaults];
		if (v == NULL) {
			[d removeObjectForKey:@"AppleActionOnDoubleClick"];
		} else {
			[d setObject:[NSString stringWithUTF8String:v] forKey:@"AppleActionOnDoubleClick"];
		}
	}
}

int cocoatest_reopen(int visible, int *handled) {
	@autoreleasepool {
		id<NSApplicationDelegate> del = NSApp.delegate;
		if (del == nil || ![del respondsToSelector:@selector(applicationShouldHandleReopen:hasVisibleWindows:)]) {
			*handled = 0;
			return 0;
		}
		*handled = 1;
		return [del applicationShouldHandleReopen:NSApp hasVisibleWindows:visible != 0] ? 1 : 0;
	}
}

void cocoatest_post_did_become_active(void) {
	@autoreleasepool {
		[[NSNotificationCenter defaultCenter] postNotificationName:NSApplicationDidBecomeActiveNotification object:NSApp];
	}
}

int cocoatest_key_equivalent(const char *key, int mods) {
	@autoreleasepool {
		NSEventModifierFlags m = 0;
		if (mods & 1) m |= NSEventModifierFlagCommand;
		if (mods & 2) m |= NSEventModifierFlagShift;
		if (mods & 4) m |= NSEventModifierFlagOption;
		if (mods & 8) m |= NSEventModifierFlagControl;
		NSString *k = [NSString stringWithUTF8String:key];
		NSWindow *w = NSApp.keyWindow;
		NSEvent *e = [NSEvent keyEventWithType:NSEventTypeKeyDown
									  location:NSZeroPoint
								 modifierFlags:m
									 timestamp:[[NSProcessInfo processInfo] systemUptime]
								  windowNumber:w != nil ? w.windowNumber : 0
									   context:nil
									characters:k
				   charactersIgnoringModifiers:k
									 isARepeat:NO
									   keyCode:0];
		return [NSApp.mainMenu performKeyEquivalent:e] ? 1 : 0;
	}
}

static void runMain(void *h) {
	cocoatestRun((uintptr_t)h);
}

void cocoatest_on_main(uintptr_t h) {
	dispatch_async_f(dispatch_get_main_queue(), (void *)h, runMain);
}

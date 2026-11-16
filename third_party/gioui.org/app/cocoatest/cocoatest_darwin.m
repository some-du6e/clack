// SPDX-License-Identifier: Unlicense OR MIT

//go:build darwin && !ios

// Clack patch (CLACK_PATCHES.md §24): AppKit helpers for the macOS window
// tests (cocoatest_darwin.go). ARC.

#import <AppKit/AppKit.h>
#import <Carbon/Carbon.h>
#import <QuartzCore/CAMetalLayer.h>
#import <objc/runtime.h>

#include "_cgo_export.h"

@interface NSView (CocoaTest)
- (NSRect)_opaqueRectForWindowMoveWhenInTitlebar;
@end

static NSWindow *windowOf(uintptr_t view) {
	return ((__bridge NSView *)(void *)view).window;
}

// primaryHeight is the primary screen's height, which Electron flips
// screen coordinates against (Clack patch, CLACK_PATCHES.md §25).
static CGFloat primaryHeight(void) {
	NSScreen *s = [NSScreen screens].firstObject;
	return s != nil ? NSHeight(s.frame) : 0;
}

// flipped is r (screen coordinates, origin at the primary screen's bottom
// left) from the primary screen's top left, y down.
static cocoatest_rect flipped(NSRect r) {
	cocoatest_rect o = {r.origin.x, primaryHeight() - NSMaxY(r), r.size.width, r.size.height};
	return o;
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
		s->screenFrame = flipped(w.frame);
		s->occlusionVisible = (w.occlusionState & NSWindowOcclusionStateVisible) != 0;
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

int cocoatest_screens(cocoatest_rect *frames, cocoatest_rect *visible, int limit) {
	@autoreleasepool {
		int n = 0;
		for (NSScreen *s in [NSScreen screens]) {
			if (n == limit) {
				break;
			}
			frames[n] = flipped(s.frame);
			visible[n] = flipped(s.visibleFrame);
			n++;
		}
		return n;
	}
}

void cocoatest_set_top_left(uintptr_t view, CGFloat x, CGFloat y) {
	@autoreleasepool {
		[windowOf(view) setFrameTopLeftPoint:NSMakePoint(x, primaryHeight() - y)];
	}
}

void cocoatest_zoom(uintptr_t view) {
	@autoreleasepool {
		[windowOf(view) zoom:nil];
	}
}

void cocoatest_toggle_full_screen(uintptr_t view) {
	@autoreleasepool {
		[windowOf(view) toggleFullScreen:nil];
	}
}

// layoutChars is what the current keyboard layout types for the key at
// code with m held (UCKeyTranslate, as the window server fills a key
// event's characters in), AppKit's function-key characters for the
// arrows; nil when the layout can't say.
static NSString *layoutChars(unsigned short code, NSEventModifierFlags m) {
	unichar fk = 0;
	switch (code) {
	case kVK_UpArrow:
		fk = NSUpArrowFunctionKey;
		break;
	case kVK_DownArrow:
		fk = NSDownArrowFunctionKey;
		break;
	case kVK_LeftArrow:
		fk = NSLeftArrowFunctionKey;
		break;
	case kVK_RightArrow:
		fk = NSRightArrowFunctionKey;
		break;
	}
	if (fk != 0) {
		return [NSString stringWithCharacters:&fk length:1];
	}
	TISInputSourceRef src = TISCopyCurrentKeyboardLayoutInputSource();
	if (src == NULL) {
		return nil;
	}
	NSString *s = nil;
	CFDataRef data = (CFDataRef)TISGetInputSourceProperty(src, kTISPropertyUnicodeKeyLayoutData);
	if (data != NULL) {
		UInt32 mods = 0;
		if (m & NSEventModifierFlagShift) mods |= shiftKey;
		if (m & NSEventModifierFlagOption) mods |= optionKey;
		if (m & NSEventModifierFlagControl) mods |= controlKey;
		if (m & NSEventModifierFlagCommand) mods |= cmdKey;
		UInt32 dead = 0;
		UniChar buf[8];
		UniCharCount n = 0;
		OSStatus st = UCKeyTranslate((const UCKeyboardLayout *)CFDataGetBytePtr(data), code, kUCKeyActionDown,
									 (mods >> 8) & 0xff, LMGetKbdType(), kUCKeyTranslateNoDeadKeysMask, &dead, 8, &n, buf);
		if (st == noErr) {
			s = [NSString stringWithCharacters:buf length:n];
		}
	}
	CFRelease(src);
	return s;
}

static NSEvent *keyEvent(NSEventType type, NSWindow *w, NSEventModifierFlags m, unsigned short code, NSString *chars, NSString *ign) {
	return [NSEvent keyEventWithType:type
							location:NSZeroPoint
					   modifierFlags:m
						   timestamp:[[NSProcessInfo processInfo] systemUptime]
						windowNumber:w.windowNumber
							 context:nil
						  characters:chars
		 charactersIgnoringModifiers:ign
						   isARepeat:NO
							 keyCode:code];
}

// the modifier keys in the order a hand presses them
static const struct {
	int mod;
	NSEventModifierFlags flag;
	unsigned short code;
} modKeys[4] = {
	{8, NSEventModifierFlagControl, kVK_Control},
	{4, NSEventModifierFlagOption, kVK_Option},
	{2, NSEventModifierFlagShift, kVK_Shift},
	{1, NSEventModifierFlagCommand, kVK_Command},
};

void cocoatest_post_key(uintptr_t viewRef, int code, int mods, int direct, char *charsOut, char *ignOut, int n) {
	@autoreleasepool {
		charsOut[0] = 0;
		ignOut[0] = 0;
		NSView *view = (__bridge NSView *)(void *)viewRef;
		NSWindow *w = view.window;
		if (w == nil) {
			return;
		}
		unsigned short c = (unsigned short)code;
		NSMutableArray<NSEvent *> *evs = [NSMutableArray array];
		NSEventModifierFlags held = 0;
		for (int i = 0; i < 4; i++) {
			if (mods & modKeys[i].mod) {
				held |= modKeys[i].flag;
				NSEvent *e = keyEvent(NSEventTypeFlagsChanged, w, held, modKeys[i].code, @"", @"");
				if (e != nil) {
					[evs addObject:e];
				}
			}
		}
		NSEventModifierFlags km = held;
		if (c == kVK_UpArrow || c == kVK_DownArrow || c == kVK_LeftArrow || c == kVK_RightArrow) {
			km |= NSEventModifierFlagFunction | NSEventModifierFlagNumericPad;
		}
		NSString *chars = layoutChars(c, held) ?: @"";
		NSString *ign = layoutChars(c, held & NSEventModifierFlagShift) ?: @"";
		strlcpy(charsOut, chars.UTF8String ?: "", n);
		strlcpy(ignOut, ign.UTF8String ?: "", n);
		NSEvent *down = keyEvent(NSEventTypeKeyDown, w, km, c, chars, ign);
		NSEvent *up = keyEvent(NSEventTypeKeyUp, w, km, c, chars, ign);
		if (down != nil) {
			[evs addObject:down];
		}
		if (up != nil) {
			[evs addObject:up];
		}
		for (int i = 3; i >= 0; i--) {
			if (mods & modKeys[i].mod) {
				held &= ~modKeys[i].flag;
				NSEvent *e = keyEvent(NSEventTypeFlagsChanged, w, held, modKeys[i].code, @"", @"");
				if (e != nil) {
					[evs addObject:e];
				}
			}
		}
		for (NSEvent *e in evs) {
			if (!direct) {
				[NSApp postEvent:e atStart:NO];
				continue;
			}
			switch (e.type) {
			case NSEventTypeFlagsChanged:
				[view flagsChanged:e];
				break;
			case NSEventTypeKeyDown:
				[view keyDown:e];
				break;
			default:
				[view keyUp:e];
				break;
			}
		}
	}
}

void cocoatest_keyboard_layout(char *out, int n) {
	@autoreleasepool {
		out[0] = 0;
		TISInputSourceRef src = TISCopyCurrentKeyboardLayoutInputSource();
		if (src == NULL) {
			return;
		}
		CFStringRef sid = (CFStringRef)TISGetInputSourceProperty(src, kTISPropertyInputSourceID);
		if (sid != NULL) {
			CFStringGetCString(sid, out, n, kCFStringEncodingUTF8);
		}
		CFRelease(src);
	}
}

// ---- TraceKeys: what a view's class gets of the key events ----

static NSMutableString *keyTrace;
static NSMutableSet<NSString *> *tracedClasses;
static IMP origKeyDown, origKeyUp, origFlags, origDoCommand, origInsert, origInsertRange;

static NSString *codepoints(NSString *s) {
	NSMutableString *o = [NSMutableString string];
	for (NSUInteger i = 0; i < s.length; i++) {
		[o appendFormat:@"%sU+%04X", i > 0 ? " " : "", (unsigned)[s characterAtIndex:i]];
	}
	return o.length > 0 ? o : @"none";
}

static NSString *flagNames(NSEventModifierFlags f) {
	NSMutableString *o = [NSMutableString string];
	if (f & NSEventModifierFlagControl) [o appendString:@"⌃"];
	if (f & NSEventModifierFlagOption) [o appendString:@"⌥"];
	if (f & NSEventModifierFlagShift) [o appendString:@"⇧"];
	if (f & NSEventModifierFlagCommand) [o appendString:@"⌘"];
	if (f & NSEventModifierFlagFunction) [o appendString:@"fn"];
	return o.length > 0 ? o : @"-";
}

static void traceEvent(NSString *what, NSEvent *e) {
	[keyTrace appendFormat:@"%@ code %d %@ chars %@ ignoring %@\n", what, (int)e.keyCode, flagNames(e.modifierFlags),
						   e.type == NSEventTypeFlagsChanged ? @"-" : codepoints(e.characters),
						   e.type == NSEventTypeFlagsChanged ? @"-" : codepoints(e.charactersIgnoringModifiers)];
}

static void tKeyDown(id self, SEL cmd, NSEvent *e) {
	traceEvent(@"keyDown:", e);
	((void (*)(id, SEL, NSEvent *))origKeyDown)(self, cmd, e);
}

static void tKeyUp(id self, SEL cmd, NSEvent *e) {
	traceEvent(@"keyUp:", e);
	((void (*)(id, SEL, NSEvent *))origKeyUp)(self, cmd, e);
}

static void tFlags(id self, SEL cmd, NSEvent *e) {
	traceEvent(@"flagsChanged:", e);
	((void (*)(id, SEL, NSEvent *))origFlags)(self, cmd, e);
}

static void tDoCommand(id self, SEL cmd, SEL action) {
	[keyTrace appendFormat:@"  doCommandBySelector: %@\n", NSStringFromSelector(action)];
	((void (*)(id, SEL, SEL))origDoCommand)(self, cmd, action);
}

static NSString *plain(id s) {
	return [s isKindOfClass:[NSAttributedString class]] ? [(NSAttributedString *)s string] : (NSString *)s;
}

static void tInsert(id self, SEL cmd, id s) {
	[keyTrace appendFormat:@"  insertText: %@\n", codepoints(plain(s))];
	((void (*)(id, SEL, id))origInsert)(self, cmd, s);
}

static void tInsertRange(id self, SEL cmd, id s, NSRange r) {
	[keyTrace appendFormat:@"  insertText:replacementRange: %@\n", codepoints(plain(s))];
	((void (*)(id, SEL, id, NSRange))origInsertRange)(self, cmd, s, r);
}

static IMP swap(Class c, SEL sel, IMP imp) {
	Method m = class_getInstanceMethod(c, sel);
	return m != NULL ? method_setImplementation(m, imp) : NULL;
}

void cocoatest_trace_keys(uintptr_t viewRef) {
	@autoreleasepool {
		Class c = [(__bridge NSView *)(void *)viewRef class];
		if (tracedClasses == nil) {
			tracedClasses = [NSMutableSet set];
			keyTrace = [NSMutableString string];
		}
		NSString *name = NSStringFromClass(c);
		if ([tracedClasses containsObject:name] || tracedClasses.count > 0) {
			return; // one class: the originals are kept in statics
		}
		[tracedClasses addObject:name];
		origKeyDown = swap(c, @selector(keyDown:), (IMP)tKeyDown);
		origKeyUp = swap(c, @selector(keyUp:), (IMP)tKeyUp);
		origFlags = swap(c, @selector(flagsChanged:), (IMP)tFlags);
		origDoCommand = swap(c, @selector(doCommandBySelector:), (IMP)tDoCommand);
		origInsert = swap(c, @selector(insertText:), (IMP)tInsert);
		origInsertRange = swap(c, @selector(insertText:replacementRange:), (IMP)tInsertRange);
	}
}

char *cocoatest_key_trace(void) {
	@autoreleasepool {
		if (keyTrace == nil || keyTrace.length == 0) {
			return NULL;
		}
		char *s = strdup(keyTrace.UTF8String);
		[keyTrace setString:@""];
		return s;
	}
}

static __weak NSView *trackedView;
static __weak NSWindow *trackedWindow;

void cocoatest_track(uintptr_t viewRef) {
	@autoreleasepool {
		NSView *view = (__bridge NSView *)(void *)viewRef;
		trackedView = view;
		trackedWindow = view.window;
	}
}

void cocoatest_alive(int *view, int *window) {
	@autoreleasepool {
		*view = trackedView != nil;
		*window = trackedWindow != nil;
	}
}

// ---- Clack patch (CLACK_PATCHES.md §32): occlusion, sleep and drawables ----

static NSWindow *coverWindow;

void cocoatest_cover(void) {
	@autoreleasepool {
		NSRect all = NSZeroRect;
		for (NSScreen *s in [NSScreen screens]) {
			all = NSUnionRect(all, s.frame);
		}
		// larger than any window under it: macOS may not count a window of
		// the same size as covering (Chromium's WindowOcclusionCheckerMac)
		all = NSInsetRect(all, -40, -40);
		if (coverWindow == nil) {
			coverWindow = [[NSWindow alloc] initWithContentRect:all
													  styleMask:NSWindowStyleMaskBorderless
														backing:NSBackingStoreBuffered
														  defer:NO];
			coverWindow.releasedWhenClosed = NO;
			coverWindow.opaque = YES;
			coverWindow.backgroundColor = NSColor.blackColor;
			coverWindow.hasShadow = NO;
			coverWindow.level = NSFloatingWindowLevel;
		}
		[coverWindow setFrame:all display:YES];
		[coverWindow orderFrontRegardless];
	}
}

void cocoatest_uncover(void) {
	@autoreleasepool {
		[coverWindow orderOut:nil];
	}
}

void cocoatest_post_workspace(int note) {
	@autoreleasepool {
		NSString *names[4] = {
			NSWorkspaceWillSleepNotification,
			NSWorkspaceDidWakeNotification,
			NSWorkspaceScreensDidSleepNotification,
			NSWorkspaceScreensDidWakeNotification,
		};
		if (note < 0 || note > 3) {
			return;
		}
		NSWorkspace *ws = [NSWorkspace sharedWorkspace];
		[ws.notificationCenter postNotificationName:names[note] object:ws];
	}
}

int cocoatest_drawable_timeout(uintptr_t viewRef) {
	@autoreleasepool {
		NSView *view = (__bridge NSView *)(void *)viewRef;
		if (![view.layer isKindOfClass:[CAMetalLayer class]]) {
			return -1;
		}
		if (@available(macOS 10.13, *)) {
			return ((CAMetalLayer *)view.layer).allowsNextDrawableTimeout ? 1 : 0;
		}
		return 0;
	}
}

// A layer whose drawables the compositor keeps: while held, every
// CAMetalLayer's nextDrawable answers as CAMetalLayer documents for a pool
// with none free, waiting a second and giving nil with
// allowsNextDrawableTimeout, waiting until the release without it.
// (Holding drawables doesn't do it: on the macOS 26 runner a layer gave 8
// unpresented ones, textures taken, without waiting.)
static IMP origNextDrawable;
static NSCondition *drawableCond;
static BOOL drawablesHeld;
static int heldCalls;

static id heldNextDrawable(id self, SEL cmd) {
	NSCondition *c = drawableCond;
	[c lock];
	if (drawablesHeld) {
		heldCalls++;
		BOOL timeout = YES;
		if (@available(macOS 10.13, *)) {
			timeout = ((CAMetalLayer *)self).allowsNextDrawableTimeout;
		}
		NSDate *limit = timeout ? [NSDate dateWithTimeIntervalSinceNow:1.0] : [NSDate distantFuture];
		while (drawablesHeld && [c waitUntilDate:limit]) {
		}
		if (drawablesHeld) {
			[c unlock];
			return nil;
		}
	}
	[c unlock];
	return ((id (*)(id, SEL))origNextDrawable)(self, cmd);
}

void cocoatest_hold_drawables(void) {
	@autoreleasepool {
		if (drawableCond == nil) {
			drawableCond = [[NSCondition alloc] init];
			Method m = class_getInstanceMethod([CAMetalLayer class], @selector(nextDrawable));
			origNextDrawable = method_setImplementation(m, (IMP)heldNextDrawable);
		}
		[drawableCond lock];
		drawablesHeld = YES;
		heldCalls = 0;
		[drawableCond unlock];
	}
}

void cocoatest_release_drawables(void) {
	NSCondition *c = drawableCond;
	if (c == nil) {
		return;
	}
	[c lock];
	drawablesHeld = NO;
	[c broadcast];
	[c unlock];
}

int cocoatest_held_calls(void) {
	NSCondition *c = drawableCond;
	if (c == nil) {
		return 0;
	}
	[c lock];
	int n = heldCalls;
	[c unlock];
	return n;
}

static void runMain(void *h) {
	cocoatestRun((uintptr_t)h);
}

void cocoatest_on_main(uintptr_t h) {
	dispatch_async_f(dispatch_get_main_queue(), (void *)h, runMain);
}

// SPDX-License-Identifier: Unlicense OR MIT

//go:build darwin && !ios

#import <AppKit/AppKit.h>
#import <objc/message.h>
#import <objc/runtime.h>

#include "_cgo_export.h"

__attribute__ ((visibility ("hidden"))) CALayer *gio_layerFactory(BOOL presentWithTrans);

@interface GioAppDelegate : NSObject<NSApplicationDelegate>
@end

@interface GioWindowDelegate : NSObject<NSWindowDelegate>
@end

@interface GioView : NSView <CALayerDelegate,NSTextInputClient>
@property uintptr_t handle;
@property BOOL presentWithTrans;
// firstMouse is a Clack patch (CLACK_PATCHES.md §8): see acceptsFirstMouse.
@property BOOL firstMouse;
// dragFiles is a Clack patch (CLACK_PATCHES.md §11): the drag over the
// view carries file URLs.
@property BOOL dragFiles;
// titlebarHidden is a Clack patch (CLACK_PATCHES.md §23): the window's
// title bar is hidden (HiddenTitleBar) and the view runs under it.
@property BOOL titlebarHidden;
// trafficLight* are a Clack patch (CLACK_PATCHES.md §23): where the
// window's buttons go (TrafficLightPosition), in points.
@property BOOL trafficLightSet;
@property CGFloat trafficLightX;
@property CGFloat trafficLightY;
// orderedOut is a Clack patch (CLACK_PATCHES.md §23): Window.Hide ordered
// the window out and it hasn't drawn since.
@property BOOL orderedOut;
@end

// AppKit's undocumented hook for the parts of a view under a transparent
// title bar (Clack patch, CLACK_PATCHES.md §23; see GioView's override).
@interface NSView (GioTitlebar)
- (NSRect)_opaqueRectForWindowMoveWhenInTitlebar;
@end

// redrawTrafficLights is a Clack patch (CLACK_PATCHES.md §23): Electron's
// WindowButtonsProxy redraw (shell/browser/ui/cocoa/window_buttons_proxy.mm)
// for TrafficLightPosition. The buttons' title bar container becomes the
// buttons' height plus 2y tall at the window's top, and the buttons sit
// x from the left (the right in a right-to-left title bar), centred in it:
// their tops y from the window's top. Not in full screen, whose title bar
// is AppKit's own (RedrawTrafficLights).
static void redrawTrafficLights(NSWindow *window) {
	if (window == nil || ![window.contentView isKindOfClass:[GioView class]]) {
		return;
	}
	GioView *view = (GioView *)window.contentView;
	if (!view.titlebarHidden || !view.trafficLightSet || (window.styleMask & NSWindowStyleMaskFullScreen) != 0) {
		return;
	}
	BOOL rtl = window.windowTitlebarLayoutDirection == NSUserInterfaceLayoutDirectionRightToLeft;
	NSButton *left = [window standardWindowButton:rtl ? NSWindowZoomButton : NSWindowCloseButton];
	NSButton *middle = [window standardWindowButton:NSWindowMiniaturizeButton];
	NSButton *right = [window standardWindowButton:rtl ? NSWindowCloseButton : NSWindowZoomButton];
	NSView *container = left.superview.superview;
	if (container == nil || middle == nil || right == nil) {
		return;
	}
	CGFloat bw = NSWidth(left.frame), bh = NSHeight(left.frame);
	CGFloat pad = NSMinX(middle.frame) - NSMaxX(left.frame);
	CGFloat x = view.trafficLightX, y = view.trafficLightY;
	CGFloat start = rtl ? NSWidth(window.frame) - 3 * bw - 2 * pad - x : x;
	NSRect cb = container.frame;
	cb.size.height = bh + 2 * y;
	cb.origin.y = NSHeight(window.frame) - NSHeight(cb);
	[container setFrame:cb];
	CGFloat by = (NSHeight(container.frame) - bh) / 2;
	[left setFrameOrigin:NSMakePoint(start, by)];
	start += bw + pad;
	[middle setFrameOrigin:NSMakePoint(start, by)];
	start += bw + pad;
	[right setFrameOrigin:NSMakePoint(start, by)];
}

// setTrafficLightsVisible shows or hides the buttons' title bar container
// (WindowButtonsProxy setVisible:).
static void setTrafficLightsVisible(NSWindow *window, BOOL visible) {
	NSView *container = [window standardWindowButton:NSWindowCloseButton].superview.superview;
	[container setHidden:!visible];
}

static BOOL hasHiddenTitlebar(NSWindow *window) {
	return [window.contentView isKindOfClass:[GioView class]] && ((GioView *)window.contentView).titlebarHidden;
}

void gio_redrawTrafficLights(CFTypeRef windowRef) {
	@autoreleasepool {
		redrawTrafficLights((__bridge NSWindow *)windowRef);
	}
}

void gio_setTitlebar(CFTypeRef viewRef, int hidden, int lightSet, CGFloat x, CGFloat y) {
	@autoreleasepool {
		GioView *view = (__bridge GioView *)viewRef;
		view.titlebarHidden = hidden != 0;
		view.trafficLightSet = lightSet != 0;
		view.trafficLightX = x;
		view.trafficLightY = y;
	}
}

void gio_setOrderedOut(CFTypeRef viewRef, int out) {
	@autoreleasepool {
		GioView *view = (__bridge GioView *)viewRef;
		view.orderedOut = out != 0;
	}
}

// gio_closeWindow is ActionClose. Clack patch (CLACK_PATCHES.md §23):
// performClose: never asks windowShouldClose: of a window with a hidden
// title bar, visible or not (the macOS 26 runner), so such a window does
// what performClose: does itself, as Electron's ElectronNSWindow does for a
// window without a title bar and Chromium does for every programmatic
// close: ask the delegate, then close.
void gio_closeWindow(CFTypeRef windowRef) {
	@autoreleasepool {
		NSWindow *window = (__bridge NSWindow *)windowRef;
		if (!hasHiddenTitlebar(window)) {
			[window performClose:nil];
			return;
		}
		id<NSWindowDelegate> del = window.delegate;
		if ([del respondsToSelector:@selector(windowShouldClose:)] && ![del windowShouldClose:window]) {
			return;
		}
		[window close];
	}
}

@implementation GioWindowDelegate
- (BOOL)windowShouldClose:(NSWindow *)window {
	GioView *view = (GioView *)window.contentView;
	return gio_onCloseRequest(view.handle) != 0;
}
// Clack patch (CLACK_PATCHES.md §23): AppKit lays the title bar out again
// after these and moves the buttons back (Electron's
// ElectronNSWindowDelegate redraws them after each).
- (void)windowDidResize:(NSNotification *)notification {
	redrawTrafficLights((NSWindow *)[notification object]);
}
- (void)windowDidBecomeMain:(NSNotification *)notification {
	redrawTrafficLights((NSWindow *)[notification object]);
}
- (void)windowDidResignMain:(NSNotification *)notification {
	redrawTrafficLights((NSWindow *)[notification object]);
}
// Clack patch (CLACK_PATCHES.md §23): full screen reveals AppKit's own
// title bar, with the title (Electron's NotifyWindowEnterFullScreen); the
// buttons' container hides while the window leaves it, so they don't jump
// (NotifyWindowWillLeaveFullScreen), and comes back moved once it has.
- (void)windowDidEnterFullScreen:(NSNotification *)notification {
	NSWindow *window = (NSWindow *)[notification object];
	if (hasHiddenTitlebar(window)) {
		window.titleVisibility = NSWindowTitleVisible;
		window.titlebarAppearsTransparent = NO;
	}
}
- (void)windowDidFailToEnterFullScreen:(NSWindow *)window {
	redrawTrafficLights(window);
}
- (void)windowDidExitFullScreen:(NSNotification *)notification {
	NSWindow *window = (NSWindow *)[notification object];
	GioView *view = (GioView *)window.contentView;
	if (hasHiddenTitlebar(window)) {
		// the title again too: an option while the window left full screen
		// showed it (Configure: still full screen then)
		window.titleVisibility = NSWindowTitleHidden;
		window.titlebarAppearsTransparent = YES;
		redrawTrafficLights(window);
		setTrafficLightsVisible(window, YES);
	}
	// a Hide waiting for this goes on the next turn of the run loop, as
	// Slack's awaited leave-full-screen does: AppKit is still finishing the
	// transition, which orders the window back in
	__weak GioView *weak = view;
	dispatch_async(dispatch_get_main_queue(), ^{
		GioView *v = weak;
		if (v != nil) {
			gio_onExitedFullScreen(v.handle, 1);
		}
	});
}
- (void)windowDidFailToExitFullScreen:(NSWindow *)window {
	if (hasHiddenTitlebar(window)) { // still full screen: AppKit's title bar
		window.titleVisibility = NSWindowTitleVisible;
		setTrafficLightsVisible(window, YES);
	}
	if ([window.contentView isKindOfClass:[GioView class]]) {
		gio_onExitedFullScreen(((GioView *)window.contentView).handle, 0);
	}
}
// Clack patch (CLACK_PATCHES.md §23): a window back on screen after
// Window.Hide draws again, however it came back (an order-front that
// didn't go through Gio: the Dock icon's reopen).
- (void)windowDidChangeOcclusionState:(NSNotification *)notification {
	NSWindow *window = (NSWindow *)[notification object];
	if (![window.contentView isKindOfClass:[GioView class]]) {
		return;
	}
	GioView *view = (GioView *)window.contentView;
	if (view.orderedOut && window.visible) {
		gio_onOcclusionChanged(view.handle);
	}
}
- (void)windowWillMiniaturize:(NSNotification *)notification {
	NSWindow *window = (NSWindow *)[notification object];
  GioView *view = (GioView *)window.contentView;
	gio_onDraw(view.handle);
}
- (void)windowDidDeminiaturize:(NSNotification *)notification {
	NSWindow *window = (NSWindow *)[notification object];
  GioView *view = (GioView *)window.contentView;
	gio_onDraw(view.handle);
}
- (void)windowWillEnterFullScreen:(NSNotification *)notification {
	NSWindow *window = (NSWindow *)[notification object];
  GioView *view = (GioView *)window.contentView;
	gio_onDraw(view.handle);
}
- (void)windowWillExitFullScreen:(NSNotification *)notification {
	NSWindow *window = (NSWindow *)[notification object];
  GioView *view = (GioView *)window.contentView;
	if (hasHiddenTitlebar(window)) { // Clack patch (CLACK_PATCHES.md §23)
		window.titleVisibility = NSWindowTitleHidden;
		if (view.trafficLightSet) {
			setTrafficLightsVisible(window, NO);
		}
	}
	gio_onDraw(view.handle);
}
- (void)windowDidChangeScreen:(NSNotification *)notification {
	NSWindow *window = (NSWindow *)[notification object];
	CGDirectDisplayID dispID = [[[window screen] deviceDescription][@"NSScreenNumber"] unsignedIntValue];
  GioView *view = (GioView *)window.contentView;
	gio_onChangeScreen(view.handle, dispID);
}
- (void)windowDidBecomeKey:(NSNotification *)notification {
	NSWindow *window = (NSWindow *)[notification object];
	GioView *view = (GioView *)window.contentView;
	if ([window firstResponder] == view) {
		gio_onFocus(view.handle, 1);
	}
	redrawTrafficLights(window); // Clack patch (CLACK_PATCHES.md §23)
}
- (void)windowDidResignKey:(NSNotification *)notification {
	NSWindow *window = (NSWindow *)[notification object];
	GioView *view = (GioView *)window.contentView;
	if ([window firstResponder] == view) {
		gio_onFocus(view.handle, 0);
	}
	redrawTrafficLights(window); // Clack patch (CLACK_PATCHES.md §23)
}
@end

static void handleMouse(GioView *view, NSEvent *event, int typ, CGFloat dx, CGFloat dy) {
	NSPoint p = [view convertPoint:[event locationInWindow] fromView:nil];
	if (!event.hasPreciseScrollingDeltas) {
		// dx and dy are in rows and columns.
		dx *= 10;
		dy *= 10;
	}
	// Origin is in the lower left corner. Convert to upper left.
	CGFloat height = view.bounds.size.height;
	// Clack patch (CLACK_PATCHES.md §23): the click count of a press or
	// release (NSEvent raises for any other kind).
	NSInteger clicks = (typ == MOUSE_DOWN || typ == MOUSE_UP) ? event.clickCount : 0;
	gio_onMouse(view.handle, (__bridge CFTypeRef)event, typ, event.buttonNumber, p.x, height - p.y, dx, dy, [event timestamp], [event modifierFlags], clicks);
}

@implementation GioView
- (void)setFrameSize:(NSSize)newSize {
	[super setFrameSize:newSize];
	[self setNeedsDisplay:YES];
}
// drawRect is called when OpenGL is used, displayLayer otherwise.
// Don't know why.
- (void)drawRect:(NSRect)r {
	gio_onDraw(self.handle);
}
- (void)displayLayer:(CALayer *)layer {
	layer.contentsScale = self.window.backingScaleFactor;
	gio_onDraw(self.handle);
}
- (CALayer *)makeBackingLayer {
	CALayer *layer = gio_layerFactory(self.presentWithTrans);
	layer.delegate = self;
	return layer;
}
- (void)viewDidMoveToWindow {
	gio_onAttached(self.handle, self.window != nil ? 1 : 0);
}
- (void)mouseDown:(NSEvent *)event {
	handleMouse(self, event, MOUSE_DOWN, 0, 0);
}
- (void)mouseUp:(NSEvent *)event {
	handleMouse(self, event, MOUSE_UP, 0, 0);
}
- (void)rightMouseDown:(NSEvent *)event {
	handleMouse(self, event, MOUSE_DOWN, 0, 0);
}
- (void)rightMouseUp:(NSEvent *)event {
	handleMouse(self, event, MOUSE_UP, 0, 0);
}
- (void)otherMouseDown:(NSEvent *)event {
	handleMouse(self, event, MOUSE_DOWN, 0, 0);
}
- (void)otherMouseUp:(NSEvent *)event {
	handleMouse(self, event, MOUSE_UP, 0, 0);
}
- (void)mouseMoved:(NSEvent *)event {
	handleMouse(self, event, MOUSE_MOVE, 0, 0);
}
- (void)mouseDragged:(NSEvent *)event {
	handleMouse(self, event, MOUSE_MOVE, 0, 0);
}
- (void)rightMouseDragged:(NSEvent *)event {
	handleMouse(self, event, MOUSE_MOVE, 0, 0);
}
- (void)otherMouseDragged:(NSEvent *)event {
	handleMouse(self, event, MOUSE_MOVE, 0, 0);
}
- (void)scrollWheel:(NSEvent *)event {
	CGFloat dx = -event.scrollingDeltaX;
	CGFloat dy = -event.scrollingDeltaY;
	handleMouse(self, event, MOUSE_SCROLL, dx, dy);
}
- (void)keyDown:(NSEvent *)event {
	NSString *keys = [event charactersIgnoringModifiers];
	gio_onKeys(self.handle, (__bridge CFTypeRef)event, (__bridge CFTypeRef)keys, [event timestamp], [event modifierFlags], true);
}
- (void)flagsChanged:(NSEvent *)event {
	[self interpretKeyEvents:[NSArray arrayWithObject:event]];
	gio_onFlagsChanged(self.handle, [event modifierFlags]);
}
- (void)keyUp:(NSEvent *)event {
	NSString *keys = [event charactersIgnoringModifiers];
	gio_onKeys(self.handle, (__bridge CFTypeRef)event, (__bridge CFTypeRef)keys, [event timestamp], [event modifierFlags], false);
}
- (void)insertText:(id)string {
	gio_onText(self.handle, (__bridge CFTypeRef)string);
}
- (void)doCommandBySelector:(SEL)action {
	if (!gio_onCommandBySelector(self.handle)) {
		[super doCommandBySelector:action];
	}
}
- (BOOL)hasMarkedText {
	int res = gio_hasMarkedText(self.handle);
	return res ? YES : NO;
}
- (NSRange)markedRange {
	return gio_markedRange(self.handle);
}
- (NSRange)selectedRange {
	return gio_selectedRange(self.handle);
}
- (void)unmarkText {
	gio_unmarkText(self.handle);
}
- (void)setMarkedText:(id)string
        selectedRange:(NSRange)selRange
     replacementRange:(NSRange)replaceRange {
	NSString *str;
	// string is either an NSAttributedString or an NSString.
	if ([string isKindOfClass:[NSAttributedString class]]) {
		str = [string string];
	} else {
		str = string;
	}
	gio_setMarkedText(self.handle, (__bridge CFTypeRef)str, selRange, replaceRange);
}
- (NSArray<NSAttributedStringKey> *)validAttributesForMarkedText {
	return nil;
}
- (NSAttributedString *)attributedSubstringForProposedRange:(NSRange)range
                                                actualRange:(NSRangePointer)actualRange {
	NSString *str = CFBridgingRelease(gio_substringForProposedRange(self.handle, range, actualRange));
	return [[NSAttributedString alloc] initWithString:str attributes:nil];
}
- (void)insertText:(id)string
  replacementRange:(NSRange)replaceRange {
	NSString *str;
	// string is either an NSAttributedString or an NSString.
	if ([string isKindOfClass:[NSAttributedString class]]) {
		str = [string string];
	} else {
		str = string;
	}
	gio_insertText(self.handle, (__bridge CFTypeRef)str, replaceRange);
}
- (NSUInteger)characterIndexForPoint:(NSPoint)p {
	return gio_characterIndexForPoint(self.handle, p);
}
- (NSRect)firstRectForCharacterRange:(NSRange)rng
                         actualRange:(NSRangePointer)actual {
    NSRect r = gio_firstRectForCharacterRange(self.handle, rng, actual);
    r = [self convertRect:r toView:nil];
    return [[self window] convertRectToScreen:r];
}
- (void)applicationWillUnhide:(NSNotification *)notification {
	gio_onDraw(self.handle);
}
- (void)applicationDidHide:(NSNotification *)notification {
	gio_onDraw(self.handle);
}
- (void)dealloc {
	gio_onDestroy(self.handle);
}
- (BOOL) becomeFirstResponder {
	gio_onFocus(self.handle, 1);
	return [super becomeFirstResponder];
}
- (BOOL)acceptsFirstResponder {
    return YES;
}
// Clack patch (CLACK_PATCHES.md §8): a panel's view takes the click that
// makes its window key, as Electron's acceptFirstMouse does.
- (BOOL)acceptsFirstMouse:(NSEvent *)event {
	return self.firstMouse;
}
// Clack patch (CLACK_PATCHES.md §23): under a transparent title bar the
// window server drags the window from the parts of the content this
// reports as not opaque, without asking the app. Chromium's
// BridgedContentView reports its whole bounds, so every click reaches the
// view and the program decides: an ActionMove area drags (mouseDown:).
- (NSRect)_opaqueRectForWindowMoveWhenInTitlebar {
	if (self.titlebarHidden) {
		return self.bounds;
	}
	if (class_getInstanceMethod([NSView class], _cmd) == NULL) {
		return NSZeroRect;
	}
	return [super _opaqueRectForWindowMoveWhenInTitlebar];
}
// Clack patch (CLACK_PATCHES.md §23): a theme change lays the title bar
// out again (Electron's OnNativeThemeUpdated posts a redraw).
- (void)viewDidChangeEffectiveAppearance {
	[super viewDidChangeEffectiveAppearance];
	if (!self.titlebarHidden) {
		return;
	}
	__weak GioView *weak = self;
	dispatch_async(dispatch_get_main_queue(), ^{
		redrawTrafficLights(weak.window);
	});
}
- (BOOL) resignFirstResponder {
	gio_onFocus(self.handle, 0);
	return [super resignFirstResponder];
}
// Clack patch (CLACK_PATCHES.md §11): files dragged from Finder, as the
// NSDraggingDestination Chromium's WebContentsViewMac gives its view.
// Kinds match app.FileDropKind: 0 enter, 1 over, 2 leave, 3 drop.
static NSString *dragPaths(id<NSDraggingInfo> sender) {
	NSArray<NSURL *> *urls = [[sender draggingPasteboard] readObjectsForClasses:@[[NSURL class]]
		options:@{NSPasteboardURLReadingFileURLsOnlyKey: @YES}];
	if (urls.count == 0) {
		return nil;
	}
	NSMutableArray<NSString *> *paths = [NSMutableArray arrayWithCapacity:urls.count];
	for (NSURL *u in urls) {
		if (u.path != nil) {
			[paths addObject:u.path];
		}
	}
	if (paths.count == 0) {
		return nil;
	}
	unichar nul = 0;
	return [paths componentsJoinedByString:[NSString stringWithCharacters:&nul length:1]];
}
static void sendDrag(GioView *view, id<NSDraggingInfo> sender, int kind, NSString *paths) {
	NSPoint p = [view convertPoint:[sender draggingLocation] fromView:nil];
	CGFloat height = view.bounds.size.height;
	gio_onFileDrop(view.handle, kind, p.x, height - p.y, (__bridge CFTypeRef)paths, [NSEvent modifierFlags]);
}
- (NSDragOperation)draggingEntered:(id<NSDraggingInfo>)sender {
	NSString *paths = dragPaths(sender);
	self.dragFiles = paths != nil;
	if (!self.dragFiles) {
		return NSDragOperationNone;
	}
	sendDrag(self, sender, 0, paths);
	return NSDragOperationCopy;
}
- (NSDragOperation)draggingUpdated:(id<NSDraggingInfo>)sender {
	if (!self.dragFiles) {
		return NSDragOperationNone;
	}
	sendDrag(self, sender, 1, nil);
	return NSDragOperationCopy;
}
- (void)draggingExited:(id<NSDraggingInfo>)sender {
	if (self.dragFiles) {
		self.dragFiles = NO;
		sendDrag(self, sender, 2, nil);
	}
}
- (BOOL)prepareForDragOperation:(id<NSDraggingInfo>)sender {
	return self.dragFiles;
}
- (BOOL)performDragOperation:(id<NSDraggingInfo>)sender {
	if (!self.dragFiles) {
		return NO;
	}
	self.dragFiles = NO;
	NSString *paths = dragPaths(sender);
	if (paths == nil) {
		sendDrag(self, sender, 2, nil);
		return NO;
	}
	sendDrag(self, sender, 3, paths);
	return YES;
}
@end

// Delegates are weakly referenced from their peers. Nothing
// else holds a strong reference to our window delegate, so
// keep a single global reference instead.
static GioWindowDelegate *globalWindowDel;

static CVReturn displayLinkCallback(CVDisplayLinkRef dl, const CVTimeStamp *inNow, const CVTimeStamp *inOutputTime, CVOptionFlags flagsIn, CVOptionFlags *flagsOut, void *handle) {
	gio_onFrameCallback(dl);
	return kCVReturnSuccess;
}

CFTypeRef gio_createDisplayLink(void) {
	CVDisplayLinkRef dl;
	CVDisplayLinkCreateWithActiveCGDisplays(&dl);
	CVDisplayLinkSetOutputCallback(dl, displayLinkCallback, nil);
	return dl;
}

int gio_startDisplayLink(CFTypeRef dl) {
	return CVDisplayLinkStart((CVDisplayLinkRef)dl);
}

int gio_stopDisplayLink(CFTypeRef dl) {
	return CVDisplayLinkStop((CVDisplayLinkRef)dl);
}

void gio_releaseDisplayLink(CFTypeRef dl) {
	CVDisplayLinkRelease((CVDisplayLinkRef)dl);
}

void gio_setDisplayLinkDisplay(CFTypeRef dl, uint64_t did) {
	CVDisplayLinkSetCurrentCGDisplay((CVDisplayLinkRef)dl, (CGDirectDisplayID)did);
}

void gio_hideCursor() {
	@autoreleasepool {
		[NSCursor hide];
	}
}

void gio_showCursor() {
	@autoreleasepool {
		[NSCursor unhide];
	}
}

// some cursors are not public, this tries to use a private cursor
// and uses fallback when the use of private cursor fails.
static void trySetPrivateCursor(SEL cursorName, NSCursor* fallback) {
	if ([NSCursor respondsToSelector:cursorName]) {
		id object = [NSCursor performSelector:cursorName];
		if ([object isKindOfClass:[NSCursor class]]) {
			[(NSCursor*)object set];
			return;
		}
	}
	[fallback set];
}

void gio_setCursor(NSUInteger curID) {
	@autoreleasepool {
		switch (curID) {
			case 0: // pointer.CursorDefault
				[NSCursor.arrowCursor set];
				break;
			// case 1: // pointer.CursorNone
			case 2: // pointer.CursorText
				[NSCursor.IBeamCursor set];
				break;
			case 3: // pointer.CursorVerticalText
				[NSCursor.IBeamCursorForVerticalLayout set];
				break;
			case 4: // pointer.CursorPointer
				[NSCursor.pointingHandCursor set];
				break;
			case 5: // pointer.CursorCrosshair
				[NSCursor.crosshairCursor set];
				break;
			case 6: // pointer.CursorAllScroll
				// For some reason, using _moveCursor fails on Monterey.
				// trySetPrivateCursor(@selector(_moveCursor), NSCursor.arrowCursor);
				[NSCursor.arrowCursor set];
				break;
			case 7: // pointer.CursorColResize
				[NSCursor.resizeLeftRightCursor set];
				break;
			case 8: // pointer.CursorRowResize
				[NSCursor.resizeUpDownCursor set];
				break;
			case 9: // pointer.CursorGrab
				[NSCursor.openHandCursor set];
				break;
			case 10: // pointer.CursorGrabbing
				[NSCursor.closedHandCursor set];
				break;
			case 11: // pointer.CursorNotAllowed
				[NSCursor.operationNotAllowedCursor set];
				break;
			case 12: // pointer.CursorWait
				trySetPrivateCursor(@selector(busyButClickableCursor), NSCursor.arrowCursor);
				break;
			case 13: // pointer.CursorProgress
				trySetPrivateCursor(@selector(busyButClickableCursor), NSCursor.arrowCursor);
				break;
			case 14: // pointer.CursorNorthWestResize
				trySetPrivateCursor(@selector(_windowResizeNorthWestCursor), NSCursor.resizeUpDownCursor);
				break;
			case 15: // pointer.CursorNorthEastResize
				trySetPrivateCursor(@selector(_windowResizeNorthEastCursor), NSCursor.resizeUpDownCursor);
				break;
			case 16: // pointer.CursorSouthWestResize
				trySetPrivateCursor(@selector(_windowResizeSouthWestCursor), NSCursor.resizeUpDownCursor);
				break;
			case 17: // pointer.CursorSouthEastResize
				trySetPrivateCursor(@selector(_windowResizeSouthEastCursor), NSCursor.resizeUpDownCursor);
				break;
			case 18: // pointer.CursorNorthSouthResize
				[NSCursor.resizeUpDownCursor set];
				break;
			case 19: // pointer.CursorEastWestResize
				[NSCursor.resizeLeftRightCursor set];
				break;
			case 20: // pointer.CursorWestResize
				[NSCursor.resizeLeftCursor set];
				break;
			case 21: // pointer.CursorEastResize
				[NSCursor.resizeRightCursor set];
				break;
			case 22: // pointer.CursorNorthResize
				[NSCursor.resizeUpCursor set];
				break;
			case 23: // pointer.CursorSouthResize
				[NSCursor.resizeDownCursor set];
				break;
			case 24: // pointer.CursorNorthEastSouthWestResize
				trySetPrivateCursor(@selector(_windowResizeNorthEastSouthWestCursor), NSCursor.resizeUpDownCursor);
				break;
			case 25: // pointer.CursorNorthWestSouthEastResize
				trySetPrivateCursor(@selector(_windowResizeNorthWestSouthEastCursor), NSCursor.resizeUpDownCursor);
				break;
			default:
				[NSCursor.arrowCursor set];
				break;
		}
	}
}

// GioPanelWindow is a Clack patch (CLACK_PATCHES.md §8): Electron's
// ElectronNSPanel, the window of a BrowserWindow with type "panel". The
// non-activating mask is reserved for NSPanel; an NSWindow gets it by
// always reporting it, so a click on the window doesn't activate the app.
@interface GioPanelWindow : NSWindow
@end

@implementation GioPanelWindow
- (NSWindowStyleMask)styleMask {
	return [super styleMask] | NSWindowStyleMaskNonactivatingPanel;
}
- (void)setCollectionBehavior:(NSWindowCollectionBehavior)behavior {
	[super setCollectionBehavior:behavior | NSWindowCollectionBehaviorCanJoinAllSpaces | NSWindowCollectionBehaviorFullScreenAuxiliary];
}
@end

CFTypeRef gio_createWindow(CFTypeRef viewRef, CGFloat width, CGFloat height, int panel, int hiddenTitlebar) {
	@autoreleasepool {
		NSRect rect = NSMakeRect(0, 0, width, height);
		NSUInteger styleMask = NSTitledWindowMask |
			NSResizableWindowMask |
			NSMiniaturizableWindowMask |
			NSClosableWindowMask;
		if (hiddenTitlebar) {
			// Clack patch (CLACK_PATCHES.md §23): the content view covers the
			// title bar from the start, so the window is the size asked for
			// (Electron's frameless content bounds are the window's).
			styleMask |= NSWindowStyleMaskFullSizeContentView;
		}

		Class cls = panel ? [GioPanelWindow class] : [NSWindow class];
		NSWindow* window = [[cls alloc] initWithContentRect:rect
												  styleMask:styleMask
													backing:NSBackingStoreBuffered
													  defer:NO];
		[window setAcceptsMouseMovedEvents:YES];
		NSView *view = (__bridge NSView *)viewRef;
		[window setContentView:view];
		window.delegate = globalWindowDel;
		if (hiddenTitlebar) {
			// Electron's frameless window: no title, a transparent title bar,
			// and no window tabs (a tab bar has no title bar to go under).
			window.titlebarAppearsTransparent = YES;
			window.titleVisibility = NSWindowTitleHidden;
			window.tabbingMode = NSWindowTabbingModeDisallowed;
			((GioView *)view).titlebarHidden = YES;
		}
		if (panel) {
			// What Electron's NativeWindowMac does for type "panel" when it
			// creates one: the floating level, and the panel's collection
			// behaviour (its setCollectionBehavior adds it).
			[window setLevel:NSFloatingWindowLevel];
			[window setCollectionBehavior:[window collectionBehavior]];
			((GioView *)view).firstMouse = YES;
		}
		return (__bridge_retained CFTypeRef)window;
	}
}

CFTypeRef gio_createView(int presentWithTrans) {
	@autoreleasepool {
		NSRect frame = NSMakeRect(0, 0, 0, 0);
		GioView* view = [[GioView alloc] initWithFrame:frame];
		view.presentWithTrans = presentWithTrans ? YES : NO;
		view.wantsLayer = YES;
		view.layerContentsRedrawPolicy = NSViewLayerContentsRedrawDuringViewResize;
		[view registerForDraggedTypes:@[NSPasteboardTypeFileURL]]; // Clack patch (CLACK_PATCHES.md §11)

		[[NSNotificationCenter defaultCenter] addObserver:view
												 selector:@selector(applicationWillUnhide:)
													 name:NSApplicationWillUnhideNotification
												   object:nil];
		[[NSNotificationCenter defaultCenter] addObserver:view
												 selector:@selector(applicationDidHide:)
													 name:NSApplicationDidHideNotification
												   object:nil];
		return CFBridgingRetain(view);
	}
}

void gio_viewSetHandle(CFTypeRef viewRef, uintptr_t handle) {
	@autoreleasepool {
		GioView *v = (__bridge GioView *)viewRef;
		v.handle = handle;
	}
}

@implementation GioAppDelegate
- (void)applicationDidFinishLaunching:(NSNotification *)aNotification {
	[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
	[NSApp activateIgnoringOtherApps:YES];
}
- (void)application:(NSApplication *)application openURLs:(NSArray<NSURL *> *)urls {
	for (NSURL *url in urls) {
		gio_onOpenURI((__bridge CFTypeRef)url.absoluteString);
	}
}
@end

void gio_main() {
	@autoreleasepool {
		[NSApplication sharedApplication];
		GioAppDelegate *del = [[GioAppDelegate alloc] init];
		[NSApp setDelegate:del];

		NSMenuItem *mainMenu = [NSMenuItem new];

		NSMenu *menu = [NSMenu new];
		NSMenuItem *hideMenuItem = [[NSMenuItem alloc] initWithTitle:@"Hide"
															  action:@selector(hide:)
													   keyEquivalent:@"h"];
		[menu addItem:hideMenuItem];
		NSMenuItem *quitMenuItem = [[NSMenuItem alloc] initWithTitle:@"Quit"
															  action:@selector(terminate:)
													   keyEquivalent:@"q"];
		[menu addItem:quitMenuItem];
		[mainMenu setSubmenu:menu];
		NSMenu *menuBar = [NSMenu new];
		[menuBar addItem:mainMenu];
		[NSApp setMainMenu:menuBar];

		globalWindowDel = [[GioWindowDelegate alloc] init];

		[NSApp run];
	}
}

@interface AppListener : NSObject
@end

static AppListener *appListener;

@implementation AppListener
- (void)launchFinished:(NSNotification *)notification {
	appListener = nil;
	gio_onFinishLaunching();
}
@end

void gio_init() {
	@autoreleasepool {
		appListener = [[AppListener alloc] init];
		[[NSNotificationCenter defaultCenter] addObserver:appListener
												 selector:@selector(launchFinished:)
													 name:NSApplicationDidFinishLaunchingNotification
												   object:nil];
	}
}

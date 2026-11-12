//go:build darwin && cgo

#import <AppKit/AppKit.h>
#include "_cgo_export.h"

// Electron's MenuMac::PopupOnUI (electron_api_menu_mac.mm): an NSMenu at
// the pointer, popUpMenuPositioningItem:nil atLocation:(x, view height −
// y) inView:view, run on the main thread. The item picked is the one
// whose action fired while the menu tracked.

@interface ClackNativeMenuTarget : NSObject
@property NSInteger picked;
- (void)pick:(NSMenuItem *)sender;
@end

@implementation ClackNativeMenuTarget
- (void)pick:(NSMenuItem *)sender {
	self.picked = sender.tag;
}
@end

void clack_nativemenu_popup(uintptr_t viewRef, int menuID, int n, const char **titles, const char **keys, const int *flags, double x, double y) {
	NSMutableArray<NSString *> *ts = [NSMutableArray arrayWithCapacity:n];
	NSMutableArray<NSString *> *ks = [NSMutableArray arrayWithCapacity:n];
	NSMutableArray<NSNumber *> *fs = [NSMutableArray arrayWithCapacity:n];
	for (int i = 0; i < n; i++) {
		[ts addObject:[NSString stringWithUTF8String:titles[i]] ?: @""];
		[ks addObject:[NSString stringWithUTF8String:keys[i]] ?: @""];
		[fs addObject:@(flags[i])];
	}
	dispatch_async(dispatch_get_main_queue(), ^{
		NSView *view = (__bridge NSView *)(void *)viewRef;
		if (view == nil || view.window == nil) {
			clackNativeMenuDone(menuID, -1);
			return;
		}
		ClackNativeMenuTarget *target = [ClackNativeMenuTarget new];
		target.picked = -1;
		NSMenu *menu = [[NSMenu alloc] initWithTitle:@""];
		menu.autoenablesItems = NO;
		for (int i = 0; i < n; i++) {
			int f = fs[i].intValue;
			if (f & 1) {
				[menu addItem:[NSMenuItem separatorItem]];
				continue;
			}
			NSMenuItem *it = [[NSMenuItem alloc] initWithTitle:ts[i] action:@selector(pick:) keyEquivalent:ks[i]];
			it.keyEquivalentModifierMask = NSEventModifierFlagCommand;
			it.target = target;
			it.tag = i;
			it.enabled = (f & 2) == 0;
			[menu addItem:it];
		}
		// Gio's px are the view's backing pixels; the view isn't flipped
		CGFloat s = view.window.backingScaleFactor;
		if (s <= 0) {
			s = 1;
		}
		NSPoint at = NSMakePoint(x / s, view.frame.size.height - y / s);
		[menu popUpMenuPositioningItem:nil atLocation:at inView:view];
		clackNativeMenuDone(menuID, (int)target.picked);
	});
}

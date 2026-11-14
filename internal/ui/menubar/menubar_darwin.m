//go:build darwin && cgo

#import <AppKit/AppKit.h>
#import <objc/runtime.h>
#include "_cgo_export.h"
#include "menubar_darwin.h"

// The menu bar (menubar.go). Built and run on the main thread. Clack's own
// items target clackTarget and call back into Go, which only reads state or
// starts a goroutine; roles go down the responder chain (target nil).

static const NSInteger kRecentPlaceholder = (1 << 20) - 1; // tag of History's recent-list separator
static const NSInteger kRecentTag = 1 << 20;              // tag of its first entry

static void dump(NSMutableString *out, NSMenu *m, int depth);
static NSString *const kClackItem = @"clack"; // identifier of the items Clack made (AppKit adds its own)
static NSMutableDictionary<NSString *, NSArray *> *editRoles; // selector -> @[key, mods]

static NSEventModifierFlags maskOf(int mods) {
	NSEventModifierFlags m = 0;
	if (mods & 1) m |= NSEventModifierFlagCommand;
	if (mods & 2) m |= NSEventModifierFlagShift;
	if (mods & 4) m |= NSEventModifierFlagOption;
	if (mods & 8) m |= NSEventModifierFlagControl;
	return m;
}

// keyMatches: e is the key press that triggered item's key equivalent (not,
// say, Return in a keyboard-navigated menu).
static BOOL keyMatches(NSEvent *e, id sender) {
	if (e == nil || e.type != NSEventTypeKeyDown || ![sender isKindOfClass:[NSMenuItem class]]) {
		return NO;
	}
	NSMenuItem *it = sender;
	if (it.keyEquivalent.length == 0) {
		return NO;
	}
	NSEventModifierFlags want = NSEventModifierFlagCommand | NSEventModifierFlagOption | NSEventModifierFlagControl;
	if ((e.modifierFlags & want) != (it.keyEquivalentModifierMask & want)) {
		return NO;
	}
	NSString *c = e.charactersIgnoringModifiers;
	return c != nil && [c caseInsensitiveCompare:it.keyEquivalent] == NSOrderedSame;
}

static Class gioView(void) { return NSClassFromString(@"GioView"); }

// forwardKey hands e to the focused Gio view, as if no menu had matched it.
static BOOL forwardKey(NSEvent *e) {
	NSWindow *w = e.window != nil ? e.window : [NSApp keyWindow];
	NSResponder *fr = w.firstResponder;
	Class g = gioView();
	if (g == nil || ![fr isKindOfClass:g]) {
		return NO;
	}
	[fr keyDown:e];
	return YES;
}

@interface ClackMenuTarget : NSObject <NSMenuDelegate, NSMenuItemValidation>
@end

static ClackMenuTarget *clackTarget;

@implementation ClackMenuTarget
- (void)clackItem:(NSMenuItem *)sender {
	if (([sender.representedObject intValue] & CLACK_MI_FORWARD) && keyMatches([NSApp currentEvent], sender) && forwardKey([NSApp currentEvent])) {
		return;
	}
	clackMenuChoose((int)sender.tag);
}
- (void)clackRecent:(NSMenuItem *)sender {
	clackMenuRecentChoose((int)(sender.tag - kRecentTag));
}
// File > Close: Electron's close role closes the focused window, as its
// red button does. The main window's close is MAIN_WINDOW_CLOSE, which
// hides it (Clack's runfromtray.go: the Dock icon brings it back), leaving
// full screen first, or quits when no workspace is signed in.
- (void)clackClose:(id)sender {
	NSWindow *w = [NSApp keyWindow];
	if (w == nil) {
		return;
	}
	[w performClose:sender];
}
- (BOOL)validateMenuItem:(NSMenuItem *)it {
	if (it.action == @selector(clackRecent:)) {
		return YES;
	}
	if (it.action == @selector(clackClose:)) {
		return [NSApp keyWindow] != nil;
	}
	char *t = clackMenuTitle((int)it.tag);
	if (t != NULL) {
		it.title = [NSString stringWithUTF8String:t];
		free(t);
	}
	return clackMenuEnabled((int)it.tag) != 0;
}
// History's recent views, after its placeholder separator.
- (void)menuNeedsUpdate:(NSMenu *)menu {
	for (NSInteger i = menu.numberOfItems - 1; i >= 0; i--) {
		if ([menu itemAtIndex:i].tag >= kRecentTag) {
			[menu removeItemAtIndex:i];
		}
	}
	NSInteger at = [menu indexOfItemWithTag:kRecentPlaceholder];
	if (at < 0) {
		return;
	}
	int n = clackMenuRecentBegin();
	[menu itemAtIndex:at].hidden = n == 0;
	for (int i = 0; i < n; i++) {
		char *t = clackMenuRecentTitle(i);
		NSString *s = t != NULL ? [NSString stringWithUTF8String:t] : @"";
		free(t);
		NSMenuItem *it = [[NSMenuItem alloc] initWithTitle:(s ?: @"") action:@selector(clackRecent:) keyEquivalent:@""];
		it.target = self;
		it.tag = kRecentTag + i;
		[menu insertItem:it atIndex:at + 1 + i];
	}
	// the menu's own items that State.Hidden hides (Show full history)
	for (NSMenuItem *it in menu.itemArray) {
		if (it.tag < kRecentPlaceholder && [it.identifier isEqualToString:kClackItem]) {
			it.hidden = clackMenuHidden((int)it.tag) != 0;
		}
	}
}
@end

// gioEdit is Gio's view's copy:, paste:, undo:, … (EditRoles). A key press
// goes to Gio unchanged; a click sends Gio the key its editors bind.
static void gioEdit(id self, SEL cmd, id sender) {
	NSEvent *e = [NSApp currentEvent];
	if (keyMatches(e, sender)) {
		[self keyDown:e];
		return;
	}
	NSArray *k = editRoles[NSStringFromSelector(cmd)];
	if (k == nil) {
		return;
	}
	NSString *key = k[0];
	NSEventModifierFlags m = maskOf([k[1] intValue]);
	unsigned short code = 0;
	if ([key isEqualToString:@"\x7f"]) { // forward delete: only over a selection, like Electron's delete role
		NSRange r = [(id<NSTextInputClient>)self selectedRange];
		if (r.location == NSNotFound || r.length == 0) {
			return;
		}
		unichar fd = NSDeleteFunctionKey;
		key = [NSString stringWithCharacters:&fd length:1];
		code = 117; // kVK_ForwardDelete
	} else {
		static NSDictionary *codes;
		if (codes == nil) {
			codes = @{@"a" : @0, @"z" : @6, @"x" : @7, @"c" : @8, @"v" : @9}; // kVK_ANSI_*
		}
		code = [codes[key] unsignedShortValue];
	}
	NSWindow *w = [self window];
	NSTimeInterval ts = [[NSProcessInfo processInfo] systemUptime];
	NSEvent *down = [NSEvent keyEventWithType:NSEventTypeKeyDown location:NSZeroPoint modifierFlags:m timestamp:ts
								 windowNumber:w.windowNumber context:nil characters:key charactersIgnoringModifiers:key isARepeat:NO keyCode:code];
	NSEvent *up = [NSEvent keyEventWithType:NSEventTypeKeyUp location:NSZeroPoint modifierFlags:m timestamp:ts
							   windowNumber:w.windowNumber context:nil characters:key charactersIgnoringModifiers:key isARepeat:NO keyCode:code];
	[self keyDown:down];
	[self keyUp:up];
}

void clack_menubar_add_edit_roles(void) {
	Class g = gioView();
	if (g == nil) {
		return;
	}
	for (NSString *s in editRoles) {
		class_addMethod(g, NSSelectorFromString(s), (IMP)gioEdit, "v@:@"); // no-op if Gio has one
	}
}

void clack_menubar_edit_role(const char *sel, const char *key, int mods) {
	NSString *s = [NSString stringWithUTF8String:sel];
	NSString *k = [NSString stringWithUTF8String:key];
	@synchronized([ClackMenuTarget class]) {
		if (editRoles == nil) {
			editRoles = [NSMutableDictionary dictionary];
		}
		editRoles[s] = @[ k, @(mods) ];
	}
}

static NSString *str(const char *s) { return s != NULL ? [NSString stringWithUTF8String:s] : @""; }

CFTypeRef clack_menubar_spec(const clack_mi *items, int n) {
	NSMutableArray *a = [NSMutableArray arrayWithCapacity:n];
	for (int i = 0; i < n; i++) {
		const clack_mi *it = &items[i];
		[a addObject:@{
			@"title" : str(it->title),
			@"key" : str(it->key),
			@"role" : str(it->role),
			@"mods" : @(it->mods),
			@"parent" : @(it->parent),
			@"flags" : @(it->flags),
			@"tag" : @(it->tag),
		}];
	}
	return CFBridgingRetain(a);
}

// The menus AppKit manages, from the last clack_menubar_build.
static NSMenu *builtServices, *builtWindows, *builtHelp;

// clack_menubar_build makes the menu bar from a spec (main thread).
CFTypeRef clack_menubar_build(CFTypeRef specRef) {
	builtServices = builtWindows = builtHelp = nil;
	NSArray *spec = (__bridge NSArray *)specRef;
	if (clackTarget == nil) {
		clackTarget = [ClackMenuTarget new];
	}
	NSMenu *bar = [[NSMenu alloc] initWithTitle:@"MainMenu"];
	NSMutableArray<NSMenuItem *> *made = [NSMutableArray arrayWithCapacity:spec.count];
	for (NSDictionary *d in spec) {
		int flags = [d[@"flags"] intValue];
		int parent = [d[@"parent"] intValue];
		NSString *title = d[@"title"], *role = d[@"role"];
		NSMenuItem *it;
		if (flags & CLACK_MI_SEP) {
			it = [NSMenuItem separatorItem];
			if (flags & CLACK_MI_RECENT) {
				it.tag = kRecentPlaceholder;
				it.hidden = YES;
			} else {
				it.tag = [d[@"tag"] intValue]; // State.Hidden can hide it
			}
		} else {
			it = [[NSMenuItem alloc] initWithTitle:title action:nil keyEquivalent:d[@"key"]];
			it.keyEquivalentModifierMask = maskOf([d[@"mods"] intValue]);
		}
		it.identifier = kClackItem;
		if (flags & CLACK_MI_MENU) {
			NSMenu *m = [[NSMenu alloc] initWithTitle:title];
			it.submenu = m;
			if ([role isEqualToString:@"window"]) {
				builtWindows = m;
			} else if ([role isEqualToString:@"help"]) {
				builtHelp = m;
			}
			[bar addItem:it];
			[made addObject:it];
			continue;
		}
		if (!(flags & CLACK_MI_SEP)) {
			it.tag = [d[@"tag"] intValue];
			if ([role isEqualToString:@"services"]) {
				it.submenu = [[NSMenu alloc] initWithTitle:title];
				builtServices = it.submenu;
			} else if (flags & CLACK_MI_SUB) {
				it.submenu = [[NSMenu alloc] initWithTitle:title];
			} else if ([role hasPrefix:@"clack"]) {
				it.action = NSSelectorFromString(role);
				it.target = clackTarget;
			} else if (role.length > 0) {
				it.action = NSSelectorFromString(role); // target nil: the responder chain
			} else {
				it.action = @selector(clackItem:);
				it.target = clackTarget;
				it.representedObject = @(flags);
			}
		}
		NSMenu *into = made[parent].submenu;
		if (flags & CLACK_MI_RECENT) {
			into.delegate = clackTarget;
		}
		[into addItem:it];
		[made addObject:it];
	}
	return CFBridgingRetain(bar);
}

// installNow makes spec NSApp's menu bar (main thread).
static void installNow(NSArray *spec) {
	NSMenu *bar = CFBridgingRelease(clack_menubar_build((__bridge CFTypeRef)spec));
	clack_menubar_add_edit_roles();
	[NSApp setMainMenu:bar];
	if (builtServices != nil) {
		[NSApp setServicesMenu:builtServices];
	}
	if (builtWindows != nil) {
		[NSApp setWindowsMenu:builtWindows];
	}
	if (builtHelp != nil) {
		[NSApp setHelpMenu:builtHelp];
	}
}

void clack_menubar_install(CFTypeRef specRef) {
	if (specRef == NULL) {
		return;
	}
	NSArray *spec = CFBridgingRelease(specRef);
	dispatch_async(dispatch_get_main_queue(), ^{
		installNow(spec);
	});
}

// clack_menubar_install_now installs spec right away and describes NSApp's
// menu bar and its windows, help and services menus (clack_menubar_dump).
// Main thread only; for tests.
char *clack_menubar_install_now(CFTypeRef specRef) {
	NSArray *spec = CFBridgingRelease(specRef);
	[NSApplication sharedApplication];
	installNow(spec);
	NSMutableString *out = [NSMutableString string];
	dump(out, [NSApp mainMenu], 0);
	[out appendFormat:@"windowsMenu=%@ helpMenu=%@ servicesMenu=%@\n", [NSApp windowsMenu].title, [NSApp helpMenu].title,
		[NSApp servicesMenu].title];
	return strdup(out.UTF8String);
}

static void dump(NSMutableString *out, NSMenu *m, int depth) {
	Class g = gioView();
	for (NSMenuItem *it in m.itemArray) {
		if (![it.identifier isEqualToString:kClackItem]) {
			continue;
		}
		for (int i = 0; i < depth; i++) {
			[out appendString:@"\t"];
		}
		if (it.isSeparatorItem) {
			[out appendFormat:@"---%@\n", it.hidden ? @" hidden" : @""];
			continue;
		}
		NSEventModifierFlags f = it.keyEquivalentModifierMask;
		NSString *mods = [NSString stringWithFormat:@"%@%@%@%@",
			(f & NSEventModifierFlagControl) ? @"⌃" : @"", (f & NSEventModifierFlagOption) ? @"⌥" : @"",
			(f & NSEventModifierFlagShift) ? @"⇧" : @"", (f & NSEventModifierFlagCommand) ? @"⌘" : @""];
		NSString *act = it.action != nil ? NSStringFromSelector(it.action) : @"";
		NSString *tgt = it.target != nil ? NSStringFromClass([it.target class]) : @"nil";
		NSString *impl = @"";
		if (it.submenu != nil) { // AppKit's own submenuAction: opens it
			act = @"";
			tgt = @"nil";
		} else if (it.action != nil && it.target == nil) {
			BOOL ok = [NSApplication instancesRespondToSelector:it.action] || [NSWindow instancesRespondToSelector:it.action] ||
					  (g != nil && [g instancesRespondToSelector:it.action]);
			impl = ok ? @" ok" : @" MISSING";
		} else if (it.action != nil) {
			impl = [it.target respondsToSelector:it.action] ? @" ok" : @" MISSING";
		}
		NSString *key = it.keyEquivalent.length > 0 ? [mods stringByAppendingString:it.keyEquivalent.uppercaseString] : @"";
		[out appendFormat:@"%@|%@|%@|%@%@%@\n", it.title, key, act, tgt, impl, it.submenu != nil ? @" >" : @""];
		if (it.submenu != nil && it.submenu != [NSApp servicesMenu]) { // AppKit fills Services
			dump(out, it.submenu, depth + 1);
		}
	}
}

// clack_menubar_dump describes the menus Clack made, one item per line:
// depth tabs, then title|key equivalent|action|target, whether the action
// is implemented (by the target, or for the responder chain by
// NSApplication, NSWindow or Gio's view) and ">" for a submenu. For tests;
// malloc'd.
char *clack_menubar_dump(CFTypeRef menuRef) {
	NSMutableString *out = [NSMutableString string];
	dump(out, (__bridge NSMenu *)menuRef, 0);
	return strdup(out.UTF8String);
}

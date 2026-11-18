// SPDX-License-Identifier: Unlicense OR MIT

//go:build darwin && !ios

#import <AppKit/AppKit.h>
#include <stdbool.h>
#if __MAC_OS_X_VERSION_MAX_ALLOWED >= 150000
#import <Accessibility/Accessibility.h>
#endif

#include "_cgo_export.h"

// Clack patch (CLACK_PATCHES.md §47): the caret's blink period as
// Chromium's OsSettingsProviderMac::CaretBlinkInterval has it
// (ui/native_theme/os_settings_provider_mac.mm, main 2026-11-17): no
// blinking when the user asked for a non-blinking insertion point (macOS
// 15's AXPrefersNonBlinkingTextInsertionIndicator), else what AppKit's
// NSTextView blinks at (ui/base/cocoa/defaults_utils.mm,
// TextInsertionCaretBlinkPeriodFromDefaults): the average of
// NSTextInsertionPointBlinkPeriodOn and ...Off when either is set, none
// (0) when the on period is over a minute, the default (-1) when neither
// is set or either is negative.
int64_t gio_caretBlinkPeriodMs(void) {
	@autoreleasepool {
#if __MAC_OS_X_VERSION_MAX_ALLOWED >= 150000
		if (@available(macOS 15.0, *)) {
			if (AXPrefersNonBlinkingTextInsertionIndicator()) {
				return 0;
			}
		}
#endif
		NSUserDefaults *defaults = [NSUserDefaults standardUserDefaults];
		NSInteger on = [defaults integerForKey:@"NSTextInsertionPointBlinkPeriodOn"];
		NSInteger off = [defaults integerForKey:@"NSTextInsertionPointBlinkPeriodOff"];
		if ((on == 0 && off == 0) || on < 0 || off < 0) {
			return -1;
		}
		if (on > 60 * 1000) {
			return 0;
		}
		return (int64_t)((on + off) / 2);
	}
}

static id caretActiveObserver;
#if __MAC_OS_X_VERSION_MAX_ALLOWED >= 150000
static id caretAXObserver;
#endif

// gio_observeCaretBlink reads the period again as Chromium does: when the
// app becomes active (the user may have changed it in System Settings)
// and when the non-blinking preference changes.
void gio_observeCaretBlink(void) {
	@autoreleasepool {
		NSNotificationCenter *c = [NSNotificationCenter defaultCenter];
		caretActiveObserver = [c addObserverForName:NSApplicationWillBecomeActiveNotification
											 object:nil
											  queue:nil
										 usingBlock:^(NSNotification *n) {
											 gio_onCaretBlinkChanged();
										 }];
#if __MAC_OS_X_VERSION_MAX_ALLOWED >= 150000
		if (@available(macOS 15.0, *)) {
			caretAXObserver = [c addObserverForName:AXPrefersNonBlinkingTextInsertionIndicatorDidChangeNotification
											 object:nil
											  queue:nil
										 usingBlock:^(NSNotification *n) {
											 gio_onCaretBlinkChanged();
										 }];
		}
#endif
	}
}

// gio_clackCaretDefaults sets the two periods in the process's own
// defaults (the application domain, over NSGlobalDomain), or removes them,
// and posts the notification the period is read again on (tests).
void gio_clackCaretDefaults(bool setOn, int64_t on, bool setOff, int64_t off, bool post) {
	@autoreleasepool {
		NSUserDefaults *d = [NSUserDefaults standardUserDefaults];
		if (setOn) {
			[d setInteger:(NSInteger)on forKey:@"NSTextInsertionPointBlinkPeriodOn"];
		} else {
			[d removeObjectForKey:@"NSTextInsertionPointBlinkPeriodOn"];
		}
		if (setOff) {
			[d setInteger:(NSInteger)off forKey:@"NSTextInsertionPointBlinkPeriodOff"];
		} else {
			[d removeObjectForKey:@"NSTextInsertionPointBlinkPeriodOff"];
		}
		if (post) {
			[[NSNotificationCenter defaultCenter] postNotificationName:NSApplicationWillBecomeActiveNotification object:NSApp];
		}
	}
}

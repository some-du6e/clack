//go:build darwin && cgo

package desktop

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation
#import <Foundation/Foundation.h>
#include <stdlib.h>

// macos-notification-state 2.0.2 do-not-disturb.mm's version split, and
// its Big Sur (dnd_prefs plist in com.apple.ncprefs) and older
// (com.apple.notificationcenterui doNotDisturb) checks; macOS 12 and
// later is montereyDND in dnd.go.
static int clack_macos_major(void) {
	NSOperatingSystemVersion v = [[NSProcessInfo processInfo] operatingSystemVersion];
	if (v.majorVersion == 10 && v.minorVersion > 15) return 11;
	return (int)v.majorVersion;
}

static int clack_dnd_old(void) {
	@try {
		NSUserDefaults *d = [[NSUserDefaults alloc] initWithSuiteName:@"com.apple.notificationcenterui"];
		int on = [[d objectForKey:@"doNotDisturb"] boolValue] ? 1 : 0;
		[d release];
		return on;
	} @catch (NSException *e) {
		return 0;
	}
}

static int clack_dnd_bigsur(void) {
	@try {
		NSUserDefaults *d = [[NSUserDefaults alloc] initWithSuiteName:@"com.apple.ncprefs"];
		NSData *data = [d dataForKey:@"dnd_prefs"];
		[d release];
		if (!data) return 0;
		NSDictionary *dict = [NSPropertyListSerialization propertyListWithData:data options:NSPropertyListImmutable format:nil error:nil];
		if (!dict) return 0;
		NSDictionary *user = [dict valueForKey:@"userPref"];
		if (user && [[user valueForKey:@"enabled"] intValue] == 1) return 1;
		NSDictionary *sched = [dict valueForKey:@"scheduledTime"];
		if (sched) {
			NSNumber *start = [sched valueForKey:@"start"], *end = [sched valueForKey:@"end"];
			if ([[sched valueForKey:@"enabled"] intValue] == 1 && start && end) {
				NSDateComponents *c = [[NSCalendar currentCalendar] components:(NSCalendarUnitHour | NSCalendarUnitMinute) fromDate:[NSDate date]];
				NSInteger cur = [c hour] * 60 + [c minute], s = [start intValue], e = [end intValue];
				if (s < e && cur >= s && cur < e) return 1;
				if (e < s && (cur >= s || cur < e)) return 1;
			}
		}
		return 0;
	} @catch (NSException *e) {
		return 0;
	}
}

static char *clack_bundle_id(void) {
	NSString *b = [[NSBundle mainBundle] bundleIdentifier];
	return b ? strdup([b UTF8String]) : NULL;
}
*/
import "C"

import (
	"os"
	"time"
	"unsafe"
)

// DoNotDisturb reports whether macOS Do Not Disturb or a Focus mode
// silences Clack now (Slack's getDoNotDisturb before a Dock bounce).
func DoNotDisturb() bool {
	switch major := int(C.clack_macos_major()); {
	case major == 11:
		return C.clack_dnd_bigsur() == 1
	case major >= 12:
		home, err := os.UserHomeDir()
		if err != nil {
			return false
		}
		bid := ""
		if p := C.clack_bundle_id(); p != nil {
			bid = C.GoString(p)
			C.free(unsafe.Pointer(p))
		}
		return montereyDND(home, bid, time.Now())
	default:
		return C.clack_dnd_old() == 1
	}
}

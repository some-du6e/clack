package desktop

/*
#cgo LDFLAGS: -framework CoreGraphics -framework CoreFoundation
#include <CoreGraphics/CoreGraphics.h>
#include <CoreFoundation/CoreFoundation.h>

// clackIdleSeconds is Chromium's ui::CalculateIdleTime on macOS
// (idle_mac.mm), what powerMonitor.getSystemIdleTime answers: the
// session's seconds since any input event.
static double clackIdleSeconds(void) {
	return CGEventSourceSecondsSinceLastEventType(kCGEventSourceStateCombinedSessionState, kCGAnyInputEventType);
}

// clackScreenLocked is the session's CGSSessionScreenIsLocked: the screen
// locked (the lock-screen Electron hears as com.apple.screenIsLocked).
static int clackScreenLocked(void) {
	CFDictionaryRef d = CGSessionCopyCurrentDictionary();
	if (d == NULL) {
		return 0;
	}
	int locked = 0;
	CFTypeRef v = CFDictionaryGetValue(d, CFSTR("CGSSessionScreenIsLocked"));
	if (v != NULL && CFGetTypeID(v) == CFBooleanGetTypeID()) {
		locked = CFBooleanGetValue((CFBooleanRef)v) ? 1 : 0;
	}
	CFRelease(d);
	return locked;
}
*/
import "C"

// SystemIdleTime is powerMonitor.getSystemIdleTime: whole seconds since
// the session's last keyboard, mouse or trackpad input.
func SystemIdleTime() int { return int(C.clackIdleSeconds()) }

// ScreenLocked reports whether the session's screen is locked
// (powerMonitor's lock-screen and unlock-screen, read when asked).
func ScreenLocked() bool { return C.clackScreenLocked() != 0 }

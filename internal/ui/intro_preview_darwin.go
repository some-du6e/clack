//go:build darwin && cgo

package ui

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>

// NSApp is nil until something calls [NSApplication sharedApplication];
// reading it creates nothing. NSRunningApplication reads the process's
// standing with the window server without making it an app.
static int clack_pv_nsapp(void) { return NSApp != nil; }
static int clack_pv_policy(void) { return (int)[[NSRunningApplication currentApplication] activationPolicy]; }
static int clack_pv_active(void) { return [[NSRunningApplication currentApplication] isActive] ? 1 : 0; }
*/
import "C"

// appKitReport is the renderer's standing with AppKit (tests: -report): it
// must never become a regular app with a Dock icon, or activate.
func appKitReport() *appKitState {
	return &appKitState{NSApp: C.clack_pv_nsapp() != 0, Policy: int(C.clack_pv_policy()), Active: C.clack_pv_active() != 0}
}

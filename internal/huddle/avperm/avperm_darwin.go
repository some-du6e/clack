//go:build darwin && cgo

package avperm

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AVFoundation -framework Foundation
#include <stdint.h>
int clack_avperm_status(int video);
void clack_avperm_request(int video, uintptr_t h);
*/
import "C"

import "runtime/cgo"

func check(m Media) Status { return fromC(C.clack_avperm_status(cint(m))) }

func cint(m Media) C.int {
	if m == Camera {
		return 1
	}
	return 0
}

// fromC maps clack_avperm_status: AVAuthorizationStatus 0..3, or -1 for a
// bundle without the usage description.
func fromC(v C.int) Status {
	switch v {
	case 0:
		return NotDetermined
	case 1:
		return Restricted
	case 2:
		return Denied
	case 3:
		return Authorized
	}
	return NoUsageString
}

func request(m Media, ch chan Status) {
	C.clack_avperm_request(cint(m), C.uintptr_t(cgo.NewHandle(func() { ch <- check(m) })))
}

// clackAVPermDone runs on AVFoundation's completion queue (any thread): it
// only re-reads the status into the buffered channel.
//
//export clackAVPermDone
func clackAVPermDone(h C.uintptr_t) {
	hd := cgo.Handle(h)
	f := hd.Value().(func())
	hd.Delete()
	f()
}

func settingsURL(m Media) string {
	if m == Camera {
		return "x-apple.systempreferences:com.apple.preference.security?Privacy_Camera"
	}
	return "x-apple.systempreferences:com.apple.preference.security?Privacy_Microphone"
}

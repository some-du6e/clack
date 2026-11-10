//go:build darwin && cgo

package screenlock

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Foundation
#include <stdint.h>
void *clack_screenlock_observe(uintptr_t h);
void clack_screenlock_stop(void *obs);
*/
import "C"

import "runtime/cgo"

// The distributed notifications arrive on the main run loop (Gio's NSApp).
func watch(send func(bool)) (func(), error) {
	h := cgo.NewHandle(send)
	obs := C.clack_screenlock_observe(C.uintptr_t(h))
	return func() {
		C.clack_screenlock_stop(obs)
		h.Delete()
	}, nil
}

//export clackScreenLocked
func clackScreenLocked(h C.uintptr_t, locked C.int) {
	cgo.Handle(h).Value().(func(bool))(locked != 0)
}

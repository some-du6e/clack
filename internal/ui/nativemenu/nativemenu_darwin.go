//go:build darwin && cgo

package nativemenu

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit
#include <stdlib.h>
#include <stdint.h>

void clack_nativemenu_popup(uintptr_t view, int id, int n, const char **titles, const char **keys, const int *flags, double x, double y);
*/
import "C"

import (
	"image"
	"sync"
	"unsafe"

	"gioui.org/app"
	"gioui.org/io/event"
)

func noteEvent(ev event.Event) {
	if e, ok := ev.(app.AppKitViewEvent); ok && e.View != 0 {
		mu.Lock()
		view = e.View
		mu.Unlock()
	}
}

func supported() bool { return Fake != nil || view != 0 }

var (
	waitMu  sync.Mutex
	waiting = map[int]func(int){}
	nextID  int
)

func popup(v uintptr, _ func(func()), items []Item, pt image.Point, done func(int)) {
	if Fake != nil {
		done(Fake(items, pt))
		return
	}
	waitMu.Lock()
	nextID++
	id := nextID
	waiting[id] = done
	waitMu.Unlock()
	n := len(items)
	titles := C.malloc(C.size_t(n) * C.size_t(unsafe.Sizeof(uintptr(0))))
	keys := C.malloc(C.size_t(n) * C.size_t(unsafe.Sizeof(uintptr(0))))
	flags := C.malloc(C.size_t(n) * C.size_t(unsafe.Sizeof(C.int(0))))
	ts := unsafe.Slice((**C.char)(titles), n)
	ks := unsafe.Slice((**C.char)(keys), n)
	fs := unsafe.Slice((*C.int)(flags), n)
	for i, it := range items {
		ts[i] = C.CString(it.Label)
		ks[i] = C.CString(it.Key)
		var f C.int
		if it.Sep {
			f |= 1
		}
		if it.Disabled {
			f |= 2
		}
		fs[i] = f
	}
	// The .m copies the strings before it returns; the menu runs later
	// on the main thread.
	C.clack_nativemenu_popup(C.uintptr_t(v), C.int(id), C.int(n), (**C.char)(titles), (**C.char)(keys), (*C.int)(flags), C.double(pt.X), C.double(pt.Y))
	for i := range items {
		C.free(unsafe.Pointer(ts[i]))
		C.free(unsafe.Pointer(ks[i]))
	}
	C.free(titles)
	C.free(keys)
	C.free(flags)
}

//export clackNativeMenuDone
func clackNativeMenuDone(id, pick C.int) {
	waitMu.Lock()
	done := waiting[int(id)]
	delete(waiting, int(id))
	waitMu.Unlock()
	if done != nil {
		go done(int(pick))
	}
}

//go:build linux && cgo && !nox11

package desktop

/*
#cgo LDFLAGS: -lX11
#include <stdlib.h>
#include <X11/Xlib.h>
#include <X11/Xatom.h>

static void clack_set_net_wm_icon(void *dpy, unsigned long win, unsigned long *data, int n) {
	Display *d = dpy;
	Atom icon = XInternAtom(d, "_NET_WM_ICON", False);
	XChangeProperty(d, (Window)win, icon, XA_CARDINAL, 32, PropModeReplace, (unsigned char *)data, n);
	XFlush(d);
}
*/
import "C"

import (
	"sync"
	"unsafe"

	"gioui.org/app"
	"gioui.org/io/event"
)

var x11Icon struct {
	once sync.Once
	data []C.ulong
	win  uintptr
}

// SetWindowIcon gives the window its icon where the platform takes one
// per window: X11's _NET_WM_ICON (Wayland compositors use the clack.desktop
// file that matches the app id instead) and the macOS Dock icon when Clack
// runs outside its .app bundle. Windows windows use the exe's icon
// resource #1. Pass every window event; pngs are icon sizes (32–256 px).
func SetWindowIcon(ev event.Event, pngs ...[]byte) {
	e, ok := ev.(app.X11ViewEvent)
	if !ok || !e.Valid() || e.Window == x11Icon.win {
		return
	}
	x11Icon.once.Do(func() {
		for _, v := range netWMIcon(decodeIcons(pngs)) {
			x11Icon.data = append(x11Icon.data, C.ulong(v)) // format 32 = C long per item
		}
	})
	if len(x11Icon.data) == 0 {
		return
	}
	x11Icon.win = e.Window
	// Gio calls XInitThreads, so Xlib may be used from this goroutine. The
	// buffer is copied into a C array: XChangeProperty keeps no pointer.
	n := len(x11Icon.data)
	buf := (*C.ulong)(C.malloc(C.size_t(n) * C.size_t(unsafe.Sizeof(C.ulong(0)))))
	defer C.free(unsafe.Pointer(buf))
	copy(unsafe.Slice(buf, n), x11Icon.data)
	C.clack_set_net_wm_icon(e.Display, C.ulong(e.Window), buf, C.int(n))
}

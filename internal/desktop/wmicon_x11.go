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
	"image"
	"sync"
	"unsafe"

	"gioui.org/app"
	"gioui.org/io/event"
)

var x11Icon struct {
	mu       sync.Mutex
	defaults []C.ulong // Clack's icons, packed once
	custom   []C.ulong // SetAppIcon's, nil for Clack's own
	dpy      unsafe.Pointer
	win      uintptr // the main window (0 once it's going)
}

// SetWindowIcon gives the window its icon where the platform takes one
// per window: X11's _NET_WM_ICON (Wayland compositors use the clack.desktop
// file that matches the app id instead) and the macOS Dock icon when Clack
// runs outside its .app bundle. Windows windows use the exe's icon
// resource #1. Pass every window event; pngs are icon sizes (32–256 px).
func SetWindowIcon(ev event.Event, pngs ...[]byte) {
	e, ok := ev.(app.X11ViewEvent)
	if !ok {
		return
	}
	x11Icon.mu.Lock()
	defer x11Icon.mu.Unlock()
	if !e.Valid() {
		// the window is about to go (Gio's shutdown): SetAppIcon leaves it
		// alone, as a property on a destroyed window is Xlib's BadWindow
		x11Icon.dpy, x11Icon.win = nil, 0
		return
	}
	if e.Window == x11Icon.win {
		return
	}
	if x11Icon.defaults == nil {
		x11Icon.defaults = packIcons(decodeIcons(pngs))
	}
	x11Icon.dpy, x11Icon.win = e.Display, e.Window
	applyX11IconLocked()
}

// SetAppIcon is the window's icon from now on (CustomAppIcon: Slick's
// window setIcon on Linux): png, a PNG, or Clack's own (defaults, the
// icon sizes SetWindowIcon is given) for nil. On Wayland, where the
// compositor takes the app's icon from its .desktop file, it changes
// nothing, as Electron's setIcon doesn't there.
func SetAppIcon(png []byte, defaults ...[]byte) {
	x11Icon.mu.Lock()
	defer x11Icon.mu.Unlock()
	if x11Icon.defaults == nil && len(defaults) > 0 {
		x11Icon.defaults = packIcons(decodeIcons(defaults))
	}
	x11Icon.custom = nil
	if png != nil {
		if icons := decodeIcons([][]byte{png}); len(icons) > 0 {
			x11Icon.custom = packIcons(icons)
		}
	}
	applyX11IconLocked()
}

func packIcons(icons []*image.NRGBA) []C.ulong {
	var out []C.ulong
	for _, v := range netWMIcon(icons) {
		out = append(out, C.ulong(v)) // format 32 = C long per item
	}
	return out
}

func applyX11IconLocked() {
	data := x11Icon.custom
	if data == nil {
		data = x11Icon.defaults
	}
	if x11Icon.win == 0 || len(data) == 0 {
		return
	}
	// Gio calls XInitThreads, so Xlib may be used from this goroutine. The
	// buffer is copied into a C array: XChangeProperty keeps no pointer.
	n := len(data)
	buf := (*C.ulong)(C.malloc(C.size_t(n) * C.size_t(unsafe.Sizeof(C.ulong(0)))))
	defer C.free(unsafe.Pointer(buf))
	copy(unsafe.Slice(buf, n), data)
	C.clack_set_net_wm_icon(x11Icon.dpy, C.ulong(x11Icon.win), buf, C.int(n))
}

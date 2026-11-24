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

// the most a request may be, in 4-byte units (BIG-REQUESTS' when the
// server has it)
static long clack_max_request(void *dpy) {
	long n = XExtendedMaxRequestSize((Display *)dpy);
	return n > 0 ? n : XMaxRequestSize((Display *)dpy);
}
*/
import "C"

import (
	"image"
	"sort"
	"sync"
	"unsafe"

	"gioui.org/app"
	"gioui.org/io/event"
)

var x11Icon struct {
	mu       sync.Mutex
	defaults [][]C.ulong // Clack's icons, packed once, one per size
	custom   [][]C.ulong // SetAppIcon's, nil for Clack's own
	dpy      unsafe.Pointer
	win      uintptr // the main window (0 once it's going)
	// dirty: the icon changed since it was last set on the window. Only
	// SetWindowIcon sets it, on the UI goroutine, which runs Gio's X11
	// loop: nothing else uses Gio's display, and nothing touches it once
	// Gio's shutdown has closed it
	dirty bool
}

// maxX11Icon is the largest _NET_WM_ICON size set for an icon a plugin
// gives (CustomAppIcon fits its own to 256 px); each smaller size down to
// 16 px goes with it, as a window manager picks the size it draws.
const maxX11Icon = 256

// SetWindowIcon gives the window its icon where the platform takes one
// per window: X11's _NET_WM_ICON (Wayland compositors use the clack.desktop
// file that matches the app id instead) and the macOS Dock icon when Clack
// runs outside its .app bundle. Windows windows use the exe's icon
// resource #1. Pass every window event (on the goroutine that reads them);
// pngs are icon sizes (32–512 px). An icon SetAppIcon changed is set here,
// at the next window event.
func SetWindowIcon(ev event.Event, pngs ...[]byte) {
	x11Icon.mu.Lock()
	defer x11Icon.mu.Unlock()
	switch e := ev.(type) {
	case app.X11ViewEvent:
		if !e.Valid() {
			// the window is going (Gio's shutdown): its display is closed
			// or about to be, and nothing is set on it again
			x11Icon.dpy, x11Icon.win = nil, 0
			return
		}
		if e.Window != x11Icon.win {
			if x11Icon.defaults == nil {
				x11Icon.defaults = packIcons(decodeIcons(pngs))
			}
			x11Icon.dpy, x11Icon.win, x11Icon.dirty = e.Display, e.Window, true
		}
	case app.DestroyEvent:
		x11Icon.dpy, x11Icon.win = nil, 0
		return
	}
	if x11Icon.dirty && x11Icon.win != 0 {
		x11Icon.dirty = false
		applyX11IconLocked()
	}
}

// SetAppIcon is the window's icon from now on (CustomAppIcon: Slick's
// window setIcon on Linux): png, a PNG, or Clack's own (defaults, the
// icon sizes SetWindowIcon is given) for nil. It's set at the window's
// next event (SetWindowIcon), so the caller asks for one. On Wayland,
// where the compositor takes the app's icon from its .desktop file, it
// changes nothing, as Electron's setIcon doesn't there.
func SetAppIcon(png []byte, defaults ...[]byte) {
	var custom [][]C.ulong
	if png != nil {
		if icons := decodeIcons([][]byte{png}); len(icons) > 0 {
			custom = packIcons(iconSizes(icons[0], maxX11Icon))
		}
	}
	x11Icon.mu.Lock()
	defer x11Icon.mu.Unlock()
	if x11Icon.defaults == nil && len(defaults) > 0 {
		x11Icon.defaults = packIcons(decodeIcons(defaults))
	}
	x11Icon.custom, x11Icon.dirty = custom, true
}

// packIcons is each icon as _NET_WM_ICON lists it (netWMIcon), smallest
// first.
func packIcons(icons []*image.NRGBA) [][]C.ulong {
	sort.Slice(icons, func(i, j int) bool { return icons[i].Bounds().Dx() < icons[j].Bounds().Dx() })
	out := make([][]C.ulong, 0, len(icons))
	for _, m := range icons {
		var one []C.ulong
		for _, v := range netWMIcon([]*image.NRGBA{m}) {
			one = append(one, C.ulong(v)) // format 32 = C long per item
		}
		out = append(out, one)
	}
	return out
}

func applyX11IconLocked() {
	icons := x11Icon.custom
	if icons == nil {
		icons = x11Icon.defaults
	}
	if x11Icon.win == 0 || len(icons) == 0 {
		return
	}
	// the property in one ChangeProperty request: its header is 6 units
	// (7 as a big request), each CARDINAL one; past the server's limit
	// the request is refused (BadLength, which Xlib's default handler
	// ends Clack for), so the largest sizes are left out until it fits
	room := int(C.clack_max_request(x11Icon.dpy)) - 7
	n := 0
	keep := 0
	for _, one := range icons {
		if n+len(one) > room {
			break
		}
		n += len(one)
		keep++
	}
	if keep == 0 {
		return
	}
	// copied into a C array: XChangeProperty keeps no pointer
	buf := (*C.ulong)(C.malloc(C.size_t(n) * C.size_t(unsafe.Sizeof(C.ulong(0)))))
	defer C.free(unsafe.Pointer(buf))
	dst := unsafe.Slice(buf, n)
	at := 0
	for _, one := range icons[:keep] {
		at += copy(dst[at:], one)
	}
	C.clack_set_net_wm_icon(x11Icon.dpy, C.ulong(x11Icon.win), buf, C.int(n))
}

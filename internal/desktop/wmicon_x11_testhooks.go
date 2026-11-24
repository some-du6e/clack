//go:build linux && cgo && !nox11

package desktop

// For wmicon_x11_test.go, as cgo can't be in a test file: a display and a
// window of the test's own, _NET_WM_ICON read back, and the server's
// request limit.

/*
#cgo LDFLAGS: -lX11
#include <X11/Xlib.h>
#include <X11/Xatom.h>

static void *clack_test_display(void) { return XOpenDisplay(NULL); }
static unsigned long clack_test_window(void *dpy) {
	Display *d = dpy;
	return XCreateSimpleWindow(d, DefaultRootWindow(d), 0, 0, 16, 16, 0, 0, 0);
}
static void clack_test_close(void *dpy, unsigned long win) {
	XDestroyWindow((Display *)dpy, (Window)win);
	XCloseDisplay((Display *)dpy);
}
static long clack_test_icon(void *dpy, unsigned long win, unsigned long **out) {
	Display *d = dpy;
	Atom type;
	int format;
	unsigned long n = 0, after = 0;
	unsigned char *prop = NULL;
	XSync(d, False);
	if (XGetWindowProperty(d, (Window)win, XInternAtom(d, "_NET_WM_ICON", False), 0, 0x7fffffff, False,
			XA_CARDINAL, &type, &format, &n, &after, &prop) != Success || prop == NULL) {
		return 0;
	}
	*out = (unsigned long *)prop;
	return (long)n;
}
static long clack_test_max_request(void *dpy) {
	long n = XExtendedMaxRequestSize((Display *)dpy);
	return n > 0 ? n : XMaxRequestSize((Display *)dpy);
}
*/
import "C"

import "unsafe"

// x11TestWindow is a display and a window of a test's own (DISPLAY's
// server); ok is false without one.
func x11TestWindow() (dpy unsafe.Pointer, win uintptr, done func(), ok bool) {
	dpy = C.clack_test_display()
	if dpy == nil {
		return nil, 0, nil, false
	}
	win = uintptr(C.clack_test_window(dpy))
	return dpy, win, func() { C.clack_test_close(dpy, C.ulong(win)) }, true
}

// x11IconWords is win's _NET_WM_ICON as the server holds it.
func x11IconWords(dpy unsafe.Pointer, win uintptr) []uint64 {
	var p *C.ulong
	n := int(C.clack_test_icon(dpy, C.ulong(win), &p))
	if n == 0 || p == nil {
		return nil
	}
	defer C.XFree(unsafe.Pointer(p))
	out := make([]uint64, n)
	for i, v := range unsafe.Slice(p, n) {
		out[i] = uint64(v)
	}
	return out
}

// x11MaxRequest is the server's request limit in 4-byte units.
func x11MaxRequest(dpy unsafe.Pointer) int { return int(C.clack_test_max_request(dpy)) }

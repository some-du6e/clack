// SPDX-License-Identifier: Unlicense OR MIT

//go:build ((linux && !android) || freebsd || openbsd) && !nox11

package app

/*
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <X11/Xlib.h>
#include <X11/Xatom.h>

// clack_xsettings copies the XSETTINGS manager's _XSETTINGS_SETTINGS
// property on dpy's default screen into a malloc'd buffer (*n bytes), or
// gives NULL without a manager or the property. The server is grabbed
// while it looks (the XSETTINGS spec's way): the manager's window can't go
// between finding it and reading it, which would be a BadWindow error and,
// with Xlib's default handler, the process's end.
static unsigned char *clack_xsettings(Display *dpy, Atom sel, Atom prop, unsigned long *n) {
	unsigned char *data = NULL, *out = NULL;
	Atom type = None;
	int format = 0;
	unsigned long items = 0, after = 0;
	*n = 0;
	if (XGetSelectionOwner(dpy, sel) == None) {
		return NULL; // no manager: no grab
	}
	XGrabServer(dpy);
	Window owner = XGetSelectionOwner(dpy, sel);
	if (owner != None &&
		XGetWindowProperty(dpy, owner, prop, 0, LONG_MAX / 4, False, prop, &type, &format, &items, &after, &data) == Success &&
		type == prop && format == 8 && data != NULL && items > 0) {
		out = malloc(items);
		if (out != NULL) {
			memcpy(out, data, items);
			*n = items;
		}
	}
	XUngrabServer(dpy);
	XFlush(dpy);
	if (data != NULL) {
		XFree(data);
	}
	return out;
}

static Atom clack_xsettings_selection(Display *dpy) {
	char name[32];
	snprintf(name, sizeof name, "_XSETTINGS_S%d", DefaultScreen(dpy));
	return XInternAtom(dpy, name, False);
}
*/
import "C"

import (
	"sync"
	"unsafe"
)

// Clack patch (CLACK_PATCHES.md §47): X11's XSETTINGS, for GTK's caret
// settings as Chromium reads them (Clack's internal/ui caretblink_linux.go).

var clackXSettings struct {
	mu        sync.Mutex
	tried     bool
	dpy       *C.Display
	sel, prop C.Atom
}

// ClackXSettings is the X settings manager's settings on DISPLAY's default
// screen: its _XSETTINGS_SETTINGS property, in the XSETTINGS spec's
// format, or nil without a display, a manager or settings. The connection
// is its own, opened on the first call and kept; any goroutine. It may
// wait on the X server: not on the UI goroutine.
func ClackXSettings() []byte {
	x := &clackXSettings
	x.mu.Lock()
	defer x.mu.Unlock()
	if !x.tried {
		x.tried = true
		C.XInitThreads() // as Gio's windows do, before any connection
		if x.dpy = C.XOpenDisplay(nil); x.dpy != nil {
			x.sel = C.clack_xsettings_selection(x.dpy)
			prop := C.CString("_XSETTINGS_SETTINGS")
			x.prop = C.XInternAtom(x.dpy, prop, C.False)
			C.free(unsafe.Pointer(prop))
		}
	}
	if x.dpy == nil {
		return nil
	}
	var n C.ulong
	p := C.clack_xsettings(x.dpy, x.sel, x.prop, &n)
	if p == nil {
		return nil
	}
	defer C.free(unsafe.Pointer(p))
	return C.GoBytes(unsafe.Pointer(p), C.int(n))
}

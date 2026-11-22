// SPDX-License-Identifier: Unlicense OR MIT

//go:build ((linux && !android) || freebsd || openbsd) && !nox11

#include <stdlib.h>
#include <X11/Xlib.h>
#include "_cgo_export.h"

// gio_x11IOError is Xlib's IO error handler (Clack patch, CLACK_PATCHES.md
// §59): the connection to the X server broke. The program records why
// (X11ConnectionLost), then the process exits with status 1, as Xlib's
// default handler (_XDefaultIOError) exits: an IO error handler must not
// return.
static int gio_x11IOError(Display *dpy) {
	gio_onX11IOError();
	exit(1);
	return 0;
}

void gio_x11SetIOErrorHandler(void) {
	XSetIOErrorHandler(gio_x11IOError);
}

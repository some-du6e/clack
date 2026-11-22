// SPDX-License-Identifier: Unlicense OR MIT

//go:build ((linux && !android) || freebsd || openbsd) && !nox11

package app

import "C"

// gio_onX11IOError is called by Xlib's IO error handler (Clack patch,
// CLACK_PATCHES.md §59) before the process exits.
//
//export gio_onX11IOError
func gio_onX11IOError() {
	if f := X11ConnectionLost; f != nil {
		f()
	}
}

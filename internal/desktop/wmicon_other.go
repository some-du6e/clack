//go:build !windows && !(linux && cgo && !nox11) && !(darwin && cgo)

package desktop

import "gioui.org/io/event"

// SetWindowIcon is a no-op here: Wayland takes the window icon from the
// clack.desktop file.
func SetWindowIcon(ev event.Event, pngs ...[]byte) {}

// SetAppIcon is a no-op here (Wayland's icon is the .desktop file's, which
// Electron's setIcon doesn't change either).
func SetAppIcon(png []byte, defaults ...[]byte) {}

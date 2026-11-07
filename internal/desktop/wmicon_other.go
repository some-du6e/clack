//go:build !(linux && cgo && !nox11) && !(darwin && cgo)

package desktop

import "gioui.org/io/event"

// SetWindowIcon is a no-op here: Windows takes the window icon from the
// exe's icon resource #1, and Wayland from the clack.desktop file.
func SetWindowIcon(ev event.Event, pngs ...[]byte) {}

//go:build stockgio

package theme

import (
	"image"

	"gioui.org/io/event"
	"gioui.org/layout"
)

// Stock Gio v0.10.3 has no Source.AreaBounds (see gio_patch53.go): the
// origin trackers learn from pointer events alone.
func areaBounds(layout.Context, event.Tag) (image.Rectangle, bool) { return image.Rectangle{}, false }

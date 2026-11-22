//go:build stockgio

package richview

import (
	"image"

	"gioui.org/io/event"
	"gioui.org/layout"
)

// Stock Gio v0.10.3 can't say where an area was (see gio_patch57.go): a
// canvas card stays where its mention was while the pointer rests on it.
func areaOrigin(layout.Context, event.Tag) (image.Point, bool) { return image.Point{}, false }

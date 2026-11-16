//go:build stockgio

package theme

import (
	"image"

	"gioui.org/op/clip"
)

// Stock Gio v0.10.3 knows no clip interiors (see gio_patch38.go): a panel
// is filled in one piece.

func rrInterior(clip.RRect) image.Rectangle { return image.Rectangle{} }

//go:build !stockgio

package richview

import (
	"image"

	"gioui.org/io/event"
	"gioui.org/layout"
)

// Gio patches 53 and 57 (third_party/gioui.org/CLACK_PATCHES.md): where
// tag's area was in the last frame, in window coordinates, and a frame
// when the frame being laid out moves it. gio_patch57_stock.go stands in
// on stock Gio (.github/scripts/gio-layer-fix.sh builds this package
// against it).
func areaOrigin(gtx layout.Context, tag event.Tag) (image.Point, bool) {
	full, _, ok := gtx.Source.FollowArea(tag)
	return full.Min, ok
}

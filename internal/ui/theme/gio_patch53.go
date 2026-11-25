//go:build !stockgio

package theme

import (
	"image"

	"gioui.org/io/event"
	"gioui.org/layout"
)

// areaBounds is Clack patch 53's Source.AreaBounds: where the router put
// tag's area last frame (AreaOrigin).
func areaBounds(gtx layout.Context, tag event.Tag) (image.Rectangle, bool) {
	full, _, ok := gtx.Source.AreaBounds(tag)
	return full, ok
}

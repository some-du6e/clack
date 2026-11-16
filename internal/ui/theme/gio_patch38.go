//go:build !stockgio

package theme

import (
	"image"

	"gioui.org/op/clip"
)

// Gio patch 38 (a rounded clip's interior) entry point. The stockgio
// build tag swaps in no interior (gio_patch38_stock.go), see
// gio_patch12.go.

func rrInterior(rr clip.RRect) image.Rectangle { return rr.Interior() }

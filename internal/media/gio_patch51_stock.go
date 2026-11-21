//go:build stockgio

package media

import (
	"image"

	"gioui.org/op/paint"
)

// Stock Gio v0.10.3 has neither external images (gio_patch51.go) nor live
// ones: a player's op is its still, the poster.
const ExternalImages = false

func externalOp(h *FrameHandle, size image.Point, still paint.ImageOp) paint.ImageOp { return still }

func liveOp(l *fakeFrames, still paint.ImageOp) paint.ImageOp { return still }

// SPDX-License-Identifier: Unlicense OR MIT

package paint

import "image"

// Clack patch 35 (CLACK_PATCHES.md §35): an image whose pixels change
// between frames without new ops, so that a frame drawn again from the
// ops it was drawn with shows an animation where it is now.

// LiveImage is an image that changes between frames: an animation that
// plays.
type LiveImage interface {
	// Image is the ImageOp to draw now: always of the size it was when
	// the live ImageOp was made, and never a uniform colour. The GPU asks
	// for it while it draws a frame, once for every paint of it the
	// frame shows (none for one outside the viewport or its clip), on the
	// goroutine drawing the frame.
	Image() ImageOp
}

// NewLiveImageOp is an ImageOp drawing l's image as it is when a frame is
// drawn, not when the ops were written. Its size is l's image's now. An l
// with no image now is an empty ImageOp (it draws nothing).
func NewLiveImageOp(l LiveImage) ImageOp {
	op := l.Image()
	if op.uniform || op.src == nil {
		return op
	}
	return ImageOp{Filter: op.Filter, src: op.src, handle: liveHandle{l}}
}

// liveHandle is a live ImageOp's handle. The GPU asks it for the image to
// draw (gpu's texHandle), whose own handle keys the texture.
type liveHandle struct{ l LiveImage }

// LiveImage is the image to draw now and its texture's key.
func (h liveHandle) LiveImage() (*image.RGBA, any) {
	op := h.l.Image()
	if op.uniform || op.src == nil {
		return nil, nil
	}
	return op.src, op.handle
}

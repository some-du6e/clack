// SPDX-License-Identifier: Unlicense OR MIT

package driver

import "image"

// ClackPartial is a Device that can draw partial frames (Clack patch 46):
// scissor a render pass's draws and copy a texture into the frame's target.
type ClackPartial interface {
	// ClackPartialOK reports whether the device can draw partial frames
	// now.
	ClackPartialOK() bool
	// ClackScissor restricts the open render pass's following draws to r,
	// in the coordinates Viewport takes; an empty r lets them draw
	// everywhere again. A render pass starts and ends unscissored. The
	// scissor discards fragments and changes nothing in the ones it keeps.
	ClackScissor(r image.Rectangle)
	// ClackCopyToTarget copies size pixels of src, from the origin, into
	// target, the Texture BeginFrame returned for the frame's render
	// target: byte for byte, with no blending, scissor or conversion. src
	// has target's format (TextureFormatOutput). Outside a render pass.
	ClackCopyToTarget(target, src Texture, size image.Point)
	// ClackCopyDraws reports whether the frame is copied into the target
	// with a draw instead (Metal: a drawable is framebufferOnly, which a
	// blit can't write).
	ClackCopyDraws() bool
}

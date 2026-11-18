package ui

import "gioui.org/gpu"

// Partial frames (Gio patch 46): the window's GPU compares each frame with
// the last, op by op, and draws it again only where they differ (a caret's
// blink, a spinner's turn, an animated emoji's next frame), with the whole
// frame's pixels. CLACK_FULL_FRAMES=1 draws every frame whole, as Gio did:
// for a driver that gets them wrong, and for measuring.
func setFrameMode() {
	gpu.ClackPartialFrames(!envFlag("FULL_FRAMES"))
}

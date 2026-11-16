// SPDX-License-Identifier: Unlicense OR MIT

package input

// Clack patch 35 (CLACK_PATCHES.md §35): a frame that is the last frame's
// ops again.

// Replay says the frame in progress shows the last frame's ops again
// without laying anything out (Clack's quiet frames): no handler asks for
// events in it, so its Frame keeps the filters every handler asked for in
// the last frame (the pointer kinds it takes, whether it takes the focus,
// its key filters) instead of the empty ones nobody filled in. Without it
// the focused editor lost the focus at each such frame (keyQueue.Frame
// drops a focus whose tag asked for no focus events) and got it back at
// the next laid-out one, and an event right after one had no filter to be
// routed by. Call it before the frame's ops go to the window.
func (s Source) Replay() {
	if s.r != nil {
		s.r.replay = true
	}
}

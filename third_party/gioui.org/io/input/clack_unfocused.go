// SPDX-License-Identifier: Unlicense OR MIT

package input

import "gioui.org/io/event"

// Clack patch 64 (CLACK_PATCHES.md): typed text that no handler has the
// focus for goes to the handler the window named for it.
//
// A browser types a key's character into whatever has the focus once
// the keydown is through, so Slack's message input, listening for
// keydown on the document, focuses itself for a printable key no input
// had (shouldInputStealKeyEvent) and the character lands in it. Gio
// routes an EditEvent to the focused handler as it queues it and drops
// it with none, and a FocusCmd from the key's handler doesn't bring it
// back: the replay queues again only events that had a receiver. With
// nothing focused, EditEvents go to the tag SetUnfocusedText names,
// whose handler reads them through a key.FocusFilter as a focused editor
// does. The focus itself stays empty: Focused(nil) still holds.

// SetUnfocusedText names the handler for typed text while no handler has
// the focus; nil drops it again, as Gio does. A disabled Source sets
// nothing.
func (s Source) SetUnfocusedText(tag event.Tag) {
	if !s.Enabled() {
		return
	}
	s.r.unfocusedText = tag
}

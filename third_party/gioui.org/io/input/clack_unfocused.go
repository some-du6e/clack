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
// does, and so do CompositionEvents (an IME's or a dead key's marked
// text, which the handler leaves alone). The focus itself stays empty:
// Focused(nil) still holds.
//
// Only text that comes with nothing focused goes there, not text a
// command's replay finds unfocused: a handler that took a key and then
// dropped the focus (a list's E opening an edit, Escape) has had that
// key's text too, which Gio then drops as it always did. Slack's keydown
// handler that takes a key prevents its default, so the character lands
// nowhere and the input's document listener leaves it be.

// SetUnfocusedText names the handler for typed text while no handler has
// the focus; nil drops it again, as Gio does. A disabled Source sets
// nothing.
func (s Source) SetUnfocusedText(tag event.Tag) {
	if !s.Enabled() {
		return
	}
	s.r.unfocusedText = tag
}

// unfocusedTextFor is the handler an EditEvent or a CompositionEvent with
// no focused handler goes to: SetUnfocusedText's, unless the router is
// replaying the events a command's state change left (requeue), whose
// text a handler that had the focus took the key of.
func (q *Router) unfocusedTextFor() event.Tag {
	if q.keyLog.replay {
		return nil
	}
	return q.unfocusedText
}

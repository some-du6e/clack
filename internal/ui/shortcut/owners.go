package shortcut

import "gioui.org/io/event"

// Escape owners. The shell reads its global bindings first in every frame,
// and Gio hands a key event to the first Event call whose filter matches,
// so the global plain Escape ("Close / mark as read") would take the key
// from any focused widget that handles it itself (a Lists cell, its inline
// editor). A widget that wants Escape while it has the key focus calls
// OwnEscape(tag) every frame it is laid out; at the start of the next
// frame the shell takes the claims (TakeEscapeOwners) and leaves plain
// Escape alone while one of those tags is focused.
//
// UI goroutine only.

var escOwners []event.Tag

// OwnEscape claims plain Escape for tag while tag has the key focus.
func OwnEscape(tag event.Tag) {
	if tag != nil {
		escOwners = append(escOwners, tag)
	}
}

// TakeEscapeOwners returns the claims made since the last call and
// forgets them.
func TakeEscapeOwners() []event.Tag {
	o := escOwners
	escOwners = nil
	return o
}

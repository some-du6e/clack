// SPDX-License-Identifier: Unlicense OR MIT

package input

import (
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
)

// Clack patch 21 (CLACK_PATCHES.md): a Source that lets widgets see the
// pointer move but not act on it. Clack's inspect tool lays the window
// out with one while it owns the clicks and keys: whatever the pointer
// is over still shows its hover state (a message's toolbar, a tooltip),
// lists still scroll, carets, focus rings and animations stay as they
// are, and no button, editor or shortcut reacts.

// HoverOnly returns a copy of s whose Event reports only pointer Enter,
// Leave, Move, Scroll and Cancel events and focus changes. Presses,
// releases, drags, key and text input, and transfers are taken from the
// queue and dropped as the widget asks for them. Filters still reach the
// router, commands still run and Focused still answers, so the router's
// state (hover, focus) stays what the widgets believe it is.
func (s Source) HoverOnly() Source {
	s2 := s
	s2.hoverOnly = true
	return s2
}

// hoverEvent reports the events a HoverOnly Source delivers.
func hoverEvent(e event.Event) bool {
	switch e := e.(type) {
	case pointer.Event:
		switch e.Kind {
		case pointer.Enter, pointer.Leave, pointer.Move, pointer.Scroll, pointer.Cancel:
			return true
		}
	case key.FocusEvent:
		return true
	}
	return false
}

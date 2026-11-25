// SPDX-License-Identifier: Unlicense OR MIT

package app

import "gioui.org/io/pointer"

// Clack patch 68 (CLACK_PATCHES.md §68): on macOS a modal loop AppKit runs
// inside the window takes mouse-ups the view never sees. A native context
// menu (Clack's nativemenu, popUpMenuPositioningItem:) takes the release
// of the right-click that opened it. gio_onMouse kept the buttons it saw
// go down (pointerBtns), so ButtonSecondary stayed down: every later left
// press came as Primary|Secondary, a context click to Clack (ctxclick), and
// the router made every move a drag of the press it never saw end.
//
// staleButtons is the part of held, the buttons Gio has down, that AppKit
// has up: at a press of btn, those neither in appKit ([NSEvent
// pressedMouseButtons], as Chromium's ModifiersFromEvent reads an event's
// buttons, components/input/web_input_event_builders_mac.mm) nor btn
// itself (a quick tap has it up by the time its press is handled); at a
// mouseMoved: (moved), all of them, since AppKit sends that only with no
// button down. Drags keep what Gio saw: a quick drag's last moves can be
// handled after its button is up, and the release follows.
func staleButtons(held, btn, appKit pointer.Buttons, press, moved bool) pointer.Buttons {
	switch {
	case press:
		return held &^ (appKit | btn)
	case moved:
		return held
	}
	return 0
}

// appKitButtons is [NSEvent pressedMouseButtons]' mask as Buttons: bit 0
// the left button, 1 the right, 2 the middle.
func appKitButtons(mask uint64) pointer.Buttons {
	var b pointer.Buttons
	if mask&1 != 0 {
		b |= pointer.ButtonPrimary
	}
	if mask&2 != 0 {
		b |= pointer.ButtonSecondary
	}
	if mask&4 != 0 {
		b |= pointer.ButtonTertiary
	}
	return b
}

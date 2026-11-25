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
// staleButtons is the part of held, the buttons Gio has down, that has
// come up without Gio seeing it. At a press of btn: btn itself if Gio has
// it down already (AppKit never sends a button's down twice without its
// up between: its release was lost, as when the composer's menu took a
// right-click's, or a Ctrl+click menu the left button's), and every other
// button appKit ([NSEvent pressedMouseButtons], as Chromium's
// ModifiersFromEvent reads an event's buttons,
// components/input/web_input_event_builders_mac.mm) has up. At a
// mouseMoved: (moved), those appKit has up, as Blink's
// WebMouseEventBuilder takes a move's button from pressedMouseButtons;
// AppKit sends a mouseMoved: only with no button down, but a tool posting
// moves while a button is held keeps its drag. Drags keep what Gio saw: a
// quick drag's last moves can be handled after its button is up, and the
// release follows.
//
// pressedMouseButtons is the state when it's read, not when the event
// came: with the main thread a long frame behind, a press of one button
// handled after another's real release (still queued) cancels that
// button's gesture before its release ends it. Rare, and a chord's only.
func staleButtons(held, btn, appKit pointer.Buttons, press, moved bool) pointer.Buttons {
	switch {
	case press:
		return held &^ (appKit &^ btn)
	case moved:
		return held &^ appKit
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

// Package ctxclick tells context-menu presses apart from ordinary clicks.
// A right-click (or a two-finger tap on a Mac trackpad) arrives as a
// secondary-button press. On macOS a Ctrl+click is also a context click:
// AppKit delivers it as a primary press with Control held. Slack's client
// gets a contextmenu event for both and no click, so a Ctrl+click on a
// link or button opens the menu and doesn't activate the link or button.
package ctxclick

import (
	"runtime"

	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/widget"
)

// Mac reports whether Ctrl+primary counts as a context click. Tests set
// it to exercise the macOS path on other systems.
var Mac = runtime.GOOS == "darwin"

// Press reports whether e is a press that opens a context menu.
func Press(e pointer.Event) bool {
	if e.Kind != pointer.Press {
		return false
	}
	if e.Buttons.Contain(pointer.ButtonSecondary) {
		return true
	}
	return Mac && e.Buttons.Contain(pointer.ButtonPrimary) && e.Modifiers.Contain(key.ModCtrl)
}

// Primary reports whether e is an ordinary primary press: one that
// selects, drags or clicks, not a macOS Ctrl+click.
func Primary(e pointer.Event) bool {
	return e.Kind == pointer.Press && e.Buttons == pointer.ButtonPrimary && !Press(e)
}

// Context reports whether c, a completed click, was a macOS Ctrl+click
// (the press already opened a context menu).
func Context(c widget.Click) bool { return Mac && c.Modifiers.Contain(key.ModCtrl) }

// Clicked is btn.Clicked without the macOS Ctrl+clicks.
func Clicked(gtx layout.Context, btn *widget.Clickable) bool {
	clicked := false
	for {
		c, ok := btn.Update(gtx)
		if !ok {
			return clicked
		}
		if !Context(c) {
			clicked = true
		}
	}
}

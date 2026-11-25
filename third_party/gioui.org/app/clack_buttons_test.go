// SPDX-License-Identifier: Unlicense OR MIT

package app

import (
	"testing"

	"gioui.org/io/pointer"
)

// Clack patch 68: which of the buttons Gio has down AppKit has up, at a
// press, at a mouseMoved: and on the rest.
func TestClackStaleButtons(t *testing.T) {
	p, s, m := pointer.ButtonPrimary, pointer.ButtonSecondary, pointer.ButtonTertiary
	for _, c := range []struct {
		name              string
		held, btn, appKit pointer.Buttons
		press, moved      bool
		want              pointer.Buttons
	}{
		{"a left press after a right press whose release a menu took", s, p, p, true, false, s},
		{"that press handled after its button came up (a tap)", s, p, 0, true, false, s},
		{"a left press with the right button held", s, p, p | s, true, false, 0},
		{"a tap, nothing held", 0, p, 0, true, false, 0},
		{"a right press with the left button held", p, s, p | s, true, false, 0},
		{"a middle press after a left press a menu took the release of", p, m, m, true, false, p},
		{"a right press after a right press whose release the composer's menu took", s, s, s, true, false, s},
		{"that press handled after its button came up", s, s, 0, true, false, s},
		{"a left click after a Ctrl+click whose release its menu took", p, p, p, true, false, p},
		{"a mouseMoved: after a right press whose release a menu took", s, 0, 0, false, true, s},
		{"a mouseMoved:, nothing held", 0, 0, 0, false, true, 0},
		{"a mouseMoved: a tool posts with the left button held", p, 0, p, false, true, 0},
		{"a drag's last move, handled after its button came up", p, 0, 0, false, false, 0},
		{"a release", p, p, 0, false, false, 0},
	} {
		if got := staleButtons(c.held, c.btn, c.appKit, c.press, c.moved); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	if got := appKitButtons(1 | 2 | 4 | 8); got != p|s|m {
		t.Errorf("appKitButtons(0b1111) = %v, want the primary, secondary and tertiary buttons", got)
	}
}

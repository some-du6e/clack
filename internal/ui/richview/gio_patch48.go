//go:build !stockgio

package richview

import (
	"time"

	"gioui.org/gesture"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/widget"
)

// Gio patch 48 (third_party/gioui.org/CLACK_PATCHES.md): the platform's
// double-click time, and the window's click count, Chromium's on each
// platform (the router stamps every press and release with it,
// pointer.Event.Clicks), which a click's count follows here as Slack's
// event detail does. gio_patch48_stock.go stands in on stock Gio
// (.github/scripts/gio-layer-fix.sh builds this package against it).

func doubleClickTime() time.Duration { return gesture.DoubleClickTime() }

// clickCounter is a view's count of its presses: the window's.
type clickCounter struct{ run gesture.ClickRun }

func (c *clickCounter) press(q input.Source, e pointer.Event) int { return c.run.Press(q, e) }

// clicksOf is the window's count of the click that press or release e
// is part of (0 without one).
func clicksOf(e pointer.Event) int { return e.Clicks }

// chromiumWordRange is Gio's ClackWordRange: what a double click at pos of
// rs selects, the segment of ICU's word rules with Chromium's.
func chromiumWordRange(rs []rune, pos int, trailingSpace bool) (int, int) {
	return widget.ClackWordRange(rs, pos, trailingSpace)
}

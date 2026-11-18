// SPDX-License-Identifier: Unlicense OR MIT

package gesture

// Clack patch 48 (CLACK_PATCHES.md §48): a press counts on from the last
// one by the platform's rule (internal/clickrule), the way Chromium counts
// clicks there, instead of within a fixed 200 ms:
//
//   - Windows: GetDoubleClickTime, and half of SM_CXDOUBLECLK ×
//     SM_CYDOUBLECLK around the run's first press; a move out of it ends
//     the run (Blink's WebMouseEventBuilder::Build).
//   - macOS: AppKit's own count, NSEvent clickCount, which the backend
//     hands over in pointer.Event.Clicks (Chromium's mac builder passes it
//     on as is); presses without one go by NSEvent.doubleClickInterval.
//   - X11, Wayland and the rest: 500 ms and 2 px from the last press, the
//     same modifiers, the fourth click a single again (ui::MouseEvent's
//     GetRepeatCount, which Chromium's Linux backends use).

import (
	"time"

	"gioui.org/internal/clickrule"
	"gioui.org/io/pointer"
)

// DoubleClickTime is how long after a press the next one still counts on
// as a double (or triple) click on this platform now: the OS's setting
// where it has one.
func DoubleClickTime() time.Duration { return clickrule.Current().Interval }

// count sets clicks for press e: the platform's own count when the
// backend sends one, else the platform's rule from this gesture's last
// press.
func (c *Click) count(e pointer.Event) {
	if e.Clicks > 0 {
		c.clicks = e.Clicks
	} else {
		r := clickrule.Current()
		ref := c.clickedPos
		if r.FromFirst {
			ref = c.runPos
		}
		if c.clicks > 0 && !c.runBroken && e.Time-c.clickedAt <= r.Interval && r.Within(ref, e.Position) &&
			(!r.SameModifiers || e.Modifiers == c.clickedMods) && (r.Cycle == 0 || c.clicks < r.Cycle) {
			c.clicks++
		} else {
			c.clicks = 1
			c.runPos = e.Position
		}
		c.rule = r
	}
	c.clickedAt, c.clickedPos, c.clickedMods, c.runBroken = e.Time, e.Position, e.Modifiers, false
}

// moved ends the run when the pointer leaves the slop around its first
// press, or stays past the interval, before the next press (Windows:
// WebMouseEventBuilder resets its count on such a move or leave).
func (c *Click) moved(e pointer.Event) {
	r := &c.rule
	if !r.MoveResets || c.clicks == 0 || c.runBroken {
		return
	}
	if !r.Within(c.runPos, e.Position) || e.Time-c.clickedAt > r.Interval {
		c.runBroken = true
	}
}

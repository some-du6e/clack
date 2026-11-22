// SPDX-License-Identifier: Unlicense OR MIT

package input

import (
	"gioui.org/internal/clickrule"
	"gioui.org/io/pointer"
)

// Clack patch 48 (CLACK_PATCHES.md §48): a window counts its clicks
// whatever widget each lands on, as Chromium does: Blink takes a press's
// count from the platform's event (EventHandler::HandleMousePressEvent,
// SetClickCount(mouse_event.click_count)) and resets it for no element, and
// the platform counts by time and place. So the router keeps the window's
// run of clicks (clickrule.Run) and gives every press its count in
// pointer.Event.Clicks, as AppKit's clickCount does on macOS (where the
// backend's count stays). gesture.Click and gesture.ClickRun count by it: a
// press on the composer's + after one on its menu's scrim, on the same
// spot, is the third of a triple click, not the +'s double click.

// countClick notes pointer event e in the window's run of clicks and
// returns it, a press with its count.
func (q *Router) countClick(e pointer.Event) pointer.Event {
	switch e.Kind {
	case pointer.Press:
		s := float32(1)
		if v, ok := q.values[clickrule.PxPerDp{}].(float32); ok && v > 0 {
			s = v
		}
		if n := q.clicks.Press(e, s); e.Clicks <= 0 {
			e.Clicks = n
		}
	case pointer.Release:
		q.clicks.Release(e)
		if e.Clicks <= 0 {
			e.Clicks = q.clicks.Count() // the press's, as a click's detail
		}
	case pointer.Move, pointer.Drag, pointer.Leave:
		q.clicks.Move(e)
	case pointer.Cancel:
		q.clicks.Cancel()
	}
	return e
}

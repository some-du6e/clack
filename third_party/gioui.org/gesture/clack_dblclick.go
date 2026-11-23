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
//   - A touch, or a pen (Gio reports both as pointer.Touch), anywhere: its
//     taps go by Chromium's gesture detector (clickrule.Tap), not by the
//     mouse's box.
//
// The count is the window's, whatever widget each press lands on: Chromium
// takes a press's count from the platform's event and resets it for no
// element. input.Router counts a window's presses (clickrule.Run) and
// stamps each with its count in pointer.Event.Clicks, as AppKit does;
// Click and ClickRun take it from there.

import (
	"time"

	"gioui.org/f32"
	"gioui.org/internal/clickrule"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
)

// DoubleClickTime is how long after a mouse press the next one still
// counts on as a double (or triple) click on this platform now: the OS's
// setting where it has one. Taps go by their own rule (ClickRun).
func DoubleClickTime() time.Duration { return clickrule.Current().Interval }

// ClickRun gives a widget that reads its own pointer events the count of
// each press in its run of quick clicks, double and triple clicks, as
// Click does: the window's count (pointer.Event.Clicks, which the router
// stamps), or for a press that comes without one (not through a router)
// its own by the platform's rule. Give it the widget's pointer's every
// Press (Press), Release (Released) and Cancel (Cancelled), its Moves and
// Leaves (Moved), and, for a widget that drags (a resize handle), its
// Drags (Dragged): a drag past the platform's slop ends the run for the
// widget, so its next press starts a new one, as a double click can't
// follow a drag in Chromium.
type ClickRun struct {
	run    clickrule.Run // for presses without the window's count
	clicks int
	// base is subtracted from the window's count: the count before the
	// press a drag's end of the run made the widget's first
	base    int
	dragged bool
	// the last press, for Dragged's slop
	src      pointer.Source
	pressPos f32.Point
	pxPerDp  float32
}

// Press counts press e, read from q, and returns its place in its run: 1
// for a single click, 2 for a double click's second press, 3 for a triple
// click's third.
func (r *ClickRun) Press(q input.Source, e pointer.Event) int {
	s := pxPerDp(q)
	n := e.Clicks
	if n <= 0 {
		n = r.run.Press(e, s)
	}
	switch {
	case r.dragged:
		r.base, r.dragged = n-1, false
	case n <= r.base:
		r.base = 0 // the window's run started over
	}
	r.clicks = n - r.base
	r.src, r.pressPos, r.pxPerDp = e.Source, e.Position, s
	return r.clicks
}

// Released notes the release e of the pointer that pressed.
func (r *ClickRun) Released(e pointer.Event) { r.run.Release(e) }

// Cancelled notes a cancel of the pointer that pressed.
func (r *ClickRun) Cancelled() { r.run.Cancel() }

// Moved notes a move, drag or leave e.
func (r *ClickRun) Moved(e pointer.Event) { r.run.Move(e) }

// Dragged notes drag e of a widget that drags: past the platform's slop
// from the press (the mouse's box; a touch's clickrule.Tap.Move) it ends
// the run, so the widget's next press is a single click.
func (r *ClickRun) Dragged(e pointer.Event) {
	if r.clicks == 0 || r.dragged {
		return
	}
	d := e.Position.Sub(r.pressPos)
	if r.src != pointer.Mouse {
		m := clickrule.Tap.Move * r.pxPerDp
		r.dragged = d.X*d.X+d.Y*d.Y > m*m
	} else {
		r.dragged = !clickrule.Current().Within(r.pressPos, e.Position)
	}
	r.run.Move(e)
}

// Clicks is the count the last press got, 0 before any.
func (r *ClickRun) Clicks() int { return r.clicks }

// pxPerDp is q's window's pixels per dp (clickrule.PxPerDp), 1 without.
func pxPerDp(q input.Source) float32 {
	if v, ok := q.Value(clickrule.PxPerDp{}).(float32); ok && v > 0 {
		return v
	}
	return 1
}

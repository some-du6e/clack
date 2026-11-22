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
//     mouse's box: within 400 ms of the last tap's release, nearer than
//     20 dp to its press, that tap within 15 dp of where it went down; 1,
//     2, 3 and round again. A finger lifting a few px from where it went
//     down doesn't end the run.
//
// ClickRun does the counting, for Click and for widgets that read their
// own pointer events.

import (
	"time"

	"gioui.org/f32"
	"gioui.org/internal/clickrule"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
)

// DoubleClickTime is how long after a mouse press the next one still
// counts on as a double (or triple) click on this platform now: the OS's
// setting where it has one. Taps go by their own rule (ClickRun).
func DoubleClickTime() time.Duration { return clickrule.Current().Interval }

// ClickRun counts a run of quick clicks, double and triple clicks, by the
// platform's rule, as Click counts its presses: for a widget that reads
// its own pointer events (a resize handle's double click). Give it the
// widget's pointer's every Press (Press), Release (Released) and Cancel
// (Cancelled), and its Moves and Leaves (Moved). A widget that drags
// gives it its Drags (Dragged): a drag ends the run, so a press after one
// starts a new run, as a double click can't follow a drag in Chromium.
type ClickRun struct {
	clicks int
	src    pointer.Source
	// the last press: its time, place and modifiers; runPos is the run's
	// first press's place
	pressAt          time.Duration
	pressPos, runPos f32.Point
	mods             key.Modifiers
	// rule is the mouse's rule the last mouse press counted by
	rule clickrule.Rule
	// broken: a move or a drag ended the run since the last press
	broken bool
	// a tap: still down, moved past clickrule.Tap.Move while down, its
	// release's time, and the window's px per dp at its press
	down, strayed bool
	upAt          time.Duration
	pxPerDp       float32
}

// Press counts press e, read from q, and returns its place in its run: 1
// for a single click, 2 for a double click's second press, 3 for a triple
// click's third.
func (r *ClickRun) Press(q input.Source, e pointer.Event) int {
	switch {
	case e.Source != pointer.Mouse:
		r.tap(q, e)
	case e.Clicks > 0:
		// AppKit's count, unless a drag ended the run
		r.clicks = e.Clicks
		if r.broken {
			r.clicks = 1
		}
		r.rule = clickrule.Current()
	default:
		rule := clickrule.Current()
		ref := r.pressPos
		if rule.FromFirst {
			ref = r.runPos
		}
		if r.clicks > 0 && r.src == pointer.Mouse && !r.broken && e.Time-r.pressAt <= rule.Interval && rule.Within(ref, e.Position) &&
			(!rule.SameModifiers || e.Modifiers == r.mods) && (rule.Cycle == 0 || r.clicks < rule.Cycle) {
			r.clicks++
		} else {
			r.clicks = 1
		}
		r.rule = rule
	}
	if r.clicks == 1 {
		r.runPos = e.Position
	}
	r.src, r.pressAt, r.pressPos, r.mods, r.broken = e.Source, e.Time, e.Position, e.Modifiers, false
	return r.clicks
}

// tap counts touch press e by clickrule.Tap, its distances in q's window's
// dp.
func (r *ClickRun) tap(q input.Source, e pointer.Event) {
	t := clickrule.Tap
	s := float32(1)
	if v, ok := q.Value(clickrule.PxPerDp{}).(float32); ok && v > 0 {
		s = v
	}
	if r.clicks > 0 && r.src == e.Source && !r.broken && !r.down && !r.strayed &&
		e.Time-r.upAt <= t.Interval && dist2(r.pressPos, e.Position) < sq(t.Slop*s) {
		r.clicks++
		if t.Cycle > 0 && r.clicks > t.Cycle {
			r.clicks = 1
		}
	} else {
		r.clicks = 1
	}
	r.down, r.strayed, r.pxPerDp = true, false, s
}

// Released notes the release e of the pointer that pressed: a tap's run
// counts on from its release.
func (r *ClickRun) Released(e pointer.Event) {
	if r.src == pointer.Mouse || !r.down {
		return
	}
	r.stray(e.Position)
	r.down, r.upAt = false, e.Time
}

// Cancelled notes a cancel of the pointer that pressed: a tap cancelled (a
// scroll took the pointer) isn't one the next press repeats.
func (r *ClickRun) Cancelled() {
	if r.src != pointer.Mouse && r.down {
		r.down, r.strayed = false, true
	}
}

// Moved notes a move, drag or leave e: where the platform's rule says so
// (Windows), a mouse leaving the box around the run's first press, or
// staying past the interval, ends the run (Blink's WebMouseEventBuilder
// resets its count on such a move or leave); a touch moving past
// clickrule.Tap.Move while down makes its tap one the next press doesn't
// repeat (always_in_bigger_tap_region_). A touch's Leave after its release
// (the router sends one) changes nothing.
func (r *ClickRun) Moved(e pointer.Event) {
	if r.clicks == 0 || r.broken {
		return
	}
	if r.src != pointer.Mouse {
		if r.down {
			r.stray(e.Position)
		}
		return
	}
	if !r.rule.MoveResets {
		return
	}
	if !r.rule.Within(r.runPos, e.Position) || e.Time-r.pressAt > r.rule.Interval {
		r.broken = true
	}
}

// Dragged notes drag e of a widget that drags: past the platform's slop
// from the press (a touch's: clickrule.Tap.Move) it ends the run, so the
// next press starts a new one.
func (r *ClickRun) Dragged(e pointer.Event) {
	if r.clicks == 0 || r.broken {
		return
	}
	if r.src != pointer.Mouse {
		r.broken = dist2(r.pressPos, e.Position) > sq(clickrule.Tap.Move*r.pxPerDp)
		return
	}
	r.broken = !r.rule.Within(r.pressPos, e.Position)
	r.Moved(e)
}

// Clicks is the count the last press got, 0 before any.
func (r *ClickRun) Clicks() int { return r.clicks }

// stray notes a down tap at p.
func (r *ClickRun) stray(p f32.Point) {
	if dist2(r.pressPos, p) > sq(clickrule.Tap.Move*r.pxPerDp) {
		r.strayed = true
	}
}

func dist2(a, b f32.Point) float32 {
	d := b.Sub(a)
	return d.X*d.X + d.Y*d.Y
}

func sq(v float32) float32 { return v * v }

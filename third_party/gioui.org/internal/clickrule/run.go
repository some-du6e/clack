// SPDX-License-Identifier: Unlicense OR MIT

package clickrule

import (
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
)

// Run counts a run of quick clicks, double and triple clicks, by the
// platform's rule: a mouse's presses by Current (AppKit's own count where
// the backend sends one, pointer.Event.Clicks), a touch's (and a pen's,
// which Gio reports as a touch) by Tap. Clack patch 48: input.Router keeps
// one for its window and gives every press the window's count (Chromium
// counts a window's clicks whatever element each lands on); gesture's
// ClickRun keeps one for presses that come without it.
type Run struct {
	clicks int
	src    pointer.Source
	// the last press: its time, place, modifiers and buttons; runPos is
	// the run's first press's place
	pressAt          time.Duration
	pressPos, runPos f32.Point
	mods             key.Modifiers
	buttons          pointer.Buttons
	// rule is the mouse's rule the last mouse press counted by
	rule Rule
	// broken: a move ended the run since the last press
	broken bool
	// a tap: still down, moved past Tap.Move while down, its release's
	// time, and its window's px per dp
	down, strayed bool
	upAt          time.Duration
	pxPerDp       float32
}

// Press counts press e, in a window of pxPerDp pixels per dp (0 is 1),
// and returns its place in its run: 1 for a single click, 2 for a double
// click's second press, 3 for a triple click's third. A mouse press of
// other buttons than the last starts a new run, as Chromium counts: Linux's
// ui::MouseEvent::IsRepeatedClickEvent compares the presses' flags (the
// buttons down and the modifiers), Windows' WebMouseEventBuilder counts on
// only for the same button (s_last_click_button). A right click on a spot,
// then a left click there, is a single left click.
func (r *Run) Press(e pointer.Event, pxPerDp float32) int {
	if pxPerDp <= 0 {
		pxPerDp = 1
	}
	switch {
	case e.Source != pointer.Mouse:
		t := Tap
		if r.clicks > 0 && r.src == e.Source && !r.broken && !r.down && !r.strayed &&
			e.Time-r.upAt <= t.Interval && dist2(r.pressPos, e.Position) < sq(t.Slop*pxPerDp) {
			r.clicks++
			if t.Cycle > 0 && r.clicks > t.Cycle {
				r.clicks = 1
			}
		} else {
			r.clicks = 1
		}
		r.down, r.strayed = true, false
	case e.Clicks > 0:
		r.clicks = e.Clicks // AppKit's
		r.rule = Current()
	default:
		rule := Current()
		ref := r.pressPos
		if rule.FromFirst {
			ref = r.runPos
		}
		if r.clicks > 0 && r.src == pointer.Mouse && !r.broken && e.Buttons == r.buttons && e.Time-r.pressAt <= rule.Interval &&
			rule.Within(ref, e.Position) && (!rule.SameModifiers || e.Modifiers == r.mods) && (rule.Cycle == 0 || r.clicks < rule.Cycle) {
			r.clicks++
		} else {
			r.clicks = 1
		}
		r.rule = rule
	}
	if r.clicks == 1 {
		r.runPos = e.Position
	}
	r.src, r.pressAt, r.pressPos, r.mods, r.buttons, r.broken, r.pxPerDp = e.Source, e.Time, e.Position, e.Modifiers, e.Buttons, false, pxPerDp
	return r.clicks
}

// Release notes the release e of the pointer that pressed: a tap's run
// counts on from its release.
func (r *Run) Release(e pointer.Event) {
	if r.src == pointer.Mouse || !r.down {
		return
	}
	r.stray(e.Position)
	r.down, r.upAt = false, e.Time
}

// Cancel notes a cancel of the pointer that pressed: a cancelled tap (a
// scroll took the pointer) isn't one the next press repeats.
func (r *Run) Cancel() {
	if r.src != pointer.Mouse && r.down {
		r.down, r.strayed = false, true
	}
}

// Move notes a move, drag or leave e: where the platform's rule says so
// (Windows), a mouse leaving the box around the run's first press, or
// staying past the interval, ends the run (Blink's WebMouseEventBuilder
// resets its count on such a move or leave); a touch moving past Tap.Move
// while down makes its tap one the next press doesn't repeat
// (always_in_bigger_tap_region_). A touch's Leave after its release (the
// router sends one) changes nothing.
func (r *Run) Move(e pointer.Event) {
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

// Beyond reports whether p is past the platform's slop from the last
// press: the mouse's box (its Slop around the press) or, for a touch,
// Tap.Move.
func (r *Run) Beyond(p f32.Point) bool {
	if r.src != pointer.Mouse {
		return dist2(r.pressPos, p) > sq(Tap.Move*r.pxPerDp)
	}
	return !r.rule.Within(r.pressPos, p)
}

// Count is the count the last press got, 0 before any.
func (r *Run) Count() int { return r.clicks }

// stray notes a down tap at p.
func (r *Run) stray(p f32.Point) {
	if dist2(r.pressPos, p) > sq(Tap.Move*r.pxPerDp) {
		r.strayed = true
	}
}

func dist2(a, b f32.Point) float32 {
	d := b.Sub(a)
	return d.X*d.X + d.Y*d.Y
}

func sq(v float32) float32 { return v * v }

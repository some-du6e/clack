// SPDX-License-Identifier: Unlicense OR MIT

// Package clickrule is the platform's rule for runs of quick clicks
// (double and triple clicks), the way Chromium counts them on each
// platform. Clack patch 48 (CLACK_PATCHES.md §48): package app sets Base
// and Read for its backend, gesture.Click counts a mouse's presses by
// Current and a touch's by Tap.
package clickrule

import (
	"image"
	"time"

	"gioui.org/f32"
)

// Rule says when a press continues a run of clicks.
type Rule struct {
	// Interval is the longest time from the run's last press to the next.
	Interval time.Duration
	// Slop is how far, px, the next press may land from the run's last
	// press (FromFirst: its first) on either axis.
	Slop image.Point
	// FromFirst measures Slop from the run's first press.
	FromFirst bool
	// MoveResets ends the run when the pointer moves out of Slop (or
	// past Interval) before the next press.
	MoveResets bool
	// SameModifiers ends the run on a press with other modifiers.
	SameModifiers bool
	// Cycle, when not 0, starts a new run after that many clicks.
	Cycle int
}

// Default is Chromium's own rule, which its X11 and Wayland backends count
// with (ui/events/event.cc MouseEvent::IsRepeatedClickEvent and
// GetRepeatCount, every native mouse event through InitializeNative):
// kDoubleClickTimeMs, a fixed 500 ms that ignores GTK's setting; within
// 2 px either way of the last press (kDoubleClickWidth and
// kDoubleClickHeight 4, halved); the same flags, so a press with other
// modifiers starts over; and SetClickCount stops at 3 with
// EF_IS_TRIPLE_CLICK set, which makes the next press's flags differ: the
// fourth click is a single again.
var Default = Rule{Interval: 500 * time.Millisecond, Slop: image.Pt(2, 2), SameModifiers: true, Cycle: 3}

var (
	// Base is the platform's rule; package app sets it for its backend.
	Base = Default
	// Read, when set, reads the platform's Interval and Slop. Current
	// calls it every time, so a change to the setting applies at once.
	Read func() (time.Duration, image.Point)
)

// Current is the platform's rule now.
func Current() Rule {
	r := Base
	if Read != nil {
		r.Interval, r.Slop = Read()
	}
	return r
}

// Within reports whether p is within Slop of ref.
func (r Rule) Within(ref, p f32.Point) bool {
	dx, dy := p.X-ref.X, p.Y-ref.Y
	return dx <= float32(r.Slop.X) && -dx <= float32(r.Slop.X) && dy <= float32(r.Slop.Y) && -dy <= float32(r.Slop.Y)
}

// TapRule says when a tap (a touch's, or a pen's: Gio reports a pen as a
// touch) continues a run of taps. Distances are dp.
type TapRule struct {
	// Interval is the longest time from the last tap's release to the
	// next press.
	Interval time.Duration
	// Slop is how near, dp, the next press must land to the last tap's
	// press (less than, as the crow flies).
	Slop float32
	// Move is how far, dp, a tap may move while down for the next press
	// to repeat it.
	Move float32
	// Cycle, when not 0, starts a new run after that many taps.
	Cycle int
}

// Tap is Chromium's rule for taps on its desktop platforms, Windows and
// Linux, which the mouse's settings don't touch: its gesture detector's
// (ui/events/gesture_detection gesture_detector.cc, IsRepeatedTap and the
// single tap repeat count) with GestureConfigurationAura's values
// (gesture_configuration.h). A press no later than double_tap_timeout_in_ms
// (400 ms) after the last tap's release, and nearer than
// max_distance_between_taps_for_double_tap (20 DIPs, plus the detector's
// .05 slop epsilon) to that tap's press, counts on, if that tap stayed
// within max_touch_move_in_pixels_for_click (15 DIPs, +.05) of where it went
// down (always_in_bigger_tap_region_). The count goes 1, 2, 3 and round
// again (max_tap_count 3, single_tap_repeat_interval); modifiers don't
// count.
var Tap = TapRule{Interval: 400 * time.Millisecond, Slop: 20.05, Move: 15.05, Cycle: 3}

// PxPerDp is the key under which package app keeps a window's pixels per
// dp (a float32) in its router's values (input.Source.Value, Clack patch
// 15), for Tap's distances. A router without one (a test's) is 1.
type PxPerDp struct{}

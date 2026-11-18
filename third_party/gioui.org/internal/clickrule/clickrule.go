// SPDX-License-Identifier: Unlicense OR MIT

// Package clickrule is the platform's rule for runs of quick clicks
// (double and triple clicks), the way Chromium counts them on each
// platform. Clack patch 48 (CLACK_PATCHES.md §48): package app sets Base
// and Read for its backend, gesture.Click counts every press by Current.
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

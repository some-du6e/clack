// SPDX-License-Identifier: Unlicense OR MIT

package gesture

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/internal/clickrule"
	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/op"
)

// Clack patch 48: a Click counts presses by the platform's rule
// (internal/clickrule), Chromium's on each platform.

// clickAt is one press (and its release 30 ms later) for clickCounts.
type clickAt struct {
	t    time.Duration
	p    f32.Point
	mods key.Modifiers
	// moves come before the press (the pointer travelling to it)
	moves []f32.Point
	// os is the platform's own count (pointer.Event.Clicks)
	os int
}

// clickCounts runs presses through a router into one Click and returns
// each press's NumClicks.
func clickCounts(t *testing.T, presses ...clickAt) []int {
	t.Helper()
	var c Click
	var ops op.Ops
	c.Add(&ops)
	var r input.Router
	c.Update(r.Source())
	r.Frame(&ops)
	var evs []event.Event
	for _, pr := range presses {
		for _, m := range pr.moves {
			evs = append(evs, pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: m, Time: pr.t - time.Millisecond})
		}
		evs = append(evs,
			pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pr.p, Time: pr.t, Modifiers: pr.mods, Clicks: pr.os},
			pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pr.p, Time: pr.t + 30*time.Millisecond, Modifiers: pr.mods, Clicks: pr.os})
	}
	r.Queue(evs...)
	var got []int
	for {
		e, ok := c.Update(r.Source())
		if !ok {
			break
		}
		if e.Kind == KindClick {
			got = append(got, e.NumClicks)
		}
	}
	if len(got) != len(presses) {
		t.Fatalf("%d clicks for %d presses", len(got), len(presses))
	}
	return got
}

// withRule makes r the platform's rule for the test.
func withRule(t *testing.T, base clickrule.Rule, read func() (time.Duration, image.Point)) {
	t.Helper()
	oldBase, oldRead := clickrule.Base, clickrule.Read
	clickrule.Base, clickrule.Read = base, read
	t.Cleanup(func() { clickrule.Base, clickrule.Read = oldBase, oldRead })
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

const sec = time.Second

// Without a backend's rule (X11, Wayland, tests) it's Chromium's Linux one:
// 500 ms and 2 px from the last press, the same modifiers, and a fourth
// quick click is a single again.
func TestClackClickRuleDefault(t *testing.T) {
	withRule(t, clickrule.Default, nil)
	at := f32.Pt(50, 50)
	for _, tc := range []struct {
		name    string
		presses []clickAt
		want    []int
	}{
		{"400 ms apart: a double click", []clickAt{{t: sec, p: at}, {t: sec + 400*time.Millisecond, p: at}}, []int{1, 2}},
		{"500 ms apart: still one", []clickAt{{t: sec, p: at}, {t: sec + 500*time.Millisecond, p: at}}, []int{1, 2}},
		{"501 ms apart: two singles", []clickAt{{t: sec, p: at}, {t: sec + 501*time.Millisecond, p: at}}, []int{1, 1}},
		{"2 px away", []clickAt{{t: sec, p: at}, {t: sec + 100*time.Millisecond, p: at.Add(f32.Pt(2, -2))}}, []int{1, 2}},
		{"3 px away", []clickAt{{t: sec, p: at}, {t: sec + 100*time.Millisecond, p: at.Add(f32.Pt(3, 0))}}, []int{1, 1}},
		{"from the last press: 2 px, then 2 more", []clickAt{{t: sec, p: at}, {t: sec + 100*time.Millisecond, p: at.Add(f32.Pt(2, 0))},
			{t: sec + 200*time.Millisecond, p: at.Add(f32.Pt(4, 0))}}, []int{1, 2, 3}},
		{"other modifiers", []clickAt{{t: sec, p: at}, {t: sec + 100*time.Millisecond, p: at, mods: key.ModShift}}, []int{1, 1}},
		{"the fourth is a single", []clickAt{{t: sec, p: at}, {t: sec + 100*time.Millisecond, p: at}, {t: sec + 200*time.Millisecond, p: at},
			{t: sec + 300*time.Millisecond, p: at}, {t: sec + 400*time.Millisecond, p: at}}, []int{1, 2, 3, 1, 2}},
		{"moves don't end it", []clickAt{{t: sec, p: at}, {t: sec + 200*time.Millisecond, p: at, moves: []f32.Point{at.Add(f32.Pt(30, 0)), at}}}, []int{1, 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := clickCounts(t, tc.presses...); !sameInts(got, tc.want) {
				t.Errorf("clicks %v, want %v", got, tc.want)
			}
		})
	}
	if got := DoubleClickTime(); got != 500*time.Millisecond {
		t.Errorf("DoubleClickTime %v, want 500ms", got)
	}
}

// Windows (app/clack_dblclick_windows.go, Blink's WebMouseEventBuilder):
// GetDoubleClickTime, half the SM_C?DOUBLECLK box around the run's first
// press, a move out of it ends the run, no cap; both read on every press.
func TestClackClickRuleWindows(t *testing.T) {
	interval, slop := 500*time.Millisecond, image.Pt(2, 2) // GetDoubleClickTime 500, SM_CXDOUBLECLK 4
	reads := 0
	withRule(t, clickrule.Rule{FromFirst: true, MoveResets: true}, func() (time.Duration, image.Point) {
		reads++
		return interval, slop
	})
	at := f32.Pt(50, 50)
	for _, tc := range []struct {
		name    string
		presses []clickAt
		want    []int
	}{
		{"400 ms apart", []clickAt{{t: sec, p: at}, {t: sec + 400*time.Millisecond, p: at}}, []int{1, 2}},
		{"501 ms apart", []clickAt{{t: sec, p: at}, {t: sec + 501*time.Millisecond, p: at}}, []int{1, 1}},
		{"from the first press: 2 px, then 2 more", []clickAt{{t: sec, p: at}, {t: sec + 100*time.Millisecond, p: at.Add(f32.Pt(2, 0))},
			{t: sec + 200*time.Millisecond, p: at.Add(f32.Pt(4, 0))}}, []int{1, 2, 1}},
		{"modifiers don't matter", []clickAt{{t: sec, p: at}, {t: sec + 100*time.Millisecond, p: at, mods: key.ModShift}}, []int{1, 2}},
		{"no cap", []clickAt{{t: sec, p: at}, {t: sec + 100*time.Millisecond, p: at}, {t: sec + 200*time.Millisecond, p: at},
			{t: sec + 300*time.Millisecond, p: at}}, []int{1, 2, 3, 4}},
		{"a move out of the box ends the run", []clickAt{{t: sec, p: at},
			{t: sec + 200*time.Millisecond, p: at, moves: []f32.Point{at.Add(f32.Pt(10, 0)), at}}}, []int{1, 1}},
		{"a move inside it doesn't", []clickAt{{t: sec, p: at},
			{t: sec + 200*time.Millisecond, p: at, moves: []f32.Point{at.Add(f32.Pt(1, 1)), at}}}, []int{1, 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := clickCounts(t, tc.presses...); !sameInts(got, tc.want) {
				t.Errorf("clicks %v, want %v", got, tc.want)
			}
		})
	}
	// the setting is read on every press: a slower double-click time in
	// the Mouse settings applies to the next click
	reads = 0
	interval = 900 * time.Millisecond
	if got := clickCounts(t, clickAt{t: sec, p: at}, clickAt{t: sec + 800*time.Millisecond, p: at}); !sameInts(got, []int{1, 2}) {
		t.Errorf("900 ms setting, 800 ms apart: %v, want [1 2]", got)
	}
	if reads != 2 {
		t.Errorf("the setting read %d times for 2 presses", reads)
	}
	interval = 200 * time.Millisecond
	if got := clickCounts(t, clickAt{t: sec, p: at}, clickAt{t: sec + 400*time.Millisecond, p: at}); !sameInts(got, []int{1, 1}) {
		t.Errorf("200 ms setting, 400 ms apart: %v, want [1 1]", got)
	}
	if got := DoubleClickTime(); got != interval {
		t.Errorf("DoubleClickTime %v, want the setting's %v", got, interval)
	}
}

// macOS: AppKit's own count wins (pointer.Event.Clicks, as Chromium's mac
// builder takes [event clickCount]); presses without one go by the rule.
func TestClackClickOSCount(t *testing.T) {
	withRule(t, clickrule.Rule{}, func() (time.Duration, image.Point) { return 500 * time.Millisecond, image.Pt(2, 2) })
	at := f32.Pt(50, 50)
	// AppKit counted a double click even where the rule wouldn't (it
	// knows its own distance and time), and a single where the rule would
	if got := clickCounts(t, clickAt{t: sec, p: at, os: 1}, clickAt{t: sec + 900*time.Millisecond, p: at.Add(f32.Pt(6, 0)), os: 2},
		clickAt{t: sec + 950*time.Millisecond, p: at.Add(f32.Pt(6, 0)), os: 1}); !sameInts(got, []int{1, 2, 1}) {
		t.Errorf("AppKit's counts: %v, want [1 2 1]", got)
	}
	if got := clickCounts(t, clickAt{t: sec, p: at}, clickAt{t: sec + 400*time.Millisecond, p: at}); !sameInts(got, []int{1, 2}) {
		t.Errorf("without AppKit's count, 400 ms apart: %v, want [1 2]", got)
	}
}

// SPDX-License-Identifier: Unlicense OR MIT

package gesture

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/internal/clickrule"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
)

// Clack patch 48: the tap rule's distances are dp in the window's scale,
// which app.Window keeps in its router (clickrule.PxPerDp).
func TestClackClickTapScale(t *testing.T) {
	withRule(t, clickrule.Default, nil)
	at := f32.Pt(50, 50)
	pt := func(dx float32) *f32.Point { p := at.Add(f32.Pt(dx, 0)); return &p }
	twice := func(q input.Source) { q.SetValue(clickrule.PxPerDp{}, float32(2)) }
	ms := time.Millisecond
	for _, tc := range []struct {
		name    string
		presses []clickAt
		want    []int
	}{
		{"taps 30 px apart at 2x (15 dp)", []clickAt{{t: sec, p: at, touch: true}, {t: sec + 150*ms, p: at.Add(f32.Pt(30, 0)), touch: true}}, []int{1, 2}},
		{"taps 45 px apart at 2x (22.5 dp)", []clickAt{{t: sec, p: at, touch: true}, {t: sec + 150*ms, p: at.Add(f32.Pt(45, 0)), touch: true}}, []int{1, 1}},
		{"a tap lifting 25 px from where it went down at 2x (12.5 dp)", []clickAt{{t: sec, p: at, touch: true, up: pt(25)}, {t: sec + 150*ms, p: at, touch: true}}, []int{1, 2}},
		{"a tap lifting 32 px at 2x (16 dp)", []clickAt{{t: sec, p: at, touch: true, up: pt(32)}, {t: sec + 150*ms, p: at, touch: true}}, []int{1, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := clickCountsOn(t, twice, tc.presses...); !sameInts(got, tc.want) {
				t.Errorf("taps %v, want %v", got, tc.want)
			}
		})
	}
}

// A widget that drags (a resize handle) counts its presses with a
// ClickRun and gives it its drags: a drag past the platform's slop ends
// the run, so the next press is a single click however soon and near it
// comes; a hand's jitter inside the slop doesn't.
func TestClackClickRunDrag(t *testing.T) {
	var r input.Router
	q := r.Source()
	at := f32.Pt(50, 50)
	ms := time.Millisecond
	press := func(run *ClickRun, src pointer.Source, p f32.Point, tm time.Duration) int {
		return run.Press(q, pointer.Event{Kind: pointer.Press, Source: src, Buttons: pointer.ButtonPrimary, Position: p, Time: tm})
	}
	up := func(run *ClickRun, src pointer.Source, p f32.Point, tm time.Duration) {
		run.Released(pointer.Event{Kind: pointer.Release, Source: src, Position: p, Time: tm})
	}
	drag := func(run *ClickRun, src pointer.Source, p f32.Point, tm time.Duration) {
		run.Dragged(pointer.Event{Kind: pointer.Drag, Source: src, Position: p, Time: tm})
	}
	for _, rule := range []struct {
		name string
		base clickrule.Rule
		read func() (time.Duration, image.Point)
	}{
		{"Linux", clickrule.Default, nil},
		{"Windows", clickrule.Rule{FromFirst: true, MoveResets: true}, func() (time.Duration, image.Point) { return 900 * ms, image.Pt(2, 2) }},
	} {
		t.Run(rule.name, func(t *testing.T) {
			withRule(t, rule.base, rule.read)
			var run ClickRun
			press(&run, pointer.Mouse, at, sec)
			drag(&run, pointer.Mouse, at.Add(f32.Pt(1, 1)), sec+20*ms) // jitter
			up(&run, pointer.Mouse, at, sec+40*ms)
			if n := press(&run, pointer.Mouse, at, sec+100*ms); n != 2 {
				t.Errorf("a press after 1 px of jitter: %d, want 2", n)
			}
			up(&run, pointer.Mouse, at, sec+120*ms)
			run = ClickRun{}
			press(&run, pointer.Mouse, at, 2*sec)
			drag(&run, pointer.Mouse, at.Add(f32.Pt(100, 0)), 2*sec+20*ms)
			drag(&run, pointer.Mouse, at, 2*sec+40*ms) // and back
			up(&run, pointer.Mouse, at, 2*sec+60*ms)
			if n := press(&run, pointer.Mouse, at, 2*sec+100*ms); n != 1 {
				t.Errorf("a press after a drag: %d, want 1 (a new run)", n)
			}
			if n := press(&run, pointer.Mouse, at, 2*sec+200*ms); n != 2 {
				t.Errorf("the next press, no drag between: %d, want 2", n)
			}
		})
	}
	t.Run("AppKit's count", func(t *testing.T) {
		var run ClickRun
		run.Press(q, pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: at, Time: sec, Clicks: 1})
		drag(&run, pointer.Mouse, at.Add(f32.Pt(100, 0)), sec+20*ms)
		if n := run.Press(q, pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: at, Time: sec + 100*ms, Clicks: 2}); n != 1 {
			t.Errorf("AppKit's 2 after a drag: %d, want 1", n)
		}
	})
	t.Run("a tap", func(t *testing.T) {
		var run ClickRun
		press(&run, pointer.Touch, at, sec)
		drag(&run, pointer.Touch, at.Add(f32.Pt(10, 0)), sec+20*ms) // inside 15 dp
		up(&run, pointer.Touch, at.Add(f32.Pt(10, 0)), sec+40*ms)
		if n := press(&run, pointer.Touch, at, sec+150*ms); n != 2 {
			t.Errorf("a tap after 10 px of drag: %d, want 2", n)
		}
		up(&run, pointer.Touch, at, sec+170*ms)
		press(&run, pointer.Touch, at, sec+300*ms)
		drag(&run, pointer.Touch, at.Add(f32.Pt(30, 0)), sec+320*ms) // past 15 dp
		up(&run, pointer.Touch, at.Add(f32.Pt(30, 0)), sec+340*ms)
		if n := press(&run, pointer.Touch, at, sec+400*ms); n != 1 {
			t.Errorf("a tap after a 30 px drag: %d, want 1", n)
		}
	})
}

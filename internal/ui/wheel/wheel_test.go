package wheel

import (
	"image"
	"math"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/gesture"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

const frame = time.Second / 60

// step is a 60 Hz frame that adds up to 200 ms in exactly 12 (1/60 s
// isn't a whole number of nanoseconds).
func step(i int) time.Duration { return time.Duration(i) * 200 * time.Millisecond / 12 }

var t0 = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

// easeInOut is CSS ease-in-out, cubic-bezier(0.42, 0, 0.58, 1), solved by
// bisection: the reference the animation's curve is checked against.
func easeInOut(x float64) float64 {
	bez := func(p1, p2, t float64) float64 {
		return 3*(1-t)*(1-t)*t*p1 + 3*(1-t)*t*t*p2 + t*t*t
	}
	lo, hi := 0.0, 1.0
	for i := 0; i < 100; i++ {
		m := (lo + hi) / 2
		if bez(0.42, 0.58, m) < x {
			lo = m
		} else {
			hi = m
		}
	}
	return bez(0, 1, (lo+hi)/2)
}

// Chromium's DurationBehavior::kInverseDelta: 12 frames of 60 Hz up to
// 120px, 6 from 480px, linear between.
func TestWheelDuration(t *testing.T) {
	for _, c := range []struct {
		px   float64
		want time.Duration
	}{
		{1, 200 * time.Millisecond},
		{100, 200 * time.Millisecond},
		{-120, 200 * time.Millisecond},
		{300, 150 * time.Millisecond},
		{480, 100 * time.Millisecond},
		{-2000, 100 * time.Millisecond},
	} {
		got := gesture.WheelDuration(c.px)
		if d := got - c.want; d < -time.Microsecond || d > time.Microsecond {
			t.Errorf("WheelDuration(%v) = %v, want %v", c.px, got, c.want)
		}
	}
}

// One notch follows ease-in-out over 200 ms and ends exactly on target.
func TestWheelCurveEasing(t *testing.T) {
	var c gesture.WheelCurve
	c.Add(t0, 100, -1e6, 1e6)
	if !c.Active() || c.Duration() != 200*time.Millisecond {
		t.Fatalf("after one 100px notch: active %v, duration %v; want a 200ms animation", c.Active(), c.Duration())
	}
	pos := 0.0
	for i := 1; i <= 12; i++ {
		at := t0.Add(step(i))
		pos += c.Tick(at)
		x := float64(i) / 12
		if want := 100 * easeInOut(min(x, 1)); math.Abs(pos-want) > 0.01 {
			t.Errorf("frame %d (%v): at %.3fpx, want %.3fpx (ease-in-out)", i, at.Sub(t0), pos, want)
		}
	}
	if math.Abs(pos-100) > 1e-9 || c.Active() {
		t.Errorf("after 200ms: at %vpx, active %v; want 100px and done", pos, c.Active())
	}
	if d := c.Tick(t0.Add(time.Second)); d != 0 {
		t.Errorf("a finished curve still moved %vpx", d)
	}
}

// track ticks the curve every millisecond from from to to and returns the
// positions.
func track(c *gesture.WheelCurve, pos *float64, from, to time.Duration) []float64 {
	var ps []float64
	for at := from; at <= to; at += time.Millisecond {
		*pos += c.Tick(t0.Add(at))
		ps = append(ps, *pos)
	}
	return ps
}

// A notch arriving mid-animation moves the target on by its distance and
// keeps the scroll's speed: no jump, no restart from rest.
func TestWheelCurveAccumulates(t *testing.T) {
	var c gesture.WheelCurve
	pos := 0.0
	c.Add(t0, 100, -1e6, 1e6)
	before := track(&c, &pos, time.Millisecond, 60*time.Millisecond)
	vBefore := before[len(before)-1] - before[len(before)-2] // px/ms just before
	c.Add(t0.Add(60*time.Millisecond), 100, -1e6, 1e6)
	if got := pos + c.Remaining(); math.Abs(got-200) > 1e-9 {
		t.Fatalf("target after two notches = %v, want 200", got)
	}
	after := track(&c, &pos, 61*time.Millisecond, 62*time.Millisecond)
	vAfter := after[0] - before[len(before)-1]
	if vBefore <= 0 || math.Abs(vAfter-vBefore)/vBefore > 0.1 {
		t.Errorf("speed across the second notch: %.3f px/ms before, %.3f after; want it kept (within 10%%)", vBefore, vAfter)
	}
	rest := track(&c, &pos, 63*time.Millisecond, 600*time.Millisecond)
	for i := 1; i < len(rest); i++ {
		if rest[i] < rest[i-1]-1e-9 {
			t.Fatalf("moved backwards at %v: %v after %v", 63*time.Millisecond+time.Duration(i)*time.Millisecond, rest[i], rest[i-1])
		}
	}
	if math.Abs(pos-200) > 1e-6 || c.Active() {
		t.Errorf("ended at %vpx (active %v), want 200 and done", pos, c.Active())
	}
	// a quick flick: six notches 35ms apart all land
	var f gesture.WheelCurve
	p := 0.0
	for i := 0; i < 6; i++ {
		at := time.Duration(i) * 35 * time.Millisecond
		f.Add(t0.Add(at), -100, -1e6, 1e6)
		track(&f, &p, at+time.Millisecond, at+35*time.Millisecond)
	}
	track(&f, &p, 6*35*time.Millisecond, time.Second)
	if math.Abs(p+600) > 1e-6 {
		t.Errorf("six -100px notches moved %vpx, want -600", p)
	}
}

// A notch the other way turns the scroll around and ends on the new
// target.
func TestWheelCurveReverses(t *testing.T) {
	var c gesture.WheelCurve
	pos := 0.0
	c.Add(t0, 100, -1e6, 1e6)
	track(&c, &pos, time.Millisecond, 80*time.Millisecond)
	mid := pos
	c.Add(t0.Add(80*time.Millisecond), -100, -1e6, 1e6)
	ps := track(&c, &pos, 81*time.Millisecond, time.Second)
	if math.Abs(pos) > 1e-6 {
		t.Errorf("ended at %vpx, want 0", pos)
	}
	if mid <= 0 || ps[len(ps)-1] >= mid {
		t.Errorf("didn't turn around: %v at the reversal, %v at the end", mid, ps[len(ps)-1])
	}
}

// The target stays inside the scroller's range, as Chromium clamps it to
// the scroll extent.
func TestWheelCurveClamps(t *testing.T) {
	var c gesture.WheelCurve
	pos := 0.0
	c.Add(t0, 100, -50, 30)
	track(&c, &pos, time.Millisecond, 50*time.Millisecond)
	c.Add(t0.Add(50*time.Millisecond), 100, -50-pos, 30-pos)
	track(&c, &pos, 51*time.Millisecond, time.Second)
	if math.Abs(pos-30) > 1e-6 {
		t.Errorf("ended at %vpx, want the range's end, 30", pos)
	}
	var d gesture.WheelCurve
	d.Add(t0, 100, -10, 0)
	if d.Active() {
		t.Errorf("a notch toward an end the scroller is at started an animation")
	}
}

// Precise (touchpad) deltas apply at once and stop a running animation;
// reduced motion makes notches instant too.
func TestWheelPassthrough(t *testing.T) {
	big := pointer.ScrollRange{Min: -1e6, Max: 1e6}
	var w gesture.Wheel
	notch := pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Scroll: f32.Pt(0, 100), Notched: true}
	if d := w.Scroll(t0, notch, big, big); d != (f32.Point{}) || !w.Active() {
		t.Fatalf("a notch: moved %v at once, active %v; want 0 now and an animation", d, w.Active())
	}
	pad := pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Scroll: f32.Pt(3, -7.5)}
	if d := w.Scroll(t0.Add(frame), pad, big, big); d != pad.Scroll {
		t.Errorf("touchpad delta: moved %v, want %v at once", d, pad.Scroll)
	}
	if w.Active() {
		t.Errorf("a touchpad delta didn't stop the wheel animation")
	}

	gesture.SetWheelAnimation(false)
	defer gesture.SetWheelAnimation(true)
	if d := w.Scroll(t0, notch, big, big); d != notch.Scroll || w.Active() {
		t.Errorf("reduced motion: a notch moved %v (active %v), want %v at once", d, w.Active(), notch.Scroll)
	}
}

// listEnv lays out a layout.List of 100 rows 50px high in a 300x400
// window through a real input.Router.
type listEnv struct {
	r    input.Router
	ops  op.Ops
	list layout.List
}

func (e *listEnv) frame(at time.Time) {
	e.ops.Reset()
	gtx := layout.Context{Ops: &e.ops, Now: at, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(300, 400)), Source: e.r.Source()}
	e.list.Axis = layout.Vertical
	e.list.Layout(gtx, 100, func(gtx layout.Context, i int) layout.Dimensions {
		return layout.Dimensions{Size: image.Pt(300, 50)}
	})
	e.r.Frame(&e.ops)
}

func (e *listEnv) scrolled() int { return e.list.Position.First*50 + e.list.Position.Offset }

func (e *listEnv) wants() bool { _, ok := e.r.WakeupTime(); return ok }

// A wheel notch through Gio's router moves the list over the following
// frames, asking for each, and lands on the notch's distance.
func TestListWheelAnimates(t *testing.T) {
	e := new(listEnv)
	e.frame(t0)
	e.r.Queue(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: f32.Pt(150, 200), Scroll: f32.Pt(0, 100), Notched: true})
	e.frame(t0)
	if got := e.scrolled(); got != 0 {
		t.Errorf("the notch's own frame scrolled %dpx, want 0 (the animation starts there)", got)
	}
	if !e.wants() {
		t.Fatalf("no next frame asked for after a notch")
	}
	var seen []int
	for i := 1; i <= 12; i++ {
		e.frame(t0.Add(step(i)))
		seen = append(seen, e.scrolled())
		if i < 12 && !e.wants() {
			t.Fatalf("frame %d: no next frame asked for mid-animation (at %dpx)", i, e.scrolled())
		}
	}
	for i := 1; i < len(seen); i++ {
		if seen[i] < seen[i-1] {
			t.Fatalf("offset went backwards: %v", seen)
		}
	}
	if seen[5] < 40 || seen[5] > 60 {
		t.Errorf("at 100ms the list is at %dpx, want about half the notch (ease-in-out): %v", seen[5], seen)
	}
	if got := e.scrolled(); got != 100 {
		t.Errorf("after 200ms the list is at %dpx, want 100: %v", got, seen)
	}
	e.frame(t0.Add(step(13)))
	if e.wants() {
		t.Errorf("still asking for frames after the animation ended")
	}
}

// Two notches in a row add up; one toward the list's top while at the top
// does nothing.
func TestListWheelAccumulates(t *testing.T) {
	e := new(listEnv)
	e.frame(t0)
	notch := func(dy float32) {
		e.r.Queue(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: f32.Pt(150, 200), Scroll: f32.Pt(0, dy), Notched: true})
	}
	notch(-100)
	e.frame(t0)
	if e.wants() || e.scrolled() != 0 {
		t.Errorf("a notch up at the top: at %dpx, animating %v; want nothing", e.scrolled(), e.wants())
	}
	notch(100)
	e.frame(t0)
	e.frame(t0.Add(3 * frame))
	notch(100)
	e.frame(t0.Add(4 * frame))
	for i := 5; i < 40; i++ {
		e.frame(t0.Add(time.Duration(i) * frame))
	}
	if got := e.scrolled(); got != 200 {
		t.Errorf("two notches: at %dpx, want 200", got)
	}
}

// Without Notched (a touchpad, a test's synthetic scroll) the list moves
// in the event's frame, as before.
func TestListPreciseScrollImmediate(t *testing.T) {
	e := new(listEnv)
	e.frame(t0)
	e.r.Queue(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: f32.Pt(150, 200), Scroll: f32.Pt(0, 37)})
	e.frame(t0)
	if got := e.scrolled(); got != 37 {
		t.Errorf("a precise 37px scroll: at %dpx, want 37 in the same frame", got)
	}
	if e.wants() {
		t.Errorf("a precise scroll asked for another frame")
	}
}

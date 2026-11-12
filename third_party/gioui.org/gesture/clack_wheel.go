// SPDX-License-Identifier: Unlicense OR MIT

package gesture

// Clack patch 9 (CLACK_PATCHES.md): notched mouse-wheel scrolling animates
// the way Chromium's does. A notch (pointer.Event.Notched) moves the
// scroller to a target along a cubic-bezier curve; notches that arrive
// while it moves push the target further and the curve is rebuilt from
// where it is, keeping its velocity. Precise input (touchpads, trackpads)
// is not animated and cancels a running animation, as Blink's
// ScrollAnimator::UserScroll does for kScrollByPrecisePixel
// (third_party/blink/renderer/core/scroll/scroll_animator.cc).
//
// The curve is cc::ScrollOffsetAnimationCurve for ScrollType::kMouseWheel
// (cc/animation/scroll_offset_animation_curve.cc and
// scroll_offset_animation_curve_factory.cc, Chromium main df2e801): an
// ease-in-out (kEaseInOutControlPoints) with DurationBehavior::kInverseDelta,
// retargeted by UpdateTarget.

import (
	"math"
	"sync/atomic"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/op"
)

const (
	// kInverseDeltaRampStartPx, kInverseDeltaRampEndPx: a delta up to
	// 120px takes kInverseDeltaMaxDuration, from 480px on
	// kInverseDeltaMinDuration, linear between.
	wheelRampStartPx = 120.0
	wheelRampEndPx   = 480.0
	// kInverseDeltaMinDuration, kInverseDeltaMaxDuration, in frames of
	// kDurationDivisor (60) per second: 100 ms and 200 ms.
	wheelMinFrames  = 6.0
	wheelMaxFrames  = 12.0
	wheelFramesPerS = 60.0
	// kEpsilon
	wheelEpsilon = 0.01
	// kEaseInOutControlPoints: cubic-bezier(0.42, 0, 0.58, 1). A retarget
	// scales y1 by the initial slope that keeps the velocity
	// (EaseInOutWithInitialSlope, the slope clamped to ±1000).
	wheelX1, wheelX2, wheelY2 = 0.42, 0.58, 1.0
	wheelMaxSlope             = 1000.0
	// VelocityBasedDurationBound's fudge factor for the ease out.
	wheelVelocityBound = 2.5
)

var wheelInstant atomic.Bool

// SetWheelAnimation turns the animation of notched wheel scrolling on (the
// default) or off, when notches move at once (reduced motion; Chromium
// asks gfx::Animation::ScrollAnimationsEnabledBySystem).
func SetWheelAnimation(on bool) { wheelInstant.Store(!on) }

// WheelAnimation reports whether notched wheel scrolling animates.
func WheelAnimation() bool { return !wheelInstant.Load() }

// WheelDuration is how long Chromium animates a wheel scroll of delta
// pixels (DurationBehavior::kInverseDelta): 200 ms up to 120px, 100 ms
// from 480px.
func WheelDuration(delta float64) time.Duration {
	slope := (wheelMinFrames - wheelMaxFrames) / (wheelRampEndPx - wheelRampStartPx)
	offset := wheelMaxFrames - wheelRampStartPx*slope
	frames := min(max(offset+math.Abs(delta)*slope, wheelMinFrames), wheelMaxFrames)
	return time.Duration(frames / wheelFramesPerS * float64(time.Second))
}

// WheelCurve is the animation of notched wheel scrolling along one axis.
// Positions are relative: Add's range is how far the scroller can move
// from the position Tick has reported so far.
type WheelCurve struct {
	active   bool
	t0       time.Time
	dur      time.Duration
	from, to float64
	y1       float64 // the bezier's first control point's y (x is wheelX1)
	pos      float64 // what Tick has reported
}

// Active reports whether the curve still has distance to report.
func (c *WheelCurve) Active() bool { return c.active }

// Stop drops the animation and whatever it had left to move.
func (c *WheelCurve) Stop() { *c = WheelCurve{} }

// Remaining is the distance the animation has yet to report.
func (c *WheelCurve) Remaining() float64 {
	if !c.active {
		return 0
	}
	return c.to - c.pos
}

// Duration is the length of the curve's current segment (from its last
// start or retarget).
func (c *WheelCurve) Duration() time.Duration { return c.dur }

// Add moves the target by delta at time t. lo and hi bound how far the
// scroller can go from where it is now, as pointer.ScrollRange does; the
// target stays inside them (ScrollAnimationUpdateTarget clamps it to the
// scroller's range).
func (c *WheelCurve) Add(t time.Time, delta, lo, hi float64) {
	target := func(to float64) float64 { return min(max(to+delta, c.pos+lo), c.pos+hi) }
	if !c.active || !t.Before(c.t0.Add(c.dur)) {
		// nothing running: a new animation from where the last one ended
		// (MouseWheelScrollAnimationCreate)
		cur := c.to
		to := target(cur)
		if math.Abs(to-cur) < wheelEpsilon {
			return
		}
		c.from, c.to, c.t0, c.dur, c.y1 = cur, to, t, WheelDuration(to-cur), 0
		c.active = true
		return
	}
	// ScrollOffsetAnimationCurve::UpdateTarget
	to := target(c.to)
	if math.Abs(c.to-to) < wheelEpsilon {
		c.to = to
		return
	}
	cur := c.value(t)
	d := to - cur
	if math.Abs(d) < wheelEpsilon {
		c.from, c.to, c.t0, c.dur = to, to, t, 0
		return
	}
	v := c.velocity(t)
	dur := WheelDuration(d)
	if b, ok := wheelVelocityDuration(v, d); ok && b < dur {
		dur = b
	}
	if dur.Seconds() < wheelEpsilon {
		c.from, c.to, c.t0, c.dur = to, to, t, 0
		return
	}
	slope := min(max(v*dur.Seconds()/d, -wheelMaxSlope), wheelMaxSlope)
	c.from, c.to, c.t0, c.dur, c.y1 = cur, to, t, dur, wheelX1*slope
}

// wheelVelocityDuration is VelocityBasedDurationBound: how long reaching
// d takes at velocity v (px/s), times the fudge factor; ok is false
// when it doesn't bound anything (no velocity, or moving the other way).
func wheelVelocityDuration(v, d float64) (time.Duration, bool) {
	if math.Abs(d) < wheelEpsilon {
		return 0, true
	}
	if math.Abs(v) < wheelEpsilon {
		return 0, false
	}
	b := d / v * wheelVelocityBound
	if b < 0 {
		return 0, false
	}
	return time.Duration(b * float64(time.Second)), true
}

// Tick reports the distance moved since the last Tick, at time t.
func (c *WheelCurve) Tick(t time.Time) float64 {
	if !c.active {
		return 0
	}
	v := c.value(t)
	d := v - c.pos
	c.pos = v
	if !t.Before(c.t0.Add(c.dur)) {
		*c = WheelCurve{}
	}
	return d
}

// value is the curve's position at t (GetValue).
func (c *WheelCurve) value(t time.Time) float64 {
	e := t.Sub(c.t0)
	if c.dur <= 0 || e >= c.dur {
		return c.to
	}
	if e <= 0 {
		return c.from
	}
	b := cubicBezier(wheelX1, c.y1, wheelX2, wheelY2)
	return c.from + (c.to-c.from)*b.y(b.solveX(e.Seconds()/c.dur.Seconds()))
}

// velocity is the curve's speed at t in px/s (CalculateVelocity).
func (c *WheelCurve) velocity(t time.Time) float64 {
	if c.dur <= 0 {
		return 0
	}
	x := min(max(t.Sub(c.t0).Seconds()/c.dur.Seconds(), 0), 1)
	b := cubicBezier(wheelX1, c.y1, wheelX2, wheelY2)
	return b.slope(x) * (c.to - c.from) / c.dur.Seconds()
}

// bezier is gfx::CubicBezier (ui/gfx/geometry/cubic_bezier.cc) for x in
// [0, 1]: the curve from (0,0) to (1,1) through two control points.
type bezier struct{ ax, bx, cx, ay, by, cy float64 }

func cubicBezier(x1, y1, x2, y2 float64) bezier {
	cx := 3 * x1
	bx := 3*(x2-x1) - cx
	cy := 3 * y1
	by := 3*(y2-y1) - cy
	return bezier{ax: 1 - cx - bx, bx: bx, cx: cx, ay: 1 - cy - by, by: by, cy: cy}
}

func (b bezier) x(t float64) float64  { return ((b.ax*t+b.bx)*t + b.cx) * t }
func (b bezier) y(t float64) float64  { return ((b.ay*t+b.by)*t + b.cy) * t }
func (b bezier) dx(t float64) float64 { return (3*b.ax*t+2*b.bx)*t + b.cx }
func (b bezier) dy(t float64) float64 { return (3*b.ay*t+2*b.by)*t + b.cy }

// solveX finds the curve parameter whose x is x: kMaxNewtonIterations (4)
// of Newton's method, then bisection, to kBezierEpsilon
// (CubicBezier::SolveCurveX).
func (b bezier) solveX(x float64) float64 {
	const eps = 1e-7
	t := x
	for i := 0; i < 4; i++ {
		x2 := b.x(t) - x
		if math.Abs(x2) < eps {
			return t
		}
		d := b.dx(t)
		if math.Abs(d) < eps {
			break
		}
		t -= x2 / d
	}
	lo, hi := 0.0, 1.0
	t = x
	for i := 0; i < 64 && hi-lo > eps; i++ {
		if b.x(t) < x {
			lo = t
		} else {
			hi = t
		}
		t = (lo + hi) / 2
	}
	return t
}

// slope is dy/dx at x (CubicBezier::Slope).
func (b bezier) slope(x float64) float64 {
	t := b.solveX(x)
	dx, dy := b.dx(t), b.dy(t)
	if dx == 0 && dy == 0 {
		return 0
	}
	return dy / dx
}

// Wheel animates notched wheel scrolling for a widget that reads
// pointer.Scroll events itself rather than through Scroll.
type Wheel struct {
	X, Y WheelCurve
}

// Scroll takes a pointer.Scroll event at time t and returns the distance
// to scroll now: all of it for precise input (which also stops a running
// animation), nothing for a notch, whose distance Tick reports as it
// animates. rx and ry bound how far the widget can scroll from where it
// is, as pointer.Filter's ranges do.
func (w *Wheel) Scroll(t time.Time, e pointer.Event, rx, ry pointer.ScrollRange) f32.Point {
	if !e.Notched || !WheelAnimation() || t.IsZero() {
		w.X.Stop()
		w.Y.Stop()
		return e.Scroll
	}
	if e.Scroll.X != 0 {
		w.X.Add(t, float64(e.Scroll.X), float64(rx.Min), float64(rx.Max))
	}
	if e.Scroll.Y != 0 {
		w.Y.Add(t, float64(e.Scroll.Y), float64(ry.Min), float64(ry.Max))
	}
	return f32.Point{}
}

// Tick reports the distance the animation moved since the last Tick, and
// asks q for another frame while it runs.
func (w *Wheel) Tick(q input.Source, t time.Time) f32.Point {
	d := f32.Point{X: float32(w.X.Tick(t)), Y: float32(w.Y.Tick(t))}
	if w.Active() {
		q.Execute(op.InvalidateCmd{})
	}
	return d
}

// Active reports whether an animation is running.
func (w *Wheel) Active() bool { return w.X.Active() || w.Y.Active() }

// Stop drops a running animation.
func (w *Wheel) Stop() {
	w.X.Stop()
	w.Y.Stop()
}

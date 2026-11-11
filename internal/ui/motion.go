package ui

import (
	"math"
	"time"
)

// Motion for Clack's own surfaces (the introduction, the sign-in card).
//
// Everything moves on damped springs, solved in closed form: a value is a
// function of the time since its spring was released, so frames can come
// at any rate (60 fps, 120 fps, or a headless renderer stepping a virtual
// clock in CI) and the motion looks the same. A screen keeps asking for
// frames (gtx.Execute(op.InvalidateCmd{})) only while one of its springs
// is still moving, then goes idle.
//
// Two families: critically damped (damping 1) arrives as fast as it can
// without overshooting, for fades, slides and colour; slightly underdamped
// (damping ~0.7) overshoots once by a few percent and settles, for things
// that land (a card, the logo, a switch's knob).

// spring is a damped harmonic oscillator. response is the period of the
// undamped oscillation in seconds (smaller is snappier) and damping the
// damping ratio: 1 is critical, below 1 overshoots.
type spring struct {
	response float64
	damping  float64
}

var (
	// springSmooth: critically damped, for fades and slides.
	springSmooth = spring{response: 0.42, damping: 1}
	// springSnappy: critically damped and quick, for small state
	// (focus rings, hover, presses).
	springSnappy = spring{response: 0.24, damping: 1}
	// springBouncy: slightly underdamped (about 5% overshoot), for
	// entrances and things that land.
	springBouncy = spring{response: 0.52, damping: 0.72}
	// springPop: underdamped and quick (about 10% overshoot), for small
	// elements popping in (a reaction, a badge, the logo's accents).
	springPop = spring{response: 0.36, damping: 0.6}
	// springGentle: critically damped and slow, for long, quiet fades.
	springGentle = spring{response: 1.1, damping: 1}
	// springShake: a stiff, lightly damped spring kicked sideways, the
	// sign-in card's "no" (about three swings).
	springShake = spring{response: 0.11, damping: 0.16}
)

// motionEps is the distance (in units of the travel) under which a
// spring counts as at rest: 0.1% of a 400 px slide is under half a pixel.
const motionEps = 1e-3

func (s spring) omega() float64 { return 2 * math.Pi / s.response }

// evolve advances an offset from rest x0 with velocity v0 by t seconds:
// the oscillator's exact solution, so any step size is stable.
func (s spring) evolve(x0, v0, t float64) (x, v float64) {
	if t <= 0 {
		return x0, v0
	}
	w, z := s.omega(), s.damping
	switch {
	case z < 1:
		wd := w * math.Sqrt(1-z*z)
		e := math.Exp(-z * w * t)
		c, sn := math.Cos(wd*t), math.Sin(wd*t)
		b := (v0 + z*w*x0) / wd
		x = e * (x0*c + b*sn)
		v = e * ((b*wd-z*w*x0)*c - (x0*wd+z*w*b)*sn)
	case z == 1:
		e := math.Exp(-w * t)
		b := v0 + w*x0
		x = (x0 + b*t) * e
		v = (v0 - w*b*t) * e
	default:
		r := w * math.Sqrt(z*z-1)
		r1, r2 := -z*w+r, -z*w-r
		c2 := (v0 - r1*x0) / (r2 - r1)
		c1 := x0 - c2
		e1, e2 := math.Exp(r1*t), math.Exp(r2*t)
		x = c1*e1 + c2*e2
		v = c1*r1*e1 + c2*r2*e2
	}
	return x, v
}

// at is the spring's step response: 0 at rest at t ≤ 0, heading for 1.
// Underdamped springs pass 1 and come back.
func (s spring) at(t float64) float64 {
	if t <= 0 {
		return 0
	}
	x, _ := s.evolve(-1, 0, t)
	return 1 + x
}

// since is at for the time from start to now.
func (s spring) since(start, now time.Time) float64 {
	return s.at(now.Sub(start).Seconds())
}

// settle is how long a step takes to come within motionEps of its end
// and stay there.
func (s spring) settle() float64 {
	w, z := s.omega(), s.damping
	switch {
	case z < 1:
		// the envelope e^(-ζωt)/√(1-ζ²) bounds the error
		return math.Log(1/(motionEps*math.Sqrt(1-z*z))) / (z * w)
	case z == 1:
		// e^(-ωt)(1+ωt) = motionEps at ωt ≈ 9.2334 (the speed is
		// already below motionEps·ω there)
		return 9.2334 / w
	}
	// overdamped: walk the monotone error out
	t, dt := 0.0, s.response/16
	for i := 0; i < 4096; i++ {
		t += dt
		x, v := s.evolve(-1, 0, t)
		if math.Abs(x) < motionEps && math.Abs(v) < motionEps*w {
			return t
		}
	}
	return t
}

// settled reports whether a step released at start is at rest by now.
func (s spring) settled(start, now time.Time) bool {
	return now.Sub(start).Seconds() >= s.settle()
}

// kick is the spring's impulse response, normalised so its first swing
// peaks at about ±1: x(t) for x(0) = 0 and a push at t = 0 (the shake).
func (s spring) kick(t float64) float64 {
	if t <= 0 {
		return 0
	}
	w, z := s.omega(), s.damping
	if z >= 1 {
		return 0
	}
	wd := w * math.Sqrt(1-z*z)
	// first peak of e^(-ζωt)·sin(ωd·t): at tan(ωd·t) = ωd/(ζω)
	tp := math.Atan2(wd, z*w) / wd
	peak := math.Exp(-z*w*tp) * math.Sin(wd*tp)
	return math.Exp(-z*w*t) * math.Sin(wd*t) / peak
}

// springVal is an interruptible value: it follows its target on spring s,
// keeping its velocity when the target changes mid-flight (a focus ring
// that grows and then shrinks again doesn't jump).
type springVal struct {
	s      spring
	x, v   float64
	target float64
	t      time.Time
	init   bool
}

// get moves the value to now, heading for target, and reports whether it
// is still moving (the caller keeps asking for frames while it is).
func (sv *springVal) get(now time.Time, target float64) (float64, bool) {
	if !sv.init {
		sv.init, sv.x, sv.v, sv.target, sv.t = true, target, 0, target, now
		return target, false
	}
	if target != sv.target {
		sv.target = target
	}
	dt := now.Sub(sv.t).Seconds()
	if dt < 0 {
		dt = 0
	}
	sv.t = now
	x, v := sv.s.evolve(sv.x-sv.target, sv.v, dt)
	if math.Abs(x) < motionEps && math.Abs(v) < motionEps*sv.s.omega() {
		sv.x, sv.v = sv.target, 0
		return sv.x, false
	}
	sv.x, sv.v = sv.target+x, v
	return sv.x, true
}

// set jumps to x at rest (no animation).
func (sv *springVal) set(now time.Time, x float64) {
	sv.init, sv.x, sv.v, sv.target, sv.t = true, x, 0, x, now
}

// push adds velocity (units per second): a kick from where it is.
func (sv *springVal) push(now time.Time, v float64) {
	if !sv.init {
		sv.set(now, 0)
	}
	sv.get(now, sv.target)
	sv.v += v
}

// lerp is a + (b-a)·t.
func lerp(a, b, t float64) float64 { return a + (b-a)*t }

// msec is a duration in milliseconds, for stagger offsets. (clamp01,
// for opacity from an overshooting spring, is colorpicker.go's.)
func msec(n float64) time.Duration { return time.Duration(n * float64(time.Millisecond)) }

// motionClock tracks whether anything drawn this frame is still moving.
type motionClock struct {
	now    time.Time
	moving bool
	// wake is the earliest future moment something starts moving by
	// itself (an auto-advance, a staggered element still waiting).
	wake time.Time
}

// spring returns s's step response for a release at start, and notes
// whether it is still moving.
func (m *motionClock) spring(s spring, start time.Time) float64 {
	t := m.now.Sub(start).Seconds()
	if t < 0 {
		m.wakeAt(start)
		return 0
	}
	if t < s.settle() {
		m.moving = true
	}
	return s.at(t)
}

// wakeAt asks for a frame at t (if it is in the future).
func (m *motionClock) wakeAt(t time.Time) {
	if !t.After(m.now) {
		m.moving = true
		return
	}
	if m.wake.IsZero() || t.Before(m.wake) {
		m.wake = t
	}
}

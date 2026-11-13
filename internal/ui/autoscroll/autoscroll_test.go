package autoscroll

import (
	"image"
	"math"
	"runtime"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/gesture"
	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
)

const frame = time.Second / 60

var t0 = time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)

// chromiumSpeed is Chromium's arithmetic written out on its own:
// HandleMouseMoveForMiddleClickAutoscroll's pow(|d|, 2.2) × -0.000008
// toward the pointer (negative: a fling's velocity is the finger's), then
// FixedVelocityCurve's offset = velocity × 5000 × seconds, which a
// GestureScrollUpdate scrolls the other way.
func chromiumSpeed(d float64) float64 {
	if math.Abs(d) <= 15 {
		return 0
	}
	sign := 1.0
	if d < 0 {
		sign = -1
	}
	velocity := math.Pow(math.Abs(d), 2.2) * -0.000008 * sign
	return -velocity * 5000
}

func TestSpeedCurve(t *testing.T) {
	for _, d := range []float32{0, 1, 15, -15, 15.5, 16, -16, 30, 50, 100, -100, 200, 400} {
		got, want := float64(gesture.AutoscrollSpeed(d)), chromiumSpeed(float64(d))
		if math.Abs(got-want) > 1e-3*math.Max(1, math.Abs(want)) {
			t.Errorf("AutoscrollSpeed(%v) = %.3f dp/s, want %.3f", d, got, want)
		}
	}
	// the curve's shape: 0 in the dead zone, ~18 dp/s just past it,
	// ~1000 dp/s at 100 dp, 2^2.2 (4.6) times faster for twice the distance
	if v := gesture.AutoscrollSpeed(100); v < 1000 || v > 1010 {
		t.Errorf("100dp: %v dp/s, want about 1004.7", v)
	}
	if r := gesture.AutoscrollSpeed(200) / gesture.AutoscrollSpeed(100); math.Abs(float64(r)-math.Pow(2, 2.2)) > 1e-3 {
		t.Errorf("200dp/100dp speed ratio %v, want 2^2.2", r)
	}
}

// env is a 300x400 window with a vertical layout.List of rows 50px high
// (optionally each with a horizontal list of ten 100px cells), its
// Autoscroller run the way Clack's App.frame runs it, through a real
// input.Router.
type env struct {
	r      input.Router
	ops    op.Ops
	as     gesture.Autoscroller
	list   layout.List
	rows   int
	nested bool
	inner  [100]layout.List
	hide   bool // the list isn't laid out
	link   bool // the rows are links: they veto the press
	metric unit.Metric

	rowTag  int
	presses []pointer.Buttons // presses the rows saw
	keys    []key.Name        // Escapes a reader after Begin saw
	ms      time.Duration     // event times
}

func newEnv(t *testing.T) *env {
	gesture.SetAutoscroll(true)
	t.Cleanup(func() { gesture.SetAutoscroll(runtime.GOOS == "windows") })
	return &env{rows: 100, metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
}

var size = image.Pt(300, 400)

func (e *env) frame(at time.Time) {
	e.r.WakeupTime() // the router keeps a wake-up until it's read: only this frame's count
	e.ops.Reset()
	gtx := layout.Context{Ops: &e.ops, Now: at, Metric: e.metric, Constraints: layout.Exact(size), Source: e.r.Source()}
	e.as.Begin(gtx.Source, gtx.Metric)
	for { // the window's Escape (a dialog's, the shortcuts')
		ev, ok := gtx.Event(key.Filter{Name: key.NameEscape})
		if !ok {
			break
		}
		if ke, ok := ev.(key.Event); ok && ke.State == key.Press {
			e.keys = append(e.keys, ke.Name)
		}
	}
	if !e.hide {
		e.list.Axis = layout.Vertical
		e.list.Layout(gtx, e.rows, func(gtx layout.Context, i int) layout.Dimensions {
			for {
				ev, ok := gtx.Event(pointer.Filter{Target: &e.rowTag, Kinds: pointer.Press})
				if !ok {
					break
				}
				if pe, ok := ev.(pointer.Event); ok && pe.Kind == pointer.Press {
					e.presses = append(e.presses, pe.Buttons)
					if e.link && gesture.AutoscrollPress(pe) {
						gesture.AutoscrollVeto(gtx.Source, pe)
					}
				}
			}
			rs := image.Pt(size.X, 50)
			defer clip.Rect{Max: rs}.Push(gtx.Ops).Pop()
			event.Op(gtx.Ops, &e.rowTag)
			if e.nested {
				gtx.Constraints = layout.Exact(rs)
				e.inner[i].Axis = layout.Horizontal
				e.inner[i].Layout(gtx, 10, func(gtx layout.Context, j int) layout.Dimensions {
					return layout.Dimensions{Size: image.Pt(100, 50)}
				})
			}
			return layout.Dimensions{Size: rs}
		})
	}
	e.as.End(gtx.Ops, gtx.Source, size)
	e.r.Frame(&e.ops)
}

func (e *env) at(i int) time.Time { return t0.Add(time.Duration(i) * frame) }

func (e *env) scrolled() int { return e.list.Position.First*50 + e.list.Position.Offset }

func (e *env) wants() bool { _, ok := e.r.WakeupTime(); return ok }

func (e *env) ev(k pointer.Kind, p f32.Point, b pointer.Buttons) {
	e.ms += 10 * time.Millisecond
	e.r.Queue(pointer.Event{Kind: k, Source: pointer.Mouse, Position: p, Buttons: b, Time: e.ms})
}

var origin = f32.Pt(150, 200)

// Holding the middle button and moving away scrolls toward the pointer at
// Chromium's speed, frame by frame; releasing after the move stops it.
func TestHoldScrollsAndReleaseStops(t *testing.T) {
	e := newEnv(t)
	e.frame(e.at(0))
	e.ev(pointer.Press, origin, pointer.ButtonTertiary)
	e.frame(e.at(0))
	if !e.as.Active() || e.as.Toggled() {
		t.Fatalf("after a middle press over the list: active %v, toggled %v; want held autoscroll", e.as.Active(), e.as.Toggled())
	}
	if c := e.r.Cursor(); c != pointer.CursorMiddlePanningVertical {
		t.Errorf("cursor at the origin %v, want MiddlePanningVertical (the list scrolls up and down only)", c)
	}
	e.frame(e.at(0)) // the grab's Cancels to the other handlers are one frame's events
	if e.wants() {
		t.Errorf("asked for frames before the pointer left the origin")
	}
	e.ev(pointer.Move, origin.Add(f32.Pt(0, 100)), pointer.ButtonTertiary)
	e.frame(e.at(0))
	if v := e.as.Velocity(); math.Abs(float64(v.Y)-chromiumSpeed(100)) > 0.1 || v.X != 0 {
		t.Errorf("100px below: velocity %v, want (0, %.2f) px/s", v, chromiumSpeed(100))
	}
	if c := e.r.Cursor(); c != pointer.CursorSouthPanning {
		t.Errorf("cursor below the origin %v, want SouthPanning", c)
	}
	if !e.wants() {
		t.Fatalf("no next frame asked for once it moves")
	}
	for i := 1; i <= 30; i++ {
		e.frame(e.at(i))
		if !e.wants() {
			t.Fatalf("frame %d: no next frame asked for while scrolling", i)
		}
	}
	want := chromiumSpeed(100) * 0.5 // 30 frames at 60 Hz
	if got := float64(e.scrolled()); math.Abs(got-want) > 1.5 {
		t.Errorf("half a second at 100px: scrolled %vpx, want %.1f", got, want)
	}
	if len(e.presses) != 1 {
		t.Errorf("the rows saw %d presses, want the middle press only", len(e.presses))
	}
	e.ev(pointer.Release, origin.Add(f32.Pt(0, 100)), 0)
	e.frame(e.at(31))
	if e.as.Active() {
		t.Fatalf("still running after the release that ended a drag")
	}
	at := e.scrolled()
	e.frame(e.at(40))
	if e.scrolled() != at || e.wants() {
		t.Errorf("after it stopped: moved %dpx, asking for frames %v; want neither", e.scrolled()-at, e.wants())
	}
	if c := e.r.Cursor(); c != pointer.CursorDefault {
		t.Errorf("cursor after it stopped: %v, want the default", c)
	}
}

// Pressing and releasing the middle button without leaving the 15dp dead
// zone leaves it running (toggled) until the next click, which stops it
// and reaches nothing under the pointer.
func TestClickTogglesAndAnyClickStops(t *testing.T) {
	e := newEnv(t)
	e.list.Position.First = 50
	e.frame(e.at(0))
	e.ev(pointer.Press, origin, pointer.ButtonTertiary)
	e.frame(e.at(0))
	e.ev(pointer.Move, origin.Add(f32.Pt(10, -12)), pointer.ButtonTertiary)
	e.frame(e.at(1))
	if e.as.Velocity() != (f32.Point{}) || e.wants() || e.scrolled() != 2500 {
		t.Errorf("inside the dead zone: velocity %v, frames %v, at %d; want none, none, 2500", e.as.Velocity(), e.wants(), e.scrolled())
	}
	e.ev(pointer.Release, origin.Add(f32.Pt(10, -12)), 0)
	e.frame(e.at(2))
	if !e.as.Toggled() {
		t.Fatalf("a middle click without a move: toggled %v, active %v; want toggled", e.as.Toggled(), e.as.Active())
	}
	e.ev(pointer.Move, origin.Add(f32.Pt(0, -60)), 0)
	e.frame(e.at(3))
	if c := e.r.Cursor(); c != pointer.CursorNorthPanning {
		t.Errorf("cursor above the origin %v, want NorthPanning", c)
	}
	for i := 4; i <= 63; i++ {
		e.frame(e.at(i))
	}
	want := 2500 - chromiumSpeed(60)*1 // a second at 60px above
	if got := float64(e.scrolled()); math.Abs(got-want) > 1.5 {
		t.Errorf("a second at 60px above: at %vpx, want %.1f", got, want)
	}
	presses := len(e.presses)
	e.ev(pointer.Press, origin.Add(f32.Pt(0, -60)), pointer.ButtonPrimary)
	e.ev(pointer.Release, origin.Add(f32.Pt(0, -60)), 0)
	e.frame(e.at(64))
	e.frame(e.at(65))
	if e.as.Active() {
		t.Fatalf("a left click didn't stop the toggled autoscroll")
	}
	if len(e.presses) != presses {
		t.Errorf("the click that stopped it reached a row (%v)", e.presses[presses:])
	}
	at := e.scrolled()
	e.frame(e.at(70))
	if e.scrolled() != at || e.wants() {
		t.Errorf("after the stopping click: moved %dpx, frames %v", e.scrolled()-at, e.wants())
	}
	// the next click is an ordinary click again
	e.ev(pointer.Press, origin, pointer.ButtonPrimary)
	e.ev(pointer.Release, origin, 0)
	e.frame(e.at(71))
	if len(e.presses) != presses+1 {
		t.Errorf("a click after it stopped: the rows saw %d new presses, want 1", len(e.presses)-presses)
	}
}

// A press and release in the same frame (a quick click) toggles too.
func TestQuickClickToggles(t *testing.T) {
	e := newEnv(t)
	e.frame(e.at(0))
	e.ev(pointer.Press, origin, pointer.ButtonTertiary)
	e.ev(pointer.Release, origin, 0)
	e.frame(e.at(0))
	e.frame(e.at(1))
	if !e.as.Toggled() {
		t.Fatalf("toggled %v, active %v after a quick middle click; want toggled", e.as.Toggled(), e.as.Active())
	}
	e.ev(pointer.Press, origin, pointer.ButtonTertiary) // a second middle click stops it
	e.ev(pointer.Release, origin, 0)
	e.frame(e.at(2))
	e.frame(e.at(3))
	if e.as.Active() {
		t.Errorf("a second middle click didn't stop it (or started another)")
	}
}

// Another button pressed while the middle one is held stops it.
func TestOtherButtonStopsHeld(t *testing.T) {
	e := newEnv(t)
	e.frame(e.at(0))
	e.ev(pointer.Press, origin, pointer.ButtonTertiary)
	e.frame(e.at(0))
	e.ev(pointer.Move, origin.Add(f32.Pt(0, 80)), pointer.ButtonTertiary)
	e.frame(e.at(1))
	e.ev(pointer.Press, origin.Add(f32.Pt(0, 80)), pointer.ButtonTertiary|pointer.ButtonPrimary)
	e.frame(e.at(2))
	if e.as.Active() {
		t.Fatalf("a left press while holding the middle button didn't stop it")
	}
	if len(e.presses) != 1 {
		t.Errorf("the rows saw %d presses, want only the first (the grab holds the rest)", len(e.presses))
	}
}

// Any key stops it and is swallowed, as Blink's KeyboardEventManager does;
// the next key goes through.
func TestKeyStopsAndIsSwallowed(t *testing.T) {
	e := newEnv(t)
	e.frame(e.at(0))
	e.ev(pointer.Press, origin, pointer.ButtonTertiary)
	e.ev(pointer.Release, origin, 0)
	e.frame(e.at(0))
	e.frame(e.at(1))
	if !e.as.Toggled() {
		t.Fatalf("not toggled")
	}
	esc := func() {
		e.r.Queue(key.Event{Name: key.NameEscape, State: key.Press}, key.Event{Name: key.NameEscape, State: key.Release})
	}
	esc()
	e.frame(e.at(2))
	if e.as.Active() {
		t.Fatalf("Escape didn't stop it")
	}
	if len(e.keys) != 0 {
		t.Errorf("the Escape that stopped it reached the window's reader")
	}
	esc()
	e.frame(e.at(3))
	if len(e.keys) != 1 {
		t.Errorf("the next Escape: the reader saw %d, want 1", len(e.keys))
	}
}

// No autoscroll where nothing overflows, over a link, or with it off
// (Linux and macOS, as Chromium); the press goes on as usual.
func TestNoStart(t *testing.T) {
	for _, c := range []struct {
		name string
		set  func(*env)
	}{
		{"content fits", func(e *env) { e.rows = 5 }},
		{"a link", func(e *env) { e.link = true }},
		{"off", func(e *env) { gesture.SetAutoscroll(false) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			c.set(e)
			e.frame(e.at(0))
			e.ev(pointer.Press, origin, pointer.ButtonTertiary)
			e.frame(e.at(0))
			if e.as.Active() {
				t.Errorf("autoscroll started")
			}
			if len(e.presses) != 1 {
				t.Errorf("the row saw %d presses, want 1", len(e.presses))
			}
			e.ev(pointer.Move, origin.Add(f32.Pt(0, 100)), pointer.ButtonTertiary)
			e.frame(e.at(1))
			e.frame(e.at(2))
			if e.scrolled() != 0 || e.wants() {
				t.Errorf("moved %dpx, frames %v", e.scrolled(), e.wants())
			}
		})
	}
}

// At the end of the list it stops asking for frames (nothing would move)
// while staying on; turned back toward the content it moves again.
func TestStallsAtTheEnd(t *testing.T) {
	e := newEnv(t)
	e.list.Position.First = 90 // 10 rows: 500px in a 400px window
	e.frame(e.at(0))
	e.ev(pointer.Press, origin, pointer.ButtonTertiary)
	e.frame(e.at(0))
	e.ev(pointer.Move, origin.Add(f32.Pt(0, 150)), pointer.ButtonTertiary)
	for i := 0; i < 20; i++ {
		e.frame(e.at(i))
	}
	if !e.as.Active() {
		t.Fatalf("stopped at the end")
	}
	if e.wants() {
		t.Errorf("at the end it still asks for frames")
	}
	at := e.scrolled()
	if at != 4600 {
		t.Errorf("at %dpx, want the end (4600)", at)
	}
	e.ev(pointer.Move, origin.Add(f32.Pt(0, -150)), pointer.ButtonTertiary)
	e.frame(e.at(20))
	e.frame(e.at(21))
	if e.scrolled() >= at || !e.wants() {
		t.Errorf("turned up: at %d (from %d), frames %v; want it moving up", e.scrolled(), at, e.wants())
	}
}

// The scroller going away (navigating elsewhere) ends it.
func TestScrollerGoneStops(t *testing.T) {
	e := newEnv(t)
	e.frame(e.at(0))
	e.ev(pointer.Press, origin, pointer.ButtonTertiary)
	e.ev(pointer.Release, origin, 0)
	e.frame(e.at(0))
	e.hide = true
	e.frame(e.at(1))
	if e.as.Active() {
		t.Errorf("running with no scroller laid out")
	}
}

// The distance is in dp: at 2 px per dp, 200px is Chromium's 100 DIPs,
// at twice the pixels a second.
func TestSpeedInDp(t *testing.T) {
	e := newEnv(t)
	e.metric = unit.Metric{PxPerDp: 2, PxPerSp: 2}
	e.frame(e.at(0))
	e.ev(pointer.Press, origin, pointer.ButtonTertiary)
	e.frame(e.at(0))
	e.ev(pointer.Move, origin.Add(f32.Pt(0, 200)), pointer.ButtonTertiary)
	e.frame(e.at(1))
	if v := e.as.Velocity(); math.Abs(float64(v.Y)-2*chromiumSpeed(100)) > 0.2 {
		t.Errorf("200px at 2px/dp: %v px/s, want %.2f", v.Y, 2*chromiumSpeed(100))
	}
}

// Nested scrollers: Chromium takes, per axis, the innermost box that can
// scroll that way. A press over a row's sideways list scrolls it sideways
// and the outer list up and down; the cursor shows both.
func TestNestedPerAxis(t *testing.T) {
	e := newEnv(t)
	e.nested = true
	e.frame(e.at(0))
	e.ev(pointer.Press, origin, pointer.ButtonTertiary) // row 4
	e.frame(e.at(0))
	e.ev(pointer.Move, origin.Add(f32.Pt(80, 80)), pointer.ButtonTertiary)
	// 20 frames: row 4 (y 200-250) is still on screen, so its list is
	// still laid out (a row scrolled away is gone, as Slack's virtual list
	// unmounts it)
	for i := 1; i <= 20; i++ {
		e.frame(e.at(i))
	}
	if c := e.r.Cursor(); c != pointer.CursorSouthEastPanning {
		t.Errorf("cursor %v, want SouthEastPanning", c)
	}
	in := &e.inner[4]
	x := in.Position.First*100 + in.Position.Offset
	want := chromiumSpeed(80) * 19 / 60 // the move's own frame starts it
	if math.Abs(float64(x)-want) > 2 || math.Abs(float64(e.scrolled())-want) > 2 {
		t.Errorf("after 19 frames at (80, 80): inner list at x=%d, outer at y=%d; want both about %.1f", x, e.scrolled(), want)
	}
	for i := range e.inner {
		if p := e.inner[i].Position; i != 4 && (p.First != 0 || p.Offset != 0) {
			t.Errorf("row %d's list moved too", i)
		}
	}
}

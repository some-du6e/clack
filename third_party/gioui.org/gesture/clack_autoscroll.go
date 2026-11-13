// SPDX-License-Identifier: Unlicense OR MIT

package gesture

// Clack patch 15 (CLACK_PATCHES.md): middle-click autoscroll, as Chromium
// does it on Windows (Slack's desktop app). Read in Chromium main 5ed4bb5
// (2026-11-13):
//
//   - third_party/blink/renderer/core/dom/node.cc DefaultEventHandler: a
//     middle mousedown that isn't in a link starts it on the nearest box
//     that IsUserScrollable (its content overflows either way).
//   - core/page/autoscroll_controller.cc: StartMiddleClickAutoscroll picks,
//     walking up from that box, the first that can scroll vertically and
//     the first that can scroll horizontally; the mode is kMiddleClickInitial.
//     HandleMouseMoveForMiddleClickAutoscroll: per axis, no speed within
//     kNoMiddleClickAutoscrollRadius (15) DIPs of the origin, else
//     |d|^kExponent (2.2) × kMultiplier (-0.000008) toward the pointer;
//     the first change of speed makes it kMiddleClickHolding; the cursor
//     follows MiddleClickAutoscrollCursor. HandleMouseReleaseForMiddleClick-
//     Autoscroll: releasing the middle button while still kMiddleClickInitial
//     makes it kMiddleClickToggled; any release while kMiddleClickHolding
//     stops it.
//   - core/input/event_handler.cc HandleMousePressEvent: any press while it
//     runs stops it and is swallowed (kHandledSuppressed, InvalidateClick).
//     core/input/keyboard_event_manager.cc KeyEvent: any key down stops it
//     and every key event is swallowed while it runs.
//   - content/browser/renderer_host/render_widget_host_impl.cc
//     AutoscrollFling, ui/events/gestures/fixed_velocity_curve.cc: the speed
//     is a fling that never decays, offset = speed × 5000
//     (kConstantMultiplier) × seconds. So the speed is
//     0.04 × |d|^2.2 DIPs a second.
//   - third_party/blink/renderer/core/exported/web_view_impl.cc
//     ApplyWebPreferences: on in Windows, behind
//     features::kMiddleClickAutoscroll on Linux (off by default; a middle
//     click pastes there), never on macOS.
//
// Chromium paints nothing at the origin: the origin icon is the cursor,
// kMiddlePanning (the round four-arrow pan_middle.cur) where the button
// went down, then the directional panning cursors. Clack draws them the
// same way (pointer.CursorMiddlePanning…, Chromium's images on Windows).
//
// The pieces: an Autoscroller per window (the app calls Begin before its
// layout and End after it; End adds the window-wide input area and holds
// the pointer grab), and an Autoscroll in each scroller (Scroll has one)
// that offers itself on the middle press and moves by Tick.

import (
	"image"
	"math"
	"runtime"
	"sync/atomic"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
)

const (
	// kNoMiddleClickAutoscrollRadius, in DIPs (dp).
	AutoscrollRadius = 15
	// kExponent, and kMultiplier × FixedVelocityCurve's kConstantMultiplier.
	autoscrollExponent = 2.2
	autoscrollFactor   = 0.000008 * 5000
)

var autoscrollOff atomic.Bool

func init() {
	// MiddleClickAutoscroll: on in Windows only (web_view_impl.cc)
	autoscrollOff.Store(runtime.GOOS != "windows")
}

// SetAutoscroll turns middle-click autoscroll on or off for every window.
// It is on in Windows and off elsewhere, as in Chromium.
func SetAutoscroll(on bool) { autoscrollOff.Store(!on) }

// AutoscrollEnabled reports whether a middle press can start autoscroll.
func AutoscrollEnabled() bool { return !autoscrollOff.Load() }

// AutoscrollSpeed is Chromium's autoscroll speed along one axis, in dp a
// second, with the pointer d dp from the origin along it: none within
// AutoscrollRadius, else 0.04 × |d|^2.2, toward the pointer.
func AutoscrollSpeed(d float32) float32 {
	a := math.Abs(float64(d))
	if a <= AutoscrollRadius {
		return 0
	}
	v := float32(math.Pow(a, autoscrollExponent) * autoscrollFactor)
	if d < 0 {
		return -v
	}
	return v
}

type autoscrollMode uint8

const (
	autoIdle    autoscrollMode = iota
	autoPending                // a middle press this frame; End decides
	autoInitial                // kMiddleClickInitial
	autoHolding                // kMiddleClickHolding
	autoToggled                // kMiddleClickToggled
)

type autoscrollKey struct{}

// Autoscroller is one window's middle-click autoscroll (Blink's
// AutoscrollController with the browser's fixed-velocity fling).
type Autoscroller struct {
	tag     int
	mode    autoscrollMode
	frame   uint32
	pid     pointer.ID
	press   time.Duration // the starting press's event time
	vetoed  bool          // a link took the press
	release bool          // the middle button came up before End saw the press
	buttons pointer.Buttons
	origin  f32.Point // window coordinates
	pos     f32.Point
	pxPerDp float32
	v       f32.Point // px/s; positive scrolls toward the end
	cursor  pointer.Cursor
	vert    *Autoscroll // the scroller moving up and down
	horiz   *Autoscroll // the one moving sideways
}

// Autoscroll is a scroller's part in its window's autoscroll.
type Autoscroll struct {
	seen       uint32 // the Autoscroller frame it last offered or ticked in
	last       time.Time
	acc        [2]float64 // sub-pixel distance not reported yet
	canX, canY bool
}

func autoscrollerOf(q input.Source) *Autoscroller {
	c, _ := q.Value(autoscrollKey{}).(*Autoscroller)
	return c
}

// Active reports whether autoscroll is running (not idle, not pending).
func (c *Autoscroller) Active() bool { return c.mode >= autoInitial }

// Toggled reports whether it runs without the button held (until the
// next click).
func (c *Autoscroller) Toggled() bool { return c.mode == autoToggled }

// Origin is where it started, in window coordinates.
func (c *Autoscroller) Origin() f32.Point { return c.origin }

// Velocity is its speed in pixels a second; positive is down or right.
func (c *Autoscroller) Velocity() f32.Point { return c.v }

// Cursor is the cursor it shows while it runs.
func (c *Autoscroller) Cursor() pointer.Cursor { return c.cursor }

// Stop ends it (StopMiddleClickAutoscroll).
func (c *Autoscroller) Stop() {
	c.mode = autoIdle
	c.v = f32.Point{}
	c.vert, c.horiz = nil, nil
	c.cursor = pointer.CursorDefault
}

var allModifiers = key.ModCtrl | key.ModCommand | key.ModShift | key.ModAlt | key.ModSuper

// Begin reads the window-wide input area's events. Call it at the start
// of each frame, before any scroller lays out, with the frame's metric.
func (c *Autoscroller) Begin(q input.Source, m unit.Metric) {
	c.frame++
	if q.Value(autoscrollKey{}) != c {
		q.SetValue(autoscrollKey{}, c)
	}
	c.pxPerDp = m.PxPerDp
	if c.pxPerDp <= 0 {
		c.pxPerDp = 1
	}
	if c.mode == autoPending { // End didn't run
		c.mode = autoIdle
	}
	c.events(q)
}

func (c *Autoscroller) events(q input.Source) {
	pf := pointer.Filter{Target: &c.tag, Kinds: pointer.Press | pointer.Release | pointer.Move | pointer.Drag | pointer.Cancel}
	for {
		var ev event.Event
		var ok bool
		if c.Active() {
			// every key, swallowed (KeyboardEventManager::KeyEvent)
			ev, ok = q.Event(pf, key.Filter{Optional: allModifiers})
		} else {
			ev, ok = q.Event(pf)
		}
		if !ok {
			return
		}
		switch e := ev.(type) {
		case key.Event:
			if e.State == key.Press {
				c.Stop()
			}
		case pointer.Event:
			c.pointer(e)
		}
	}
}

func (c *Autoscroller) pointer(e pointer.Event) {
	switch e.Kind {
	case pointer.Press:
		pressed := e.Buttons &^ c.buttons
		c.buttons = e.Buttons
		if c.Active() {
			// any press stops it, and goes no further: the toggled area
			// blocks it, and the grab holds a held button's
			c.Stop()
			return
		}
		if c.mode == autoIdle && AutoscrollEnabled() && e.Source == pointer.Mouse && pressed == pointer.ButtonTertiary {
			c.mode = autoPending
			c.pid = e.PointerID
			c.press = e.Time
			c.vetoed, c.release = false, false
			c.origin, c.pos = e.Position, e.Position
			c.vert, c.horiz = nil, nil
		}
	case pointer.Release:
		released := c.buttons &^ e.Buttons
		c.buttons = e.Buttons
		switch c.mode {
		case autoPending:
			if released.Contain(pointer.ButtonTertiary) {
				c.release = true
			}
		case autoInitial:
			if released.Contain(pointer.ButtonTertiary) {
				c.mode = autoToggled
			}
		case autoHolding:
			c.Stop()
		}
	case pointer.Move, pointer.Drag:
		c.buttons = e.Buttons
		c.pos = e.Position
		if c.Active() {
			c.update()
		}
	case pointer.Cancel:
		c.buttons = 0
		if c.mode != autoToggled {
			c.Stop()
		}
	}
}

// update is HandleMouseMoveForMiddleClickAutoscroll.
func (c *Autoscroller) update() {
	if c.vert != nil && c.horiz != nil && !c.vert.canX && !c.vert.canY && !c.horiz.canX && !c.horiz.canY {
		c.Stop() // neither box is user-scrollable any more
		return
	}
	d := c.pos.Sub(c.origin).Mul(1 / c.pxPerDp)
	v := f32.Point{X: AutoscrollSpeed(d.X) * c.pxPerDp, Y: AutoscrollSpeed(d.Y) * c.pxPerDp}
	if v == c.v {
		return
	}
	c.v = v
	if c.mode == autoInitial {
		c.mode = autoHolding
	}
	c.cursor = panCursor(v, c.vert != nil && c.vert.canY, c.horiz != nil && c.horiz.canX)
}

// panCursor is MiddleClickAutoscrollCursor: the direction the pointer is
// from the origin, among the axes that can scroll.
func panCursor(v f32.Point, vert, horiz bool) pointer.Cursor {
	east, west := v.X > 0, v.X < 0
	north, south := v.Y < 0, v.Y > 0
	switch {
	case north && vert:
		switch {
		case horiz && east:
			return pointer.CursorNorthEastPanning
		case horiz && west:
			return pointer.CursorNorthWestPanning
		}
		return pointer.CursorNorthPanning
	case south && vert:
		switch {
		case horiz && east:
			return pointer.CursorSouthEastPanning
		case horiz && west:
			return pointer.CursorSouthWestPanning
		}
		return pointer.CursorSouthPanning
	case east && horiz:
		return pointer.CursorEastPanning
	case west && horiz:
		return pointer.CursorWestPanning
	case vert && !horiz:
		return pointer.CursorMiddlePanningVertical
	case horiz && !vert:
		return pointer.CursorMiddlePanningHorizontal
	}
	return pointer.CursorMiddlePanning
}

// End starts an autoscroll a middle press asked for this frame, drops
// scrollers that weren't laid out, and adds the window-wide input area
// (above everything: call it last, with the window's size). While it is
// toggled the area takes every pointer event, so the click that stops it
// reaches nothing else; otherwise it passes them on.
func (c *Autoscroller) End(ops *op.Ops, q input.Source, size image.Point) {
	switch c.mode {
	case autoPending:
		if c.vetoed || (c.vert == nil && c.horiz == nil) {
			c.mode = autoIdle
			break
		}
		c.mode = autoInitial
		if c.release {
			c.mode = autoToggled
		} else {
			// the held button's drags and release come here wherever
			// the pointer goes, and nothing else sees them
			q.Execute(pointer.GrabCmd{Tag: &c.tag, ID: c.pid})
		}
		c.v = f32.Point{}
		c.cursor = panCursor(c.v, c.vert != nil && c.vert.canY, c.horiz != nil && c.horiz.canX)
		if c.pos != c.origin {
			c.update()
		}
	case autoInitial, autoHolding, autoToggled:
		// StopAutoscrollIfNeeded: a scroller that went away
		if c.vert != nil && c.vert.seen != c.frame {
			c.vert = nil
		}
		if c.horiz != nil && c.horiz.seen != c.frame {
			c.horiz = nil
		}
		if c.vert == nil && c.horiz == nil {
			c.Stop()
		}
	}
	defer clip.Rect{Max: size}.Push(ops).Pop()
	if c.mode != autoToggled {
		defer pointer.PassOp{}.Push(ops).Pop()
	}
	if c.Active() {
		c.cursor.Add(ops)
		c.events(q) // keeps the key filter registered for the next frame's keys
	}
	event.Op(ops, &c.tag)
}

// Veto keeps the middle press e from starting autoscroll: Chromium doesn't
// start it on a link.
func AutoscrollVeto(q input.Source, e pointer.Event) {
	if c := autoscrollerOf(q); c != nil && c.mode == autoPending && e.Time == c.press {
		c.vetoed = true
	}
}

// AutoscrollPress reports whether e is a press that may start autoscroll:
// the middle mouse button.
func AutoscrollPress(e pointer.Event) bool {
	return e.Kind == pointer.Press && e.Source == pointer.Mouse && e.Buttons.Contain(pointer.ButtonTertiary)
}

// Offer volunteers the scroller for the autoscroll that the middle press
// e may start. canX and canY report whether its content overflows along
// each axis; one that can't scroll at all isn't taken. Scrollers lay out
// outside in, so a later offer is nested deeper and takes the axes it can
// scroll (Chromium starts from the innermost box).
func (a *Autoscroll) Offer(q input.Source, e pointer.Event, canX, canY bool) {
	c := autoscrollerOf(q)
	if c == nil || c.mode != autoPending || e.Time != c.press || !AutoscrollPress(e) || (!canX && !canY) {
		return
	}
	a.canX, a.canY = canX, canY
	a.seen = c.frame
	a.last = time.Time{}
	a.acc = [2]float64{}
	if canY {
		c.vert = a
	}
	if canX {
		c.horiz = a
	}
}

// Running reports whether the window's autoscroll moves this scroller.
func (a *Autoscroll) Running(q input.Source) bool {
	c := autoscrollerOf(q)
	return c != nil && c.Active() && (c.vert == a || c.horiz == a)
}

// Tick is the distance, in whole pixels, the autoscroll moves the
// scroller now (t is the frame time), within rx and ry: how far it can go
// each way, as pointer.Filter's ranges. While it moves it asks for the
// next frame; when it can't (no speed, or the end in the way) it asks for
// none until something else draws one.
func (a *Autoscroll) Tick(q input.Source, t time.Time, rx, ry pointer.ScrollRange) image.Point {
	c := autoscrollerOf(q)
	onX := c != nil && c.Active() && c.horiz == a
	onY := c != nil && c.Active() && c.vert == a
	if !onX && !onY {
		a.last = time.Time{}
		a.acc = [2]float64{}
		return image.Point{}
	}
	a.seen = c.frame
	a.canX = rx.Min < 0 || rx.Max > 0
	a.canY = ry.Min < 0 || ry.Max > 0
	var dt float64
	if !a.last.IsZero() && t.After(a.last) {
		dt = t.Sub(a.last).Seconds()
	}
	a.last = t
	var d image.Point
	moving := false
	step := func(i int, on bool, v float32, r pointer.ScrollRange) int {
		if !on || v == 0 || (v > 0 && r.Max <= 0) || (v < 0 && r.Min >= 0) {
			a.acc[i] = 0
			return 0
		}
		moving = true
		a.acc[i] += float64(v) * dt
		n := int(a.acc[i]) // toward zero; the rest carries over
		a.acc[i] -= float64(n)
		if n < r.Min || n > r.Max {
			n = min(max(n, r.Min), r.Max)
			a.acc[i] = 0
		}
		return n
	}
	d.X = step(0, onX, c.v.X, rx)
	d.Y = step(1, onY, c.v.Y, ry)
	if moving {
		q.Execute(op.InvalidateCmd{})
	} else {
		a.last = time.Time{} // integrate from the frame it moves again
	}
	return d
}

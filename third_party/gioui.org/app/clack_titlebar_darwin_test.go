// SPDX-License-Identifier: Unlicense OR MIT

//go:build darwin && !ios

package app

import (
	"image"
	"math"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/app/cocoatest"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/io/system"
	"gioui.org/op"
	"gioui.org/op/clip"
)

// Clack patch (CLACK_PATCHES.md §23) on a real macOS desktop
// (CLACK_MACOS_WINDOW_TESTS, ci's macos job). AppKit's windows need
// app.Main on the main goroutine, so TestMain runs it there and the tests
// on another; cocoatest does what the tests need on AppKit's main thread.

func macWindowTestsEnabled() bool { return os.Getenv("CLACK_MACOS_WINDOW_TESTS") != "" }

func TestMain(m *testing.M) {
	if !macWindowTestsEnabled() {
		os.Exit(m.Run())
	}
	go func() { os.Exit(m.Run()) }()
	Main()
}

// The test window's top 40 points are a drag area (system.ActionMove)
// with a widget at x 300 to 400 over it, as Clack's top nav has them.
const (
	macBandH            = 40
	macWidgetX0         = 300
	macWidgetX1         = 400
	macTestW, macTestH  = 800, 600
	macTrafficX, macTrY = 12, 12 // Slack's main window: {x, floor(24/2)}
)

type macTestWindow struct {
	w        *Window
	view     uintptr
	frames   atomic.Int64
	wakeups  atomic.Int64 // events that are neither frames nor config
	presses  atomic.Int64 // on the widget
	releases atomic.Int64
	cfg      atomic.Pointer[Config]
	closing  chan *ClosingEvent
	done     chan struct{}
}

// newMacTestWindow opens a Gio window with a hidden title bar and the
// buttons at {12, 12}. abort decides each close; an aborted one hides the
// window from inside the event, as Clack's runFromTray does.
func newMacTestWindow(t *testing.T, abort func() bool, opts ...Option) *macTestWindow {
	t.Helper()
	return newMacTestWindowWith(t, abort, append([]Option{HiddenTitleBar(true), TrafficLightPosition(macTrafficX, macTrY)}, opts...)...)
}

// newMacTestWindowWith opens the test window with just opts (a title bar
// unless they hide it).
func newMacTestWindowWith(t *testing.T, abort func() bool, opts ...Option) *macTestWindow {
	t.Helper()
	h := &macTestWindow{w: new(Window), closing: make(chan *ClosingEvent, 4), done: make(chan struct{})}
	h.w.Option(append([]Option{Title("Clack title bar test"), Size(macTestW, macTestH)}, opts...)...)
	views := make(chan uintptr, 1)
	widget := new(int)
	go func() {
		defer close(h.done)
		var ops op.Ops
		for {
			switch e := h.w.Event().(type) {
			case AppKitViewEvent:
				if e.View != 0 {
					select {
					case views <- e.View:
					default:
					}
				}
			case FrameEvent:
				ops.Reset()
				gtx := NewContext(&ops, e)
				for {
					ev, ok := gtx.Event(pointer.Filter{Target: widget, Kinds: pointer.Press | pointer.Release})
					if !ok {
						break
					}
					if pe, ok := ev.(pointer.Event); ok {
						switch pe.Kind {
						case pointer.Press:
							h.presses.Add(1)
						case pointer.Release:
							h.releases.Add(1)
						}
					}
				}
				band := gtx.Dp(macBandH)
				area := clip.Rect{Max: image.Pt(e.Size.X, band)}.Push(gtx.Ops)
				system.ActionInputOp(system.ActionMove).Add(gtx.Ops)
				area.Pop()
				wa := clip.Rect{Min: image.Pt(gtx.Dp(macWidgetX0), 0), Max: image.Pt(gtx.Dp(macWidgetX1), band)}.Push(gtx.Ops)
				event.Op(gtx.Ops, widget)
				wa.Pop()
				e.Frame(gtx.Ops)
				h.frames.Add(1)
			case *ClosingEvent:
				if abort() {
					e.Abort()
					h.w.Hide() // as Clack does: on the UI goroutine, inside the event
				}
				h.closing <- e
			case ConfigEvent:
				c := e.Config
				h.cfg.Store(&c)
			case DestroyEvent:
				return
			default:
				h.wakeups.Add(1)
			}
		}
	}()
	select {
	case h.view = <-views:
	case <-time.After(30 * time.Second):
		t.Fatal("no AppKitViewEvent")
	}
	macWaitUntil(t, "the first frame", func() bool { return h.frames.Load() > 0 })
	time.Sleep(300 * time.Millisecond)
	return h
}

func macWaitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (h *macTestWindow) state() cocoatest.Window {
	var s cocoatest.Window
	cocoatest.OnMain(func() { s = cocoatest.State(h.view) })
	return s
}

func (h *macTestWindow) hit(x, y float64) (cocoatest.Hit, string) {
	var r cocoatest.Hit
	var cls string
	cocoatest.OnMain(func() { r, cls = cocoatest.HitTest(h.view, x, y) })
	return r, cls
}

// click posts a left click (clicks: its click count) at a window point.
func (h *macTestWindow) click(x, y float64, clicks int) {
	cocoatest.OnMain(func() {
		cocoatest.PostMouse(h.view, true, x, y, clicks)
		cocoatest.PostMouse(h.view, false, x, y, clicks)
	})
}

// doubleClick posts a double-click: two clicks, counted 1 then 2.
func (h *macTestWindow) doubleClick(x, y float64) {
	h.click(x, y, 1)
	h.click(x, y, 2)
}

// makeKey makes the window key: a synthesized click on a window that
// isn't key only makes it key (acceptsFirstMouse is NO).
func (h *macTestWindow) makeKey(t *testing.T) {
	t.Helper()
	cocoatest.OnMain(func() { cocoatest.OrderFront(h.view) })
	deadline := time.Now().Add(10 * time.Second)
	for {
		s := h.state()
		if s.Key {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the window never became key (app active %v, visible %v)", s.AppActive, s.Visible)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (h *macTestWindow) closeWindow(t *testing.T) {
	t.Helper()
	cocoatest.OnMain(func() { cocoatest.Press(h.view, cocoatest.Close) }) // the red button
	select {
	case <-h.closing:
	case <-time.After(10 * time.Second):
		t.Fatal("no ClosingEvent from the close button")
	}
}

// destroy closes the window, the close not aborted: ActionClose (Quit), or
// with redButton the close button, and waits for its DestroyEvent.
func (h *macTestWindow) destroy(t *testing.T) { h.destroyBy(t, false) }

func (h *macTestWindow) destroyBy(t *testing.T, redButton bool) {
	t.Helper()
	how := "ActionClose"
	if redButton {
		how = "the close button"
		cocoatest.OnMain(func() { cocoatest.Press(h.view, cocoatest.Close) })
	} else {
		h.w.Perform(system.ActionClose)
	}
	select {
	case <-h.closing:
	case <-h.done:
		return
	case <-time.After(10 * time.Second):
		t.Fatalf("%s: no ClosingEvent (window %+v)", how, h.state())
	}
	select {
	case <-h.done:
	case <-time.After(10 * time.Second):
		t.Fatalf("%s: a ClosingEvent, not aborted, but no DestroyEvent (window %+v)", how, h.state())
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 0.5 }

// checkTrafficLights: the buttons at {12, 12}, in a row, in a title bar
// container at the window's top that is their height plus 24 tall.
func checkTrafficLights(t *testing.T, when string, s cocoatest.Window) {
	t.Helper()
	b := s.Buttons
	if s.ButtonsHidden != [3]bool{} {
		t.Errorf("%s: buttons hidden %v", when, s.ButtonsHidden)
	}
	if !near(b[0].X, macTrafficX) || !near(b[0].Y, macTrY) {
		t.Errorf("%s: the close button at %v, want x %d y %d", when, b[0], macTrafficX, macTrY)
	}
	for i := 1; i < 3; i++ {
		if !near(b[i].Y, b[0].Y) || b[i].X <= b[i-1].X+b[i-1].W-0.5 {
			t.Errorf("%s: button %d at %v after %v: not in a row", when, i, b[i], b[i-1])
		}
	}
	c := s.TitlebarContainer
	if !near(c.Y, 0) || !near(c.H, b[0].H+2*macTrY) {
		t.Errorf("%s: the title bar container %v, want y 0 and %v tall", when, c, b[0].H+2*macTrY)
	}
}

// HiddenTitleBar(true) on macOS: the content covers the whole window,
// which is the size asked for; the title bar is transparent, its title
// hidden and tabs off; the buttons show, at TrafficLightPosition, and stay
// there after a title change, a resize and a key change; Gio still sees a
// decorated window; every point in the title bar band outside the buttons
// is the content view's for AppKit, and the window server may drag the
// window from none of it on its own.
func TestHiddenTitleBarMac(t *testing.T) {
	if !macWindowTestsEnabled() {
		t.Skip("CLACK_MACOS_WINDOW_TESTS not set")
	}
	h := newMacTestWindow(t, func() bool { return false })
	defer h.destroyBy(t, true) // the red button, not aborted: the window goes
	s := h.state()
	t.Logf("window: %+v", s)
	if !s.FullSizeContent || !s.TitlebarTransparent || !s.TitleHidden || !s.TabbingDisallowed {
		t.Errorf("full-size content %v, transparent title bar %v, title hidden %v, tabbing off %v: want all",
			s.FullSizeContent, s.TitlebarTransparent, s.TitleHidden, s.TabbingDisallowed)
	}
	if !near(s.Frame.W, macTestW) || !near(s.Frame.H, macTestH) {
		t.Errorf("window %vx%v, want %dx%d", s.Frame.W, s.Frame.H, macTestW, macTestH)
	}
	if s.Content != (cocoatest.Rect{W: s.Frame.W, H: s.Frame.H}) {
		t.Errorf("content view %v, want the whole %vx%v window", s.Content, s.Frame.W, s.Frame.H)
	}
	checkTrafficLights(t, "opened", s)
	if !s.HasOpaqueForMove {
		t.Log("AppKit has no _opaqueRectForWindowMoveWhenInTitlebar")
	} else if s.OpaqueForMove != (cocoatest.Rect{W: s.Frame.W, H: s.Frame.H}) {
		t.Errorf("opaque for window moves %v, want the whole view", s.OpaqueForMove)
	}
	if c := h.cfg.Load(); c == nil || !c.Decorated {
		t.Errorf("Gio's config %+v: want Decorated (no fallback decorations)", c)
	}

	b := s.Buttons[cocoatest.Close]
	for _, c := range []struct {
		what string
		x, y float64
		want cocoatest.Hit
	}{
		{"the title bar band", 500, 6, cocoatest.HitContent},
		{"the band right of the buttons", b.X + 3*b.W + 30, b.Y + b.H/2, cocoatest.HitContent},
		{"the widget in the band", (macWidgetX0 + macWidgetX1) / 2, 10, cocoatest.HitContent},
		{"under the band", 500, 300, cocoatest.HitContent},
		{"the close button", b.X + b.W/2, b.Y + b.H/2, cocoatest.HitButton},
	} {
		if got, cls := h.hit(c.x, c.y); got != c.want {
			t.Errorf("a click on %s (%v, %v) goes to %d (%s), want %d", c.what, c.x, c.y, got, cls, c.want)
		}
	}

	h.w.Option(Title("Clack title bar test - 3 new items"))
	time.Sleep(300 * time.Millisecond)
	checkTrafficLights(t, "after a title change", h.state())
	h.w.Option(Size(macTestW+100, macTestH+50))
	macWaitUntil(t, "the resize", func() bool { return near(h.state().Frame.W, macTestW+100) })
	time.Sleep(300 * time.Millisecond)
	checkTrafficLights(t, "after a resize", h.state())
	h.makeKey(t)
	time.Sleep(200 * time.Millisecond)
	checkTrafficLights(t, "key", h.state())

	// a click on the widget in the band is the widget's, not the title bar's
	p0, r0 := h.presses.Load(), h.releases.Load()
	h.click((macWidgetX0+macWidgetX1)/2, 10, 1)
	macWaitUntil(t, "the widget's press and release", func() bool { return h.presses.Load() > p0 && h.releases.Load() > r0 })
}

// A double-click on a drag area does what AppleActionOnDoubleClick says:
// unset or "Maximize" zooms and zooms back, "Minimize" minimizes, "None"
// does nothing; a double-click on a widget over the area is the widget's.
func TestTitlebarDoubleClickMac(t *testing.T) {
	if !macWindowTestsEnabled() {
		t.Skip("CLACK_MACOS_WINDOW_TESTS not set")
	}
	h := newMacTestWindow(t, func() bool { return false })
	defer h.destroy(t)
	defer cocoatest.OnMain(func() { cocoatest.SetDoubleClickAction("", false) })
	h.makeKey(t)
	const dx, dy = 520, 20 // the drag area, clear of the widget and the buttons

	cocoatest.OnMain(func() { cocoatest.SetDoubleClickAction("", false) })
	h.doubleClick(dx, dy)
	macWaitUntil(t, "zoomed by a double-click (the default)", func() bool { return h.state().Zoomed })
	time.Sleep(500 * time.Millisecond)
	h.makeKey(t)
	h.doubleClick(dx, dy)
	macWaitUntil(t, "zoomed back by a second double-click", func() bool { return !h.state().Zoomed })
	time.Sleep(500 * time.Millisecond)

	cocoatest.OnMain(func() { cocoatest.SetDoubleClickAction("Maximize", true) })
	h.makeKey(t)
	h.doubleClick(dx, dy)
	macWaitUntil(t, `zoomed by a double-click ("Maximize")`, func() bool { return h.state().Zoomed })
	time.Sleep(500 * time.Millisecond)
	h.makeKey(t)
	h.doubleClick(dx, dy)
	macWaitUntil(t, "zoomed back", func() bool { return !h.state().Zoomed })
	time.Sleep(500 * time.Millisecond)

	cocoatest.OnMain(func() { cocoatest.SetDoubleClickAction("None", true) })
	h.makeKey(t)
	h.doubleClick(dx, dy)
	time.Sleep(time.Second)
	if s := h.state(); s.Zoomed || s.Miniaturized {
		t.Errorf(`"None": zoomed %v minimized %v after a double-click`, s.Zoomed, s.Miniaturized)
	}

	// the widget's double-click: two presses for it, no zoom
	cocoatest.OnMain(func() { cocoatest.SetDoubleClickAction("", false) })
	p0 := h.presses.Load()
	h.doubleClick((macWidgetX0+macWidgetX1)/2, dy)
	macWaitUntil(t, "the widget's two presses", func() bool { return h.presses.Load() >= p0+2 })
	time.Sleep(500 * time.Millisecond)
	if h.state().Zoomed {
		t.Error("a double-click on the widget zoomed the window")
	}

	cocoatest.OnMain(func() { cocoatest.SetDoubleClickAction("Minimize", true) })
	h.makeKey(t)
	h.doubleClick(dx, dy)
	macWaitUntil(t, `minimized by a double-click ("Minimize")`, func() bool { return h.state().Miniaturized })
	h.w.Perform(system.ActionRaise)
	macWaitUntil(t, "back from the Dock", func() bool { s := h.state(); return !s.Miniaturized && s.Visible })
}

// Window.Hide on macOS: from inside an aborted ClosingEvent (the red
// button) the window is ordered out, not destroyed; hidden, it draws no
// frames but Invalidate still wakes the app; ActionRaise shows it and it
// draws again, and so does an order-front that doesn't go through Gio (the
// Dock's reopen); a close that isn't aborted destroys a hidden window.
func TestHideMac(t *testing.T) {
	if !macWindowTestsEnabled() {
		t.Skip("CLACK_MACOS_WINDOW_TESTS not set")
	}
	var abort atomic.Bool
	abort.Store(true)
	h := newMacTestWindow(t, abort.Load)
	hidden := func() {
		t.Helper()
		h.closeWindow(t)
		macWaitUntil(t, "ordered out", func() bool { return !h.state().Visible })
		select {
		case <-h.done:
			t.Fatal("the close destroyed the window")
		default:
		}
		time.Sleep(500 * time.Millisecond)
		f0, w0 := h.frames.Load(), h.wakeups.Load()
		for range 5 {
			h.w.Invalidate()
			time.Sleep(100 * time.Millisecond)
		}
		if n := h.frames.Load() - f0; n != 0 {
			t.Errorf("hidden: %d frames after Invalidate, want none", n)
		}
		if h.wakeups.Load() == w0 {
			t.Error("hidden: Invalidate delivered no event to the app")
		}
	}

	hidden()
	f0 := h.frames.Load()
	h.w.Perform(system.ActionRaise)
	macWaitUntil(t, "visible after ActionRaise", func() bool { return h.state().Visible })
	macWaitUntil(t, "a frame after ActionRaise", func() bool { return h.frames.Load() > f0 })
	checkTrafficLights(t, "after ActionRaise", h.state())

	hidden()
	f0 = h.frames.Load()
	cocoatest.OnMain(func() { cocoatest.OrderFront(h.view) })
	macWaitUntil(t, "visible after an order-front", func() bool { return h.state().Visible })
	h.w.Invalidate()
	macWaitUntil(t, "a frame after an order-front outside Gio", func() bool { return h.frames.Load() > f0 })

	hidden()
	abort.Store(false)
	h.destroy(t) // Quit while hidden: performClose: on an ordered-out window
}

// A close that isn't aborted ends the window with a DestroyEvent, with a
// title bar or without, whether or not the test read the window first;
// the log says whether the view and the NSWindow are gone a moment later
// (Gio's DestroyEvent comes from the view's dealloc, and the runner kept
// closed windows: windowWillClose: lets the view go).
func TestMacCloseDestroys(t *testing.T) {
	if !macWindowTestsEnabled() {
		t.Skip("CLACK_MACOS_WINDOW_TESTS not set")
	}
	for _, c := range []struct {
		name string
		read bool
		opts []Option
	}{
		{"plain", false, nil},
		{"plain, read first", true, nil},
		{"hidden title bar", true, []Option{HiddenTitleBar(true)}},
		{"hidden title bar and traffic lights", true, []Option{HiddenTitleBar(true), TrafficLightPosition(macTrafficX, macTrY)}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newMacTestWindowWith(t, func() bool { return false }, c.opts...)
			cocoatest.OnMain(func() { cocoatest.Track(h.view) })
			if c.read {
				h.state()
			}
			cocoatest.OnMain(func() { cocoatest.Press(h.view, cocoatest.Close) })
			select {
			case <-h.closing:
			case <-time.After(10 * time.Second):
				t.Fatal("no ClosingEvent from the close button")
			}
			destroyed := false
			select {
			case <-h.done:
				destroyed = true
			case <-time.After(5 * time.Second):
			}
			time.Sleep(500 * time.Millisecond)
			var view, window bool
			cocoatest.OnMain(func() { view, window = cocoatest.Alive() })
			t.Logf("DestroyEvent %v; then the view alive %v, the NSWindow alive %v", destroyed, view, window)
			if !destroyed {
				t.Error("a close that wasn't aborted brought no DestroyEvent")
			}
		})
	}
}

// A Hide in full screen leaves full screen and orders the window out once
// it has (Slack awaits leave-full-screen); full screen shows AppKit's own
// title bar with the title, and the hidden title bar and the buttons'
// place come back after it. In stages, each logged: AppKit's own
// toggleFullScreen: from the main queue (the control: skipped when the
// runner can't enter or leave full screen), Hide outside a close, then
// the close button's Hide (the case that counts).
func TestHideLeavesFullscreenMac(t *testing.T) {
	if !macWindowTestsEnabled() {
		t.Skip("CLACK_MACOS_WINDOW_TESTS not set")
	}
	var abort atomic.Bool
	abort.Store(true)
	h := newMacTestWindow(t, abort.Load)
	defer func() {
		abort.Store(false)
		h.destroy(t)
	}()
	// enter puts the window in full screen and waits for AppKit to finish
	// (windowDidEnterFullScreen: shows the title)
	enter := func(stage string) bool {
		t.Helper()
		h.makeKey(t)
		h.w.Option(Fullscreen.Option())
		deadline := time.Now().Add(20 * time.Second)
		for {
			if s := h.state(); s.FullScreen && !s.TitleHidden {
				break
			}
			if time.Now().After(deadline) {
				t.Logf("%s: not in full screen after 20 s (%+v)", stage, h.state())
				return false
			}
			time.Sleep(50 * time.Millisecond)
		}
		time.Sleep(time.Second) // the animation's end
		return true
	}
	// left waits up to 15 s for the window out of full screen, ordered out
	// when hidden is set, and logs how it went
	left := func(stage string, hidden bool) bool {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		last := time.Now()
		for {
			s := h.state()
			if !s.FullScreen && s.Visible != hidden {
				t.Logf("%s: out of full screen (visible %v)", stage, s.Visible)
				return true
			}
			if time.Now().After(deadline) {
				t.Logf("%s: still full screen %v, visible %v after 15 s", stage, s.FullScreen, s.Visible)
				return false
			}
			if time.Since(last) > 3*time.Second {
				last = time.Now()
				t.Logf("%s: full screen %v visible %v title hidden %v", stage, s.FullScreen, s.Visible, s.TitleHidden)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	if !enter("control") {
		t.Skip("the runner didn't enter full screen")
	}
	if s := h.state(); s.TitleHidden || s.TitlebarTransparent {
		t.Errorf("full screen: title hidden %v, transparent title bar %v; want AppKit's own title bar", s.TitleHidden, s.TitlebarTransparent)
	}
	cocoatest.OnMain(func() { cocoatest.ToggleFullScreen(h.view) })
	if !left("control, toggleFullScreen: from the main queue", false) {
		t.Skip("the runner doesn't leave full screen for toggleFullScreen: either")
	}
	time.Sleep(time.Second)

	if enter("Hide outside a close") {
		h.w.Hide()
		if !left("Hide outside a close", true) {
			t.Error("Hide in full screen (outside a close) didn't leave full screen and hide")
		}
		h.w.Perform(system.ActionRaise)
		macWaitUntil(t, "visible after ActionRaise", func() bool { return h.state().Visible })
		time.Sleep(time.Second)
	}

	if !enter("the close button") {
		t.Fatal("not in full screen again")
	}
	h.closeWindow(t)
	if !left("the close button's Hide", true) {
		t.Fatal("the close button's Hide in full screen didn't leave full screen and hide")
	}
	f0 := h.frames.Load()
	h.w.Perform(system.ActionRaise)
	macWaitUntil(t, "visible after ActionRaise", func() bool { return h.state().Visible })
	macWaitUntil(t, "a frame", func() bool { return h.frames.Load() > f0 })
	time.Sleep(300 * time.Millisecond)
	s := h.state()
	if s.FullScreen || !s.TitleHidden || !s.TitlebarTransparent {
		t.Errorf("back: full screen %v, title hidden %v, transparent %v", s.FullScreen, s.TitleHidden, s.TitlebarTransparent)
	}
	checkTrafficLights(t, "after full screen", s)
}

package ui

import (
	"image"
	"runtime"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/gesture"
	"gioui.org/io/key"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Middle-click autoscroll (Gio patch 14) in Clack's own frame: App.frame
// runs the window's Autoscroller around the layout, so a middle press over
// a channel's messages or the sidebar scrolls it while the button is held,
// a click leaves it on until the next key or click, and it stops cleanly.
// The router tests of the gesture itself are in internal/ui/autoscroll.
func TestAutoscrollInClack(t *testing.T) {
	gesture.SetAutoscroll(true) // Windows' default, on any test runner
	t.Cleanup(func() { gesture.SetAutoscroll(runtime.GOOS == "windows") })
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.size = image.Pt(1200, 520) // short enough that the sidebar overflows too
	e.waitReady()
	ch := fs.ChannelIDs()[2]
	e.openChannel(ch)
	e.until("history", 20*time.Second, func() bool { return e.loadedMessages(ch) > 30 })
	e.settle(500 * time.Millisecond)
	// mid-channel, so there is room both ways (it may open at the top of
	// what's loaded): the bottom, then 1200px up
	for i := 0; i < 40; i++ {
		e.scroll(e.listPoint(), 400)
		e.frame()
	}
	for i := 0; i < 4; i++ {
		e.scroll(e.listPoint(), -300)
		e.frame()
	}
	e.settle(300 * time.Millisecond)

	rows := func() map[string]int {
		c := e.a.convos[ch+"/"]
		if c == nil {
			return nil
		}
		l, ok := c.list.(channelList)
		if !ok {
			return nil
		}
		m := map[string]int{}
		for _, r := range l.VisibleRows() {
			m[r.Key] = r.Y
		}
		return m
	}
	pt := func(p image.Point) f32.Point { return f32.Pt(float32(p.X), float32(p.Y)) }
	ev := func(k pointer.Kind, p image.Point, b pointer.Buttons) {
		e.r.Queue(pointer.Event{Kind: k, Source: pointer.Mouse, Position: pt(p), Buttons: b, Time: time.Since(e.start)})
	}
	// run draws frames for d and returns how far the messages moved,
	// summed frame by frame (in a short window a fast scroll leaves no
	// row on screen in both ends' snapshots)
	run := func(d time.Duration) (moved int) {
		prev := rows()
		for dl := time.Now().Add(d); time.Now().Before(dl); {
			e.frame()
			cur := rows()
			moved += movedBetween(prev, cur)
			prev = cur
			time.Sleep(time.Second / 60)
		}
		return moved
	}
	as := &e.a.autoscroll
	// start presses the middle button at the first of pts where it starts
	// autoscroll (a link under the pointer rightly doesn't), and leaves it
	// held there.
	start := func(what string, pts ...image.Point) image.Point {
		t.Helper()
		for _, p := range pts {
			ev(pointer.Move, p, 0)
			ev(pointer.Press, p, pointer.ButtonTertiary)
			e.frame()
			if as.Active() {
				return p
			}
			ev(pointer.Release, p, 0)
			e.frame()
		}
		t.Fatalf("a middle press over %s (at %v) started no autoscroll", what, pts)
		return image.Point{}
	}
	lp := e.listPoint()
	candidates := []image.Point{lp, lp.Add(image.Pt(0, 37)), lp.Add(image.Pt(0, -41)), image.Pt(e.size.X-60, lp.Y)}

	// held: 120 dp above the origin scrolls toward older messages
	p := start("the messages", candidates...)
	ev(pointer.Move, p.Sub(image.Pt(0, 120)), pointer.ButtonTertiary)
	e.frame()
	if c := e.r.Cursor(); c != pointer.CursorNorthPanning {
		t.Errorf("cursor while scrolling up: %v, want NorthPanning", c)
	}
	moved := run(400 * time.Millisecond)
	t.Logf("held 120dp above for 400ms: the messages moved %dpx (Chromium's speed there: %.0f px/s)", moved, gesture.AutoscrollSpeed(120))
	if moved > -100 {
		t.Errorf("the messages moved %dpx in 400ms, want well over 100px toward older ones", moved)
	}
	ev(pointer.Release, p.Sub(image.Pt(0, 120)), 0)
	e.frame()
	if as.Active() {
		t.Fatalf("still running after releasing the held button")
	}
	if m := run(150 * time.Millisecond); m != 0 {
		t.Errorf("the messages kept moving (%dpx) after the release", m)
	}

	// clicked: on until Escape, which goes no further
	route := e.a.router.Current()
	p = start("the messages", candidates...)
	ev(pointer.Release, p, 0)
	e.frame()
	if !as.Toggled() {
		t.Fatalf("a middle click left it toggled %v, active %v; want toggled", as.Toggled(), as.Active())
	}
	ev(pointer.Move, p.Add(image.Pt(0, 80)), 0)
	if m := run(300 * time.Millisecond); m <= 0 {
		t.Errorf("toggled, 80dp below: the messages moved %dpx, want toward newer ones", m)
	}
	e.r.Queue(key.Event{Name: key.NameEscape, State: key.Press}, key.Event{Name: key.NameEscape, State: key.Release})
	e.frame()
	if as.Active() {
		t.Fatalf("Escape didn't stop it")
	}
	if e.a.router.Current() != route || len(e.a.overlays) != 0 {
		t.Errorf("the Escape that stopped it did something else (route %v, %d overlays)", e.a.router.Current(), len(e.a.overlays))
	}
	ev(pointer.Move, p, 0) // back over the origin

	// the sidebar
	sbX := e.a.px(int(theme.RailW)) + e.a.px(60)
	y0 := e.a.sidebar.list.Position
	sb := start("the sidebar", image.Pt(sbX, e.size.Y/2), image.Pt(sbX, e.size.Y/2+30))
	ev(pointer.Move, sb.Add(image.Pt(0, 100)), pointer.ButtonTertiary)
	run(300 * time.Millisecond)
	ev(pointer.Release, sb.Add(image.Pt(0, 100)), 0)
	e.frame()
	y1 := e.a.sidebar.list.Position
	if y1.First < y0.First || (y1.First == y0.First && y1.Offset <= y0.Offset) {
		t.Errorf("the sidebar didn't scroll down: %+v → %+v", y0, y1)
	}
	if as.Active() {
		t.Errorf("still running after the release")
	}
}

package ui

import (
	"image"
	"runtime"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
)

// gesture plays a trackpad gesture: steps of (dx, dy) finger travel in pt,
// each also delivered as Gio scroll with left of its horizontal part left
// over by the views (1 = nothing consumed). Natural scrolling (content
// follows the fingers): Gio's Scroll.X is minus the finger travel.
type swipeScript struct {
	steps   [][2]float32
	left    float32
	enabled bool
	noEnd   bool
	cancel  bool
}

func (g swipeScript) play(s *swiper, t0 time.Duration, wall time.Time) (time.Duration, time.Time) {
	t := t0
	for i, st := range g.steps {
		ph := swipeChanged
		if i == 0 {
			ph = swipeBegan
		}
		s.raw(swipeSample{t: t, phase: ph, dx: st[0], dy: st[1], inverted: true, enabled: g.enabled}, wall)
		if st[0] != 0 && g.left > 0 {
			s.leftover(t, -st[0]*g.left*2, 2) // 2 px per pt (Retina)
		}
		t += 8 * time.Millisecond
		wall = wall.Add(8 * time.Millisecond)
	}
	if !g.noEnd {
		ph := swipeEnded
		if g.cancel {
			ph = swipeCancelled
		}
		s.raw(swipeSample{t: t, phase: ph, inverted: true, enabled: g.enabled}, wall)
	}
	return t, wall
}

func repeat(n int, dx, dy float32) [][2]float32 {
	out := make([][2]float32, n)
	for i := range out {
		out[i] = [2]float32{dx, dy}
	}
	return out
}

// settle polls once the gesture has settled and returns the navigation.
func settle(s *swiper, wall time.Time) int {
	if st := s.poll(wall); st.nav != 0 {
		return st.nav
	}
	return s.poll(wall.Add(swipeSettle + time.Millisecond)).nav
}

func TestSwipeThreshold(t *testing.T) {
	cases := []struct {
		name string
		g    swipeScript
		want int
	}{
		{"fingers right over-scroll goes back", swipeScript{steps: repeat(15, 10, 0.5), left: 1, enabled: true}, -1},
		{"fingers left goes forward", swipeScript{steps: repeat(15, -10, 0), left: 1, enabled: true}, 1},
		{"short swipe", swipeScript{steps: repeat(8, 10, 0), left: 1, enabled: true}, 0},
		{"vertical scroll with sideways drift", swipeScript{steps: repeat(30, 4.5, 10), left: 1, enabled: true}, 0},
		{"diagonal", swipeScript{steps: repeat(20, 10, 6), left: 1, enabled: true}, 0},
		{"starts vertical, then sideways", swipeScript{steps: append(repeat(2, 0, 6), repeat(20, 10, 0)...), left: 1, enabled: true}, 0},
		{"a sideways scroller took it", swipeScript{steps: repeat(20, 10, 0), left: 0, enabled: true}, 0},
		{"a sideways scroller took half", swipeScript{steps: repeat(20, 10, 0), left: 0.5, enabled: true}, 0},
		{"swipe between pages is off", swipeScript{steps: repeat(20, 10, 0), left: 1, enabled: false}, 0},
		{"cancelled", swipeScript{steps: repeat(20, 10, 0), left: 1, enabled: true, cancel: true}, 0},
		{"there and back again", swipeScript{steps: append(repeat(15, 10, 0), repeat(12, -10, 0)...), left: 1, enabled: true}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var s swiper
			_, wall := c.g.play(&s, time.Second, time.Unix(100, 0))
			if got := settle(&s, wall); got != c.want {
				t.Fatalf("nav %d, want %d", got, c.want)
			}
			if again := s.poll(wall.Add(time.Second)).nav; again != 0 {
				t.Fatalf("navigated twice (%d)", again)
			}
		})
	}
}

func TestSwipeWaitsForFingersAndSettle(t *testing.T) {
	var s swiper
	g := swipeScript{steps: repeat(15, 10, 0), left: 1, enabled: true, noEnd: true}
	tEnd, wall := g.play(&s, time.Second, time.Unix(100, 0))
	st := s.poll(wall)
	if st.nav != 0 {
		t.Fatal("navigated before the fingers lifted")
	}
	if st.dir != -1 || st.progress < 1 {
		t.Fatalf("overlay %+v, want back at full progress", st)
	}
	s.raw(swipeSample{t: tEnd, phase: swipeEnded, inverted: true, enabled: true}, wall)
	if st := s.poll(wall); st.nav != 0 || st.wake.IsZero() {
		t.Fatalf("settling: %+v (want no nav yet and a wake-up)", st)
	}
	if st := s.poll(wall.Add(swipeSettle)); st.nav != -1 {
		t.Fatalf("after settling: %+v", st)
	}
}

// Momentum scroll after the fingers lift isn't part of the gesture, and a
// mouse wheel (no raw samples at all) never swipes.
func TestSwipeIgnoresMomentumAndWheels(t *testing.T) {
	var s swiper
	g := swipeScript{steps: repeat(8, 10, 0), left: 1, enabled: true}
	tEnd, wall := g.play(&s, time.Second, time.Unix(100, 0))
	for i := 1; i <= 30; i++ { // momentum: left over, after the end
		s.leftover(tEnd+time.Duration(i)*8*time.Millisecond, -40, 2)
	}
	if got := settle(&s, wall); got != 0 {
		t.Fatalf("momentum completed a short swipe: %d", got)
	}
	var w swiper
	for i := 0; i < 50; i++ {
		w.leftover(time.Duration(i)*time.Millisecond, -100, 1)
	}
	if got := settle(&w, time.Unix(100, 0)); got != 0 {
		t.Fatalf("wheel navigated: %d", got)
	}
}

// TestNavBindings: Slack's history keys per platform.
func TestNavBindings(t *testing.T) {
	type k struct {
		n key.Name
		m key.Modifiers
	}
	want := map[string]map[k]int{
		"darwin":  {{"[", key.ModCommand}: -1, {"]", key.ModCommand}: 1},
		"linux":   {{"[", key.ModCtrl}: -1, {"]", key.ModCtrl}: 1, {key.NameLeftArrow, key.ModAlt}: -1, {key.NameRightArrow, key.ModAlt}: 1},
		"windows": {{"[", key.ModCtrl}: -1, {"]", key.ModCtrl}: 1, {key.NameLeftArrow, key.ModAlt}: -1, {key.NameRightArrow, key.ModAlt}: 1},
	}
	for goos, w := range want {
		got := map[k]int{}
		for _, b := range navBindings(goos) {
			got[k{b.name, b.mods}] = b.dir
		}
		if len(got) != len(w) {
			t.Errorf("%s: %v, want %v", goos, got, w)
		}
		for kk, d := range w {
			if got[kk] != d {
				t.Errorf("%s: %v+%q = %d, want %d", goos, kk.m, kk.n, got[kk], d)
			}
		}
	}
	// Option+←/→ edit text on macOS: not history keys there
	for _, b := range navBindings("darwin") {
		if b.mods&key.ModAlt != 0 {
			t.Errorf("darwin binds %v+%q", b.mods, b.name)
		}
	}
}

// navTestApp is an App with history and nothing else, for the input tests.
func navTestApp(t *testing.T) *App {
	a := New(Options{ConfigDir: t.TempDir()}, nil)
	for _, r := range []string{"c/C1", "c/C2", "c/C3"} {
		a.router.Push(Route(r))
	}
	return a
}

type navFrame struct {
	a   *App
	r   input.Router
	ops op.Ops
}

// frame runs the parts of App.frame that route history input: the swipe
// ancestor, the root pointer handler and the shortcuts.
func (f *navFrame) frame() {
	f.ops.Reset()
	gtx := layout.Context{Ops: &f.ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(800, 600)), Source: f.r.Source(), Now: time.Now()}
	area := f.a.navGestures(gtx)
	f.a.readRootPointer(gtx)
	f.a.keys.process(gtx, f.a)
	f.a.rootPointer(gtx)
	area.Pop()
	f.r.Frame(&f.ops)
}

// The keys and the mouse's back/forward buttons go through Gio's real
// routing to the history.
func TestNavKeysAndMouseButtons(t *testing.T) {
	f := &navFrame{a: navTestApp(t)}
	f.frame()
	cur := func() Route { return f.a.router.Current() }
	press := func(n key.Name, m key.Modifiers) {
		f.r.Queue(key.Event{Name: n, Modifiers: m, State: key.Press}, key.Event{Name: n, Modifiers: m, State: key.Release})
		f.frame()
	}
	// every binding of this platform, through Gio's key routing
	for _, b := range navBindings(runtime.GOOS) {
		before := cur()
		want := Route("c/C2") // back from C3
		if b.dir > 0 {
			press(navBindings(runtime.GOOS)[0].name, navBindings(runtime.GOOS)[0].mods) // to C2 first
			before, want = cur(), "c/C3"
		}
		press(b.name, b.mods)
		if cur() != want {
			t.Fatalf("%v+%q from %q: %q, want %q", b.mods, b.name, before, cur(), want)
		}
		if b.dir < 0 {
			press(navBindings(runtime.GOOS)[1].name, navBindings(runtime.GOOS)[1].mods) // back to C3
		}
		if cur() != "c/C3" {
			t.Fatalf("reset: %q", cur())
		}
	}

	pt := f32.Pt(400, 300)
	btn := func(b pointer.Buttons) {
		f.r.Queue(
			pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt},
			pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: b, Position: pt},
			pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt},
		)
		f.frame()
	}
	btn(pointer.ButtonQuaternary)
	if cur() != "c/C2" {
		t.Fatalf("after mouse back: %q", cur())
	}
	btn(pointer.ButtonQuinary)
	if cur() != "c/C3" {
		t.Fatalf("after mouse forward: %q", cur())
	}
	btn(pointer.ButtonPrimary)
	if cur() != "c/C3" {
		t.Fatalf("a left click navigated: %q", cur())
	}
}

// The window-wide handler gets the horizontal scroll that an inner
// scrollable leaves over, and none of what it takes.
func TestNavGesturesGetLeftoverScroll(t *testing.T) {
	f := &navFrame{a: navTestApp(t)}
	var inner int
	innerX := pointer.ScrollRange{}
	frame := func() {
		f.ops.Reset()
		gtx := layout.Context{Ops: &f.ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(800, 600)), Source: f.r.Source(), Now: time.Now()}
		area := f.a.navGestures(gtx)
		for {
			if _, ok := gtx.Event(pointer.Filter{Target: &inner, Kinds: pointer.Scroll, ScrollX: innerX,
				ScrollY: pointer.ScrollRange{Min: -1000, Max: 1000}}); !ok {
				break
			}
		}
		st := clip.Rect(image.Rect(100, 100, 700, 500)).Push(gtx.Ops) // a list, inside the window
		event.Op(gtx.Ops, &inner)
		st.Pop()
		area.Pop()
		f.r.Frame(&f.ops)
	}
	frame()
	// a gesture the glue reported, over the list
	f.a.swipe.raw(swipeSample{t: time.Second, phase: swipeBegan, dx: 10, inverted: true, enabled: true}, time.Now())
	f.r.Queue(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: f32.Pt(400, 300), Scroll: f32.Pt(-10, 0), Time: time.Second})
	frame()
	frame()
	if g := f.a.swipe.gs[f.a.swipe.cur]; g.free != 10 {
		t.Fatalf("left over %v, want 10 (the list scrolls only vertically)", g.free)
	}
	innerX = pointer.ScrollRange{Min: -1000, Max: 1000} // now it scrolls sideways too
	frame()
	f.a.swipe.raw(swipeSample{t: 2 * time.Second, phase: swipeBegan, dx: 10, inverted: true, enabled: true}, time.Now())
	f.r.Queue(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: f32.Pt(400, 300), Scroll: f32.Pt(-10, 0), Time: 2 * time.Second})
	frame()
	frame()
	if g := f.a.swipe.gs[f.a.swipe.cur]; g.free != 0 {
		t.Fatalf("left over %v, want 0 (the list took it)", g.free)
	}
}

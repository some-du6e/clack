package hovercard

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

var t0 = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

// clock drives a controller with reports at given times.
type clock struct {
	t   *testing.T
	c   *Controller
	now time.Time
}

func newClock(t *testing.T) *clock { return &clock{t: t, c: &Controller{}} }

// at runs a frame d after t0, hovering key (nil: nothing), and checks
// whether a card is open.
func (k *clock) at(d time.Duration, key any, open bool) {
	k.t.Helper()
	k.now = t0.Add(d)
	if key != nil {
		k.c.Report(key, "U1", image.Rect(100, 300, 160, 320), f32.Point{})
	}
	k.c.Frame(k.now, false, false)
	if _, _, got := k.c.Open(); got != open {
		k.t.Fatalf("at %v (hovering %v): open = %v, want %v", d, key, got, open)
	}
}

func TestShowDelay(t *testing.T) {
	k := newClock(t)
	a := "a"
	k.at(0, a, false)
	k.at(349*time.Millisecond, a, false)
	k.at(350*time.Millisecond, a, true)
	req, _, _ := k.c.Open()
	if req.UserID != "U1" || req.Anchor != image.Rect(100, 300, 160, 320) {
		t.Errorf("opened %+v", req)
	}
}

func TestLeaveBeforeDelayCancels(t *testing.T) {
	k := newClock(t)
	k.at(0, "a", false)
	k.at(200*time.Millisecond, nil, false)
	k.at(400*time.Millisecond, nil, false)
	// coming back restarts the full delay
	k.at(500*time.Millisecond, "a", false)
	k.at(849*time.Millisecond, "a", false)
	k.at(850*time.Millisecond, "a", true)
}

func TestHideGrace(t *testing.T) {
	k := newClock(t)
	k.at(0, "a", false)
	k.at(350*time.Millisecond, "a", true)
	k.at(1000*time.Millisecond, nil, true) // left: closes 250ms later
	k.at(1249*time.Millisecond, nil, true)
	k.at(1250*time.Millisecond, nil, false)
}

func TestPointerOnCardKeepsItOpen(t *testing.T) {
	k := newClock(t)
	k.at(0, "a", false)
	k.at(350*time.Millisecond, "a", true)
	k.at(400*time.Millisecond, nil, true) // crossing the 12px gap
	k.c.overCard = true                   // reached the card within 250ms
	k.at(600*time.Millisecond, nil, true)
	k.at(5*time.Second, nil, true)
	k.c.overCard = false // left the card
	k.at(6*time.Second, nil, true)
	k.at(6*time.Second+249*time.Millisecond, nil, true)
	k.at(6*time.Second+250*time.Millisecond, nil, false)
}

func TestBackOnTriggerCancelsHide(t *testing.T) {
	k := newClock(t)
	k.at(0, "a", false)
	k.at(350*time.Millisecond, "a", true)
	k.at(400*time.Millisecond, nil, true)
	k.at(600*time.Millisecond, "a", true) // back within the grace
	k.at(2*time.Second, "a", true)
}

func TestSwitchTriggers(t *testing.T) {
	k := newClock(t)
	k.at(0, "a", false)
	k.at(350*time.Millisecond, "a", true)
	k.at(1000*time.Millisecond, "b", true) // a closes at +250, b opens at +350
	k.at(1249*time.Millisecond, "b", true)
	k.at(1250*time.Millisecond, "b", false)
	k.at(1349*time.Millisecond, "b", false)
	k.at(1350*time.Millisecond, "b", true)
	if req, _, _ := k.c.Open(); req.Key != "b" {
		t.Errorf("open card is %v, want b", req.Key)
	}
}

func TestBlockedAndHold(t *testing.T) {
	k := newClock(t)
	k.at(0, "a", false)
	k.at(350*time.Millisecond, "a", true)
	// the card's own menu: held open with the pointer away from both
	k.now = t0.Add(time.Second)
	k.c.Frame(k.now, true, true)
	k.c.Frame(k.now.Add(time.Second), true, true)
	if _, _, open := k.c.Open(); !open {
		t.Fatal("a held card closed")
	}
	// any other overlay: closed, and nothing opens under it
	k.c.Report("a", "U1", image.Rect(0, 0, 1, 1), f32.Point{})
	k.c.Frame(k.now.Add(2*time.Second), true, false)
	if _, _, open := k.c.Open(); open {
		t.Fatal("a card stayed open under a menu")
	}
	for i := 0; i < 5; i++ {
		k.c.Report("a", "U1", image.Rect(0, 0, 1, 1), f32.Point{})
		k.c.Frame(k.now.Add(3*time.Second+time.Duration(i)*200*time.Millisecond), true, false)
	}
	if _, _, open := k.c.Open(); open {
		t.Fatal("a card opened under a menu")
	}
}

func TestPressClosesUntilLeft(t *testing.T) {
	k := newClock(t)
	k.at(0, "a", false)
	k.at(350*time.Millisecond, "a", true)
	k.c.Pressed("a")
	k.at(400*time.Millisecond, "a", false)
	k.at(2*time.Second, "a", false) // still resting on it: stays closed
	k.c.Left("a")
	k.at(3*time.Second, "a", false)
	k.at(3*time.Second+350*time.Millisecond, "a", true)
}

func TestFrameDeadlines(t *testing.T) {
	c := &Controller{}
	c.Report("a", "U1", image.Rect(0, 0, 1, 1), f32.Point{})
	if next := c.Frame(t0, false, false); !next.Equal(t0.Add(ShowDelay)) {
		t.Errorf("next = %v, want the show deadline", next.Sub(t0))
	}
	c.Report("a", "U1", image.Rect(0, 0, 1, 1), f32.Point{})
	if next := c.Frame(t0.Add(ShowDelay), false, false); !next.Equal(t0.Add(ShowDelay + Fade)) {
		t.Errorf("next = %v, want the end of the fade", next.Sub(t0))
	}
	if o := c.Opacity(t0.Add(ShowDelay + Fade/2)); o < .45 || o > .55 {
		t.Errorf("opacity mid-fade = %v", o)
	}
	if next := c.Frame(t0.Add(time.Second), false, false); !next.Equal(t0.Add(time.Second + HideDelay)) {
		t.Errorf("next = %v, want the hide deadline", next.Sub(t0))
	}
}

// Place is Slack's Popover.keepInBounds for "top-left", offsetY -12,
// tryOppositePosition, allowances 16/10.
func TestPlace(t *testing.T) {
	win := image.Pt(1000, 800)
	sz := image.Pt(340, 250)
	for _, tc := range []struct {
		name   string
		anchor image.Rectangle
		want   image.Point
	}{
		{"above", image.Rect(100, 400, 180, 422), image.Pt(100, 400-12-250)},
		{"flips below near the top", image.Rect(100, 120, 180, 142), image.Pt(100, 142+12)},
		{"exactly fits above", image.Rect(100, 272, 180, 294), image.Pt(100, 10)},
		{"one px short flips", image.Rect(100, 271, 180, 293), image.Pt(100, 293+12)},
		{"clamped right", image.Rect(900, 500, 960, 522), image.Pt(1000-16-340, 500-12-250)},
		{"clamped left", image.Rect(4, 500, 40, 522), image.Pt(16, 500-12-250)},
	} {
		if got := Place(tc.anchor, sz, win, 12, 16, 10); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	// neither side fits: stays clamped to the top allowance
	small := image.Pt(1000, 300)
	if got := Place(image.Rect(100, 120, 180, 142), sz, small, 12, 16, 10); got != image.Pt(100, 10) {
		t.Errorf("no room: %v, want clamped (100,10)", got)
	}
}

// A Trigger under a real router: the pointer resting on it opens the card
// at the trigger's window bounds after 350ms, and a press closes it.
func TestTriggerRouter(t *testing.T) {
	var r input.Router
	c := &Controller{}
	SetDefault(c)
	defer SetDefault(nil)
	ptr := image.Point{}
	c.Pointer = func() image.Point { return ptr }
	var tr Trigger
	off := image.Pt(200, 300) // the trigger's window position
	size := image.Pt(80, 22)
	ops := new(op.Ops)
	now := t0
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Now: now, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(1000, 800)), Source: r.Source()}
		st := op.Offset(off).Push(ops)
		tr.Layout(gtx, "U42", size)
		st.Pop()
		c.Frame(now, false, false)
		r.Frame(ops)
	}
	frame()
	ptr = image.Pt(230, 310)
	r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(230, 310)})
	frame()
	if !tr.Hovered() {
		t.Fatal("trigger not hovered")
	}
	now = now.Add(349 * time.Millisecond)
	frame()
	if _, _, open := c.Open(); open {
		t.Fatal("opened before 350ms")
	}
	now = now.Add(time.Millisecond)
	frame()
	req, _, open := c.Open()
	if !open {
		t.Fatal("not open after 350ms")
	}
	if want := (image.Rectangle{Min: off, Max: off.Add(size)}); req.Anchor != want || req.UserID != "U42" {
		t.Errorf("anchor %v user %s, want %v U42", req.Anchor, req.UserID, want)
	}
	r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: f32.Pt(230, 310), Buttons: pointer.ButtonPrimary})
	frame()
	frame()
	if _, _, open := c.Open(); open {
		t.Fatal("a click on the trigger left the card open")
	}
	// moving off and back: the full delay again
	r.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: f32.Pt(230, 310)})
	ptr = image.Pt(600, 600)
	r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(600, 600)})
	frame()
	if tr.Hovered() {
		t.Fatal("still hovered after leaving")
	}
	ptr = image.Pt(210, 305)
	r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(210, 305)})
	now = now.Add(time.Second)
	frame()
	now = now.Add(350 * time.Millisecond)
	frame()
	if _, _, open := c.Open(); !open {
		t.Fatal("no card after coming back")
	}
}

// PlaceTop is keepInBounds for "top" (a link preview's hover card),
// measured live (2026-11-09): a 716x114 wrapper at (458,361) puts the
// 340x299 card at (646,50), centered, 12px above. With no room above it
// flips below; it stays 16px from the window's sides.
func TestPlaceTop(t *testing.T) {
	win := image.Pt(1440, 900)
	wrap := image.Rect(458, 361, 458+716, 361+114)
	if p := PlaceTop(wrap, image.Pt(340, 299), win, 12, 16, 10); p != image.Pt(646, 50) {
		t.Errorf("above: %v, want (646,50)", p)
	}
	low := image.Rect(458, 100, 458+716, 214)
	if p := PlaceTop(low, image.Pt(340, 299), win, 12, 16, 10); p != image.Pt(646, 214+12) {
		t.Errorf("no room above: %v, want below at y %d", p, 214+12)
	}
	edge := image.Rect(0, 500, 100, 614)
	if p := PlaceTop(edge, image.Pt(340, 299), win, 12, 16, 10); p.X != 16 {
		t.Errorf("at the left edge: x %d, want 16", p.X)
	}
}

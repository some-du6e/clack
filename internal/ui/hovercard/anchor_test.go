package hovercard

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
)

// trigEnv lays a Trigger out at off (or not at all) over a window-wide
// background handler, as the ui root's tracker is, so the router keeps
// the mouse wherever it rests.
type trigEnv struct {
	t    *testing.T
	r    input.Router
	c    *Controller
	tr   Trigger
	ptr  image.Point
	off  image.Point
	size image.Point
	show bool // lay the trigger out this frame
	now  time.Time
	ops  op.Ops
	bg   int
}

func newTrigEnv(t *testing.T) *trigEnv {
	e := &trigEnv{t: t, c: &Controller{}, off: image.Pt(200, 300), size: image.Pt(80, 22), show: true, now: t0}
	SetDefault(e.c)
	t.Cleanup(func() { SetDefault(nil) })
	e.c.Pointer = func() image.Point { return e.ptr }
	return e
}

func (e *trigEnv) frame() {
	e.ops.Reset()
	gtx := layout.Context{Ops: &e.ops, Now: e.now, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(1000, 800)), Source: e.r.Source()}
	for {
		if _, ok := gtx.Event(pointer.Filter{Target: &e.bg, Kinds: pointer.Move | pointer.Enter | pointer.Leave}); !ok {
			break
		}
	}
	bg := clip.Rect{Max: image.Pt(1000, 800)}.Push(gtx.Ops)
	event.Op(gtx.Ops, &e.bg)
	bg.Pop()
	if e.show {
		st := op.Offset(e.off).Push(gtx.Ops)
		e.tr.Layout(gtx, "U42", e.size)
		st.Pop()
	}
	e.c.Frame(e.now, false, false)
	e.r.Frame(&e.ops)
}

func (e *trigEnv) move(p image.Point) {
	e.ptr = p
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(p.X), float32(p.Y))})
	e.frame()
}

func (e *trigEnv) wait(d time.Duration) {
	e.now = e.now.Add(d)
	e.frame()
}

func (e *trigEnv) rect() image.Rectangle { return image.Rectangle{Min: e.off, Max: e.off.Add(e.size)} }

// A trigger that isn't laid out for a frame never hears the Leave Gio
// owed it (Router.Frame drops the handler). Drawn again with the pointer
// elsewhere it must not report. Trigger also clears on the Cancel Gio
// gives the new handler, so this guards Missed rather than catching the
// old bug; richview's TestBodyMissedFrameForgetsHover is the body that
// opened Karim's card over the Activity filter chips.
func TestTriggerMissedFrameForgetsHover(t *testing.T) {
	e := newTrigEnv(t)
	e.frame()
	e.move(image.Pt(230, 310))
	if !e.tr.Hovered() {
		t.Fatal("trigger not hovered")
	}
	e.show = false // another tab: the row isn't drawn
	e.wait(10 * time.Millisecond)
	e.move(image.Pt(40, 150)) // the pointer goes to the filter chips
	e.wait(10 * time.Millisecond)
	e.show = true // back: the row is drawn again, away from the pointer
	e.wait(10 * time.Millisecond)
	if e.tr.Hovered() {
		t.Error("the trigger still thinks the pointer is on it")
	}
	e.wait(ShowDelay + 10*time.Millisecond)
	if req, _, open := e.c.Open(); open {
		t.Fatalf("a card opened with the pointer at %v, nowhere near the trigger %v: anchor %v", e.ptr, e.rect(), req.Anchor)
	}
	if p, ok := e.c.Pending(); ok {
		t.Fatalf("a card is pending: %+v", p)
	}
}

// The layout moving a trigger under a resting pointer: the probe's Leave
// gives the trigger the pointer's position in its coordinates now, so
// the card opens over where the trigger is (HoverCardTrigger measures it
// when the delay ends), not where it was when the pointer last moved.
func TestTriggerMovedUnderRestingPointer(t *testing.T) {
	e := newTrigEnv(t)
	e.frame()
	e.move(image.Pt(230, 310))
	e.wait(100 * time.Millisecond)
	e.off = e.off.Add(image.Pt(-20, 6)) // a re-layout: still under the pointer
	e.wait(10 * time.Millisecond)
	e.wait(10 * time.Millisecond)
	e.wait(ShowDelay)
	req, _, open := e.c.Open()
	if !open {
		t.Fatal("no card with the pointer resting on the trigger")
	}
	if req.Anchor != e.rect() {
		t.Errorf("anchor %v, want the trigger where it is now %v", req.Anchor, e.rect())
	}
	// moved out from under the pointer: its mouseleave, the 250ms close
	e.off = image.Pt(500, 500)
	e.wait(10 * time.Millisecond) // laid out away: Gio's hit test owes it a Leave
	e.wait(10 * time.Millisecond) // the Leave: the 250ms close starts
	if _, _, open := e.c.Open(); !open {
		t.Error("closed without the 250ms grace")
	}
	e.wait(HideDelay + 10*time.Millisecond)
	if _, _, open := e.c.Open(); open {
		t.Error("the card stayed open after the trigger moved out from under the pointer")
	}
}

// A trigger that unmounts takes its card down at once (the card is its
// child in Slack's React tree), not after the 250ms close.
func TestTriggerUnmountClosesAtOnce(t *testing.T) {
	e := newTrigEnv(t)
	e.frame()
	e.move(image.Pt(230, 310))
	e.wait(ShowDelay + 10*time.Millisecond)
	if _, _, open := e.c.Open(); !open {
		t.Fatal("no card")
	}
	e.wait(10 * time.Millisecond) // a frame with the card open: the trigger says it's there
	e.show = false
	e.wait(10 * time.Millisecond)
	if _, _, open := e.c.Open(); open {
		t.Error("the card outlived its trigger's unmount")
	}
	// mounted again under the pointer: the full delay again
	e.show = true
	e.wait(10 * time.Millisecond)
	e.wait(10 * time.Millisecond)
	if _, _, open := e.c.Open(); open {
		t.Error("the card came back without the show delay")
	}
	e.wait(ShowDelay + 10*time.Millisecond)
	if _, _, open := e.c.Open(); !open {
		t.Error("no card after the show delay over the remounted trigger")
	}
}

// Show's cards (tests and shots) have no trigger: they never count as
// unmounted.
func TestShowNeedsNoTrigger(t *testing.T) {
	c := &Controller{}
	c.Show(Request{Key: "shot", UserID: "U1"}, t0)
	c.overCard = true
	c.Frame(t0.Add(time.Millisecond), false, false)
	c.Frame(t0.Add(2*time.Millisecond), false, false)
	if _, _, open := c.Open(); !open {
		t.Error("a shown card closed with no trigger reporting")
	}
}

// Popover keeps the card where it placed it: a window resize or the
// trigger moving doesn't move it; a new size (its content's
// ResizeObserver) places it again, from the side it's on (sticky).
func TestKeep(t *testing.T) {
	var k Keep
	anchor := image.Rect(400, 300, 460, 320)
	sz := image.Pt(340, 200)
	p := k.PlaceTop(anchor, sz, image.Pt(1440, 900), 12, 16, 10)
	if want := image.Pt(260, 88); p != want {
		t.Fatalf("placed at %v, want %v", p, want)
	}
	if q := k.PlaceTop(image.Rect(10, 600, 50, 620), sz, image.Pt(500, 400), 12, 16, 10); q != p {
		t.Errorf("a resize and a moved trigger moved the card to %v", q)
	}
	if q := k.PlaceTop(anchor, image.Pt(340, 220), image.Pt(1440, 900), 12, 16, 10); q != image.Pt(260, 68) {
		t.Errorf("a new size placed it at %v, want (260,68)", q)
	}
	// flipped below a trigger near the top; a smaller card stays below
	// (Slack starts from the opposite side), Place alone would go above
	var f Keep
	top := image.Rect(400, 150, 460, 170)
	if q := f.PlaceTop(top, sz, image.Pt(1440, 900), 12, 16, 10); q.Y != 182 {
		t.Fatalf("near the top: %v, want below at y 182", q)
	}
	if q := f.PlaceTop(top, image.Pt(340, 100), image.Pt(1440, 900), 12, 16, 10); q.Y != 182 {
		t.Errorf("the smaller card went to %v, want it to stay below (stickyPosition)", q)
	}
	if q := PlaceTop(top, image.Pt(340, 100), image.Pt(1440, 900), 12, 16, 10); q.Y != 38 {
		t.Errorf("PlaceTop from scratch: %v, want above at 38", q)
	}
}

// A canvas mention's card follows its mention while it reports (Quip's
// positioners); a message's stays where it opened.
func TestCanvasCardFollows(t *testing.T) {
	c := &Controller{}
	r1, r2 := image.Rect(100, 300, 160, 320), image.Rect(100, 260, 160, 280)
	rep := func(key any, r image.Rectangle, canvas bool) {
		if canvas {
			c.ReportCanvasChannel(key, "C1", r, f32.Point{})
		} else {
			c.ReportChannel(key, "C1", r, f32.Point{})
		}
	}
	for _, canvas := range []bool{true, false} {
		*c = Controller{}
		rep("k", r1, canvas)
		c.Frame(t0, false, false)
		rep("k", r1, canvas)
		c.Frame(t0.Add(time.Second), false, false)
		rep("k", r2, canvas)
		c.Frame(t0.Add(time.Second+10*time.Millisecond), false, false)
		req, _, open := c.Open()
		if !open {
			t.Fatalf("canvas %v: no card", canvas)
		}
		want := r1
		if canvas {
			want = r2
		}
		if req.Anchor != want {
			t.Errorf("canvas %v: anchor %v, want %v", canvas, req.Anchor, want)
		}
	}
}

// queue sends a Move to p without a frame.
func (e *trigEnv) queue(p image.Point) {
	e.ptr = p
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(p.X), float32(p.Y))})
}

// Two Moves over the trigger between frames: the probe hears only the
// first, the trigger both. Its position is the second's, so the anchor
// is the trigger's box (23c6d543 kept the first: the anchor was off by
// the second Move).
func TestTriggerTwoMovesBetweenFrames(t *testing.T) {
	e := newTrigEnv(t)
	e.frame()
	e.move(image.Pt(230, 310))
	e.queue(image.Pt(240, 312))
	e.queue(image.Pt(262, 317))
	e.wait(10 * time.Millisecond)
	e.wait(ShowDelay + 10*time.Millisecond)
	req, _, open := e.c.Open()
	if !open {
		t.Fatal("no card with the pointer resting on the trigger")
	}
	if req.Anchor != e.rect() {
		t.Errorf("anchor %v, want the trigger %v", req.Anchor, e.rect())
	}
}

// A layout move, then a Move, read in one frame: the Move wins.
func TestTriggerMoveAfterLayoutMoveWins(t *testing.T) {
	e := newTrigEnv(t)
	e.frame()
	e.move(image.Pt(230, 310))
	e.off = e.off.Add(image.Pt(-10, 4)) // still under the pointer
	e.wait(10 * time.Millisecond)
	e.queue(image.Pt(250, 318))
	e.wait(10 * time.Millisecond)
	e.wait(ShowDelay + 10*time.Millisecond)
	req, _, open := e.c.Open()
	if !open {
		t.Fatal("no card with the pointer resting on the trigger")
	}
	if req.Anchor != e.rect() {
		t.Errorf("anchor %v, want the trigger %v", req.Anchor, e.rect())
	}
}

// place is what the ui root does with an open card: placed through a
// Keep that starts from the trigger's side, then the side handed back.
func (e *trigEnv) place(k *Keep) image.Point {
	req, _, _ := e.c.Open()
	k.Start(req.Opposite)
	p := k.PlaceTop(req.Anchor, image.Pt(340, 200), image.Pt(1000, 800), OffsetY, AllowanceX, AllowanceY)
	e.c.SetPlaced(k.Opp())
	return p
}

// Slack's Popover is the trigger's child: its usingOppositePosition lasts
// while the trigger is mounted, open or closed, so a card flipped below
// its trigger starts below the next time (stickyPosition), even where it
// would fit above. A trigger that unmounts comes back with a new Popover.
func TestStickySideLastsWhileMounted(t *testing.T) {
	e := newTrigEnv(t)
	e.off = image.Pt(200, 60) // no room above
	e.frame()
	open := func() {
		t.Helper()
		e.move(e.off.Add(image.Pt(30, 10)))
		e.wait(ShowDelay + 10*time.Millisecond)
		if _, _, ok := e.c.Open(); !ok {
			t.Fatal("no card")
		}
	}
	leave := func() {
		e.move(image.Pt(900, 700))
		e.wait(10 * time.Millisecond)
		e.wait(HideDelay + 10*time.Millisecond)
		if _, _, ok := e.c.Open(); ok {
			t.Fatal("the card didn't close")
		}
	}
	open()
	var k1 Keep
	if p := e.place(&k1); p.Y != e.rect().Max.Y+OffsetY {
		t.Fatalf("near the top: placed at %v, want below the trigger %v", p, e.rect())
	}
	e.wait(10 * time.Millisecond) // the trigger keeps the side
	leave()
	e.off = image.Pt(200, 400) // room above now
	e.wait(10 * time.Millisecond)
	open()
	if req, _, _ := e.c.Open(); !req.Opposite {
		t.Error("the reopened card doesn't start from the side its Popover was left on")
	}
	var k2 Keep
	if p := e.place(&k2); p.Y != e.rect().Max.Y+OffsetY {
		t.Errorf("reopened at %v, want below the trigger %v (stickyPosition: below fits)", p, e.rect())
	}
	e.wait(10 * time.Millisecond)
	leave()
	e.show = false // unmounted: a new Popover
	e.wait(10 * time.Millisecond)
	e.show = true
	e.wait(10 * time.Millisecond)
	open()
	if req, _, _ := e.c.Open(); req.Opposite {
		t.Error("a remounted trigger's card starts from the opposite side")
	}
}

// With the pointer resting on the card, a canvas card follows its
// mention as the mention's layout puts it (Follow): a collaborator's
// block inserted above moves it with no pointer event. A report (the
// pointer back on the mention) wins; another trigger's Follow, and a
// message card's, move nothing.
func TestCanvasCardFollowsLayout(t *testing.T) {
	r1, r2, r3 := image.Rect(100, 300, 160, 320), image.Rect(100, 340, 160, 360), image.Rect(100, 380, 160, 400)
	for _, canvas := range []bool{true, false} {
		c := &Controller{}
		now := t0
		rep := func(r image.Rectangle) {
			if canvas {
				c.ReportCanvasChannel("k", "C1", r, f32.Point{})
			} else {
				c.ReportChannel("k", "C1", r, f32.Point{})
			}
		}
		frame := func() {
			c.Mounted("k")
			c.Frame(now, false, false)
			now = now.Add(10 * time.Millisecond)
		}
		rep(r1)
		frame()
		now = now.Add(time.Second)
		rep(r1)
		frame()
		if _, _, open := c.Open(); !open {
			t.Fatalf("canvas %v: no card", canvas)
		}
		if c.Follows("k") != canvas {
			t.Errorf("canvas %v: Follows %v", canvas, c.Follows("k"))
		}
		c.overCard = true // the pointer rests on the card: no reports
		c.Follow("k", r2)
		c.Follow("other", r3)
		frame()
		req, _, open := c.Open()
		if !open {
			t.Fatalf("canvas %v: the card closed with the pointer on it", canvas)
		}
		want := r1
		if canvas {
			want = r2
		}
		if req.Anchor != want {
			t.Errorf("canvas %v: anchor %v after the mention moved to %v, want %v", canvas, req.Anchor, r2, want)
		}
		// no Follow this frame: the card stays where it was told last
		frame()
		if req, _, _ := c.Open(); req.Anchor != want {
			t.Errorf("canvas %v: anchor %v with no Follow, want %v", canvas, req.Anchor, want)
		}
		// the pointer back on the mention: its report wins
		c.overCard = false
		rep(r3)
		c.Follow("k", r2)
		frame()
		if req, _, _ := c.Open(); canvas && req.Anchor != r3 {
			t.Errorf("canvas: anchor %v with a report at %v and a Follow at %v, want the report", req.Anchor, r3, r2)
		}
	}
}

// A canvas card takes the wheel (Quip's bubble is outside the canvas's
// scroller); a message card's area passes it to the list under it.
func TestCanvasCardTakesWheel(t *testing.T) {
	for _, canvas := range []bool{true, false} {
		var r input.Router
		var ops op.Ops
		var list int
		c := &Controller{}
		card := image.Rect(100, 100, 422, 260)
		c.Show(Request{Key: "k", ChannelID: "C1", Canvas: canvas}, t0)
		scrolled := 0
		frame := func() {
			ops.Reset()
			gtx := layout.Context{Ops: &ops, Now: t0, Constraints: layout.Exact(image.Pt(1000, 800)), Source: r.Source()}
			for {
				e, ok := gtx.Event(pointer.Filter{Target: &list, Kinds: pointer.Scroll, ScrollY: pointer.ScrollRange{Min: -1000, Max: 1000}})
				if !ok {
					break
				}
				if pe, ok := e.(pointer.Event); ok && pe.Kind == pointer.Scroll {
					scrolled++
				}
			}
			bg := clip.Rect{Max: image.Pt(1000, 800)}.Push(gtx.Ops)
			event.Op(gtx.Ops, &list)
			bg.Pop()
			c.CardArea(gtx, card)
			r.Frame(&ops)
		}
		frame()
		frame()
		at := f32.Pt(200, 180)
		r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at},
			pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: at, Scroll: f32.Pt(0, 120)})
		frame()
		frame()
		if canvas && scrolled != 0 {
			t.Errorf("a wheel on the canvas card scrolled the list under it (%d events)", scrolled)
		}
		if !canvas && scrolled == 0 {
			t.Error("a wheel on a message card didn't reach the list under it")
		}
	}
}

// A wheel gesture that began off the card (on its mention) stays on the
// list under it when the card, following the mention, slides under the
// pointer: Chromium latches a gesture to the scroller it began on. Once
// it has ended (500ms without an event), a gesture that begins on the
// card is the card's.
func TestCanvasCardWheelLatch(t *testing.T) {
	var r input.Router
	var ops op.Ops
	var list int
	c := &Controller{}
	c.Show(Request{Key: "k", ChannelID: "C1", Canvas: true}, t0)
	now := t0
	card := image.Rect(100, 100, 422, 180) // flush under a mention at y 80-100
	scrolled := 0
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: &ops, Now: now, Constraints: layout.Exact(image.Pt(1000, 800)), Source: r.Source()}
		for {
			e, ok := gtx.Event(pointer.Filter{Target: &list, Kinds: pointer.Scroll, ScrollY: pointer.ScrollRange{Min: -1000, Max: 1000}})
			if !ok {
				break
			}
			if pe, ok := e.(pointer.Event); ok && pe.Kind == pointer.Scroll {
				scrolled++
			}
		}
		bg := clip.Rect{Max: image.Pt(1000, 800)}.Push(gtx.Ops)
		event.Op(gtx.Ops, &list)
		bg.Pop()
		c.CardArea(gtx, card)
		r.Frame(&ops)
		now = now.Add(16 * time.Millisecond)
	}
	at := f32.Pt(200, 95) // on the mention
	wheel := func() {
		r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at},
			pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: at, Scroll: f32.Pt(0, 4)})
		frame()
	}
	frame()
	frame()
	wheel()
	if scrolled != 1 {
		t.Fatalf("a wheel on the mention reached the list %d times, want 1", scrolled)
	}
	card = card.Sub(image.Pt(0, 10)) // the canvas scrolled: the card follows its mention under the pointer
	frame()
	wheel()
	wheel()
	if scrolled != 3 {
		t.Errorf("the gesture that began on the mention reached the list %d times of 3 once the card slid under the pointer", scrolled)
	}
	now = now.Add(600 * time.Millisecond) // the gesture ends
	frame()
	wheel()
	if scrolled != 3 {
		t.Errorf("a gesture that began on the card reached the list under it (%d)", scrolled)
	}
}

package richview

import (
	"image"
	"strings"
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

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/hovercard"
)

// bodyEnv lays a message body out at off in a window with a window-wide
// handler under it (the ui root's tracker), runs the hover card
// controller after it each frame, and can leave the body out of a frame
// (another Activity tab, a recycled row).
type bodyEnv struct {
	t    *testing.T
	r    input.Router
	ops  op.Ops
	st   *Style
	v    *View
	c    *hovercard.Controller
	ptr  image.Point
	off  image.Point
	show bool
	now  time.Time
	bg   int
}

func newBodyEnv(t *testing.T) *bodyEnv {
	words := strings.Repeat("synthetic words wrap onto the next line ", 4)
	d := doc(para(rt.Span{Kind: rt.Channel, Text: "#general", ID: "C0GENERAL1"}, txt(" has the plan, "+words)))
	e := &bodyEnv{t: t, st: newTestStyle(), v: New(d, Options{}), c: &hovercard.Controller{}, off: image.Pt(100, 200), show: true,
		now: time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC)}
	e.c.Pointer = func() image.Point { return e.ptr }
	hovercard.SetDefault(e.c)
	t.Cleanup(func() { hovercard.SetDefault(nil) })
	e.frame()
	return e
}

func (e *bodyEnv) frame() {
	e.ops.Reset()
	gtx := layout.Context{Ops: &e.ops, Now: e.now, Source: e.r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(1000, 800))}
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
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(400, 800)}
		e.v.Layout(g, e.st, &recHandler{})
		st.Pop()
	}
	e.c.Frame(e.now, false, false)
	e.r.Frame(&e.ops)
}

func (e *bodyEnv) wait(d time.Duration) {
	e.now = e.now.Add(d)
	e.frame()
}

func (e *bodyEnv) move(p image.Point) {
	e.ptr = p
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(p.X), float32(p.Y))})
	e.wait(10 * time.Millisecond)
}

// mention is the #channel's window box.
func (e *bodyEnv) mention() image.Rectangle {
	return e.v.targetBounds(findFrag(e.t, &e.v.d, "#general").target).Add(e.off)
}

// Karim's #welcome-to-hack-club card (2026-11-20): a body hovered on its
// #channel, left out of a frame while the pointer went elsewhere, then
// drawn again. Gio gave its new handler only a Cancel, which ends a press
// and not the hover, so the body went on reporting the channel with an
// anchor of its old local pointer moved by the pointer's travel, and a
// card opened with nothing under it. Slack's spans unmount with the row
// and mount unhovered.
func TestBodyMissedFrameForgetsHover(t *testing.T) {
	e := newBodyEnv(t)
	m := e.mention()
	e.move(m.Min.Add(m.Size().Div(2)))
	if p, ok := e.c.Pending(); !ok || p.ChannelID != "C0GENERAL1" {
		t.Fatalf("on the #channel: pending %v %+v", ok, p)
	}
	e.show = false // another tab
	e.wait(10 * time.Millisecond)
	e.move(image.Pt(30, 120)) // the pointer goes to the filter chips
	e.show = true             // back
	e.wait(10 * time.Millisecond)
	e.wait(hovercard.ShowDelay + 10*time.Millisecond)
	if req, _, open := e.c.Open(); open {
		t.Fatalf("a card opened with the pointer at %v, the body at %v: anchor %v (the mention is at %v)", e.ptr, e.off, req.Anchor, e.mention())
	}
	if p, ok := e.c.Pending(); ok {
		t.Fatalf("a card is pending with the pointer at %v: anchor %v", e.ptr, p.Anchor)
	}
	if e.v.Pointer() {
		t.Error("the body still thinks the pointer is on it")
	}
}

// The body moving under a resting pointer (a scroll, a list laying out
// again, a pane narrowing it): the probe's Leave tells it where the
// pointer is now. Off the #channel, its pending card is dropped and its
// open card closes 250ms later (Chromium's hover update: mouseleave);
// still on it, the card opens over where it is now and doesn't follow
// it after.
func TestBodyMovedUnderRestingPointer(t *testing.T) {
	e := newBodyEnv(t)
	m := e.mention()
	at := m.Min.Add(m.Size().Div(2))
	e.move(at)
	e.wait(100 * time.Millisecond)
	// a line up: the pointer is on the body's second line, plain words
	e.off.Y -= m.Dy() + 4
	e.wait(10 * time.Millisecond)
	e.wait(10 * time.Millisecond)
	if !e.v.Pointer() {
		t.Fatal("the body is still under the pointer but doesn't know it")
	}
	e.wait(hovercard.ShowDelay)
	if req, _, open := e.c.Open(); open {
		t.Fatalf("the #channel moved off the pointer and its card opened anyway, at %v (the mention is at %v)", req.Anchor, e.mention())
	}

	// back under the pointer, 3px off: the card opens over it as it is
	e.off.Y += m.Dy() + 4 - 3
	e.wait(10 * time.Millisecond)
	e.wait(10 * time.Millisecond)
	e.wait(hovercard.ShowDelay + 10*time.Millisecond)
	req, _, open := e.c.Open()
	if !open {
		t.Fatal("no card with the pointer resting on the #channel")
	}
	if want := e.mention(); req.Anchor != want {
		t.Errorf("the card opened anchored at %v, the mention is at %v", req.Anchor, want)
	}
	// open, the body moves off the pointer: the 250ms close
	e.off.Y -= 2 * m.Dy()
	e.wait(10 * time.Millisecond)
	e.wait(10 * time.Millisecond)
	e.wait(hovercard.HideDelay + 10*time.Millisecond)
	if _, _, open := e.c.Open(); open {
		t.Error("the card stayed open after its #channel moved out from under the pointer")
	}
}

// A body that unmounts takes its open card with it at once.
func TestBodyUnmountClosesCard(t *testing.T) {
	e := newBodyEnv(t)
	m := e.mention()
	e.move(m.Min.Add(m.Size().Div(2)))
	e.wait(hovercard.ShowDelay + 10*time.Millisecond)
	e.wait(10 * time.Millisecond)
	if _, _, open := e.c.Open(); !open {
		t.Fatal("no card")
	}
	e.show = false
	e.wait(10 * time.Millisecond)
	if _, _, open := e.c.Open(); open {
		t.Error("the card outlived its body's unmount")
	}
}

// queue sends a Move to p without a frame (several land between frames).
func (e *bodyEnv) queue(p image.Point) {
	e.ptr = p
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(p.X), float32(p.Y))})
}

// words is a point on the body's plain words, a line under the #channel.
func (e *bodyEnv) words() image.Point {
	m := e.mention()
	return image.Pt(m.Max.X+40, m.Max.Y+m.Dy()/2+4)
}

// The probe's Leave is older than the body's own events in a frame: the
// layout moved the body (the probe's Leave, at the pointer's place then)
// and the pointer then moved onto the #channel. The Move wins: the card
// pending is the channel's, over where it is. (23c6d543 read the probe
// after the Move: the body went back to the words.)
func TestBodyMoveAfterLayoutMoveWins(t *testing.T) {
	e := newBodyEnv(t)
	e.move(e.words())
	if !e.v.Pointer() {
		t.Fatal("the body isn't hovered")
	}
	if _, ok := e.c.Pending(); ok {
		t.Fatal("the words reported a card")
	}
	e.off.Y += 5 // a layout move under the resting pointer
	e.wait(10 * time.Millisecond)
	m := e.mention()
	e.queue(m.Min.Add(m.Size().Div(2))) // then onto the #channel
	e.wait(10 * time.Millisecond)
	p, ok := e.c.Pending()
	if !ok || p.ChannelID != "C0GENERAL1" {
		t.Fatalf("the pointer moved onto the #channel after the layout moved: pending %v %+v", ok, p)
	}
	if p.Anchor != m {
		t.Errorf("anchor %v, the mention is at %v", p.Anchor, m)
	}
}

// Two Moves between frames, the second off the #channel: the probe hears
// only the first (it is off the probe from then on), the body both. The
// body is on the words, and no card opens. (23c6d543 let the probe's
// Leave, at the first Move, win: the body stayed on the #channel and its
// card opened with the pointer on the words, anchored off by the second
// Move.)
func TestBodyTwoMovesBetweenFrames(t *testing.T) {
	e := newBodyEnv(t)
	m := e.mention()
	at := m.Min.Add(m.Size().Div(2))
	e.move(at)
	if p, ok := e.c.Pending(); !ok || p.ChannelID != "C0GENERAL1" {
		t.Fatalf("on the #channel: pending %v %+v", ok, p)
	}
	e.queue(at.Add(image.Pt(3, 0))) // still on it
	e.queue(e.words())              // off it
	e.wait(10 * time.Millisecond)
	e.wait(10 * time.Millisecond)
	if !e.v.Pointer() {
		t.Fatal("the body isn't hovered")
	}
	if p, ok := e.c.Pending(); ok {
		t.Fatalf("the pointer is on the words and the #channel's card is pending, anchored at %v", p.Anchor)
	}
	e.wait(hovercard.ShowDelay + 10*time.Millisecond)
	if req, _, open := e.c.Open(); open {
		t.Fatalf("a card opened with the pointer on the words: anchor %v (the mention is at %v)", req.Anchor, m)
	}
	// and the other way: two Moves ending on the #channel
	e.queue(e.words().Add(image.Pt(5, 0)))
	e.queue(at)
	e.wait(10 * time.Millisecond)
	if p, ok := e.c.Pending(); !ok || p.Anchor != m {
		t.Fatalf("two Moves ending on the #channel: pending %v, anchor %v, the mention is at %v", ok, p.Anchor, m)
	}
}

// A body keeps each trigger's Popover side while it is drawn (Slack's
// Popover state, under stickyPosition), and drops it when it misses a
// frame (its spans unmounted).
func TestBodyKeepsPopoverSide(t *testing.T) {
	e := newBodyEnv(t)
	m := e.mention()
	at := m.Min.Add(m.Size().Div(2))
	e.move(at)
	e.wait(hovercard.ShowDelay + 10*time.Millisecond)
	if _, _, open := e.c.Open(); !open {
		t.Fatal("no card")
	}
	e.c.SetPlaced(true) // the root placed it on the opposite side
	e.wait(10 * time.Millisecond)
	e.move(e.words())
	e.wait(hovercard.HideDelay + 10*time.Millisecond)
	e.move(at)
	if p, ok := e.c.Pending(); !ok || !p.Opposite {
		t.Fatalf("back on the #channel: pending %v, opposite %v; want the side its card was left on", ok, p.Opposite)
	}
	e.move(e.words())
	e.show = false
	e.wait(10 * time.Millisecond)
	e.show = true
	e.wait(10 * time.Millisecond)
	e.move(at)
	if p, ok := e.c.Pending(); !ok || p.Opposite {
		t.Fatalf("after a missed frame: pending %v, opposite %v; want a new Popover's side", ok, p.Opposite)
	}
}

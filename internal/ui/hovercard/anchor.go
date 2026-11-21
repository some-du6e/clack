package hovercard

import (
	"image"
	"math"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
)

// A trigger learns where it is in the window from the pointer: its local
// pointer position and the window pointer of the same event give its
// origin (Report's off). Two things leave that pairing stale, and with
// it the card's anchor and whether the trigger is hovered at all:
//
//   - Gio forgets a handler that isn't in a frame's ops (Router.Frame
//     deletes it, and the hit test skips its Leave); drawn again, the
//     new handler gets only a Cancel (pointerHandler.ResetEvent). A
//     message body (richview) clears its hover on a Leave, and a Cancel
//     ends only a press, so a body hidden for a frame (another Activity
//     tab, a recycled row, a pane that replaced its page) and drawn
//     again with the pointer elsewhere still thought the pointer was on
//     it, at the old local position: it reported an anchor of local +
//     (window pointer now - local pointer then), anywhere in the window
//     (Karim's #welcome-to-hack-club card over the Activity filter chips,
//     2026-11-20). Slack's trigger unmounts and mounts again unhovered.
//     Probe.Missed says when that happened (Trigger clears on the Cancel
//     as well).
//   - A layout that moves a trigger under a resting pointer sends it no
//     event while the pointer stays inside its area (a message body:
//     the mention under the pointer is now another word), so it keeps
//     the old target and position. Chromium recomputes the hover after a
//     scroll or layout and sends mouseover/mouseleave to the spans that
//     moved. The probe is a 1px pass-through area where the trigger
//     believes the pointer is: Gio's hit test runs again at the pointer's
//     last position after every frame, so when the layout moves the
//     probe away the probe gets a Leave carrying the pointer's position
//     in the trigger's coordinates now (Probe.Moved). It asks for Leave
//     only, so the Enter that follows every move asks for no frame.

// Probe keeps a trigger's pointer position honest (see above). The
// zero value is ready; it belongs to one trigger.
type Probe struct {
	tag  int
	seen int // Controller.Seq()+1 when the trigger last laid out (0: never)
}

// Missed reports whether the trigger missed a frame since it last laid
// out: the controller ran more than once since (Gio dropped its handler,
// and any Leave it was owed). Call it once per layout, first.
func (p *Probe) Missed() bool {
	c := current
	if c == nil {
		return false
	}
	s := c.Seq()
	missed := p.seen != 0 && s > p.seen // seen is last Seq+1: contiguous while s <= seen
	p.seen = s + 1
	return missed
}

// Moved returns the pointer's position (local) when the layout moved the
// probe out from under it since the last frame (or the pointer moved,
// which the trigger also hears as a Move there). Read it before the
// trigger's own events: the Leave is the probe's only event until it is
// laid out again, so after several Moves between frames it carries the
// first, and a Move after a layout move is newer than it.
func (p *Probe) Moved(gtx layout.Context) (f32.Point, bool) {
	var pos f32.Point
	moved := false
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &p.tag, Kinds: pointer.Leave})
		if !ok {
			break
		}
		if pe, ok := e.(pointer.Event); ok && pe.Kind == pointer.Leave {
			pos, moved = pe.Position, true
		}
	}
	return pos, moved
}

// Layout puts the probe at at (local): a 1px pass-through area. Lay it
// out in the trigger's own coordinates, after its own input area.
func (p *Probe) Layout(gtx layout.Context, at f32.Point) {
	x, y := int(math.Floor(float64(at.X))), int(math.Floor(float64(at.Y)))
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	defer clip.Rect{Min: image.Pt(x, y), Max: image.Pt(x+1, y+1)}.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, &p.tag)
}

// Keep is the Popover's contentBounds: where a card sits. It is placed
// when the card first draws and again only when its own size changes
// (the ResizeObserver on popoverContent), never because the trigger
// moved or the window resized. A card placed on the opposite side
// starts there the next time (stickyPosition).
type Keep struct {
	sz, pos image.Point
	opp     bool
	set     bool
}

// Place is Place's keepInBounds through k.
func (k *Keep) Place(anchor image.Rectangle, sz, win image.Point, gap, ax, ay int) image.Point {
	return k.place(false, anchor, sz, win, gap, ax, ay)
}

// PlaceTop is PlaceTop's keepInBounds through k.
func (k *Keep) PlaceTop(anchor image.Rectangle, sz, win image.Point, gap, ax, ay int) image.Point {
	return k.place(true, anchor, sz, win, gap, ax, ay)
}

// Start sets the side a new card starts from (Request.Opposite: the
// trigger's Popover was left on the opposite side), before it is first
// placed.
func (k *Keep) Start(opp bool) {
	if !k.set {
		k.opp = opp
	}
}

// Opp reports whether the card sits on the opposite side.
func (k *Keep) Opp() bool { return k.opp }

// At keeps what place computes for a card of size sz.
func (k *Keep) At(sz image.Point, place func() image.Point) image.Point {
	if !k.set || sz != k.sz {
		k.sz, k.pos, k.set = sz, place(), true
	}
	return k.pos
}

func (k *Keep) place(centre bool, anchor image.Rectangle, sz, win image.Point, gap, ax, ay int) image.Point {
	if k.set && sz == k.sz {
		return k.pos
	}
	pos, opp := keepInBounds(centre, anchor, sz, win, gap, ax, ay, k.opp)
	k.sz, k.pos, k.opp, k.set = sz, pos, opp, true
	return pos
}

// keepInBounds is TxLv's keepInBounds for "top-left" (centre false) or
// "top" (centre true) with offsetY -gap and tryOppositePosition, starting
// from the opposite side when opp (usingOppositePosition with
// stickyPosition). It returns the position and whether the card now sits
// on the opposite side.
func keepInBounds(centre bool, anchor image.Rectangle, sz, win image.Point, gap, ax, ay int, opp bool) (image.Point, bool) {
	left, right := ax, win.X-ax
	top, bottom := ay, win.Y-ay
	x := anchor.Min.X
	if centre {
		x = anchor.Min.X + (anchor.Dx()-sz.X)/2
	}
	if x < left {
		x = left
	} else if x+sz.X > right {
		x = max(left, right-sz.X)
	}
	above := anchor.Min.Y - gap - sz.Y
	below := anchor.Max.Y + gap
	y0, alt := above, below
	if opp {
		y0, alt = below, above
	}
	y := y0
	if y < top {
		y = top
	} else if y+sz.Y > bottom {
		y = max(top, bottom-sz.Y)
	}
	if y != y0 && alt >= top && alt+sz.Y <= bottom {
		return image.Pt(x, alt), !opp
	}
	return image.Pt(x, y), opp
}

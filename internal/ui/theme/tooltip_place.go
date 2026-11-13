package theme

import (
	"image"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
)

// Where Slack puts a tooltip (client-boot, build 32bb3cc8205b, read from
// the public CDN without a cookie). Every tooltip in Clack is placed by
// PlaceTooltip, so they all keep inside the window the same way.
//
//   - BaseTooltip (F2XN) opens a Popover (L1PI) with its position (default
//     "top"), offsetX / offsetY (default 0), allowanceX 3, allowanceY 3 and
//     tryOppositePosition, over the hovered element's client rect.
//   - The Popover's content is TooltipTip (tgOB): the .c-tooltip__tip with
//     .c-tooltip__tip--{position}, whose 8px margin on the target's side
//     (margin-bottom for top*, margin-top for bottom*, margin-right for
//     left, margin-left for right) is part of the box the Popover measures.
//   - keepInBounds (L1PI) puts that box at getInitialPosition, then keeps
//     it allowanceX inside the window's left and right edges and
//     allowanceY inside its top and bottom by shifting it. When the top
//     had to move and tryOppositePosition is on, it takes the vertically
//     opposite position (top ↔ bottom, top-left ↔ bottom-left, left and
//     right stay) with offsetY negated, if that fits; the tip then draws
//     with the opposite position's class (renderChildren passes it), so
//     the margin and the arrow move to the other side. Left and right
//     never flip.
//   - The arrow (.c-tooltip__tip__arrow, a 9px square turned 45°) stays on
//     the target for top and bottom: TooltipTip sets its left to the
//     target's centre minus the tip's left, so when keepInBounds shifts
//     the box the arrow stays put. top-left / bottom-left put it
//     arrowOffset from the box's left (BaseTooltip: the target's half
//     width), top-right / bottom-right that far from its right; left and
//     right centre it on the box (top: 50%).
//
// Texty's tooltips (TextyTooltip, boot-shared) are Popovers opened from a
// mouseover handler with allowanceX 0, allowanceY 0 and no
// tryOppositePosition (TextyTip).

// Tooltip positions (TooltipTip's, A.Z in tgOB).
const (
	TipTop         = "top"
	TipTopLeft     = "top-left"
	TipTopRight    = "top-right"
	TipBottom      = "bottom"
	TipBottomLeft  = "bottom-left"
	TipBottomRight = "bottom-right"
	TipLeft        = "left"
	TipRight       = "right"
)

// TipOpts are a tooltip Popover's placement props, in px.
type TipOpts struct {
	Pos              string // a Tip* position ("" is TipTop)
	OffsetX, OffsetY int
	// AllowanceX / AllowanceY keep the box that far inside the window.
	AllowanceX, AllowanceY int
	// TryOpposite is tryOppositePosition.
	TryOpposite bool
	// ArrowOffset is tipArrowOffset for the -left / -right positions; 0
	// is BaseTooltip's (the target's half width).
	ArrowOffset int
}

// BaseTooltip is BaseTooltip's Popover at pos: allowance 3 both ways and
// tryOppositePosition.
func BaseTooltip(gtx layout.Context, pos string) TipOpts {
	return TipOpts{Pos: pos, AllowanceX: gtx.Dp(3), AllowanceY: gtx.Dp(3), TryOpposite: true}
}

// TextyTip is TextyTooltip's Popover at pos: allowance 0, no opposite.
func TextyTip(pos string) TipOpts { return TipOpts{Pos: pos} }

// TipPlace is a placed tooltip: the c-tooltip__tip's box, its arrow and
// the position it drew with (the opposite one after a flip).
type TipPlace struct {
	Pos     string
	Box     image.Rectangle
	Arrow   TooltipArrow
	Flipped bool
}

// PlaceTooltip places a c-tooltip__tip of size box for a target (both in
// the same coordinates) inside bounds, the window in those coordinates;
// an empty bounds doesn't limit it.
func PlaceTooltip(gtx layout.Context, target image.Rectangle, box image.Point, bounds image.Rectangle, o TipOpts) TipPlace {
	pos := o.Pos
	if pos == "" {
		pos = TipTop
	}
	gap := gtx.Dp(8)
	content := box
	if isSide(pos) {
		content.X += gap
	} else {
		content.Y += gap
	}
	at, flipped := KeepInBounds(target, content, bounds, pos, o.OffsetX, o.OffsetY, o.AllowanceX, o.AllowanceY, o.TryOpposite)
	if flipped {
		pos = PopoverOpposite[pos]
	}
	// the tip inside the content box: its margin is on the target's side
	tip := at
	switch pos {
	case TipBottom, TipBottomLeft, TipBottomRight:
		tip.Y += gap
	case TipRight:
		tip.X += gap
	}
	r := image.Rectangle{Min: tip, Max: tip.Add(box)}
	return TipPlace{Pos: pos, Box: r, Arrow: tipArrow(gtx, pos, r, target, o.ArrowOffset), Flipped: flipped}
}

// tipArrow is TooltipTip's arrow for a box r at pos.
func tipArrow(gtx layout.Context, pos string, r, target image.Rectangle, off int) TooltipArrow {
	if off == 0 {
		off = target.Dx() / 2 // BaseTooltip: tipArrowOffset || targetBounds.width / 2
	}
	cx := (target.Min.X + target.Max.X) / 2
	a := TooltipArrow{Half: gtx.Dp(6)}
	switch pos {
	case TipTop:
		a.C = image.Pt(cx, r.Max.Y)
	case TipBottom:
		a.C = image.Pt(cx, r.Min.Y)
	case TipTopLeft:
		a.C = image.Pt(r.Min.X+off, r.Max.Y)
	case TipBottomLeft:
		a.C = image.Pt(r.Min.X+off, r.Min.Y)
	case TipTopRight:
		a.C = image.Pt(r.Max.X-off, r.Max.Y)
	case TipBottomRight:
		a.C = image.Pt(r.Max.X-off, r.Min.Y)
	case TipLeft:
		a.C = image.Pt(r.Max.X, (r.Min.Y+r.Max.Y)/2)
	case TipRight:
		a.C = image.Pt(r.Min.X, (r.Min.Y+r.Max.Y)/2)
	}
	return a
}

func isSide(pos string) bool { return pos == TipLeft || pos == TipRight }

// ---- Slack's Popover placement (client-boot L1PI) ----

// PopoverOpposite is L1PI's table of vertically opposite positions.
var PopoverOpposite = map[string]string{
	"top": "bottom", "top-left": "bottom-left", "top-right": "bottom-right",
	"left": "left", "left-bottom": "left-top", "left-top": "left-bottom",
	"right": "right", "right-bottom": "right-top", "right-top": "right-bottom",
	"bottom": "top", "bottom-left": "top-left", "bottom-right": "top-right",
}

// popoverBox is getInitialPosition's result.
type popoverBox struct{ top, bottom, left, right int }

// popoverInitial is getInitialPosition for a target t and a card sz,
// offsets n (x) and a (y); its "right-bottom" bottom (i.bottom + height)
// is Slack's as written.
func popoverInitial(pos string, t image.Rectangle, sz image.Point, n, a int) popoverBox {
	w, h := sz.X, sz.Y
	switch pos {
	case "top":
		return popoverBox{t.Min.Y - h + a, t.Min.Y + a, t.Min.X + (t.Dx()-w)/2 + n, t.Max.X - (t.Dx()-w)/2 - n}
	case "top-left":
		return popoverBox{t.Min.Y - h + a, t.Min.Y + a, t.Min.X + n, t.Min.X + w + n}
	case "top-right":
		return popoverBox{t.Min.Y - h + a, t.Min.Y + a, t.Max.X - w + n, t.Max.X + n}
	case "left":
		return popoverBox{t.Min.Y + (t.Dy()-h)/2 + a, t.Max.Y - (t.Dy()-h)/2 - a, t.Min.X - w + n, t.Min.X + n}
	case "left-bottom":
		return popoverBox{t.Min.Y + a, t.Min.Y + h + a, t.Min.X - w + n, t.Min.X + n}
	case "left-top":
		return popoverBox{t.Max.Y - h + a, t.Max.Y + a, t.Min.X - w + n, t.Min.X + n}
	case "right":
		return popoverBox{t.Min.Y + (t.Dy()-h)/2 + a, t.Max.Y - (t.Dy()-h)/2 - a, t.Max.X + n, t.Max.X + w + n}
	case "right-bottom":
		return popoverBox{t.Min.Y + a, t.Max.Y + h + a, t.Max.X + n, t.Max.X + w + n}
	case "right-top":
		return popoverBox{t.Max.Y - h + a, t.Max.Y + a, t.Max.X + n, t.Max.X + w + n}
	case "bottom":
		return popoverBox{t.Max.Y + a, t.Max.Y + h + a, t.Min.X + (t.Dx()-w)/2 + n, t.Max.X - (t.Dx()-w)/2 - n}
	case "bottom-left":
		return popoverBox{t.Max.Y + a, t.Max.Y + h + a, t.Min.X + n, t.Min.X + w + n}
	case "bottom-right":
		return popoverBox{t.Max.Y + a, t.Max.Y + h + a, t.Max.X - w + n, t.Max.X + n}
	}
	return popoverBox{t.Min.Y, t.Min.Y + h, t.Min.X, t.Min.X + w}
}

// KeepInBounds is L1PI's keepInBounds: the top-left of a card sz for
// target t at pos inside bounds b (empty: unbounded), allowances ax / ay,
// and whether it took the opposite position (tryOpposite).
func KeepInBounds(t image.Rectangle, sz image.Point, b image.Rectangle, pos string, offX, offY, ax, ay int, tryOpposite bool) (image.Point, bool) {
	s := popoverInitial(pos, t, sz, offX, offY)
	if b.Empty() {
		return image.Pt(s.left, s.top), false
	}
	minX, maxX, minY, maxY := b.Min.X+ax, b.Max.X-ax, b.Min.Y+ay, b.Max.Y-ay
	left, top := s.left, s.top
	if s.left < minX {
		left = minX
	} else if s.right > maxX {
		left = max(minX, maxX-sz.X)
	}
	if s.top < minY {
		top = minY
	} else if s.bottom > maxY {
		top = max(minY, maxY-sz.Y)
	}
	if top != s.top && tryOpposite {
		o := popoverInitial(PopoverOpposite[pos], t, sz, offX, -offY)
		if !(o.top < minY || o.bottom > maxY) {
			return image.Pt(left, o.top), PopoverOpposite[pos] != pos
		}
	}
	return image.Pt(left, top), false
}

// ---- the window, for tooltips laid out in local coordinates ----

// WindowSize and WindowPointer are set by the ui root: the window's size
// and the last pointer position in window coordinates.
var (
	WindowSize    func() image.Point
	WindowPointer func() (image.Point, bool)
)

// TipOrigin learns a widget's window origin from pointer events, as ui's
// originTracker does: Gio has no global transform, but a pass-through
// handler sees the same event in local coordinates that the root sees in
// window coordinates, so origin = window − local. A tooltip shows while
// its target is hovered, so the pointer has been there.
type TipOrigin struct {
	tag int
	pt  image.Point
	ok  bool
}

// Track reads last frame's events and registers a pass-through handler
// over r (local coordinates).
func (o *TipOrigin) Track(gtx layout.Context, r image.Rectangle) {
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &o.tag, Kinds: pointer.Move | pointer.Enter | pointer.Press | pointer.Drag})
		if !ok {
			break
		}
		if pe, ok := e.(pointer.Event); ok {
			o.Learn(pe.Position)
		}
	}
	if r.Empty() {
		return
	}
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, &o.tag)
}

// Learn pairs a pointer position in local coordinates with the window
// pointer.
func (o *TipOrigin) Learn(local f32.Point) {
	if WindowPointer == nil {
		return
	}
	if w, ok := WindowPointer(); ok {
		o.pt = w.Sub(image.Pt(int(local.X+.5), int(local.Y+.5)))
		o.ok = true
	}
}

// Set gives the origin when the owner knows it.
func (o *TipOrigin) Set(p image.Point) { o.pt, o.ok = p, true }

// Bounds is the window in local coordinates, or an empty rectangle until
// the origin and the window's size are known (PlaceTooltip doesn't limit
// the tooltip then).
func (o *TipOrigin) Bounds() image.Rectangle {
	if !o.ok {
		return image.Rectangle{}
	}
	return WindowBounds(o.pt)
}

// WindowBounds is the window in the coordinates of a widget whose window
// origin is org; empty when the window's size isn't known.
func WindowBounds(org image.Point) image.Rectangle {
	if WindowSize == nil {
		return image.Rectangle{}
	}
	sz := WindowSize()
	if sz.X <= 0 || sz.Y <= 0 {
		return image.Rectangle{}
	}
	return image.Rectangle{Max: sz}.Sub(org)
}

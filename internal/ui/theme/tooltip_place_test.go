package theme

import (
	"image"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

// PlaceTooltip is BaseTooltip's Popover (F2XN → L1PI keepInBounds,
// allowance 3, tryOppositePosition) around TooltipTip (tgOB: the 8px
// margin, the arrow), worked by hand in a 400×300 window for a 120×32
// tip, at every edge and corner.
func TestPlaceTooltipLikeSlack(t *testing.T) {
	gtx := layout.Context{Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	win := image.Rect(0, 0, 400, 300)
	box := image.Pt(120, 32)
	for _, c := range []struct {
		name   string
		target image.Rectangle
		o      TipOpts
		win    image.Rectangle
		pos    string
		at     image.Point // the tip's top-left
		arrow  image.Point
	}{
		// the content box is the tip plus its 8px margin: 120×40, its
		// bottom on the target's top, centred
		{"top, room", image.Rect(140, 140, 180, 168), BaseTooltip(gtx, TipTop), win, TipTop, image.Pt(100, 100), image.Pt(160, 132)},
		// left edge: shifted to 3px inside, the arrow stays on the target
		{"top, left edge", image.Rect(0, 140, 28, 168), BaseTooltip(gtx, TipTop), win, TipTop, image.Pt(3, 100), image.Pt(14, 132)},
		{"top, right edge", image.Rect(372, 140, 400, 168), BaseTooltip(gtx, TipTop), win, TipTop, image.Pt(277, 100), image.Pt(386, 132)},
		// top edge: the top had to move and "bottom" fits, so it flips:
		// the margin goes on top (margin-top), the arrow on the box's top
		{"top, top edge", image.Rect(140, 10, 180, 38), BaseTooltip(gtx, TipTop), win, TipBottom, image.Pt(100, 46), image.Pt(160, 46)},
		{"top, top-left corner", image.Rect(0, 0, 28, 28), BaseTooltip(gtx, TipTop), win, TipBottom, image.Pt(3, 36), image.Pt(14, 36)},
		{"top, top-right corner", image.Rect(372, 0, 400, 28), BaseTooltip(gtx, TipTop), win, TipBottom, image.Pt(277, 36), image.Pt(386, 36)},
		// bottom edge for "bottom": flips above
		{"bottom, bottom edge", image.Rect(140, 270, 180, 298), BaseTooltip(gtx, TipBottom), win, TipTop, image.Pt(100, 230), image.Pt(160, 262)},
		{"bottom, bottom-left corner", image.Rect(0, 272, 28, 300), BaseTooltip(gtx, TipBottom), win, TipTop, image.Pt(3, 232), image.Pt(14, 264)},
		{"bottom, room", image.Rect(140, 140, 180, 168), BaseTooltip(gtx, TipBottom), win, TipBottom, image.Pt(100, 176), image.Pt(160, 176)},
		// no room either way (a 50px window): shifted, not flipped
		{"top, no room", image.Rect(140, 10, 180, 38), BaseTooltip(gtx, TipTop), image.Rect(0, 0, 400, 50), TipTop, image.Pt(100, 3), image.Pt(160, 35)},
		// left and right never flip: kept inside, over the target
		{"right, right edge", image.Rect(372, 140, 400, 168), BaseTooltip(gtx, TipRight), win, TipRight, image.Pt(277, 138), image.Pt(277, 154)},
		{"left, left edge", image.Rect(0, 140, 28, 168), BaseTooltip(gtx, TipLeft), win, TipLeft, image.Pt(3, 138), image.Pt(123, 154)},
		{"right, room", image.Rect(140, 140, 180, 168), BaseTooltip(gtx, TipRight), win, TipRight, image.Pt(188, 138), image.Pt(188, 154)},
		// bottom-right: right edges aligned, the arrow the target's half
		// width from the box's right (BaseTooltip's tipArrowOffset)
		{"bottom-right", image.Rect(300, 100, 328, 128), BaseTooltip(gtx, TipBottomRight), win, TipBottomRight, image.Pt(208, 136), image.Pt(314, 136)},
		{"bottom-right, bottom edge", image.Rect(300, 270, 328, 298), BaseTooltip(gtx, TipBottomRight), win, TipTopRight, image.Pt(208, 230), image.Pt(314, 262)},
		// offsetY adds to the top (getInitialPosition), negated on a flip
		{"top, offsetY -4", image.Rect(140, 140, 180, 168), TipOpts{Pos: TipTop, OffsetY: -4, AllowanceX: 3, AllowanceY: 3, TryOpposite: true}, win, TipTop, image.Pt(100, 96), image.Pt(160, 128)},
		// Texty's Popover: allowance 0, no opposite
		{"texty, left edge", image.Rect(0, 140, 28, 168), TextyTip(TipTop), win, TipTop, image.Pt(0, 100), image.Pt(14, 132)},
		{"texty, top edge", image.Rect(140, 10, 180, 38), TextyTip(TipTop), win, TipTop, image.Pt(100, 0), image.Pt(160, 32)},
		// an unknown window doesn't limit it
		{"top, no bounds", image.Rect(0, 10, 28, 38), BaseTooltip(gtx, TipTop), image.Rectangle{}, TipTop, image.Pt(-46, -30), image.Pt(14, 2)},
	} {
		pl := PlaceTooltip(gtx, c.target, box, c.win, c.o)
		if pl.Pos != c.pos || pl.Box.Min != c.at || pl.Box.Size() != box || pl.Arrow.C != c.arrow || pl.Arrow.Half != 6 {
			t.Errorf("%s: %s at %v (%v), arrow %+v; want %s at %v, arrow at %v", c.name, pl.Pos, pl.Box.Min, pl.Box.Size(), pl.Arrow, c.pos, c.at, c.arrow)
		}
		if !c.win.Empty() {
			in := c.win.Inset(0)
			if c.o.AllowanceX > 0 {
				in = image.Rect(c.win.Min.X+3, c.win.Min.Y+3, c.win.Max.X-3, c.win.Max.Y-3)
			}
			if !pl.Box.In(in) {
				t.Errorf("%s: %v isn't inside %v", c.name, pl.Box, in)
			}
		}
	}
}

// TipOrigin learns a widget's window origin from a pointer event its
// pass-through handler sees in local coordinates, so a widget laid out
// 300,200 into the window knows where the window's edges are.
func TestTipOriginFromPointer(t *testing.T) {
	defer func(s func() image.Point, p func() (image.Point, bool)) { WindowSize, WindowPointer = s, p }(WindowSize, WindowPointer)
	WindowSize = func() image.Point { return image.Pt(400, 300) }
	ptr := image.Pt(310, 210)
	WindowPointer = func() (image.Point, bool) { return ptr, true }
	var r input.Router
	var o TipOrigin
	ops := new(op.Ops)
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
		off := op.Offset(image.Pt(300, 200)).Push(ops)
		o.Track(gtx, image.Rect(0, 0, 28, 28))
		off.Pop()
		r.Frame(ops)
	}
	frame()
	if !o.Bounds().Empty() {
		t.Fatalf("bounds %v before the pointer came", o.Bounds())
	}
	r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(ptr.X), float32(ptr.Y))})
	frame()
	if got, want := o.Bounds(), image.Rect(-300, -200, 100, 100); got != want {
		t.Errorf("bounds %v, want the window %v in the widget's coordinates", got, want)
	}
}

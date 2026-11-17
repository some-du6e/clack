// SPDX-License-Identifier: Unlicense OR MIT

package clip

import (
	"image"
	"math"
	"testing"

	"gioui.org/f32"
	"gioui.org/internal/ops"
	"gioui.org/op"
)

// Clack patch 42: a stroke of a rounded rectangle covers no pixel in its
// hole. Every pixel whose square comes within w/2 of the outline lies
// outside it, for the strokes Clack draws (the workspace shadow's 1.5 px
// rings, 1-4 px borders) and for widths whose half isn't a whole or a half
// pixel, corners of any radius and none.
func TestRRectStrokeHole(t *testing.T) {
	for _, c := range []struct {
		rr   RRect
		w    float32
		want bool // a hole
	}{
		{UniformRRect(image.Rect(62, 32, 1272, 792), 8), 1.5, true},         // the workspace shadow's first ring, 1x
		{UniformRRect(image.Rect(124, 64, 2560, 1600), 32), 1.5, true},      // its 16th at 2x
		{UniformRRect(image.Rect(330, 40, 1916, 1078), 6), 1, true},         // the dark workspace outline
		{RRect{Rect: image.Rect(70, 40, 1276, 796), NE: 6, SE: 6}, 2, true}, // square left corners
		{UniformRRect(image.Rect(10, 10, 600, 400), 0), 4, true},
		{UniformRRect(image.Rect(10, 10, 600, 400), 30), 0.5, true},
		{UniformRRect(image.Rect(10, 10, 600, 400), 30), 2.5, true},
		{RRect{Rect: image.Rect(0, 0, 500, 300), NW: 40, NE: 2, SE: 18, SW: 0}, 3, true},
		{UniformRRect(image.Rect(0, 0, 400, 40), 20), 1.5, false}, // a pill: all corners
		{UniformRRect(image.Rect(0, 0, 4, 4), 1), 1, false},
		{UniformRRect(image.Rect(0, 0, 300, 300), 8), 0, false},
	} {
		h := c.rr.StrokeHole(c.w)
		if h.Empty() != !c.want {
			t.Errorf("%+v w=%g: hole %v, want one: %v", c.rr, c.w, h, c.want)
			continue
		}
		if h.Empty() {
			continue
		}
		if !h.In(c.rr.Rect) {
			t.Errorf("%+v w=%g: hole %v outside the rectangle", c.rr, c.w, h)
		}
		// the nearest point of a pixel to the outline is at most half its
		// diagonal nearer than its centre
		reach := float64(c.w)/2 + math.Sqrt2/2
		for y := h.Min.Y; y < h.Max.Y; y++ {
			for x := h.Min.X; x < h.Max.X; x++ {
				if d := math.Abs(rrDistance(c.rr, float64(x)+.5, float64(y)+.5)); d <= reach {
					t.Fatalf("%+v w=%g: pixel (%d,%d) in the hole %v is %.2f px from the outline", c.rr, c.w, x, y, h, d)
				}
			}
		}
	}

	// only a stroke of a rounded rectangle says where its hole is
	holes := func(build func(o *op.Ops)) []image.Rectangle {
		var o op.Ops
		build(&o)
		var rd ops.Reader
		rd.Reset(&o.Internal)
		var out []image.Rectangle
		for e, ok := rd.Decode(); ok; e, ok = rd.Decode() {
			if ops.OpType(e.Data[0]) == ops.TypeClipHole {
				out = append(out, ops.DecodeClipHole(e.Data))
			}
		}
		return out
	}
	rr := UniformRRect(image.Rect(0, 0, 800, 600), 8)
	if got := holes(func(o *op.Ops) { Stroke{Path: rr.Path(o), Width: 1.5}.Op().Push(o).Pop() }); len(got) != 1 || got[0] != rr.StrokeHole(1.5) {
		t.Errorf("a stroke of an RRect wrote holes %v, want [%v]", got, rr.StrokeHole(1.5))
	}
	if got := holes(func(o *op.Ops) { rr.Push(o).Pop() }); len(got) != 0 {
		t.Errorf("an RRect fill wrote holes %v", got)
	}
	if got := holes(func(o *op.Ops) {
		Stroke{Path: Ellipse(image.Rect(0, 0, 800, 600)).Path(o), Width: 2}.Op().Push(o).Pop()
	}); len(got) != 0 {
		t.Errorf("a stroked ellipse wrote holes %v", got)
	}
	if got := holes(func(o *op.Ops) {
		var p Path
		p.Begin(o)
		p.MoveTo(f32.Pt(0, 0))
		p.LineTo(f32.Pt(800, 0))
		p.LineTo(f32.Pt(800, 600))
		p.Close()
		Stroke{Path: p.End(), Width: 2}.Op().Push(o).Pop()
	}); len(got) != 0 {
		t.Errorf("a stroked path wrote holes %v", got)
	}
}

// rrDistance is the distance from (x, y) to the outline of rr, negative
// inside: the distance to the inset rectangle less the corner's radius,
// the corner being the one of the quarter (x, y) is in.
func rrDistance(rr RRect, x, y float64) float64 {
	r := rr.Rect
	cx, cy := float64(r.Min.X+r.Max.X)/2, float64(r.Min.Y+r.Max.Y)/2
	var rad int
	switch {
	case x < cx && y < cy:
		rad = rr.NW
	case x >= cx && y < cy:
		rad = rr.NE
	case x >= cx:
		rad = rr.SE
	default:
		rad = rr.SW
	}
	k := float64(rad)
	hx, hy := float64(r.Dx())/2-k, float64(r.Dy())/2-k
	qx, qy := math.Abs(x-cx)-hx, math.Abs(y-cy)-hy
	return math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - k
}

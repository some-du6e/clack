// SPDX-License-Identifier: Unlicense OR MIT

package clip

import (
	"image"
	"math"

	"gioui.org/f32"
	f32internal "gioui.org/internal/f32"
	"gioui.org/internal/ops"
	"gioui.org/op"
)

// Rect represents the clip area of a pixel-aligned rectangle.
type Rect image.Rectangle

// Op returns the op for the rectangle.
func (r Rect) Op() Op {
	return Op{
		outline: true,
		path:    r.Path(),
	}
}

// Push the clip operation on the clip stack.
func (r Rect) Push(ops *op.Ops) Stack {
	return r.Op().Push(ops)
}

// Path returns the PathSpec for the rectangle.
func (r Rect) Path() PathSpec {
	return PathSpec{
		shape:  ops.Rect,
		bounds: image.Rectangle(r),
	}
}

// UniformRRect returns an RRect with all corner radii set to the
// provided radius.
func UniformRRect(rect image.Rectangle, radius int) RRect {
	return RRect{
		Rect: rect,
		SE:   radius,
		SW:   radius,
		NE:   radius,
		NW:   radius,
	}
}

// RRect represents the clip area of a rectangle with rounded
// corners.
//
// Specify a square with corner radii equal to half the square size to
// construct a circular clip area.
type RRect struct {
	Rect image.Rectangle
	// The corner radii.
	SE, SW, NW, NE int
}

// Op returns the op for the rounded rectangle.
func (rr RRect) Op(ops *op.Ops) Op {
	if rr.SE == 0 && rr.SW == 0 && rr.NW == 0 && rr.NE == 0 {
		return Rect(rr.Rect).Op()
	}
	return Outline{Path: rr.Path(ops)}.Op()
}

// Push the rectangle clip on the clip stack.
func (rr RRect) Push(ops *op.Ops) Stack {
	return rr.Op(ops).Push(ops)
}

// Path returns the PathSpec for the rounded rectangle.
func (rr RRect) Path(ops *op.Ops) PathSpec {
	var p Path
	p.Begin(ops)

	// https://pomax.github.io/bezierinfo/#circles_cubic.
	const q = 4 * (math.Sqrt2 - 1) / 3
	const iq = 1 - q

	se, sw, nw, ne := float32(rr.SE), float32(rr.SW), float32(rr.NW), float32(rr.NE)
	rrf := f32internal.FRect(rr.Rect)
	w, n, e, s := rrf.Min.X, rrf.Min.Y, rrf.Max.X, rrf.Max.Y

	p.MoveTo(f32.Point{X: w + nw, Y: n})
	p.LineTo(f32.Point{X: e - ne, Y: n}) // N
	p.CubeTo(                            // NE
		f32.Point{X: e - ne*iq, Y: n},
		f32.Point{X: e, Y: n + ne*iq},
		f32.Point{X: e, Y: n + ne})
	p.LineTo(f32.Point{X: e, Y: s - se}) // E
	p.CubeTo(                            // SE
		f32.Point{X: e, Y: s - se*iq},
		f32.Point{X: e - se*iq, Y: s},
		f32.Point{X: e - se, Y: s})
	p.LineTo(f32.Point{X: w + sw, Y: s}) // S
	p.CubeTo(                            // SW
		f32.Point{X: w + sw*iq, Y: s},
		f32.Point{X: w, Y: s - sw*iq},
		f32.Point{X: w, Y: s - sw})
	p.LineTo(f32.Point{X: w, Y: n + nw}) // W
	p.CubeTo(                            // NW
		f32.Point{X: w, Y: n + nw*iq},
		f32.Point{X: w + nw*iq, Y: n},
		f32.Point{X: w + nw, Y: n})

	spec := p.End()
	spec.interior = rr.Interior()
	spec.rrect, spec.hasRRect = rr, true
	return spec
}

// StrokeHole is where a stroke of width w along the rounded rectangle
// covers no pixel as Gio's renderer computes coverage (Clack patch 42):
// the rows between the top corners' arcs and the bottom ones', a pixel
// past them and past the top and bottom edges' strokes, and the columns
// more than w/2 and a pixel in from the straight sides. A pixel's coverage
// adds up the curves above it in its column; in the hole the top edges'
// cancel. Empty if the rectangle has no such hole.
func (rr RRect) StrokeHole(w float32) image.Rectangle {
	if !(w > 0) || rr.NW < 0 || rr.NE < 0 || rr.SE < 0 || rr.SW < 0 {
		return image.Rectangle{}
	}
	m := int(math.Ceil(float64(w) / 2))
	r := rr.Rect
	h := image.Rectangle{
		Min: image.Pt(r.Min.X+m+1, r.Min.Y+max(rr.NW, rr.NE, m)+1),
		Max: image.Pt(r.Max.X-m-1, r.Max.Y-max(rr.SW, rr.SE, m)-1),
	}
	if h.Empty() {
		return image.Rectangle{}
	}
	return h
}

// Interior is where the rounded rectangle, as a clip, covers every pixel
// fully as Gio's renderer computes coverage (Clack patch 38): the columns
// between the corners' arcs, a pixel in from every edge. The stencil adds
// up the curves above a pixel; under the arcs that is a sum of slivers
// rounded to half floats, so those columns are left out, and a pixel next
// to an edge can come out a rounding short of full. The renderer draws
// what lies inside it as if the clip weren't there.
func (rr RRect) Interior() image.Rectangle {
	r := rr.Rect
	// not image.Rect: that swaps the corners of an inverted rectangle,
	// where the arcs overlap (a circle has no interior)
	in := image.Rectangle{
		Min: image.Pt(r.Min.X+max(rr.NW, rr.SW)+1, r.Min.Y+1),
		Max: image.Pt(r.Max.X-max(rr.NE, rr.SE)-1, r.Max.Y-1),
	}
	if in.Empty() || rr.NW < 0 || rr.SW < 0 || rr.NE < 0 || rr.SE < 0 {
		return image.Rectangle{}
	}
	return in
}

// Ellipse represents the largest axis-aligned ellipse that
// is contained in its bounds.
type Ellipse image.Rectangle

// Op returns the op for the filled ellipse.
func (e Ellipse) Op(ops *op.Ops) Op {
	return Outline{Path: e.Path(ops)}.Op()
}

// Push the filled ellipse clip op on the clip stack.
func (e Ellipse) Push(ops *op.Ops) Stack {
	return e.Op(ops).Push(ops)
}

// Path constructs a path for the ellipse.
func (e Ellipse) Path(o *op.Ops) PathSpec {
	bounds := image.Rectangle(e)
	if bounds.Dx() == 0 || bounds.Dy() == 0 {
		return PathSpec{shape: ops.Rect}
	}

	var p Path
	p.Begin(o)

	bf := f32internal.FRect(bounds)
	center := bf.Max.Add(bf.Min).Mul(.5)
	diam := bf.Dx()
	r := diam * .5
	// We'll model the ellipse as a circle scaled in the Y
	// direction.
	scale := bf.Dy() / diam

	// https://pomax.github.io/bezierinfo/#circles_cubic.
	const q = 4 * (math.Sqrt2 - 1) / 3

	curve := r * q
	top := f32.Point{X: center.X, Y: center.Y - r*scale}

	p.MoveTo(top)
	p.CubeTo(
		f32.Point{X: center.X + curve, Y: center.Y - r*scale},
		f32.Point{X: center.X + r, Y: center.Y - curve*scale},
		f32.Point{X: center.X + r, Y: center.Y},
	)
	p.CubeTo(
		f32.Point{X: center.X + r, Y: center.Y + curve*scale},
		f32.Point{X: center.X + curve, Y: center.Y + r*scale},
		f32.Point{X: center.X, Y: center.Y + r*scale},
	)
	p.CubeTo(
		f32.Point{X: center.X - curve, Y: center.Y + r*scale},
		f32.Point{X: center.X - r, Y: center.Y + curve*scale},
		f32.Point{X: center.X - r, Y: center.Y},
	)
	p.CubeTo(
		f32.Point{X: center.X - r, Y: center.Y - curve*scale},
		f32.Point{X: center.X - curve, Y: center.Y - r*scale},
		top,
	)
	ellipse := p.End()
	ellipse.shape = ops.Ellipse
	return ellipse
}

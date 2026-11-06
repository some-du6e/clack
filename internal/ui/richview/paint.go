package richview

import (
	"image"
	"image/color"
	"math"

	"gioui.org/f32"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
)

func dpUnit(v float32) unit.Dp { return unit.Dp(v) }

// roundRect builds a rounded rectangle path with float coordinates and
// per-corner radii (NW, NE, SE, SW).
func roundRect(ops *op.Ops, r f32Rect, rad [4]float32) clip.PathSpec {
	var p clip.Path
	p.Begin(ops)
	const k = 0.5522847 // cubic approximation of a quarter circle
	nw, ne, se, sw := rad[0], rad[1], rad[2], rad[3]
	p.MoveTo(f32.Pt(r.x0+nw, r.y0))
	p.LineTo(f32.Pt(r.x1-ne, r.y0))
	if ne > 0 {
		p.CubeTo(f32.Pt(r.x1-ne+ne*k, r.y0), f32.Pt(r.x1, r.y0+ne-ne*k), f32.Pt(r.x1, r.y0+ne))
	}
	p.LineTo(f32.Pt(r.x1, r.y1-se))
	if se > 0 {
		p.CubeTo(f32.Pt(r.x1, r.y1-se+se*k), f32.Pt(r.x1-se+se*k, r.y1), f32.Pt(r.x1-se, r.y1))
	}
	p.LineTo(f32.Pt(r.x0+sw, r.y1))
	if sw > 0 {
		p.CubeTo(f32.Pt(r.x0+sw-sw*k, r.y1), f32.Pt(r.x0, r.y1-sw+sw*k), f32.Pt(r.x0, r.y1-sw))
	}
	p.LineTo(f32.Pt(r.x0, r.y0+nw))
	if nw > 0 {
		p.CubeTo(f32.Pt(r.x0, r.y0+nw-nw*k), f32.Pt(r.x0+nw-nw*k, r.y0), f32.Pt(r.x0+nw, r.y0))
	}
	p.Close()
	return p.End()
}

type f32Rect struct{ x0, y0, x1, y1 float32 }

func fr(r image.Rectangle) f32Rect {
	return f32Rect{float32(r.Min.X), float32(r.Min.Y), float32(r.Max.X), float32(r.Max.Y)}
}

func uni(v int) [4]float32 { f := float32(v); return [4]float32{f, f, f, f} }

// strokeRRect draws a crisp border of width bw inside r.
func strokeRRect(ops *op.Ops, r image.Rectangle, radius, bw int, col color.NRGBA) {
	strokeRadii(ops, r, uni(radius), bw, col)
}

func strokeRadii(ops *op.Ops, r image.Rectangle, rad [4]float32, bw int, col color.NRGBA) {
	if bw <= 0 || col.A == 0 || r.Empty() {
		return
	}
	h := float32(bw) / 2
	rr := fr(r)
	rr.x0 += h
	rr.y0 += h
	rr.x1 -= h
	rr.y1 -= h
	for i := range rad {
		rad[i] = float32(math.Max(0, float64(rad[i]-h)))
	}
	path := roundRect(ops, rr, rad)
	paint.FillShape(ops, col, clip.Stroke{Path: path, Width: float32(bw)}.Op())
}

func fillRadii(ops *op.Ops, r image.Rectangle, radii [4]int, col color.NRGBA) {
	if col.A == 0 || r.Empty() {
		return
	}
	paint.FillShape(ops, col, clip.RRect{Rect: r, NW: radii[0], NE: radii[1], SE: radii[2], SW: radii[3]}.Op(ops))
}

func drawBox(ops *op.Ops, b *box) {
	if b.r.Empty() {
		return
	}
	switch b.kind {
	case bFill:
		if b.perCorner {
			fillRadii(ops, b.r, b.corners, b.fill)
		} else {
			fillRadii(ops, b.r, [4]int{b.radius, b.radius, b.radius, b.radius}, b.fill)
		}
	case bBorder:
		fillRadii(ops, b.r, [4]int{b.radius, b.radius, b.radius, b.radius}, b.fill)
		strokeRRect(ops, b.r, b.radius, b.bw, b.stroke)
	case bCircle:
		paint.FillShape(ops, b.fill, clip.Ellipse(b.r).Op(ops))
	case bRing:
		h := float32(max(1, b.bw)) / 2
		rr := fr(b.r)
		rad := (rr.x1 - rr.x0) / 2
		rr.x0 += h
		rr.y0 += h
		rr.x1 -= h
		rr.y1 -= h
		path := roundRect(ops, rr, [4]float32{rad - h, rad - h, rad - h, rad - h})
		paint.FillShape(ops, b.fill, clip.Stroke{Path: path, Width: float32(max(1, b.bw))}.Op())
	case bSquare:
		// Browser square bullets are slightly smaller than discs.
		in := b.r.Dx() / 8
		paint.FillShape(ops, b.fill, clip.Rect(b.r.Inset(in)).Op())
	case bPlay:
		var p clip.Path
		p.Begin(ops)
		r := fr(b.r)
		p.MoveTo(f32.Pt(r.x0, r.y0))
		p.LineTo(f32.Pt(r.x1, (r.y0+r.y1)/2))
		p.LineTo(f32.Pt(r.x0, r.y1))
		p.Close()
		paint.FillShape(ops, b.fill, clip.Outline{Path: p.End()}.Op())
	case bChevron:
		var p clip.Path
		p.Begin(ops)
		r := fr(b.r)
		p.MoveTo(f32.Pt(r.x0, r.y0))
		p.LineTo(f32.Pt(r.x1, (r.y0+r.y1)/2))
		p.LineTo(f32.Pt(r.x0, r.y1))
		paint.FillShape(ops, b.fill, clip.Stroke{Path: p.End(), Width: float32(max(1, b.bw))}.Op())
	}
}

// drawImage paints img into r. cover crops to fill; otherwise it fits
// (contain) and centers.
func drawImage(ops *op.Ops, img paint.ImageOp, r image.Rectangle, radii [4]int, cover bool) {
	sz := img.Size()
	if sz.X <= 0 || sz.Y <= 0 || r.Empty() {
		return
	}
	sx := float32(r.Dx()) / float32(sz.X)
	sy := float32(r.Dy()) / float32(sz.Y)
	s := sx
	if cover {
		if sy > s {
			s = sy
		}
	} else if sy < s {
		s = sy
	}
	w, h := float32(sz.X)*s, float32(sz.Y)*s
	off := f32.Pt(float32(r.Min.X)+(float32(r.Dx())-w)/2, float32(r.Min.Y)+(float32(r.Dy())-h)/2)
	cs := clip.RRect{Rect: r, NW: radii[0], NE: radii[1], SE: radii[2], SW: radii[3]}.Push(ops)
	t := op.Affine(f32.AffineId().Scale(f32.Point{}, f32.Pt(s, s)).Offset(off)).Push(ops)
	img.Add(ops)
	paint.PaintOp{}.Add(ops)
	t.Pop()
	cs.Pop()
}

// DrawImage is exported for the messages package (avatars, reaction emoji):
// it paints img covering r with uniform corner radius.
func DrawImage(ops *op.Ops, img paint.ImageOp, r image.Rectangle, radius int, cover bool) {
	drawImage(ops, img, r, [4]int{radius, radius, radius, radius}, cover)
}

// StrokeRRect is exported for the messages package: a crisp inner border.
func StrokeRRect(ops *op.Ops, r image.Rectangle, radius, bw int, col color.NRGBA) {
	strokeRRect(ops, r, radius, bw, col)
}

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
	rad, _ = cssRadii(r, rad)
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

// strokeSliced draws the border of one line's piece of an inline box that
// wraps (inline code): no side where the box goes on (cut), as Chromium
// paints box-decoration-break: slice. The ring is the box less its padding
// box, which reaches the box's edge on a cut side.
func strokeSliced(ops *op.Ops, r image.Rectangle, radii [4]int, bw int, col color.NRGBA, cut uint8) {
	if bw <= 0 || col.A == 0 || r.Empty() {
		return
	}
	or := fr(r)
	orad := [4]float32{float32(radii[0]), float32(radii[1]), float32(radii[2]), float32(radii[3])}
	orad, _ = cssRadii(r, orad)
	w := float32(bw)
	ir := f32Rect{or.x0 + w, or.y0 + w, or.x1 - w, or.y1 - w}
	if cut&cutStart != 0 {
		ir.x0 = or.x0
	}
	if cut&cutEnd != 0 {
		ir.x1 = or.x1
	}
	var irad [4]float32
	for i := range irad {
		irad[i] = float32(math.Max(0, float64(orad[i]-w)))
	}
	var p clip.Path
	p.Begin(ops)
	addRoundRect(&p, or, orad, false)
	addRoundRect(&p, ir, irad, true)
	paint.FillShape(ops, col, clip.Outline{Path: p.End()}.Op())
}

// addRoundRect adds a closed rounded rectangle to p, clockwise or (ccw)
// counter-clockwise, so a reversed one cuts a hole (non-zero winding).
func addRoundRect(p *clip.Path, r f32Rect, rad [4]float32, ccw bool) {
	const k = 0.5522847
	nw, ne, se, sw := rad[0], rad[1], rad[2], rad[3]
	if !ccw {
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
		return
	}
	p.MoveTo(f32.Pt(r.x0+nw, r.y0))
	if nw > 0 {
		p.CubeTo(f32.Pt(r.x0+nw-nw*k, r.y0), f32.Pt(r.x0, r.y0+nw-nw*k), f32.Pt(r.x0, r.y0+nw))
	}
	p.LineTo(f32.Pt(r.x0, r.y1-sw))
	if sw > 0 {
		p.CubeTo(f32.Pt(r.x0, r.y1-sw+sw*k), f32.Pt(r.x0+sw-sw*k, r.y1), f32.Pt(r.x0+sw, r.y1))
	}
	p.LineTo(f32.Pt(r.x1-se, r.y1))
	if se > 0 {
		p.CubeTo(f32.Pt(r.x1-se+se*k, r.y1), f32.Pt(r.x1, r.y1-se+se*k), f32.Pt(r.x1, r.y1-se))
	}
	p.LineTo(f32.Pt(r.x1, r.y0+ne))
	if ne > 0 {
		p.CubeTo(f32.Pt(r.x1, r.y0+ne-ne*k), f32.Pt(r.x1-ne+ne*k, r.y0), f32.Pt(r.x1-ne, r.y0))
	}
	p.Close()
}

// cssRadii scales corner radii (NW, NE, SE, SW) the way a browser does
// when adjacent corners don't fit along a side (CSS Backgrounds 3,
// "Corner Curve Overlap"): every radius is multiplied by the smallest
// side length / sum of that side's two radii, when that is below 1.
// Slack's attachment bar (.c-message_attachment__border, width 4px,
// border-radius 8px) is a 4px stadium with 2px round ends that way;
// clip.RRect doesn't clamp, so radius 8 on a 4px box drew a path that
// ran back past its own start and crossed itself (horns at both ends).
func cssRadii(r image.Rectangle, rad [4]float32) ([4]float32, bool) {
	f := float32(1)
	side := func(l int, a, b float32) {
		if s := a + b; s > 0 && float32(l)/s < f {
			f = float32(l) / s
		}
	}
	w, h := r.Dx(), r.Dy()
	side(w, rad[0], rad[1]) // top
	side(w, rad[3], rad[2]) // bottom
	side(h, rad[0], rad[3]) // left
	side(h, rad[1], rad[2]) // right
	if f >= 1 {
		return rad, false
	}
	for i := range rad {
		rad[i] *= f
	}
	return rad, true
}

func fillRadii(ops *op.Ops, r image.Rectangle, radii [4]int, col color.NRGBA) {
	if col.A == 0 || r.Empty() {
		return
	}
	rad := [4]float32{float32(radii[0]), float32(radii[1]), float32(radii[2]), float32(radii[3])}
	if rad, scaled := cssRadii(r, rad); scaled {
		// fractional after scaling (a 5px bar's 2.5px ends), so the
		// float path rather than clip.RRect's int radii
		paint.FillShape(ops, col, clip.Outline{Path: roundRect(ops, fr(r), rad)}.Op())
		return
	}
	paint.FillShape(ops, col, clip.RRect{Rect: r, NW: radii[0], NE: radii[1], SE: radii[2], SW: radii[3]}.Op(ops))
}

// radii are b's corner radii (NW NE SE SW).
func (b *box) radii() [4]int {
	if b.perCorner {
		return b.corners
	}
	return [4]int{b.radius, b.radius, b.radius, b.radius}
}

func drawBox(ops *op.Ops, b *box) {
	if b.r.Empty() {
		return
	}
	switch b.kind {
	case bFill:
		fillRadii(ops, b.r, b.radii(), b.fill)
	case bBorder:
		fillRadii(ops, b.r, b.radii(), b.fill)
		if b.cut != 0 {
			strokeSliced(ops, b.r, b.radii(), b.bw, b.stroke, b.cut)
		} else {
			strokeRRect(ops, b.r, b.radius, b.bw, b.stroke)
		}
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
	case bCaret:
		var p clip.Path
		p.Begin(ops)
		r := fr(b.r)
		p.MoveTo(f32.Pt(r.x0, r.y0))
		p.LineTo(f32.Pt(r.x1, r.y0))
		p.LineTo(f32.Pt((r.x0+r.x1)/2, r.y1))
		p.Close()
		paint.FillShape(ops, b.fill, clip.Outline{Path: p.End()}.Op())
	case bCaretRight:
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
	case bFade:
		// linear-gradient(to top, fill, transparent): fill at the bottom
		clear := b.fill
		clear.A = 0
		cs := clip.RRect{Rect: b.r, NW: b.corners[0], NE: b.corners[1], SE: b.corners[2], SW: b.corners[3]}.Push(ops)
		paint.LinearGradientOp{Stop1: f32.Pt(0, float32(b.r.Min.Y)), Color1: clear, Stop2: f32.Pt(0, float32(b.r.Max.Y)), Color2: b.fill}.Add(ops)
		paint.PaintOp{}.Add(ops)
		cs.Pop()
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
	if cover && s >= 1 && s < 1.02 {
		// a copy decoded to cover the box (coverPx): drawn at its own
		// pixels, the overhang cropped, not stretched by the rounding
		s = 1
	}
	w, h := float32(sz.X)*s, float32(sz.Y)*s
	// on whole device pixels: a half-pixel offset resamples every pixel
	off := f32.Pt(float32(math.Round(float64(float32(r.Min.X)+(float32(r.Dx())-w)/2))),
		float32(math.Round(float64(float32(r.Min.Y)+(float32(r.Dy())-h)/2))))
	cs := clip.RRect{Rect: r, NW: radii[0], NE: radii[1], SE: radii[2], SW: radii[3]}.Push(ops)
	t := op.Affine(f32.AffineId().Scale(f32.Point{}, f32.Pt(s, s)).Offset(off)).Push(ops)
	img.Add(ops)
	paint.PaintOp{}.Add(ops)
	t.Pop()
	cs.Pop()
}

// coverPx is the decode size (the larger side, as imgcache scales) whose
// copy covers box: got is the copy decoded at px. When got's larger side
// is px (the source was scaled down) and its smaller side falls short of
// the box, the size that scales its smaller side to the box's; else px
// (the source is no larger, or it already covers).
func coverPx(got, box image.Point, px int) int {
	if got.X <= 0 || got.Y <= 0 || box.X <= 0 || box.Y <= 0 || max(got.X, got.Y) < px {
		return px
	}
	s := max(float64(box.X)/float64(got.X), float64(box.Y)/float64(got.Y))
	if s <= 1 {
		return px
	}
	return int(math.Ceil(float64(px) * s))
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

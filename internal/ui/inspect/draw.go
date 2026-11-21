package inspect

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// The pen (Karim, 2026-11-21: "also add an drawing thing to the dev
// tools, optional tho"). In the review, P turns it on: a drag draws a
// freehand line over the shot, one with Shift held at the press a
// rectangle, and with A on an arrow, in red, yellow or blue (1, 2, 3), 3
// dp wide. Z, Mod+Z, Backspace or Delete takes the last line back, and
// Esc or P goes back to blurring with the lines kept. Enter copies the
// image with them burned in over everything else (Compose: after the
// blur and the outline), and nothing of them goes in the report. P in a
// session, frozen or not, grabs the whole window into a review that
// starts with the pen on.
//
// The lines are kept in the shot's px, as the review's rects are, so
// they land on the pixels they were drawn over whatever the window does
// meanwhile. The review draws them through Gio over the shot; the copy
// rasterizes them on the CPU (burnStrokes).

// PenKey turns the pen on and off in a review, and in a session grabs
// the whole window to draw on.
const PenKey key.Name = "P"

// penWidth is a line's width, dp: 3 px at 1x, as wide in dp at any scale.
const penWidth = 3

// penCols are the pen's colours, keys 1 to 3: red (the first), yellow,
// blue.
var penCols = [...]color.NRGBA{
	{R: 0xff, G: 0x2d, B: 0x2d, A: 0xff},
	{R: 0xff, G: 0xcc, B: 0x00, A: 0xff},
	{R: 0x1e, G: 0x7b, B: 0xff, A: 0xff},
}

// Stroke is a line drawn on the shot in the review: polylines in the
// shot's px (a freehand line is one, a rectangle one closed one, an
// arrow its shaft and its head), W px wide.
type Stroke struct {
	Col   color.NRGBA
	W     float32
	Lines [][]f32.Point
}

type penShape uint8

const (
	penFree penShape = iota
	penRect
	penArrow
)

// pen is the review's drawing state.
type pen struct {
	on      bool
	col     int  // penCols
	arrow   bool // a drag draws an arrow (A)
	strokes []Stroke

	// the line being drawn: its shape, its points (shot px; a rectangle's
	// or an arrow's two ends), the last pointer position taken (window px)
	drawing bool
	shape   penShape
	pts     []f32.Point
	last    f32.Point
}

// penPx is a line's width in r's shot (px): penWidth dp at the shot's
// px per dp.
func (r *review) penPx() float32 {
	return penWidth * r.c.scale * r.c.k
}

// toShotF maps p (window px) into the shot (px, fractional), by last
// frame's view.
func (r *review) toShotF(p f32.Point) f32.Point {
	v, s := r.view, r.shot.Bounds()
	if v.Empty() {
		return f32.Point{}
	}
	return f32.Pt(
		float32(s.Min.X)+(p.X-float32(v.Min.X))*float32(s.Dx())/float32(v.Dx()),
		float32(s.Min.Y)+(p.Y-float32(v.Min.Y))*float32(s.Dy())/float32(v.Dy()))
}

// penKey handles a key press in the review with the pen on. It reports
// whether it took the key (Enter and the shortcut go to reviewKey).
func (t *Tool) penKey(ke key.Event) bool {
	r := t.rev
	p := &r.pen
	mod := ke.Modifiers.Contain(key.ModShortcut)
	switch {
	case ke.Name == key.NameEscape || ke.Name == PenKey:
		if p.drawing && ke.Name == key.NameEscape {
			p.drawing = false // only the line being drawn
			return true
		}
		p.drawing, p.on = false, false // back to blurring, the lines kept
	case ke.Name == "Z" || ke.Name == key.NameDeleteBackward || ke.Name == key.NameDeleteForward:
		if p.drawing {
			p.drawing = false
		} else if n := len(p.strokes); n > 0 {
			p.strokes = p.strokes[:n-1]
		}
	case ke.Name == "A" && !mod:
		p.arrow = !p.arrow
	case (ke.Name == "1" || ke.Name == "2" || ke.Name == "3") && !mod:
		p.col = int(ke.Name[0] - '1')
	default:
		return false
	}
	return true
}

// penPointer handles the review's pointer events with the pen on: a
// primary drag that starts over the shot draws a line.
func (t *Tool) penPointer(pe pointer.Event) {
	r := t.rev
	p := &r.pen
	switch pe.Kind {
	case pointer.Press:
		if pe.Buttons&pointer.ButtonPrimary == 0 || p.drawing || r.view.Empty() || !pe.Position.Round().In(r.view) {
			return
		}
		p.drawing, p.last = true, pe.Position
		p.shape = penFree
		switch {
		case pe.Modifiers.Contain(key.ModShift):
			p.shape = penRect
		case p.arrow:
			p.shape = penArrow
		}
		at := r.toShotF(pe.Position)
		p.pts = append(p.pts[:0], at, at)
	case pointer.Drag:
		if p.drawing {
			p.move(r, pe.Position, t.scale)
		}
	case pointer.Release:
		if p.drawing && pe.Buttons&pointer.ButtonPrimary == 0 {
			p.move(r, pe.Position, t.scale)
			p.end(r, t.scale)
		}
	case pointer.Cancel:
		p.drawing = false
	}
}

// move takes the pointer at q (window px): a freehand line gets a point
// once the pointer has moved a px, a rectangle or an arrow its other end.
func (p *pen) move(r *review, q f32.Point, scale float32) {
	if p.shape != penFree {
		p.pts[len(p.pts)-1] = r.toShotF(q)
		return
	}
	if dist32(q, p.last) < max(scale/2, 1) {
		return
	}
	p.last = q
	if len(p.pts) == 2 && p.pts[0] == p.pts[1] {
		p.pts[1] = r.toShotF(q) // the press's second point, until it moves
		return
	}
	p.pts = append(p.pts, r.toShotF(q))
}

// end ends the line being drawn: it's kept when it went somewhere (a
// freehand line or an arrow minMark dp long, a rectangle minMark dp each
// way); less was a click.
func (p *pen) end(r *review, scale float32) {
	if !p.drawing {
		return
	}
	p.drawing = false
	if s, ok := p.stroke(r, scale); ok {
		p.strokes = append(p.strokes, s)
	}
}

// stroke is the line being drawn as it would be kept, and whether it
// would be.
func (p *pen) stroke(r *review, scale float32) (Stroke, bool) {
	s := Stroke{Col: penCols[p.col], W: r.penPx()}
	if len(p.pts) < 2 {
		return s, false
	}
	least := float32(minMark) * scale * r.c.k // shot px
	a, b := p.pts[0], p.pts[len(p.pts)-1]
	switch p.shape {
	case penRect:
		if abs32(b.X-a.X) < least || abs32(b.Y-a.Y) < least {
			return s, false
		}
		s.Lines = [][]f32.Point{{a, f32.Pt(b.X, a.Y), b, f32.Pt(a.X, b.Y), a}}
	case penArrow:
		d := b.Sub(a)
		n := float32(math.Hypot(float64(d.X), float64(d.Y)))
		if n < least {
			return s, false
		}
		u := d.Mul(1 / n)
		h := min(4*s.W, n/2) // the head's sides
		c, sn := float32(math.Cos(math.Pi/6)), float32(math.Sin(math.Pi/6))
		left := f32.Pt(-(u.X*c - u.Y*sn), -(u.Y*c + u.X*sn)).Mul(h).Add(b)
		right := f32.Pt(-(u.X*c + u.Y*sn), -(u.Y*c - u.X*sn)).Mul(h).Add(b)
		s.Lines = [][]f32.Point{{a, b}, {left, b, right}}
	default:
		far := float32(0)
		for _, q := range p.pts {
			far = max(far, abs32(q.X-a.X), abs32(q.Y-a.Y))
		}
		if far < least {
			return s, false
		}
		s.Lines = [][]f32.Point{append([]f32.Point(nil), p.pts...)}
	}
	return s, true
}

// drawStrokes draws the review's lines over the shot in view, and the
// one being drawn.
func (t *Tool) drawStrokes(gtx layout.Context) {
	r := t.rev
	if r.view.Empty() {
		return
	}
	p := &r.pen
	defer clip.Rect(r.view).Push(gtx.Ops).Pop()
	for _, s := range p.strokes {
		r.drawStroke(gtx, s)
	}
	if p.drawing {
		if s, ok := p.stroke(r, t.scale); ok {
			r.drawStroke(gtx, s)
		}
	}
}

// drawStroke draws s (shot px) over the shot in r's view.
func (r *review) drawStroke(gtx layout.Context, s Stroke) {
	v, sb := r.view, r.shot.Bounds()
	f := float32(v.Dx()) / float32(sb.Dx())
	at := func(q f32.Point) f32.Point {
		return f32.Pt(float32(v.Min.X)+(q.X-float32(sb.Min.X))*f, float32(v.Min.Y)+(q.Y-float32(sb.Min.Y))*f)
	}
	for _, l := range s.Lines {
		if len(l) < 2 {
			continue
		}
		var path clip.Path
		path.Begin(gtx.Ops)
		path.MoveTo(at(l[0]))
		for _, q := range l[1:] {
			path.LineTo(at(q))
		}
		paint.FillShape(gtx.Ops, s.Col, clip.Stroke{Path: path.End(), Width: s.W * f}.Op())
	}
}

// penBanner is the review's banner with the pen on.
func penBanner(p *pen, armed bool) string {
	shape := "A arrow"
	if p.arrow {
		shape = "A freehand"
	}
	msg := "Draw: drag to draw · Shift rectangle · " + shape + " · 1 2 3 colour · Z undo · Esc back to blur"
	if armed {
		msg += " · Enter copy"
	}
	if n := len(p.strokes); n > 0 {
		msg += fmt.Sprintf(" · %d drawn", n)
	}
	return msg
}

// Strokes are the lines drawn in the review so far (shot px), and
// whether the pen is on; ok false outside a review (tests).
func (t *Tool) Strokes() (strokes []Stroke, drawing, ok bool) {
	if t.state != reviewing || t.rev == nil {
		return nil, false, false
	}
	return append([]Stroke(nil), t.rev.pen.strokes...), t.rev.pen.on, true
}

// burnStrokes draws strokes (px in a shot whose bounds are sb) onto img,
// where the shot's top-left is at at, clipped to the shot there: each a
// union of round-capped segments, its edge antialiased by the distance
// of each px's centre from the nearest segment.
func burnStrokes(img *image.RGBA, at image.Point, sb image.Rectangle, strokes []Stroke) {
	area := image.Rectangle{Min: at, Max: at.Add(sb.Size())}.Intersect(img.Bounds())
	off := f32.Pt(float32(at.X-sb.Min.X), float32(at.Y-sb.Min.Y)) // shot px to img px
	for _, s := range strokes {
		burnStroke(img, area, off, s)
	}
}

func burnStroke(img *image.RGBA, area image.Rectangle, off f32.Point, s Stroke) {
	rad := max(s.W/2, .5)
	reach := int(math.Ceil(float64(rad))) + 1
	var b image.Rectangle
	for _, l := range s.Lines {
		for _, q := range l {
			q = q.Add(off)
			b = b.Union(image.Rect(int(math.Floor(float64(q.X)))-reach, int(math.Floor(float64(q.Y)))-reach,
				int(math.Ceil(float64(q.X)))+reach, int(math.Ceil(float64(q.Y)))+reach))
		}
	}
	b = b.Intersect(area)
	if b.Empty() {
		return
	}
	cov := make([]float32, b.Dx()*b.Dy())
	for _, l := range s.Lines {
		for i := range l {
			p0 := l[i].Add(off)
			p1 := p0
			if i+1 < len(l) {
				p1 = l[i+1].Add(off)
			} else if len(l) > 1 {
				continue // the last point ends the segment before it
			}
			sr := image.Rect(int(math.Floor(float64(min(p0.X, p1.X))))-reach, int(math.Floor(float64(min(p0.Y, p1.Y))))-reach,
				int(math.Ceil(float64(max(p0.X, p1.X))))+reach, int(math.Ceil(float64(max(p0.Y, p1.Y))))+reach).Intersect(b)
			d := p1.Sub(p0)
			dd := d.X*d.X + d.Y*d.Y
			for y := sr.Min.Y; y < sr.Max.Y; y++ {
				row := cov[(y-b.Min.Y)*b.Dx():]
				for x := sr.Min.X; x < sr.Max.X; x++ {
					c := f32.Pt(float32(x)+.5, float32(y)+.5).Sub(p0)
					tt := float32(0)
					if dd > 0 {
						tt = max(0, min((c.X*d.X+c.Y*d.Y)/dd, 1))
					}
					e := c.Sub(d.Mul(tt))
					dist := float32(math.Sqrt(float64(e.X*e.X + e.Y*e.Y)))
					if a := rad + .5 - dist; a > row[x-b.Min.X] {
						row[x-b.Min.X] = min(a, 1)
					}
				}
			}
		}
	}
	ca := float32(s.Col.A) / 255
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			a := cov[(y-b.Min.Y)*b.Dx()+x-b.Min.X] * ca
			if a <= 0 {
				continue
			}
			o := img.PixOffset(x, y)
			px := img.Pix[o : o+4 : o+4]
			for i, c := range [3]uint8{s.Col.R, s.Col.G, s.Col.B} {
				px[i] = uint8(float32(c)*a + float32(px[i])*(1-a) + .5)
			}
			px[3] = uint8(255*a + float32(px[3])*(1-a) + .5)
		}
	}
}

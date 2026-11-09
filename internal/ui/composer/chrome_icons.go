package composer

import (
	"image"
	"image/color"
	"math"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Composer glyphs the shared icon set lacks ("Aa", the hover emoji, the
// filled send arrow, the checkbox tick). Drawn
// from the spec's descriptions on a 20-unit grid with 1.5-unit strokes like
// the rest of yoloslack's icons; every function draws at the current origin
// in a px×px box.

// crPen builds one path on a grid scaled to pixels.
type crPen struct {
	p theme.PathPen // cached by content (stable op identity across frames)
	k float32
}

func (q *crPen) begin(gtx layout.Context, px int, grid float32) {
	q.k = float32(px) / grid
	q.p.Begin(gtx.Ops)
}

func (q *crPen) pt(x, y float32) f32.Point { return f32.Pt(x*q.k, y*q.k) }
func (q *crPen) m(x, y float32)            { q.p.MoveTo(q.pt(x, y)) }
func (q *crPen) l(x, y float32)            { q.p.LineTo(q.pt(x, y)) }
func (q *crPen) z()                        { q.p.Close() }

func (q *crPen) c(x1, y1, x2, y2, x, y float32) {
	q.p.CubeTo(q.pt(x1, y1), q.pt(x2, y2), q.pt(x, y))
}

// circle adds a full circle contour (counter-clockwise).
func (q *crPen) circle(cx, cy, r float32) {
	q.m(cx+r, cy)
	ctr := q.pt(cx, cy)
	q.p.ArcTo(ctr, ctr, 2*math.Pi)
	q.z()
}

// stroke paints the path as a w-unit line, theme.StrokeGamma pixels
// wider or thinner so it carries Chrome's ink (as the icons package does).
func (q *crPen) stroke(gtx layout.Context, w float32, col color.NRGBA) {
	px := w * q.k
	px = max(px+theme.StrokeGamma(col), px/2)
	paint.FillShape(gtx.Ops, col, clip.Stroke{Path: q.p.End(), Width: px}.Op())
}

func (q *crPen) fill(gtx layout.Context, col color.NRGBA) {
	paint.FillShape(gtx.Ops, col, clip.Outline{Path: q.p.End()}.Op())
}

// crIconAa: the "Show formatting" glyph, a capital and a small a, with an
// underline bar when formatting is shown.
func crIconAa(gtx layout.Context, px int, col color.NRGBA, underline bool) {
	var q crPen
	q.begin(gtx, px, 20)
	// A
	q.m(1.5, 15)
	q.l(6.1, 3.25)
	q.l(6.9, 3.25)
	q.l(11.5, 15)
	q.m(3.2, 11)
	q.l(9.8, 11)
	// a: bowl, then the stem with its top hook
	q.m(17.75, 11.5)
	q.l(17.75, 15)
	q.m(12.9, 9.4)
	q.c(13.5, 8.75, 14.3, 8.4, 15.25, 8.4)
	q.c(16.75, 8.4, 17.75, 9.3, 17.75, 10.8)
	q.l(17.75, 11.75)
	q.stroke(gtx, 1.5, col)
	q.begin(gtx, px, 20)
	q.m(17.75, 11.6)
	q.l(15.4, 11.6)
	q.c(13.7, 11.6, 12.6, 12.35, 12.6, 13.4)
	q.c(12.6, 14.45, 13.45, 15.15, 14.7, 15.15)
	q.c(16.2, 15.15, 17.75, 14.2, 17.75, 12.6)
	q.stroke(gtx, 1.5, col)
	if underline {
		q.begin(gtx, px, 20)
		q.m(1, 18.25)
		q.l(19, 18.25)
		q.stroke(gtx, 1.5, col)
	}
}

// crIconEmojiHover: Slack's hovered emoji button, a yellow disc behind a
// dark grinning face, the face scaled 1.05 and turned 15°.
func crIconEmojiHover(gtx layout.Context, px int, disc, ink color.NRGBA) {
	fillCircle(gtx, image.Pt(px/2, px/2), px/2, disc)
	ctr := f32.Pt(float32(px)/2, float32(px)/2)
	aff := f32.AffineId().Scale(ctr, f32.Pt(1.05, 1.05)).Rotate(ctr, 15*math.Pi/180)
	defer op.Affine(aff).Push(gtx.Ops).Pop()
	var q crPen
	q.begin(gtx, px, 20)
	q.circle(10, 10, 8.1)
	q.stroke(gtx, 1.5, ink)
	q.begin(gtx, px, 20)
	q.circle(7.6, 8.1, 1.25)
	q.circle(12.4, 8.1, 1.25)
	// open mouth: flat top, round bottom
	q.m(6.3, 11.1)
	q.l(13.7, 11.1)
	q.c(13.7, 13.3, 12.05, 14.9, 10, 14.9)
	q.c(7.95, 14.9, 6.3, 13.3, 6.3, 11.1)
	q.z()
	q.fill(gtx, ink)
}

// crIconSend: the filled paper plane pointing right, with a slit cut in
// from the middle of its tail.
func crIconSend(gtx layout.Context, px int, col color.NRGBA) {
	var q crPen
	q.begin(gtx, px, 20)
	q.m(3.1, 3.6)
	q.c(2.95, 2.85, 3.7, 2.3, 4.4, 2.65)
	q.l(17.15, 9.15)
	q.c(17.8, 9.5, 17.8, 10.5, 17.15, 10.85)
	q.l(4.4, 17.35)
	q.c(3.7, 17.7, 2.95, 17.15, 3.1, 16.4)
	q.l(4.35, 10.85)
	q.l(10.6, 10.85)
	q.c(11.07, 10.85, 11.45, 10.47, 11.45, 10)
	q.c(11.45, 9.53, 11.07, 9.15, 10.6, 9.15)
	q.l(4.35, 9.15)
	q.z()
	q.fill(gtx, col)
}

// crIconCheck: the checkbox tick, on a 13-unit grid (the box size).
func crIconCheck(gtx layout.Context, px int, col color.NRGBA) {
	var q crPen
	q.begin(gtx, px, 13)
	q.m(2.9, 6.6)
	q.l(5.4, 9.1)
	q.l(10.2, 3.9)
	q.stroke(gtx, 2, col)
}

package popups

import (
	"image"
	"image/color"
	"math"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
)

// Category tab glyphs for the emoji picker: simple outline icons on a 20×20
// grid with 1.5-unit strokes, drawn here (our own shapes, not Slack's
// assets). Glyph i belongs to category tab i; two extra glyphs are used by
// the top "Emoji | GIFs" bar.
const (
	epGlyphSearch = iota
	epGlyphSmiley
	epGlyphLeaf
	epGlyphBurger
	epGlyphPlane
	epGlyphBall
	epGlyphBulb
	epGlyphPeace
	epGlyphFlag
	epGlyphCustom
	epGlyphGif
	epGlyphEmojiTab
)

// epPen builds clip paths in glyph units.
type epPen struct {
	gtx  layout.Context
	s    float32 // px per unit
	path clip.Path
	col  color.NRGBA
}

func (e *epPen) pt(x, y float32) f32.Point { return f32.Pt(x*e.s, y*e.s) }

func (e *epPen) begin()            { e.path.Begin(e.gtx.Ops) }
func (e *epPen) move(x, y float32) { e.path.MoveTo(e.pt(x, y)) }
func (e *epPen) line(x, y float32) { e.path.LineTo(e.pt(x, y)) }
func (e *epPen) close()            { e.path.Close() }
func (e *epPen) cube(a, b, c, d, x, y float32) {
	e.path.CubeTo(e.pt(a, b), e.pt(c, d), e.pt(x, y))
}

// stroke paints the current path as a 1.5-unit line.
func (e *epPen) stroke() { e.strokeW(1.75) }

func (e *epPen) strokeW(w float32) {
	paint.FillShape(e.gtx.Ops, e.col, clip.Stroke{Path: e.path.End(), Width: w * e.s}.Op())
}

func (e *epPen) fill() { e.fillC(e.col) }

func (e *epPen) fillC(c color.NRGBA) {
	paint.FillShape(e.gtx.Ops, c, clip.Outline{Path: e.path.End()}.Op())
}

// arc appends a circular arc from a0 to a1 degrees (screen coordinates:
// 0 = right, 90 = down). move starts a new sub-path at the first point.
func (e *epPen) arc(cx, cy, r, a0, a1 float32, move bool) {
	n := int(math.Abs(float64(a1-a0))/12) + 2
	for i := 0; i <= n; i++ {
		a := float64(a0+(a1-a0)*float32(i)/float32(n)) * math.Pi / 180
		sin, cos := math.Sincos(a)
		x, y := cx+r*float32(cos), cy+r*float32(sin)
		if i == 0 && move {
			e.move(x, y)
		} else {
			e.line(x, y)
		}
	}
}

// circle appends a closed circle sub-path.
func (e *epPen) circle(cx, cy, r float32) {
	e.arc(cx, cy, r, 0, 360, true)
	e.close()
}

// rrect appends a closed rounded rectangle sub-path.
func (e *epPen) rrect(x0, y0, x1, y1, r float32) {
	e.arc(x1-r, y0+r, r, 270, 360, true)
	e.arc(x1-r, y1-r, r, 0, 90, false)
	e.arc(x0+r, y1-r, r, 90, 180, false)
	e.arc(x0+r, y0+r, r, 180, 270, false)
	e.close()
}

// seg strokes one straight line.
func (e *epPen) seg(x0, y0, x1, y1 float32) {
	e.begin()
	e.move(x0, y0)
	e.line(x1, y1)
	e.stroke()
}

// dot fills a small circle.
func (e *epPen) dot(cx, cy, r float32) {
	e.begin()
	e.circle(cx, cy, r)
	e.fill()
}

// rot returns (x, y) rotated by deg around (10, 10) after offsetting the
// centered coordinates.
func epRot(x, y, deg float32) (float32, float32) {
	sin, cos := math.Sincos(float64(deg) * math.Pi / 180)
	s, c := float32(sin), float32(cos)
	return 10 + x*c - y*s, 10 + x*s + y*c
}

// plane outline, pointing up, centered at the origin.
var epPlanePts = [...]float32{
	0, -8, 1.3, -6.6, 1.3, -2.6, 7.6, 1.4, 7.6, 3, 1.3, 1, 1.3, 5, 3.6, 6.8, 3.6, 8, 0, 7,
	-3.6, 8, -3.6, 6.8, -1.3, 5, -1.3, 1, -7.6, 3, -7.6, 1.4, -1.3, -2.6, -1.3, -6.6,
}

// emojiGlyph draws glyph kind at size dp in col.
func emojiGlyph(gtx layout.Context, kind int, size float32, col color.NRGBA) layout.Dimensions {
	px := dp(gtx, size)
	dims := layout.Dimensions{Size: image.Pt(px, px)}
	if kind == epGlyphSearch {
		return icons.Get("search")(gtx, unit.Dp(size), col)
	}
	e := &epPen{gtx: gtx, s: float32(px) / 20, col: col}
	switch kind {
	case epGlyphSmiley:
		e.begin()
		e.circle(10, 10, 7)
		e.stroke()
		e.begin()
		e.arc(10, 10, 3.9, 30, 150, true)
		e.stroke()
		e.dot(7.6, 8.3, 1)
		e.dot(12.4, 8.3, 1)
	case epGlyphLeaf:
		e.begin()
		e.move(4.6, 15.4)
		e.cube(3.4, 7.6, 8.6, 3.4, 16.2, 3.8)
		e.cube(16.6, 11.4, 12.4, 16.6, 4.6, 15.4)
		e.close()
		e.stroke()
		e.seg(3, 17, 11.5, 8.5)
	case epGlyphBurger:
		e.begin()
		e.arc(10, 9.2, 6.6, 180, 360, true)
		e.close()
		e.stroke()
		e.seg(3, 12, 17, 12)
		e.begin()
		e.rrect(3.6, 14.4, 16.4, 17.2, 1.3)
		e.stroke()
	case epGlyphPlane:
		e.begin()
		for i := 0; i < len(epPlanePts); i += 2 {
			x, y := epRot(epPlanePts[i], epPlanePts[i+1], 45)
			if i == 0 {
				e.move(x, y)
			} else {
				e.line(x, y)
			}
		}
		e.close()
		e.stroke()
	case epGlyphBall:
		// an american football tilted 45°, with laces
		e.begin()
		const a, b = 7.6, 4.4
		for i := 0; i <= 36; i++ {
			t := float64(i) * 2 * math.Pi / 36
			sin, cos := math.Sincos(t)
			x, y := epRot(a*float32(cos), b*float32(sin), -45)
			if i == 0 {
				e.move(x, y)
			} else {
				e.line(x, y)
			}
		}
		e.close()
		e.stroke()
		x0, y0 := epRot(-2.6, 0, -45)
		x1, y1 := epRot(2.6, 0, -45)
		e.seg(x0, y0, x1, y1)
		for _, lx := range [3]float32{-1.4, 0, 1.4} {
			x0, y0 := epRot(lx, -1.3, -45)
			x1, y1 := epRot(lx, 1.3, -45)
			e.seg(x0, y0, x1, y1)
		}
	case epGlyphBulb:
		e.begin()
		e.arc(10, 8, 5.6, 135, 405, true)
		e.line(12.5, 14)
		e.line(7.5, 14)
		e.close()
		e.stroke()
		e.seg(7.5, 16.3, 12.5, 16.3)
		e.seg(8.6, 18.4, 11.4, 18.4)
	case epGlyphPeace:
		e.begin()
		e.circle(10, 10, 7)
		e.stroke()
		e.seg(10, 3, 10, 17)
		e.seg(10, 10.6, 5.1, 14.9)
		e.seg(10, 10.6, 14.9, 14.9)
	case epGlyphFlag:
		e.seg(5, 2.6, 5, 17.6)
		e.begin()
		e.move(5, 3.6)
		e.line(15.6, 3.6)
		e.line(13, 7.3)
		e.line(15.6, 11)
		e.line(5, 11)
		e.close()
		e.stroke()
	case epGlyphCustom:
		// four rounded bars in a pinwheel, each with a dot (Slack-ish mark)
		e.begin()
		e.rrect(6.3, 2.5, 8.7, 8.7, 1.2)
		e.rrect(11.3, 6.3, 17.5, 8.7, 1.2)
		e.rrect(11.3, 11.3, 13.7, 17.5, 1.2)
		e.rrect(2.5, 11.3, 8.7, 13.7, 1.2)
		e.circle(12.5, 3.7, 1.2)
		e.circle(16.3, 12.5, 1.2)
		e.circle(7.5, 16.3, 1.2)
		e.circle(3.7, 7.5, 1.2)
		e.fill()
	case epGlyphGif:
		e.begin()
		e.rrect(3, 3, 17, 17, 3.6)
		e.stroke()
		e.begin()
		e.arc(10, 10, 3.6, 30, 150, true)
		e.stroke()
		e.dot(7.7, 8.4, .95)
		e.dot(12.3, 8.4, .95)
	case epGlyphEmojiTab:
		e.begin()
		e.circle(10, 10, 7.4)
		e.fillC(rgb(0xffcc4d))
		e.begin()
		e.circle(10, 10, 7.4)
		e.strokeW(1.3)
		e.begin()
		e.arc(10, 10, 4, 25, 155, true)
		e.strokeW(1.4)
		e.dot(7.5, 8.2, 1.05)
		e.dot(12.5, 8.2, 1.05)
	}
	return dims
}

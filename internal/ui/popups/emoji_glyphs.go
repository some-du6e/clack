package popups

import (
	"image"
	"image/color"
	"math"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// Category tab glyphs for the emoji picker: outline icons on a 20×20 grid
// with uniform round-capped strokes and filled small details, drawn here
// from our own geometry (matched by eye to Slack's tab icons). Glyph i
// belongs to category tab i; two extra glyphs are used by the top
// "Emoji | GIFs" bar.
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

// stroke paints the current path as a 1.75-unit line. The shapes are
// designed for 1.5 units; Gio's linear-space antialiasing makes thin strokes
// read lighter than a browser's, so we draw them a little heavier.
// clip.Stroke only does round caps and joins.
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

// emojiGlyph draws glyph kind at size dp in col.
func emojiGlyph(gtx layout.Context, kind int, size float32, col color.NRGBA) layout.Dimensions {
	px := dp(gtx, size)
	dims := layout.Dimensions{Size: image.Pt(px, px)}
	e := &epPen{gtx: gtx, s: float32(px) / 20, col: col}
	switch kind {
	case epGlyphSearch:
		// magnifier: ring + handle
		e.begin()
		e.circle(9, 9, 6.75)
		e.stroke()
		e.seg(13.77, 13.77, 17.75, 17.75)
	case epGlyphSmiley:
		e.begin()
		e.circle(10, 10, 8.25)
		e.stroke()
		e.dot(7.5, 8, 1.5)
		e.dot(12.5, 8, 1.5)
		// open smile: flat top with rounded corners, curved bottom (filled)
		e.begin()
		e.move(7.3, 11.5)
		e.line(12.7, 11.5)
		e.arc(12.7, 12.3, .8, 270, 360, false)
		e.cube(13.5, 13.25, 12.1, 14.95, 10, 14.95)
		e.cube(7.9, 14.95, 6.5, 13.25, 6.5, 12.3)
		e.arc(7.3, 12.3, .8, 180, 270, false)
		e.close()
		e.fill()
	case epGlyphLeaf:
		// leaf tilted 45°: round left/top lobe, pointed tip at the top right
		e.begin()
		e.move(5.25, 15.45)
		e.cube(-.3, 8.7, 6.3, 1.75, 16.6, 3.45)
		e.line(17.95, 4)
		e.cube(16, 7.55, 17.1, 9.7, 15.1, 13.5)
		e.cube(13.1, 17.3, 8.65, 17.55, 5.25, 15.45)
		e.close()
		e.stroke()
		// midrib running out into the stem, and two L-shaped veins
		e.seg(2.25, 17.75, 12.5, 7.5)
		e.begin()
		e.move(6.75, 9.75)
		e.line(6.75, 13.25)
		e.line(10.25, 13.25)
		e.move(9.75, 7.75)
		e.line(9.75, 10.25)
		e.line(12.25, 10.25)
		e.stroke()
	case epGlyphBurger:
		// top bun dome, its sides running down to the patty
		e.begin()
		e.move(3.25, 10.75)
		e.line(3.25, 7.6)
		e.cube(3.25, 4.7, 6, 2.25, 10, 2.25)
		e.cube(14, 2.25, 16.75, 4.7, 16.75, 7.6)
		e.line(16.75, 10.75)
		e.stroke()
		e.seg(3.25, 8.25, 16.75, 8.25)
		e.dot(7.25, 5.75, .75)
		e.dot(10, 5.75, .75)
		e.dot(12.75, 5.75, .75)
		// patty pill with a cheese drip hanging from its top edge
		e.begin()
		e.move(8.35, 10.75)
		e.line(3.5, 10.75)
		e.arc(3.5, 12.5, 1.75, 270, 90, false)
		e.line(16.5, 14.25)
		e.arc(16.5, 12.5, 1.75, 90, -90, false)
		e.line(13.15, 10.75)
		e.cube(13.15, 12.2, 12.25, 13.25, 10.75, 13.25)
		e.cube(9.25, 13.25, 8.35, 12.2, 8.35, 10.75)
		e.close()
		e.stroke()
		// bottom bun
		e.begin()
		e.move(3.25, 14.25)
		e.line(3.25, 15.25)
		e.arc(5.25, 15.25, 2, 180, 90, false)
		e.arc(14.75, 15.25, 2, 90, 0, false)
		e.line(16.75, 14.25)
		e.stroke()
	case epGlyphPlane:
		// airplane flying up-right, one outline mirrored across x+y = 20:
		// nose, upper wing, tail fins, lower wing, back to the nose
		e.begin()
		e.move(17.45, 2.55)
		e.cube(17.2, 2.3, 16.25, 1.95, 14.75, 2.8)
		e.line(12.2, 5.25)
		e.line(4.65, 3.2)
		e.cube(3.1, 3, 1.45, 4.55, 2.55, 5.4)
		e.line(8.9, 8.7)
		e.line(5.6, 12)
		e.cube(3.5, 11.6, 1.7, 12.6, 2.4, 13.7)
		e.cube(2.95, 14.75, 3.4, 13.9, 4.75, 15.25)
		e.cube(6.1, 16.6, 5.25, 17.05, 6.3, 17.6)
		e.cube(7.4, 18.3, 8.4, 16.5, 8, 14.4)
		e.line(11.3, 11.1)
		e.line(14.6, 17.45)
		e.cube(15.45, 18.55, 17, 16.9, 16.8, 15.35)
		e.line(14.75, 7.8)
		e.line(17.2, 5.25)
		e.cube(18.05, 3.75, 17.7, 2.8, 17.45, 2.55)
		e.close()
		e.stroke()
	case epGlyphBall:
		// american football tilted 45° with blunt rounded ends
		e.begin()
		e.move(17.28, 2.72)
		e.cube(16.01, 1.44, 9.54, .7, 5.12, 5.12)
		e.cube(.7, 9.54, 1.44, 16.01, 2.72, 17.28)
		e.cube(3.99, 18.56, 10.46, 19.3, 14.88, 14.88)
		e.cube(19.3, 10.46, 18.56, 3.99, 17.28, 2.72)
		e.close()
		e.stroke()
		// seams across both ends
		e.begin()
		e.move(11.98, 1.94)
		e.line(18.06, 8.02)
		e.move(1.94, 11.98)
		e.line(8.02, 18.06)
		e.stroke()
		// laces: a spine with three cross stitches
		e.begin()
		e.move(7.98, 12.02)
		e.line(12.02, 7.98)
		e.move(6.99, 11.03)
		e.line(8.97, 13.01)
		e.move(11.03, 6.99)
		e.line(13.01, 8.97)
		e.move(9.01, 9.01)
		e.line(10.99, 10.99)
		e.stroke()
	case epGlyphBulb:
		// round bulb narrowing into a neck with rounded bottom corners
		e.begin()
		e.arc(10, 7, 5.25, 136.6, 403.4, true)
		e.cube(11.58, 12.97, 12.05, 12.3, 12.05, 13.7)
		e.line(12.05, 14.15)
		e.arc(10.95, 14.15, 1.1, 0, 90, false)
		e.arc(9.05, 14.15, 1.1, 90, 180, false)
		e.line(7.95, 13.7)
		e.cube(7.95, 12.3, 8.42, 12.97, 6.19, 10.61)
		e.close()
		e.stroke()
		e.seg(8.75, 18.25, 11.25, 18.25)
	case epGlyphPeace:
		e.begin()
		e.circle(10, 10, 8.25)
		e.stroke()
		e.seg(10, 1.75, 10, 18.25)
		e.begin()
		e.move(3.69, 15.31)
		e.line(10, 9)
		e.line(16.31, 15.31)
		e.stroke()
	case epGlyphFlag:
		// pole and a waving flag (the bottom edge repeats the top wave)
		e.begin()
		e.move(3.25, 17.75)
		e.line(3.25, 2.95)
		e.cube(11.4, .4, 10.05, 6.35, 16.75, 3.4)
		e.line(16.75, 13.4)
		e.cube(10.05, 16.35, 11.4, 10.4, 3.25, 12.95)
		e.stroke()
	case epGlyphCustom:
		// Slack's hash mark: four capsules in a pinwheel, each with a
		// drop-shaped dot (a circle with one square corner), all filled
		const r = 1.875
		e.begin()
		e.rrect(10.5, 1, 14.25, 9.5, r)
		e.rrect(1, 5.75, 9.5, 9.5, r)
		e.rrect(10.5, 10.5, 19, 14.25, r)
		e.rrect(5.75, 10.5, 9.5, 19, r)
		e.arc(7.625, 2.875, r, 90, 360, true)
		e.line(9.5, 4.75)
		e.close()
		e.arc(17.125, 7.625, r, 180, 450, true)
		e.line(15.25, 9.5)
		e.close()
		e.arc(12.375, 17.125, r, 270, 540, true)
		e.line(10.5, 15.25)
		e.close()
		e.arc(2.875, 12.375, r, 0, 270, true)
		e.line(4.75, 10.5)
		e.close()
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

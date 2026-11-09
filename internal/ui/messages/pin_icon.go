package messages

import (
	"image"
	"image/color"
	"strconv"
	"strings"
	"sync"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// Slack's pinned label icon. MessageLabels (boot-shared) maps the pinned
// label to the legacy font Icon "thumb-tack-filled" (size "inherit": the
// 13px label font), drawn in #e8912d (.c-message_kit__labels--pinned
// .c-message_kit__labels__icon). The CSS gives it as
// .c-icon--thumb-tack-filled:before{content:""} in font "Slack v2"
// (slack-icons-v2-6509901.woff on a.slack-edge.com/bv1-13-br/), whose
// glyph is icon_pin_filled: the head up and to the left, the needle down
// to the right. The outline below is that glyph as fontTools' SVGPathPen
// prints it, in font units (2000/em, y up, ascent 1700).
//
// Kept here, not in internal/ui/icons (another agent's package while
// this lands), and drawn straight from the font's own outline.
const pinGlyph = "M1046 1241 1081 1434C1091 1487 1055 1600 937 1600C921 1600 880 1597 835 1571L285 1253C229 1221 200 1172 200 1123C200 1065 241 1008 303 982L475 910L589 713C562 642 540 526 540 423C540 294 566 169 674 169C699 169 726 179 747 191L1076 381L1389 -161C1405 -189 1426 -200 1449 -200C1489 -200 1529 -170 1529 -124C1529 -110 1526 -99 1519 -86L1206 456L1534 645C1581 672 1600 709 1600 750C1600 887 1338 1017 1160 1044Z"

const (
	glyphUPM    = 2000
	glyphAscent = 1700
)

// glyphSeg is one absolute segment of a font outline: 'M', 'L', 'C' or 'Z'.
type glyphSeg struct {
	op byte
	p  [3]f32.Point
}

var (
	pinOnce sync.Once
	pinSegs []glyphSeg
)

// parseGlyph reads SVGPathPen output: absolute M/L/C/Z with implicit
// repeats (extra pairs after M are lines).
func parseGlyph(d string) []glyphSeg {
	var out []glyphSeg
	var nums []float32
	var cmd byte
	flush := func() {
		pt := func(i int) f32.Point { return f32.Pt(nums[i], nums[i+1]) }
		switch cmd {
		case 'M', 'L':
			for i := 0; i+1 < len(nums); i += 2 {
				op := cmd
				if cmd == 'M' && i > 0 {
					op = 'L'
				}
				out = append(out, glyphSeg{op: op, p: [3]f32.Point{pt(i)}})
			}
		case 'C':
			for i := 0; i+5 < len(nums); i += 6 {
				out = append(out, glyphSeg{op: 'C', p: [3]f32.Point{pt(i), pt(i + 2), pt(i + 4)}})
			}
		case 'Z':
			out = append(out, glyphSeg{op: 'Z'})
		}
		nums = nums[:0]
	}
	tok := strings.FieldsFunc(strings.NewReplacer("M", " M ", "L", " L ", "C", " C ", "Z", " Z ").Replace(d), func(r rune) bool { return r == ' ' || r == ',' })
	for _, t := range tok {
		switch t {
		case "M", "L", "C", "Z":
			flush()
			cmd = t[0]
			if cmd == 'Z' {
				flush()
			}
		default:
			if v, err := strconv.ParseFloat(t, 32); err == nil {
				nums = append(nums, float32(v))
			}
		}
	}
	flush()
	return out
}

// drawPin fills Slack's pin glyph in an em×em box whose top-left is at
// origin (the font's em box: ascent above the baseline, so the baseline
// sits at origin.Y + em*0.85).
func drawPin(gtx layout.Context, origin image.Point, em int, col color.NRGBA) {
	pinOnce.Do(func() { pinSegs = parseGlyph(pinGlyph) })
	k := float32(em) / glyphUPM
	tr := func(p f32.Point) f32.Point {
		return f32.Pt(float32(origin.X)+p.X*k, float32(origin.Y)+(glyphAscent-p.Y)*k)
	}
	var path clip.Path
	path.Begin(gtx.Ops)
	for _, s := range pinSegs {
		switch s.op {
		case 'M':
			path.MoveTo(tr(s.p[0]))
		case 'L':
			path.LineTo(tr(s.p[0]))
		case 'C':
			path.CubeTo(tr(s.p[0]), tr(s.p[1]), tr(s.p[2]))
		case 'Z':
			path.Close()
		}
	}
	paint.FillShape(gtx.Ops, col, clip.Outline{Path: path.End()}.Op())
}

// pinGlyphBounds is the glyph's ink box at em px with the em box's
// top-left at the origin (tests).
func pinGlyphBounds(em int) image.Rectangle {
	pinOnce.Do(func() { pinSegs = parseGlyph(pinGlyph) })
	k := float32(em) / glyphUPM
	minX, minY, maxX, maxY := float32(1e9), float32(1e9), float32(-1e9), float32(-1e9)
	for _, s := range pinSegs {
		n := 1
		if s.op == 'C' {
			n = 3
		}
		if s.op == 'Z' {
			continue
		}
		for _, p := range s.p[:n] {
			x, y := p.X*k, (glyphAscent-p.Y)*k
			minX, minY = min(minX, x), min(minY, y)
			maxX, maxY = max(maxX, x), max(maxY, y)
		}
	}
	return image.Rect(int(minX), int(minY), int(maxX+0.999), int(maxY+0.999))
}

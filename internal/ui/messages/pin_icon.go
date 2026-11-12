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

// jumpGlyph is the broadcast label's icon: MessageLabels maps the
// "broadcast" label (a thread reply also sent to the channel) to the font
// Icon "jump", .c-icon--jump:before{content:"\e084"}, glyph icon_jump in
// the same font (a # with an arrow pointing left), drawn in sk_highlight
// (.c-message_kit__labels--broadcast .c-message_kit__labels__icon).
const jumpGlyph = "M934 1025 991 1314C999 1354 966 1400 917 1400C880 1400 850 1373 843 1336L782 1025H545L602 1314C610 1354 577 1400 528 1400C491 1400 461 1373 454 1336L393 1025H168C127 1025 93 991 93 950C93 909 127 875 168 875H364L295 525H70C29 525 -5 491 -5 450C-5 409 29 375 70 375H266L209 86C201 46 234 0 283 0C320 0 350 27 357 64L418 375H655L598 86C590 46 623 0 672 0C709 0 739 27 746 64L807 375H1032C1073 375 1107 409 1107 450C1107 491 1073 525 1032 525H836L905 875H1130C1171 875 1205 909 1205 950C1205 991 1171 1025 1130 1025ZM1454 750 1628 921C1643 936 1650 955 1650 974C1650 994 1643 1012 1628 1027C1613 1042 1594 1050 1575 1050C1556 1050 1537 1042 1522 1027L1222 730C1207 715 1200 696 1200 677C1200 658 1207 639 1222 624L1522 322C1537 307 1556 300 1575 300C1594 300 1613 307 1628 322C1643 337 1650 356 1650 375C1650 394 1642 414 1628 428L1458 600H1924C1968 600 2000 632 2000 675C2000 718 1968 750 1924 750ZM447 525 516 875H753L684 525Z"

// eyeGlyph is the ephemeral label's icon ("Only visible to you"): the
// font Icon "eye-filled", .c-icon--eye-filled:before{content:"\e584"},
// glyph icon_eye_filled. No rule colours it, so it takes the label's
// rgba(29,28,29,.7).
const eyeGlyph = "M1000 0C1600 0 1900 550 1900 700C1900 850 1600 1400 1000 1400C400 1400 100 850 100 700C100 550 400 0 1000 0ZM1000 250C751 250 550 451 550 700C550 949 751 1150 1000 1150C1249 1150 1450 949 1450 700C1450 451 1249 250 1000 250ZM1246 745C1226 735 1204 730 1180 730C1097 730 1030 797 1030 880C1030 904 1035 926 1045 946C1031 949 1015 950 1000 950C862 950 750 838 750 700C750 562 862 450 1000 450C1138 450 1250 562 1250 700C1250 715 1249 730 1246 745Z"

// bookmarkGlyph is the Later label's icon (a saved message that isn't a
// to-do): MessageLabels maps "later" to the font Icon "bookmark-filled",
// .c-icon--bookmark-filled:before{content:"\e588"}, glyph
// icon_bookmark_filled in the same font, drawn in sk_highlight (the
// .c-message_kit__labels--later label's colour).
const bookmarkGlyph = "M553 -100C581 -100 612 -88 637 -62L1000 320L1363 -62C1388 -88 1419 -100 1447 -100C1502 -100 1550 -55 1550 16V1175C1550 1333 1434 1450 1275 1450H725C566 1450 450 1334 450 1175V16C450 -55 498 -100 553 -100Z"

var (
	bookmarkOnce sync.Once
	bookmarkSegs []glyphSeg
	pinOnce      sync.Once
	pinSegs      []glyphSeg
	jumpOnce     sync.Once
	jumpSegs     []glyphSeg
	eyeOnce      sync.Once
	eyeSegs      []glyphSeg
)

// parseGlyph reads SVGPathPen output: absolute M/L/H/V/C/Z with implicit
// repeats (extra pairs after M are lines). H and V become lines from the
// current point (the jump and bookmark glyphs use them).
func parseGlyph(d string) []glyphSeg {
	var out []glyphSeg
	var nums []float32
	var cmd byte
	var cur, start f32.Point
	flush := func() {
		pt := func(i int) f32.Point { return f32.Pt(nums[i], nums[i+1]) }
		line := func(p f32.Point) {
			out = append(out, glyphSeg{op: 'L', p: [3]f32.Point{p}})
			cur = p
		}
		switch cmd {
		case 'M', 'L':
			for i := 0; i+1 < len(nums); i += 2 {
				if cmd == 'M' && i == 0 {
					out = append(out, glyphSeg{op: 'M', p: [3]f32.Point{pt(i)}})
					cur, start = pt(i), pt(i)
					continue
				}
				line(pt(i))
			}
		case 'H':
			for _, x := range nums {
				line(f32.Pt(x, cur.Y))
			}
		case 'V':
			for _, y := range nums {
				line(f32.Pt(cur.X, y))
			}
		case 'C':
			for i := 0; i+5 < len(nums); i += 6 {
				out = append(out, glyphSeg{op: 'C', p: [3]f32.Point{pt(i), pt(i + 2), pt(i + 4)}})
				cur = pt(i + 4)
			}
		case 'Z':
			out = append(out, glyphSeg{op: 'Z'})
			cur = start
		}
		nums = nums[:0]
	}
	tok := strings.FieldsFunc(strings.NewReplacer("M", " M ", "L", " L ", "H", " H ", "V", " V ", "C", " C ", "Z", " Z ").Replace(d), func(r rune) bool { return r == ' ' || r == ',' })
	for _, t := range tok {
		switch t {
		case "M", "L", "H", "V", "C", "Z":
			flush()
			cmd = t[0]
			if cmd == 'Z' {
				flush()
				cmd = 0 // one Z per close
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
	drawGlyph(gtx, pinSegs, origin, em, col)
}

// drawJump fills the jump glyph like drawPin.
func drawJump(gtx layout.Context, origin image.Point, em int, col color.NRGBA) {
	jumpOnce.Do(func() { jumpSegs = parseGlyph(jumpGlyph) })
	drawGlyph(gtx, jumpSegs, origin, em, col)
}

// drawBookmark fills the bookmark-filled glyph like drawPin.
func drawBookmark(gtx layout.Context, origin image.Point, em int, col color.NRGBA) {
	bookmarkOnce.Do(func() { bookmarkSegs = parseGlyph(bookmarkGlyph) })
	drawGlyph(gtx, bookmarkSegs, origin, em, col)
}

// drawEye fills the eye-filled glyph like drawPin.
func drawEye(gtx layout.Context, origin image.Point, em int, col color.NRGBA) {
	eyeOnce.Do(func() { eyeSegs = parseGlyph(eyeGlyph) })
	drawGlyph(gtx, eyeSegs, origin, em, col)
}

// drawGlyph fills a font outline (non-zero winding, as the font's
// counters are wound against their contours) in an em×em box at origin.
func drawGlyph(gtx layout.Context, segs []glyphSeg, origin image.Point, em int, col color.NRGBA) {
	k := float32(em) / glyphUPM
	tr := func(p f32.Point) f32.Point {
		return f32.Pt(float32(origin.X)+p.X*k, float32(origin.Y)+(glyphAscent-p.Y)*k)
	}
	var path clip.Path
	path.Begin(gtx.Ops)
	for _, s := range segs {
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

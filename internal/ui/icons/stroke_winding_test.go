package icons

import (
	"sort"
	"testing"

	"gioui.org/f32"
)

// TestStrokeLoopsWound holds every stroked part to one winding. Gio's
// stroker (third_party/gioui.org/internal/stroke, StrokeQuads.stroke) winds
// an open subpath's band the same way whichever way the line runs, but a
// closed subpath's band (its two offset contours) by the contour's own
// direction, and one clip.Stroke op fills its bands by the sum of their
// windings: where a loop wound against the lines meets one, the two cancel
// and leave a hole. pdf-file's right loop, drawn counter-clockwise on
// screen, opened pinholes where it met its lines (icon-shots run
// 45441921230: 6 to 9 holes at 4x where Slack's glyph has 4). circle()
// runs clockwise on screen, the lines' way; hole() is for fills.
func TestStrokeLoopsWound(t *testing.T) {
	// The check itself: a line beside hole() (counter-clockwise) is
	// flagged, a line beside circle() isn't.
	if loopsAgainstLines(parsePath("M2 10 L18 10 "+hole(10, 10, 4))) != 1 || loopsAgainstLines(parsePath("M2 10 L18 10 "+circle(10, 10, 4))) != 0 {
		t.Fatal("loopsAgainstLines: want hole() beside a line flagged and circle() not")
	}
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		for i, p := range registry[n].parts {
			if p.stroke == 0 || p.fill { // fl parts stroke each subpath on its own
				continue
			}
			if k := loopsAgainstLines(parsePath(p.d)); k > 0 {
				t.Errorf("%s part %d: %d closed subpath(s) wound counter-clockwise on screen beside lines wound clockwise: Gio cancels the bands where they meet (reverse the loop)", n, i, k)
			}
		}
	}
}

// loopsAgainstLines counts the closed subpaths of one stroke wound
// counter-clockwise on screen (Gio's ccw() false: a negative shoelace area
// in y-down coordinates) when the stroke also has a band wound the other
// way: an open subpath, or a closed one wound clockwise. A subpath is
// closed when it ends in Z or where it began (Gio's beg == end).
func loopsAgainstLines(segs []seg) int {
	var neg, pos int
	var pts []f32.Point
	closed := false
	flush := func() {
		if len(pts) > 1 && pts[len(pts)-1] == pts[0] {
			closed = true
		}
		switch {
		case len(pts) < 2:
		case !closed:
			pos++
		case shoelace(pts) < 0:
			neg++
		default:
			pos++
		}
		pts, closed = pts[:0], false
	}
	for _, sg := range segs {
		switch sg.op {
		case 'M':
			flush()
			pts = append(pts, sg.p[0])
		case 'L':
			pts = append(pts, sg.p[0])
		case 'C':
			p0 := pts[len(pts)-1]
			for k := 1; k <= 8; k++ {
				u := float32(k) / 8
				v := 1 - u
				pts = append(pts, p0.Mul(v*v*v).Add(sg.p[0].Mul(3*v*v*u)).Add(sg.p[1].Mul(3*v*u*u)).Add(sg.p[2].Mul(u*u*u)))
			}
		case 'Z':
			closed = true
		}
	}
	flush()
	if pos == 0 {
		return 0 // all one winding: the bands add, nothing cancels
	}
	return neg
}

// shoelace is the signed area of the closed polygon pts, positive when it
// runs clockwise on screen (y down).
func shoelace(pts []f32.Point) float32 {
	var a float32
	for i := range pts {
		p, q := pts[i], pts[(i+1)%len(pts)]
		a += p.X*q.Y - q.X*p.Y
	}
	return a / 2
}

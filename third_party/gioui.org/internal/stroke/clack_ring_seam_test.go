package stroke

// The intro's "stray line" (Karim, 2026-11-22 #clack 1795380747): a thin
// vertical line hanging down from a stroked ring's 3 o'clock point. Gio's
// stencil covers from each outline segment down to the path's bottom, so
// an outline loop that doesn't close leaks a column there. These check the
// stroked outline of a ring built as clip.Path builds MoveTo + ArcTo(2π) +
// Close (and of open arcs swept up to 2π, the spinners'): every run of
// segments continuous and closed, no point off the ring's annulus, no NaN.

import (
	"fmt"
	"math"
	"testing"

	"gioui.org/f32"
)

// clackArc is MoveTo(c+(r,0)), ArcTo(c, c, sweep) and, closed, Close, as
// op/clip's Path records them (its first contour is 1: split opens a group on each new one).
func clackArc(c f32.Point, r, sweep float32, closed bool) StrokeQuads {
	start := f32.Pt(c.X+r, c.Y)
	pen := start
	m, segs := ArcTransform(pen, c, c, sweep)
	var qs StrokeQuads
	for range segs {
		p0 := pen
		p1 := m.Transform(p0)
		p2 := m.Transform(p1)
		ctl := p1.Mul(2).Sub(p0.Add(p2).Mul(.5))
		qs = append(qs, StrokeQuad{Contour: 1, Quad: QuadSegment{From: p0, Ctrl: ctl, To: p2}})
		pen = p2
	}
	if closed && pen != start {
		qs = append(qs, StrokeQuad{Contour: 1, Quad: QuadSegment{From: pen, Ctrl: pen.Add(start).Mul(.5), To: start}})
	}
	return qs
}

// clackOutlineFaults reports what's wrong with a stroked outline: runs of
// segments that don't close (the stencil leaks below their gap), points
// farther than slack from the band [inner, outer] around c, NaNs.
func clackOutlineFaults(out StrokeQuads, c f32.Point, inner, outer, slack float64) []string {
	var faults []string
	if len(out) == 0 {
		return []string{"no outline"}
	}
	runStart := 0
	closeRun := func(end int) {
		a, b := out[runStart].Quad.From, out[end].Quad.To
		if a != b {
			faults = append(faults, fmt.Sprintf("run %d..%d open: %v → %v (gap %.3g)", runStart, end, b, a, lenPt(a.Sub(b))))
		}
	}
	for i, q := range out {
		for _, p := range []f32.Point{q.Quad.From, q.Quad.To} {
			if math.IsNaN(float64(p.X)) || math.IsNaN(float64(p.Y)) || math.IsInf(float64(p.X), 0) || math.IsInf(float64(p.Y), 0) {
				faults = append(faults, fmt.Sprintf("segment %d: %v", i, p))
				continue
			}
			d := math.Hypot(float64(p.X-c.X), float64(p.Y-c.Y))
			if d < inner-slack || d > outer+slack {
				faults = append(faults, fmt.Sprintf("segment %d: %v is %.3f from the centre, the band is %.3f..%.3f", i, p, d, inner, outer))
			}
		}
		if i > 0 && q.Quad.From != out[i-1].Quad.To {
			closeRun(i - 1)
			runStart = i
		}
	}
	closeRun(len(out) - 1)
	return faults
}

var (
	clackCentres = []f32.Point{{X: 100, Y: 100}, {X: 333.3, Y: 271.7}, {X: 512.25, Y: 384.5}, {X: 777.77, Y: 1001.13}, {X: 1439.6, Y: 899.9}}
	clackRadii   = []float32{2, 4, 7.5, 12, 20, 44, 52.8, 88, 104, 250}
	clackWidths  = []float32{1, 1.6, 2, 2.4, 3, 7}
)

// clackShortArc is clackArc closed, its arc ending one float step past
// its start in y, on the side whose closing midpoint rounds onto the start
// (the closing line's end): what ArcTo's rounding did to 73 of 1248 rings,
// made the same on every architecture (arm64's fused multiply-adds round
// ArcTo differently).
func clackShortArc(c f32.Point, r float32) StrokeQuads {
	qs := clackArc(c, r, 2*math.Pi, false)
	start := qs[0].Quad.From
	for _, dir := range []float64{1, -1} {
		pen := f32.Pt(start.X, math.Nextafter32(start.Y, float32(math.Inf(int(dir)))))
		if mid := pen.Add(start).Mul(.5); mid == start {
			last := &qs[len(qs)-1].Quad
			last.To = pen
			return append(qs, StrokeQuad{Contour: 1, Quad: QuadSegment{From: pen, Ctrl: mid, To: start}})
		}
	}
	return nil // start.Y odd: both midpoints round onto the pen
}

// A closed ring (MoveTo, ArcTo 2π, Close), and one whose arc ends a float
// step short: its outline's two loops close.
func TestClackRingSeamClosedRing(t *testing.T) {
	n := 0
	for _, c := range clackCentres {
		for _, r := range clackRadii {
			for _, w := range clackWidths {
				if w >= r {
					continue
				}
				hw := float64(w) / 2
				for _, short := range []bool{false, true} {
					in := clackArc(c, r, 2*math.Pi, true)
					if short {
						if in = clackShortArc(c, r); in == nil {
							continue
						}
					}
					out := in.stroke(StrokeStyle{Width: w})
					if f := clackOutlineFaults(out, c, float64(r)-hw, float64(r)+hw, 0.05); len(f) > 0 {
						n++
						if n <= 12 {
							t.Errorf("ring c %v r %g w %g (a float step short: %v): %d faults, first %q", c, r, w, short, len(f), f[0])
						}
					}
				}
			}
		}
	}
	if n > 12 {
		t.Errorf("…and %d more rings", n-12)
	}
}

// Open arcs swept up to 2π, as the spinners draw them: their outline (a
// single loop, its round caps) closes and stays on the band.
func TestClackRingSeamSweeps(t *testing.T) {
	n := 0
	for _, c := range clackCentres {
		for _, r := range []float32{6, 12, 35, 44} {
			for _, w := range []float32{1.5, 2, 4} {
				for k := 0; k <= 64; k++ {
					sweep := float32(2*math.Pi) * (0.9 + 0.1*float32(k)/64)
					out := clackArc(c, r, sweep, false).stroke(StrokeStyle{Width: w})
					hw := float64(w) / 2
					if f := clackOutlineFaults(out, c, float64(r)-hw, float64(r)+hw, 0.05); len(f) > 0 {
						n++
						if n <= 12 {
							t.Errorf("sweep %.6f c %v r %g w %g: %d faults, first %q", sweep, c, r, w, len(f), f[0])
						}
					}
				}
			}
		}
	}
	if n > 12 {
		t.Errorf("…and %d more sweeps", n-12)
	}
}

// clackNaN is the first outline segment with a NaN point, or -1.
func clackNaN(out StrokeQuads) int {
	for i, q := range out {
		if q.Quad.From != q.Quad.From || q.Quad.Ctrl != q.Quad.Ctrl || q.Quad.To != q.Quad.To {
			return i
		}
	}
	return -1
}

// clackWinding is the outline's nonzero winding number at p, each quad
// cut into 16 lines.
func clackWinding(out StrokeQuads, p f32.Point) int {
	w := 0
	cross := func(a, b f32.Point) {
		if (a.Y <= p.Y) == (b.Y <= p.Y) {
			return
		}
		x := a.X + (p.Y-a.Y)/(b.Y-a.Y)*(b.X-a.X)
		if x > p.X {
			if b.Y > a.Y {
				w++
			} else {
				w--
			}
		}
	}
	for _, q := range out {
		prev := q.Quad.From
		for i := 1; i <= 16; i++ {
			t := float32(i) / 16
			u := 1 - t
			pt := q.Quad.From.Mul(u * u).Add(q.Quad.Ctrl.Mul(2 * u * t)).Add(q.Quad.To.Mul(t * t))
			cross(prev, pt)
			prev = pt
		}
	}
	return w
}

// A polyline whose first segment is one float step long (its midpoint
// rounds onto its end, so its control point is its end): the stroke covers
// everything within a half width of the line, no hole by its start.
// addLine took the t = 1 normal from the start it had already offset, so
// that segment's sides ended a half width along the line instead of
// across it (Gio patch 62, ring-seam's review).
func TestClackRingSeamTinyFirstSegment(t *testing.T) {
	const w, hw = 2, 1
	for _, b := range []f32.Point{{X: 100, Y: 50}, {X: 333.3, Y: 384.5}, {X: 40.25, Y: 12.5}} {
		for _, d := range []f32.Point{{X: 1, Y: 0}, {X: 0, Y: 1}, {X: 1, Y: 1}, {X: -1, Y: 0}} {
			step := func(v, dir float32) float32 {
				switch {
				case dir > 0:
					return math.Nextafter32(v, float32(math.Inf(1)))
				case dir < 0:
					return math.Nextafter32(v, float32(math.Inf(-1)))
				}
				return v
			}
			e := f32.Pt(step(b.X, d.X), step(b.Y, d.Y))
			a := f32.Pt(b.X-10, b.Y)
			in := StrokeQuads{
				{Contour: 1, Quad: QuadSegment{From: e, Ctrl: e.Add(b).Mul(.5), To: b}},
				{Contour: 1, Quad: QuadSegment{From: b, Ctrl: b.Add(a).Mul(.5), To: a}},
			}
			out := in.stroke(StrokeStyle{Width: w})
			if i := clackNaN(out); i >= 0 {
				t.Errorf("start %v → %v → %v: segment %d of the outline is NaN", e, b, a, i)
				continue
			}
			holes := 0
			for y := float64(b.Y) - hw; y <= float64(b.Y)+hw; y += 0.05 {
				for x := float64(b.X) - 3; x <= float64(b.X)+hw; x += 0.05 {
					// the distance to the segment b–a (a lies 10 to the left)
					dx := math.Max(x-float64(b.X), 0)
					if math.Hypot(dx, y-float64(b.Y)) >= hw-0.06 {
						continue
					}
					if clackWinding(out, f32.Pt(float32(x), float32(y))) == 0 {
						holes++
					}
				}
			}
			if holes > 0 {
				t.Errorf("start %v → %v → %v: %.3f px² of the stroke uncovered by its start", e, b, a, float64(holes)*0.05*0.05)
			}
		}
	}
}

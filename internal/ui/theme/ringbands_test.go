package theme

import (
	"image"
	"math"
	"testing"
)

// ringBands' bands don't overlap, lie in the stroke's bounds and hold
// every pixel within w/2 and a pixel more of the rounded outline: the
// pixels Gio's stroke can give coverage. The pixels (the coverage of each
// band against the whole ring on a GPU) are TestFrameCostPixels'.
func TestRingBands(t *testing.T) {
	for _, c := range []struct {
		r      image.Rectangle
		radius int
		w      float32
		none   bool // too small for bands to pay
	}{
		{image.Rect(70, 40, 1276, 796), 8, 1.5, false},    // the workspace shadow's first ring at 1x
		{image.Rect(56, 26, 1290, 810), 22, 1.5, false},   // its last
		{image.Rect(140, 80, 2552, 1592), 16, 1.5, false}, // at 2x
		{image.Rect(328, 40, 1916, 1078), 8, 1, false},    // the dark workspace outline
		{image.Rect(100, 100, 900, 700), 0, 2, false},     // square corners
		{image.Rect(10, 10, 1200, 700), 30, 3, false},     // wide stroke, big radius
		{image.Rect(0, 0, 36, 36), 8, 1.5, true},          // an avatar's ring
		{image.Rect(0, 0, 4000, 26), 12, 1.5, true},       // the sides meet: the bands are all of it
	} {
		bands, n := ringBands(c.r, c.radius, c.w)
		if c.none || n == 0 {
			if c.none != (n == 0) {
				t.Errorf("%v r=%d w=%g: %d bands, want none %v", c.r, c.radius, c.w, n, c.none)
			}
			continue
		}
		m := int(c.w*.5 + .5)
		box := c.r.Inset(-m)
		for i, b := range bands[:n] {
			if !b.In(box) {
				t.Errorf("%v r=%d w=%g: band %v outside the stroke's bounds %v", c.r, c.radius, c.w, b, box)
			}
			for _, o := range bands[i+1 : n] {
				if b.Overlaps(o) {
					t.Errorf("%v r=%d w=%g: bands %v and %v overlap", c.r, c.radius, c.w, b, o)
				}
			}
		}
		missed := 0
		var first image.Point
		for y := box.Min.Y; y < box.Max.Y; y++ {
			for x := box.Min.X; x < box.Max.X; x++ {
				d := rrDistance(c.r, float64(c.radius), float64(x)+.5, float64(y)+.5)
				if math.Abs(d) > float64(c.w)/2+1 {
					continue
				}
				in := false
				for _, b := range bands[:n] {
					if image.Pt(x, y).In(b) {
						in = true
						break
					}
				}
				if !in {
					if missed == 0 {
						first = image.Pt(x, y)
					}
					missed++
				}
			}
		}
		if missed > 0 {
			t.Errorf("%v r=%d w=%g: %d pixels near the outline in no band, first %v", c.r, c.radius, c.w, missed, first)
		}
	}
}

// rrDistance is the signed distance from (x, y) to the outline of the
// rounded rectangle r with corner radius rad (negative inside).
func rrDistance(r image.Rectangle, rad, x, y float64) float64 {
	cx, cy := float64(r.Min.X+r.Max.X)/2, float64(r.Min.Y+r.Max.Y)/2
	hx, hy := float64(r.Dx())/2-rad, float64(r.Dy())/2-rad
	qx, qy := math.Abs(x-cx)-hx, math.Abs(y-cy)-hy
	return math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - rad
}

package ui

// Karim's "stray line" (2026-11-22, #clack 1795380747.235139, Windows
// α.146): on the intro replay's "You're all set" a thin vertical line in
// the ring's colour hung down from the check badge's ring, from its 3
// o'clock point, where ringAt starts and closes its path. These render the
// badge through its pop, ringAt at several radii, widths, scales and
// sub-pixel centres, and the spinner through its whole turn, and fail on
// any drawn pixel off the ring's band.

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"runtime"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// What counts as drawn off a ring: a channel above seamInk, more than
// seamSlack px outside its band (antialiasing and Direct3D's soft edge
// stay within 3). On GL, 16/255: Gio patch 63's 32-bit coverage leaves no
// column at all (a 1% column shows as 25 on a dark background, sRGB).
// Direct3D 11 and Metal keep a column of under 1% (up to 21/255 on black)
// under any shape, filled or stroked, that 32-bit coverage doesn't remove
// (a separate Gio issue): there the tests catch a line from 32/255.
const seamSlack = 3.0

var seamInk = map[bool]uint8{true: 16, false: 32}[runtime.GOOS == "linux"]

// seamFrame is an intro frame at now on a w×h window, one device px per
// layout unit.
func seamFrame(ops *op.Ops, w, h int, now time.Time) *introFrame {
	gtx := layout.Context{Ops: ops, Now: now, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(w, h))}
	paint.FillShape(ops, color.NRGBA{A: 0xff}, clip.Rect{Max: image.Pt(w, h)}.Op())
	return &introFrame{gtx: gtx, in: &intro{}, mc: &motionClock{now: now}, now: now, reduced: false, u: 1, sz: image.Pt(w, h)}
}

// offBand lists the drawn pixels (a channel above min) farther than
// slack outside the band [inner, outer] around c, and whether any is.
func offBand(img *image.RGBA, c f32.Point, inner, outer, slack float64, min uint8) []string {
	var out []string
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			p := img.RGBAAt(x, y)
			if p.R <= min && p.G <= min && p.B <= min {
				continue
			}
			d := math.Hypot(float64(x)+0.5-float64(c.X), float64(y)+0.5-float64(c.Y))
			if d < inner-slack || d > outer+slack {
				out = append(out, fmt.Sprintf("(%d,%d) %v at %.1f", x, y, p, d))
			}
		}
	}
	return out
}

func reportOff(t *testing.T, what string, off []string) {
	t.Helper()
	if len(off) > 0 {
		t.Errorf("%s: %d pixels off the ring, first %v", what, len(off), off[:min(4, len(off))])
	}
}

// ringAt at several radii, widths and pop scales, on sub-pixel centres.
func TestRingSeamRingAt(t *testing.T) {
	const w, h = 320, 320
	for _, r := range []float32{6, 12, 20, 44, 52, 88, 104} {
		for _, sw := range []float32{1.6, 2, 2.4, 3} {
			for _, s := range []float32{0.6, 1, 1.18, 1.3} {
				for _, c := range []f32.Point{{X: 160, Y: 160}, {X: 160.37, Y: 159.71}, {X: 159.5, Y: 160.5}} {
					if (r+sw)*s > 150 {
						continue
					}
					var ops op.Ops
					f := seamFrame(&ops, w, h, time.Unix(1_800_000_000, 0))
					st := op.Affine(f32.Affine2D{}.Scale(c, f32.Pt(s, s))).Push(&ops)
					ringAt(f, c, r, color.NRGBA{R: 0xff, A: 0xff}, sw)
					st.Pop()
					img := pixeltest.Render(t, w, h, &ops)
					hw := float64(sw*s) / 2
					R := float64(r * s)
					reportOff(t, fmt.Sprintf("ringAt r %g w %g scale %g c %v", r, sw, s, c), offBand(img, c, R-hw, R+hw, seamSlack, seamInk))
				}
			}
		}
	}
}

// The "You're all set" badge through its pop, before its ripple: nothing
// drawn past its ring.
func TestRingSeamDoneBadge(t *testing.T) {
	const w, h = 400, 320
	start := time.Unix(1_800_000_000, 0)
	c := f32.Pt(200.5, 147.3) // the beat's sz.Y*0.40 - 20 lands off the pixel grid
	d := float32(88)
	for ms := 120; ms <= 740; ms += 20 {
		now := start.Add(time.Duration(ms) * time.Millisecond)
		var ops op.Ops
		f := seamFrame(&ops, w, h, now)
		e := f.pop(start.Add(msec(80)), 0.6, springBouncy) // the badge's scale, as drawDoneBadge pops it
		f.mc = &motionClock{now: now}
		drawDoneBadge(f, c, d, start, fx{a: 1, s: 1})
		img := pixeltest.Render(t, w, h, &ops)
		outer := float64(d/2+1.2) * float64(e.s)
		reportOff(t, fmt.Sprintf("badge at %dms (scale %.3f)", ms, e.s), offBand(img, c, 0, outer, seamSlack, seamInk))
	}
}

// The spinner (popups.InfiniteSpinner: its track's two half turns and
// its arcs) through a whole 1.2 s turn, every arc sweep: nothing off its
// ring.
func TestRingSeamSpinnerSweep(t *testing.T) {
	const w, h = 160, 160
	const d = 96
	o := image.Pt(32, 32)
	start := time.Unix(1_800_000_000, 0)
	for k := 0; k < 120; k++ {
		now := start.Add(time.Duration(k) * 10 * time.Millisecond)
		for _, noMargin := range []bool{false, true} {
			var ops op.Ops
			gtx := layout.Context{Ops: &ops, Now: now, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(w, h))}
			paint.FillShape(&ops, color.NRGBA{A: 0xff}, clip.Rect{Max: image.Pt(w, h)}.Op())
			st := op.Offset(o).Push(&ops)
			popups.InfiniteSpinner(gtx, d, noMargin, color.NRGBA{G: 0xff, A: 0xff}, color.NRGBA{R: 0xff, A: 0xff})
			st.Pop()
			img := pixeltest.Render(t, w, h, &ops)
			box := float64(100)
			if noMargin {
				box = 78
			}
			rr, hw := float64(d)*35/box, float64(d)*8/box/2
			c := f32.Pt(float32(o.X)+d/2, float32(o.Y)+d/2)
			reportOff(t, fmt.Sprintf("spinner at %dms (noMargin %v)", k*10, noMargin), offBand(img, c, rr-hw, rr+hw, seamSlack, seamInk))
		}
	}
}

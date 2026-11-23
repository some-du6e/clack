package ui

// Karim's "stray line" (2026-11-22, #clack 1795380747.235139, Windows
// α.146): on the intro replay's "You're all set" a thin vertical line in
// the ring's colour hung down from the check badge's ring, from its 3
// o'clock point, where ringAt starts and closes its path. These render the
// badge through its pop, ringAt at several radii, widths, scales and
// sub-pixel centres, and the spinner through its whole turn, and fail on
// any drawn pixel off the ring's band, on a stroke outline with a NaN
// point (Gio patch 62: the stroker runs on the CPU, so this holds on every
// backend, whatever its GPU does with NaN), and on a ring not drawn at its
// seam (3 o'clock) or across from it.

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/gpu"
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
// stay within 3). With 32-bit coverage (Gio patch 63, GL where it can be
// blended), 16/255: no column at all (a 1% column shows as 25 on a dark
// background, sRGB). With half float (Direct3D 11, Metal, a GL driver
// without EXT_float_blend) the stencil sums leave a column of under 1%
// (up to 23/255 on black) under any shape, filled or stroked: there the
// tests catch a line from 32/255.
const seamSlack = 3.0

var coverageOnce sync.Once

// seamInk is the bar for the coverage the renderer used; call it after a
// Render.
func seamInk(t *testing.T) uint8 {
	t.Helper()
	bits := gpu.ClackCoverageBits()
	coverageOnce.Do(func() {
		t.Logf("path coverage: %d-bit float, off-band ink from %d/255", bits, map[bool]int{true: 16, false: 32}[bits == 32])
	})
	if bits == 32 {
		return 16
	}
	return 32
}

// renderRing renders ops, failing what if a stroke outline built for it
// has a NaN point.
func renderRing(t *testing.T, what string, w, h int, ops *op.Ops) *image.RGBA {
	t.Helper()
	before := gpu.ClackStrokeNaNs()
	img := pixeltest.Render(t, w, h, ops)
	if n := gpu.ClackStrokeNaNs() - before; n > 0 {
		t.Errorf("%s: %d stroke outlines with NaN points", what, n)
	}
	return img
}

// linear is an sRGB channel in linear light.
func linear(v uint8) float64 {
	c := float64(v) / 255
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// bandInk is the ink (in linear light, px²: the brightest channel's) on a
// ring's band, centre c, radius R, half width hw, in the five rows through
// c at 3 o'clock (side 1, the seam) or 9 (side -1).
func bandInk(img *image.RGBA, c f32.Point, R, hw float64, side int) float64 {
	x0 := float64(c.X) + float64(side)*R
	y0 := int(math.Floor(float64(c.Y))) - 2
	var ink float64
	for y := y0; y < y0+5; y++ {
		for x := int(math.Floor(x0 - hw - 1.5)); x < int(math.Ceil(x0+hw+1.5)); x++ {
			p := img.RGBAAt(x, y)
			ink += max(linear(p.R), linear(p.G), linear(p.B))
		}
	}
	return ink
}

// reportDrawn fails what unless the band holds at least 60% of the ink a
// band 2·hw wide of colour alpha a across five rows does, at the seam and
// across from it.
func reportDrawn(t *testing.T, what string, img *image.RGBA, c f32.Point, R, hw, a float64) {
	t.Helper()
	want := 0.6 * 2 * hw * 5 * a
	for _, side := range []int{1, -1} {
		if ink := bandInk(img, c, R, hw, side); ink < want {
			t.Errorf("%s: %.2f px² of ink on the band at %d o'clock, want at least %.2f: the ring isn't drawn there", what, ink, map[int]int{1: 3, -1: 9}[side], want)
		}
	}
}

// CI's llvmpipe (linux-gio) blends 32-bit float: Gio patch 63 must pick
// R32F there, or the 16/255 bar falls back to 32 and nothing would notice.
func TestRingSeamCoverageBits(t *testing.T) {
	if runtime.GOOS != "linux" || os.Getenv("GITHUB_ACTIONS") != "true" {
		t.Skip("CI's linux-gio only: a local GL driver may lack EXT_float_blend")
	}
	var ops op.Ops
	f := seamFrame(&ops, 64, 64, time.Unix(1_800_000_000, 0))
	ringAt(f, f32.Pt(32, 32), 12, color.NRGBA{R: 0xff, A: 0xff}, 2)
	renderRing(t, "coverage", 64, 64, &ops)
	if bits := gpu.ClackCoverageBits(); bits != 32 {
		t.Errorf("path coverage in %d-bit float on llvmpipe, want 32 (Gio patch 63)", bits)
	}
}

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
					what := fmt.Sprintf("ringAt r %g w %g scale %g c %v", r, sw, s, c)
					img := renderRing(t, what, w, h, &ops)
					hw := float64(sw*s) / 2
					R := float64(r * s)
					reportOff(t, what, offBand(img, c, R-hw, R+hw, seamSlack, seamInk(t)))
					reportDrawn(t, what, img, c, R, hw, 1)
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
		what := fmt.Sprintf("badge at %dms (scale %.3f)", ms, e.s)
		img := renderRing(t, what, w, h, &ops)
		outer := float64(d/2+1.2) * float64(e.s)
		reportOff(t, what, offBand(img, c, 0, outer, seamSlack, seamInk(t)))
		// its ring: radius d/2, 2.4 wide, at 0.9 under the pop's opacity
		reportDrawn(t, what, img, c, float64(d/2*e.s), float64(1.2*e.s), 0.9*float64(min(e.a, 1)))
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
			what := fmt.Sprintf("spinner at %dms (noMargin %v)", k*10, noMargin)
			img := renderRing(t, what, w, h, &ops)
			box := float64(100)
			if noMargin {
				box = 78
			}
			rr, hw := float64(d)*35/box, float64(d)*8/box/2
			c := f32.Pt(float32(o.X)+d/2, float32(o.Y)+d/2)
			reportOff(t, what, offBand(img, c, rr-hw, rr+hw, seamSlack, seamInk(t)))
			reportDrawn(t, what, img, c, rr, hw, 0.25) // the track, at .25 (the arcs over it are brighter)
		}
	}
}

// floatShortRing is theme.RingPath's circle with its last arc ending one
// float step from its start in y, on the side whose closing midpoint
// rounds onto the start, and closed: Close adds a line one step long whose
// control point is its end. That's Karim's seam, what MoveTo + ArcTo(2π) +
// Close drew for some centres and radii (ArcTo's pen missing its start),
// built here the same on every architecture; ok is false where c.Y's
// float is odd (both midpoints round onto the pen).
func floatShortRing(ops *op.Ops, c f32.Point, r float32) (spec clip.PathSpec, ok bool) {
	const q = 0.5522847498307936
	k := r * q
	start := f32.Pt(c.X+r, c.Y)
	var pen f32.Point
	for _, dir := range []int{1, -1} {
		p := f32.Pt(start.X, math.Nextafter32(start.Y, float32(math.Inf(dir))))
		if p.Add(start).Mul(.5) == start {
			pen, ok = p, true
			break
		}
	}
	if !ok {
		return clip.PathSpec{}, false
	}
	var p clip.Path
	p.Begin(ops)
	p.MoveTo(start)
	p.CubeTo(f32.Pt(c.X+r, c.Y+k), f32.Pt(c.X+k, c.Y+r), f32.Pt(c.X, c.Y+r))
	p.CubeTo(f32.Pt(c.X-k, c.Y+r), f32.Pt(c.X-r, c.Y+k), f32.Pt(c.X-r, c.Y))
	p.CubeTo(f32.Pt(c.X-r, c.Y-k), f32.Pt(c.X-k, c.Y-r), f32.Pt(c.X, c.Y-r))
	p.CubeTo(f32.Pt(c.X+k, c.Y-r), f32.Pt(c.X+r, c.Y-k), pen)
	p.Close()
	return p.End(), true
}

// A ring whose path closes by a float step (Gio patch 62, on whatever GPU
// runs it): no NaN in its outline, nothing off its band, drawn at its seam.
// The intro's rings no longer close this way (theme.RingPath), but any
// path built from rounded arcs can.
func TestRingSeamCloseByFloatError(t *testing.T) {
	const w, h = 320, 320
	n := 0
	for _, c := range []f32.Point{{X: 160, Y: 160}, {X: 159.5, Y: 160.5}, {X: 160.37, Y: 159.71}, {X: 100.25, Y: 120}} {
		for _, r := range []float32{6, 12, 44, 88} {
			for _, sw := range []float32{1.6, 2.4, 3} {
				var ops op.Ops
				f := seamFrame(&ops, w, h, time.Unix(1_800_000_000, 0))
				spec, ok := floatShortRing(&ops, c, r)
				if !ok {
					continue
				}
				n++
				paint.FillShape(f.gtx.Ops, color.NRGBA{R: 0xff, A: 0xff}, clip.Stroke{Path: spec, Width: sw}.Op())
				what := fmt.Sprintf("float-short ring r %g w %g c %v", r, sw, c)
				img := renderRing(t, what, w, h, &ops)
				hw := float64(sw) / 2
				reportOff(t, what, offBand(img, c, float64(r)-hw, float64(r)+hw, seamSlack, seamInk(t)))
				reportDrawn(t, what, img, c, float64(r), hw, 1)
			}
		}
	}
	if n == 0 {
		t.Fatal("no centre whose float is even: nothing tested")
	}
}

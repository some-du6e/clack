// SPDX-License-Identifier: Unlicense OR MIT

package headless

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"testing"

	"gioui.org/f32"
	"gioui.org/gpu"
	"gioui.org/gpu/internal/driver"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// Clack patches 36-40: how exactly each GPU backend repeats a path's
// coverage when only its stencil region (the atlas placement) changes,
// and what the frame-cost patches change against the ops Clack drew
// before them. Logs, never fails; CLACK_GPU_COST_PROBE turns it on
// (flake-check's gpu-cost job).
func TestClackPlacementNoise(t *testing.T) {
	if os.Getenv("CLACK_GPU_COST_PROBE") == "" {
		t.Skip("CLACK_GPU_COST_PROBE not set")
	}
	const w, h = 1400, 900
	win, err := NewWindow(w, h)
	if err != nil {
		t.Skipf("no headless GPU: %v", err)
	}
	defer win.Release()
	win.ClackStatsDetail(true)
	shot := func(draw func(o *op.Ops)) (*image.RGBA, gpu.FrameStats) {
		var o op.Ops
		draw(&o)
		if err := win.Frame(&o); err != nil {
			t.Fatal(err)
		}
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		if err := win.Screenshot(img); err != nil {
			t.Fatal(err)
		}
		// a second frame of the same ops: its stats are the steady
		// state's (paths built the frame before)
		if err := win.Frame(&o); err != nil {
			t.Fatal(err)
		}
		return img, win.ClackFrameStats()
	}
	all := image.Rect(0, 0, w, h)
	bg := noiseGradient(w, h)
	ws := image.Rect(80, 60, w-30, h-30)
	rings := make([]clip.PathSpec, 15)
	for i := 1; i <= 14; i++ {
		ri := ws.Inset(-i)
		rings[i] = clip.UniformRRect(image.Rectangle{Max: ri.Size()}, 12+i).Path(new(op.Ops))
	}
	alpha := func(i int) uint8 {
		f := float32(17-i) / 16
		return uint8(49 * f * f)
	}
	ring := func(o *op.Ops, i int, c color.NRGBA) {
		ri := ws.Inset(-i)
		st := op.Offset(ri.Min).Push(o)
		paint.FillShape(o, c, clip.Stroke{Path: rings[i], Width: 1.5}.Op())
		st.Pop()
	}
	// the workspace shadow as Clack drew it: 14 full rings
	full := func(o *op.Ops, dummy bool) {
		bg(o)
		if dummy {
			// a path stenciled first: every later path lands elsewhere in the atlas
			paint.FillShape(o, color.NRGBA{R: 1, A: 1}, clip.Ellipse(image.Rect(3, 3, 41, 37)).Op(o))
		}
		for i := 1; i <= 14; i++ {
			ring(o, i, color.NRGBA{A: alpha(i)})
		}
	}
	// the same rings, each stenciled in the bands around it (theme's
	// ringBands)
	bands := func(o *op.Ops) {
		bg(o)
		for i := 1; i <= 14; i++ {
			ri := ws.Inset(-i)
			for _, b := range ringBandsProbe(ri, 12+i, 1) {
				c := clip.Rect(b).Push(o)
				ring(o, i, color.NRGBA{A: alpha(i)})
				c.Pop()
			}
		}
	}
	// the bands with the sides only as wide as the stroke (the columns
	// under the corners' arcs left out)
	narrow := func(o *op.Ops) {
		bg(o)
		for i := 1; i <= 14; i++ {
			ri := ws.Inset(-i)
			bs := ringBandsProbe(ri, 12+i, 1)
			bs[1].Max.X = ri.Min.X + 1
			bs[2].Min.X = ri.Max.X - 1
			for _, b := range bs {
				c := clip.Rect(b).Push(o)
				ring(o, i, color.NRGBA{A: alpha(i)})
				c.Pop()
			}
		}
	}
	a, sa := shot(func(o *op.Ops) { full(o, false) })
	a2, _ := shot(func(o *op.Ops) { full(o, false) })
	b, _ := shot(func(o *op.Ops) { full(o, true) })
	c, sc := shot(bands)
	cn, scn := shot(narrow)
	t.Logf("rings in bands, narrow sides: %s; %s", diffStats(a, cn, all), statLine(scn))
	t.Logf("rings, same ops twice: %s", diffStats(a, a2, all))
	t.Logf("rings after a dummy path (atlas shifted): %s", diffStats(a, b, image.Rect(45, 45, w, h)))
	t.Logf("rings in bands: %s", diffStats(a, c, all))
	t.Logf("rings, full: %s", statLine(sa))
	t.Logf("rings, in bands: %s", statLine(sc))

	// a nine-slice image of the same shadow, at best: its alpha read back
	// from the rings themselves over white, rounded to 8 bits, then one
	// blend over the background
	white, _ := shot(func(o *op.Ops) {
		paint.Fill(o, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
		paint.FillShape(o, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, clip.Rect(all).Op())
		for i := 1; i <= 14; i++ {
			ring(o, i, color.NRGBA{A: alpha(i)})
		}
	})
	mask := image.NewNRGBA(all)
	for i := 0; i < len(white.Pix); i += 4 {
		a := 1 - srgbToLinear(white.Pix[i])
		mask.Pix[i+3] = uint8(math.Round(a * 255))
	}
	maskOp := paint.NewImageOp(mask)
	maskOp.Filter = paint.FilterNearest
	d, _ := shot(func(o *op.Ops) {
		bg(o)
		maskOp.Add(o)
		paint.PaintOp{}.Add(o)
	})
	t.Logf("an 8-bit image of the rings (one blend) instead: %s", diffStats(a, d, all))

	// content in a rounded panel: glyph-like paths clipped by the panel's
	// RRect (with its interior, Clack patch 38) against the same clip as a
	// plain path (no interior: an intersection each, as before)
	panel := image.Rect(300, 80, w-60, h-60)
	rr := clip.RRect{Rect: panel, NE: 24, SE: 24}
	glyphs := func(o *op.Ops) {
		for k := 0; k < 300; k++ {
			x := 302 + (k%20)*52
			y := 82 + (k/20)*48
			r := image.Rect(x, y, x+9+k%7, y+11+k%5)
			paint.FillShape(o, color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xff}, clip.Ellipse(r).Op(o))
			var p clip.Path
			p.Begin(o)
			p.MoveTo(f32.Pt(float32(x)+12.3, float32(y)+1.7))
			p.QuadTo(f32.Pt(float32(x)+30.1, float32(y)+4.2), f32.Pt(float32(x)+21.6, float32(y)+15.9))
			p.LineTo(f32.Pt(float32(x)+14.2, float32(y)+13.1))
			p.Close()
			paint.FillShape(o, color.NRGBA{R: 0x61, G: 0x60, B: 0x61, A: 0xff}, clip.Outline{Path: p.End()}.Op())
		}
	}
	white1 := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	dummyFirst := false
	panelOld := func(o *op.Ops) {
		bg(o)
		if dummyFirst {
			paint.FillShape(o, color.NRGBA{R: 1, A: 1}, clip.Ellipse(image.Rect(3, 3, 41, 37)).Op(o))
		}
		st := clip.Outline{Path: plainRRect(o, rr)}.Op().Push(o)
		paint.FillShape(o, white1, clip.Rect(panel).Op())
		glyphs(o)
		st.Pop()
	}
	panelNew := func(o *op.Ops) {
		bg(o)
		st := rr.Push(o)
		in := rr.Interior()
		r := rr.Rect
		for _, f := range []image.Rectangle{in,
			image.Rect(r.Min.X, r.Min.Y, in.Min.X, r.Max.Y),
			image.Rect(in.Max.X, r.Min.Y, r.Max.X, r.Max.Y),
			image.Rect(in.Min.X, r.Min.Y, in.Max.X, in.Min.Y),
			image.Rect(in.Min.X, in.Max.Y, in.Max.X, r.Max.Y)} {
			paint.FillShape(o, white1, clip.Rect(f).Op())
		}
		glyphs(o)
		st.Pop()
	}
	e, se := shot(panelOld)
	dummyFirst = true
	e2, _ := shot(panelOld)
	dummyFirst = false
	f, sf := shot(panelNew)
	gpu.ClackLegacyClips(true)
	f2, _ := shot(panelNew)
	gpu.ClackLegacyClips(false)
	inner := rr.Interior()
	t.Logf("panel, before, after a dummy path (atlas shifted): %s", diffStats(e, e2, all))
	t.Logf("panel, its clip dropped inside (patch 38) and stenciled in regions (39): %s", diffStats(e, f, all))
	t.Logf("  in the interior: %s", diffStats(e, f, inner))
	t.Logf("  biggest: %s", worstPixels(e, f, all, 6))
	t.Logf("panel, split fill only (patches 38, 39 off): %s", diffStats(e, f2, all))
	// Metal's render commands run one at a time instead of batched (patch
	// 37): the same commands, so the same pixels
	driver.ClackBatchCommands = false
	f3, sf3 := shot(panelNew)
	c3, _ := shot(bands)
	driver.ClackBatchCommands = true
	t.Logf("panel, commands one by one instead of batched: %s; %s", diffStats(f, f3, all), statLine(sf3))
	t.Logf("rings in bands, commands one by one: %s", diffStats(c, c3, all))
	t.Logf("panel, before: %s", statLine(se))
	t.Logf("panel, after: %s", statLine(sf))

	// rounded rects recorded where they're drawn against cached at the
	// origin by size and radius and translated (theme.RRect, Circle and
	// Image since 2026-11-17, like theme.CachedRRect)
	var cacheOps op.Ops
	cached := map[[3]int]clip.PathSpec{}
	shapes := func(o *op.Ops, cache bool) {
		bg(o)
		for k := 0; k < 400; k++ {
			x := 7 + (k*37)%(w-60)
			y := 5 + (k*53)%(h-60)
			r := image.Rect(x, y, x+12+k%40, y+12+k%30)
			rad := 2 + k%9
			col := color.NRGBA{R: uint8(k * 7), G: 0xcc, B: uint8(255 - k), A: 0xff}
			if !cache {
				paint.FillShape(o, col, clip.UniformRRect(r, rad).Op(o))
				continue
			}
			key := [3]int{r.Dx(), r.Dy(), rad}
			spec, ok := cached[key]
			if !ok {
				spec = clip.UniformRRect(image.Rectangle{Max: r.Size()}, rad).Path(&cacheOps)
				cached[key] = spec
			}
			st := op.Offset(r.Min).Push(o)
			paint.FillShape(o, col, clip.Outline{Path: spec}.Op())
			st.Pop()
		}
	}
	// each recorded twice, as two frames are: the second frame builds
	// the paths the first didn't leave in the cache
	twice := func(cache bool) (*image.RGBA, gpu.FrameStats) {
		var o op.Ops
		shapes(&o, cache)
		if err := win.Frame(&o); err != nil {
			t.Fatal(err)
		}
		o.Reset()
		shapes(&o, cache)
		if err := win.Frame(&o); err != nil {
			t.Fatal(err)
		}
		st := win.ClackFrameStats()
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		if err := win.Screenshot(img); err != nil {
			t.Fatal(err)
		}
		return img, st
	}
	g, sg := twice(false)
	g2, sg2 := twice(true)
	t.Logf("400 rounded rects at their place vs cached at the origin, translated: %s", diffStats(g, g2, all))
	t.Logf("  the second frame built %d paths (%d KB) recorded where drawn, %d (%d KB) cached", sg.PathsBuilt, sg.PathBytes/1024, sg2.PathsBuilt, sg2.PathBytes/1024)
}

// plainRRect is rr's path built as a plain path: the same curves, no
// interior.
func plainRRect(o *op.Ops, rr clip.RRect) clip.PathSpec {
	const q = 4 * (math.Sqrt2 - 1) / 3
	const iq = 1 - q
	se, sw, nw, ne := float32(rr.SE), float32(rr.SW), float32(rr.NW), float32(rr.NE)
	w, n, e, s := float32(rr.Rect.Min.X), float32(rr.Rect.Min.Y), float32(rr.Rect.Max.X), float32(rr.Rect.Max.Y)
	var p clip.Path
	p.Begin(o)
	p.MoveTo(f32.Point{X: w + nw, Y: n})
	p.LineTo(f32.Point{X: e - ne, Y: n})
	p.CubeTo(f32.Point{X: e - ne*iq, Y: n}, f32.Point{X: e, Y: n + ne*iq}, f32.Point{X: e, Y: n + ne})
	p.LineTo(f32.Point{X: e, Y: s - se})
	p.CubeTo(f32.Point{X: e, Y: s - se*iq}, f32.Point{X: e - se*iq, Y: s}, f32.Point{X: e - se, Y: s})
	p.LineTo(f32.Point{X: w + sw, Y: s})
	p.CubeTo(f32.Point{X: w + sw*iq, Y: s}, f32.Point{X: w, Y: s - sw*iq}, f32.Point{X: w, Y: s - sw})
	p.LineTo(f32.Point{X: w, Y: n + nw})
	p.CubeTo(f32.Point{X: w, Y: n + nw*iq}, f32.Point{X: w + nw*iq, Y: n}, f32.Point{X: w + nw, Y: n})
	return p.End()
}

// worstPixels lists the n pixels of r that differ most between a and b.
func worstPixels(a, b *image.RGBA, r image.Rectangle, n int) string {
	type px struct {
		p    image.Point
		d    int
		a, b color.RGBA
	}
	var top []px
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			ca, cb := a.RGBAAt(x, y), b.RGBAAt(x, y)
			d := max(absDiff(ca.R, cb.R), absDiff(ca.G, cb.G), absDiff(ca.B, cb.B), absDiff(ca.A, cb.A))
			if d == 0 || (len(top) == n && d <= top[n-1].d) {
				continue
			}
			top = append(top, px{image.Pt(x, y), d, ca, cb})
			for i := len(top) - 1; i > 0 && top[i].d > top[i-1].d; i-- {
				top[i], top[i-1] = top[i-1], top[i]
			}
			if len(top) > n {
				top = top[:n]
			}
		}
	}
	s := ""
	for _, t := range top {
		s += fmt.Sprintf(" %v by %d (%v vs %v);", t.p, t.d, t.a, t.b)
	}
	return s
}

func absDiff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

func statLine(s gpu.FrameStats) string {
	return fmt.Sprintf("%d draws, %d passes, %d API calls (%d left out, %d crossings), %d blits, %d covers, %d intersections, %d paths stenciled; stencil %.2f Mpx, cover %.2f, blit %.2f, intersect %.2f, cleared %.2f, atlas %.2f",
		s.Draws, s.Passes, s.APICalls, s.Skipped, s.Crossings, s.Blits, s.Covers, s.Intersections, s.StencilPaths,
		float64(s.StencilPx)/1e6, float64(s.CoverPx)/1e6, float64(s.BlitPx)/1e6, float64(s.IntersectPx)/1e6, float64(s.ClearPx)/1e6, float64(s.AtlasPx)/1e6)
}

func srgbToLinear(v uint8) float64 {
	c := float64(v) / 255
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// ringBandsProbe is theme's ringBands for a ring stroked on r (radius
// rad, bounds grown by m): the top and bottom with the corners, the
// sides as wide as the corners.
func ringBandsProbe(r image.Rectangle, rad, m int) []image.Rectangle {
	e := max(rad, m) + 1
	box := r.Inset(-m)
	return []image.Rectangle{
		image.Rect(box.Min.X, box.Min.Y, box.Max.X, r.Min.Y+e),
		image.Rect(box.Min.X, r.Min.Y+e, r.Min.X+e, r.Max.Y-e),
		image.Rect(r.Max.X-e, r.Min.Y+e, box.Max.X, r.Max.Y-e),
		image.Rect(box.Min.X, r.Max.Y-e, box.Max.X, box.Max.Y),
	}
}

// noiseGradient paints a smooth, aubergine-like background: a small image
// upsampled ×8 with linear filtering, like Clack's window gradient.
func noiseGradient(w, h int) func(o *op.Ops) {
	sw, sh := w/8+1, h/8+1
	src := image.NewRGBA(image.Rect(0, 0, sw, sh))
	for y := 0; y < sh; y++ {
		for x := 0; x < sw; x++ {
			fx, fy := float64(x)/float64(sw), float64(y)/float64(sh)
			r := 0x3f + 0x30*math.Sin(fx*3.1+fy*1.3)
			g := 0x0e + 0x0a*math.Cos(fx*2.2-fy*2.9)
			b := 0x40 + 0x28*math.Sin(fy*4.1+fx*0.7)
			src.Pix[(y*sw+x)*4+0] = uint8(r)
			src.Pix[(y*sw+x)*4+1] = uint8(g)
			src.Pix[(y*sw+x)*4+2] = uint8(b)
			src.Pix[(y*sw+x)*4+3] = 0xff
		}
	}
	img := paint.NewImageOp(src)
	img.Filter = paint.FilterLinear
	return func(o *op.Ops) {
		st := op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(8, 8))).Push(o)
		img.Add(o)
		paint.PaintOp{}.Add(o)
		st.Pop()
	}
}

func diffStats(a, b *image.RGBA, r image.Rectangle) string {
	n, maxd := 0, 0
	hist := [4]int{}
	var first image.Point
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i := a.PixOffset(x, y)
			d := 0
			for k := 0; k < 4; k++ {
				v := int(a.Pix[i+k]) - int(b.Pix[i+k])
				if v < 0 {
					v = -v
				}
				if v > d {
					d = v
				}
			}
			if d > 0 {
				if n == 0 {
					first = image.Pt(x, y)
				}
				n++
				if d > maxd {
					maxd = d
				}
				if d < 4 {
					hist[d]++
				} else {
					hist[0]++
				}
			}
		}
	}
	if n == 0 {
		return fmt.Sprintf("identical (%d px)", r.Dx()*r.Dy())
	}
	return fmt.Sprintf("%d of %d px differ, max %d (by 1: %d, by 2: %d, by 3: %d, more: %d), first at %v", n, r.Dx()*r.Dy(), maxd, hist[1], hist[2], hist[3], hist[0], first)
}

// Clack patch 40: an atlas page grown for one big frame is made to size
// again once frames have needed far less for a while.
func TestClackAtlasShrinks(t *testing.T) {
	const w, h = 1000, 800
	win, err := NewWindow(w, h)
	if err != nil {
		if os.Getenv("CLACK_PIXEL_TESTS") != "" {
			t.Fatalf("headless GPU: %v", err)
		}
		t.Skipf("no headless GPU: %v", err)
	}
	defer win.Release()
	frame := func(big bool) gpu.FrameStats {
		var o op.Ops
		r := image.Rect(10, 10, 60, 40)
		if big {
			r = image.Rect(0, 0, w, h)
		}
		paint.FillShape(&o, color.NRGBA{R: 0xff, A: 0xff}, clip.UniformRRect(r, 8).Op(&o))
		if err := win.Frame(&o); err != nil {
			t.Fatal(err)
		}
		return win.ClackFrameStats()
	}
	big := frame(true).AtlasPx
	var small gpu.FrameStats
	for range 70 {
		small = frame(false)
	}
	t.Logf("atlas %d px after the big frame, %d after 70 small ones", big, small.AtlasPx)
	if small.AtlasPx*4 > big {
		t.Errorf("the atlas stayed at %d px of the %d px the big frame needed", small.AtlasPx, big)
	}
}

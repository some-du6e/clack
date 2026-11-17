// SPDX-License-Identifier: Unlicense OR MIT

package headless

import (
	"image"
	"image/color"
	"math"
	"os"
	"testing"

	"gioui.org/f32"
	"gioui.org/gpu"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// Clack patches 36-43: how exactly each GPU backend repeats a path's
// coverage when only its stencil region (its place in the atlas) changes,
// and what each frame-cost patch changes against Gio's old way
// (gpu.ClackLegacyParts), in the shapes Clack draws: the workspace's
// shadow as theme.Shadow draws it at 1x (8 rings of 1.5 px, radius 6+i,
// PanelShadow's alpha after LinearAlpha), text-like paths in a rounded
// panel, rounded rects, icons. A probe: it logs and never fails
// (CLACK_GPU_COST_PROBE, flake-check's gpu-cost job). The pixel checks
// that fail are in clack_identity_test.go and internal/ui/theme's
// TestFrameCostPixels.
func TestClackPlacementNoise(t *testing.T) {
	if os.Getenv("CLACK_GPU_COST_PROBE") == "" {
		t.Skip("CLACK_GPU_COST_PROBE not set")
	}
	const w, h = 1400, 900
	win := clackWindow(t, w, h)
	defer win.Release()
	win.ClackStatsDetail(true)
	all := image.Rect(0, 0, w, h)
	bg := noiseGradient(w, h)
	ws := image.Rect(68, 38, w-6, h-6)
	const spread = 8
	rings := make([]clip.PathSpec, spread+1)
	var keep op.Ops
	for i := 1; i <= spread; i++ {
		ri := ws.Inset(-i)
		rings[i] = clip.UniformRRect(image.Rectangle{Max: ri.Size()}, 6+i).Path(&keep)
	}
	alpha := func(i int) uint8 {
		f := float32(spread-i+1) / spread
		return uint8(0x31 * f * f)
	}
	shadow := func(o *op.Ops) {
		for i := 1; i <= spread; i++ {
			ri := ws.Inset(-i)
			st := op.Offset(ri.Min).Push(o)
			paint.FillShape(o, color.NRGBA{A: alpha(i)}, clip.Stroke{Path: rings[i], Width: 1.5}.Op())
			st.Pop()
		}
	}
	legacy := func(f func()) {
		gpu.ClackLegacyClips(true)
		defer gpu.ClackLegacyClips(false)
		f()
	}
	var a, a2, b, c *image.RGBA
	var sa, sc gpu.FrameStats
	legacy(func() {
		a, sa = clackShot(t, win, func(o *op.Ops) { bg(o); shadow(o) })
		a2, _ = clackShot(t, win, func(o *op.Ops) { bg(o); shadow(o) })
		b, _ = clackShot(t, win, func(o *op.Ops) {
			bg(o)
			// a path stenciled first: every later path lands elsewhere
			paint.FillShape(o, color.NRGBA{}, clip.Ellipse(image.Rect(3, 3, 42, 37)).Op(o))
			shadow(o)
		})
	})
	c, sc = clackShot(t, win, func(o *op.Ops) { bg(o); shadow(o) })
	t.Logf("shadow, the same ops twice: %s", diffStats(a, a2, all))
	t.Logf("shadow after a transparent path first (the atlas moved): %s", diffStats(a, b, all))
	t.Logf("shadow drawn around its rings' holes (patch 42): %s", diffStats(a, c, all))
	t.Logf("shadow, the old way: %s", statLine(sa))
	t.Logf("shadow, around the holes: %s", statLine(sc))

	// a nine-slice image of the same shadow, at best: its alpha read back
	// from the rings themselves over white, rounded to 8 bits, then one
	// blend over the background
	var white *image.RGBA
	legacy(func() {
		white, _ = clackShot(t, win, func(o *op.Ops) {
			paint.FillShape(o, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, clip.Rect(all).Op())
			shadow(o)
		})
	})
	mask := image.NewNRGBA(all)
	for i := 0; i < len(white.Pix); i += 4 {
		mask.Pix[i+3] = uint8(math.Round((1 - srgbToLinear(white.Pix[i])) * 255))
	}
	maskOp := paint.NewImageOp(mask)
	maskOp.Filter = paint.FilterNearest
	d, _ := clackShot(t, win, func(o *op.Ops) {
		bg(o)
		maskOp.Add(o)
		paint.PaintOp{}.Add(o)
	})
	t.Logf("an 8-bit image of the shadow (one blend) instead: %s", diffStats(a, d, all))

	// Clack's window in small: each part of the clip work against the old
	// way, and the old way against itself with the atlas moved
	scene := clackScene(w, h, f32.Affine2D{})
	part := func(interiors, regions, splits bool) (*image.RGBA, gpu.FrameStats) {
		gpu.ClackLegacyParts(interiors, regions, splits)
		defer gpu.ClackLegacyClips(false)
		return clackShot(t, win, scene)
	}
	old, so := part(true, true, true)
	gpu.ClackLegacyClips(true)
	moved, _ := clackShot(t, win, func(o *op.Ops) {
		paint.FillShape(o, color.NRGBA{}, clip.Ellipse(image.Rect(1, 1, 467, 301)).Op(o))
		scene(o)
	})
	gpu.ClackLegacyClips(false)
	cur, sn := part(false, false, false)
	t.Logf("window, the old way after a transparent path first: %s", diffStats(old, moved, all))
	t.Logf("window, new against old: %s", diffStats(old, cur, all))
	t.Logf("  worst: %s", worstPixels(old, cur, all, 6))
	for _, p := range []struct {
		what                       string
		interiors, regions, splits bool
	}{
		{"interiors alone (38)", false, true, true},
		{"regions alone (39)", true, false, true},
		{"splits alone (42)", true, true, false},
	} {
		img, _ := part(p.interiors, p.regions, p.splits)
		t.Logf("window, %s: %s", p.what, diffStats(old, img, all))
	}
	t.Logf("window, the old way: %s", statLine(so))
	t.Logf("window, new: %s", statLine(sn))

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
		img := image.NewRGBA(all)
		if err := win.Screenshot(img); err != nil {
			t.Fatal(err)
		}
		return img, st
	}
	g, sg := twice(false)
	g2, sg2 := twice(true)
	t.Logf("400 rounded rects at their place vs cached at the origin, translated: %s", diffStats(g, g2, all))
	t.Logf("  the second frame built %d paths (%d KB) recorded where drawn, %d (%d KB) cached", sg.PathsBuilt, sg.PathBytes/1024, sg2.PathsBuilt, sg2.PathBytes/1024)

	// thin strokes (icons) inside a big rounded clip: the old way (an
	// intersection with the clip's path each), the new way (the clip left
	// off inside its interior, patch 38), each against the same strokes
	// with no clip at all; that frame again after one more path drawn
	// first is the floor
	icons := func(o *op.Ops) {
		col := color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xff}
		for k := 0; k < 200; k++ {
			x := 320 + (k%20)*50
			y := 100 + (k/20)*70
			paint.FillShape(o, col, clip.Stroke{Path: clip.Ellipse(image.Rect(x, y, x+14+k%5, y+14+k%5)).Path(o), Width: 1.5}.Op())
			paint.FillShape(o, col, clip.Stroke{Path: clip.UniformRRect(image.Rect(x+20, y, x+36, y+16), 3).Path(o), Width: 1.5}.Op())
			var p clip.Path
			p.Begin(o)
			cx, cy := float32(x)+8.3, float32(y)+32.6
			for i := 0; i <= 16; i++ {
				a := float64(i) * math.Pi / 8
				r := float32(7)
				if i%2 == 1 {
					r = 5
				}
				pt := f32.Pt(cx+r*float32(math.Cos(a)), cy+r*float32(math.Sin(a)))
				if i == 0 {
					p.MoveTo(pt)
				} else {
					p.LineTo(pt)
				}
			}
			p.Close()
			paint.FillShape(o, col, clip.Stroke{Path: p.End(), Width: 1.5}.Op())
		}
	}
	irr := clip.UniformRRect(image.Rect(300, 80, w-60, h-60), 24)
	white1 := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	iconScene := func(clipped, dummy bool) func(o *op.Ops) {
		return func(o *op.Ops) {
			bg(o)
			if dummy {
				paint.FillShape(o, color.NRGBA{}, clip.Ellipse(image.Rect(1, 1, 427, 301)).Op(o))
			}
			paint.FillShape(o, white1, clip.Rect(irr.Rect).Op())
			if clipped {
				st := irr.Push(o)
				icons(o)
				st.Pop()
				return
			}
			icons(o)
		}
	}
	var iOld, iRef, iRef2 *image.RGBA
	legacy(func() {
		iOld, _ = clackShot(t, win, iconScene(true, false))
		iRef, _ = clackShot(t, win, iconScene(false, false))
		iRef2, _ = clackShot(t, win, iconScene(false, true))
	})
	iNew, _ := clackShot(t, win, iconScene(true, false))
	t.Logf("icons in a rounded clip, the old way (an intersection each) against no clip: %s", diffStats(iRef, iOld, all))
	t.Logf("  the new way (the clip left off inside) against no clip: %s", diffStats(iRef, iNew, all))
	t.Logf("  no clip after one more path first (the floor): %s", diffStats(iRef, iRef2, all))
	t.Logf("  the old way's worst: %s", worstPixels(iRef, iOld, all, 4))
}

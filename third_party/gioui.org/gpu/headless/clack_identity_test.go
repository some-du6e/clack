// SPDX-License-Identifier: Unlicense OR MIT

package headless

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/f32"
	"gioui.org/gpu"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// Pixel checks of the frame-cost patches that must hold exactly on every
// backend (ci's linux-gio, macos and windows jobs; CLACK_PIXEL_TESTS).

// Clack patch 37: the backends' state caches and Metal's command
// batching leave out calls, never change a pixel. With them off every
// call Gio made is made: the same draws in the same order, so any pixel
// that differs is a cache bug. Checked on the first frame after turning
// them on and on the next, at an integer offset, under a scale and at a
// fraction of a pixel.
func TestClackStateCacheIdentical(t *testing.T) {
	const w, h = 1000, 700
	win := clackWindow(t, w, h)
	defer win.Release()
	defer gpu.ClackStateCaches(true)
	for _, c := range []struct {
		name string
		tr   f32.Affine2D
	}{
		{"at an integer offset", f32.Affine2D{}},
		{"scaled by 1.37", f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(1.37, 1.37))},
		{"at a fraction of a pixel", f32.Affine2D{}.Offset(f32.Pt(0.375, 0.625))},
	} {
		scene := clackScene(w, h, c.tr)
		gpu.ClackStateCaches(false)
		off, soff := clackShot(t, win, scene)
		gpu.ClackStateCaches(true)
		on, son := clackFrames(t, win, 2, scene)
		for i, img := range on {
			if n, m := diffCount(off, img); n > 0 {
				t.Errorf("%s, frame %d with the caches on: %d px differ by up to %d levels:%s", c.name, i+1, n, m, worstPixels(off, img, img.Bounds(), 5))
			}
		}
		if son[1].APICalls >= 0 {
			if soff.Skipped != 0 {
				t.Errorf("%s: with the caches off %d calls were left out", c.name, soff.Skipped)
			}
			if son[1].Skipped == 0 || son[1].APICalls >= soff.APICalls {
				t.Errorf("%s: with the caches on %d calls, %d left out; off %d: the caches didn't run", c.name, son[1].APICalls, son[1].Skipped, soff.APICalls)
			}
		}
		t.Logf("%s: caches off: %s", c.name, statLine(soff))
		t.Logf("%s: caches on: %s", c.name, statLine(son[1]))
	}
}

// Clack patch 40: opacity layers of one depth share an atlas page. The
// window's nested layers (a hover fade holding three siblings, one with a
// layer of its own, over a rounded clip) come out exactly as with a page
// for each nested layer, in fewer pages.
func TestClackLayerPages(t *testing.T) {
	const w, h = 1000, 700
	scene := clackScene(w, h, f32.Affine2D{})
	render := func(legacy bool) (*image.RGBA, gpu.FrameStats) {
		win := clackWindow(t, w, h)
		defer win.Release()
		gpu.ClackLegacyLayers(legacy)
		defer gpu.ClackLegacyLayers(false)
		return clackShot(t, win, scene)
	}
	old, so := render(true)
	cur, sn := render(false)
	if n, m := diffCount(old, cur); n > 0 {
		t.Errorf("layers sharing pages: %d px differ by up to %d levels:%s", n, m, worstPixels(old, cur, cur.Bounds(), 5))
	}
	if sn.LayerPages >= so.LayerPages {
		t.Errorf("%d layer pages, %d with a page each: none shared", sn.LayerPages, so.LayerPages)
	}
	t.Logf("layer pages: %d (%d px), a page each: %d (%d px)", sn.LayerPages, sn.LayerPx, so.LayerPages, so.LayerPx)
}

// Clack patch 43: a path cached at the origin and drawn at a fraction of
// a pixel is built at that fraction, though the same path was drawn at a
// whole-pixel offset first (a card at rest, then sliding): it matches the
// same path recorded afresh at that fraction.
func TestClackSubPixelCache(t *testing.T) {
	const w, h = 200, 120
	win := clackWindow(t, w, h)
	defer win.Release()
	var keep op.Ops
	spec := clip.UniformRRect(image.Rect(0, 0, 36, 36), 8).Path(&keep)
	draw := func(tr f32.Affine2D, fresh bool) func(o *op.Ops) {
		return func(o *op.Ops) {
			paint.FillShape(o, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, clip.Rect(image.Rect(0, 0, w, h)).Op())
			defer op.Affine(tr).Push(o).Pop()
			s := spec
			if fresh {
				s = clip.UniformRRect(image.Rect(0, 0, 36, 36), 8).Path(o)
			}
			paint.FillShape(o, color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xff}, clip.Outline{Path: s}.Op())
		}
	}
	frac := f32.Affine2D{}.Offset(f32.Pt(20.375, 30.625))
	clackShot(t, win, draw(f32.Affine2D{}.Offset(f32.Pt(20, 30)), false))
	got, _ := clackShot(t, win, draw(frac, false))
	want, _ := clackShot(t, win, draw(frac, true))
	if n, m := diffCount(got, want); n > 0 {
		t.Errorf("the cached path at (20.375, 30.625): %d px differ by up to %d levels from it recorded there afresh (drawn at the first use's fraction?):%s", n, m, worstPixels(got, want, got.Bounds(), 5))
	}
}

// Clack patch 42: Gio splits an op into parts only at whole-pixel
// offsets. Under a scale or a fraction of a pixel (inspect shots,
// animations) the window draws exactly as it does with the splits off:
// no part's antialiased edge meets another's.
func TestClackSplitsUnderTransform(t *testing.T) {
	const w, h = 1000, 700
	win := clackWindow(t, w, h)
	defer win.Release()
	for _, c := range []struct {
		name string
		tr   f32.Affine2D
	}{
		{"scaled by 1.37", f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(1.37, 1.37))},
		{"as an inspect shot", f32.Affine2D{}.Offset(f32.Pt(-120, -80)).Scale(f32.Point{}, f32.Pt(1.963, 1.963))},
		{"at a fraction of a pixel", f32.Affine2D{}.Offset(f32.Pt(0.4, 0.7))},
	} {
		scene := clackScene(w, h, c.tr)
		gpu.ClackLegacyParts(false, false, true)
		old, so := clackShot(t, win, scene)
		gpu.ClackLegacyClips(false)
		cur, sn := clackShot(t, win, scene)
		if n, m := diffCount(old, cur); n > 0 {
			t.Errorf("%s: with the splits %d px differ by up to %d levels:%s", c.name, n, m, worstPixels(old, cur, cur.Bounds(), 5))
		}
		if sn.Blits+sn.Covers+sn.Intersections != so.Blits+so.Covers+so.Intersections {
			t.Errorf("%s: %d ops drawn with the splits, %d without", c.name, sn.Blits+sn.Covers+sn.Intersections, so.Blits+so.Covers+so.Intersections)
		}
	}
}

// Clack patch 40: an atlas page grown for one big frame stays for 59
// small ones and is made to size on the 60th, a big frame grows it back
// at once, and a page no frame needs, as when a frame has no path at all,
// goes after 60 frames and not before. The same goes for a layer page.
func TestClackAtlasShrinks(t *testing.T) {
	const w, h = 1000, 800
	win := clackWindow(t, w, h)
	defer win.Release()
	red := color.NRGBA{R: 0xff, A: 0xff}
	frame := func(big, path bool) gpu.FrameStats {
		var o op.Ops
		r := image.Rect(10, 10, 60, 40)
		if big {
			r = image.Rect(0, 0, w, h)
		}
		if path {
			paint.FillShape(&o, red, clip.UniformRRect(r, 8).Op(&o))
		} else {
			paint.FillShape(&o, red, clip.Rect(r).Op())
		}
		if err := win.Frame(&o); err != nil {
			t.Fatal(err)
		}
		return win.ClackFrameStats()
	}
	big := frame(true, true).AtlasPx
	for i := 1; i < 60; i++ {
		if px := frame(false, true).AtlasPx; px != big {
			t.Fatalf("small frame %d: the atlas is %d px, want the %d px it had kept until the 60th", i, px, big)
		}
	}
	if small := frame(false, true).AtlasPx; small*4 > big {
		t.Errorf("small frame 60: the atlas is %d px of the %d px the big frame needed", small, big)
	}
	if px := frame(true, true).AtlasPx; px != big {
		t.Errorf("a big frame after: the atlas is %d px, want %d at once", px, big)
	}
	for i := 1; i < 60; i++ {
		if px := frame(false, false).AtlasPx; px != big {
			t.Fatalf("frame %d without a path: the atlas is %d px, want the %d px kept until the 60th", i, px, big)
		}
	}
	if px := frame(false, false).AtlasPx; px != 0 {
		t.Errorf("after 60 frames without a path the atlas is %d px, want none", px)
	}

	layers := func(nested bool) gpu.FrameStats {
		var o op.Ops
		l0 := paint.PushOpacity(&o, 0.8)
		paint.FillShape(&o, red, clip.Rect(image.Rect(0, 0, 300, 200)).Op())
		if nested {
			l1 := paint.PushOpacity(&o, 0.5)
			paint.FillShape(&o, color.NRGBA{B: 0xff, A: 0xff}, clip.Rect(image.Rect(50, 50, 250, 150)).Op())
			l1.Pop()
		}
		l0.Pop()
		if err := win.Frame(&o); err != nil {
			t.Fatal(err)
		}
		return win.ClackFrameStats()
	}
	two := layers(true)
	if two.LayerPages != 2 {
		t.Fatalf("a layer in a layer drew into %d pages, want 2", two.LayerPages)
	}
	for i := 1; i < 60; i++ {
		if s := layers(false); s.LayerPx != two.LayerPx {
			t.Fatalf("frame %d with one layer: the layer pages are %d px, want the %d px kept until the 60th", i, s.LayerPx, two.LayerPx)
		}
	}
	if s := layers(false); s.LayerPx >= two.LayerPx {
		t.Errorf("after 60 frames with one layer the pages are %d px, want less than %d: the second page stays", s.LayerPx, two.LayerPx)
	}
}

// Clack patch 40 with 44: a page whose set only some frames use shrinks as
// well. A frame that doesn't use the page needs none of it and counts
// toward its 60 frames; such frames used to reset the count, so a page
// drawn into every other frame kept its biggest size for good.
func TestClackAtlasShrinksUsedNowAndThen(t *testing.T) {
	const w, h = 1000, 800
	win := clackWindow(t, w, h)
	defer win.Release()
	red := color.NRGBA{R: 0xff, A: 0xff}
	frame := func(big, path bool) gpu.FrameStats {
		var o op.Ops
		r := image.Rect(10, 10, 60, 40)
		if big {
			r = image.Rect(0, 0, w, h)
		}
		if path {
			paint.FillShape(&o, red, clip.UniformRRect(r, 8).Op(&o))
		} else {
			paint.FillShape(&o, red, clip.Rect(r).Op())
		}
		if err := win.Frame(&o); err != nil {
			t.Fatal(err)
		}
		return win.ClackFrameStats()
	}
	big := frame(true, true).AtlasPx
	shrunk := 0
	for i := 1; i <= 120; i++ {
		// a small path every other frame, nothing to stencil in between
		if px := frame(false, i%2 == 0).AtlasPx; px*4 <= big && px > 0 {
			shrunk = i
			break
		}
	}
	if shrunk == 0 {
		t.Fatalf("after 120 frames drawing a small path every other frame the atlas still holds the big frame's %d px", big)
	}
	if shrunk < 60 {
		t.Errorf("the atlas shrank after %d frames, before the 60th", shrunk)
	}
}

// Clack patch 44: a page that grows keeps the dimension that still fits.
// Gio made the texture just the new need, so a page whose needs alternate
// between wide and tall was made again on every frame that used it: here
// the atlas would flip between a wide page and a tall one.
func TestClackAtlasGrowKeepsSize(t *testing.T) {
	const w, h = 1000, 800
	win := clackWindow(t, w, h)
	defer win.Release()
	red := color.NRGBA{R: 0xff, A: 0xff}
	frame := func(r image.Rectangle) int64 {
		var o op.Ops
		paint.FillShape(&o, red, clip.UniformRRect(r, 8).Op(&o))
		if err := win.Frame(&o); err != nil {
			t.Fatal(err)
		}
		return win.ClackFrameStats().AtlasPx
	}
	wide, tall := image.Rect(0, 0, 900, 40), image.Rect(0, 0, 300, 600)
	frame(wide)
	both := frame(tall)
	for i := 0; i < 8; i++ {
		r, name := wide, "wide"
		if i%2 == 1 {
			r, name = tall, "tall"
		}
		if px := frame(r); px != both {
			t.Fatalf("frame %d (%s): the atlas is %d px, want the %d px it grew to for both shapes: it was made again", i+3, name, px, both)
		}
	}
}

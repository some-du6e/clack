// SPDX-License-Identifier: Unlicense OR MIT

package headless

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// TestClackSetSize: one window draws frames of three sizes (Clack patch
// 52), each pixel for pixel what a new window of that size draws, on the
// same GPU, its caches kept: two paths recorded once into ops of their own
// (the GPU caches a path by its op's key: its Ops, its place there and the
// Ops' version) are drawn in every frame and built only in the first, so
// a frame after a SetSize builds only the one path its own ops hold. The
// new windows draw first, each released before the next is made: on EGL
// two headless windows can't outlive each other (Release terminates the
// default display under the other).
func TestClackSetSize(t *testing.T) {
	var fixed op.Ops // recorded once: every frame draws the same ops
	ell := clip.Ellipse(image.Rect(6, 20, 26, 40)).Path(&fixed)
	ring := clip.UniformRRect(image.Rect(4, 18, 28, 42), 5).Path(&fixed)
	scene := func(o *op.Ops, sz image.Point) {
		paint.FillShape(o, color.NRGBA{R: 0x20, G: 0x40, B: 0x60, A: 0xff}, clip.Rect{Max: sz}.Op())
		paint.FillShape(o, color.NRGBA{R: 0xf0, G: 0x10, B: 0x30, A: 0xff}, clip.Rect(image.Rect(sz.X/4, sz.Y/3, sz.X*3/4, sz.Y*2/3)).Op())
		// the frame's own path: built every frame
		paint.FillShape(o, color.NRGBA{G: 0xc0, B: 0x20, A: 0xff}, clip.Ellipse(image.Rect(2, 2, 2+sz.Y/2, 2+sz.Y/2)).Op(o))
		// the cached ones
		paint.FillShape(o, color.NRGBA{R: 0xe0, G: 0xe0, B: 0x40, A: 0xff}, clip.Outline{Path: ell}.Op())
		paint.FillShape(o, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, clip.Stroke{Path: ring, Width: 1.5}.Op())
	}
	shot := func(w *Window, sz image.Point) *image.RGBA {
		t.Helper()
		var o op.Ops
		scene(&o, sz)
		if err := w.Frame(&o); err != nil {
			t.Fatal(err)
		}
		img := image.NewRGBA(image.Rectangle{Max: sz})
		if err := w.Screenshot(img); err != nil {
			t.Fatal(err)
		}
		return img
	}
	sizes := []image.Point{{320, 180}, {37, 91}, {64, 48}}
	want := map[image.Point]*image.RGBA{}
	for _, sz := range sizes {
		fresh := clackWindow(t, sz.X, sz.Y)
		want[sz] = shot(fresh, sz)
		if n := fresh.ClackFrameStats().PathsBuilt; n != 3 {
			t.Errorf("a new %v window's first frame built %d paths, want all 3", sz, n)
		}
		fresh.Release()
	}
	win := clackWindow(t, 64, 48)
	defer win.Release()
	shot(win, image.Pt(64, 48))
	gp := win.gpu
	for _, sz := range sizes {
		if err := win.SetSize(sz.X, sz.Y); err != nil {
			t.Fatalf("SetSize %v: %v", sz, err)
		}
		if win.Size() != sz || win.gpu != gp {
			t.Fatalf("after SetSize %v: size %v, same GPU %v", sz, win.Size(), win.gpu == gp)
		}
		got := shot(win, sz)
		if n := win.ClackFrameStats().PathsBuilt; n != 1 {
			t.Errorf("after SetSize %v: the frame built %d paths, want 1 (the frame's own; the 2 drawn every frame kept in the GPU's cache across the resize)", sz, n)
		}
		diff := 0
		for i := range got.Pix {
			if got.Pix[i] != want[sz].Pix[i] {
				diff++
			}
		}
		if diff > 0 {
			t.Errorf("%v: %d of %d bytes differ from a new window's frame", sz, diff, len(got.Pix))
		}
	}
	if err := win.SetSize(0, 10); err == nil {
		t.Error("SetSize(0, 10) took an empty size")
	}
}

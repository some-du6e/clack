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
// 52), each pixel for pixel what a new window of that size draws. The new
// windows draw first, each released before the next is made: on EGL two
// headless windows can't outlive each other (Release terminates the
// default display under the other).
func TestClackSetSize(t *testing.T) {
	scene := func(o *op.Ops, sz image.Point) {
		paint.FillShape(o, color.NRGBA{R: 0x20, G: 0x40, B: 0x60, A: 0xff}, clip.Rect{Max: sz}.Op())
		paint.FillShape(o, color.NRGBA{R: 0xf0, G: 0x10, B: 0x30, A: 0xff}, clip.Rect(image.Rect(sz.X/4, sz.Y/3, sz.X*3/4, sz.Y*2/3)).Op())
		paint.FillShape(o, color.NRGBA{G: 0xc0, B: 0x20, A: 0xff}, clip.Ellipse(image.Rect(2, 2, 2+sz.Y/2, 2+sz.Y/2)).Op(o))
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

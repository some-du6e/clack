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

// TestClackRollbackKeys: what is written to an Ops after a Rollback has
// op keys of its own (Clack patch 3's rollback epoch), so the GPU's caches
// never take it for what was at the same place before. Clack's inspect
// tool draws each frozen frame as the frame's ops rolled back to their
// checkpoint, with the tool's drawing appended: a rounded rect (a path,
// cached by its op key) drawn there at a new place after a rollback is
// drawn there, pixel for pixel what a new window draws, not where the
// rect of the frame before was.
func TestClackRollbackKeys(t *testing.T) {
	sz := image.Pt(160, 100)
	bg := color.NRGBA{R: 0x20, G: 0x40, B: 0x60, A: 0xff}
	ink := color.NRGBA{R: 0xf0, G: 0x10, B: 0x30, A: 0xff}
	content := func(o *op.Ops) { paint.FillShape(o, bg, clip.Rect{Max: sz}.Op()) }
	overlay := func(o *op.Ops, r image.Rectangle) {
		paint.FillShape(o, ink, clip.UniformRRect(r, 7).Op(o))
		paint.FillShape(o, ink, clip.Stroke{Path: clip.UniformRRect(r.Inset(-4), 9).Path(o), Width: 2}.Op())
	}
	shot := func(w *Window, o *op.Ops) *image.RGBA {
		t.Helper()
		if err := w.Frame(o); err != nil {
			t.Fatal(err)
		}
		img := image.NewRGBA(image.Rectangle{Max: sz})
		if err := w.Screenshot(img); err != nil {
			t.Fatal(err)
		}
		return img
	}
	rects := []image.Rectangle{image.Rect(12, 10, 60, 44), image.Rect(80, 40, 150, 90), image.Rect(30, 50, 70, 80)}
	// what new windows draw: one at a time (on EGL two headless windows
	// can't outlive each other)
	var want []*image.RGBA
	for _, r := range rects {
		fresh := clackWindow(t, sz.X, sz.Y)
		var o op.Ops
		content(&o)
		overlay(&o, r)
		want = append(want, shot(fresh, &o))
		fresh.Release()
	}
	win := clackWindow(t, sz.X, sz.Y)
	defer win.Release()
	var o op.Ops
	content(&o)
	cp := o.Checkpoint()
	for i, r := range rects {
		if i > 0 && !o.Rollback(cp) {
			t.Fatal("Rollback refused the checkpoint")
		}
		overlay(&o, r)
		got := shot(win, &o)
		diff := 0
		for j := range got.Pix {
			if got.Pix[j] != want[i].Pix[j] {
				diff++
			}
		}
		if diff > 0 {
			t.Errorf("frame %d after a rollback, the rect at %v: %d bytes differ from a new window's", i, r, diff)
		}
	}
}

// SPDX-License-Identifier: Unlicense OR MIT

package gpu

import (
	"image"
	"image/color"
	"runtime"
	"testing"
	"weak"

	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// Clack patch 18: an image the frames stop painting can be collected. The
// draw ops are collected every frame into the same arrays; a longer
// earlier frame's image ops kept their pixels reachable past the end of a
// shorter frame (decoded images outlived the image cache that dropped
// them).
func TestDrawOpsReleaseImagesOfEarlierFrames(t *testing.T) {
	var d drawOps
	d.pathCache = newOpCache()
	viewport := image.Pt(800, 600)
	frame := func(img *image.RGBA) {
		var ops op.Ops
		for i := 0; i < 32; i++ {
			st := clip.Rect(image.Rect(0, i, 10, i+1)).Push(&ops)
			paint.ColorOp{Color: color.NRGBA{R: uint8(i), A: 0xff}}.Add(&ops)
			paint.PaintOp{}.Add(&ops)
			st.Pop()
		}
		if img != nil {
			st := clip.Rect(image.Rect(0, 100, 256, 356)).Push(&ops)
			paint.NewImageOp(img).Add(&ops)
			paint.PaintOp{}.Add(&ops)
			st.Pop()
		}
		d.reset(viewport)
		d.collect(&ops, viewport)
	}
	img := image.NewRGBA(image.Rect(0, 0, 256, 256))
	w := weak.Make(img)
	frame(img)
	withImage := len(d.imageOps)
	img = nil
	frame(nil)
	if without := len(d.imageOps); without >= withImage {
		t.Fatalf("%d image ops with the image, %d without: the test needs the first frame longer", withImage, without)
	}
	runtime.GC()
	if w.Value() != nil {
		t.Error("the draw ops still reach an image only an earlier frame painted")
	}
}

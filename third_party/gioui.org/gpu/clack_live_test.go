// SPDX-License-Identifier: Unlicense OR MIT

package gpu

import (
	"image"
	"testing"

	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// countLive is a LiveImage of two images, counting the times it's asked
// after the first (NewLiveImageOp's).
type countLive struct {
	ops  []paint.ImageOp
	cur  int
	asks int
}

func (l *countLive) Image() paint.ImageOp {
	l.asks++
	return l.ops[l.cur]
}

// Clack patch 35: a live image draws the image it shows when the frame
// is drawn, keyed by that image (a frame drawn again from the same ops
// shows it as it is then), and only paints the frame shows ask for it: one
// outside the viewport never does.
func TestDrawOpsLiveImage(t *testing.T) {
	imgs := []*image.RGBA{image.NewRGBA(image.Rect(0, 0, 8, 8)), image.NewRGBA(image.Rect(0, 0, 8, 8))}
	on := &countLive{ops: []paint.ImageOp{paint.NewImageOp(imgs[0]), paint.NewImageOp(imgs[1])}}
	off := &countLive{ops: on.ops}
	var ops op.Ops
	st := clip.Rect(image.Rect(0, 0, 8, 8)).Push(&ops)
	paint.NewLiveImageOp(on).Add(&ops)
	paint.PaintOp{}.Add(&ops)
	st.Pop()
	off2 := op.Offset(image.Pt(2000, 2000)).Push(&ops) // past the 800×600 viewport
	paint.NewLiveImageOp(off).Add(&ops)
	paint.PaintOp{}.Add(&ops)
	off2.Pop()
	on.asks, off.asks = 0, 0

	var d drawOps
	d.pathCache = newOpCache()
	viewport := image.Pt(800, 600)
	resolved := func() []imageOpData {
		d.reset(viewport)
		d.collect(&ops, viewport)
		var out []imageOpData
		for _, img := range d.imageOps {
			if img.material.material == materialTexture {
				out = append(out, resolveLive(img.material.data))
			}
		}
		return out
	}
	r := resolved()
	if len(r) != 1 {
		t.Fatalf("%d image paints on screen, want 1 (the other is outside the viewport)", len(r))
	}
	live := func(d imageOpData) bool { _, ok := d.handle.(liveImage); return ok }
	if r[0].src != imgs[0] || live(r[0]) {
		t.Error("the first frame doesn't draw the image the live one shows, by its own key")
	}
	on.cur = 1
	if r = resolved(); r[0].src != imgs[1] || live(r[0]) {
		t.Error("the same ops drawn again don't draw the image the live one shows now")
	}
	if on.asks != 2 || off.asks != 0 {
		t.Errorf("asked %d times on screen (want 2, once a frame) and %d off it (want 0)", on.asks, off.asks)
	}
	plain := imageOpData{src: imgs[0], handle: new(int)}
	if resolveLive(plain) != plain {
		t.Error("an ImageOp that isn't live changed")
	}
}

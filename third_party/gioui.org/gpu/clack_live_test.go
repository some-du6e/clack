// SPDX-License-Identifier: Unlicense OR MIT

package gpu

import (
	"image"
	"image/color"
	"image/draw"
	"testing"

	"gioui.org/gpu/internal/driver"
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

// uploadDevice is a driver.Device whose textures record the first pixel
// each upload sends; the rest of the interface is never called here (nil:
// a call panics).
type uploadDevice struct {
	driver.Device
	uploads []color.RGBA
}

func (d *uploadDevice) NewTexture(driver.TextureFormat, int, int, driver.TextureFilter, driver.TextureFilter, driver.BufferBinding) (driver.Texture, error) {
	return &uploadTexture{d: d}, nil
}

type uploadTexture struct {
	driver.Texture
	d *uploadDevice
}

func (t *uploadTexture) Upload(_, _ image.Point, pixels []byte, _ int) {
	t.d.uploads = append(t.d.uploads, color.RGBA{R: pixels[0], G: pixels[1], B: pixels[2], A: pixels[3]})
}
func (t *uploadTexture) Release() {}

// Clack patch 35: a live image draws the image it shows when the frame is
// drawn. The renderer's own upload path (uploadImages, texHandle) asks the
// live image for its image and uploads that one, keyed by that image's own
// handle, so the same ops drawn again upload the image it shows then (a
// quiet frame plays an animation on), and an image drawn again unchanged
// uploads nothing. Only the paints a frame shows ask: one outside the
// viewport never does. A texHandle that doesn't resolve live images (the
// hook gone) uploads the image the op was made with every time.
func TestDrawOpsLiveImage(t *testing.T) {
	solid := func(c color.RGBA) *image.RGBA {
		m := image.NewRGBA(image.Rect(0, 0, 8, 8))
		draw.Draw(m, m.Bounds(), &image.Uniform{C: c}, image.Point{}, draw.Src)
		return m
	}
	red, blue := color.RGBA{R: 0xff, A: 0xff}, color.RGBA{B: 0xff, A: 0xff}
	imgs := []*image.RGBA{solid(red), solid(blue)}
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

	dev := &uploadDevice{}
	r := &renderer{ctx: dev}
	cache := newTextureCache()
	var d drawOps
	d.pathCache = newOpCache()
	viewport := image.Pt(800, 600)
	// frame collects and uploads the ops as gpu.Frame does, and returns
	// the uploads it made
	frame := func() []color.RGBA {
		t.Helper()
		dev.uploads = nil
		d.reset(viewport)
		d.collect(&ops, viewport)
		n := 0
		for _, img := range d.imageOps {
			if img.material.material == materialTexture {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("%d image paints on screen, want 1 (the other is outside the viewport)", n)
		}
		r.uploadImages(cache, d.imageOps)
		cache.frame()
		return dev.uploads
	}
	if got := frame(); len(got) != 1 || got[0] != red {
		t.Errorf("the first frame uploaded %v, want the live image's red", got)
	}
	if got := frame(); len(got) != 0 {
		t.Errorf("the same ops again, the live image unchanged: uploaded %v, want nothing (its texture is cached)", got)
	}
	on.cur = 1
	if got := frame(); len(got) != 1 || got[0] != blue {
		t.Errorf("the same ops after the live image moved on uploaded %v, want blue", got)
	}
	on.cur = 0
	if got := frame(); len(got) != 1 || got[0] != red {
		t.Errorf("back on its first image: uploaded %v, want red again (its texture went when no frame drew it)", got)
	}
	if on.asks != 4 || off.asks != 0 {
		t.Errorf("asked %d times on screen (want 4, once a frame) and %d off it (want 0)", on.asks, off.asks)
	}
	plain := imageOpData{src: imgs[0], handle: new(int)}
	if resolveLive(plain) != plain {
		t.Error("an ImageOp that isn't live changed")
	}
}

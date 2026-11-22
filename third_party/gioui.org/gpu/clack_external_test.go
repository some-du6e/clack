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

// externalDevice is an uploadDevice that can draw external textures,
// counting what the renderer asks of it and every native reference: an
// ask hands one out, a wrapper takes it over, a drop or the wrapper's
// release lets it go.
type externalDevice struct {
	uploadDevice
	wraps, releases, drops, forgets int
	native                          []uintptr
	refs                            map[uintptr]int
}

func newExternalDevice() *externalDevice { return &externalDevice{refs: map[uintptr]int{}} }

func (d *externalDevice) ClackDevice() (uint8, uintptr) { return driver.ClackAPIMetal, 0xd3 }

func (d *externalDevice) ClackWrap(native uintptr, w, h int, f driver.TextureFilter) (driver.Texture, error) {
	d.wraps++
	d.native = append(d.native, native)
	return &externalTexture{d: d, native: native}, nil
}

func (d *externalDevice) ClackDrop(native uintptr) {
	d.drops++
	d.refs[native]--
}

func (d *externalDevice) ClackForgetState() { d.forgets++ }

// held is the references held by wrappers not released (each holds one).
func (d *externalDevice) held() int {
	n := 0
	for _, r := range d.refs {
		n += r
	}
	return n
}

type externalTexture struct {
	driver.Texture
	d      *externalDevice
	native uintptr
}

func (t *externalTexture) Release() {
	t.d.releases++
	t.d.refs[t.native]--
}

// testExternal is an ExternalImage handing out tex with key, and a
// reference to it, or nothing while !ok; it counts the asks.
type testExternal struct {
	dev  *externalDevice
	tex  uintptr
	key  uint64
	ok   bool
	asks int
	got  paint.ExternalDevice
}

func (e *testExternal) ExternalTexture(dev paint.ExternalDevice) (paint.ExternalTexture, bool) {
	e.asks++
	e.got = dev
	if e.ok && e.dev != nil {
		e.dev.refs[e.tex]++
	}
	return paint.ExternalTexture{Handle: e.tex, Key: e.key}, e.ok
}

// liveOf is a LiveImage showing op.
type liveOf struct{ op paint.ImageOp }

func (l *liveOf) Image() paint.ImageOp { return l.op }

func solidRGBA(c color.RGBA) *image.RGBA {
	m := image.NewRGBA(image.Rect(0, 0, 8, 8))
	draw.Draw(m, m.Bounds(), &image.Uniform{C: c}, image.Point{}, draw.Src)
	return m
}

// frameRunner draws ops as gpu.Frame does, on dev, and returns each
// frame's uploads.
func frameRunner(t *testing.T, dev driver.Device) func(ops *op.Ops) []color.RGBA {
	r := &renderer{ctx: dev}
	cache := newTextureCache()
	var d drawOps
	d.pathCache = newOpCache()
	viewport := image.Pt(800, 600)
	return func(ops *op.Ops) []color.RGBA {
		t.Helper()
		up := dev.(interface{ uploaded() *[]color.RGBA }).uploaded()
		*up = nil
		r.stats = FrameStats{}
		d.reset(viewport)
		d.collect(ops, viewport)
		r.uploadImages(cache, d.imageOps)
		for _, img := range d.imageOps {
			if img.material.material == materialTexture && img.material.tex == nil {
				t.Fatal("an image paint has no texture to draw")
			}
		}
		cache.frame()
		return *up
	}
}

// paintOps paints op at the origin (and again at (20,0) when twice), and
// another external op outside the viewport.
func paintOps(o paint.ImageOp, twice bool, off *testExternal) *op.Ops {
	var ops op.Ops
	n := 1
	if twice {
		n = 2
	}
	for i := range n {
		st := op.Offset(image.Pt(i*20, 0)).Push(&ops)
		cl := clip.Rect(image.Rect(0, 0, 16, 9)).Push(&ops)
		o.Add(&ops)
		paint.PaintOp{}.Add(&ops)
		cl.Pop()
		st.Pop()
	}
	if off != nil {
		st := op.Offset(image.Pt(2000, 2000)).Push(&ops) // past the 800×600 viewport
		paint.NewExternalImageOp(off, image.Pt(16, 9), paint.ImageOp{}).Add(&ops)
		paint.PaintOp{}.Add(&ops)
		st.Pop()
	}
	return &ops
}

// Clack patch 51: an external image's paint asks for its texture once a
// frame while the frame shows it, wraps a texture once per key (the same
// key draws the wrapper the frame before made, a new one wraps again and
// the old wrapper goes when a frame doesn't draw it), takes over the
// reference each ask hands out (a wrapper keeps it, a cache hit drops
// it), draws its still while it has none, and nothing for a zero or
// uniform still. A device that can't draw external textures never asks,
// and draws the still.
func TestDrawOpsExternalImage(t *testing.T) {
	if uint8(paint.ExternalDirect3D11) != driver.ClackAPIDirect3D11 || uint8(paint.ExternalMetal) != driver.ClackAPIMetal {
		t.Fatal("paint's and driver's API numbers differ")
	}
	yellow := color.RGBA{R: 0xff, G: 0xff, A: 0xff}
	still := paint.NewImageOp(solidRGBA(yellow))
	dev := newExternalDevice()
	frame := frameRunner(t, dev)
	on := &testExternal{dev: dev, tex: 0xa, key: 1, ok: true}
	off := &testExternal{dev: dev, tex: 0xb, key: 1, ok: true}
	ops := paintOps(paint.NewExternalImageOp(on, image.Pt(16, 9), still), false, off)
	if up := frame(ops); len(up) != 0 || dev.wraps != 1 || dev.releases != 0 || dev.held() != 1 {
		t.Errorf("first frame: %d wraps, %d releases, %d references held, uploads %v; want 1 wrap holding the ask's reference, no upload", dev.wraps, dev.releases, dev.held(), up)
	}
	if on.asks != 1 || on.got != (paint.ExternalDevice{API: paint.ExternalMetal, Handle: 0xd3}) || dev.native[0] != 0xa || dev.forgets != 1 {
		t.Errorf("first frame: %d asks on %+v wrapping %#x, %d forgets; want 1 ask on the device wrapping its texture, then the state forgotten", on.asks, on.got, dev.native, dev.forgets)
	}
	if off.asks != 0 {
		t.Errorf("the paint outside the viewport asked %d times", off.asks)
	}
	if up := frame(ops); len(up) != 0 || dev.wraps != 1 || dev.drops != 1 || dev.held() != 1 || on.asks != 2 {
		t.Errorf("the same key again: %d wraps, %d drops, %d held, %d asks, uploads %v; want the frame before's wrapper, this ask's reference dropped", dev.wraps, dev.drops, dev.held(), on.asks, up)
	}
	on.key = 2
	frame(ops)
	if dev.wraps != 2 || dev.releases != 1 || dev.held() != 1 {
		t.Errorf("a new key: %d wraps, %d releases, %d held; want it wrapped and the old key's wrapper (and reference) gone", dev.wraps, dev.releases, dev.held())
	}
	on.asks = 0
	frame(paintOps(paint.NewExternalImageOp(on, image.Pt(16, 9), still), true, off))
	if on.asks != 2 || dev.wraps != 2 || dev.releases != 1 || dev.held() != 1 {
		t.Errorf("two paints of it: %d asks, %d wraps, %d releases, %d held; want 2 asks of the one wrapper, both extra references dropped", on.asks, dev.wraps, dev.releases, dev.held())
	}
	on.ok = false
	if up := frame(ops); len(up) != 1 || up[0] != yellow || dev.releases != 2 || dev.held() != 0 {
		t.Errorf("no texture: uploads %v, %d releases, %d held; want the still's yellow, the wrapper and its reference gone", up, dev.releases, dev.held())
	}
	if up := frame(ops); len(up) != 0 {
		t.Errorf("no texture again: uploads %v; want the still's texture from the frame before", up)
	}
	for name, s := range map[string]paint.ImageOp{"zero": {}, "uniform": paint.NewImageOp(&image.Uniform{C: yellow})} {
		d := newExternalDevice()
		e := &testExternal{dev: d, tex: 0xa, key: 1}
		if up := frameRunner(t, d)(paintOps(paint.NewExternalImageOp(e, image.Pt(16, 9), s), false, nil)); len(up) != 1 || up[0] != (color.RGBA{}) {
			t.Errorf("no texture, %s still: uploads %v; want only the transparent 1×1", name, up)
		}
	}
	plain := &uploadDevice{}
	e := &testExternal{tex: 0xa, key: 1, ok: true}
	if up := frameRunner(t, plain)(paintOps(paint.NewExternalImageOp(e, image.Pt(16, 9), still), false, nil)); len(up) != 1 || up[0] != yellow || e.asks != 0 {
		t.Errorf("a device without external textures: uploads %v, %d asks; want the still and no ask", up, e.asks)
	}
}

// Clack patch 51: a still that is itself external (a viewer's player over
// the inline player's last frame), or live showing an external image, is
// resolved in turn: its texture, else its own still; one nested too deep,
// or a header with no pixels, draws nothing instead of uploading it.
func TestDrawOpsExternalNestedStill(t *testing.T) {
	yellow, blue := color.RGBA{R: 0xff, G: 0xff, A: 0xff}, color.RGBA{B: 0xff, A: 0xff}
	dev := newExternalDevice()
	frame := frameRunner(t, dev)
	inner := &testExternal{dev: dev, tex: 0xc, key: 7, ok: true}
	outer := &testExternal{dev: dev, tex: 0xd, key: 1}
	innerOp := paint.NewExternalImageOp(inner, image.Pt(16, 9), paint.NewImageOp(solidRGBA(yellow)))
	ops := paintOps(paint.NewExternalImageOp(outer, image.Pt(16, 9), innerOp), false, nil)
	if up := frame(ops); len(up) != 0 || dev.wraps != 1 || dev.native[0] != 0xc || outer.asks != 1 || inner.asks != 1 {
		t.Errorf("outer without a texture over inner with one: %d wraps (%#x), uploads %v, asks %d/%d; want inner's texture wrapped", dev.wraps, dev.native, up, outer.asks, inner.asks)
	}
	inner.ok = false
	if up := frame(ops); len(up) != 1 || up[0] != yellow {
		t.Errorf("neither has a texture: uploads %v, want inner's still", up)
	}
	// live, showing an external image
	live := paint.NewLiveImageOp(&liveOf{op: innerOp})
	if up := frame(paintOps(paint.NewExternalImageOp(outer, image.Pt(16, 9), live), false, nil)); len(up) > 1 || (len(up) == 1 && up[0] != yellow) {
		t.Errorf("a live still showing an external one: uploads %v, want inner's still at most", up)
	}
	inner.ok = true
	frame(paintOps(paint.NewExternalImageOp(outer, image.Pt(16, 9), live), false, nil))
	if dev.native[len(dev.native)-1] != 0xc {
		t.Errorf("a live still showing an external one with a texture: wrapped %#x, want inner's", dev.native)
	}
	// nested too deep: each still another external image without one
	deep := paint.NewImageOp(solidRGBA(blue))
	var es []*testExternal
	for i := 0; i < 6; i++ {
		e := &testExternal{dev: dev, tex: uintptr(0x100 + i), key: 1}
		es = append(es, e)
		deep = paint.NewExternalImageOp(e, image.Pt(16, 9), deep)
	}
	if up := frame(paintOps(deep, false, nil)); len(up) > 1 || (len(up) == 1 && up[0] != (color.RGBA{})) {
		t.Errorf("stills nested six deep: uploads %v, want nothing (the transparent 1×1 at most)", up)
	}
	if dev.held() != 0 {
		t.Errorf("%d references held after a frame without the wrapper, want none", dev.held())
	}
}

func (d *uploadDevice) uploaded() *[]color.RGBA { return &d.uploads }

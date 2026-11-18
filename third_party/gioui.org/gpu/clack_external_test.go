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
// counting what the renderer asks of it.
type externalDevice struct {
	uploadDevice
	wraps, releases, forgets int
	native                   []uintptr
}

func (d *externalDevice) ClackDevice() (uint8, uintptr) { return driver.ClackAPIMetal, 0xd3 }

func (d *externalDevice) ClackWrap(native uintptr, w, h int, f driver.TextureFilter) (driver.Texture, error) {
	d.wraps++
	d.native = append(d.native, native)
	return &externalTexture{d: d}, nil
}

func (d *externalDevice) ClackForgetState() { d.forgets++ }

type externalTexture struct {
	driver.Texture
	d *externalDevice
}

func (t *externalTexture) Release() { t.d.releases++ }

// testExternal is an ExternalImage handing out tex with key, or nothing
// while !ok; it counts the asks.
type testExternal struct {
	tex  uintptr
	key  uint64
	ok   bool
	asks int
	dev  paint.ExternalDevice
}

func (e *testExternal) ExternalTexture(dev paint.ExternalDevice) (paint.ExternalTexture, bool) {
	e.asks++
	e.dev = dev
	return paint.ExternalTexture{Handle: e.tex, Key: e.key}, e.ok
}

// Clack patch 50: an external image's paint asks for its texture once a
// frame while the frame shows it, wraps a texture once per key (the same
// key draws the wrapper the frame before made, a new one wraps again and
// the old wrapper goes when a frame doesn't draw it), draws its still
// while it has none, and nothing for a zero or uniform still. A device
// that can't draw external textures never asks, and draws the still.
func TestDrawOpsExternalImage(t *testing.T) {
	if uint8(paint.ExternalDirect3D11) != driver.ClackAPIDirect3D11 || uint8(paint.ExternalMetal) != driver.ClackAPIMetal {
		t.Fatal("paint's and driver's API numbers differ")
	}
	solid := func(c color.RGBA) *image.RGBA {
		m := image.NewRGBA(image.Rect(0, 0, 8, 8))
		draw.Draw(m, m.Bounds(), &image.Uniform{C: c}, image.Point{}, draw.Src)
		return m
	}
	yellow := color.RGBA{R: 0xff, G: 0xff, A: 0xff}
	still := paint.NewImageOp(solid(yellow))
	on := &testExternal{tex: 0xa, key: 1, ok: true}
	off := &testExternal{tex: 0xb, key: 1, ok: true}
	record := func(e *testExternal, still paint.ImageOp, twice bool) *op.Ops {
		var ops op.Ops
		n := 1
		if twice {
			n = 2
		}
		for i := range n {
			st := op.Offset(image.Pt(i*20, 0)).Push(&ops)
			cl := clip.Rect(image.Rect(0, 0, 16, 9)).Push(&ops)
			paint.NewExternalImageOp(e, image.Pt(16, 9), still).Add(&ops)
			paint.PaintOp{}.Add(&ops)
			cl.Pop()
			st.Pop()
		}
		st := op.Offset(image.Pt(2000, 2000)).Push(&ops) // past the 800×600 viewport
		paint.NewExternalImageOp(off, image.Pt(16, 9), still).Add(&ops)
		paint.PaintOp{}.Add(&ops)
		st.Pop()
		return &ops
	}
	viewport := image.Pt(800, 600)
	run := func(dev driver.Device) func(ops *op.Ops) []color.RGBA {
		r := &renderer{ctx: dev}
		cache := newTextureCache()
		var d drawOps
		d.pathCache = newOpCache()
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

	dev := &externalDevice{}
	frame := run(dev)
	ops := record(on, still, false)
	if up := frame(ops); len(up) != 0 || dev.wraps != 1 || dev.releases != 0 {
		t.Errorf("first frame: %d wraps, %d releases, uploads %v; want 1 wrap and no upload", dev.wraps, dev.releases, up)
	}
	if on.asks != 1 || on.dev != (paint.ExternalDevice{API: paint.ExternalMetal, Handle: 0xd3}) || dev.native[0] != 0xa || dev.forgets != 1 {
		t.Errorf("first frame: %d asks on %+v wrapping %#x, %d forgets; want 1 ask on the device wrapping its texture, then the state forgotten", on.asks, on.dev, dev.native, dev.forgets)
	}
	if off.asks != 0 {
		t.Errorf("the paint outside the viewport asked %d times", off.asks)
	}
	if up := frame(ops); len(up) != 0 || dev.wraps != 1 || dev.releases != 0 || on.asks != 2 {
		t.Errorf("the same key again: %d wraps, %d releases, %d asks, uploads %v; want the frame before's wrapper", dev.wraps, dev.releases, on.asks, up)
	}
	on.key = 2
	frame(ops)
	if dev.wraps != 2 || dev.releases != 1 {
		t.Errorf("a new key: %d wraps, %d releases; want it wrapped and the old key's wrapper gone", dev.wraps, dev.releases)
	}
	twice := record(on, still, true)
	on.asks = 0
	frame(twice)
	if on.asks != 2 || dev.wraps != 2 || dev.releases != 1 {
		t.Errorf("two paints of it: %d asks, %d wraps, %d releases; want 2 asks of the one wrapper", on.asks, dev.wraps, dev.releases)
	}
	on.ok = false
	if up := frame(ops); len(up) != 1 || up[0] != yellow || dev.releases != 2 {
		t.Errorf("no texture: uploads %v, %d releases; want the still's yellow, and the wrapper gone", up, dev.releases)
	}
	if up := frame(ops); len(up) != 0 {
		t.Errorf("no texture again: uploads %v; want the still's texture from the frame before", up)
	}
	for name, s := range map[string]paint.ImageOp{"zero": {}, "uniform": paint.NewImageOp(&image.Uniform{C: yellow})} {
		f := run(&externalDevice{})
		if up := f(record(on, s, false)); len(up) != 1 || up[0] != (color.RGBA{}) {
			t.Errorf("no texture, %s still: uploads %v; want only the transparent 1×1", name, up)
		}
	}

	plain := &uploadDevice{}
	on = &testExternal{tex: 0xa, key: 1, ok: true}
	if up := run(plain)(record(on, still, false)); len(up) != 1 || up[0] != yellow || on.asks != 0 {
		t.Errorf("a device without external textures: uploads %v, %d asks; want the still and no ask", up, on.asks)
	}
}

func (d *uploadDevice) uploaded() *[]color.RGBA { return &d.uploads }

// SPDX-License-Identifier: Unlicense OR MIT

package headless

import (
	"image"
	"image/color"
	"image/draw"
	"testing"

	"gioui.org/f32"
	"gioui.org/gpu/internal/driver"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// Clack patch 50 on a real device: external images.

// hostImage hands out its texture with key while ok.
type hostImage struct {
	tex  uintptr
	key  uint64
	ok   bool
	asks int
	dev  paint.ExternalDevice
}

func (h *hostImage) ExternalTexture(dev paint.ExternalDevice) (paint.ExternalTexture, bool) {
	h.asks++
	h.dev = dev
	return paint.ExternalTexture{Handle: h.tex, Key: h.key}, h.ok
}

// externalWindow is a pixel test's window and its device's external
// texture support (nil on OpenGL and Vulkan, which draw the still).
func externalWindow(t *testing.T, w, h int) (*Window, driver.ClackExternal, driver.ClackExternalTest) {
	t.Helper()
	win := clackWindow(t, w, h)
	t.Cleanup(win.Release)
	x, _ := win.dev.(driver.ClackExternal)
	xt, _ := win.dev.(driver.ClackExternalTest)
	if (x == nil) != (xt == nil) {
		t.Fatalf("%T: ClackExternal without ClackExternalTest, or the other way", win.dev)
	}
	return win, x, xt
}

// deviceTexture is a texture the device made (as a decoder makes its
// frames) holding img, and its native handle.
func deviceTexture(t *testing.T, win *Window, xt driver.ClackExternalTest, img *image.RGBA) (driver.Texture, uintptr) {
	t.Helper()
	var tex driver.Texture
	var native uintptr
	err := contextDo(win.ctx, func() error {
		var err error
		tex, err = win.dev.NewTexture(driver.TextureFormatSRGBA, img.Bounds().Dx(), img.Bounds().Dy(), driver.FilterLinear, driver.FilterLinear, driver.BufferBindingTexture)
		if err != nil {
			return err
		}
		driver.UploadImage(tex, image.Point{}, img)
		native = xt.ClackNative(tex)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { contextDo(win.ctx, func() error { tex.Release(); return nil }) })
	return tex, native
}

// pattern is a 32×24 image with something in every pixel: four colour
// quadrants, a diagonal gradient, a half-transparent band.
func pattern() *image.RGBA {
	m := image.NewRGBA(image.Rect(0, 0, 32, 24))
	for y := range 24 {
		for x := range 32 {
			c := color.RGBA{R: uint8(x * 8), G: uint8(y * 10), B: uint8(255 - (x+y)*4), A: 0xff}
			switch {
			case x < 16 && y < 12:
				c.R = 0xff
			case x >= 16 && y >= 12:
				c.G = 0xff
			}
			if y == 20 || y == 21 { // premultiplied: half of a half-blue
				c = color.RGBA{B: 0x40, A: 0x80}
			}
			m.SetRGBA(x, y, c)
		}
	}
	return m
}

// Clack patch 50: a texture the device made, drawn through its native
// handle, gives the pixels of an ImageOp of the same image, under a
// rounded clip, an opacity layer and a rotating, magnifying transform
// (magnified, the ImageOp's mipmaps don't come into it). Without external
// textures (OpenGL), the op draws its still the same way.
func TestClackExternalImage(t *testing.T) {
	win, x, xt := externalWindow(t, 96, 96)
	img := pattern()
	scene := func(im paint.ImageOp) func(o *op.Ops) {
		return func(o *op.Ops) {
			paint.Fill(o, color.NRGBA{R: 0x20, G: 0x30, B: 0x40, A: 0xff})
			tr := f32.AffineId().Scale(f32.Point{}, f32.Pt(1.75, 1.75)).Rotate(f32.Point{}, 0.3).Offset(f32.Pt(30, 6))
			st := op.Affine(tr).Push(o)
			rr := clip.RRect{Rect: image.Rect(0, 0, 32, 24), SE: 6, SW: 6, NW: 6, NE: 6}.Push(o)
			op1 := paint.PushOpacity(o, 0.7)
			im.Add(o)
			paint.PaintOp{}.Add(o)
			op1.Pop()
			rr.Pop()
			st.Pop()
		}
	}
	want, _ := clackShot(t, win, scene(paint.NewImageOp(img)))
	var e *hostImage
	var ext paint.ImageOp
	if x == nil {
		t.Logf("%T has no external textures: the op draws its still", win.dev)
		e = &hostImage{tex: 1, key: 1, ok: true}
		ext = paint.NewExternalImageOp(e, img.Bounds().Size(), paint.NewImageOp(img))
	} else {
		_, native := deviceTexture(t, win, xt, img)
		e = &hostImage{tex: native, key: 1, ok: true}
		ext = paint.NewExternalImageOp(e, img.Bounds().Size(), paint.ImageOp{})
	}
	got, st := clackShot(t, win, scene(ext))
	bg := want.RGBAAt(1, 1)
	n, diff := 0, 0
	for y := range 96 {
		for x := range 96 {
			w, g := want.RGBAAt(x, y), got.RGBAAt(x, y)
			if w != bg {
				n++
			}
			if w != g {
				if diff < 5 {
					t.Errorf("(%d,%d): external %v, ImageOp %v", x, y, g, w)
				}
				diff++
			}
		}
	}
	if diff > 0 {
		t.Errorf("%d of 9216 pixels differ", diff)
	}
	if n < 1000 {
		t.Fatalf("only %d pixels show the image: the scene draws nothing to compare", n)
	}
	if x == nil {
		if e.asks != 0 || st.Externals != 0 {
			t.Errorf("a device without external textures asked %d times (stats %d)", e.asks, st.Externals)
		}
		return
	}
	api, dev := x.ClackDevice()
	if e.dev != (paint.ExternalDevice{API: paint.ExternalAPI(api), Handle: dev}) || e.dev.Handle == 0 {
		t.Errorf("asked on %+v, want the device drawing (%d, %#x)", e.dev, api, dev)
	}
	if e.asks != 2 || st.Externals != 1 || st.ExternalWraps != 0 {
		t.Errorf("two frames: %d asks; the second %d externals, %d wraps; want 2 asks and the first frame's wrapper", e.asks, st.Externals, st.ExternalWraps)
	}
	if n := xt.ClackWrapped(); n != 1 {
		t.Errorf("%d wrapped textures held, want 1", n)
	}
	clackShot(t, win, func(o *op.Ops) { paint.Fill(o, color.NRGBA{A: 0xff}) })
	if n := xt.ClackWrapped(); n != 0 {
		t.Errorf("frames without the image left %d wrapped textures, want none", n)
	}
	t.Logf("%T: %d pixels of the image, all the ImageOp's", win.dev, n)
}

// Clack patch 50: the same key wraps nothing new, a new key wraps once and
// the old key's wrapper goes when a frame doesn't draw it, new pixels in
// the same texture under a new key are drawn, no texture draws the still,
// and a zero or uniform still draws nothing.
func TestClackExternalKeys(t *testing.T) {
	win, x, xt := externalWindow(t, 32, 32)
	solid := func(c color.RGBA) *image.RGBA {
		m := image.NewRGBA(image.Rect(0, 0, 8, 8))
		draw.Draw(m, m.Bounds(), &image.Uniform{C: c}, image.Point{}, draw.Src)
		return m
	}
	red, blue, green, yellow := color.RGBA{R: 0xff, A: 0xff}, color.RGBA{B: 0xff, A: 0xff}, color.RGBA{G: 0xff, A: 0xff}, color.RGBA{R: 0xff, G: 0xff, A: 0xff}
	bg := color.RGBA{R: 0x10, G: 0x10, B: 0x10, A: 0xff}
	e := &hostImage{ok: true}
	still := paint.NewImageOp(solid(yellow))
	frame := func(still paint.ImageOp) (color.RGBA, int, int) {
		t.Helper()
		img, st := clackFrames(t, win, 1, func(o *op.Ops) {
			paint.Fill(o, color.NRGBA(bg))
			cl := clip.Rect(image.Rect(4, 4, 20, 20)).Push(o)
			paint.NewExternalImageOp(e, image.Pt(16, 16), still).Add(o)
			paint.PaintOp{}.Add(o)
			cl.Pop()
		})
		return img[0].RGBAAt(12, 12), st[0].Externals, st[0].ExternalWraps
	}
	if x == nil {
		if c, asks, _ := frame(still); c != yellow || asks != 0 || e.asks != 0 {
			t.Errorf("%T: %v, %d asks; want the still and no ask", win.dev, c, e.asks)
		}
		t.Skipf("%T has no external textures", win.dev)
	}
	texB, b := deviceTexture(t, win, xt, solid(blue))
	_, a := deviceTexture(t, win, xt, solid(red))
	e.tex, e.key = a, 1
	if c, n, w := frame(still); c != red || n != 1 || w != 1 || xt.ClackWrapped() != 1 {
		t.Errorf("first frame: %v, %d externals, %d wraps, %d held; want red, wrapped once", c, n, w, xt.ClackWrapped())
	}
	if c, n, w := frame(still); c != red || n != 1 || w != 0 || xt.ClackWrapped() != 1 {
		t.Errorf("the same key: %v, %d externals, %d wraps, %d held; want red and nothing wrapped", c, n, w, xt.ClackWrapped())
	}
	e.tex, e.key = b, 2
	if c, _, w := frame(still); c != blue || w != 1 || xt.ClackWrapped() != 1 {
		t.Errorf("another texture: %v, %d wraps, %d held; want blue, the red one's wrapper gone", c, w, xt.ClackWrapped())
	}
	if err := contextDo(win.ctx, func() error { driver.UploadImage(texB, image.Point{}, solid(green)); return nil }); err != nil {
		t.Fatal(err)
	}
	e.key = 3
	if c, _, w := frame(still); c != green || w != 1 || xt.ClackWrapped() != 1 {
		t.Errorf("new pixels in it, a new key: %v, %d wraps, %d held; want green", c, w, xt.ClackWrapped())
	}
	e.ok = false
	if c, n, w := frame(still); c != yellow || n != 1 || w != 0 || xt.ClackWrapped() != 0 {
		t.Errorf("no texture: %v, %d externals, %d wraps, %d held; want the still's yellow, nothing held", c, n, w, xt.ClackWrapped())
	}
	if c, _, _ := frame(paint.ImageOp{}); c != bg {
		t.Errorf("no texture, a zero still: %v, want the background", c)
	}
	if c, _, _ := frame(paint.NewImageOp(&image.Uniform{C: yellow})); c != bg {
		t.Errorf("no texture, a uniform still: %v, want the background", c)
	}
}

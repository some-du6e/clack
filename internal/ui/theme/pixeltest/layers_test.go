package pixeltest

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// Gio v0.10.3 turns a full-window opaque colour paint (outside any
// opacity layer) into the clear colour and throws away the image ops
// before it, but it kept the opacity layers recorded before it, whose op
// ranges point into the discarded ops: drawLayers then panicked with
// "index out of range [609] with length 135" (Clack alpha.18, macOS).
// third_party/gioui.org drops those layers too. Before the fill there are
// more ops than after it, so the stale range still slices (within the
// capacity) and the panic is the crash's index panic.
func TestOpaqueFillAfterOpacityLayer(t *testing.T) {
	const w, h = 96, 64
	red := color.NRGBA{R: 0xff, A: 0xff}
	green := color.NRGBA{G: 0xff, A: 0xff}
	blue := color.NRGBA{B: 0xff, A: 0xff}
	white := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}

	var ops op.Ops
	for i := range 40 {
		paint.FillShape(&ops, red, clip.Rect(image.Rect(i, 0, i+1, 8)).Op())
	}
	o := paint.PushOpacity(&ops, 0.5)
	for i := range 20 {
		paint.FillShape(&ops, green, clip.Rect(image.Rect(10+i, 16, 11+i, 24)).Op())
	}
	o.Pop()
	// covers everything above: Gio clears to it instead of drawing it
	paint.FillShape(&ops, blue, clip.Rect{Max: image.Pt(w, h)}.Op())
	// a layer after the fill still draws, at its opacity
	o = paint.PushOpacity(&ops, 0.5)
	paint.FillShape(&ops, white, clip.Rect(image.Rect(48, 32, 80, 56)).Op())
	o.Pop()

	img := Render(t, w, h, &ops) // unpatched: panics on headless's GPU goroutine
	for _, p := range []image.Point{{5, 4}, {15, 20}, {90, 60}} {
		if c := img.RGBAAt(p.X, p.Y); c.R > 8 || c.G > 8 || c.B < 0xf0 {
			t.Errorf("pixel %v = %v, want the opaque blue fill over the earlier content", p, c)
		}
	}
	// white at .5 over blue: grey-blue, neither of the two
	if c := img.RGBAAt(64, 44); c.B < 0xf0 || c.R < 0x40 || c.R > 0xe0 || c.G < 0x40 || c.G > 0xe0 {
		t.Errorf("pixel (64,44) = %v, want white at half opacity over blue", c)
	}
}

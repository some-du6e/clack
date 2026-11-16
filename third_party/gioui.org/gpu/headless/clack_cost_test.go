// SPDX-License-Identifier: Unlicense OR MIT

package headless

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"testing"

	"gioui.org/f32"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// Clack: how exactly the GPU reproduces a path's coverage when only its
// stencil region (the atlas placement) changes. Logs, never fails: the
// numbers decide how the frame-cost patches are checked.
func TestClackPlacementNoise(t *testing.T) {
	if os.Getenv("CLACK_GPU_COST_PROBE") == "" {
		t.Skip("CLACK_GPU_COST_PROBE not set")
	}
	const w, h = 1400, 900
	win, err := NewWindow(w, h)
	if err != nil {
		t.Skipf("no headless GPU: %v", err)
	}
	defer win.Release()
	shot := func(draw func(o *op.Ops)) *image.RGBA {
		var o op.Ops
		draw(&o)
		if err := win.Frame(&o); err != nil {
			t.Fatal(err)
		}
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		if err := win.Screenshot(img); err != nil {
			t.Fatal(err)
		}
		return img
	}
	bg := noiseGradient(w, h)
	ws := image.Rect(80, 60, w-30, h-30)
	rings := make([]clip.PathSpec, 15)
	ringOps := make([]*op.Ops, 15)
	for i := 1; i <= 14; i++ {
		ri := ws.Inset(-i)
		ringOps[i] = new(op.Ops)
		rings[i] = clip.UniformRRect(image.Rectangle{Max: ri.Size()}, 12+i).Path(ringOps[i])
	}
	alpha := func(i int) uint8 {
		f := float32(17-i) / 16
		return uint8(49 * f * f)
	}
	// the workspace shadow as Clack draws it today: 14 full rings
	full := func(o *op.Ops, dummy bool) {
		bg(o)
		if dummy {
			// a path stenciled first: every later path lands elsewhere in the atlas
			paint.FillShape(o, color.NRGBA{R: 1, A: 1}, clip.Ellipse(image.Rect(3, 3, 41, 37)).Op(o))
		}
		for i := 1; i <= 14; i++ {
			ri := ws.Inset(-i)
			st := op.Offset(ri.Min).Push(o)
			paint.FillShape(o, color.NRGBA{A: alpha(i)}, clip.Stroke{Path: rings[i], Width: 1.5}.Op())
			st.Pop()
		}
	}
	// the same rings, each stenciled in four bands around its stroke
	bands := func(o *op.Ops) {
		bg(o)
		for i := 1; i <= 14; i++ {
			ri := ws.Inset(-i)
			for _, b := range ringBandsProbe(ri, 12+i, 1) {
				c := clip.Rect(b).Push(o)
				st := op.Offset(ri.Min).Push(o)
				paint.FillShape(o, color.NRGBA{A: alpha(i)}, clip.Stroke{Path: rings[i], Width: 1.5}.Op())
				st.Pop()
				c.Pop()
			}
		}
	}
	a := shot(func(o *op.Ops) { full(o, false) })
	a2 := shot(func(o *op.Ops) { full(o, false) })
	b := shot(func(o *op.Ops) { full(o, true) })
	c := shot(bands)
	t.Logf("rings, same ops twice: %s", diffStats(a, a2, image.Rect(0, 0, w, h)))
	t.Logf("rings after a dummy path (atlas shifted): %s", diffStats(a, b, image.Rect(45, 45, w, h)))
	t.Logf("rings in four bands each: %s", diffStats(a, c, image.Rect(0, 0, w, h)))

	// content in a rounded panel: glyph-like paths clipped by the panel's
	// RRect (an intersection each) against the same paths unclipped, in
	// the panel's interior
	panel := image.Rect(300, 80, w-60, h-60)
	glyphs := func(o *op.Ops) {
		for k := 0; k < 300; k++ {
			x := 310 + (k%20)*50
			y := 100 + (k/20)*48
			r := image.Rect(x, y, x+9+k%7, y+11+k%5)
			paint.FillShape(o, color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xff}, clip.Ellipse(r).Op(o))
			var p clip.Path
			p.Begin(o)
			p.MoveTo(f32.Pt(float32(x)+12.3, float32(y)+1.7))
			p.QuadTo(f32.Pt(float32(x)+30.1, float32(y)+4.2), f32.Pt(float32(x)+21.6, float32(y)+15.9))
			p.LineTo(f32.Pt(float32(x)+14.2, float32(y)+13.1))
			p.Close()
			paint.FillShape(o, color.NRGBA{R: 0x61, G: 0x60, B: 0x61, A: 0xff}, clip.Outline{Path: p.End()}.Op())
		}
	}
	clipped := func(o *op.Ops) {
		bg(o)
		st := clip.RRect{Rect: panel, NE: 24, SE: 24}.Push(o)
		paint.FillShape(o, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, clip.Rect(panel).Op())
		glyphs(o)
		st.Pop()
	}
	unclipped := func(o *op.Ops) {
		bg(o)
		st := clip.RRect{Rect: panel, NE: 24, SE: 24}.Push(o)
		paint.FillShape(o, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, clip.Rect(panel).Op())
		st.Pop()
		glyphs(o)
	}
	d := shot(clipped)
	e := shot(unclipped)
	inner := image.Rect(panel.Min.X+1, panel.Min.Y+1, panel.Max.X-25, panel.Max.Y-1)
	t.Logf("panel content clipped vs not, interior: %s", diffStats(d, e, inner))
	t.Logf("panel content clipped vs not, whole window: %s", diffStats(d, e, image.Rect(0, 0, w, h)))

	// the panel's corners when its RRect is stenciled over the whole panel
	// (an op spans it) against stenciled in the corner bands only
	corner := func(o *op.Ops, wide bool) {
		bg(o)
		rr := clip.RRect{Rect: panel, NE: 24, SE: 24}
		spec := rr.Path(o)
		for _, r := range []image.Rectangle{
			image.Rect(panel.Max.X-25, panel.Min.Y, panel.Max.X, panel.Max.Y),
		} {
			c := clip.Rect(r).Push(o)
			paint.FillShape(o, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, clip.Outline{Path: spec}.Op())
			c.Pop()
		}
		if wide {
			c := clip.Rect(image.Rect(panel.Min.X, panel.Min.Y, panel.Max.X-25, panel.Max.Y)).Push(o)
			paint.FillShape(o, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, clip.Outline{Path: spec}.Op())
			c.Pop()
		}
	}
	f := shot(func(o *op.Ops) { corner(o, true) })
	g := shot(func(o *op.Ops) { corner(o, false) })
	strip := image.Rect(panel.Max.X-25, panel.Min.Y, panel.Max.X, panel.Max.Y)
	t.Logf("panel corners, stencil region whole panel vs the strip: %s", diffStats(f, g, strip))
}

// ringBandsProbe is the four bands around a ring stroked on r (radius
// rad, half width at most hw) that hold all of its coverage: the top and
// bottom with the corners, the sides between.
func ringBandsProbe(r image.Rectangle, rad, hw int) []image.Rectangle {
	return []image.Rectangle{
		image.Rect(r.Min.X-hw, r.Min.Y-hw, r.Max.X+hw, r.Min.Y+rad),
		image.Rect(r.Min.X-hw, r.Min.Y+rad, r.Min.X+hw, r.Max.Y-rad),
		image.Rect(r.Max.X-hw, r.Min.Y+rad, r.Max.X+hw, r.Max.Y-rad),
		image.Rect(r.Min.X-hw, r.Max.Y-rad, r.Max.X+hw, r.Max.Y+hw),
	}
}

// noiseGradient paints a smooth, aubergine-like background: a small image
// upsampled ×8 with linear filtering, like Clack's window gradient.
func noiseGradient(w, h int) func(o *op.Ops) {
	sw, sh := w/8+1, h/8+1
	src := image.NewRGBA(image.Rect(0, 0, sw, sh))
	for y := 0; y < sh; y++ {
		for x := 0; x < sw; x++ {
			fx, fy := float64(x)/float64(sw), float64(y)/float64(sh)
			r := 0x3f + 0x30*math.Sin(fx*3.1+fy*1.3)
			g := 0x0e + 0x0a*math.Cos(fx*2.2-fy*2.9)
			b := 0x40 + 0x28*math.Sin(fy*4.1+fx*0.7)
			src.Pix[(y*sw+x)*4+0] = uint8(r)
			src.Pix[(y*sw+x)*4+1] = uint8(g)
			src.Pix[(y*sw+x)*4+2] = uint8(b)
			src.Pix[(y*sw+x)*4+3] = 0xff
		}
	}
	img := paint.NewImageOp(src)
	img.Filter = paint.FilterLinear
	return func(o *op.Ops) {
		st := op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(8, 8))).Push(o)
		img.Add(o)
		paint.PaintOp{}.Add(o)
		st.Pop()
	}
}

func diffStats(a, b *image.RGBA, r image.Rectangle) string {
	n, maxd := 0, 0
	hist := [4]int{}
	var first image.Point
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i := a.PixOffset(x, y)
			d := 0
			for k := 0; k < 4; k++ {
				v := int(a.Pix[i+k]) - int(b.Pix[i+k])
				if v < 0 {
					v = -v
				}
				if v > d {
					d = v
				}
			}
			if d > 0 {
				if n == 0 {
					first = image.Pt(x, y)
				}
				n++
				if d > maxd {
					maxd = d
				}
				if d < 4 {
					hist[d]++
				} else {
					hist[0]++
				}
			}
		}
	}
	if n == 0 {
		return fmt.Sprintf("identical (%d px)", r.Dx()*r.Dy())
	}
	return fmt.Sprintf("%d of %d px differ, max %d (by 1: %d, by 2: %d, by 3: %d, more: %d), first at %v", n, r.Dx()*r.Dy(), maxd, hist[1], hist[2], hist[3], hist[0], first)
}

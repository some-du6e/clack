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
	"gioui.org/gpu"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// Helpers of the Clack frame-cost tests (patches 36-43).

// clackWindow is a headless window for a pixel test: without a GPU the
// test is skipped, unless CLACK_PIXEL_TESTS says ci has one.
func clackWindow(t *testing.T, w, h int) *Window {
	t.Helper()
	win, err := NewWindow(w, h)
	if err != nil {
		if os.Getenv("CLACK_PIXEL_TESTS") != "" {
			t.Fatalf("headless GPU: %v", err)
		}
		t.Skipf("no headless GPU: %v", err)
	}
	return win
}

// clackFrames draws draw's ops in n frames and returns each one's pixels
// and stats.
func clackFrames(t *testing.T, win *Window, n int, draw func(o *op.Ops)) ([]*image.RGBA, []gpu.FrameStats) {
	t.Helper()
	var o op.Ops
	draw(&o)
	var imgs []*image.RGBA
	var sts []gpu.FrameStats
	for range n {
		if err := win.Frame(&o); err != nil {
			t.Fatal(err)
		}
		img := image.NewRGBA(image.Rectangle{Max: win.size})
		if err := win.Screenshot(img); err != nil {
			t.Fatal(err)
		}
		imgs = append(imgs, img)
		sts = append(sts, win.ClackFrameStats())
	}
	return imgs, sts
}

// clackShot draws draw's ops twice (the second frame is the steady
// state: paths built, atlases sized) and returns the pixels and the
// second frame's stats.
func clackShot(t *testing.T, win *Window, draw func(o *op.Ops)) (*image.RGBA, gpu.FrameStats) {
	t.Helper()
	var o op.Ops
	draw(&o)
	for range 2 {
		if err := win.Frame(&o); err != nil {
			t.Fatal(err)
		}
	}
	sz := win.size
	img := image.NewRGBA(image.Rectangle{Max: sz})
	if err := win.Screenshot(img); err != nil {
		t.Fatal(err)
	}
	return img, win.ClackFrameStats()
}

// clackScene draws what Clack's window draws, under tr: a background
// image, the workspace's shadow (8 rings, as theme.Shadow at 1x), a
// rounded panel holding text-like paths, a rounded avatar image, rounded
// rects, a linear gradient, icons (strokes), nested opacity layers, and
// the dark mode outline.
func clackScene(w, h int, tr f32.Affine2D) func(o *op.Ops) {
	bg := noiseGradient(w, h)
	avatar := image.NewRGBA(image.Rect(0, 0, 48, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 48; x++ {
			avatar.SetRGBA(x, y, color.RGBA{R: uint8(x * 5), G: uint8(y * 5), B: 0x80, A: 0xff})
		}
	}
	av := paint.NewImageOp(avatar)
	av.Filter = paint.FilterLinear
	ws := image.Rect(68, 38, w-6, h-6)
	panel := image.Rect(330, 40, w-4, h-4)
	ink := color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xff}
	return func(o *op.Ops) {
		defer op.Affine(tr).Push(o).Pop()
		bg(o)
		for i := 1; i <= 8; i++ {
			f := float32(8-i+1) / 8
			ri := ws.Inset(-i)
			st := op.Offset(ri.Min).Push(o)
			rr := clip.UniformRRect(image.Rectangle{Max: ri.Size()}, 6+i)
			paint.FillShape(o, color.NRGBA{A: uint8(0x31 * f * f)}, clip.Stroke{Path: rr.Path(o), Width: 1.5}.Op())
			st.Pop()
		}
		pst := clip.RRect{Rect: panel, NE: 6, SE: 6}.Push(o)
		paint.FillShape(o, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, clip.Rect(panel).Op())
		paint.FillShape(o, color.NRGBA{R: 0xf8, G: 0xf8, B: 0xf8, A: 0xff}, clip.Rect(image.Rect(panel.Min.X, 300, panel.Max.X, 340)).Op())
		for k := 0; k < 120; k++ {
			x := panel.Min.X + 4 + (k*97)%(panel.Dx()-40)
			y := panel.Min.Y + 4 + (k*61)%(panel.Dy()-40)
			paint.FillShape(o, ink, clip.Ellipse(image.Rect(x, y, x+7+k%6, y+9+k%4)).Op(o))
			var p clip.Path
			p.Begin(o)
			p.MoveTo(f32.Pt(float32(x)+12.3, float32(y)+1.7))
			p.QuadTo(f32.Pt(float32(x)+30.1, float32(y)+4.2), f32.Pt(float32(x)+21.6, float32(y)+15.9))
			p.LineTo(f32.Pt(float32(x)+14.2, float32(y)+13.1))
			p.Close()
			paint.FillShape(o, color.NRGBA{R: 0x61, G: 0x60, B: 0x61, A: 0xff}, clip.Outline{Path: p.End()}.Op())
		}
		// an avatar
		ast := op.Offset(image.Pt(panel.Min.X+20, panel.Min.Y+60)).Push(o)
		cst := clip.UniformRRect(image.Rect(0, 0, 36, 36), 8).Push(o)
		sst := op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(0.75, 0.75))).Push(o)
		av.Add(o)
		paint.PaintOp{}.Add(o)
		sst.Pop()
		cst.Pop()
		ast.Pop()
		// rounded rects and icons
		for k := 0; k < 40; k++ {
			r := image.Rect(panel.Min.X+60+k*13, panel.Max.Y-80, panel.Min.X+70+k*13+k%5, panel.Max.Y-62)
			paint.FillShape(o, color.NRGBA{R: uint8(k * 6), G: 0x64, B: 0xa3, A: 0xff}, clip.UniformRRect(r, 4).Op(o))
			ic := clip.UniformRRect(image.Rect(r.Min.X, r.Min.Y-30, r.Min.X+12, r.Min.Y-18), 3)
			paint.FillShape(o, ink, clip.Stroke{Path: ic.Path(o), Width: 1.5}.Op())
		}
		// a pill across the panel's rounded corner: an intersection
		paint.FillShape(o, color.NRGBA{R: 0xe0, G: 0x1e, B: 0x5a, A: 0xff}, clip.UniformRRect(image.Rect(panel.Max.X-40, panel.Min.Y+2, panel.Max.X+10, panel.Min.Y+30), 8).Op(o))
		// a gradient
		gst := clip.Rect(image.Rect(panel.Min.X+100, panel.Min.Y+100, panel.Min.X+300, panel.Min.Y+140)).Push(o)
		paint.LinearGradientOp{
			Stop1: f32.Pt(float32(panel.Min.X+100), 0), Color1: color.NRGBA{R: 0x12, G: 0x64, B: 0xa3, A: 0xff},
			Stop2: f32.Pt(float32(panel.Min.X+300), 0), Color2: color.NRGBA{R: 0xe0, G: 0x1e, B: 0x5a, A: 0xc0},
		}.Add(o)
		paint.PaintOp{}.Add(o)
		gst.Pop()
		// nested opacity layers: a hover fade with siblings in it
		l0 := paint.PushOpacity(o, 0.9)
		for i, a := range []float32{0.5, 0.7, 0.35} {
			x := panel.Min.X + 340 + i*70
			l1 := paint.PushOpacity(o, a)
			paint.FillShape(o, color.NRGBA{R: 0x2e, G: 0xb6, B: 0x7d, A: 0xff}, clip.Rect(image.Rect(x, panel.Min.Y+160, x+100, panel.Min.Y+220)).Op())
			paint.FillShape(o, ink, clip.UniformRRect(image.Rect(x+10, panel.Min.Y+170, x+60, panel.Min.Y+200), 9).Op(o))
			if i == 1 {
				l2 := paint.PushOpacity(o, 0.6)
				paint.FillShape(o, color.NRGBA{R: 0xec, G: 0xb2, B: 0x2e, A: 0xff}, clip.Ellipse(image.Rect(x+30, panel.Min.Y+180, x+90, panel.Min.Y+240)).Op(o))
				l2.Pop()
			}
			l1.Pop()
		}
		l0.Pop()
		pst.Pop()
		// the dark mode workspace outline
		ost := op.Offset(ws.Min).Push(o)
		paint.FillShape(o, color.NRGBA{R: 0x4a, G: 0x3e, B: 0x4f, A: 0xff}, clip.Stroke{Path: clip.UniformRRect(image.Rectangle{Max: ws.Size()}, 6).Path(o), Width: 1}.Op())
		ost.Pop()
	}
}

// diffCount is how many pixels of a and b differ and by how many levels
// at most.
func diffCount(a, b *image.RGBA) (n, maxd int) {
	for i := 0; i+3 < len(a.Pix); i += 4 {
		d := 0
		for k := 0; k < 4; k++ {
			d = max(d, absDiff(a.Pix[i+k], b.Pix[i+k]))
		}
		if d > 0 {
			n++
			maxd = max(maxd, d)
		}
	}
	return n, maxd
}

// worstPixels lists the n pixels of r that differ most between a and b.
func worstPixels(a, b *image.RGBA, r image.Rectangle, n int) string {
	type px struct {
		p    image.Point
		d    int
		a, b color.RGBA
	}
	var top []px
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			ca, cb := a.RGBAAt(x, y), b.RGBAAt(x, y)
			d := max(absDiff(ca.R, cb.R), absDiff(ca.G, cb.G), absDiff(ca.B, cb.B), absDiff(ca.A, cb.A))
			if d == 0 || (len(top) == n && d <= top[n-1].d) {
				continue
			}
			top = append(top, px{image.Pt(x, y), d, ca, cb})
			for i := len(top) - 1; i > 0 && top[i].d > top[i-1].d; i-- {
				top[i], top[i-1] = top[i-1], top[i]
			}
			if len(top) > n {
				top = top[:n]
			}
		}
	}
	s := ""
	for _, t := range top {
		s += fmt.Sprintf(" %v by %d (%v vs %v);", t.p, t.d, t.a, t.b)
	}
	return s
}

func absDiff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

func statLine(s gpu.FrameStats) string {
	return fmt.Sprintf("%d draws, %d passes, %d API calls (%d left out, %d crossings), %d paths built, %d blits, %d covers, %d intersections, %d paths stenciled; stencil %.2f Mpx, cover %.2f, blit %.2f, intersect %.2f, cleared %.2f, atlas %.2f",
		s.Draws, s.Passes, s.APICalls, s.Skipped, s.Crossings, s.PathsBuilt, s.Blits, s.Covers, s.Intersections, s.StencilPaths,
		float64(s.StencilPx)/1e6, float64(s.CoverPx)/1e6, float64(s.BlitPx)/1e6, float64(s.IntersectPx)/1e6, float64(s.ClearPx)/1e6, float64(s.AtlasPx)/1e6)
}

func srgbToLinear(v uint8) float64 {
	c := float64(v) / 255
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// noiseGradient draws a smooth, aubergine-like background like the
// window's: a small image upsampled ×8 with linear filtering.
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

// diffStats describes how a and b differ in r.
func diffStats(a, b *image.RGBA, r image.Rectangle) string {
	n, maxd := 0, 0
	hist := [4]int{}
	var first image.Point
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i := a.PixOffset(x, y)
			d := 0
			for k := 0; k < 4; k++ {
				d = max(d, absDiff(a.Pix[i+k], b.Pix[i+k]))
			}
			if d > 0 {
				if n == 0 {
					first = image.Pt(x, y)
				}
				n++
				maxd = max(maxd, d)
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

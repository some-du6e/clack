package theme

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"testing"

	"gioui.org/f32"
	"gioui.org/gpu"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// The frame-cost work (2026-11-16; Gio patches 36-40, strokeRRect,
// FillPanel) must not change what the window looks like. These draw the
// workspace's shadow (light) or border (dark), a dialog's and the thread
// pane's shadows, and the rounded primary panel with content up to its
// edges, the old way (rings in one piece, one panel fill, Gio keeping
// every clip on every op over one stencil region: SetLegacyFrameCost,
// gpu.ClackLegacyClips) and the new way, and compare the pixels.
//
// What may differ: Gio's coverage of a path depends by a rounding on
// where its stencil lands in the atlas (the GPU interpolates from the
// atlas origin). Stenciling a ring in bands, or a clip in regions, moves
// it and every path packed after it, so edge pixels can come out a level
// or two apart, as they already do today whenever anything drawn before
// them changes. Each scene measures that floor itself: the old way drawn
// again after one more tiny path (the atlas shifted). The new way may
// differ from the old by no more levels than that and in at most a few
// times as many pixels.

type costScene struct {
	name  string
	dark  bool
	scale float32
	// sidebar: the panel's left corners are square (the sidebar's there)
	sidebar bool
}

func TestFrameCostPixels(t *testing.T) {
	var scenes []costScene
	for _, dark := range []bool{false, true} {
		for _, s := range []float32{1, 2} {
			for _, sb := range []bool{true, false} {
				mode := "light"
				if dark {
					mode = "dark"
				}
				side := "sidebar"
				if !sb {
					side = "no-sidebar"
				}
				scenes = append(scenes, costScene{name: fmt.Sprintf("%s-%gx-%s", mode, s, side), dark: dark, scale: s, sidebar: sb})
			}
		}
	}
	for _, sc := range scenes {
		t.Run(sc.name, func(t *testing.T) {
			old, oldSt, g := sc.render(t, true, false)
			shifted, _, _ := sc.render(t, true, true)
			cur, curSt, _ := sc.render(t, false, false)
			floor := costCompare(old, shifted, g)
			got := costCompare(old, cur, g)
			t.Logf("the old way after one more path (the noise floor): %s", floor)
			t.Logf("new against old: %s", got)
			t.Logf("old: %s", costStats(oldSt))
			t.Logf("new: %s", costStats(curSt))
			if lim := max(floor.max, 2); got.max > lim {
				t.Errorf("a pixel differs by %d levels, more than the %d placement alone moves one", got.max, lim)
			}
			if lim := 4*floor.n + 256; got.n > lim {
				t.Errorf("%d px differ, more than %d (4× the %d placement alone moves, plus 256)", got.n, lim, floor.n)
			}
			if oldSt.StencilPx > 0 && curSt.StencilPx*2 > oldSt.StencilPx {
				t.Errorf("stencil work %d px, not under half the old %d px", curSt.StencilPx, oldSt.StencilPx)
			}
		})
	}
}

// costGradient is a smooth, aubergine-like background like the window's:
// a small image upsampled ×8 with linear filtering.
func costGradient(w, h int) paint.ImageOp {
	sw, sh := w/8+1, h/8+1
	src := image.NewRGBA(image.Rect(0, 0, sw, sh))
	for y := 0; y < sh; y++ {
		for x := 0; x < sw; x++ {
			fx, fy := float64(x)/float64(sw), float64(y)/float64(sh)
			i := (y*sw + x) * 4
			src.Pix[i+0] = uint8(0x3f + 0x30*math.Sin(fx*3.1+fy*1.3))
			src.Pix[i+1] = uint8(0x0e + 0x0a*math.Cos(fx*2.2-fy*2.9))
			src.Pix[i+2] = uint8(0x40 + 0x28*math.Sin(fy*4.1+fx*0.7))
			src.Pix[i+3] = 0xff
		}
	}
	img := paint.NewImageOp(src)
	img.Filter = paint.FilterLinear
	return img
}

// costGeom is where a scene's parts are, to say where pixels differ.
type costGeom struct {
	ws, panel, interior, pane, dialog image.Rectangle
	spread                            int // the dialog's shadow
}

// where names the part of the scene p is in.
func (g costGeom) where(p image.Point) string {
	switch {
	case p.In(g.dialog):
		return "dialog"
	case p.In(g.dialog.Inset(-g.spread - 2)):
		return "dialog shadow"
	case p.In(g.interior) && p.In(g.pane):
		return "thread pane"
	case p.In(g.interior):
		return "panel interior"
	case p.In(g.panel):
		return "panel edge"
	case p.In(g.ws):
		return "sidebar"
	}
	return "workspace shadow"
}

type costResult struct {
	n, max int
	parts  map[string]int
	worst  string
}

func (r costResult) String() string {
	if r.n == 0 {
		return "identical"
	}
	return fmt.Sprintf("%d px differ, max %d; by part %v; most:%s", r.n, r.max, r.parts, r.worst)
}

func costCompare(a, b *image.RGBA, g costGeom) costResult {
	res := costResult{parts: map[string]int{}}
	type px struct {
		p    image.Point
		d    int
		a, b color.RGBA
	}
	var top []px
	r := a.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i := a.PixOffset(x, y)
			d := 0
			for k := 0; k < 4; k++ {
				v := int(a.Pix[i+k]) - int(b.Pix[i+k])
				d = max(d, v, -v)
			}
			if d == 0 {
				continue
			}
			res.n++
			res.max = max(res.max, d)
			res.parts[g.where(image.Pt(x, y))]++
			if len(top) < 5 || d > top[len(top)-1].d {
				top = append(top, px{image.Pt(x, y), d, a.RGBAAt(x, y), b.RGBAAt(x, y)})
				for j := len(top) - 1; j > 0 && top[j].d > top[j-1].d; j-- {
					top[j], top[j-1] = top[j-1], top[j]
				}
				if len(top) > 5 {
					top = top[:5]
				}
			}
		}
	}
	for _, t := range top {
		res.worst += fmt.Sprintf(" %v (%s) by %d %v vs %v;", t.p, g.where(t.p), t.d, t.a, t.b)
	}
	return res
}

// render draws the scene the old way (legacy) or the new, after one more
// tiny path if shift (the atlas placement moved), and returns the pixels,
// the frame's stats and where the scene's parts are.
func (sc costScene) render(t *testing.T, legacy, shift bool) (*image.RGBA, gpu.FrameStats, costGeom) {
	SetLegacyFrameCost(legacy)
	gpu.ClackLegacyClips(legacy)
	defer SetLegacyFrameCost(false)
	defer gpu.ClackLegacyClips(false)
	return sc.draw(t, shift)
}

// draw renders the scene.
func (sc costScene) draw(t *testing.T, shift bool) (*image.RGBA, gpu.FrameStats, costGeom) {
	t.Helper()
	sz := image.Pt(int(1100*sc.scale), int(700*sc.scale))
	th := New(sc.dark)
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: sc.scale, PxPerSp: sc.scale}, Constraints: layout.Exact(sz)}
	grad := costGradient(sz.X, sz.Y)
	func() {
		defer op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(8, 8))).Push(gtx.Ops).Pop()
		grad.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
	}()
	if shift {
		// a path stenciled before all the others, nearly invisible
		paint.FillShape(gtx.Ops, color.NRGBA{R: 1, A: 1}, clip.Ellipse(image.Rect(1, 1, 9, 7)).Op(gtx.Ops))
	}
	navH, railW, inset := gtx.Dp(TopNavH), gtx.Dp(RailW), gtx.Dp(PanelInset)
	ws := image.Rect(railW, navH, sz.X-inset, sz.Y-inset)
	rad := gtx.Dp(PanelRadius)
	sbW := 0
	if sc.sidebar {
		sbW = gtx.Dp(260)
	}
	primary := image.Rect(ws.Min.X+sbW, ws.Min.Y, ws.Max.X, ws.Max.Y)
	nw, sw := 0, 0
	if sbW == 0 {
		nw, sw = rad, rad
	}
	rr := clip.RRect{Rect: primary, NW: nw, SW: sw, NE: rad, SE: rad}
	aubergine := color.NRGBA{R: 0x3f, G: 0x0e, B: 0x40, A: 0xff}
	pane := image.Rect(primary.Max.X-gtx.Dp(380), primary.Min.Y, primary.Max.X, primary.Max.Y)
	dlg := image.Rect(sz.X/2-gtx.Dp(260), sz.Y/2-gtx.Dp(180), sz.X/2+gtx.Dp(260), sz.Y/2+gtx.Dp(180))
	g := costGeom{ws: ws, panel: primary, interior: rr.Interior(), pane: pane, dialog: dlg, spread: gtx.Dp(24)}
	{
		// the workspace: Slack's shadow in light mode, its outline in dark
		if !sc.dark {
			Shadow(gtx, ws.Add(image.Pt(-gtx.Dp(2), -gtx.Dp(2))), rad, gtx.Dp(8), th.C.PanelShadow)
		}
		if sbW > 0 {
			Fill(gtx, image.Rect(ws.Min.X, ws.Min.Y, primary.Min.X, ws.Max.Y), Over(th.C.SidebarBg, aubergine))
		}
		func() {
			defer rr.Push(gtx.Ops).Pop()
			FillPanel(gtx, rr, th.C.Bg)
			// content: rows from edge to edge (a hovered message, a
			// divider), text-like shapes all over and up into the
			// corners, a scroll bar on the right
			w, h := primary.Dx(), primary.Dy()
			Fill(gtx, image.Rect(primary.Min.X, primary.Min.Y+h/3, primary.Max.X, primary.Min.Y+h/3+gtx.Dp(40)), th.C.BgHover)
			Fill(gtx, image.Rect(primary.Min.X, primary.Min.Y+gtx.Dp(49), primary.Max.X, primary.Min.Y+gtx.Dp(50)), th.C.Border)
			for k := 0; k < 240; k++ {
				x := primary.Min.X + (k*97)%w
				y := primary.Min.Y + (k*61)%h
				d := gtx.Dp(6) + k%7
				Circle(gtx, image.Pt(x+d/2, y+d/2), d, th.C.Fg)
				var p clip.Path
				p.Begin(gtx.Ops)
				fx, fy := float32(x)+0.3, float32(y)+0.7
				p.MoveTo(f32.Pt(fx+12.3, fy+1.7))
				p.QuadTo(f32.Pt(fx+30.1, fy+4.2), f32.Pt(fx+21.6, fy+15.9))
				p.LineTo(f32.Pt(fx+14.2, fy+13.1))
				p.Close()
				paint.FillShape(gtx.Ops, th.C.FgMuted, clip.Outline{Path: p.End()}.Op())
			}
			RRect(gtx, image.Rect(primary.Max.X-gtx.Dp(10), primary.Min.Y+gtx.Dp(60), primary.Max.X-gtx.Dp(2), primary.Min.Y+gtx.Dp(260)), gtx.Dp(4), th.C.ScrollThumb)
			// the thread pane's shadow over the conversation
			Shadow(gtx, pane, 0, gtx.Dp(8), WithAlpha(th.C.PanelShadow, 0x30))
			Fill(gtx, pane, th.C.Bg)
		}()
		if b := th.C.WorkspaceBorder; b.A > 0 {
			Border(gtx, ws, rad, gtx.Dp(1), Over(b, aubergine))
		}
		// a dialog and its shadow
		Shadow(gtx, dlg, gtx.Dp(8), gtx.Dp(24), LinearAlpha(color.NRGBA{A: 0x59}, th.C.Bg))
		RRect(gtx, dlg, gtx.Dp(8), th.C.BgRaised)
	}
	img, st := costShot(t, sz, &ops)
	return img, st, g
}

// costShot renders ops twice (the second frame's stats are the steady
// state's, the paths built) and returns the pixels and the stats.
func costShot(t *testing.T, sz image.Point, ops *op.Ops) (*image.RGBA, gpu.FrameStats) {
	t.Helper()
	if os.Getenv("CLACK_PIXEL_TESTS") == "" {
		t.Skip("CLACK_PIXEL_TESTS not set (ci's linux-gio pixel tests and flake-check's gpu-cost run this)")
	}
	w, err := pixeltest.NewWindow(sz.X, sz.Y)
	if err != nil {
		t.Fatalf("headless GPU: %v", err)
	}
	defer w.Release()
	w.ClackStatsDetail(true)
	for range 2 {
		if err := w.Frame(ops); err != nil {
			t.Fatal(err)
		}
	}
	img := image.NewRGBA(image.Rectangle{Max: sz})
	if err := w.Screenshot(img); err != nil {
		t.Fatal(err)
	}
	return img, w.ClackFrameStats()
}

func costStats(s gpu.FrameStats) string {
	return fmt.Sprintf("%d draws, %d passes, %d API calls (%d left out), %d blits, %d covers, %d intersections, %d paths stenciled; stencil %.2f Mpx, cover %.2f, intersect %.2f, cleared %.2f, atlas %.2f",
		s.Draws, s.Passes, s.APICalls, s.Skipped, s.Blits, s.Covers, s.Intersections, s.StencilPaths,
		float64(s.StencilPx)/1e6, float64(s.CoverPx)/1e6, float64(s.IntersectPx)/1e6, float64(s.ClearPx)/1e6, float64(s.AtlasPx)/1e6)
}

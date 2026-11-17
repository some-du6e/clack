package theme

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"runtime"
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

// The frame-cost work (2026-11-16/17; Gio patches 36-43 and theme's
// cached shapes) must not change what the window looks like. These draw
// the workspace's shadow (light) or outline (dark), a dialog's and the
// thread pane's shadows, and the rounded primary panel with content up to
// its edges (text-like paths, a rounded avatar image, an opacity layer),
// the old way (SetLegacyFrameCost, gpu.ClackLegacyClips,
// gpu.ClackLegacyLayers) and the new way, and compare the pixels: at 1x,
// 1.25x, 1.5x and 2x, over Slack's aubergine and over a light peach chrome
// like Karim's, and under the transforms Clack draws under besides an
// integer offset (a scale, an inspect shot's scale, a fraction of a
// pixel).
//
// What may differ: Gio's coverage of a path depends by a rounding on how
// its stencil is placed (its region's size and where it lands in the
// atlas), so drawing a ring around its hole or a clip in regions moves
// later paths and their edge pixels, as anything drawn before them moving
// in the atlas already does: thin strokes by up to 10 or so levels in a
// whole window. Each scene measures that floor itself, the old way again
// with a transparent path drawn first or after the shadow's rings (every
// path, or every later one, moved), and holds the new way to max(6, the
// floor's levels + 2), or max(8, …) on Windows, and to 0.5% of the pixels
// drawn over the background or 4× the floor's count and 256. The 6:
// llvmpipe and Metal differ by 4 at most. The 8: WARP differs by up to 6
// where only a shape's own stencil changed (the shadow's corner over a
// light chrome, drawn in regions; a dialog's corner, cached at the
// origin), which no move in the atlas shows, so it gets the same two
// levels of headroom; the seams and bands this guards were 12 to 35. Windows CI's WARP may differ by up to 12 levels on the panel's
// edge only: its old way is off there (the clip stenciled over the whole
// panel; (152,80) at 2x without a sidebar, fully inside the panel, came
// out 246 instead of 255), where the new way is exact. With the backends'
// state caches off (gpu.ClackStateCaches) the new way must come out
// exactly the same. Under a transform, Gio draws every op whole: the
// workspace with nothing over the panel but its fill must come out exactly
// as the old way, where a split made of abutting clips would blend twice
// along its seams.

// costTransform is a transform the whole window is drawn under.
type costTransform int

const (
	trNone     costTransform = iota
	trScale                  // ×1.37
	trInspect                // an inspect shot: the crop's corner to the origin, ×3200/side
	trFraction               // (0.4, 0.7) px
)

func (k costTransform) String() string {
	return [...]string{"", "scaled-1.37", "inspect-1.963", "fraction"}[k]
}

type costScene struct {
	name  string
	dark  bool
	scale float32
	// sidebar: the panel's left corners are square (the sidebar's there)
	sidebar bool
	// chrome: a flat colour behind the workspace, or Slack's aubergine
	// gradient if zero
	chrome color.NRGBA
	tr     costTransform
	// plain: nothing drawn over the panel but its fill
	plain bool
}

// peach is a chrome like Karim's custom theme (#FED4BE), where the
// workspace shadow shows about 3× as much as over aubergine.
var peach = color.NRGBA{R: 0xfe, G: 0xd4, B: 0xbe, A: 0xff}

func costScenes() []costScene {
	var scenes []costScene
	mode := map[bool]string{false: "light", true: "dark"}
	for _, dark := range []bool{false, true} {
		for _, s := range []float32{1, 1.25, 1.5, 2} {
			for _, sb := range []bool{true, false} {
				side := map[bool]string{true: "sidebar", false: "no-sidebar"}[sb]
				scenes = append(scenes, costScene{name: fmt.Sprintf("%s-%gx-%s", mode[dark], s, side), dark: dark, scale: s, sidebar: sb})
			}
		}
	}
	for _, s := range []float32{1, 2} {
		scenes = append(scenes, costScene{name: fmt.Sprintf("light-%gx-peach", s), scale: s, sidebar: true, chrome: peach})
	}
	for _, dark := range []bool{false, true} {
		for _, tr := range []costTransform{trScale, trInspect, trFraction} {
			scenes = append(scenes, costScene{name: fmt.Sprintf("%s-1x-%v", mode[dark], tr), dark: dark, scale: 1, sidebar: true, tr: tr})
		}
	}
	return scenes
}

func TestFrameCostPixels(t *testing.T) {
	for _, sc := range costScenes() {
		t.Run(sc.name, func(t *testing.T) {
			old, oldSt, g := sc.render(t, costOld, costProbe{})
			cur, curSt, _ := sc.render(t, costNew, costProbe{})
			bg, _, _ := sc.draw(t, costNew, costProbe{}, true)
			// the floor: the old way with every path moved in the atlas,
			// the shadow's rings too (a transparent path drawn first) or
			// those after them
			var floor costResult
			for _, r := range costProbes(g.size) {
				for _, first := range []bool{true, false} {
					moved, _, _ := sc.render(t, costOld, costProbe{r, first})
					f := costCompare(old, moved, g)
					t.Logf("the old way with a transparent %v path %s: %s", r.Size(), map[bool]string{true: "first", false: "after the shadow"}[first], f)
					if f.n > floor.n {
						floor.n = f.n
					}
					floor.max = max(floor.max, f.max)
				}
			}
			got := costCompare(old, cur, g)
			drawn := costCompare(old, bg, g).n
			t.Logf("new against old: %s", got)
			t.Logf("  by levels: %v (the floor: up to %d px, max %d)", got.hist(), floor.n, floor.max)
			t.Logf("%d px drawn over the background", drawn)
			t.Logf("old: %s", costStats(oldSt))
			t.Logf("new: %s", costStats(curSt))
			least := 6
			if runtime.GOOS == "windows" {
				// WARP moves a cached shape's own corner pixels by up to 6
				// levels where only its stencil changed (light-2x-peach's
				// shadow corner reads exactly 6 in ci's windows-applog), so
				// it gets two levels of headroom too; the seams and bands
				// this holds off were 12 to 35 levels
				least = 8
			}
			lim := max(least, floor.max+2)
			for part, m := range got.partMax {
				l := lim
				if part == "panel edge" && runtime.GOOS == "windows" {
					l = max(l, 12) // WARP's old way at the panel's corners, see above
				}
				if m > l {
					t.Errorf("a pixel of the %s differs by %d levels, more than %d (the floor's %d + 2, at least %d)", part, m, l, floor.max, least)
				}
			}
			if lim := max(4*floor.n+256, drawn/200); got.n > lim {
				t.Errorf("%d px differ, more than %d (0.5%% of the %d px drawn, or 4× the %d the floor moves plus 256)", got.n, lim, drawn, floor.n)
			}
			if sc.tr == trNone && oldSt.StencilPx > 0 && curSt.StencilPx*2 > oldSt.StencilPx {
				t.Errorf("stencil work %d px, not under half the old %d px", curSt.StencilPx, oldSt.StencilPx)
			}
			// the backends' state caches (Gio patch 37) change no pixel
			var uncached *image.RGBA
			var uncachedSt gpu.FrameStats
			func() {
				gpu.ClackStateCaches(false)
				// a failing render (t.Fatal) mustn't leave the caches off
				// for the rest of the test binary
				defer gpu.ClackStateCaches(true)
				uncached, uncachedSt, _ = sc.draw(t, costNew, costProbe{}, false)
			}()
			if c := costCompare(uncached, cur, g); c.n > 0 {
				t.Errorf("with the state caches on %d px differ from them off: %s", c.n, c)
			}
			if curSt.APICalls >= 0 && (uncachedSt.Skipped != 0 || curSt.Skipped == 0) {
				t.Errorf("calls left out: %d with the caches off, %d on", uncachedSt.Skipped, curSt.Skipped)
			}
			if sc.tr != trNone {
				// the workspace with nothing over the panel but its fill:
				// under a transform every op is drawn whole, one region each,
				// so it comes out exactly as the old way; a split into
				// abutting fills or bands would blend twice along its seams
				plain := sc
				plain.plain = true
				po, _, pg := plain.render(t, costOld, costProbe{})
				pn, _, _ := plain.render(t, costNew, costProbe{})
				if c := costCompare(po, pn, pg); c.n > 0 {
					t.Errorf("under the transform the panel's fill, the shadow and the outline differ from the old way: %s (seams?)", c)
				}
			}
		})
	}
}

// costProbe is a transparent path drawn in a scene, first (before the
// shadow's rings) or after the rings, so every path after it lands
// elsewhere in the atlas and nothing else changes.
type costProbe struct {
	r     image.Rectangle
	first bool
}

// costProbes are the transparent paths of the floor: a tiny one, a third
// of the window, nearly all of it, of odd sizes, so the paths after them
// move by odd amounts too.
func costProbes(size image.Point) []image.Rectangle {
	return []image.Rectangle{
		image.Rect(1, 1, 8, 6),
		image.Rect(1, 1, size.X/3|1, size.Y/3|1),
		image.Rect(0, 0, size.X-1, size.Y-1),
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

// costGeom is where a scene's parts are, in its own coordinates, and
// how they map to the window's pixels.
type costGeom struct {
	ws, panel, interior, pane, dialog image.Rectangle
	spread                            int // the dialog's shadow
	size                              image.Point
	tr                                f32.Affine2D // scene to window
}

// where names the part of the scene the window pixel p is in.
func (g costGeom) where(p image.Point) string {
	q := g.tr.Invert().Transform(f32.Pt(float32(p.X)+.5, float32(p.Y)+.5))
	s := image.Pt(int(math.Floor(float64(q.X))), int(math.Floor(float64(q.Y))))
	switch {
	case s.In(g.dialog):
		return "dialog"
	case s.In(g.dialog.Inset(-g.spread - 2)):
		return "dialog shadow"
	case s.In(g.interior) && s.In(g.pane):
		return "thread pane"
	case s.In(g.interior):
		return "panel interior"
	case s.In(g.panel):
		return "panel edge"
	case s.In(g.ws):
		return "sidebar"
	}
	return "workspace shadow"
}

type costResult struct {
	n, max  int
	parts   map[string]int
	partMax map[string]int
	levels  map[int]int
	worst   string
}

func (r costResult) String() string {
	if r.n == 0 {
		return "identical"
	}
	return fmt.Sprintf("%d px differ, max %d; by part %v; most:%s", r.n, r.max, r.parts, r.worst)
}

// hist is how many pixels differ by 1, 2, 3, 4-7, 8-15 and 16 levels or
// more.
func (r costResult) hist() string {
	var b [6]int
	for d, n := range r.levels {
		var i int
		switch {
		case d >= 16:
			i = 5
		case d >= 8:
			i = 4
		case d >= 4:
			i = 3
		default:
			i = d - 1
		}
		b[i] += n
	}
	return fmt.Sprintf("1: %d, 2: %d, 3: %d, 4-7: %d, 8-15: %d, 16+: %d", b[0], b[1], b[2], b[3], b[4], b[5])
}

func costCompare(a, b *image.RGBA, g costGeom) costResult {
	res := costResult{parts: map[string]int{}, partMax: map[string]int{}, levels: map[int]int{}}
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
			res.levels[d]++
			part := g.where(image.Pt(x, y))
			res.parts[part]++
			res.partMax[part] = max(res.partMax[part], d)
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

// costMode is the old way (every frame-cost change off) or the new.
type costMode struct{ legacy bool }

var (
	costOld = costMode{legacy: true}
	costNew = costMode{}
)

// render draws the scene the old way or the new, with the probe, and
// returns the pixels, the frame's stats and where the scene's parts are.
func (sc costScene) render(t *testing.T, m costMode, probe costProbe) (*image.RGBA, gpu.FrameStats, costGeom) {
	t.Helper()
	return sc.draw(t, m, probe, false)
}

// draw renders the scene, or only its background.
func (sc costScene) draw(t *testing.T, m costMode, probe costProbe, bgOnly bool) (*image.RGBA, gpu.FrameStats, costGeom) {
	t.Helper()
	SetLegacyFrameCost(m.legacy)
	gpu.ClackLegacyClips(m.legacy)
	gpu.ClackLegacyLayers(m.legacy)
	if !probe.first {
		SetFrameCostProbe(probe.r)
	}
	defer SetLegacyFrameCost(false)
	defer gpu.ClackLegacyClips(false)
	defer gpu.ClackLegacyLayers(false)
	defer SetFrameCostProbe(image.Rectangle{})
	sz := image.Pt(int(1100*sc.scale), int(700*sc.scale))
	tr, out := f32.Affine2D{}, sz
	switch sc.tr {
	case trScale:
		tr = f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(1.37, 1.37))
		out = image.Pt(int(math.Ceil(float64(sz.X)*1.37)), int(math.Ceil(float64(sz.Y)*1.37)))
	case trInspect:
		// a crop wider than 800 px at 1x, as inspect.Shoot draws it, at
		// the density of Karim's 1630 px rows (3200/1630)
		crop, k := image.Rect(300, 30, 1050, 520), float32(3200.0/1630)
		tr = f32.Affine2D{}.Offset(f32.Pt(-float32(crop.Min.X), -float32(crop.Min.Y))).Scale(f32.Point{}, f32.Pt(k, k))
		out = image.Pt(int(float32(crop.Dx())*k+.5), int(float32(crop.Dy())*k+.5))
	case trFraction:
		tr = f32.Affine2D{}.Offset(f32.Pt(0.4, 0.7))
	}
	th := New(sc.dark)
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: sc.scale, PxPerSp: sc.scale}, Constraints: layout.Exact(sz)}
	trs := op.Affine(tr).Push(gtx.Ops)
	aubergine := color.NRGBA{R: 0x3f, G: 0x0e, B: 0x40, A: 0xff}
	chrome := aubergine
	if sc.chrome.A > 0 {
		chrome = sc.chrome
		Fill(gtx, image.Rectangle{Max: sz}, chrome)
	} else {
		grad := costGradient(sz.X, sz.Y)
		func() {
			defer op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(8, 8))).Push(gtx.Ops).Pop()
			grad.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
		}()
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
	pane := image.Rect(primary.Max.X-gtx.Dp(380), primary.Min.Y, primary.Max.X, primary.Max.Y)
	dlg := image.Rect(sz.X/2-gtx.Dp(260), sz.Y/2-gtx.Dp(180), sz.X/2+gtx.Dp(260), sz.Y/2+gtx.Dp(180))
	g := costGeom{ws: ws, panel: primary, interior: rr.Interior(), pane: pane, dialog: dlg, spread: gtx.Dp(24), size: out, tr: tr}
	if !bgOnly {
		if probe.first && !probe.r.Empty() {
			paint.FillShape(gtx.Ops, color.NRGBA{}, clip.Ellipse(probe.r).Op(gtx.Ops))
		}
		// the workspace: Slack's shadow in light mode (8 rings at 1x),
		// its outline in dark
		if !sc.dark {
			Shadow(gtx, ws.Add(image.Pt(-gtx.Dp(2), -gtx.Dp(2))), rad, gtx.Dp(8), th.C.PanelShadow)
		}
		FrameCostProbe(gtx)
		if sbW > 0 {
			Fill(gtx, image.Rect(ws.Min.X, ws.Min.Y, primary.Min.X, ws.Max.Y), Over(th.C.SidebarBg, chrome))
		}
		func() {
			// as the window draws its panel (app.go layoutShell)
			defer rr.Push(gtx.Ops).Pop()
			Fill(gtx, primary, th.C.Bg)
			if sc.plain {
				return
			}
			// content: rows from edge to edge (a hovered message, a
			// divider), text-like shapes all over and up into the
			// corners, an avatar, a fading hover, a scroll bar
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
			func() {
				defer op.Offset(image.Pt(primary.Min.X+gtx.Dp(20), primary.Min.Y+gtx.Dp(70))).Push(gtx.Ops).Pop()
				Image(gtx, costAvatar(), image.Pt(gtx.Dp(36), gtx.Dp(36)), gtx.Dp(8))
			}()
			func() {
				l := paint.PushOpacity(gtx.Ops, 0.6)
				defer l.Pop()
				hover := image.Rect(primary.Min.X+gtx.Dp(80), primary.Min.Y+gtx.Dp(120), primary.Min.X+gtx.Dp(420), primary.Min.Y+gtx.Dp(170))
				RRect(gtx, hover, gtx.Dp(6), th.C.BgRaised)
				l2 := paint.PushOpacity(gtx.Ops, 0.5)
				Circle(gtx, image.Pt(hover.Min.X+gtx.Dp(30), hover.Min.Y+gtx.Dp(25)), gtx.Dp(28), th.C.Fg)
				l2.Pop()
			}()
			RRect(gtx, image.Rect(primary.Max.X-gtx.Dp(10), primary.Min.Y+gtx.Dp(60), primary.Max.X-gtx.Dp(2), primary.Min.Y+gtx.Dp(260)), gtx.Dp(4), th.C.ScrollThumb)
			// the thread pane's shadow over the conversation
			Shadow(gtx, pane, 0, gtx.Dp(8), WithAlpha(th.C.PanelShadow, 0x30))
			Fill(gtx, pane, th.C.Bg)
		}()
		if b := th.C.WorkspaceBorder; b.A > 0 {
			Border(gtx, ws, rad, gtx.Dp(1), Over(b, chrome))
		}
		if !sc.plain {
			// a dialog and its shadow
			Shadow(gtx, dlg, gtx.Dp(8), gtx.Dp(24), LinearAlpha(color.NRGBA{A: 0x59}, th.C.Bg))
			RRect(gtx, dlg, gtx.Dp(8), th.C.BgRaised)
		}
	}
	trs.Pop()
	img, st := costShot(t, out, &ops)
	return img, st, g
}

// costAvatar is a 48 px picture to draw as an avatar.
var costAvatar = func() func() paint.ImageOp {
	var img paint.ImageOp
	return func() paint.ImageOp {
		if img.Size() == (image.Point{}) {
			src := image.NewRGBA(image.Rect(0, 0, 48, 48))
			for y := 0; y < 48; y++ {
				for x := 0; x < 48; x++ {
					src.SetRGBA(x, y, color.RGBA{R: uint8(x * 5), G: uint8(y * 5), B: 0x80, A: 0xff})
				}
			}
			img = paint.NewImageOp(src)
		}
		return img
	}
}()

// costShot renders ops twice (the second frame's stats are the steady
// state's, the paths built) and returns the pixels and the stats.
func costShot(t *testing.T, sz image.Point, ops *op.Ops) (*image.RGBA, gpu.FrameStats) {
	t.Helper()
	if os.Getenv("CLACK_PIXEL_TESTS") == "" {
		t.Skip("CLACK_PIXEL_TESTS not set (ci's linux-gio, macos and windows pixel steps and flake-check's gpu-cost run this)")
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
	return fmt.Sprintf("%d draws, %d passes, %d API calls (%d left out, %d crossings), %d blits, %d covers, %d intersections, %d paths stenciled; stencil %.2f Mpx, cover %.2f, intersect %.2f, cleared %.2f, atlas %.2f, %d layer pages",
		s.Draws, s.Passes, s.APICalls, s.Skipped, s.Crossings, s.Blits, s.Covers, s.Intersections, s.StencilPaths,
		float64(s.StencilPx)/1e6, float64(s.CoverPx)/1e6, float64(s.IntersectPx)/1e6, float64(s.ClearPx)/1e6, float64(s.AtlasPx)/1e6, s.LayerPages)
}

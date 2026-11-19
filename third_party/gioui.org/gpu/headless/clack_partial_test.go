// SPDX-License-Identifier: Unlicense OR MIT

package headless

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand"
	"runtime"
	"testing"

	"gioui.org/f32"
	"gioui.org/gpu"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// Clack patch 46: a partial frame (drawn again only where it differs from
// the last, scissored) is the whole frame's pixels, bit for bit, on every
// backend (ci's linux-gio, macos and windows-applog jobs;
// CLACK_PIXEL_TESTS). The whole frame is the one the same GPU draws at
// that point (gpu.ClackPartialWhole): with partial frames the atlas
// places stick from frame to frame, and a path's coverage depends on its
// place by a rounding, so the frame Gio would draw afresh can differ from
// both by that rounding (TestClackPlacementNoise's floor).

// partialVars are what a frame of partialScene changes against its base.
type partialVars struct {
	caret      bool    // the composer's caret (2×18 px)
	spinner    int     // a spinner's turn (0: none)
	emoji      int     // the live image's frame
	ring       uint8   // a shadow ring's alpha (0: its own)
	fade       float32 // the hover fade's opacity (0: 0.9)
	moved      int     // an ellipse in the panel moved by this many px
	first      bool    // a small op drawn before all others
	grow       int     // the first path grows: every later path moves in the atlas
	clearWhite bool    // a full-window opaque fill first: the frame's clear colour
	drop       bool    // one of the text-like paths missing
	swap       bool    // two overlapping ops drawn the other way round
	// probe: a transparent path stenciled first (1-3), after the shadow
	// rings (4-6) or last (7-9: the page's size changes, no place
	// moves), tiny, a third of the window or nearly all of it
	// (TestFrameCostWindow's costShift), or a 1×2 or 2×1 first (10, 11:
	// every later place moves by a pixel), so paths land elsewhere in the
	// atlas, near or far (the placement floor), and no pixel changes
	probe int
}

// partialLive is a live image of a few frames (an animated emoji), at
// frame *cur.
type partialLive struct {
	ops []paint.ImageOp
	cur *int
}

func (l partialLive) Image() paint.ImageOp { return l.ops[*l.cur%len(l.ops)] }

// partialScene draws what Clack's window draws (a background image, the
// workspace's shadow rings, a rounded panel holding text-like paths, an
// avatar in a rounded clip, an animated emoji, a pill across the panel's
// corner, a gradient, nested opacity layers, a composer and its caret, a
// spinner), with v's changes, under tr. Its images are made once, so
// their handles stay.
func partialScene(w, h int) func(o *op.Ops, tr f32.Affine2D, v partialVars) {
	bg := noiseGradient(w, h)
	avatar := image.NewRGBA(image.Rect(0, 0, 48, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 48; x++ {
			avatar.SetRGBA(x, y, color.RGBA{R: uint8(x * 5), G: uint8(y * 5), B: 0x80, A: 0xff})
		}
	}
	av := paint.NewImageOp(avatar)
	av.Filter = paint.FilterLinear
	var frames []paint.ImageOp
	for f := 0; f < 3; f++ {
		m := image.NewRGBA(image.Rect(0, 0, 22, 22))
		for y := 0; y < 22; y++ {
			for x := 0; x < 22; x++ {
				m.SetRGBA(x, y, color.RGBA{R: uint8(40 + 70*f), G: uint8(x * 11), B: uint8(y * 11), A: 0xff})
			}
		}
		frames = append(frames, paint.NewImageOp(m))
	}
	cur := new(int)
	live := partialLive{ops: frames, cur: cur}
	ws := image.Rect(60, 30, w-6, h-6)
	panel := image.Rect(240, 32, w-4, h-4)
	ink := color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xff}
	return func(o *op.Ops, tr f32.Affine2D, v partialVars) {
		*cur = v.emoji
		defer op.Affine(tr).Push(o).Pop()
		if v.clearWhite {
			paint.Fill(o, color.NRGBA{R: 0xff, G: 0xfe, B: 0xfd, A: 0xff})
		}
		bg(o)
		probe := func() {
			r := [...]image.Rectangle{
				image.Rect(1, 1, 8, 6),
				image.Rect(1, 1, w/3|1, h/3|1),
				image.Rect(0, 0, w-1, h-1),
			}[(v.probe-1)%3]
			switch v.probe {
			case 10:
				r = image.Rect(1, 1, 2, 3)
			case 11:
				r = image.Rect(1, 1, 3, 2)
			}
			paint.FillShape(o, color.NRGBA{}, clip.Ellipse(r).Op(o))
		}
		if v.probe >= 1 && v.probe <= 3 || v.probe >= 10 {
			probe()
		}
		if v.first {
			paint.FillShape(o, color.NRGBA{R: 0xe0, G: 0x1e, B: 0x5a, A: 0xff}, clip.Rect(image.Rect(2, 2, 10, 6)).Op())
		}
		paint.FillShape(o, color.NRGBA{R: 0xec, G: 0xb2, B: 0x2e, A: 0xff}, clip.Ellipse(image.Rect(8, 60, 30+v.grow, 74+v.grow/2)).Op(o))
		for i := 1; i <= 4; i++ {
			a := uint8(0x31 * (5 - i) / 4)
			if i == 2 && v.ring != 0 {
				a = v.ring
			}
			ri := ws.Inset(-i)
			st := op.Offset(ri.Min).Push(o)
			rr := clip.UniformRRect(image.Rectangle{Max: ri.Size()}, 6+i)
			paint.FillShape(o, color.NRGBA{A: a}, clip.Stroke{Path: rr.Path(o), Width: 1.5}.Op())
			st.Pop()
		}
		if v.probe >= 4 && v.probe <= 6 {
			probe()
		}
		pst := clip.RRect{Rect: panel, NE: 6, SE: 6}.Push(o)
		paint.FillShape(o, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, clip.Rect(panel).Op())
		for k := 0; k < 60; k++ {
			if v.drop && k == 17 {
				continue
			}
			x := panel.Min.X + 4 + (k*97)%(panel.Dx()-40)
			y := panel.Min.Y + 4 + (k*61)%(panel.Dy()-100)
			paint.FillShape(o, ink, clip.Ellipse(image.Rect(x, y, x+7+k%6, y+9+k%4)).Op(o))
			var p clip.Path
			p.Begin(o)
			p.MoveTo(f32.Pt(float32(x)+12.3, float32(y)+1.7))
			p.QuadTo(f32.Pt(float32(x)+30.1, float32(y)+4.2), f32.Pt(float32(x)+21.6, float32(y)+15.9))
			p.LineTo(f32.Pt(float32(x)+14.2, float32(y)+13.1))
			p.Close()
			paint.FillShape(o, color.NRGBA{R: 0x61, G: 0x60, B: 0x61, A: 0xff}, clip.Outline{Path: p.End()}.Op())
		}
		mx := panel.Min.X + 200 + v.moved
		paint.FillShape(o, color.NRGBA{R: 0x12, G: 0x64, B: 0xa3, A: 0xff}, clip.Ellipse(image.Rect(mx, panel.Min.Y+200, mx+31, panel.Min.Y+219)).Op(o))
		// an avatar in a rounded clip, scaled
		ast := op.Offset(image.Pt(panel.Min.X+20, panel.Min.Y+60)).Push(o)
		cst := clip.UniformRRect(image.Rect(0, 0, 36, 36), 8).Push(o)
		sst := op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(0.75, 0.75))).Push(o)
		av.Add(o)
		paint.PaintOp{}.Add(o)
		sst.Pop()
		cst.Pop()
		ast.Pop()
		// the animated emoji, in a rounded clip (a path its frames redraw)
		est := op.Offset(image.Pt(panel.Min.X+300, panel.Min.Y+40)).Push(o)
		ecl := clip.UniformRRect(image.Rect(0, 0, 22, 22), 5).Push(o)
		paint.NewLiveImageOp(live).Add(o)
		paint.PaintOp{}.Add(o)
		ecl.Pop()
		est.Pop()
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
		// two overlapping ops, in either order
		ra := clip.Rect(image.Rect(panel.Min.X+30, panel.Min.Y+260, panel.Min.X+90, panel.Min.Y+300))
		rb := clip.Rect(image.Rect(panel.Min.X+60, panel.Min.Y+280, panel.Min.X+120, panel.Min.Y+320))
		ca, cb := color.NRGBA{R: 0x2e, G: 0xb6, B: 0x7d, A: 0xd0}, color.NRGBA{R: 0x7c, G: 0x3d, B: 0x85, A: 0xd0}
		if v.swap {
			paint.FillShape(o, cb, rb.Op())
			paint.FillShape(o, ca, ra.Op())
		} else {
			paint.FillShape(o, ca, ra.Op())
			paint.FillShape(o, cb, rb.Op())
		}
		// nested opacity layers: a hover fade with siblings in it
		fade := v.fade
		if fade == 0 {
			fade = 0.9
		}
		l0 := paint.PushOpacity(o, fade)
		for i, a := range []float32{0.5, 0.7, 0.35} {
			x := panel.Min.X + 150 + i*70
			l1 := paint.PushOpacity(o, a)
			paint.FillShape(o, color.NRGBA{R: 0x2e, G: 0xb6, B: 0x7d, A: 0xff}, clip.Rect(image.Rect(x, panel.Min.Y+340, x+100, panel.Min.Y+400)).Op())
			paint.FillShape(o, ink, clip.UniformRRect(image.Rect(x+10, panel.Min.Y+350, x+60, panel.Min.Y+380), 9).Op(o))
			if i == 1 {
				l2 := paint.PushOpacity(o, 0.6)
				paint.FillShape(o, color.NRGBA{R: 0xec, G: 0xb2, B: 0x2e, A: 0xff}, clip.Ellipse(image.Rect(x+30, panel.Min.Y+360, x+90, panel.Min.Y+420)).Op(o))
				l2.Pop()
			}
			l1.Pop()
		}
		l0.Pop()
		// the composer: its box and caret
		box := image.Rect(panel.Min.X+20, panel.Max.Y-90, panel.Max.X-20, panel.Max.Y-20)
		bst := op.Offset(box.Min).Push(o)
		paint.FillShape(o, color.NRGBA{R: 0x86, G: 0x86, B: 0x86, A: 0xff}, clip.Stroke{Path: clip.UniformRRect(image.Rectangle{Max: box.Size()}, 8).Path(o), Width: 1}.Op())
		bst.Pop()
		if v.caret {
			paint.FillShape(o, ink, clip.Rect(image.Rect(box.Min.X+40, box.Min.Y+20, box.Min.X+42, box.Min.Y+38)).Op())
		}
		if v.spinner > 0 {
			c := f32.Pt(float32(box.Max.X-30), float32(box.Min.Y+30))
			for k, sweep := range []float32{math.Pi / 2, math.Pi / 3} {
				a0 := float64(v.spinner)*0.45 + float64(k)*2
				var p clip.Path
				p.Begin(o)
				p.MoveTo(f32.Pt(c.X+7*float32(math.Cos(a0)), c.Y+7*float32(math.Sin(a0))))
				p.ArcTo(c, c, sweep)
				paint.FillShape(o, color.NRGBA{R: 0x12, G: 0x64, B: 0xa3, A: 0xff}, clip.Stroke{Path: p.End(), Width: 2}.Op())
			}
		}
		pst.Pop()
		// the dark mode workspace outline
		ost := op.Offset(ws.Min).Push(o)
		paint.FillShape(o, color.NRGBA{R: 0x4a, G: 0x3e, B: 0x4f, A: 0xff}, clip.Stroke{Path: clip.UniformRRect(image.Rectangle{Max: ws.Size()}, 6).Path(o), Width: 1}.Op())
		ost.Pop()
		if v.probe >= 7 && v.probe <= 9 {
			probe()
		}
	}
}

// partialStep is a frame of a sequence and what it must be: partial (1),
// whole (2) or either (0), and the most damage it may have (-1: any).
type partialStep struct {
	name    string
	tr      f32.Affine2D
	v       partialVars
	want    int
	maxDmg  int64
	minFall bool // must cost much less than a whole frame (draws, API calls)
}

// partialRun draws steps in win, each a fresh op list as every frame of a
// window is, and returns each frame's pixels and stats.
func partialRun(t *testing.T, win *Window, scene func(o *op.Ops, tr f32.Affine2D, v partialVars), steps []partialStep) ([]*image.RGBA, []gpu.FrameStats) {
	t.Helper()
	var o op.Ops
	var imgs []*image.RGBA
	var sts []gpu.FrameStats
	for _, s := range steps {
		o.Reset()
		scene(&o, s.tr, s.v)
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

// TestClackPartialFrames draws a sequence of frames, each a change a real
// window sees (a caret's blink, a spinner's turn, an animated emoji's next
// frame, a hover's colour, a fade, an op moved, added, dropped or drawn in
// another order, a path that grows before every other, a new clear
// colour, a transform), three ways from the same start: whole
// (gpu.ClackPartialWhole), partial into the texture target, and partial
// through the frame texture and its copy into the target (a window's
// way). Every frame must have the whole frame's pixels exactly; the
// frames a small change made must be partial, with a small damage, and
// cost a fraction of the whole frame.
func TestClackPartialFrames(t *testing.T) {
	const w, h = 800, 600
	win := clackWindow(t, w, h)
	defer win.Release()
	defer gpu.ClackPartialTextures(false)
	defer gpu.ClackFrameTexture(false)
	defer gpu.ClackPartialWhole(false)
	scene := partialScene(w, h)
	id := f32.Affine2D{}
	scaled := f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(1.37, 1.37))
	frac := f32.Affine2D{}.Offset(f32.Pt(0.375, 0.625))
	steps := []partialStep{
		{"the first frame", id, partialVars{}, 2, -1, false},
		{"the caret shows", id, partialVars{caret: true}, 1, 64, true},
		{"the caret hides", id, partialVars{}, 1, 64, true},
		{"the same frame again", id, partialVars{}, 1, 0, true},
		{"a spinner appears", id, partialVars{spinner: 1}, 1, -1, false},
		{"the spinner turns", id, partialVars{spinner: 2}, 1, -1, false},
		{"the spinner turns, the caret shows", id, partialVars{spinner: 3, caret: true}, 1, -1, false},
		{"the emoji's next frame", id, partialVars{spinner: 3, caret: true, emoji: 1}, 1, 22 * 22, true},
		{"a ring's colour", id, partialVars{spinner: 3, caret: true, emoji: 1, ring: 0x50}, 1, -1, false},
		{"the fade's opacity", id, partialVars{spinner: 3, caret: true, emoji: 1, fade: 0.5}, 1, -1, false},
		{"an op before all others", id, partialVars{spinner: 3, emoji: 2, first: true}, 1, -1, false},
		{"an ellipse moves", id, partialVars{spinner: 3, emoji: 2, moved: 3}, 1, -1, false},
		{"two ops change places", id, partialVars{spinner: 3, emoji: 2, moved: 3, swap: true}, 1, -1, false},
		// the paths after it keep their atlas places (sticky): only its
		// own ops are damage
		{"a path drops out", id, partialVars{spinner: 3, emoji: 2, moved: 3, swap: true, drop: true}, 1, 2000, false},
		{"the first path grows", id, partialVars{grow: 9}, 1, -1, false},
		{"a new clear colour", id, partialVars{clearWhite: true}, 2, -1, false},
		{"the caret, on that clear colour", id, partialVars{clearWhite: true, caret: true}, 1, 64, true},
		{"scaled", scaled, partialVars{}, 2, -1, false},
		{"scaled, the caret", scaled, partialVars{caret: true}, 0, -1, false},
		{"scaled, the emoji", scaled, partialVars{caret: true, emoji: 1}, 0, -1, false},
		{"at a fraction of a pixel", frac, partialVars{}, 2, -1, false},
		{"at a fraction of a pixel, the caret", frac, partialVars{caret: true}, 0, -1, false},
		{"back at an integer offset", id, partialVars{}, 2, -1, false},
		// whole frames in a row (a scroll's): through the frame texture,
		// then drawn straight into the target; the first small change
		// after them straight in too (a still frame in a scroll), the
		// second whole into the frame texture again, the third partial
		{"scaled again, a whole frame after a whole one", scaled, partialVars{}, 2, -1, false},
		{"back again, a whole frame after a whole one", id, partialVars{}, 2, -1, false},
		{"the caret again", id, partialVars{caret: true}, 1, 64, true},
		{"the caret hides again", id, partialVars{}, 1, 64, true},
		{"the caret shows a third time", id, partialVars{caret: true}, 1, 64, true},
	}
	// a random walk of the same changes after them
	rng := rand.New(rand.NewSource(45))
	v := partialVars{}
	for i := 0; i < 24; i++ {
		switch rng.Intn(8) {
		case 0:
			v.caret = !v.caret
		case 1:
			v.spinner = (v.spinner + 1) % 5
		case 2:
			v.emoji++
		case 3:
			v.moved = rng.Intn(6)
		case 4:
			v.swap = !v.swap
		case 5:
			v.drop = !v.drop
		case 6:
			v.fade = []float32{0, 0.5, 0.75}[rng.Intn(3)]
		case 7:
			v.ring = []uint8{0, 0x20, 0x50}[rng.Intn(3)]
		}
		steps = append(steps, partialStep{fmt.Sprintf("random step %d", i), id, v, 0, -1, false})
	}

	// the whole frames: partial frames' state, every frame drawn whole
	// straight into the target
	gpu.ClackPartialTextures(true)
	gpu.ClackPartialWhole(true)
	gpu.ClackPartialReset()
	whole, ws := partialRun(t, win, scene, steps)
	gpu.ClackPartialWhole(false)
	for i, st := range ws {
		if st.Partial {
			t.Fatalf("%s: a partial frame drawn whole", steps[i].name)
		}
	}
	// Gio's own frames (partial frames off: every region packed afresh in
	// the frame's order), and the same with transparent paths that move
	// the others in the atlas (partialVars.probe) or with the atlas packed
	// in three other orders, which moves anti-aliased edges by a rounding
	// (the placement floor). The sticky places must
	// stay within max(floorBase, floor+2) levels of Gio's own, in twice the
	// floor's pixels and 1,024 more (gio-atlas-placement-noise). WARP's
	// base is 8: placement moves its edges by 7 levels in
	// TestFrameCostWindow's own floor (gpu-cost: "no clip after one more
	// path first"), and this scene's moved ellipse shows it at one edge
	// where no probe does (Gio's own 0.97 coverage on its second row, the
	// sticky place's 1.0).
	floorBase := 6
	if runtime.GOOS == "windows" {
		floorBase = 8
	}
	gpu.ClackPartialTextures(false)
	fresh, fs := partialRun(t, win, scene, steps)
	var probed [14][]*image.RGBA
	for k := range probed {
		if k >= 11 {
			// Gio's own frames with the atlas packed in another order
			// (gpu.ClackPackOrder): every path on other texels, as
			// sticky places put them
			gpu.ClackPackOrder(int64(k - 10))
			probed[k], _ = partialRun(t, win, scene, steps)
			gpu.ClackPackOrder(0)
			continue
		}
		ps := append([]partialStep(nil), steps...)
		for i := range ps {
			ps[i].v.probe = k + 1
		}
		probed[k], _ = partialRun(t, win, scene, ps)
	}
	worstN, worstM, floorWorst, shuffleWorst := 0, 0, 0, 0
	for i := range steps {
		if fs[i].Partial {
			t.Fatalf("%s: a partial frame with partial frames off", steps[i].name)
		}
		fn, fm := 0, 0
		for k := range probed {
			n, m := diffCount(fresh[i], probed[k][i])
			fn, fm = max(fn, n), max(fm, m)
			if k >= 11 {
				shuffleWorst = max(shuffleWorst, m)
			}
		}
		floorWorst = max(floorWorst, fm)
		n, m := diffCount(fresh[i], whole[i])
		worstN, worstM = max(worstN, n), max(worstM, m)
		if m > max(floorBase, fm+2) || n > 2*fn+1024 {
			t.Errorf("%s: the sticky whole frame differs from Gio's own in %d px by up to %d levels; a path moved in the atlas moves %d px by up to %d (want %d levels and %d px at most):%s",
				steps[i].name, n, m, fn, fm, max(floorBase, fm+2), 2*fn+1024, worstPixels(fresh[i], whole[i], whole[i].Bounds(), 6))
		}
		if i < 4 || i%8 == 0 || n > 0 {
			t.Logf("%s: sticky against Gio's own: %d px by up to %d levels (the floor: %d px by up to %d)", steps[i].name, n, m, fn, fm)
		}
	}
	t.Logf("sticky against Gio's own, the worst step: %d px, %d levels; the floor up to %d levels (the atlas packed in other orders: %d)", worstN, worstM, floorWorst, shuffleWorst)
	for _, mode := range []struct {
		name                 string
		textures, frameTexes bool
	}{
		{"partial frames into the target", true, false},
		{"partial frames through the frame texture", false, true},
	} {
		gpu.ClackPartialTextures(mode.textures)
		gpu.ClackFrameTexture(mode.frameTexes)
		gpu.ClackPartialReset()
		imgs, sts := partialRun(t, win, scene, steps)
		partials, copied, direct := 0, 0, 0
		stale, smallAfter := false, 0 // the frame texture stale; small changes since
		for i, img := range imgs {
			s, st := steps[i], sts[i]
			if n, m := diffCount(whole[i], img); n > 0 {
				t.Errorf("%s, %s: %d px differ from the whole frame by up to %d levels (partial %v, %d rects, %d px damaged, %d px copied):%s",
					mode.name, s.name, n, m, st.Partial, st.DamageRects, st.DamagePx, st.CopyPx, worstPixels(whole[i], img, img.Bounds(), 6))
			}
			if st.Partial {
				partials++
			}
			// through the frame texture: a whole frame after a whole frame
			// (a scroll's) draws straight into the target, with no copy,
			// and leaves the frame texture stale, so the next small change
			// is drawn whole into it again (and the one after is partial)
			// the frame texture's copy into the target: CopyPx less the clear
			// colour a partial frame copies into its damage
			framePx := st.CopyPx
			if st.Partial {
				framePx -= st.DamagePx
			}
			refill := false
			if mode.frameTexes {
				prev := gpu.FrameStats{Partial: true}
				if i > 0 {
					prev = sts[i-1]
				}
				// the small changes since the frame texture went stale: the
				// first is drawn straight in, the second fills it again
				refill = stale && !st.Partial && s.want == 1
				if refill {
					switch {
					case smallAfter == 0 && framePx != 0:
						t.Errorf("%s, %s: the first small change after frames drawn straight into the target went through the frame texture (%d px copied), want it straight in too", mode.name, s.name, framePx)
					case smallAfter == 1 && framePx != int64(w*h):
						t.Errorf("%s, %s: the second small change after frames drawn straight into the target copied %d px, want the frame texture filled again (%d)", mode.name, s.name, framePx, w*h)
					}
					smallAfter++
				}
				switch {
				case framePx == int64(w*h):
					copied++
				case framePx == 0 && !st.Partial:
					direct++
				default:
					t.Errorf("%s, %s: %d px of the frame texture copied (partial %v, %d px damaged), want the frame's %d or none", mode.name, s.name, framePx, st.Partial, st.DamagePx, w*h)
				}
				switch {
				case st.Partial && framePx == 0:
					t.Errorf("%s, %s: a partial frame not copied into the target", mode.name, s.name)
				case s.want == 2 && i > 0 && !prev.Partial && framePx != 0:
					t.Errorf("%s, %s: a whole frame after a whole frame went through the frame texture (%d px copied), want it drawn straight into the target", mode.name, s.name, framePx)
				case s.want == 2 && prev.Partial && framePx == 0:
					t.Errorf("%s, %s: a whole frame after a partial one drawn straight into the target, want it through the frame texture (the next may be partial)", mode.name, s.name)
				case st.Partial && stale:
					t.Errorf("%s, %s: a partial frame from a stale frame texture", mode.name, s.name)
				}
				// what the frame left: the frame texture filled, or stale
				switch {
				case framePx == int64(w*h):
					stale, smallAfter = false, 0
				case framePx == 0 && !st.Partial:
					if !refill {
						smallAfter = 0 // a whole frame drawn straight in: a stale run starts
					}
					stale = true
				}
			} else if framePx != 0 {
				t.Errorf("%s, %s: %d px of a frame texture copied, want none (the target keeps its pixels)", mode.name, s.name, framePx)
			}
			switch {
			case s.want == 1 && !st.Partial && !refill:
				t.Errorf("%s, %s: drawn whole, want a partial frame", mode.name, s.name)
			case s.want == 2 && st.Partial:
				t.Errorf("%s, %s: drawn partial (%d px damaged), want a whole frame", mode.name, s.name, st.DamagePx)
			}
			if st.Partial && s.maxDmg >= 0 && st.DamagePx > s.maxDmg {
				t.Errorf("%s, %s: %d px damaged in %d rects, want at most %d", mode.name, s.name, st.DamagePx, st.DamageRects, s.maxDmg)
			}
			if s.minFall && st.Partial {
				full := ws[i]
				if st.Draws*10 > full.Draws {
					t.Errorf("%s, %s: %d draws, the whole frame %d: want a tenth at most", mode.name, s.name, st.Draws, full.Draws)
				}
				if st.APICalls >= 0 && full.APICalls > 0 && st.APICalls*5 > full.APICalls {
					t.Errorf("%s, %s: %d API calls, the whole frame %d: want a fifth at most", mode.name, s.name, st.APICalls, full.APICalls)
				}
			}
			if i < 10 || st.Partial != (s.want == 1) {
				t.Logf("%s, %s: partial %v (%d rects, %d px damaged, %d px copied): %s", mode.name, s.name, st.Partial, st.DamageRects, st.DamagePx, st.CopyPx, statLine(st))
			}
		}
		t.Logf("%s: %d of %d frames partial", mode.name, partials, len(steps))
		if partials < len(steps)/3 {
			t.Errorf("%s: only %d of %d frames partial", mode.name, partials, len(steps))
		}
		if mode.frameTexes {
			// every copied frame matched the one drawn straight into the
			// target above: the copy (Metal's a draw, a blit elsewhere)
			// is exact
			t.Logf("%s: %d frames copied into the target, %d drawn straight into it, each the whole frame's pixels", mode.name, copied, direct)
			if copied < len(steps)/2 || direct == 0 {
				t.Errorf("%s: %d frames copied, %d drawn straight in; want most copied and some straight in", mode.name, copied, direct)
			}
		}
	}
	t.Logf("whole, the caret's frame: %s", statLine(ws[1]))
}

// Clack patch 46: a frame drawn again from the same ops (the client's
// quiet frames: an animation is all that changed) is a partial frame
// whose damage is the live image's, and is the whole frame's pixels; the
// same ops with nothing changed draw nothing; and the same ops drawn
// whole after partial frames that built none of their paths are still the
// whole frame (a path a partial frame didn't build was built from its
// path data as if that were vertices, the second time its ops came).
func TestClackPartialSameOps(t *testing.T) {
	const w, h = 800, 600
	win := clackWindow(t, w, h)
	defer win.Release()
	defer gpu.ClackPartialTextures(false)
	defer gpu.ClackPartialWhole(false)
	gpu.ClackPartialTextures(true)
	scene := partialScene(w, h)
	var o op.Ops
	shot := func(v partialVars, record bool) (*image.RGBA, gpu.FrameStats) {
		t.Helper()
		if record {
			o.Reset()
			scene(&o, f32.Affine2D{}, v)
		}
		if err := win.Frame(&o); err != nil {
			t.Fatal(err)
		}
		img := image.NewRGBA(image.Rectangle{Max: win.size})
		if err := win.Screenshot(img); err != nil {
			t.Fatal(err)
		}
		return img, win.ClackFrameStats()
	}
	// the whole frames, as the partial ones go below: the frame, the same
	// ops again, the emoji's next frame
	gpu.ClackPartialWhole(true)
	gpu.ClackPartialReset()
	shot(partialVars{caret: true}, true)
	ref0, _ := shot(partialVars{caret: true}, false)
	scene(&op.Ops{}, f32.Affine2D{}, partialVars{caret: true, emoji: 1})
	ref1, _ := shot(partialVars{}, false)
	shot(partialVars{}, false)
	gpu.ClackPartialWhole(false)
	gpu.ClackPartialReset()
	shot(partialVars{caret: true}, true)
	again, st := shot(partialVars{caret: true}, false)
	if !st.Partial || st.DamagePx != 0 || st.Draws != 0 {
		t.Errorf("the same ops again, nothing changed: partial %v, %d px damaged, %d draws; want a partial frame drawing nothing", st.Partial, st.DamagePx, st.Draws)
	}
	if n, m := diffCount(ref0, again); n > 0 {
		t.Errorf("the same ops again: %d px differ by up to %d levels", n, m)
	}
	// the emoji moves on, the ops stay
	scene(&op.Ops{}, f32.Affine2D{}, partialVars{caret: true, emoji: 1}) // only sets the live image's frame
	next, st := shot(partialVars{}, false)
	if !st.Partial || st.DamagePx != 22*22 {
		t.Errorf("the same ops, the emoji's next frame: partial %v, %d px damaged in %d rects; want the emoji's 484", st.Partial, st.DamagePx, st.DamageRects)
	}
	if n, m := diffCount(ref1, next); n > 0 {
		t.Errorf("the same ops, the emoji's next frame: %d px differ from the whole frame by up to %d levels:%s", n, m, worstPixels(ref1, next, next.Bounds(), 6))
	}
	// the same ops once more, drawn whole: every path built, the ones the
	// partial frames left unbuilt included
	gpu.ClackPartialWhole(true)
	again2, st := shot(partialVars{}, false)
	gpu.ClackPartialWhole(false)
	if n, m := diffCount(ref1, again2); n > 0 {
		t.Errorf("the same ops drawn whole after partial frames: %d px differ from the whole frame by up to %d levels (%d paths built):%s", n, m, st.PathsBuilt, worstPixels(ref1, again2, again2.Bounds(), 6))
	}
	t.Logf("the emoji's frame: %s", statLine(st))
	// a partial frame of new ops (the caret's blink, a layout), then quiet
	// frames: the same ops again build and tessellate nothing (the paths
	// the partial frame didn't stencil keep their path data unbuilt)
	_, st = shot(partialVars{}, true)
	if !st.Partial {
		t.Errorf("the caret hidden, new ops: drawn whole, want a partial frame")
	}
	t.Logf("a layout's partial frame: %d paths tessellated, %d built", st.PathsTessellated, st.PathsBuilt)
	for i := range 2 {
		_, st = shot(partialVars{}, false)
		if st.PathsTessellated != 0 || st.PathsBuilt != 0 {
			t.Errorf("quiet frame %d after a partial frame: %d paths tessellated, %d built; want none", i+1, st.PathsTessellated, st.PathsBuilt)
		}
	}
}

// Clack patch 46: a spinner at the start of the frame's paths, whose arcs
// change every frame (codeSpinner every 33 ms while an agent works), for
// 3,000 frames: the sticky atlas's sequence stays within a constant of the
// frame's regions, the packer's work a frame within a small factor of a
// fresh packing's, the frames partial; the last is the whole frame's
// pixels. Before, each frame left two holes the replay packed ever after.
func TestClackPartialSpinner(t *testing.T) {
	const w, h = 800, 600
	win := clackWindow(t, w, h)
	defer win.Release()
	defer gpu.ClackPartialTextures(false)
	defer gpu.ClackPartialWhole(false)
	gpu.ClackPartialTextures(true)
	gpu.ClackPartialReset()
	scene := partialScene(w, h)
	var o op.Ops
	draw := func(f int) gpu.FrameStats {
		t.Helper()
		o.Reset()
		// the spinner first: every path after it keeps its place
		c := f32.Pt(30, 330)
		for k, sweep := range []float32{math.Pi / 2, math.Pi / 3} {
			a0 := float64(f)*0.37 + float64(k)*2.1
			var p clip.Path
			p.Begin(&o)
			p.MoveTo(f32.Pt(c.X+7*float32(math.Cos(a0)), c.Y+7*float32(math.Sin(a0))))
			p.ArcTo(c, c, sweep)
			paint.FillShape(&o, color.NRGBA{R: 0x12, G: 0x64, B: 0xa3, A: 0xff}, clip.Stroke{Path: p.End(), Width: 2}.Op())
		}
		scene(&o, f32.Affine2D{}, partialVars{caret: f%30 < 15})
		if err := win.Frame(&o); err != nil {
			t.Fatal(err)
		}
		return win.ClackFrameStats()
	}
	first := draw(0)
	var maxHoles, partials int
	var maxWork int64
	var work []int64
	for f := 1; f < 3000; f++ {
		st := draw(f)
		if st.Partial {
			partials++
		}
		maxHoles, maxWork = max(maxHoles, st.StickyHoles), max(maxWork, st.PackWork)
		work = append(work, st.PackWork)
		if f%500 == 0 {
			t.Logf("frame %d: %d sticky slots (%d holes), packer work %d (the first frame's %d), partial %v, %d px damaged", f, st.StickySlots, st.StickyHoles, st.PackWork, first.PackWork, st.Partial, st.DamagePx)
		}
	}
	if maxHoles > 64 {
		t.Errorf("up to %d hole slots in the sticky sequence; want 64 at most", maxHoles)
	}
	if maxWork > 3*first.PackWork {
		t.Errorf("the packer's work reached %d a frame, %.1fx the first frame's %d; want 3x at most", maxWork, float64(maxWork)/float64(first.PackWork), first.PackWork)
	}
	if partials < 2900 {
		t.Errorf("%d of 2,999 spinner frames partial; want nearly all", partials)
	}
	// the last frame against the whole frame the same GPU draws for it
	img := image.NewRGBA(image.Rectangle{Max: win.size})
	if err := win.Screenshot(img); err != nil {
		t.Fatal(err)
	}
	gpu.ClackPartialWhole(true)
	if err := win.Frame(&o); err != nil {
		t.Fatal(err)
	}
	gpu.ClackPartialWhole(false)
	ref := image.NewRGBA(image.Rectangle{Max: win.size})
	if err := win.Screenshot(ref); err != nil {
		t.Fatal(err)
	}
	if n, m := diffCount(ref, img); n > 0 {
		t.Errorf("after 3,000 spinner frames: %d px differ from the whole frame by up to %d levels:%s", n, m, worstPixels(ref, img, img.Bounds(), 6))
	}
}

// Clack patch 46: the damage of two frames is the clips of the ops one has
// and the other doesn't (a shortest edit script), in disjoint rectangles.
func TestClackPartialDamageRects(t *testing.T) {
	// the frames are drawn by a real window; the damage is only Go
	const w, h = 400, 300
	win := clackWindow(t, w, h)
	defer win.Release()
	defer gpu.ClackPartialTextures(false)
	gpu.ClackPartialTextures(true)
	// each rectangle's colour is its own, wherever it is in the list
	rects := func(rs ...image.Rectangle) func(o *op.Ops) {
		return func(o *op.Ops) {
			paint.Fill(o, color.NRGBA{R: 0xf0, G: 0xf0, B: 0xf0, A: 0xff})
			for _, r := range rs {
				paint.FillShape(o, color.NRGBA{R: uint8(r.Min.X), G: uint8(r.Min.Y), B: 0x80, A: 0xc0}, clip.Rect(r).Op())
			}
		}
	}
	a := image.Rect(10, 10, 20, 30)
	b := image.Rect(100, 100, 130, 110)
	c := image.Rect(300, 200, 310, 250)
	a2 := image.Rect(15, 10, 25, 30)
	b2 := image.Rect(105, 100, 135, 110)
	c2 := image.Rect(300, 205, 310, 255)
	area := func(r image.Rectangle) int64 { return int64(r.Dx() * r.Dy()) }
	for _, s := range []struct {
		name       string
		from, to   func(o *op.Ops)
		want       int64 // damaged pixels
		wantRects  int
		wantWhole  bool
		wantNoDraw bool
	}{
		{"nothing changed", rects(a, b, c), rects(a, b, c), 0, 0, false, true},
		{"one op added", rects(a, c), rects(a, b, c), area(b), 1, false, false},
		{"one op dropped", rects(a, b, c), rects(a, c), area(b), 1, false, false},
		{"one op moved", rects(a, b, c), rects(a, b2, c), area(b.Union(b2)), 1, false, false},
		// two disjoint ops in the other order: one is dropped and added
		// again, the other kept
		{"two ops drawn the other way round", rects(a, b, c), rects(b, a, c), -1, 1, false, false},
		{"two ops far apart moved", rects(a, b, c), rects(a2, b, c2), area(a.Union(a2)) + area(c.Union(c2)), 2, false, false},
		{"most of the window", rects(a), rects(a, image.Rect(0, 0, 380, 290)), 0, 0, true, false},
	} {
		var o op.Ops
		s.from(&o)
		if err := win.Frame(&o); err != nil {
			t.Fatal(err)
		}
		o.Reset()
		s.to(&o)
		if err := win.Frame(&o); err != nil {
			t.Fatal(err)
		}
		st := win.ClackFrameStats()
		switch {
		case s.wantWhole:
			if st.Partial {
				t.Errorf("%s: a partial frame (%d px damaged), want it whole", s.name, st.DamagePx)
			}
		case !st.Partial:
			t.Errorf("%s: drawn whole, want a partial frame", s.name)
		case s.want < 0 && st.DamagePx != area(a) && st.DamagePx != area(b):
			t.Errorf("%s: %d px damaged in %d rects, want one of the two ops' (%d or %d)", s.name, st.DamagePx, st.DamageRects, area(a), area(b))
		case s.want >= 0 && (st.DamagePx != s.want || st.DamageRects != s.wantRects):
			t.Errorf("%s: %d px damaged in %d rects, want %d in %d", s.name, st.DamagePx, st.DamageRects, s.want, s.wantRects)
		case s.wantNoDraw && st.Draws != 0:
			t.Errorf("%s: %d draws, want none", s.name, st.Draws)
		}
	}
}

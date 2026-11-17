// SPDX-License-Identifier: Unlicense OR MIT

package headless

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand"
	"testing"

	"gioui.org/f32"
	"gioui.org/gpu"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// Clack patch 45: a partial frame (drawn again only where it differs from
// the last, scissored) is the whole frame's pixels, bit for bit, on every
// backend (ci's linux-gio, macos and windows-applog jobs;
// CLACK_PIXEL_TESTS).

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
		// the animated emoji
		est := op.Offset(image.Pt(panel.Min.X+300, panel.Min.Y+40)).Push(o)
		paint.NewLiveImageOp(live).Add(o)
		paint.PaintOp{}.Add(o)
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
// another order, a path whose atlas place moves every later one, a new
// clear colour, a transform), three ways: whole, partial into the texture
// target, and partial through the frame texture and its copy into the
// target (a window's way). Every frame must have the whole frame's pixels
// exactly; the frames a small change made must be partial, with a small
// damage, and cost a fraction of the whole frame.
func TestClackPartialFrames(t *testing.T) {
	const w, h = 800, 600
	win := clackWindow(t, w, h)
	defer win.Release()
	defer gpu.ClackPartialFrames(true)
	defer gpu.ClackPartialTextures(false)
	defer gpu.ClackFrameTexture(false)
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
		// every path after it lands elsewhere in the atlas: damage
		// everywhere they're drawn, maybe the whole frame
		{"a path drops out", id, partialVars{spinner: 3, emoji: 2, moved: 3, swap: true, drop: true}, 0, -1, false},
		{"the first path grows", id, partialVars{grow: 9}, 0, -1, false},
		{"a new clear colour", id, partialVars{clearWhite: true}, 2, -1, false},
		{"the caret, on that clear colour", id, partialVars{clearWhite: true, caret: true}, 1, 64, true},
		{"scaled", scaled, partialVars{}, 2, -1, false},
		{"scaled, the caret", scaled, partialVars{caret: true}, 0, -1, false},
		{"scaled, the emoji", scaled, partialVars{caret: true, emoji: 1}, 0, -1, false},
		{"at a fraction of a pixel", frac, partialVars{}, 2, -1, false},
		{"at a fraction of a pixel, the caret", frac, partialVars{caret: true}, 0, -1, false},
		{"back at an integer offset", id, partialVars{}, 2, -1, false},
		{"the caret again", id, partialVars{caret: true}, 1, 64, true},
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

	gpu.ClackPartialFrames(false)
	whole, ws := partialRun(t, win, scene, steps)
	for i, st := range ws {
		if st.Partial {
			t.Fatalf("%s: a partial frame with partial frames off", steps[i].name)
		}
	}
	gpu.ClackPartialFrames(true)
	for _, mode := range []struct {
		name                 string
		textures, frameTexes bool
	}{
		{"partial frames into the target", true, false},
		{"partial frames through the frame texture", false, true},
	} {
		gpu.ClackPartialTextures(mode.textures)
		gpu.ClackFrameTexture(mode.frameTexes)
		imgs, sts := partialRun(t, win, scene, steps)
		partials := 0
		for i, img := range imgs {
			s, st := steps[i], sts[i]
			if n, m := diffCount(whole[i], img); n > 0 {
				t.Errorf("%s, %s: %d px differ from the whole frame by up to %d levels (partial %v, %d rects, %d px damaged):%s",
					mode.name, s.name, n, m, st.Partial, st.DamageRects, st.DamagePx, worstPixels(whole[i], img, img.Bounds(), 6))
			}
			if st.Partial {
				partials++
			}
			switch {
			case s.want == 1 && !st.Partial:
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
	}
	t.Logf("whole, the caret's frame: %s", statLine(ws[1]))
}

// Clack patch 45: a frame drawn again from the same ops (the client's
// quiet frames: an animation is all that changed) is a partial frame
// whose damage is the live image's, and is the whole frame's pixels; the
// same ops with nothing changed draw nothing.
func TestClackPartialSameOps(t *testing.T) {
	const w, h = 800, 600
	win := clackWindow(t, w, h)
	defer win.Release()
	defer gpu.ClackPartialTextures(false)
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
	// the whole frames: the emoji at frames 0 and 1
	ref0, _ := shot(partialVars{caret: true}, true)
	ref1, _ := shot(partialVars{caret: true, emoji: 1}, true)
	gpu.ClackPartialTextures(true)
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
	t.Logf("the emoji's frame: %s", statLine(st))
}

// Clack patch 45: the damage of two frames is the clips of the ops one has
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

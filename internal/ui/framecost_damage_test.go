package ui

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"os"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"gioui.org/gpu"
	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache/webptest"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// TestFrameCostDamage is what a small change costs the GPU, on each OS's
// backend (flake-check's gpu-cost job: llvmpipe, Metal, WARP;
// CLACK_GPU_COST): Clack's whole window over internal/fakeslack (synthetic
// data), the composer focused, and the frames a window draws for
//   - the caret's blink (every widget.CaretBlinkInterval: 500 ms, the OS's
//     on Windows),
//   - the typing indicator appearing under the composer (at each
//     user_typing event, every few seconds while someone types),
//   - a spinner's turn (a working agent's in the sidebar asks every 33 ms,
//     the LoadingButton's every 16 ms),
//   - an animated custom emoji's next frame (:explodes:, 13 frames of 30 to
//     70 ms; a quiet frame, the same ops drawn again),
//
// each drawn whole (as Gio drew every frame), partial into the target and
// partial through the frame texture and its copy (a window's way, Gio
// patch 46). It logs each one's draws, API calls, pixels and time from
// submit to the end of its rasterization, and requires the partial frames'
// pixels to be the whole frame's exactly (but the emoji's, which plays on
// the wall clock) and their GPU work to be a small part of it.
func TestFrameCostDamage(t *testing.T) {
	if os.Getenv("CLACK_GPU_COST") == "" {
		t.Skip("CLACK_GPU_COST not set (flake-check's gpu-cost job runs this)")
	}
	defer damageModes(false, false)
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	name, u := fs.FixtureEmoji()
	if name == "" || !strings.HasPrefix(u, fs.URL+"img/") {
		t.Fatalf("the fake's custom emoji %q is at %q", name, u)
	}
	path := strings.TrimPrefix(u, fs.URL+"img/")
	data := webptest.Explodes()
	fs.SetImages(func(p string) ([]byte, string, bool) {
		if p == path {
			return data, "image/png", true // a WebP behind a .png URL, as Slack serves :explodes:
		}
		return nil, "", false
	})
	users := fs.UserIDs()
	ch := fs.ChannelIDs()[1]
	for _, c := range []struct {
		name  string
		scale float32
		dp    image.Point
		theme string
	}{
		{"1440 1x", 1, image.Pt(1440, 900), ""},
		{"karim 1x", 1, image.Pt(1918, 1080), costKarim},
		{"1280 2x", 2, image.Pt(1280, 800), ""},
	} {
		if c.theme != "" {
			fs.SetPrefs(map[string]any{"ia_theme": c.theme})
		}
		e := startEnv(t, fs, t.TempDir())
		e.scale = c.scale
		e.size = image.Pt(int(float32(c.dp.X)*c.scale), int(float32(c.dp.Y)*c.scale))
		onAnimRepaint(e, func() {})
		e.waitReady()
		e.openChannel(ch)
		e.settle(2 * time.Second)
		e.focusComposer(ch, "")
		e.settle(1500 * time.Millisecond)
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless GPU: %v", err)
		}
		e.gpu = w
		// no animated image plays until the emoji's part: the scenes'
		// frames are drawn several times from the same ops, and a live
		// image moving on between them (a GIF that finished loading on a
		// slow runner) changes pixels no op did (Slack's "Allow
		// animations" off: stills). After boot, whose prefs set it. And
		// the live images held on the frame they show (the inspect
		// freeze's hold): a slow WARP runner had :explodes: in view move
		// on between draws of the same ops anyway (flake-check
		// 44434705984, 2026-11-19: 132 px by 80 levels in its colours).
		e.a.images.SetAnimate(false)
		e.a.images.HoldLive(true)
		e.costSettle(t, c.name)
		e.gpu = nil // the frames below draw only what's measured
		var a, b op.Ops
		now := time.Now()
		blink := widget.CaretBlinkInterval()
		if blink <= 0 {
			t.Fatalf("%s: the caret doesn't blink (%v)", c.name, blink)
		}
		// the caret: shown in one of two layouts a blink apart
		caret := func() {
			e.layInto(&a, now, nil)
			e.layInto(&b, now.Add(blink), nil)
		}
		caret()
		damageScene(t, w, c.name+", the caret's blink", fmt.Sprintf("every %v", blink), &a, &b, caret, func(o *op.Ops, shift int) {
			e.layProbed(o, now.Add(blink), nil, shift)
		}, true, damageLimits{rects: 1, frac: 0.002})
		// a spinner's turn over the sidebar, 33 ms apart
		sp := func(at time.Time) func(gtx layout.Context) {
			return func(gtx layout.Context) {
				gtx.Now = at
				defer op.Offset(image.Pt(int(24*e.pxPerDp()), int(320*e.pxPerDp()))).Push(gtx.Ops).Pop()
				codeSpinner(gtx, int(16*e.pxPerDp()), color.NRGBA{R: 0xd1, G: 0xd2, B: 0xd3, A: 0xff})
			}
		}
		spin := func() {
			e.layInto(&a, now, sp(now))
			e.layInto(&b, now, sp(now.Add(33*time.Millisecond)))
		}
		spin()
		damageScene(t, w, c.name+", a spinner's turn", "every 33 ms (an agent's; the LoadingButton's every 16 ms)", &a, &b, spin, func(o *op.Ops, shift int) {
			e.layProbed(o, now, sp(now.Add(33*time.Millisecond)), shift)
		}, true, damageLimits{frac: 0.01})
		// the typing indicator appears
		e.layInto(&a, now, nil)
		raw, _ := json.Marshal(map[string]any{"type": "user_typing", "channel": ch, "user": users[1]})
		e.a.data.core.Inject(slack.Event{Type: "user_typing", Channel: json.RawMessage(`"` + ch + `"`), Raw: raw})
		comp, ok := e.a.convoFor(ch, "").comp.(interface{ TypingLine() string })
		if !ok {
			t.Fatalf("%s: the composer is a %T", c.name, e.a.convoFor(ch, "").comp)
		}
		dl := time.Now().Add(10 * time.Second)
		for comp.TypingLine() == "" {
			if time.Now().After(dl) {
				t.Fatalf("%s: no typing line", c.name)
			}
			e.idleFrame(time.Now())
			time.Sleep(5 * time.Millisecond)
		}
		e.layInto(&b, now, nil)
		damageScene(t, w, c.name+", the typing indicator ("+comp.TypingLine()+")", "at each user_typing event", &a, &b, nil, func(o *op.Ops, shift int) {
			e.layProbed(o, now, nil, shift)
		}, true, damageLimits{frac: 0.05})
		// the animated emoji: a message with it, at the bottom
		e.a.images.HoldLive(false)
		e.a.images.SetAnimate(true)
		self := e.a.Core().Self().ID
		ts, ev := fs.PostQuiet(ch, self, "boom :"+name+": boom")
		evb, _ := json.Marshal(ev)
		e.a.data.core.Inject(slack.Event{Type: "message", Channel: json.RawMessage(`"` + ch + `"`), Raw: evb})
		e.until("the emoji's message", 20*time.Second, func() bool {
			if tl := e.a.data.core.S.Timeline(ch); tl != nil {
				for _, m := range tl.Msgs {
					if m.TS == ts {
						return true
					}
				}
			}
			return false
		})
		for i := 0; i < 40; i++ {
			e.scroll(e.listPoint(), 4000)
			e.frame()
		}
		e.until("the emoji playing", 20*time.Second, func() bool { return e.a.images.Stats().Players > 0 })
		e.settle(time.Second)
		e.layInto(&a, time.Now(), nil)
		damageQuiet(t, w, c.name+", the emoji's next frame (a quiet frame)", "every 30 to 70 ms while it plays", &a, e.a.images.Stats)
		w.Release()
		e.shutdown()
	}
}

// damageLimits are what a partial frame may cost against the whole one:
// rects the damage rectangles (0: any), frac the window's pixels it may
// damage.
type damageLimits struct {
	rects int
	frac  float64
}

// layInto lays the window out at t into o, as App.Run's frames do, with
// extra drawn over it.
func (e *perfEnv) layInto(o *op.Ops, t time.Time, extra func(gtx layout.Context)) {
	e.layProbed(o, t, extra, 0)
}

// layProbed is layInto with one more transparent path (costShift's: drawn
// first for 1-3, right after the workspace's shadow for 4-6): every later
// path lands elsewhere in Gio's atlas and no pixel changes but by the
// placement rounding.
func (e *perfEnv) layProbed(o *op.Ops, t time.Time, extra func(gtx layout.Context), shift int) {
	if shift > 3 {
		theme.SetFrameCostProbe(costShift(shift, e.size))
		defer theme.SetFrameCostProbe(image.Rectangle{})
	}
	e.a.drain()
	e.now = t
	o.Reset()
	sc := e.scale
	if sc == 0 {
		sc = 1
	}
	gtx := layout.Context{Ops: o, Now: t, Metric: unit.Metric{PxPerDp: sc, PxPerSp: sc},
		Constraints: layout.Exact(e.size), Source: e.r.Source()}
	if d := costShift(shift, e.size); !d.Empty() && shift <= 3 {
		paint.FillShape(gtx.Ops, color.NRGBA{}, clip.Ellipse(d).Op(gtx.Ops))
	}
	e.a.frame(gtx)
	if extra != nil {
		extra(gtx)
	}
	e.r.Frame(o)
}

func (e *perfEnv) pxPerDp() float32 {
	if e.scale == 0 {
		return 1
	}
	return e.scale
}

// damageDraw draws o and waits for its rasterization; it returns the
// frame's stats and the milliseconds that took.
func damageDraw(t *testing.T, w *headless.Window, o *op.Ops) (gpu.FrameStats, float64) {
	t.Helper()
	t0 := time.Now()
	if err := w.Frame(o); err != nil {
		t.Fatal(err)
	}
	if err := w.ClackSync(); err != nil {
		t.Fatal(err)
	}
	return w.ClackFrameStats(), float64(time.Since(t0).Microseconds()) / 1000
}

func damageShot(t *testing.T, w *headless.Window) *image.RGBA {
	t.Helper()
	img := image.NewRGBA(image.Rectangle{Max: w.Size()})
	if err := w.Screenshot(img); err != nil {
		t.Fatal(err)
	}
	return img
}

// damageMeasure draws a, then b, five times, in the mode set, and returns
// the last b's stats, the median time of the b frames and the pixels.
func damageMeasure(t *testing.T, w *headless.Window, a, b *op.Ops) (gpu.FrameStats, float64, *image.RGBA) {
	t.Helper()
	var st gpu.FrameStats
	var ms []float64
	damageDraw(t, w, a)
	for range 5 {
		damageDraw(t, w, a)
		var m float64
		st, m = damageDraw(t, w, b)
		ms = append(ms, m)
	}
	sort.Float64s(ms)
	return st, ms[len(ms)/2], damageShot(t, w)
}

// damageScene measures the frame b after a: whole, then partial into the
// window, then partial through the frame texture. Partial frames must have
// the whole frame's pixels (the whole frame the same GPU draws then) and,
// when cheap, cost a small part of a whole frame; the quiet frames after
// one (b's ops again) must tessellate nothing. The whole frame the sticky
// atlas places give must be within the placement floor of Gio's own (b
// packed afresh: the same frame with a transparent path first, or after
// the workspace's shadow, layB's shift). relay lays a and b out again:
// when something else changed between them (a load landing on a slow
// runner), the damage is more than the change's, and the scene is laid
// out again, up to three times.
func damageScene(t *testing.T, w *headless.Window, name, rate string, a, b *op.Ops, relay func(), layB func(o *op.Ops, shift int), cheap bool, lim damageLimits) {
	t.Helper()
	vp0 := int64(w.Size().X * w.Size().Y)
	for try := 0; relay != nil && try < 3; try++ {
		damageModes(true, false)
		damageDraw(t, w, a)
		st, _ := damageDraw(t, w, b)
		if _, _, px := damageOf(st); float64(px) <= lim.frac*float64(vp0) {
			break
		}
		t.Logf("%s: %s between the two layouts; laid out again", name, damagePart(st))
		relay()
	}
	damageModes(false, false)
	ws, wms, plain := damageMeasure(t, w, a, b)
	t.Logf("%s (%s), whole: %s; %.1f ms a frame", name, rate, costLine(ws), wms)
	vp := int64(plain.Bounds().Dx() * plain.Bounds().Dy())
	// Gio's own frame moved in the atlas: the placement floor, as
	// TestFrameCostWindow takes it (a tiny path, a third of the window
	// and nearly all of it, first and after the workspace's shadow)
	floorN, floorM := 0, 0
	for shift := 1; shift <= 6; shift++ {
		var bp op.Ops
		layB(&bp, shift)
		damageDraw(t, w, a)
		damageDraw(t, w, &bp)
		n, m := costCount(plain, damageShot(t, w))
		floorN, floorM = max(floorN, n), max(floorM, m)
	}
	limM, limN := max(6, floorM+2), 2*floorN+1024
	for _, m := range []struct {
		name              string
		partial, frameTex bool
	}{
		{"partial into the window", true, false},
		{"partial through the frame texture", false, true},
	} {
		damageModes(m.partial, m.frameTex)
		ps, pms, img := damageMeasure(t, w, a, b)
		// the quiet frames after it: b's ops again, while only an
		// animation elsewhere moves on (a partial frame uploads only the
		// paths its damage stencils; the rest keep their path data)
		var qs gpu.FrameStats
		var qms []float64
		for i := range 5 {
			s, ms := damageDraw(t, w, b)
			qs, qms = s, append(qms, ms)
			// a path is tessellated only to be uploaded, in the frame's
			// damage; a frame without damage builds nothing
			_, _, px := damageOf(s)
			if n := damageTessellated(s); n > s.PathsBuilt || px == 0 && (n != 0 || s.PathsBuilt != 0) {
				t.Errorf("%s, %s: quiet frame %d after a partial frame (%s) tessellated %d paths and built %d; want only paths it uploads, none without damage",
					name, m.name, i+1, damagePart(s), n, s.PathsBuilt)
			}
		}
		sort.Float64s(qms)
		t.Logf("%s, %s, quiet frames after it: %s, %d paths tessellated, %s; %.1f ms a frame (the partial frame %d paths tessellated)",
			name, m.name, damagePart(qs), damageTessellated(qs), costLine(qs), qms[len(qms)/2], damageTessellated(ps))
		// the whole frame the same GPU draws for b now
		damageWhole(true)
		damageDraw(t, w, b)
		whole := damageShot(t, w)
		damageWhole(false)
		damageModes(false, false)
		if n, mx := costCount(plain, whole); mx > limM || n > limN {
			t.Errorf("%s, %s: the whole frame with sticky atlas places differs from Gio's own in %d px by up to %d levels; a path moved in Gio's atlas moves %d px by up to %d (want %d levels and %d px at most): %s",
				name, m.name, n, mx, floorN, floorM, limM, limN, costDiff(plain, whole))
		} else {
			t.Logf("%s, %s: the sticky whole frame against Gio's own: %d px by up to %d levels (the floor %d px by up to %d)", name, m.name, n, mx, floorN, floorM)
		}
		dmg := damagePart(ps)
		t.Logf("%s, %s: %s, %s; %.1f ms a frame (%.0f%% of the whole frame's)", name, m.name, dmg, costLine(ps), pms, 100*pms/max(wms, 0.001))
		if n, mx := costCount(whole, img); n > 0 {
			t.Errorf("%s, %s: %d px differ from the whole frame, by up to %d levels: %s; %s; Gio's whole frame against the partial %s, against the whole %s",
				name, m.name, n, mx, costDiff(whole, img), damageWhere(whole, img, plain), costDiff(plain, img), costDiff(plain, whole))
			if dir := os.Getenv("CLACK_GPU_COST_SHOTS"); dir != "" {
				costCrops(t, dir, strings.NewReplacer(" ", "-", ",", "", "'", "", "(", "", ")", "").Replace(name+"-"+m.name), whole, img)
			}
		}
		if !cheap {
			continue
		}
		damageCheck(t, name+", "+m.name, ws, ps, vp, lim)
		// the software rasterizers' time is the pixels' (ci's llvmpipe and WARP)
		if runtime.GOOS != "darwin" && pms > wms/2 {
			t.Errorf("%s, %s: %.1f ms a frame, the whole frame %.1f ms: want half at most", name, m.name, pms, wms)
		}
	}
}

// damageQuiet measures the frame a quiet frame draws: a's ops again, an
// animation on screen moved on (stats: the image cache's, to wait for its
// next frame). The pixels aren't compared: the animation plays on the
// wall clock (gpu/headless TestClackPartialSameOps compares them).
func damageQuiet(t *testing.T, w *headless.Window, name, rate string, a *op.Ops, adv func() imgStats) {
	t.Helper()
	// the frames where the animation moved on: it moves on when the GPU
	// asks for its frame, once that one is decoded
	run := func() (gpu.FrameStats, float64) {
		var st gpu.FrameStats
		var ms []float64
		damageDraw(t, w, a)
		for try := 0; len(ms) < 5; try++ {
			if try == 60 {
				t.Fatalf("%s: the animation moved on in %d of %d frames", name, len(ms), try)
			}
			time.Sleep(80 * time.Millisecond) // past its longest frame
			n0 := adv().Advances
			s, m := damageDraw(t, w, a)
			if adv().Advances == n0 {
				continue
			}
			st = s
			ms = append(ms, m)
		}
		sort.Float64s(ms)
		return st, ms[len(ms)/2]
	}
	damageModes(false, false)
	ws, wms := run()
	t.Logf("%s (%s), whole: %s; %.1f ms a frame", name, rate, costLine(ws), wms)
	vp := int64(w.Size().X * w.Size().Y)
	for _, m := range []struct {
		name              string
		partial, frameTex bool
	}{
		{"partial into the window", true, false},
		{"partial through the frame texture", false, true},
	} {
		damageModes(m.partial, m.frameTex)
		ps, pms := run()
		damageModes(false, false)
		t.Logf("%s, %s: %s, %s; %.1f ms a frame (%.0f%% of the whole frame's)", name, m.name, damagePart(ps), costLine(ps), pms, 100*pms/max(wms, 0.001))
		damageCheck(t, name+", "+m.name, ws, ps, vp, damageLimits{frac: 0.01})
	}
}

// damageCheck: a partial frame, its damage within lim, and a small part
// of the whole frame's draws, API calls and pixels.
func damageCheck(t *testing.T, name string, ws, ps gpu.FrameStats, vp int64, lim damageLimits) {
	t.Helper()
	part, rects, px := damageOf(ps)
	if !part {
		t.Errorf("%s: drawn whole, want a partial frame", name)
		return
	}
	if lim.rects > 0 && rects > lim.rects {
		t.Errorf("%s: damage in %d rects, want %d at most", name, rects, lim.rects)
	}
	if f := float64(px) / float64(vp); f > lim.frac {
		t.Errorf("%s: %d px damaged, %.2f%% of the window, want %.2f%% at most", name, px, 100*f, 100*lim.frac)
	}
	if ps.Draws*10 > ws.Draws {
		t.Errorf("%s: %d draws, the whole frame %d: want a tenth at most", name, ps.Draws, ws.Draws)
	}
	if ps.APICalls >= 0 && ws.APICalls > 0 && ps.APICalls*5 > ws.APICalls {
		t.Errorf("%s: %d API calls, the whole frame %d: want a fifth at most", name, ps.APICalls, ws.APICalls)
	}
	drawn := func(s gpu.FrameStats) int64 { return s.CoverPx + s.BlitPx + s.IntersectPx + max(s.StencilPx, 0) }
	if w := drawn(ws); w > 0 && drawn(ps)*10 > w {
		t.Errorf("%s: %d px stenciled, covered, blitted and intersected, the whole frame %d: want a tenth at most", name, drawn(ps), w)
	}
}

// damageWhere says where a and b differ: the box around it and a few of
// its pixels in a, b and c.
func damageWhere(a, b, c *image.RGBA) string {
	var box image.Rectangle
	var samples []string
	r := a.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i := a.PixOffset(x, y)
			same := true
			for k := 0; k < 4; k++ {
				same = same && a.Pix[i+k] == b.Pix[i+k]
			}
			if same {
				continue
			}
			p := image.Rectangle{Min: image.Pt(x, y), Max: image.Pt(x+1, y+1)}
			if box.Empty() {
				box = p
			} else {
				box = box.Union(p)
			}
			if len(samples) < 4 && (x+y)%97 == 0 {
				samples = append(samples, fmt.Sprintf("%v: %v vs %v (Gio's %v)", image.Pt(x, y), a.RGBAAt(x, y), b.RGBAAt(x, y), c.RGBAAt(x, y)))
			}
		}
	}
	return fmt.Sprintf("in %v: %s", box, strings.Join(samples, "; "))
}

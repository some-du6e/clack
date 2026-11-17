package ui

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"gioui.org/gpu"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// TestFrameCostWindow draws Clack's whole window over internal/fakeslack
// (synthetic data; nothing reaches Slack) the old way and the new, and
// says what one frame costs each: the workspace shadow's rings in one
// piece or in bands, the panel's fill in one piece or around its
// interior, rounded rects and circles recorded where they're drawn or
// cached at the origin (theme.SetLegacyFrameCost), and Gio keeping every
// clip path on every op over one stencil region or not
// (gpu.ClackLegacyClips, Gio patches 38 and 39). The same frame (state,
// time) is laid out each way and drawn, light and dark, at 1x and 2x, a
// channel and a channel with its thread open, and in Karim's window
// (costKarim). It logs the draw calls, the graphics API calls (and those
// the backend's state cache left out, Gio patch 37), the paths built,
// the stencil, cover and cleared pixels, the frame's time to the end of
// its rasterization, and the pixels that differ. CLACK_GPU_COST=1
// (flake-check's gpu-cost job, on each OS's GPU backend).
func TestFrameCostWindow(t *testing.T) {
	if os.Getenv("CLACK_GPU_COST") == "" {
		t.Skip("CLACK_GPU_COST not set (flake-check's gpu-cost job runs this)")
	}
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	users := fs.UserIDs()
	ch := fs.ChannelIDs()[1]
	root, _ := fs.PostQuiet(ch, users[1], "a thread for the frame-cost window")
	for i := 0; i < 6; i++ {
		fs.PostReplyQuiet(ch, root, users[1+i%3], fmt.Sprintf("reply %d, long enough to wrap the thread pane's line at least once or twice", i))
	}
	for _, c := range []struct {
		name  string
		scale float32
		dp    image.Point
		modes []bool // dark
		theme string // the account's ia_theme ("": fakeslack's)
	}{
		{"1x", 1, image.Pt(1280, 800), []bool{false, true}, ""},
		{"2x", 2, image.Pt(1280, 800), []bool{false, true}, ""},
		{"karim 1x", 1, image.Pt(1918, 1080), []bool{false}, costKarim},
	} {
		if c.theme != "" {
			fs.SetPrefs(map[string]any{"ia_theme": c.theme})
		}
		e := startEnv(t, fs, t.TempDir())
		e.scale = c.scale
		e.size = image.Pt(int(float32(c.dp.X)*c.scale), int(float32(c.dp.Y)*c.scale))
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless GPU: %v", err)
		}
		e.gpu = w
		e.waitReady()
		e.openChannel(ch)
		e.settle(2 * time.Second)
		for _, dark := range c.modes {
			e.a.setDark(dark)
			e.settle(2 * time.Second)
			for _, thread := range []bool{false, true} {
				if thread {
					e.a.OpenThread(ch, root)
					e.until("the thread pane", 20*time.Second, func() bool { return e.a.secondary.kind == secThread })
					e.settle(1500 * time.Millisecond)
				}
				name := fmt.Sprintf("%s %s%s", c.name, map[bool]string{false: "light", true: "dark"}[dark], map[bool]string{false: "", true: " thread"}[thread])
				if c.theme != "" && !e.a.acct.t.UseCustomHex {
					t.Fatalf("%s: the account theme %s didn't apply", name, c.theme)
				}
				e.costSettle(t, name)
				now := time.Now()
				old, so, to := e.costFrame(t, true, 0, now, true)
				cur, sn, tn := e.costFrame(t, false, 0, now, true)
				t.Logf("%s, old: %s, %.1f ms a frame", name, costLine(so), to)
				t.Logf("%s, new: %s, %.1f ms a frame", name, costLine(sn), tn)
				// the noise floor: the old way with every path moved in
				// the atlas, by three paths drawn first
				var worst *image.RGBA
				var floorN, floorMax int
				for sh := 1; sh <= 3; sh++ {
					moved, _, _ := e.costFrame(t, true, sh, now, false)
					n, m := costCount(old, moved)
					t.Logf("%s, the old way after a transparent %v path first: %s", name, costShift(sh, e.size).Size(), costDiff(old, moved))
					if n > floorN {
						floorN, worst = n, moved
					}
					floorMax = max(floorMax, m)
				}
				moved, _, _ := e.costFrame(t, false, 3, now, false)
				t.Logf("%s, the new way after a transparent %v path first: %s", name, costShift(3, e.size).Size(), costDiff(cur, moved))
				n, m := costCount(old, cur)
				t.Logf("%s, new against old: %s; the old way's own floor: up to %d px, max %d", name, costDiff(old, cur), floorN, floorMax)
				if n > 0 && m > floorMax {
					t.Logf("%s: new against old differs by %d levels, more than the %d the old way's floor shows", name, m, floorMax)
				}
				if dir := os.Getenv("CLACK_GPU_COST_SHOTS"); dir != "" {
					costCrops(t, dir, strings.ReplaceAll(name, " ", "-"), old, cur)
					if worst != nil {
						costCrops(t, dir, strings.ReplaceAll(name, " ", "-")+"-floor", old, worst)
					}
				}
				if thread {
					e.a.CloseSecondary()
					e.settle(500 * time.Millisecond)
				}
			}
		}
		e.gpu = nil
		w.Release()
		e.shutdown()
	}
}

// costKarim approximates Karim's account theme as his inspect report of
// 2026-11-17 has it (Clack α.102 on windows/amd64: light mode, a custom
// theme #FED4BE, a 1918×1080 dp window at 1x): the custom colour as the
// primary, Slack's defaults for the rest. Light mode is the case with the
// workspace shadow's 14 rings.
const costKarim = `{"primary":{"palette":"aubergine","custom":"#FED4BE"},"highlight1":{"palette":"aubergine"},"highlight2":{"palette":"jade"},"important":{"palette":"aubergine"},"brightness":6,"sidebarInverted":false,"useCustomHex":true}`

// costFrame lays the window out at now the old way (legacy) or the new,
// twice, each into fresh ops as every frame is, after one more
// transparent path drawn first if shift > 0 (costShift: every later path
// lands elsewhere in the atlas and nothing else changes). It draws the
// first layout twice and then the second, and returns that frame's pixels
// and stats: its paths built are the ones the frame before didn't leave
// in Gio's path cache. If timed, it also returns the median time of 5
// more frames from layout to the end of their rasterization (else 0).
func (e *perfEnv) costFrame(t *testing.T, legacy bool, shift int, now time.Time, timed bool) (*image.RGBA, gpu.FrameStats, float64) {
	t.Helper()
	theme.SetLegacyFrameCost(legacy)
	gpu.ClackLegacyClips(legacy)
	defer theme.SetLegacyFrameCost(false)
	defer gpu.ClackLegacyClips(false)
	w := e.gpu
	lay := func() {
		e.now = now
		gtx := e.context()
		if d := costShift(shift, e.size); !d.Empty() {
			paint.FillShape(gtx.Ops, color.NRGBA{}, clip.Ellipse(d).Op(gtx.Ops))
		}
		e.a.frame(gtx)
		e.r.Frame(&e.ops)
	}
	draw := func() {
		if err := w.Frame(&e.ops); err != nil {
			t.Fatal(err)
		}
	}
	// the stencil fragment counts keep every path's quads; turning them
	// on empties the path cache, which the first layout's draws fill
	w.ClackStatsDetail(true)
	lay()
	draw()
	draw()
	lay()
	draw()
	st := w.ClackFrameStats()
	img := image.NewRGBA(image.Rectangle{Max: e.size})
	if err := w.Screenshot(img); err != nil {
		t.Fatal(err)
	}
	w.ClackStatsDetail(false)
	if !timed {
		return img, st, 0
	}
	lay()
	draw()
	var ms []float64
	for range 5 {
		t0 := time.Now()
		lay()
		draw()
		if err := w.ClackSync(); err != nil {
			t.Fatal(err)
		}
		ms = append(ms, float64(time.Since(t0).Microseconds())/1000)
	}
	sort.Float64s(ms)
	return img, st, ms[len(ms)/2]
}

// costShift is the transparent path costFrame draws first: none, a tiny
// one, a third of the window and nearly all of it, all of odd sizes, so
// the paths after it move by odd amounts too.
func costShift(shift int, size image.Point) image.Rectangle {
	switch shift {
	case 1:
		return image.Rect(1, 1, 8, 6)
	case 2:
		return image.Rect(1, 1, size.X/3|1, size.Y/3|1)
	case 3:
		return image.Rect(0, 0, size.X-1, size.Y-1)
	}
	return image.Rectangle{}
}

// costSettle waits until the window stops changing: the same frame (laid
// out at the same time) a second of running apart draws the same pixels
// (images, replies and members load in the background).
func (e *perfEnv) costSettle(t *testing.T, name string) {
	t.Helper()
	for try := 0; try < 6; try++ {
		at := time.Now()
		a, _, _ := e.costFrame(t, false, 0, at, false)
		e.settle(time.Second)
		b, _, _ := e.costFrame(t, false, 0, at, false)
		if string(a.Pix) == string(b.Pix) {
			return
		}
	}
	t.Logf("%s: the window still changes between frames, so its comparisons include that", name)
}

func costLine(s gpu.FrameStats) string {
	return fmt.Sprintf("%d draws, %d passes, %d API calls (%d more without the state cache; %d crossings into C or the system), %d paths built (%.0f KB), %d blits, %d covers, %d intersections, %d paths stenciled; stencil %.2f Mpx, cover %.2f, blit %.2f, intersect %.2f, cleared %.2f, atlas %.2f",
		s.Draws, s.Passes, s.APICalls, s.Skipped, s.Crossings, s.PathsBuilt, float64(s.PathBytes)/1024, s.Blits, s.Covers, s.Intersections, s.StencilPaths,
		float64(s.StencilPx)/1e6, float64(s.CoverPx)/1e6, float64(s.BlitPx)/1e6, float64(s.IntersectPx)/1e6, float64(s.ClearPx)/1e6, float64(s.AtlasPx)/1e6)
}

// costCount is how many pixels of a and b differ, and by how many
// levels at most.
func costCount(a, b *image.RGBA) (n, maxd int) {
	for i := 0; i+3 < len(a.Pix); i += 4 {
		d := 0
		for k := 0; k < 4; k++ {
			v := int(a.Pix[i+k]) - int(b.Pix[i+k])
			d = max(d, v, -v)
		}
		if d > 0 {
			n++
			maxd = max(maxd, d)
		}
	}
	return n, maxd
}

func costDiff(a, b *image.RGBA) string {
	n, maxd := 0, 0
	var by [4]int
	r := a.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i := a.PixOffset(x, y)
			d := 0
			for k := 0; k < 4; k++ {
				v := int(a.Pix[i+k]) - int(b.Pix[i+k])
				d = max(d, v, -v)
			}
			if d > 0 {
				n++
				maxd = max(maxd, d)
				by[min(d, 3)]++
			}
		}
	}
	if n == 0 {
		return fmt.Sprintf("identical (%d px)", r.Dx()*r.Dy())
	}
	return fmt.Sprintf("%d of %d px differ, max %d (by 1: %d, by 2: %d, by 3 or more: %d)", n, r.Dx()*r.Dy(), maxd, by[1], by[2], by[3])
}

// costCrops writes the places where a and b differ most: for each of up
// to four, 40×40 px of a, of b and of their difference (×10), side by
// side and 6× larger, to dir/<name>-<n>.png, and logs where they are.
func costCrops(t *testing.T, dir, name string, a, b *image.RGBA) {
	type spot struct {
		p image.Point
		d int
	}
	var spots []spot
	r := a.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i := a.PixOffset(x, y)
			d := 0
			for k := 0; k < 3; k++ {
				v := int(a.Pix[i+k]) - int(b.Pix[i+k])
				d = max(d, v, -v)
			}
			if d < 2 {
				continue
			}
			near := false
			for j := range spots {
				q := spots[j].p.Sub(image.Pt(x, y))
				if q.X*q.X+q.Y*q.Y < 24*24 {
					near = true
					if d > spots[j].d {
						spots[j] = spot{image.Pt(x, y), d}
					}
					break
				}
			}
			if !near {
				spots = append(spots, spot{image.Pt(x, y), d})
			}
		}
	}
	sort.Slice(spots, func(i, j int) bool { return spots[i].d > spots[j].d })
	if len(spots) > 4 {
		spots = spots[:4]
	}
	const c, z = 40, 6
	for n, sp := range spots {
		out := image.NewRGBA(image.Rect(0, 0, 3*c*z+2*z, c*z))
		for y := 0; y < c; y++ {
			for x := 0; x < c; x++ {
				px, py := sp.p.X-c/2+x, sp.p.Y-c/2+y
				var ca, cb color.RGBA
				if image.Pt(px, py).In(r) {
					ca, cb = a.RGBAAt(px, py), b.RGBAAt(px, py)
				}
				diff := func(u, v uint8) uint8 {
					d := (int(u) - int(v)) * 10
					return uint8(min(255, max(d, -d)))
				}
				cd := color.RGBA{diff(ca.R, cb.R), diff(ca.G, cb.G), diff(ca.B, cb.B), 0xff}
				for k, col := range []color.RGBA{ca, cb, cd} {
					for dy := 0; dy < z; dy++ {
						for dx := 0; dx < z; dx++ {
							out.SetRGBA(k*(c*z+z)+x*z+dx, y*z+dy, col)
						}
					}
				}
			}
		}
		f, err := os.Create(filepath.Join(dir, fmt.Sprintf("%s-%d.png", name, n)))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, out); err != nil {
			t.Fatal(err)
		}
		f.Close()
		t.Logf("%s: crop %d at %v, by %d: %v vs %v", name, n, sp.p, sp.d, a.RGBAAt(sp.p.X, sp.p.Y), b.RGBAAt(sp.p.X, sp.p.Y))
	}
}

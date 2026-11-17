package ui

import (
	"fmt"
	"image"
	"os"
	"sort"
	"testing"
	"time"

	"gioui.org/gpu"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// TestFrameCostWindow draws Clack's whole window over internal/fakeslack
// (synthetic data; nothing reaches Slack) the old way and the new, and
// says what one frame costs each: the workspace shadow's rings in one
// piece or in bands, the panel's fill in one piece or around its interior
// (theme.SetLegacyFrameCost), and Gio keeping every clip path on every op
// over one stencil region or not (gpu.ClackLegacyClips, Gio patches 38
// and 39). The same frame (state, time) is laid out each way and drawn,
// light and dark, at 1x and 2x, a channel and a channel with its thread
// open. It logs the draw calls, the graphics API calls (and those the
// backend's state cache left out, Gio patch 37), the stencil, cover and
// cleared pixels, the frame's time to the end of its rasterization, and
// the pixels that differ. CLACK_GPU_COST=1 (flake-check's gpu-cost job,
// on each OS's GPU backend).
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
	for _, scale := range []float32{1, 2} {
		e := startEnv(t, fs, t.TempDir())
		e.scale = scale
		e.size = image.Pt(int(1280*scale), int(800*scale))
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless GPU: %v", err)
		}
		e.gpu = w
		e.waitReady()
		e.openChannel(ch)
		e.settle(2 * time.Second)
		for _, dark := range []bool{false, true} {
			e.a.setDark(dark)
			e.settle(2 * time.Second)
			for _, thread := range []bool{false, true} {
				if thread {
					e.a.OpenThread(ch, root)
					e.until("the thread pane", 20*time.Second, func() bool { return e.a.secondary.kind == secThread })
					e.settle(1500 * time.Millisecond)
				}
				name := fmt.Sprintf("%gx %s%s", scale, map[bool]string{false: "light", true: "dark"}[dark], map[bool]string{false: "", true: " thread"}[thread])
				now := time.Now()
				old, so, to := e.costFrame(t, true, now)
				cur, sn, tn := e.costFrame(t, false, now)
				t.Logf("%s, old: %s, %.1f ms a frame", name, costLine(so), to)
				t.Logf("%s, new: %s, %.1f ms a frame", name, costLine(sn), tn)
				t.Logf("%s: %s", name, costDiff(old, cur))
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

// costFrame lays the window out at now the old way (legacy) or the new,
// draws it a few times (paths built, atlases sized) and returns its
// pixels, the last frame's stats and the median time of 5 more frames to
// the end of their rasterization.
func (e *perfEnv) costFrame(t *testing.T, legacy bool, now time.Time) (*image.RGBA, gpu.FrameStats, float64) {
	t.Helper()
	theme.SetLegacyFrameCost(legacy)
	gpu.ClackLegacyClips(legacy)
	defer theme.SetLegacyFrameCost(false)
	defer gpu.ClackLegacyClips(false)
	e.now = now
	gtx := e.context()
	e.a.frame(gtx)
	e.r.Frame(&e.ops)
	w := e.gpu
	w.ClackStatsDetail(true)
	for range 3 {
		if err := w.Frame(&e.ops); err != nil {
			t.Fatal(err)
		}
	}
	st := w.ClackFrameStats()
	w.ClackStatsDetail(false)
	img := image.NewRGBA(image.Rectangle{Max: e.size})
	if err := w.Screenshot(img); err != nil {
		t.Fatal(err)
	}
	var ms []float64
	for range 5 {
		t0 := time.Now()
		if err := w.Frame(&e.ops); err != nil {
			t.Fatal(err)
		}
		if err := w.ClackSync(); err != nil {
			t.Fatal(err)
		}
		ms = append(ms, float64(time.Since(t0).Microseconds())/1000)
	}
	sort.Float64s(ms)
	return img, st, ms[len(ms)/2]
}

func costLine(s gpu.FrameStats) string {
	return fmt.Sprintf("%d draws, %d passes, %d API calls (%d more without the state cache; %d crossings into C or the system), %d blits, %d covers, %d intersections, %d paths stenciled; stencil %.2f Mpx, cover %.2f, blit %.2f, intersect %.2f, cleared %.2f, atlas %.2f",
		s.Draws, s.Passes, s.APICalls, s.Skipped, s.Crossings, s.Blits, s.Covers, s.Intersections, s.StencilPaths,
		float64(s.StencilPx)/1e6, float64(s.CoverPx)/1e6, float64(s.BlitPx)/1e6, float64(s.IntersectPx)/1e6, float64(s.ClearPx)/1e6, float64(s.AtlasPx)/1e6)
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

//go:build linux || windows || (darwin && cgo)

package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"sync"
	"testing"
	"time"

	"gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// TestRealWindowCaretCost is a caret's blink on Clack's real window
// (App.Run's app.Window and driver: X11 under Xvfb on Linux, Metal on
// macOS, Direct3D on Windows; ci's linux-idle, macos and windows-idle
// jobs and flake-check's gpu-cost), signed in to internal/fakeslack, the
// composer focused and nothing else moving. For 6 s with partial frames
// (Gio patch 46) and 6 s with every frame whole (CLACK_FULL_FRAMES), in
// turns of 3 s so background work lands in both alike, it counts the
// frames the window drew, what the GPU did for each (app.SetGPUFrameHook)
// and the process's CPU time. The blinks must be partial frames of the
// caret's few pixels, each with a tenth of a whole frame's draws at most,
// and where the GPU is a software rasterizer (ci's llvmpipe and WARP: the
// GPU's work is the CPU's) the 6 s of blinks must cost half the CPU of
// whole frames at most. Then it scrolls the message list (a wheel's frame
// every 16 ms) in four turns of 1.5 s each way and reports what the
// scroll's frames cost: whole frames in a row draw straight into the
// window, without the frame texture's copy, and the median turn's CPU a
// frame stays within scrollCPUMax of CLACK_FULL_FRAMES' on every OS. It reads the window's pixels back (caretReadback): on
// X11 the caret's pixels toggle between blinks and a partial frame's window
// is the whole frame's, bit for bit; on macOS each blink's drawable is the
// frame texture's, bit for bit. Synthetic data only.
func TestRealWindowCaretCost(t *testing.T) {
	if on, why := realWindowGate(); !on {
		t.Skip(why)
	}
	caretReadbackSetup(t) // before the window opens
	sz := fakeslack.Small
	sz.Unread = 0
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	home := t.TempDir()
	t.Setenv("CLACK_HOME", home)
	cache := filepath.Join(home, "cache")

	var mu sync.Mutex
	var measuring bool
	var frames int
	var gpuFrames []caretGPUFrame
	frameCh := make(chan caretGPUFrame, 256) // every frame, for caretReadback
	stopHook := caretGPUHook(func(f caretGPUFrame) {
		mu.Lock()
		if measuring {
			gpuFrames = append(gpuFrames, f)
		}
		mu.Unlock()
		select {
		case frameCh <- f:
		default:
		}
	})
	t.Cleanup(stopHook)
	t.Cleanup(func() { caretPartialFrames(true) })
	images := imgcache.New(fs.Fetch, "", nil)
	a := New(Options{ConfigDir: home, NoRealtime: true, Started: time.Now(), OnWindowEvent: func(e event.Event) {
		realWindowEvent(e)
		caretWindowEvent(e)
		if _, ok := e.(app.FrameEvent); ok {
			mu.Lock()
			if measuring {
				frames++
			}
			mu.Unlock()
		}
	}}, images)
	// a small window: Mesa's llvmpipe draws Clack's 1440×900 in about
	// 400 ms; CLACK_CARET_WINDOW=1440x900 measures that one
	ww, wh := 800, 600
	if v := os.Getenv("CLACK_CARET_WINDOW"); v != "" {
		if _, err := fmt.Sscanf(v, "%dx%d", &ww, &wh); err != nil {
			t.Fatalf("CLACK_CARET_WINDOW=%q: %v", v, err)
		}
	}
	a.prefs.MainWindow = &mainWindowSettings{Size: &winSize{Width: ww, Height: wh}, Bounds: &winRect{X: 60, Y: 60, Width: ww, Height: wh}}
	a.Attach(core.New(slack.New(fs.Creds()), store.New(cache)), cache)
	done := make(chan error, 1)
	go func() { done <- a.Run() }()
	t.Cleanup(func() {
		a.Quit()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Error("Quit didn't end Run")
		}
	})
	until := func(what string, d time.Duration, ok func() bool) {
		t.Helper()
		for dl := time.Now().Add(d); !ok(); time.Sleep(50 * time.Millisecond) {
			select {
			case err := <-done:
				t.Fatalf("%s: Run returned (%v)", what, err)
			default:
			}
			if time.Now().After(dl) {
				t.Fatalf("timed out waiting for %s", what)
			}
		}
	}
	onUI := func(f func()) {
		t.Helper()
		ran := make(chan struct{})
		a.Post(func() { f(); close(ran) })
		select {
		case <-ran:
		case <-time.After(30 * time.Second):
			t.Fatal("work posted to the UI goroutine didn't run")
		}
	}
	var booted time.Time
	until("core ready", 90*time.Second, func() bool {
		onUI(func() {
			if a.data != nil {
				booted = a.data.booted
			}
		})
		return !booted.IsZero()
	})
	realWindowShow(t)
	ch := fs.ChannelIDs()[0]
	onUI(func() { a.Navigate("c/" + ch) })
	until("the channel's history", 60*time.Second, func() bool {
		found := false
		onUI(func() { found = a.data.core.S.Timeline(ch) != nil })
		return found
	})
	until("the composer", 30*time.Second, func() bool {
		ok := false
		onUI(func() {
			if c := a.convoFor(ch, ""); c != nil && c.comp != nil {
				c.comp.Focus()
				ok = true
			}
		})
		return ok
	})
	// core's deferred boot loads, the channel's images and names
	if d := 8*time.Second - time.Since(booted); d > 0 {
		time.Sleep(d)
	}
	time.Sleep(2 * time.Second)

	type run struct {
		frames int
		gpu    []caretGPUFrame
		cpu    time.Duration
	}
	// measure is d of the window's frames with partial frames on or off,
	// while during runs (nil: nothing but the caret)
	measure := func(partial bool, d time.Duration, during func(stop <-chan struct{})) run {
		caretPartialFrames(partial)
		time.Sleep(time.Second) // the mode's first frame, whole
		mu.Lock()
		measuring, frames, gpuFrames = true, 0, nil
		mu.Unlock()
		stop := make(chan struct{})
		ended := make(chan struct{})
		go func() {
			defer close(ended)
			if during != nil {
				during(stop)
			}
		}()
		c0 := procCPU()
		time.Sleep(d)
		cpu := procCPU() - c0
		close(stop)
		<-ended
		mu.Lock()
		defer mu.Unlock()
		measuring = false
		return run{frames: frames, gpu: append([]caretGPUFrame(nil), gpuFrames...), cpu: cpu}
	}
	// turns of 3 s, partial and whole, twice
	add := func(r *run, x run) {
		r.frames += x.frames
		r.gpu = append(r.gpu, x.gpu...)
		r.cpu += x.cpu
	}
	var part, whole run
	for range 2 {
		add(&part, measure(true, 3*time.Second, nil))
		add(&whole, measure(false, 3*time.Second, nil))
	}
	caretPartialFrames(true)

	median := func(fs []caretGPUFrame, f func(caretGPUFrame) int64) int64 {
		var v []int64
		for _, x := range fs {
			v = append(v, f(x))
		}
		if len(v) == 0 {
			return 0
		}
		sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
		return v[len(v)/2]
	}
	draws := func(f caretGPUFrame) int64 { return int64(f.draws) }
	calls := func(f caretGPUFrame) int64 { return f.apiCalls }
	px := func(f caretGPUFrame) int64 { return f.damagePx }
	// the blinks: partial frames of a small damage (a frame something
	// else changed, a load landing, isn't judged by the caret's limits)
	partials := 0
	var blinks []caretGPUFrame
	for _, f := range part.gpu {
		if f.partial {
			partials++
			if f.damagePx > 0 && f.damagePx <= int64(ww*wh)/200 {
				blinks = append(blinks, f)
			}
		}
	}
	blink := widget.CaretBlinkInterval()
	t.Logf("a %d×%d window", ww, wh)
	t.Logf("partial frames: %d frames drawn in 6 s (the caret blinks every %v), %d through the GPU, %d partial, %d of them blinks: median %d draws, %d API calls, %d px damaged; the process's CPU %v",
		part.frames, blink, len(part.gpu), partials, len(blinks), median(blinks, draws), median(blinks, calls), median(blinks, px), part.cpu.Round(time.Millisecond))
	t.Logf("whole frames: %d frames drawn in 6 s, %d through the GPU: median %d draws, %d API calls; the process's CPU %v",
		whole.frames, len(whole.gpu), median(whole.gpu, draws), median(whole.gpu, calls), whole.cpu.Round(time.Millisecond))
	if want := int(6*time.Second/blink) / 2; blink <= 0 || part.frames < want || whole.frames < want {
		t.Fatalf("%d and %d frames in 6 s, want %d at least each: the caret doesn't blink (%v)", part.frames, whole.frames, want, blink)
	}
	if len(part.gpu) == 0 {
		t.Errorf("no GPU frame stats from the window (app.SetGPUFrameHook)")
	} else {
		if partials*10 < len(part.gpu)*8 {
			t.Errorf("%d of %d blink frames partial, want 80%% at least", partials, len(part.gpu))
		}
		if want := int(6*time.Second/blink) / 2; len(blinks) < want {
			t.Errorf("%d partial frames of the caret's few pixels (0.5%% of the window at most) in 6 s, want %d at least", len(blinks), want)
		}
		if dp, dw := median(blinks, draws), median(whole.gpu, draws); dp*10 > dw {
			t.Errorf("a blink takes %d draws, a whole frame %d: want a tenth at most", dp, dw)
		}
	}
	if runtime.GOOS != "darwin" && whole.cpu > 0 && part.cpu*2 > whole.cpu {
		t.Errorf("6 s of blinks cost %v of CPU with partial frames, %v with whole ones: want half at most", part.cpu.Round(time.Millisecond), whole.cpu.Round(time.Millisecond))
	}

	// the window's own pixels (X11): the caret toggles, a partial frame is
	// the whole frame's
	caretReadback(t, a, frameCh, blink, ww, wh)

	// a scroll: the message list moved by a wheel's frame every 16 ms
	// (up for half a turn, back down for the other half), 1.5 s at a time
	// with partial frames on and off, four times each
	const turn = 1500 * time.Millisecond
	scroll := func(stop <-chan struct{}) {
		tick := time.NewTicker(16 * time.Millisecond)
		defer tick.Stop()
		half := int(turn/(16*time.Millisecond)) / 2
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			case <-tick.C:
			}
			d := -24
			if i >= half {
				d = 24
			}
			a.Post(func() {
				if v, ok := a.convoFor(ch, "").list.(interface{ ScrollPx(int) }); ok {
					v.ScrollPx(d)
				}
			})
		}
	}
	var sPart, sWhole run
	var pTurns, wTurns []time.Duration // each turn's CPU a frame
	perFrame := func(r run) time.Duration { return r.cpu / time.Duration(max(r.frames, 1)) }
	for range 4 {
		p := measure(true, turn, scroll)
		add(&sPart, p)
		w := measure(false, turn, scroll)
		add(&sWhole, w)
		pTurns, wTurns = append(pTurns, perFrame(p)), append(wTurns, perFrame(w))
	}
	caretPartialFrames(true)
	onUI(func() {
		if v, ok := a.convoFor(ch, "").list.(interface{ ScrollToBottom() }); ok {
			v.ScrollToBottom()
		}
	})
	scrollLine := func(r run) string {
		var wholeN, partN, copied int
		var copyPx int64
		for _, f := range r.gpu {
			if f.partial {
				partN++
			} else {
				wholeN++
			}
			if f.copyPx > 0 {
				copied++
				copyPx += f.copyPx
			}
		}
		per := time.Duration(0)
		if r.frames > 0 {
			per = r.cpu / time.Duration(r.frames)
		}
		atlas := func(f caretGPUFrame) int64 { return f.atlasPx }
		return fmt.Sprintf("%d frames in 6 s (%d whole, %d partial), %d through the frame texture's copy (%.1f Mpx copied), the coverage atlas %.2f Mpx (median); the process's CPU %v, %v a frame",
			r.frames, wholeN, partN, copied, float64(copyPx)/1e6, float64(median(r.gpu, atlas))/1e6, r.cpu.Round(time.Millisecond), per.Round(10*time.Microsecond))
	}
	t.Logf("a scroll with partial frames: %s", scrollLine(sPart))
	t.Logf("a scroll with every frame whole (CLACK_FULL_FRAMES): %s", scrollLine(sWhole))
	// whole frames in a row draw straight into the window
	var wholeRun, copiedRun int
	for i, f := range sPart.gpu {
		if i > 0 && !f.partial && !sPart.gpu[i-1].partial {
			wholeRun++
			if f.copyPx > 0 {
				copiedRun++
			}
		}
	}
	if wholeRun > 0 && copiedRun*5 > wholeRun {
		t.Errorf("%d of %d scroll frames drawn whole after a whole frame went through the frame texture's copy; want them drawn straight into the window", copiedRun, wholeRun)
	}
	// a scroll's frames are whole: partial frames' signatures, diff and
	// sticky places must cost them little (on a Mac's GPU the CPU's part
	// of a frame is a few ms, so their share shows there most: 10% in
	// most runs). The median turn of each, so one turn's background work
	// (a load landing, a collection) doesn't decide it.
	if sWhole.frames > 0 && sPart.frames > 0 {
		mid := func(v []time.Duration) time.Duration {
			v = slices.Clone(v)
			slices.Sort(v)
			return (v[len(v)/2-1] + v[len(v)/2]) / 2
		}
		pp, pw := mid(pTurns), mid(wTurns)
		ratio := float64(pp) / float64(max(pw, 1))
		t.Logf("a scroll's frame costs %v of CPU with partial frames, %v without (%.0f%%; the turns' %v and %v)", pp.Round(10*time.Microsecond), pw.Round(10*time.Microsecond), 100*ratio, pTurns, wTurns)
		if ratio > scrollCPUMax {
			t.Errorf("a scroll's frame costs %v of CPU with partial frames, %v without: %.0f%%, want %.0f%% at most", pp.Round(10*time.Microsecond), pw.Round(10*time.Microsecond), 100*ratio, 100*scrollCPUMax)
		}
	}
}

// scrollCPUMax bounds a scroll frame's CPU with partial frames against
// without (TestRealWindowCaretCost): what they add to whole frames, with
// the runners' noise (the Mac's turns ran 110% to 129%).
const scrollCPUMax = 1.25

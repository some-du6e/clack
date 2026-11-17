//go:build linux || windows || (darwin && cgo)

package ui

import (
	"path/filepath"
	"runtime"
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
// (Gio patch 45) and 6 s with every frame whole (CLACK_FULL_FRAMES), it
// counts the frames the window drew, what the GPU did for each
// (app.SetGPUFrameHook) and the process's CPU time. The blinks must be
// partial frames of the caret's few pixels, each with a tenth of a whole
// frame's draws at most, and where the GPU is a software rasterizer (ci's
// llvmpipe and WARP: the GPU's work is the CPU's) the 6 s of blinks must
// cost half the CPU of whole frames at most. Synthetic data only.
func TestRealWindowCaretCost(t *testing.T) {
	if on, why := realWindowGate(); !on {
		t.Skip(why)
	}
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
	stopHook := caretGPUHook(func(f caretGPUFrame) {
		mu.Lock()
		if measuring {
			gpuFrames = append(gpuFrames, f)
		}
		mu.Unlock()
	})
	t.Cleanup(stopHook)
	t.Cleanup(func() { caretPartialFrames(true) })
	images := imgcache.New(fs.Fetch, "", nil)
	a := New(Options{ConfigDir: home, NoRealtime: true, Started: time.Now(), OnWindowEvent: func(e event.Event) {
		realWindowEvent(e)
		if _, ok := e.(app.FrameEvent); ok {
			mu.Lock()
			if measuring {
				frames++
			}
			mu.Unlock()
		}
	}}, images)
	// a small window: Mesa's llvmpipe draws Clack's 1440×900 in about
	// 400 ms
	a.prefs.MainWindow = &mainWindowSettings{Size: &winSize{Width: 800, Height: 600}, Bounds: &winRect{X: 60, Y: 60, Width: 800, Height: 600}}
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
	measure := func() run {
		mu.Lock()
		measuring, frames, gpuFrames = true, 0, nil
		mu.Unlock()
		c0 := procCPU()
		time.Sleep(6 * time.Second)
		cpu := procCPU() - c0
		mu.Lock()
		defer mu.Unlock()
		measuring = false
		return run{frames: frames, gpu: append([]caretGPUFrame(nil), gpuFrames...), cpu: cpu}
	}
	part := measure()
	caretPartialFrames(false)
	time.Sleep(time.Second) // the first whole frame
	whole := measure()
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
	partials := 0
	for _, f := range part.gpu {
		if f.partial {
			partials++
		}
	}
	blink := widget.CaretBlinkInterval()
	t.Logf("partial frames: %d frames drawn in 6 s (the caret blinks every %v), %d through the GPU, %d partial: median %d draws, %d API calls, %d px damaged; the process's CPU %v",
		part.frames, blink, len(part.gpu), partials, median(part.gpu, draws), median(part.gpu, calls), median(part.gpu, px), part.cpu.Round(time.Millisecond))
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
		if d := median(part.gpu, px); d > 800*600/200 {
			t.Errorf("a blink damages %d px (median), want the caret's few (0.5%% of the window at most)", d)
		}
		if dp, dw := median(part.gpu, draws), median(whole.gpu, draws); dp*10 > dw {
			t.Errorf("a blink takes %d draws, a whole frame %d: want a tenth at most", dp, dw)
		}
	}
	if runtime.GOOS != "darwin" && whole.cpu > 0 && part.cpu*2 > whole.cpu {
		t.Errorf("6 s of blinks cost %v of CPU with partial frames, %v with whole ones: want half at most", part.cpu.Round(time.Millisecond), whole.cpu.Round(time.Millisecond))
	}
}

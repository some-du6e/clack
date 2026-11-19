//go:build windows

package ui

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"gioui.org/app"
	"gioui.org/gpu"
	"gioui.org/io/event"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// TestRealWindowMemory is what partial frames (Gio patch 46) cost in
// memory on Clack's real window (App.Run's, Direct3D 11; ci's windows-idle
// job): Karim's window (its client area 1918×1080 at 1x, his light custom
// theme, a channel with a thread open, the composer focused, its caret
// blinking), with partial frames and with CLACK_FULL_FRAMES=1 (Gio's own
// whole frames and atlas packing), each in a process of its own (this
// test's binary again: one App per process, and the frame mode is set as
// Run starts), memPairs pairs of them, the two modes in turn. Each
// measures, once the boot's loads have settled: the process's private
// working set (Task Manager's Memory) and commit (private bytes), the Go
// heap, and what the Direct3D 11 device holds (FrameStats.
// DeviceTextureBytes and DeviceBufferBytes; the frame texture and clear
// texture partial frames keep, FrameTextureBytes). The pairs' median
// difference in commit must be what partial frames keep by design: the
// frame texture and the clear texture, and memBudgetSlack for their CPU
// side (each op's signature, the diff, the sticky atlas places).
// Synthetic data only.
func TestRealWindowMemory(t *testing.T) {
	if os.Getenv("CLACK_MEMORY_CHILD") != "" {
		memoryChild(t)
		return
	}
	if on, why := realWindowGate(); !on {
		t.Skip(why)
	}
	mb := func(b int64) string { return fmt.Sprintf("%.1f MB", float64(b)/(1<<20)) }
	line := func(name string, m memSample) {
		t.Logf("%s: a %v window; private working set %s, commit %s; Go heap in use %s (the Go runtime %s from the OS); the device's textures %s (the frame and clear textures %s, the coverage atlas %.2f Mpx, layers %.2f Mpx), buffers %s",
			name, m.Size, mb(m.PrivateWS), mb(m.Commit), mb(m.HeapInuse), mb(m.GoSys), mb(m.DevTex), mb(m.FrameTex), float64(m.AtlasPx)/1e6, float64(m.LayerPx)/1e6, mb(m.DevBuf))
	}
	cost := func(name string, d memDiff, frameTex int64) {
		t.Logf("%s: partial frames cost commit %s, private working set %s; of it the device's textures %s (the frame and clear textures %s, the rest %s), buffers %s, the Go runtime %s (heap in use %s); not accounted for %s",
			name, mb(d.Commit), mb(d.WS), mb(d.Tex), mb(frameTex), mb(d.Tex-frameTex), mb(d.Buf), mb(d.Go), mb(d.Heap), mb(d.Unaccounted))
	}
	var parts, fulls []memSample
	var diffs []memDiff
	for i := range memPairs {
		p := runMemoryChild(t, false)
		f := runMemoryChild(t, true)
		parts, fulls = append(parts, p), append(fulls, f)
		line(fmt.Sprintf("pair %d, partial frames", i+1), p)
		line(fmt.Sprintf("pair %d, every frame whole (CLACK_FULL_FRAMES)", i+1), f)
		diffs = append(diffs, memDiffOf(p, f))
		cost(fmt.Sprintf("pair %d", i+1), diffs[i], p.FrameTex)
	}
	part, full, d := memMedians(parts), memMedians(fulls), memDiffMedians(diffs)
	line("partial frames, the medians", part)
	line("every frame whole, the medians", full)
	cost("the pairs' medians", d, part.FrameTex)
	for i := range parts {
		if parts[i].Size != memWindow || fulls[i].Size != memWindow {
			t.Logf("pair %d: the windows were %v and %v, not %v (the runner's display)", i+1, parts[i].Size, fulls[i].Size, memWindow)
		}
		if want := int64(parts[i].Size.X*parts[i].Size.Y) * 4; parts[i].FrameTex < want {
			t.Errorf("pair %d: partial frames hold %s of frame and clear textures, want the window's %s frame texture at least", i+1, mb(parts[i].FrameTex), mb(want))
		}
		if fulls[i].FrameTex != 0 {
			t.Errorf("pair %d: every frame whole: %s of frame and clear textures held, want none", i+1, mb(fulls[i].FrameTex))
		}
	}
	if budget := part.FrameTex + memBudgetSlack; d.Commit > budget {
		t.Errorf("partial frames cost %s of commit (the pairs' median), more than their frame and clear textures (%s) and %s: the device's textures %s, buffers %s, the Go runtime %s",
			mb(d.Commit), mb(part.FrameTex), mb(memBudgetSlack), mb(d.Tex), mb(d.Buf), mb(d.Go))
	}
}

// memWindow is Karim's window (his inspect report of 2026-11-17): its
// client area, what Gio draws.
var memWindow = struct{ X, Y int }{1918, 1080}

// memPairs is how many pairs of processes TestRealWindowMemory runs, a
// partial frames one then a CLACK_FULL_FRAMES one: one process's figures
// vary from the next one's.
const memPairs = 3

// memDiff is what partial frames cost in one pair: the partial frames
// process's figure less the whole frames one's.
type memDiff struct {
	Commit, WS         int64
	Tex, Buf, Go, Heap int64
	Unaccounted        int64 // commit less the device's textures and buffers and the Go runtime
}

func memDiffOf(p, f memSample) memDiff {
	d := memDiff{Commit: p.Commit - f.Commit, WS: p.PrivateWS - f.PrivateWS, Tex: p.DevTex - f.DevTex,
		Buf: p.DevBuf - f.DevBuf, Go: p.GoSys - f.GoSys, Heap: p.HeapInuse - f.HeapInuse}
	d.Unaccounted = d.Commit - d.Tex - d.Buf - d.Go
	return d
}

// memMedian is the median of v (the upper one of an even count).
func memMedian(v []int64) int64 {
	s := append([]int64(nil), v...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2]
}

// memMedians is each figure's median over the samples (the first one's
// size and counts).
func memMedians(ms []memSample) memSample {
	of := func(f func(memSample) int64) int64 {
		var v []int64
		for _, m := range ms {
			v = append(v, f(m))
		}
		return memMedian(v)
	}
	m := ms[0]
	m.PrivateWS = of(func(s memSample) int64 { return s.PrivateWS })
	m.Commit = of(func(s memSample) int64 { return s.Commit })
	m.HeapInuse = of(func(s memSample) int64 { return s.HeapInuse })
	m.GoSys = of(func(s memSample) int64 { return s.GoSys })
	m.DevTex = of(func(s memSample) int64 { return s.DevTex })
	m.DevBuf = of(func(s memSample) int64 { return s.DevBuf })
	m.FrameTex = of(func(s memSample) int64 { return s.FrameTex })
	m.AtlasPx = of(func(s memSample) int64 { return s.AtlasPx })
	m.LayerPx = of(func(s memSample) int64 { return s.LayerPx })
	return m
}

// memDiffMedians is each figure's median over the pairs.
func memDiffMedians(ds []memDiff) memDiff {
	of := func(f func(memDiff) int64) int64 {
		var v []int64
		for _, d := range ds {
			v = append(v, f(d))
		}
		return memMedian(v)
	}
	return memDiff{
		Commit: of(func(d memDiff) int64 { return d.Commit }), WS: of(func(d memDiff) int64 { return d.WS }),
		Tex: of(func(d memDiff) int64 { return d.Tex }), Buf: of(func(d memDiff) int64 { return d.Buf }),
		Go: of(func(d memDiff) int64 { return d.Go }), Heap: of(func(d memDiff) int64 { return d.Heap }),
		Unaccounted: of(func(d memDiff) int64 { return d.Unaccounted }),
	}
}

// memBudgetSlack is what partial frames may keep beyond their frame and
// clear textures: every op's signature and the last frame's, the diff's
// trace, the sticky atlas places (under a megabyte at Karim's window), and
// the measurement's noise between two processes.
const memBudgetSlack = 4 << 20

// memSample is what a child measured (JSON, a line its parent reads).
type memSample struct {
	Size                  struct{ X, Y int }
	PrivateWS, Commit     int64
	HeapInuse, GoSys      int64
	DevTex, DevBuf        int64
	FrameTex              int64
	AtlasPx, LayerPx      int64
	Partial               bool
	Frames, PartialFrames int
	DevTexMin, DevTexMax  int64
	CommitMin, CommitMax  int64
}

// runMemoryChild runs this test's binary as a child measuring one mode.
func runMemoryChild(t *testing.T, full bool) memSample {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestRealWindowMemory$", "-test.v", "-test.count=1", "-test.timeout=10m")
	cmd.Env = append(os.Environ(), "CLACK_MEMORY_CHILD=1")
	if full {
		cmd.Env = append(cmd.Env, "CLACK_FULL_FRAMES=1")
	}
	out, err := cmd.CombinedOutput()
	var m memSample
	found := false
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		if s, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "CLACK_MEMORY "); ok {
			found = json.Unmarshal([]byte(s), &m) == nil
		}
	}
	if err != nil || !found {
		tail := string(out)
		if len(tail) > 4000 {
			tail = tail[len(tail)-4000:]
		}
		t.Fatalf("the child (full frames %v): %v, measured %v:\n%s", full, err, found, tail)
	}
	return m
}

// memoryChild opens Karim's window and prints what it measured.
func memoryChild(t *testing.T) {
	sz := fakeslack.Small
	sz.Unread = 0
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	fs.SetPrefs(map[string]any{"ia_theme": costKarim})
	users := fs.UserIDs()
	ch := fs.ChannelIDs()[1]
	root, _ := fs.PostQuiet(ch, users[1], "a thread for the memory window")
	for i := 0; i < 6; i++ {
		fs.PostReplyQuiet(ch, root, users[1+i%3], fmt.Sprintf("reply %d, long enough to wrap the thread pane's line at least once or twice", i))
	}
	home := t.TempDir()
	t.Setenv("CLACK_HOME", home)
	cache := filepath.Join(home, "cache")

	var mu sync.Mutex
	var last gpu.FrameStats
	var frames, partials int
	var size struct{ X, Y int }
	var hwnd uintptr
	app.SetGPUFrameHook(func(s gpu.FrameStats) {
		mu.Lock()
		last = s
		frames++
		if s.Partial {
			partials++
		}
		mu.Unlock()
	})
	t.Cleanup(func() { app.SetGPUFrameHook(nil) })
	images := imgcache.New(fs.Fetch, "", nil)
	a := New(Options{ConfigDir: home, NoRealtime: true, Started: time.Now(), OnWindowEvent: func(e event.Event) {
		realWindowEvent(e)
		switch e := e.(type) {
		case app.FrameEvent:
			mu.Lock()
			size.X, size.Y = e.Size.X, e.Size.Y
			mu.Unlock()
		case app.Win32ViewEvent:
			if e.HWND != 0 {
				mu.Lock()
				hwnd = e.HWND
				mu.Unlock()
			}
		}
	}}, images)
	a.prefs.MainWindow = &mainWindowSettings{Size: &winSize{Width: memWindow.X, Height: memWindow.Y},
		Bounds: &winRect{X: 0, Y: 0, Width: memWindow.X, Height: memWindow.Y}}
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
	// the bounds above are the window's outer size (its frame and the
	// invisible resize borders inside them): its client area, what Gio
	// draws, made Karim's at the screen's top left
	var win uintptr
	until("the window's HWND", 30*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		win = hwnd
		return win != 0
	})
	memSizeClient(win, memWindow.X, memWindow.Y)
	at := func() bool {
		mu.Lock()
		defer mu.Unlock()
		return size == memWindow
	}
	for dl := time.Now().Add(10 * time.Second); !at() && time.Now().Before(dl); {
		time.Sleep(50 * time.Millisecond)
	}
	onUI(func() { a.Navigate("c/" + ch) })
	until("the channel's history", 60*time.Second, func() bool {
		found := false
		onUI(func() { found = a.data.core.S.Timeline(ch) != nil })
		return found
	})
	onUI(func() { a.OpenThread(ch, root) })
	until("the thread pane", 30*time.Second, func() bool {
		open := false
		onUI(func() { open = a.secondary.kind == secThread })
		return open
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
	// core's deferred boot loads, the channel's images and names, then a
	// while of blinks
	if d := 8*time.Second - time.Since(booted); d > 0 {
		time.Sleep(d)
	}
	time.Sleep(5 * time.Second)

	// five samples a second apart, each after a collection that gives the
	// Go heap's free pages back: the medians
	var samples []memSample
	for range 5 {
		time.Sleep(time.Second)
		runtime.GC()
		debug.FreeOSMemory()
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		ws, commit, err := memProcess()
		if err != nil {
			t.Fatalf("GetProcessMemoryInfo: %v", err)
		}
		mu.Lock()
		s := memSample{PrivateWS: ws, Commit: commit, HeapInuse: int64(ms.HeapInuse),
			GoSys: int64(ms.Sys - ms.HeapReleased), DevTex: last.DeviceTextureBytes, DevBuf: last.DeviceBufferBytes,
			FrameTex: last.FrameTextureBytes, AtlasPx: last.AtlasPx, LayerPx: last.LayerPx, Partial: last.Partial}
		s.Size = size
		mu.Unlock()
		samples = append(samples, s)
	}
	m := memMedians(samples)
	m.Size, m.Partial = samples[len(samples)-1].Size, samples[len(samples)-1].Partial
	m.DevTexMin, m.DevTexMax = samples[0].DevTex, samples[0].DevTex
	m.CommitMin, m.CommitMax = samples[0].Commit, samples[0].Commit
	for _, s := range samples {
		m.DevTexMin, m.DevTexMax = min(m.DevTexMin, s.DevTex), max(m.DevTexMax, s.DevTex)
		m.CommitMin, m.CommitMax = min(m.CommitMin, s.Commit), max(m.CommitMax, s.Commit)
	}
	mu.Lock()
	m.Frames, m.PartialFrames = frames, partials
	mu.Unlock()
	b, _ := json.Marshal(m)
	fmt.Printf("CLACK_MEMORY %s\n", b)
}

// memSizeClient moves and sizes hwnd's window so its client area is w×h at
// the screen's top left, its frame around that.
func memSizeClient(hwnd uintptr, w, h int) {
	var wr, cr tbRect
	tbGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&wr)))
	tbGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&cr)))
	o := tbOrigin(hwnd) // the client area's top left on the screen
	x, y := int(wr.Left)-o.X, int(wr.Top)-o.Y
	fw := int(wr.Right-wr.Left) - int(cr.Right-cr.Left)
	fh := int(wr.Bottom-wr.Top) - int(cr.Bottom-cr.Top)
	const noZOrder, noActivate = 0x0004, 0x0010 // SWP_NOZORDER, SWP_NOACTIVATE
	tbSetWindowPos.Call(hwnd, 0, uintptr(x), uintptr(y), uintptr(w+fw), uintptr(h+fh), noZOrder|noActivate)
}

// memCounters is PROCESS_MEMORY_COUNTERS_EX2.
type memCounters struct {
	CB, PageFaultCount         uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
	PrivateUsage               uintptr
	PrivateWorkingSetSize      uintptr
	SharedCommitUsage          uint64
}

var memGetProcessMemoryInfo = windows.NewLazySystemDLL("kernel32.dll").NewProc("K32GetProcessMemoryInfo")

// memProcess is the process's private working set and commit.
func memProcess() (privateWS, commit int64, err error) {
	var c memCounters
	c.CB = uint32(unsafe.Sizeof(c))
	r, _, e := memGetProcessMemoryInfo.Call(uintptr(windows.CurrentProcess()), uintptr(unsafe.Pointer(&c)), uintptr(c.CB))
	if r == 0 {
		return 0, 0, e
	}
	return int64(c.PrivateWorkingSetSize), int64(c.PrivateUsage), nil
}

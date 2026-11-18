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
// job): Karim's window (1918×1080 at 1x, his light custom theme, a channel
// with a thread open, the composer focused, its caret blinking), once with
// partial frames and once with CLACK_FULL_FRAMES=1, each in a process of
// its own (this test's binary again: one App per process, and the frame
// mode is set as Run starts). Each measures, once the boot's loads have
// settled: the process's private working set (Task Manager's Memory) and
// commit (private bytes), the Go heap, and what the Direct3D 11 device
// holds (FrameStats.DeviceTextureBytes and DeviceBufferBytes; the frame
// texture and clear texture partial frames keep, FrameTextureBytes). The
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
	part := runMemoryChild(t, false)
	full := runMemoryChild(t, true)
	mb := func(b int64) string { return fmt.Sprintf("%.1f MB", float64(b)/(1<<20)) }
	line := func(name string, m memSample) {
		t.Logf("%s: a %v window; private working set %s, commit %s; Go heap in use %s (the Go runtime %s from the OS); the device's textures %s (the frame and clear textures %s, the coverage atlas %.2f Mpx, layers %.2f Mpx), buffers %s",
			name, m.Size, mb(m.PrivateWS), mb(m.Commit), mb(m.HeapInuse), mb(m.GoSys), mb(m.DevTex), mb(m.FrameTex), float64(m.AtlasPx)/1e6, float64(m.LayerPx)/1e6, mb(m.DevBuf))
	}
	line("partial frames", part)
	line("every frame whole (CLACK_FULL_FRAMES)", full)
	dCommit, dWS := part.Commit-full.Commit, part.PrivateWS-full.PrivateWS
	dTex, dBuf, dGo := part.DevTex-full.DevTex, part.DevBuf-full.DevBuf, part.GoSys-full.GoSys
	t.Logf("partial frames cost: commit %s, private working set %s; of it the device's textures %s (the frame and clear textures %s, the rest %s), buffers %s, the Go runtime %s (heap in use %s); not accounted for %s",
		mb(dCommit), mb(dWS), mb(dTex), mb(part.FrameTex), mb(dTex-part.FrameTex), mb(dBuf), mb(dGo), mb(part.HeapInuse-full.HeapInuse), mb(dCommit-dTex-dBuf-dGo))
	if part.Size != (memWindow) || full.Size != memWindow {
		t.Logf("the windows were %v and %v, not %v (the runner's display)", part.Size, full.Size, memWindow)
	}
	if want := int64(part.Size.X*part.Size.Y) * 4; part.FrameTex < want {
		t.Errorf("partial frames hold %s of frame and clear textures, want the window's %s frame texture at least", mb(part.FrameTex), mb(want))
	}
	if full.FrameTex != 0 {
		t.Errorf("every frame whole: %s of frame and clear textures held, want none", mb(full.FrameTex))
	}
	if budget := part.FrameTex + memBudgetSlack; dCommit > budget {
		t.Errorf("partial frames cost %s of commit, more than their frame and clear textures (%s) and %s: the device's textures %s, buffers %s, the Go runtime %s",
			mb(dCommit), mb(part.FrameTex), mb(memBudgetSlack), mb(dTex), mb(dBuf), mb(dGo))
	}
}

// memWindow is Karim's window (his inspect report of 2026-11-17).
var memWindow = struct{ X, Y int }{1918, 1080}

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
		if fe, ok := e.(app.FrameEvent); ok {
			mu.Lock()
			size.X, size.Y = fe.Size.X, fe.Size.Y
			mu.Unlock()
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
	med := func(f func(memSample) int64) int64 {
		var v []int64
		for _, s := range samples {
			v = append(v, f(s))
		}
		sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
		return v[len(v)/2]
	}
	m := samples[len(samples)-1]
	m.PrivateWS = med(func(s memSample) int64 { return s.PrivateWS })
	m.Commit = med(func(s memSample) int64 { return s.Commit })
	m.HeapInuse = med(func(s memSample) int64 { return s.HeapInuse })
	m.GoSys = med(func(s memSample) int64 { return s.GoSys })
	m.DevTex = med(func(s memSample) int64 { return s.DevTex })
	m.DevBuf = med(func(s memSample) int64 { return s.DevBuf })
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

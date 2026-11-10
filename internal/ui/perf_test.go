package ui

// Performance benchmarks of the whole client (ui root + core + every
// feature package) against internal/fakeslack, a synthetic workspace the
// size of the Hack Club Slack (65k custom emoji, 400 channels, 3000 users).
// The perf workflow runs them; see .github/workflows/perf.yml.
//
// Frames are laid out headlessly with a real input.Router (shortcuts,
// clicks, scrolling and typing go through Gio's event routing). With
// CLACK_BENCH_GPU=1 every frame is also rendered by Gio's GPU renderer into
// an offscreen EGL surface (llvmpipe in CI), which is where path vertex
// data and textures live.

import (
	"image"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

var (
	perfOnce sync.Once
	perfFake *fakeslack.Server
	perfErr  error
)

// perfServer is the fake Slack: fakeslackd in another process when
// CLACK_FAKESLACK_URL is set (bench.sh does that, so the heap numbers are
// the client's alone), otherwise in-process.
func perfServer(tb testing.TB) *fakeslack.Server {
	perfOnce.Do(func() {
		if os.Getenv("CLACK_BENCH_LOG") == "" {
			log.SetOutput(io.Discard) // keeps benchmark output parseable
		}
		sz := fakeslack.HackClub
		if testing.Short() {
			sz = fakeslack.Small
		}
		if u := os.Getenv("CLACK_FAKESLACK_URL"); u != "" {
			perfFake = fakeslack.Remote(u, sz)
			return
		}
		perfFake, perfErr = fakeslack.Start(sz)
	})
	if perfErr != nil {
		tb.Fatal(perfErr)
	}
	return perfFake
}

type perfEnv struct {
	tb    testing.TB
	fs    *fakeslack.Server
	a     *App
	home  string
	r     input.Router
	ops   op.Ops
	size  image.Point
	gpu   *headless.Window
	now   time.Time
	start time.Time
}

// newPerfEnv starts the client against the fake. warm reuses (or creates)
// a config dir whose metadata and sidebar caches were written by an
// earlier session, like any start after the first.
func newPerfEnv(tb testing.TB, warm bool) *perfEnv {
	fs := perfServer(tb)
	home := tb.TempDir()
	if warm {
		home = warmHome(tb, fs)
	}
	return startEnv(tb, fs, home)
}

var (
	warmOnce sync.Once
	warmDir  string
)

// warmHome runs one full session to fill the caches, then hands out copies.
//
// CLACK_BENCH_WARMDIR names a frozen warm dir made by an earlier process
// (TestPerfMakeWarmDir), so no warm-up session lives in the measured one.
func warmHome(tb testing.TB, fs *fakeslack.Server) string {
	warmOnce.Do(func() {
		if d := os.Getenv("CLACK_BENCH_WARMDIR"); d != "" {
			if _, err := os.Stat(filepath.Join(d, "cache", "meta.json")); err == nil {
				warmDir = d
				return
			}
		}
		dir, err := os.MkdirTemp("", "clack-perf-warm-")
		if err != nil {
			tb.Fatal(err)
		}
		e := startEnv(tb, fs, dir)
		e.waitReady()
		e.openChannel(fs.ChannelIDs()[1])
		e.settle(300 * time.Millisecond)
		<-e.a.data.stop() // saves sidebar.json and meta.json
		e.shutdown()
		frozen, err := os.MkdirTemp("", "clack-perf-warm-frozen-")
		if err != nil {
			tb.Fatal(err)
		}
		if d := os.Getenv("CLACK_BENCH_WARMDIR"); d != "" {
			frozen = d
		}
		copyTree(tb, dir, frozen)
		warmDir = frozen
	})
	dst := tb.TempDir()
	copyTree(tb, warmDir, dst)
	return dst
}

// copyTree copies the warm config dir. The warm session's background
// goroutines may still be renaming *.tmp files into place: skip those and
// anything that vanishes mid-walk.
func copyTree(tb testing.TB, src, dst string) {
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		rel, _ := filepath.Rel(src, p)
		t := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(t, 0o700)
		}
		if strings.HasSuffix(p, ".tmp") {
			return nil
		}
		b, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			return nil
		} else if err != nil {
			return err
		}
		return os.WriteFile(t, b, 0o600)
	})
	if err != nil {
		tb.Fatal(err)
	}
}

func startEnv(tb testing.TB, fs *fakeslack.Server, home string) *perfEnv {
	e := &perfEnv{tb: tb, fs: fs, home: home, size: image.Pt(1440, 900), now: time.Now(), start: time.Now()}
	images := imgcache.New(fs.Fetch, "", nil)
	e.a = New(Options{ConfigDir: home, Started: e.start, NoRealtime: true}, images) // no first-run live-updates banner shifting the layout
	cache := filepath.Join(home, "cache")
	svc := core.New(slack.New(fs.Creds()), store.New(cache))
	e.a.Attach(svc, cache)
	// what Run does once the window exists
	images.SetInvalidate(func() {})
	e.a.th.Images = images
	if installHooks != nil {
		installHooks(e.a)
	}
	e.a.data.start(e.a)
	if os.Getenv("CLACK_BENCH_GPU") != "" {
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			tb.Logf("no GPU renderer (%v): layout only", err)
		} else {
			e.gpu = w
		}
	}
	tb.Cleanup(e.shutdown)
	return e
}

// shutdown stops the session's background work so it can be collected.
func (e *perfEnv) shutdown() {
	if e.a == nil || e.a.data == nil {
		return
	}
	if e.a.data.cancel != nil {
		e.a.data.cancel()
	}
	if c, ok := any(e.a.data.core).(interface{ Shutdown() }); ok { // core.App.Shutdown (absent in the baseline tree)
		c.Shutdown()
	}
	if e.gpu != nil {
		e.gpu.Release()
		e.gpu = nil
	}
}

// loopProfile writes allocation profiles right before and after a
// benchmark's measured loop (CLACK_BENCH_PROFDIR), so
// "go tool pprof -base before after" shows only per-op allocations.
func loopProfile(b *testing.B) func() {
	dir := os.Getenv("CLACK_BENCH_PROFDIR")
	if dir == "" {
		return func() {}
	}
	name := strings.ReplaceAll(b.Name(), "/", "_")
	write := func(suffix string) {
		f, err := os.Create(filepath.Join(dir, name+suffix))
		if err != nil {
			b.Fatal(err)
		}
		defer f.Close()
		_ = pprof.Lookup("allocs").WriteTo(f, 0)
	}
	write(".loop-before.pprof")
	return func() { write(".loop-after.pprof") }
}

// frame lays out (and with a GPU, renders) one frame the way App.Run does.
func (e *perfEnv) frame() {
	a := e.a
	if a.th.Shaper == nil {
		sh := <-a.shaperCh
		a.th.SetShapers(sh[0], sh[1])
	}
	a.drain()
	e.now = time.Now()
	gtx := e.context()
	before := a.router.Current()
	a.frame(gtx)
	if a.router.Current() != before {
		gtx = e.context()
		a.frame(gtx)
	}
	e.r.Frame(&e.ops)
	if e.gpu != nil {
		if err := e.gpu.Frame(&e.ops); err != nil {
			e.tb.Fatal(err)
		}
	}
}

func (e *perfEnv) context() layout.Context {
	e.ops.Reset()
	return layout.Context{Ops: &e.ops, Now: e.now, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(e.size), Source: e.r.Source()}
}

// until runs frames until cond holds (or fails the benchmark after d).
func (e *perfEnv) until(what string, d time.Duration, cond func() bool) {
	dl := time.Now().Add(d)
	for !cond() {
		if time.Now().After(dl) {
			e.tb.Fatalf("timed out after %v waiting for %s", d, what)
		}
		e.frame()
		time.Sleep(time.Millisecond)
	}
	e.frame()
}

// settle runs frames for d (async loads: history, images, names).
func (e *perfEnv) settle(d time.Duration) {
	for dl := time.Now().Add(d); time.Now().Before(dl); {
		e.frame()
		time.Sleep(2 * time.Millisecond)
	}
}

func (e *perfEnv) waitReady() {
	e.until("core ready", 30*time.Second, func() bool { return !e.a.data.booted.IsZero() })
}

func (e *perfEnv) openChannel(ch string) {
	e.a.Navigate("c/" + ch)
	e.frame()
}

func (e *perfEnv) key(name key.Name, mods key.Modifiers) {
	e.r.Queue(key.Event{Name: name, Modifiers: mods, State: key.Press}, key.Event{Name: name, Modifiers: mods, State: key.Release})
}

// typeText sends s as one input-method edit at the router's selection.
func (e *perfEnv) typeText(s string) {
	st := e.r.EditorState()
	e.r.Queue(key.EditEvent{Range: st.Selection.Range, Text: s})
}

func (e *perfEnv) click(p image.Point) {
	pt := f32.Pt(float32(p.X), float32(p.Y))
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt},
	)
}

func (e *perfEnv) scroll(p image.Point, dy float32) {
	pt := f32.Pt(float32(p.X), float32(p.Y))
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt},
		pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: pt, Scroll: f32.Pt(0, dy)},
	)
}

// listPoint is a point inside the message list of the open channel.
func (e *perfEnv) listPoint() image.Point {
	o := e.a.primaryOrigin
	return image.Pt(o.X+(e.size.X-o.X)/2, o.Y+(e.size.Y-o.Y)/3)
}

// loadedMessages counts the messages core holds for ch.
func (e *perfEnv) loadedMessages(ch string) int {
	if tl := e.a.data.core.S.Timeline(ch); tl != nil {
		return len(tl.Msgs)
	}
	return 0
}

func liveHeapMB() float64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return float64(m.HeapAlloc) / (1 << 20)
}

func rssMB() float64 {
	debug.FreeOSMemory()
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, l := range strings.Split(string(b), "\n") {
		if f := strings.Fields(l); len(f) >= 2 && f[0] == "VmRSS:" {
			kb, _ := strconv.Atoi(f[1])
			return float64(kb) / 1024
		}
	}
	return 0
}

// TestPerfMakeWarmDir writes the warm config dir for later benchmark
// processes (only when CLACK_BENCH_WARMDIR is set).
func TestPerfMakeWarmDir(t *testing.T) {
	if os.Getenv("CLACK_BENCH_WARMDIR") == "" {
		t.Skip("CLACK_BENCH_WARMDIR not set")
	}
	warmHome(t, perfServer(t))
}

// ---- benchmarks ----

// BenchmarkStartup measures a warm start: process-level setup (shaper,
// caches) to the first frame with sidebar rows, and to the frame after core
// is ready. Layout only unless CLACK_BENCH_GPU is set.
func BenchmarkStartup(b *testing.B) {
	perfServer(b)
	warmHome(b, perfFake)
	var side, ready time.Duration
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		home := b.TempDir()
		copyTree(b, warmDir, home)
		runtime.GC()
		b.StartTimer()
		e := startEnv(b, perfFake, home)
		e.until("sidebar rows", 10*time.Second, func() bool { return e.a.sidebar.rowCount > 0 })
		side += time.Since(e.start)
		e.waitReady()
		ready += time.Since(e.start)
		b.StopTimer()
		e.shutdown()
		b.StartTimer()
	}
	b.ReportMetric(float64(side.Milliseconds())/float64(b.N), "sidebar-ms")
	b.ReportMetric(float64(ready.Milliseconds())/float64(b.N), "ready-ms")
}

// BenchmarkBootCold is the first start ever (no caches): core fetches
// everything, emoji included.
func BenchmarkBootCold(b *testing.B) {
	for i := 0; i < b.N; i++ {
		e := newPerfEnv(b, false)
		e.waitReady()
		b.StopTimer()
		e.shutdown()
		b.StartTimer()
	}
}

// BenchmarkChannelSwitch alternates between cached channels; one op is the
// navigation plus the frame that shows the new channel.
func BenchmarkChannelSwitch(b *testing.B) {
	e := newPerfEnv(b, true)
	e.waitReady()
	ids := e.fs.ChannelIDs()[1:9]
	for _, ch := range ids { // fill the history caches and views
		e.openChannel(ch)
		e.until("history "+ch, 10*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
		e.settle(50 * time.Millisecond)
	}
	b.ReportAllocs()
	done := loopProfile(b)
	defer done()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.openChannel(ids[i%len(ids)])
		e.frame()
	}
}

// BenchmarkScroll500 scrolls a channel with 500 loaded messages (a fifth of
// them images) back and forth; one op is one scroll event plus its frame.
func BenchmarkScroll500(b *testing.B) {
	e := newPerfEnv(b, true)
	e.waitReady()
	ch := e.fs.ChannelIDs()[2]
	e.openChannel(ch)
	e.until("history", 10*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
	p := e.listPoint()
	dl := time.Now().Add(60 * time.Second)
	for e.loadedMessages(ch) < 500 && time.Now().Before(dl) { // infinite scroll to 500
		e.scroll(p, -2000)
		e.frame()
		time.Sleep(5 * time.Millisecond)
	}
	if n := e.loadedMessages(ch); n < 500 {
		b.Logf("only %d messages loaded", n)
	}
	// one pass down and up so every image is decoded once
	for i := 0; i < 200; i++ {
		e.scroll(p, 600)
		e.frame()
	}
	e.settle(500 * time.Millisecond)
	b.ReportAllocs()
	done := loopProfile(b)
	defer done()
	b.ResetTimer()
	dir := float32(240)
	for i := 0; i < b.N; i++ {
		if i%100 == 0 {
			dir = -dir
		}
		e.scroll(p, dir)
		e.frame()
	}
	b.StopTimer()
	b.ReportMetric(float64(e.loadedMessages(ch)), "msgs")
	b.ReportMetric(liveHeapMB(), "heap-MB")
}

// BenchmarkEmojiPickerOpen opens the picker (65k custom emoji) and lays out
// its first frame; one op is open + frame + close.
func BenchmarkEmojiPickerOpen(b *testing.B) {
	e := newPerfEnv(b, true)
	e.waitReady()
	e.openChannel(e.fs.ChannelIDs()[1])
	e.settle(100 * time.Millisecond)
	t0 := time.Now()
	e.a.OpenEmojiPicker(image.Pt(700, 700), func(string) {})
	e.frame()
	first := time.Since(t0)
	e.key(key.NameEscape, 0)
	e.frame()
	e.settle(100 * time.Millisecond)
	b.ReportAllocs()
	done := loopProfile(b)
	defer done()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.a.OpenEmojiPicker(image.Pt(700, 700), func(string) {})
		e.frame()
		e.key(key.NameEscape, 0)
		e.frame()
	}
	b.ReportMetric(float64(first.Microseconds())/1000, "first-open-ms")
}

// BenchmarkEmojiSearch types into the picker's search field; one op is one
// keystroke and its frame (queries cycle through "p", "pa", "par", …).
func BenchmarkEmojiSearch(b *testing.B) {
	e := newPerfEnv(b, true)
	e.waitReady()
	e.openChannel(e.fs.ChannelIDs()[1])
	e.settle(100 * time.Millisecond)
	e.a.OpenEmojiPicker(image.Pt(700, 700), func(string) {})
	e.settle(100 * time.Millisecond)
	word := "parrot"
	b.ReportAllocs()
	done := loopProfile(b)
	defer done()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%len(word) == 0 && i > 0 {
			for j := 0; j < len(word); j++ {
				e.key(key.NameDeleteBackward, 0)
			}
			e.frame()
		}
		e.typeText(word[i%len(word) : i%len(word)+1])
		e.frame()
	}
}

// BenchmarkQuickSwitcher opens Ctrl+K and types a query; one op is open,
// four keystrokes with their frames, and Escape.
func BenchmarkQuickSwitcher(b *testing.B) {
	e := newPerfEnv(b, true)
	e.waitReady()
	e.openChannel(e.fs.ChannelIDs()[1])
	e.settle(100 * time.Millisecond)
	b.ReportAllocs()
	done := loopProfile(b)
	defer done()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.key("K", key.ModShortcut)
		e.frame()
		e.frame()
		for _, c := range "hack" {
			e.typeText(string(c))
			e.frame()
		}
		e.key(key.NameEscape, 0)
		e.frame()
	}
}

// BenchmarkTypingFrame types one character into the focused composer; one
// op is the edit and its frame. Allocations per op are the number to watch.
func BenchmarkTypingFrame(b *testing.B) {
	e := newPerfEnv(b, true)
	e.waitReady()
	ch := e.fs.ChannelIDs()[1]
	e.openChannel(ch)
	e.until("history", 10*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
	e.settle(200 * time.Millisecond)
	c := e.a.convos[ch+"/"]
	if c == nil || c.compRect.Empty() {
		b.Fatal("no composer laid out")
	}
	e.click(c.compRect.Min.Add(image.Pt(40, c.compRect.Dy()/2)))
	e.frame()
	e.frame()
	b.ReportAllocs()
	done := loopProfile(b)
	defer done()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%200 == 199 { // keep the draft short: select all, delete
			e.key("A", key.ModShortcut)
			e.key(key.NameDeleteBackward, 0)
			e.frame()
		}
		e.typeText(string(rune('a' + i%26)))
		e.frame()
	}
}

// BenchmarkIdleFrame is a frame with nothing changed (a redraw the app did
// not need). Ideally ~zero work; the wakeups metric counts frames that
// asked for another frame (animations, timers).
func BenchmarkIdleFrame(b *testing.B) {
	e := newPerfEnv(b, true)
	e.waitReady()
	ch := e.fs.ChannelIDs()[1]
	e.openChannel(ch)
	e.until("history", 10*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
	e.settle(500 * time.Millisecond)
	wake := 0
	b.ReportAllocs()
	done := loopProfile(b)
	defer done()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.frame()
		if t, ok := e.r.WakeupTime(); ok && t.Sub(e.now) < time.Second {
			wake++
		}
	}
	b.ReportMetric(float64(wake)/float64(b.N), "wakeups/op")
}

// BenchmarkSessionMemory is a normal session: warm start, three channels
// opened and scrolled, the emoji picker and the switcher used once. It
// reports the live Go heap and the process RSS afterwards (one op).
func BenchmarkSessionMemory(b *testing.B) {
	for i := 0; i < b.N; i++ {
		e := newPerfEnv(b, true)
		e.waitReady()
		for _, ch := range e.fs.ChannelIDs()[1:4] {
			e.openChannel(ch)
			e.until("history", 10*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
			for j := 0; j < 30; j++ {
				e.scroll(e.listPoint(), -300)
				e.frame()
			}
			e.settle(200 * time.Millisecond)
		}
		e.a.OpenEmojiPicker(image.Pt(700, 700), func(string) {})
		e.settle(200 * time.Millisecond)
		e.key(key.NameEscape, 0)
		e.key("K", key.ModShortcut)
		e.settle(100 * time.Millisecond)
		e.key(key.NameEscape, 0)
		e.settle(2 * time.Second) // idle
		b.StopTimer()
		b.ReportMetric(liveHeapMB(), "heap-MB")
		b.ReportMetric(rssMB(), "rss-MB")
		// long idle: one frame a second (a blinking caret's worth) while
		// the background sweeps run
		for t0 := time.Now(); time.Since(t0) < 75*time.Second; {
			e.frame()
			time.Sleep(time.Second)
		}
		b.ReportMetric(liveHeapMB(), "idle-heap-MB")
		b.ReportMetric(rssMB(), "idle-rss-MB")
		if p := os.Getenv("CLACK_BENCH_HEAPPROFILE"); p != "" {
			writeHeapProfile(b, p)
		}
		e.shutdown()
		b.StartTimer()
	}
}

func writeHeapProfile(tb testing.TB, path string) {
	f, err := os.Create(path)
	if err != nil {
		tb.Fatal(err)
	}
	defer f.Close()
	runtime.GC()
	if err := pprof.Lookup("heap").WriteTo(f, 0); err != nil {
		tb.Fatal(err)
	}
}

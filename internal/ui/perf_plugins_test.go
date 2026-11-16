package ui

// Scroll frames under a plugin set like Karim's (perf workflow, jobs:
// plugins): the Library's Taut plugins that are on by default plus Slick's
// Message Logger, run unmodified from the published `plugins` release, in
// the whole client over internal/fakeslack (the plugin bench's workspace
// with more channels and the Activity inbox). Together they patch what
// the freeze reports of 2026-11-15 showed the UI goroutine stuck in:
// MessageListItem, MessageWrapper, MessageText, Blocks, the thread pane's
// rows, the gutter, MessageSender, MessagePaneInput and TypingNames, and
// the messages slice (patched views and injected messages).
//
// Like a long session, it first opens 40 channels (the plugin mirror then
// watches them all), then measures, a wheel step a frame at 60 frames a
// second:
//
//	channel       the 500-message #plugin-bench, bottom to top and back
//	channel-live  the same while people type, post, react, edit and delete
//	              in it and post in the other channels (realtime events
//	              through core.Inject)
//	channel-gc    channel-live with the heap near its memory limit, so the
//	              collector runs all the time (cmd/clack's 768 MB limit in
//	              a long Hack Club session)
//	thread-live   a 300-reply thread in the side pane, replies and typing
//	              arriving
//	activity      the Activity page, scrolled, with the same traffic
//
// For each it records every frame's time on the UI goroutine (drain +
// layout; no GPU, so the sleep to the next frame isn't counted), the
// process CPU, allocations and collections. A freeze is one long frame, so
// the report leads with the longest frame and the frames over 50, 100 and
// 250 ms, then the median and p95.
//
//	PLUGIN_BENCH_LIBRARY  the `plugins` release's files (index.json + bundles)
//	PERF_PLUGINS          system:id list; "taut:@default" is every Taut
//	                      plugin the Library turns on by default
//	                      (default "taut:@default,slick:MessageLogger")
//	PERF_PLUGIN_OUT       where the JSON result goes
//	PERF_PLUGIN_PROF      a path prefix: a CPU and an allocation profile per
//	                      pass (<prefix>-<pass>.cpu.pprof, .allocs0/1.pprof
//	                      before and after) and the mutex and block
//	                      profiles over all passes
//	PERF_PLUGIN_RECORDER  n: then n pairs of channel-live passes with the
//	                      execution-trace flight recorder off and on
//	                      (flightrec.go; trees that have it)
//
// Synthetic data only; nothing reaches Slack. It uses nothing newer than
// the plugin bench, so the perf workflow runs this same file on the
// baseline tree for the "before" column.

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache/webptest"
	"github.com/some-du6e/clack/internal/jsrt"
	"github.com/some-du6e/clack/internal/pluginlib"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// perfProfPrefix is PERF_PLUGIN_PROF while the passes run.
var perfProfPrefix string

// perfPluginSize is the bench's workspace with the channels of a longer
// session and the Activity inbox.
var perfPluginSize = func() fakeslack.Size {
	s := benchSize
	s.Channels, s.Views = 60, true
	return s
}()

// perfPass is one measured pass.
type perfPass struct {
	Name     string  `json:"name"`
	Frames   int     `json:"frames"`
	MedianMS float64 `json:"median_ms"`
	P95MS    float64 `json:"p95_ms"`
	P99MS    float64 `json:"p99_ms"`
	MaxMS    float64 `json:"max_ms"`
	Over50   int     `json:"over_50ms"`
	Over100  int     `json:"over_100ms"`
	Over250  int     `json:"over_250ms"`
	TotalMS  float64 `json:"total_ms"` // sum of the frames
	CPUMS    float64 `json:"cpu_ms"`   // process CPU over the pass
	Allocs   float64 `json:"allocs"`   // allocations per frame (whole process)
	AllocKB  float64 `json:"alloc_kb"` // KB allocated per frame (whole process)
	GCs      int     `json:"gcs"`      // collections during the pass
	GCPause  float64 `json:"gc_pause"` // ms of stop-the-world pauses
	Events   int     `json:"events"`   // realtime events injected
	// Trees are the host trees JS delivered during the 300 frames that
	// changed their root's tree; TreesSame the ones identical to it (a
	// React commit with the same output)
	Trees     int   `json:"trees"`
	TreesSame int   `json:"trees_same"`
	Renders   int64 `json:"renders"` // Go→JS Render/RenderIn calls
	// the idle passes (perfIdle): how long they ran, the animation frames
	// the image cache's players moved on to meanwhile (every size of every
	// animated image counts), and the players playing at the end
	Seconds    float64        `json:"seconds,omitempty"`
	AnimFrames int            `json:"anim_frames,omitempty"`
	Players    int            `json:"players,omitempty"`
	Quiet      int            `json:"quiet_frames,omitempty"` // of Frames: the last frame's ops again, no layout (quiet.go)
	Why        map[string]int `json:"not_quiet,omitempty"`    // the frames laid out, by why they weren't quiet
	// Slow are the frames over 40 ms: what the frame goroutine was doing
	// 40 and 120 ms in (function names), with what landed meanwhile
	Slow []perfSlow `json:"slow,omitempty"`
	// TraceMB and TraceMS: with the flight recorder on, the trace of its
	// window written after the pass, as a slow frame writes it
	TraceMB float64 `json:"trace_mb,omitempty"`
	TraceMS float64 `json:"trace_ms,omitempty"`
}

// perfRecorder starts the execution-trace flight recorder (flightrec.go)
// and returns its stop and a function writing its window (bytes, time).
// perf_flightrec_test.go sets it; the base tree (which runs this file) has
// no recorder, so there it stays nil and the recorder passes are skipped.
var perfRecorder func() (stop func(), dump func() (int, time.Duration), err error)

type perfSlow struct {
	MS    float64  `json:"ms"`
	Trees int      `json:"trees"` // host trees delivered during the frame
	GCs   uint32   `json:"gcs"`   // collections that ended during it
	Stack []string `json:"stack"`
}

// perfGCs counts finished collections without stopping the world (as
// ReadMemStats does: an extra stop every frame would be what's measured).
func perfGCs() uint64 {
	s := []metrics.Sample{{Name: "/gc/cycles/total:gc-cycles"}}
	metrics.Read(s)
	if s[0].Value.Kind() != metrics.KindUint64 {
		return 0
	}
	return s[0].Value.Uint64()
}

// perfOffHeapWasmMB is the plugin engines' wasm memory that lives outside
// the Go heap (jsrt.Snapshot's WasmMemory where WasmReserved is set). The
// base tree runs this file too, and its Snapshot may have neither field:
// then it's 0 (the memory is a Go slice, in the live heap).
func perfOffHeapWasmMB() float64 {
	var n uint64
	for _, s := range jsrt.Snapshots() {
		v := reflect.ValueOf(s)
		mem, res := v.FieldByName("WasmMemory"), v.FieldByName("WasmReserved")
		if mem.IsValid() && res.IsValid() && res.Uint() > 0 {
			n += mem.Uint()
		}
	}
	return float64(n) / (1 << 20)
}

// perfGoID is the calling goroutine's id ("goroutine 7 [running]:").
func perfGoID() string {
	buf := make([]byte, 64)
	f := strings.Fields(string(buf[:runtime.Stack(buf, false)]))
	if len(f) < 2 {
		return ""
	}
	return f[1]
}

// perfStack is goroutine id's stack, function names only (no arguments,
// no message text), and the state it's in.
func perfStack(id string) []string {
	buf := make([]byte, 4<<20)
	buf = buf[:runtime.Stack(buf, true)]
	for _, g := range strings.Split(string(buf), "\n\n") {
		if !strings.HasPrefix(g, "goroutine "+id+" [") {
			continue
		}
		var out []string
		for i, l := range strings.Split(g, "\n") {
			if i == 0 {
				out = append(out, l) // its state: running, runnable, a lock…
				continue
			}
			if strings.HasPrefix(l, "\t") {
				continue
			}
			if j := strings.LastIndex(l, "("); j > 0 {
				l = l[:j]
			}
			l = strings.TrimPrefix(l, "github.com/some-du6e/clack/internal/")
			out = append(out, l)
			if len(out) == 45 {
				break
			}
		}
		return out
	}
	return nil
}

type perfPluginResult struct {
	Plugins  []string `json:"plugins"`
	Failed   []string `json:"failed,omitempty"`
	Patched  []string `json:"patched"`
	Messages int      `json:"messages"`
	Replies  int      `json:"replies"`
	Visited  int      `json:"visited"`
	HeapMB   float64  `json:"heap_mb"` // live heap after setup
	// WasmMB is the plugin engines' wasm memory outside the Go heap (jsrt's
	// reservation; 0 where it is a Go slice, inside HeapMB)
	WasmMB float64    `json:"wasm_mb,omitempty"`
	Passes []perfPass `json:"passes"`
}

func TestPerfPluginScroll(t *testing.T) {
	dir := os.Getenv("PLUGIN_BENCH_LIBRARY")
	if dir == "" {
		t.Skip("PLUGIN_BENCH_LIBRARY not set: the perf workflow (jobs: plugins) downloads the Library and runs this")
	}
	srv := httptest.NewServer(http.StripPrefix("/rel/", http.FileServer(http.Dir(dir))))
	t.Cleanup(srv.Close)
	t.Setenv("CLACK_PLUGIN_LIBRARY", srv.URL+"/rel/index.json")
	b, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ix pluginlib.Index
	if err := json.Unmarshal(b, &ix); err != nil {
		t.Fatal(err)
	}
	want := os.Getenv("PERF_PLUGINS")
	if want == "" {
		want = "taut:@default,slick:MessageLogger"
	}
	var set []pluginlib.Entry
	for _, w := range strings.Split(want, ",") {
		sys, id, _ := strings.Cut(strings.TrimSpace(w), ":")
		for _, e := range ix.For(sys) {
			if id == "@default" && e.DefaultEnabled || e.ID == id {
				set = append(set, e)
			}
		}
	}
	if len(set) == 0 {
		t.Fatalf("PERF_PLUGINS %q matches nothing in the Library", want)
	}

	fs, err := fakeslack.Start(perfPluginSize)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	e := benchStart(t, fs)
	defer func() {
		if e.a.data != nil {
			e.a.data.stopTaut()
		}
		e.shutdown()
	}()
	e.waitReady()
	e.until("Taut and Slick", 60*time.Second, func() bool { return e.a.Taut() != nil && e.a.Slick() != nil })
	e.tt = e.a.Taut()
	sl := e.a.Slick()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// the runtimes up first (a no-op plugin in each), as in the bench
	if _, err := e.tt.Import(ctx, synthTaut("BenchWarm", "Bench warm-up", "no-op", "other", false), ""); err != nil {
		t.Fatal(err)
	}
	if err := e.tt.SetEnabled(ctx, "BenchWarm", true); err != nil {
		t.Fatal(err)
	}
	if _, err := sl.Import(ctx, synthSlick("BenchWarmSlick", "Bench warm-up", "no-op", "app")); err != nil {
		t.Fatal(err)
	}
	if err := sl.SetEnabled(ctx, "BenchWarmSlick", true); err != nil {
		t.Fatal(err)
	}
	e.settle(300 * time.Millisecond)
	slackc.SetBridge(countingBridge{e.tt.Bridge(), &e.calls})

	res := perfPluginResult{}
	lib := e.a.pluginLib()
	if lib == nil {
		t.Fatal("no plugin library")
	}
	if _, err := lib.Index(ctx, true); err != nil {
		t.Fatal(err)
	}
	page := newLibraryPage(e.a, nil)
	for _, entry := range set {
		name := entry.System + ":" + entry.ID
		if err := perfInstall(ctx, e, page, entry); err != nil {
			t.Logf("%s: %v", name, err)
			res.Failed = append(res.Failed, name)
			continue
		}
		res.Plugins = append(res.Plugins, name)
	}
	e.settle(1500 * time.Millisecond)
	res.Patched = slackc.PatchedNames()
	t.Logf("running %d plugins (%s), failed %v; patched: %s", len(res.Plugins), strings.Join(res.Plugins, " "), res.Failed, strings.Join(res.Patched, " "))

	// a long session: many channels opened (the mirror watches each)
	chans := fs.ChannelIDs()
	var others []string
	for _, ch := range chans {
		if ch == fakeslack.BenchChannelID || len(others) >= 40 {
			continue
		}
		e.openChannel(ch)
		e.settle(120 * time.Millisecond)
		others = append(others, ch)
	}
	res.Visited = len(others)

	// a long thread in another channel, served by conversations.replies
	thCh := others[0]
	thRoot, _ := fs.PostQuiet(thCh, e.c.Authors[0], "the long thread for the scroll pass")
	const replies = 300
	for i := 0; i < replies; i++ {
		text := fmt.Sprintf("reply %d: %s", i, strings.Repeat("words that wrap ", 1+i%9))
		fs.PostReplyQuiet(thCh, thRoot, e.c.Authors[i%len(e.c.Authors)], text)
	}
	res.Replies = replies

	e.openBench()
	e.loadAll()
	res.Messages = e.loadedMessages(fakeslack.BenchChannelID)
	runtime.GC()
	res.HeapMB, res.WasmMB = liveHeapMB(), perfOffHeapWasmMB()
	t.Logf("%d messages loaded, %d channels visited, live heap %.0f MB, wasm memory outside it %.0f MB", res.Messages, res.Visited, res.HeapMB, res.WasmMB)

	prof := os.Getenv("PERF_PLUGIN_PROF")
	if prof != "" {
		runtime.SetMutexProfileFraction(5)
		runtime.SetBlockProfileRate(20000) // one sample per 20 µs blocked
		defer func() {
			runtime.SetMutexProfileFraction(0)
			runtime.SetBlockProfileRate(0)
		}()
	}
	perfProfPrefix = prof
	// slow-frame samples show the runtime's frames too (a GC assist, a
	// stop-the-world wait), not just the Go code that was running
	debug.SetTraceback("system")
	traffic := func(ch, thread string) *perfTraffic {
		return &perfTraffic{e: e, ch: ch, thread: thread, others: others[1:]}
	}
	res.Passes = append(res.Passes, e.perfScroll("channel", e.listPoint, nil))
	res.Passes = append(res.Passes, e.perfScroll("channel-live", e.listPoint, traffic(fakeslack.BenchChannelID, "")))
	func() {
		// the heap a quarter above what's live: the collector runs all the
		// time, as near cmd/clack's limit. What's live includes the plugin
		// engine's wasm memory wherever it is: in the Go heap (a Go
		// slice) the quarter above counted it, so where it lives outside
		// (jsrt's reservation) the limit keeps that headroom
		runtime.GC()
		wasm := perfOffHeapWasmMB()
		lim := int64(((liveHeapMB()+wasm)*1.25-wasm)*(1<<20)) + 32<<20
		old := debug.SetMemoryLimit(lim)
		defer debug.SetMemoryLimit(old)
		res.Passes = append(res.Passes, e.perfScroll("channel-gc", e.listPoint, traffic(fakeslack.BenchChannelID, "")))
	}()

	e.a.OpenThread(thCh, thRoot)
	e.until("the thread pane", 20*time.Second, func() bool { return e.a.secondary.kind == secThread })
	e.settle(1500 * time.Millisecond)
	thPoint := func() image.Point {
		o := e.a.secondaryOrigin
		return image.Pt(o.X+(e.size.X-o.X)/2, o.Y+(e.size.Y-o.Y)/3)
	}
	for i := 0; i < 60; i++ { // to the thread's end first, like toBottom
		e.scroll(thPoint(), 4000)
		e.frame()
	}
	e.settle(300 * time.Millisecond)
	res.Passes = append(res.Passes, e.perfScroll("thread-live", thPoint, traffic(thCh, thRoot)))
	e.a.CloseSecondary()

	e.a.Navigate("activity")
	e.settle(2 * time.Second)
	res.Passes = append(res.Passes, e.perfScroll("activity", e.listPoint, traffic(fakeslack.BenchChannelID, "")))

	// what the flight recorder costs: PERF_PLUGIN_RECORDER pairs of the
	// channel-live pass with it off and on, in turns (off-on, on-off, …),
	// without the CPU profiler (the tracer records its samples too)
	if pairs, _ := strconv.Atoi(os.Getenv("PERF_PLUGIN_RECORDER")); pairs > 0 && perfRecorder != nil {
		e.openBench()
		profPrefix := perfProfPrefix
		perfProfPrefix = ""
		for i := 0; i < pairs; i++ {
			order := []bool{false, true}
			if i%2 == 1 {
				order = []bool{true, false}
			}
			for _, on := range order {
				name := "channel-live, recorder off"
				var stop func()
				var dump func() (int, time.Duration)
				if on {
					var err error
					if stop, dump, err = perfRecorder(); err != nil {
						t.Fatalf("the flight recorder: %v", err)
					}
					name = "channel-live, recorder on"
				}
				e.settle(2 * time.Second) // the recorder holds a few generations first
				p := e.perfScroll(name, e.listPoint, traffic(fakeslack.BenchChannelID, ""))
				if on {
					n, took := dump()
					p.TraceMB, p.TraceMS = float64(n)/1e6, float64(took.Microseconds())/1000
					stop()
				}
				res.Passes = append(res.Passes, p)
			}
		}
		perfProfPrefix = profPrefix
	}

	// an animated custom emoji on screen (perfAnimEmoji): the channel left
	// alone, frames drawn only when something asks for one, as the window
	// draws them; first without the emoji (the baseline: nothing asks),
	// then with it in new messages and reactions, then the same at 60
	// frames a second (something else drawing meanwhile)
	e.openBench()
	e.toBottom()
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(-100, -100)}) // off the window
	e.settle(2 * time.Second)
	res.Passes = append(res.Passes, e.perfIdle("idle", 5*time.Second, false))
	if e.perfAnimEmoji(t) {
		res.Passes = append(res.Passes, e.perfIdle("emoji", 5*time.Second, false))
		res.Passes = append(res.Passes, e.perfIdle("emoji-60hz", 5*time.Second, true))
	}

	if prof != "" {
		for _, k := range []string{"mutex", "block"} {
			if f, err := os.Create(prof + "." + k + ".pprof"); err == nil {
				_ = pprof.Lookup(k).WriteTo(f, 0)
				f.Close()
			}
		}
	}
	for _, p := range res.Passes {
		for _, sl := range p.Slow {
			t.Logf("slow frame in %s: %.1f ms, %d trees delivered meanwhile, %d collections; the frame goroutine 40 ms in:\n\t%s", p.Name, sl.MS, sl.Trees, sl.GCs, strings.Join(sl.Stack, "\n\t"))
		}
	}
	for _, p := range res.Passes {
		t.Logf("pass %-12s frames %d: max %.1f ms, >50ms %d, >100ms %d, >250ms %d, median %.2f ms, p95 %.2f ms, p99 %.2f ms, total %.0f ms, cpu %.0f ms, %.0f allocs %.0f KB/frame, %d GCs %.1f ms paused, events %d, trees changed %d (identical %d), renders %d",
			p.Name, p.Frames, p.MaxMS, p.Over50, p.Over100, p.Over250, p.MedianMS, p.P95MS, p.P99MS, p.TotalMS, p.CPUMS, p.Allocs, p.AllocKB, p.GCs, p.GCPause, p.Events, p.Trees, p.TreesSame, p.Renders)
	}
	for _, p := range res.Passes {
		if p.Seconds > 0 {
			t.Logf("idle pass %-10s %d frames in %.1f s (%.1f ms of frames a second, %d quiet; laid out: %v), %d animation frames shown, %d players at the end", p.Name, p.Frames, p.Seconds, p.TotalMS/p.Seconds, p.Quiet, p.Why, p.AnimFrames, p.Players)
		}
	}
	if out := os.Getenv("PERF_PLUGIN_OUT"); out != "" {
		b, _ := json.MarshalIndent(res, "", "  ")
		if err := os.WriteFile(out, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// perfInstall installs and starts one Library plugin the way the
// Library's Install button does.
func perfInstall(ctx context.Context, e *benchEnv, page *libraryPage, entry pluginlib.Entry) error {
	code, main, err := e.a.pluginLib().Download(ctx, entry)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	h, ok := page.host(entry.System)
	if !ok {
		return fmt.Errorf("no %s host", entry.System)
	}
	if err := h.install(ctx, entry, code, main, false); err != nil {
		return fmt.Errorf("install: %w", err)
	}
	if entry.System == pluginlib.Taut {
		err = e.tt.SetEnabled(ctx, entry.ID, true)
	} else {
		err = e.a.Slick().SetEnabled(ctx, entry.ID, true)
	}
	if err != nil {
		return fmt.Errorf("enable: %w", err)
	}
	dl := time.Now().Add(10 * time.Second)
	for {
		running, why := false, ""
		if entry.System == pluginlib.Taut {
			for _, c := range e.tt.Settings().Categories {
				for _, p := range c.Plugins {
					if p.ID == entry.ID {
						running, why = p.Running, p.Error
					}
				}
			}
		} else {
			for _, p := range e.a.Slick().Settings().Plugins {
				if p.ID == entry.ID {
					running, why = p.Running, p.StartError
				}
			}
		}
		if running {
			return nil
		}
		if why != "" {
			return fmt.Errorf("not running: %s", why)
		}
		if time.Now().After(dl) {
			return fmt.Errorf("didn't start in 10 s")
		}
		e.frame()
		time.Sleep(5 * time.Millisecond)
	}
}

// perfScroll scrolls the list under at() up 150 wheel steps and down 150,
// one a frame at 60 frames a second, timing each frame; with traffic,
// realtime events arrive meanwhile.
func (e *benchEnv) perfScroll(name string, at func() image.Point, traffic *perfTraffic) perfPass {
	runtime.GC()
	treeCounts := func() (all, same int) {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.deliveries, e.same
	}
	trees0, same0 := treeCounts()
	calls0 := e.calls.snapshot()
	if perfProfPrefix != "" {
		writeProf := func(suffix string) {
			runtime.GC() // the allocs profile is as of the last collection
			if f, err := os.Create(perfProfPrefix + "-" + name + suffix); err == nil {
				_ = pprof.Lookup("allocs").WriteTo(f, 0)
				f.Close()
			}
		}
		writeProf(".allocs0.pprof")
		defer writeProf(".allocs1.pprof")
		if f, err := os.Create(perfProfPrefix + "-" + name + ".cpu.pprof"); err == nil {
			if pprof.StartCPUProfile(f) == nil {
				defer func() {
					pprof.StopCPUProfile()
					f.Close()
				}()
			} else {
				f.Close()
			}
		}
	}
	var ms0, ms1 runtime.MemStats
	runtime.ReadMemStats(&ms0)
	c0 := procCPU()
	if traffic != nil {
		traffic.start()
	}
	var ds []float64
	var slow []perfSlow
	me := perfGoID()
	step := func(dy float32) {
		e.scroll(at(), dy)
		gc0, tr0 := perfGCs(), e.delivered()
		done := make(chan struct{})
		var sample []string
		sampled := make(chan struct{})
		go func() {
			defer close(sampled)
			if len(slow) >= 6 {
				return
			}
			for _, at := range []time.Duration{40 * time.Millisecond, 80 * time.Millisecond} {
				select {
				case <-done:
					return
				case <-time.After(at):
				}
				if sample != nil {
					sample = append(sample, "-- 120 ms in:")
				}
				sample = append(sample, perfStack(me)...)
			}
		}()
		t0 := time.Now()
		e.frame()
		d := time.Since(t0)
		close(done)
		<-sampled
		ds = append(ds, float64(d.Microseconds())/1000)
		if d > 40*time.Millisecond && len(slow) < 6 {
			slow = append(slow, perfSlow{MS: float64(d.Microseconds()) / 1000, Trees: e.delivered() - tr0, GCs: uint32(perfGCs() - gc0), Stack: sample})
		}
		if d < 16*time.Millisecond {
			time.Sleep(16*time.Millisecond - d) // the next vsync
		}
	}
	for i := 0; i < 150; i++ {
		step(-260)
	}
	for i := 0; i < 150; i++ {
		step(260)
	}
	events := 0
	if traffic != nil {
		events = traffic.stop()
	}
	trees1, same1 := treeCounts()
	runtime.ReadMemStats(&ms1)
	e.settle(400 * time.Millisecond) // the JS renders of rows that scrolled in
	cpu := procCPU() - c0
	calls := e.calls.snapshot()
	n := float64(len(ds))
	p := perfPass{Name: name, Frames: len(ds), CPUMS: float64(cpu.Microseconds()) / 1000, Events: events,
		Trees: trees1 - trees0 - (same1 - same0), TreesSame: same1 - same0, Renders: calls[0] - calls0[0] + calls[1] - calls0[1],
		Allocs: float64(ms1.Mallocs-ms0.Mallocs) / n, AllocKB: float64(ms1.TotalAlloc-ms0.TotalAlloc) / 1024 / n,
		GCs: int(ms1.NumGC - ms0.NumGC), GCPause: float64(ms1.PauseTotalNs-ms0.PauseTotalNs) / 1e6, Slow: slow}
	for _, d := range ds {
		p.TotalMS += d
		switch {
		case d > 250:
			p.Over250++
			fallthrough
		case d > 100:
			p.Over100++
			fallthrough
		case d > 50:
			p.Over50++
		}
	}
	sort.Float64s(ds)
	p.MedianMS, p.P95MS, p.P99MS, p.MaxMS = ds[len(ds)/2], ds[len(ds)*95/100], ds[len(ds)*99/100], ds[len(ds)-1]
	p.Allocs, p.AllocKB = math.Round(p.Allocs), math.Round(p.AllocKB)
	return p
}

// perfTraffic is a busy workspace: in the open conversation, someone
// types every 150 ms, a message (or thread reply) every 700 ms, a
// reaction every 300 ms, an edit every 1.5 s and a delete every 2.5 s
// (Message Logger keeps deleted messages: injected ones); in the other
// channels a message every 250 ms. Each live message mentions someone,
// and every third shares a file someone else owns. A large workspace's
// profile changes come too: a status (or huddle) change every 50 ms,
// through the people the messages name, and a display name change every
// 4 s. As realtime events through core's handler, from their own
// goroutine like the socket's.
type perfTraffic struct {
	e          *benchEnv
	ch, thread string
	others     []string

	quit   chan struct{}
	done   sync.WaitGroup
	n      atomic.Int64
	status int
}

// profile is a user_change for member id as the socket brings it: the
// stored profile with a new status, and with rename its display name
// changed too (a suffix added or taken off).
func (p *perfTraffic) profile(id string, rename bool) {
	u, ok := p.e.a.data.core.S.RawUser(id)
	if !ok || u == nil {
		return
	}
	c := *u
	p.status++
	c.Profile.StatusText, c.Profile.StatusEmoji = fmt.Sprintf("in a meeting (%d)", p.status), ":spiral_calendar_pad:"
	if rename {
		if n, cut := strings.CutSuffix(c.Profile.DisplayName, " (away)"); cut {
			c.Profile.DisplayName = n
		} else {
			c.Profile.DisplayName += " (away)"
		}
	}
	raw, _ := json.Marshal(map[string]any{"type": "user_change", "user": c})
	p.e.a.data.core.Inject(slack.Event{Type: "user_change", Raw: raw})
	p.n.Add(1)
}

func (p *perfTraffic) start() {
	p.quit = make(chan struct{})
	p.done.Add(1)
	go p.run()
}

func (p *perfTraffic) stop() int {
	close(p.quit)
	p.done.Wait()
	return int(p.n.Load())
}

func (p *perfTraffic) inject(ch, typ, subtype string, raw map[string]any) {
	raw["type"] = typ
	if subtype != "" {
		raw["subtype"] = subtype
	}
	raw["channel"] = ch
	b, _ := json.Marshal(raw)
	chJSON, _ := json.Marshal(ch)
	p.e.a.data.core.Inject(slack.Event{Type: typ, Subtype: subtype, Channel: chJSON, Raw: b})
	p.n.Add(1)
}

func liveTS() string {
	now := time.Now()
	return fmt.Sprintf("%d.%06d", now.Unix(), now.Nanosecond()/1000)
}

func (p *perfTraffic) run() {
	defer p.done.Done()
	authors := p.e.c.Authors
	// the messages to react to, edit and delete: what core holds now
	var targets []string
	if p.thread != "" {
		if msgs, ok := p.e.a.data.core.S.Thread(p.ch, p.thread); ok {
			for _, m := range msgs {
				targets = append(targets, m.TS)
			}
		}
	} else if tl := p.e.a.data.core.S.Timeline(p.ch); tl != nil {
		for _, m := range tl.Msgs {
			targets = append(targets, m.TS)
		}
	}
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	start := time.Now()
	var lastType, lastMsg, lastReact, lastEdit, lastDel, lastOther, lastStatus, lastRename time.Duration
	seq, msgs := 0, 0
	for {
		select {
		case <-p.quit:
			return
		case <-tick.C:
		}
		el := time.Since(start)
		u := authors[seq%len(authors)]
		seq++
		if el-lastType >= 150*time.Millisecond {
			lastType = el
			ev := map[string]any{"user": u}
			if p.thread != "" {
				ev["thread_ts"] = p.thread
			}
			p.inject(p.ch, "user_typing", "", ev)
		}
		if el-lastMsg >= 700*time.Millisecond {
			lastMsg = el
			ts := liveTS()
			msgs++
			who := authors[(seq*5+1)%len(authors)]
			text := "live message " + ts + " for "
			ev := map[string]any{"user": u, "text": text + "<@" + who + ">", "ts": ts, "event_ts": ts, "client_msg_id": "live-" + ts,
				"blocks": []any{map[string]any{"type": "rich_text", "block_id": "live", "elements": []any{map[string]any{"type": "rich_text_section",
					"elements": []any{map[string]any{"type": "text", "text": text}, map[string]any{"type": "user", "user_id": who}}}}}}}
			if msgs%3 == 0 {
				ev["files"] = []any{map[string]any{"id": fmt.Sprintf("F0LIVE%05d", msgs), "name": "notes.pdf", "title": "Notes", "mimetype": "application/pdf",
					"filetype": "pdf", "pretty_type": "PDF", "mode": "hosted", "user": authors[(seq*3+2)%len(authors)], "size": 2048}}
			}
			if p.thread != "" {
				ev["thread_ts"] = p.thread
			}
			p.inject(p.ch, "message", "", ev)
			targets = append(targets, ts)
		}
		if el-lastStatus >= 50*time.Millisecond {
			lastStatus = el
			p.profile(authors[(seq*7)%len(authors)], false)
		}
		if el-lastRename >= 4*time.Second {
			lastRename = el
			p.profile(authors[(seq*3)%len(authors)], true)
		}
		if len(p.others) > 0 && el-lastOther >= 250*time.Millisecond {
			lastOther = el
			ts := liveTS()
			p.inject(p.others[seq%len(p.others)], "message", "", map[string]any{"user": u, "text": "elsewhere " + ts, "ts": ts, "event_ts": ts})
		}
		if len(targets) == 0 {
			continue
		}
		pick := func(k int) string { return targets[(seq*7+k)%len(targets)] }
		if el-lastReact >= 300*time.Millisecond {
			lastReact = el
			p.inject(p.ch, "reaction_added", "", map[string]any{"user": u, "reaction": "eyes", "item": map[string]any{"type": "message", "channel": p.ch, "ts": pick(0)}})
		}
		if el-lastEdit >= 1500*time.Millisecond {
			lastEdit = el
			ts := pick(1)
			msg := map[string]any{"type": "message", "user": u, "text": "edited " + ts, "ts": ts, "edited": map[string]any{"user": u, "ts": liveTS()}}
			if p.thread != "" {
				msg["thread_ts"] = p.thread
			}
			p.inject(p.ch, "message", "message_changed", map[string]any{"message": msg, "ts": liveTS(), "event_ts": liveTS(), "hidden": true})
		}
		if el-lastDel >= 2500*time.Millisecond {
			lastDel = el
			ts := pick(2)
			p.inject(p.ch, "message", "message_deleted", map[string]any{"deleted_ts": ts, "ts": liveTS(), "event_ts": liveTS(), "hidden": true})
		}
	}
}

// perfExplodes stands in for :explodes:, a custom emoji Slack serves as an
// animated WebP behind a .png URL (synthetic pixels; the real file's
// shape): a 32×32 canvas with alpha, looping forever, 13 frames of 30 to
// 70 ms, the first the whole canvas drawn without blending, the rest small
// rectangles blended over what's there, none disposed. The real file's
// lossy pictures are lossless here (webptest writes VP8L only); a player
// decodes them once either way.
func perfExplodes() []byte {
	type fr struct{ x, y, w, h, ms int }
	rects := []fr{{0, 0, 32, 32, 70}, {14, 14, 2, 2, 30}, {6, 6, 21, 21, 40}, {4, 8, 24, 16, 50},
		{10, 2, 12, 28, 60}, {0, 12, 32, 8, 30}, {8, 8, 16, 16, 40}, {2, 2, 28, 28, 70},
		{12, 0, 8, 32, 50}, {16, 16, 14, 14, 30}, {0, 0, 18, 18, 60}, {6, 20, 20, 10, 40}, {10, 10, 12, 12, 50}}
	frames := make([]webptest.Frame, len(rects))
	for i, r := range rects {
		c := color.NRGBA{R: uint8(255 - 15*i), G: uint8(40 + 16*i), B: uint8(20 * (i % 4)), A: 255}
		if i > 0 && i%3 == 0 {
			c.A = 160 // blended over the canvas
		}
		frames[i] = webptest.Solid(r.x, r.y, r.w, r.h, r.ms, c)
		frames[i].NoBlend = i == 0
	}
	return webptest.Anim(32, 32, 0, frames...)
}

// perfAnimEmoji serves perfExplodes as a custom emoji nothing has drawn yet
// and puts it on screen in the open channel: in four new messages with
// text, in two on its own (jumbomoji), and as a reaction on two of them;
// then draws until a player plays. It reports whether one did.
func (e *benchEnv) perfAnimEmoji(t *testing.T) bool {
	all := e.a.data.core.CustomEmoji()
	names := make([]string, 0, len(all))
	for n, u := range all {
		if strings.HasPrefix(u, e.fs.URL+"img/") && strings.HasSuffix(u, ".png") && !e.a.images.Loaded(u) {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		t.Log("emoji passes skipped: no custom emoji to stand in for :explodes:")
		return false
	}
	sort.Strings(names)
	name := names[len(names)-1]
	path := strings.TrimPrefix(all[name], e.fs.URL+"img/")
	data := perfExplodes()
	var served, asked atomic.Int32
	var seenMu sync.Mutex
	var seen []string // what was asked for under the emoji's name (diagnostics)
	e.fs.SetImages(func(p string) ([]byte, string, bool) {
		if strings.Contains(p, "/"+name+"/") {
			asked.Add(1)
			seenMu.Lock()
			if len(seen) < 4 {
				seen = append(seen, p)
			}
			seenMu.Unlock()
		}
		if p == path {
			served.Add(1)
			return data, "image/png", true // a WebP behind a .png URL, as Slack serves it
		}
		return nil, "", false
	})
	tr := &perfTraffic{e: e}
	ch := fakeslack.BenchChannelID
	var posted []string
	// in the fake's history (a history fetch keeps them) and to the client
	// as the socket would bring them
	for i := 0; i < 6; i++ {
		text := fmt.Sprintf("kaboom :%s: number %d, and again :%s:", name, i, name)
		if i >= 4 {
			text = ":" + name + ":"
		}
		ts, ev := e.fs.PostQuiet(ch, e.c.Authors[i%len(e.c.Authors)], text)
		delete(ev, "type")
		delete(ev, "channel")
		tr.inject(ch, "message", "", ev)
		posted = append(posted, ts)
	}
	for _, ts := range posted[2:4] {
		e.fs.ReactQuiet(ch, ts, name, e.c.Authors[0])
		tr.inject(ch, "reaction_added", "", map[string]any{"user": e.c.Authors[0], "reaction": name, "item": map[string]any{"type": "message", "channel": ch, "ts": ts}})
	}
	e.toBottom()
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(-100, -100)})
	ok := e.wait(15*time.Second, func() bool { return e.a.images.Stats().Players > 0 })
	e.settle(1500 * time.Millisecond) // every size's player, the rows' plugin renders
	s := e.a.images.Stats()
	inTL := 0
	if tl := e.a.data.core.S.Timeline(ch); tl != nil {
		for _, m := range tl.Msgs {
			for _, ts := range posted {
				if m.TS == ts {
					inTL++
				}
			}
		}
	}
	seenMu.Lock()
	t.Logf("emoji :%s: (an animated WebP behind a .png URL): %d players, %d animated images, playing %v; %d of %d messages in the timeline; its image asked for %d times (%d served: %q, want %q); loaded %v, animation at 22/32/44 px %v/%v/%v",
		name, s.Players, s.Animated, ok, inTL, len(posted), asked.Load(), served.Load(), seen, path, e.a.images.Loaded(all[name]),
		e.a.images.IsAnimation(all[name], 22), e.a.images.IsAnimation(all[name], 32), e.a.images.IsAnimation(all[name], 44))
	seenMu.Unlock()
	return ok
}

// perfQuietStart and perfQuietFrame, set by perf_quiet_test.go (trees with
// quiet frames, quiet.go), wire an idle pass's image cache as the window
// does (wake at its animation repaints too) and draw a frame as the
// window's loop does (the last frame's ops again when only an animation
// asked for it; asked, askedAt: what the last frame asked for). nil (the
// base tree): every frame is laid out.
var (
	perfQuietStart func(e *benchEnv, wake func()) (stop func())
	perfQuietFrame func(e *benchEnv, asked bool, askedAt time.Time) (quiet bool)
	perfQuietWhy   func(e *benchEnv) string // why the last frame wasn't quiet
)

// perfIdle leaves the client alone for d, the pointer off the window, and
// draws a frame only when something asks for one, as the window does: the
// image cache's repaint (an animation's next frame), any other Invalidate
// (data, a plugin's tree) or a widget's wake-up (op.InvalidateCmd), at
// most one per 16 ms (the display's). every draws
// one every 16 ms instead, as when something else keeps the window drawing.
// Each frame is timed like perfScroll's.
func (e *benchEnv) perfIdle(name string, d time.Duration, every bool) perfPass {
	wake := make(chan struct{}, 1)
	e.a.images.SetInvalidate(func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	})
	defer e.a.images.SetInvalidate(func() {})
	// and at the window's other repaints (App.Invalidate: data, a plugin's
	// tree, posted work), as the window draws one for each
	prevHook := invalidateHook
	invalidateHook = func(a *App) {
		if prevHook != nil {
			prevHook(a)
		}
		if a == e.a {
			select {
			case wake <- struct{}{}:
			default:
			}
		}
	}
	defer func() { invalidateHook = prevHook }()
	// the pass's Go→JS renders by component, and for a root rendered again
	// what in its props changed (top-level keys, msg's one level down)
	rl := &perfRenderLog{last: map[uint64]map[string]json.RawMessage{}, names: map[string]int{}, keys: map[string]int{}}
	slackc.SetBridge(perfRenderBridge{countingBridge{e.tt.Bridge(), &e.calls}, rl})
	defer slackc.SetBridge(countingBridge{e.tt.Bridge(), &e.calls})
	defer func() {
		if top := rl.top(); top != "" {
			e.t.Logf("pass %s: renders %s", name, top)
		}
	}()
	if perfQuietStart != nil && !every {
		defer perfQuietStart(e, func() {
			select {
			case wake <- struct{}{}:
			default:
			}
		})()
	}
	runtime.GC()
	treeCounts := func() (all, same int) {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.deliveries, e.same
	}
	trees0, same0 := treeCounts()
	calls0 := e.calls.snapshot()
	if perfProfPrefix != "" {
		writeProf := func(suffix string) {
			runtime.GC()
			if f, err := os.Create(perfProfPrefix + "-" + name + suffix); err == nil {
				_ = pprof.Lookup("allocs").WriteTo(f, 0)
				f.Close()
			}
		}
		writeProf(".allocs0.pprof")
		defer writeProf(".allocs1.pprof")
		if f, err := os.Create(perfProfPrefix + "-" + name + ".cpu.pprof"); err == nil {
			if pprof.StartCPUProfile(f) == nil {
				defer func() {
					pprof.StopCPUProfile()
					f.Close()
				}()
			} else {
				f.Close()
			}
		}
	}
	st0 := e.a.images.Stats()
	var ms0, ms1 runtime.MemStats
	runtime.ReadMemStats(&ms0)
	c0 := procCPU()
	var ds []float64
	start := time.Now()
	end := start.Add(d)
	var last time.Time
	var asked bool // what the last frame asked for (the window's FrameEvent.Asked)
	var askedAt time.Time
	quiet := 0
	var why map[string]int // the frames laid out, by why they weren't quiet
	for {
		if !last.IsZero() {
			if every {
				time.Sleep(time.Until(last.Add(16 * time.Millisecond)))
			} else {
				var timer <-chan time.Time
				if asked {
					timer = time.After(max(time.Until(askedAt), 0))
				}
				select {
				case <-wake:
				case <-timer:
				case <-time.After(max(time.Until(end), 0)):
				}
				if since := time.Since(last); since < 16*time.Millisecond { // the next vsync
					time.Sleep(16*time.Millisecond - since)
				}
			}
		}
		if !time.Now().Before(end) {
			break
		}
		last = time.Now()
		if perfQuietFrame != nil && !every {
			if perfQuietFrame(e, asked, askedAt) {
				quiet++
			} else if perfQuietWhy != nil {
				if why == nil {
					why = map[string]int{}
				}
				why[perfQuietWhy(e)]++
			}
		} else {
			e.frame()
		}
		ds = append(ds, float64(time.Since(last).Microseconds())/1000)
		askedAt, asked = e.r.WakeupTime()
	}
	cpu := procCPU() - c0
	runtime.ReadMemStats(&ms1)
	trees1, same1 := treeCounts()
	calls := e.calls.snapshot()
	st1 := e.a.images.Stats()
	n := float64(max(len(ds), 1))
	p := perfPass{Name: name, Frames: len(ds), CPUMS: float64(cpu.Microseconds()) / 1000,
		Trees: trees1 - trees0 - (same1 - same0), TreesSame: same1 - same0, Renders: calls[0] - calls0[0] + calls[1] - calls0[1],
		Allocs: math.Round(float64(ms1.Mallocs-ms0.Mallocs) / n), AllocKB: math.Round(float64(ms1.TotalAlloc-ms0.TotalAlloc) / 1024 / n),
		GCs: int(ms1.NumGC - ms0.NumGC), GCPause: float64(ms1.PauseTotalNs-ms0.PauseTotalNs) / 1e6,
		Seconds: time.Since(start).Seconds(), AnimFrames: int(st1.Advances - st0.Advances), Players: st1.Players, Quiet: quiet, Why: why}
	for _, d := range ds {
		p.TotalMS += d
		switch {
		case d > 250:
			p.Over250++
			fallthrough
		case d > 100:
			p.Over100++
			fallthrough
		case d > 50:
			p.Over50++
		}
	}
	if len(ds) > 0 {
		sort.Float64s(ds)
		p.MedianMS, p.P95MS, p.P99MS, p.MaxMS = ds[len(ds)/2], ds[len(ds)*95/100], ds[len(ds)*99/100], ds[len(ds)-1]
	}
	return p
}

// perfRenderLog is a pass's renders (perfRenderBridge): how many per
// component, and the props that changed when a root was rendered again.
type perfRenderLog struct {
	mu    sync.Mutex
	last  map[uint64]map[string]json.RawMessage
	names map[string]int
	keys  map[string]int
}

func (l *perfRenderLog) note(root uint64, name string, props []byte) {
	var m map[string]json.RawMessage
	_ = json.Unmarshal(props, &m)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.names[name]++
	if old, ok := l.last[root]; ok {
		for _, path := range perfJSONDiff(name, mustJSON(old), props, 16) {
			l.keys[path]++
		}
	}
	l.last[root] = m
}

func mustJSON(m map[string]json.RawMessage) []byte {
	b, _ := json.Marshal(m)
	return b
}

// perfJSONDiff lists the paths under which a and b (JSON) differ, down to
// depth levels: the leaves that changed, with the old and new values of a
// short one (synthetic data only).
func perfJSONDiff(path string, a, b []byte, depth int) []string {
	if string(a) == string(b) {
		return nil
	}
	var ma, mb map[string]json.RawMessage
	if depth > 0 && json.Unmarshal(a, &ma) == nil && json.Unmarshal(b, &mb) == nil {
		var out []string
		for k, v := range mb {
			if w, ok := ma[k]; !ok {
				out = append(out, path+"."+k+" (new)")
			} else {
				out = append(out, perfJSONDiff(path+"."+k, w, v, depth-1)...)
			}
		}
		for k := range ma {
			if _, ok := mb[k]; !ok {
				out = append(out, path+"."+k+" (gone)")
			}
		}
		return out
	}
	var la, lb []json.RawMessage
	if depth > 0 && json.Unmarshal(a, &la) == nil && json.Unmarshal(b, &lb) == nil {
		if len(la) != len(lb) {
			return []string{fmt.Sprintf("%s (%d → %d items)", path, len(la), len(lb))}
		}
		var out []string
		for i := range lb {
			out = append(out, perfJSONDiff(fmt.Sprintf("%s.%d", path, i), la[i], lb[i], depth-1)...)
		}
		return out
	}
	if len(a) <= 40 && len(b) <= 40 {
		return []string{fmt.Sprintf("%s: %s → %s", path, a, b)}
	}
	return []string{path}
}

// top lists the components rendered most and the props that changed most.
func (l *perfRenderLog) top() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.names) == 0 {
		return ""
	}
	list := func(m map[string]int) string {
		type kv struct {
			k string
			n int
		}
		var s []kv
		for k, n := range m {
			s = append(s, kv{k, n})
		}
		sort.Slice(s, func(i, j int) bool { return s[i].n > s[j].n || s[i].n == s[j].n && s[i].k < s[j].k })
		var b strings.Builder
		for i, x := range s {
			if i == 12 {
				break
			}
			fmt.Fprintf(&b, " %s×%d", x.k, x.n)
		}
		return b.String()
	}
	return "by component:" + list(l.names) + "; changed props:" + list(l.keys)
}

// perfRenderBridge counts renders as countingBridge does and logs them in
// a perfRenderLog.
type perfRenderBridge struct {
	countingBridge
	log *perfRenderLog
}

func (b perfRenderBridge) Render(root uint64, name string, props []byte) {
	b.log.note(root, name, props)
	b.countingBridge.Render(root, name, props)
}

func (b perfRenderBridge) RenderIn(root, parent uint64, parentK int64, name string, props []byte) {
	b.log.note(root, name, props)
	b.countingBridge.RenderIn(root, parent, parentK, name, props)
}

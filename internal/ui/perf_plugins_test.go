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
//	idle-busy     the channel left alone for 10 s while the workspace goes
//	              on around it (messages and updates in the channels opened
//	              earlier, profile changes of people no row draws, badges,
//	              a pref's echo, a new emoji), frames drawn only when
//	              something asks, as the window draws them (perfIdle): the
//	              frames and Go→JS tasks a second; Slack draws nothing then
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
//	PERF_PLUGIN_GPU       n: then the channel scrolled n frames with the
//	                      GPU renderer drawing each (light, then dark),
//	                      each frame waited for to the end of its
//	                      rasterization (Mesa llvmpipe rasterizes on the
//	                      CPU, so what a frame costs the GPU shows as time)
//
// Synthetic data only; nothing reaches Slack. It uses nothing newer than
// the plugin bench, so the perf workflow runs this same file on the
// baseline tree for the "before" column.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
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
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache/webptest"
	"github.com/some-du6e/clack/internal/jsrt"
	"github.com/some-du6e/clack/internal/pluginlib"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
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
	// Tasks are the Go→JS tasks queued over the pass (the mirror's
	// invalidations, view fetches, renders, events), and BacklogMS how
	// long the runtime took to get through them once the frames ended:
	// its trees from then count for the pass (a runtime that fell behind
	// read as nothing having changed)
	Tasks     int64   `json:"tasks"`
	BacklogMS float64 `json:"backlog_ms"`
	// Unmounts are the roots unmounted during the pass: slackc's sweep of
	// roots the steps before it drew, 5 to 7 s after their last draw
	// (slackc/frame.go). One Go→JS task each, left out of Tasks and
	// TasksPS: they're cleanup of what came before, not the pass's work
	Unmounts int64 `json:"unmounts"`
	// Polls are core's poll ticks during the pass (pollLoop, every 20 s
	// with no socket, as the bench runs): each replaces the open channel's
	// history with the fake's, and with it any injected edit the fake
	// never had. The idle passes start right after one
	Polls int64 `json:"polls"`
	// PollsCPU are the polls over all the pass's CPU counts (its frames,
	// then the JS drain and settle after them): a tick there costs CPU
	// that Polls doesn't show
	PollsCPU int64 `json:"polls_cpu"`
	// ListLive is whether the bench channel's list drew the model the
	// messages hub keeps for it at the end of the pass (perfListModel):
	// "live", or "dropped" (it took no live event). Before 78375754 the
	// bench, opened after 40 other channels, was dropped in every pass
	ListLive string `json:"list_live,omitempty"`
	// Sources are an idle pass's frame requests by who asked (the top
	// eight; perfIdle logs them)
	Sources map[string]int `json:"sources,omitempty"`
	// the idle passes (perfIdle): how long they ran, the animation frames
	// the image cache's players moved on to meanwhile (every size of every
	// animated image counts), and the players playing at the end
	Seconds    float64        `json:"seconds,omitempty"`
	AnimFrames int            `json:"anim_frames,omitempty"`
	Players    int            `json:"players,omitempty"`
	Quiet      int            `json:"quiet_frames,omitempty"` // of Frames: the last frame's ops again, no layout (quiet.go)
	Why        map[string]int `json:"not_quiet,omitempty"`    // the frames laid out, by why they weren't quiet
	// and a second: the frames the window drew, the Go→JS tasks and the
	// renders (nil outside the idle passes)
	FPS       *float64 `json:"fps,omitempty"`
	TasksPS   *float64 `json:"tasks_ps,omitempty"`
	RendersPS *float64 `json:"renders_ps,omitempty"`
	// Slow are the frames over 40 ms: what the frame goroutine was doing
	// 40 and 120 ms in (function names), with what landed meanwhile
	Slow []perfSlow `json:"slow,omitempty"`
	// TraceMB and TraceMS: with the flight recorder on, the trace of its
	// window written after the pass, as a slow frame writes it
	TraceMB float64 `json:"trace_mb,omitempty"`
	TraceMS float64 `json:"trace_ms,omitempty"`
	// the GPU passes, per frame, where the tree counts them (perfGPUStats):
	// draw calls, calls into the graphics API and the ones its state
	// cache left out, and the pixels covered through a path and blitted
	Draws      float64 `json:"draws,omitempty"`
	APICalls   float64 `json:"api_calls,omitempty"`
	APISkipped float64 `json:"api_skipped,omitempty"`
	Crossings  float64 `json:"crossings,omitempty"`   // calls from Go into C or the system
	PathsBuilt float64 `json:"paths_built,omitempty"` // paths tessellated and uploaded
	CoverMpx   float64 `json:"cover_mpx,omitempty"`
	BlitMpx    float64 `json:"blit_mpx,omitempty"`
}

// perfJSTasks is how many tasks Go has queued for the Taut runtime so far
// (jsrt's enqN, read by reflection: the base tree runs this file too, and
// its jsrt exports no count).
func perfJSTasks(e *benchEnv) int64 {
	rt, err := e.tt.JS()
	if err != nil || rt == nil {
		return 0
	}
	v := reflect.ValueOf(rt).Elem().FieldByName("enqN")
	if !v.IsValid() {
		return 0
	}
	if f := v.FieldByName("v"); f.IsValid() && f.CanUint() {
		return int64(f.Uint())
	}
	return 0
}

// perfQuiesce draws laid-out frames for at least atLeast, then until no list
// is loading and no Go→JS render, no JS tree and no unmount has come for
// still, at most d. atLeast covers slackc's sweep of the roots the steps
// before drew (staleAfter + sweepEvery after their last draw, 7 s): the
// idle passes measured that sweep, a wave of unmounts landing in
// whichever pass came 5 to 7 s after the last scroll.
func (e *benchEnv) perfQuiesce(d, still, atLeast time.Duration) {
	count := func() int64 {
		return e.calls.render.Load() + e.calls.renderIn.Load() + e.calls.unmount.Load() + int64(e.delivered())
	}
	loading := func() bool {
		for _, c := range e.a.convos {
			if l, ok := c.list.(interface{ Loading() bool }); ok && l.Loading() {
				return true
			}
		}
		return false
	}
	if perfReg != nil {
		perfReg.keepProps(true) // from here: the idle pass after it says what changed in a root's props
	}
	calls0, trees0 := e.calls.snapshot(), e.delivered()
	prev, last, start := count(), time.Now(), time.Now()
	dl := time.Now().Add(d)
	for time.Now().Before(dl) && (time.Since(start) < atLeast || time.Since(last) < still || loading()) {
		e.frame()
		time.Sleep(5 * time.Millisecond)
		if n := count(); n != prev {
			prev, last = n, time.Now()
		}
	}
	calls, trees := e.calls.snapshot(), e.delivered()
	capped := ""
	if !time.Now().Before(dl) {
		capped = ", at its cap: still busy"
	}
	e.t.Logf("quiesce %.1f s%s: %d renders, %d trees, %d unmounts meanwhile; the last %.1f s still", time.Since(start).Seconds(), capped,
		calls[0]-calls0[0]+calls[1]-calls0[1], trees-trees0, calls[5]-calls0[5], time.Since(last).Seconds())
}

// perfPollWatch counts core's poll ticks (perfPolls) for the run.
var perfPollWatch *perfPolls

// perfPolls follows core's "poll" events: pollLoop's tick, every 20 s
// with no socket.
type perfPolls struct {
	n    atomic.Int64
	tick chan struct{}
}

// watchPolls starts following core's polls.
func (e *benchEnv) watchPolls(t *testing.T) *perfPolls {
	evs, stop := e.a.data.core.Subscribe()
	p := &perfPolls{tick: make(chan struct{}, 1)}
	done := make(chan struct{})
	go func() {
		for {
			select {
			case ev, ok := <-evs:
				if !ok {
					return
				}
				if ev.Type == "poll" {
					p.n.Add(1)
					select {
					case p.tick <- struct{}{}:
					default:
					}
				}
			case <-done:
				return
			}
		}
	}()
	t.Cleanup(func() { close(done); stop() })
	return p
}

// polls is how many polls have come so far (0 with no watch).
func perfPollCount() int64 {
	if perfPollWatch == nil {
		return 0
	}
	return perfPollWatch.n.Load()
}

// waitPoll draws frames until core's next poll, at most d.
func (e *benchEnv) waitPoll(d time.Duration) bool {
	p := perfPollWatch
	if p == nil {
		return false
	}
	select { // a tick from before: the next one
	case <-p.tick:
	default:
	}
	for dl := time.Now().Add(d); time.Now().Before(dl); {
		select {
		case <-p.tick:
			return true
		default:
		}
		e.frame()
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// perfViewStats are how often taut's view epoch moved and how many
// batches its view cache fetched from JS, so far (read by reflection).
func perfViewStats(e *benchEnv) (epoch, fetches uint64) {
	ui := reflect.ValueOf(e.tt).Elem().FieldByName("ui")
	if !ui.IsValid() || ui.IsNil() {
		return 0, 0
	}
	v := ui.Elem().FieldByName("views")
	if !v.IsValid() {
		return 0, 0
	}
	// either may be missing (the base tree's cache, older than the epoch)
	if f := v.FieldByName("epoch"); f.IsValid() {
		if f = f.FieldByName("v"); f.IsValid() && f.CanUint() {
			epoch = f.Uint()
		}
	}
	if f := v.FieldByName("fetches"); f.IsValid() {
		if f = f.FieldByName("v"); f.IsValid() && f.CanInt() {
			fetches = uint64(f.Int())
		}
	}
	return epoch, fetches
}

// perfDrain waits for the Taut runtime to get through what's queued, and
// returns how long that took.
func perfDrain(e *benchEnv) time.Duration {
	rt, err := e.tt.JS()
	if err != nil || rt == nil {
		return 0
	}
	t0 := time.Now()
	_ = rt.Do(func(*jsrt.Ctx) error { return nil })
	return time.Since(t0)
}

// perfGPUFrame is what the GPU renderer did for a frame.
type perfGPUFrame struct {
	Draws, APICalls, Skipped, Crossings int64
	CoverPx, BlitPx                     int64
	PathsBuilt                          int64
}

// perfGPUStats reads the last frame's from the GPU window; trees with
// Gio's frame stats (Clack patch 36) set it in perf_gpustats_test.go. The
// base tree runs this file without it: there the GPU passes have times
// only.
var perfGPUStats func(w *headless.Window) perfGPUFrame

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
	// EditLatencyMS: edits of a message on screen in the bench channel,
	// each from its event to the first frame that draws it, frames drawn
	// back to back (perfEditLatency); EditViewed whether the row drew the
	// plugins' view of the message when the edit came
	EditLatencyMS []float64 `json:"edit_latency_ms,omitempty"`
	EditViewed    []bool    `json:"edit_viewed,omitempty"`
	// MaskedEditMS: the same for edits a plugin changes (a word masked in
	// the messages slice, as Censorship does): the row draws the plugins'
	// view on both trees, so both wait for JS's copy of each revision
	// (perfMaskedEditLatency). MaskedEditViewed as EditViewed.
	MaskedEditMS     []float64 `json:"masked_edit_ms,omitempty"`
	MaskedEditViewed []bool    `json:"masked_edit_viewed,omitempty"`
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
	// every root from here on, for the idle passes' log
	perfReg = newPerfRootReg()
	t.Cleanup(func() { perfReg = nil })
	perfPollWatch = e.watchPolls(t)
	t.Cleanup(func() { perfPollWatch = nil })
	slackc.SetBridge(perfRegBridge{countingBridge{e.tt.Bridge(), &e.calls}, perfReg})

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

	// a long session: many channels opened (the mirror watches each). 38,
	// the bench channel the 39th, a slot spare: the messages hub keeps 40
	// conversations' models, and before plugin-render-3 the 41st opened
	// kept none (its new model read as the oldest), so the bench list,
	// opened after 40 others, took no live event in any pass, only its own
	// 30 s refresh (TestListsPastTheModelLimitTakeLiveMessages). Under the
	// limit both trees draw the live list (each pass's list_live says so);
	// "channel-live, past 40" goes past it.
	chans := fs.ChannelIDs()
	var others []string
	for _, ch := range chans {
		if ch == fakeslack.BenchChannelID || len(others) >= 38 {
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
	t.Logf("the bench list, opened: %s", e.perfListModel(fakeslack.BenchChannelID))
	e.perfLoadAll()
	t.Logf("the bench list, all loaded: %s", e.perfListModel(fakeslack.BenchChannelID))
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
	// from the bottom, past slackc's sweep of the rows loading drew (5 to
	// 7 s after their last draw): how many of them were gone when the pass
	// came back to them hung on how long setup took, and the pass's
	// renders with it (mounted again after the sweep, or not)
	e.toBottom()
	e.perfQuiesce(40*time.Second, 1500*time.Millisecond, 7500*time.Millisecond)
	// and past JS's backlog: what it still had queued from the setup came
	// back as trees in the pass, first mounts counted as the pass's
	// renders (a setup that left one tree's JS further behind showed more)
	if d := perfDrain(e); d > 20*time.Millisecond {
		t.Logf("JS's backlog before the channel pass: %.0f ms", float64(d.Microseconds())/1000)
		e.perfQuiesce(20*time.Second, 1500*time.Millisecond, 0)
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

	// the channel again with the GPU drawing every frame, light and dark
	if n, _ := strconv.Atoi(os.Getenv("PERF_PLUGIN_GPU")); n > 0 {
		e.openBench()
		e.settle(time.Second)
		profPrefix := perfProfPrefix
		perfProfPrefix = ""
		for _, dark := range []bool{false, true} {
			res.Passes = append(res.Passes, e.perfGPUScroll(dark, n))
		}
		perfProfPrefix = profPrefix
	}

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

	// past the hub's 40 models, as Karim's sessions go: 41 conversations
	// drawn after the bench channel (the 38 others again, and three new
	// ones), then the channel shown again and live. On a tree with
	// 78375754 the hub lets the bench's model go (drawn longest ago) and
	// takes it back as it shows; before it, the bench's model stays and
	// each new one is dropped as it's made
	{
		var fresh []string
		for _, ch := range chans {
			if len(fresh) == 3 {
				break
			}
			if ch != fakeslack.BenchChannelID && !slices.Contains(others, ch) {
				fresh = append(fresh, ch)
			}
		}
		for _, ch := range append(append([]string(nil), others...), fresh...) {
			e.openChannel(ch)
			e.settle(80 * time.Millisecond)
		}
		e.openBench()
		e.toBottom()
		t.Logf("the bench list, shown again after %d conversations: %s", len(others)+len(fresh), e.perfListModel(fakeslack.BenchChannelID))
		e.perfQuiesce(40*time.Second, 1500*time.Millisecond, 7500*time.Millisecond)
		res.Passes = append(res.Passes, e.perfScroll("channel-live, past 40", e.listPoint, traffic(fakeslack.BenchChannelID, "")))
	}

	// update to screen: an edit of a message on screen, from its event to
	// the frame that draws it, under the plugins (a row drawing their view
	// waits for JS's copy of the new revision)
	if !e.waitPoll(25 * time.Second) {
		t.Log("edit latency: no poll within 25 s")
	}
	res.EditLatencyMS, res.EditViewed = e.perfEditLatency(t, 6)
	res.MaskedEditMS, res.MaskedEditViewed = e.perfMaskedEditLatency(t, 6)

	// an animated custom emoji on screen (perfAnimEmoji): the channel left
	// alone, frames drawn only when something asks for one, as the window
	// draws them; first without the emoji (the baseline: nothing asks),
	// then with it in new messages and reactions, then the same at 60
	// frames a second (something else drawing meanwhile)
	e.openBench()
	e.toBottom()
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(-100, -100)}) // off the window
	// The live passes' edits, reactions and deletions were events the fake
	// server never saw. Core's poll (pollLoop, every 20 s with no socket,
	// as the bench runs) replaces the open channel's history with the
	// fake's (pollOnce → loadLatest → SetLatest), and the rows they changed
	// go back: renders in whichever pass the poll lands (every head idle
	// window of one run and one base window: 0 → 6 renders, 12 → 19
	// frames). The hub's 30 s refresh only reads core's cache. So each idle
	// pass starts right after a poll, past the sweep of what the steps
	// before drew (atLeast), and ends before the next: both trees alike.
	afterPoll := func(atLeast time.Duration) {
		if !e.waitPoll(25 * time.Second) {
			t.Log("no poll within 25 s: the idle pass starts without one")
		}
		e.perfQuiesce(15*time.Second, 1500*time.Millisecond, atLeast)
	}
	afterPoll(7500 * time.Millisecond)
	res.Passes = append(res.Passes, e.perfIdle("idle", 5*time.Second, false, nil))
	// nothing on screen changes for 10 s while the workspace goes on
	// around the open channel
	busy := traffic(fakeslack.BenchChannelID, "")
	busy.idle = true
	afterPoll(2 * time.Second) // the idle pass swept what was left
	res.Passes = append(res.Passes, e.perfIdle("idle-busy", 10*time.Second, false, busy))
	if e.perfAnimEmoji(t) {
		// its messages scrolled the rows before them away: their sweep too
		afterPoll(7500 * time.Millisecond)
		res.Passes = append(res.Passes, e.perfIdle("emoji", 5*time.Second, false, nil))
		afterPoll(2 * time.Second)
		res.Passes = append(res.Passes, e.perfIdle("emoji-60hz", 5*time.Second, true, nil))
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
		t.Logf("pass %-12s frames %d: max %.1f ms, >50ms %d, >100ms %d, >250ms %d, median %.2f ms, p95 %.2f ms, p99 %.2f ms, total %.0f ms, cpu %.0f ms, %.0f allocs %.0f KB/frame, %d GCs %.1f ms paused, events %d, trees changed %d (identical %d), renders %d, JS tasks %d, JS backlog %.0f ms",
			p.Name, p.Frames, p.MaxMS, p.Over50, p.Over100, p.Over250, p.MedianMS, p.P95MS, p.P99MS, p.TotalMS, p.CPUMS, p.Allocs, p.AllocKB, p.GCs, p.GCPause, p.Events, p.Trees, p.TreesSame, p.Renders, p.Tasks, p.BacklogMS)
		if p.Draws > 0 {
			t.Logf("pass %-12s per frame: %.0f draw calls, %.0f API calls (%.0f left out, %.0f crossings), %.0f paths built, %.2f Mpx covered, %.2f Mpx blitted", p.Name, p.Draws, p.APICalls, p.APISkipped, p.Crossings, p.PathsBuilt, p.CoverMpx, p.BlitMpx)
		}
	}
	for _, p := range res.Passes {
		if p.Seconds > 0 {
			t.Logf("idle pass %-10s %d frames in %.1f s (%.1f ms of frames a second, %d quiet; laid out: %v), %d animation frames shown, %d players at the end; %d events, %d Go→JS tasks, %d renders, JS backlog %.0f ms", p.Name, p.Frames, p.Seconds, p.TotalMS/p.Seconds, p.Quiet, p.Why, p.AnimFrames, p.Players, p.Events, p.Tasks, p.Renders, p.BacklogMS)
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
	calls0, tasks0, unm0, polls0 := e.calls.snapshot(), perfJSTasks(e), e.calls.unmount.Load(), perfPollCount()
	// what rendered, by component and why (counted only: the pass copies
	// no props), and how often the views were fetched: what a pass's
	// renders and tasks are, where two trees differ
	cl := newPerfRenderLog(0)
	cl.countOnly = true
	_, fetch0 := perfViewStats(e)
	if perfReg != nil {
		perfReg.start(cl)
		defer func() {
			perfReg.stop()
			_, fetch1 := perfViewStats(e)
			e.t.Logf("pass %s: %d view fetches; the bench list draws %s", name, fetch1-fetch0, e.perfListModel(fakeslack.BenchChannelID))
			for _, line := range cl.top() {
				e.t.Logf("pass %s: %s", name, line)
			}
		}()
	}
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
	tasks := perfJSTasks(e) - tasks0
	unm := e.calls.unmount.Load() - unm0 // beside tasks: those of roots the pass itself scrolled away are its own
	polls := perfPollCount() - polls0
	backlog := perfDrain(e) // what JS still had to answer: its trees are this pass's
	trees1, same1 := treeCounts()
	runtime.ReadMemStats(&ms1)
	e.settle(400 * time.Millisecond) // the JS renders of rows that scrolled in
	cpu := procCPU() - c0
	pollsCPU := perfPollCount() - polls0
	calls := e.calls.snapshot()
	n := float64(len(ds))
	p := perfPass{Name: name, Frames: len(ds), CPUMS: float64(cpu.Microseconds()) / 1000, Events: events, Tasks: tasks, Unmounts: unm, Polls: polls,
		PollsCPU: pollsCPU, ListLive: perfListLive(e),
		BacklogMS: float64(backlog.Microseconds()) / 1000,
		Trees:     trees1 - trees0 - (same1 - same0), TreesSame: same1 - same0, Renders: calls[0] - calls0[0] + calls[1] - calls0[1],
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

// perfGPUScroll scrolls the channel n frames (half up, half down) with
// the GPU renderer drawing each, light or dark, and times each frame from
// the input to the end of its rasterization (a one-pixel read back waits
// for it): layout, collect, encode and raster, which Mesa's llvmpipe does
// on the CPU. With perfGPUStats it records the renderer's counts too.
func (e *benchEnv) perfGPUScroll(dark bool, n int) perfPass {
	name := "gpu channel, light"
	if dark {
		name = "gpu channel, dark"
	}
	w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
	if err != nil {
		e.t.Logf("%s: no GPU renderer (%v)", name, err)
		return perfPass{Name: name}
	}
	e.gpu = w
	defer func() {
		w.Release()
		e.gpu = nil
		e.a.setDark(false)
	}()
	e.a.setDark(dark)
	e.settle(1500 * time.Millisecond) // llvmpipe frames are slow; tokens restyle over several
	runtime.GC()
	px := image.NewRGBA(image.Rect(0, 0, 1, 1))
	var ms0, ms1 runtime.MemStats
	runtime.ReadMemStats(&ms0)
	c0 := procCPU()
	var ds []float64
	var sum perfGPUFrame
	counted := 0
	for i := 0; i < n; i++ {
		dy := float32(-260)
		if i >= n/2 {
			dy = 260
		}
		e.scroll(e.listPoint(), dy)
		t0 := time.Now()
		e.frame()
		if err := w.Screenshot(px); err != nil {
			e.t.Fatalf("%s: %v", name, err)
		}
		d := time.Since(t0)
		ds = append(ds, float64(d.Microseconds())/1000)
		if perfGPUStats != nil {
			f := perfGPUStats(w)
			sum.Draws += f.Draws
			sum.APICalls += f.APICalls
			sum.Skipped += f.Skipped
			sum.Crossings += f.Crossings
			sum.PathsBuilt += f.PathsBuilt
			sum.CoverPx += f.CoverPx
			sum.BlitPx += f.BlitPx
			counted++
		}
		if d < 16*time.Millisecond {
			time.Sleep(16*time.Millisecond - d)
		}
	}
	runtime.ReadMemStats(&ms1)
	cpu := procCPU() - c0
	p := perfPass{Name: name, Frames: len(ds), CPUMS: float64(cpu.Microseconds()) / 1000,
		Allocs: math.Round(float64(ms1.Mallocs-ms0.Mallocs) / float64(len(ds))), AllocKB: math.Round(float64(ms1.TotalAlloc-ms0.TotalAlloc) / 1024 / float64(len(ds))),
		GCs: int(ms1.NumGC - ms0.NumGC), GCPause: float64(ms1.PauseTotalNs-ms0.PauseTotalNs) / 1e6}
	if counted > 0 {
		k := float64(counted)
		p.Draws, p.APICalls, p.APISkipped, p.Crossings = math.Round(float64(sum.Draws)/k), math.Round(float64(sum.APICalls)/k), math.Round(float64(sum.Skipped)/k), math.Round(float64(sum.Crossings)/k)
		p.CoverMpx, p.BlitMpx = float64(sum.CoverPx)/k/1e6, float64(sum.BlitPx)/k/1e6
		p.PathsBuilt = math.Round(float64(sum.PathsBuilt) / k)
	}
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
	return p
}

// perfTraffic is a busy workspace: in the open conversation, someone
// types every 150 ms, a message (or thread reply) every 700 ms, a
// reaction every 300 ms, an edit every 1.5 s and a delete every 2.5 s
// (Message Logger keeps deleted messages: injected ones); in the other
// channels a message every 250 ms. Each live message mentions someone,
// and every third shares a file someone else owns (a processed upload's
// complete file object). A large workspace's profile changes come too,
// as Enterprise Grid sends them: 4 a second for members no row draws (a
// status, a huddle joined), one every 2.5 s for an author on screen,
// and once in a pass an author's new display name; badge counts every
// 2 s and a followed thread's state every 4 s. As realtime events
// through core's handler, from their own goroutine like the socket's.
//
// idle is the idle pass's traffic: none in the open conversation, the
// rest as above, plus Slack's pref_change echo every 10 s and a custom
// emoji added every 3 s.
type perfTraffic struct {
	e          *benchEnv
	ch, thread string
	others     []string
	idle       bool

	quit    chan struct{}
	done    sync.WaitGroup
	n       atomic.Int64
	status  int
	undrawn []string // directory members no row draws
}

// profile is a user_change for member id as the socket brings it: the
// stored profile with a new status (and huddle state), and with rename
// its display name changed too (a suffix added or taken off).
func (p *perfTraffic) profile(id string, rename bool) {
	u, ok := p.e.a.data.core.S.RawUser(id)
	if !ok || u == nil {
		return
	}
	c := *u
	p.status++
	c.Profile.StatusText, c.Profile.StatusEmoji = fmt.Sprintf("in a meeting (%d)", p.status), ":spiral_calendar_pad:"
	if p.status%3 == 0 {
		c.Profile.HuddleState = "in_a_huddle"
	} else {
		c.Profile.HuddleState = ""
	}
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
	authors := map[string]bool{p.e.a.data.core.S.SelfID(): true}
	for _, a := range p.e.c.Authors {
		authors[a] = true
	}
	for _, id := range p.e.a.data.core.S.UserIDs() {
		if !authors[id] && len(p.undrawn) < 300 {
			p.undrawn = append(p.undrawn, id)
		}
	}
	sort.Strings(p.undrawn)
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

// raw is a realtime event with no conversation (badges, a thread's
// state, a pref's echo, an emoji).
func (p *perfTraffic) raw(typ string, body map[string]any) {
	body["type"] = typ
	b, _ := json.Marshal(body)
	p.e.a.data.core.Inject(slack.Event{Type: typ, Raw: b})
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
	var elsewhere [][2]string // the other channels' messages (channel, ts): the idle pass's reactions and edits
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	start := time.Now()
	var lastType, lastMsg, lastReact, lastEdit, lastDel, lastOther, lastUndrawn, lastDrawn, lastBadge, lastThread, lastPref, lastEmoji time.Duration
	renamed := false
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
		// profile changes: mostly people no row draws
		if len(p.undrawn) > 0 && el-lastUndrawn >= 250*time.Millisecond {
			lastUndrawn = el
			p.profile(p.undrawn[(seq*7)%len(p.undrawn)], false)
		}
		if !p.idle && el-lastDrawn >= 2500*time.Millisecond {
			lastDrawn = el
			p.profile(authors[(seq*3)%len(authors)], false)
		}
		if !p.idle && !renamed && el >= time.Second {
			renamed = true
			p.profile(authors[(seq*5)%len(authors)], true)
		}
		if el-lastBadge >= 2*time.Second {
			lastBadge = el
			p.raw("badge_counts_updated", map[string]any{"channels": 3, "dms": 1, "threads_mention_count": 0})
		}
		if el-lastThread >= 4*time.Second {
			lastThread = el
			p.raw("update_thread_state", map[string]any{"has_unreads": false, "mention_count": 0, "timestamp": liveTS()})
		}
		if p.idle && el-lastPref >= 10*time.Second {
			lastPref = el
			p.raw("pref_change", map[string]any{"name": "emoji_use", "value": "{}"})
		}
		if p.idle && el-lastEmoji >= 3*time.Second {
			lastEmoji = el
			name := fmt.Sprintf("perf-emoji-%d", seq)
			p.raw("emoji_changed", map[string]any{"subtype": "add", "name": name, "value": "https://emoji.invalid/" + name + ".png", "event_ts": liveTS()})
		}
		if len(p.others) > 0 && el-lastOther >= 250*time.Millisecond {
			lastOther = el
			ts := liveTS()
			ch := p.others[seq%len(p.others)]
			p.inject(ch, "message", "", map[string]any{"user": u, "text": "elsewhere " + ts, "ts": ts, "event_ts": ts})
			elsewhere = append(elsewhere, [2]string{ch, ts})
		}
		if p.idle {
			// reactions and edits elsewhere: updates to messages in
			// channels opened earlier, none on screen
			if len(elsewhere) > 0 && el-lastReact >= 500*time.Millisecond {
				lastReact = el
				at := elsewhere[(seq*7)%len(elsewhere)]
				p.inject(at[0], "reaction_added", "", map[string]any{"user": u, "reaction": "eyes", "item": map[string]any{"type": "message", "channel": at[0], "ts": at[1]}})
			}
			if len(elsewhere) > 0 && el-lastEdit >= 2*time.Second {
				lastEdit = el
				at := elsewhere[(seq*3)%len(elsewhere)]
				msg := map[string]any{"type": "message", "user": u, "text": "edited elsewhere " + at[1], "ts": at[1], "edited": map[string]any{"user": u, "ts": liveTS()}}
				p.inject(at[0], "message", "message_changed", map[string]any{"message": msg, "ts": liveTS(), "event_ts": liveTS(), "hidden": true})
			}
			continue
		}
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
				id := fmt.Sprintf("F0LIVE%05d", msgs)
				ev["files"] = []any{map[string]any{"id": id, "name": "notes.pdf", "title": "Notes", "mimetype": "application/pdf",
					"filetype": "pdf", "pretty_type": "PDF", "mode": "hosted", "user": authors[(seq*3+2)%len(authors)], "size": 2048,
					"url_private": "https://files.invalid/" + id + "/notes.pdf", "url_private_download": "https://files.invalid/" + id + "/download/notes.pdf",
					"permalink": "https://files.invalid/" + id, "created": time.Now().Unix()}}
			}
			if p.thread != "" {
				ev["thread_ts"] = p.thread
			}
			p.inject(p.ch, "message", "", ev)
			targets = append(targets, ts)
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
	inTimeline := func() int {
		n := 0
		if tl := e.a.data.core.S.Timeline(ch); tl != nil {
			for _, m := range tl.Msgs {
				for _, ts := range posted {
					if m.TS == ts {
						n++
					}
				}
			}
		}
		return n
	}
	// what the list drew of them, for a pass whose image is never asked
	// for: the rows on screen, the image queue, core's polls meanwhile
	polls0 := perfPollCount()
	state := func() string {
		var b strings.Builder
		fmt.Fprintf(&b, "route %q", e.a.router.Current().Channel())
		if c := e.a.convos[ch+"/"]; c == nil {
			b.WriteString(", no list for the channel")
		} else {
			if l, ok := c.list.(interface{ AtBottom() bool }); ok {
				fmt.Fprintf(&b, ", at the bottom %v", l.AtBottom())
			}
			if l, ok := c.list.(interface {
				LayoutSig() (int, bool, uint64)
			}); ok {
				n, loaded, _ := l.LayoutSig()
				fmt.Fprintf(&b, ", %d messages in the list (loaded %v)", n, loaded)
			}
			if l, ok := c.list.(interface{ VisibleRows() []messages.RowY }); ok {
				rows := l.VisibleRows()
				mine, last := 0, ""
				for _, r := range rows {
					for _, ts := range posted {
						if strings.TrimPrefix(r.Key, "g") == ts {
							mine++
						}
					}
					last = r.Key
				}
				fmt.Fprintf(&b, ", %d rows on screen (%d of these, the last %s)", len(rows), mine, last)
			}
			if l, ok := c.list.(interface{ RowText(string) string }); ok {
				fmt.Fprintf(&b, ", the newest drew %q", l.RowText(posted[len(posted)-1]))
			}
		}
		fmt.Fprintf(&b, "; %d of %d in core's timeline, %d images loading, its image asked for %d times, %d polls since the posts; the list draws %s",
			inTimeline(), len(posted), e.a.images.Pending(), asked.Load(), perfPollCount()-polls0, e.perfListModel(ch))
		if u := e.a.data.core.CustomEmoji()[name]; u != all[name] {
			fmt.Fprintf(&b, ", its URL now %q", u)
		}
		return b.String()
	}
	playing := func() bool { return e.a.images.Stats().Players > 0 }
	e.toBottom()
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(-100, -100)})
	t.Logf("emoji: posted and at the bottom: %s", state())
	polled := false
	ok := e.wait(15*time.Second, func() bool {
		if !polled && perfPollCount() > polls0 {
			polled = true
			t.Logf("emoji: at core's poll: %s", state())
		}
		return playing()
	})
	if !ok {
		// to the bottom again, then the list refreshed: the one that
		// brings the image says what kept it away
		t.Logf("emoji: no player in 15 s: %s", state())
		e.toBottom()
		if ok = e.wait(5*time.Second, playing); ok {
			t.Logf("emoji: playing after a second scroll to the bottom: %s", state())
		} else {
			e.a.refreshVisible()
			if ok = e.wait(5*time.Second, playing); ok {
				t.Logf("emoji: playing after the list's refresh: %s", state())
			}
		}
	}
	e.settle(1500 * time.Millisecond) // every size's player, the rows' plugin renders
	s := e.a.images.Stats()
	inTL := inTimeline()
	seenMu.Lock()
	t.Logf("emoji :%s: (an animated WebP behind a .png URL): %d players, %d animated images, playing %v; %d of %d messages in the timeline; its image asked for %d times (%d served: %q, want %q); loaded %v, animation at 22/32/44 px %v/%v/%v",
		name, s.Players, s.Animated, ok, inTL, len(posted), asked.Load(), served.Load(), seen, path, e.a.images.Loaded(all[name]),
		e.a.images.IsAnimation(all[name], 22), e.a.images.IsAnimation(all[name], 32), e.a.images.IsAnimation(all[name], 44))
	seenMu.Unlock()
	return ok
}

// perfListModel says whether ch's list draws the model the messages hub
// keeps for ch, the one core's events and polls change (by reflection:
// messages' listState.m against hub.chans[ch]; the hub writes chans on
// this goroutine). A list left with another takes no live change until
// its own 30 s refresh.
func (e *benchEnv) perfListModel(ch string) string {
	c := e.a.convos[ch+"/"]
	if c == nil {
		return "no list"
	}
	cl, ok := c.list.(channelList)
	if !ok || cl.ChannelView == nil {
		return "not a channel list"
	}
	s := reflect.ValueOf(cl.ChannelView).Elem().FieldByName("s")
	if !s.IsValid() || s.IsNil() {
		return "no list state"
	}
	m, hb := s.Elem().FieldByName("m"), s.Elem().FieldByName("hb")
	if !m.IsValid() || !hb.IsValid() || hb.IsNil() {
		return "no model"
	}
	chans := hb.Elem().FieldByName("chans")
	if !chans.IsValid() {
		return "no hub map"
	}
	switch got := chans.MapIndex(reflect.ValueOf(ch)); {
	case !got.IsValid():
		return fmt.Sprintf("a model the hub dropped (it keeps none for the channel, %d models)", chans.Len())
	case got.Pointer() != m.Pointer():
		return fmt.Sprintf("a model the hub dropped (it keeps another for the channel, %d models)", chans.Len())
	}
	return fmt.Sprintf("the hub's model (%d models)", chans.Len())
}

// srcCount is one frame requester's count (topSources).
type srcCount struct {
	k string
	n int
}

// topSources are the n requesters that asked for the most frames, most
// first.
func topSources(sources map[string]int, n int) []srcCount {
	var s []srcCount
	for k, c := range sources {
		s = append(s, srcCount{k, c})
	}
	sort.Slice(s, func(i, j int) bool { return s[i].n > s[j].n || s[i].n == s[j].n && s[i].k < s[j].k })
	if len(s) > n {
		s = s[:n]
	}
	return s
}

// perfListLive is perfListModel's verdict on the bench list: "live" (it
// draws the hub's model), "dropped", or "" (no bench list).
func perfListLive(e *benchEnv) string {
	switch v := e.perfListModel(fakeslack.BenchChannelID); {
	case strings.HasPrefix(v, "the hub's model"):
		return "live"
	case strings.HasPrefix(v, "a model the hub dropped"):
		return "dropped"
	}
	return ""
}

// perfLoadAll scrolls the bench list up until it holds the whole channel,
// by its own count (LayoutSig: what the list drew from), not core's: core
// held the 500 while the list's model still loaded its pages, and the
// channel pass began at the top of half of them, loading the rest.
func (e *benchEnv) perfLoadAll() {
	listCount := func() int {
		c := e.a.convos[fakeslack.BenchChannelID+"/"]
		if c == nil {
			return 0
		}
		if l, ok := c.list.(interface {
			LayoutSig() (int, bool, uint64)
		}); ok {
			n, _, _ := l.LayoutSig()
			return n
		}
		return e.loadedMessages(fakeslack.BenchChannelID)
	}
	p := e.listPoint()
	last, same := -1, 0
	for i := 0; i < 80 && same < 6; i++ {
		for k := 0; k < 20; k++ {
			e.scroll(p, -3000)
			e.frame()
		}
		e.settle(200 * time.Millisecond)
		n := listCount()
		if n >= fakeslack.BenchMessages {
			break
		}
		if n == last {
			same++
		} else {
			same = 0
		}
		last = n
	}
	e.t.Logf("loaded: %d messages in the list, %d in core", listCount(), e.loadedMessages(fakeslack.BenchChannelID))
}

// perfEditLatency edits a message on screen in the bench channel n times
// (after one edit first: Message Logger keeps an edited message's history
// in the messages slice, so its row draws the plugins' view from then on)
// and times each, from the edit's event to the first frame that draws it,
// frames drawn back to back as a window draws while anything moves. A
// row drawing the plugins' view keeps the revision before until JS's copy
// of the new one is ready: that is the latency a vote or an edit shows
// under Karim's plugins. viewed: whether the row drew the view as the edit
// came. Read by reflection (the row's drawn text and viewed flag), as the
// base tree's list has no accessor for them.
func (e *benchEnv) perfEditLatency(t *testing.T, n int) (ms []float64, viewed []bool) {
	edit, drawn, row, ok := e.perfEditTarget(t, "edit latency")
	if !ok {
		return nil, nil
	}
	first := fmt.Sprintf("edited for the latency pass %d", time.Now().UnixNano())
	edit(first)
	if !drawn(first, 10*time.Second) {
		t.Log("edit latency: the first edit was never drawn")
		return nil, nil
	}
	e.settle(1500 * time.Millisecond) // JS answers: the row on the plugins' view, if they change it
	for i := 0; i < n; i++ {
		text := fmt.Sprintf("edit %d of the latency pass %d", i, time.Now().UnixNano())
		_, v, _ := row()
		t0 := time.Now()
		edit(text)
		ok := drawn(text, 10*time.Second)
		d := float64(time.Since(t0).Microseconds()) / 1000
		if !ok {
			d = -1 // never drawn
		}
		ms, viewed = append(ms, d), append(viewed, v)
		e.settle(700 * time.Millisecond)
	}
	t.Logf("edit latency (ms, the row on the view): %v %v", ms, viewed)
	return ms, viewed
}

// perfMaskJS masks "job" in the bench channel's messages (the messages
// slice, as Censorship does): a row whose message has the word draws the
// plugins' view on any tree.
var perfMaskJS = []byte(`(() => {
const TautPlugin = globalThis.TautPlugin
class PerfMaskJob extends TautPlugin {
  static id = 'PerfMaskJob'
  static pluginName = 'Mask a word (perf)'
  static description = 'perf: masks a word in the bench channel'
  static authors = ['clack']
  static category = 'messages'
  static defaultConfig = { enabled: true }
  start() {
    const fix = (t) => (typeof t === 'string' ? t.replace(/job/g, '***') : t)
    const els = (list) => (Array.isArray(list) ? list.map((e) => (e && e.type === 'text' ? { ...e, text: fix(e.text) } : e && e.elements ? { ...e, elements: els(e.elements) } : e)) : list)
    const mask = (ts, m) => (m && typeof m.text === 'string' && m.text.includes('job') ? { ...m, text: fix(m.text), blocks: els(m.blocks) } : m)
    const { patchSlice, mapEntries } = this.api.redux
    patchSlice('messages', (ch, bucket) => (ch === '` + fakeslack.BenchChannelID + `' && bucket && typeof bucket === 'object' ? mapEntries(bucket, mask) : bucket))
  }
}
return PerfMaskJob
})()`)

// perfMaskedEditLatency is perfEditLatency for a row the plugins change:
// a word a plugin masks (perfMaskJS, installed for the pass) is in each
// edit, so the row draws the plugins' view on both trees, and each edit
// is timed to the first frame that draws its masked text. perfEditLatency
// isn't like for like across trees: where a row goes back to its source
// once the plugins leave its message as it is, its row draws the edit at
// once, and where it stays on the view it waits for JS.
func (e *benchEnv) perfMaskedEditLatency(t *testing.T, n int) (ms []float64, viewed []bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	id, err := e.tt.Import(ctx, perfMaskJS, "")
	if err == nil {
		err = e.tt.SetEnabled(ctx, id, true)
	}
	if err != nil {
		t.Logf("masked edit latency: the mask plugin: %v", err)
		return nil, nil
	}
	defer func() { _ = e.tt.SetEnabled(context.Background(), id, false) }()
	running := func() bool {
		for _, c := range e.tt.Settings().Categories {
			for _, p := range c.Plugins {
				if p.ID == id {
					return p.Running
				}
			}
		}
		return false
	}
	for dl := time.Now().Add(10 * time.Second); !running(); {
		if time.Now().After(dl) {
			t.Log("masked edit latency: the mask plugin didn't start in 10 s")
			return nil, nil
		}
		e.frame()
		time.Sleep(5 * time.Millisecond)
	}
	edit, drawn, row, ok := e.perfEditTarget(t, "masked edit latency")
	if !ok {
		return nil, nil
	}
	nonce := time.Now().UnixNano()
	edit(fmt.Sprintf("the job fair of the masked pass %d", nonce))
	if !drawn(fmt.Sprintf("the *** fair of the masked pass %d", nonce), 10*time.Second) {
		t.Log("masked edit latency: the first edit was never drawn masked")
		return nil, nil
	}
	e.settle(1500 * time.Millisecond)
	for i := 0; i < n; i++ {
		_, v, _ := row()
		t0 := time.Now()
		edit(fmt.Sprintf("the job fair %d of the masked pass %d", i, nonce))
		ok := drawn(fmt.Sprintf("the *** fair %d of the masked pass %d", i, nonce), 10*time.Second)
		d := float64(time.Since(t0).Microseconds()) / 1000
		if !ok {
			d = -1
		}
		ms, viewed = append(ms, d), append(viewed, v)
		e.settle(700 * time.Millisecond)
	}
	t.Logf("masked edit latency (ms, the row on the view): %v %v", ms, viewed)
	return ms, viewed
}

// perfEditTarget finds a message of someone else's on screen in the
// bench channel and returns how to edit it (as the socket brings an
// edit), wait until a frame draws text in its row, and read the row (its
// drawn text and whether it draws the plugins' view).
func (e *benchEnv) perfEditTarget(t *testing.T, what string) (edit func(text string), drawn func(text string, d time.Duration) bool,
	row func() (text string, isViewed, ok bool), ok bool) {
	ch := fakeslack.BenchChannelID
	e.openBench()
	e.toBottom()
	e.settle(500 * time.Millisecond)
	c := e.a.convos[ch+"/"]
	if c == nil {
		t.Log(what + ": no bench list")
		return nil, nil, nil, false
	}
	cl, isList := c.list.(channelList)
	if !isList || cl.ChannelView == nil {
		t.Log(what + ": not a channel list")
		return nil, nil, nil, false
	}
	self := e.a.data.core.S.SelfID()
	ts, user := "", ""
	byTS := map[string]slack.Message{}
	if tl := e.a.data.core.S.Timeline(ch); tl != nil {
		for _, m := range tl.Msgs {
			byTS[m.TS] = m
		}
	}
	rows := cl.VisibleRows()
	for i := len(rows) - 1; i >= 0 && ts == ""; i-- {
		if m, ok := byTS[rows[i].Key]; ok && m.User != "" && m.User != self && m.Subtype == "" && m.BotID == "" {
			ts, user = m.TS, m.User
		}
	}
	if ts == "" {
		t.Log(what + ": no message of someone else's on screen")
		return nil, nil, nil, false
	}
	row = func() (text string, isViewed, ok bool) {
		s := reflect.ValueOf(cl.ChannelView).Elem().FieldByName("s")
		if !s.IsValid() || s.IsNil() {
			return "", false, false
		}
		rs := s.Elem().FieldByName("rows")
		if !rs.IsValid() {
			return "", false, false
		}
		r := rs.MapIndex(reflect.ValueOf(ts))
		if !r.IsValid() || r.IsNil() {
			return "", false, false
		}
		m, v := r.Elem().FieldByName("m"), r.Elem().FieldByName("viewed")
		if !m.IsValid() || !v.IsValid() {
			return "", false, false
		}
		return m.FieldByName("Text").String(), v.Bool(), true
	}
	tr := &perfTraffic{e: e}
	edit = func(text string) {
		evTS := liveTS()
		tr.inject(ch, "message", "message_changed", map[string]any{"hidden": true, "ts": evTS, "event_ts": evTS, "message": map[string]any{
			"type": "message", "user": user, "ts": ts, "text": text, "edited": map[string]any{"user": user, "ts": evTS}}})
	}
	drawn = func(text string, d time.Duration) bool {
		for t0 := time.Now(); time.Since(t0) < d; {
			e.frame()
			if txt, _, _ := row(); strings.Contains(txt, text) {
				return true
			}
			time.Sleep(time.Millisecond)
		}
		return false
	}
	return edit, drawn, row, true
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
func (e *benchEnv) perfIdle(name string, d time.Duration, every bool, traffic *perfTraffic) perfPass {
	wake := make(chan struct{}, 1)
	e.a.images.SetInvalidate(func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	})
	defer e.a.images.SetInvalidate(func() {})
	// and at the window's other repaints (App.Invalidate: data, a plugin's
	// tree, posted work), as the window draws one for each; who asked, for
	// the log
	var srcMu sync.Mutex
	sources := map[string]int{}
	prevHook := invalidateHook
	invalidateHook = func(a *App) {
		if prevHook != nil {
			prevHook(a)
		}
		if a == e.a {
			pcs := make([]uintptr, 16)
			fr := runtime.CallersFrames(pcs[:runtime.Callers(3, pcs)]) // past Invalidate
			var names []string
			for len(names) < 4 {
				f, more := fr.Next()
				names = append(names, strings.TrimPrefix(f.Function, "github.com/some-du6e/clack/internal/"))
				if !more {
					break
				}
			}
			srcMu.Lock()
			sources[strings.Join(names, " < ")]++
			srcMu.Unlock()
			select {
			case wake <- struct{}{}:
			default:
			}
		}
	}
	defer func() { invalidateHook = prevHook }()
	defer func() {
		srcMu.Lock()
		defer srcMu.Unlock()
		for _, x := range topSources(sources, 8) {
			e.t.Logf("pass %s: %3d× Invalidate from %s", name, x.n, x.k)
		}
	}()
	// the pass's Go→JS renders by component: for a root not rendered
	// before why (perfRootReg), for one rendered again what in its props
	// changed (top-level keys, msg's one level down)
	if perfReg == nil {
		perfReg = newPerfRootReg()
		slackc.SetBridge(perfRegBridge{countingBridge{e.tt.Bridge(), &e.calls}, perfReg})
	}
	rl := newPerfRenderLog(80)
	perfReg.start(rl)
	ep0, fetch0 := perfViewStats(e)
	defer func() {
		perfReg.stop()
		ep1, fetch1 := perfViewStats(e)
		e.t.Logf("pass %s: taut's view epoch moved %d times, %d view fetches", name, ep1-ep0, fetch1-fetch0)
		for _, line := range rl.top() {
			e.t.Logf("pass %s: %s", name, line)
		}
		for _, line := range rl.lines { // every pass that rendered: what changed
			e.t.Logf("pass %s render: %s", name, line)
		}
		perfReg.keepProps(false)
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
	tasks0, unm0, polls0 := perfJSTasks(e), e.calls.unmount.Load(), perfPollCount()
	var ms0, ms1 runtime.MemStats
	runtime.ReadMemStats(&ms0)
	c0 := procCPU()
	if traffic != nil {
		traffic.start()
	}
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
	events := 0
	if traffic != nil {
		events = traffic.stop()
	}
	tasks := perfJSTasks(e) - tasks0
	unm := e.calls.unmount.Load() - unm0
	tasks -= unm // the sweep's (Unmounts)
	polls := perfPollCount() - polls0
	backlog := perfDrain(e)
	cpu := procCPU() - c0
	pollsCPU := perfPollCount() - polls0
	if pollsCPU != 0 {
		e.t.Logf("WARNING: pass %s: %d of core's polls landed in it (the alignment didn't hold): its frames, renders and CPU have the poll's in them", name, pollsCPU)
	}
	runtime.ReadMemStats(&ms1)
	trees1, same1 := treeCounts()
	calls := e.calls.snapshot()
	st1 := e.a.images.Stats()
	n := float64(max(len(ds), 1))
	p := perfPass{Name: name, Frames: len(ds), CPUMS: float64(cpu.Microseconds()) / 1000,
		Trees: trees1 - trees0 - (same1 - same0), TreesSame: same1 - same0, Renders: calls[0] - calls0[0] + calls[1] - calls0[1],
		Allocs: math.Round(float64(ms1.Mallocs-ms0.Mallocs) / n), AllocKB: math.Round(float64(ms1.TotalAlloc-ms0.TotalAlloc) / 1024 / n),
		GCs: int(ms1.NumGC - ms0.NumGC), GCPause: float64(ms1.PauseTotalNs-ms0.PauseTotalNs) / 1e6,
		Seconds: time.Since(start).Seconds(), AnimFrames: int(st1.Advances - st0.Advances), Players: st1.Players, Quiet: quiet, Why: why,
		Events: events, Tasks: tasks, Unmounts: unm, Polls: polls, PollsCPU: pollsCPU, BacklogMS: float64(backlog.Microseconds()) / 1000,
		ListLive: perfListLive(e)}
	srcMu.Lock()
	p.Sources = map[string]int{}
	for _, kv := range topSources(sources, 8) {
		p.Sources[kv.k] = kv.n
	}
	srcMu.Unlock()
	secs := max(p.Seconds, 0.001)
	fps, tps, rps := float64(len(ds))/secs, float64(tasks)/secs, float64(p.Renders)/secs
	p.FPS, p.TasksPS, p.RendersPS = &fps, &tps, &rps
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

// perfReg is the run's perfRootReg (TestPerfPluginScroll).
var perfReg *perfRootReg

// perfRegBridge is countingBridge recording in a perfRootReg too (here,
// not in countingBridge: the perf job runs this file on the base tree
// too, with that tree's plugin_bench_test.go).
type perfRegBridge struct {
	countingBridge
	reg *perfRootReg
}

func (b perfRegBridge) Render(root uint64, name string, props []byte) {
	b.reg.note(root, 0, 0, name, props)
	b.countingBridge.Render(root, name, props)
}

func (b perfRegBridge) RenderIn(root, parent uint64, parentK int64, name string, props []byte) {
	b.reg.note(root, parent, parentK, name, props)
	b.countingBridge.RenderIn(root, parent, parentK, name, props)
}

func (b perfRegBridge) Unmount(root uint64) {
	b.reg.unmount(root)
	b.countingBridge.Unmount(root)
}

// perfRootReg records every plugin root the run renders and unmounts
// (perfRegBridge), so an idle pass can tell why a root it renders wasn't
// there before: one never drawn, or one mounted again (a root of the same
// component and key unmounted first, its parent mounted again, or its
// parent's tree re-keyed the "@" node it hangs under).
type perfRootReg struct {
	mu    sync.Mutex
	roots map[uint64]*perfRoot
	byKey map[string]uint64 // a root's logical key → its latest root
	gone  map[uint64]bool
	log   *perfRenderLog // the idle pass running, if any
	t0    time.Time      // its start
	keep  bool           // keep props with no log (keepProps)
}

type perfRoot struct {
	name, key, logical string
	parent             uint64
	parentK            int64
	hash               uint64 // of the last props sent
	props              []byte // and the props, while a pass logs (nil otherwise: the scroll passes pay no copy)
}

func newPerfRootReg() *perfRootReg {
	return &perfRootReg{roots: map[uint64]*perfRoot{}, byKey: map[string]uint64{}, gone: map[uint64]bool{}}
}

// propsKey is props' "$key" as JSON text, found without decoding them.
func propsKey(props []byte) string {
	i := bytes.Index(props, []byte(`"$key":`))
	if i < 0 {
		return ""
	}
	rest := props[i+len(`"$key":`):]
	j := 0
	for j < len(rest) && rest[j] != ',' && rest[j] != '}' {
		j++
	}
	return string(rest[:j])
}

// rootKey tells a top-level root's props apart: the message it draws
// (channelId and msg.ts), else its "$key". slackc drops a root's own
// "$key" before the bridge sees its props, so keyed by that alone every
// MessageWrapper, MessageText or Blocks root shared one key, and each
// mount read as "a second root".
func rootKey(props []byte) string {
	var p struct {
		Channel string `json:"channelId"`
		TS      string `json:"ts"`
		Msg     struct {
			TS      string `json:"ts"`
			Channel string `json:"channel"`
		} `json:"msg"`
	}
	if json.Unmarshal(props, &p) == nil {
		ch, ts := p.Channel, p.Msg.TS
		if ch == "" {
			ch = p.Msg.Channel
		}
		if ts == "" {
			ts = p.TS
		}
		if ch != "" || ts != "" {
			return ch + "/" + ts
		}
	}
	return propsKey(props)
}

func (g *perfRootReg) note(root, parent uint64, parentK int64, name string, props []byte) {
	g.mu.Lock()
	defer g.mu.Unlock()
	r, seen := g.roots[root]
	why := "again"
	var prev []byte
	var prevHash uint64
	if seen {
		prev, prevHash = r.props, r.hash
	} else {
		// decoded once per root: the scroll passes pay it for each root
		// they mount, not each render
		key := propsKey(props)
		if parent == 0 {
			key = rootKey(props)
		}
		logical := name + "|" + key
		if parent != 0 {
			pl := "?"
			if p := g.roots[parent]; p != nil {
				pl = p.logical
			}
			logical = pl + ">" + logical
			if key == "" {
				logical += fmt.Sprintf("@%d", parentK)
			}
		}
		r = &perfRoot{name: name, key: key, logical: logical, parent: parent, parentK: parentK}
		why = "new"
		if old, ok := g.byKey[logical]; ok && old != root {
			o := g.roots[old]
			switch {
			case g.gone[old]:
				why = "mounted again after its unmount"
			case o != nil && o.parent != parent:
				why = "mounted again: its parent was"
			case o != nil && o.parentK != parentK:
				why = "mounted again: its parent's tree re-keyed it"
			default:
				why = "mounted again: a second root"
			}
		}
		g.roots[root] = r
		g.byKey[logical] = root
	}
	hh := fnv.New64a()
	hh.Write(props)
	r.hash = hh.Sum64()
	r.props = nil
	if g.keep || g.log != nil && !g.log.countOnly {
		r.props = append([]byte(nil), props...)
	}
	if l := g.log; l != nil {
		l.note(root, r, why, prev, seen && prevHash == r.hash, props, time.Since(g.t0))
	}
}

// keepProps has the registry keep each root's last props (from a
// quiesce on, for the idle pass after it to diff against).
func (g *perfRootReg) keepProps(on bool) {
	g.mu.Lock()
	g.keep = on
	g.mu.Unlock()
}

func (g *perfRootReg) unmount(root uint64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.gone[root] = true
	if r := g.roots[root]; r != nil {
		r.props = nil
		if g.log != nil {
			g.log.unmounted(r)
		}
	}
}

// start has the idle pass's renders logged in l; stop ends it.
func (g *perfRootReg) start(l *perfRenderLog) {
	g.mu.Lock()
	g.log, g.t0 = l, time.Now()
	g.mu.Unlock()
}

func (g *perfRootReg) stop() {
	g.mu.Lock()
	g.log = nil
	g.mu.Unlock()
}

// perfRenderLog is an idle pass's renders (perfRootReg): how many per
// component; for a root not rendered before, why (perfRootReg.note); for
// one rendered again, what in its props changed since its last render, or
// that nothing did; and the roots unmounted. The first renders are listed
// one by one.
type perfRenderLog struct {
	countOnly bool // counts by component and why, no props diffed (the scroll passes)
	names     map[string]int
	keys      map[string]int
	whys      map[string]int // why × component, for roots not rendered before
	same      map[string]int // components rendered again with the very props
	unmounts  map[string]int
	lines     []string
	maxLines  int
	nRenders  int
	nAgain    int
	nNewRoots int
}

func newPerfRenderLog(lines int) *perfRenderLog {
	return &perfRenderLog{names: map[string]int{}, keys: map[string]int{},
		whys: map[string]int{}, same: map[string]int{}, unmounts: map[string]int{}, maxLines: lines}
}

// note logs a render of root, whose props were prev before (nil: none
// recorded); perfRootReg's lock held.
func (l *perfRenderLog) note(root uint64, r *perfRoot, why string, prev []byte, sameHash bool, props []byte, at time.Duration) {
	l.nRenders++
	l.names[r.name]++
	var paths []string
	switch {
	case why != "again":
		l.nNewRoots++
		l.whys[why+": "+r.name]++
	case prev == nil && sameHash: // last sent before the pass logged: only its hash kept
		l.nAgain++
		l.same[r.name]++
	case l.countOnly:
		l.nAgain++
		l.whys["rendered again: "+r.name]++
	case prev != nil:
		l.nAgain++
		paths = perfJSONDiff(r.name, prev, props, 16)
		if len(paths) == 0 {
			l.same[r.name]++
		}
		for _, path := range paths {
			l.keys[path]++
		}
	default:
		l.nAgain++
		l.whys["rendered again, props unknown: "+r.name]++
	}
	if len(l.lines) < l.maxLines {
		if len(paths) > 3 {
			paths = append(paths[:3:3], "…")
		}
		line := fmt.Sprintf("%6.0f ms %s %s key %s root %d", float64(at.Microseconds())/1000, r.name, why, r.key, root)
		if r.parent != 0 {
			line += fmt.Sprintf(" in %d@%d", r.parent, r.parentK)
		}
		if len(paths) > 0 {
			line += " changed " + strings.Join(paths, ", ")
		}
		l.lines = append(l.lines, line)
	}
}

func (l *perfRenderLog) unmounted(r *perfRoot) { l.unmounts[r.name]++ }

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

// top lists the components rendered most, why roots weren't there before,
// the props that changed most, the roots rendered with the very props they
// had, and the roots unmounted.
func (l *perfRenderLog) top() []string {
	if l.nRenders == 0 && len(l.unmounts) == 0 {
		return nil
	}
	list := func(m map[string]int, max int) string {
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
			if i == max {
				fmt.Fprintf(&b, " (+%d more)", len(s)-max)
				break
			}
			fmt.Fprintf(&b, " %s×%d", x.k, x.n)
		}
		return b.String()
	}
	return []string{
		fmt.Sprintf("%d renders: %d of roots not rendered before, %d of roots rendered before; by component:%s", l.nRenders, l.nNewRoots, l.nAgain, list(l.names, 14)),
		"renders of roots not rendered before, why:" + list(l.whys, 16),
		fmt.Sprintf("changed props (%d paths):%s", len(l.keys), list(l.keys, 20)),
		"rendered again with the very props:" + list(l.same, 10),
		"unmounted:" + list(l.unmounts, 10),
	}
}

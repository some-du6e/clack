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
//
// Synthetic data only; nothing reaches Slack. It uses nothing newer than
// the plugin bench, so the perf workflow runs this same file on the
// baseline tree for the "before" column.

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"runtime/pprof"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
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
	// Slow are the frames over 40 ms: what the frame goroutine was doing
	// 40 and 120 ms in (function names), with what landed meanwhile
	Slow []perfSlow `json:"slow,omitempty"`
}

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
	Plugins  []string   `json:"plugins"`
	Failed   []string   `json:"failed,omitempty"`
	Patched  []string   `json:"patched"`
	Messages int        `json:"messages"`
	Replies  int        `json:"replies"`
	Visited  int        `json:"visited"`
	HeapMB   float64    `json:"heap_mb"` // live heap after setup
	Passes   []perfPass `json:"passes"`
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
	res.HeapMB = liveHeapMB()
	t.Logf("%d messages loaded, %d channels visited, live heap %.0f MB", res.Messages, res.Visited, res.HeapMB)

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
		// time, as near cmd/clack's limit
		runtime.GC()
		lim := int64(liveHeapMB()*1.25*(1<<20)) + 32<<20
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
// channels a message every 250 ms. As realtime events through core's
// handler, from their own goroutine like the socket's.
type perfTraffic struct {
	e          *benchEnv
	ch, thread string
	others     []string

	quit chan struct{}
	done sync.WaitGroup
	n    atomic.Int64
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
	var lastType, lastMsg, lastReact, lastEdit, lastDel, lastOther time.Duration
	seq := 0
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
			ev := map[string]any{"user": u, "text": "live message " + ts, "ts": ts, "event_ts": ts, "client_msg_id": "live-" + ts}
			if p.thread != "" {
				ev["thread_ts"] = p.thread
			}
			p.inject(p.ch, "message", "", ev)
			targets = append(targets, ts)
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

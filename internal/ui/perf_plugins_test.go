package ui

// Scroll frames under a plugin set like Karim's (perf workflow, jobs:
// plugins): the Library's Taut plugins that are on by default plus Slick's
// Message Logger, run unmodified from the published `plugins` release, in
// the whole client over internal/fakeslack (the plugin bench's
// workspace). Together they patch what the freeze reports of 2026-11-15
// showed the UI goroutine stuck in: MessageListItem, MessageWrapper,
// MessageText, Blocks, the thread pane's rows, the gutter, MessageSender,
// MessagePaneInput and TypingNames, and the messages slice (patched
// views and injected messages).
//
// The passes, each a wheel step a frame:
//
//	channel       the 500-message #plugin-bench, bottom to top and back
//	channel-live  the same while people type, post, react, edit and
//	              delete in it (realtime events through core.Inject)
//	thread-live   a 300-reply thread in the side pane, with replies and
//	              typing arriving
//
// For each it records every frame's time on the UI goroutine (drain +
// layout; no GPU) and the process CPU. A freeze is one long frame, so the
// report leads with the longest frame and the frames over 50, 100 and 250
// ms, then the median and p95.
//
//	PLUGIN_BENCH_LIBRARY  the `plugins` release's files (index.json + bundles)
//	PERF_PLUGINS          system:id list; "taut:@default" is every Taut
//	                      plugin the Library turns on by default
//	                      (default "taut:@default,slick:MessageLogger")
//	PERF_PLUGIN_OUT       where the JSON result goes
//	PERF_PLUGIN_PROF      a path prefix: a CPU profile per pass
//	                      (<prefix>-<pass>.cpu.pprof) and the mutex and
//	                      block profiles over all passes
//
// Synthetic data only; nothing reaches Slack. It uses nothing newer than
// the plugin bench, so the perf workflow runs this same file on the
// baseline tree for the "before" column.

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
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
	Events   int     `json:"events"`   // realtime events injected
	Trees    int     `json:"trees"`    // host trees delivered
	Renders  int64   `json:"renders"`  // Go→JS Render/RenderIn calls
}

type perfPluginResult struct {
	Plugins  []string   `json:"plugins"`
	Failed   []string   `json:"failed,omitempty"`
	Patched  []string   `json:"patched"`
	Messages int        `json:"messages"`
	Replies  int        `json:"replies"`
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

	fs, err := fakeslack.Start(benchSize)
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

	// a long thread in another channel, served by conversations.replies
	thCh := fs.ChannelIDs()[1]
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
	t.Logf("%d messages loaded", res.Messages)

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
	res.Passes = append(res.Passes, e.perfScroll("channel", e.listPoint, nil))
	live := &perfTraffic{e: e, ch: fakeslack.BenchChannelID}
	res.Passes = append(res.Passes, e.perfScroll("channel-live", e.listPoint, live))

	e.a.OpenThread(thCh, thRoot)
	e.until("the thread pane", 20*time.Second, func() bool { return e.a.secondary.kind == secThread })
	e.settle(1500 * time.Millisecond)
	thPoint := func() image.Point {
		o := e.a.secondaryOrigin
		return image.Pt(o.X+(e.size.X-o.X)/2, o.Y+(e.size.Y-o.Y)/3)
	}
	// to the thread's end first, like toBottom
	for i := 0; i < 60; i++ {
		e.scroll(thPoint(), 4000)
		e.frame()
	}
	e.settle(300 * time.Millisecond)
	thLive := &perfTraffic{e: e, ch: thCh, thread: thRoot}
	res.Passes = append(res.Passes, e.perfScroll("thread-live", thPoint, thLive))

	if prof != "" {
		for _, k := range []string{"mutex", "block"} {
			if f, err := os.Create(prof + "." + k + ".pprof"); err == nil {
				_ = pprof.Lookup(k).WriteTo(f, 0)
				f.Close()
			}
		}
	}
	for _, p := range res.Passes {
		t.Logf("pass %-12s frames %d: max %.1f ms, >50ms %d, >100ms %d, >250ms %d, median %.2f ms, p95 %.2f ms, p99 %.2f ms, total %.0f ms, cpu %.0f ms, events %d, trees %d, renders %d",
			p.Name, p.Frames, p.MaxMS, p.Over50, p.Over100, p.Over250, p.MedianMS, p.P95MS, p.P99MS, p.TotalMS, p.CPUMS, p.Events, p.Trees, p.Renders)
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
// one a frame, timing each frame; with traffic, realtime events arrive
// meanwhile.
func (e *benchEnv) perfScroll(name string, at func() image.Point, traffic *perfTraffic) perfPass {
	runtime.GC()
	trees0, calls0 := e.delivered(), e.calls.snapshot()
	if perfProfPrefix != "" {
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
	c0 := procCPU()
	if traffic != nil {
		traffic.start()
	}
	var ds []float64
	step := func(dy float32) {
		e.scroll(at(), dy)
		t0 := time.Now()
		e.frame()
		ds = append(ds, float64(time.Since(t0).Microseconds())/1000)
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
	e.settle(400 * time.Millisecond) // the JS renders of rows that scrolled in
	cpu := procCPU() - c0
	calls := e.calls.snapshot()
	p := perfPass{Name: name, Frames: len(ds), CPUMS: float64(cpu.Microseconds()) / 1000, Events: events,
		Trees: e.delivered() - trees0, Renders: calls[0] - calls0[0] + calls[1] - calls0[1]}
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

// perfTraffic is a busy conversation: people typing every 250 ms, a new
// message (or thread reply) every 1.5 s, a reaction every 600 ms, an edit
// every 3 s and a delete every 5 s (Message Logger keeps deleted messages:
// injected ones), as realtime events through core's handler.
type perfTraffic struct {
	e          *benchEnv
	ch, thread string

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

func (p *perfTraffic) inject(typ, subtype string, raw map[string]any) {
	raw["type"] = typ
	if subtype != "" {
		raw["subtype"] = subtype
	}
	raw["channel"] = p.ch
	b, _ := json.Marshal(raw)
	chJSON, _ := json.Marshal(p.ch)
	p.e.a.data.core.Inject(slack.Event{Type: typ, Subtype: subtype, Channel: chJSON, Raw: b})
	p.n.Add(1)
}

func (p *perfTraffic) run() {
	defer p.done.Done()
	authors := p.e.c.Authors
	// the messages to react to, edit and delete: what core holds now
	var targets []string
	if tl := p.e.a.data.core.S.Timeline(p.ch); tl != nil {
		for _, m := range tl.Msgs {
			if p.thread == "" || m.ThreadTS == p.thread {
				targets = append(targets, m.TS)
			}
		}
	}
	if p.thread != "" {
		if msgs, ok := p.e.a.data.core.S.Thread(p.ch, p.thread); ok {
			targets = targets[:0]
			for _, m := range msgs {
				targets = append(targets, m.TS)
			}
		}
	}
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	start := time.Now()
	var lastType, lastMsg, lastReact, lastEdit, lastDel time.Duration
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
		if el-lastType >= 250*time.Millisecond {
			lastType = el
			ev := map[string]any{"user": u}
			if p.thread != "" {
				ev["thread_ts"] = p.thread
			}
			p.inject("user_typing", "", ev)
		}
		if el-lastMsg >= 1500*time.Millisecond {
			lastMsg = el
			now := time.Now()
			ts := fmt.Sprintf("%d.%06d", now.Unix(), now.Nanosecond()/1000)
			ev := map[string]any{"user": u, "text": "live message " + ts, "ts": ts, "event_ts": ts, "client_msg_id": "live-" + ts}
			if p.thread != "" {
				ev["thread_ts"] = p.thread
			}
			p.inject("message", "", ev)
			targets = append(targets, ts)
		}
		if len(targets) == 0 {
			continue
		}
		pick := func(k int) string { return targets[(seq*7+k)%len(targets)] }
		if el-lastReact >= 600*time.Millisecond {
			lastReact = el
			p.inject("reaction_added", "", map[string]any{"user": u, "reaction": "eyes", "item": map[string]any{"type": "message", "channel": p.ch, "ts": pick(0)}})
		}
		if el-lastEdit >= 3*time.Second {
			lastEdit = el
			ts := pick(1)
			msg := map[string]any{"type": "message", "user": u, "text": "edited " + ts, "ts": ts, "edited": map[string]any{"user": u, "ts": ts}}
			if p.thread != "" {
				msg["thread_ts"] = p.thread
			}
			p.inject("message", "message_changed", map[string]any{"message": msg, "ts": ts, "event_ts": ts, "hidden": true})
		}
		if el-lastDel >= 5*time.Second {
			lastDel = el
			ts := pick(2)
			p.inject("message", "message_deleted", map[string]any{"deleted_ts": ts, "ts": ts, "event_ts": ts, "hidden": true})
		}
	}
}

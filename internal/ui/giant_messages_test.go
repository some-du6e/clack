package ui

// TestGiantMessagesBounded: #giant-wall (fakeslack Size.Giant, synthetic
// text) holds messages the shape of the ones Heliodex's channel has
// (2026-11-20: opening it took Clack to 13 GB): of its latest 100, 92 are
// each about 908,000 characters of plain text in about 92,000 lines, one
// rich_text block with one rich_text_section holding one text element.
// The test opens it, scrolls through the newest message, pages back to
// the 100th newest and jumps around, and fails when
//
//   - the live Go heap (after a collection) grows past giantHeapMB over
//     what it was before the channel opened, or the process's peak RSS
//     (VmHWM) past giantRSSMB over its RSS then;
//   - the app's part of any frame from the open on (events, layout, ops:
//     all but the GPU renderer's drawing of them) takes longer than
//     giantFrameMax, or a whole frame longer than giantWholeMax;
//   - a giant message doesn't draw what Slack draws. Slack's message pane
//     cuts nothing: Message (boot-shared b5pm) renders Blocks (lby4)
//     without truncateLimits, so MessageBlocks (u33y) and the rich text
//     renderer draw the whole text element, BaseText (modern.vendor KYzC)
//     a <br> for every newline. The row holds the whole text, every line
//     22px tall, and the newest message's last line sits at the bottom.
//
// Skipped unless CLACK_GIANT_MESSAGES is set (flake-check's
// giant-messages job; its value is how many of the latest messages the
// channel holds, 100 when not a number). CLACK_GIANT_OUT names a
// directory for heap profiles (one per stage) and a report.

import (
	"context"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"runtime/debug"
	"runtime/metrics"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
)

const (
	// giantHeapMB bounds the live heap's growth from before the channel
	// opened to any stage's end (CI, 2026-11-20: +308 MB at most, nearly
	// all of it the messages' text and blocks, 1.9 MB each)
	giantHeapMB = 500
	// giantRSSMB bounds the peak RSS's growth over the RSS before the
	// channel opened (+825 MB, the fake in its own process)
	giantRSSMB = 1500
	// giantFrameMax bounds the app's part of every frame from the open on
	// (a frame less its GPU raster: perfEnv.raster). The longest is a
	// giant row laid out again once back on screen (its layout released
	// while away: messages' releaseBigRows), 92,000 lines in 112-299 ms
	// on CI's runners, the same work; before the fixes, 13.4 s.
	giantFrameMax = 500 * time.Millisecond
	// giantWholeMax bounds every whole frame, raster and all. llvmpipe's
	// path stenciling costs about the same for any new screen of text,
	// whatever the messages' size (an ordinary channel's open: up to
	// 124 ms on one runner, the giant channel's screens of nothing but
	// text up to 304 ms on it), and runners differ by half again in it.
	giantWholeMax = time.Second
	// giantAbortMB: past this RSS growth the test stops driving frames
	// (the bound is long gone; the runner keeps its memory).
	giantAbortMB = 6000
	// giantAppMemoryLimit is cmd/clack's soft memory limit (trim.go's
	// memoryLimit, set unless GOMEMLIMIT is): the session runs as the app
	// does, the collector running sooner as the heap nears it, not at
	// twice what's live (the plugin session's peak, without it, 1.5-1.7 GB
	// over its start)
	giantAppMemoryLimit = 768 << 20
	// giantPanicMB: past this RSS the watchdog writes a heap profile and
	// panics, should a single frame run away.
	giantPanicMB = 11000
)

// giantProcStatus reads kB fields of /proc/self/status, in MB.
func giantProcStatus(keys ...string) map[string]float64 {
	out := map[string]float64{}
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return out
	}
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		for _, k := range keys {
			if f[0] == k+":" {
				kb, _ := strconv.Atoi(f[1])
				out[k] = float64(kb) / 1024
			}
		}
	}
	return out
}

// giantHeapObjectsMB is the heap's objects, live and not yet swept (no
// collection, no stop: cheap enough for every frame).
func giantHeapObjectsMB() float64 {
	s := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
	metrics.Read(s)
	if s[0].Value.Kind() != metrics.KindUint64 {
		return 0
	}
	return float64(s[0].Value.Uint64()) / (1 << 20)
}

// giantPauses reads the process's stop-the-world histograms: the
// collector's pauses, and of them the time spent stopping the world (a
// goroutine in the plugin engine's wasm can't be preempted: memory
// gc-stw-waits-on-wasm).
func giantPauses() (total, stopping *metrics.Float64Histogram) {
	s := []metrics.Sample{{Name: "/sched/pauses/total/gc:seconds"}, {Name: "/sched/pauses/stopping/gc:seconds"}}
	metrics.Read(s)
	if s[0].Value.Kind() != metrics.KindFloat64Histogram || s[1].Value.Kind() != metrics.KindFloat64Histogram {
		return nil, nil
	}
	return s[0].Value.Float64Histogram(), s[1].Value.Float64Histogram()
}

// giantLongest is the lower bound of the longest bucket that b counted
// more in than a: the longest pause between the two readings, about.
func giantLongest(a, b *metrics.Float64Histogram) time.Duration {
	if a == nil || b == nil || len(a.Counts) != len(b.Counts) {
		return 0
	}
	for i := len(b.Counts) - 1; i >= 0; i-- {
		if b.Counts[i] > a.Counts[i] {
			if lo := b.Buckets[i]; lo > 0 {
				return time.Duration(lo * float64(time.Second))
			}
			return 0
		}
	}
	return 0
}

type giantFrame struct {
	stage string
	i     int // its index, the CPU profile's giant_frame label
	d     time.Duration
	r     time.Duration // of d, the GPU renderer's
	gc    time.Duration // its longest collector's pause, about
	stop  time.Duration // of that kind, its longest wait for the world to stop
	heap  float64       // heap objects after it, MB
	rss   float64       // VmRSS after it, MB
}

func TestGiantMessagesBounded(t *testing.T) {
	n, out := giantSetup(t)
	fs := giantServer(t, n, true)
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	giantSession(t, e, fs, n, out, nil)
}

// TestGiantMessagesKeywords is the same session with Channel keywords
// set (global_keywords: three of the newest message's own words), which
// every conversion of every message marks (richtext.HighlightDoc): a
// match cuts a raw span in parts that stay raw, so the giant rows keep
// their lazy layout.
func TestGiantMessagesKeywords(t *testing.T) {
	n, out := giantSetup(t)
	fs := giantServer(t, n, true)
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	lines := strings.Split(fakeslack.GiantText(0), "\n")
	if len(lines) < 100 {
		t.Fatalf("the newest giant message has %d lines", len(lines))
	}
	var kws []string
	for _, i := range []int{10, len(lines) / 2, len(lines) - 3} {
		if w := strings.Trim(lines[i], "',.!-"); len(w) >= 4 && !strings.ContainsAny(w, "',.!-") {
			kws = append(kws, w)
		}
	}
	if len(kws) == 0 {
		t.Fatal("no keyword among the newest message's words")
	}
	c := e.a.data.core
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := c.SetGlobalNotifyPref(ctx, "global_keywords", strings.Join(kws, ",")); err != nil {
		t.Fatal(err)
	}
	e.until("the keywords", 30*time.Second, func() bool { return len(c.Prefs().Keywords) == len(kws) })
	t.Logf("keywords %q", kws)
	giantSession(t, e, fs, n, out, nil)
}

// TestGiantMessagesPlugins is the same session under a Taut plugin that
// patches the messages slice (passing it through, as Slick's and Taut's
// message plugins patch it): every row then draws the plugins' view of
// its message, and the mirror hands JS the open channel's messages.
// CLACK_GIANT_PLUGINS=1 too. The fake is fakeslackd in its own process
// when CLACK_FAKESLACK_URL names one, as for TestGiantMessagesBounded: its
// own copy of the messages and the responses it encodes aren't the
// client's memory (in this process they were 300 MB live and a gigabyte
// of garbage the session).
func TestGiantMessagesPlugins(t *testing.T) {
	n, out := giantSetup(t)
	if os.Getenv("CLACK_GIANT_PLUGINS") == "" {
		t.Skip("CLACK_GIANT_PLUGINS not set (flake-check's giant-messages job runs this)")
	}
	fs := giantServer(t, n, true)
	be := benchStart(t, fs)
	e := be.perfEnv
	e.waitReady()
	e.until("Taut", 60*time.Second, func() bool { return e.a.Taut() != nil })
	tt := e.a.Taut()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := tt.Import(ctx, []byte(editPassSlice), ""); err != nil {
		t.Fatal(err)
	}
	if err := tt.SetEnabled(ctx, "EditPassSlice", true); err != nil {
		t.Fatal(err)
	}
	e.until("the messages slice patched", 30*time.Second, func() bool { return tt.SlicePatched("messages") })
	giantSession(t, e, fs, n, out, func() string { return fmt.Sprintf("plugin engine %.0f MB", perfOffHeapWasmMB()) })
}

// giantSetup reads the job's settings: how many latest messages
// #giant-wall holds, and where the profiles and report go.
func giantSetup(t *testing.T) (n int, out string) {
	if os.Getenv("CLACK_GIANT_MESSAGES") == "" {
		t.Skip("CLACK_GIANT_MESSAGES not set (flake-check's giant-messages job runs this)")
	}
	n = 100
	if v, err := strconv.Atoi(os.Getenv("CLACK_GIANT_MESSAGES")); err == nil && v > 0 {
		n = v
	}
	out = os.Getenv("CLACK_GIANT_OUT")
	if out != "" {
		out = filepath.Join(out, t.Name())
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return n, out
}

// giantServer is the fake: fakeslackd at CLACK_FAKESLACK_URL when remote
// may be (its memory then isn't the client's), else in this process.
func giantServer(t *testing.T, n int, remote bool) *fakeslack.Server {
	sz := fakeslack.Small
	sz.Giant = n
	if u := os.Getenv("CLACK_FAKESLACK_URL"); u != "" && remote { // fakeslackd -small -giant n
		return fakeslack.Remote(u, sz)
	}
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	return fs
}

// giantSession opens #giant-wall in e and measures it (see
// TestGiantMessagesBounded); extra, if set, adds to each stage's line.
func giantSession(t *testing.T, e *perfEnv, fs *fakeslack.Server, n int, out string, extra func() string) {
	ch := fakeslack.GiantWallID
	profile := func(name string) {
		if out == "" {
			return
		}
		f, err := os.Create(filepath.Join(out, name+".heap.pprof"))
		if err != nil {
			t.Log(err)
			return
		}
		defer f.Close()
		_ = pprof.Lookup("heap").WriteTo(f, 0)
	}
	if os.Getenv("GOMEMLIMIT") == "" {
		old := debug.SetMemoryLimit(giantAppMemoryLimit)
		defer debug.SetMemoryLimit(old)
	}
	// the reference, for the report: a channel of ordinary messages opened
	// the same way, each frame timed. A frame that draws a new screen of
	// text costs the renderer (llvmpipe on the runners: mostly its path
	// stenciling) about as much whatever the messages' size.
	ref := fs.ChannelIDs()[1]
	e.a.Navigate("c/" + ref)
	var refMax, refRaster time.Duration
	for dl := time.Now().Add(3 * time.Second); time.Now().Before(dl); {
		t0 := time.Now()
		e.frame()
		if d := time.Since(t0); d > refMax {
			refMax, refRaster = d, e.raster
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Logf("reference: #%s's open, the longest frame %v (the GPU renderer's %v)", ref, refMax.Round(time.Millisecond), refRaster.Round(time.Millisecond))
	baseHeap := liveHeapMB()
	_ = os.WriteFile("/proc/self/clear_refs", []byte("5"), 0) // VmHWM from here (Linux)
	baseRSS := giantProcStatus("VmRSS")["VmRSS"]
	profile("0-before")
	t.Logf("before #giant-wall: live heap %.0f MB, RSS %.0f MB", baseHeap, baseRSS)

	// the watchdog: one frame that runs away can't take the runner with it
	// (its panic shows every goroutine, the UI's frame among them)
	debug.SetTraceback("all")
	stop := make(chan struct{})
	defer close(stop)
	// frameAt is when the frame being laid out began (unix ns, 0 between
	// frames), frameOf its stage
	var frameAt atomic.Int64
	var frameOf atomic.Pointer[string]
	go func() {
		tk := time.NewTicker(50 * time.Millisecond)
		defer tk.Stop()
		over := false
		for {
			select {
			case <-stop:
				return
			case <-tk.C:
			}
			rss := giantProcStatus("VmRSS")["VmRSS"]
			if g := rss - baseRSS; g > giantRSSMB && !over {
				over = true
				t.Errorf("RSS %.0f MB, %.0f MB over the %.0f MB before the channel opened (bound %d MB)", rss, g, baseRSS, giantRSSMB)
			}
			if rss > giantPanicMB {
				if at := frameAt.Load(); at != 0 {
					stage := "a"
					if s := frameOf.Load(); s != nil {
						stage = *s
					}
					t.Errorf("a %s frame has run for %v and counting (bound %v)", stage, time.Since(time.Unix(0, at)).Round(time.Millisecond), giantWholeMax)
				}
				profile("watchdog")
				panic(fmt.Sprintf("giant messages: RSS %.0f MB passed %d MB in one frame (before the channel: %.0f MB)", rss, giantPanicMB, baseRSS))
			}
		}
	}()

	var frames []giantFrame
	aborted := ""
	deadline := time.Now().Add(12 * time.Minute)
	step := func(stage string) bool {
		if aborted != "" {
			return false
		}
		if time.Now().After(deadline) {
			aborted = fmt.Sprintf("%s: still driving frames 12 minutes on; stopped", stage)
			return false
		}
		pt0, ps0 := giantPauses()
		i := len(frames)
		t0 := time.Now()
		frameOf.Store(&stage)
		frameAt.Store(t0.UnixNano())
		pprof.Do(context.Background(), pprof.Labels("giant_frame", strconv.Itoa(i)), func(context.Context) { e.frame() })
		frameAt.Store(0)
		d := time.Since(t0)
		pt1, ps1 := giantPauses()
		st := giantProcStatus("VmRSS")
		frames = append(frames, giantFrame{stage: stage, i: i, d: d, r: e.raster, gc: giantLongest(pt0, pt1), stop: giantLongest(ps0, ps1), heap: giantHeapObjectsMB(), rss: st["VmRSS"]})
		if g := st["VmRSS"] - baseRSS; g > giantAbortMB {
			aborted = fmt.Sprintf("%s: RSS %.0f MB, %.0f MB over the %.0f MB before the channel opened; stopped driving frames", stage, st["VmRSS"], g, baseRSS)
		}
		time.Sleep(2 * time.Millisecond)
		return aborted == ""
	}
	settle := func(stage string, d time.Duration) {
		for dl := time.Now().Add(d); time.Now().Before(dl) && step(stage); {
		}
	}
	list := func() *channelList {
		if c := e.a.convos[ch+"/"]; c != nil {
			if l, ok := c.list.(channelList); ok {
				return &l
			}
		}
		return nil
	}
	type stageMem struct {
		name      string
		heap, rss float64
	}
	var mems []stageMem
	measure := func(name string) {
		if aborted != "" {
			return
		}
		h := liveHeapMB()
		r := giantProcStatus("VmRSS")["VmRSS"]
		mems = append(mems, stageMem{name, h, r})
		profile(name)
		shown := 0
		if l := list(); l != nil {
			shown, _, _ = l.LayoutSig()
		}
		more := ""
		if extra != nil {
			more = ", " + extra()
		}
		t.Logf("%s: live heap %.0f MB (+%.0f), RSS %.0f MB (+%.0f), %d messages in the list (core's window %d)%s", name, h, h-baseHeap, r, r-baseRSS, shown, e.loadedMessages(ch), more)
	}

	// a CPU profile of everything from the open on
	if out != "" {
		if f, err := os.Create(filepath.Join(out, "frames.cpu.pprof")); err == nil {
			if pprof.StartCPUProfile(f) == nil {
				defer func() { pprof.StopCPUProfile(); f.Close() }()
			}
		}
	}
	// 1. open it: the newest 28 messages (it opens at the "New" line: the
	// whole channel is unread)
	e.a.Navigate("c/" + ch)
	t0 := time.Now()
	for dl := time.Now().Add(90 * time.Second); time.Now().Before(dl) && step("open"); {
		if l := list(); l != nil {
			if msgs, loaded, _ := l.LayoutSig(); loaded && msgs > 0 && len(l.VisibleRows()) > 0 {
				break
			}
		}
	}
	t.Logf("open: first rows after %v", time.Since(t0).Round(time.Millisecond))
	settle("open", 2*time.Second)
	measure("1-open")

	// what Slack draws: the whole newest message, 22px a line, its last
	// line at the bottom
	newest := fakeslack.GiantTS(0)
	if l := list(); l != nil && aborted == "" {
		l.ScrollToBottom()
		settle("bottom", time.Second)
	}
	if aborted == "" {
		if l := list(); l == nil {
			t.Errorf("no list for #giant-wall")
		} else {
			want := fakeslack.GiantText(0)
			if got := l.RowText(newest); got != want {
				t.Errorf("the newest message's row holds %d bytes of text, %d lines; Slack draws all %d bytes, %d lines (BaseText: a <br> a newline, nothing cut)",
					len(got), strings.Count(got, "\n")+1, len(want), strings.Count(want, "\n")+1)
			}
			lines := strings.Count(want, "\n") + 1
			rows := l.VisibleRows()
			top, next := 0, -1
			for i, r := range rows {
				if r.Key == newest {
					top = r.Y
					if i+1 < len(rows) {
						next = rows[i+1].Y
					}
				}
			}
			if next < 0 {
				t.Errorf("the newest message isn't followed on screen by the list's end (rows %v)", rows)
			} else if h := next - top; h < lines*22 || h > lines*22+200 {
				t.Errorf("the newest message's row is %d px tall; Slack's is its header and %d lines of 22px (%d px)", h, lines, lines*22)
			}
		}
	}

	// what's on screen is drawn: in the middle of the newest message every
	// 22px band of the text column has ink (the GPU renderer's frame)
	inked := func(what string) {
		l := list()
		if e.gpu == nil || l == nil || aborted != "" {
			return
		}
		img := image.NewRGBA(image.Rect(0, 0, e.size.X, e.size.Y))
		if err := e.gpu.Screenshot(img); err != nil {
			t.Errorf("%s: screenshot: %v", what, err)
			return
		}
		luma := func(x, y int) int {
			c := img.RGBAAt(x, y)
			return (299*int(c.R) + 587*int(c.G) + 114*int(c.B)) / 1000
		}
		x0, y0 := l.Origin.X+60, l.Origin.Y+40
		bands, blank := 0, 0
		for y := y0; y+22 <= y0+440 && y+22 <= e.size.Y; y += 22 {
			bands++
			bg := luma(x0-30, y+11) // the gutter: no avatar in the middle of a message
			ink := 0
			for yy := y; yy < y+22; yy++ {
				for x := x0; x < x0+240; x++ {
					if d := luma(x, yy) - bg; d > 80 || d < -80 {
						ink++
					}
				}
			}
			if ink < 10 {
				blank++
			}
		}
		if blank > 1 {
			t.Errorf("%s: %d of %d 22px bands of the message's text on screen drew nothing", what, blank, bands)
		}
	}
	if l := list(); l != nil && aborted == "" {
		l.ScrollPx(-20000)
		settle("scroll", 500*time.Millisecond)
		inked("20,000px up the newest message")
		l.ScrollPx(-1500)
		step("scroll")
		inked("a jump of 1,500px")
	}

	// 2. up through the newest message and back down
	for i := 0; i < 80 && step("scroll"); i++ {
		e.scroll(e.listPoint(), -2500)
	}
	for i := 0; i < 80 && step("scroll"); i++ {
		e.scroll(e.listPoint(), 2500)
	}
	settle("scroll", time.Second)
	measure("2-scrolled")

	// 3. page back to the 100th newest (two pages more), and to the middle
	if l := list(); l != nil && aborted == "" {
		l.Highlight(fakeslack.GiantTS(min(n, 100) - 1))
		for dl := time.Now().Add(120 * time.Second); time.Now().Before(dl) && step("page back") && l.JumpPending() != ""; {
		}
		if l.JumpPending() != "" && aborted == "" {
			t.Errorf("the jump to the 100th newest message is still pending after 2 minutes (%d messages loaded)", e.loadedMessages(ch))
		}
		settle("page back", 2*time.Second)
		measure("3-paged")
		for _, k := range []int{60, 30, 2} {
			l.Highlight(fakeslack.GiantTS(k))
			settle(fmt.Sprintf("jump %d", k), 1500*time.Millisecond)
		}
		l.ScrollToBottom()
		settle("bottom", 2*time.Second)
		measure("4-jumped")
	}

	// 4. left: the rows' layouts go once idle (rowIdle), the messages stay
	e.openChannel(fs.ChannelIDs()[1])
	settle("left", 2*time.Second)
	measure("5-left")

	// the report
	var worst []giantFrame
	var total time.Duration
	for _, f := range frames {
		total += f.d
	}
	worst = append(worst, frames...)
	sort.Slice(worst, func(i, j int) bool { return worst[i].d > worst[j].d })
	if len(worst) > 12 {
		worst = worst[:12]
	}
	// the app's longest parts of a frame
	var app []giantFrame
	app = append(app, frames...)
	sort.Slice(app, func(i, j int) bool { return app[i].d-app[i].r > app[j].d-app[j].r })
	if len(app) > 5 {
		app = app[:5]
	}
	hwm := giantProcStatus("VmHWM")["VmHWM"]
	var b strings.Builder
	fmt.Fprintf(&b, "## Giant messages (#giant-wall, %d latest)\n\n", n)
	fmt.Fprintf(&b, "before the channel: live heap %.0f MB, RSS %.0f MB; peak RSS %.0f MB (VmHWM), %d frames, %v in frames\n\n", baseHeap, baseRSS, hwm, len(frames), total.Round(time.Millisecond))
	fmt.Fprintf(&b, "| stage | live heap | RSS |\n|---|---|---|\n")
	for _, m := range mems {
		fmt.Fprintf(&b, "| %s | %.0f MB (+%.0f) | %.0f MB (+%.0f) |\n", m.name, m.heap, m.heap-baseHeap, m.rss, m.rss-baseRSS)
	}
	fmt.Fprintf(&b, "\nlongest frames (an ordinary channel's open: %v, the GPU renderer's %v):\n\n", refMax.Round(time.Millisecond), refRaster.Round(time.Millisecond))
	for _, f := range worst {
		fmt.Fprintf(&b, "- %s (frame %d): %v, the GPU renderer's %v, a collector's pause of %v or more (%v of it stopping the world); heap objects %.0f MB, RSS %.0f MB\n", f.stage, f.i, f.d.Round(time.Millisecond), f.r.Round(time.Millisecond), f.gc.Round(time.Millisecond), f.stop.Round(time.Millisecond), f.heap, f.rss)
	}
	fmt.Fprintf(&b, "\nthe app's longest parts of a frame (less the GPU renderer's):\n\n")
	for _, f := range app {
		fmt.Fprintf(&b, "- %s (frame %d): %v of %v\n", f.stage, f.i, (f.d - f.r).Round(time.Millisecond), f.d.Round(time.Millisecond))
	}
	t.Log("\n" + b.String())
	if out != "" {
		_ = os.WriteFile(filepath.Join(out, "report.md"), []byte(b.String()), 0o644)
	}

	if aborted != "" {
		t.Errorf("%s", aborted)
	}
	for _, m := range mems {
		if g := m.heap - baseHeap; g > giantHeapMB {
			t.Errorf("%s: live heap %.0f MB, %.0f MB over the %.0f MB before the channel opened (bound %d MB)", m.name, m.heap, g, baseHeap, giantHeapMB)
		}
	}
	if g := hwm - baseRSS; g > giantRSSMB {
		t.Errorf("peak RSS %.0f MB, %.0f MB over the %.0f MB before the channel opened (bound %d MB)", hwm, g, baseRSS, giantRSSMB)
	}
	slow := 0
	for _, f := range frames {
		if a := f.d - f.r; a > giantFrameMax || f.d > giantWholeMax {
			if slow < 5 {
				t.Errorf("a %s frame (frame %d) took %v, the app %v of it, a collector's pause %v or more of that (%v stopping the world) (bounds %v whole, %v the app's)", f.stage, f.i, f.d.Round(time.Millisecond), a.Round(time.Millisecond), f.gc.Round(time.Millisecond), f.stop.Round(time.Millisecond), giantWholeMax, giantFrameMax)
			}
			slow++
		}
	}
	if slow > 5 {
		t.Errorf("… %d frames in all over a bound", slow)
	}
}

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
//   - any frame from the open on takes longer than giantFrameMax;
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
	"fmt"
	"os"
	"path/filepath"
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
	// opened to any stage's end.
	giantHeapMB = 700
	// giantRSSMB bounds the peak RSS's growth over the RSS before the
	// channel opened.
	giantRSSMB = 2000
	// giantFrameMax bounds every frame from the open on.
	giantFrameMax = 300 * time.Millisecond
	// giantAbortMB: past this RSS growth the test stops driving frames
	// (the bound is long gone; the runner keeps its memory).
	giantAbortMB = 6000
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

type giantFrame struct {
	stage string
	d     time.Duration
	heap  float64 // heap objects after it, MB
	rss   float64 // VmRSS after it, MB
}

func TestGiantMessagesBounded(t *testing.T) {
	if os.Getenv("CLACK_GIANT_MESSAGES") == "" {
		t.Skip("CLACK_GIANT_MESSAGES not set (flake-check's giant-messages job runs this)")
	}
	n := 100
	if v, err := strconv.Atoi(os.Getenv("CLACK_GIANT_MESSAGES")); err == nil && v > 0 {
		n = v
	}
	out := os.Getenv("CLACK_GIANT_OUT")
	if out != "" {
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sz := fakeslack.Small
	sz.Giant = n
	var fs *fakeslack.Server
	if u := os.Getenv("CLACK_FAKESLACK_URL"); u != "" { // fakeslackd -small -giant n: the heap is the client's alone
		fs = fakeslack.Remote(u, sz)
	} else {
		var err error
		if fs, err = fakeslack.Start(sz); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { fs.Close() })
	}
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

	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	e.openChannel(fs.ChannelIDs()[1])
	e.settle(time.Second)
	baseHeap := liveHeapMB()
	baseRSS := giantProcStatus("VmRSS")["VmRSS"]
	profile("0-before")
	t.Logf("before #giant-wall: live heap %.0f MB, RSS %.0f MB", baseHeap, baseRSS)

	// the watchdog: one frame that runs away can't take the runner with it
	stop := make(chan struct{})
	defer close(stop)
	var peakRSS atomic.Int64
	go func() {
		tk := time.NewTicker(50 * time.Millisecond)
		defer tk.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tk.C:
			}
			rss := giantProcStatus("VmRSS")["VmRSS"]
			if int64(rss) > peakRSS.Load() {
				peakRSS.Store(int64(rss))
			}
			if rss > giantPanicMB {
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
		t0 := time.Now()
		e.frame()
		d := time.Since(t0)
		st := giantProcStatus("VmRSS")
		frames = append(frames, giantFrame{stage: stage, d: d, heap: giantHeapObjectsMB(), rss: st["VmRSS"]})
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
		t.Logf("%s: live heap %.0f MB (+%.0f), RSS %.0f MB (+%.0f), %d messages loaded", name, h, h-baseHeap, r, r-baseRSS, e.loadedMessages(ch))
	}

	// 1. open it: the newest 50 messages, the newest at the bottom
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
	hwm := giantProcStatus("VmHWM")["VmHWM"]
	var b strings.Builder
	fmt.Fprintf(&b, "## Giant messages (#giant-wall, %d latest)\n\n", n)
	fmt.Fprintf(&b, "before the channel: live heap %.0f MB, RSS %.0f MB; peak RSS %.0f MB (VmHWM), %d frames, %v in frames\n\n", baseHeap, baseRSS, hwm, len(frames), total.Round(time.Millisecond))
	fmt.Fprintf(&b, "| stage | live heap | RSS |\n|---|---|---|\n")
	for _, m := range mems {
		fmt.Fprintf(&b, "| %s | %.0f MB (+%.0f) | %.0f MB (+%.0f) |\n", m.name, m.heap, m.heap-baseHeap, m.rss, m.rss-baseRSS)
	}
	fmt.Fprintf(&b, "\nlongest frames:\n\n")
	for _, f := range worst {
		fmt.Fprintf(&b, "- %s: %v (heap objects %.0f MB, RSS %.0f MB)\n", f.stage, f.d.Round(time.Millisecond), f.heap, f.rss)
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
		if f.d > giantFrameMax {
			if slow < 5 {
				t.Errorf("a %s frame took %v (bound %v)", f.stage, f.d.Round(time.Millisecond), giantFrameMax)
			}
			slow++
		}
	}
	if slow > 5 {
		t.Errorf("… %d frames in all over %v", slow, giantFrameMax)
	}
}

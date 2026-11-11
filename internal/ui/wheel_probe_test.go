package ui

import (
	"fmt"
	"image"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"
)

// TestWheelFrameProbe measures what scrolling a long channel with a
// notched mouse wheel costs per frame (CLACK_WHEEL_PROBE=1; ci's
// windows-applog job runs it on windows-latest). The Hack Club sized
// fakeslack, 300+ loaded messages, a 1440x900 window; with CLACK_BENCH_GPU
// set every frame is also rendered (Direct3D 11 on Windows, when the
// runner has a device).
//
// Input is synthetic wheel notches at a real cadence (bursts of six 35 ms
// apart, like a quick flick, 500 ms between bursts), frames paced at
// 60 Hz on the wall clock: a frame that runs past its vsync pushes the
// next one to the following vsync, as a real swap chain would. Each frame
// reports its layout and render times and how far the list moved.
func TestWheelFrameProbe(t *testing.T) {
	if os.Getenv("CLACK_WHEEL_PROBE") == "" {
		t.Skip("CLACK_WHEEL_PROBE not set")
	}
	e := newPerfEnv(t, true)
	e.waitReady()
	ch := e.fs.ChannelIDs()[2]
	e.openChannel(ch)
	e.until("history", 20*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
	p := e.listPoint()
	dl := time.Now().Add(60 * time.Second)
	for e.loadedMessages(ch) < 300 && time.Now().Before(dl) { // infinite scroll up to 300
		e.scroll(p, -2000)
		e.frame()
		time.Sleep(5 * time.Millisecond)
	}
	// one pass down so every image on the way is decoded once, then the
	// bottom
	for i := 0; i < 150; i++ {
		e.scroll(p, 600)
		e.frame()
	}
	e.settle(800 * time.Millisecond)
	for i := 0; i < 20; i++ { // up a few screens: rows above and below
		e.scroll(p, -400)
		e.frame()
	}
	e.settle(800 * time.Millisecond)
	t.Logf("wheel-probe: %d messages loaded, gpu=%v, %s", e.loadedMessages(ch), e.gpu != nil, wheelProbeInput)

	pr := &wheelProbe{e: e, ch: ch, p: p}
	pr.run(t, "up", -1)
	pr.run(t, "down", +1)
}

type wheelProbe struct {
	e  *perfEnv
	ch string
	p  image.Point
}

type probeFrame struct {
	at           time.Duration // since the first notch
	layout, gpu  time.Duration
	moved        int // px the list moved since the previous frame
	notches      int // notches delivered before this frame
	wantsNext    bool
	missedVsyncs int
	interval     time.Duration
	visible      int
}

// run sends 3 bursts of 6 notches (dir -1 scrolls toward older messages)
// and records frames until 400 ms after the last one.
func (pr *wheelProbe) run(t *testing.T, name string, dir float32) {
	e := pr.e
	const vsync = time.Second / 60
	var at []time.Duration // notch times from the start
	for b := 0; b < 3; b++ {
		for i := 0; i < 6; i++ {
			at = append(at, time.Duration(b)*(5*35*time.Millisecond+500*time.Millisecond)+time.Duration(i)*35*time.Millisecond)
		}
	}
	end := at[len(at)-1] + 400*time.Millisecond
	start := time.Now()
	next := start
	sent := 0
	prev := pr.rows()
	var frames []probeFrame
	lastFrame := start
	for {
		if d := time.Until(next); d > 0 {
			time.Sleep(d)
		}
		now := time.Now()
		if now.Sub(start) > end {
			break
		}
		n := 0
		for sent < len(at) && at[sent] <= now.Sub(start) {
			e.r.Queue(wheelNotch(pr.p, dir))
			sent++
			n++
		}
		lay, gpu := pr.frame()
		rows := pr.rows()
		f := probeFrame{at: now.Sub(start), layout: lay, gpu: gpu, moved: movedBetween(prev, rows), notches: n, interval: now.Sub(lastFrame), visible: len(rows)}
		_, f.wantsNext = e.r.WakeupTime()
		prev, lastFrame = rows, now
		// the next vsync after this frame's work
		next = next.Add(vsync)
		for done := time.Now(); next.Before(done); next = next.Add(vsync) {
			f.missedVsyncs++
		}
		frames = append(frames, f)
	}
	pr.report(t, name, frames)
}

// frame is perfEnv.frame with the layout (App.frame plus the router) and
// the GPU render timed apart.
func (pr *wheelProbe) frame() (lay, gpu time.Duration) {
	e := pr.e
	a := e.a
	a.drain()
	t0 := time.Now()
	e.now = t0
	gtx := e.context()
	before := a.router.Current()
	a.frame(gtx)
	if a.router.Current() != before {
		gtx = e.context()
		a.frame(gtx)
	}
	e.r.Frame(&e.ops)
	t1 := time.Now()
	if e.gpu != nil {
		if err := e.gpu.Frame(&e.ops); err != nil {
			e.tb.Fatal(err)
		}
		// Frame only submits; reading a pixel waits for the GPU
		_ = e.gpu.Screenshot(image.NewRGBA(image.Rect(0, 0, e.size.X, e.size.Y)))
	}
	return t1.Sub(t0), time.Since(t1)
}

func (pr *wheelProbe) rows() map[string]int {
	c := pr.e.a.convos[pr.ch+"/"]
	if c == nil {
		return nil
	}
	l, ok := c.list.(channelList)
	if !ok {
		return nil
	}
	m := map[string]int{}
	for _, r := range l.VisibleRows() {
		m[r.Key] = r.Y
	}
	return m
}

// movedBetween is how far the rows moved (positive: content moved up,
// i.e. scrolled toward newer messages), from any row on screen in both.
func movedBetween(a, b map[string]int) int {
	for k, y := range b {
		if y0, ok := a[k]; ok {
			return y0 - y
		}
	}
	return 0
}

func (pr *wheelProbe) report(t *testing.T, name string, fs []probeFrame) {
	var lay, gpu, tot []time.Duration
	missed, moving, wants := 0, 0, 0
	maxGap := time.Duration(0)
	var sb strings.Builder
	for i, f := range fs {
		lay = append(lay, f.layout)
		gpu = append(gpu, f.gpu)
		tot = append(tot, f.layout+f.gpu)
		missed += f.missedVsyncs
		if f.moved != 0 {
			moving++
		}
		if f.wantsNext {
			wants++
		}
		if i > 0 && f.interval > maxGap {
			maxGap = f.interval
		}
		fmt.Fprintf(&sb, "  %6.1fms n=%d layout=%5.2fms gpu=%5.2fms moved=%4dpx wantsNext=%v missed=%d\n",
			ms(f.at), f.notches, ms(f.layout), ms(f.gpu), f.moved, f.wantsNext, f.missedVsyncs)
	}
	t.Logf("wheel-probe %s: %d frames, %d moved, %d asked for another frame, %d vsyncs missed, longest frame gap %.1fms\n"+
		"  layout  p50 %.2f p90 %.2f p99 %.2f max %.2f ms\n"+
		"  gpu     p50 %.2f p90 %.2f p99 %.2f max %.2f ms\n"+
		"  total   p50 %.2f p90 %.2f p99 %.2f max %.2f ms\n%s",
		name, len(fs), moving, wants, missed, ms(maxGap),
		pct(lay, 50), pct(lay, 90), pct(lay, 99), pct(lay, 100),
		pct(gpu, 50), pct(gpu, 90), pct(gpu, 99), pct(gpu, 100),
		pct(tot, 50), pct(tot, 90), pct(tot, 99), pct(tot, 100), sb.String())
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

func pct(ds []time.Duration, p int) float64 {
	if len(ds) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	i := (len(s) - 1) * p / 100
	return ms(s[i])
}

// wheelProbeInput describes the notch events.
const wheelProbeInput = "one notch = Scroll 120 (stock Gio's WM_MOUSEWHEEL: the raw WHEEL_DELTA as pixels, applied at once)"

// wheelNotch is one notch of a mouse wheel at p as Gio's Windows backend
// delivers it.
func wheelNotch(p image.Point, dir float32) pointer.Event {
	return pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: f32.Pt(float32(p.X), float32(p.Y)), Scroll: f32.Pt(0, 120*dir)}
}

//go:build windows && cgo

package mf

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/op"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/media"
	"github.com/some-du6e/clack/internal/media/mediatest"
)

// need skips a test without Media Foundation or a GPU, unless
// CLACK_MEDIA_TESTS says CI has both.
func need(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	if os.Getenv("CLACK_MEDIA_TESTS") != "" {
		t.Fatal(err)
	}
	t.Skip(err)
}

// read is one of the demuxer's reads: where, and the bytes it got.
type read struct {
	off int64
	n   int
}

// The demuxer's reads and the engine's events by file, from the hooks
// (the events as code@ms after the player was made).
var (
	hookMu sync.Mutex
	reads  = map[string][]read{}
	trace  = map[string][]string{}
	born   = map[string]time.Time{}
)

func init() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	testRead = func(id string, off int64, n int) {
		hookMu.Lock()
		defer hookMu.Unlock()
		reads[id] = append(reads[id], read{off, n})
	}
	testEvent = func(id string, code uint32) {
		hookMu.Lock()
		defer hookMu.Unlock()
		trace[id] = append(trace[id], fmt.Sprintf("%d@%d", code, time.Since(born[id]).Milliseconds()))
	}
}

func readsOf(id string) []read {
	hookMu.Lock()
	defer hookMu.Unlock()
	return append([]read(nil), reads[id]...)
}

// traceOf is the engine's events so far, as code@ms (MF_MEDIA_ENGINE_EVENT:
// 1 loadstart, 2 progress, 3 suspend, 7 stalled, 10 loadedmetadata, 11
// loadeddata, 12 waiting, 13 playing, 14 canplay, 15 canplaythrough, 21
// durationchange, 1009 first frame, 1014 a stream failed).
func traceOf(id string) string {
	hookMu.Lock()
	defer hookMu.Unlock()
	return strings.Join(trace[id], " ")
}

// rig is a player drawn into a headless Gio window on Direct3D 11, as
// Clack's window draws it: the GPU's asks are the window's frames.
type rig struct {
	t      *testing.T
	win    *headless.Window
	p      *player
	o      *mediatest.Opener
	id     string
	size   image.Point
	ready  atomic.Int64
	readyC chan struct{}
	evC    chan media.Event
	mu     sync.Mutex
	events []media.Event
}

var rigN atomic.Int64

func newRig(t *testing.T, data []byte, whole bool, size image.Point, setup ...func(*mediatest.Opener)) *rig {
	t.Helper()
	need(t, backend{}.Available())
	win, err := headless.NewWindow(size.X, size.Y)
	need(t, err)
	id := fmt.Sprintf("FMF%d", rigN.Add(1))
	src, o := mediatest.Source(id, data, whole)
	for _, f := range setup {
		f(o)
	}
	hookMu.Lock()
	born[id] = time.Now()
	hookMu.Unlock()
	r := &rig{t: t, win: win, o: o, id: id, size: size, readyC: make(chan struct{}, 1), evC: make(chan media.Event, 1024)}
	pl, err := backend{}.New(src, func() {
		r.ready.Add(1)
		select {
		case r.readyC <- struct{}{}:
		default:
		}
	})
	if err != nil {
		win.Release()
		t.Fatal(err)
	}
	r.p = pl.(*player)
	r.p.SetDrawSize(size)
	go func() {
		for ev := range r.p.Events() {
			r.mu.Lock()
			r.events = append(r.events, ev)
			r.mu.Unlock()
			select {
			case r.evC <- ev:
			default:
			}
		}
	}()
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("%s: the engine's events %s; the demuxer's reads %v", id, traceOf(id), readsOf(id))
		}
		r.p.Close()
		select {
		case <-r.p.done:
		case <-time.After(10 * time.Second):
			t.Error("the player's media thread outlived Close by 10 s")
		}
		win.Release()
	})
	return r
}

// draw draws a frame of the window, the player's op over all of it, and
// returns the centre pixel.
func (r *rig) draw() color.RGBA {
	r.t.Helper()
	var ops op.Ops
	r.p.Op(paint.ImageOp{}, r.size).Add(&ops)
	paint.PaintOp{}.Add(&ops)
	if err := r.win.Frame(&ops); err != nil {
		r.t.Fatal(err)
	}
	img := image.NewRGBA(image.Rectangle{Max: r.size})
	if err := r.win.Screenshot(img); err != nil {
		r.t.Fatal(err)
	}
	return img.RGBAAt(r.size.X/2, r.size.Y/2)
}

// wait draws (as a window showing the player would) until an event of
// kind k comes, and returns it.
func (r *rig) wait(k media.EventKind, within time.Duration) media.Event {
	r.t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		select {
		case ev := <-r.evC:
			if ev.Kind == k {
				return ev
			}
		case <-r.readyC:
			r.draw()
		case <-time.After(15 * time.Millisecond):
			r.draw()
		}
	}
	r.t.Fatalf("no %v within %v; events %v; state %+v; the engine's events %s", k, within, r.kinds(), r.p.State(), traceOf(r.id))
	return media.Event{}
}

func (r *rig) kinds() []media.EventKind {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []media.EventKind
	for _, ev := range r.events {
		if ev.Kind != media.TimeUpdate {
			out = append(out, ev.Kind)
		}
	}
	return out
}

func inOrder(got, want []media.EventKind) bool {
	i := 0
	for _, k := range got {
		if i < len(want) && k == want[i] {
			i++
		}
	}
	return i == len(want)
}

// sample is a drawn frame: at is the clock's position when it was drawn,
// pts the frame's own presentation time (the transfer's), which its
// colour is checked at. They part where the first frame came late: the
// first one transferred then can be the engine's first frame (grey, pts 0)
// with the clock already 1.6 s on (mf-first-frame's runs, 2 in 10). The
// seconds are counted from the clock's position at the first frame drawn
// (at), the frames in them by their own time.
type sample struct {
	at, pts time.Duration
	// prev is the frame transferred before pts's (-1: none): the drawn
	// frame can be that one, a frame behind (the ring)
	prev time.Duration
	c    color.RGBA
}

// matches is whether the drawn colour is the fixture's at the frame's
// time, or at the frame before's.
func (s sample) matches() bool {
	return mediatest.Matches(s.pts, s.c) || s.prev >= 0 && mediatest.Matches(s.prev, s.c)
}

// playThrough plays to the end, drawing every new frame, and returns what
// each drawn frame showed and when the first one came.
func (r *rig) playThrough() ([]sample, time.Duration) {
	r.t.Helper()
	r.draw() // the first ask: the player binds to Gio's device
	start := time.Now()
	r.p.Play()
	var samples []sample
	var first time.Duration
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case ev := <-r.evC:
			if ev.Kind == media.Ended {
				return samples, first
			}
			if ev.Kind == media.Error {
				r.t.Fatalf("error %d; events %v", ev.State.Err, r.kinds())
			}
			continue
		case <-r.readyC:
		case <-time.After(15 * time.Millisecond):
		}
		c := r.draw()
		if c.A != 0 {
			if first == 0 {
				first = time.Since(start)
			}
			pos := r.p.State().Pos
			pts, prev := pos, time.Duration(-1)
			if v := r.p.framePts.Load(); v >= 0 {
				pts, prev = time.Duration(v), time.Duration(r.p.prevPts.Load())
			}
			samples = append(samples, sample{at: pos, pts: pts, prev: prev, c: c})
		}
	}
	r.t.Fatalf("no end within 15 s; events %v; state %+v; stats %+v; the engine's events %s", r.kinds(), r.p.State(), r.p.Stats(), traceOf(r.id))
	return nil, 0
}

// checkColors: the frames drawn well inside each second are the
// fixture's grey, light grey and red (mediatest.Matches: the range, the
// sRGB view and the channel order), and every second from the first frame
// drawn on has several (one where the first frame came inside it, with
// partWindow of it left). A second that ended before the first frame was
// drawn isn't counted: where the first frame came late, the engine's clock
// had run on without it (a runner's software decoding, 1 to 1.65 s after
// the engine played), which the first frame's budget after Play judges;
// its colours are still checked if any of its frames was drawn.
func checkColors(t *testing.T, samples []sample) {
	t.Helper()
	if len(samples) == 0 {
		t.Fatal("no frame drawn")
	}
	defer func() {
		if t.Failed() {
			sampleTrace(t, samples)
		}
	}()
	from := samples[0].at // where the clock was when the first frame was drawn
	for i, w := range colorWindows {
		n, bad := 0, 0
		for _, s := range samples {
			if s.pts < w[0] || s.pts > w[1] {
				continue
			}
			n++
			if !s.matches() {
				if bad < 3 {
					t.Errorf("the frame of %v (drawn at %v) was %v, want %s", s.pts, s.at, s.c, mediatest.Want(s.pts))
				}
				bad++
			}
		}
		switch left := w[1] - from; {
		case left < partWindow:
			t.Logf("second %d: before the first frame drawn (at %v): %d frames drawn, %d off", i+1, from, n, bad)
			continue
		case w[0] < from && n < 1:
			t.Errorf("second %d: no frame drawn in its %v after the first frame (at %v)", i+1, left, from)
		case w[0] >= from && n < 3:
			t.Errorf("second %d: %d frames drawn, want several (the first frame drawn at %v)", i+1, n, from)
		}
		t.Logf("second %d: %d frames drawn, %d off", i+1, n, bad)
	}
}

// sampleTrace logs the samples (every nth, the last 8 whole) for a failed check:
// the clock's position, the frame's time and the one before, the colour.
func sampleTrace(t *testing.T, samples []sample) {
	t.Helper()
	var b strings.Builder
	n := max(len(samples)/40, 1)
	for i, s := range samples {
		if i%n == 0 || i >= len(samples)-8 {
			fmt.Fprintf(&b, " %d:%v/%v/%v=%d", i, s.at.Round(time.Millisecond), s.pts.Round(time.Millisecond), s.prev.Round(time.Millisecond), s.c.R)
		}
	}
	t.Logf("%d samples (i:clock/pts/prev=red):%s", len(samples), b.String())
}

// partWindow is the least of a colour's window, after the first frame
// drawn, that must draw a frame (a frame at 30 fps is 33 ms).
const partWindow = 150 * time.Millisecond

// colorWindows are the parts of the fixture's three seconds well inside
// each colour.
var colorWindows = [3][2]time.Duration{{300 * time.Millisecond, 700 * time.Millisecond}, {1300 * time.Millisecond, 1700 * time.Millisecond}, {2300 * time.Millisecond, 2700 * time.Millisecond}}

// The first player in the process, the fixture with its audio track:
// its metadata within 10 s of its engine being made. The platform's start
// before that (the window, Direct3D, MFStartup, the audio output's check)
// is logged as its own number, not held to the budget: on a runner it is
// the machine's, not Clack's (3 s cold under load, 2026-11-21's DIAG), and
// with no audio output Media Foundation's own load of a file with an
// audio track takes a floor of ~2.1 s, muted or not (TestMFAudioTrackLoad;
// 6.8 s cold under load), which stays inside the budget. On a runner the
// first engines of one process, made unmuted, didn't get there in 15 s;
// with no output the engine is made muted.
func TestMFColdStart(t *testing.T) {
	t0 := time.Now()
	r := newRig(t, mediatest.GreyGreyRed, false, image.Pt(320, 180))
	r.draw()
	md := r.wait(media.LoadedMetadata, 60*time.Second)
	at := time.Now()
	opened := r.opened()
	audio, _ := r.audio()
	if opened.IsZero() {
		t.Fatalf("loadedmetadata with no engine made; the engine's events %s", traceOf(r.id))
	}
	took := at.Sub(opened)
	t.Logf("loadedmetadata %v after the engine was made; the platform's start (window, device, MFStartup, the audio check) %v before it (audio: %s); the engine's events %s",
		took, opened.Sub(t0), audio, traceOf(r.id))
	if took > 10*time.Second {
		t.Errorf("loadedmetadata %v after the engine was made, want under 10 s", took)
	}
	if d := md.State.Duration; d < 2950*time.Millisecond || d > 3050*time.Millisecond {
		t.Errorf("duration %v", d)
	}
}

// opened is when the player made its engine (zero: not yet).
func (r *rig) opened() time.Time {
	done := make(chan time.Time, 1)
	r.p.post(func() { done <- r.p.tOpened })
	select {
	case at := <-done:
		return at
	case <-time.After(5 * time.Second):
		return time.Time{}
	}
}

// A first transfer that comes 1.5 s after the data, as the runners' did
// under load (the first frame 1 to 1.65 s after the engine played, its
// clock running on: TestMFRGBA on train-141, TestMFPlaysFixture on
// menu-click-rerender, 2026-11-21): the frames from then on are right and
// every second after it draws them. The seconds before it drew nothing,
// and the checks counted them, so those tests failed on the runner's
// timing; the first frame's budget after Play (TestMFPlaysFixture) is
// what judges a late start. It runs after TestMFColdStart (this file's
// order is the run order): that one must meet the process's first,
// cold engine.
func TestMFLateFirstTransfer(t *testing.T) {
	delayFirstTransfer.Store(int64(1500 * time.Millisecond))
	defer delayFirstTransfer.Store(0)
	for _, c := range []struct{ name, format string }{{"BGRA", ""}, {"RGBA", "rgba"}} {
		t.Run(c.name, func(t *testing.T) {
			if c.format != "" {
				t.Setenv("CLACK_MF_FORMAT", c.format)
			}
			r := newRig(t, mediatest.GreyGreyRed, false, image.Pt(320, 180))
			samples, first := r.playThrough()
			if len(samples) == 0 {
				t.Fatal("no frame drawn")
			}
			// the hook held the first transfer while the clock ran
			if samples[0].at < time.Second {
				t.Fatalf("the first frame drawn is at %v: the hook didn't hold it past the first second", samples[0].at)
			}
			t.Logf("first frame %v after Play, at %v; %d frames drawn", first, samples[0].at, len(samples))
			if c.format == "" {
				checkColors(t, samples)
			} else {
				checkRGBA(t, samples)
			}
		})
	}
}

// audio is what the player found of the audio output, and whether its
// engine is made muted, once it's known.
func (r *rig) audio() (string, bool) {
	type answer struct {
		s    string
		none bool
	}
	done := make(chan answer, 1)
	r.p.post(func() { done <- answer{r.p.audio, r.p.noAudio} })
	select {
	case a := <-done:
		return a.s, a.none
	case <-time.After(5 * time.Second):
		return "(the media thread didn't answer)", false
	}
}

// The fixture plays to its end through a headless Gio window: the
// metadata (3 s, 320×180), the events in order, the three colours, in
// BGRA frames; the first frame within 2 s of Play where there's an audio
// output. Where there's none (the runners) the audio track's engine took
// 2.2 s to its metadata, its audio renderer made or not, and the silent
// fixture's 0.02 s: there 5 s, and TestMFSilent holds the video's own
// path to 2 s.
func TestMFPlaysFixture(t *testing.T) {
	r := newRig(t, mediatest.GreyGreyRed, false, image.Pt(320, 180))
	samples, first := r.playThrough()
	checkColors(t, samples)
	audio, none := r.audio()
	budget := 2 * time.Second
	if none {
		budget = 5 * time.Second
	}
	if first > budget {
		t.Errorf("the first frame came %v after Play, want under %v (audio: %s)", first, budget, audio)
	}
	if r.p.format != fmtBGRA {
		t.Errorf("format %s, want BGRA", formatName(r.p.format))
	}
	t.Logf("first frame %v after Play; %d frames drawn; stats %+v", first, len(samples), r.p.Stats())
	got := r.kinds()
	if !inOrder(got, []media.EventKind{media.LoadedMetadata, media.LoadedData, media.Playing, media.Ended}) || !inOrder(got, []media.EventKind{media.Play, media.Playing}) {
		t.Errorf("events %v", got)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ev := range r.events {
		if ev.Kind == media.LoadedMetadata {
			if d := ev.State.Duration; d < 2950*time.Millisecond || d > 3050*time.Millisecond || ev.State.Size != image.Pt(320, 180) {
				t.Errorf("loadedmetadata: %v %v, want 3 s and 320×180", d, ev.State.Size)
			}
		}
	}
}

// A seek while paused fires seeking and seeked and shows the frame there,
// light grey, within 500 ms; paused, 2 s of the window drawing cost no
// transfer and no repaint.
func TestMFSeekAndPause(t *testing.T) {
	r := newRig(t, mediatest.GreyGreyRed, false, image.Pt(320, 180))
	r.draw()
	r.p.Play()
	r.wait(media.Playing, 10*time.Second)
	r.p.Pause()
	r.wait(media.Pause, 5*time.Second)
	t0 := time.Now()
	r.p.Seek(1500 * time.Millisecond)
	sk := r.wait(media.Seeked, 5*time.Second)
	var at time.Duration
	for time.Since(t0) < 2*time.Second {
		if c := r.draw(); mediatest.Matches(1500*time.Millisecond, c) {
			at = time.Since(t0)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if at == 0 || at > 500*time.Millisecond {
		t.Errorf("light grey %v after Seek(1.5 s) (seeked at %v), want within 500 ms", at, sk.State.Pos)
	}
	t.Logf("seeked to %v; light grey drawn %v after the seek", sk.State.Pos, at)
	time.Sleep(300 * time.Millisecond)
	st0, n0 := r.p.Stats(), r.ready.Load()
	for t1 := time.Now(); time.Since(t1) < 2*time.Second; time.Sleep(30 * time.Millisecond) {
		r.draw()
	}
	st1, n1 := r.p.Stats(), r.ready.Load()
	if st1.Transfers != st0.Transfers || n1 != n0 || st1.Ticks != st0.Ticks {
		t.Errorf("paused for 2 s, drawn: %d transfers, %d repaints, %d ticks; want none", st1.Transfers-st0.Transfers, n1-n0, st1.Ticks-st0.Ticks)
	}
	if st1.Asks <= st0.Asks {
		t.Error("the window's frames didn't ask")
	}
}

// Paused, a change of the drawn size (a zoom, another monitor's scale, a
// narrower tile) makes the ring again, and the paused frame goes into the
// new one: a paused <video> keeps its frame at any size. Round 3's
// review, a MINOR: the new ring stayed empty until a play or a seek (the
// engine has no new frame to tick), and the tile showed its poster.
func TestMFPausedFrameSurvivesResize(t *testing.T) {
	r := newRig(t, mediatest.GreyGreyRed, false, image.Pt(320, 180))
	r.draw()
	r.p.Play()
	r.wait(media.Playing, 10*time.Second)
	r.p.Pause()
	r.wait(media.Pause, 5*time.Second)
	r.p.Seek(1500 * time.Millisecond)
	r.wait(media.Seeked, 5*time.Second)
	shown := func(what string) {
		t.Helper()
		var c color.RGBA
		for t0 := time.Now(); time.Since(t0) < 2*time.Second; time.Sleep(10 * time.Millisecond) {
			if c = r.draw(); mediatest.Matches(1500*time.Millisecond, c) {
				return
			}
		}
		t.Fatalf("%s: %v drawn, want the paused frame (%s)", what, c, mediatest.Want(1500*time.Millisecond))
	}
	shown("paused at 1.5 s")
	st0 := r.p.Stats()
	r.p.SetDrawSize(image.Pt(160, 90)) // a new ring, 160x90
	time.Sleep(300 * time.Millisecond)
	shown("the drawn size halved")
	st1 := r.p.Stats()
	if st1.Transfers <= st0.Transfers {
		t.Errorf("no transfer into the new ring (%d before, %d after)", st0.Transfers, st1.Transfers)
	}
	time.Sleep(200 * time.Millisecond)
	st2 := r.p.Stats()
	for t1 := time.Now(); time.Since(t1) < time.Second; time.Sleep(30 * time.Millisecond) {
		r.draw()
	}
	if st3 := r.p.Stats(); st3.Ticks != st2.Ticks || st3.Transfers != st2.Transfers {
		t.Errorf("paused, its frame back: %d ticks and %d transfers in a second, want none", st3.Ticks-st2.Ticks, st3.Transfers-st2.Transfers)
	}
}

// Paused, the frame it shows reads back (media.FrameCopier: a tile keeps
// it when its idle player lets go, Chromium's idle suspend): light grey
// at 1.5 s, at the ring's size.
func TestMFSnapshot(t *testing.T) {
	r := newRig(t, mediatest.GreyGreyRed, false, image.Pt(320, 180))
	r.draw()
	r.p.Play()
	r.wait(media.Playing, 10*time.Second)
	r.p.Pause()
	r.wait(media.Pause, 5*time.Second)
	r.p.Seek(1500 * time.Millisecond)
	r.wait(media.Seeked, 5*time.Second)
	for t0 := time.Now(); time.Since(t0) < 2*time.Second; time.Sleep(10 * time.Millisecond) {
		if mediatest.Matches(1500*time.Millisecond, r.draw()) {
			break
		}
	}
	img, ok := r.p.CopyFrame()
	if !ok {
		t.Fatal("no snapshot of the paused frame")
	}
	b := img.Bounds()
	if c := img.RGBAAt(b.Dx()/2, b.Dy()/2); !mediatest.Matches(1500*time.Millisecond, c) {
		t.Errorf("the snapshot's centre is %v, want %s", c, mediatest.Want(1500*time.Millisecond))
	}
	t.Logf("snapshot %v", b)
}

// A file with its moov after the mdat and 4 MB before it plays; the
// demuxer's reads get to the moov. (The 25 KB fixture alone came in one
// read at 0.)
func TestMFMoovAtEnd(t *testing.T) {
	const pad = 4 << 20
	moov := int64(mediatest.MoovAtEndMoov + pad)
	r := newRig(t, mediatest.MoovAtEndPadded(pad), false, image.Pt(320, 180))
	samples, _ := r.playThrough()
	checkColors(t, samples)
	rs := readsOf(r.id)
	got := false
	total := 0
	for _, rd := range rs {
		got = got || rd.off <= moov && moov < rd.off+int64(rd.n)
		total += rd.n
	}
	if !got {
		t.Errorf("no read got the moov (byte %d): reads %v", moov, rs)
	}
	t.Logf("the demuxer read %d bytes of %d in %d reads %v; GETs at %v", total, moov+int64(len(mediatest.MoovAtEnd)-mediatest.MoovAtEndMoov), len(rs), rs, r.o.Stats().Offsets)
}

// A server ignoring ranges (a 200): the backend is told the file can't be
// read by ranges. It plays it in order, or refuses it with error 4 (the
// brief's fallback: Slack's error look); the test says which.
func TestMFNotSeekable(t *testing.T) {
	r := newRig(t, mediatest.GreyGreyRed, true, image.Pt(320, 180))
	r.draw()
	r.p.Play()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case ev := <-r.evC:
			switch ev.Kind {
			case media.Ended:
				t.Logf("a file read only in order plays to its end; events %v", r.kinds())
				return
			case media.Error:
				if ev.State.Err != media.ErrSrcNotSupported {
					t.Errorf("error %d, want 4 (or playing)", ev.State.Err)
				}
				t.Logf("a file read only in order is refused: error %d; events %v", ev.State.Err, r.kinds())
				return
			}
		case <-r.readyC:
			r.draw()
		case <-time.After(15 * time.Millisecond):
			r.draw()
		}
	}
	t.Fatalf("neither the end nor an error in 15 s; events %v; state %+v", r.kinds(), r.p.State())
}

// A file whose fetch fails before its metadata (signed out, a sign-in
// page, a refused GET) is error 4, as Chromium's MediaLoadingFailed gives
// a src it couldn't load, and as the fake does.
func TestMFFetchFails(t *testing.T) {
	r := newRig(t, mediatest.GreyGreyRed, false, image.Pt(320, 180), func(o *mediatest.Opener) {
		o.Fail, o.FailStatus = errors.New("slack: not a file URL"), 200
	})
	r.draw()
	r.p.Play()
	if ev := r.wait(media.Error, 10*time.Second); ev.State.Err != media.ErrSrcNotSupported {
		t.Errorf("error %d, want 4", ev.State.Err)
	}
}

// The fixture with no audio track plays where the runner has no audio
// output; the test says which case ran.
func TestMFSilent(t *testing.T) {
	if err := audioEndpoint(); err != nil {
		t.Logf("no default audio output (%v): the silent fixture plays as any file would here", err)
	} else {
		t.Log("the runner has an audio output")
	}
	r := newRig(t, mediatest.Silent, false, image.Pt(320, 180))
	samples, first := r.playThrough()
	checkColors(t, samples)
	if first > 2*time.Second {
		t.Errorf("the first frame came %v after Play, want under 2 s", first)
	}
	t.Logf("first frame %v after Play", first)
}

// 300 frames through the shared ring: how many a busy texture skipped
// and how many asks couldn't take the newest (the keyed mutexes, neither
// side waiting).
func TestMFRing300(t *testing.T) {
	r := newRig(t, mediatest.GreyGreyRed, false, image.Pt(320, 180))
	r.p.SetLoop(true)
	r.draw()
	r.p.Play()
	deadline := time.Now().Add(30 * time.Second)
	for r.p.Stats().Transfers < 300 && time.Now().Before(deadline) {
		select {
		case <-r.readyC:
		case <-time.After(15 * time.Millisecond):
		}
		r.draw()
	}
	st := r.p.Stats()
	t.Logf("%d transfers, %d skipped (no free texture), %d asks couldn't take the newest, %d asks", st.Transfers, st.Skipped, st.Timeouts, st.Asks)
	if st.Transfers < 300 {
		t.Fatalf("%d frames in 30 s", st.Transfers)
	}
	if st.Skipped > 15 || st.Timeouts > 15 {
		t.Errorf("%d skipped, %d timed out of 300: want 5%% at most", st.Skipped, st.Timeouts)
	}
}

// RGBA frames (CLACK_MF_FORMAT=rgba, the fallback) keep the channel
// order: red stays red. Their greys are logged, not checked: where Media
// Foundation converts in software it gives RGBA frames BT.709's transfer
// curve converted to sRGB's (64 reads 79, 192 reads 198), BGRA ones not.
func TestMFRGBA(t *testing.T) {
	t.Setenv("CLACK_MF_FORMAT", "rgba")
	r := newRig(t, mediatest.GreyGreyRed, false, image.Pt(320, 180))
	samples, _ := r.playThrough()
	if r.p.format != fmtRGBA {
		t.Errorf("format %s, want RGBA", formatName(r.p.format))
	}
	checkRGBA(t, samples)
}

// checkRGBA: RGBA frames' red is the fixture's red, and the grey and light
// grey seconds drew frames from the first frame drawn on (checkColors).
func checkRGBA(t *testing.T, samples []sample) {
	t.Helper()
	if len(samples) == 0 {
		t.Fatal("no frame drawn")
	}
	defer func() {
		if t.Failed() {
			sampleTrace(t, samples)
		}
	}()
	from := samples[0].at // where the clock was when the first frame was drawn
	var by [3][]sample
	for _, s := range samples {
		for i, w := range colorWindows {
			if s.pts > w[0] && s.pts < w[1] {
				by[i] = append(by[i], s)
			}
		}
	}
	grey, light, red := by[0], by[1], by[2]
	if (len(grey) == 0 && colorWindows[0][1]-from >= partWindow) || (len(light) == 0 && colorWindows[1][1]-from >= partWindow) || len(red) < 3 {
		t.Fatalf("%d, %d, %d frames drawn in the three seconds (the first frame drawn at %v)", len(grey), len(light), len(red), from)
	}
	for _, s := range red {
		if !s.matches() {
			t.Errorf("the third second drew %v (the frame of %v), want %s", s.c, s.pts, mediatest.Want(s.pts))
			break
		}
	}
	first := func(ss []sample) any {
		if len(ss) == 0 {
			return "none (before the first frame)"
		}
		return ss[0].c
	}
	t.Logf("RGBA frames: grey 64 reads %v, 192 reads %v, red %v", first(grey), first(light), red[0].c)
}

// An engine refusing its first frame in its format (RGBA here) is made
// again for the other where it was: the playback goes on to the end, the
// colours right, its events once; meanwhile it's a stall, the clock held
// (no position runs on and comes back) and waiting, then playing, sent.
func TestMFFormatFallback(t *testing.T) {
	t.Setenv("CLACK_MF_FORMAT", "rgba")
	refused.Store(0)
	defer refused.Store(0)
	forceTransferFailures.Store(1)
	defer forceTransferFailures.Store(0)
	r := newRig(t, mediatest.GreyGreyRed, false, image.Pt(320, 180))
	samples, _ := r.playThrough()
	checkColors(t, samples)
	if !r.p.fellBack || r.p.format != fmtBGRA || refused.Load() != formatBit(fmtRGBA) {
		t.Errorf("fell back %v, format %s, refused %b", r.p.fellBack, formatName(r.p.format), refused.Load())
	}
	for i := 1; i < len(samples); i++ {
		if samples[i].at < samples[i-1].at-100*time.Millisecond {
			t.Errorf("the position went back from %v to %v (frame %d of %d): it ran on while the engine was made again", samples[i-1].at, samples[i].at, i, len(samples))
			break
		}
	}
	got := r.kinds()
	n := 0
	for _, k := range got {
		if k == media.LoadedMetadata {
			n++
		}
	}
	if n != 1 {
		t.Errorf("loadedmetadata %d times (events %v), want once", n, got)
	}
	lastWaiting, lastPlaying := -1, -1
	for i, k := range got {
		switch k {
		case media.Waiting:
			lastWaiting = i
		case media.Playing:
			lastPlaying = i
		}
	}
	if lastPlaying < lastWaiting || !inOrder(got, []media.EventKind{media.Playing, media.Ended}) {
		t.Errorf("events %v, want playing after the last waiting, then ended", got)
	}
	t.Logf("events %v", got)
}

// Closed, every player lets go of all it held: the engines, their
// devices, the shared textures and Gio's copies, the byte streams, the
// reads.
func TestMFCloseReleases(t *testing.T) {
	func() {
		r := newRig(t, mediatest.GreyGreyRed, false, image.Pt(160, 90))
		r.draw()
		r.p.Play()
		r.wait(media.Playing, 10*time.Second)
		r.p.Close()
		<-r.p.done
	}()
	deadline := time.Now().Add(5 * time.Second)
	var l [liveN]int32
	for time.Now().Before(deadline) {
		if l = live(); l == ([liveN]int32{}) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if l != ([liveN]int32{}) {
		t.Errorf("live after Close: players %d, streams %d, notify %d, engines %d, devices %d, textures %d, reads %d", l[0], l[1], l[2], l[3], l[4], l[5], l[6])
	}
}

// The engine's device is made, and the engine shut down, without askMu,
// which Gio's ask takes on its render thread. Round 3's review, a MINOR:
// D3D11CreateDevice (with its retry), the DXGI manager and the engine's
// Shutdown take tens of milliseconds on a real GPU, and the frame after a
// first click, or a close while drawn, waited them out.
func TestMFSlowCallsLeaveTheAskFree(t *testing.T) {
	var mu sync.Mutex
	var binds, closes []bool
	free := func(p *player) bool {
		if !p.askMu.TryLock() {
			return false
		}
		p.askMu.Unlock()
		return true
	}
	bindHook = func(p *player) {
		ok := free(p)
		mu.Lock()
		binds = append(binds, ok)
		mu.Unlock()
	}
	closeHook = func(p *player) {
		ok := free(p)
		mu.Lock()
		closes = append(closes, ok)
		mu.Unlock()
	}
	r := newRig(t, mediatest.GreyGreyRed, false, image.Pt(160, 90))
	t.Cleanup(func() { bindHook, closeHook = nil, nil }) // the player is closed below
	r.draw()
	r.p.Play()
	r.wait(media.Playing, 10*time.Second)
	r.p.Close()
	<-r.p.done
	mu.Lock()
	defer mu.Unlock()
	if len(binds) == 0 {
		t.Error("the device was never made")
	}
	for _, ok := range binds {
		if !ok {
			t.Errorf("the device was made holding askMu (%v)", binds)
			break
		}
	}
	if len(closes) != 1 || !closes[0] {
		t.Errorf("the engine was shut down holding askMu (%v)", closes)
	}
}

// Closed while its frames are drawn, the same ops replayed frame after
// frame (quiet frames): an ask racing Close either hands out a texture
// with Gio's own reference or nothing, the frames after Close draw the
// still, and nothing is freed under Gio.
func TestMFCloseWhileDrawn(t *testing.T) {
	r := newRig(t, mediatest.GreyGreyRed, false, image.Pt(320, 180))
	var ops op.Ops
	r.p.Op(paint.ImageOp{}, r.size).Add(&ops)
	paint.PaintOp{}.Add(&ops)
	shot := func() color.RGBA {
		t.Helper()
		if err := r.win.Frame(&ops); err != nil {
			t.Fatal(err)
		}
		img := image.NewRGBA(image.Rectangle{Max: r.size})
		if err := r.win.Screenshot(img); err != nil {
			t.Fatal(err)
		}
		return img.RGBAAt(r.size.X/2, r.size.Y/2)
	}
	shot()
	r.p.Play()
	deadline := time.Now().Add(10 * time.Second)
	for shot().A == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("no frame in 10 s; events %v", r.kinds())
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop, done := make(chan struct{}), make(chan int)
	go func() {
		n := 0
		for {
			select {
			case <-stop:
				done <- n
				return
			default:
			}
			if err := r.win.Frame(&ops); err != nil {
				t.Error(err)
			}
			n++
		}
	}()
	time.Sleep(300 * time.Millisecond)
	r.p.Close()
	time.Sleep(300 * time.Millisecond)
	close(stop)
	n := <-done
	if c := shot(); c.A != 0 {
		t.Errorf("after Close the op draws %v, want its still (nothing)", c)
	}
	t.Logf("%d frames drawn around Close", n)
}

// The time a file with an audio track takes to its metadata where there's
// no audio output (the runners: 2.2 s in round 2, muted or not), against
// the silent fixture's: the engine made muted (what the player does with
// no output), then with its audio renderer, then a file with no audio
// track. Logged for the question; it fails only on no metadata in 30 s.
func TestMFAudioTrackLoad(t *testing.T) {
	defer forceAudioRenderer.Store(false)
	for _, c := range []struct {
		name     string
		data     []byte
		renderer bool
	}{
		{"an audio track, the engine muted", mediatest.GreyGreyRed, false},
		{"an audio track, the engine with its audio renderer", mediatest.GreyGreyRed, true},
		{"no audio track", mediatest.Silent, false},
	} {
		forceAudioRenderer.Store(c.renderer)
		t0 := time.Now()
		r := newRig(t, c.data, false, image.Pt(320, 180))
		r.draw()
		got := false
		for dl := time.Now().Add(30 * time.Second); !got && time.Now().Before(dl); {
			select {
			case ev := <-r.evC:
				got = ev.Kind == media.LoadedMetadata
			case <-time.After(15 * time.Millisecond):
				r.draw()
			}
		}
		took := time.Since(t0)
		audio, _ := r.audio()
		if !got {
			t.Errorf("%s: no metadata within 30 s (audio: %s); the engine's events %s", c.name, audio, traceOf(r.id))
			continue
		}
		t.Logf("%s: loadedmetadata %v after the player was made (audio: %s); the engine's events %s", c.name, took.Round(time.Millisecond), audio, traceOf(r.id))
	}
}

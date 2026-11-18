//go:build windows && cgo

package mf

import (
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

func newRig(t *testing.T, data []byte, whole bool, size image.Point) *rig {
	t.Helper()
	need(t, backend{}.Available())
	win, err := headless.NewWindow(size.X, size.Y)
	need(t, err)
	id := fmt.Sprintf("FMF%d", rigN.Add(1))
	src, o := mediatest.Source(id, data, whole)
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

type sample struct {
	at time.Duration
	c  color.RGBA
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
			samples = append(samples, sample{r.p.State().Pos, c})
		}
	}
	r.t.Fatalf("no end within 15 s; events %v; state %+v; stats %+v; the engine's events %s", r.kinds(), r.p.State(), r.p.Stats(), traceOf(r.id))
	return nil, 0
}

// checkColors: the frames drawn well inside each second are the
// fixture's grey, light grey and red (mediatest.Matches: the range, the
// sRGB view and the channel order).
func checkColors(t *testing.T, samples []sample) {
	t.Helper()
	for i, w := range [][2]time.Duration{{300 * time.Millisecond, 700 * time.Millisecond}, {1300 * time.Millisecond, 1700 * time.Millisecond}, {2300 * time.Millisecond, 2700 * time.Millisecond}} {
		n, bad := 0, 0
		for _, s := range samples {
			if s.at < w[0] || s.at > w[1] {
				continue
			}
			n++
			if !mediatest.Matches(s.at, s.c) {
				if bad < 3 {
					t.Errorf("at %v the frame drawn was %v, want %s", s.at, s.c, mediatest.Want(s.at))
				}
				bad++
			}
		}
		if n < 3 {
			t.Errorf("second %d: %d frames drawn, want several", i+1, n)
		}
		t.Logf("second %d: %d frames drawn, %d off", i+1, n, bad)
	}
}

// The first player in the process, the fixture with its audio track:
// its metadata within 5 s of the window being made (Media Foundation's
// and Direct3D's start included). On the runners (no audio output)
// engines made with an audio renderer took 2 s to their metadata, and
// the first ones in a process didn't get there in 15 s; with no output
// the engine is made with none.
func TestMFColdStart(t *testing.T) {
	t0 := time.Now()
	r := newRig(t, mediatest.GreyGreyRed, false, image.Pt(320, 180))
	r.draw()
	md := r.wait(media.LoadedMetadata, 60*time.Second)
	took := time.Since(t0)
	t.Logf("loadedmetadata %v after the first player's window was made (audio: %s); the engine's events %s", took, r.audio(), traceOf(r.id))
	if took > 5*time.Second {
		t.Errorf("loadedmetadata %v after the window was made, want under 5 s", took)
	}
	if d := md.State.Duration; d < 2950*time.Millisecond || d > 3050*time.Millisecond {
		t.Errorf("duration %v", d)
	}
}

// audio is what the player found of the audio output, once it's known.
func (r *rig) audio() string {
	done := make(chan string, 1)
	r.p.post(func() { done <- r.p.audio })
	select {
	case a := <-done:
		return a
	case <-time.After(5 * time.Second):
		return "(the media thread didn't answer)"
	}
}

// The fixture plays to its end through a headless Gio window: the
// metadata (3 s, 320×180), the events in order, the three colours, the
// first frame within 2 s, in BGRA frames.
func TestMFPlaysFixture(t *testing.T) {
	r := newRig(t, mediatest.GreyGreyRed, false, image.Pt(320, 180))
	samples, first := r.playThrough()
	checkColors(t, samples)
	if first > 2*time.Second {
		t.Errorf("the first frame came %v after Play, want under 2 s", first)
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

// The fixture with no audio track plays where the runner has no audio
// output; the test says which case ran.
func TestMFSilent(t *testing.T) {
	if err := audioEndpoint(); err != nil {
		t.Logf("no default audio output (%v): the silent fixture plays as any file would here", err)
	} else {
		t.Log("the runner has an audio output")
	}
	r := newRig(t, mediatest.Silent, false, image.Pt(320, 180))
	samples, _ := r.playThrough()
	checkColors(t, samples)
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
	var grey, light, red []color.RGBA
	for _, s := range samples {
		switch {
		case s.at > 300*time.Millisecond && s.at < 700*time.Millisecond:
			grey = append(grey, s.c)
		case s.at > 1300*time.Millisecond && s.at < 1700*time.Millisecond:
			light = append(light, s.c)
		case s.at > 2300*time.Millisecond && s.at < 2700*time.Millisecond:
			red = append(red, s.c)
		}
	}
	if len(grey) == 0 || len(light) == 0 || len(red) < 3 {
		t.Fatalf("%d, %d, %d frames drawn in the three seconds", len(grey), len(light), len(red))
	}
	for _, c := range red {
		if !mediatest.Matches(2500*time.Millisecond, c) {
			t.Errorf("the third second drew %v, want %s", c, mediatest.Want(2500*time.Millisecond))
			break
		}
	}
	t.Logf("RGBA frames: grey 64 reads %v, 192 reads %v, red %v", grey[0], light[0], red[0])
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

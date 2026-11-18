//go:build windows && cgo

package mf

import (
	"fmt"
	"image"
	"image/color"
	"os"
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

// rig is a player drawn into a headless Gio window on Direct3D 11, as
// Clack's window draws it: the GPU's asks are the window's frames.
type rig struct {
	t      *testing.T
	win    *headless.Window
	p      *player
	o      *mediatest.Opener
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
	src, o := mediatest.Source(fmt.Sprintf("FMF%d", rigN.Add(1)), data, whole)
	r := &rig{t: t, win: win, o: o, size: size, readyC: make(chan struct{}, 1), evC: make(chan media.Event, 1024)}
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
	r.t.Fatalf("no %v within %v; events %v; state %+v", k, within, r.kinds(), r.p.State())
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
	r.t.Fatalf("no end within 15 s; events %v; state %+v; stats %+v", r.kinds(), r.p.State(), r.p.Stats())
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

// The fixture plays to its end through a headless Gio window: the
// metadata (3 s, 320×180), the events in order, the three colours, the
// first frame within 2 s.
func TestMFPlaysFixture(t *testing.T) {
	r := newRig(t, mediatest.GreyGreyRed, false, image.Pt(320, 180))
	samples, first := r.playThrough()
	checkColors(t, samples)
	if first > 2*time.Second {
		t.Errorf("the first frame came %v after Play, want under 2 s", first)
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

// The file with its moov after the mdat plays; the demuxer's reads jump
// to the moov.
func TestMFMoovAtEnd(t *testing.T) {
	r := newRig(t, mediatest.MoovAtEnd, false, image.Pt(320, 180))
	var mu sync.Mutex
	var reads []int64
	r.p.readLog = func(off int64, n int) { mu.Lock(); reads = append(reads, off); mu.Unlock() }
	samples, _ := r.playThrough()
	checkColors(t, samples)
	mu.Lock()
	defer mu.Unlock()
	moov := false
	for _, off := range reads {
		moov = moov || off >= mediatest.MoovAtEndMoov
	}
	if !moov {
		t.Errorf("no read at the moov (byte %d): reads at %v", mediatest.MoovAtEndMoov, reads)
	}
	t.Logf("the demuxer's reads at %v; GETs at %v", reads, r.o.Stats().Offsets)
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

// BGRA frames (CLACK_MF_FORMAT=bgra) show the same colours: red stays red.
func TestMFBGRA(t *testing.T) {
	t.Setenv("CLACK_MF_FORMAT", "bgra")
	r := newRig(t, mediatest.GreyGreyRed, false, image.Pt(320, 180))
	samples, _ := r.playThrough()
	checkColors(t, samples)
	if r.p.format != fmtBGRA {
		t.Errorf("format %d, want BGRA", r.p.format)
	}
}

// An engine refusing RGBA frames is made again for BGRA where it was: the
// playback goes on to the end, the colours right, its events once.
func TestMFFormatFallback(t *testing.T) {
	defer preferBGRA.Store(false)
	preferBGRA.Store(false)
	forceTransferFailures.Store(1)
	defer forceTransferFailures.Store(0)
	r := newRig(t, mediatest.GreyGreyRed, false, image.Pt(320, 180))
	samples, _ := r.playThrough()
	checkColors(t, samples)
	if !r.p.triedBGRA || r.p.format != fmtBGRA || !preferBGRA.Load() {
		t.Errorf("tried BGRA %v, format %d, preferred %v", r.p.triedBGRA, r.p.format, preferBGRA.Load())
	}
	n := 0
	for _, k := range r.kinds() {
		if k == media.LoadedMetadata {
			n++
		}
	}
	if n != 1 {
		t.Errorf("loadedmetadata %d times (events %v), want once", n, r.kinds())
	}
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

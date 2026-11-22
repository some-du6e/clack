//go:build darwin && cgo

package avf

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"os"
	"runtime"
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

// The main goroutine keeps the main thread, where TestMain runs the main
// run loop (AVFoundation posts some of its changes on the main queue,
// which Clack's NSApplication runs) while the tests run on others.
func init() { runtime.LockOSThread() }

func TestMain(m *testing.M) {
	code := make(chan int, 1)
	go func() {
		code <- m.Run()
		stopMainLoop()
	}()
	runMainLoop()
	os.Exit(<-code)
}

// rig is a player drawn into a headless Gio window on Metal, as Clack's
// window draws it: the GPU's asks are the window's frames.
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

// requests are the data requests the assets made, by file.
var (
	reqMu    sync.Mutex
	requests = map[string][]string{}
	notes    = map[string][]string{}
)

func init() {
	readLog = func(id string, off, n int64, toEnd bool) {
		reqMu.Lock()
		defer reqMu.Unlock()
		if toEnd {
			requests[id] = append(requests[id], fmt.Sprintf("%d-end", off))
		} else {
			requests[id] = append(requests[id], fmt.Sprintf("%d+%d", off, n))
		}
	}
	noteLog = func(id, line string) {
		reqMu.Lock()
		defer reqMu.Unlock()
		notes[id] = append(notes[id], line)
	}
}

func notesOf(id string) []string {
	reqMu.Lock()
	defer reqMu.Unlock()
	return append([]string(nil), notes[id]...)
}

func requestsOf(id string) []string {
	reqMu.Lock()
	defer reqMu.Unlock()
	return append([]string(nil), requests[id]...)
}

func newRig(t *testing.T, data []byte, whole bool, size, draw image.Point, setup ...func(*mediatest.Opener)) *rig {
	t.Helper()
	win, err := headless.NewWindow(size.X, size.Y)
	if err != nil {
		if os.Getenv("CLACK_MEDIA_TESTS") != "" {
			t.Fatal(err)
		}
		t.Skip(err)
	}
	id := fmt.Sprintf("FAV%d", rigN.Add(1))
	src, o := mediatest.Source(id, data, whole)
	for _, f := range setup {
		f(o)
	}
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
	r.p.SetDrawSize(draw)
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
		win.Release()
		t.Logf("%s: the asset's data requests: %v; GETs at %v", id, requestsOf(id), o.Stats().Offsets)
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

// sample is a drawn frame: at is the clock's position then, wall when
// the test drew it.
type sample struct {
	at   time.Duration
	wall time.Time
	c    color.RGBA
}

// playThrough plays to the end, drawing every new frame, and returns what
// each drawn frame showed and when the first one came.
func (r *rig) playThrough() ([]sample, time.Duration) {
	r.t.Helper()
	r.draw() // the first ask: the player takes Gio's device
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
			samples = append(samples, sample{r.p.State().Pos, time.Now(), c})
		}
	}
	r.t.Fatalf("no end within 15 s; events %v; state %+v; stats %+v", r.kinds(), r.p.State(), r.p.Stats())
	return nil, 0
}

// checkColors: the frames drawn well inside each second are the
// fixture's grey, light grey and red (the range, the sRGB texture, the
// channel order), several in each second the test drew through
// (colorReport).
func checkColors(t *testing.T, samples []sample) {
	t.Helper()
	errs, logs := colorReport(samples)
	for _, l := range logs {
		t.Log(l)
	}
	for _, e := range errs {
		t.Error(e)
	}
}

// colorReport is checkColors' verdict: each second's window (the 400 ms
// well inside it, by the position each draw read) wants several draws,
// each of the second's colour. A second the test's own draws stepped
// over doesn't count: a gap of 300 ms or more between two draws where the
// window lies (on a loaded runner one draw took longer than the second's
// 400 ms window: directories-usergroups-2's push ci 44999661783, 129
// draws and second 2 none; the gap can begin or end inside the window
// too, TestAVFSilent's 2 draws in second 1, slack-connect-hub's push ci
// 45034919995). Draws that go on while the position doesn't move through
// the window (it stood, then jumped) still fail, and so does a video that
// stops: the draws go on, showing the wrong colour. A short second logs
// the draws around it, the position each read and when.
func colorReport(samples []sample) (errs, logs []string) {
	stepped := func(w [2]time.Duration) (time.Duration, bool) {
		for i := 1; i < len(samples); i++ {
			a, b := samples[i-1], samples[i]
			if a.at < w[1] && b.at > w[0] {
				if gap := b.wall.Sub(a.wall); gap >= 300*time.Millisecond {
					return gap, true
				}
			}
		}
		return 0, false
	}
	around := func(w [2]time.Duration) string {
		if len(samples) == 0 {
			return "none"
		}
		t0 := samples[0].wall
		var b strings.Builder
		for i, s := range samples {
			near := s.at >= w[0]-200*time.Millisecond && s.at <= w[1]+200*time.Millisecond
			edge := i+1 < len(samples) && samples[i+1].at > w[0] && s.at < w[0] || i > 0 && samples[i-1].at < w[1] && s.at > w[1]
			if near || edge {
				fmt.Fprintf(&b, " %v@%v", s.at.Round(time.Millisecond), s.wall.Sub(t0).Round(time.Millisecond))
			}
		}
		return strings.TrimSpace(b.String())
	}
	for i, w := range [][2]time.Duration{{300 * time.Millisecond, 700 * time.Millisecond}, {1300 * time.Millisecond, 1700 * time.Millisecond}, {2300 * time.Millisecond, 2700 * time.Millisecond}} {
		n, bad := 0, 0
		for _, s := range samples {
			if s.at < w[0] || s.at > w[1] {
				continue
			}
			n++
			if !mediatest.Matches(s.at, s.c) {
				if bad < 3 {
					errs = append(errs, fmt.Sprintf("at %v the frame drawn was %v, want %s", s.at, s.c, mediatest.Want(s.at)))
				}
				bad++
			}
		}
		if n < 3 {
			if gap, ok := stepped(w); ok {
				logs = append(logs, fmt.Sprintf("second %d: %d frames drawn: the test didn't draw through it (one draw %v); the draws around it (position@wall) %s", i+1, n, gap, around(w)))
				continue
			}
			errs = append(errs, fmt.Sprintf("second %d: %d frames drawn, want several; the draws around it (position@wall) %s", i+1, n, around(w)))
		}
		logs = append(logs, fmt.Sprintf("second %d: %d frames drawn, %d off", i+1, n, bad))
	}
	return errs, logs
}

// colorReport's rule on draws made up for it: a stall of the test's draws
// across the whole window, or beginning inside it, or ending inside it,
// isn't the video's; a position that stands still and then jumps while
// the draws go on is, and so is a wrong colour.
func TestColorReportStalls(t *testing.T) {
	grey, light, red := color.RGBA{64, 64, 64, 255}, color.RGBA{192, 192, 192, 255}, color.RGBA{220, 30, 30, 255}
	colorAt := func(at time.Duration) color.RGBA {
		switch {
		case at < time.Second:
			return grey
		case at < 2*time.Second:
			return light
		}
		return red
	}
	t0 := time.Unix(0, 0)
	// play draws every 15 ms of wall time, the position following the
	// wall from 0 to 3 s, except where pos gives another (a position that
	// stands) or skip says the test drew nothing then
	play := func(skip func(time.Duration) bool, pos func(time.Duration) time.Duration) []sample {
		var out []sample
		for w := time.Duration(0); w <= 3*time.Second; w += 15 * time.Millisecond {
			if skip != nil && skip(w) {
				continue
			}
			at := w
			if pos != nil {
				at = pos(w)
			}
			out = append(out, sample{at: at, wall: t0.Add(w), c: colorAt(at)})
		}
		return out
	}
	between := func(a, b time.Duration) func(time.Duration) bool {
		return func(w time.Duration) bool { return w > a && w < b }
	}
	for _, c := range []struct {
		name    string
		samples []sample
		ok      bool
	}{
		{"every draw", play(nil, nil), true},
		{"a stall across second 1's window", play(between(250*time.Millisecond, 750*time.Millisecond), nil), true},
		{"a stall beginning inside it", play(between(310*time.Millisecond, 720*time.Millisecond), nil), true},
		{"a stall ending inside it", play(between(280*time.Millisecond, 670*time.Millisecond), nil), true},
		{"the position standing, then jumping", play(nil, func(w time.Duration) time.Duration {
			if w > 100*time.Millisecond && w < 690*time.Millisecond {
				return 100 * time.Millisecond
			}
			return w
		}), false},
		{"draws 210 ms apart in it (no gap of 300 ms)", play(func(w time.Duration) bool {
			return w >= 290*time.Millisecond && w <= 700*time.Millisecond && w != 495*time.Millisecond
		}, nil), false},
	} {
		errs, logs := colorReport(c.samples)
		if ok := len(errs) == 0; ok != c.ok {
			t.Errorf("%s: passes %v, want %v: %q (%q)", c.name, ok, c.ok, errs, logs)
		}
	}
	wrong := play(nil, nil)
	for i := range wrong {
		if wrong[i].at > 1300*time.Millisecond && wrong[i].at < 1700*time.Millisecond {
			wrong[i].c = grey // a video that stopped in second 1
		}
	}
	if errs, _ := colorReport(wrong); len(errs) == 0 {
		t.Error("a stopped video's second 2 passes")
	}
}

// The first player of the process takes AVFoundation's cold start (its
// frameworks, the decoder; 0.8 s to 2.3 s on the runners, against 0.04 s
// to 0.27 s for every later one), as Media Foundation's first does
// (TestMFColdStart): this one takes it, so the 2 s first-frame budget
// below measures warm players. It fails only past 10 s.
func TestAVFColdStart(t *testing.T) {
	t0 := time.Now()
	r := newRig(t, mediatest.GreyGreyRed, false, image.Pt(320, 180), image.Pt(320, 180))
	r.draw()
	r.p.Play()
	r.wait(media.FirstFrame, 30*time.Second)
	took := time.Since(t0)
	t.Logf("the process's first player: first frame %v after it was made", took)
	if took > 10*time.Second {
		t.Errorf("the first frame %v after the first player was made, want under 10 s", took)
	}
	r.p.Close()
}

// SKEPTIC note 3: the fixture plays to its end from Clack's ranged
// source through the resource loader: the metadata (3 s, 320×180), the
// events in order, the three colours (BGRA8Unorm_sRGB textures), the
// first frame within 2 s; the asset's data requests are logged.
func TestAVFPlaysFixture(t *testing.T) {
	r := newRig(t, mediatest.GreyGreyRed, false, image.Pt(320, 180), image.Pt(320, 180))
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

// Paused, the frame it shows reads back (media.FrameCopier: a tile keeps
// it when its idle player lets go, Chromium's idle suspend): light grey
// at 1.5 s, at the pixel buffer's size.
func TestAVFSnapshot(t *testing.T) {
	r := newRig(t, mediatest.GreyGreyRed, false, image.Pt(320, 180), image.Pt(320, 180))
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

// A seek while paused fires seeking and seeked and shows the frame there,
// light grey, within 500 ms; paused, 2 s of the window drawing cost no
// frame and no repaint.
func TestAVFSeekAndPause(t *testing.T) {
	r := newRig(t, mediatest.GreyGreyRed, false, image.Pt(320, 180), image.Pt(320, 180))
	r.draw()
	r.p.Play()
	r.wait(media.Playing, 10*time.Second)
	r.p.Pause()
	r.wait(media.Pause, 5*time.Second)
	t0 := time.Now()
	r.p.Seek(1500 * time.Millisecond)
	sk := r.wait(media.Seeked, 5*time.Second)
	var at time.Duration
	drawn := false // its own flag, not at == 0 (mf's TestMFSeekAndPause: a frame drawn within one clock tick)
	for time.Since(t0) < 2*time.Second {
		if c := r.draw(); mediatest.Matches(1500*time.Millisecond, c) {
			at, drawn = time.Since(t0), true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !drawn || at > 500*time.Millisecond {
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
		t.Errorf("paused for 2 s, drawn: %d frames, %d repaints, %d pulls; want none", st1.Transfers-st0.Transfers, n1-n0, st1.Ticks-st0.Ticks)
	}
}

// A file with its moov after the mdat and 4 MB before it plays; a data
// request gets to the moov.
func TestAVFMoovAtEnd(t *testing.T) {
	const pad = 4 << 20
	moov := int64(mediatest.MoovAtEndMoov + pad)
	r := newRig(t, mediatest.MoovAtEndPadded(pad), false, image.Pt(320, 180), image.Pt(320, 180))
	samples, _ := r.playThrough()
	checkColors(t, samples)
	got := false
	for _, q := range requestsOf(r.id) {
		var off, n int64
		if _, err := fmt.Sscanf(q, "%d+%d", &off, &n); err == nil {
			got = got || off <= moov && moov < off+n
		} else if _, err := fmt.Sscanf(q, "%d-end", &off); err == nil {
			got = got || off <= moov
		}
	}
	if !got {
		t.Errorf("no data request got to the moov (byte %d): %v", moov, requestsOf(r.id))
	}
}

// A file whose fetch fails before its metadata (signed out, a sign-in
// page, a refused GET) is error 4, as Chromium's MediaLoadingFailed gives
// a src it couldn't load, and as the fake does.
func TestAVFFetchFails(t *testing.T) {
	r := newRig(t, mediatest.GreyGreyRed, false, image.Pt(320, 180), image.Pt(320, 180), func(o *mediatest.Opener) {
		o.Fail, o.FailStatus = errors.New("slack: not a file URL"), 200
	})
	r.draw()
	r.p.Play()
	if ev := r.wait(media.Error, 10*time.Second); ev.State.Err != media.ErrSrcNotSupported {
		t.Errorf("error %d, want 4", ev.State.Err)
	}
}

// A server ignoring ranges (a 200): the backend is told the file can't be
// read by ranges. It plays it in order, or refuses it with error 4 (the
// brief's fallback: Slack's error look); the test says which.
func TestAVFNotSeekable(t *testing.T) {
	r := newRig(t, mediatest.GreyGreyRed, true, image.Pt(320, 180), image.Pt(320, 180))
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

// The fixture with no audio track plays.
func TestAVFSilent(t *testing.T) {
	r := newRig(t, mediatest.Silent, false, image.Pt(320, 180), image.Pt(320, 180))
	samples, _ := r.playThrough()
	checkColors(t, samples)
}

// SKEPTIC note 3: asked for frames of 160×90 from the 320×180 fixture,
// the output gives them at that size (else it says what it gave, and the
// memory each frame costs over the drawn size).
func TestAVFDrawnSize(t *testing.T) {
	r := newRig(t, mediatest.GreyGreyRed, false, image.Pt(160, 90), image.Pt(160, 90))
	r.draw()
	r.p.Play()
	r.wait(media.FirstFrame, 10*time.Second)
	for i := 0; i < 10; i++ {
		r.draw()
		time.Sleep(15 * time.Millisecond)
	}
	got := r.p.PixelSize()
	if got != image.Pt(160, 90) {
		t.Errorf("frames %v, want the drawn 160×90 (%d bytes a frame over it)", got, (got.X*got.Y-160*90)*4)
	}
	t.Logf("asked for 160×90, the output gave %v", got)
}

// mdatTail is the fixture with a 50 MB free box after it: how much the
// asset reads when there's much more file than it plays (its to-end
// requests are logged).
func mdatTail() []byte {
	f := append([]byte(nil), mediatest.GreyGreyRed...)
	free := make([]byte, 50<<20)
	binary.BigEndian.PutUint32(free, uint32(len(free)))
	copy(free[4:], "free")
	return append(f, free...)
}

// A file much longer than what plays: the asset's reads are logged, and
// what the source fetched while 3 s played.
func TestAVFReadsAhead(t *testing.T) {
	data := mdatTail()
	r := newRig(t, data, false, image.Pt(320, 180), image.Pt(320, 180))
	r.playThrough()
	st := r.o.Stats()
	t.Logf("50 MB past a 3 s video: the source fetched %d bytes in %d GETs (%v); requests %v", st.Bytes, len(st.Offsets), st.Offsets, requestsOf(r.id))
	rs := r.p.rs.Stats()
	t.Logf("the source: %d GETs, %d merged, %d restarts; the requests to the end: %q", rs.Requests, rs.Merged, rs.Restarts, notesOf(r.id))
	// round 4 step 5: AVFoundation's two requests to the end (pacing holds
	// neither in a file under 30 s) share their bytes, not reading the
	// file once each (they read 52 to 103 MB of the 52). It drops the
	// first and asks again from where the asset's bytes end: with the
	// responds queued unbounded the first had read up to 28 MB past that,
	// out of the source's cache, and 17 MB came twice (2 of 3 runs on
	// f7dca4f1); bounded, the GET's read-ahead was evicted before the
	// bytes read again, and 2 or 3 blocks came twice (222f3fc9). A body's
	// read in flight as another GET brings the same bytes may come twice.
	if again := st.Again(); again > 64<<10 {
		t.Errorf("%d of the %d bytes the source fetched came twice (GETs at %v, their spans %v)", again, st.Bytes, st.Offsets, st.Spans)
	}
}

// Closed, every player lets go of all it held: the player, its frames'
// pixel buffers and textures, its data requests.
func TestAVFCloseReleases(t *testing.T) {
	func() {
		r := newRig(t, mediatest.GreyGreyRed, false, image.Pt(160, 90), image.Pt(160, 90))
		r.draw()
		r.p.Play()
		r.wait(media.Playing, 10*time.Second)
		r.wait(media.FirstFrame, 5*time.Second)
		r.p.Close()
	}()
	runtime.GC()
	deadline := time.Now().Add(5 * time.Second)
	var l [4]int32
	for time.Now().Before(deadline) {
		if l = live(); l == ([4]int32{}) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if l != ([4]int32{}) {
		t.Errorf("live after Close: players %d, pixel buffers %d, textures %d, data requests %d", l[0], l[1], l[2], l[3])
	}
}

// Closed while its frames are drawn, the same ops replayed frame after
// frame (quiet frames): an ask racing Close either hands out a texture
// with Gio's own reference or nothing, the frames after Close draw the
// still, and nothing is freed under Gio.
func TestAVFCloseWhileDrawn(t *testing.T) {
	r := newRig(t, mediatest.GreyGreyRed, false, image.Pt(320, 180), image.Pt(320, 180))
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

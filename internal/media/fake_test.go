package media_test

import (
	"errors"
	"image"
	"image/color"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/media"
	"github.com/some-du6e/clack/internal/media/mediatest"
)

// recorder keeps a player's events, and counts frameReady.
type recorder struct {
	mu     sync.Mutex
	events []media.Event
	ready  atomic.Int64
	ch     chan media.Event
}

// inOrder is whether want's events are in got, in that order.
func inOrder(got, want []media.EventKind) bool {
	i := 0
	for _, k := range got {
		if i < len(want) && k == want[i] {
			i++
		}
	}
	return i == len(want)
}

func newRecorder() *recorder { return &recorder{ch: make(chan media.Event, 1024)} }

func (r *recorder) frameReady() { r.ready.Add(1) }

func (r *recorder) onEvent(ev media.Event) {
	r.mu.Lock()
	r.events = append(r.events, ev)
	r.mu.Unlock()
	select {
	case r.ch <- ev:
	default:
	}
}

// wait waits for an event of kind k, and returns it.
func (r *recorder) wait(t *testing.T, k media.EventKind, within time.Duration) media.Event {
	t.Helper()
	deadline := time.After(within)
	for {
		select {
		case ev := <-r.ch:
			if ev.Kind == k {
				return ev
			}
		case <-deadline:
			t.Fatalf("no %v within %v; got %v", k, within, r.kinds())
		}
	}
}

func (r *recorder) kinds() []media.EventKind {
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

func (r *recorder) count(k media.EventKind) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, ev := range r.events {
		if ev.Kind == k {
			n++
		}
	}
	return n
}

// gpu asks for h's frame every 10 ms (a window drawing it) until stop,
// keeping the colours it was handed and when.
type gpu struct {
	stop chan struct{}
	done chan struct{}
	mu   sync.Mutex
	seen []shown
}

type shown struct {
	at time.Duration // the player's position then
	c  color.NRGBA
}

func draw(h media.Player) *gpu {
	g := &gpu{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(g.done)
		tk := time.NewTicker(10 * time.Millisecond)
		defer tk.Stop()
		for {
			select {
			case <-g.stop:
				return
			case <-tk.C:
				if c, ok := media.FakeAsk(h); ok {
					g.mu.Lock()
					g.seen = append(g.seen, shown{h.State().Pos, c})
					g.mu.Unlock()
				}
			}
		}
	}()
	return g
}

func (g *gpu) end() []shown {
	close(g.stop)
	<-g.done
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.seen
}

func open(t *testing.T, m *media.Manager, id string, data []byte) (*media.Handle, *recorder, *mediatest.Opener) {
	t.Helper()
	src, o := mediatest.Source(id, data, false)
	r := newRecorder()
	h, err := m.Open(src, r.frameReady, r.onEvent)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	return h, r, o
}

func fakeManager() *media.Manager {
	return media.NewManager(media.NewFake(media.FakeOptions{Log: quiet}))
}

// The fake plays the fixture as a <video> does: metadata (3 s, 320×180),
// data and the first frame, play and playing, a timeupdate every 250 ms,
// the fixture's colours as it goes, then pause and ended at 3 s.
func TestFakePlaysFixture(t *testing.T) {
	m := fakeManager()
	h, r, _ := open(t, m, "FF1", mediatest.GreyGreyRed)
	h.Play()
	g := draw(h)
	md := r.wait(t, media.LoadedMetadata, 5*time.Second)
	if d := md.State.Duration; d < 2950*time.Millisecond || d > 3050*time.Millisecond || md.State.Size.X != 320 || md.State.Size.Y != 180 {
		t.Errorf("loadedmetadata: %v %v, want 3 s, 320×180", d, md.State.Size)
	}
	end := r.wait(t, media.Ended, 10*time.Second)
	seen := g.end()
	if !end.State.Ended || !end.State.Paused || end.State.Pos != end.State.Duration {
		t.Errorf("ended: %+v", end.State)
	}
	// play() can come before the metadata (play, waiting, …, playing) or
	// after it (…, play, playing)
	got := r.kinds()
	for _, order := range [][]media.EventKind{
		{media.DurationChange, media.LoadedMetadata, media.LoadedData, media.FirstFrame, media.Playing, media.Pause, media.Ended},
		{media.Play, media.Playing},
	} {
		if !inOrder(got, order) {
			t.Errorf("events %v, want %v in order", got, order)
		}
	}
	if n := len(got); n != 9 && n != 8 || got[n-1] != media.Ended || (n == 9) != (got[0] == media.Play && got[1] == media.Waiting) {
		t.Errorf("events %v", got)
	}
	if n := r.count(media.TimeUpdate); n < 9 || n > 16 {
		t.Errorf("%d timeupdates over 3 s, want about 12 (every 250 ms)", n)
	}
	bad, n := 0, 0
	for _, s := range seen {
		if s.at > 0 {
			n++
			if !mediatest.Matches(s.at, color.RGBA(s.c)) {
				bad++
				if bad < 4 {
					t.Errorf("at %v the frame was %v, want %s", s.at, s.c, mediatest.Want(s.at))
				}
			}
		}
	}
	if n < 100 {
		t.Errorf("only %d frames drawn over 3 s", n)
	}
	if fr := h.Stats().Frames; fr < 60 || fr > 100 {
		t.Errorf("%d frames published over 3 s at 30 fps", fr)
	}
}

// Without the GPU asking (minimized, covered, scrolled off), frames stop
// a second after the last ask, and so do the ticks; an ask brings them
// back. Paused, nothing is published or polled.
func TestFakeFramesNeedAsks(t *testing.T) {
	m := fakeManager()
	h, r, _ := open(t, m, "FF2", mediatest.GreyGreyRed)
	h.SetLoop(true)
	h.Play()
	r.wait(t, media.Playing, 5*time.Second)
	time.Sleep(1500 * time.Millisecond) // no asks after Play's
	st1 := h.Stats()
	time.Sleep(500 * time.Millisecond)
	st2 := h.Stats()
	if st2.Frames != st1.Frames || st2.Ticks != st1.Ticks || r.ready.Load() > 40 {
		t.Errorf("no asks for over a second: frames %d→%d, ticks %d→%d, %d repaints", st1.Frames, st2.Frames, st1.Ticks, st2.Ticks, r.ready.Load())
	}
	if p := h.State().Pos; p < 1500*time.Millisecond {
		t.Errorf("the clock stopped with the frames: at %v", p)
	}
	g := draw(h)
	time.Sleep(500 * time.Millisecond)
	g.end()
	st3 := h.Stats()
	if st3.Frames-st2.Frames < 10 {
		t.Errorf("asked again: %d frames in 0.5 s", st3.Frames-st2.Frames)
	}
	h.Pause()
	r.wait(t, media.Pause, time.Second)
	g = draw(h)
	time.Sleep(100 * time.Millisecond)
	st4 := h.Stats()
	time.Sleep(time.Second)
	g.end()
	st5 := h.Stats()
	if st5.Frames != st4.Frames || st5.Ticks != st4.Ticks {
		t.Errorf("paused, drawn: frames %d→%d, ticks %d→%d in 1 s, want none", st4.Frames, st5.Frames, st4.Ticks, st5.Ticks)
	}
}

// A seek while paused shows the frame there (once); seeking and seeked
// fire.
func TestFakeSeekPaused(t *testing.T) {
	m := fakeManager()
	h, r, _ := open(t, m, "FF3", mediatest.GreyGreyRed)
	r.wait(t, media.FirstFrame, 5*time.Second)
	if c, ok := media.FakeAsk(h); !ok || !mediatest.Matches(0, color.RGBA(c)) {
		t.Errorf("the first frame: %v %v, want grey 64", c, ok)
	}
	before := r.ready.Load()
	h.Seek(1500 * time.Millisecond)
	ev := r.wait(t, media.Seeked, 2*time.Second)
	if ev.State.Pos != 1500*time.Millisecond || !ev.State.Paused {
		t.Errorf("seeked: %+v", ev.State)
	}
	if c, _ := media.FakeAsk(h); !mediatest.Matches(1500*time.Millisecond, color.RGBA(c)) {
		t.Errorf("after the seek: %v, want grey 192", c)
	}
	if n := r.ready.Load() - before; n != 1 {
		t.Errorf("%d repaints for a seek while paused, want 1", n)
	}
	if r.count(media.Seeking) != 1 {
		t.Errorf("events %v", r.kinds())
	}
}

// A looping video seeks to 0 at its end instead of ending.
func TestFakeLoops(t *testing.T) {
	m := fakeManager()
	h, r, _ := open(t, m, "FF4", mediatest.Silent)
	h.SetLoop(true)
	h.Seek(2700 * time.Millisecond)
	h.Play()
	g := draw(h)
	r.wait(t, media.Seeked, 5*time.Second) // the Seek
	r.wait(t, media.Seeked, 5*time.Second) // the loop
	g.end()
	if r.count(media.Ended) != 0 || h.State().Paused {
		t.Errorf("events %v: a loop ended", r.kinds())
	}
	if p := h.State().Pos; p > time.Second {
		t.Errorf("after the loop at %v", p)
	}
}

// What can't play errors with HTML5's codes: a source that fails to open
// (2, network), bytes that aren't a video (4), a decode error (3).
func TestFakeErrors(t *testing.T) {
	m := fakeManager()
	src, o := mediatest.Source("FF5", mediatest.GreyGreyRed, false)
	o.Fail = errors.New("slack: not a file URL")
	r := newRecorder()
	h, err := m.Open(src, r.frameReady, r.onEvent)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if ev := r.wait(t, media.Error, 5*time.Second); ev.State.Err != media.ErrSrcNotSupported {
		t.Errorf("a source that fails before its metadata: error %d, want 4 (Chromium's for a src)", ev.State.Err)
	}
	_, r2, _ := open(t, m, "FF6", []byte("<html>sign in</html>"))
	if ev := r2.wait(t, media.Error, 5*time.Second); ev.State.Err != media.ErrSrcNotSupported {
		t.Errorf("a page: error %d, want 4", ev.State.Err)
	}
	m3 := media.NewManager(media.NewFake(media.FakeOptions{Err: media.ErrDecode, Log: quiet}))
	h3, r3, _ := open(t, m3, "FF7", mediatest.GreyGreyRed)
	h3.Play()
	if ev := r3.wait(t, media.Error, 5*time.Second); ev.State.Err != media.ErrDecode || ev.State.Duration == 0 {
		t.Errorf("a decode error: %+v", ev.State)
	}
	time.Sleep(300 * time.Millisecond)
	if r3.count(media.Playing) != 0 || h3.Stats().Frames != 0 {
		t.Errorf("a player with an error played: %v", r3.kinds())
	}
}

// Close ends the player: its events, its source's GET, its place in the
// manager.
func TestFakeClose(t *testing.T) {
	m := fakeManager()
	h, r, o := open(t, m, "FF8", mediatest.GreyGreyRed)
	h.Play()
	r.wait(t, media.Playing, 5*time.Second)
	if m.Len() != 1 {
		t.Errorf("Len %d", m.Len())
	}
	h.Close()
	select {
	case <-h.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the handle's goroutine outlived Close")
	}
	if m.Len() != 0 {
		t.Errorf("Len %d after Close", m.Len())
	}
	deadline := time.Now().Add(2 * time.Second)
	for o.Stats().Open != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := o.Stats().Open; n != 0 {
		t.Errorf("%d bodies open after Close", n)
	}
	n := r.ready.Load()
	time.Sleep(200 * time.Millisecond)
	if r.ready.Load() != n {
		t.Error("frames after Close")
	}
}

// The fake draws its still, the poster, until its first frame and after
// an error, as the real backends draw theirs.
func TestFakePoster(t *testing.T) {
	poster := paint.NewImageOp(image.NewRGBA(image.Rect(0, 0, 32, 18)))
	m := media.NewManager(media.NewFake(media.FakeOptions{Log: quiet, FailAfter: 300 * time.Millisecond}))
	src, o := mediatest.Source("FF9", mediatest.GreyGreyRed, false)
	o.Delay = 200 * time.Millisecond // the first frame comes late
	r := newRecorder()
	h, err := m.Open(src, r.frameReady, r.onEvent)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	h.Op(poster, image.Pt(320, 180))
	if got := media.FakeImage(h); got != poster {
		t.Error("before the first frame: not the poster")
	}
	r.wait(t, media.FirstFrame, 5*time.Second)
	if got := media.FakeImage(h); got == poster {
		t.Error("after the first frame: still the poster")
	}
	h.Play()
	g := draw(h)
	r.wait(t, media.Error, 5*time.Second)
	g.end()
	if got := media.FakeImage(h); got != poster {
		t.Error("after an error: not the poster")
	}
}

// A player that fails mid-file isn't playing: the timeupdates stop and
// the manager doesn't list it, though HTML leaves it unpaused.
func TestManagerFailedNotPlaying(t *testing.T) {
	m := media.NewManager(media.NewFake(media.FakeOptions{Log: quiet, FailAfter: 500 * time.Millisecond}))
	h, r, _ := open(t, m, "FF10", mediatest.GreyGreyRed)
	h.Play()
	g := draw(h)
	defer g.end()
	ev := r.wait(t, media.Error, 5*time.Second)
	if ev.State.Err != media.ErrNetwork || ev.State.Paused {
		t.Errorf("error %+v, want 2 and not paused", ev.State)
	}
	n := r.count(media.TimeUpdate)
	time.Sleep(700 * time.Millisecond)
	if d := r.count(media.TimeUpdate) - n; d != 0 {
		t.Errorf("%d timeupdates after the error", d)
	}
	if pl := m.Playing(); len(pl) != 0 {
		t.Errorf("%d listed as playing after the error", len(pl))
	}
	if h.Events() != nil {
		t.Error("a handle's Events isn't nil: a reader there would take events from the coordinator")
	}
}

// Close ends the source's GET mid-body: a big file on a slow link is
// still being read ahead when the player closes.
func TestFakeCloseEndsGET(t *testing.T) {
	m := fakeManager()
	src, o := mediatest.Source("FF11", bigFile(40*mb), false)
	o.Delay = 5 * time.Millisecond
	r := newRecorder()
	h, err := m.Open(src, r.frameReady, r.onEvent)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if n := o.Stats().Open; n != 1 {
		t.Fatalf("%d bodies open before Close, want the one reading ahead", n)
	}
	h.Close()
	<-h.Done()
	deadline := time.Now().Add(2 * time.Second)
	for o.Stats().Open != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := o.Stats().Open; n != 0 {
		t.Errorf("%d bodies open after Close", n)
	}
}

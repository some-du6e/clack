package media

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/op/paint"
)

// The fake backend plays a file for tests, behaving as a real backend
// does from outside: it reads the file through a RangeSource (its moov
// gives the duration and size), plays on the wall clock, fires the
// HTMLMediaElement events in their order, publishes a frame per frame
// interval only while it plays and the GPU asked for one within the last
// second, and draws its frames as a live image (Gio patch 35) of solid
// colours: the frame at t is Color(t).

// FakeOptions are a fake backend's.
type FakeOptions struct {
	// Color is the frame's colour at t; nil is grey, light grey, red for
	// the first, second and third second (the fixtures' colours).
	Color func(t time.Duration) color.NRGBA
	// FPS is the frame rate, 30 when 0.
	FPS float64
	// Err fails every player with this MediaError code once its metadata
	// is read.
	Err int
	// FailAfter, when set, fails a playing player with error 2 (network)
	// once it played that long.
	FailAfter time.Duration
	// Log is the RangeSource's log (nil: log.Printf).
	Log func(format string, args ...any)
}

// NewFake is the fake backend.
func NewFake(opt FakeOptions) Backend {
	if opt.FPS <= 0 {
		opt.FPS = 30
	}
	if opt.Color == nil {
		opt.Color = fixtureColor
	}
	return &fakeBackend{opt: opt}
}

// fixtureColor is the fixtures' frame colours: 0x404040, 0xc0c0c0, red.
func fixtureColor(t time.Duration) color.NRGBA {
	switch {
	case t < time.Second:
		return color.NRGBA{R: 0x40, G: 0x40, B: 0x40, A: 0xff}
	case t < 2*time.Second:
		return color.NRGBA{R: 0xc0, G: 0xc0, B: 0xc0, A: 0xff}
	}
	return color.NRGBA{R: 0xff, A: 0xff}
}

type fakeBackend struct{ opt FakeOptions }

func (b *fakeBackend) Name() string     { return "fake" }
func (b *fakeBackend) Available() error { return nil }

func (b *fakeBackend) New(src Source, frameReady func()) (Player, error) {
	if src.Open == nil {
		return nil, errors.New("media: a source with no Open")
	}
	p := &fakePlayer{
		opt:        b.opt,
		rs:         NewRangeSource(src, RangeOptions{Log: b.opt.Log}),
		frameReady: frameReady,
		events:     make(chan Event, 64),
		wake:       make(chan struct{}, 1),
		quit:       make(chan struct{}),
		frames:     map[image.Point]*fakeFrames{},
		st:         State{Paused: true, Volume: 1},
		rate:       1,
	}
	p.lastAsk.Store(time.Now().UnixNano())
	go p.run()
	return p, nil
}

// fakePlayer is a fake <video>. Its controls are queued for its goroutine
// (the media thread); State is a snapshot under mu.
type fakePlayer struct {
	opt        FakeOptions
	rs         *RangeSource
	frameReady func()
	events     chan Event
	wake       chan struct{} // a control queued, or an ask after a gap
	quit       chan struct{}
	quitOnce   sync.Once

	qmu   sync.Mutex
	queue []func()

	mu     sync.Mutex
	st     State
	rate   float64
	base   time.Duration // the position when playing last (re)started
	since  time.Time     // when
	loaded bool
	frame  int64 // the frame published, -1 none
	color  color.NRGBA
	shown  bool // a frame is published
	// startAt is a seek before the metadata
	startAt  time.Duration
	hasStart bool
	frames   map[image.Point]*fakeFrames
	last     *fakeFrames // Op's last

	lastAsk                          atomic.Int64 // unix ns
	nFrames, nTicks, nAsks, nDropped atomic.Int64
}

func (p *fakePlayer) post(f func()) {
	p.qmu.Lock()
	p.queue = append(p.queue, f)
	p.qmu.Unlock()
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *fakePlayer) Play()                { p.post(p.play) }
func (p *fakePlayer) Pause()               { p.post(p.pause) }
func (p *fakePlayer) Seek(t time.Duration) { p.post(func() { p.seek(t) }) }
func (p *fakePlayer) SetVolume(v float64) {
	p.post(func() { p.mu.Lock(); p.st.Volume = min(max(v, 0), 1); p.mu.Unlock() })
}
func (p *fakePlayer) SetMuted(m bool) { p.post(func() { p.mu.Lock(); p.st.Muted = m; p.mu.Unlock() }) }
func (p *fakePlayer) SetLoop(l bool)  { p.post(func() { p.mu.Lock(); p.st.Loop = l; p.mu.Unlock() }) }
func (p *fakePlayer) SetRate(r float64) {
	p.post(func() {
		if r <= 0 {
			return
		}
		p.mu.Lock()
		p.rebase()
		p.rate = r
		p.mu.Unlock()
	})
}
func (p *fakePlayer) SetDrawSize(image.Point) {}

func (p *fakePlayer) State() State {
	p.mu.Lock()
	defer p.mu.Unlock()
	st := p.st
	st.Pos = p.pos()
	return st
}

func (p *fakePlayer) Events() <-chan Event { return p.events }

func (p *fakePlayer) Stats() Stats {
	rs := p.rs.Stats()
	n := p.nFrames.Load()
	return Stats{Frames: n, Transfers: n, Ticks: p.nTicks.Load(), Asks: p.nAsks.Load(),
		Requests: rs.Requests, BytesRead: rs.BytesRead, Dropped: p.nDropped.Load()}
}

// Close ends the player: its clock stops where it was (a <video> taken
// out of the page plays nothing), and its goroutine ends.
func (p *fakePlayer) Close() {
	p.quitOnce.Do(func() {
		p.mu.Lock()
		p.st.Pos = p.pos()
		p.st.Paused = true
		p.mu.Unlock()
		close(p.quit)
	})
}

// Op is a live image (patch 35) of the frame published, size big: solid
// frames of the frame's colour, transparent before the first.
func (p *fakePlayer) Op(still paint.ImageOp, size image.Point) paint.ImageOp {
	if size.X <= 0 || size.Y <= 0 {
		return paint.ImageOp{}
	}
	p.mu.Lock()
	f := p.frames[size]
	if f == nil {
		if len(p.frames) > 4 {
			clear(p.frames)
		}
		f = &fakeFrames{p: p, size: size, imgs: map[color.NRGBA]paint.ImageOp{}}
		p.frames[size] = f
	}
	p.last = f
	p.mu.Unlock()
	f.mu.Lock()
	f.still = still // drawn until the first frame, and after an error
	f.mu.Unlock()
	// NewLiveImageOp takes the image's size from it: that isn't the GPU's
	// ask
	f.making.Store(true)
	defer f.making.Store(false)
	return liveOp(f, still)
}

// CopyFrame is the frame shown now (FrameCopier): the frame's colour at
// the size Op last drew.
func (p *fakePlayer) CopyFrame() (*image.RGBA, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.shown || p.last == nil {
		return nil, false
	}
	img := image.NewRGBA(image.Rectangle{Max: p.last.size})
	c := color.RGBA{R: p.color.R, G: p.color.G, B: p.color.B, A: 255}
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	return img, true
}

// FakeImage is the image a fake player's live op hands the GPU now (tests
// that draw nothing: the poster until its first frame, a frame's colour
// after), as the GPU's ask.
func FakeImage(pl Player) paint.ImageOp {
	if h, ok := pl.(*Handle); ok {
		pl = h.Player
	}
	p, ok := pl.(*fakePlayer)
	if !ok {
		return paint.ImageOp{}
	}
	p.mu.Lock()
	f := p.last
	p.mu.Unlock()
	if f == nil {
		return paint.ImageOp{}
	}
	return f.Image()
}

// FakeAsk is the GPU asking a fake player for its frame, for tests that
// draw nothing: the ask its live image gets (which keeps frames coming),
// and the colour of the frame it hands out; false before the first frame
// or for a player that isn't the fake's.
func FakeAsk(pl Player) (color.NRGBA, bool) {
	if h, ok := pl.(*Handle); ok {
		pl = h.Player
	}
	p, ok := pl.(*fakePlayer)
	if !ok {
		return color.NRGBA{}, false
	}
	p.mu.Lock()
	f := p.last
	p.mu.Unlock()
	if f == nil {
		p.Op(paint.ImageOp{}, image.Pt(16, 16))
		p.mu.Lock()
		f = p.last
		p.mu.Unlock()
	}
	f.Image()
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.color, p.shown
}

// fakeFrames is a player's frames at one size.
type fakeFrames struct {
	p      *fakePlayer
	size   image.Point
	making atomic.Bool
	mu     sync.Mutex
	imgs   map[color.NRGBA]paint.ImageOp
	still  paint.ImageOp
}

// Image is the frame to draw now, asked by the GPU as it draws a frame.
func (f *fakeFrames) Image() paint.ImageOp {
	p := f.p
	if !f.making.Load() {
		now := time.Now().UnixNano()
		if prev := p.lastAsk.Swap(now); now-prev > int64(time.Second) {
			select { // asked again after a gap: frames go on
			case p.wake <- struct{}{}:
			default:
			}
		}
		p.nAsks.Add(1)
	}
	p.mu.Lock()
	c, shown := p.color, p.shown && p.st.Err == 0
	p.mu.Unlock()
	f.mu.Lock()
	defer f.mu.Unlock()
	if !shown {
		// the poster, as the real backends draw their still (stretched over
		// the frame's size, as theirs); while the live op is made, a frame
		// of its size, which the op's size is taken from
		if !f.making.Load() && f.still.Size() != (image.Point{}) {
			return f.still
		}
		c = color.NRGBA{}
	}
	op, ok := f.imgs[c]
	if !ok {
		m := image.NewRGBA(image.Rectangle{Max: f.size})
		draw.Draw(m, m.Bounds(), &image.Uniform{C: c}, image.Point{}, draw.Src)
		op = paint.NewImageOp(m)
		f.imgs[c] = op
	}
	return op
}

// pos is the position now. Call with mu held.
func (p *fakePlayer) pos() time.Duration {
	if p.st.Paused || p.st.Ended || p.st.Waiting || p.st.Err != 0 {
		return p.st.Pos
	}
	t := p.base + time.Duration(float64(time.Since(p.since))*p.rate)
	if p.st.Duration > 0 && t > p.st.Duration {
		t = p.st.Duration
	}
	return t
}

// rebase makes the position now the base of the clock. Call with mu held.
func (p *fakePlayer) rebase() {
	p.st.Pos = p.pos()
	p.base, p.since = p.st.Pos, time.Now()
}

// emit sends ev with the state now; a full channel drops it.
func (p *fakePlayer) emit(k EventKind) {
	select {
	case p.events <- Event{Kind: k, State: p.State()}:
	default:
		p.nDropped.Add(1)
	}
}

func (p *fakePlayer) run() {
	defer close(p.events)
	defer p.rs.Close()
	loaded := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { loaded <- p.load(ctx) }()
	var tick *time.Ticker
	var end *time.Timer
	defer func() {
		if tick != nil {
			tick.Stop()
		}
		if end != nil {
			end.Stop()
		}
	}()
	interval := time.Duration(float64(time.Second) / p.opt.FPS / 2)
	interval = min(max(interval, 4*time.Millisecond), 20*time.Millisecond)
	for {
		var tc, ec <-chan time.Time
		if tick != nil {
			tc = tick.C
		}
		if end != nil {
			ec = end.C
		}
		select {
		case <-p.quit:
			return
		case err := <-loaded:
			p.onLoaded(err)
		case <-p.wake:
			p.qmu.Lock()
			q := p.queue
			p.queue = nil
			p.qmu.Unlock()
			for _, f := range q {
				f()
			}
		case <-tc:
			p.nTicks.Add(1)
			p.advance()
		case <-ec:
			end = nil
			p.advance() // the end, while nothing draws it
		}
		// poll for frames only while playing and asked for within the last
		// second; otherwise only the end is waited for (the clock and the
		// sound go on)
		p.mu.Lock()
		playing := p.loaded && !p.st.Paused && !p.st.Ended && !p.st.Waiting && p.st.Err == 0
		asked := time.Now().UnixNano()-p.lastAsk.Load() < int64(time.Second)
		left := time.Duration(float64(p.st.Duration-p.pos())/p.rate) + time.Millisecond
		p.mu.Unlock()
		switch {
		case playing && asked && tick == nil:
			tick = time.NewTicker(interval)
		case !(playing && asked) && tick != nil:
			tick.Stop()
			tick = nil
		}
		switch {
		case playing && !asked && end == nil:
			end = time.NewTimer(max(left, 0))
		case !(playing && !asked) && end != nil:
			end.Stop()
			end = nil
		}
	}
}

// load reads the file's metadata through the source: its first bytes,
// then its moov wherever it is.
func (p *fakePlayer) load(ctx context.Context) error {
	if _, err := p.rs.Info(ctx); err != nil {
		return err
	}
	// read where the boxes lead, as a demuxer does: the start, then the
	// moov wherever it is
	buf := make([]byte, 64<<10)
	for i := 0; i < 256; i++ {
		info := p.rs.Probe()
		if info.Entry != "" {
			if info.Entry == "none" || info.Duration <= 0 || info.Width <= 0 {
				return errNotSupported
			}
			return nil
		}
		if info.NextAt < 0 {
			return errNotSupported
		}
		_, err := p.rs.ReadSome(ctx, buf, info.NextAt)
		if errors.Is(err, io.EOF) {
			if info := p.rs.Probe(); info.Entry != "" && info.Entry != "none" {
				return nil
			}
			return errNotSupported
		}
		if err != nil {
			return err
		}
	}
	return errNotSupported
}

var errNotSupported = errors.New("media: no video the fake can play")

func (p *fakePlayer) onLoaded(err error) {
	if err != nil {
		// before the metadata, a failed fetch is error 4 as a bad file is:
		// Chromium's MediaLoadingFailed for a src (Slack's case)
		p.fail(ErrSrcNotSupported)
		return
	}
	info := p.rs.Probe()
	p.mu.Lock()
	p.loaded = true
	p.st.Duration = info.Duration
	p.st.Size = image.Pt(info.Width, info.Height)
	p.st.Ready = HaveMetadata
	p.frame = -1
	p.mu.Unlock()
	p.emit(DurationChange)
	p.emit(LoadedMetadata)
	if p.opt.Err != 0 {
		p.fail(p.opt.Err)
		return
	}
	p.mu.Lock()
	start, seek := p.startAt, p.hasStart
	p.hasStart = false
	p.mu.Unlock()
	if seek {
		p.seek(start)
	}
	p.mu.Lock()
	p.st.Ready = HaveEnoughData
	p.mu.Unlock()
	p.emit(LoadedData)
	p.publish(true)
	p.emit(FirstFrame)
	p.mu.Lock()
	waiting := p.st.Waiting
	if waiting {
		p.st.Waiting = false
		p.base, p.since = p.st.Pos, time.Now()
	}
	p.mu.Unlock()
	if waiting {
		p.emit(Playing)
	}
}

func (p *fakePlayer) fail(code int) {
	p.mu.Lock()
	p.rebase() // the clock stops where the error came
	p.st.Err = code
	p.mu.Unlock()
	p.emit(Error)
}

func (p *fakePlayer) play() {
	p.mu.Lock()
	if p.st.Err != 0 || !p.st.Paused {
		p.mu.Unlock()
		return
	}
	restart := p.st.Ended
	p.st.Paused, p.st.Ended = false, false
	if restart {
		p.st.Pos = 0
	}
	p.base, p.since = p.st.Pos, time.Now()
	ready := p.loaded && p.st.Ready >= HaveFutureData
	p.st.Waiting = !ready
	p.mu.Unlock()
	p.lastAsk.Store(time.Now().UnixNano()) // the click: it's on screen
	if restart {
		p.emit(Seeking)
		p.emit(Seeked)
	}
	p.emit(Play)
	if ready {
		p.emit(Playing)
	} else {
		p.emit(Waiting)
	}
}

func (p *fakePlayer) pause() {
	p.mu.Lock()
	if p.st.Paused {
		p.mu.Unlock()
		return
	}
	p.rebase()
	p.st.Paused, p.st.Waiting = true, false
	p.mu.Unlock()
	p.emit(TimeUpdate)
	p.emit(Pause)
}

func (p *fakePlayer) seek(t time.Duration) {
	p.mu.Lock()
	if !p.loaded {
		// before the metadata: where playback starts once it's there
		// (HTML's default playback start position, which currentTime
		// reads until then)
		p.startAt, p.hasStart = t, true
		p.st.Pos = max(t, 0)
		p.mu.Unlock()
		return
	}
	t = min(max(t, 0), p.st.Duration)
	p.st.Pos, p.base, p.since = t, t, time.Now()
	p.st.Ended = false
	p.mu.Unlock()
	p.emit(Seeking)
	p.publish(true) // the frame there, playing or paused
	p.emit(TimeUpdate)
	p.emit(Seeked)
}

// advance is the media thread's poll: a new frame when one is due, the
// end (or the loop back to 0) when it's there.
func (p *fakePlayer) advance() {
	p.mu.Lock()
	if fa := p.opt.FailAfter; fa > 0 && !p.st.Paused && p.st.Err == 0 && p.pos() >= fa {
		p.mu.Unlock()
		p.fail(ErrNetwork) // mid-file: Chromium's network error after the metadata
		return
	}
	pos := p.pos()
	end := p.st.Duration > 0 && pos >= p.st.Duration && !p.st.Paused
	loop := p.st.Loop
	p.mu.Unlock()
	if end {
		if loop {
			p.mu.Lock()
			p.st.Pos, p.base, p.since = 0, 0, time.Now()
			p.mu.Unlock()
			p.emit(Seeking)
			p.publish(false)
			p.emit(Seeked)
			return
		}
		p.mu.Lock()
		p.st.Pos = p.st.Duration
		p.st.Paused, p.st.Ended = true, true
		p.mu.Unlock()
		p.emit(TimeUpdate)
		p.emit(Pause)
		p.emit(Ended)
		return
	}
	p.publish(false)
}

// publish makes the frame at the position now the one drawn, when it's a
// new one, and asks for a repaint: only while the GPU asked for a frame
// within the last second, unless forced (the first frame, a seek's: the
// frame there, playing or paused).
func (p *fakePlayer) publish(force bool) {
	if !force && time.Now().UnixNano()-p.lastAsk.Load() >= int64(time.Second) {
		return
	}
	p.mu.Lock()
	pos := p.pos()
	f := int64(pos.Seconds() * p.opt.FPS)
	if f == p.frame && p.shown && !force {
		p.mu.Unlock()
		return
	}
	p.frame, p.shown = f, true
	p.color = p.opt.Color(pos)
	p.mu.Unlock()
	p.nFrames.Add(1)
	p.frameReady()
}

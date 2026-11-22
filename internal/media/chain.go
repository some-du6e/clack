package media

import (
	"errors"
	"fmt"
	"image"
	"strings"
	"sync"
	"time"

	"gioui.org/op/paint"
)

// Taker is optional on a Backend: whether it plays a kind of source at
// all (a Go audio backend plays no video). Without it a backend takes
// every source.
type Taker interface {
	Takes(src Source) bool
}

// Chain plays each source with the backends that take it, in the order
// Order gives (nil: the order given): when a player fails before its
// metadata because it can't play the file (MEDIA_ERR_SRC_NOT_SUPPORTED,
// MEDIA_ERR_DECODE), the next backend gets the file and every call made
// meanwhile, as if it had been the one from the start (AVFoundation plays
// AAC, Go audio the WebM and Ogg it can't read). A source only one backend
// takes (every video: Go audio takes none) gets that backend's own
// player, so what it is besides a Player (a FrameCopier: the idle release
// reads its frame) and how it closes stay as they are.
type Chain struct {
	backends []Backend
	order    func(src Source, bs []Backend) []Backend
}

// NewChain is a Chain of the available backends among bs (nils skipped).
func NewChain(order func(src Source, bs []Backend) []Backend, bs ...Backend) *Chain {
	c := &Chain{order: order}
	for _, b := range bs {
		if b != nil && b.Available() == nil {
			c.backends = append(c.backends, b)
		}
	}
	return c
}

func (c *Chain) Name() string {
	var n []string
	for _, b := range c.backends {
		n = append(n, b.Name())
	}
	return strings.Join(n, " + ")
}

func (c *Chain) Available() error {
	if len(c.backends) == 0 {
		return ErrNoBackend
	}
	return nil
}

// Takes reports whether a backend of the chain takes src.
func (c *Chain) Takes(src Source) bool { return len(c.candidates(src)) > 0 }

func (c *Chain) candidates(src Source) []Backend {
	bs := c.backends
	if c.order != nil {
		bs = c.order(src, bs)
	}
	var out []Backend
	for _, b := range bs {
		if t, ok := b.(Taker); !ok || t.Takes(src) {
			out = append(out, b)
		}
	}
	return out
}

func (c *Chain) New(src Source, frameReady func()) (Player, error) {
	bs := c.candidates(src)
	switch len(bs) {
	case 0:
		return nil, fmt.Errorf("media: no backend plays %s (%s)", src.FileID, src.Mime)
	case 1:
		return bs[0].New(src, frameReady)
	}
	p := &chainPlayer{src: src, frameReady: frameReady, rest: bs, events: make(chan Event, 64), vol: 1, rate: 1}
	if err := p.next(); err != nil {
		return nil, err
	}
	return p, nil
}

// chainPlayer is the current backend's player, with what was asked of it
// so far to hand on to the next.
type chainPlayer struct {
	src        Source
	frameReady func()
	events     chan Event

	mu      sync.Mutex
	cur     Player
	rest    []Backend
	loaded  bool // the current one reached its metadata: no more fallback
	closed  bool
	playing bool
	seek    time.Duration
	seeked  bool
	vol     float64
	muted   bool
	loop    bool
	rate    float64
	size    image.Point
	wg      sync.WaitGroup
}

// next makes the next backend's player and replays the calls on it.
func (p *chainPlayer) next() error {
	var last error
	for len(p.rest) > 0 {
		b := p.rest[0]
		p.rest = p.rest[1:]
		pl, err := b.New(p.src, p.frameReady)
		if err != nil {
			last = err
			continue
		}
		p.cur = pl
		pl.SetVolume(p.vol)
		pl.SetMuted(p.muted)
		pl.SetLoop(p.loop)
		if p.rate != 1 {
			pl.SetRate(p.rate)
		}
		if p.size != (image.Point{}) {
			pl.SetDrawSize(p.size)
		}
		if p.seeked {
			pl.Seek(p.seek)
		}
		if p.playing {
			pl.Play()
		}
		p.wg.Add(1)
		go p.forward(pl)
		return nil
	}
	if last == nil {
		last = errors.New("media: no backend left")
	}
	return last
}

// forward passes a player's events on, until one fails it can't play: then
// the next backend takes over and that error is swallowed.
func (p *chainPlayer) forward(pl Player) {
	defer p.wg.Done()
	for ev := range pl.Events() {
		p.mu.Lock()
		if p.closed || p.cur != pl {
			p.mu.Unlock()
			continue
		}
		switch ev.Kind {
		case LoadedMetadata:
			p.loaded = true
		case Error:
			if !p.loaded && len(p.rest) > 0 && (ev.State.Err == ErrSrcNotSupported || ev.State.Err == ErrDecode) {
				old := pl
				if err := p.next(); err == nil {
					p.mu.Unlock()
					go old.Close()
					continue
				}
			}
		}
		select {
		case p.events <- ev:
		default:
		}
		p.mu.Unlock()
	}
}

func (p *chainPlayer) call(f func(Player)) {
	p.mu.Lock()
	cur := p.cur
	p.mu.Unlock()
	if cur != nil {
		f(cur)
	}
}

func (p *chainPlayer) Play() {
	p.mu.Lock()
	p.playing = true
	p.mu.Unlock()
	p.call(Player.Play)
}

func (p *chainPlayer) Pause() {
	p.mu.Lock()
	p.playing = false
	p.mu.Unlock()
	p.call(Player.Pause)
}

func (p *chainPlayer) Seek(t time.Duration) {
	p.mu.Lock()
	p.seek, p.seeked = t, true
	p.mu.Unlock()
	p.call(func(pl Player) { pl.Seek(t) })
}

func (p *chainPlayer) SetVolume(v float64) {
	p.mu.Lock()
	p.vol = v
	p.mu.Unlock()
	p.call(func(pl Player) { pl.SetVolume(v) })
}

func (p *chainPlayer) SetMuted(m bool) {
	p.mu.Lock()
	p.muted = m
	p.mu.Unlock()
	p.call(func(pl Player) { pl.SetMuted(m) })
}

func (p *chainPlayer) SetLoop(l bool) {
	p.mu.Lock()
	p.loop = l
	p.mu.Unlock()
	p.call(func(pl Player) { pl.SetLoop(l) })
}

func (p *chainPlayer) SetRate(r float64) {
	p.mu.Lock()
	p.rate = r
	p.mu.Unlock()
	p.call(func(pl Player) { pl.SetRate(r) })
}

func (p *chainPlayer) SetDrawSize(px image.Point) {
	p.mu.Lock()
	p.size = px
	p.mu.Unlock()
	p.call(func(pl Player) { pl.SetDrawSize(px) })
}

func (p *chainPlayer) Op(still paint.ImageOp, size image.Point) paint.ImageOp {
	p.mu.Lock()
	cur := p.cur
	p.mu.Unlock()
	if cur == nil {
		return still
	}
	return cur.Op(still, size)
}

func (p *chainPlayer) State() State {
	p.mu.Lock()
	cur := p.cur
	p.mu.Unlock()
	return cur.State()
}

func (p *chainPlayer) Events() <-chan Event { return p.events }

func (p *chainPlayer) Stats() Stats {
	p.mu.Lock()
	cur := p.cur
	p.mu.Unlock()
	return cur.Stats()
}

// Backend is the backend playing it now (logs, tests).
func (p *chainPlayer) Backend() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return fmt.Sprintf("%T", p.cur)
}

// Close closes the current player and returns at once: the players'
// events end in their own time (Media Foundation's after its engine's
// shutdown), and the chain's close after them, never on the caller's
// goroutine (the controller holds its lock there).
func (p *chainPlayer) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	cur := p.cur
	p.mu.Unlock()
	cur.Close()
	go func() {
		p.wg.Wait()
		close(p.events)
	}()
}

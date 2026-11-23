package jsrt

import (
	"math"
	"sync"
	"time"
)

// MediaHost plays the page's <audio> and <video> elements (Options.Media):
// an element's play() opens its source through it, as a browser's media
// pipeline loads the element's resource and plays it (dom.js).
type MediaHost interface {
	// Open makes a player of src, paused, loading it. data is the bytes
	// when the page read them itself (a blob: URL, or a URL one of the
	// page's own fetch handlers served: a plugin's protocol); nil leaves
	// src to the host (its own sources, such as Slack's sounds). An error
	// is the element's MEDIA_ERR_SRC_NOT_SUPPORTED. Called on its own
	// goroutine.
	Open(src string, data []byte) (MediaPlayer, error)
}

// MediaPlayer is one element's playback, as the element drives it. Every
// method but Next returns at once.
type MediaPlayer interface {
	Play()
	Pause()
	// Seek is currentTime = t.
	Seek(t time.Duration)
	SetVolume(v float64)
	SetMuted(m bool)
	SetLoop(l bool)
	// Next waits for the player's next event; ok is false once it is
	// closed and has no more.
	Next() (ev MediaEvent, ok bool)
	Close()
}

// MediaEvent is an HTMLMediaElement event and the player's state as it
// fired.
type MediaEvent struct {
	// Type is the DOM event's ("loadedmetadata", "playing", "pause",
	// "timeupdate", "ended", "error", …).
	Type          string
	Pos, Duration time.Duration
	Paused, Ended bool
	Ready         int // readyState
	Err           int // MediaError code, 0 for none
}

// mediaPlayers are the players the page's loads opened, each by its own
// handle: an element's load holds the player it opened, so a load the
// element left behind (a newer src, a newer load) closes its own player
// and never the one that took its place.
type mediaPlayers struct {
	mu  sync.Mutex
	m   map[int]MediaPlayer
	seq int
}

// add keeps pl under a new handle.
func (p *mediaPlayers) add(pl MediaPlayer) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seq++
	p.m[p.seq] = pl
	return p.seq
}

func (p *mediaPlayers) get(id int) MediaPlayer {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.m[id]
}

// drop closes id's player and forgets it.
func (p *mediaPlayers) drop(id int) {
	p.mu.Lock()
	pl := p.m[id]
	delete(p.m, id)
	p.mu.Unlock()
	if pl != nil {
		pl.Close()
	}
}

func (p *mediaPlayers) closeAll() {
	p.mu.Lock()
	all := p.m
	p.m = map[int]MediaPlayer{}
	p.mu.Unlock()
	for _, pl := range all {
		pl.Close()
	}
}

// installMedia defines the media host functions on h (installHost): the
// element's player, its controls and its events. Without Options.Media
// none is defined and play() rejects as a browser with no media pipeline.
func (r *Runtime) installMedia(def func(name string, fn HostFunc)) {
	host := r.opts.Media
	if host == nil {
		return
	}
	players := &mediaPlayers{m: map[int]MediaPlayer{}}
	go func() {
		<-r.done
		players.closeAll()
	}()
	arg := func(a []Value, i int) Value {
		if i < len(a) {
			return a[i]
		}
		return Value{}
	}
	// mediaOpen(src, data?): a player for an element's source, paused, as
	// the handle the element's controls and events name it by (mediaCtl,
	// mediaNext); rejects with NotSupportedError when the source can't be
	// played
	def("mediaOpen", func(js *Ctx, _ Value, a []Value) (any, error) {
		src := arg(a, 0).String()
		var data []byte
		if d := arg(a, 1); d.IsObject() {
			data = d.Bytes()
		}
		p, resolve, reject := js.NewPromise()
		go func() {
			pl, err := host.Open(src, data)
			if err != nil || pl == nil {
				reject(&JSError{Name: "NotSupportedError", Message: "Failed to load because no supported source was found."})
				return
			}
			select {
			case <-r.done: // the runtime closed while it opened
				pl.Close()
				return
			default:
			}
			resolve(players.add(pl))
		}()
		return p, nil
	})
	// mediaCtl(handle, op, value): play, pause, seek (s), volume, muted,
	// loop, close (the handle's player only)
	def("mediaCtl", func(js *Ctx, _ Value, a []Value) (any, error) {
		id, op := arg(a, 0).Int(), arg(a, 1).String()
		if op == "close" {
			players.drop(id)
			return nil, nil
		}
		pl := players.get(id)
		if pl == nil {
			return nil, nil
		}
		v := arg(a, 2)
		switch op {
		case "play":
			pl.Play()
		case "pause":
			pl.Pause()
		case "seek":
			if s := v.Float(); s >= 0 && !math.IsInf(s, 0) && !math.IsNaN(s) {
				pl.Seek(time.Duration(s * float64(time.Second)))
			}
		case "volume":
			pl.SetVolume(math.Max(0, math.Min(1, v.Float())))
		case "muted":
			pl.SetMuted(v.Bool())
		case "loop":
			pl.SetLoop(v.Bool())
		}
		return nil, nil
	})
	// mediaNext(handle): the player's next event, null once it is closed
	def("mediaNext", func(js *Ctx, _ Value, a []Value) (any, error) {
		pl := players.get(arg(a, 0).Int())
		p, resolve, _ := js.NewPromise()
		if pl == nil {
			resolve(nil)
			return p, nil
		}
		go func() {
			ev, ok := pl.Next()
			if !ok {
				resolve(nil)
				return
			}
			resolve(func(js *Ctx) (Value, error) {
				o := js.NewObject()
				_ = o.Set("type", ev.Type)
				_ = o.Set("pos", ev.Pos.Seconds())
				d := math.NaN()
				if ev.Duration > 0 {
					d = ev.Duration.Seconds()
				}
				_ = o.Set("duration", d)
				_ = o.Set("paused", ev.Paused)
				_ = o.Set("ended", ev.Ended)
				_ = o.Set("ready", float64(ev.Ready))
				_ = o.Set("err", float64(ev.Err))
				return o, nil
			})
		}()
		return p, nil
	})
}

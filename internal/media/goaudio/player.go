// Package goaudio plays a message's audio file the way an <audio> element
// does, decoding it in Go: WAV (PCM), MP3 (go-mp3) and Opus in Ogg, WebM
// and MP4 (the huddle's libopus), and on Windows what Go can't (AAC) with
// Media Foundation's source reader; mixed to at most two channels,
// resampled to 48 kHz stereo, at any playbackRate keeping the pitch
// (WSOLA), out of the huddle's speakers (huddle.OpenOutput). Without a
// sound card it keeps time on its own, so a clip still plays (silently)
// and ends. It is the media Backend for audio on Linux and Windows, and
// on macOS for what AVFoundation can't read (WebM, Ogg).
//
// The file comes through media.Source, read whole into memory (audio
// messages are a few MB) and never written to disk. Its readers are total
// (decode.go): any bytes at all open, fail with an error, or end; a
// decoder that panics anyway (go-mp3, libopus) is the element's
// MEDIA_ERR_DECODE (safe.go), never a crash.
package goaudio

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"log"
	"sync"
	"time"

	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/media"
)

// maxBytes is the largest file it reads (an hour of 320 kbps MP3 is
// 144 MB).
const maxBytes = 256 << 20

// Backend plays audio sources.
type Backend struct {
	// Output opens the speakers with pull; nil is huddle.OpenOutput. A
	// failure (no device: CI) falls back to a clock with no sound.
	Output func(pull func(out []int16)) (io.Closer, error)
	// Logf logs; nil is log.Printf.
	Logf func(string, ...any)
}

func (b *Backend) Name() string     { return "Go audio" }
func (b *Backend) Available() error { return nil }

// Takes reports whether the source is audio (media.Taker): it plays no
// video.
func (b *Backend) Takes(src media.Source) bool { return IsAudio(src) }

// IsAudio reports whether the source is an audio file (media.IsAudio).
func IsAudio(src media.Source) bool { return media.IsAudio(src) }

func (b *Backend) New(src media.Source, frameReady func()) (media.Player, error) {
	if !IsAudio(src) {
		return nil, fmt.Errorf("goaudio: %s isn't audio", src.FileID)
	}
	logf := b.Logf
	if logf == nil {
		logf = log.Printf
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &player{b: b, src: src, logf: logf, ctx: ctx, cancel: cancel, events: make(chan media.Event, 64),
		rate: 1, done: make(chan struct{})}
	p.st.Paused, p.st.Volume = true, 1
	go p.load()
	return p, nil
}

type player struct {
	b      *Backend
	src    media.Source
	logf   func(string, ...any)
	ctx    context.Context
	cancel context.CancelFunc
	events chan media.Event
	done   chan struct{} // load finished

	mu       sync.Mutex
	st       media.State
	closed   bool
	rate     float64
	s        stream
	mx       *mixer
	rs       *resampler
	ts       *stretcher
	out      []float32 // rendered 48 kHz stereo, not played yet
	eof      bool      // the stream gave all it had
	startAt  time.Duration
	hasStart bool
	playWait bool // Play came before the data
	// sink is the speakers (or the clock), open only while it plays:
	// opening is a sink on its way; sinkGen the one whose pulls play (a
	// sink let go of may pull once more while it closes)
	sink    io.Closer
	opening bool
	sinkGen uint64
	stats   media.Stats
	scratch []float32 // a Read's frames, the stream's channels
	folded  []float32 // those mixed to at most two
	mix     []float32 // resampled
	stereo  []float32
	hop     []float32
}

// closeFn is a Device's Close as an io.Closer.
type closeFn func()

func (f closeFn) Close() error { f(); return nil }

func (p *player) emitLocked(k media.EventKind) {
	select {
	case p.events <- media.Event{Kind: k, State: p.st}:
	default:
		p.stats.Dropped++
	}
}

// load reads the file, decodes its header, and opens the speakers if it
// plays. Close ends it at the next look at ctx (the download's, the
// readers').
func (p *player) load() {
	defer close(p.done)
	rc, info, err := p.src.Open(p.ctx, 0)
	if err != nil {
		p.fail(media.ErrNetwork, err)
		return
	}
	limit := int64(maxBytes)
	if info.Total > 0 && info.Total < limit {
		limit = info.Total
	}
	b, err := io.ReadAll(io.LimitReader(rc, limit))
	rc.Close()
	p.mu.Lock()
	p.stats.Requests++
	p.stats.BytesRead += int64(len(b))
	p.mu.Unlock()
	if err != nil {
		p.fail(media.ErrNetwork, err)
		return
	}
	s, err := safeOpen(p.ctx, b, p.src.Mime)
	if err != nil {
		if p.ctx.Err() != nil {
			return // closed while it read
		}
		code := media.ErrDecode
		if errors.Is(err, errUnsupported) {
			code = media.ErrSrcNotSupported
		}
		p.fail(code, err)
		return
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		s.Close()
		return
	}
	mask := uint32(0)
	if m, ok := s.(masked); ok {
		mask = m.ChannelMask()
	}
	p.s = s
	p.mx = newMixer(s.Channels(), mask)
	p.rs = newResampler(s.Rate(), p.mx.out)
	p.ts = newStretcher()
	p.st.Duration = time.Duration(s.Frames()) * time.Second / time.Duration(s.Rate())
	p.st.Ready = media.HaveMetadata
	if p.hasStart {
		p.seekLocked(min(max(p.startAt, 0), p.st.Duration))
	}
	if p.st.Err != 0 { // the seek's decoder failed
		p.mu.Unlock()
		return
	}
	p.emitLocked(media.DurationChange)
	p.emitLocked(media.LoadedMetadata)
	p.st.Ready = media.HaveEnoughData
	p.emitLocked(media.LoadedData)
	start := p.playWait
	p.playWait = false
	if start {
		p.st.Waiting = false
		p.emitLocked(media.Playing)
	}
	p.wantSinkLocked()
	dur := p.st.Duration
	p.mu.Unlock()
	p.logf("media: %s: Go audio (%s, %d Hz × %d, %v, %d bytes)", p.src.FileID, s.Codec(), s.Rate(), s.Channels(), dur.Round(time.Millisecond), len(b))
}

// fail is the element's error: the code, the error event.
func (p *player) fail(code int, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failLocked(code, err)
}

// failLocked ends the playback with MediaError code: the error event
// alone, as HTML's (the controller takes it as the end of playing), and
// the speakers let go of.
func (p *player) failLocked(code int, err error) {
	if p.closed || p.st.Err != 0 {
		return
	}
	p.logf("media: %s: Go audio: media_error_code %d: %v", p.src.FileID, code, err)
	p.st.Err, p.st.Waiting, p.playWait = code, false, false
	p.dropSinkLocked()
	p.emitLocked(media.Error)
}

// wantSinkLocked opens the speakers once it plays with its data and has
// none: a paused player holds no sound card (nor its 10 ms callbacks).
func (p *player) wantSinkLocked() {
	if p.closed || p.sink != nil || p.opening || p.s == nil || p.st.Paused || p.st.Err != 0 {
		return
	}
	p.opening = true
	p.sinkGen++
	go p.openSink(p.sinkGen)
}

// dropSinkLocked lets go of the speakers (a pause, the end, an error),
// closing them off the sound card's thread, which may be the caller.
func (p *player) dropSinkLocked() {
	p.sinkGen++ // its pulls play nothing from now on
	if s := p.sink; s != nil {
		p.sink = nil
		go s.Close()
	}
}

func (p *player) openSink(gen uint64) {
	pull := func(out []int16) { p.pull(gen, out) }
	out := p.b.Output
	if out == nil {
		out = func(pull func([]int16)) (io.Closer, error) {
			d, err := huddle.OpenOutput(p.logf, pull)
			if err != nil {
				return nil, err
			}
			return closeFn(d.Close), nil
		}
	}
	sink, err := out(pull)
	if err != nil {
		p.logf("media: %s: no speakers (%v): keeping time without sound", p.src.FileID, err)
		sink = newClock(pull)
	}
	p.mu.Lock()
	p.opening = false
	if p.closed || gen != p.sinkGen || p.sink != nil {
		// paused (or closed) while it opened; playing again since, it
		// opens one of its own
		p.wantSinkLocked()
		p.mu.Unlock()
		sink.Close()
		return
	}
	p.sink = sink
	p.mu.Unlock()
}

// pull is sink gen's: the next frames, silence while paused or once
// that sink was let go of.
func (p *player) pull(gen uint64, out []int16) {
	p.mu.Lock()
	defer p.mu.Unlock()
	frames := len(out) / 2
	if p.closed || gen != p.sinkGen || p.s == nil || p.st.Paused || p.st.Err != 0 {
		clear(out)
		return
	}
	got := p.renderLocked(frames)
	if p.st.Err != 0 { // the decoder failed
		clear(out)
		return
	}
	vol := float32(p.st.Volume)
	if p.st.Muted {
		vol = 0
	}
	for i := 0; i < got*2; i++ {
		v := p.out[i] * vol
		switch {
		case v > 1:
			v = 1
		case v < -1:
			v = -1
		case v != v: // NaN
			v = 0
		}
		out[i] = int16(v * 32767)
	}
	clear(out[got*2:])
	p.out = append(p.out[:0], p.out[got*2:]...)
	// the clock: the source time these frames covered
	p.st.Pos += time.Duration(float64(got) * p.rate * float64(time.Second) / outRate)
	if p.st.Pos > p.st.Duration {
		p.st.Pos = p.st.Duration
	}
	if got < frames && p.eof && len(p.out) == 0 {
		p.endLocked()
	}
}

// renderLocked fills p.out with at least frames frames (fewer at the
// end): decode, mix to at most two channels, resample to 48 kHz, to
// stereo, then the rate. A decoder that fails is the element's
// MEDIA_ERR_DECODE.
func (p *player) renderLocked(frames int) int {
	ch, oc := p.s.Channels(), p.mx.out
	if cap(p.scratch) < 1024*ch {
		p.scratch = make([]float32, 1024*ch)
	}
	if cap(p.folded) < 1024*oc {
		p.folded = make([]float32, 1024*oc)
	}
	if cap(p.mix) < 2048*oc {
		p.mix = make([]float32, 2048*oc)
	}
	if cap(p.hop) < 4*wsolaH*2 {
		p.hop = make([]float32, 4*wsolaH*2)
	}
	for len(p.out)/2 < frames {
		if !p.eof {
			n, err := safeRead(p.s, p.scratch[:1024*ch])
			if n > 0 {
				p.rs.push(p.mx.mix(p.scratch[:n*ch], n, p.folded[:1024*oc]))
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					p.failLocked(media.ErrDecode, err)
					return 0
				}
				p.eof = true
			}
		}
		got := p.rs.pull(p.mix[:2048*oc], p.eof)
		p.stereo = p.stereo[:0]
		for i := 0; i < got; i++ {
			l := p.mix[i*oc]
			r := l
			if oc > 1 {
				r = p.mix[i*oc+1]
			}
			p.stereo = append(p.stereo, l, r)
		}
		if p.rate == 1 {
			p.out = append(p.out, p.stereo...)
			if got == 0 && p.eof {
				break
			}
			continue
		}
		p.ts.rate = p.rate
		p.ts.push(p.stereo)
		n := p.ts.pull(p.hop[:4*wsolaH*2], p.eof)
		p.out = append(p.out, p.hop[:n*2]...)
		if n == 0 && got == 0 && p.eof {
			break
		}
	}
	return min(frames, len(p.out)/2)
}

// endLocked is the end of the media: the loop back to 0, or paused, ended.
func (p *player) endLocked() {
	if p.st.Loop {
		p.seekLocked(0)
		p.emitLocked(media.Seeking)
		p.emitLocked(media.Seeked)
		return
	}
	p.st.Pos = p.st.Duration
	p.st.Paused, p.st.Ended = true, true
	p.dropSinkLocked()
	p.emitLocked(media.TimeUpdate)
	p.emitLocked(media.Pause)
	p.emitLocked(media.Ended)
}

// seekLocked starts the pipeline over at t.
func (p *player) seekLocked(t time.Duration) {
	f := int64(t.Seconds() * float64(p.s.Rate()))
	if err := safeSeek(p.s, f); err != nil {
		p.failLocked(media.ErrDecode, fmt.Errorf("seek: %w", err))
		return
	}
	p.rs.reset()
	p.ts.reset()
	p.out, p.eof = p.out[:0], false
	p.st.Pos, p.st.Ended = t, false
}

func (p *player) Play() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.st.Err != 0 || !p.st.Paused {
		return
	}
	restart := p.st.Ended
	p.st.Paused, p.st.Ended = false, false
	if restart && p.s != nil {
		p.seekLocked(0)
		if p.st.Err != 0 {
			return
		}
		p.emitLocked(media.Seeking)
		p.emitLocked(media.Seeked)
	}
	p.emitLocked(media.Play)
	if p.s == nil {
		p.playWait, p.st.Waiting = true, true
		p.emitLocked(media.Waiting)
		return
	}
	p.emitLocked(media.Playing)
	p.wantSinkLocked()
}

func (p *player) Pause() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.st.Paused {
		return
	}
	p.st.Paused, p.st.Waiting, p.playWait = true, false, false
	p.dropSinkLocked()
	p.emitLocked(media.TimeUpdate)
	p.emitLocked(media.Pause)
}

func (p *player) Seek(t time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.st.Err != 0 {
		return
	}
	if p.s == nil {
		p.startAt, p.hasStart = t, true
		p.st.Pos = max(t, 0)
		return
	}
	p.seekLocked(min(max(t, 0), p.st.Duration))
	if p.st.Err != 0 {
		return
	}
	p.emitLocked(media.Seeking)
	p.emitLocked(media.TimeUpdate)
	p.emitLocked(media.Seeked)
}

func (p *player) SetVolume(v float64) {
	p.mu.Lock()
	p.st.Volume = min(max(v, 0), 1)
	p.mu.Unlock()
}

func (p *player) SetMuted(m bool) {
	p.mu.Lock()
	p.st.Muted = m
	p.mu.Unlock()
}

func (p *player) SetLoop(l bool) {
	p.mu.Lock()
	p.st.Loop = l
	p.mu.Unlock()
}

// SetRate is playbackRate, keeping the pitch (Chromium plays 0.0625-16).
func (p *player) SetRate(r float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	r = min(max(r, 0.0625), 16)
	if r == p.rate || p.closed {
		return
	}
	p.rate = r
	if p.s != nil && p.st.Err == 0 { // what's rendered was at the old rate
		p.seekLocked(p.st.Pos)
	}
}

func (p *player) SetDrawSize(image.Point) {}

func (p *player) Op(still paint.ImageOp, _ image.Point) paint.ImageOp { return still }

func (p *player) State() media.State {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.st
}

func (p *player) Events() <-chan media.Event { return p.events }

func (p *player) Stats() media.Stats {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stats
}

// Close ends the player at once: no event follows (every emit looks at
// closed under mu), the speakers close off the caller's thread (the
// controller calls it holding its lock, on the UI goroutine), the decoder
// goes, and the load still reading (a download, a reader) ends at its
// next look at ctx, closing what it made.
func (p *player) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	p.sinkGen++
	sink, s := p.sink, p.s
	p.sink, p.s = nil, nil
	close(p.events)
	p.mu.Unlock()
	p.cancel()
	if sink != nil {
		go sink.Close()
	}
	if s != nil {
		s.Close()
	}
}

// clock pulls like a sound card, at real time, into nothing: playback
// with no speakers (CI, a machine without an output).
type clock struct {
	stop chan struct{}
	once sync.Once
	done chan struct{}
}

func newClock(pull func([]int16)) *clock {
	c := &clock{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(c.done)
		t := time.NewTicker(10 * time.Millisecond)
		defer t.Stop()
		start := time.Now()
		var pulled int64
		buf := make([]int16, 2*outRate/10)
		for {
			select {
			case <-c.stop:
				return
			case <-t.C:
			}
			due := int64(time.Since(start).Seconds()*outRate) - pulled
			for due > 0 {
				n := min(due, int64(len(buf)/2))
				pull(buf[:n*2])
				pulled += n
				due -= n
			}
		}
	}()
	return c
}

func (c *clock) Close() error {
	c.once.Do(func() { close(c.stop) })
	<-c.done
	return nil
}

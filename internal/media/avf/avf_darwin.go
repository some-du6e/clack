//go:build darwin && cgo

package avf

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AVFoundation -framework CoreMedia -framework CoreVideo -framework Metal -framework QuartzCore -framework Foundation
#include <stdlib.h>
#include "avf_darwin.h"
*/
import "C"

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"io"
	"log"
	"math"
	"strings"
	"sync"
	"time"
	"unsafe"

	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/media"
)

func init() { media.Register(backend{}) }

type backend struct{}

func (backend) Name() string { return "avfoundation" }

// Available: AVFoundation is part of every macOS Clack runs on.
func (backend) Available() error { return nil }

func (backend) New(src media.Source, frameReady func()) (media.Player, error) {
	if src.Open == nil {
		return nil, errors.New("avf: a source with no Open")
	}
	if frameReady == nil {
		frameReady = func() {}
	}
	p := &player{src: src, frameReady: frameReady, loads: map[uint64]context.CancelFunc{}, slots: make(chan struct{}, respondAhead), done: make(chan struct{}), born: time.Now()}
	p.Init()
	p.pend.volume, p.pend.rate, p.pend.seekTo = 1, 1, -1
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.rs = media.NewRangeSource(src, media.RangeOptions{})
	p.frames = media.NewFrameHandle(p)
	p.id = register(p)
	go p.open()
	return p, nil
}

// players are the live players by id: the queue's callbacks name their
// player by id, and one that's gone is ignored.
var (
	regMu  sync.Mutex
	reg    = map[uintptr]*player{}
	nextID uintptr
)

func register(p *player) uintptr {
	regMu.Lock()
	defer regMu.Unlock()
	nextID++
	reg[nextID] = p
	return nextID
}

func unregister(id uintptr) {
	regMu.Lock()
	defer regMu.Unlock()
	delete(reg, id)
}

func lookup(h C.uintptr_t) *player {
	regMu.Lock()
	defer regMu.Unlock()
	return reg[uintptr(h)]
}

// player is one AVPlayer. Its Objective-C side (avf_darwin.m) runs on its
// own serial queue; obj is that object while it lives (objMu: Close takes
// it whole, every call into it shares it).
type player struct {
	media.Base
	src        media.Source
	rs         *media.RangeSource
	frameReady func()
	frames     *media.FrameHandle
	id         uintptr
	ctx        context.Context
	cancel     context.CancelFunc

	objMu  sync.RWMutex
	obj    unsafe.Pointer
	closed bool

	mu    sync.Mutex
	pend  pending // the controls before obj was made
	loads map[uint64]context.CancelFunc
	// toEnd is each request to the end's offset as it reads (under mu),
	// moved closed at each change (holdBehind)
	toEnd map[uint64]int64
	moved chan struct{}
	// slots holds a token for each respond the queue hasn't handed on yet
	// (at most respondAhead)
	slots  chan struct{}
	failed bool
	done   chan struct{}

	// the first frame's timing line: when New made the player, and each
	// step's time after (the file's head, the asset, loadedmetadata,
	// loadeddata), once
	born                         time.Time
	head, asset, meta, data      time.Duration
	metaSeen, dataSeen, firstLog bool
}

// readLog, when set (tests), sees each of an asset's data requests, and
// noteLog how each request to the end began and ended.
var (
	readLogMu sync.Mutex
	readLog   func(id string, off, n int64, toEnd bool)
	noteLog   func(id, line string)
)

// respondAhead is how many responds (256 KB each) may wait on the queue at
// once. The queue hands each to AVFoundation in turn; unbounded, a fast
// network queued tens of MB ahead of what the asset had, and its second
// request to the end began where the asset's bytes ended, that far behind
// the first one: past the source's cache, a GET read it all again.
const respondAhead = 4

type pending struct {
	play, seek   bool
	seekTo       float64
	volume, rate float64
	muted, loop  bool
	draw         image.Point
}

// call runs f with obj, or records the control in pend while there's no
// obj yet; nothing once closed.
func (p *player) call(f func(obj unsafe.Pointer), rec func(*pending)) {
	p.objMu.RLock()
	defer p.objMu.RUnlock()
	if p.closed {
		return
	}
	if p.obj == nil {
		p.mu.Lock()
		rec(&p.pend)
		p.mu.Unlock()
		return
	}
	f(p.obj)
}

func (p *player) Play() {
	p.TouchAsk()
	p.call(func(o unsafe.Pointer) { C.clack_avf_play(o) }, func(s *pending) { s.play = true })
}
func (p *player) Pause() {
	p.call(func(o unsafe.Pointer) { C.clack_avf_pause(o) }, func(s *pending) { s.play = false })
}
func (p *player) Seek(t time.Duration) {
	p.TouchAsk()
	// currentTime reads the new position at once, as HTML's does (before
	// the metadata too: the default playback start position)
	p.Update(func(st *media.State) {
		st.Pos, st.Ended = max(t, 0), false
		if st.Duration > 0 && st.Pos > st.Duration {
			st.Pos = st.Duration
		}
	})
	p.call(func(o unsafe.Pointer) { C.clack_avf_seek(o, C.double(t.Seconds())) }, func(s *pending) { s.seek, s.seekTo = true, t.Seconds() })
}
func (p *player) SetVolume(v float64) {
	v = min(max(v, 0), 1)
	p.Update(func(st *media.State) { st.Volume = v })
	p.call(func(o unsafe.Pointer) { C.clack_avf_volume(o, C.double(v)) }, func(s *pending) { s.volume = v })
}
func (p *player) SetMuted(m bool) {
	p.Update(func(st *media.State) { st.Muted = m })
	p.call(func(o unsafe.Pointer) { C.clack_avf_muted(o, b2i(m)) }, func(s *pending) { s.muted = m })
}
func (p *player) SetLoop(l bool) {
	p.Update(func(st *media.State) { st.Loop = l })
	p.call(func(o unsafe.Pointer) { C.clack_avf_loop(o, b2i(l)) }, func(s *pending) { s.loop = l })
}
func (p *player) SetRate(r float64) {
	if r <= 0 {
		return
	}
	p.Base.SetRate(r)
	p.call(func(o unsafe.Pointer) { C.clack_avf_rate(o, C.double(r)) }, func(s *pending) { s.rate = r })
}
func (p *player) SetDrawSize(px image.Point) {
	p.call(func(o unsafe.Pointer) { C.clack_avf_drawsize(o, C.int(px.X), C.int(px.Y)) }, func(s *pending) { s.draw = px })
}

func b2i(b bool) C.int {
	if b {
		return 1
	}
	return 0
}

func (p *player) Op(still paint.ImageOp, size image.Point) paint.ImageOp {
	return p.frames.Op(still, size)
}

func (p *player) Stats() media.Stats {
	st := p.BaseStats(p.rs)
	p.objMu.RLock()
	if p.obj != nil {
		var pulls, frames C.int64_t
		var w, h C.uint32_t
		C.clack_avf_counts(p.obj, &pulls, &frames, &w, &h)
		st.Ticks = int64(pulls)
	}
	p.objMu.RUnlock()
	return st
}

// PixelSize is the size of the frames AVFoundation hands out (the output
// was asked for the drawn size: SKEPTIC note 3 checks it's honoured).
func (p *player) PixelSize() image.Point {
	p.objMu.RLock()
	defer p.objMu.RUnlock()
	if p.obj == nil {
		return image.Point{}
	}
	var pulls, frames C.int64_t
	var w, h C.uint32_t
	C.clack_avf_counts(p.obj, &pulls, &frames, &w, &h)
	return image.Pt(int(w), int(h))
}

// Texture implements media.Frames: Gio's ask.
func (p *player) Texture(dev media.Device) (media.Texture, bool) {
	if dev.API != media.GPUMetal || dev.Handle == 0 {
		return media.Texture{}, false
	}
	p.NAsks.Add(1)
	p.objMu.RLock()
	defer p.objMu.RUnlock()
	if p.obj == nil {
		return media.Texture{}, false
	}
	var tex C.uintptr_t
	var key C.uint64_t
	if C.clack_avf_ask(p.obj, C.uintptr_t(dev.Handle), &tex, &key) == 0 {
		return media.Texture{}, false
	}
	return media.Texture{Handle: uintptr(tex), Key: uint64(key)}, true
}

// CopyFrame is the frame shown now (media.FrameCopier): the newest pixel
// buffer read back, at its size.
func (p *player) CopyFrame() (*image.RGBA, bool) {
	p.objMu.RLock()
	defer p.objMu.RUnlock()
	if p.obj == nil {
		return nil, false
	}
	var w, h C.int
	C.clack_avf_snapshot(p.obj, nil, 0, &w, &h)
	if w <= 0 || h <= 0 {
		return nil, false
	}
	img := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	if C.clack_avf_snapshot(p.obj, (*C.uint8_t)(unsafe.Pointer(&img.Pix[0])), C.size_t(len(img.Pix)), &w, &h) != 1 {
		return nil, false // a frame of another size came between
	}
	return img, true
}

func (p *player) Close() {
	p.objMu.Lock()
	if p.closed {
		p.objMu.Unlock()
		return
	}
	p.closed = true
	if p.obj != nil {
		C.clack_avf_close(p.obj)
		p.obj = nil
	}
	p.objMu.Unlock()
	p.mu.Lock()
	for _, c := range p.loads {
		c()
	}
	p.loads = map[uint64]context.CancelFunc{}
	p.mu.Unlock()
	p.cancel()
	p.rs.Close()
	unregister(p.id)
	p.CloseEvents()
	close(p.done)
}

// open makes the asset once the server answered (its size and whether it
// answers ranges are the content information AVFoundation asks first).
func (p *player) open() {
	info, err := p.rs.Info(p.ctx)
	p.mu.Lock()
	p.head = time.Since(p.born)
	p.mu.Unlock()
	if err != nil {
		// before the metadata a failed fetch is error 4, as for a file that
		// can't be played: Chromium's MediaLoadingFailed for a src (the fake's)
		p.fail(media.ErrSrcNotSupported, "the file: "+p.rs.Detail(err))
		return
	}
	if info.Total <= 0 {
		p.fail(media.ErrSrcNotSupported, "the server didn't say the file's size")
		return
	}
	ext := strings.ToLower(strings.TrimPrefix(p.src.Ext, "."))
	if ext == "" {
		ext = "mp4"
	}
	var tok [8]byte
	rand.Read(tok[:])
	url := C.CString("clack-media://" + hex.EncodeToString(tok[:]) + "/" + p.src.FileID + "." + ext)
	defer C.free(unsafe.Pointer(url))
	uti := C.CString(utiFor(p.src.Mime, ext))
	defer C.free(unsafe.Pointer(uti))
	seekable := C.int(0)
	if info.Seekable {
		seekable = 1
	}
	p.objMu.Lock()
	defer p.objMu.Unlock()
	if p.closed {
		return
	}
	p.mu.Lock()
	s := p.pend
	p.mu.Unlock()
	obj := C.clack_avf_new(C.uintptr_t(p.id), C.int64_t(info.Total), seekable, uti, url, C.int(s.draw.X), C.int(s.draw.Y))
	if obj == nil {
		p.fail(media.ErrSrcNotSupported, "the asset's URL")
		return
	}
	p.obj = obj
	if s.volume != 1 {
		C.clack_avf_volume(obj, C.double(s.volume))
	}
	if s.muted {
		C.clack_avf_muted(obj, 1)
	}
	if s.loop {
		C.clack_avf_loop(obj, 1)
	}
	if s.rate != 1 {
		C.clack_avf_rate(obj, C.double(s.rate))
	}
	if s.seek {
		C.clack_avf_seek(obj, C.double(s.seekTo))
	}
	if s.play {
		C.clack_avf_play(obj)
	}
	p.mu.Lock()
	p.asset = time.Since(p.born)
	p.mu.Unlock()
	log.Printf("media: %s: AVFoundation (%s, %d bytes, seekable %v)", p.src.FileID, utiFor(p.src.Mime, ext), info.Total, info.Seekable)
}

// timing notes an event's time after the player was made, and logs the
// first frame's line once (as Media Foundation's): each step in seconds,
// and the frames' size.
func (p *player) timing(k media.EventKind) {
	p.mu.Lock()
	since := time.Since(p.born)
	switch k {
	case media.LoadedMetadata:
		if !p.metaSeen {
			p.meta, p.metaSeen = since, true
		}
	case media.LoadedData:
		if !p.dataSeen {
			p.data, p.dataSeen = since, true
		}
	}
	if k != media.FirstFrame || p.firstLog {
		p.mu.Unlock()
		return
	}
	p.firstLog = true
	head, asset, meta, data := p.head, p.asset, p.meta, p.data
	p.mu.Unlock()
	s := func(d time.Duration) string { return fmt.Sprintf("%.2f s", d.Seconds()) }
	line := fmt.Sprintf("media: %s: first frame %s after the player was made (the file's head %s, the asset %s, loadedmetadata %s, loadeddata %s)",
		p.src.FileID, s(since), s(head), s(asset), s(meta), s(data))
	// off the queue: PixelSize takes objMu, which Close holds while it
	// closes the queue
	go func() {
		px, sz := p.PixelSize(), p.State().Size
		log.Printf("%s; frames %d×%d (video %d×%d)", line, px.X, px.Y, sz.X, sz.Y)
	}()
}

// utiFor is the asset's type for AVFoundation's content information.
func utiFor(mime, ext string) string {
	switch {
	case ext == "mov" || mime == "video/quicktime":
		return "com.apple.quicktime-movie"
	case ext == "m4v" || mime == "video/x-m4v":
		return "com.apple.m4v-video"
	// a message's audio player's files (Slack's m4a, mp3 and wav, and
	// the aac renditions it makes of them)
	case ext == "m4a" || mime == "audio/mp4" || mime == "audio/x-m4a":
		return "com.apple.m4a-audio"
	case ext == "mp3" || mime == "audio/mpeg":
		return "public.mp3"
	case ext == "wav" || mime == "audio/wav" || mime == "audio/x-wav" || mime == "audio/vnd.wave":
		return "com.microsoft.waveform-audio"
	case ext == "aac" || mime == "audio/aac":
		return "public.aac-audio"
	}
	return "public.mpeg-4"
}

// fail ends the playback with a MediaError code, as the element's error
// event; the poster stays (Slack's error look).
func (p *player) fail(code int, why string) {
	p.mu.Lock()
	if p.failed {
		p.mu.Unlock()
		return
	}
	p.failed = true
	p.mu.Unlock()
	log.Printf("media: %s: media_error_code %d: %s", p.src.FileID, code, why)
	p.Update(func(st *media.State) { st.Err = code })
	p.Emit(media.Error)
}

// state is the queue's state into the snapshot.
func (p *player) state(s *C.clackAVState) {
	p.Update(func(st *media.State) {
		st.Pos = secs(float64(s.pos))
		if d := float64(s.dur); d > 0 && !math.IsInf(d, 0) {
			st.Duration = secs(d)
		}
		st.Paused, st.Ended, st.Waiting, st.Ready = s.paused != 0, s.ended != 0, s.waiting != 0, int(s.ready)
		if s.err != 0 {
			st.Err = int(s.err)
		}
		if s.w > 0 && s.h > 0 {
			st.Size = image.Pt(int(s.w), int(s.h))
		}
	})
}

func secs(s float64) time.Duration {
	if math.IsNaN(s) || s < 0 {
		return 0
	}
	return time.Duration(s * float64(time.Second))
}

// The queue's events (avf_darwin.h's CLACK_AV_*): 0 is the state only.
var kinds = [...]media.EventKind{
	1:  media.LoadedMetadata,
	2:  media.LoadedData,
	3:  media.Play,
	4:  media.Playing,
	5:  media.Pause,
	6:  media.Waiting,
	7:  media.Seeking,
	8:  media.Seeked,
	9:  media.Ended,
	10: media.DurationChange,
	11: media.Error,
	12: media.FirstFrame,
}

//export clackAVFEvent
func clackAVFEvent(h C.uintptr_t, code C.int, s *C.clackAVState) {
	p := lookup(h)
	if p == nil {
		return
	}
	p.state(s)
	c := int(code)
	if c <= 0 || c >= len(kinds) {
		return // the state only
	}
	if kinds[c] == media.Error {
		log.Printf("media: %s: media_error_code %d (AVFoundation %d)", p.src.FileID, int(s.err), int32(s.errCode))
	}
	p.timing(kinds[c])
	p.Emit(kinds[c])
}

//export clackAVFFrame
func clackAVFFrame(h C.uintptr_t) {
	if p := lookup(h); p != nil {
		p.NFrames.Add(1)
		p.NTransfers.Add(1)
		p.frameReady()
	}
}

// clackAVFLoad is a data request of the asset's: Go streams the bytes from
// the source into it, at most 256 KB at a time, until its end (the file's
// for a request of all to the end) or AVFoundation cancels it.
//
//export clackAVFLoad
func clackAVFLoad(h C.uintptr_t, req C.uint64_t, off, n C.int64_t, toEnd C.int) {
	p := lookup(h)
	if p == nil {
		return
	}
	readLogMu.Lock()
	f := readLog
	readLogMu.Unlock()
	if f != nil {
		f(p.src.FileID, int64(off), int64(n), toEnd != 0)
	}
	if toEnd != 0 {
		// its offset counts now, on the queue: a request to the end
		// already reading holds at its next read (holdBehind), not only
		// once this one's goroutine runs
		p.mu.Lock()
		var others []int64
		for _, at := range p.toEnd {
			others = append(others, at)
		}
		p.mu.Unlock()
		p.readTo(uint64(req), int64(off))
		p.note("%d-end began; the others at %v, %d responds queued", int64(off), others, len(p.slots))
	}
	ctx, cancel := context.WithCancel(p.ctx)
	p.mu.Lock()
	p.loads[uint64(req)] = cancel
	p.mu.Unlock()
	go p.load(ctx, uint64(req), int64(off), int64(n), toEnd != 0)
}

func (p *player) load(ctx context.Context, req uint64, off, n int64, toEnd bool) {
	defer func() {
		p.mu.Lock()
		if c, ok := p.loads[req]; ok {
			c()
			delete(p.loads, req)
		}
		p.mu.Unlock()
	}()
	end := off + n
	cur, how := off, "cancelled"
	if toEnd {
		end = math.MaxInt64
		defer p.readTo(req, -1)
		defer func() { p.note("%d-end ended at %d: %s", off, cur, how) }()
	}
	buf := make([]byte, 256<<10)
	for cur < end {
		if toEnd {
			p.readTo(req, cur)
			if !p.pace(ctx, cur) || !p.holdBehind(ctx, req, cur) {
				return // cancelled while held
			}
		}
		k, err := p.rs.ReadSome(ctx, buf[:min(int64(len(buf)), end-cur)], cur)
		if k > 0 {
			// a slot first: the queue has handed the responds before on
			select {
			case p.slots <- struct{}{}:
			case <-ctx.Done():
				return
			}
			p.objMu.RLock()
			if p.obj != nil && ctx.Err() == nil {
				C.clack_avf_respond(p.obj, C.uint64_t(req), unsafe.Pointer(&buf[0]), C.int(k))
			} else {
				<-p.slots
			}
			p.objMu.RUnlock()
			cur += int64(k)
		}
		if err != nil {
			if ctx.Err() != nil {
				return // cancelled: AVFoundation dropped it
			}
			how = "failed"
			if errors.Is(err, io.EOF) {
				how = "at the end"
			}
			p.finish(req, !errors.Is(err, io.EOF))
			return
		}
	}
	how = "done"
	p.finish(req, false)
}

// note logs a line to noteLog (tests).
func (p *player) note(format string, args ...any) {
	readLogMu.Lock()
	f := noteLog
	readLogMu.Unlock()
	if f != nil {
		f(p.src.FileID, fmt.Sprintf(format, args...))
	}
}

// clackAVFResponded is the queue having handed a respond on (or dropped
// it, its request gone): its slot frees.
//
//export clackAVFResponded
func clackAVFResponded(h C.uintptr_t) {
	if p := lookup(h); p != nil {
		select {
		case <-p.slots:
		default:
		}
	}
}

// spread is how far a request to the end may read ahead of another one
// still reading. AVFoundation asks for two to the end (0-end, then X-end
// a few hundred KB in), which pacing holds only in files over 30 s:
// within it, what the one ahead fetched is still in the source's cache
// when the one behind gets there (the span between them and the GETs'
// read-ahead past them fit in it), so the GET behind ends there (the
// RangeSource's overtaken) and each byte comes from the network once.
const spread = media.CacheMax/2 - 2*media.BlockSize

// readTo notes request req's offset (-1: it ended) and wakes the requests
// held behind it.
func (p *player) readTo(req uint64, at int64) {
	p.mu.Lock()
	if p.toEnd == nil {
		p.toEnd = map[uint64]int64{}
	}
	if at < 0 {
		delete(p.toEnd, req)
	} else {
		p.toEnd[req] = at
	}
	if p.moved != nil {
		close(p.moved)
		p.moved = nil
	}
	p.mu.Unlock()
}

// holdBehind holds request req at cur while it's more than spread ahead
// of another request to the end still reading; false: ctx ended.
func (p *player) holdBehind(ctx context.Context, req uint64, cur int64) bool {
	for {
		p.mu.Lock()
		lo := int64(math.MaxInt64)
		for r, at := range p.toEnd {
			if r != req && at < lo {
				lo = at
			}
		}
		if lo == math.MaxInt64 || cur <= lo+spread {
			p.mu.Unlock()
			return true
		}
		if p.moved == nil {
			p.moved = make(chan struct{})
		}
		moved := p.moved
		p.mu.Unlock()
		select {
		case <-ctx.Done():
			return false
		case <-moved:
		}
	}
}

// pace holds a request to the end of the resource at media.PaceLimit:
// AVFoundation takes every byte responded at once, so unheld it would pull
// the whole file in as fast as the network goes. AVPlayer keeps that
// request open while paused, and Base.Pace waits on the state then, not a
// timer. false: ctx ended.
func (p *player) pace(ctx context.Context, cur int64) bool {
	return p.Pace(ctx, p.rs, cur)
}

func (p *player) finish(req uint64, failed bool) {
	p.objMu.RLock()
	defer p.objMu.RUnlock()
	if p.obj != nil {
		C.clack_avf_finish(p.obj, C.uint64_t(req), b2i(failed))
	}
}

//export clackAVFCancel
func clackAVFCancel(h C.uintptr_t, req C.uint64_t) {
	if p := lookup(h); p != nil {
		p.mu.Lock()
		if c, ok := p.loads[uint64(req)]; ok {
			c()
			delete(p.loads, uint64(req))
		}
		p.mu.Unlock()
	}
}

// watchingOf is whether obj's state watch timer runs (tests: a paused
// audio item has none).
func watchingOf(obj unsafe.Pointer) bool { return C.clack_avf_watching(obj) != 0 }

// history turns on, for the players made from now on, a history of what
// Clack asked and what AVPlayer did (tests: debug reads it).
func history(on bool) { C.clack_avf_history(b2i(on)) }

// debug is the player's history and AVPlayer's state now (tests): its
// rate, timeControlStatus, reasonForWaitingToPlay, position, the item's
// timebase and buffer.
func (p *player) debug() string {
	p.objMu.RLock()
	defer p.objMu.RUnlock()
	if p.obj == nil {
		return "(no AVPlayer: not made yet, or closed)"
	}
	c := C.clack_avf_debug(p.obj)
	defer C.free(unsafe.Pointer(c))
	return C.GoString(c)
}

// live is the Objective-C side's live objects: players, pixel buffers,
// textures, data requests.
func live() [4]int32 {
	var out [4]C.int32_t
	C.clack_avf_live(&out[0])
	return [4]int32{int32(out[0]), int32(out[1]), int32(out[2]), int32(out[3])}
}

// runMainLoop runs the main thread's run loop until stopMainLoop: tests
// only (AVFoundation posts some changes on the main queue, which Clack's
// NSApplication runs). Call it on the main thread.
func runMainLoop()  { C.clack_avf_run_main_loop() }
func stopMainLoop() { C.clack_avf_stop_main_loop() }

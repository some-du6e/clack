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
	p := &player{src: src, frameReady: frameReady, loads: map[uint64]context.CancelFunc{}, done: make(chan struct{})}
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

	mu     sync.Mutex
	pend   pending // the controls before obj was made
	loads  map[uint64]context.CancelFunc
	failed bool
	done   chan struct{}
}

// readLog, when set (tests), sees each of an asset's data requests.
var (
	readLogMu sync.Mutex
	readLog   func(id string, off, n int64, toEnd bool)
)

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
	if err != nil {
		p.fail(media.ErrNetwork, "the file: "+p.rs.Detail(err))
		return
	}
	if info.Total <= 0 {
		p.fail(media.ErrSrcNotFound, "the server didn't say the file's size")
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
		p.fail(media.ErrSrcNotFound, "the asset's URL")
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
	log.Printf("media: %s: AVFoundation (%s, %d bytes, seekable %v)", p.src.FileID, utiFor(p.src.Mime, ext), info.Total, info.Seekable)
}

// utiFor is the asset's type for AVFoundation's content information.
func utiFor(mime, ext string) string {
	switch {
	case ext == "mov" || mime == "video/quicktime":
		return "com.apple.quicktime-movie"
	case ext == "m4v" || mime == "video/x-m4v":
		return "com.apple.m4v-video"
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
	if toEnd {
		end = math.MaxInt64
	}
	buf := make([]byte, 256<<10)
	for cur := off; cur < end; {
		k, err := p.rs.ReadSome(ctx, buf[:min(int64(len(buf)), end-cur)], cur)
		if k > 0 {
			p.objMu.RLock()
			if p.obj != nil && ctx.Err() == nil {
				C.clack_avf_respond(p.obj, C.uint64_t(req), unsafe.Pointer(&buf[0]), C.int(k))
			}
			p.objMu.RUnlock()
			cur += int64(k)
		}
		if err != nil {
			if ctx.Err() != nil {
				return // cancelled: AVFoundation dropped it
			}
			p.finish(req, !errors.Is(err, io.EOF))
			return
		}
	}
	p.finish(req, false)
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

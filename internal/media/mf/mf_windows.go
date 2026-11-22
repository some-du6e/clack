//go:build windows && cgo

package mf

/*
#cgo LDFLAGS: -ld3d11 -lole32 -loleaut32
#include <stdlib.h>
#include "mf_windows.h"
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"log"
	"math"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/media"
)

// The DXGI formats of the ring and of Gio's copy of a frame.
const (
	fmtRGBA = 28 // DXGI_FORMAT_R8G8B8A8_UNORM (Gio's own images' bytes)
	fmtBGRA = 87 // DXGI_FORMAT_B8G8R8A8_UNORM
	srgbOf  = 1  // the _SRGB format is the next one for RGBA (29); BGRA's is 91
)

// Media Engine events (MF_MEDIA_ENGINE_EVENT): below 1000 HTML5's.
const (
	evError          = 5
	evPlay           = 8
	evPause          = 9
	evLoadedMetadata = 10
	evLoadedData     = 11
	evWaiting        = 12
	evPlaying        = 13
	evSeeking        = 16
	evSeeked         = 17
	evEnded          = 19
	evDurationChange = 21
	evFirstFrame     = 1009
	evResourceLost   = 1012
	evStreamError    = 1014 // one stream failed to render (no audio device): the rest plays
	evAudioEndpoint  = 1016
)

const (
	eAbort      = -2147467260 // E_ABORT
	eFail       = -2147467259 // E_FAIL
	waitTimeout = 0x102       // WAIT_TIMEOUT: clack_me_tick found no texture of the ring free
)

// The controls (mf_windows.h's CLACK_MF_*) and the live objects' kinds.
const (
	opPlay = 1 + iota
	opPause
	opSeek
	opVolume
	opMuted
	opLoop
	opRate
)

const liveN = 7 // CLACK_MF_LIVE_N

func init() { media.Register(backend{}) }

type backend struct{}

func (backend) Name() string { return "mediafoundation" }

// Available is nil once mfplat.dll is loaded and MFStartup answered
// (Windows N without the Media Feature Pack has no mfplat.dll).
func (backend) Available() error {
	if hr := C.clack_me_startup(); hr != 0 {
		return fmt.Errorf("mf: mfplat.dll / MFStartup: %s", hrString(int32(hr)))
	}
	return nil
}

func (b backend) New(src media.Source, frameReady func()) (media.Player, error) {
	if err := b.Available(); err != nil {
		return nil, err
	}
	if src.Open == nil {
		return nil, errors.New("mf: a source with no Open")
	}
	if frameReady == nil {
		frameReady = func() {}
	}
	p := &player{src: src, frameReady: frameReady, wake: make(chan struct{}, 1), mfEv: make(chan mfEvent, 256),
		bind: make(chan struct{}, 1), quit: make(chan struct{}), done: make(chan struct{}), volume: 1, rate: 1, born: time.Now()}
	p.Init()
	p.framePts.Store(-1)
	p.prevPts.Store(-1)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.rs = media.NewRangeSource(src, media.RangeOptions{})
	p.frames = media.NewFrameHandle(p)
	p.id = register(p)
	p.c = C.clack_me_new(C.uintptr_t(p.id))
	if p.c == nil {
		unregister(p.id)
		return nil, errors.New("mf: out of memory")
	}
	go p.run()
	return p, nil
}

// players are the live players by id: the C side's callbacks name their
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

// refused are the formats an engine in this process wouldn't transfer
// its first frame in (formatBit's): the players after it start with the
// other.
var refused atomic.Uint32

func formatBit(f uint32) uint32 {
	if f == fmtBGRA {
		return 2
	}
	return 1
}

func formatName(f uint32) string {
	if f == fmtBGRA {
		return "BGRA"
	}
	return "RGBA"
}

func otherFormat(f uint32) uint32 {
	if f == fmtBGRA {
		return fmtRGBA
	}
	return fmtBGRA
}

// forceTransferFailures makes the next transfers fail (tests of the
// format fallback).
var forceTransferFailures atomic.Int32

// delayFirstTransfer holds a player's first transfer until its clock is
// this far in (nanoseconds; tests: a runner's first frame that came 1 to
// 1.65 s after the engine played, so the clock ran through the video's
// first second with nothing drawn). By the clock, not a time after
// loadeddata: in a cold process the engine plays a while after its data,
// and a fixed delay left the first frame short of the second it was to
// miss.
var delayFirstTransfer atomic.Int64

// forceAudioRenderer makes engines with their audio renderer even where
// there's no audio output (tests: the 2.2 s to the metadata, muted or not).
var forceAudioRenderer atomic.Bool

// The tests' hooks, set before any player is made: each of the demuxer's
// reads as it ends (the file's id, where, the bytes it got, when the
// engine asked for it), and each of the engine's events as it comes
// (codes below 1000 are HTML5's).
var (
	testRead  func(id string, off int64, n int, asked time.Time)
	testEvent func(id string, code uint32)
)

type mfEvent struct {
	gen  uint32 // the engine's
	code uint32
	p1   uint64
	p2   uint32
}

// player is one Media Engine. Its controls run on its media thread (a
// goroutine locked to an OS thread in COM's multithreaded apartment),
// which owns the engine, its device and the ring; Gio's asks come on
// Gio's render thread, under askMu (which the ring's changes and Close
// take too).
type player struct {
	// framePts is the presentation time of the frame transferred last
	// (any goroutine; -1 before one): the tests check a drawn frame's
	// colour at its own time, not at the clock's position
	framePts atomic.Int64
	// prevPts is the one transferred before it: the frame drawn can be
	// either (the ring's draw takes a slot the newest transfer may not have
	// reached yet)
	prevPts atomic.Int64
	media.Base
	src        media.Source
	rs         *media.RangeSource
	frameReady func()
	frames     *media.FrameHandle
	id         uintptr
	ctx        context.Context
	cancel     context.CancelFunc
	born       time.Time // when New made it

	askMu  sync.Mutex
	c      *C.clackMF // nil once closed (under askMu)
	gioDev uintptr    // the device Gio last asked with (under askMu)

	qmu   sync.Mutex
	queue []func()
	wake  chan struct{}
	mfEv  chan mfEvent
	bind  chan struct{}
	quit  chan struct{}
	once  sync.Once
	done  chan struct{}

	// the media thread's
	info           media.Info
	infoErr        error
	infoOK         bool
	bound          bool
	opened         bool
	failed         bool
	format         uint32
	engineGen      uint32 // the engine's, as its events carry it
	fellBack       bool   // made again for the other format (once)
	okTransfers    int    // the frames this engine transferred
	reopening      bool
	forceFrame     bool // a new ring: the frame presented goes into it (clack_me_tick's force)
	reopens        int
	ringW, ringH   uint32
	native, draw   image.Point
	ringOK         bool
	fps            float64
	wantPlay       bool
	seekTo         float64 // a seek before the engine was there, -1 none
	volume, rate   float64
	muted, loop    bool
	wantFrame      bool          // tick until the frame at the position is drawn
	wantUntil      time.Time     // or until then
	wantFrom       int64         // NTransfers when it was asked for
	emptyTicks     int           // polls in a row with no new frame
	lastPts        time.Duration // the newest frame transferred's, -1 none
	seeking        bool          // a seek sent, no seeked yet: poll on
	seekUntil      time.Time     // (at most until then)
	lastRefresh    time.Time
	restoreSeek    bool
	transferErrs   int
	loggedRing     bool
	loggedAskFail  bool
	loggedTransfer bool
	loggedFirst    bool
	// the default audio output: known once the check answered (or took
	// too long); none, the engine is made muted
	audioKnown bool
	noAudio    bool
	audio      string // what the check found, for the logs
	// when the steps to the first frame were done (the timing log)
	tInfo, tBound, tOpened, tMeta, tData time.Time
}

func (p *player) post(f func()) {
	p.qmu.Lock()
	p.queue = append(p.queue, f)
	p.qmu.Unlock()
	p.poke()
}

func (p *player) poke() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *player) Play()  { p.post(func() { p.wantPlay = true; p.TouchAsk(); p.control(opPlay, 0) }) }
func (p *player) Pause() { p.post(func() { p.wantPlay = false; p.control(opPause, 0) }) }
func (p *player) Seek(t time.Duration) {
	p.post(func() {
		p.TouchAsk()
		// currentTime reads the new position at once, as HTML's does
		p.Update(func(st *media.State) {
			st.Pos, st.Ended = max(t, 0), false
			if st.Duration > 0 && st.Pos > st.Duration {
				st.Pos = st.Duration
			}
		})
		if !p.opened {
			p.seekTo = t.Seconds()
			return
		}
		p.control(opSeek, t.Seconds())
		p.wantFrameHere()
	})
}
func (p *player) SetVolume(v float64) {
	p.post(func() { p.volume = min(max(v, 0), 1); p.control(opVolume, p.volume); p.syncControls() })
}
func (p *player) SetMuted(m bool) {
	p.post(func() { p.muted = m; p.control(opMuted, b2f(m)); p.syncControls() })
}
func (p *player) SetLoop(l bool) {
	p.post(func() { p.loop = l; p.control(opLoop, b2f(l)); p.syncControls() })
}
func (p *player) SetRate(r float64) {
	if r <= 0 {
		return
	}
	p.post(func() { p.rate = r; p.control(opRate, r); p.Base.SetRate(r) })
}
func (p *player) SetDrawSize(px image.Point) {
	p.post(func() {
		p.draw = px
		p.makeRing()
	})
}

func (p *player) Op(still paint.ImageOp, size image.Point) paint.ImageOp {
	return p.frames.Op(still, size)
}

// CopyFrame is the frame shown now (media.FrameCopier): the ring's newest
// texture read back on the media thread, at the ring's size.
func (p *player) CopyFrame() (*image.RGBA, bool) {
	type shot struct {
		img *image.RGBA
		ok  bool
	}
	ch := make(chan shot, 1)
	p.post(func() {
		if p.c == nil || !p.ringOK || p.ringW == 0 || p.ringH == 0 {
			ch <- shot{}
			return
		}
		img := image.NewRGBA(image.Rect(0, 0, int(p.ringW), int(p.ringH)))
		r := C.clack_me_snapshot(p.c, (*C.uint8_t)(unsafe.Pointer(&img.Pix[0])), C.size_t(len(img.Pix)), C.uint32_t(p.ringW), C.uint32_t(p.ringH))
		ch <- shot{img, r == 1}
	})
	select {
	case s := <-ch:
		return s.img, s.ok
	case <-p.done:
		return nil, false
	case <-time.After(2 * time.Second):
		return nil, false
	}
}

func (p *player) Stats() media.Stats {
	st := p.BaseStats(p.rs)
	p.askMu.Lock()
	if p.c != nil {
		var skips, timeouts, copies C.int64_t
		C.clack_me_counts(p.c, &skips, &timeouts, &copies)
		st.Skipped, st.Timeouts = int64(skips), int64(timeouts)
	}
	p.askMu.Unlock()
	return st
}

// Close ends the player: its reads abort at once (the media thread may
// be in a cgo call), and the media thread lets go of the rest.
func (p *player) Close() {
	p.once.Do(func() {
		p.cancel()
		p.rs.Close()
		close(p.quit)
	})
}

// closing is whether Close was called.
func (p *player) closing() bool {
	select {
	case <-p.quit:
		return true
	default:
		return false
	}
}

// bindHook and closeHook run on the media thread just before the
// device's making and the engine's Shutdown (tests: askMu is free then).
var bindHook, closeHook func(*player)

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// syncControls keeps the snapshot's volume, muted and loop as set.
func (p *player) syncControls() {
	p.Update(func(st *media.State) { st.Volume, st.Muted, st.Loop = p.volume, p.muted, p.loop })
}

// control runs a control on the engine, once it's there.
func (p *player) control(op int, v float64) {
	if !p.opened || p.c == nil {
		return
	}
	hr := C.clack_me_control(p.c, C.int(op), C.double(v))
	if op == opSeek && hr == 0 {
		p.seeking, p.seekUntil = true, time.Now().Add(10*time.Second)
	}
	if hr == 0 || op == opPlay || op == opPause || p.noAudio && (op == opVolume || op == opMuted) {
		return
	}
	log.Printf("media: %s: control %d: %s", p.src.FileID, int(op), hrString(int32(hr)))
}

// Texture implements media.Frames: Gio's ask, on its render thread.
func (p *player) Texture(dev media.Device) (media.Texture, bool) {
	if dev.API != media.GPUDirect3D11 || dev.Handle == 0 {
		return media.Texture{}, false
	}
	if p.Asked() {
		p.poke() // asked again after a gap: the frames go on
	}
	p.askMu.Lock()
	defer p.askMu.Unlock()
	if p.c == nil {
		return media.Texture{}, false
	}
	var key C.uint64_t
	var status C.int
	var info C.clackMFInfo
	srv := C.clack_me_ask(p.c, C.uintptr_t(dev.Handle), &key, &status, &info)
	if status&1 != 0 {
		p.gioDev = dev.Handle
		select {
		case p.bind <- struct{}{}:
		default:
		}
	}
	if status&2 != 0 && !p.loggedAskFail {
		p.loggedAskFail = true
		log.Printf("media: %s: opening the frames on Gio's device: %s: %s", p.src.FileID, C.GoString(&info.step[0]), hrString(int32(info.hr)))
	}
	if srv == 0 {
		return media.Texture{}, false
	}
	return media.Texture{Handle: uintptr(srv), Key: uint64(key)}, true
}

// run is the media thread.
func (p *player) run() {
	runtime.LockOSThread() // never unlocked: the thread goes with the goroutine
	C.clack_me_thread_init()
	defer close(p.done)
	defer C.clack_me_thread_done()
	p.seekTo, p.lastPts = -1, -1
	infoCh := make(chan error, 1)
	go func() {
		info, err := p.rs.Info(p.ctx)
		p.info = info // read by run only after infoCh
		infoCh <- err
	}()
	audio := checkAudio()
	audioLate := time.NewTimer(audioWait)
	defer audioLate.Stop()
	var tick *time.Ticker
	defer func() {
		if tick != nil {
			tick.Stop()
		}
	}()
	for {
		var tc <-chan time.Time
		if tick != nil {
			tc = tick.C
		}
		var audioC <-chan struct{}
		var lateC <-chan time.Time
		if !p.audioKnown {
			audioC, lateC = audio.done, audioLate.C
		}
		select {
		case <-p.quit:
			p.shutdown()
			return
		case err := <-infoCh:
			p.infoOK, p.infoErr, p.tInfo = true, err, time.Now()
			if err != nil {
				// before the metadata a failed fetch is error 4, as for a file that
				// can't be played: Chromium's MediaLoadingFailed for a src (the fake's)
				p.fail(media.ErrSrcNotSupported, "the file: "+p.rs.Detail(err))
			}
		case <-audioC:
			p.audioKnown = true
			if p.noAudio = audio.err != nil; p.noAudio {
				p.audio = fmt.Sprintf("no output (%v): the engine is made muted (MF_MEDIA_ENGINE_FORCEMUTE)", audio.err)
				if forceAudioRenderer.Load() {
					p.noAudio = false
					p.audio = fmt.Sprintf("no output (%v), the engine made with its audio renderer all the same (the test's)", audio.err)
				}
			} else {
				p.audio = "the default output"
			}
		case <-lateC:
			p.audioKnown, p.noAudio = true, true
			p.audio = fmt.Sprintf("the check of the default output didn't answer within %v: the engine is made muted (MF_MEDIA_ENGINE_FORCEMUTE)", audioWait)
		case <-p.bind:
			p.doBind()
		case <-p.wake:
			p.qmu.Lock()
			q := p.queue
			p.queue = nil
			p.qmu.Unlock()
			for _, f := range q {
				f()
			}
		case ev := <-p.mfEv:
			p.onEvent(ev)
		case <-tc:
			p.tick()
		}
		p.maybeOpen()
		tick = p.ticker(tick)
	}
}

// ticker is the poll for frames: only while it plays and Gio asked within
// the last second, or while the frame drawn isn't the one at the position
// (the data came, a seek, an engine made again; the engine needs the
// polls to present a paused seek's frame before it sends seeked).
func (p *player) ticker(t *time.Ticker) *time.Ticker {
	st := p.State()
	playing := !st.Paused && !st.Ended && st.Err == 0
	now := time.Now()
	if p.wantFrame && !p.seeking && (!now.Before(p.wantUntil) || p.frameHere(st.Pos)) {
		p.wantFrame = false
	}
	seeking := p.seeking && now.Before(p.seekUntil)
	want := p.opened && p.ringOK && !p.failed && ((playing && p.AskedWithin(time.Second)) || p.wantFrame || seeking || p.forceFrame)
	switch {
	case want && t == nil:
		fps := p.fps
		if fps <= 0 || math.IsNaN(fps) {
			fps = 30
		}
		d := time.Duration(float64(time.Second) / fps / 2)
		return time.NewTicker(min(max(d, 4*time.Millisecond), 20*time.Millisecond))
	case !want && t != nil:
		t.Stop()
		return nil
	}
	return t
}

// wantFrameHere asks for polls until the frame at the position is drawn,
// for 2 s at most.
func (p *player) wantFrameHere() {
	p.wantFrame, p.wantUntil, p.wantFrom, p.emptyTicks = true, time.Now().Add(2*time.Second), p.NTransfers.Load(), 0
}

// frameHere is whether the frame transferred last is the one at pos: its
// presentation time within a frame and a half of it, or the engine had
// nothing newer for 8 polls since one came.
func (p *player) frameHere(pos time.Duration) bool {
	if p.lastPts < 0 {
		return false
	}
	fps := p.fps
	if fps <= 0 || math.IsNaN(fps) {
		fps = 30
	}
	slack := time.Duration(1.5 * float64(time.Second) / fps)
	return absDur(pos-p.lastPts) <= slack || p.NTransfers.Load() > p.wantFrom && p.emptyTicks >= 8
}

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// doBind makes the engine's device on the adapter of Gio's. The device
// and its manager are made without askMu, which Gio's ask takes on its
// render thread (D3D11CreateDevice takes tens of milliseconds on a real
// GPU); only the swap, which lets go of the ring the ask reads, takes it.
func (p *player) doBind() {
	if p.c == nil {
		return
	}
	var info C.clackMFInfo
	p.askMu.Lock()
	gio := C.clack_me_gio_ref(p.c)
	p.askMu.Unlock()
	var dev C.uintptr_t
	if bindHook != nil {
		bindHook(p)
	}
	hr := C.clack_me_bind(p.c, gio, &info, &dev)
	C.clack_me_dev_release(gio)
	if hr == 0 && dev != 0 {
		p.askMu.Lock()
		C.clack_me_bind_set(p.c, dev)
		p.askMu.Unlock()
	}
	if hr != 0 {
		p.fail(media.ErrSrcNotSupported, fmt.Sprintf("the device on %q: %s: %s", C.GoString(&info.adapter[0]), C.GoString(&info.step[0]), hrString(int32(hr))))
		return
	}
	if !p.bound {
		decode := "video support (hardware decoding)"
		if uint32(info.flags)&0x800 == 0 { // D3D11_CREATE_DEVICE_VIDEO_SUPPORT
			decode = "no video support (" + hrString(int32(info.videoHR)) + "): Media Foundation decodes in software"
		}
		log.Printf("media: %s: Media Foundation on %q (vendor %#x, device %#x, feature level %s), %s", p.src.FileID,
			C.GoString(&info.adapter[0]), uint32(info.vendor), uint32(info.device), featureLevel(uint32(info.featureLevel)), decode)
		p.tBound = time.Now()
	}
	p.bound = true
	p.ringOK = false
	p.makeRing()
}

// maybeOpen makes the engine once the file's size is known, Gio's device
// is bound and the audio output is known.
func (p *player) maybeOpen() {
	if p.opened || p.failed || !p.bound || !p.infoOK || p.infoErr != nil || !p.audioKnown || p.c == nil {
		return
	}
	if p.info.Total <= 0 {
		p.fail(media.ErrSrcNotSupported, "the server didn't say the file's size")
		return
	}
	if p.format == 0 {
		p.format = p.pickFormat()
	}
	ext := strings.ToLower(strings.TrimPrefix(p.src.Ext, "."))
	if ext == "" {
		ext = "mp4"
	}
	u16, err := syscall.UTF16FromString("clack-media://" + p.src.FileID + "." + ext)
	if err != nil {
		p.fail(media.ErrSrcNotSupported, err.Error())
		return
	}
	var info C.clackMFInfo
	seekable := C.int(0)
	if p.info.Seekable {
		seekable = 1
	}
	flags := C.uint32_t(0)
	if p.noAudio {
		flags |= C.CLACK_MF_FORCEMUTE
	}
	p.engineGen++
	if hr := C.clack_me_open(p.c, C.int64_t(p.info.Total), seekable, (*C.uint16_t)(unsafe.Pointer(&u16[0])), C.uint32_t(p.format), flags, C.uint32_t(p.engineGen), &info); hr != 0 {
		p.fail(media.ErrSrcNotSupported, fmt.Sprintf("the engine: %s: %s", C.GoString(&info.step[0]), hrString(int32(hr))))
		return
	}
	runtime.KeepAlive(u16)
	p.opened, p.okTransfers = true, 0
	if p.tOpened.IsZero() {
		p.tOpened = time.Now()
	}
	p.control(opVolume, p.volume)
	p.control(opMuted, b2f(p.muted))
	p.control(opLoop, b2f(p.loop))
	if p.rate != 1 {
		p.control(opRate, p.rate)
	}
	if p.seekTo >= 0 {
		p.control(opSeek, p.seekTo)
		p.seekTo = -1
	}
	if p.wantPlay {
		p.control(opPlay, 0)
	}
}

// pickFormat is the frames' format. BGRA first: the format Microsoft's
// frame-server samples and Chromium's MediaFoundationRenderer ask for,
// and the one whose colours came out exact where Media Foundation decodes
// and converts in software (the runners' Basic Render Driver), where RGBA
// frames got BT.709's transfer curve converted to sRGB's (grey 64 read
// 79, 192 read 198). RGBA first with CLACK_MF_FORMAT=rgba. A format an
// engine in this process refused comes last, and one Gio's device can't
// sample as _SRGB isn't used.
func (p *player) pickFormat() uint32 {
	order := [2]uint32{fmtBGRA, fmtRGBA}
	if os.Getenv("CLACK_MF_FORMAT") == "rgba" {
		order = [2]uint32{fmtRGBA, fmtBGRA}
	}
	if refused.Load()&formatBit(order[0]) != 0 {
		order[0], order[1] = order[1], order[0]
	}
	dev := p.gioDevice()
	for _, f := range order {
		if gioSamples(dev, f) {
			return f
		}
	}
	return order[0]
}

func (p *player) gioDevice() uintptr {
	p.askMu.Lock()
	defer p.askMu.Unlock()
	return p.gioDev
}

// gioSamples is whether Gio's device samples f's _SRGB format (true while
// no device asked).
func gioSamples(dev uintptr, f uint32) bool {
	return dev == 0 || C.clack_me_gio_format_ok(C.uintptr_t(dev), C.uint32_t(srgb(f))) != 0
}

func srgb(f uint32) uint32 {
	if f == fmtBGRA {
		return 91 // DXGI_FORMAT_B8G8R8A8_UNORM_SRGB
	}
	return f + srgbOf // 29, DXGI_FORMAT_R8G8B8A8_UNORM_SRGB
}

// makeRing (re)makes the shared textures at the video's size fitted into
// the drawn size (never bigger than the video), once both are known.
func (p *player) makeRing() {
	if !p.bound || !p.opened || p.c == nil || p.native.X <= 0 || p.native.Y <= 0 {
		return
	}
	w, h := fit(p.native, p.draw)
	if p.ringOK && w == p.ringW && h == p.ringH {
		return
	}
	var info C.clackMFInfo
	p.askMu.Lock()
	hr := C.clack_me_ring(p.c, C.uint32_t(w), C.uint32_t(h), &info)
	p.askMu.Unlock()
	if hr != 0 {
		p.fail(media.ErrDecode, fmt.Sprintf("the frames' textures: %s: %s", C.GoString(&info.step[0]), hrString(int32(hr))))
		return
	}
	p.ringOK, p.ringW, p.ringH = true, w, h
	if p.okTransfers > 0 {
		// the new ring has no frame: the one the engine presented goes into
		// it whatever OnVideoStreamTick says (paused, it has no new one), so
		// a paused video keeps its frame at any size, as a <video> does
		p.forceFrame = true
	}
	if !p.loggedRing {
		p.loggedRing = true
		handles := "NT"
		if info.nt == 0 {
			handles = "legacy shared"
		}
		log.Printf("media: %s: frames %d×%d (video %d×%d) %s, through %s handles", p.src.FileID, w, h, p.native.X, p.native.Y, formatName(p.format), handles)
	}
}

// fit is the video's size scaled into the drawn size, keeping its aspect,
// never larger than it (the drawn size zero: the video's).
func fit(native, draw image.Point) (uint32, uint32) {
	if draw.X <= 0 || draw.Y <= 0 || (draw.X >= native.X && draw.Y >= native.Y) {
		return uint32(native.X), uint32(native.Y)
	}
	s := min(float64(draw.X)/float64(native.X), float64(draw.Y)/float64(native.Y))
	w, h := max(int(math.Round(float64(native.X)*s)), 2), max(int(math.Round(float64(native.Y)*s)), 2)
	return uint32(w), uint32(h)
}

// tick is the media thread's poll for a new frame.
func (p *player) tick() {
	p.NTicks.Add(1)
	if time.Since(p.lastRefresh) > media.TimeUpdateEvery {
		p.refresh()
	}
	if d := delayFirstTransfer.Load(); d > 0 && p.okTransfers == 0 && !p.tData.IsZero() && p.State().Pos < time.Duration(d) {
		return
	}
	var hr C.int32_t
	var pts C.int64_t
	force := p.forceFrame
	var f C.int
	if force {
		f = 1
	}
	r := C.clack_me_tick(p.c, f, &hr, &pts)
	if r == 1 && forceTransferFailures.Load() > 0 {
		forceTransferFailures.Add(-1)
		r, hr = -1, eFail
	}
	switch r {
	case 0:
		if hr == 1 { // S_FALSE: no new frame
			p.emptyTicks++
		}
		if force && hr != waitTimeout { // a failed forced transfer: the poster stays, as before
			p.forceFrame = false
		}
	case 1:
		p.forceFrame = false
		p.transferErrs = 0
		p.okTransfers++
		p.emptyTicks = 0
		if pts >= 0 { // a forced transfer's frame is the one presented
			p.lastPts = time.Duration(pts) * 100
			p.prevPts.Store(p.framePts.Load())
			p.framePts.Store(int64(p.lastPts))
		}
		p.NTransfers.Add(1)
		p.NFrames.Add(1)
		if !p.loggedFirst {
			p.loggedFirst = true
			p.logTiming()
		}
		p.frameReady()
	case -1:
		p.transferFailed(int32(hr))
	}
}

// logTiming is the line saying where the time to the first frame went,
// each step's end in seconds after the player was made.
func (p *player) logTiming() {
	at := func(t time.Time) string {
		if t.IsZero() {
			return "-"
		}
		return fmt.Sprintf("%.2f s", t.Sub(p.born).Seconds())
	}
	log.Printf("media: %s: first frame %s after the player was made (the file's head %s, the device %s, the engine %s, loadedmetadata %s, loadeddata %s); audio: %s",
		p.src.FileID, at(time.Now()), at(p.tInfo), at(p.tBound), at(p.tOpened), at(p.tMeta), at(p.tData), p.audio)
}

// transferFailed: an engine refusing its first frame in its format is
// made again for the other one, where the playback was (once a player,
// and the players after it start with the other); otherwise it's logged,
// and ten in a row end the playback with a decode error.
func (p *player) transferFailed(hr int32) {
	if other := otherFormat(p.format); p.okTransfers == 0 && !p.fellBack && gioSamples(p.gioDevice(), other) {
		p.fellBack = true
		refused.Or(formatBit(p.format))
		log.Printf("media: %s: the engine refused a %s frame (%s): %s from here on", p.src.FileID, formatName(p.format), hrString(hr), formatName(other))
		p.reopen(other)
		return
	}
	p.transferErrs++
	if !p.loggedTransfer {
		p.loggedTransfer = true
		log.Printf("media: %s: TransferVideoFrame: %s", p.src.FileID, hrString(hr))
	}
	if p.transferErrs >= 10 {
		p.fail(media.ErrDecode, fmt.Sprintf("TransferVideoFrame: %s", hrString(hr)))
	}
}

// reopen makes the engine again for another format, from where it was,
// playing if it played; its events until it's back are not passed on. To
// the page it's a stall: the clock holds, the last frame stays drawn,
// and a playing player sends waiting, then playing once the new engine
// plays.
func (p *player) reopen(format uint32) {
	st := p.State()
	// the engine's Shutdown, without askMu: the ask reads none of it
	C.clack_me_close_engine(p.c)
	p.opened, p.ringOK, p.format, p.reopening = false, false, format, true
	p.lastPts, p.seeking = -1, false
	p.restoreSeek = st.Pos > 0
	if p.restoreSeek {
		p.seekTo = st.Pos.Seconds()
	}
	p.wantPlay = !st.Paused
	if p.wantPlay && !st.Waiting {
		p.Update(func(s *media.State) { s.Waiting, s.Ready = true, min(s.Ready, media.HaveMetadata) })
		p.Emit(media.Waiting)
	}
	p.reopens++
	n := p.reopens
	time.AfterFunc(reopenWait, func() {
		p.post(func() {
			if p.reopening && p.reopens == n {
				p.fail(media.ErrDecode, fmt.Sprintf("the engine made again for %s didn't play within %v", formatName(p.format), reopenWait))
			}
		})
	})
	p.maybeOpen()
}

// reopenWait is how long an engine made again has to play where the one
// before it was.
const reopenWait = 15 * time.Second

// refresh reads the engine's state into the snapshot.
func (p *player) refresh() {
	if !p.opened || p.c == nil || p.reopening {
		return
	}
	p.lastRefresh = time.Now()
	var s C.clackMFState
	C.clack_me_state(p.c, &s)
	if f := float64(s.fps); f > 0 {
		p.fps = f
	}
	if w, h := int(s.w), int(s.h); w > 0 && h > 0 && image.Pt(w, h) != p.native {
		p.native = image.Pt(w, h)
		p.makeRing()
	}
	p.Update(func(st *media.State) {
		st.Pos = secs(float64(s.pos))
		if d := float64(s.dur); !math.IsNaN(d) && !math.IsInf(d, 0) && d > 0 {
			st.Duration = secs(d)
		}
		st.Paused, st.Ended, st.Ready = s.paused != 0, s.ended != 0, int(s.ready)
		if s.err != 0 {
			st.Err = int(s.err)
		}
		st.Size = p.native
		st.Volume, st.Muted, st.Loop = p.volume, p.muted, p.loop
	})
}

func secs(s float64) time.Duration {
	if math.IsNaN(s) || s < 0 {
		return 0
	}
	return time.Duration(s * float64(time.Second))
}

// onEvent hands on the engine's event, its state read with it.
func (p *player) onEvent(ev mfEvent) {
	if p.c == nil || ev.gen != p.engineGen {
		return // closed, or from the engine before this one
	}
	if testEvent != nil {
		testEvent(p.src.FileID, ev.code)
	}
	switch ev.code {
	case evSeeking:
		if !p.seeking {
			p.seeking, p.seekUntil = true, time.Now().Add(10*time.Second) // the engine's own (a loop)
		}
	case evSeeked:
		p.seeking = false
		p.wantFrameHere() // until the frame there is drawn (it may be already)
	case evLoadedMetadata:
		if p.reopening {
			p.makeRing() // the same video: the polls a paused seek needs
		}
	}
	switch ev.code {
	case evStreamError:
		log.Printf("media: %s: a stream can't be played (no audio device?), the rest plays: %s", p.src.FileID, hrString(int32(ev.p2)))
		return
	case evAudioEndpoint:
		log.Printf("media: %s: the audio device changed", p.src.FileID)
		return
	case evResourceLost:
		log.Printf("media: %s: the device was lost", p.src.FileID)
		return
	}
	if p.reopening {
		// the engine made again: its own events up to where it was aren't
		// news; it's back once it plays (or, paused, has the frame where
		// it was), or paused there by a Pause meanwhile
		back := false
		switch ev.code {
		case evError:
			p.reopening = false // passed on
		case evPlaying:
			back = p.wantPlay
		case evPause:
			back = !p.wantPlay
		case evSeeked:
			back = !p.wantPlay && p.restoreSeek
		case evLoadedData:
			back = !p.wantPlay && !p.restoreSeek
		}
		if !back {
			if p.reopening {
				return
			}
		} else {
			p.reopening = false
			waiting := p.State().Waiting
			p.refresh()
			p.makeRing()
			p.wantFrameHere()
			if waiting {
				p.Update(func(st *media.State) { st.Waiting = false })
				if ev.code == evPlaying {
					p.Emit(media.Playing)
				}
			}
			if ev.code == evPause {
				p.Emit(media.Pause)
			}
			return
		}
	}
	p.refresh()
	var k media.EventKind
	switch ev.code {
	case evLoadedMetadata:
		k = media.LoadedMetadata
		if p.tMeta.IsZero() {
			p.tMeta = time.Now()
		}
		p.makeRing()
	case evLoadedData:
		k = media.LoadedData
		if p.tData.IsZero() {
			p.tData = time.Now()
		}
		p.wantFrameHere()
	case evPlay:
		k = media.Play
	case evPause:
		k = media.Pause
	case evWaiting:
		k = media.Waiting
		p.Update(func(st *media.State) { st.Waiting = true })
	case evPlaying:
		k = media.Playing
		p.Update(func(st *media.State) { st.Waiting = false })
	case evSeeking:
		k = media.Seeking
	case evSeeked:
		k = media.Seeked
	case evEnded:
		k = media.Ended
	case evDurationChange:
		k = media.DurationChange
	case evFirstFrame:
		k = media.FirstFrame
	case evError:
		k = media.Error
		code := int(ev.p1)
		if code < media.ErrAborted || code > media.ErrSrcNotSupported {
			code = media.ErrDecode
		}
		p.Update(func(st *media.State) { st.Err = code })
		log.Printf("media: %s: media_error_code %d (%s)", p.src.FileID, code, hrString(int32(ev.p2)))
	default:
		return
	}
	p.Emit(k)
}

// fail ends the playback with a MediaError code, as the element's error
// event; the poster stays (Slack's error look).
func (p *player) fail(code int, why string) {
	if p.failed || p.closing() {
		return // a read Close aborted isn't the file's error
	}
	p.failed = true
	log.Printf("media: %s: media_error_code %d: %s", p.src.FileID, code, why)
	p.Update(func(st *media.State) { st.Err = code })
	p.Emit(media.Error)
}

// shutdown lets go of everything, on the media thread. The ask sees the
// player closed once p.c is nil (under askMu); the engine's Shutdown and
// the releases come after, without the lock Gio's render thread takes.
func (p *player) shutdown() {
	p.cancel()
	p.rs.Close()
	p.askMu.Lock()
	c := p.c
	p.c = nil
	p.askMu.Unlock()
	if c != nil {
		if closeHook != nil {
			closeHook(p)
		}
		C.clack_me_close(c)
		C.clack_me_free(c)
	}
	unregister(p.id)
	p.CloseEvents()
}

// readFull reads len(b) bytes at off, fewer at the end of the file.
func (p *player) readFull(b []byte, off int64) (int, error) {
	n := 0
	for n < len(b) {
		m, err := p.rs.ReadSome(p.ctx, b[n:], off+int64(n))
		n += m
		if err != nil {
			if errors.Is(err, io.EOF) {
				return n, nil
			}
			return n, err
		}
	}
	return n, nil
}

//export clackMFEvent
func clackMFEvent(h C.uintptr_t, gen C.uint32_t, ev C.uint, p1 C.uintptr_t, p2 C.uint) {
	if p := lookup(h); p != nil {
		select {
		case p.mfEv <- mfEvent{gen: uint32(gen), code: uint32(ev), p1: uint64(p1), p2: uint32(p2)}:
		default:
			p.NDropped.Add(1)
		}
	}
}

//export clackMFRead
func clackMFRead(h C.uintptr_t, req unsafe.Pointer, pb unsafe.Pointer, cb C.ulong, off C.longlong) {
	p := lookup(h)
	go func() {
		if p == nil {
			C.clack_me_read_done(req, 0, eAbort)
			return
		}
		asked := time.Now()
		n, err := p.readFull(unsafe.Slice((*byte)(pb), int(cb)), int64(off))
		if testRead != nil {
			testRead(p.src.FileID, int64(off), n, asked)
		}
		hr := C.int32_t(0)
		if err != nil && n == 0 {
			hr = eFail
			if errors.Is(err, media.ErrClosed) || errors.Is(err, context.Canceled) {
				hr = eAbort
			}
		}
		C.clack_me_read_done(req, C.int32_t(n), hr)
	}()
}

//export clackMFReadSync
func clackMFReadSync(h C.uintptr_t, pb unsafe.Pointer, cb C.ulong, off C.longlong) C.longlong {
	p := lookup(h)
	if p == nil {
		return -1
	}
	asked := time.Now()
	n, err := p.readFull(unsafe.Slice((*byte)(pb), int(cb)), int64(off))
	if testRead != nil {
		testRead(p.src.FileID, int64(off), n, asked)
	}
	if err != nil && n == 0 {
		return -1
	}
	return C.longlong(n)
}

// audioWait is how long a player waits for the check of the default
// audio output before it makes its engine muted.
const audioWait = 2 * time.Second

// audioCall is a check of the default audio output; players made while
// one runs share its answer.
type audioCall struct {
	done chan struct{}
	err  error // set before done closes
}

var (
	audioMu  sync.Mutex
	audioCur *audioCall
)

// checkAudio is the check in flight, or a new one: a device may have come
// or gone since the last.
func checkAudio() *audioCall {
	audioMu.Lock()
	defer audioMu.Unlock()
	if c := audioCur; c != nil {
		select {
		case <-c.done:
		default:
			return c
		}
	}
	c := &audioCall{done: make(chan struct{})}
	audioCur = c
	go func() {
		c.err = audioEndpoint()
		close(c.done)
	}()
	return c
}

// audioEndpoint is whether Windows has a default audio output, asked on a
// thread of its own in COM's multithreaded apartment.
func audioEndpoint() error {
	ch := make(chan int32)
	go func() {
		runtime.LockOSThread() // the thread goes with the goroutine
		C.clack_me_thread_init()
		hr := C.clack_me_audio_endpoint()
		C.clack_me_thread_done()
		ch <- int32(hr)
	}()
	if hr := <-ch; hr != 0 {
		return errors.New(hrString(hr))
	}
	return nil
}

// live is the C side's live objects by kind.
func live() [liveN]int32 {
	var out [liveN]C.int32_t
	C.clack_me_live(&out[0])
	var r [liveN]int32
	for i := range out {
		r[i] = int32(out[i])
	}
	return r
}

func hrString(hr int32) string {
	switch uint32(hr) {
	case 0x80070490:
		return "0x80070490 (E_NOTFOUND)"
	case 0x8007007e:
		return "0x8007007e (module not found)"
	case 0xc00d36c4:
		return "0xc00d36c4 (MF_E_UNSUPPORTED_BYTESTREAM_TYPE)"
	case 0xc00d36b4:
		return "0xc00d36b4 (MF_E_INVALIDMEDIATYPE)"
	case 0x80070057:
		return "0x80070057 (E_INVALIDARG)"
	case 0x80004001:
		return "0x80004001 (E_NOTIMPL)"
	case 0x887a0005:
		return "0x887a0005 (DXGI_ERROR_DEVICE_REMOVED)"
	case 0x887a0004:
		return "0x887a0004 (DXGI_ERROR_UNSUPPORTED)"
	}
	return fmt.Sprintf("%#x", uint32(hr))
}

func featureLevel(l uint32) string {
	return fmt.Sprintf("%d_%d", l>>12, (l>>8)&0xf)
}

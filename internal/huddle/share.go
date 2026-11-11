package huddle

// Sharing your own screen: Slack's content share, ported from build
// 4d0c78085232 (2026-11-10):
//
//   - The picker lists sources from the desktop bridge's
//     calls.getScreenPreviewThumbnails({width: 346, height: 260}, [type])
//     (client-boot 2kEl Yx, mTT/ V): Electron DesktopCapturerSources with
//     ids "screen:<id>:<n>" / "window:<id>:<n>", screens sorted first, the
//     last id part forced to 0, and Stage Manager's pseudo-windows ("App
//     Icon Window", "Gesture Blocking Window", "Gesture Blocking Overlay")
//     dropped (aOZQ).
//   - toggleScreenShare (boot-shared ah2Y) starts the capture through the
//     client store's startScreenShare: ContentShareManager.setScreenshare
//     (client-boot d9DD) captures the source at 15 fps, at most 2560 px on
//     a side, with the content hint "text", and system audio when "Share
//     system audio" is ticked; then the Chime SDK's startContentShare.
//   - The Chime SDK (DefaultContentShareController.
//     createContentShareMeetingSessionConfigure) joins a second attendee
//     on the same meeting: attendee id and join token with "#content"
//     appended, no video subscriptions (NoVideoDownlinkBandwidthPolicy).
//     It publishes the screen and an audio stream (the system audio, or
//     silence) like any attendee; everyone's INDEX lists the video under
//     "<attendee>#content", which is how receivers tell a share from a
//     camera (session.go's onIndex).
//   - The main session tells the others what is shared (M3Ec's
//     startScreenShare): a ContentShareDetails peer message with the
//     source type ("desktop:screen", "desktop:window"; TOUi k0) and its
//     selfDrawEnabled capability (Capabilities.isDrawEnabled).
//   - Errors map to the huddle's alerts: NotAllowedError is the
//     screen-recording permission (noSystemScreenShare), NotReadableError
//     is ScreenShareNotReadableError, NotFoundError SSNotFoundError, the
//     rest screenShareError (d9DD).

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ShareKind is the screen picker's source type (D7xu's tabs: "Share
// entire screen" and "Window").
type ShareKind int

const (
	ShareScreen ShareKind = iota // a whole display: "screen:<id>:0"
	ShareWindow                  // one window: "window:<id>:0"
)

func (k ShareKind) String() string {
	if k == ShareWindow {
		return "window"
	}
	return "screen"
}

// ShareSource is one tile of Slack's screen picker: a desktop capturer
// source.
type ShareSource struct {
	ID        string      // "screen:<id>:0" or "window:<id>:0"
	Name      string      // "Entire Screen", "Screen 2", a window's title
	Thumb     *image.RGBA // the tile's picture, at most ShareThumbSize (nil: none)
	AppName   string      // the window's app (huddles_screen_picker_app_groups)
	DisplayID string      // the display a screen source is
	// Bounds is the display's or window's frame in screen coordinates
	// (points): HuddleMiniPanelMounter centres the presenter's panel at its
	// bottom, the border goes around it.
	Bounds image.Rectangle
}

// Kind is the source's type, from its id.
func (s ShareSource) Kind() ShareKind {
	if strings.HasPrefix(s.ID, "window:") {
		return ShareWindow
	}
	return ShareScreen
}

// nativeID is the id's middle part: the backend's display or window id.
func (s ShareSource) nativeID() uint64 {
	p := strings.Split(s.ID, ":")
	if len(p) < 2 {
		return 0
	}
	n, _ := strconv.ParseUint(p[1], 10, 64)
	return n
}

// ContentSourceType is TOUi's determineFullContentShareSourceType for a
// desktop app: what ContentShareDetails.sourceType says.
func (s ShareSource) ContentSourceType() string {
	switch {
	case strings.HasPrefix(s.ID, "screen:"):
		return "desktop:screen"
	case strings.HasPrefix(s.ID, "window:"):
		return "desktop:window"
	}
	return "desktop:unknown"
}

// shareSourceID builds an id the way mTT/ normalizes Electron's: the
// last part is always 0.
func shareSourceID(k ShareKind, native uint64) string {
	return k.String() + ":" + strconv.FormatUint(native, 10) + ":0"
}

// ShareThumbSize is the box the picker's thumbnails fit in
// (getScreenPreviewThumbnails({width: 346, height: 260})).
var ShareThumbSize = image.Pt(346, 260)

// stageManagerNames are aOZQ's filterStageManagerSources.
var stageManagerNames = map[string]bool{"App Icon Window": true, "Gesture Blocking Window": true, "Gesture Blocking Overlay": true}

// ScreenCapture is a running capture of one source. Start returns once
// capture runs (or failed); frames then arrive on one goroutine the
// capture owns, in NV12, at most 2560 px on a side, about 15 a second.
// A frame is valid only until onFrame returns (backends reuse buffers).
// onAudio, when not nil and the capture shares system audio, gets 48 kHz
// stereo interleaved PCM in any chunk size; onEnd runs once if the source
// goes away (a window closed, a display unplugged): Slack's track "ended"
// listener, which stops the share.
type ScreenCapture interface {
	Start(onFrame func(*CameraFrame), onAudio func([]int16), onEnd func(error)) error
	Close()
}

// shareBackend is this build's screen capture (share_darwin.go,
// share_windows.go, share_linux.go); nil where there is none.
type shareBackend interface {
	// List is getScreenPreviewThumbnails for one kind, thumbnails fitted
	// in thumb.
	List(kind ShareKind, thumb image.Point) ([]ShareSource, error)
	// Open prepares a capture of src (with system audio when audio is
	// set and SystemAudio() says the OS can).
	Open(src ShareSource, audio bool, logf func(string, ...any)) (ScreenCapture, error)
	// SystemAudio reports whether "Share system audio" can work here.
	SystemAudio() bool
}

var shareOS shareBackend

// ShareSupported reports whether this build can share a screen: an OS
// backend, or CLACK_HUDDLE_TESTSCREEN.
func ShareSupported() bool { return shareBackendFor() != nil }

// ShareSystemAudio reports whether the capture can share system audio
// (the picker's "Share system audio" checkbox).
func ShareSystemAudio() bool {
	b := shareBackendFor()
	return b != nil && b.SystemAudio()
}

func shareBackendFor() shareBackend {
	if testScreenEnv() {
		return testShareBackend{}
	}
	return shareOS
}

// ErrShareUnsupported: this build can't capture the screen.
var ErrShareUnsupported = errors.New("screen sharing isn't supported in this build")

// ListShareSources lists the screens or the windows Slack's picker shows,
// with thumbnails: getScreenPreviewThumbnails sorted screens first, ids
// normalized and Stage Manager's pseudo-windows filtered out. It can take
// a while (thumbnails): call it off the UI goroutine.
func ListShareSources(kind ShareKind) ([]ShareSource, error) {
	b := shareBackendFor()
	if b == nil {
		return nil, ErrShareUnsupported
	}
	l, err := b.List(kind, ShareThumbSize)
	if err != nil {
		return nil, err
	}
	out := l[:0]
	for _, s := range l {
		if s.Kind() != kind || stageManagerNames[s.Name] {
			continue
		}
		if p := strings.Split(s.ID, ":"); len(p) == 3 && p[2] != "0" {
			s.ID = p[0] + ":" + p[1] + ":0"
		}
		out = append(out, s)
	}
	return out, nil
}

// screenNames names displays the way Electron's desktopCapturer does:
// "Entire Screen" for one, "Screen 1", "Screen 2"… for several.
func screenNames(n int) []string {
	if n == 1 {
		return []string{"Entire Screen"}
	}
	out := make([]string, n)
	for i := range out {
		out[i] = "Screen " + strconv.Itoa(i+1)
	}
	return out
}

// ---- the session side ----

// ShareState is where your screen share is.
type ShareState int

const (
	ShareOff         ShareState = iota
	ShareStarting               // capturing and joining the content attendee
	ShareOn                     // publishing
	ShareDenied                 // NotAllowedError: the OS didn't allow screen capture
	ShareNotReadable            // NotReadableError: another app holds the capture
	ShareNotFound               // NotFoundError: the source is gone
	ShareFailed                 // anything else (screenShareError); Err says what
	ShareUnsupported            // no capture backend in this build
)

func (s ShareState) String() string {
	return [...]string{"off", "starting", "on", "denied", "not readable", "not found", "failed", "unsupported"}[s]
}

// Sharing reports whether the share is on or starting: Slack's
// isScreenshareEnabled, which toggleScreenShare sets as it starts
// (markShareStarted).
func (s ShareState) Sharing() bool { return s == ShareStarting || s == ShareOn }

// ShareStatus is your screen share's state.
type ShareStatus struct {
	State  ShareState
	Err    string
	Source ShareSource // what you share (no thumbnail)
	Audio  bool        // with system audio
}

// SelfShareKey is Session.Frame's key for your own share as the others
// see it (the content share tile the SDK binds to your stream).
const SelfShareKey = "self#content"

// Errors a capture backend returns, as Chromium's getUserMedia names them.
var (
	ErrShareDenied      = errors.New("NotAllowedError: screen capture isn't allowed")
	ErrShareNotReadable = errors.New("NotReadableError: the screen can't be read")
	ErrShareNotFound    = errors.New("NotFoundError: the source is gone")
)

// The content stream as the Chime SDK describes a local video without
// simulcast (NScaleVideoUplinkBandwidthPolicy: 15 fps, 1500 kbps for up
// to two publishers), and ContentShareManager's capture limits.
const (
	shareFPS        = 15
	shareMaxKbps    = 1500
	shareBitrate    = 1_200_000
	shareMinBitrate = 200_000
	shareMaxSide    = 2560
	shareAudioBps   = 96_000
)

// sharePipe is the screen half of a Session.
type sharePipe struct {
	opMu sync.Mutex // serializes starting and stopping

	mu      sync.Mutex
	want    bool
	gen     uint64
	status  ShareStatus
	src     ShareSource
	audio   bool
	cap     ScreenCapture
	conn    *contentConn
	present atomic.Bool // Chime lists <you>#content (contentShareDidStart)

	preview selfPreview
}

// StartShare shares src (Slack's toggleScreenShare with a mediaSourceId):
// it returns at once and runs the capture and the content attendee on a
// goroutine; watch State().Share. audio asks for system audio (the
// picker's "Share system audio").
func (s *Session) StartShare(src ShareSource, audio bool) {
	p := &s.share
	src.Thumb = nil
	p.mu.Lock()
	p.want = true
	p.gen++
	gen := p.gen
	p.src, p.audio = src, audio
	p.status = ShareStatus{State: ShareStarting, Source: src, Audio: audio}
	p.mu.Unlock()
	s.changed()
	go s.applyShare(gen)
}

// StopShare stops your screen share (toggleScreenShare while sharing).
func (s *Session) StopShare() {
	p := &s.share
	p.mu.Lock()
	p.want = false
	p.gen++
	gen := p.gen
	p.status = ShareStatus{}
	p.mu.Unlock()
	s.changed()
	go s.applyShare(gen)
}

// ShareStatus is your screen share's state (also State().Share).
func (s *Session) ShareStatus() ShareStatus {
	s.share.mu.Lock()
	defer s.share.mu.Unlock()
	return s.share.status
}

func (s *Session) setShareStatus(gen uint64, st ShareStatus) {
	p := &s.share
	p.mu.Lock()
	if gen == p.gen {
		p.status = st
	}
	p.mu.Unlock()
	s.changed()
}

func (s *Session) applyShare(gen uint64) {
	p := &s.share
	p.opMu.Lock()
	defer p.opMu.Unlock()
	p.mu.Lock()
	cur, want, src, audio := p.gen, p.want, p.src, p.audio
	p.mu.Unlock()
	if gen != cur || s.ctx.Err() != nil {
		return
	}
	s.shareOff()
	if !want {
		return
	}
	if err := s.shareOn(gen, src, audio); err != nil {
		st := ShareStatus{State: ShareFailed, Err: err.Error(), Source: src}
		switch {
		case errors.Is(err, ErrShareUnsupported):
			st.State = ShareUnsupported
		case errors.Is(err, ErrShareDenied):
			st.State = ShareDenied
		case errors.Is(err, ErrShareNotReadable):
			st.State = ShareNotReadable
		case errors.Is(err, ErrShareNotFound):
			st.State = ShareNotFound
		}
		s.logf("huddle: screen share: %v", err)
		s.shareOff()
		p.mu.Lock()
		if gen == p.gen {
			p.want = false
		}
		p.mu.Unlock()
		s.setShareStatus(gen, st)
	}
}

// openShare opens the capture: Options.OpenShare, the test screen
// (CLACK_HUDDLE_TESTSCREEN), or the OS backend.
func (s *Session) openShare(src ShareSource, audio bool) (ScreenCapture, error) {
	if s.o.OpenShare != nil {
		return s.o.OpenShare(src, audio, s.logf)
	}
	b := shareBackendFor()
	if b == nil {
		return nil, ErrShareUnsupported
	}
	return b.Open(src, audio && b.SystemAudio(), s.logf)
}

func (s *Session) shareOn(gen uint64, src ShareSource, audio bool) error {
	p := &s.share
	capt, err := s.openShare(src, audio)
	if err != nil {
		return err
	}
	cc := newContentConn(s, audio)
	p.mu.Lock()
	stale := gen != p.gen
	if !stale {
		p.cap, p.conn = capt, cc
	}
	p.mu.Unlock()
	if stale {
		capt.Close()
		return nil
	}
	p.preview.start()
	ended := func(err error) {
		s.logf("huddle: screen share source ended: %v", err)
		go func() {
			p.mu.Lock()
			still := p.gen == gen && p.want
			p.mu.Unlock()
			if still {
				s.StopShare()
			}
		}()
	}
	var onAudio func([]int16)
	if audio {
		onAudio = cc.systemAudio
	}
	if err := capt.Start(func(f *CameraFrame) { s.onShareFrame(cc, f) }, onAudio, ended); err != nil {
		return err
	}
	if err := cc.start(); err != nil {
		return fmt.Errorf("content attendee: %w", err)
	}
	// M3Ec's startScreenShare: what you share and whether others may draw
	// on it (selfDrawEnabled(isDrawSupported() && …)). Clack can't show a
	// presenter the drawings (no host draw window), so it says no, as
	// Slack's own Linux app does.
	_ = s.SendPeer(TopicData, &PeerMessage{Capabilities: &Capabilities{IsDrawEnabled: false}})
	_ = s.SendPeer(TopicData, &PeerMessage{ContentShareDetails: &ContentShareDetails{SourceType: src.ContentSourceType(), ItemID: s.join.Attendee.AttendeeID}})
	s.setShareStatus(gen, ShareStatus{State: ShareOn, Source: src, Audio: audio})
	s.logf("huddle: sharing %s (%s)", src.ID, src.Name)
	return nil
}

// shareOff stops the capture and leaves the content attendee.
func (s *Session) shareOff() {
	p := &s.share
	p.mu.Lock()
	capt, cc := p.cap, p.conn
	p.cap, p.conn = nil, nil
	p.mu.Unlock()
	if capt != nil {
		capt.Close()
	}
	if cc != nil {
		cc.stop()
		s.logf("huddle: screen share stopped")
	}
	p.preview.clear()
	s.changed()
}

// closeShare is finish's: everything off, nothing renegotiated.
func (s *Session) closeShare() {
	p := &s.share
	p.mu.Lock()
	p.want = false
	p.gen++
	if p.status.State != ShareOff {
		p.status = ShareStatus{}
	}
	p.mu.Unlock()
	s.shareOff()
}

// onShareFrame runs on the capture's goroutine: the presenter's preview,
// then encode and send on the content attendee.
func (s *Session) onShareFrame(cc *contentConn, f *CameraFrame) {
	if f == nil || f.W < 16 || f.H < 16 {
		return
	}
	s.share.preview.offer(f, s.changed)
	cc.sendFrame(f)
}

// ---- the test screen ----

// testScreenEnv: CLACK_HUDDLE_TESTSCREEN=1 (or WxH) replaces the OS
// capture with a synthetic screen and two windows, for the fake Chime,
// tests and the demo. Nothing is captured.
func testScreenEnv() bool {
	v := os.Getenv("CLACK_HUDDLE_TESTSCREEN")
	return v != "" && v != "0"
}

func testScreenSize() (int, int) {
	if a, b, ok := strings.Cut(os.Getenv("CLACK_HUDDLE_TESTSCREEN"), "x"); ok {
		w, _ := strconv.Atoi(a)
		h, _ := strconv.Atoi(b)
		if w >= 16 && h >= 16 && w <= 3840 && h <= 2160 {
			return w &^ 1, h &^ 1
		}
	}
	return 640, 360
}

type testShareBackend struct{}

// testWindows are the synthetic windows (id, title, app, size).
var testWindows = []struct {
	id         uint64
	name, app  string
	w, h       int
	background color.RGBA
}{
	{101, "Release notes", "TextEdit", 800, 600, color.RGBA{0xf8, 0xf8, 0xf8, 0xff}},
	{102, "zsh", "Terminal", 720, 450, color.RGBA{0x1d, 0x1c, 0x1d, 0xff}},
}

func (testShareBackend) List(kind ShareKind, thumb image.Point) ([]ShareSource, error) {
	if kind == ShareScreen {
		w, h := testScreenSize()
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		paintTestScreen(img, 0)
		return []ShareSource{{ID: shareSourceID(ShareScreen, 1), Name: screenNames(1)[0], DisplayID: "1",
			Bounds: image.Rect(0, 0, w, h), Thumb: thumbOf(img, thumb)}}, nil
	}
	var out []ShareSource
	for i, tw := range testWindows {
		img := image.NewRGBA(image.Rect(0, 0, tw.w, tw.h))
		paintTestWindow(img, tw.background, i)
		out = append(out, ShareSource{ID: shareSourceID(ShareWindow, tw.id), Name: tw.name, AppName: tw.app,
			Bounds: image.Rect(40+60*i, 40+40*i, 40+60*i+tw.w, 40+40*i+tw.h), Thumb: thumbOf(img, thumb)})
	}
	return out, nil
}

func (testShareBackend) SystemAudio() bool { return true }

func (testShareBackend) Open(src ShareSource, audio bool, logf func(string, ...any)) (ScreenCapture, error) {
	w, h := testScreenSize()
	if src.Kind() == ShareWindow {
		for _, tw := range testWindows {
			if shareSourceID(ShareWindow, tw.id) == src.ID {
				w, h = tw.w, tw.h
			}
		}
	}
	return NewTestScreen(w, h, shareFPS, audio), nil
}

// NewTestScreen is a ScreenCapture of a synthetic w×h screen at fps: rows
// of "text" with a block that moves, and (audio) a quiet 330 Hz tone as
// the system audio. It needs no permission and captures nothing.
func NewTestScreen(w, h, fps int, audio bool) ScreenCapture {
	if fps <= 0 {
		fps = shareFPS
	}
	return &testScreen{w: max(16, w&^1), h: max(16, h&^1), fps: fps, audio: audio, stop: make(chan struct{})}
}

type testScreen struct {
	w, h, fps int
	audio     bool
	stop      chan struct{}
	once      sync.Once
}

func (c *testScreen) Start(onFrame func(*CameraFrame), onAudio func([]int16), onEnd func(error)) error {
	go func() {
		img := image.NewRGBA(image.Rect(0, 0, c.w, c.h))
		f := &CameraFrame{}
		t := time.NewTicker(time.Second / time.Duration(c.fps))
		defer t.Stop()
		for n := 0; ; n++ {
			select {
			case <-c.stop:
				return
			case <-t.C:
			}
			paintTestScreen(img, n)
			rgbaToNV12(f, img)
			f.At = time.Now()
			onFrame(f)
		}
	}()
	if c.audio && onAudio != nil {
		go func() {
			t := time.NewTicker(20 * time.Millisecond)
			defer t.Stop()
			buf := make([]int16, frameSamples*2)
			ph := 0.0
			for {
				select {
				case <-c.stop:
					return
				case <-t.C:
				}
				for i := 0; i < frameSamples; i++ {
					v := int16(2000 * math.Sin(ph))
					ph += 2 * 3.14159265358979 * 330 / sampleRate
					buf[2*i], buf[2*i+1] = v, v
				}
				onAudio(buf)
			}
		}()
	}
	return nil
}

func (c *testScreen) Close() { c.once.Do(func() { close(c.stop) }) }

// paintTestScreen draws frame n of the synthetic screen: a light desktop
// with a dark menu bar, rows of grey "text" and a blue block that walks
// across.
func paintTestScreen(img *image.RGBA, n int) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	fillRect(img, image.Rect(0, 0, w, h), color.RGBA{0xf4, 0xed, 0xe4, 0xff})
	bar := max(4, h/24)
	fillRect(img, image.Rect(0, 0, w, bar), color.RGBA{0x1d, 0x1c, 0x1d, 0xff})
	line := max(4, h/18)
	for y, i := bar*2, 0; y+line/2 < h; y, i = y+line, i+1 {
		lw := w/2 + (i*37)%(w/3+1)
		fillRect(img, image.Rect(w/16, y, min(w-w/16, w/16+lw), y+line/2), color.RGBA{0x61, 0x60, 0x61, 0xff})
	}
	bs := max(8, h/8)
	bx := (n * 8) % max(1, w-bs)
	fillRect(img, image.Rect(bx, h-bs-bar, bx+bs, h-bar), color.RGBA{0x12, 0x64, 0xa3, 0xff})
}

// paintTestWindow draws a synthetic window: a title bar and some lines.
func paintTestWindow(img *image.RGBA, bg color.RGBA, i int) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	fillRect(img, image.Rect(0, 0, w, h), bg)
	fillRect(img, image.Rect(0, 0, w, h/14), color.RGBA{0xe8, 0xe8, 0xe8, 0xff})
	ink := color.RGBA{0x1d, 0x1c, 0x1d, 0xff}
	if bg.R < 0x80 {
		ink = color.RGBA{0x2b, 0xac, 0x76, 0xff}
	}
	for y := h / 7; y+h/40 < h; y += h / 14 {
		fillRect(img, image.Rect(w/12, y, w/12+(w/2+(y*7+i*90)%(w/3)), y+h/40), ink)
	}
}

func fillRect(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	r = r.Intersect(img.Rect)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		row := img.Pix[y*img.Stride+r.Min.X*4 : y*img.Stride+r.Max.X*4]
		for i := 0; i < len(row); i += 4 {
			row[i], row[i+1], row[i+2], row[i+3] = c.R, c.G, c.B, c.A
		}
	}
}

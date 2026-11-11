//go:build darwin && cgo

package huddle

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -Wno-unguarded-availability-new
#cgo LDFLAGS: -framework AppKit -framework CoreGraphics -framework CoreMedia -framework CoreVideo -framework Foundation
#include <stdint.h>
#include <stdlib.h>
typedef struct {
	int kind;
	uint64_t id;
	char name[256];
	char app[128];
	int x, y, w, h;
	int tw, th;
	uint8_t *thumb;
} clackSCSource;
int clack_sc_supported(void);
int clack_sc_access(void);
void clack_sc_request(void);
int clack_sc_list(int kind, int tw, int th, clackSCSource **out, int *n, char **err);
void clack_sc_free_list(clackSCSource *l, int n);
void *clack_sc_start(int kind, uint64_t id, int w, int h, int audio, uintptr_t handle, char **err, int *denied, int *gone);
void clack_sc_refilter(void *p);
void clack_sc_stop(void *p);
void clack_sc_exclude(uint32_t wid, int on);
void clack_sc_display_pixels(uint32_t id, int *w, int *h);
double clack_sc_backing_scale(int x, int y);
void clack_border_show(int kind, uint64_t id, double width, double r, double g, double b, double a);
void clack_border_hide(void);
void clack_panel_setup(uintptr_t view, int x, int y, int w, int h);
*/
import "C"

import (
	"errors"
	"fmt"
	"image"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// macOS screen capture: ScreenCaptureKit (share_darwin.m), loaded at run
// time (macOS 12.3+). Thumbnails need macOS 14 (SCScreenshotManager);
// before that the picker's tiles have titles only. System audio needs
// macOS 13 and leaves Clack's own sound out (excludesCurrentProcessAudio).
func init() { shareOS = scShares{} }

// borderSupported: Slack draws the border on macOS and Windows
// (BIqb: !isLinux() && isDesktopApp()).
const borderSupported = true

// macOSAtLeast reports whether this is macOS major or later.
func macOSAtLeast(major int) bool {
	v, err := syscall.Sysctl("kern.osproductversion")
	if err != nil {
		return false
	}
	maj, _, _ := strings.Cut(v, ".")
	n, _ := strconv.Atoi(maj)
	return n >= major
}

type scShares struct{}

func (scShares) supported() bool { return C.clack_sc_supported() != 0 }

func (scShares) SystemAudio() bool { return C.clack_sc_supported() != 0 && macOSAtLeast(13) }

func (scShares) List(kind ShareKind, thumb image.Point) ([]ShareSource, error) {
	var l *C.clackSCSource
	var n C.int
	var cerr *C.char
	k := C.int(0)
	if kind == ShareWindow {
		k = 1
	}
	st := C.clack_sc_list(k, C.int(thumb.X), C.int(thumb.Y), &l, &n, &cerr)
	if cerr != nil {
		defer C.free(unsafe.Pointer(cerr))
	}
	switch st {
	case 0:
	case 1:
		return nil, ErrShareDenied
	case 2:
		return nil, ErrShareUnsupported
	default:
		return nil, fmt.Errorf("screencapturekit: %s", C.GoString(cerr))
	}
	defer C.clack_sc_free_list(l, n)
	items := unsafe.Slice(l, int(n))
	var out []ShareSource
	names := screenNames(max(1, int(n)))
	for i, it := range items {
		s := ShareSource{Bounds: image.Rect(int(it.x), int(it.y), int(it.x+it.w), int(it.y+it.h))}
		if it.kind == 0 {
			s.ID, s.Name, s.DisplayID = shareSourceID(ShareScreen, uint64(it.id)), names[i], strconv.FormatUint(uint64(it.id), 10)
		} else {
			s.ID, s.Name, s.AppName = shareSourceID(ShareWindow, uint64(it.id)), C.GoString(&it.name[0]), C.GoString(&it.app[0])
		}
		if it.thumb != nil && it.tw > 0 && it.th > 0 {
			img := image.NewRGBA(image.Rect(0, 0, int(it.tw), int(it.th)))
			copy(img.Pix, unsafe.Slice((*byte)(unsafe.Pointer(it.thumb)), len(img.Pix)))
			s.Thumb = img
		}
		out = append(out, s)
	}
	return out, nil
}

func (scShares) Open(src ShareSource, audio bool, logf func(string, ...any)) (ScreenCapture, error) {
	if C.clack_sc_supported() == 0 {
		return nil, ErrShareUnsupported
	}
	c := &scCapture{src: src, audio: audio, logf: logf, frames: make(chan *CameraFrame, 1), free: make(chan *CameraFrame, 2),
		stop: make(chan struct{})}
	c.free <- &CameraFrame{}
	c.free <- &CameraFrame{}
	// the capture size: the display's pixels, or the window's points at
	// its screen's scale, at most 2560 on a side (ContentShareManager)
	w, h := src.Bounds.Dx(), src.Bounds.Dy()
	if src.Kind() == ShareScreen {
		var pw, ph C.int
		C.clack_sc_display_pixels(C.uint32_t(src.nativeID()), &pw, &ph)
		if pw > 0 && ph > 0 {
			w, h = int(pw), int(ph)
		}
	} else {
		s := float64(C.clack_sc_backing_scale(C.int(src.Bounds.Min.X+w/2), C.int(src.Bounds.Min.Y+h/2)))
		w, h = int(float64(w)*s), int(float64(h)*s)
	}
	if w < 16 || h < 16 {
		return nil, ErrShareNotFound
	}
	c.w, c.h = capSize(w, h)
	return c, nil
}

// scCaptures maps a capture's id to it for the C callbacks (an id, not a
// cgo.Handle: a callback still queued after Close finds nothing).
var (
	scCaptures sync.Map // uintptr -> *scCapture
	scNext     atomic.Uintptr
)

type scCapture struct {
	src     ShareSource
	audio   bool
	logf    func(string, ...any)
	w, h    int
	id      uintptr
	mu      sync.Mutex
	p       unsafe.Pointer
	frames  chan *CameraFrame // filled frames, to the goroutine
	free    chan *CameraFrame // empty ones, back to the SCK queue
	onAudio func([]int16)
	onEnd   func(error)
	stop    chan struct{}
	once    sync.Once
	done    chan struct{}
}

func (c *scCapture) Start(onFrame func(*CameraFrame), onAudio func([]int16), onEnd func(error)) error {
	c.onAudio, c.onEnd = onAudio, onEnd
	c.id = scNext.Add(1)
	scCaptures.Store(c.id, c)
	kind := C.int(0)
	if c.src.Kind() == ShareWindow {
		kind = 1
	}
	audio := C.int(0)
	if c.audio && onAudio != nil {
		audio = 1
	}
	var cerr *C.char
	var denied, gone C.int
	p := C.clack_sc_start(kind, C.uint64_t(c.src.nativeID()), C.int(c.w), C.int(c.h), audio, C.uintptr_t(c.id), &cerr, &denied, &gone)
	if p == nil {
		scCaptures.Delete(c.id)
		msg := "can't start the capture"
		if cerr != nil {
			msg = C.GoString(cerr)
			C.free(unsafe.Pointer(cerr))
		}
		switch {
		case denied != 0:
			return fmt.Errorf("%w: %s", ErrShareDenied, msg)
		case gone != 0:
			return fmt.Errorf("%w: %s", ErrShareNotFound, msg)
		}
		return errors.New("screencapturekit: " + msg)
	}
	c.mu.Lock()
	c.p = p
	c.mu.Unlock()
	c.done = make(chan struct{})
	go func() {
		defer close(c.done)
		for {
			select {
			case <-c.stop:
				return
			case f := <-c.frames:
				onFrame(f)
				c.free <- f
			}
		}
	}()
	return nil
}

func (c *scCapture) Close() {
	c.once.Do(func() {
		scCaptures.Delete(c.id)
		c.mu.Lock()
		p := c.p
		c.p = nil
		c.mu.Unlock()
		if p != nil {
			C.clack_sc_stop(p)
		}
		close(c.stop)
		if c.done != nil {
			<-c.done
		}
	})
}

// refilter re-applies the excluded windows to a running display capture.
func (c *scCapture) refilter() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.p != nil {
		C.clack_sc_refilter(c.p)
	}
}

// clackSCFrame runs on the SCK queue with the pixel buffer locked: copy
// into a free frame and hand it over, or drop the picture when the
// encoder is behind.
//
//export clackSCFrame
func clackSCFrame(h C.uintptr_t, y unsafe.Pointer, ys C.int, uv unsafe.Pointer, uvs C.int, w, hh C.int) {
	v, ok := scCaptures.Load(uintptr(h))
	if !ok || y == nil || uv == nil || w < 16 || hh < 16 {
		return
	}
	c := v.(*scCapture)
	var f *CameraFrame
	select {
	case f = <-c.free:
	default:
		return
	}
	W, H := int(w)&^1, int(hh)&^1
	ensureNV12(f, W, H)
	src := unsafe.Slice((*byte)(y), int(ys)*H)
	for r := 0; r < H; r++ {
		copy(f.Y[r*W:(r+1)*W], src[r*int(ys):])
	}
	srcUV := unsafe.Slice((*byte)(uv), int(uvs)*(H/2))
	for r := 0; r < H/2; r++ {
		copy(f.UV[r*W:(r+1)*W], srcUV[r*int(uvs):])
	}
	f.At = time.Now()
	select {
	case c.frames <- f:
	default:
		c.free <- f
	}
}

//export clackSCAudio
func clackSCAudio(h C.uintptr_t, pcm *C.int16_t, frames C.int) {
	v, ok := scCaptures.Load(uintptr(h))
	if !ok || pcm == nil || frames <= 0 {
		return
	}
	c := v.(*scCapture)
	if c.onAudio != nil {
		c.onAudio(unsafe.Slice((*int16)(unsafe.Pointer(pcm)), int(frames)*2))
	}
}

//export clackSCStopped
func clackSCStopped(h C.uintptr_t, cerr *C.char) {
	msg := C.GoString(cerr)
	C.free(unsafe.Pointer(cerr))
	v, ok := scCaptures.Load(uintptr(h))
	if !ok {
		return
	}
	c := v.(*scCapture)
	if c.onEnd != nil {
		go c.onEnd(fmt.Errorf("%w: %s", ErrShareNotFound, msg))
	}
}

// screenAccess is the Screen Recording permission (CGPreflightScreenCaptureAccess).
func screenAccess() bool { return C.clack_sc_access() != 0 }

// requestScreenAccess shows the OS's prompt (CGRequestScreenCaptureAccess).
func requestScreenAccess() { C.clack_sc_request() }

// screenSettingsURL is Privacy & Security › Screen Recording.
const screenSettingsURL = "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture"

// showBorder draws Slack's border around src (BIqb's X: width 6, colour
// 0, 255/265, 0.2, 1) above everything, out of every capture.
func showBorder(src ShareSource) {
	k := C.int(0)
	if src.Kind() == ShareWindow {
		k = 1
	}
	C.clack_border_show(k, C.uint64_t(src.nativeID()), 6, 0, 255.0/265.0, 0.2, 1)
}

func hideBorder() { C.clack_border_hide() }

// setupPanel makes the window behind an AppKit view the presenter's mini
// panel at r (global points, top-left origin): above everything, out of
// captures, on every Space.
func setupPanel(h PanelHandle, r image.Rectangle) {
	if h.View == 0 {
		return
	}
	C.clack_panel_setup(C.uintptr_t(h.View), C.int(r.Min.X), C.int(r.Min.Y), C.int(r.Dx()), C.int(r.Dy()))
	scCaptures.Range(func(_, v any) bool {
		go v.(*scCapture).refilter()
		return true
	})
}

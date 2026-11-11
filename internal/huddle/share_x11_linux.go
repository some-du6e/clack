//go:build linux && cgo

package huddle

/*
#cgo LDFLAGS: -lX11 -ldl
#include <stdint.h>
#include <stdlib.h>
int clack_x11_open(char *err, int n);
int clack_x11_monitors(int *out, int max);
int clack_x11_windows(unsigned long *ids, int max);
int clack_x11_window_info(unsigned long w, char *name, int nn, char *cls, int cn, int *x, int *y, int *wd, int *ht);
int clack_x11_thumb(unsigned long win, int x, int y, int w, int h, uint8_t *rgba, int tw, int th);
void *clack_x11_cap_new(unsigned long win, int x, int y, int w, int h, int cursor);
int clack_x11_cap_grab(void *p, uint8_t *y, uint8_t *uv, int dw, int dh, int *nw, int *nh);
void clack_x11_cap_free(void *p);
*/
import "C"

import (
	"errors"
	"fmt"
	"image"
	"os"
	"strconv"
	"sync"
	"time"
	"unsafe"
)

// X11 screen capture (share_x11_linux.c): on an X11 session, the screen
// picker's screens are the RandR monitors and its windows the window
// manager's client list; capture is MIT-SHM at 15 fps with the cursor
// drawn in. Slack's Linux app has no "Share system audio" (0S5z shows
// it on macOS and Windows only), so neither does this backend. Wayland
// sessions share through the desktop portal instead (Slack skips its
// picker there: window.slackPlatform.isWayland).
func init() {
	if os.Getenv("DISPLAY") != "" && !waylandSession() {
		shareOS = &x11Shares{}
	}
}

// waylandSession: Clack's window is a Wayland one (Gio prefers Wayland),
// where X11 grabs see only XWayland clients.
func waylandSession() bool {
	return os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("XDG_SESSION_TYPE") == "wayland"
}

// x11Mu serializes this backend's Xlib connection.
var x11Mu sync.Mutex

type x11Shares struct {
	once sync.Once
	err  error
}

func (b *x11Shares) open() error {
	b.once.Do(func() {
		var buf [256]C.char
		if C.clack_x11_open(&buf[0], C.int(len(buf))) != 0 {
			b.err = errors.New(C.GoString(&buf[0]))
		}
	})
	return b.err
}

func (b *x11Shares) SystemAudio() bool { return false }

func (b *x11Shares) List(kind ShareKind, thumb image.Point) ([]ShareSource, error) {
	x11Mu.Lock()
	defer x11Mu.Unlock()
	if err := b.open(); err != nil {
		return nil, err
	}
	if kind == ShareScreen {
		var mon [4 * 16]C.int
		n := int(C.clack_x11_monitors(&mon[0], 16))
		names := screenNames(max(n, 1))
		var out []ShareSource
		for i := 0; i < n; i++ {
			r := image.Rect(int(mon[4*i]), int(mon[4*i+1]), int(mon[4*i]+mon[4*i+2]), int(mon[4*i+1]+mon[4*i+3]))
			s := ShareSource{ID: shareSourceID(ShareScreen, uint64(i)), Name: names[i], DisplayID: strconv.Itoa(i), Bounds: r}
			s.Thumb = x11Thumb(0, r, thumb)
			out = append(out, s)
		}
		return out, nil
	}
	var ids [256]C.ulong
	n := int(C.clack_x11_windows(&ids[0], C.int(len(ids))))
	var out []ShareSource
	for i := 0; i < n; i++ {
		var name [512]C.char
		var cls [128]C.char
		var x, y, w, h C.int
		if C.clack_x11_window_info(ids[i], &name[0], C.int(len(name)), &cls[0], C.int(len(cls)), &x, &y, &w, &h) != 0 {
			continue
		}
		s := ShareSource{ID: shareSourceID(ShareWindow, uint64(ids[i])), Name: C.GoString(&name[0]), AppName: C.GoString(&cls[0]),
			Bounds: image.Rect(int(x), int(y), int(x+w), int(y+h))}
		if s.Name == "" {
			s.Name = s.AppName
		}
		if s.Name == "" {
			continue
		}
		s.Thumb = x11Thumb(ids[i], image.Rect(0, 0, int(w), int(h)), thumb)
		out = append(out, s)
	}
	return out, nil
}

// x11Thumb grabs win (0: the root area r) into a thumbnail fitted in box.
func x11Thumb(win C.ulong, r image.Rectangle, box image.Point) *image.RGBA {
	tw, th := fitIn(r.Dx(), r.Dy(), box)
	if tw == 0 {
		return nil
	}
	img := image.NewRGBA(image.Rect(0, 0, tw, th))
	if C.clack_x11_thumb(win, C.int(r.Min.X), C.int(r.Min.Y), C.int(r.Dx()), C.int(r.Dy()),
		(*C.uint8_t)(unsafe.Pointer(&img.Pix[0])), C.int(tw), C.int(th)) != 0 {
		return nil
	}
	return img
}

func (b *x11Shares) Open(src ShareSource, audio bool, logf func(string, ...any)) (ScreenCapture, error) {
	x11Mu.Lock()
	defer x11Mu.Unlock()
	if err := b.open(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrShareNotReadable, err)
	}
	c := &x11Capture{stop: make(chan struct{}), logf: logf}
	r := src.Bounds
	if src.Kind() == ShareWindow {
		c.win = C.ulong(src.nativeID())
		var name [8]C.char
		var cls [8]C.char
		var x, y, w, h C.int
		if C.clack_x11_window_info(c.win, &name[0], C.int(len(name)), &cls[0], C.int(len(cls)), &x, &y, &w, &h) != 0 {
			return nil, ErrShareNotFound
		}
		r = image.Rect(0, 0, int(w), int(h))
	} else {
		var mon [4 * 16]C.int
		n := int(C.clack_x11_monitors(&mon[0], 16))
		i := int(src.nativeID())
		if i >= n {
			return nil, ErrShareNotFound
		}
		r = image.Rect(int(mon[4*i]), int(mon[4*i+1]), int(mon[4*i]+mon[4*i+2]), int(mon[4*i+1]+mon[4*i+3]))
	}
	if r.Dx() < 16 || r.Dy() < 16 {
		return nil, ErrShareNotFound
	}
	c.size = r.Size()
	c.p = C.clack_x11_cap_new(c.win, C.int(r.Min.X), C.int(r.Min.Y), C.int(r.Dx()), C.int(r.Dy()), 1)
	return c, nil
}

// x11Capture grabs one monitor area or window at shareFPS.
type x11Capture struct {
	p    unsafe.Pointer
	win  C.ulong
	size image.Point
	logf func(string, ...any)
	stop chan struct{}
	once sync.Once
	done chan struct{}
}

func (c *x11Capture) Start(onFrame func(*CameraFrame), _ func([]int16), onEnd func(error)) error {
	c.done = make(chan struct{})
	go func() {
		defer close(c.done)
		t := time.NewTicker(time.Second / shareFPS)
		defer t.Stop()
		f := &CameraFrame{}
		for {
			select {
			case <-c.stop:
				return
			case <-t.C:
			}
			dw, dh := capSize(c.size.X, c.size.Y)
			ensureNV12(f, dw, dh)
			var nw, nh C.int
			x11Mu.Lock()
			st := C.clack_x11_cap_grab(c.p, (*C.uint8_t)(unsafe.Pointer(&f.Y[0])), (*C.uint8_t)(unsafe.Pointer(&f.UV[0])),
				C.int(f.W), C.int(f.H), &nw, &nh)
			x11Mu.Unlock()
			switch {
			case st == 1: // the window was resized: the next grab is the new size
				c.size = image.Pt(int(nw), int(nh))
				continue
			case st == 3 || st == 9: // BadWindow, BadDrawable: it's gone
				if onEnd != nil {
					onEnd(ErrShareNotFound)
				}
				return
			case st != 0: // BadMatch while a window is off screen, or a transient failure
				continue
			}
			f.At = time.Now()
			onFrame(f)
		}
	}()
	return nil
}

func (c *x11Capture) Close() {
	c.once.Do(func() {
		close(c.stop)
		if c.done != nil {
			<-c.done
		}
		x11Mu.Lock()
		C.clack_x11_cap_free(c.p)
		x11Mu.Unlock()
	})
}

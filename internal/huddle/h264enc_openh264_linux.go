//go:build linux && cgo

package huddle

/*
#cgo LDFLAGS: -ldl
#include <stdint.h>
#include <stdlib.h>
int clack_oh_load(char *err, int n);
void clack_oh_version(int *v);
void *clack_oh_new(int w, int h, int fps, int bps, int screen, int *status);
int clack_oh_encode(void *p, const uint8_t *y, int ys, const uint8_t *uv, int uvs, long long ms, int key, uint8_t **out, int *n, int *idr);
void clack_oh_bitrate(void *p, int bps);
void clack_oh_free(void *p);
*/
import "C"

import (
	"errors"
	"fmt"
	"sync"
	"time"
	"unsafe"
)

func init() { newHWEncoders = append(newHWEncoders, newOpenH264) }

var (
	ohOnce sync.Once
	ohErr  error
)

// openH264Available loads libopenh264 once.
func openH264Available() error {
	ohOnce.Do(func() {
		var buf [256]C.char
		if C.clack_oh_load(&buf[0], C.int(len(buf))) != 0 {
			ohErr = errors.New(C.GoString(&buf[0]))
		}
	})
	return ohErr
}

// openH264Version is the loaded library's version ("" if none).
func openH264Version() string {
	if openH264Available() != nil {
		return ""
	}
	var v [3]C.int
	C.clack_oh_version(&v[0])
	return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2])
}

// ohEncoder is an OpenH264 encoder: constrained baseline, its screen
// content mode for a screen share, keyframes only when asked for.
type ohEncoder struct {
	mu    sync.Mutex
	p     unsafe.Pointer
	w, h  int
	start time.Time
}

func newOpenH264(o encOpts) (videoEncoder, error) {
	if err := openH264Available(); err != nil {
		return nil, err
	}
	screen := C.int(0)
	if o.Screen {
		screen = 1
	}
	var st C.int
	p := C.clack_oh_new(C.int(o.W), C.int(o.H), C.int(o.FPS), C.int(o.Bps), screen, &st)
	if p == nil {
		return nil, fmt.Errorf("openh264: Initialize: %d", int(st))
	}
	return &ohEncoder{p: p, w: o.W, h: o.H, start: time.Now()}, nil
}

func (e *ohEncoder) Name() string { return "OpenH264 " + openH264Version() }

func (e *ohEncoder) Encode(f *CameraFrame, key bool) ([]byte, error) {
	if f.W != e.w || f.H != e.h || len(f.Y) < f.YStride*(f.H-1)+f.W || len(f.UV) < f.UVStride*(f.H/2-1)+f.W {
		return nil, errors.New("openh264: frame doesn't match the encoder")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.p == nil {
		return nil, errors.New("openh264: closed")
	}
	k := C.int(0)
	if key {
		k = 1
	}
	var out *C.uint8_t
	var n, idr C.int
	ms := C.longlong(max(0, f.At.Sub(e.start).Milliseconds()))
	if st := C.clack_oh_encode(e.p, (*C.uint8_t)(unsafe.Pointer(&f.Y[0])), C.int(f.YStride), (*C.uint8_t)(unsafe.Pointer(&f.UV[0])),
		C.int(f.UVStride), ms, k, &out, &n, &idr); st != 0 {
		return nil, fmt.Errorf("openh264: EncodeFrame: %d", int(st))
	}
	if n == 0 {
		return nil, nil // skipped by rate control
	}
	return C.GoBytes(unsafe.Pointer(out), n), nil
}

func (e *ohEncoder) SetBitrate(bps int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.p != nil && bps > 0 {
		C.clack_oh_bitrate(e.p, C.int(bps))
	}
}

func (e *ohEncoder) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.p != nil {
		C.clack_oh_free(e.p)
		e.p = nil
	}
}

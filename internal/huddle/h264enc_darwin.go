//go:build darwin && cgo

package huddle

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework VideoToolbox -framework CoreMedia -framework CoreVideo -framework CoreFoundation -framework Foundation
#include <stdint.h>
void *clack_vt_new(int w, int h, int fps, int bps, uintptr_t handle, int *status);
int clack_vt_encode(void *p, const uint8_t *y, int ys, const uint8_t *uv, int uvs, int64_t ptsUs, int key);
void clack_vt_bitrate(void *p, int bps);
void clack_vt_free(void *p);
*/
import "C"

import (
	"errors"
	"fmt"
	"runtime/cgo"
	"sync"
	"time"
	"unsafe"
)

func init() { newHWEncoder = newVTEncoder }

// vtEncoder is a VideoToolbox VTCompressionSession: realtime, constrained
// baseline, no frame reordering, keyframes on demand.
type vtEncoder struct {
	mu    sync.Mutex
	p     unsafe.Pointer
	h     cgo.Handle
	w, hh int
	out   chan vtOut
	start time.Time
}

type vtOut struct {
	au  []byte
	key bool
}

func newVTEncoder(w, h, fps, bps int) (videoEncoder, error) {
	e := &vtEncoder{w: w, hh: h, out: make(chan vtOut, 4), start: time.Now()}
	e.h = cgo.NewHandle(e)
	var st C.int
	p := C.clack_vt_new(C.int(w), C.int(h), C.int(fps), C.int(bps), C.uintptr_t(e.h), &st)
	if p == nil {
		e.h.Delete()
		return nil, fmt.Errorf("VTCompressionSessionCreate: OSStatus %d", int(st))
	}
	e.p = p
	return e, nil
}

func (e *vtEncoder) Name() string { return "VideoToolbox" }

func (e *vtEncoder) Encode(f *CameraFrame, key bool) ([]byte, error) {
	if f.W != e.w || f.H != e.hh || len(f.Y) < f.YStride*(f.H-1)+f.W || len(f.UV) < f.UVStride*(f.H/2-1)+f.W {
		return nil, errors.New("videotoolbox: frame doesn't match the encoder")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.p == nil {
		return nil, errors.New("videotoolbox: closed")
	}
	for len(e.out) > 0 { // stale output from a failed encode
		<-e.out
	}
	k := C.int(0)
	if key {
		k = 1
	}
	pts := f.At.Sub(e.start).Microseconds()
	if st := C.clack_vt_encode(e.p, (*C.uint8_t)(unsafe.Pointer(&f.Y[0])), C.int(f.YStride),
		(*C.uint8_t)(unsafe.Pointer(&f.UV[0])), C.int(f.UVStride), C.int64_t(pts), k); st != 0 {
		return nil, fmt.Errorf("videotoolbox: encode: OSStatus %d", int(st))
	}
	select {
	case o := <-e.out:
		return o.au, nil
	default:
		return nil, nil // dropped by rate control
	}
}

func (e *vtEncoder) SetBitrate(bps int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.p != nil && bps > 0 {
		C.clack_vt_bitrate(e.p, C.int(bps))
	}
}

func (e *vtEncoder) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.p != nil {
		C.clack_vt_free(e.p)
		e.p = nil
		e.h.Delete()
	}
}

// clackVTOutput runs on a VideoToolbox thread: copy, offer, return.
//
//export clackVTOutput
func clackVTOutput(h C.uintptr_t, data unsafe.Pointer, n C.int, key C.int) {
	e, ok := cgo.Handle(h).Value().(*vtEncoder)
	if !ok || data == nil || n <= 0 {
		return
	}
	o := vtOut{au: C.GoBytes(data, n), key: key != 0}
	select {
	case e.out <- o:
	default:
	}
}

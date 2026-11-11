//go:build windows && cgo

package huddle

/*
#cgo LDFLAGS: -lmfplat -lmfuuid -lole32 -loleaut32 -luuid
#include <stdint.h>
void *clack_mf_new(int w, int h, int fps, int bps, int *status);
int clack_mf_encode(void *p, const uint8_t *y, int ys, const uint8_t *uv, int uvs, long long t100ns, int key, uint8_t **out, int *n,
                    uint8_t **seq, int *seqLen);
void clack_mf_bitrate(void *p, int bps);
void clack_mf_free(void *p);
*/
import "C"

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"time"
	"unsafe"
)

func init() { newHWEncoders = append(newHWEncoders, newMFEncoder) }

// mfEncoder is Media Foundation's H.264 encoder MFT (h264enc_windows.c).
type mfEncoder struct {
	mu    sync.Mutex
	p     unsafe.Pointer
	w, h  int
	start time.Time
}

func newMFEncoder(o encOpts) (videoEncoder, error) {
	var st C.int
	p := C.clack_mf_new(C.int(o.W), C.int(o.H), C.int(o.FPS), C.int(o.Bps), &st)
	if p == nil {
		return nil, fmt.Errorf("media foundation H.264 encoder: HRESULT %#x", uint32(st))
	}
	return &mfEncoder{p: p, w: o.W, h: o.H, start: time.Now()}, nil
}

func (e *mfEncoder) Name() string { return "Media Foundation" }

func (e *mfEncoder) Encode(f *CameraFrame, key bool) ([]byte, error) {
	if f.W != e.w || f.H != e.h || len(f.Y) < f.YStride*(f.H-1)+f.W || len(f.UV) < f.UVStride*(f.H/2-1)+f.W {
		return nil, errors.New("media foundation: frame doesn't match the encoder")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.p == nil {
		return nil, errors.New("media foundation: closed")
	}
	k := C.int(0)
	if key {
		k = 1
	}
	var out, seq *C.uint8_t
	var n, seqLen C.int
	t := C.longlong(max(0, f.At.Sub(e.start).Nanoseconds()/100))
	if st := C.clack_mf_encode(e.p, (*C.uint8_t)(unsafe.Pointer(&f.Y[0])), C.int(f.YStride), (*C.uint8_t)(unsafe.Pointer(&f.UV[0])),
		C.int(f.UVStride), t, k, &out, &n, &seq, &seqLen); st != 0 {
		return nil, fmt.Errorf("media foundation: encode: HRESULT %#x", uint32(st))
	}
	if n == 0 {
		return nil, nil
	}
	au := C.GoBytes(unsafe.Pointer(out), n)
	if hasIDR(au) && !hasNAL(au, 7) && seqLen > 0 { // a keyframe without its SPS and PPS
		au = append(C.GoBytes(unsafe.Pointer(seq), seqLen), au...)
	}
	return au, nil
}

// hasNAL reports whether an Annex B access unit has a NAL unit of type t.
func hasNAL(au []byte, t byte) bool {
	for {
		i := bytes.Index(au, []byte{0, 0, 1})
		if i < 0 || i+3 >= len(au) {
			return false
		}
		if au[i+3]&0x1f == t {
			return true
		}
		au = au[i+3:]
	}
}

func (e *mfEncoder) SetBitrate(bps int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.p != nil && bps > 0 {
		C.clack_mf_bitrate(e.p, C.int(bps))
	}
}

func (e *mfEncoder) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.p != nil {
		C.clack_mf_free(e.p)
		e.p = nil
	}
}

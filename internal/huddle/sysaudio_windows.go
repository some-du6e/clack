//go:build windows && cgo

package huddle

/*
#include <stdint.h>
int clack_loopback_probe(void);
void *clack_loopback_start(uintptr_t handle, int *status);
void clack_loopback_stop(void *p);
*/
import "C"

import (
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"
)

// The system audio for "Share system audio" (sysaudio_windows.c):
// process loopback without Clack's own sound.
func init() {
	loopbackSupported = func() bool {
		loopOnce.Do(func() { loopOK = C.clack_loopback_probe() != 0 })
		return loopOK
	}
	startLoopback = startWASAPILoopback
}

var (
	loopOnce  sync.Once
	loopOK    bool
	loopbacks sync.Map // id -> func([]int16)
	loopNext  atomic.Uintptr
)

func startWASAPILoopback(onAudio func([]int16)) (func(), error) {
	id := loopNext.Add(1)
	loopbacks.Store(id, onAudio)
	var st C.int
	p := C.clack_loopback_start(C.uintptr_t(id), &st)
	if p == nil {
		loopbacks.Delete(id)
		return nil, fmt.Errorf("system audio loopback: HRESULT %#x", uint32(st))
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			C.clack_loopback_stop(p)
			loopbacks.Delete(id)
		})
	}, nil
}

// clackLoopbackPCM runs on the capture thread: 48 kHz stereo frames.
//
//export clackLoopbackPCM
func clackLoopbackPCM(h C.uintptr_t, pcm *C.int16_t, frames C.int) {
	v, ok := loopbacks.Load(uintptr(h))
	if !ok || pcm == nil || frames <= 0 {
		return
	}
	v.(func([]int16))(unsafe.Slice((*int16)(unsafe.Pointer(pcm)), int(frames)*2))
}

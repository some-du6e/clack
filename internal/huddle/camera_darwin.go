//go:build darwin && cgo

package huddle

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AVFoundation -framework CoreMedia -framework CoreVideo -framework Foundation
#include <stdint.h>
#include <stdlib.h>
void *clack_cam_open(uintptr_t h, char **err);
void clack_cam_close(void *c);
*/
import "C"

import (
	"errors"
	"runtime/cgo"
	"sync"
	"time"
	"unsafe"
)

const cameraBackend = "avfoundation"

func init() { openDefaultCamera = openAVCamera }

// avCamera is the default camera through AVFoundation: an AVCaptureSession
// with an AVCaptureVideoDataOutput delivering NV12 on its own serial
// dispatch queue (camera_darwin.m). The queue's callback copies the planes
// into a fresh CameraFrame and offers it to a buffered channel without
// blocking (a late frame is dropped, like alwaysDiscardsLateVideoFrames);
// one goroutine hands frames to onFrame.
type avCamera struct {
	logf   func(string, ...any)
	frames chan *CameraFrame
	stop   chan struct{}
	h      cgo.Handle
	mu     sync.Mutex
	c      unsafe.Pointer
	once   sync.Once
}

func openAVCamera(logf func(string, ...any)) (Camera, error) {
	return &avCamera{logf: logf, frames: make(chan *CameraFrame, 2), stop: make(chan struct{})}, nil
}

// Start configures and starts the capture session ([AVCaptureSession
// startRunning] blocks until the camera runs, so call it off the UI
// goroutine; Session does). Access must already be granted (avperm).
func (c *avCamera) Start(onFrame func(*CameraFrame)) error {
	c.h = cgo.NewHandle(c)
	var cerr *C.char
	p := C.clack_cam_open(C.uintptr_t(c.h), &cerr)
	if p == nil {
		c.h.Delete()
		msg := "camera: can't start capture"
		if cerr != nil {
			msg = "camera: " + C.GoString(cerr)
			C.free(unsafe.Pointer(cerr))
		}
		return errors.New(msg)
	}
	c.mu.Lock()
	c.c = p
	c.mu.Unlock()
	go func() {
		for {
			select {
			case <-c.stop:
				return
			case f := <-c.frames:
				onFrame(f)
			}
		}
	}()
	return nil
}

func (c *avCamera) Close() {
	c.once.Do(func() {
		close(c.stop)
		c.mu.Lock()
		p := c.c
		c.c = nil
		c.mu.Unlock()
		if p != nil {
			C.clack_cam_close(p) // stops the session and drains the queue
			c.h.Delete()
		}
	})
}

// clackCamFrame runs on the capture queue with the pixel buffer locked: it
// copies, offers the frame, and returns at once.
//
//export clackCamFrame
func clackCamFrame(h C.uintptr_t, y unsafe.Pointer, ys C.int, uv unsafe.Pointer, uvs C.int, w, hh C.int, full C.int) {
	c, ok := cgo.Handle(h).Value().(*avCamera)
	if !ok || y == nil || uv == nil || w <= 0 || hh <= 0 {
		return
	}
	W, H := int(w)&^1, int(hh)&^1
	f := &CameraFrame{W: W, H: H, YStride: W, UVStride: W, FullRange: full != 0, At: time.Now(),
		Y: make([]byte, W*H), UV: make([]byte, W*H/2)}
	src := unsafe.Slice((*byte)(y), int(ys)*H)
	for r := 0; r < H; r++ {
		copy(f.Y[r*W:(r+1)*W], src[r*int(ys):])
	}
	srcUV := unsafe.Slice((*byte)(uv), int(uvs)*(H/2))
	for r := 0; r < H/2; r++ {
		copy(f.UV[r*W:(r+1)*W], srcUV[r*int(uvs):])
	}
	select {
	case c.frames <- f:
	default: // the encoder is behind: drop this one
	}
}

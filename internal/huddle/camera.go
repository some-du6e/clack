package huddle

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CameraFrame is one captured picture in NV12: a full-size luma plane and
// a half-size plane of interleaved Cb,Cr pairs (what macOS cameras deliver
// natively and VideoToolbox encodes without conversion). W and H are even.
type CameraFrame struct {
	W, H      int
	Y         []byte // YStride*H
	UV        []byte // UVStride*H/2, Cb Cr Cb Cr …
	YStride   int
	UVStride  int
	FullRange bool // 0..255 levels (else BT.601 video range 16..235)
	At        time.Time
}

// Camera is a video source. Start begins capture and returns without
// waiting for a permission prompt (Session asks first, see avperm).
// onFrame runs on one goroutine the camera owns, one frame at a time, and
// the frame then belongs to the callee: capture threads (AVFoundation's
// queue) only copy the picture and hand it over, never blocking.
type Camera interface {
	Start(onFrame func(*CameraFrame)) error
	Close()
}

// openDefaultCamera opens the system's default camera: AVFoundation on
// macOS (camera_darwin.go). Nil where Clack has no capture backend yet
// (Linux would be V4L2, Windows Media Foundation; see camera_other.go).
var openDefaultCamera func(logf func(string, ...any)) (Camera, error)

// ErrCameraUnsupported: this build has no camera backend.
var ErrCameraUnsupported = errors.New("camera not supported on this platform yet")

// CameraSupported reports whether SetCamera(true) can work here: a capture
// backend exists, or CLACK_HUDDLE_TESTCAM=1 replaces the camera with a
// test pattern.
func CameraSupported() bool { return openDefaultCamera != nil || testCamEnv() }

func testCamEnv() bool {
	v := os.Getenv("CLACK_HUDDLE_TESTCAM")
	return v != "" && v != "0"
}

// testCamSize parses CLACK_HUDDLE_TESTCAM=WxH (anything else: 320x180).
func testCamSize() (int, int) {
	if a, b, ok := strings.Cut(os.Getenv("CLACK_HUDDLE_TESTCAM"), "x"); ok {
		w, _ := strconv.Atoi(a)
		h, _ := strconv.Atoi(b)
		if w >= 16 && h >= 16 && w <= 1920 && h <= 1080 {
			return w &^ 1, h &^ 1
		}
	}
	return 320, 180
}

// testCamera is a synthetic camera: colour bars with a white block that
// sweeps across and a frame counter in binary along the bottom, in BT.601
// video range. It needs no device and no permission.
type testCamera struct {
	w, h, fps int
	stop      chan struct{}
	once      sync.Once
}

// NewTestCamera is a Camera that renders a moving test pattern of w×h
// (rounded down to even) at fps, for the fake Chime, tests and the demo.
func NewTestCamera(w, h, fps int) Camera {
	if fps <= 0 {
		fps = 15
	}
	return &testCamera{w: max(16, w&^1), h: max(16, h&^1), fps: fps, stop: make(chan struct{})}
}

// The bars, as BT.601 video-range Y, Cb, Cr: white, yellow, cyan, green,
// magenta, red, blue, black.
var testBars = [8][3]byte{{235, 128, 128}, {210, 16, 146}, {170, 166, 16}, {145, 54, 34},
	{106, 202, 222}, {81, 90, 240}, {41, 240, 110}, {16, 128, 128}}

func (c *testCamera) Start(onFrame func(*CameraFrame)) error {
	go func() {
		t := time.NewTicker(time.Second / time.Duration(c.fps))
		defer t.Stop()
		for n := 0; ; n++ {
			select {
			case <-c.stop:
				return
			case <-t.C:
			}
			onFrame(c.render(n))
		}
	}()
	return nil
}

func (c *testCamera) render(n int) *CameraFrame {
	w, h := c.w, c.h
	f := &CameraFrame{W: w, H: h, Y: make([]byte, w*h), UV: make([]byte, w*h/2), YStride: w, UVStride: w, At: time.Now()}
	for y := 0; y < h; y++ {
		row := f.Y[y*w:]
		for x := 0; x < w; x++ {
			row[x] = testBars[x*8/w][0]
		}
	}
	for y := 0; y < h/2; y++ {
		row := f.UV[y*w:]
		for x := 0; x < w/2; x++ {
			b := testBars[(2*x)*8/w]
			row[2*x], row[2*x+1] = b[1], b[2]
		}
	}
	// a white block sweeping left to right, one 8 px step per frame
	bs := max(16, h/4)
	bx := (n * 8) % max(1, w-bs)
	by := (h - bs) / 2
	for y := by; y < by+bs; y++ {
		for x := bx; x < bx+bs; x++ {
			f.Y[y*w+x] = 235
		}
	}
	for y := by / 2; y < (by+bs)/2; y++ {
		for x := bx / 2; x < (bx+bs)/2; x++ {
			f.UV[y*w+2*x], f.UV[y*w+2*x+1] = 128, 128
		}
	}
	// the frame number in binary: 16 cells, white = 1
	cell := max(2, w/32) &^ 1
	for bit := 0; bit < 16 && (bit+1)*cell <= w; bit++ {
		v := byte(16)
		if n>>bit&1 != 0 {
			v = 235
		}
		for y := h - cell; y < h; y++ {
			for x := bit * cell; x < (bit+1)*cell; x++ {
				f.Y[y*w+x] = v
			}
		}
	}
	return f
}

func (c *testCamera) Close() { c.once.Do(func() { close(c.stop) }) }

//go:build linux

package ui

import (
	"bytes"
	"fmt"
	"image"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/app"
	"gioui.org/io/event"
)

// caretX11Window is the real window's X11 id (app.X11ViewEvent), 0 until
// it has one.
var caretX11Window atomic.Uint64

func caretReadbackSetup(*testing.T) {}

func caretWindowEvent(e event.Event) {
	if v, ok := e.(app.X11ViewEvent); ok {
		caretX11Window.Store(uint64(v.Window))
	}
}

// caretShotPy reads an X11 window's pixels (XGetImage; cgo can't be in a
// test): a line "width height bytes-per-pixel", then the rows, BGRX.
const caretShotPy = `
import sys
from Xlib import display, X
d = display.Display()
w = d.create_resource_object('window', int(sys.argv[1]))
g = w.get_geometry()
img = w.get_image(0, 0, g.width, g.height, X.ZPixmap, 0xffffffff)
n = len(img.data) // max(g.width * g.height, 1)
sys.stdout.buffer.write(b'%d %d %d\n' % (g.width, g.height, n))
sys.stdout.buffer.write(img.data)
`

// caretShot is the X11 window's pixels as Xvfb has them: what the window
// presented.
func caretShot(win uint64) (*image.RGBA, error) {
	out, err := exec.Command("python3", "-c", caretShotPy, strconv.FormatUint(win, 10)).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%v: %s", err, bytes.TrimSpace(ee.Stderr))
		}
		return nil, err
	}
	nl := bytes.IndexByte(out, '\n')
	var w, h, bpp int
	if nl < 0 {
		return nil, fmt.Errorf("no header in %d bytes", len(out))
	}
	if _, err := fmt.Sscanf(string(out[:nl]), "%d %d %d", &w, &h, &bpp); err != nil {
		return nil, fmt.Errorf("header %q: %v", out[:nl], err)
	}
	data := out[nl+1:]
	if bpp != 4 || len(data) != w*h*4 {
		return nil, fmt.Errorf("%d bytes for %d×%d at %d bytes a pixel", len(data), w, h, bpp)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		img.Pix[4*i], img.Pix[4*i+1], img.Pix[4*i+2], img.Pix[4*i+3] = data[4*i+2], data[4*i+1], data[4*i], 0xff
	}
	return img, nil
}

// caretShotDiff is how many pixels of a and b differ, by how many levels at
// most, and the box around them.
func caretShotDiff(a, b *image.RGBA) (n, maxd int, box image.Rectangle) {
	r := a.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i := a.PixOffset(x, y)
			d := 0
			for k := 0; k < 3; k++ {
				d = max(d, int(max(a.Pix[i+k], b.Pix[i+k])-min(a.Pix[i+k], b.Pix[i+k])))
			}
			if d == 0 {
				continue
			}
			n++
			maxd = max(maxd, d)
			p := image.Rect(x, y, x+1, y+1)
			if box.Empty() {
				box = p
			} else {
				box = box.Union(p)
			}
		}
	}
	return n, maxd, box
}

// caretReadback reads the real window's pixels back from Xvfb between
// blinks: after a blink's partial frame (A), after a whole frame of the
// same layout drawn right after it (B: the window must be A, bit for bit),
// and after the next blink (C: the caret's pixels toggle, nothing else
// changes).
func caretReadback(t *testing.T, a *App, frames <-chan caretGPUFrame, blink time.Duration, ww, wh int) {
	t.Helper()
	win := caretX11Window.Load()
	if win == 0 || os.Getenv("DISPLAY") == "" {
		t.Logf("readback: no X11 window (DISPLAY %q): skipped", os.Getenv("DISPLAY"))
		return
	}
	if _, err := caretShot(win); err != nil {
		if os.Getenv("CI") != "" {
			t.Errorf("readback: %v (ci installs python3-xlib)", err)
		} else {
			t.Logf("readback: %v: skipped", err)
		}
		return
	}
	drain := func() {
		for {
			select {
			case <-frames:
			default:
				return
			}
		}
	}
	// next is the next frame the window draws that ok takes
	next := func(what string, d time.Duration, ok func(caretGPUFrame) bool) (caretGPUFrame, bool) {
		dl := time.After(d)
		t0 := time.Now()
		var seen []string // the frames that came instead, for the failure
		for {
			select {
			case f := <-frames:
				if ok(f) {
					return f, true
				}
				seen = append(seen, fmt.Sprintf("%v: partial %v, %d px damaged, %d draws, whole: %q", time.Since(t0).Round(time.Millisecond), f.partial, f.damagePx, f.draws, f.whole))
			case <-dl:
				if len(seen) == 0 {
					seen = []string{"none"}
				}
				t.Errorf("readback: no %s in %v; the frames that came: %s", what, d, strings.Join(seen, "; "))
				return caretGPUFrame{}, false
			}
		}
	}
	isBlink := func(f caretGPUFrame) bool {
		return f.partial && f.damagePx > 0 && f.damagePx <= int64(ww*wh)/200
	}
	shot := func() *image.RGBA {
		t.Helper()
		img, err := caretShot(win)
		if err != nil {
			t.Fatalf("readback: %v", err)
		}
		return img
	}
	// Partial frames came back on just now, after the whole-frames turns,
	// and the window takes a few frames to draw partial again: a fresh
	// frame texture fills, the sticky atlas's first repack makes the next
	// frame's damage too big (whole, straight into the window), and two
	// more fill the texture again (gpu.FrameStats.Whole says which). At
	// 1440x900 that was four blinks, all of the 2.4 s the first wait gave
	// (3 of 50 runs: 44960352218, 44963639630). So the window settles
	// first.
	drain()
	if _, ok := next("partial frame since partial frames came back on", 10*blink, func(f caretGPUFrame) bool { return f.partial }); !ok {
		return
	}
	for try := 0; try < 4; try++ {
		drain()
		if _, ok := next("blink's partial frame", 4*blink, isBlink); !ok {
			return
		}
		t0 := time.Now()
		time.Sleep(60 * time.Millisecond) // Xvfb has the swap
		shotA := shot()
		// the same layout drawn whole: a frame posted now, before the
		// next blink
		caretWhole(true)
		drain()
		a.Post(func() {})
		f, ok := next("whole frame", 2*time.Second, func(f caretGPUFrame) bool { return !f.partial })
		caretWhole(false)
		if !ok {
			return
		}
		time.Sleep(60 * time.Millisecond)
		shotB := shot()
		if time.Since(t0) > blink-100*time.Millisecond {
			t.Logf("readback: the whole frame came %v after the blink, maybe past the next; again", time.Since(t0).Round(time.Millisecond))
			continue
		}
		drain()
		if _, ok := next("next blink's partial frame", 4*blink, isBlink); !ok {
			return
		}
		time.Sleep(60 * time.Millisecond)
		shotC := shot()
		n, m, box := caretShotDiff(shotA, shotB)
		t.Logf("readback: the window after a blink's partial frame against a whole frame of it (%d draws): %d px differ by up to %d levels in %v", f.draws, n, m, box)
		if n > 0 {
			t.Errorf("readback: the window after a partial frame differs from the whole frame in %d px by up to %d levels, in %v", n, m, box)
		}
		n, m, box = caretShotDiff(shotA, shotC)
		t.Logf("readback: the window a blink later: %d px differ by up to %d levels, in %v (%d×%d)", n, m, box, box.Dx(), box.Dy())
		switch {
		case n == 0:
			t.Errorf("readback: the caret's pixels didn't toggle between blinks")
		case box.Dx()*box.Dy() > shotA.Bounds().Dx()*shotA.Bounds().Dy()/200:
			t.Errorf("readback: a blink changed %d px in %v, more than the caret's few", n, box)
		}
		return
	}
	t.Errorf("readback: no whole frame between two blinks in 4 tries")
}

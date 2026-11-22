// Package pixeltest renders Gio ops with the GPU renderer (llvmpipe in CI)
// for tests that need to see pixels: what draws on top of what, and in
// which colour. Only tests import it.
package pixeltest

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/op"
)

// NewWindow is headless.NewWindow, retried: Mesa's EGL on Xvfb sometimes
// fails eglInitialize (0x3001, EGL_NOT_INITIALIZED) while other test
// binaries bring theirs up, and succeeds a moment later. Every GPU test
// opens its window through it. The error after the last try says how
// many there were, so a runner with no working EGL reads as that, not as
// a flake.
func NewWindow(w, h int) (*headless.Window, error) {
	win, err := newHeadless(w, h)
	tries := 1
	for ; err != nil && tries < 6; tries++ {
		time.Sleep(time.Duration(200*tries) * time.Millisecond)
		win, err = newHeadless(w, h)
	}
	if err != nil {
		return nil, fmt.Errorf("%w (%d tries over %v: no working EGL on this machine)", err, tries, time.Duration(100*tries*(tries-1))*time.Millisecond)
	}
	return win, nil
}

// injectEGLFailures makes the next headless windows fail as Mesa's
// eglInitialize does (tests of Render's retries).
var injectEGLFailures atomic.Int32

func newHeadless(w, h int) (*headless.Window, error) {
	if injectEGLFailures.Load() > 0 {
		injectEGLFailures.Add(-1)
		return nil, errors.New("eglInitialize failed: 0x3001 (injected)")
	}
	return headless.NewWindow(w, h)
}

// gpu is the process's verdict on the headless GPU: 0 not known yet, 1 a
// window came up, 2 none did (the first try, retried, failed).
var gpu atomic.Int32

// Render paints ops into a w×h image. Without a GPU context the test is
// skipped, unless CLACK_PIXEL_TESTS is set (the CI step that installs
// Mesa), where it fails. Every window comes through NewWindow's retries:
// a GPU that worked once in the process and fails now is Mesa's flaky
// eglInitialize (0x3001) under the suite's load, not a missing GPU, and
// skipping there ended a test halfway (its temp dir still in use:
// "TempDir RemoveAll cleanup: directory not empty"; full-test's
// TestPatchedReactionPillLikeNative and TestViewsPatchedReactionPillLikeNative,
// 2026-11-22). Only a process whose first window never came up skips.
func Render(t testing.TB, w, h int, ops *op.Ops) *image.RGBA {
	t.Helper()
	pixel := os.Getenv("CLACK_PIXEL_TESTS") != ""
	if gpu.Load() == 2 && !pixel {
		t.Skip("no headless GPU in this process; CI runs this with CLACK_PIXEL_TESTS=1")
	}
	win, err := NewWindow(w, h)
	switch {
	case err == nil:
		gpu.CompareAndSwap(0, 1)
	case pixel || gpu.Load() == 1:
		t.Fatalf("headless GPU: %v", err)
	default:
		gpu.Store(2)
		t.Skipf("no headless GPU (%v); CI runs this with CLACK_PIXEL_TESTS=1", err)
	}
	defer win.Release()
	if err := win.Frame(ops); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if err := win.Screenshot(img); err != nil {
		t.Fatal(err)
	}
	return img
}

// Ink counts the pixels in r that are at least half fg over bg (and near
// the fg-bg line, so other colours don't count).
func Ink(img *image.RGBA, r image.Rectangle, fg, bg color.NRGBA) int {
	d := [3]float64{float64(fg.R) - float64(bg.R), float64(fg.G) - float64(bg.G), float64(fg.B) - float64(bg.B)}
	dd := d[0]*d[0] + d[1]*d[1] + d[2]*d[2]
	if dd == 0 {
		return 0
	}
	n := 0
	r = r.Intersect(img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			c := img.RGBAAt(x, y)
			p := [3]float64{float64(c.R) - float64(bg.R), float64(c.G) - float64(bg.G), float64(c.B) - float64(bg.B)}
			a := (p[0]*d[0] + p[1]*d[1] + p[2]*d[2]) / dd
			if a < 0.5 {
				continue
			}
			var res float64
			for i := range p {
				e := p[i] - a*d[i]
				res += e * e
			}
			if math.Sqrt(res) < 48 {
				n++
			}
		}
	}
	return n
}

// Count counts the pixels in r within tol (per channel) of c.
func Count(img *image.RGBA, r image.Rectangle, c color.NRGBA, tol int) int {
	n := 0
	r = r.Intersect(img.Bounds())
	ab := func(v int) int {
		if v < 0 {
			return -v
		}
		return v
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			p := img.RGBAAt(x, y)
			if ab(int(p.R)-int(c.R)) <= tol && ab(int(p.G)-int(c.G)) <= tol && ab(int(p.B)-int(c.B)) <= tol {
				n++
			}
		}
	}
	return n
}

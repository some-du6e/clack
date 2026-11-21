// SPDX-License-Identifier: Unlicense OR MIT

package headless

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// Clack patch 55: headless windows on EGL share EGL_DEFAULT_DISPLAY, and
// one's Release no longer terminates it under the others. A window made
// before another one was released still draws; before the patch its next
// frame failed (eglMakeCurrent 0x3008: the inspect tool's per-shot window
// took a test's long-lived one down, 2026-11-14).
func TestClackWindowOutlivesAnother(t *testing.T) {
	sz := image.Pt(32, 24)
	a := clackWindow(t, sz.X, sz.Y)
	defer a.Release()
	b := clackWindow(t, sz.X, sz.Y)
	var o op.Ops
	paint.FillShape(&o, color.NRGBA{G: 0xff, A: 0xff}, clip.Rect{Max: sz}.Op())
	if err := b.Frame(&o); err != nil {
		t.Fatal(err)
	}
	b.Release()
	o.Reset()
	paint.FillShape(&o, color.NRGBA{R: 0xff, A: 0xff}, clip.Rect{Max: sz}.Op())
	if err := a.Frame(&o); err != nil {
		t.Fatalf("the first window's frame after the second one's Release: %v", err)
	}
	img := image.NewRGBA(image.Rectangle{Max: sz})
	if err := a.Screenshot(img); err != nil {
		t.Fatalf("the first window's screenshot after the second one's Release: %v", err)
	}
	if got := img.RGBAAt(sz.X/2, sz.Y/2); got != (color.RGBA{R: 0xff, A: 0xff}) {
		t.Errorf("the first window drew %v, want red", got)
	}
}

package pixeltest

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// A headless GPU that worked once in the process and then fails
// eglInitialize (Mesa's 0x3001 under the suite's load) is retried, not
// taken for a missing GPU: the render in the middle of a test comes out,
// with or without CLACK_PIXEL_TESTS. Skipping there ended a test halfway
// (full-test's reaction pill tests, 2026-11-22).
func TestRenderRetriesAFlakyEGL(t *testing.T) {
	ops := new(op.Ops)
	paint.FillShape(ops, color.NRGBA{R: 255, A: 255}, clip.Rect(image.Rect(0, 0, 8, 8)).Op())
	first := t.Run("the first window", func(t *testing.T) { Render(t, 8, 8, ops) })
	if !first {
		t.Fatal("the first render failed")
	}
	if gpu.Load() != 1 {
		t.Skip("no headless GPU here")
	}
	t.Setenv("CLACK_PIXEL_TESTS", "") // full-test's way: no fatal flag
	injectEGLFailures.Store(2)
	defer injectEGLFailures.Store(0)
	var img *image.RGBA
	var skipped bool
	ok := t.Run("a window after two failed eglInitialize", func(t *testing.T) {
		defer func() { skipped = t.Skipped() }()
		img = Render(t, 8, 8, ops)
	})
	if !ok || skipped || img == nil {
		t.Fatalf("the render after a flaky eglInitialize: ok %v, skipped %v", ok, skipped)
	}
	if c := img.RGBAAt(4, 4); c.R < 200 {
		t.Errorf("rendered %v, want red", c)
	}
}

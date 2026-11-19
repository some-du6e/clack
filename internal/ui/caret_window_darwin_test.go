//go:build darwin && cgo

package ui

import (
	"testing"
	"time"

	"gioui.org/app"
	"gioui.org/io/event"
)

// On macOS the window's drawables are made readable for the readback
// alone (app.ClackReadableDrawables: a window's are framebufferOnly, and
// the test's blinks and scroll are measured on those), and the GPU reads
// the window back after each copy of the frame texture into it
// (gpu.ClackVerifyCopy): Metal's draw of the frame texture into the
// drawable must give the drawable its pixels, bit for bit.

func caretReadbackSetup(*testing.T) {}

func caretWindowEvent(event.Event) {}

func caretReadback(t *testing.T, a *App, frames <-chan caretGPUFrame, blink time.Duration, ww, wh int) {
	t.Helper()
	app.ClackReadableDrawables(true)
	defer app.ClackReadableDrawables(false)
	caretVerifyCopy(true)
	defer caretVerifyCopy(false)
	for drained := false; !drained; {
		select {
		case <-frames:
		default:
			drained = true
		}
	}
	checked, partial := 0, 0
	dl := time.After(6*blink + 2*time.Second)
	for checked < 4 {
		select {
		case f := <-frames:
			if !f.copyChecked {
				continue
			}
			checked++
			if f.partial {
				partial++
			}
			if f.copyMismatchPx > 0 {
				t.Errorf("readback: the drawable after the frame texture's copy differs from it in %d px (partial %v, %d px damaged)", f.copyMismatchPx, f.partial, f.damagePx)
			}
		case <-dl:
			t.Errorf("readback: %d frames' drawables read back in %v, want 4", checked, 6*blink+2*time.Second)
			return
		}
	}
	t.Logf("readback: %d frames' drawables read back after the frame texture's copy (%d partial), each the frame texture's pixels", checked, partial)
}

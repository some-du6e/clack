//go:build windows

package ui

import (
	"testing"
	"time"

	"gioui.org/io/event"
)

// The real window's pixels are read back on X11 (Xvfb) and macOS (the
// drawable) only: Direct3D's frame copy is proven bit for bit by
// gpu/headless's TestClackPartialFrames through the frame texture on WARP.

func caretReadbackSetup(*testing.T) {}

func caretWindowEvent(event.Event) {}

func caretReadback(t *testing.T, a *App, frames <-chan caretGPUFrame, blink time.Duration, ww, wh int) {
	t.Logf("readback: X11 and macOS only")
}

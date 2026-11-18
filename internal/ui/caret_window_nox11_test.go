//go:build windows || (darwin && cgo)

package ui

import (
	"testing"
	"time"

	"gioui.org/io/event"
)

// The real window's pixels are read back on X11 only (Xvfb): Metal's and
// Direct3D's frame copies are proven bit for bit by gpu/headless's
// TestClackPartialFrames through the frame texture on those GPUs.

func caretWindowEvent(event.Event) {}

func caretReadback(t *testing.T, a *App, frames <-chan caretGPUFrame, blink time.Duration, ww, wh int) {
	t.Logf("readback: X11 only")
}

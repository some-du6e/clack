//go:build linux || windows

package ui

import (
	"os"
	"runtime"
	"testing"

	"gioui.org/io/event"
)

// realWindowGate: TestRealWindowQuietFrames runs with
// CLACK_REAL_WINDOW_TESTS set, on Linux under an X server (Xvfb in ci).
func realWindowGate() (bool, string) {
	if os.Getenv("CLACK_REAL_WINDOW_TESTS") == "" {
		return false, "CLACK_REAL_WINDOW_TESTS not set"
	}
	if runtime.GOOS == "linux" {
		if os.Getenv("DISPLAY") == "" {
			return false, "no DISPLAY (run it under Xvfb)"
		}
		os.Unsetenv("WAYLAND_DISPLAY") // X11, not Wayland
	}
	return true, ""
}

func realWindowEvent(event.Event) {}
func realWindowShow(t *testing.T) {}

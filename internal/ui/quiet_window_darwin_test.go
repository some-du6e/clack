//go:build darwin && cgo

package ui

import (
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/app"
	"gioui.org/app/cocoatest"
	"gioui.org/io/event"
)

// realWindowGate: TestRealWindowQuietFrames runs with
// CLACK_MACOS_WINDOW_TESTS set (TestMain then runs app.Main).
func realWindowGate() (bool, string) {
	if !macWindowTests {
		return false, "CLACK_MACOS_WINDOW_TESTS not set"
	}
	return true, ""
}

var realWindowView atomic.Uintptr // the main window's NSView

func realWindowEvent(e event.Event) {
	if v, ok := e.(app.AppKitViewEvent); ok && v.View != 0 {
		realWindowView.Store(v.View)
	}
}

// realWindowShow brings the window to the front, key: an occluded window
// draws nothing (Gio patch 32).
func realWindowShow(t *testing.T) {
	t.Helper()
	view := realWindowView.Load()
	if view == 0 {
		t.Fatal("no AppKitViewEvent")
	}
	onMain(func() { cocoatest.OrderFront(view) })
	for dl := time.Now().Add(10 * time.Second); time.Now().Before(dl); time.Sleep(50 * time.Millisecond) {
		var key bool
		onMain(func() { key = cocoatest.State(view).Key })
		if key {
			return
		}
	}
	t.Log("the window isn't key; going on")
}

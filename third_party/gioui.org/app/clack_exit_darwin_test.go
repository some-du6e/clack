// SPDX-License-Identifier: Unlicense OR MIT

//go:build darwin && !ios

package app

import (
	"testing"
	"time"

	"gioui.org/app/cocoatest"
	"gioui.org/io/event"
)

// Clack patch (CLACK_PATCHES.md §58) on a real macOS desktop
// (CLACK_MACOS_WINDOW_TESTS, ci's macos job). AppKit's terminate:, which
// the Dock's Quit, log out, restart and a quit Apple Event all reach,
// asks the program first: one ranging over Events gets a TerminateEvent
// while AppKit waits (NSTerminateLater), the process alive and the main
// thread still running work (Window.Perform's, Gio's frames), so the
// program can quit as its own Quit does. Here it carries on instead
// (replyToApplicationShouldTerminate: NO), and a second terminate: asks
// again. Review of karim-1118: Gio's delegate had no
// applicationShouldTerminate:, so AppKit ended Clack without its exit
// line.
func TestTerminateEventMac(t *testing.T) {
	if !macWindowTestsEnabled() {
		t.Skip("CLACK_MACOS_WINDOW_TESTS not set")
	}
	got := make(chan event.Event, 4)
	cocoatest.OnMain(func() {
		yieldGlobalEvent = func(e event.Event) bool { got <- e; return true }
	})
	t.Cleanup(func() { cocoatest.OnMain(func() { yieldGlobalEvent = nil }) })
	for round := 1; round <= 2; round++ {
		cocoatest.Terminate()
		select {
		case e := <-got:
			if _, ok := e.(TerminateEvent); !ok {
				t.Fatalf("round %d: %T, want a TerminateEvent", round, e)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("round %d: no TerminateEvent: AppKit's terminate: didn't ask the program", round)
		}
		ran := make(chan struct{})
		go func() {
			cocoatest.OnMain(func() {})
			close(ran)
		}()
		select {
		case <-ran:
		case <-time.After(5 * time.Second):
			t.Fatalf("round %d: the main thread runs no work while AppKit waits on the program", round)
		}
		cocoatest.OnMain(func() { replyToTerminate(false) })
	}
}

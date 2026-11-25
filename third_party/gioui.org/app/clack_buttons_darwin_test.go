// SPDX-License-Identifier: Unlicense OR MIT

//go:build darwin && !ios

package app

import (
	"slices"
	"testing"
	"time"

	"gioui.org/app/cocoatest"
	"gioui.org/io/pointer"
)

// Clack patch 68 on a real macOS window (CLACK_MACOS_WINDOW_TESTS): a
// right press whose release never reaches the view, as when the native
// menu it opened took it, leaves no button down. The next left press is
// the primary button's alone, not Primary|Secondary (a context click to
// Clack's ctxclick: Karim's left clicks in the composer opened its text
// menu, 2026-11-24), after a Cancel ending the right press's gesture. A
// mouseMoved: (AppKit sends it with no button down) ends it the same way,
// and is a move, not a drag of the lost press.
func TestClackLostReleaseMac(t *testing.T) {
	if !macWindowTestsEnabled() {
		t.Skip("CLACK_MACOS_WINDOW_TESTS not set")
	}
	h := newMacTestWindowWith(t, func() bool { return false })
	defer h.destroy(t)
	h.makeKey(t)
	const x, y = 400, 300 // window points, under the band
	// until reads the content's pointer events up to one of kind, and
	// returns it with the kinds before it
	until := func(what string, kind pointer.Kind) (pointer.Event, []pointer.Kind) {
		t.Helper()
		var before []pointer.Kind
		deadline := time.After(10 * time.Second)
		for {
			select {
			case e := <-h.ptrs:
				if e.Kind == kind {
					return e, before
				}
				before = append(before, e.Kind)
			case <-deadline:
				t.Fatalf("%s: no %v (the events before it: %v; window %+v)", what, kind, before, h.state())
			}
		}
	}
	drain := func() {
		for {
			select {
			case <-h.ptrs:
			default:
				return
			}
		}
	}
	lostRight := func(what string) {
		t.Helper()
		cocoatest.OnMain(func() { cocoatest.PostButton(h.view, cocoatest.RightButton, true, x, y, 1) })
		if e, _ := until(what, pointer.Press); e.Buttons != pointer.ButtonSecondary {
			t.Fatalf("%s came with %v, want the secondary button (a posted rightMouseDown)", what, e.Buttons)
		}
	}
	drain()

	lostRight("a right press, its release never posted")
	h.click(x, y, 1)
	e, before := until("the left press after it", pointer.Press)
	if e.Buttons != pointer.ButtonPrimary {
		t.Errorf("the left press after a right press with no release came with %v, want the primary button alone (with the secondary it is a context click)", e.Buttons)
	}
	if !slices.Contains(before, pointer.Cancel) {
		t.Errorf("nothing ended the right press before the left one: the events between them were %v, want a Cancel", before)
	}
	until("the left release", pointer.Release)
	time.Sleep(200 * time.Millisecond)
	drain()

	lostRight("a second right press, its release never posted")
	cocoatest.OnMain(func() { cocoatest.PostMoved(h.view, x+20, y+10) })
	_, before = until("the pointer moving with no button down", pointer.Move)
	if !slices.Contains(before, pointer.Cancel) || slices.Contains(before, pointer.Drag) {
		t.Errorf("the events from the lost right press to the move were %v, want a Cancel and no drag", before)
	}
}

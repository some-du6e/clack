// SPDX-License-Identifier: Unlicense OR MIT

package app

import (
	"testing"

	"gioui.org/io/event"
)

// Clack patch (CLACK_PATCHES.md §26): every Invalidate is followed by a
// FrameEvent or a wake-up handed to the client after it, also when it
// came while the driver was still delivering an earlier one. Clack runs
// its posted work on wake-ups while its window is hidden or minimized
// (and so draws no frames); a notification's taskbar flash, posted from
// a goroutine while the wake-up that showed the notification was still
// being handled, waited for the next event of any kind
// (internal/ui TestTaskbarWindow, 2026-11-14).

// wakeupTestDriver counts the driver Invalidates; the rest of the driver
// interface is never called here (nil: a call panics).
type wakeupTestDriver struct {
	driver
	invalidates int
	animating   bool
}

func (d *wakeupTestDriver) Invalidate()         { d.invalidates++ }
func (d *wakeupTestDriver) SetAnimating(a bool) { d.animating = a }

func isWakeup(e event.Event, ok bool) bool {
	_, w := e.(wakeupEvent)
	return ok && w
}

func TestInvalidateWhileHandlingAWakeup(t *testing.T) {
	d := &wakeupTestDriver{}
	w := &Window{driver: d}
	cb := &callbacks{w: w}
	if e, ok := w.nextEvent(); ok {
		t.Fatalf("an event before anything happened: %T", e)
	}

	// an Invalidate reaches the driver, which wakes the window loop
	// (callbacks.Invalidate): one wake-up for the client
	w.Invalidate()
	if d.invalidates != 1 {
		t.Fatalf("%d driver Invalidates, want 1", d.invalidates)
	}
	cb.Invalidate()
	if e, ok := w.nextEvent(); !isWakeup(e, ok) {
		t.Fatalf("after an Invalidate: %T, %v, want a wake-up", e, ok)
	}
	// While the client handles that wake-up, another goroutine
	// invalidates (Clack's invalidator, for work posted meanwhile). The
	// driver isn't asked again before the first is delivered...
	w.Invalidate()
	if d.invalidates != 1 {
		t.Errorf("%d driver Invalidates while the first was being delivered, want 1", d.invalidates)
	}
	// ...and the client gets a wake-up of its own once it's done.
	if e, ok := w.nextEvent(); !isWakeup(e, ok) {
		t.Fatalf("an Invalidate while the client handled a wake-up got %T, %v: lost", e, ok)
	}
	if e, ok := w.nextEvent(); ok {
		t.Fatalf("another event after the extra wake-up: %T", e)
	}
	// the next Invalidate goes to the driver again
	w.Invalidate()
	if d.invalidates != 2 {
		t.Fatalf("%d driver Invalidates after the client had everything, want 2", d.invalidates)
	}

	// An Invalidate made before the wake-up went out is covered by it:
	// no wake-up of its own.
	w.Invalidate() // the driver has one in flight: not asked
	cb.Invalidate()
	if e, ok := w.nextEvent(); !isWakeup(e, ok) {
		t.Fatalf("after two Invalidates: %T, %v, want a wake-up", e, ok)
	}
	if e, ok := w.nextEvent(); ok {
		t.Errorf("an extra event for an Invalidate the wake-up covered: %T", e)
	}
	if d.invalidates != 2 {
		t.Errorf("%d driver Invalidates, want 2", d.invalidates)
	}

	// A destroyed window (processEvent's DestroyEvent: no driver, no
	// Invalidates) gets no wake-up for one.
	w.invMu.Lock()
	w.mayInvalidate, w.driver = false, nil
	w.invMu.Unlock()
	w.Invalidate()
	if e, ok := w.nextEvent(); ok {
		t.Errorf("an event for a window without a driver: %T", e)
	}
}

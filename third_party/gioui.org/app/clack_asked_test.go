// SPDX-License-Identifier: Unlicense OR MIT

package app

import (
	"image"
	"testing"
	"time"

	"gioui.org/op"
)

// askedTestDriver is a driver whose frames go nowhere; the rest of the
// driver interface is never called here (nil: a call panics).
type askedTestDriver struct{ driver }

func (d *askedTestDriver) Frame(*op.Ops)       {}
func (d *askedTestDriver) Invalidate()         {}
func (d *askedTestDriver) SetAnimating(a bool) {}

// Clack patch 35: a FrameEvent says whether the window's own frames asked
// for it (an op.InvalidateCmd, input a handler took) and for when, as the
// window had it when the frame began; a frame only an Invalidate brought
// asked for nothing. A frame that replaces one the client wasn't handed
// yet keeps what that one was asked for.
func TestFrameEventAsked(t *testing.T) {
	w := &Window{driver: &askedTestDriver{}}
	now := time.Now()
	frame := func() FrameEvent {
		t.Helper()
		w.processEvent(frameEvent{FrameEvent: FrameEvent{Now: now, Size: image.Pt(100, 80)}})
		e, ok := w.nextEvent()
		fe, isFrame := e.(FrameEvent)
		if !ok || !isFrame {
			t.Fatalf("got %T, %v, want a FrameEvent", e, ok)
		}
		w.coalesced.framePending = false // as if the client drew it
		return fe
	}
	if e := frame(); e.Asked || !e.AskedAt.IsZero() {
		t.Errorf("nothing asked for a frame: Asked %v at %v", e.Asked, e.AskedAt)
	}
	later := now.Add(300 * time.Millisecond)
	w.setNextFrame(later) // an op.InvalidateCmd{At: later} (WakeupTime after a frame)
	if e := frame(); !e.Asked || !e.AskedAt.Equal(later) {
		t.Errorf("a wake-up asked for at +300 ms: Asked %v at %v", e.Asked, e.AskedAt.Sub(now))
	}
	if e := frame(); e.Asked {
		t.Error("the next frame was asked for too: the wake-up wasn't taken by the frame it came with")
	}
	// input a handler took asks for a frame at once; another frame comes
	// before the client is handed the first
	w.setNextFrame(time.Time{})
	w.processEvent(frameEvent{FrameEvent: FrameEvent{Now: now, Size: image.Pt(100, 80)}})
	if e := frame(); !e.Asked || !e.AskedAt.IsZero() {
		t.Errorf("a frame replacing an asked-for one the client never had: Asked %v at %v", e.Asked, e.AskedAt)
	}
}

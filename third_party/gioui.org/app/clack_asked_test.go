// SPDX-License-Identifier: Unlicense OR MIT

package app

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/op"
	"gioui.org/op/clip"
)

// askedTestDriver is a driver whose frames go nowhere and that runs a
// Window.Run function at once; the rest of the driver interface is never
// called here (nil: a call panics).
type askedTestDriver struct {
	driver
	invalidates int
}

func (d *askedTestDriver) Frame(*op.Ops)            {}
func (d *askedTestDriver) Invalidate()              { d.invalidates++ }
func (d *askedTestDriver) SetAnimating(bool)        {}
func (d *askedTestDriver) SetCursor(pointer.Cursor) {}
func (d *askedTestDriver) Configure([]Option)       {}
func (d *askedTestDriver) Run(f func())             { f() }

// Clack patch 35: a FrameEvent says whether the window's own frames asked
// for it, and for when: the router's wake-ups (an op.InvalidateCmd, as
// updateState finds it after a frame), input a handler took, a change of
// options. Window.Invalidate asks for a frame and nothing else: it comes
// back to the window as callbacks.Invalidate, the way every driver
// delivers it (the macOS and Windows event loops' Wakeup, X11's and
// Wayland's dispatch), and a frame only that brought has Asked false. It
// used to set the same next-frame time the window's asks do, so every
// frame an Invalidate brought said the window had asked for it, and ui
// laid out every frame an animation asked for (no quiet frame in a real
// window). An ask the window has when an Invalidate comes stays asked, and
// a frame that replaces one the client wasn't handed yet keeps what that
// one was asked for.
func TestFrameEventAsked(t *testing.T) {
	d := &askedTestDriver{}
	w := &Window{driver: d}
	cb := &callbacks{w: w}
	t.Cleanup(func() { // the wake-up timer's goroutine (scheduleInvalidate)
		if q := w.timer.quit; q != nil {
			q <- struct{}{}
			<-q
		}
	})
	now := time.Now()
	// frame is the next FrameEvent the driver draws, handed to the client
	frame := func() FrameEvent {
		t.Helper()
		w.processEvent(frameEvent{FrameEvent: FrameEvent{Now: now, Size: image.Pt(100, 80)}})
		for {
			e, ok := w.nextEvent()
			if !ok {
				t.Fatal("no FrameEvent")
			}
			if fe, isFrame := e.(FrameEvent); isFrame {
				w.coalesced.framePending = false // as if the client drew it
				return fe
			}
		}
	}
	// invalidate is Window.Invalidate as the driver delivers it
	invalidate := func() {
		w.invMu.Lock()
		w.mayInvalidate = true
		w.invMu.Unlock()
		n := d.invalidates
		w.Invalidate()
		if d.invalidates != n+1 {
			t.Fatal("Window.Invalidate didn't reach the driver")
		}
		cb.Invalidate()
	}

	if e := frame(); e.Asked || !e.AskedAt.IsZero() {
		t.Errorf("nothing asked for a frame: Asked %v at %v", e.Asked, e.AskedAt)
	}
	invalidate()
	if !w.hasNextFrame {
		t.Fatal("an Invalidate asked for no frame at all")
	}
	if e := frame(); e.Asked {
		t.Errorf("a frame only an Invalidate brought: Asked %v at %v, want nothing asked", e.Asked, e.AskedAt.Sub(now))
	}

	// a wake-up the router has after a frame (op.InvalidateCmd{At: later}),
	// and the Invalidate its timer brings at that time
	later := now.Add(time.Hour) // its timer never fires during the test
	w.queue.Source().Execute(op.InvalidateCmd{At: later})
	w.updateState()
	invalidate()
	if e := frame(); !e.Asked || !e.AskedAt.Equal(later) {
		t.Errorf("a wake-up asked for in an hour, then an Invalidate: Asked %v at %v, want in an hour", e.Asked, e.AskedAt.Sub(now))
	}
	// the frame took the ask: a client that drew it from its last ops asks
	// for it again (ui's replayFrame), one that laid out had its widgets
	// ask again
	invalidate()
	if e := frame(); e.Asked {
		t.Errorf("the ask outlived the frame it came with: Asked %v at %v", e.Asked, e.AskedAt.Sub(now))
	}

	// input a handler takes asks for a frame at once
	tag := new(int)
	var ops op.Ops
	cl := clip.Rect(image.Rect(0, 0, 100, 80)).Push(&ops)
	event.Op(&ops, tag)
	cl.Pop()
	for {
		if _, ok := w.queue.Source().Event(pointer.Filter{Target: tag, Kinds: pointer.Press}); !ok {
			break
		}
	}
	w.queue.Frame(&ops)
	if handled := w.processEvent(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: f32.Pt(10, 10), Buttons: pointer.ButtonPrimary}); !handled {
		t.Fatal("the press wasn't handled: the test shows nothing")
	}
	invalidate() // an animation's, meanwhile
	if e := frame(); !e.Asked || !e.AskedAt.IsZero() {
		t.Errorf("a press a handler took: Asked %v at %v, want at once", e.Asked, e.AskedAt.Sub(now))
	}

	// options
	w.Option(Title("asked"))
	if e := frame(); !e.Asked || !e.AskedAt.IsZero() {
		t.Errorf("an Option: Asked %v at %v, want at once", e.Asked, e.AskedAt.Sub(now))
	}

	// another frame comes before the client is handed the first, which
	// was asked for at once
	w.askFrame(time.Time{})
	w.processEvent(frameEvent{FrameEvent: FrameEvent{Now: now, Size: image.Pt(100, 80)}})
	if e := frame(); !e.Asked || !e.AskedAt.IsZero() {
		t.Errorf("a frame replacing an asked-for one the client never had: Asked %v at %v", e.Asked, e.AskedAt)
	}
}

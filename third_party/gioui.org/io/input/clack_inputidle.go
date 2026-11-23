// SPDX-License-Identifier: Unlicense OR MIT

package input

import (
	"sync/atomic"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
)

// Clack patch 33 (CLACK_PATCHES.md): when a router was last given input.
// Clack holds work that would stall the window (a forced collection) for
// a pause in the person's input, and a pause is no pointer, key or text
// input at all, wherever it goes: a focused widget's keys, a grabbed
// drag, a popover drawn with op.Defer and a second window's input never
// reach a handler at the window's root. Frames are no sign of input
// either: a focused editor's caret redraws the window twice a second for
// as long as it's focused.

var (
	inputEpoch = time.Now()
	inputAt    atomic.Int64 // monotonic ns since inputEpoch, +1; 0 = none yet
	pressHook  atomic.Pointer[func(mouse bool)]
)

// noteInput records e when it's the person's input (Queue), and tells
// the press hook of a pointer press and a key press.
func noteInput(e event.Event) {
	switch e := e.(type) {
	case pointer.Event:
		if e.Kind == pointer.Cancel {
			return // the router's or the window's own, not input
		}
		if e.Kind == pointer.Press {
			pressed(true)
		}
	case key.Event:
		if e.State == key.Press {
			pressed(false)
		}
	case key.EditEvent:
	default:
		return
	}
	inputAt.Store(int64(time.Since(inputEpoch)) + 1)
}

func pressed(mouse bool) {
	if f := pressHook.Load(); f != nil {
		(*f)(mouse)
	}
}

// SetPressHook makes f see every pointer press (mouse true) and key press
// (mouse false) any router in the process is queued, wherever it goes, on
// the goroutine that queues it (a window's event loop), before it's
// routed. nil stops it. f must not block: it holds the window's events.
// Clack tells the realtime socket of the person's activity with it, as
// Slack's client does on mousedown and keydown.
func SetPressHook(f func(mouse bool)) {
	if f == nil {
		pressHook.Store(nil)
		return
	}
	pressHook.Store(&f)
}

// SinceInput is how long ago any router in the process was queued a
// pointer, key or text-edit event, or how long the process has run when
// none was. It reads the monotonic clock, so a step of the wall clock
// moves nothing. Any goroutine.
func SinceInput() time.Duration {
	now := time.Since(inputEpoch)
	t := inputAt.Load()
	if t == 0 {
		return now
	}
	return now - time.Duration(t-1)
}

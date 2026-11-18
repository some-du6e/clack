// SPDX-License-Identifier: Unlicense OR MIT

package input

import (
	"sync/atomic"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
)

// Clack patch 33 (CLACK_PATCHES.md): when a router was last given input,
// and how many presses it was given.
//
// Clack holds work that would stall the window (a forced collection) for
// a pause in the person's input, and a pause is no pointer, key or text
// input at all, wherever it goes: a focused widget's keys, a grabbed
// drag, a popover drawn with op.Defer and a second window's input never
// reach a handler at the window's root. Frames are no sign of input
// either: a focused editor's caret redraws the window twice a second for
// as long as it's focused. A message menu pick a plugin answers late runs
// only if no press came since (internal/ui/popups' maPick).

var (
	inputEpoch   = time.Now()
	inputAt      atomic.Int64  // monotonic ns since inputEpoch, +1; 0 = none yet
	inputPresses atomic.Uint64 // pointer presses, key presses and text edits so far
)

// noteInput records e when it's the person's input (Queue).
func noteInput(e event.Event) {
	switch e := e.(type) {
	case pointer.Event:
		if e.Kind == pointer.Cancel {
			return // the router's or the window's own, not input
		}
		if e.Kind == pointer.Press {
			inputPresses.Add(1)
		}
	case key.Event:
		if e.State == key.Press {
			inputPresses.Add(1)
		}
	case key.EditEvent:
		inputPresses.Add(1)
	default:
		return
	}
	inputAt.Store(int64(time.Since(inputEpoch)) + 1)
}

// Presses counts the pointer presses, key presses and text edits any
// router in the process was queued: a person acted between two reads when
// it moved. Moves, scrolls and releases aren't acts. Any goroutine.
func Presses() uint64 { return inputPresses.Load() }

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

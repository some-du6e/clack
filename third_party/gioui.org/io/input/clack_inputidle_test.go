// SPDX-License-Identifier: Unlicense OR MIT

package input

import (
	"testing"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
)

// Clack patch 33: a router's queued pointer, key and edit events count as
// input wherever they go; its own cancels and focus changes don't.
func TestSinceInput(t *testing.T) {
	var r Router
	time.Sleep(30 * time.Millisecond)
	r.Queue(pointer.Event{Kind: pointer.Cancel}, key.FocusEvent{Focus: true})
	if d := SinceInput(); d < 25*time.Millisecond {
		t.Fatalf("a cancel and a focus change counted as input (%v ago)", d)
	}
	for _, e := range []event.Event{
		key.Event{Name: key.NameDownArrow, State: key.Press},
		key.EditEvent{Text: "a"},
		pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse},
	} {
		time.Sleep(30 * time.Millisecond)
		r.Queue(e)
		if d := SinceInput(); d >= 25*time.Millisecond {
			t.Errorf("%T queued but the last input is %v ago", e, d)
		}
	}
}

// Clack patch 33: pointer presses, key presses and text edits count as
// presses; moves, scrolls, releases, cancels and focus changes don't.
func TestSinceInputPresses(t *testing.T) {
	var r Router
	n := Presses()
	r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse},
		pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse},
		pointer.Event{Kind: pointer.Cancel},
		key.Event{Name: key.NameDownArrow, State: key.Release},
		key.FocusEvent{Focus: true},
	)
	if d := Presses() - n; d != 0 {
		t.Fatalf("moves, scrolls, releases, a cancel and a focus change: %d presses", d)
	}
	r.Queue(
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary},
		key.Event{Name: key.NameDownArrow, State: key.Press},
		key.EditEvent{Text: "a"},
	)
	if d := Presses() - n; d != 3 {
		t.Errorf("a pointer press, a key press and an edit: %d presses, want 3", d)
	}
}

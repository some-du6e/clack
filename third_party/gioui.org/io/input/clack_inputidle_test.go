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

// Clack patch 33's press hook sees each pointer press and key press a
// router is queued, and nothing else: a move, a release, a scroll, a
// key's release, an edit, a cancel.
func TestPressHook(t *testing.T) {
	var got []bool
	SetPressHook(func(mouse bool) { got = append(got, mouse) })
	defer SetPressHook(nil)
	var r Router
	r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse},
		pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse},
		pointer.Event{Kind: pointer.Cancel},
		key.Event{Name: key.NameDownArrow, State: key.Press},
		key.Event{Name: key.NameDownArrow, State: key.Release},
		key.EditEvent{Text: "a"},
	)
	if len(got) != 2 || !got[0] || got[1] {
		t.Errorf("the press hook saw %v, want [true false] (the mouse press, the key press)", got)
	}
	SetPressHook(nil)
	r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse})
	if len(got) != 2 {
		t.Errorf("a nil hook still saw a press: %v", got)
	}
}

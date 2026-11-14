// SPDX-License-Identifier: Unlicense OR MIT

package input

import (
	"image"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/op"
)

func sourceEvents(s Source, filters ...event.Filter) []event.Event {
	var out []event.Event
	for {
		e, ok := s.Event(filters...)
		if !ok {
			return out
		}
		out = append(out, e)
	}
}

// Clack patch 21: a HoverOnly Source reports the pointer's Enter and
// Move over a button but drops its Press and Release, so nothing clicks;
// the dropped events are gone for every later reader too.
func TestHoverOnlyDropsClicks(t *testing.T) {
	var ops op.Ops
	var r Router
	btn := new(int)
	f := addPointerHandler(&r, &ops, btn, image.Rect(0, 0, 100, 100))
	r.Frame(&ops)
	p := f32.Pt(50, 50)
	r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: p},
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32.Pt(60, 50)},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: f32.Pt(60, 50)},
	)
	got := sourceEvents(r.Source().HoverOnly(), f)
	assertEventPointerTypeSequence(t, got, pointer.Enter, pointer.Move)
	if rest := sourceEvents(r.Source(), f); len(rest) != 0 {
		t.Errorf("events left for a plain Source after a HoverOnly read: %v", rest)
	}
}

// A HoverOnly Source still scrolls.
func TestHoverOnlyScrolls(t *testing.T) {
	var ops op.Ops
	var r Router
	list := new(int)
	f := pointer.Filter{Target: list, Kinds: pointer.Scroll, ScrollY: pointer.ScrollRange{Min: -100, Max: 100}}
	events(&r, -1, f)
	addPointerHandler(&r, &ops, list, image.Rect(0, 0, 100, 100))
	events(&r, -1, f)
	r.Frame(&ops)
	r.Queue(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: f32.Pt(50, 50), Scroll: f32.Pt(0, 20)})
	got := sourceEvents(r.Source().HoverOnly(), f)
	if len(got) != 1 {
		t.Fatalf("HoverOnly scroll events = %v, want one Scroll", got)
	}
	if e := got[0].(pointer.Event); e.Kind != pointer.Scroll || e.Scroll.Y != 20 {
		t.Errorf("got %v, want a Scroll of 20", e)
	}
}

// A HoverOnly Source keeps the focus: the focused editor still reads as
// focused, but the keys and text typed into it are dropped (a plain
// Source over the same setup gets both).
func TestHoverOnlyKeepsFocusDropsKeys(t *testing.T) {
	ed := new(int)
	f := []event.Filter{key.FocusFilter{Target: ed}, key.Filter{Focus: ed, Name: "A"}}
	typed := []event.Event{key.Event{Name: "A", State: key.Press}, key.EditEvent{Text: "a"}}
	setup := func() *Router {
		r := new(Router)
		frame := func() {
			events(r, -1, f...)
			var ops op.Ops
			event.Op(&ops, ed)
			r.Frame(&ops)
		}
		frame()
		r.Source().Execute(key.FocusCmd{Tag: ed})
		frame()
		frame()
		r.Queue(typed...)
		return r
	}
	plain := setup()
	assertEventSequence(t, sourceEvents(plain.Source(), f...), typed...)

	r := setup()
	h := r.Source().HoverOnly()
	if !h.Focused(ed) || !h.Enabled() {
		t.Errorf("HoverOnly: Focused = %v, Enabled = %v, want both", h.Focused(ed), h.Enabled())
	}
	assertEventSequence(t, sourceEvents(h, f...))
	if rest := sourceEvents(r.Source(), f...); len(rest) != 0 {
		t.Errorf("events left after a HoverOnly read: %v", rest)
	}
}

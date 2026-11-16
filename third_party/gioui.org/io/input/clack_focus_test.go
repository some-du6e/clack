// SPDX-License-Identifier: Unlicense OR MIT

package input

import (
	"image"
	"testing"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/op"
	"gioui.org/op/clip"
)

// Clack patch 34: FocusTag is the focused tag; CanFocus says whether a
// tag drawn with a focus filter last frame would keep the focus, and
// AskedFocus whether one asked in the frame in progress; Within
// follows the clip areas: a field drawn inside a layer's area is within
// the layer, one beside it isn't, and one drawn from a deferred macro is
// within only what that macro draws around it.
func TestFocusTagCanFocusWithin(t *testing.T) {
	var r Router
	layer, field, beside, inner := new(int), new(int), new(int), new(int)
	frame := func(drawField bool) {
		var ops op.Ops
		for _, tag := range []event.Tag{layer, field, beside, inner} {
			events(&r, -1, key.FocusFilter{Target: tag})
		}
		lc := clip.Rect(image.Rect(0, 0, 200, 200)).Push(&ops)
		event.Op(&ops, layer)
		if drawField {
			fc := clip.Rect(image.Rect(10, 10, 100, 30)).Push(&ops)
			event.Op(&ops, field)
			fc.Pop()
		}
		// a deferred macro inside the layer: its ops run from the root
		m := op.Record(&ops)
		ic := clip.Rect(image.Rect(20, 40, 60, 60)).Push(&ops)
		event.Op(&ops, inner)
		ic.Pop()
		op.Defer(&ops, m.Stop())
		lc.Pop()
		bc := clip.Rect(image.Rect(300, 0, 400, 50)).Push(&ops)
		event.Op(&ops, beside)
		bc.Pop()
		r.Frame(&ops)
	}
	frame(true)
	s := r.Source()
	if s.FocusTag() != nil {
		t.Fatalf("FocusTag with nothing focused = %v", s.FocusTag())
	}
	s.Execute(key.FocusCmd{Tag: field})
	if s.FocusTag() != field {
		t.Errorf("FocusTag after focusing the field = %v", s.FocusTag())
	}
	for _, c := range []struct {
		what           string
		tag, container event.Tag
		within         bool
	}{
		{"the field in the layer", field, layer, true},
		{"the layer in itself", layer, layer, true},
		{"the layer in the field", layer, field, false},
		{"the tag beside the layer", beside, layer, false},
		{"the deferred tag", inner, layer, false},
		{"nil", nil, layer, false},
	} {
		if got := s.Within(c.tag, c.container); got != c.within {
			t.Errorf("Within(%s) = %v, want %v", c.what, got, c.within)
		}
	}
	if !s.CanFocus(field) || !s.CanFocus(layer) || s.CanFocus(new(int)) || s.CanFocus(nil) {
		t.Errorf("CanFocus: field %v, layer %v, an unknown tag %v, nil %v; want true, true, false, false",
			s.CanFocus(field), s.CanFocus(layer), s.CanFocus(new(int)), s.CanFocus(nil))
	}
	// AskedFocus: this frame's asks, not the last frame's
	if s.AskedFocus(field) {
		t.Error("AskedFocus(field) before anything asked in this frame")
	}
	events(&r, -1, key.FocusFilter{Target: field})
	if !s.AskedFocus(field) || s.AskedFocus(beside) || s.AskedFocus(nil) {
		t.Errorf("AskedFocus: field %v (asked), beside %v (not yet), nil %v; want true, false, false",
			s.AskedFocus(field), s.AskedFocus(beside), s.AskedFocus(nil))
	}
	frame(false) // the field isn't drawn: Gio drops its focus, and it can't take it back
	if s.CanFocus(field) {
		t.Error("CanFocus(field) after a frame that didn't draw it")
	}
	if s.FocusTag() != nil {
		t.Errorf("FocusTag after the focused field went = %v, want nil", s.FocusTag())
	}
	if s.Disabled().FocusTag() != nil || s.Disabled().CanFocus(layer) || s.Disabled().AskedFocus(layer) || s.Disabled().Within(layer, layer) {
		t.Error("a disabled Source answers")
	}
}

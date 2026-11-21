// SPDX-License-Identifier: Unlicense OR MIT

package input

import (
	"image"
	"testing"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/op"
	"gioui.org/op/clip"
)

// Clack patch 53: a tag's area, in window coordinates, and the part of it
// its clip areas show.
func TestClackAreaBounds(t *testing.T) {
	var ops op.Ops
	var r Router
	tag := new(int)
	other := new(int)
	// a 100x50 viewport at (10, 20); in it, scrolled, a 60x40 tile at
	// (30, 40) of the viewport, so 10px of its height is below the viewport
	off := op.Offset(image.Pt(10, 20)).Push(&ops)
	vp := clip.Rect(image.Rect(0, 0, 100, 50)).Push(&ops)
	in := op.Offset(image.Pt(30, 20)).Push(&ops)
	tile := clip.Rect(image.Rect(0, 0, 60, 40)).Push(&ops)
	event.Op(&ops, tag)
	tile.Pop()
	in.Pop()
	vp.Pop()
	off.Pop()
	for {
		if _, ok := r.Event(pointer.Filter{Target: tag, Kinds: pointer.Press}); !ok {
			break
		}
	}
	r.Frame(&ops)
	full, shown, ok := r.AreaBounds(tag)
	if !ok {
		t.Fatal("no area for the tag")
	}
	if want := image.Rect(40, 40, 100, 80); full != want {
		t.Errorf("full %v, want %v", full, want)
	}
	if want := image.Rect(40, 40, 100, 70); shown != want {
		t.Errorf("shown %v, want %v (the viewport ends at y 70)", shown, want)
	}
	if _, _, ok := r.AreaBounds(other); ok {
		t.Error("a tag with no area has bounds")
	}
	if _, _, ok := r.Source().AreaBounds(tag); !ok {
		t.Error("Source.AreaBounds didn't reach the router")
	}
}

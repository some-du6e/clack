// SPDX-License-Identifier: Unlicense OR MIT

package input

import (
	"image"
	"testing"

	"gioui.org/io/event"
	"gioui.org/op"
	"gioui.org/op/clip"
)

// Clack patch 57: a frame that moves an area read through FollowArea asks
// for the frame that sees it there; one that leaves it in place, or moves
// an area nothing follows, asks for none.
func TestClackFollowArea(t *testing.T) {
	var r Router
	tag, other := new(int), new(int)
	frame := func(at image.Point, tags ...event.Tag) {
		var ops op.Ops
		for _, tg := range tags {
			off := op.Offset(at).Push(&ops)
			cl := clip.Rect(image.Rect(0, 0, 60, 20)).Push(&ops)
			event.Op(&ops, tg)
			cl.Pop()
			off.Pop()
		}
		r.Frame(&ops)
	}
	wakes := func() bool {
		_, w := r.WakeupTime()
		return w
	}
	frame(image.Pt(10, 10), tag, other)
	wakes()
	// laid out where it was: no frame
	if full, _, ok := r.FollowArea(tag); !ok || full != image.Rect(10, 10, 70, 30) {
		t.Fatalf("FollowArea %v %v", full, ok)
	}
	frame(image.Pt(10, 10), tag, other)
	if wakes() {
		t.Error("an area that stayed put asked for a frame")
	}
	// moved by the frame that read it (a block inserted above): a frame now
	r.FollowArea(tag)
	frame(image.Pt(10, 40), tag, other)
	if tm, w := r.WakeupTime(); !w || !tm.IsZero() {
		t.Errorf("a moved area asked for a frame %v at %v, want one now", w, tm)
	}
	// that frame reads it where it is, and lays it out there: nothing more
	if full, _, _ := r.FollowArea(tag); full != image.Rect(10, 40, 70, 60) {
		t.Errorf("the next read %v, want the moved area", full)
	}
	frame(image.Pt(10, 40), tag, other)
	if wakes() {
		t.Error("the read that caught up asked for another frame")
	}
	// a read is for one frame: later moves, and moves of areas nothing
	// follows, ask for nothing
	frame(image.Pt(10, 70), tag, other)
	if wakes() {
		t.Error("a move nothing read asked for a frame")
	}
	r.FollowArea(tag)
	frame(image.Pt(10, 70), tag)
	if wakes() {
		t.Error("another tag's area going asked for a frame")
	}
	// the area going, and coming back
	r.FollowArea(tag)
	frame(image.Pt(10, 70), other)
	if !wakes() {
		t.Error("a followed area that went asked for no frame")
	}
	if _, _, ok := r.FollowArea(tag); ok {
		t.Error("a tag with no area has bounds")
	}
	frame(image.Pt(10, 70), tag)
	if !wakes() {
		t.Error("a followed area that came back asked for no frame")
	}
	if _, _, ok := r.Source().FollowArea(tag); !ok {
		t.Error("Source.FollowArea didn't reach the router")
	}
}

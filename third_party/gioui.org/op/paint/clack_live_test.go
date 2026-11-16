// SPDX-License-Identifier: Unlicense OR MIT

package paint

import (
	"image"
	"testing"
)

// testLive is a LiveImage showing whichever of its images cur says.
type testLive struct {
	ops []ImageOp
	cur int
}

func (l *testLive) Image() ImageOp { return l.ops[l.cur] }

// Clack patch 35: a live ImageOp has its image's size, and its handle
// answers the image the live one shows now, with that image's own key.
func TestLiveImageOp(t *testing.T) {
	a := NewImageOp(image.NewRGBA(image.Rect(0, 0, 4, 3)))
	b := NewImageOp(image.NewRGBA(image.Rect(0, 0, 4, 3)))
	l := &testLive{ops: []ImageOp{a, b}}
	op := NewLiveImageOp(l)
	if op.Size() != image.Pt(4, 3) {
		t.Fatalf("size %v, want the image's 4×3", op.Size())
	}
	h, ok := op.handle.(liveHandle)
	if !ok {
		t.Fatalf("handle %T, want a live one", op.handle)
	}
	if src, k := h.LiveImage(); src != a.src || k != a.handle {
		t.Error("before it moved on: not the first image")
	}
	l.cur = 1
	if src, k := h.LiveImage(); src != b.src || k != b.handle {
		t.Error("after it moved on: not the second image")
	}
	if e := NewLiveImageOp(&testLive{ops: []ImageOp{{}}}); e.src != nil || e.handle != nil {
		t.Error("a live image with no image now isn't an empty ImageOp")
	}
}

// SPDX-License-Identifier: Unlicense OR MIT

package clip

import (
	"image"
	"testing"

	"gioui.org/internal/ops"
	"gioui.org/op"
)

// Clack patch 38: a rounded rectangle's interior is the columns between
// its arcs, a pixel in from each edge; only a filled outline carries it in
// the ops, right before its clip.
func TestRRectInterior(t *testing.T) {
	r := image.Rect(10, 20, 110, 70)
	for _, c := range []struct {
		rr   RRect
		want image.Rectangle
	}{
		{UniformRRect(r, 6), image.Rect(17, 21, 103, 69)},
		{RRect{Rect: r, NE: 12, SE: 12}, image.Rect(11, 21, 97, 69)},
		{RRect{Rect: r, NW: 4, SW: 9, NE: 2, SE: 0}, image.Rect(20, 21, 107, 69)},
		{UniformRRect(image.Rect(0, 0, 20, 20), 10), image.Rectangle{}}, // a circle: all arcs
		{UniformRRect(image.Rect(0, 0, 40, 2), 1), image.Rectangle{}},   // too thin
	} {
		if got := c.rr.Interior(); got != c.want {
			t.Errorf("%+v: interior %v, want %v", c.rr, got, c.want)
		}
	}
	interiors := func(build func(o *op.Ops)) []image.Rectangle {
		var o op.Ops
		build(&o)
		var rd ops.Reader
		rd.Reset(&o.Internal)
		var out []image.Rectangle
		for e, ok := rd.Decode(); ok; e, ok = rd.Decode() {
			if ops.OpType(e.Data[0]) == ops.TypeClipInterior {
				out = append(out, ops.DecodeClipInterior(e.Data))
			}
		}
		return out
	}
	rr := UniformRRect(r, 6)
	if got := interiors(func(o *op.Ops) { rr.Push(o).Pop() }); len(got) != 1 || got[0] != rr.Interior() {
		t.Errorf("an RRect clip wrote interiors %v, want [%v]", got, rr.Interior())
	}
	if got := interiors(func(o *op.Ops) { Stroke{Path: rr.Path(o), Width: 2}.Op().Push(o).Pop() }); len(got) != 0 {
		t.Errorf("a stroke of an RRect wrote interiors %v: its inside isn't covered", got)
	}
	if got := interiors(func(o *op.Ops) { Ellipse(r).Push(o).Pop() }); len(got) != 0 {
		t.Errorf("an ellipse wrote interiors %v", got)
	}
}

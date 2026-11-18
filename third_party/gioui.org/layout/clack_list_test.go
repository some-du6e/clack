// SPDX-License-Identifier: Unlicense OR MIT

package layout

import (
	"image"
	"testing"

	"gioui.org/op"
)

// Clack patch 45: a ScrollToEnd list with StartAligned lays out content
// shorter than itself from its start, and still keeps to its end once the
// content outgrows it.
func TestListStartAligned(t *testing.T) {
	gtx := Context{Ops: new(op.Ops), Constraints: Exact(image.Pt(20, 100))}
	el := func(gtx Context, i int) Dimensions { return Dimensions{Size: image.Pt(20, 10)} }
	l := List{Axis: Vertical, ScrollToEnd: true, StartAligned: true}
	for _, pass := range []string{"first layout", "at its end"} {
		l.Layout(gtx, 3, el)
		if p := l.Position; p.First != 0 || p.Offset != 0 || p.OffsetLast != 70 || p.BeforeEnd {
			t.Errorf("3 items in 100px, %s: %+v, want the first at the top, 70px after the last, the list at its end", pass, p)
		}
	}
	l.Layout(gtx, 15, el)
	if p := l.Position; p.First+p.Count != 15 || p.OffsetLast != 0 || p.BeforeEnd {
		t.Errorf("15 items in 100px: %+v, want the last at the bottom", p)
	}
	// without it, short content is end aligned, as upstream
	e := List{Axis: Vertical, ScrollToEnd: true}
	e.Layout(gtx, 3, el)
	if p := e.Position; p.Offset != -70 {
		t.Errorf("end aligned: offset %d, want -70", p.Offset)
	}
}

// SPDX-License-Identifier: Unlicense OR MIT

package layout

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
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

// Clack patch 50: ScrollDelta is what the list's scrolling moved it in
// the last Layout, and nothing when Position or a child's size did.
func TestListScrollDelta(t *testing.T) {
	r := new(input.Router)
	gtx := Context{Ops: new(op.Ops), Constraints: Exact(image.Pt(20, 100)), Source: r.Source()}
	size := 10
	el := func(gtx Context, i int) Dimensions { return Dimensions{Size: image.Pt(20, size)} }
	var l List
	l.Axis = Vertical
	l.Layout(gtx, 30, el)
	if d := l.ScrollDelta(); d != 0 {
		t.Errorf("first layout: %d, want 0", d)
	}
	r.Frame(gtx.Ops)
	r.Queue(
		pointer.Event{Source: pointer.Mouse, Kind: pointer.Move, Position: f32.Pt(5, 5)},
		pointer.Event{Source: pointer.Mouse, Kind: pointer.Scroll, Position: f32.Pt(5, 5), Scroll: f32.Pt(0, 7)},
	)
	gtx.Ops.Reset()
	l.Layout(gtx, 30, el)
	if d := l.ScrollDelta(); d != 7 {
		t.Errorf("a 7px wheel: %d, want 7", d)
	}
	r.Frame(gtx.Ops)
	gtx.Ops.Reset()
	size = 20 // the children grow
	l.Position.First += 2
	l.Layout(gtx, 30, el)
	if d := l.ScrollDelta(); d != 0 {
		t.Errorf("after Position and the sizes changed: %d, want 0", d)
	}
}

// Clack patch 50: StopScroll ends a running wheel notch's curve (patch
// 9): the layouts after it move the list no more.
func TestListStopScroll(t *testing.T) {
	r := new(input.Router)
	t0 := time.Unix(1700000000, 0)
	gtx := Context{Ops: new(op.Ops), Constraints: Exact(image.Pt(20, 100)), Source: r.Source()}
	el := func(gtx Context, i int) Dimensions { return Dimensions{Size: image.Pt(20, 10)} }
	run := func(stop bool) (mid, after int) {
		var l List
		l.Axis = Vertical
		frame := func(at time.Duration) int {
			gtx.Now = t0.Add(at)
			gtx.Ops.Reset()
			l.Layout(gtx, 100, el)
			r.Frame(gtx.Ops)
			return l.ScrollDelta()
		}
		frame(0)
		r.Queue(
			pointer.Event{Source: pointer.Mouse, Kind: pointer.Move, Position: f32.Pt(5, 5)},
			pointer.Event{Source: pointer.Mouse, Kind: pointer.Scroll, Position: f32.Pt(5, 5), Scroll: f32.Pt(0, 120), Notched: true},
		)
		frame(16 * time.Millisecond) // the notch starts its curve
		mid = frame(32 * time.Millisecond)
		if stop {
			l.StopScroll()
		}
		after = frame(64 * time.Millisecond)
		return mid, after
	}
	if mid, after := run(false); mid <= 0 || after <= 0 {
		t.Fatalf("a notch's curve moved the list %d then %d px; want it moving over both frames", mid, after)
	}
	if _, after := run(true); after != 0 {
		t.Errorf("after StopScroll the curve moved the list %d px; want none", after)
	}
}

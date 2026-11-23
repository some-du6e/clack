//go:build !stockgio

package richview

// The code block's copy button shows "Copied!" for copiedFor, and every
// frame while it does asks for the frame that reverts it: asked only in
// the click's frame, a frame in between asking for an earlier wakeup of
// its own (the tooltip's delay) replaced the window's timer, and the check
// stayed until something else drew.
// Not built for the stock Gio check (input.ExecuteHook is Clack patch 10).

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	rt "github.com/some-du6e/clack/internal/richtext"
)

func TestCodeCopiedAsksAgain(t *testing.T) {
	v := New(doc(rt.Block{Kind: rt.Preformatted, Spans: []rt.Span{txt("synthetic code")}}), Options{})
	st := newTestStyle()
	var r input.Router
	var ops op.Ops
	now := time.Unix(1_800_000_000, 0)
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: &ops, Now: now, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(400, 1000))}
		v.Layout(gtx, st, &recHandler{})
		r.Frame(&ops)
	}
	frame()
	ci := -1
	for i := range v.d.codes {
		if v.d.codes[i].block {
			ci = i
		}
	}
	if ci < 0 {
		t.Fatal("no code block")
	}
	br := v.copyButton(&v.d.codes[ci])
	p := f32.Pt(float32(br.Min.X+br.Dx()/2), float32(br.Min.Y+br.Dy()/2))
	r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p, Time: 500 * time.Millisecond})
	frame()
	r.Queue(press(p, time.Second), release(p, time.Second+20*time.Millisecond))
	frame()
	frame()
	if !v.copiedNow(ci, now) {
		t.Fatalf("the copy button took no click (copied %d)", v.copied)
	}
	want := v.copiedAt.Add(copiedFor)
	var at []time.Time
	input.ExecuteHook = func(c input.Command) {
		if inv, ok := c.(op.InvalidateCmd); ok {
			at = append(at, inv.At)
		}
	}
	defer func() { input.ExecuteHook = nil }()
	now = now.Add(100 * time.Millisecond)
	frame()
	for _, a := range at {
		if a.Equal(want) {
			return
		}
	}
	t.Errorf("a frame while Copied! shows asked for %v, not its end at %v", at, want)
}

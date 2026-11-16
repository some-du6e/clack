package ui

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/inputidle"
)

// Input, not frames, is what cmd/clack's trim waits on for a pause
// (inputidle): the window-wide pass-through handler counts a wheel scroll
// as input and leaves all of it to the scrollable under it.
func TestRootSeesScrollWithoutTakingIt(t *testing.T) {
	f := &navFrame{a: navTestApp(t)}
	var list int
	var got float32
	frame := func() {
		f.ops.Reset()
		gtx := layout.Context{Ops: &f.ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(800, 600)), Source: f.r.Source(), Now: time.Now()}
		f.a.readRootPointer(gtx)
		for {
			e, ok := gtx.Event(pointer.Filter{Target: &list, Kinds: pointer.Scroll, ScrollY: pointer.ScrollRange{Min: -10000, Max: 10000}})
			if !ok {
				break
			}
			if pe, ok := e.(pointer.Event); ok && pe.Kind == pointer.Scroll {
				got += pe.Scroll.Y
			}
		}
		area := clip.Rect{Max: image.Pt(800, 600)}.Push(gtx.Ops)
		event.Op(gtx.Ops, &list)
		area.Pop()
		f.a.rootPointer(gtx) // above everything, as App.frame registers it
		f.r.Frame(&f.ops)
	}
	frame()
	frame()
	t0 := time.Now()
	f.r.Queue(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: f32.Pt(400, 300), Scroll: f32.Pt(0, 120)})
	frame()
	frame()
	if since := inputidle.Since(); since > time.Since(t0) {
		t.Errorf("the scroll wasn't noted as input (the last input was %v ago, the scroll %v)", since, time.Since(t0))
	}
	if got != 120 {
		t.Errorf("the scrollable got %v of the 120 scrolled: the root took some", got)
	}
}

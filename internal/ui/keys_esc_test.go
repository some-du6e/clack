package ui

import (
	"image"
	"testing"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/shortcut"
)

// A focused widget that claims plain Escape (shortcut.OwnEscape: a Lists
// cell, its editor, the list search) gets it; without the claim the
// shell's "Close / mark as read" binding, read first in the frame, takes it.
func TestEscapeOwners(t *testing.T) {
	a := navTestApp(t)
	var r input.Router
	var ops op.Ops
	var w int
	claim := false
	got := 0
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(400, 300)), Source: r.Source(), Now: time.Now()}
		a.keys.process(gtx, a)
		if claim {
			shortcut.OwnEscape(&w)
		}
		for {
			e, ok := gtx.Event(key.FocusFilter{Target: &w}, key.Filter{Focus: &w, Name: key.NameEscape})
			if !ok {
				break
			}
			if ke, ok := e.(key.Event); ok && ke.State == key.Press {
				got++
			}
		}
		area := clip.Rect{Max: image.Pt(400, 300)}.Push(gtx.Ops)
		event.Op(gtx.Ops, &w)
		area.Pop()
		r.Frame(&ops)
	}
	esc := func() {
		r.Queue(key.Event{Name: key.NameEscape, State: key.Press}, key.Event{Name: key.NameEscape, State: key.Release})
		frame()
		frame()
	}
	frame()
	r.Source().Execute(key.FocusCmd{Tag: &w})
	frame()
	frame()
	esc()
	if got != 0 {
		t.Fatal("Escape reached an unclaimed widget before the shell's binding")
	}
	claim = true
	frame()
	esc()
	if got != 1 {
		t.Fatalf("the claiming widget got %d Escapes", got)
	}
}

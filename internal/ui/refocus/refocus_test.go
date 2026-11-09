package refocus

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
)

// Restore gives the focus back only when it is nowhere or still on the
// closed modal's tags, and only once.
func TestRestore(t *testing.T) {
	Reset()
	var r input.Router
	var ops op.Ops
	var field, modal, other int
	now := time.Now()
	draw := map[*int]bool{&field: true, &modal: false, &other: true}
	frame := func(before func(gtx layout.Context)) {
		ops.Reset()
		gtx := layout.Context{Ops: &ops, Source: r.Source(), Now: now, Constraints: layout.Exact(image.Pt(100, 100))}
		if before != nil {
			before(gtx)
		}
		if Take(gtx, &field) {
			gtx.Execute(key.FocusCmd{Tag: &field})
		}
		for tag, on := range draw {
			if !on {
				continue
			}
			for {
				if _, ok := gtx.Event(key.FocusFilter{Target: tag}); !ok {
					break
				}
			}
			st := clip.Rect{Max: image.Pt(10, 10)}.Push(gtx.Ops)
			event.Op(gtx.Ops, tag)
			st.Pop()
		}
		Note(gtx, &field)
		r.Frame(&ops)
		now = now.Add(16 * time.Millisecond)
	}
	focus := func(tag *int) { frame(func(gtx layout.Context) { gtx.Execute(key.FocusCmd{Tag: tag}) }); frame(nil) }

	focus(&field)
	if Last(now) != &field {
		t.Fatal("the focused field isn't Last")
	}
	draw[&modal] = true
	focus(&modal) // a modal opens and takes the focus
	draw[&modal] = false
	Restore(&field, &modal) // it closes
	frame(nil)
	frame(nil)
	if !r.Source().Focused(&field) {
		t.Fatal("the field didn't get the focus back")
	}
	if Pending(&field) {
		t.Fatal("the request is still pending")
	}

	// something else took the focus meanwhile: no restore
	draw[&modal] = true
	focus(&modal)
	draw[&modal] = false
	focus(&other)
	Restore(&field, &modal)
	frame(nil)
	frame(nil)
	if !r.Source().Focused(&other) {
		t.Fatal("Restore took the focus from what had it")
	}
}

// SPDX-License-Identifier: Unlicense OR MIT

package input

import (
	"image"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/op"
	"gioui.org/op/clip"
)

// Clack patch 45: ResetPointers starts every handler's pointer state over
// where the pointer is. Frames in which a and b ask for nothing (a frame
// drawn again from its old ops, as Clack's inspect tool freezes one) let
// the pointer go from a to b without either hearing of it: without the
// reset a stays hovered for good. The reset gives both a Cancel and b an
// Enter, at once although the tool's handler already read its events in
// that frame, and the router goes on from there: no second Enter for b,
// and leaving b for a is a Leave and an Enter as usual. A pointer that
// left the window gets no Enter.
func TestResetPointers(t *testing.T) {
	hover := pointer.Enter | pointer.Leave | pointer.Cancel // as gesture.Hover asks
	type env struct {
		r          *Router
		a, b, tool *int
		fa, fb, ft pointer.Filter
		read       func(f pointer.Filter) []event.Event
		draw, move func()
	}
	setup := func(t *testing.T) *env {
		e := &env{r: new(Router), a: new(int), b: new(int), tool: new(int)}
		e.fa = pointer.Filter{Target: e.a, Kinds: hover}
		e.fb = pointer.Filter{Target: e.b, Kinds: hover}
		e.ft = pointer.Filter{Target: e.tool, Kinds: hover | pointer.Move}
		e.read = func(f pointer.Filter) []event.Event { return events(e.r, -1, f) }
		e.draw = func() {
			var ops op.Ops
			area := func(tag event.Tag, rect image.Rectangle) {
				defer clip.Rect(rect).Push(&ops).Pop()
				event.Op(&ops, tag)
			}
			area(e.a, image.Rect(0, 0, 100, 100))
			area(e.b, image.Rect(100, 0, 200, 100))
			pp := pointer.PassOp{}.Push(&ops) // the tool's window-wide area, over both
			area(e.tool, image.Rect(0, 0, 200, 100))
			pp.Pop()
			e.r.Frame(&ops)
		}
		at := func(x float32) {
			e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(x, 50)})
		}
		// a hovered, everyone reading
		e.read(e.fa)
		e.read(e.fb)
		e.read(e.ft)
		e.draw()
		at(50)
		assertEventPointerTypeSequence(t, e.read(e.fa), pointer.Enter)
		assertEventPointerTypeSequence(t, e.read(e.fb))
		e.read(e.ft)
		e.draw()
		// frozen: only the tool reads, and the pointer goes over b
		e.read(e.ft)
		e.draw()
		at(150)
		assertEventPointerTypeSequence(t, e.read(e.ft), pointer.Move)
		e.draw()
		e.read(e.ft)
		e.move = func() { at(50) }
		return e
	}

	t.Run("without", func(t *testing.T) {
		e := setup(t)
		// a and b read again, with no reset: neither hears the move
		assertEventPointerTypeSequence(t, e.read(e.fa))
		assertEventPointerTypeSequence(t, e.read(e.fb))
		e.draw()
		if got := e.read(e.fa); len(got) != 0 {
			t.Errorf("a without the reset: %v", pointerTypes(got))
		}
		if got := e.read(e.fb); len(got) != 0 {
			t.Errorf("b without the reset: %v; the drift this patch is for is gone", pointerTypes(got))
		}
	})

	t.Run("reset", func(t *testing.T) {
		e := setup(t)
		e.r.Source().ResetPointers()
		assertEventPointerTypeSequence(t, e.read(e.fa), pointer.Cancel)
		assertEventPointerTypeSequence(t, e.read(e.fb), pointer.Cancel, pointer.Enter)
		assertEventPointerTypeSequence(t, e.read(e.ft), pointer.Cancel, pointer.Enter)
		e.draw()
		assertEventPointerTypeSequence(t, e.read(e.fa))
		assertEventPointerTypeSequence(t, e.read(e.fb))
		e.read(e.ft)
		e.draw()
		e.move()
		assertEventPointerTypeSequence(t, e.read(e.fa), pointer.Enter)
		assertEventPointerTypeSequence(t, e.read(e.fb), pointer.Leave)
	})

	t.Run("left the window", func(t *testing.T) {
		e := setup(t)
		e.r.Queue(pointer.Event{Kind: pointer.Leave, Source: pointer.Mouse, Position: f32.Pt(250, 50)})
		e.read(e.ft)
		e.r.Source().ResetPointers()
		assertEventPointerTypeSequence(t, e.read(e.fa), pointer.Cancel)
		assertEventPointerTypeSequence(t, e.read(e.fb), pointer.Cancel)
	})
}

// SPDX-License-Identifier: Unlicense OR MIT

package input

import (
	"fmt"
	"image"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/op"
	"gioui.org/op/clip"
)

// Clack patch 49: ResetPointers starts every handler's pointer state over
// where the pointer is. Frames in which a, b and c ask for nothing (a
// frame drawn again from its old ops, as Clack's inspect tool freezes one)
// let the pointer go from a to b without any of them hearing of it:
// without the reset a stays hovered for good and b never knows it is.
//
// The reset sends what the router would have sent had they been reading,
// at once although the tool's handler already read its events in that
// frame, every event with the mouse's own PointerID (Windows numbers it 1;
// gesture.Hover ignores an event of another pointer):
//   - a, which last read an Enter and isn't under the pointer: a Cancel and
//     a Leave (a tracker that clears its hover only on Leave, as Clack's
//     message bodies and plugin trees do, needs the Leave);
//   - b, under the pointer: a Cancel and an Enter;
//   - c, under it too but asking only for Move and Leave (a list's row
//     hover): the Move;
//   - the tool's own area: a Cancel, an Enter and a Move.
//
// The router goes on from there: no second Enter at the frame's end, and
// leaving b for a is a Leave and an Enter as usual. A mouse that left the
// window, pressed, gets no Enter anywhere, and a gets its Leave.
func TestResetPointers(t *testing.T) {
	hover := pointer.Enter | pointer.Leave | pointer.Cancel // as gesture.Hover asks
	rows := pointer.Press | pointer.Release | pointer.Move | pointer.Leave | pointer.Scroll
	type env struct {
		r              *Router
		a, b, c, tool  *int
		fa, fb, fc, ft pointer.Filter
		id             pointer.ID
		read           func(f pointer.Filter) []event.Event
		draw           func()
		at             func(x float32)
	}
	setup := func(t *testing.T, id pointer.ID) *env {
		e := &env{r: new(Router), a: new(int), b: new(int), c: new(int), tool: new(int), id: id}
		e.fa = pointer.Filter{Target: e.a, Kinds: hover}
		e.fb = pointer.Filter{Target: e.b, Kinds: hover}
		e.fc = pointer.Filter{Target: e.c, Kinds: rows}
		e.ft = pointer.Filter{Target: e.tool, Kinds: hover | pointer.Move | pointer.Press | pointer.Release}
		e.read = func(f pointer.Filter) []event.Event { return events(e.r, -1, f) }
		e.draw = func() {
			var ops op.Ops
			area := func(tag event.Tag, rect image.Rectangle, pass bool) {
				if pass {
					defer pointer.PassOp{}.Push(&ops).Pop()
				}
				defer clip.Rect(rect).Push(&ops).Pop()
				event.Op(&ops, tag)
			}
			area(e.a, image.Rect(0, 0, 100, 100), false)
			area(e.b, image.Rect(100, 0, 200, 100), false)
			area(e.c, image.Rect(100, 0, 200, 100), true)  // over b, letting b have it too
			area(e.tool, image.Rect(0, 0, 200, 100), true) // the tool's window-wide area, over all
			e.r.Frame(&ops)
		}
		e.at = func(x float32) {
			e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, PointerID: id, Position: f32.Pt(x, 50)})
		}
		// a hovered, everyone reading
		for _, f := range []pointer.Filter{e.fa, e.fb, e.fc, e.ft} {
			e.read(f)
		}
		e.draw()
		e.at(50)
		assertEventPointerTypeSequence(t, e.read(e.fa), pointer.Enter)
		assertEventPointerTypeSequence(t, e.read(e.fb))
		assertEventPointerTypeSequence(t, e.read(e.fc))
		e.read(e.ft)
		e.draw()
		// frozen: only the tool reads, and the pointer goes over b and c
		e.read(e.ft)
		e.draw()
		e.at(150)
		assertEventPointerTypeSequence(t, e.read(e.ft), pointer.Move)
		e.draw()
		e.read(e.ft)
		return e
	}
	// ids checks that every event is the mouse's.
	ids := func(t *testing.T, who string, evs []event.Event, id pointer.ID) {
		t.Helper()
		for _, ev := range evs {
			if pe, ok := ev.(pointer.Event); ok && pe.PointerID != id {
				t.Errorf("%s got a %v of pointer %d, want the mouse's %d", who, pe.Kind, pe.PointerID, id)
			}
		}
	}

	t.Run("without", func(t *testing.T) {
		e := setup(t, 1)
		// a, b and c read again, with no reset: none hears the move
		for _, f := range []pointer.Filter{e.fa, e.fb, e.fc} {
			assertEventPointerTypeSequence(t, e.read(f))
		}
		e.draw()
		if got := e.read(e.fa); len(got) != 0 {
			t.Errorf("a without the reset: %v", pointerTypes(got))
		}
		if got := e.read(e.fb); len(got) != 0 {
			t.Errorf("b without the reset: %v; the drift this patch is for is gone", pointerTypes(got))
		}
	})

	// Windows numbers the mouse 1; macOS and X11 leave it 0
	for _, id := range []pointer.ID{1, 0} {
		t.Run(fmt.Sprintf("reset, mouse %d", id), func(t *testing.T) {
			e := setup(t, id)
			e.r.Source().ResetPointers()
			ga, gb, gc, gt := e.read(e.fa), e.read(e.fb), e.read(e.fc), e.read(e.ft)
			assertEventPointerTypeSequence(t, ga, pointer.Cancel, pointer.Leave)
			assertEventPointerTypeSequence(t, gb, pointer.Cancel, pointer.Enter)
			assertEventPointerTypeSequence(t, gc, pointer.Move)
			assertEventPointerTypeSequence(t, gt, pointer.Cancel, pointer.Enter, pointer.Move)
			for who, evs := range map[string][]event.Event{"a": ga, "b": gb, "c": gc, "the tool": gt} {
				ids(t, who, evs, id)
			}
			if len(ga) == 2 {
				if p := ga[1].(pointer.Event).Position; p != f32.Pt(150, 50) {
					t.Errorf("a's Leave at %v, want where the pointer is, 150,50", p)
				}
			}
			e.draw()
			for _, f := range []pointer.Filter{e.fa, e.fb, e.fc} {
				assertEventPointerTypeSequence(t, e.read(f))
			}
			e.read(e.ft)
			e.draw()
			e.at(50)
			ga, gb = e.read(e.fa), e.read(e.fb)
			assertEventPointerTypeSequence(t, ga, pointer.Enter)
			assertEventPointerTypeSequence(t, gb, pointer.Leave)
			ids(t, "a", ga, id)
			ids(t, "b", gb, id)
		})
	}

	t.Run("left the window, pressed", func(t *testing.T) {
		e := setup(t, 1)
		// pressed over b (the tool's area takes the press), then out of
		// the window there: the pointer stays in the router's state,
		// pressed, its last event a Leave over b
		e.r.Queue(
			pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, PointerID: 1, Buttons: pointer.ButtonPrimary, Position: f32.Pt(150, 50)},
			pointer.Event{Kind: pointer.Leave, Source: pointer.Mouse, PointerID: 1, Buttons: pointer.ButtonPrimary, Position: f32.Pt(150, 50)},
		)
		e.read(e.ft)
		e.r.Source().ResetPointers()
		assertEventPointerTypeSequence(t, e.read(e.fb), pointer.Cancel)
		assertEventPointerTypeSequence(t, e.read(e.fc))
		assertEventPointerTypeSequence(t, e.read(e.fa), pointer.Cancel, pointer.Leave)
	})
}

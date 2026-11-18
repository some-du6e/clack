// SPDX-License-Identifier: Unlicense OR MIT

package gesture

import (
	"fmt"
	"image"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/op"
	"gioui.org/op/clip"
)

// Clack patch 49: two gesture.Hover widgets side by side, under a
// window-wide pass-through area the way Clack's inspect tool lays its own
// over the window. The pointer rests on a, then the window freezes: its
// frames are drawn again from the last ops and nothing lays out, so
// neither Hover reads its events while the pointer goes over b. When the
// freeze ends (Source.ResetPointers) a is no longer hovered and b is, for
// Windows' mouse (PointerID 1: Hover drops a Cancel or Leave of another
// pointer) as for macOS' and X11's (0).
func TestHoverResetPointers(t *testing.T) {
	for _, id := range []pointer.ID{1, 0} {
		t.Run(fmt.Sprintf("mouse %d", id), func(t *testing.T) {
			var r input.Router
			var a, b Hover
			tool := new(int)
			toolF := pointer.Filter{Target: tool, Kinds: pointer.Enter | pointer.Leave | pointer.Move | pointer.Cancel}
			frame := func(laidOut bool) {
				src := r.Source()
				var ops op.Ops
				area := func(tag event.Tag, rect image.Rectangle) {
					defer clip.Rect(rect).Push(&ops).Pop()
					event.Op(&ops, tag)
				}
				if laidOut {
					a.Update(src)
					b.Update(src)
				}
				area(&a, image.Rect(0, 0, 100, 100))
				area(&b, image.Rect(100, 0, 200, 100))
				for {
					if _, ok := src.Event(toolF); !ok {
						break
					}
				}
				pp := pointer.PassOp{}.Push(&ops)
				area(tool, image.Rect(0, 0, 200, 100))
				pp.Pop()
				r.Frame(&ops)
			}
			hovered := func(h *Hover) bool { return h.Update(input.Source{}) } // reads nothing
			move := func(x float32) {
				r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, PointerID: id, Position: f32.Pt(x, 50)})
			}
			frame(true)
			move(50)
			frame(true)
			if !hovered(&a) || hovered(&b) {
				t.Fatalf("the pointer on a: a hovered %v, b %v", hovered(&a), hovered(&b))
			}
			// frozen
			frame(false)
			move(150)
			frame(false)
			frame(false)
			r.Source().ResetPointers()
			frame(true)
			if hovered(&a) {
				t.Error("a, which the pointer left while frozen, is still hovered after the reset")
			}
			if !hovered(&b) {
				t.Error("b, under the pointer, isn't hovered after the reset")
			}
			// and on from there: back onto a
			move(50)
			frame(true)
			if !hovered(&a) || hovered(&b) {
				t.Errorf("back on a: a hovered %v, b %v", hovered(&a), hovered(&b))
			}
		})
	}
}

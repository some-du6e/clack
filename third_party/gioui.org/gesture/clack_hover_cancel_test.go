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

// Clack patch 60: a Hover the pointer leaves in a frame that doesn't lay
// it out (a message's gutter time, drawn only while its row is hovered)
// never reads the Leave: the router drops it with the handler. When the
// Hover is laid out again its first event is the new handler's Cancel,
// which names pointer 0. The hover must end there for Windows' mouse
// (PointerID 1) as for macOS' and X11's (0).
func TestClackHoverEndsOnResetCancel(t *testing.T) {
	for _, id := range []pointer.ID{0, 1} {
		t.Run(fmt.Sprintf("mouse %d", id), func(t *testing.T) {
			var r input.Router
			var h Hover
			frame := func(laidOut bool) bool {
				var ops op.Ops
				hov := false
				if laidOut {
					hov = h.Update(r.Source())
					a := clip.Rect(image.Rect(0, 0, 100, 100)).Push(&ops)
					event.Op(&ops, &h)
					a.Pop()
				}
				r.Frame(&ops)
				return hov
			}
			move := func(x float32) {
				r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, PointerID: id, Position: f32.Pt(x, 50)})
			}
			frame(true)
			move(50)
			frame(true)
			if !frame(true) {
				t.Fatal("control: the pointer on the area, the Hover isn't hovered")
			}
			// the pointer leaves in a frame that doesn't lay the Hover out
			move(150)
			frame(false)
			if frame(true) {
				t.Error("laid out again with the pointer away, the Hover is still hovered (its handler's reset Cancel was dropped)")
			}
			// and it hovers again when the pointer comes back
			move(50)
			frame(true)
			if !frame(true) {
				t.Error("back on the area, the Hover isn't hovered")
			}
		})
	}
}

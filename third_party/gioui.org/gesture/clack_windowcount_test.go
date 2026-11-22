// SPDX-License-Identifier: Unlicense OR MIT

package gesture

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/internal/clickrule"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/op"
	"gioui.org/op/clip"
)

// Clack patch 48: a window counts its clicks whatever widget each lands
// on, as Chromium does. Blink takes a press's count from the platform's
// event (EventHandler::HandleMousePressEvent: SetClickCount(
// mouse_event.click_count)) and resets it for no element; the platform
// counts by time and place alone. So the composer's + pressed, its menu's
// scrim pressed on the same spot, then the + again is a triple click: the
// + doesn't see a double click.
func TestClackClickWindowCount(t *testing.T) {
	withRule(t, clickrule.Default, nil)
	var plus, scrim Click
	var r input.Router
	at := f32.Pt(50, 50)
	got := map[*Click][]int{}
	read := func() {
		for _, c := range []*Click{&plus, &scrim} {
			for {
				e, ok := c.Update(r.Source())
				if !ok {
					break
				}
				if e.Kind == KindClick {
					got[c] = append(got[c], e.NumClicks)
				}
			}
		}
	}
	frame := func(withScrim bool) {
		read()
		var ops op.Ops
		st := clip.Rect(image.Rect(0, 0, 100, 100)).Push(&ops)
		plus.Add(&ops)
		st.Pop()
		if withScrim {
			st := clip.Rect(image.Rect(0, 0, 200, 200)).Push(&ops)
			scrim.Add(&ops)
			st.Pop()
		}
		r.Frame(&ops)
	}
	click := func(tm time.Duration) {
		r.Queue(
			pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: at, Time: tm},
			pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: at, Time: tm + 30*time.Millisecond})
	}
	frame(false)
	click(sec) // the +: its menu opens
	frame(true)
	click(sec + 150*time.Millisecond) // the menu's scrim over the +
	frame(false)
	click(sec + 300*time.Millisecond) // the + again
	frame(false)
	read()
	if p, s := got[&plus], got[&scrim]; !sameInts(p, []int{1, 3}) || !sameInts(s, []int{2}) {
		t.Errorf("+, scrim, +: the + %v, the scrim %v; want [1 3] and [2], the window's count", p, s)
	}
	// a press elsewhere, then the + inside the double-click time: the
	// platform's rule decides, by place (2 px on Linux), not the widget
	got = map[*Click][]int{}
	r.Queue(
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32.Pt(150, 150), Time: 3 * sec},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: f32.Pt(150, 150), Time: 3*sec + 30*time.Millisecond})
	frame(false)
	click(3*sec + 100*time.Millisecond)
	frame(false)
	read()
	if p := got[&plus]; !sameInts(p, []int{1}) {
		t.Errorf("a press 100 px off, then the +: %v, want [1]", p)
	}
}

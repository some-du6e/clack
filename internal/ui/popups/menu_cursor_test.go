package popups

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
)

// Synthetic fixtures only.

// Over a menu the pointer is Slack's, whatever draws under it: the
// default arrow on its panel (.c-menu), the hand on an item
// (.c-menu_item__button{cursor:pointer}). Under a chart's More actions
// menu the message's text cursor showed through (Karim, α.160).
func TestMenuCursors(t *testing.T) {
	h, _, _ := emBooted(t)
	m := NewMenuOpts(h, []MenuItem{{Label: "Copy table", Do: func() {}}, {Label: "Download table", Do: func() {}}},
		MenuOpts{Anchor: image.Rect(100, 100, 132, 132), Popover: "bottom-left"})
	var r input.Router
	var ops op.Ops
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: &ops, Source: r.Source(), Now: time.Now(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(1200, 800))}
		// what's under the menu: a view with the text cursor all over
		s := clip.Rect{Max: image.Pt(1200, 800)}.Push(gtx.Ops)
		pointer.CursorText.Add(gtx.Ops)
		s.Pop()
		m.Layout(gtx)
		r.Frame(&ops)
	}
	frame()
	frame()
	at := func(p image.Point) pointer.Cursor {
		r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(p.X), float32(p.Y))})
		frame()
		return r.Cursor()
	}
	if len(m.rows) != 2 || m.rect.Empty() {
		t.Fatalf("menu rect %v rows %v", m.rect, m.rows)
	}
	row := m.rows[0]
	if c := at(row.Min.Add(row.Size().Div(2))); c != pointer.CursorPointer {
		t.Errorf("an item: cursor %v, want the hand", c)
	}
	if c := at(image.Pt(m.rect.Min.X+20, m.rect.Min.Y+3)); c != pointer.CursorDefault {
		t.Errorf("the panel's padding: cursor %v, want the default arrow", c)
	}
}

package popups

import (
	"image"
	"reflect"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

// An email chip's tooltip hover passes the pointer on to the chip: a
// click on the chip's remove button (its last 20px, 4px in from the
// right) removes the address. The hover was drawn over the chip, not
// pass-through, so the button never got the press.
func TestInviteChipRemoveClick(t *testing.T) {
	h, _ := invEnv(t)
	m := OpenInviteModal(h, InviteFromTeamMenu)
	var r input.Router
	ops := new(op.Ops)
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(1440, 900)), Now: time.Now()}
		m.Layout(gtx)
		r.Frame(ops)
	}
	m.SetText("one@x.example, two@x.example,")
	frame()
	frame()
	if len(m.chips) != 2 {
		t.Fatalf("%d chips, want 2", len(m.chips))
	}
	chip, _, ok := r.AreaBounds(&m.chips[0].hv)
	if !ok {
		t.Fatal("the first chip has no hover area")
	}
	p := f32.Pt(float32(chip.Max.X-4-10)+.5, float32(chip.Min.Y+chip.Dy()/2)+.5)
	r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: p},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p},
	)
	frame()
	frame()
	if got := m.Emails(); !reflect.DeepEqual(got, []string{"two@x.example"}) {
		t.Errorf("after a click on the first chip's remove button: %q, want [two@x.example]", got)
	}
}

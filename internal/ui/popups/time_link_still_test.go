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
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// A time's tooltip shows for a pointer that came in and stopped: the
// frame the Enter arrives in starts the 300 ms delay and asks for the
// frame that shows it; nothing else moves, so no other frame comes.
func TestTimeLinkTipForAStillPointer(t *testing.T) {
	th := theme.New(false)
	var tl TimeLink
	var r input.Router
	var ops op.Ops
	now := time.Unix(1000, 0)
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: &ops, Source: r.Source(), Now: now, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(300, 200))}
		st := op.Offset(image.Pt(20, 60)).Push(gtx.Ops)
		timeLinkLayoutT(&tl, gtx, th, image.Pt(60, 16), 12, func() string { return "Today at 3:04:05 PM" })
		st.Pop()
		r.Frame(&ops)
	}
	frame()
	r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(40, 68)})
	frame() // the Enter's frame; then nothing moves
	w, ok := r.WakeupTime()
	if !ok || w.After(now.Add(TimeLinkTipDelay+50*time.Millisecond)) {
		t.Fatalf("the frame the pointer came in asked for no frame by the tooltip's delay (wakeup %v %v, now %v)", w, ok, now)
	}
	now = w
	frame()
	if !tl.TipShowing(now) {
		t.Error("with the pointer resting on the time, its tooltip didn't show")
	}
}

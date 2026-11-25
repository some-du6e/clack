package messages

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

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// A failed send's footer (MessageResend) wraps as Slack's does: in a
// narrow thread pane "Slack couldn’t send this message", Try again and
// Cancel take more lines and stay inside it, and a click on Cancel there
// removes the failed copy. It drew on one line, Cancel past the pane's
// edge where nothing could click it.
func TestFailedFooterWraps(t *testing.T) {
	th := theme.New(false)
	gtx0 := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	s := textStyle{size: 13, lineH: 20}
	ls := textStyle{size: 13, weight: theme.Bold, lineH: 20}
	full := measure(gtx0, th, s, i18n.T("message", "Slack couldn’t send this message")).X + 6 +
		measure(gtx0, th, ls, i18n.T("message", "Try again")).X + measure(gtx0, th, s, " | ").X +
		measure(gtx0, th, ls, i18n.T("message", "Cancel")).X + 12
	for _, w := range []int{full + 40, full - 30, 150} {
		discarded := 0
		r := &row{}
		rc := &rowCtx{th: th, c: colorsFor(th), onDiscard: func(*row) { discarded++ }}
		var rt input.Router
		var h int
		frame := func() {
			ops := new(op.Ops)
			gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Now: time.Now(),
				Constraints: layout.Constraints{Max: image.Pt(w, 400)}, Source: rt.Source()}
			h = r.failedLine(gtx, rc, 0, 0, w)
			rt.Frame(ops)
		}
		frame()
		pcs := failedFooterLayout(gtx0, th, s, ls, w)
		lines := 0
		var cancel image.Rectangle
		for _, p := range pcs {
			if p.r.Max.X > w {
				t.Errorf("width %d: %q ends at %d, past the pane", w, p.txt, p.r.Max.X)
			}
			lines = max(lines, p.l+1)
			if p.cancel {
				cancel = p.r
			}
		}
		if wantH := lines*20 + 4; h != wantH {
			t.Errorf("width %d: the footer is %dpx for %d lines, want %d", w, h, lines, wantH)
		}
		if w >= full+40 && lines != 1 {
			t.Errorf("width %d: %d lines with room for one", w, lines)
		}
		if w < full && lines < 2 {
			t.Errorf("width %d: one line too narrow for it (needs %d)", w, full)
		}
		c := f32.Pt(float32(cancel.Min.X+cancel.Max.X)/2, float32(cancel.Min.Y+cancel.Max.Y)/2)
		rt.Queue(pointer.Event{Kind: pointer.Move, Position: c, Source: pointer.Mouse},
			pointer.Event{Kind: pointer.Press, Position: c, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary},
			pointer.Event{Kind: pointer.Release, Position: c, Source: pointer.Mouse})
		frame()
		frame()
		if discarded != 1 {
			t.Errorf("width %d: a click on Cancel at %v removed the failed copy %d times, want once", w, c, discarded)
		}
	}
}

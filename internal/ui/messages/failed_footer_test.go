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
		lines := (h - 4) / 20
		if w >= full+40 && lines != 1 {
			t.Errorf("width %d: %d lines (%dpx) with room for one", w, lines, h)
		}
		if w < full && lines < 2 {
			t.Errorf("width %d: %dpx, one line too narrow for it (it needs %d)", w, h, full)
		}
		// Cancel is somewhere inside the pane: a click there removes the
		// failed copy (each spot clicked in turn, Try again's too)
		found := false
		for y := 2; y < h && !found; y += 3 {
			for x := 2; x < w && !found; x += 3 {
				c := f32.Pt(float32(x), float32(y))
				rt.Queue(pointer.Event{Kind: pointer.Move, Position: c, Source: pointer.Mouse},
					pointer.Event{Kind: pointer.Press, Position: c, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary},
					pointer.Event{Kind: pointer.Release, Position: c, Source: pointer.Mouse})
				frame()
				frame()
				found = discarded > 0
			}
		}
		if !found {
			t.Errorf("width %d: nothing inside the pane is Cancel: the failed copy can't be removed", w)
		}
		if discarded > 1 {
			t.Errorf("width %d: one click removed it %d times", w, discarded)
		}
	}
}

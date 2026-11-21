package blockkit

// The slim toggle line's caret is an atomic inline (inline-flex): when the
// plan's title fits its line but the caret doesn't, the caret breaks onto
// a line of its own and the bar grows a line, instead of running past the
// block's edge where its clip hides it.

import (
	"image"
	"testing"
	"time"

	"gioui.org/font"
	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestAgentToggleCaretBreaks(t *testing.T) {
	h := &agentHost{th: theme.New(false)}
	gtx0 := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	titleW := measure(gtx0, h.th, 13, font.Normal, "Thinking completed") // plan-complete's title
	height := func(w int) int {
		m := agentMsg(t, "plan-complete")
		me := MessageElements(h, &m)
		v := richview.New(m.Doc, richview.Options{Elements: me})
		st := styleFor(h.th, nil)
		var r input.Router
		var hgt int
		for i := 0; i < 2; i++ {
			ops := new(op.Ops)
			gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Now: time.Now(),
				Constraints: layout.Constraints{Max: image.Pt(w, 400)}, Source: r.Source()}
			v.Layout(gtx, st, &surfHandler{h: h})
			r.Frame(ops)
			if sl := v.ElementSlots(); len(sl) > 0 {
				hgt = sl[0].Dy()
			}
		}
		return hgt
	}
	roomy := height(480)
	tight := height(20 + titleW + 8) // the title fits, the caret's 4+13px don't
	if tight <= roomy {
		t.Errorf("caret without room: the bar is %dpx, as tall as with room (%dpx): want the caret on a line of its own", tight, roomy)
	}
}

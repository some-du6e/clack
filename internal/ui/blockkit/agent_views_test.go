package blockkit

// The same plan drawn in two places at once (a thread's root in the
// channel and in the thread pane): each drawing has its own toggle and
// its own height transition, as each of Slack's rendered PlanBlocks keeps
// its state. Opened in one, the other stays closed; open in both at
// different widths, each settles at its own height and the frames go
// quiet after the .25s transition (no relayout, no frame asked for).

import (
	"image"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestAgentPlanTwoViews(t *testing.T) {
	h := &agentHost{th: theme.New(false)}
	m := agentMsg(t, "plan-long-details")
	me := MessageElements(h, &m)
	a, b := twoDrawings(me)
	va := richview.New(m.Doc, richview.Options{Elements: a})
	vb := richview.New(m.Doc, richview.Options{Elements: b})
	st := styleFor(h.th, nil)
	hand := &surfHandler{h: h}
	var r input.Router
	now := time.Now().Add(time.Hour) // ahead of the measuring wall clock (TestAgentTaskShowMore)
	frame := func() {
		now = now.Add(100 * time.Millisecond)
		ops := new(op.Ops)
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Now: now,
			Constraints: layout.Exact(image.Pt(1000, 1400)), Source: r.Source()}
		for _, d := range []struct {
			v *richview.View
			x int
			w int
		}{{va, 20, 480}, {vb, 540, 300}} {
			off := op.Offset(image.Pt(d.x, 12)).Push(ops)
			g := gtx
			g.Constraints = layout.Constraints{Max: image.Pt(d.w, 1400)}
			d.v.Layout(g, st, hand)
			off.Pop()
		}
		r.Frame(ops)
	}
	slot := func(v *richview.View) int {
		sl := v.ElementSlots()
		if len(sl) == 0 {
			t.Fatal("no slot")
		}
		return sl[0].Dy()
	}
	frame()
	closed := slot(va)
	openIn(a, "plan\x00p5")
	for i := 0; i < 6; i++ {
		frame()
	}
	if ha, hb := slot(va), slot(vb); ha <= closed+40 || hb != closed {
		t.Errorf("opened in one view: %d there and %d in the other, want it open there and %d (closed) in the other", ha, hb, closed)
	}
	openIn(b, "plan\x00p5")
	for i := 0; i < 8; i++ {
		frame()
	}
	ha, hb := slot(va), slot(vb)
	if hb <= ha {
		t.Errorf("open in both: %d at 480px, %d at 300px, want the narrower one taller", ha, hb)
	}
	v0 := me.ElementsVersion()
	r.WakeupTime() // read (and clear) what the frames so far asked for
	frame()
	frame()
	if a2, b2 := slot(va), slot(vb); a2 != ha || b2 != hb {
		t.Errorf("settled heights moved: %d, %d then %d, %d", ha, hb, a2, b2)
	}
	if v := me.ElementsVersion(); v != v0 {
		t.Errorf("not quiet after the transition: the elements' version moved %d to %d (relayout every frame)", v0, v)
	}
	if at, ok := r.WakeupTime(); ok {
		t.Errorf("not quiet after the transition: a frame asked for another at %v", at.Sub(now))
	}
}

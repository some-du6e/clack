package blockkit

// A slim plan task's caret (TaskItem 3fJq): .taskHeaderSlim is a
// fit-content flex row (padding 4px 0, gap 4px) and .taskHeaderCaret sits
// align-self start with margin-top 6px, so its 13px box starts 10px into
// the header and centres on the title's x-height; a wrapped title takes
// the whole width, the caret after it (the header's box, and its hit
// area, is the full width).

import (
	"image"
	"testing"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// inkCenterY is the darkness-weighted row of the ink in r (on white).
func inkCenterY(img *image.RGBA, r image.Rectangle) (float64, bool) {
	var sum, wsum float64
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			c := img.RGBAAt(x, y)
			lum := (299*int(c.R) + 587*int(c.G) + 114*int(c.B)) / 1000
			if d := 255 - lum; d > 40 {
				sum += float64(d)
				wsum += float64(d) * float64(y)
			}
		}
	}
	if sum == 0 {
		return 0, false
	}
	return wsum / sum, true
}

func TestAgentTaskCaretPlace(t *testing.T) {
	h := &agentHost{th: theme.New(false)}
	now := time.Now().Add(time.Hour) // see TestAgentTaskShowMore
	shot := func(fx, block string) (*agentShot, *op.Ops) {
		m := agentMsg(t, fx)
		s := newAgentShot(h, m)
		st := s.rows[0].me.agentState()
		st.open["plan\x00"+block] = true
		st.open["task\x00"+block+"\x00t1"] = false // the header alone
		var ops *op.Ops
		for i := 0; i < 3; i++ {
			now = now.Add(100 * time.Millisecond)
			ops = s.frame(560, 300, now)
		}
		return s, ops
	}

	// wrapped: the header is the whole width (no GPU needed)
	s, _ := shot("plan-caret-wrap", "p9")
	sl := s.rows[0].v.ElementSlots()[0]
	cw := sl.Dx() - 20 // the step gutter (12px) and its 8px gap
	tp := s.rows[0].me.agentState().taps["task\x00p9\x00t1"]
	if tp == nil || tp.sz.X != cw {
		w := -1
		if tp != nil {
			w = tp.sz.X
		}
		t.Errorf("a wrapped title's header is %dpx wide, want the whole %dpx (the caret after the title's box)", w, cw)
	}

	// one line: the caret on the title's x-height (on a GPU)
	s, ops := shot("plan-caret", "p8")
	img := pixeltest.Render(t, 560, 300, ops)
	sl = s.rows[0].v.ElementSlots()[0].Add(s.rows[0].at)
	cx := sl.Min.X + 20
	gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	titleW := measure(gtx, h.th, 13, font.Normal, "run some scans")
	y0, y1 := sl.Min.Y+21, sl.Max.Y // under the plan's toggle line
	tc, ok1 := inkCenterY(img, image.Rect(cx, y0, cx+titleW, y1))
	cc, ok2 := inkCenterY(img, image.Rect(cx+titleW+3, y0, cx+titleW+4+13+2, y1))
	if !ok1 || !ok2 {
		t.Fatalf("no ink: title %v caret %v (slot %v)", ok1, ok2, sl)
	}
	if d := cc - tc; d < -2 || d > 2 {
		t.Errorf("caret centred at y %.1f, the title's x-height at %.1f (%+.1fpx): want the caret 10px into the header, on the title", cc, tc, d)
	}
}

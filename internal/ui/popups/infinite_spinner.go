package popups

import (
	"image/color"
	"math"
	"time"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// infiniteSpinner is Slack's InfiniteSpinner (client-boot xRE+) d px
// square at the default speed: in a 100-unit box the track (r 35, stroke
// 8, content-ter at .25) and two 55-unit arcs (stroke-dasharray 55 200,
// dashoffset 90, round caps) in col; the ring turns from -45° once every
// 1.2s on cubic-bezier(.25,.29,.54,.86), the tail on
// cubic-bezier(.41,.24,.64,.69). The app card's loading state draws it
// jumbo (52px) in blue (content-hgl-1).
func infiniteSpinner(gtx layout.Context, d int, col, track color.NRGBA) {
	sc := float32(d) / 100
	r := 35 * sc
	sw := 8 * sc
	ctr := f32.Pt(float32(d)/2, float32(d)/2)
	arc := func(a0, sweep float32, c color.NRGBA, round bool) {
		var p clip.Path
		p.Begin(gtx.Ops)
		p.MoveTo(f32.Pt(ctr.X+r*float32(math.Cos(float64(a0))), ctr.Y+r*float32(math.Sin(float64(a0)))))
		p.ArcTo(ctr, ctr, sweep)
		st := clip.Stroke{Path: p.End(), Width: sw}.Op().Push(gtx.Ops)
		paint.ColorOp{Color: c}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		st.Pop()
		if round { // stroke-linecap: round
			for _, a := range []float32{a0, a0 + sweep} {
				pt := f32.Pt(ctr.X+r*float32(math.Cos(float64(a))), ctr.Y+r*float32(math.Sin(float64(a))))
				var cp clip.Path
				cp.Begin(gtx.Ops)
				cp.MoveTo(pt.Add(f32.Pt(sw/2, 0)))
				cp.ArcTo(pt, pt, 2*math.Pi)
				cp.Close()
				cs := clip.Outline{Path: cp.End()}.Op().Push(gtx.Ops)
				paint.ColorOp{Color: c}.Add(gtx.Ops)
				paint.PaintOp{}.Add(gtx.Ops)
				cs.Pop()
			}
		}
	}
	track.A = uint8(float32(track.A)*.25 + .5)
	arc(0, 2*math.Pi-0.001, track, false)
	t := float64(gtx.Now.UnixMilli()%1200) / 1200
	circ := 2 * math.Pi * 35.0
	dash := float32(55 / circ * 2 * math.Pi)
	base := float32(-90/circ*2*math.Pi) - float32(math.Pi/4) // dashoffset 90, the ring's rotate(-45deg)
	arc(base+float32(2*math.Pi*cubicBezierY(.25, .29, .54, .86, t)), dash, col, true)
	arc(base+float32(2*math.Pi*cubicBezierY(.41, .24, .64, .69, t)), dash, col, true)
	if gtx.Source.Enabled() {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(33 * time.Millisecond)})
	}
}

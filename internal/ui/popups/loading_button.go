package popups

import (
	"image"
	"image/color"
	"math"
	"time"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Slack's LoadingButton (modern.vendor / boot-shared CEte), loading, as
// Edit profile's crop and remove panes draw Save and Yes, Remove Photo
// while their call runs (loading: on lV):
//
//   - The button is aria-disabled (disableWhenLoading), so ButtonGeneric
//     (bV0n) gives it c-button--disabled: background base-ter, no hover,
//     no pointer, not pressable.
//   - Its label is in span.c-button--loading_spinner--children, visually
//     hidden: clip-path inset(50%), 1px by 1px, position absolute. Out of
//     the flow, so the button is its min-width (c-button--medium: 80px,
//     height 36, padding 0 12px 1px) around the spinner alone.
//   - The spinner is InfiniteSpinner (9ezy) with size medium (16px:
//     --sk_infinite_spinner-size), color blue and noMargin (viewBox 0 0
//     78 78 instead of 0 0 100 100), margin-left 0
//     (c-button--loading_spinner--loading), centred by the button's flex.
//
// The legacy c-button only; nothing draws a v2 Button loading yet.

// loadingButtonGrp draws a c-button of size as LoadingButton draws it
// loading, at least minW dp wide; grp as buttonGrp's.
func loadingButtonGrp(gtx layout.Context, th *theme.Theme, c *widget.Clickable, size BtnSize, minW float32, grp grpSide) layout.Dimensions {
	var mods uint64 = 1 << BtnDisabled
	h, rad, pad, spin := float32(28), float32(4), float32(12), float32(12)
	if size == BtnMedium {
		mods |= 1 << 5
		h, rad, spin = 36, 8, 16
	} else {
		mods |= 1 << 4
	}
	st := buttonClass.Enter(gtx, css.StateDisabled, mods)
	defer st.Leave()
	if st.Hidden() {
		return layout.Dimensions{}
	}
	return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions {
		cl := colorsOf(th)
		s := dp(gtx, spin)
		w := max(dp(gtx, minW), 2*dp(gtx, pad)+s, gtx.Constraints.Min.X)
		sz := st.Size(gtx, image.Pt(w, dp(gtx, h)))
		rrectGrp(gtx, image.Rectangle{Max: sz}, dp(gtx, rad), grp, st.Background(cl.Disabled))
		// centred in the content box: the 1px bottom padding lifts it half
		// a pixel
		at(gtx, image.Pt((sz.X-s)/2, (sz.Y-dp(gtx, 1)-s)/2), func(gtx layout.Context) layout.Dimensions {
			kitSpinner(gtx, s, 78, codeTok(th, "content-ter"), codeTok(th, "content-hgl-1"))
			return layout.Dimensions{Size: image.Pt(s, s)}
		})
		return layout.Dimensions{Size: sz}
	})
}

// kitSpinner is Slack Kit's InfiniteSpinner in a d box whose SVG viewBox
// is box units (100, or 78 with noMargin): the track circle (r 35, stroke
// 8, the track colour at .25) and the arc (stroke-dasharray 55 200,
// dashoffset 90: the quarter from 12 to 3 o'clock, round caps) twice, the
// ring's and the tail's, each turning once per 1.2s on its own easing
// (cubic-bezier(.25,.29,.54,.86) and (.41,.24,.64,.69)). It keeps frames
// coming while it shows.
func kitSpinner(gtx layout.Context, d int, box float32, track, arc color.NRGBA) {
	sw := float32(d) * 8 / box
	rr := float32(d) * 35 / box
	ctr := f32.Pt(float32(d)/2, float32(d)/2)
	t := float64(gtx.Now.UnixMilli()%1200) / 1200
	track.A = uint8(float32(track.A)*0.25 + .5)
	ring := func(a0 float64, col color.NRGBA, sweep float64) { // Gio's strokes have round caps
		var p clip.Path
		p.Begin(gtx.Ops)
		p.MoveTo(f32.Pt(ctr.X+rr*float32(math.Cos(a0)), ctr.Y+rr*float32(math.Sin(a0))))
		p.ArcTo(ctr, ctr, float32(sweep))
		paint.FillShape(gtx.Ops, col, clip.Stroke{Path: p.End(), Width: sw}.Op())
	}
	// two half turns: one ArcTo of a full turn draws nothing
	ring(0, track, math.Pi)
	ring(math.Pi, track, math.Pi)
	q := 1.5 * math.Pi
	ring(q+2*math.Pi*cubicBezierY(.25, .29, .54, .86, t), arc, math.Pi/2)
	ring(q+2*math.Pi*cubicBezierY(.41, .24, .64, .69, t), arc, math.Pi/2)
	gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(16 * time.Millisecond)})
}

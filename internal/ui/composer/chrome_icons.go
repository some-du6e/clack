package composer

import (
	"image"
	"image/color"
	"math"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/icons"
)

// crIconEmojiHover is EmojiToolbarButton's hover (boot-shared FLB+): the
// :after disc (1em of the 18px button, slack-yellow or the skin tone's
// colour) under .is-second's SvgIcon emoji-filled at scale(1.05)
// rotate(15deg) about its centre, at the current origin in a px×px box.
func crIconEmojiHover(gtx layout.Context, px int, disc, ink color.NRGBA) {
	fillCircle(gtx, image.Pt(px/2, px/2), px/2, disc)
	ctr := f32.Pt(float32(px)/2, float32(px)/2)
	aff := f32.AffineId().Scale(ctr, f32.Pt(1.05, 1.05)).Rotate(ctr, 15*math.Pi/180)
	defer op.Affine(aff).Push(gtx.Ops).Pop()
	icons.Get("emoji-filled")(gtx, unit.Dp(float32(px)/gtx.Metric.PxPerDp), ink)
}

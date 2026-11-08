package messages

import (
	"image"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// tooltipClass is Slack's tooltip box.
var tooltipClass = slackc.Class("c-tooltip__tip c-tooltip__tip--top", "role", "tooltip")

// reactionTipDelay: Slack's reaction pills show who reacted after the same
// short rest as its emoji tooltips (richview.EmojiTipDelay).
const reactionTipDelay = 300 * time.Millisecond

// reactionTooltip is Slack's reaction tooltip: the emoji large on a light
// tile, then "Alice, Bob and you reacted with :tada:" wrapped,
// in the dark tooltip box centred above anchor.
func reactionTooltip(gtx layout.Context, h Host, th *theme.Theme, c *colors, txt, url string, anchor, bounds image.Point) {
	pad := dp(gtx, 12)
	tile, es := dp(gtx, 44), dp(gtx, 36)
	maxW := dp(gtx, 220)
	// Slack's tooltip is a portal under <body> (.c-tooltip__tip): a
	// tooltip rule styles it, a row's rules don't
	tip := slackc.BodyScope().Child(tooltipClass, 0)
	ts := cssText(tip, textStyle{size: 13, weight: theme.Bold, col: c.TooltipText, lineH: 18, maxLines: 8})

	// the text, wrapped at maxW, centred
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(maxW, 1<<20)}
	td := label(g, th, ts, txt)
	tcall := m.Stop()

	w := max(td.Size.X, tile) + 2*pad
	hgt := pad + tile + dp(gtx, 8) + td.Size.Y + pad
	x := min(max(anchor.X-w/2, dp(gtx, 4)), bounds.X-dp(gtx, 4)-w)
	y := anchor.Y - hgt
	if y < 0 {
		y = anchor.Y + dp(gtx, 40)
	}
	r := image.Rect(x, y, x+w, y+hgt)
	if o := tip.Opacity(); o < 1 {
		defer paint.PushOpacity(gtx.Ops, max(o, 0)).Pop()
	}
	rrect(gtx.Ops, r, dp(gtx, 8), tip.Background(c.Tooltip))
	tr := image.Rect(x+(w-tile)/2, y+pad, x+(w-tile)/2+tile, y+pad+tile)
	rrect(gtx.Ops, tr, dp(gtx, 8), rgb(0xffffff))
	drawEmoji(gtx, h, url, image.Rect(tr.Min.X+(tile-es)/2, tr.Min.Y+(tile-es)/2, tr.Min.X+(tile-es)/2+es, tr.Min.Y+(tile-es)/2+es))
	t := op.Offset(image.Pt(x+(w-td.Size.X)/2, tr.Max.Y+dp(gtx, 8))).Push(gtx.Ops)
	tcall.Add(gtx.Ops)
	t.Pop()
}

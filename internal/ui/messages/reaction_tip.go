package messages

import (
	"image"
	"image/color"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// tooltipClass is Slack's tooltip box.
var tooltipClass = slackc.Class("c-tooltip__tip c-tooltip__tip--top", "role", "tooltip")

// reactionTipDelay: Slack's reaction pills show who reacted after the same
// short rest as its emoji tooltips (richview.EmojiTipDelay).
const reactionTipDelay = 300 * time.Millisecond

// reactionTooltip is Slack's ReactionTip (boot-shared Qvn6) in its
// Tooltip (F2XN, position "top"), centred above anchor (6px over the
// pill: list.go):
//
//   - .c-tooltip__tip: padding 8px 12px, radius 8, 13px/1.25 bold, centred,
//     max-width 200px ("small"), 400px ("large") once the count passes 9;
//     margin-bottom 8px, and the arrow (a 9px square turned -45deg, its
//     corner 6px under the box) pointing at the pill.
//   - .c-reaction__tip--emoji: the emoji LARGER (one reaction in the
//     group: a 64px white tile, radius 4, padding 4, so a 56px emoji),
//     min-height 32, margin-bottom 4.
//   - the text: the names in the tip's bold, then TooltipSubtitle
//     ("reacted with :name:", regular weight, inverse.secondary) inline,
//     wrapped as one paragraph.
//
// txt is reactionTip's (whose count picks the width: tipCountFor).
func reactionTooltip(gtx layout.Context, h Host, th *theme.Theme, c *colors, txt, url string, anchor, bounds image.Point) {
	padX, padY := dp(gtx, 12), dp(gtx, 8)
	tile, es := dp(gtx, 64), dp(gtx, 56)
	maxW := dp(gtx, 200)
	if tipCountFor(txt) > 9 {
		maxW = dp(gtx, 400)
	}
	// Slack's tooltip is a portal under <body> (.c-tooltip__tip): a
	// tooltip rule styles it, a row's rules don't
	tip := slackc.BodyScope().Child(tooltipClass, 0)
	names, sub := splitReactionTip(txt)
	bold := cssText(tip, textStyle{size: 13, weight: theme.Bold, col: c.TooltipText, lineH: 16.25})
	reg := bold
	reg.weight, reg.col = theme.Regular, tooltipSubtitle(th, c, bold.col)
	runs := []tipRun{{bold, names}, {reg, sub}}

	// the text, wrapped at the box's content width, centred
	m := op.Record(gtx.Ops)
	td := flowTip(gtx, th, runs, maxW-2*padX)
	tcall := m.Stop()

	w := max(td.X, tile) + 2*padX
	gap := dp(gtx, 4)
	hgt := padY + tile + gap + td.Y + padY
	// the box's bottom 8px over the pill (anchor is 6px over it)
	bottom := anchor.Y + dp(gtx, 6) - dp(gtx, 8)
	x := min(max(anchor.X-w/2, dp(gtx, 4)), bounds.X-dp(gtx, 4)-w)
	y := bottom - hgt
	flip := y < 0
	if flip {
		y = anchor.Y + dp(gtx, 40)
	}
	r := image.Rect(x, y, x+w, y+hgt)
	if o := tip.Opacity(); o < 1 {
		defer paint.PushOpacity(gtx.Ops, max(o, 0)).Pop()
	}
	bg := tip.Background(c.Tooltip)
	var arrow theme.TooltipArrow
	if !flip {
		// the arrow's visible half: 6px under the box at the pill's centre
		arrow = theme.TooltipArrow{C: image.Pt(anchor.X, r.Max.Y), Half: dp(gtx, 6)}
	}
	// box and arrow, outlined together in dark mode (theme/tooltip.go)
	theme.TooltipShape(gtx, r, dp(gtx, 8), arrow, bg, tipGlow(th))
	tr := image.Rect(x+(w-tile)/2, y+padY, x+(w-tile)/2+tile, y+padY+tile)
	rrect(gtx.Ops, tr, dp(gtx, 4), rgb(0xffffff))
	drawEmoji(gtx, h, url, image.Rect(tr.Min.X+(tile-es)/2, tr.Min.Y+(tile-es)/2, tr.Min.X+(tile-es)/2+es, tr.Min.Y+(tile-es)/2+es))
	t := op.Offset(image.Pt(x+(w-td.X)/2, tr.Max.Y+gap)).Push(gtx.Ops)
	tcall.Add(gtx.Ops)
	t.Pop()
}

// lastTip is the reaction count of the tip reactionTip wrote last: the
// list sets its tooltip from reactionTip while laying the hovered row out
// and draws it in the same frame (UI goroutine only).
var lastTip struct {
	txt   string
	count int
}

// tipCountFor is the reaction count behind txt: reactionTip's, else the
// reactors it names.
func tipCountFor(txt string) int {
	if txt == lastTip.txt {
		return lastTip.count
	}
	names, _ := splitReactionTip(txt)
	return strings.Count(names, ",") + 1 + strings.Count(names, " and ")
}

// splitReactionTip cuts reactionTip's text before its subtitle ("reacted
// with :name:"): the names, then the subtitle.
func splitReactionTip(txt string) (names, sub string) {
	if i := strings.LastIndex(txt, reactionTipSubtitle); i >= 0 {
		return strings.TrimSpace(txt[:i]), txt[i:]
	}
	return txt, ""
}

// tipGlow is the dark mode tooltip outline (theme's TooltipGlow; none in
// light mode or without a theme).
func tipGlow(th *theme.Theme) color.NRGBA {
	if th == nil {
		return color.NRGBA{}
	}
	return th.C.TooltipGlow
}

// tooltipSubtitle is TooltipSubtitle's colour, inverse.secondary
// (--dt_color-content-inv-sec: #cbcccf on the light tip, #b9babd on the
// dark one; theme's TooltipSub).
func tooltipSubtitle(th *theme.Theme, c *colors, text color.NRGBA) color.NRGBA {
	if text != c.TooltipText {
		return text // a plugin's tooltip colour: the subtitle follows it
	}
	if th == nil {
		return theme.LightPalette.TooltipSub
	}
	return th.C.TooltipSub
}

// tipRun is one styled run of tooltip text.
type tipRun struct {
	st  textStyle
	txt string
}

// flowTip lays runs out as one inline paragraph at most maxW wide: words
// in their run's style, wrapped at spaces, each line centred on the
// widest (text-align: center); returns the size.
func flowTip(gtx layout.Context, th *theme.Theme, runs []tipRun, maxW int) image.Point {
	type word struct {
		st textStyle
		s  string
		w  int
	}
	type line struct {
		ws []word
		w  int
	}
	var lines []line
	cur := line{}
	for _, run := range runs {
		space := measure(gtx, th, run.st, "x x").X - measure(gtx, th, run.st, "xx").X
		for _, f := range strings.Fields(run.txt) {
			wd := word{st: run.st, s: f, w: measure(gtx, th, run.st, f).X}
			add := wd.w
			if len(cur.ws) > 0 {
				add += space
			}
			if len(cur.ws) > 0 && cur.w+add > maxW {
				lines = append(lines, cur)
				cur, add = line{}, wd.w
			}
			if len(cur.ws) > 0 {
				// the space before the word, in the word's run (the
				// subtitle's leading space is the names' trailing one)
				cur.ws = append(cur.ws, word{st: run.st, s: " ", w: space})
			}
			cur.ws = append(cur.ws, wd)
			cur.w += add
		}
	}
	if len(cur.ws) > 0 {
		lines = append(lines, cur)
	}
	lh := gtx.Sp(16.25)
	if len(runs) > 0 && runs[0].st.lineH > 0 {
		lh = gtx.Sp(unit.Sp(runs[0].st.lineH))
	}
	wmax := 0
	for _, l := range lines {
		wmax = max(wmax, l.w)
	}
	for i, l := range lines {
		x := (wmax - l.w) / 2
		for _, wd := range l.ws {
			if wd.s != " " {
				st := wd.st
				at(gtx, x, i*lh, func(gtx layout.Context) layout.Dimensions {
					return lineBox(gtx, st, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, st, wd.s) })
				})
			}
			x += wd.w
		}
	}
	return image.Pt(wmax, len(lines)*lh)
}

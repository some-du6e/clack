package messages

import (
	"image"
	"image/color"
	"strings"
	"time"
	"unicode"

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
// Tooltip (F2XN, position "top") for one reaction: txt is reactionTip's
// (whose count picks the width: tipCountFor), url its emoji.
func reactionTooltip(gtx layout.Context, h Host, th *theme.Theme, c *colors, txt, url string, anchor, bounds image.Point) {
	reactionGroupTooltip(gtx, h, th, c, &groupTip{lines: []string{txt}, urls: []string{url}, wide: tipCountFor(txt) > 9}, anchor, bounds)
}

// reactionGroupTooltip is Slack's ReactionTip (boot-shared Qvn6) in its
// Tooltip (F2XN, position "top") for a pill's group, centred above anchor
// (6px over the pill: list.go):
//
//   - .c-tooltip__tip: padding 8px 12px, radius 8, 13px/1.25 bold, centred,
//     max-width 200px ("small"), 400px ("large": Reaction's count past 9
//     or more than four reactions in the group); margin-bottom 8px, and
//     the arrow (a 9px square turned -45deg, its corner 6px under the
//     box) pointing at the pill.
//   - .c-reaction__tip--emoji: the group's emoji in rows of eight (lodash
//     chunk), each row a centred flex line with gap 4, min-height 32,
//     margin-bottom 4. One reaction is LARGER (a 64px white tile, radius
//     4, padding 4, so a 56px emoji); more are LARGE (36px tiles, padding
//     2: 32px emoji).
//   - the text: one .c-reaction__tip_group per reaction (16px between
//     them), each the names in the tip's bold, then TooltipSubtitle
//     ("reacted with :name:", regular weight, inverse.secondary) inline,
//     wrapped as one paragraph. Where the subtitle sits is the locale's
//     (getTip's rich strings: ja-JP's follows the name with no space).
func reactionGroupTooltip(gtx layout.Context, h Host, th *theme.Theme, c *colors, gt *groupTip, anchor, bounds image.Point) {
	padX, padY := dp(gtx, 12), dp(gtx, 8)
	maxW := dp(gtx, 200)
	if gt.wide {
		maxW = dp(gtx, 400)
	}
	// Slack's tooltip is a portal under <body> (.c-tooltip__tip): a
	// tooltip rule styles it, a row's rules don't
	tip := slackc.BodyScope().Child(tooltipClass, 0)
	bold := cssText(tip, textStyle{size: 13, weight: theme.Bold, col: c.TooltipText, lineH: 16.25})
	reg := bold
	reg.weight, reg.col = theme.Regular, tooltipSubtitle(th, c, bold.col)

	// the emoji rows
	tile, es := dp(gtx, 64), dp(gtx, 56)
	if len(gt.urls) > 1 {
		tile, es = dp(gtx, 36), dp(gtx, 32)
	}
	gap := dp(gtx, 4)
	rows := (len(gt.urls) + reactTipRow - 1) / reactTipRow
	rowH := max(tile, dp(gtx, 32))
	emojiW := 0
	for i := 0; i < rows; i++ {
		n := min(reactTipRow, len(gt.urls)-i*reactTipRow)
		emojiW = max(emojiW, n*tile+(n-1)*gap)
	}
	emojiH := rows * (rowH + gap)

	// the groups' text, each wrapped at the box's content width, centred
	type para struct {
		call op.CallOp
		size image.Point
	}
	paras := make([]para, 0, len(gt.lines))
	textW, textH := 0, 0
	for i, txt := range gt.lines {
		m := op.Record(gtx.Ops)
		td := flowTip(gtx, th, tipRuns(txt, bold, reg), maxW-2*padX)
		paras = append(paras, para{m.Stop(), td})
		textW = max(textW, td.X)
		textH += td.Y
		if i > 0 {
			textH += dp(gtx, 16) // .c-reaction__tip_group:not(:last-child) { margin-bottom: 16px }
		}
	}

	w := max(textW, emojiW) + 2*padX
	hgt := padY + emojiH + textH + padY
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
	ty := y + padY
	for i := 0; i < rows; i++ {
		urls := gt.urls[i*reactTipRow : min((i+1)*reactTipRow, len(gt.urls))]
		n := len(urls)
		tx := x + (w-(n*tile+(n-1)*gap))/2
		for _, url := range urls {
			tr := image.Rect(tx, ty+(rowH-tile)/2, tx+tile, ty+(rowH-tile)/2+tile)
			rrect(gtx.Ops, tr, dp(gtx, 4), rgb(0xffffff))
			o := (tile - es) / 2
			drawEmoji(gtx, h, url, image.Rect(tr.Min.X+o, tr.Min.Y+o, tr.Min.X+o+es, tr.Min.Y+o+es))
			tx += tile + gap
		}
		ty += rowH + gap
	}
	for i, p := range paras {
		if i > 0 {
			ty += dp(gtx, 16)
		}
		t := op.Offset(image.Pt(x+(w-p.size.X)/2, ty)).Push(gtx.Ops)
		p.call.Add(gtx.Ops)
		t.Pop()
		ty += p.size.Y
	}
}

// reactTipRow is ReactionTip's emoji per row (lodash chunk(…, 8)).
const reactTipRow = 8

// groupTip is a pill's ReactionTip: one getTip line per reaction of the
// group (reactionTip's marked runs), the reactions' emoji, and the
// tooltip's width ("large").
type groupTip struct {
	lines []string
	urls  []string
	wide  bool
}

// text is the tip as one string (tests, the list's "is a tip up").
func (gt *groupTip) text() string { return strings.Join(gt.lines, "\n") }

// groupTip is ReactionTip for pill k: Reaction passes the group's count
// and reactions. A multi-skin-tone emoji's reactions read as one line
// under the group's name with all their users (isMultiSkintone); past 50
// reactions in a group of several tones each line names its first ten
// (getTip's isOverflow then: that reaction's count past 10).
func (r *row) groupTip(h Host, k int, selfID string) groupTip {
	g := &r.groups[k]
	rs := r.m.Reactions
	gt := groupTip{wide: g.count > 9 || len(g.idx) > 4}
	for _, i := range g.idx {
		re := &rs[i]
		url := re.URL
		if url == "" && h.Core() != nil {
			url, _, _ = h.Core().Emoji(re.Name)
		}
		gt.urls = append(gt.urls, url)
	}
	if multiSkinTone[g.name] {
		one := rs[g.idx[0]]
		one.Name, one.Users = g.name, groupUsers(rs, g)
		gt.lines = []string{reactionTipLine(h, &one, selfID, false)}
		return gt
	}
	cut := g.count > 50 && len(g.idx) > 1
	for _, i := range g.idx {
		gt.lines = append(gt.lines, reactionTipLine(h, &rs[i], selfID, cut))
	}
	return gt
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
	names, _ := splitReactionTip(plainTip(txt))
	return strings.Count(names, ",") + strings.Count(names, "、") + 1 + strings.Count(names, " and ")
}

// tipRuns are txt's runs: TooltipSubtitle's (between tipSubOn and
// tipSubOff, reactionTip's marks) in sub, the rest in bold; a tooltip
// without marks splits at "reacted with" (splitReactionTip).
func tipRuns(txt string, bold, sub textStyle) []tipRun {
	if !strings.Contains(txt, tipSubOn) {
		names, s := splitReactionTip(txt)
		return []tipRun{{bold, names + " "}, {sub, s}}
	}
	var runs []tipRun
	add := func(st textStyle, s string) {
		if s == "" {
			return
		}
		if n := len(runs); n > 0 && runs[n-1].st == st {
			runs[n-1].txt += s
			return
		}
		runs = append(runs, tipRun{st, s})
	}
	for txt != "" {
		i := strings.Index(txt, tipSubOn)
		if i < 0 {
			add(bold, txt)
			break
		}
		add(bold, txt[:i])
		txt = txt[i+len(tipSubOn):]
		j := strings.Index(txt, tipSubOff)
		if j < 0 {
			j = len(txt)
		}
		add(sub, txt[:j])
		txt = txt[min(j+len(tipSubOff), len(txt)):]
	}
	return runs
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
// in their run's style, wrapped at spaces and between CJK characters (not
// before closing or after opening punctuation), each line centred on the
// widest (text-align: center); returns the size. Runs meet without a
// space unless their text has one ("Ann" + "さんが" is one word).
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
	for _, tk := range tipTokens(runs) {
		space := 0
		if tk.space {
			space = measure(gtx, th, tk.st, "x x").X - measure(gtx, th, tk.st, "xx").X
		}
		wd := word{st: tk.st, s: tk.s, w: measure(gtx, th, tk.st, tk.s).X}
		add := wd.w
		if len(cur.ws) > 0 {
			add += space
		}
		if len(cur.ws) > 0 && tk.brk && cur.w+add > maxW {
			lines = append(lines, cur)
			cur, add = line{}, wd.w
		}
		if len(cur.ws) > 0 && space > 0 {
			// the space before the word, in the word's run (the
			// subtitle's leading space is the names' trailing one)
			cur.ws = append(cur.ws, word{st: tk.st, s: " ", w: space})
		}
		cur.ws = append(cur.ws, wd)
		cur.w += add
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

// tipToken is one unbreakable piece of tooltip text.
type tipToken struct {
	st    textStyle
	s     string
	space bool // whitespace came before it
	brk   bool // a line may break before it
}

// tipTokens cuts runs into tokens: words split at whitespace, each CJK
// character its own token, a style change ending a token without a space.
func tipTokens(runs []tipRun) []tipToken {
	var out []tipToken
	space := false
	var last rune // the last character of the token before
	for _, run := range runs {
		var cur []rune
		curSpace, curBrk := false, false
		flush := func() {
			if len(cur) > 0 {
				out = append(out, tipToken{st: run.st, s: string(cur), space: curSpace, brk: curBrk})
				last = cur[len(cur)-1]
				cur = cur[:0]
			}
		}
		for _, r := range run.txt {
			switch {
			case r != '\u00a0' && unicode.IsSpace(r) || r == '\u200b': // a no-break space stays in its word
				flush()
				space = true
			case r == '\u2060': // a word joiner (group_cjk) holds its neighbours together
			case isCJK(r):
				flush()
				brk := space || len(out) > 0 && !noBreakBefore(r) && !noBreakAfter(last)
				out = append(out, tipToken{st: run.st, s: string(r), space: space, brk: brk})
				last, space = r, false
			default:
				if len(cur) == 0 {
					curSpace = space
					curBrk = space || len(out) > 0 && isCJK(last) && !noBreakAfter(last)
				}
				cur = append(cur, r)
				space = false
			}
		}
		flush()
	}
	return out
}

// isCJK: ideographs, kana, hangul and their punctuation (line breaks
// may fall between them).
func isCJK(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) ||
		r >= 0x3000 && r <= 0x303f || r >= 0xff00 && r <= 0xffef || r == 0x30fc
}

// noBreakBefore and noBreakAfter are kinsoku: no line starts with closing
// punctuation or a small kana, none ends with opening punctuation.
func noBreakBefore(r rune) bool {
	return strings.ContainsRune("、。，．・：；？！ー）」』】〕〉》）］｝ぁぃぅぇぉっゃゅょゎァィゥェォッャュョヮヵヶ々", r)
}

func noBreakAfter(r rune) bool { return strings.ContainsRune("（「『【〔〈《（［｛", r) }

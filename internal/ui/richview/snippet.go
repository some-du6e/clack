package richview

// A snippet in a message (CDN build 2c0d02ca130f):
//
//   - MessageFile (boot-shared Zw0E) draws a snippet as a PillowFile with
//     no header (MessageFileHeader returns null for a snippet whose
//     preview shows; the title is the files' meta line above it,
//     filemeta.go) and no onClick (isExpandable is true for every
//     snippet), its preview FileSnippet (MT+a) wrapped in the file's
//     ExpandableContainer (9TNB).
//   - FileSnippet renders `isExpanded && content_highlight_html ||
//     preview_highlight || ""`: the server's CodeMirror HTML
//     (richtext.ParseCodeMirror) in .c-pillow_file__snippet (margin
//     -12px: edge to edge in the pillow) under the client theme's
//     CodeMirror class (FkeY: cm-s-default, cm-s-yonce in dark mode).
//     The theme's .cm-* rules colour the spans; the server's root keeps
//     its own cm-s-default, so in dark mode the default rules yonce
//     doesn't override apply too (cmRules, in stylesheet order).
//   - The lines (.CodeMirror-code > div): 13px monospace at 1.50001,
//     margin-left 32px and padding 0 8px, the pre's padding 0 4px,
//     pre-wrap; the first line 4px from the top and the last 4px from
//     the bottom (one line: 18px each; two: 8px). Each line's :before is
//     its number: 32px wide at left -32px, right-aligned 4px in, in
//     foreground_high .5 on foreground_min .04 behind a 1px
//     foreground_low .13 rule.
//   - Expandable (isExpandableSnippet: lines_more > 0 or
//     preview_is_truncated) and not expanded, it is collapsed
//     (c-pillow_file__content--collapsed, c-pillow_file__snippet--
//     collapsed): at most 200px tall, the 40px fade to the primary
//     background (linear-gradient over the whole box, radius 0 0 12 12),
//     cursor pointer, and a click anywhere expands it (fetchAndExpand,
//     core snippets.go).
//   - ExpandablePill (uIB5), opacity 0 until the pillow is hovered: a
//     24px pill (radius 12, #616061, white 13px, padding 0 8px, a 4px
//     gap) with an inline 1em glyph: collapsed, expand-diagonal "Show
//     more ({lineCount} lines)" (the file's lines) centred on the
//     snippet; expanded, reduce-diagonal "Show less" 12px off its
//     bottom; expanding, the medium blue InfiniteSpinner in its place.
//   - Expanded and truncated (is_truncated: cut to 12,800 characters for
//     display, or content_highlight_html_truncated from files.info),
//     .c-pillow_file__snippet__truncated under it, edge to edge: a 1px
//     foreground_low .13 rule, padding 4px 8px, 13px in foreground_max
//     .7: the info-circle glyph, a no-break space and "This snippet was
//     truncated for display; <a>see it in full</a>" (a link to the
//     file's permalink).
//
// A CSV or TSV snippet's table (cm-csv-tsv-wrapper) is drawn as lines of
// tab-separated cells. .cm-s-yonce .cm-error's border-bottom isn't drawn,
// and the Solanum theme's cm-negative / cm-positive colours aren't used.

import (
	"image"
	"image/color"
	"strconv"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
)

// snipPill is a snippet's ExpandablePill, shown while its pillow is
// hovered.
type snipPill struct {
	card      image.Rectangle // the pillow
	btn       image.Rectangle // the pill's button (expanding: the spinner's box)
	label     string
	more      bool // Show more (expand-diagonal); else Show less
	expanding bool // the spinner in place of the button
}

// cmLook is a token's look from the CodeMirror theme.
type cmLook struct {
	col               color.NRGBA
	hasCol            bool
	italic, bold      bool
	underline, strike bool
}

const (
	cmStyleItalic = 1 + iota
	cmStyleNormal
)

const (
	cmDecoUnderline = 1 + iota
	cmDecoStrike
	cmDecoNone
)

// cmRule is one CodeMirror theme rule: an element with all of cls,
// under an element with class theme ("": none), span: the selector names
// the element (span.cm-type). In stylesheet order (client-boot-styles:
// codemirror.css, then the yonce theme).
type cmRule struct {
	theme  string
	span   bool
	cls    []string
	col    uint32 // 0xrrggbb; 0: none
	style  uint8
	weight uint8 // 1 bold, 2 normal (400)
	deco   uint8
}

func cr(theme, cls string, col uint32) cmRule {
	return cmRule{theme: theme, cls: strings.Fields(cls), col: col}
}

var cmRules = func() []cmRule {
	const d, y = "cm-s-default", "cm-s-yonce"
	rs := []cmRule{
		cr(d, "cm-header", 0x0000ff), cr(d, "cm-quote", 0x009900),
		cr("", "cm-negative", 0xdd4444), cr("", "cm-positive", 0x229922),
		{cls: []string{"cm-header"}, weight: 1}, {cls: []string{"cm-strong"}, weight: 1},
		{cls: []string{"cm-em"}, style: cmStyleItalic},
		{cls: []string{"cm-link"}, deco: cmDecoUnderline},
		{cls: []string{"cm-strikethrough"}, deco: cmDecoStrike},
		cr(d, "cm-keyword", 0x770088), cr(d, "cm-atom", 0x221199), cr(d, "cm-number", 0x116644),
		cr(d, "cm-def", 0x0000ff), cr(d, "cm-variable-2", 0x0055aa),
		cr(d, "cm-variable-3", 0x008855), cr(d, "cm-type", 0x008855),
		cr(d, "cm-comment", 0xaa5500), cr(d, "cm-string", 0xaa1111), cr(d, "cm-string-2", 0xff5500),
		cr(d, "cm-meta", 0x555555), cr(d, "cm-qualifier", 0x555555), cr(d, "cm-builtin", 0x3300aa),
		cr(d, "cm-bracket", 0x999977), cr(d, "cm-tag", 0x117700), cr(d, "cm-attribute", 0x0000cc),
		cr(d, "cm-hr", 0x999999), cr(d, "cm-link", 0x0000cc),
		cr(d, "cm-error", 0xff0000), cr("", "cm-invalidchar", 0xff0000),

		cr(y, "cm-keyword", 0x00a7aa), cr(y, "cm-atom", 0xf39b35),
		cr(y, "cm-number", 0xa06fca), {theme: y, span: true, cls: []string{"cm-type"}, col: 0xa06fca},
		cr(y, "cm-def", 0x98e342),
		{theme: y, cls: []string{"cm-property"}, col: 0xd1d2d3, style: cmStyleItalic},
		{theme: y, span: true, cls: []string{"cm-variable"}, col: 0xd1d2d3, style: cmStyleItalic},
		{theme: y, span: true, cls: []string{"cm-variable-2"}, col: 0xda7dae, style: cmStyleItalic},
		{theme: y, span: true, cls: []string{"cm-variable-3"}, col: 0xa06fca},
		{theme: y, cls: []string{"cm-type", "cm-def"}, col: 0xfc4384, style: cmStyleNormal, deco: cmDecoUnderline},
		{theme: y, cls: []string{"cm-property", "cm-def"}, col: 0xfc4384, style: cmStyleNormal},
		{theme: y, cls: []string{"cm-callee"}, col: 0xfc4384, style: cmStyleNormal},
		cr(y, "cm-operator", 0xfc4384), cr(y, "cm-qualifier", 0xfc4384), cr(y, "cm-tag", 0xfc4384),
		cr(y, "cm-tag cm-bracket", 0xd1d2d3), cr(y, "cm-attribute", 0xa06fca),
		{theme: y, cls: []string{"cm-comment"}, col: 0x696d70, style: cmStyleItalic, weight: 2},
		cr(y, "cm-comment cm-tag", 0xfc4384), cr(y, "cm-comment cm-attribute", 0xd1d2d3),
		cr(y, "cm-string", 0xe6db74), cr(y, "cm-string-2", 0xf39b35), cr(y, "cm-meta", 0xd1d2d3),
		cr(y, "cm-builtin", 0xfc4384), cr(y, "cm-header", 0xda7dae), cr(y, "cm-hr", 0x98e342),
		{theme: y, cls: []string{"cm-link"}, col: 0x696d70, style: cmStyleItalic, deco: cmDecoNone},
	}
	return rs
}()

// cmThemes are the theme classes over a snippet's spans: the client's
// (FileSnippet's getCodeMirrorThemeClass) and the server root's own.
func cmThemes(code *richtext.CodeMirror, dark bool) map[string]bool {
	th := map[string]bool{"cm-s-default": !dark, "cm-s-yonce": dark}
	if code != nil {
		for _, c := range code.Root {
			if strings.HasPrefix(c, "cm-s-") {
				th[c] = true
			}
		}
	}
	return th
}

// cmElement is one span's look by the rules: the winning declaration of
// each property (the higher specificity, then the later rule).
func cmElement(classes []string, themes map[string]bool) (col uint32, style, weight, deco uint8) {
	has := func(c string) bool {
		for _, x := range classes {
			if x == c {
				return true
			}
		}
		return false
	}
	type win struct{ spec, idx int }
	var wc, ws, ww, wd win
	wc.spec, ws.spec, ww.spec, wd.spec = -1, -1, -1, -1
	for i, r := range cmRules {
		if r.theme != "" && !themes[r.theme] {
			continue
		}
		ok := true
		for _, c := range r.cls {
			if !has(c) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		spec := len(r.cls) * 10
		if r.theme != "" {
			spec += 10
		}
		if r.span {
			spec++
		}
		better := func(w *win) bool { return spec > w.spec || spec == w.spec && i > w.idx }
		if r.col != 0 && better(&wc) {
			wc, col = win{spec, i}, r.col
		}
		if r.style != 0 && better(&ws) {
			ws, style = win{spec, i}, r.style
		}
		if r.weight != 0 && better(&ww) {
			ww, weight = win{spec, i}, r.weight
		}
		if r.deco != 0 && better(&wd) {
			wd, deco = win{spec, i}, r.deco
		}
	}
	return
}

// cmLookOf is a run's look: its spans' rules outermost first, colour and
// font inherited from the nearest span that sets them, decorations from
// every span (CSS paints an ancestor's line over its descendants).
func cmLookOf(spans []string, themes map[string]bool) cmLook {
	var lk cmLook
	for _, sp := range spans {
		col, style, weight, deco := cmElement(strings.Fields(sp), themes)
		if col != 0 {
			lk.col, lk.hasCol = rgb(uint8(col>>16), uint8(col>>8), uint8(col)), true
		}
		switch style {
		case cmStyleItalic:
			lk.italic = true
		case cmStyleNormal:
			lk.italic = false
		}
		switch weight {
		case 1:
			lk.bold = true
		case 2:
			lk.bold = false
		}
		switch deco {
		case cmDecoUnderline:
			lk.underline = true
		case cmDecoStrike:
			lk.strike = true
		}
	}
	return lk
}

// snippetLines is the code FileSnippet shows: the parsed highlight, else
// (no highlight sent) the plain preview's lines, unstyled.
func snippetLines(code *richtext.CodeMirror, preview string) []richtext.CodeLine {
	if code != nil {
		return code.Lines
	}
	preview = strings.TrimRight(preview, "\n")
	if preview == "" {
		return nil
	}
	var out []richtext.CodeLine
	for _, ln := range strings.Split(preview, "\n") {
		if ln == "" {
			out = append(out, nil)
			continue
		}
		out = append(out, richtext.CodeLine{{Text: ln}})
	}
	return out
}

// snippetPads are line i of n's padding-top and padding-bottom (Dp).
func snippetPads(i, n int) (top, bot float32) {
	switch {
	case n == 1:
		return 18, 18
	case n == 2 && i == 0:
		return 8, 0
	case n == 2:
		return 0, 8
	case i == 0:
		return 4, 0
	case i == n-1:
		return 0, 4
	}
	return 0, 0
}

// snippet is Slack's snippet message file (see the top of this file).
func (l *lay) snippet(f *richtext.File, x, y, w int) int {
	p := &l.st.Palette
	cw := min(w, l.dp(1000))
	bw := l.dp(1)
	expandable := f.LinesMore > 0 || f.PreviewTruncated // isExpandableSnippet
	collapsed := expandable && !f.Expanded
	code := f.Highlight
	if f.Expanded && f.Content != nil {
		code = f.Content
	}
	lines := snippetLines(code, f.Preview)
	if len(lines) == 0 {
		lines = []richtext.CodeLine{nil}
	}
	themes := cmThemes(code, p.Dark)

	// the pillow: the file's context menu, no click of its own
	card := l.d.addTarget(target{kind: tFile, file: f, menuOnly: true})
	bi := len(l.d.boxes)
	l.d.boxes = append(l.d.boxes, box{kind: bBorder, fill: p.Panel, stroke: p.Border, bw: bw, radius: l.dp(12), target: card, hover: p.BorderHover})
	hi := len(l.d.hits)
	l.d.hits = append(l.d.hits, hit{t: card})

	top := y + bw
	lh := l.dp(19.5)
	gw := l.dp(32)
	left := x + bw
	right := x + cw - bw
	codeX := left + gw + l.dp(8) + l.dp(4) // the line's margin and padding, the pre's padding
	codeW := max(right-l.dp(8)-l.dp(4)-codeX, 1)
	limit := 1 << 30
	if collapsed {
		limit = top + l.dp(200) // max-height
	}
	num := rgba(29, 28, 29, 0.5)
	if p.Dark {
		num = rgba(209, 210, 211, 0.5)
	}
	ns := flowStyle{face: font.Font{Typeface: l.st.Mono}, size: l.sp(13), lineH: lh, col: Over(num, p.Panel), emoji: l.dp(13), maxLines: 1, noSelect: true}
	nk := l.faceFor(ns.face, ns.size, 0)
	ly := top
	for i, ln := range lines {
		if ly >= limit {
			break
		}
		pt, pb := snippetPads(i, len(lines))
		ty := ly + l.dp(pt)
		rows := 1 << 20
		if collapsed {
			if rows = (limit - ty) / lh; rows < 1 {
				break // under the 200px: hidden
			}
		}
		n := strconv.Itoa(i + 1)
		nw := int(l.measure(n, nk) + 0.999)
		l.plainFlow(n, left+gw-bw-l.dp(4)-nw, ty, nw+l.dp(2), &ns, -1)
		h := lh
		if len(ln) > 0 {
			cs := flowStyle{face: font.Font{Typeface: l.st.Mono}, size: l.sp(13), lineH: lh, col: p.Text, emoji: l.dp(13), pre: true}
			if rows < 1<<20 {
				cs.maxLines = rows
			}
			for _, r := range ln {
				lk := cmLookOf(r.Classes, themes)
				col := p.Text
				if lk.hasCol {
					col = lk.col
				}
				var st richtext.Style
				if lk.bold {
					st |= richtext.Bold
				}
				if lk.italic {
					st |= richtext.Italic
				}
				if lk.underline {
					st |= richtext.Underline
				}
				if lk.strike {
					st |= richtext.Strike
				}
				l.addText(r.Text, &cs, l.faceFor(cs.face, cs.size, st), col, st, -1, -1)
			}
			h, _ = l.flow(codeX, ty, codeW, &cs)
			h = max(h, lh)
		}
		if i < len(lines)-1 {
			l.plainAdd("\n")
		}
		ly = ty + h + l.dp(pb)
	}
	bottom := min(ly, limit)
	cb := image.Rect(left, top, right, bottom) // .c-pillow_file__snippet
	rr := l.dp(12) - bw
	gut, rule := rgba(29, 28, 29, 0.04), rgba(29, 28, 29, 0.13)
	if p.Dark {
		gut, rule = rgba(232, 232, 232, 0.04), rgba(232, 232, 232, 0.13)
	}
	sw := 0
	if !(f.Expanded && f.ContentTruncated) {
		sw = rr // the gutter's corner is the pillow's
	}
	l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(left, top, left+gw, bottom), fill: Over(gut, p.Panel),
		target: -1, perCorner: true, corners: [4]int{rr, 0, 0, sw}})
	l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(left+gw-bw, top, left+gw, bottom), fill: Over(rule, p.Panel), target: -1})
	if collapsed { // the 40px fade to the pane, over the code
		fr := image.Rect(left, max(top, bottom-l.dp(40)), right, bottom)
		l.d.boxes = append(l.d.boxes, box{kind: bFade, r: fr, fill: p.Panel, target: -1, layer: 1, perCorner: true, corners: [4]int{0, 0, rr, rr}})
	}
	h := bottom - y
	lastTg := card
	pill := -1
	// the pill and the collapsed snippet's click
	if expandable {
		more := !f.Expanded
		label := i18n.T("files", "Show less")
		if more {
			label = i18n.T("files", "Show more")
			if f.Lines > 0 {
				label = i18n.T("files", "Show more ({lineCount, plural, =1 {# line} other {# lines}})", "lineCount", f.Lines)
			}
		}
		pk := l.faceFor(font.Font{Typeface: l.st.Face}, l.sp(13), 0)
		pw := l.dp(8) + l.sp(13) + l.dp(4) + int(l.measure(label, pk)+0.999) + l.dp(8)
		ph := l.dp(24)
		px := cb.Min.X + (cb.Dx()-pw)/2
		py := cb.Min.Y + (cb.Dy()-ph)/2 // --expand: centred on the snippet
		if !more {
			py = cb.Max.Y - l.dp(12) - ph // --collapse: 12px off its bottom
		}
		btn := image.Rect(px, py, px+pw, py+ph)
		if f.Expanding {
			s := l.dp(16)
			c := image.Pt((cb.Min.X+cb.Max.X)/2, py+ph/2)
			btn = image.Rect(c.X-s/2, c.Y-s/2, c.X-s/2+s, c.Y-s/2+s)
		}
		act := FileShowLess
		if more {
			act = FileShowMore
		}
		tg := l.d.addTarget(target{kind: tSnippet, file: f, act: act})
		lastTg = tg
		if more {
			l.d.hits = append(l.d.hits, hit{r: cb, t: tg}) // the collapsed snippet: a click expands it
		} else if !f.Expanding {
			l.d.hits = append(l.d.hits, hit{r: btn, t: tg})
		}
		pill = len(l.d.pills)
		l.d.pills = append(l.d.pills, snipPill{btn: btn, label: label, more: more, expanding: f.Expanding})
	}
	if f.Expanded && f.ContentTruncated {
		h += l.snippetTruncated(f, x, bottom, cw)
		lastTg = int16(len(l.d.targets) - 1)
	}
	r := image.Rect(x, y, x+cw, y+h)
	l.d.boxes[bi].r = r
	l.d.boxes[bi].tgTo = lastTg
	l.d.hits[hi].r = r
	if pill >= 0 {
		l.d.pills[pill].card = r
	}
	l.pillowBar(f, r)
	return h
}

// snippetTruncated lays out .c-pillow_file__snippet__truncated at y under
// the snippet and returns its height.
func (l *lay) snippetTruncated(f *richtext.File, x, y, cw int) int {
	p := &l.st.Palette
	bw := l.dp(1)
	rule := rgba(29, 28, 29, 0.13)
	if p.Dark {
		rule = rgba(232, 232, 232, 0.13)
	}
	l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(x+bw, y, x+cw-bw, y+bw), fill: Over(rule, p.Panel), target: -1})
	fs := l.small(p.Text2)
	fs.lineH = l.dp(19) // 13px at the page's 1.46668
	k := l.faceFor(fs.face, fs.size, 0)
	l.plainAdd("\n")
	l.addIcon("info", fs.size, k, fs.col, -1, -1) // info-circle, 1em
	l.addText(" ", &fs, k, fs.col, 0, -1, -1)
	for _, pt := range i18n.NS("files").RT("This snippet was truncated for display; <a>see it in full</a>") {
		if pt.Tag == "a" {
			tg := l.d.addTarget(target{kind: tLink, url: f.Permalink, underline: true})
			l.addText(pt.Text, &fs, k, p.Link, 0, -1, tg)
			continue
		}
		l.addText(pt.Text, &fs, k, fs.col, 0, -1, -1)
	}
	ty := y + bw + l.dp(4)
	h, _ := l.flow(x+bw+l.dp(8), ty, cw-2*bw-2*l.dp(8), &fs)
	return bw + l.dp(4) + max(h, fs.lineH) + l.dp(4) + bw
}

// drawPills draws the hovered snippets' pills.
func (v *View) drawPills(gtx layout.Context, st *Style) {
	if !v.hovering || len(v.d.pills) == 0 {
		return
	}
	pt := image.Pt(int(v.ptr.X), int(v.ptr.Y))
	for i := range v.d.pills {
		pl := &v.d.pills[i]
		if !pt.In(pl.card) {
			continue
		}
		if pl.expanding {
			sp := spinPrim{r: pl.btn}
			sp.draw(gtx, st)
			continue
		}
		r := pl.btn
		paint.FillShape(gtx.Ops, rgb(0x61, 0x60, 0x61), clip.UniformRRect(r, r.Dy()/2).Op(gtx.Ops))
		white := rgb(255, 255, 255)
		is := v.spx(13)
		name := "expand" // expand-diagonal
		if !pl.more {
			name = "collapse" // reduce-diagonal
		}
		ix := r.Min.X + v.px(8)
		v.secIcon(gtx, name, image.Rect(ix, r.Min.Y+(r.Dy()-is)/2, ix+is, r.Min.Y+(r.Dy()-is)/2+is), white)
		v.pillText(gtx, st, pl.label, image.Pt(ix+is+v.px(4), r.Min.Y), r.Dy(), white)
	}
}

// pillText paints s in 13px Lato at x, vertically centred in a line h
// tall from y.
func (v *View) pillText(gtx layout.Context, st *Style, s string, at image.Point, h int, col color.NRGBA) {
	l := lay{st: st, d: &v.d, scale: v.laidPx, spx: v.laidSp}
	if l.scale <= 0 {
		l.scale, l.spx = 1, 1
	}
	k := l.faceFor(font.Font{Typeface: st.Face}, l.sp(13), 0)
	sh := st.word(k, s)
	bm := st.spaceOf(k)
	base := at.Y + int((float32(h)-(bm.asc+bm.desc))/2+bm.asc+0.5)
	t := op.Offset(image.Pt(at.X, base)).Push(gtx.Ops)
	glyphs, _ := st.pathOf(sh, true)
	cs := clip.Outline{Path: glyphs}.Op().Push(gtx.Ops)
	paint.ColorOp{Color: col}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	cs.Pop()
	t.Pop()
}

// spx is sp in px at the last layout's scale.
func (v *View) spx(sp int) int {
	s := v.laidSp
	if s <= 0 {
		s = 1
	}
	return int(float32(sp)*s + 0.5)
}

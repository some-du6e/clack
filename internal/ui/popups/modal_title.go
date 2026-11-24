package popups

import (
	"image"
	"strings"

	"gioui.org/layout"
	"github.com/go-text/typesetting/segmenter"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// A modal's title as Slack's title bar wraps it. .c-sk-modal_title_bar__text
// has no white-space rule, so its h1 wraps like any block of text: at
// UAX #14's break opportunities, each line 1.36365 × 22px = 30px below the
// last, and the bar (min-height 70px, padding 20px 28px) grows by whole
// lines. A channel entity in the title (TitleIcon, TitleAfter: Slack's
// InlineChannelEntity) wraps with its icon: .c-base_inline_entity is
// white-space: pre, so the icon and the name's first unit stay together
// on one line.
//
// Gio's shaper spaces a wrapped paragraph's lines 1.2 × its line height
// apart (gotext.go), so the lines are broken here and each is drawn as
// one line at y = n × 30px.

// titleLine is one line of a wrapped title: pre, then the entity's icon
// (when the entity starts on this line) and post.
type titleLine struct {
	pre, icon, post string
}

// titleUnit is one break unit of the title, its trailing spaces kept.
type titleUnit struct {
	text string
	icon string // the entity's icon, drawn before text
}

// titleWrap caches the last wrap: the title only changes with TitleFunc
// or the window's width.
type titleWrap struct {
	th                *theme.Theme
	title, ic, after  string
	maxW              int
	size, lh, iconGap float32
	lines             []titleLine
}

// titleBreakUnits is s cut at its line break opportunities (UAX #14, the
// rules Chrome wraps Slack's text by), each piece with the spaces after it.
func titleBreakUnits(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	var seg segmenter.Segmenter
	seg.Init([]rune(s))
	for it := seg.LineIterator(); it.Next(); {
		out = append(out, string(it.Line().Text))
	}
	return out
}

// titleUnits are the title's break units: Title's, then the channel
// entity (the icon with TitleAfter's first unit), then the rest of
// TitleAfter's.
func titleUnits(title, ic, after string) []titleUnit {
	var out []titleUnit
	for _, u := range titleBreakUnits(title) {
		out = append(out, titleUnit{text: u})
	}
	for i, u := range titleBreakUnits(after) {
		tu := titleUnit{text: u}
		if i == 0 {
			tu.icon = ic
		}
		out = append(out, tu)
	}
	if ic != "" && after == "" {
		out = append(out, titleUnit{icon: ic})
	}
	return out
}

// fillTitleLines puts units on lines greedily, as Chrome's line breaker
// does: a unit joins the line when the line with it fits without its
// trailing spaces (they hang past the line's end); one too wide for a
// line of its own takes a line anyway (its end is cut with an ellipsis
// when drawn). Each candidate line is measured whole, as Chrome shapes
// it, so kerning across words counts and no per-word rounding adds up.
// width measures a string on one line, iconW is the icon's advance.
func fillTitleLines(units []titleUnit, maxW, iconW int, width func(string) int) []titleLine {
	var lines []titleLine
	var cur titleLine
	empty := true
	for _, u := range units {
		cand := cur
		if u.icon != "" {
			cand.icon = u.icon
		}
		if cand.icon != "" {
			cand.post += u.text
		} else {
			cand.pre += u.text
		}
		if !empty && lineWidth(cand, iconW, width) > maxW {
			lines = append(lines, cur)
			cand = titleLine{icon: u.icon}
			if u.icon != "" {
				cand.post = u.text
			} else {
				cand.pre = u.text
			}
		}
		cur, empty = cand, false
	}
	if !empty {
		lines = append(lines, cur)
	}
	return lines
}

// lineWidth is a line's width without its trailing spaces: pre whole
// when nothing follows it, else up to the icon, its spaces included.
func lineWidth(ln titleLine, iconW int, width func(string) int) int {
	if ln.icon == "" {
		return width(strings.TrimRight(ln.pre, " "))
	}
	return leadWidth(ln.pre, width) + iconW + width(strings.TrimRight(ln.post, " "))
}

// leadWidth is s's width with its trailing spaces, each counted as one
// space's advance: measured on their own, a right-to-left word's
// trailing space is a separate run the shaper gives no advance (go-text
// wrapping.go postProcessLine), so "דוד " would measure as "דוד".
func leadWidth(s string, width func(string) int) int {
	t := strings.TrimRight(s, " ")
	if n := len(s) - len(t); n > 0 {
		return width(t) + n*(width("x x")-width("xx"))
	}
	return width(t)
}

// titleWrapW is the width a title wraps in: .c-sk-modal_title_bar's
// padding 20px 28px, and padding-right 76px past the close button
// (c-sk-modal_title_bar--close_button_visible). Dialogs with their own
// header metrics (PadX, HeadH: Slack's c-dialog__header, padding 16px
// 24px, the close button a flex item after the title) keep 64px.
func (m *Modal) titleWrapW(gtx layout.Context, w, padX int) int {
	switch {
	case m.o.PadX > 0 || m.o.HeadH > 0 || m.o.CloseFirst:
		return w - padX - dp(gtx, 64)
	case m.o.NoClose:
		return w - 2*padX
	}
	return w - padX - dp(gtx, 76)
}

// titleLines are the title's lines in maxW; nil when it doesn't wrap
// (TruncateTitle, a Header, no title).
func (m *Modal) titleLines(gtx layout.Context, th *theme.Theme, cl *Colors, sz, lh float32, maxW int) []titleLine {
	o := m.o
	if o.TruncateTitle || o.Header != nil {
		return nil
	}
	title := m.Title()
	if title == "" && o.TitleIcon == "" && o.TitleAfter == "" {
		return nil
	}
	c := &m.wrap
	if c.lines != nil && c.th == th && c.title == title && c.ic == o.TitleIcon && c.after == o.TitleAfter &&
		c.maxW == maxW && c.size == sz && c.lh == lh && c.iconGap == gtx.Metric.PxPerDp {
		return c.lines
	}
	width := titleWidthFunc(gtx, th, cl, sz, lh)
	lines := fillTitleLines(titleUnits(title, o.TitleIcon, o.TitleAfter), maxW, dp(gtx, 22), width)
	*c = titleWrap{th: th, title: title, ic: o.TitleIcon, after: o.TitleAfter, maxW: maxW, size: sz, lh: lh, iconGap: gtx.Metric.PxPerDp, lines: lines}
	return lines
}

// titleWidthFunc measures a string as a title line draws it, on one line.
func titleWidthFunc(gtx layout.Context, th *theme.Theme, cl *Colors, sz, lh float32) func(string) int {
	return func(s string) int {
		if s == "" {
			return 0
		}
		_, d := record(loose(gtx, image.Pt(1<<20, 1<<20)), func(gtx layout.Context) layout.Dimensions {
			return para(gtx, th, sz, lh, theme.Black, cl.Fg, s, 1)
		})
		return d.Size.X
	}
}

// drawTitleLines draws a wrapped title, each line lh below the last.
func drawTitleLines(gtx layout.Context, th *theme.Theme, cl *Colors, sz, lhSp float32, maxW int, lines []titleLine) layout.Dimensions {
	lh := dp(gtx, lhSp)
	width := titleWidthFunc(gtx, th, cl, sz, lhSp)
	for i, ln := range lines {
		y := i * lh
		x := 0
		if ln.pre != "" {
			at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(maxW, lh)}
				return para(gtx, th, sz, lhSp, theme.Black, cl.Fg, ln.pre, 1)
			})
			if ln.icon != "" {
				x = leadWidth(ln.pre, width)
			}
		}
		if ln.icon != "" {
			at(gtx, image.Pt(x, y+dp(gtx, 6)), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, ln.icon, 20, cl.Fg)
			})
			x += dp(gtx, 22)
		}
		if ln.post != "" {
			at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, maxW-x), lh)}
				return para(gtx, th, sz, lhSp, theme.Black, cl.Fg, ln.post, 1)
			})
		}
	}
	return layout.Dimensions{Size: image.Pt(maxW, len(lines)*lh)}
}

// TitleLines is the title's lines as the last frame drew them, the
// entity's icon as "[icon]" (tests); one line when it didn't wrap.
func (m *Modal) TitleLines() []string {
	if len(m.drawnLines) == 0 {
		return []string{m.Title()}
	}
	var out []string
	for _, ln := range m.drawnLines {
		s := ln.pre
		if ln.icon != "" {
			s += "[" + ln.icon + "]"
		}
		out = append(out, strings.TrimRight(s+ln.post, " "))
	}
	return out
}

// TitleCut reports that the last frame drew a plain title on one line
// cut with an ellipsis: it is wider than its line (TruncateTitle, or a
// single unbreakable word).
func (m *Modal) TitleCut() bool { return m.cut }

// TitleBar and CloseRect are the title bar's and the close button's
// rectangles in the card as last drawn (tests).
func (m *Modal) TitleBar() image.Rectangle  { return m.barRect }
func (m *Modal) CloseRect() image.Rectangle { return m.closeRect }

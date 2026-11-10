package views

import (
	"image"
	"image/color"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// spanLine draws rich spans on one line, ellipsized at the max width:
// text in col, links in the link blue, member and channel mentions as
// Slack's mention pills (#1264a3 on #1d9bd1@0.1, radius 3), custom and
// standard emoji as images at 18/13 of the text size. The channel
// browser's purposes (Slack renders them through its rich text, so
// ":hackclub:", "<#C…>" and links show as emoji, "#name" and links).
func (b *base) spanLine(gtx C, spans []richtext.Span, size unit.Sp, col color.NRGBA) D {
	maxW := gtx.Constraints.Max.X
	h := gtx.Sp(size * 18 / 13)
	es := h
	ell := b.th.Measure(gtx, size, theme.Regular, "…")
	x := 0
	pill := argb(0x1a1d9bd1)
	for _, s := range spans {
		if s.Kind == richtext.Break {
			break
		}
		if s.Kind == richtext.Emoji && s.URL != "" {
			if x+es > maxW {
				break
			}
			url := s.URL
			dirAt(gtx, image.Pt(x, 0), func(gtx C) D { return b.emojiImg(gtx, url, unit.Dp(float32(es)/gtx.Metric.PxPerDp)) })
			x += es + gtx.Dp(1)
			continue
		}
		txt := strings.ReplaceAll(s.Text, "\n", " ")
		if txt == "" {
			continue
		}
		w := font.Weight(wReg)
		if s.Style&richtext.Bold != 0 {
			w = wBold
		}
		fg := col
		mention := false
		switch s.Kind {
		case richtext.Link:
			fg = b.p.Link
		case richtext.User, richtext.Channel, richtext.Usergroup:
			fg, mention = b.p.Link, true
		}
		tw := b.th.Measure(gtx, size, w, txt)
		trunc := x+tw > maxW
		if trunc {
			tw = maxW - x - ell
			if tw <= 0 {
				break
			}
		}
		if mention {
			rrect(gtx, image.Rect(x, 0, x+tw, h), gtx.Dp(3), pill)
		}
		t := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(tw+ell, h)}
		b.th.Layout(g, theme.TextStyle{Size: size, Weight: w, Italic: s.Style&richtext.Italic != 0, Color: fg, MaxLines: 1, LineHeight: size * 18 / 13}, txt)
		t.Pop()
		x += tw
		if trunc {
			x += ell
			break
		}
	}
	return D{Size: image.Pt(min(x, maxW), h)}
}

// spanEntry caches a purpose's parsed first line. unresolved: a mention
// still showed its id; parse again once core has had time to name it.
type spanEntry struct {
	spans      []richtext.Span
	at         time.Time
	unresolved bool
}

// richLineCache parses mrkdwn to first-line spans, by raw text.
type richLineCache map[string]*spanEntry

func (c richLineCache) get(raw string, parse func(string) []richtext.Block, now time.Time) []richtext.Span {
	if e, ok := c[raw]; ok && (!e.unresolved || now.Sub(e.at) < time.Second) {
		return e.spans
	}
	var spans []richtext.Span
	for _, bl := range parse(raw) {
		if len(bl.Spans) > 0 {
			spans = bl.Spans
			break
		}
	}
	unresolved := false
	for _, s := range spans {
		if (s.Kind == richtext.Channel || s.Kind == richtext.User) && s.ID != "" && strings.Contains(s.Text, s.ID) {
			unresolved = true
		}
	}
	c[raw] = &spanEntry{spans: spans, at: now, unresolved: unresolved}
	return spans
}

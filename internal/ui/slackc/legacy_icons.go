package slackc

import (
	"image"
	"image/color"
	"math"
	"strings"
	"unicode/utf8"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
)

// Slack's legacy icon font. Older Slack markup draws icons as a glyph of
// the "Slack v2" font (slack-icons-v2 woff) in a ::before:
// .c-deprecated-icon:before {font-family: Slack v2} with the icon's
// codepoint from its .c-icon--<name>:before {content: "\e…"}
// (client-boot-styles). Plugins that copy Slack's markup use it too
// (ClickToLoad's play button: i.c-deprecated-icon.c-icon--play). Clack
// has no copy of that font; it draws the same glyph from its vector
// icons, sized like the font's 1em glyph and in the text's color.
const legacyIconURL = "slack-legacy-icon:"

// legacyIcons maps the font's codepoints Clack draws to its icon names.
var legacyIcons = map[rune]string{
	0xe134: "play", // .c-icon--play
}

// legacyGlyph is the icon a word of text in Slack's icon font stands for,
// "" for ordinary text.
func legacyGlyph(st *css.Style, w string) string {
	if !strings.Contains(strings.ToLower(st.FontFamily), "slack v2") {
		return ""
	}
	r, n := utf8.DecodeRuneInString(w)
	if n != len(w) {
		return ""
	}
	return legacyIcons[r]
}

// legacyIcon draws icon name es px square on the baseline at x, base,
// placed like an emoji token.
func (p *painter) legacyIcon(name string, x, base, es float32, col color.NRGBA) {
	if es < 1 {
		return
	}
	top := float32(math.Round(float64(base - es*0.8)))
	t := op.Offset(image.Pt(int(math.Round(float64(x))), int(top))).Push(p.ops)
	defer t.Pop()
	gtx := layout.Context{Ops: p.ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(int(es), int(es)))}
	icons.GetSlack(name)(gtx, unit.Dp(es), col)
}

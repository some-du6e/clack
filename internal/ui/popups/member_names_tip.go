package popups

// TooltipMemberNames (boot-shared CB7H), the body of the composer's
// @group tooltip (TextyTooltipUserGroup) and of the canvas composer's
// GroupMembersTooltip, read from build 32bb3cc8205b on the public CDN:
//
//   - The members in the order the group lists them; past 11, the first
//     10 and "{count} others" (channel_token) for the rest.
//   - Each name is TooltipMemberName (NZA6): the viewer's pref-compliant
//     name (getPrefCompliantMemberNameById), "You" for the viewer when
//     first and "you" after; a member not loaded yet is BaseMissingMember
//     (FToV): a c-missing_text bar, 120px wide (MissingText's medium),
//     8px tall, fully rounded, --dt_color-base-ter at opacity .4
//     (--subtler).
//   - They are joined by listify (nTGe): "A and B", "A, B, and C" in
//     en-US, the locale's separators elsewhere (i18n.Listify).
//   - The text sits straight in the c-tooltip__tip: 13px, bold, line
//     height 1.25, centred, wrapping at its 200px max-width (padding 8px
//     12px inside it) and breaking a word that can't fit
//     (overflow-wrap: break-word).

import (
	"image"
	"image/color"
	"strconv"
	"strings"

	"gioui.org/layout"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// TipName is one TooltipMemberNames item: a name, or Missing for a
// member not loaded yet (the c-missing_text bar).
type TipName struct {
	Text    string
	Missing bool
}

// TooltipMemberNamesMax is how many members are listed before the rest
// become "{count} others": all of up to 11, else the first 10.
const TooltipMemberNamesMax = 11

// TooltipMemberNameItems is TooltipMemberNames' list for ids: name(id)
// for each listed member (ok false: not loaded yet), the viewer self as
// "You" first or "you" after, and "{count} others" past 11.
func TooltipMemberNameItems(ids []string, self string, name func(id string) (string, bool)) []TipName {
	shown := ids
	if len(ids) > TooltipMemberNamesMax {
		shown = ids[:TooltipMemberNamesMax-1]
	}
	out := make([]TipName, 0, len(shown)+1)
	for i, id := range shown {
		if id == self && self != "" {
			if i == 0 {
				out = append(out, TipName{Text: tokenNS.T("You")})
			} else {
				out = append(out, TipName{Text: tokenNS.T("you")})
			}
			continue
		}
		n, ok := name(id)
		if !ok {
			out = append(out, TipName{Missing: true})
			continue
		}
		out = append(out, TipName{Text: n})
	}
	if len(shown) < len(ids) {
		out = append(out, TipName{Text: tokenNS.T("{count} others", "count", len(ids)-len(shown))})
	}
	return out
}

var tokenNS = i18n.NS("channel_token")

// tipRun is a piece of the joined list: text, or a missing member's bar.
type tipRun struct {
	text string
	bar  bool
}

// tipRuns joins items with listify's separators for the locale: the
// items go through i18n.Listify as markers, so the separators (and
// their order) are the locale's.
func tipRuns(items []TipName) []tipRun {
	marks := make([]string, len(items))
	for i := range items {
		marks[i] = "" + strconv.Itoa(i) + ""
	}
	joined := i18n.Listify(marks, false)
	var runs []tipRun
	for joined != "" {
		i := strings.Index(joined, "")
		if i < 0 {
			runs = append(runs, tipRun{text: joined})
			break
		}
		if i > 0 {
			runs = append(runs, tipRun{text: joined[:i]})
		}
		j := strings.Index(joined[i:], "")
		n, _ := strconv.Atoi(joined[i+len("") : i+j])
		if it := items[n]; it.Missing {
			runs = append(runs, tipRun{bar: true})
		} else {
			runs = append(runs, tipRun{text: it.Text})
		}
		joined = joined[i+j+len(""):]
	}
	return runs
}

// tipPiece is one unbreakable piece on a line: a word, a space, or a bar.
type tipPiece struct {
	text  string
	bar   bool
	space bool
	w     int
}

// MemberNamesTip draws the c-tooltip__tip's body for items at the origin,
// its padding included, and returns the box's size: the box TooltipBox
// draws under it.
func MemberNamesTip(gtx layout.Context, th *theme.Theme, items []TipName) layout.Dimensions {
	cl := colorsOf(th)
	padX, padY := dp(gtx, 12), dp(gtx, 8)
	maxW := dp(gtx, 200) - 2*padX
	st := theme.TextStyle{Size: 13, Weight: theme.Bold, Color: cl.TooltipText, LineHeight: unit.Sp(13 * 1.25), MaxLines: 1}
	measure := func(s string) int { return th.Measure(gtx, st.Size, st.Weight, s) }

	// pieces: words and the spaces between them (the break opportunities)
	var ps []tipPiece
	for _, r := range tipRuns(items) {
		if r.bar {
			ps = append(ps, tipPiece{bar: true, w: dp(gtx, 120)})
			continue
		}
		for i, w := range strings.Split(r.text, " ") {
			if i > 0 {
				ps = append(ps, tipPiece{text: " ", space: true, w: measure(" ")})
			}
			if w != "" {
				ps = append(ps, tipPiece{text: w, w: measure(w)})
			}
		}
	}
	// break-word: a word wider than the line breaks between characters
	var fit []tipPiece
	for _, p := range ps {
		if p.bar || p.space || p.w <= maxW {
			fit = append(fit, p)
			continue
		}
		cur := ""
		for _, r := range p.text {
			if cur != "" && measure(cur+string(r)) > maxW {
				fit = append(fit, tipPiece{text: cur, w: measure(cur)})
				cur = ""
			}
			cur += string(r)
		}
		if cur != "" {
			fit = append(fit, tipPiece{text: cur, w: measure(cur)})
		}
	}
	// greedy lines; a space where a line breaks hangs (no width)
	var lines [][]tipPiece
	var line []tipPiece
	lw := 0
	for _, p := range fit {
		if !p.space && len(line) > 0 && lw+p.w > maxW {
			for len(line) > 0 && line[len(line)-1].space {
				lw -= line[len(line)-1].w
				line = line[:len(line)-1]
			}
			lines = append(lines, line)
			line, lw = nil, 0
		}
		if p.space && len(line) == 0 {
			continue
		}
		line = append(line, p)
		lw += p.w
	}
	for len(line) > 0 && line[len(line)-1].space {
		line = line[:len(line)-1]
	}
	if len(line) > 0 {
		lines = append(lines, line)
	}

	// the line box: Gio's line for 13px at 1.25, and its baseline, where
	// the empty inline-block bars sit
	_, ref := record(loose(gtx, image.Pt(1<<20, 1<<20)), func(gtx layout.Context) layout.Dimensions {
		return th.Layout(gtx, st, "A")
	})
	lh, base := ref.Size.Y, ref.Size.Y-ref.Baseline
	widths := make([]int, len(lines))
	w := 0
	for i, l := range lines {
		for _, p := range l {
			widths[i] += p.w
		}
		w = max(w, widths[i])
	}
	sz := image.Pt(w+2*padX, len(lines)*lh+2*padY)
	bar := missingBarColor(th, cl.Tooltip)
	for i, l := range lines {
		x := padX + (w-widths[i])/2
		y := padY + i*lh
		run := ""
		flush := func() {
			if run == "" {
				return
			}
			call, d := record(loose(gtx, image.Pt(1<<20, lh)), func(gtx layout.Context) layout.Dimensions {
				return th.Layout(gtx, st, run)
			})
			macroAt(gtx, call, image.Pt(x, y))
			x += d.Size.X
			run = ""
		}
		for _, p := range l {
			if p.bar {
				flush()
				h := dp(gtx, 8)
				rrect(gtx, image.Rect(x, y+base-h, x+p.w, y+base), h/2, bar)
				x += p.w
				continue
			}
			run += p.text
		}
		flush()
	}
	return layout.Dimensions{Size: sz}
}

// missingBarColor is c-missing_text--unknown's --dt_color-base-ter (the
// theme's, else Slack's #eaeaea / #212428) at --subtler's opacity .4
// over the tip.
func missingBarColor(th *theme.Theme, tip color.NRGBA) color.NRGBA {
	c := rgb(0xeaeaea)
	dark := th != nil && th.Dark
	if dark {
		c = rgb(0x212428)
	}
	if th != nil && th.Tokens != nil {
		theme.SetTok(th.Tokens, &c, theme.Both("var(--dt_color-base-ter)"), dark)
	}
	c.A = uint8(float32(c.A)*.4 + .5)
	return linearAlpha(c, tip)
}

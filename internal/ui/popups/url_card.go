package popups

import (
	"image"
	"strings"
	"time"
	"unicode/utf8"

	"gioui.org/layout"
	"gioui.org/op"
	gtext "gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/hovercard"
	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// URLHoverCard is Slack's UrlHoverCard (boot-shared cXYm, build
// fa938a086569): with truncated_url_hover_card on, a message link's
// hover (BaseMrkdwnLink's renderDefaultLink → UrlHoverCardTrigger, kQRT)
// opens it through HoverCardTrigger at position "top" instead of the
// link's URL tooltip.
//
//   - HoverCard (Q7nt) with fitContent: elevation_3, sk_primary_background,
//     rounded-lg, no fixed 340px width.
//   - .urlHoverCardContainer: a column, gap 8, min-width 280, max-width
//     400, padding 16.
//   - .urlHoverCardHeader (bold, space-between): the 16px link icon and
//     "Link" (gap 8), then the actions (gap 8): IconButton size small
//     (28px, the 16px icon) new-window, "Open in new tab" (window.open
//     _blank), and copy, "Copy link", whose tooltip says "Copied!" for 2s
//     after a click (clipboard.writeText of the href). Both tooltips sit
//     below their buttons.
//   - The URL, lowercased and cut at maxDisplayLength 2000 with "…", in
//     Truncate lines 5, word-break: break-all.
//
// Its strings are url_hover_card's (translated because the experiment
// that shows the card is on).
type URLHoverCard struct {
	h      Host
	anchor image.Rectangle
	url    string
	rect   image.Rectangle

	open, copy       widget.Clickable
	openTip, copyTip TipState
	copiedUntil      time.Time
	openR, copyR     image.Rectangle // window coordinates (Buttons)
}

var nsURLCard = i18n.NS("url_hover_card")

// urlCardMaxDisplay is UrlHoverCard's maxDisplayLength default.
const urlCardMaxDisplay = 2000

// NewURLHoverCard is the card for a link to url over anchor (window
// coordinates).
func NewURLHoverCard(h Host, anchor image.Rectangle, url string) *URLHoverCard {
	return &URLHoverCard{h: h, anchor: anchor, url: url,
		openTip: TipState{Delay: 150 * time.Millisecond}, copyTip: TipState{Delay: 150 * time.Millisecond}}
}

// URLCardText is the URL as the card shows it: lowercased, cut at 2000
// characters with "…".
func URLCardText(u string) string {
	b := strings.ToLower(u)
	if utf8.RuneCountInString(b) > urlCardMaxDisplay {
		r := []rune(b)
		b = string(r[:urlCardMaxDisplay]) + "…"
	}
	return b
}

// URL is the link the card is for.
func (p *URLHoverCard) URL() string { return p.url }

// Rect is the drawn card (window coordinates).
func (p *URLHoverCard) Rect() image.Rectangle { return p.rect }

// CopyTip is the copy button's tooltip text now: "Copied!" for 2s after a
// click, else "Copy link".
func (p *URLHoverCard) CopyTip(now time.Time) string {
	if now.Before(p.copiedUntil) {
		return nsURLCard.T("Copied!")
	}
	return nsURLCard.T("Copy link")
}

// Buttons are the action buttons' boxes as last drawn, window
// coordinates: Open in new tab, then Copy link (tests).
func (p *URLHoverCard) Buttons() (open, copy image.Rectangle) { return p.openR, p.copyR }

// Layout draws the card in the window (gtx at the window origin).
func (p *URLHoverCard) Layout(gtx layout.Context) layout.Dimensions {
	th := p.h.Theme()
	cl := colorsOf(th)
	win := gtx.Constraints.Max
	if p.open.Clicked(gtx) {
		_ = openurl.Open(p.url) // window.open(url, "_blank", "noopener,noreferrer")
	}
	if p.copy.Clicked(gtx) {
		copyText(gtx, p.url)
		p.copiedUntil = gtx.Now.Add(2 * time.Second)
		gtx.Execute(op.InvalidateCmd{At: p.copiedUntil})
	}

	pad, gap, b := dp(gtx, 16), dp(gtx, 8), dp(gtx, 28)
	label := nsURLCard.T("Link")
	shown := URLCardText(p.url)
	labelW := dp(gtx, 16) + gap + textW(gtx, th, 15, theme.Bold, label)
	headW := labelW + gap + 2*b + gap
	// fit-content between min-width 280 and max-width 400 (border-box)
	w := max(headW, textW(gtx, th, 15, theme.Regular, shown)) + 2*pad
	w = min(max(w, dp(gtx, 280)), dp(gtx, 400))
	if mw := win.X - 2*dp(gtx, hovercard.AllowanceX); w > mw {
		w = max(mw, 1)
	}
	inner := max(1, w-2*pad)
	call, d := record(loose(gtx, image.Pt(w, win.Y)), func(gtx layout.Context) layout.Dimensions {
		// the header row: the link icon and "Link", the actions at the end
		at(gtx, image.Pt(pad, pad+(b-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, "link", 16, cl.Fg)
		})
		at(gtx, image.Pt(pad+dp(gtx, 16)+gap, pad), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, b, func(gtx layout.Context) layout.Dimensions {
				return th.Label(gtx, 15, theme.Bold, cl.Fg, label)
			})
		})
		btn := func(x int, c *widget.Clickable, st *TipState, name, tip string) {
			at(gtx, image.Pt(x, pad), func(gtx layout.Context) layout.Dimensions {
				col := cl.Fg2 // c-icon_button--default: content-sec, content-pry on hover
				if c.Hovered() {
					col = cl.Fg
				}
				d := iconButton(gtx, c, name, 28, 16, col, cl.BgHover, 8)
				Tooltip(gtx, th, st, c.Hovered(), d.Size, Below, tip, "")
				return d
			})
		}
		btn(pad+inner-2*b-gap, &p.open, &p.openTip, "new-window", nsURLCard.T("Open in new tab"))
		btn(pad+inner-b, &p.copy, &p.copyTip, "copy", p.CopyTip(gtx.Now))
		// the URL: five lines at most, broken anywhere
		ud := at(gtx, image.Pt(pad, pad+b+gap), func(gtx layout.Context) layout.Dimensions {
			g := loose(gtx, image.Pt(inner, 1<<20))
			return th.Layout(g, theme.TextStyle{Size: unit.Sp(15), Weight: theme.Regular, Color: cl.Fg, LineHeight: unit.Sp(22),
				MaxLines: 5, Wrap: gtext.WrapGraphemes}, shown)
		})
		return layout.Dimensions{Size: image.Pt(w, pad+b+gap+ud.Size.Y+pad)}
	})
	sz := image.Pt(w, d.Size.Y)
	pos := hovercard.PlaceTop(p.anchor, sz, win, dp(gtx, hovercard.OffsetY), dp(gtx, hovercard.AllowanceX), dp(gtx, hovercard.AllowanceY))
	p.rect = image.Rectangle{Min: pos, Max: pos.Add(sz)}
	bx := image.Rect(pad+inner-b, pad, pad+inner, pad+b).Add(pos)
	p.copyR, p.openR = bx, bx.Sub(image.Pt(b+gap, 0))
	rad := dp(gtx, 8)
	card(gtx, cl, p.rect, rad)
	cs := clipRRect(gtx, p.rect, rad)
	macroAt(gtx, call, pos)
	cs.Pop()
	return layout.Dimensions{Size: win}
}

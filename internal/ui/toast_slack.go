package ui

import (
	"image"
	"image/color"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/gesture"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Slack's Toast (client-boot c5oy over 4HH1, CSS .c-toast): at the
// window's bottom right, 20px in (getToastOffset), base-inv-pry with
// content-inv-pry text, radius 8, padding 12 16, 15px on 1.5 lines, at
// most 350px wide; the svgIcon (20px) 8px before the text
// (.c-toast__wrapper gap 8); a 1px rgba(29,28,29,.13) ring and 0 4px 12px
// #00000014 under it; gone after its duration (6s by default). The share
// dialog's ScheduleForwardToast is one ("Message scheduled", the
// schedule-send icon, shouldAutoFade).

// ToastIcon shows text in Slack's Toast with icon (popups.IconToaster).
func (a *App) ToastIcon(text, icon string) {
	a.toast = toastState{text: text, icon: icon, until: time.Now().Add(6 * time.Second)}
	a.Invalidate()
}

// layoutSlackToast draws the icon toast.
func (a *App) layoutSlackToast(gtx layout.Context) {
	c := &a.th.C
	sz := gtx.Constraints.Max
	px, py := gtx.Dp(16), gtx.Dp(12)
	is, gap := gtx.Dp(20), gtx.Dp(8)
	lh := gtx.Dp(22) // 15px × 1.5
	tw := a.th.Measure(gtx, 15, theme.Regular, a.toast.text)
	if max := gtx.Dp(350) - 2*px - is - gap; tw > max {
		tw = max
	}
	w, h := 2*px+is+gap+tw, 2*py+lh
	off := gtx.Dp(20)
	r := image.Rect(sz.X-off-w, sz.Y-off-h, sz.X-off, sz.Y-off)
	theme.Shadow(gtx, r.Add(image.Pt(0, gtx.Dp(4))), gtx.Dp(8), gtx.Dp(12), theme.WithAlpha(c.PanelShadow, 0x14))
	theme.RRect(gtx, r.Inset(-max(1, gtx.Dp(1))), gtx.Dp(8)+1, theme.WithAlpha(c.Fg, 0x21))
	theme.RRect(gtx, r, gtx.Dp(8), c.Tooltip)
	at(gtx, image.Pt(r.Min.X+px, r.Min.Y+(h-is)/2), image.Pt(is, is), func(gtx layout.Context) layout.Dimensions {
		return icons.Get(a.toast.icon)(gtx, unit.Dp(20), c.TooltipText)
	})
	at(gtx, image.Pt(r.Min.X+px+is+gap, r.Min.Y), image.Pt(tw, h), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
			return a.th.Label(gtx, 15, theme.Regular, c.TooltipText, a.toast.text)
		})
	})
}

// ToastParts shows Slack's Toast with an rt() string's parts: <strong> and
// <b> runs bold, <a> runs underlined links (.c-toast a: content-inv-pry,
// underlined) that open their href, lines wrapping inside the 350px
// toast; icon ("" none) is the svgIcon in iconCol (zero: the text's).
// Slack's channel section error toasts (wAP7, A46L: warning-filled in
// sk_raspberry_red) and the bulk leave's "We’re tidying up …"
// (LeaveChannelsInProgressToast, NIXs) are ones; fadeDelay 5s for the
// section toasts, Toast's default 6s for the leave's (d).
func (a *App) ToastParts(parts []i18n.Part, icon string, iconCol color.NRGBA, d time.Duration) {
	a.toast = toastState{text: i18n.Plain(parts), parts: parts, icon: icon, iconCol: iconCol, rich: true, until: time.Now().Add(d)}
	a.Invalidate()
}

// slackRed is --dt_color-brand-core-slack-red (.sk_raspberry_red).
var slackRed = color.NRGBA{R: 0xe0, G: 0x1e, B: 0x5a, A: 0xff}

// toastWord is one laid-out word of a rich toast.
type toastWord struct {
	text   string
	weight font.Weight
	href   string
	x, w   int
	line   int
	space  bool // a space before it on its line
}

// layoutRichToast draws ToastParts' toast.
func (a *App) layoutRichToast(gtx layout.Context) {
	c := &a.th.C
	sz := gtx.Constraints.Max
	px, py := gtx.Dp(16), gtx.Dp(12)
	is, gap := 0, 0
	if a.toast.icon != "" {
		is, gap = gtx.Dp(20), gtx.Dp(8)
	}
	lh := gtx.Dp(22) // 15px × 1.5
	maxW := gtx.Dp(350) - 2*px - is - gap
	words, lines, tw := a.wrapToastParts(gtx, a.toast.parts, maxW)
	w, h := 2*px+is+gap+tw, 2*py+lines*lh
	off := gtx.Dp(20)
	r := image.Rect(sz.X-off-w, sz.Y-off-h, sz.X-off, sz.Y-off)
	theme.Shadow(gtx, r.Add(image.Pt(0, gtx.Dp(4))), gtx.Dp(8), gtx.Dp(12), theme.WithAlpha(c.PanelShadow, 0x14))
	theme.RRect(gtx, r.Inset(-max(1, gtx.Dp(1))), gtx.Dp(8)+1, theme.WithAlpha(c.Fg, 0x21))
	theme.RRect(gtx, r, gtx.Dp(8), c.Tooltip)
	if a.toast.icon != "" {
		ic := a.toast.iconCol
		if ic == (color.NRGBA{}) {
			ic = c.TooltipText
		}
		// .c-toast__wrapper centres its items
		at(gtx, image.Pt(r.Min.X+px, r.Min.Y+(h-is)/2), image.Pt(is, is), func(gtx layout.Context) layout.Dimensions {
			return icons.Get(a.toast.icon)(gtx, unit.Dp(20), ic)
		})
	}
	tx := r.Min.X + px + is + gap
	for _, wd := range words {
		wd := wd
		y := r.Min.Y + py + wd.line*lh
		at(gtx, image.Pt(tx+wd.x, y), image.Pt(wd.w+gtx.Dp(2), lh), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return a.th.Label(gtx, 15, wd.weight, c.TooltipText, wd.text)
			})
		})
		if wd.href == "" {
			continue
		}
		lr := image.Rect(tx+wd.x, y, tx+wd.x+wd.w, y+lh)
		if wd.space { // the underline runs under the space between a link's words
			lr.Min.X -= a.th.Measure(gtx, 15, wd.weight, " ")
		}
		theme.Fill(gtx, image.Rect(lr.Min.X, y+gtx.Dp(18), lr.Max.X, y+gtx.Dp(18)+max(1, gtx.Dp(1))), c.TooltipText)
		a.toastLink(gtx, lr, wd.href)
	}
}

// wrapToastParts lays parts out in words over lines at most maxW wide; it
// returns them, the line count and the widest line.
func (a *App) wrapToastParts(gtx layout.Context, parts []i18n.Part, maxW int) ([]toastWord, int, int) {
	var words []toastWord
	line, x, widest := 0, 0, 0
	pendingSpace := false
	for _, p := range parts {
		wt := theme.Regular
		if p.Tag == "strong" || p.Tag == "b" {
			wt = theme.Bold
		}
		href := ""
		if p.Tag == "a" {
			href = toastHelpURL
		}
		for i, f := range strings.Split(p.Text, " ") {
			if i > 0 {
				pendingSpace = true
			}
			if f == "" {
				continue
			}
			ww := a.th.Measure(gtx, 15, wt, f)
			sp := 0
			if pendingSpace && x > 0 {
				sp = a.th.Measure(gtx, 15, wt, " ")
			}
			if x > 0 && x+sp+ww > maxW {
				line, x, sp = line+1, 0, 0
			}
			words = append(words, toastWord{text: f, weight: wt, href: href, x: x + sp, w: ww, line: line, space: sp > 0 && href != "" && len(words) > 0 && words[len(words)-1].href == href})
			x += sp + ww
			widest = max(widest, x)
			pendingSpace = false
		}
	}
	return words, line + 1, min(widest, maxW)
}

// toastHelpURL is renderGenericError's link: https://{qF()}/help/contact,
// qF being the slack.com domain (UQaz).
const toastHelpURL = "https://slack.com/help/contact"

// toastLink makes r (window coordinates) open href on a click.
func (a *App) toastLink(gtx layout.Context, r image.Rectangle, href string) {
	for {
		ev, ok := a.toastClick.Update(gtx.Source)
		if !ok {
			break
		}
		if ev.Kind == gesture.KindClick {
			a.OpenURL(href)
		}
	}
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	pointer.CursorPointer.Add(gtx.Ops)
	a.toastClick.Add(gtx.Ops)
}

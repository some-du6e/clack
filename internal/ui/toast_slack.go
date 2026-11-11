package ui

import (
	"image"
	"time"

	"gioui.org/layout"
	"gioui.org/unit"

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

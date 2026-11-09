package ui

import (
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// The "Demo workspace" banner (`clack demo`, Options.Demo): a strip above
// the workspace, in the realtime banner's place, that stays for the whole
// session so a demo can't be mistaken for real Slack.

const demoBannerH = unit.Dp(30)

// demoBannerH is the banner's height with its gap (0 outside the demo).
func (a *App) demoBannerHeight(gtx layout.Context) int {
	if a.opts.Demo == "" {
		return 0
	}
	return gtx.Dp(demoBannerH + bannerGap)
}

var (
	demoBannerBg     = color.NRGBA{R: 0xf2, G: 0xe6, B: 0xfa, A: 0xff} // lilac
	demoBannerBorder = color.NRGBA{R: 0x9b, G: 0x6b, B: 0xc9, A: 0xff}
	demoBannerFg     = color.NRGBA{R: 0x3b, G: 0x1a, B: 0x5a, A: 0xff}
)

func (a *App) layoutDemoBanner(gtx layout.Context, r image.Rectangle) {
	rad := gtx.Dp(8)
	theme.RRect(gtx, r, rad, demoBannerBg)
	theme.Border(gtx, r, rad, gtx.Dp(1), demoBannerBorder)
	h := r.Dy()
	pad := gtx.Dp(14)
	title := "Demo workspace"
	tw := a.th.Measure(gtx, 13, theme.Bold, title)
	at(gtx, image.Pt(r.Min.X+pad, r.Min.Y), image.Pt(tw+1, h), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
			return a.th.Label(gtx, 13, theme.Bold, demoBannerFg, title)
		})
	})
	x := r.Min.X + pad + tw + gtx.Dp(10)
	at(gtx, image.Pt(x, r.Min.Y), image.Pt(max(0, r.Max.X-pad-x), h), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
			return a.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Color: demoBannerFg, MaxLines: 1}, a.opts.Demo)
		})
	})
}

package ui

import (
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// Live updates (Slack's realtime websocket) are opt-in. Clack shares the
// session of the user's browser, and on Enterprise Grid Slack can end that
// session everywhere when it sees it used in a way it finds unusual. So
// the socket stays closed until the user turns it on: first run asks with
// a banner, Preferences > Advanced has the switch, and -no-realtime
// (CLACK_NO_REALTIME=1) forces it off. With it off, core polls the Web API
// (counts and the open conversations every 20 s).

// realtimeState is the first-run "Live updates are off" banner.
type realtimeState struct {
	on, keepOff widget.Clickable
}

// realtimeOn reports whether the socket should be open.
func (a *App) realtimeOn() bool {
	return !a.opts.NoRealtime && a.prefs.Realtime != nil && *a.prefs.Realtime
}

// setRealtime saves the choice and applies it to the running session.
func (a *App) setRealtime(on bool) {
	if a.opts.NoRealtime {
		on = false
	}
	a.prefs.Realtime = &on
	a.savePrefs()
	if a.data != nil {
		c := a.data.core
		go c.SetRealtime(on) // turning it off waits for the socket to close
	}
	if on {
		a.Toast("Live updates on. Turn them off in Preferences > Advanced.")
	}
	a.Invalidate()
}

func (a *App) toggleRealtime() { a.setRealtime(!a.realtimeOn()) }

const realtimeBannerH = unit.Dp(64)

// bannerGap is the space between the banner and the workspace.
const bannerGap = unit.Dp(6)

// realtimeAskH is the height of the first-run banner (with its gap), 0
// once the user chose or when it is forced off.
func (a *App) realtimeAskH(gtx layout.Context) int {
	if a.data == nil || !a.data.started || a.prefs.Realtime != nil || a.opts.NoRealtime {
		return 0
	}
	return gtx.Dp(realtimeBannerH + bannerGap)
}

// Slack's warning banner yellow (c-banner--warning).
var bannerWarn = color.NRGBA{R: 0xfc, G: 0xf0, B: 0xc8, A: 0xff}

// layoutRealtimeBanner asks once whether to open the realtime connection.
func (a *App) layoutRealtimeBanner(gtx layout.Context, r image.Rectangle) {
	s := &a.rtAsk
	fg := color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xff}
	theme.RRect(gtx, r, gtx.Dp(8), bannerWarn)
	theme.Border(gtx, r, gtx.Dp(8), gtx.Dp(1), color.NRGBA{R: 0xe8, G: 0xb3, B: 0x2c, A: 0xff})
	h := r.Dy()
	pad := gtx.Dp(16)
	button := func(c *widget.Clickable, label string, primary bool, right int) int {
		tw := a.th.Measure(gtx, 13, theme.Bold, label)
		bw, bh := tw+gtx.Dp(24), gtx.Dp(28)
		bx := right - bw
		at(gtx, image.Pt(bx, r.Min.Y+(h-bh)/2), image.Pt(bw, bh), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, c, func(gtx layout.Context, sz image.Point, hovered bool) {
				bg, bc := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, color.NRGBA{R: 0x86, G: 0x86, B: 0x86, A: 0xff}
				if primary {
					bg, bc = color.NRGBA{R: 0x00, G: 0x7a, B: 0x5a, A: 0xff}, bg
					if hovered {
						bg = color.NRGBA{R: 0x14, G: 0x8a, B: 0x67, A: 0xff}
					}
				} else if hovered {
					bg = color.NRGBA{R: 0xf8, G: 0xf8, B: 0xf8, A: 0xff}
				}
				theme.RRect(gtx, image.Rectangle{Max: sz}, gtx.Dp(4), bg)
				if !primary {
					theme.Border(gtx, image.Rectangle{Max: sz}, gtx.Dp(4), gtx.Dp(1), bc)
				}
			}, func(gtx layout.Context) layout.Dimensions {
				col := fg
				if primary {
					col = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
				}
				at(gtx, image.Pt(gtx.Dp(12), 0), image.Pt(tw+1, bh), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, bh, func(gtx layout.Context) layout.Dimensions {
						return a.th.Label(gtx, 13, theme.Bold, col, label)
					})
				})
				return layout.Dimensions{Size: image.Pt(bw, bh)}
			})
		})
		return bx
	}
	x := button(&s.keepOff, "Keep off", true, r.Max.X-pad)
	x = button(&s.on, "Turn on live updates", false, x-gtx.Dp(8))
	msg := "Live updates are off, so messages refresh every 20 seconds. Turning them on opens Slack's realtime connection. " +
		"Clack shares your browser's session, and on some Enterprise Grid workspaces Slack can sign that session out everywhere if it notices."
	at(gtx, image.Pt(r.Min.X+pad, r.Min.Y), image.Pt(max(0, x-gtx.Dp(12)-r.Min.X-pad), h), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
			return a.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Color: fg, MaxLines: 3}, msg)
		})
	})
	if clicked(&s.on) {
		a.setRealtime(true)
	}
	if clicked(&s.keepOff) {
		a.setRealtime(false)
	}
}

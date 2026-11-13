package popups

import (
	"image"
	"image/color"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// PeekContainer paints Slack's ChannelHeaderPeekContainer box (boot-shared
// 07S2, client-boot-styles .container__ZCbmL, build 3889f0c93d30) at r:
// ctr-pry, radius 12, box-shadow 0 0 0 1px rgba(var(--sk_foreground_low),
// .13) and 0 18px 48px #0000001a. The channel-tab peeks (a channel-tab
// slug's ChannelTabSlugPeekTrigger, a channel tab's ChannelTabPeekTrigger)
// draw their content inside it.
func PeekContainer(gtx layout.Context, th *theme.Theme, r image.Rectangle) {
	cl := colorsOf(th)
	rad := dp(gtx, 12)
	softShadow(gtx, r, rad, dp(gtx, 18), dp(gtx, 48), color.NRGBA{A: 0x1a})
	ring := color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0x21}
	if th.Dark {
		ring = color.NRGBA{R: 0xd1, G: 0xd2, B: 0xd3, A: 0x21}
	}
	theme.SetTok(th.Tokens, &ring, theme.Both("rgba(var(--sk_foreground_low),0.13)"), th.Dark)
	cardRing(gtx, r, rad, theme.Over(ring, cl.Bg), cl.Bg)
	rrect(gtx, r, rad, cl.Bg)
}

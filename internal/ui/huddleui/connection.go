package huddleui

import (
	"image"
	"image/color"

	"gioui.org/op"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The connection-quality button at the right of HuddleCompactHeader
// (gantry-v2-async-client-boot-shared XuJY; build 274f7c7f1b69):
//
//   - .p-huddle_compact_header .p-huddle_header__button: constants-white,
//     brand-core-black ink, radius 8px, padding 4px 6px around the 20px
//     icon; margin-left auto (the header's right end). It isn't drawn with
//     huddles_new_grids on (pSxm KR).
//   - the icon is useDetermineTroubleshootingIcon (TgjK): four bars on a
//     20px grid; no connection (noConnection, or the call's isDisconnected)
//     dims them to .32 under an ×; otherwise the call's MOS picks four
//     bars (> 3.9, or no MOS yet), two (3.2 to 3.9) or one (< 3.2).
//     Clack's media layer measures no MOS, so a connected call shows four
//     bars, a reconnecting one the ×.
//
// Slack's click opens the troubleshooting panel (TroubleshootingPanel,
// iHiP), which Clack doesn't have: the button is drawn without hover or
// click (docs/HUDDLES-AUDIT.md).
const (
	connBtnW = 32 // 6 + 20 + 6
	connBtnH = 28 // 4 + 20 + 4
)

var connInk = color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xff} // brand-core-black

// connectionButton draws the button with its top left at p.
func (w *window) connectionButton(gtx C, st huddle.State, p image.Point) {
	r := image.Rectangle{Min: p, Max: p.Add(image.Pt(gtx.Dp(connBtnW), gtx.Dp(connBtnH)))}
	theme.RRect(gtx, r, gtx.Dp(8), hudWhite)
	lost := st.Phase == huddle.Reconnecting
	connIcon(gtx, image.Pt(r.Min.X+gtx.Dp(6), r.Min.Y+gtx.Dp(4)), lost)
}

// connIcon draws TgjK's icon 20dp square at p: icons' huddle-connection
// (the four bars) or, lost, the bars at opacity .32 under the ×
// (huddle-connection-x). The SVG's opacity blends in sRGB over the white
// button; theme.LinearAlpha gives the alpha that carries the same ink in
// Gio's linear-light blending.
func connIcon(gtx C, p image.Point, lost bool) {
	defer op.Offset(p).Push(gtx.Ops).Pop()
	bars := connInk
	if lost {
		bars = theme.LinearAlpha(theme.WithAlpha(connInk, 82), hudWhite)
	}
	icons.Get("huddle-connection")(gtx, 20, bars)
	if lost {
		icons.Get("huddle-connection-x")(gtx, 20, connInk)
	}
}

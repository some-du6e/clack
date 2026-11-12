package huddleui

import (
	"image"
	"image/color"

	"gioui.org/f32"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/huddle"
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

// connBars are TgjK's bars: x, y, height on the 20px grid (2.5 wide, rx .5).
var connBars = [4][3]float32{{2, 14, 3}, {6.5, 11, 6}, {11, 7, 10}, {15.5, 3, 14}}

// connectionButton draws the button with its top left at p.
func (w *window) connectionButton(gtx C, st huddle.State, p image.Point) {
	r := image.Rectangle{Min: p, Max: p.Add(image.Pt(gtx.Dp(connBtnW), gtx.Dp(connBtnH)))}
	theme.RRect(gtx, r, gtx.Dp(8), hudWhite)
	lost := st.Phase == huddle.Reconnecting
	connIcon(gtx, image.Pt(r.Min.X+gtx.Dp(6), r.Min.Y+gtx.Dp(4)), float32(gtx.Dp(20)), lost)
}

// connIcon draws TgjK's icon in a size×size box at p: four bars, or
// (lost) the bars at .32 and the ×.
func connIcon(gtx C, p image.Point, size float32, lost bool) {
	u := size / 20
	o := f32.Pt(float32(p.X), float32(p.Y))
	for _, b := range connBars {
		c := connInk
		if lost {
			c.A = 82 // opacity .32
		}
		rr := clip.RRect{Rect: image.Rect(int(o.X+b[0]*u+.5), int(o.Y+b[1]*u+.5), int(o.X+(b[0]+2.5)*u+.5), int(o.Y+(b[1]+b[2])*u+.5)), SE: int(.5*u + .5), SW: int(.5*u + .5), NE: int(.5*u + .5), NW: int(.5*u + .5)}
		paint.FillShape(gtx.Ops, c, rr.Op(gtx.Ops))
	}
	if !lost {
		return
	}
	// M3 4 L8 9 and M8 4 L3 9, stroke 1.5, round caps
	for _, l := range [2][2]f32.Point{{{X: 3, Y: 4}, {X: 8, Y: 9}}, {{X: 8, Y: 4}, {X: 3, Y: 9}}} {
		var path clip.Path
		path.Begin(gtx.Ops)
		path.MoveTo(o.Add(l[0].Mul(u)))
		path.LineTo(o.Add(l[1].Mul(u)))
		paint.FillShape(gtx.Ops, connInk, clip.Stroke{Path: path.End(), Width: 1.5 * u}.Op())
	}
}

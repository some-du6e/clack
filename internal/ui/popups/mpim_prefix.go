package popups

import (
	"image"
	"image/color"
	"strconv"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op/clip"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// MpimPrefix is Slack's BaseChannelPrefixMpim (boot-shared kn1I) with
// variant "secondary", as the channel header's and the details' MPDMTitle
// draw it before a named group DM's name: the first other member's avatar
// (sizeDp; 16 there) clipped by MPIMMask ("M1,0 H0 V1 H0.4 V0.625 C0.4,0.501
// 0.501,0.4 0.625,0.4 H1 V0" in the avatar's box), and the counter
// (mpimCounter: the avatar's size, font 75% of it, radius 20%, centred on
// the avatar's bottom-right corner; secondary: --dt_color-ctr-sec,
// content.sec, box-shadow 0 1px 3px #00000019). count is
// getMpimMemberCount (max(members-1, 2)). avatar draws the avatar at the
// constraints' size; nil leaves the masked area empty.
func MpimPrefix(gtx layout.Context, th *theme.Theme, sizeDp float32, count int, avatar layout.Widget) layout.Dimensions {
	cl := colorsOf(th)
	s := dp(gtx, sizeDp)
	if avatar != nil {
		fs := float32(s)
		var p clip.Path
		p.Begin(gtx.Ops)
		p.MoveTo(f32.Pt(fs, 0))
		p.LineTo(f32.Pt(0, 0))
		p.LineTo(f32.Pt(0, fs))
		p.LineTo(f32.Pt(0.4*fs, fs))
		p.LineTo(f32.Pt(0.4*fs, 0.625*fs))
		p.CubeTo(f32.Pt(0.4*fs, 0.501*fs), f32.Pt(0.501*fs, 0.4*fs), f32.Pt(0.625*fs, 0.4*fs))
		p.LineTo(f32.Pt(fs, 0.4*fs))
		p.Close()
		st := clip.Outline{Path: p.End()}.Op().Push(gtx.Ops)
		ag := gtx
		ag.Constraints = layout.Exact(image.Pt(s, s))
		avatar(ag)
		st.Pop()
	}
	o := s / 2
	r := image.Rect(o, o, o+s, o+s)
	rad := s / 5
	theme.Shadow(gtx, r.Add(image.Pt(0, dp(gtx, 1))), rad, dp(gtx, 3), color.NRGBA{A: 0x19})
	ctr := rgb(0xf8f8f8) // --dt_color-ctr-sec
	if th != nil && th.Dark {
		ctr = rgb(0x212428)
	}
	rrect(gtx, r, rad, ctr)
	n := strconv.Itoa(count)
	fsz := sizeDp * 0.75 // font-size: calc(var(--mpim-size) * .75)
	tw := textW(gtx, th, fsz, theme.Regular, n)
	at(gtx, image.Pt(o+(s-tw)/2, o), func(gtx layout.Context) layout.Dimensions {
		return vcenter(exact(gtx, image.Pt(tw, s)), s, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min = image.Point{}
			return text(gtx, th, fsz, theme.Regular, cl.Fg2, n) // content.sec
		})
	})
	return layout.Dimensions{Size: image.Pt(o+s, o+s)}
}

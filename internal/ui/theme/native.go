package theme

import (
	"image"
	"image/color"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// Native form controls. Slack's Preferences checkboxes and radios are
// <input class="c-input_checkbox|c-input_radio"> with appearance:auto and
// accent-color:auto (computed in the live dialog, 2026-11-09), so Chromium
// paints them: a 13px box, not a Slack token. Their scheme is the page's
// color-scheme, which slack-kit's token sheet sets per mode
// (`:root,.sk-client-theme--light{color-scheme:light}`,
// `.sk-client-theme--dark{color-scheme:dark}`); no account theme or theme
// token reaches them. The colours are Chromium's web native-control
// colours, read off Chrome 157's render of plain checkboxes and radios in
// both schemes (normal and :hover).
type nativeColors struct {
	accent, accentHover color.NRGBA // checked fill, radio ring and dot
	bg                  color.NRGBA // unchecked fill, radio gap
	border, borderHover color.NRGBA // unchecked 1px outline
	check               color.NRGBA // the checkmark
	disAccent, disCheck color.NRGBA // :disabled checked
	disBorder, disBg    color.NRGBA // :disabled unchecked
}

var nativeLight = nativeColors{
	accent: rgb(0x0075ff), accentHover: rgb(0x005cc8),
	bg:     rgb(0xffffff),
	border: rgb(0x767676), borderHover: rgb(0x4f4f4f),
	check:     rgb(0xffffff),
	disAccent: rgb(0xd1d1d1), disCheck: rgb(0xededed),
	disBorder: rgb(0xd1d1d1), disBg: rgb(0xf8f8f8),
}

var nativeDark = nativeColors{
	accent: rgb(0x99c8ff), accentHover: rgb(0xd1e6ff),
	bg:     rgb(0x3b3b3b),
	border: rgb(0x858585), borderHover: rgb(0xacacac),
	check:     rgb(0x3b3b3b),
	disAccent: rgb(0x757575), disCheck: rgb(0x3b3b3b),
	disBorder: rgb(0x626262), disBg: rgb(0x3b3b3b),
}

// NativeAccent is the checked fill of a native checkbox in the mode.
func NativeAccent(dark bool) color.NRGBA {
	if dark {
		return nativeDark.accent
	}
	return nativeLight.accent
}

// NativeCheckSize is a native checkbox's or radio's box (dp).
const NativeCheckSize = 13

// NativeCheckState is how a native checkbox or radio is drawn.
type NativeCheckState struct {
	Radio, On, Hover bool
	Disabled         bool    // :disabled
	Opacity          float32 // 0 or 1: opaque; Slack's .c-input_checkbox--disabled is .38
}

// NativeCheck paints Chromium's checkbox (radius 2, 1px border, a 2px
// check) or radio (1px ring, 1.5px gap, 8px dot) at the origin, 13dp,
// in dark's color-scheme.
func NativeCheck(gtx layout.Context, dark bool, s NativeCheckState) layout.Dimensions {
	nc := nativeLight
	if dark {
		nc = nativeDark
	}
	sz := gtx.Dp(NativeCheckSize)
	if s.Opacity > 0 && s.Opacity < 1 {
		defer paint.PushOpacity(gtx.Ops, s.Opacity).Pop()
	}
	accent, border, bg, check := nc.accent, nc.border, nc.bg, nc.check
	if s.Hover && !s.Disabled {
		accent, border = nc.accentHover, nc.borderHover
	}
	if s.Disabled {
		accent, border, bg, check = nc.disAccent, nc.disBorder, nc.disBg, nc.disCheck
	}
	px := func(v float32) float32 { return v * float32(sz) / NativeCheckSize }
	r := image.Rect(0, 0, sz, sz)
	bw := max(1, gtx.Dp(1))
	if s.Radio {
		c := image.Pt(sz/2, sz/2)
		if s.On {
			Circle(gtx, c, sz, accent)
			Circle(gtx, c, sz-2*bw, bg)
			d := int(px(8) + .5)
			Circle(gtx, c, d, accent)
		} else {
			Circle(gtx, c, sz, border)
			Circle(gtx, c, sz-2*bw, bg)
		}
		return layout.Dimensions{Size: r.Max}
	}
	rad := gtx.Dp(2)
	if !s.On {
		RRect(gtx, r, rad, border)
		RRect(gtx, r.Inset(bw), max(0, rad-bw), bg)
		return layout.Dimensions{Size: r.Max}
	}
	RRect(gtx, r, rad, accent)
	// the check's centre line, measured off an 8x render: (2.7, 6.6) down
	// to (5.1, 9.0) and up to (10.4, 2.4), 1.95px wide
	var p clip.Path
	p.Begin(gtx.Ops)
	p.MoveTo(f32.Pt(px(2.7), px(6.6)))
	p.LineTo(f32.Pt(px(5.13), px(9.03)))
	p.LineTo(f32.Pt(px(10.4), px(2.44)))
	paint.FillShape(gtx.Ops, check, clip.Stroke{Path: p.End(), Width: px(1.95)}.Op())
	return layout.Dimensions{Size: r.Max}
}

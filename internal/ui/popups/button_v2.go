package popups

import (
	"image"
	"image/color"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// buttonV2 draws Slack Kit's v2 Button (theme/button.go) for kind:
// BtnPrimary is "primary", BtnOutline "secondary", BtnDanger "danger" and
// BtnDisabled "disabled". ring draws the :focus-visible ring.
func buttonV2(gtx layout.Context, th *theme.Theme, c *widget.Clickable, kind BtnKind, size theme.ButtonV2Size, label, iconName string, ring bool) layout.Dimensions {
	return buttonV2Grp(gtx, th, c, kind, size, label, iconName, ring, grpNone)
}

// buttonV2Grp is buttonV2 in a ButtonGroup (button_group.go): inGroup's
// _first / _last corners, the primary's divider, the last icon-only
// button's narrower width.
func buttonV2Grp(gtx layout.Context, th *theme.Theme, c *widget.Clickable, kind BtnKind, size theme.ButtonV2Size, label, iconName string, ring bool, grp grpSide) layout.Dimensions {
	variation := "primary"
	switch kind {
	case BtnOutline:
		variation = "secondary"
	case BtnDanger:
		variation = "danger"
	case BtnDisabled:
		variation = "disabled"
	}
	pal := theme.ButtonV2Palette(th, variation)
	hf, rf, ff, isf := theme.ButtonV2Metrics(size)
	h, rad, pad := dp(gtx, hf), dp(gtx, rf), dp(gtx, theme.ButtonV2PadX)
	tw := 0
	if label != "" {
		tw = textW(gtx, th, ff, theme.Bold, label)
	}
	iw := 0
	if iconName != "" {
		iw = dp(gtx, isf)
		if label != "" {
			iw += dp(gtx, theme.ButtonV2Gap)
		}
	}
	w := tw + iw + 2*pad
	if label == "" { // isIconOnly: as wide as tall
		w = h
		if grp == grpLast { // inGroup isIconOnly _last: width 6 / 7 / 9
			w = dp(gtx, v2GroupIconW(size))
		}
	}
	sz := image.Pt(max(w, gtx.Constraints.Min.X), h)
	draw := func(gtx layout.Context) layout.Dimensions {
		r := image.Rectangle{Max: sz}
		bg := pal.Bg
		if kind != BtnDisabled {
			switch {
			case c.Pressed():
				bg = pal.Pressed
			case c.Hovered():
				bg = pal.Hover
			}
		}
		if ring {
			// box-shadow a11y: 1px otl-hgl-1, then 4px of it at 30%
			solid := theme.ButtonV2Focus(th)
			FocusRingColors4(gtx, solid, theme.WithAlpha(solid, 0x4d), r, rad)
		}
		if pal.Border.A > 0 {
			in := r.Inset(border1(gtx))
			if grp == grpLast { // _notFirst: borderInlineStartWidth 0
				in.Min.X = r.Min.X
			}
			rrectGrp(gtx, r, rad, grp, pal.Border)
			rrectGrp(gtx, in, max(0, rad-border1(gtx)), grp, bg)
		} else {
			rrectGrp(gtx, r, rad, grp, bg)
		}
		if grp == grpFirst && (variation == "primary" || variation == "danger") {
			// inGroup _notLast: a 1px end border, jade.60 (dark jade.30)
			// on primary, flamingo.40 (dark flamingo.50) on danger
			fill(gtx, image.Rect(r.Max.X-border1(gtx), 0, r.Max.X, r.Max.Y), v2GroupDivider(th, variation))
		}
		x := (sz.X - tw - iw) / 2
		if iconName != "" {
			at(gtx, image.Pt(x, (h-dp(gtx, isf))/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, iconName, isf, pal.Fg)
			})
			x += iw
		}
		if label != "" {
			at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
				return vcenter(exact(gtx, image.Pt(tw, h)), h, func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min = image.Point{}
					return text(gtx, th, ff, theme.Bold, pal.Fg, label)
				})
			})
		}
		cur := pointer.CursorPointer
		if kind == BtnDisabled {
			cur = pointer.CursorNotAllowed // disabled: cursor not-allowed
		}
		st := clip.Rect(r).Push(gtx.Ops)
		cur.Add(gtx.Ops)
		st.Pop()
		return layout.Dimensions{Size: sz}
	}
	if kind == BtnDisabled {
		return draw(gtx)
	}
	return c.Layout(gtx, draw)
}

// FocusRingColors4 is FocusRingColors with the v2 Button's 4px halo.
func FocusRingColors4(gtx layout.Context, solid, halo color.NRGBA, r image.Rectangle, rad int) {
	hw := dp(gtx, theme.ButtonV2Halo)
	one := max(1, dp(gtx, 1))
	stroke(gtx, r.Inset(-hw), rad+hw, hw-one, halo)
	stroke(gtx, r.Inset(-one), rad+one, one, solid)
}

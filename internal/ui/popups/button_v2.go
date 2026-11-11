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
			rrect(gtx, r, rad, pal.Border)
			rrect(gtx, r.Inset(max(1, dp(gtx, 1))), max(0, rad-1), bg)
		} else {
			rrect(gtx, r, rad, bg)
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

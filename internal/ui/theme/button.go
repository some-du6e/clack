package theme

import "image/color"

// Slack Kit's v2 Button, as the slack_kit_v2_button experiment turns it on
// (gantry-v2-async-message-share-dialog jdUv isSlackKitV2ButtonEnabled;
// the component and its Panda recipe are in the same chunk, the atomic
// classes in slack-kit-gantry-v2-styles, build 4d0c78085232):
//
//	base: bold, padding-inline 3 (12px), inline-flex centred, no border,
//	  transition colors 80ms; :focus-visible box-shadow a11y
//	  (0 0 0 1px otl-hgl-1, 0 0 0 4px otl-hgl-1 at 30%)
//	size small: height 8 (32px), radius base (4), font caption (13)
//	     medium: height 9 (36px), radius large (8), font base (15)
//	     large: height 11 (44px), radius xlarge (12), font subtitle (18)
//	icons: 16 / 18 / 20, 4px from the label (gap 1)
//	primary: base.inverse.highlight.2 on inverse.primary text
//	secondary: base.primary, primary text, 1px solid tertiary (otl-ter)
//	danger: base.inverse.important on inverse.primary
//	disabled: base.tertiary, secondary text, cursor not-allowed
//	hover / active: hocusStyle / pressedStyle, the variation's -hover /
//	  -pressed token as a background-image blended with
//	  --dt_blend-mode-interaction (multiply light, lighten dark)
//	icon only: width = height (padding 0); the last in a ButtonGroup is
//	  6 / 7 / 9 (24 / 28 / 36px)
//
// Unlike the legacy c-button there is no min-width (80px medium), no 1px
// bottom padding, no shadow under the pointer, and the focus halo is 4px
// rather than 5px.

// ButtonV2Size is the v2 Button's size.
type ButtonV2Size uint8

const (
	ButtonV2Medium ButtonV2Size = iota // the default
	ButtonV2Small
	ButtonV2Large
)

// ButtonV2Metrics are a size's height, corner radius, font size and icon
// size, in dp.
func ButtonV2Metrics(s ButtonV2Size) (h, radius, font, icon float32) {
	switch s {
	case ButtonV2Small:
		return 32, 4, 13, 16
	case ButtonV2Large:
		return 44, 12, 18, 20
	}
	return 36, 8, 15, 18
}

// ButtonV2PadX is padding-inline 3, ButtonV2Gap the icon-to-label gap 1
// and ButtonV2Halo the focus ring's spread (dp).
const (
	ButtonV2PadX = 12
	ButtonV2Gap  = 4
	ButtonV2Halo = 4
)

// ButtonV2Colors is a variation's paint: the background, the text and
// icon colour, the border (zero: none), and what the pointer adds over the
// background (Over at rest is zero).
type ButtonV2Colors struct {
	Bg, Fg, Border color.NRGBA
	Hover, Pressed color.NRGBA // already blended over Bg
}

type v2Tok struct {
	tok         string
	light, dark color.NRGBA
}

func (k v2Tok) of(t *Theme) color.NRGBA {
	c := k.light
	if t != nil && t.Dark {
		c = k.dark
	}
	if t != nil && t.Tokens != nil && k.tok != "" {
		SetColor(t.Tokens, &c, k.tok)
	}
	return c
}

var (
	v2InvHgl2        = v2Tok{"var(--dt_color-base-inv-hgl-2)", rgb(0x007a5a), rgb(0x177e56)}
	v2InvHgl2Hover   = v2Tok{"var(--dt_color-base-inv-hgl-2-hover)", rgba(0x0a4032, 0x21), rgba(0xd8f5e9, 0x14)}
	v2InvHgl2Press   = v2Tok{"var(--dt_color-base-inv-hgl-2-pressed)", rgba(0x0a4032, 0x38), rgba(0xd8f5e9, 0x2e)}
	v2InvPry         = v2Tok{"var(--dt_color-content-inv-pry)", rgb(0xffffff), rgb(0xf8f8f8)}
	v2BasePry        = v2Tok{"var(--dt_color-base-pry)", rgb(0xffffff), rgb(0x1a1d21)}
	v2ContentPry     = v2Tok{"var(--dt_color-content-pry)", rgb(0x1d1c1d), rgb(0xf8f8f8)}
	v2OtlTer         = v2Tok{"var(--dt_color-otl-ter)", rgba(0x5e5d60, 0x21), rgba(0x797c81, 0x4d)}
	v2BaseSecHover   = v2Tok{"var(--dt_color-base-sec-hover)", rgba(0x454447, 0x0f), rgba(0xf8f8f8, 0x14)}
	v2BaseSecPress   = v2Tok{"var(--dt_color-base-sec-pressed)", rgba(0x454447, 0x21), rgba(0xf8f8f8, 0x2e)}
	v2InvImp         = v2Tok{"var(--dt_color-base-inv-imp)", rgb(0xe01e5a), rgb(0xd3305d)}
	v2InvImpHover    = v2Tok{"var(--dt_color-base-inv-imp-hover)", rgba(0x59000f, 0x17), rgba(0xf7dce4, 0x14)}
	v2InvImpPress    = v2Tok{"var(--dt_color-base-inv-imp-pressed)", rgba(0x59000f, 0x38), rgba(0xf7dce4, 0x2e)}
	v2BaseTer        = v2Tok{"var(--dt_color-base-ter)", rgb(0xeaeaea), rgb(0x212428)}
	v2ContentSec     = v2Tok{"var(--dt_color-content-sec)", rgb(0x454447), rgb(0xb9babd)}
	v2FocusRingSolid = v2Tok{"var(--dt_color-otl-hgl-1)", rgb(0x1264a3), rgb(0x2ba5ce)}
)

// ButtonV2Palette is variation's colours ("primary", "secondary",
// "danger", "disabled"; "pending" paints like disabled) in t's mode and
// stylesheet tokens.
func ButtonV2Palette(t *Theme, variation string) ButtonV2Colors {
	dark := t != nil && t.Dark
	var c ButtonV2Colors
	switch variation {
	case "secondary":
		c.Bg, c.Fg, c.Border = v2BasePry.of(t), v2ContentPry.of(t), v2OtlTer.of(t)
		c.Hover = BlendInteraction(c.Bg, v2BaseSecHover.of(t), dark)
		c.Pressed = BlendInteraction(c.Bg, v2BaseSecPress.of(t), dark)
	case "danger":
		c.Bg, c.Fg = v2InvImp.of(t), v2InvPry.of(t)
		c.Hover = BlendInteraction(c.Bg, v2InvImpHover.of(t), dark)
		c.Pressed = BlendInteraction(c.Bg, v2InvImpPress.of(t), dark)
	case "disabled", "pending":
		c.Bg, c.Fg = v2BaseTer.of(t), v2ContentSec.of(t)
		c.Hover, c.Pressed = c.Bg, c.Bg
	default: // primary
		c.Bg, c.Fg = v2InvHgl2.of(t), v2InvPry.of(t)
		c.Hover = BlendInteraction(c.Bg, v2InvHgl2Hover.of(t), dark)
		c.Pressed = BlendInteraction(c.Bg, v2InvHgl2Press.of(t), dark)
	}
	return c
}

// ButtonV2Focus is the :focus-visible ring's solid colour (the halo is it
// at 30%).
func ButtonV2Focus(t *Theme) color.NRGBA { return v2FocusRingSolid.of(t) }

// BlendInteraction paints overlay over an opaque bg the way Slack's
// hocusStyle does: background-blend-mode var(--dt_blend-mode-interaction)
// (multiply in light mode, lighten in dark), the blend weighted by the
// overlay's alpha.
func BlendInteraction(bg, overlay color.NRGBA, dark bool) color.NRGBA {
	a := float32(overlay.A) / 255
	mix := func(b, o uint8) uint8 {
		fb, fo := float32(b)/255, float32(o)/255
		var bl float32
		if dark {
			bl = max(fb, fo) // lighten
		} else {
			bl = fb * fo // multiply
		}
		v := (1-a)*fb + a*bl
		return uint8(v*255 + 0.5)
	}
	return color.NRGBA{R: mix(bg.R, overlay.R), G: mix(bg.G, overlay.G), B: mix(bg.B, overlay.B), A: bg.A}
}

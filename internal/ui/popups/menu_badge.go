package popups

import (
	"image"
	"image/color"
	"strconv"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// A menu row's tag and badge, as Slack Kit's MenuItem draws them (client-
// boot eBmv, build 4d0c78085232): after the label and the shortcut,
// renderTagMaybe, then renderBadgeMaybe, then the submenu caret.
//
//   - tag ({label, style}, or a component): a micro Tag (QHiw, recipe
//     jF6I): 20px tall, padding-inline 4, radius 4, 12px regular, the
//     style's colours, "white" while the row is highlighted. MenuFromTemplate
//     (OCqs) passes a template item's tag through, and
//     ChannelNotificationPrefMenu (PND5) tags a customized "Every new
//     message" or "Just mentions" with {label "Customized", style
//     "informative"}.
//   - badgeCount (with badgeStyle, default "red"): a c-badge pill (S1+c
//     Badge, maxCount 9 so 10 reads "9+"): 18px, padding 0 9 (0 7 and
//     max-width 25 for "9+"), 12px bold, margin-left 4, margin-right 8
//     before a submenu caret; "white" while highlighted, though
//     .c-menu_item__li--highlighted .c-badge hides it there except in
//     search's More filters menu (.p-more_filters_dropdown__menu).
//
// The colours are the styles' tokens (client-boot-styles and
// slack-kit-gantry-v2-styles), so a theme's stylesheet recolours them.

// MenuTag is a menu row's tag (Slack's MenuItem tag prop).
type MenuTag struct {
	Label string
	// Style is the Tag style: "informative", "neutral",
	// "inverse-informative", "success", "urgent", "warning" or
	// "education" ("" is "neutral", Tag's default).
	Style string
	// HighStyle is the style on the highlighted row ("" is "white", a
	// {label, style} tag's); EducationTag (Yu9P's "New") goes from
	// "inverse-education" to "education".
	HighStyle string
}

// chipTok is one colour of a tag or badge style: the token and its light
// and dark values.
type chipTok struct {
	tok         string
	light, dark color.NRGBA
}

func (c chipTok) of(th *theme.Theme) color.NRGBA {
	v := c.light
	if th != nil && th.Dark {
		v = c.dark
	}
	if th != nil && th.Tokens != nil && c.tok != "" {
		theme.SetColor(th.Tokens, &v, c.tok)
	}
	return v
}

func rgba8(v uint32, a uint8) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: a}
}

var (
	tokBaseHgl1    = chipTok{"var(--dt_color-base-hgl-1)", rgb(0xe3f8ff), rgb(0x001a2d)}
	tokSurfHgl1    = chipTok{"var(--dt_color-surf-hgl-1)", rgba8(0x36c5f0, 0x21), rgba8(0x036696, 0x40)}
	tokContentHgl1 = chipTok{"var(--dt_color-content-hgl-1)", rgb(0x1264a3), rgb(0x2ba5ce)}
	tokContentPry  = chipTok{"var(--dt_color-content-pry)", rgb(0x1d1c1d), rgb(0xf8f8f8)}
	tokContentInv  = chipTok{"var(--dt_color-content-inv-pry)", rgb(0xffffff), rgb(0xf8f8f8)}
	tokWhite       = chipTok{"var(--dt_color-constants-white)", rgb(0xffffff), rgb(0xffffff)}
	tokBlack       = chipTok{"var(--dt_color-constants-black)", rgb(0x000000), rgb(0x000000)}
)

// menuTagColors are a Tag style's background and text (recipe jF6I).
func menuTagColors(th *theme.Theme, style string) (bg, fg color.NRGBA) {
	b, f := chipTok{"var(--dt_color-surf-pry)", rgba8(0x1d1c1d, 0x0f), rgba8(0xf8f8f8, 0x0f)}, tokContentPry // neutral
	switch style {
	case "informative":
		// base.highlight.1, surface.highlight.1 in dark mode
		b, f = tokBaseHgl1, tokContentHgl1
		if th != nil && th.Dark {
			b = tokSurfHgl1
		}
	case "inverse-informative":
		b, f = chipTok{"var(--dt_color-base-inv-hgl-1)", rgb(0x1264a3), rgb(0x0a77a7)}, tokContentInv
	case "success":
		b = chipTok{"var(--dt_color-surf-hgl-2)", rgba8(0x4cc894, 0x40), rgba8(0x106f4d, 0x54)}
		f = chipTok{"var(--dt_color-content-hgl-2)", rgb(0x007a5a), rgb(0x3daa7c)}
	case "urgent":
		b = chipTok{"var(--dt_color-surf-imp)", rgba8(0xff81aa, 0x21), rgba8(0xb41541, 0x40)}
		f = chipTok{"var(--dt_color-content-imp)", rgb(0xc01343), rgb(0xde678a)}
	case "warning":
		b = chipTok{"var(--dt_color-surf-hgl-3)", rgba8(0xffc600, 0x2e), rgba8(0xa07300, 0x2e)}
		f = chipTok{"var(--dt_color-content-hgl-3)", rgb(0x6b5000), rgb(0xdea700)}
	case "education":
		// base.education and content.education (EducationTag, 7ux/:
		// the + menu's "New" on its GIF row)
		b = chipTok{"var(--dt_color-base-education)", rgb(0xf0f3fc), rgb(0x040b3d)}
		f = chipTok{"var(--dt_color-content-education)", rgb(0x112377), rgb(0xe4e8f4)}
	case "inverse-education":
		// base.inverse.education and inverse.education (jF6I): the bell's
		// More options "New" (Yu9P) off the highlight
		b = chipTok{"var(--dt_color-base-inv-education)", rgb(0x2840a9), rgb(0xc7ceeb)}
		f = chipTok{"var(--dt_color-content-inv-education)", rgb(0xf0f3fc), rgb(0x0a165a)}
	case "white":
		// constants.white with palettes.ocean.70 (ocean.30 in dark mode:
		// rgb(var(--dt_color-plt-ocean-30)) is 19,87,149 there)
		b, f = tokWhite, chipTok{"", rgb(0x1264a3), rgb(0x135795)}
	}
	return b.of(th), f.of(th)
}

// menuBadgeColors are a c-badge style's background and text.
func menuBadgeColors(th *theme.Theme, style string) (bg, fg color.NRGBA) {
	var b, f chipTok
	switch style {
	case "themed":
		b, f = themedBadge[0], themedBadge[1] // channel_card.go
	case "blue":
		b, f = tokSurfHgl1, tokContentHgl1
	case "white":
		b, f = tokWhite, chipTok{"var(--dt_color-base-inv-hgl-1)", rgb(0x1264a3), rgb(0x0a77a7)}
	case "black":
		b, f = tokBlack, chipTok{"var(--dt_color-constants-white)", rgb(0xffffff), rgb(0xffffff)}
	case "min_gray":
		b = chipTok{"var(--dt_color-base-sec)", rgb(0xf8f8f8), rgb(0x1a1d21)}
		f = chipTok{"var(--dt_color-content-sec)", rgb(0x454447), rgb(0xb9babd)}
	default: // red
		b, f = chipTok{"var(--dt_color-base-inv-imp)", rgb(0xe01e5a), rgb(0xd3305d)}, tokContentInv
	}
	return b.of(th), f.of(th)
}

// badgeText is Badge's label: the count, or "9+" past maxCount 9.
func badgeText(n int) (s string, capped bool) {
	if n > 9 {
		return "9+", true
	}
	return strconv.Itoa(n), false
}

// menuTagWidth is a micro Tag's width for label.
func menuTagWidth(gtx layout.Context, th *theme.Theme, label string) int {
	return textW(gtx, th, 12, theme.Regular, label) + 2*dp(gtx, 4)
}

// drawMenuTag draws the micro Tag with its right edge at right, centred
// in a row h tall; it returns the tag's left edge.
func drawMenuTag(gtx layout.Context, th *theme.Theme, t *MenuTag, hl bool, right, h int) int {
	style := t.Style
	if hl {
		style = "white"
		if t.HighStyle != "" {
			style = t.HighStyle
		}
	}
	bg, fg := menuTagColors(th, style)
	bg = overMenu(th, bg)
	w, tagH := menuTagWidth(gtx, th, t.Label), dp(gtx, 20)
	r := image.Rect(right-w, (h-tagH)/2, right, (h-tagH)/2+tagH)
	rrect(gtx, r, dp(gtx, 4), bg)
	at(gtx, image.Pt(r.Min.X+dp(gtx, 4), r.Min.Y), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, tagH, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 12, theme.Regular, fg, t.Label)
		})
	})
	return r.Min.X
}

// menuBadgeWidth is the c-badge pill's width for n.
func menuBadgeWidth(gtx layout.Context, th *theme.Theme, n int) int {
	s, capped := badgeText(n)
	pad := dp(gtx, 9)
	if capped {
		pad = dp(gtx, 7)
	}
	w := textW(gtx, th, 12, theme.Bold, s) + 2*pad
	if capped {
		w = min(w, dp(gtx, 25))
	}
	return w
}

// drawMenuBadge draws the pill for n with its right edge at right,
// centred in a row h tall; it returns the pill's left edge.
func drawMenuBadge(gtx layout.Context, th *theme.Theme, n int, style string, hl bool, right, h int) int {
	if hl {
		style = "white"
	}
	bg, fg := menuBadgeColors(th, style)
	bg = overMenu(th, bg)
	s, _ := badgeText(n)
	w, bh := menuBadgeWidth(gtx, th, n), dp(gtx, 18)
	r := image.Rect(right-w, (h-bh)/2, right, (h-bh)/2+bh)
	rrect(gtx, r, bh/2, bg)
	tw := textW(gtx, th, 12, theme.Bold, s)
	at(gtx, image.Pt(r.Min.X+(w-tw)/2, r.Min.Y), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, bh, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 12, theme.Bold, fg, s)
		})
	})
	return r.Min.X
}

// overMenu composites a translucent chip colour over the menu's
// background the way the page does (Gio blends in linear light, which
// would draw it brighter), so a fill matches Slack's.
func overMenu(th *theme.Theme, c color.NRGBA) color.NRGBA {
	if c.A == 0xff {
		return c
	}
	return theme.Over(c, colorsOf(th).Bg)
}

// tagAndBadge draws it's badge (rightmost, margin-left 4) and tag ending
// at right, centred in a row h tall, and returns where the shortcut and
// the label end.
func (m *Menu) tagAndBadge(gtx layout.Context, th *theme.Theme, it *MenuItem, hl bool, right, h int) int {
	if it.BadgeCount > 0 && (!hl || it.BadgeHighlighted) {
		right = drawMenuBadge(gtx, th, it.BadgeCount, it.BadgeStyle, hl, right, h) - dp(gtx, 4)
	}
	if it.Tag != nil && it.Tag.Label != "" {
		right = drawMenuTag(gtx, th, it.Tag, hl, right, h)
	}
	return right
}

// tagAndBadgeW is the width tagAndBadge takes from the label (the row at
// rest).
func tagAndBadgeW(gtx layout.Context, th *theme.Theme, it *MenuItem) int {
	w := 0
	if it.BadgeCount > 0 {
		w += menuBadgeWidth(gtx, th, it.BadgeCount) + dp(gtx, 4)
	}
	if it.Tag != nil && it.Tag.Label != "" {
		w += menuTagWidth(gtx, th, it.Tag.Label)
	}
	return w
}

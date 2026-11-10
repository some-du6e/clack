package ui

import (
	"image/color"

	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// prefsColors are the Preferences dialog's colours, each Slack's token
// for that element. They come from the web client's stylesheets
// (client-boot-styles cf726c4f2eaecc74 for the rules, slack-kit-gantry-v2-styles
// f859766a17f82b2a for the token values in .sk-client-theme--light /
// --dark; bv1-13, 2026-11-09) and match getComputedStyle in the live dialog:
//
//	.c-tabs__tab_menu--vertical .c-tabs__tab             color var(--dt_color-content-pry)
//	  .c-tabs__tab--active                               background var(--dt_color-base-inv-hgl-1), color var(--dt_color-content-inv-pry)
//	  :not(.c-tabs__tab--active):hover                   var(--dt_color-base-pry-hover) over var(--dt_color-base-pry)
//	.p-prefs_dialog__menu .c-tabs__tab:not(--active) .c-tabs__tab_icon--left
//	                                                     color rgba(var(--sk_foreground_max),.7)
//	.p-prefs_dialog__layout                              border-top 1px rgba(var(--sk_foreground_low),.13)
//	hr                                                   border-top 1px rgb(var(--sk_foreground_low_solid))
//	body (h2, legends, labels)                           rgb(var(--sk_primary_foreground)) = th.C.Fg
//	p (the dialog's notes)                               rgba(var(--sk_foreground_max),.7)
//	.c-label__subtext                                    var(--dt_color-content-ter)
//	.c-label--disabled                                   var(--dt_color-content-sec)
//	a, .c-link, .c-link--button                          var(--dt_color-content-hgl-1)
//	.c-select_button                                     border var(--dt_color-otl-pry), background var(--dt_color-base-pry), icon var(--dt_color-content-pry)
//	.c-button--outline                                   border var(--dt_color-otl-sec), background var(--dt_color-base-pry); :hover var(--dt_color-base-sec)
//	.c-button--primary                                   background var(--dt_color-base-inv-hgl-2), color var(--dt_color-constants-white)
//
// None of them is a --dt_color-theme-* token, and Slack's theme module
// (getThemeVariables) writes only --dt_color-theme-* and the palette ramps,
// so the account theme (Aubergine, a dark sidebar, custom hexes) never
// changes the dialog: it is the same blue and grey under every ia_theme.
// A stylesheet that overrides the tokens themselves (a Slick theme,
// user.css) recolours it, as in Slack. Checkboxes and radios are native
// (theme.NativeCheck).
type prefsColors struct {
	line, hr                     color.NRGBA
	tabText, tabIcon, tabHover   color.NRGBA
	tabActive, tabActiveText     color.NRGBA
	text, note, subtext, disText color.NRGBA
	link                         color.NRGBA
	tabSec                       color.NRGBA // horizontal .c-tabs__tab: content-sec
	focus                        color.NRGBA // :focus ring, --dt_color-otl-hgl-1
	field, fieldBorder, fieldFg  color.NRGBA // .c-select_button / .c-input_text
	outline, outlineHover        color.NRGBA // .c-button--outline border, hover fill
	primary, primaryText         color.NRGBA
	divider                      color.NRGBA // Clack's rule above Plugins (Slick's: --dt_color-otl-ter)
	dark                         bool        // the native controls' color-scheme
}

// prefsLight and prefsDark are the token sheet's values per mode.
var prefsLight = prefsColors{
	line: hex8(0x1d1c1d21), hr: hex8(0xddddddff),
	tabText: hex8(0x1d1c1dff), tabIcon: hex8(0x1d1c1db3),
	tabHover:  theme.Over(hex8(0x4544470f), hex8(0xffffffff)),
	tabActive: hex8(0x1264a3ff), tabActiveText: hex8(0xffffffff),
	text: hex8(0x1d1c1dff), note: hex8(0x1d1c1db3), subtext: hex8(0x5e5d60ff), disText: hex8(0x454447ff),
	link: hex8(0x1264a3ff), tabSec: hex8(0x454447ff), focus: hex8(0x1264a3ff),
	field: hex8(0xffffffff), fieldBorder: hex8(0x7c7a7fff), fieldFg: hex8(0x1d1c1dff),
	outline: hex8(0x5e5d6073), outlineHover: hex8(0xf8f8f8ff),
	primary: hex8(0x007a5aff), primaryText: hex8(0xffffffff),
	divider: hex8(0x5e5d6021),
}

var prefsDark = prefsColors{
	line: hex8(0xe8e8e821), hr: hex8(0x35373bff),
	tabText: hex8(0xf8f8f8ff), tabIcon: hex8(0xe8e8e8b3),
	tabHover:  theme.Over(hex8(0xf8f8f814), hex8(0x1a1d21ff)),
	tabActive: hex8(0x0a77a7ff), tabActiveText: hex8(0xf8f8f8ff),
	text: hex8(0xd1d2d3ff), note: hex8(0xe8e8e8b3), subtext: hex8(0x9a9b9eff), disText: hex8(0xb9babdff),
	link: hex8(0x2ba5ceff), tabSec: hex8(0xb9babdff), focus: hex8(0x2ba5ceff),
	field: hex8(0x1a1d21ff), fieldBorder: hex8(0x797c81ff), fieldFg: hex8(0xf8f8f8ff),
	outline: hex8(0x797c8180), outlineHover: hex8(0x1a1d21ff),
	primary: hex8(0x177e56ff), primaryText: hex8(0xffffffff),
	divider: hex8(0x797c814d),
	dark:    true,
}

func prefsColorsOf(th *theme.Theme) prefsColors {
	pc := prefsLight
	if th.Dark {
		pc = prefsDark
	}
	pc.text = th.C.Fg // the body colour, already read from the theme's tokens
	t := th.Tokens
	if t == nil || !t.Overridden() {
		return pc
	}
	tc := prefsTabColorsOf(th)
	pc.tabText, pc.tabActive, pc.tabActiveText, pc.tabHover = tc.text, tc.active, tc.activeText, tc.hover
	set := func(dst *color.NRGBA, expr string) { theme.SetColor(t, dst, expr) }
	set(&pc.line, "rgba(var(--sk_foreground_low),0.13)")
	set(&pc.hr, "rgb(var(--sk_foreground_low_solid))")
	set(&pc.tabIcon, "rgba(var(--sk_foreground_max),0.7)")
	set(&pc.note, "rgba(var(--sk_foreground_max),0.7)")
	set(&pc.subtext, "var(--dt_color-content-ter)")
	set(&pc.disText, "var(--dt_color-content-sec)")
	set(&pc.link, "var(--dt_color-content-hgl-1)")
	set(&pc.tabSec, "var(--dt_color-content-sec)")
	set(&pc.focus, "var(--dt_color-otl-hgl-1)")
	set(&pc.field, "var(--dt_color-base-pry)")
	set(&pc.fieldBorder, "var(--dt_color-otl-pry)")
	set(&pc.fieldFg, "var(--dt_color-content-pry)")
	set(&pc.outline, "var(--dt_color-otl-sec)")
	set(&pc.outlineHover, "var(--dt_color-base-sec)")
	set(&pc.primary, "var(--dt_color-base-inv-hgl-2)")
	set(&pc.primaryText, "var(--dt_color-constants-white)")
	set(&pc.divider, "var(--dt_color-otl-ter)")
	return pc
}

// prefsColorsKey is what prefsColorsOf depends on.
type prefsColorsKey struct {
	th   *theme.Theme
	dark bool
	tok  uint64
	fg   color.NRGBA
	ok   bool
}

// prefsColorsCache holds prefsColorsOf for the theme it was computed for.
type prefsColorsCache struct {
	pc  prefsColors
	key prefsColorsKey
}

func (c *prefsColorsCache) get(th *theme.Theme) *prefsColors {
	k := prefsColorsKey{th: th, dark: th.Dark, fg: th.C.Fg, ok: true}
	if th.Tokens != nil {
		k.tok = th.Tokens.Key()
	}
	if c.key != k {
		c.pc, c.key = prefsColorsOf(th), k
	}
	return &c.pc
}

// colors is prefsColorsOf(p.a.th), computed once per theme change.
func (p *prefsModal) colors() *prefsColors { return p.pcc.get(p.a.th) }

// on flattens a translucent colour over the dialog's surface (Gio blends
// in linear light; Chrome in sRGB).
func (pc prefsColors) on(th *theme.Theme, c color.NRGBA) color.NRGBA {
	if c.A == 0xff {
		return c
	}
	return theme.Over(c, prefsSurface(th))
}

// prefsSurface is the dialog's .c-sk-modal background (base-pry).
func prefsSurface(th *theme.Theme) color.NRGBA { return popups.BasePryOf(th) }

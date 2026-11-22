package views

// The rail tabs' hover peeks draw these views' lists on Slack's peek card
// (PeekView, boot-shared 9eDG: .p-peek_card--as-card). PeekTrigger's
// Popover renders it through client-boot 2VER PortableReactModal, in a
// portal under document.body: outside .p-ia4_client and the sidebar's
// theme scope. So the tokens the lists paint with resolve at the root in
// the client's own theme (ctr-pry white or the dark ctr-pry instead of
// the aubergine column, content-pry text, the account theme's root
// --dt_color-theme-* over 858H / qmPJ's defaults), and no
// .p-ia4_client rule reaches it. internal/ui/rail_peek_*.go draw the
// card's header and call the views' Peek* methods for the rest.

import (
	"image"
	"image/color"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// peekPal is the card's tokens.
type peekPal struct {
	bg       color.NRGBA // --dt_color-ctr-pry
	hover    color.NRGBA // .p-peek_card .p-activity_ia4_page__item:hover: base-pry under base-pry-hover
	pryHover color.NRGBA // --dt_color-base-pry-hover, that hover's image layer
	rule     color.NRGBA // --dt_color-theme-otl-inv-pry (an item's border-bottom)
	fg       color.NRGBA // --dt_color-content-pry
	sec      color.NRGBA // --dt_color-content-sec
	ter      color.NRGBA // --dt_color-content-ter
	invSec   color.NRGBA // --dt_color-theme-content-inv-sec
	invTer   color.NRGBA // --dt_color-theme-content-inv-ter
	baseImp  color.NRGBA // --dt_color-theme-base-imp
	contImp  color.NRGBA // --dt_color-theme-content-imp
	skel     color.NRGBA // --dt_color-base-ter
	otlTer   color.NRGBA // --dt_color-otl-ter
	otlSec   color.NRGBA // --dt_color-otl-sec
	maxSolid color.NRGBA // rgb(var(--sk_foreground_max_solid))
}

// peekPalOf is the card's tokens in th's mode: kit.css and the 858H /
// qmPJ defaults (the high-contrast dark set 7uI9 is only Slack's for
// highContrast); the account theme's root-scope theme-base-imp,
// -content-imp, -content-inv-sec, -inv-ter and -otl-inv-pry (ThemeImp,
// ThemeImpText, ThemeInvSec, ThemeInvTer, ThemeOtlInvPry) where it
// changes them; a stylesheet's overrides through th.Tokens, per variable:
// a token the account theme spells (its Theme stylesheet re-spells it on
// its own ramp) gives way only to a sheet's own declaration of it, not
// to a ramp its var() chain reaches (theme.Declares).
func peekPalOf(th *theme.Theme) peekPal {
	dark := th != nil && th.Dark
	var p peekPal
	if dark {
		p = peekPal{
			bg:       rgb(0x212428),
			hover:    flatten(argb(0x14f8f8f8), rgb(0x1a1d21)),
			pryHover: argb(0x14f8f8f8),
			rule:     argb(0x38efe1f5),
			fg:       rgb(0xf8f8f8),
			sec:      rgb(0xb9babd),
			ter:      rgb(0x9a9b9e),
			invSec:   argb(0xcce3ceeb),
			invTer:   argb(0x99e3ceeb),
			baseImp:  rgb(0xcfa1de),
			contImp:  rgb(0x200221),
			skel:     rgb(0x212428),
			otlTer:   argb(0x4d797c81),
			otlSec:   argb(0x80797c81),
			maxSolid: rgb(0xababad),
		}
	} else {
		p = peekPal{
			bg:       rgb(0xffffff),
			hover:    flatten(argb(0x0f454447), rgb(0xffffff)),
			pryHover: argb(0x0f454447),
			rule:     argb(0x21611f69),
			fg:       rgb(0x1d1c1d),
			sec:      rgb(0x454447),
			ter:      rgb(0x5e5d60),
			invSec:   argb(0xcc39063a),
			invTer:   argb(0x8039063a),
			baseImp:  rgb(0x83388a),
			contImp:  rgb(0xf9edff),
			skel:     rgb(0xeaeaea),
			otlTer:   argb(0x215e5d60),
			otlSec:   argb(0x735e5d60),
			maxSolid: rgb(0x616061),
		}
	}
	if th == nil {
		return p
	}
	tk := th.Tokens
	set := func(dst *color.NRGBA, expr string) { theme.SetTok(tk, dst, theme.Both(expr), dark) }
	for _, f := range []struct {
		dst  *color.NRGBA
		acct color.NRGBA
		name string
	}{
		{&p.baseImp, th.C.ThemeImp, "--dt_color-theme-base-imp"},
		{&p.contImp, th.C.ThemeImpText, "--dt_color-theme-content-imp"},
		{&p.invSec, th.C.ThemeInvSec, "--dt_color-theme-content-inv-sec"},
		{&p.invTer, th.C.ThemeInvTer, "--dt_color-theme-content-inv-ter"},
		{&p.rule, th.C.ThemeOtlInvPry, "--dt_color-theme-otl-inv-pry"},
	} {
		if f.acct.A != 0 {
			*f.dst = f.acct // theme.IATheme.Fields' root scope
			if !theme.Declares(tk, f.name) {
				continue
			}
		}
		set(f.dst, "var("+f.name+")")
	}
	set(&p.bg, "var(--dt_color-ctr-pry)")
	set(&p.fg, "var(--dt_color-content-pry)")
	set(&p.sec, "var(--dt_color-content-sec)")
	set(&p.ter, "var(--dt_color-content-ter)")
	set(&p.skel, "var(--dt_color-base-ter)")
	set(&p.otlTer, "var(--dt_color-otl-ter)")
	set(&p.otlSec, "var(--dt_color-otl-sec)")
	set(&p.maxSolid, "rgb(var(--sk_foreground_max_solid))")
	base, hov := p.bg, color.NRGBA{}
	if dark {
		base, hov = rgb(0x1a1d21), argb(0x14f8f8f8)
	} else {
		hov = argb(0x0f454447)
	}
	b2, h2 := base, hov
	set(&b2, "var(--dt_color-base-pry)")
	set(&h2, "var(--dt_color-base-pry-hover)")
	if b2 != base || h2 != hov {
		p.hover = flatten(h2, b2)
	}
	p.pryHover = h2
	return p
}

// peekToggle is UnreadsToggleButton (boot-shared XrTy) in the peek's
// header. The card is outside .p-ia4_client, so the
// .p-ia4_client .p-unreads_toggle__* rules (theme-content-inv-sec) don't
// reach it: the 12px bold label, the 28x18 switch's 1px border and its
// 12px handle 2px in are content-sec; on (--selected) the label is
// content-pry, the switch theme-base-imp with a theme-content-imp border
// and handle, translated 10px.
func (b *base) peekToggle(gtx C, t *toggle, label string) D {
	pp := peekPalOf(b.th)
	return t.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		lc, edge, knob, fillC := peekToggleInk(pp, t.Value)
		return row(gtx, layout.Middle,
			layout.Rigid(func(gtx C) D { return b.label(gtx, 12, wBold, lc, label) }),
			hgap(8),
			layout.Rigid(func(gtx C) D {
				w, h := gtx.Dp(28), gtx.Dp(18)
				r := image.Rect(0, 0, w, h)
				bw := max(1, gtx.Dp(1))
				k := gtx.Dp(12)
				if fillC.A > 0 {
					rrect(gtx, r, h/2, fillC)
				}
				border(gtx, r, h/2, bw, edge)
				x := bw + gtx.Dp(2)
				if t.Value {
					x += gtx.Dp(10)
				}
				theme.Circle(gtx, image.Pt(x+k/2, h/2), k, knob)
				return D{Size: image.Pt(w, h)}
			}),
		)
	})
}

// peekToggleInk is the toggle's label, border, handle and fill colours:
// off content-sec (the label, border and handle) with no fill; on, the
// label content-pry, the switch theme-base-imp with a theme-content-imp
// border and handle.
func peekToggleInk(pp peekPal, on bool) (label, edge, knob, fill color.NRGBA) {
	if on {
		return pp.fg, pp.contImp, pp.contImp, pp.baseImp
	}
	return pp.sec, pp.sec, pp.sec, color.NRGBA{}
}

package views

// The rail tabs' hover peeks draw these views' lists on Slack's peek card
// (PeekView, boot-shared 9eDG: .p-peek_card--as-card). The card is a
// Popover at the document root, outside the sidebar's theme scope, so the
// tokens the lists paint with resolve in the client's own theme: ctr-pry
// white (or the dark ctr-pry) instead of the aubergine column, content-pry
// text, and the --dt_color-theme-* values of 858H (light) or 7uI9 (dark)
// rather than the inverted sidebar's jmNE. internal/ui/rail_peek_*.go
// draw the card's header and call the views' Peek* methods for the rest.

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
	rule     color.NRGBA // --dt_color-theme-otl-inv-pry (an item's border-bottom)
	line     color.NRGBA // rgba(var(--sk_foreground_low),.13) (.p-peek_card--dms rows, header rule)
	fg       color.NRGBA // --dt_color-content-pry
	sec      color.NRGBA // --dt_color-content-sec
	ter      color.NRGBA // --dt_color-content-ter
	invPry   color.NRGBA // --dt_color-theme-content-inv-pry
	invSec   color.NRGBA // --dt_color-theme-content-inv-sec
	baseImp  color.NRGBA // --dt_color-theme-base-imp
	contImp  color.NRGBA // --dt_color-theme-content-imp
	skel     color.NRGBA // --dt_color-base-ter
	otlTer   color.NRGBA // --dt_color-otl-ter
	otlSec   color.NRGBA // --dt_color-otl-sec
	maxSolid color.NRGBA // rgb(var(--sk_foreground_max_solid))
}

// peekPalOf is the card's tokens in th's mode (kit.css and 858H / 7uI9
// defaults, a stylesheet's overrides through th.Tokens).
func peekPalOf(th *theme.Theme) peekPal {
	dark := th != nil && th.Dark
	var p peekPal
	if dark {
		p = peekPal{
			bg:       rgb(0x212428),
			hover:    flatten(argb(0x14f8f8f8), rgb(0x1a1d21)),
			rule:     argb(0x80efe1f5),
			line:     argb(0x21d1d2d3),
			fg:       rgb(0xf8f8f8),
			sec:      rgb(0xb9babd),
			ter:      rgb(0x9a9b9e),
			invPry:   rgb(0xffffff),
			invSec:   rgb(0xefe1f5),
			baseImp:  rgb(0xd8bae1),
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
			rule:     argb(0x21611f69),
			line:     argb(0x211d1c1d),
			fg:       rgb(0x1d1c1d),
			sec:      rgb(0x454447),
			ter:      rgb(0x5e5d60),
			invPry:   rgb(0x39063a),
			invSec:   argb(0xcc39063a),
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
	set(&p.bg, "var(--dt_color-ctr-pry)")
	set(&p.fg, "var(--dt_color-content-pry)")
	set(&p.sec, "var(--dt_color-content-sec)")
	set(&p.ter, "var(--dt_color-content-ter)")
	set(&p.rule, "var(--dt_color-theme-otl-inv-pry)")
	set(&p.line, "rgba(var(--sk_foreground_low),0.13)")
	set(&p.invPry, "var(--dt_color-theme-content-inv-pry)")
	set(&p.invSec, "var(--dt_color-theme-content-inv-sec)")
	set(&p.baseImp, "var(--dt_color-theme-base-imp)")
	set(&p.contImp, "var(--dt_color-theme-content-imp)")
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
	return p
}

// peekToggle is UnreadsToggleButton (boot-shared XrTy) in the peek's
// header under .p-ia4_client: the 12px bold label (theme-content-inv-sec,
// inv-pry when on), 8px gap, the 28x18 switch with a 1px border and the
// 12px handle 2px in (translated 10px when on: theme-base-imp fill,
// theme-content-imp border and handle).
func (b *base) peekToggle(gtx C, t *toggle, label string) D {
	pp := peekPalOf(b.th)
	return t.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		lc := pp.invSec
		if t.Value {
			lc = pp.invPry
		}
		return row(gtx, layout.Middle,
			layout.Rigid(func(gtx C) D { return b.label(gtx, 12, wBold, lc, label) }),
			hgap(8),
			layout.Rigid(func(gtx C) D {
				w, h := gtx.Dp(28), gtx.Dp(18)
				r := image.Rect(0, 0, w, h)
				bw := max(1, gtx.Dp(1))
				k := gtx.Dp(12)
				knob, edge := pp.invSec, pp.invSec
				if t.Value {
					rrect(gtx, r, h/2, pp.baseImp)
					knob, edge = pp.contImp, pp.contImp
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

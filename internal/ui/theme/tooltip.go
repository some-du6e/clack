package theme

import (
	"image"
	"image/color"
	"math"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// Tooltips, as Slack draws them (client-boot-styles, build 4d0c78085232).
//
//	.c-tooltip__tip { --sk_tooltip-background: var(--dt_color-base-inv-pry);
//	  color: var(--dt_color-content-inv-pry); border-radius: 8px; … }
//	.c-tooltip__tip__arrow { 9px square (arrow size 6 × 1.5) in the
//	  tooltip's background, turned 45° on the edge facing the target }
//	.sk-client-theme--dark .c-tooltip__tip {
//	  filter: drop-shadow(0 0 1px var(--dt_color-otl-pry)) }
//
// The dark-mode filter outlines the box and its arrow together. With the
// page's tokens that is #1d1c1d with #fff text in light mode, and #212428
// with #f8f8f8 text and a #797c81 outline in dark mode. TooltipSubtitle
// (inverse.secondary, --dt_color-content-inv-sec) is #cbcccf / #b9babd.
// The shortcut chips in a tooltip are KeyboardKey's tooltip variant
// (client-boot HgMH: constants.black, inverse.primary text) in both modes.
//
// Every tooltip in Clack takes its colours from the palette (Tooltip,
// TooltipText, TooltipSub, TooltipGlow: a stylesheet's tokens reach them
// through paletteTokens) and its shape from TooltipShape, so a mode or a
// theme changes them all at once. The toast (.c-toast) and the remove
// buttons over previews (.c-message_attachment__delete_container--overlay)
// paint with the same two tokens.

// TooltipColors are a theme's tooltip colours.
type TooltipColors struct {
	Bg, Text color.NRGBA
	// Subtitle is TooltipSubtitle's text (a reaction tip's "reacted with").
	Subtitle color.NRGBA
	// Glow is the dark mode drop-shadow's colour; zero in light mode.
	Glow color.NRGBA
}

// Tooltip returns the theme's tooltip colours.
func (t *Theme) Tooltip() TooltipColors {
	return TooltipColors{Bg: t.C.Tooltip, Text: t.C.TooltipText, Subtitle: t.C.TooltipSub, Glow: t.C.TooltipGlow}
}

// TooltipArrow is a tooltip's arrow: a square turned 45° whose centre C
// sits on the box edge facing the target, Half px from its centre to each
// corner. Half 0 is no arrow.
type TooltipArrow struct {
	C    image.Point
	Half int
}

// TooltipShape fills a tooltip's box r (corner radius rad) and arrow a in
// bg, with the dark mode outline (glow; zero draws none) around both. The
// outline is drawn first, under both shapes, so it only shows outside
// their union.
func TooltipShape(gtx layout.Context, r image.Rectangle, rad int, a TooltipArrow, bg, glow color.NRGBA) {
	if glow.A != 0 {
		tooltipGlow(gtx, r, rad, a, glow)
	}
	RRect(gtx, r, rad, bg)
	if a.Half > 0 {
		paint.FillShape(gtx.Ops, bg, clip.Outline{Path: diamondPath(gtx, a, 0)}.Op())
	}
}

// tooltipGlow approximates drop-shadow(0 0 1px glow): the shape's alpha
// blurred with a Gaussian of σ 0.5 CSS px, seen outside the opaque shape.
// Each device pixel of the first CSS pixel around the edge gets a 1px ring
// whose alpha is that blur's mean over the pixel (about 19% at 1x; 32%
// then 7% at 2x).
func tooltipGlow(gtx layout.Context, r image.Rectangle, rad int, a TooltipArrow, glow color.NRGBA) {
	n := max(1, gtx.Dp(1))
	for i := 0; i < n; i++ {
		al := uint8(math.Round(float64(glow.A) * glowBand(float64(i)/float64(n), float64(i+1)/float64(n))))
		if al == 0 {
			continue
		}
		c := color.NRGBA{R: glow.R, G: glow.G, B: glow.B, A: al}
		// the ring between i and i+1 px outside the edge: a 1px stroke on
		// the box grown by i+½
		off := f32.Pt(float32(r.Min.X-i)-0.5, float32(r.Min.Y-i)-0.5)
		t := op.Affine(f32.Affine2D{}.Offset(off)).Push(gtx.Ops)
		paint.FillShape(gtx.Ops, c, clip.Stroke{Path: RRectPath(r.Dx()+2*i+1, r.Dy()+2*i+1, rad+i), Width: 1}.Op())
		t.Pop()
		if a.Half > 0 {
			paint.FillShape(gtx.Ops, c, clip.Stroke{Path: diamondPath(gtx, a, float32(i)+0.5), Width: 1}.Op())
		}
	}
}

// glowBand is the mean of the blurred edge's alpha over [u0, u1) CSS px
// outside it: 1 − Φ(u / 0.5).
func glowBand(u0, u1 float64) float64 {
	const steps = 8
	s := 0.0
	for k := 0; k < steps; k++ {
		u := u0 + (u1-u0)*(float64(k)+0.5)/steps
		s += 0.5 * math.Erfc(2*u/math.Sqrt2)
	}
	return s / steps
}

// diamondPath is the arrow's outline, grown by out px (its corners stay
// on the diagonals).
func diamondPath(gtx layout.Context, a TooltipArrow, out float32) clip.PathSpec {
	cx, cy := float32(a.C.X), float32(a.C.Y)
	h := float32(a.Half) + out*float32(math.Sqrt2)
	var p clip.Path
	p.Begin(gtx.Ops)
	p.MoveTo(f32.Pt(cx, cy-h))
	p.LineTo(f32.Pt(cx+h, cy))
	p.LineTo(f32.Pt(cx, cy+h))
	p.LineTo(f32.Pt(cx-h, cy))
	p.Close()
	return p.End()
}

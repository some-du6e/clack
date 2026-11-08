package composer

import (
	"image"
	"image/color"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Small drawing helpers shared by every file in the package. Sizes are in
// Dp/Sp and converted with gtx so the composer scales with the window.

// Lato is the UI typeface (embedded by internal/ui/theme).
const Lato = theme.Typeface

// Mono is the code typeface list; the shaper falls back left to right.
const Mono = theme.MonoTypeface + ", Monaco, Menlo, Consolas, Courier New, Go Mono, monospace"

func rgb(c uint32) color.NRGBA {
	return color.NRGBA{R: uint8(c >> 16), G: uint8(c >> 8), B: uint8(c), A: 0xff}
}

func argb(c uint32) color.NRGBA {
	return color.NRGBA{A: uint8(c >> 24), R: uint8(c >> 16), G: uint8(c >> 8), B: uint8(c)}
}

func withAlpha(c color.NRGBA, a uint8) color.NRGBA { c.A = a; return c }

// fillRect paints r with col.
func fillRect(gtx layout.Context, r image.Rectangle, col color.NRGBA) {
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	paint.ColorOp{Color: col}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

// fillRRect paints a rounded rectangle; radius in pixels.
func fillRRect(gtx layout.Context, r image.Rectangle, radius int, col color.NRGBA) {
	defer clip.UniformRRect(r, radius).Push(gtx.Ops).Pop()
	paint.ColorOp{Color: col}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

// strokeRRect strokes a rounded rectangle border of width w pixels, drawn
// inside r.
func strokeRRect(gtx layout.Context, r image.Rectangle, radius, w int, col color.NRGBA) {
	if w <= 0 {
		return
	}
	outer := clip.UniformRRect(r, radius)
	inner := clip.UniformRRect(r.Inset(w), max(radius-w, 0))
	defer clip.Outline{Path: ringPath(gtx.Ops, outer, inner)}.Op().Push(gtx.Ops).Pop()
	paint.ColorOp{Color: col}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

func ringPath(ops *op.Ops, outer, inner clip.RRect) clip.PathSpec {
	var p clip.Path
	p.Begin(ops)
	appendRRect(&p, outer, false)
	appendRRect(&p, inner, true)
	return p.End()
}

// appendRRect adds a closed rounded-rect contour; reverse winds it the
// other way so an inner contour cuts a hole under the nonzero rule.
func appendRRect(p *clip.Path, rr clip.RRect, reverse bool) {
	r := rr.Rect
	x0, y0, x1, y1 := float32(r.Min.X), float32(r.Min.Y), float32(r.Max.X), float32(r.Max.Y)
	se, sw, nw, ne := float32(rr.SE), float32(rr.SW), float32(rr.NW), float32(rr.NE)
	const k = 0.5523 // cubic approximation of a quarter circle
	if !reverse {
		p.MoveTo(f32pt(x0+nw, y0))
		p.LineTo(f32pt(x1-ne, y0))
		p.CubeTo(f32pt(x1-ne+ne*k, y0), f32pt(x1, y0+ne-ne*k), f32pt(x1, y0+ne))
		p.LineTo(f32pt(x1, y1-se))
		p.CubeTo(f32pt(x1, y1-se+se*k), f32pt(x1-se+se*k, y1), f32pt(x1-se, y1))
		p.LineTo(f32pt(x0+sw, y1))
		p.CubeTo(f32pt(x0+sw-sw*k, y1), f32pt(x0, y1-sw+sw*k), f32pt(x0, y1-sw))
		p.LineTo(f32pt(x0, y0+nw))
		p.CubeTo(f32pt(x0, y0+nw-nw*k), f32pt(x0+nw-nw*k, y0), f32pt(x0+nw, y0))
		p.Close()
		return
	}
	p.MoveTo(f32pt(x0+nw, y0))
	p.CubeTo(f32pt(x0+nw-nw*k, y0), f32pt(x0, y0+nw-nw*k), f32pt(x0, y0+nw))
	p.LineTo(f32pt(x0, y1-sw))
	p.CubeTo(f32pt(x0, y1-sw+sw*k), f32pt(x0+sw-sw*k, y1), f32pt(x0+sw, y1))
	p.LineTo(f32pt(x1-se, y1))
	p.CubeTo(f32pt(x1-se+se*k, y1), f32pt(x1, y1-se+se*k), f32pt(x1, y1-se))
	p.LineTo(f32pt(x1, y0+ne))
	p.CubeTo(f32pt(x1, y0+ne-ne*k), f32pt(x1-ne+ne*k, y0), f32pt(x1-ne, y0))
	p.LineTo(f32pt(x0+nw, y0))
	p.Close()
}

func f32pt(x, y float32) f32.Point { return f32.Pt(x, y) }

// fillCircle paints a circle centred at c with radius rad (pixels).
func fillCircle(gtx layout.Context, c image.Point, rad int, col color.NRGBA) {
	r := image.Rect(c.X-rad, c.Y-rad, c.X+rad, c.Y+rad)
	defer clip.Ellipse(r).Push(gtx.Ops).Pop()
	paint.ColorOp{Color: col}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

// textStyle is a single-run label style.
type textStyle struct {
	Size       unit.Sp
	LineHeight unit.Sp // 0: shaper default
	Weight     font.Weight
	Italic     bool
	Typeface   font.Typeface // "" means Lato
	Color      color.NRGBA
	MaxLines   int
	Align      text.Alignment
}

func (s textStyle) font() font.Font {
	f := font.Font{Typeface: s.Typeface, Weight: s.Weight}
	if f.Typeface == "" {
		f.Typeface = Lato
	}
	if s.Italic {
		f.Style = font.Italic
	}
	return f
}

// label lays out one string.
func label(gtx layout.Context, sh *text.Shaper, s textStyle, txt string) layout.Dimensions {
	// shared renderer (theme.DrawText): Embolden, emoji images, cleanup
	return theme.DrawText(gtx, sh, theme.LabelOpts{Font: s.font(), Size: s.Size, LineHeight: s.LineHeight, MaxLines: s.MaxLines, Align: s.Align, Color: s.Color}, txt)
}

// offset runs w translated by (x, y) pixels.
func offset(gtx layout.Context, x, y int, w layout.Widget) layout.Dimensions {
	defer op.Offset(image.Pt(x, y)).Push(gtx.Ops).Pop()
	return w(gtx)
}

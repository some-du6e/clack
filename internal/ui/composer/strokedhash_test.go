package composer

import (
	"image/color"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// strokedHash is the hand-stroked # (in an s×s box) the composer drew
// until alpha.24, before it used Slack's glyphs; the placeholder and
// broadcast-row tests measure the real glyphs' ink against it, for scale.
func strokedHash(gtx layout.Context, s int, col color.NRGBA) {
	f := float32(s)
	var p clip.Path
	p.Begin(gtx.Ops)
	p.MoveTo(f32.Pt(f*0.43, f*0.17))
	p.LineTo(f32.Pt(f*0.34, f*0.83))
	p.MoveTo(f32.Pt(f*0.68, f*0.17))
	p.LineTo(f32.Pt(f*0.59, f*0.83))
	p.MoveTo(f32.Pt(f*0.2, f*0.38))
	p.LineTo(f32.Pt(f*0.84, f*0.38))
	p.MoveTo(f32.Pt(f*0.16, f*0.62))
	p.LineTo(f32.Pt(f*0.8, f*0.62))
	paint.FillShape(gtx.Ops, col, clip.Stroke{Path: p.End(), Width: f * 0.09}.Op())
}

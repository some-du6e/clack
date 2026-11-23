package theme

import (
	"gioui.org/f32"
	"gioui.org/op"
	"gioui.org/op/clip"
)

// RingPath is a whole circle (centre c, radius r) for stroking: four cubic
// quarter arcs from its 3 o'clock point round and back to it exactly, as
// clip.Ellipse builds one but at float precision. MoveTo + ArcTo(2π) +
// Close doesn't land exactly: ArcTo steps a rotation matrix 32 times in
// float32 (16 segments) and its pen ends a float's error from the start, so Close adds
// a near-zero segment whose direction is rounding noise, and the stroke's
// outline doubles back on itself at the seam (Karim's stray line under
// the intro's check badge, 2026-11-22).
func RingPath(ops *op.Ops, c f32.Point, r float32) clip.PathSpec {
	// https://pomax.github.io/bezierinfo/#circles_cubic
	const q = 0.5522847498307936 // 4(√2-1)/3
	k := r * q
	var p clip.Path
	p.Begin(ops)
	start := f32.Pt(c.X+r, c.Y)
	p.MoveTo(start)
	p.CubeTo(f32.Pt(c.X+r, c.Y+k), f32.Pt(c.X+k, c.Y+r), f32.Pt(c.X, c.Y+r))
	p.CubeTo(f32.Pt(c.X-k, c.Y+r), f32.Pt(c.X-r, c.Y+k), f32.Pt(c.X-r, c.Y))
	p.CubeTo(f32.Pt(c.X-r, c.Y-k), f32.Pt(c.X-k, c.Y-r), f32.Pt(c.X, c.Y-r))
	p.CubeTo(f32.Pt(c.X+k, c.Y-r), f32.Pt(c.X+r, c.Y-k), start)
	p.Close() // the pen is the start: no closing segment
	return p.End()
}

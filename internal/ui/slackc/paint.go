package slackc

import (
	"image"
	"image/color"
	"math"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc/css"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// hitEntry is one painted box, in paint order, for hit testing.
type hitEntry struct {
	b      *box
	r      rectF // local coordinates (after inv)
	clip   rectF
	inv    f32.Affine2D // scope → local
	hasInv bool
}

// painter paints one scope's boxes.
type painter struct {
	v       *View
	s       *scope
	gtx     layout.Context
	ops     *op.Ops
	clip    rectF
	xf      f32.Affine2D // local → scope
	hasXf   bool
	filt    []css.Filter // color filters in effect (grayscale…)
	inDefer bool
}

var noClip = rectF{-inf, -inf, inf, inf}

func (p *painter) col(c color.NRGBA) color.NRGBA {
	for i := len(p.filt) - 1; i >= 0; i-- {
		c = applyFilter(p.filt[i], c)
	}
	return c
}

func applyFilter(f css.Filter, c color.NRGBA) color.NRGBA {
	r, g, b := float32(c.R), float32(c.G), float32(c.B)
	if f.Grayscale > 0 {
		y := 0.2126*r + 0.7152*g + 0.0722*b
		t := min(f.Grayscale, 1)
		r, g, b = r+(y-r)*t, g+(y-g)*t, b+(y-b)*t
	}
	if f.Sepia > 0 {
		t := min(f.Sepia, 1)
		sr := 0.393*r + 0.769*g + 0.189*b
		sg := 0.349*r + 0.686*g + 0.168*b
		sb := 0.272*r + 0.534*g + 0.131*b
		r, g, b = r+(sr-r)*t, g+(sg-g)*t, b+(sb-b)*t
	}
	if f.Saturate > 0 && f.Saturate != 1 {
		y := 0.2126*r + 0.7152*g + 0.0722*b
		s := f.Saturate
		r, g, b = y+(r-y)*s, y+(g-y)*s, y+(b-y)*s
	}
	if f.Invert > 0 {
		t := min(f.Invert, 1)
		r, g, b = r+(255-2*r)*t, g+(255-2*g)*t, b+(255-2*b)*t
	}
	if f.Brightness > 0 && f.Brightness != 1 {
		r, g, b = r*f.Brightness, g*f.Brightness, b*f.Brightness
	}
	if f.Contrast > 0 && f.Contrast != 1 {
		r, g, b = (r-128)*f.Contrast+128, (g-128)*f.Contrast+128, (b-128)*f.Contrast+128
	}
	a := float32(c.A)
	if f.Opacity > 0 && f.Opacity < 1 {
		a *= f.Opacity
	}
	return color.NRGBA{R: u8(r), G: u8(g), B: u8(b), A: u8(a)}
}

func u8(x float32) uint8 {
	if x <= 0 {
		return 0
	}
	if x >= 255 {
		return 255
	}
	return uint8(x + 0.5)
}

func colorFilter(f css.Filter) bool {
	return f.Grayscale > 0 || f.Sepia > 0 || f.Invert > 0 || f.Brightness > 0 && f.Brightness != 1 ||
		f.Contrast > 0 && f.Contrast != 1 || f.Saturate > 0 && f.Saturate != 1 || f.Opacity > 0 && f.Opacity < 1
}

func irect(r rectF) image.Rectangle {
	return image.Rect(int(math.Floor(float64(r.x0))), int(math.Floor(float64(r.y0))), int(math.Ceil(float64(r.x1))), int(math.Ceil(float64(r.y1))))
}

// rect records b's painted box (scope coordinates) for events and rects.
func (p *painter) rect(b *box, r rectF) {
	if p.s == nil {
		return
	}
	sr := r
	if p.hasXf {
		sr = xfRect(p.xf, r)
	}
	if b.rectFr != p.v.frameNo || b.scope != p.s {
		b.rect, b.rectFr = sr, p.v.frameNo
		p.s.painted = append(p.s.painted, b)
	} else {
		b.rect = b.rect.union(sr)
	}
	if !p.hasXf {
		sr = sr.inter(p.clip)
	}
	p.s.bounds = p.s.bounds.union(sr)
}

func (p *painter) hit(b *box, r rectF) {
	if r.empty() || p.s == nil {
		return
	}
	e := hitEntry{b: b, r: r, clip: p.clip}
	if p.hasXf {
		e.inv, e.hasInv = p.xf.Invert(), true
	}
	p.s.hits = append(p.s.hits, e)
}

func xfRect(m f32.Affine2D, r rectF) rectF {
	pts := [4]f32.Point{m.Transform(f32.Pt(r.x0, r.y0)), m.Transform(f32.Pt(r.x1, r.y0)), m.Transform(f32.Pt(r.x0, r.y1)), m.Transform(f32.Pt(r.x1, r.y1))}
	o := rectF{pts[0].X, pts[0].Y, pts[0].X, pts[0].Y}
	for _, q := range pts[1:] {
		o.x0, o.y0 = min(o.x0, q.X), min(o.y0, q.Y)
		o.x1, o.y1 = max(o.x1, q.X), max(o.y1, q.Y)
	}
	return o
}

// radii resolves border radii (NW NE SE SW) for a w×h box, scaled down
// when they overlap (CSS backgrounds §5.5).
func (v *View) radii(b *box, w, h float32) [4]float32 {
	st := &b.st
	var r [4]float32
	nz := false
	for i := 0; i < 4; i++ {
		l := st.Radius[i]
		if l.Kind != css.LenFixed {
			continue
		}
		base := min(w, h)
		if l.Pct != 0 {
			base = w // approximation of elliptical % radii
		}
		r[i] = max(v.length(l, base), 0)
		if r[i] > 0 {
			nz = true
		}
	}
	if !nz {
		return r
	}
	f := float32(1)
	if s := r[0] + r[1]; s > w && s > 0 {
		f = min(f, w/s)
	}
	if s := r[3] + r[2]; s > w && s > 0 {
		f = min(f, w/s)
	}
	if s := r[0] + r[3]; s > h && s > 0 {
		f = min(f, h/s)
	}
	if s := r[1] + r[2]; s > h && s > 0 {
		f = min(f, h/s)
	}
	if f < 1 {
		for i := range r {
			r[i] *= f
		}
	}
	return r
}

func hasRadius(r [4]float32) bool { return r[0] > 0 || r[1] > 0 || r[2] > 0 || r[3] > 0 }

// rrectPath builds a rounded rect path (clockwise; ccw reverses it).
func rrectPath(ops *op.Ops, r rectF, rad [4]float32, ccw bool) clip.PathSpec {
	var p clip.Path
	p.Begin(ops)
	addRRect(&p, r, rad, ccw)
	return p.End()
}

func addRRect(p *clip.Path, r rectF, rad [4]float32, ccw bool) {
	const k = 0.5522847
	nw, ne, se, sw := rad[0], rad[1], rad[2], rad[3]
	if !ccw {
		p.MoveTo(f32.Pt(r.x0+nw, r.y0))
		p.LineTo(f32.Pt(r.x1-ne, r.y0))
		if ne > 0 {
			p.CubeTo(f32.Pt(r.x1-ne+ne*k, r.y0), f32.Pt(r.x1, r.y0+ne-ne*k), f32.Pt(r.x1, r.y0+ne))
		}
		p.LineTo(f32.Pt(r.x1, r.y1-se))
		if se > 0 {
			p.CubeTo(f32.Pt(r.x1, r.y1-se+se*k), f32.Pt(r.x1-se+se*k, r.y1), f32.Pt(r.x1-se, r.y1))
		}
		p.LineTo(f32.Pt(r.x0+sw, r.y1))
		if sw > 0 {
			p.CubeTo(f32.Pt(r.x0+sw-sw*k, r.y1), f32.Pt(r.x0, r.y1-sw+sw*k), f32.Pt(r.x0, r.y1-sw))
		}
		p.LineTo(f32.Pt(r.x0, r.y0+nw))
		if nw > 0 {
			p.CubeTo(f32.Pt(r.x0, r.y0+nw-nw*k), f32.Pt(r.x0+nw-nw*k, r.y0), f32.Pt(r.x0+nw, r.y0))
		}
		p.Close()
		return
	}
	p.MoveTo(f32.Pt(r.x0+nw, r.y0))
	if nw > 0 {
		p.CubeTo(f32.Pt(r.x0+nw-nw*k, r.y0), f32.Pt(r.x0, r.y0+nw-nw*k), f32.Pt(r.x0, r.y0+nw))
	}
	p.LineTo(f32.Pt(r.x0, r.y1-sw))
	if sw > 0 {
		p.CubeTo(f32.Pt(r.x0, r.y1-sw+sw*k), f32.Pt(r.x0+sw-sw*k, r.y1), f32.Pt(r.x0+sw, r.y1))
	}
	p.LineTo(f32.Pt(r.x1-se, r.y1))
	if se > 0 {
		p.CubeTo(f32.Pt(r.x1-se+se*k, r.y1), f32.Pt(r.x1, r.y1-se+se*k), f32.Pt(r.x1, r.y1-se))
	}
	p.LineTo(f32.Pt(r.x1, r.y0+ne))
	if ne > 0 {
		p.CubeTo(f32.Pt(r.x1, r.y0+ne-ne*k), f32.Pt(r.x1-ne+ne*k, r.y0), f32.Pt(r.x1-ne, r.y0))
	}
	p.Close()
}

// fillRR fills r with radii.
func (p *painter) fillRR(r rectF, rad [4]float32, c color.NRGBA) {
	if c.A == 0 || r.empty() {
		return
	}
	c = p.col(c)
	if !hasRadius(rad) {
		paint.FillShape(p.ops, c, clip.Rect(irectSnap(r)).Op())
		return
	}
	paint.FillShape(p.ops, c, clip.Outline{Path: rrectPath(p.ops, r, rad, false)}.Op())
}

// irectSnap rounds a float rect to pixels (box edges are snapped).
func irectSnap(r rectF) image.Rectangle {
	return image.Rect(int(math.Round(float64(r.x0))), int(math.Round(float64(r.y0))), int(math.Round(float64(r.x1))), int(math.Round(float64(r.y1))))
}

func shrinkRad(rad [4]float32, d float32) [4]float32 {
	for i := range rad {
		rad[i] = max(rad[i]-d, 0)
	}
	return rad
}

func insetRad(rad [4]float32, t, r, b, l float32) [4]float32 {
	return [4]float32{max(rad[0]-max(t, l), 0), max(rad[1]-max(t, r), 0), max(rad[2]-max(b, r), 0), max(rad[3]-max(b, l), 0)}
}

// ringPath is the area between outer and inner rounded rects.
func ringPath(ops *op.Ops, outer rectF, orad [4]float32, inner rectF, irad [4]float32) clip.PathSpec {
	var p clip.Path
	p.Begin(ops)
	addRRect(&p, outer, orad, false)
	if !inner.empty() {
		addRRect(&p, inner, irad, true)
	}
	return p.End()
}

// --- box painting ---

// paintBox paints b with its border box at (x, y) in painter coordinates.
func (p *painter) paintBox(b *box, x, y float32) {
	st := &b.st
	if st.Display == css.DisplayNone {
		return
	}
	b.ax, b.ay = x, y
	b.vis = true
	pushed := 0
	var tstack op.TransformStack
	saveXf, saveHasXf, saveClip := p.xf, p.hasXf, p.clip
	if len(st.Transform) > 0 {
		m := p.v.transform(b, x, y)
		tstack = op.Affine(m).Push(p.ops)
		pushed |= 1
		if p.hasXf {
			p.xf = p.xf.Mul(m)
		} else {
			p.xf, p.hasXf = m, true
		}
		p.clip = noClip
	}
	var ostack paint.OpacityStack
	if st.Opacity < 1 {
		ostack = paint.PushOpacity(p.ops, st.Opacity)
		pushed |= 2
	}
	f := st.Filter
	nf := len(p.filt)
	if colorFilter(f) {
		p.filt = append(p.filt, f)
	}
	if f.Blur > 0.3 {
		m := op.Record(p.ops)
		p.content(b, x, y)
		p.blurCall(m.Stop(), f.Blur*p.v.dp)
	} else {
		p.content(b, x, y)
	}
	p.filt = p.filt[:nf]
	if pushed&2 != 0 {
		ostack.Pop()
	}
	if pushed&1 != 0 {
		tstack.Pop()
	}
	p.xf, p.hasXf, p.clip = saveXf, saveHasXf, saveClip
}

// blurCall replays recorded content several times at sub-radius offsets
// with split opacity: a cheap blur that makes text unreadable.
func (p *painter) blurCall(call op.CallOp, r float32) {
	type off struct{ x, y float32 }
	var offs [17]off
	n := 0
	offs[n] = off{}
	n++
	for i := 0; i < 6; i++ {
		a := float64(i) * math.Pi / 3
		offs[n] = off{float32(math.Cos(a)) * r * 0.45, float32(math.Sin(a)) * r * 0.45}
		n++
	}
	for i := 0; i < 10; i++ {
		a := float64(i)*math.Pi/5 + 0.3
		offs[n] = off{float32(math.Cos(a)) * r * 0.9, float32(math.Sin(a)) * r * 0.9}
		n++
	}
	alpha := float32(2.2) / float32(n)
	for i := 0; i < n; i++ {
		t := op.Affine(f32.AffineId().Offset(f32.Pt(offs[i].x, offs[i].y))).Push(p.ops)
		o := paint.PushOpacity(p.ops, alpha)
		call.Add(p.ops)
		o.Pop()
		t.Pop()
	}
}

// transform returns the box's CSS transform as an affine map in painter
// coordinates (transform-origin relative to the border box at x, y).
func (v *View) transform(b *box, x, y float32) f32.Affine2D {
	st := &b.st
	ox := x + v.length(st.TransformOrigin[0], b.w)
	oy := y + v.length(st.TransformOrigin[1], b.h)
	m := f32.AffineId()
	for _, t := range st.Transform {
		var tm f32.Affine2D
		switch t.Kind {
		case css.TTranslate:
			tm = f32.AffineId().Offset(f32.Pt(v.length(t.X, b.w), v.length(t.Y, b.h)))
		case css.TScale:
			tm = f32.AffineId().Scale(f32.Point{}, f32.Pt(t.SX, t.SY))
		case css.TRotate:
			tm = f32.AffineId().Rotate(f32.Point{}, t.Angle)
		case css.TSkew:
			tm = f32.AffineId().Shear(f32.Point{}, t.SX, t.SY)
		case css.TMatrix:
			tm = f32.NewAffine2D(t.M[0], t.M[2], t.M[4]*v.dp, t.M[1], t.M[3], t.M[5]*v.dp)
		default:
			continue
		}
		// "t1 t2" is the matrix T1·T2 (t2 applies to points first).
		m = m.Mul(tm)
	}
	// p ↦ o + M(p - o)
	return f32.AffineId().Offset(f32.Pt(ox, oy)).Mul(m).Mul(f32.AffineId().Offset(f32.Pt(-ox, -oy)))
}

// content paints b's decorations, content and stacking layers.
func (p *painter) content(b *box, x, y float32) {
	st := &b.st
	v := p.v
	visible := st.Visibility == css.Visible
	r := rectF{x, y, x + b.w, y + b.h}
	rad := v.radii(b, b.w, b.h)
	if b.isElem() || b.kind == kPseudo || b.kind == kAnon {
		p.rect(b, r)
		if b.isElem() && st.PointerEvents {
			p.hit(b, r)
		}
		p.areas(b, r)
		if b.kind == kElem && b.repl == rNone && p.s != nil && b.focusable() {
			p.focusArea(b, r)
		}
	}
	if visible {
		p.shadows(b, r, rad, false)
		p.background(b, r, rad)
		p.border(b, r, rad)
		p.shadows(b, r, rad, true)
		if b.listMarker > 0 {
			p.marker(b, x, y)
		}
	}
	if b.ctxRoot {
		for _, k := range b.neg {
			p.layer(b, k, x, y)
		}
	}
	// overflow clip
	clipped := st.OverflowX != css.OverflowVisible || st.OverflowY != css.OverflowVisible
	var cst clip.Stack
	saveClip := p.clip
	if clipped {
		pr := rectF{x + b.bd[3], y + b.bd[0], x + b.w - b.bd[1], y + b.h - b.bd[2]}
		prad := insetRad(rad, b.bd[0], b.bd[1], b.bd[2], b.bd[3])
		if st.OverflowX == css.OverflowVisible {
			pr.x0, pr.x1 = -inf, inf
		}
		if st.OverflowY == css.OverflowVisible {
			pr.y0, pr.y1 = -inf, inf
		}
		if hasRadius(prad) && st.OverflowX != css.OverflowVisible && st.OverflowY != css.OverflowVisible {
			cst = clip.Outline{Path: rrectPath(p.ops, pr, prad, false)}.Op().Push(p.ops)
		} else {
			cst = clip.Rect(irectSnap(pr.inter(rectF{-1e6, -1e6, 1e6, 1e6}))).Push(p.ops)
		}
		p.clip = p.clip.inter(pr)
	}
	sx, sy := float32(0), float32(0)
	if b.ns != nil {
		sx, sy = b.ns.scrollX, b.ns.scrollY
	}
	cx, cy := x-sx, y-sy
	switch {
	case b.native:
		if visible {
			p.native(b, x+b.bd[3]+b.pad[3], y+b.bd[0]+b.pad[0])
		}
	case b.repl != rNone && b.repl != rBr:
		if visible || b.repl == rInput || b.repl == rTextarea {
			p.replaced(b, x, y, visible)
		}
	default:
		ri := 0
		for i := 0; i < len(b.lk); i++ {
			if ri < len(b.runs) && b.runs[ri].start == i {
				p.run(b, b.runs[ri], cx, cy)
				i = b.runs[ri].end - 1
				ri++
				continue
			}
			k := b.lk[i]
			if k.kind == kText || k.layered || outOfFlow(&k.st) || k.st.Display == css.DisplayNone {
				continue
			}
			p.paintBox(k, cx+k.x+k.relX, cy+k.y+k.relY)
		}
	}
	if clipped {
		cst.Pop()
		p.clip = saveClip
	}
	if b.ctxRoot {
		for _, k := range b.pos {
			p.layer(b, k, x, y)
		}
	}
	if visible {
		p.scrollbar(b, r)
		p.outline(b, r, rad)
	}
}

// layer paints a positioned descendant k of stacking context ctx (whose
// border box is at x, y).
func (p *painter) layer(ctx, k *box, x, y float32) {
	if k.fixedVP {
		p.fixed(k)
		return
	}
	// chain of layout parents from k up to ctx
	var chain [48]*box
	n := 0
	ok := false
	for a := k.lp; a != nil && n < len(chain); a = a.lp {
		chain[n] = a
		n++
		if a == ctx {
			ok = true
			break
		}
	}
	if !ok {
		// not a layout descendant (shouldn't happen): paint at its offset
		p.paintBox(k, x+k.x+k.relX, y+k.y+k.relY)
		return
	}
	// walk down from ctx accumulating position and clips
	ax, ay := x, y
	var stacks [48]clip.Stack
	ns := 0
	saveClip := p.clip
	for i := n - 1; i >= 0; i-- {
		a := chain[i]
		if a != ctx {
			ax += a.x + a.relX
			ay += a.y + a.relY
		}
		st := &a.st
		if st.OverflowX != css.OverflowVisible || st.OverflowY != css.OverflowVisible {
			pr := rectF{ax + a.bd[3], ay + a.bd[0], ax + a.w - a.bd[1], ay + a.h - a.bd[2]}
			if st.OverflowX == css.OverflowVisible {
				pr.x0, pr.x1 = -1e6, 1e6
			}
			if st.OverflowY == css.OverflowVisible {
				pr.y0, pr.y1 = -1e6, 1e6
			}
			stacks[ns] = clip.Rect(irectSnap(pr)).Push(p.ops)
			ns++
			p.clip = p.clip.inter(pr)
		}
		if a.ns != nil {
			ax -= a.ns.scrollX
			ay -= a.ns.scrollY
		}
	}
	p.paintBox(k, ax+k.x+k.relX, ay+k.y+k.relY)
	for i := ns - 1; i >= 0; i-- {
		stacks[i].Pop()
	}
	p.clip = saveClip
}

// fixed paints a viewport-fixed box: deferred to the top of the frame
// (outside any clip), at its window position.
func (p *painter) fixed(k *box) {
	ox, oy := p.s.windowOrigin()
	x, y := k.x-ox, k.y-oy
	if p.inDefer || p.v.overlay {
		p.backdrop(k, ox, oy)
		p.paintBox(k, x, y)
		return
	}
	m := op.Record(p.ops)
	saveClip := p.clip
	p.clip = noClip
	p.inDefer = true
	p.backdrop(k, ox, oy)
	p.paintBox(k, x, y)
	p.inDefer = false
	p.clip = saveClip
	// a pass-through handler inside the deferred ops so events over the
	// fixed box reach this scope too
	r := rectF{x, y, x + k.w, y + k.h}
	if k.backdrop != nil {
		r = rectF{-ox, -oy, -ox + p.v.vw, -oy + p.v.vh}
	}
	ar := clip.Rect(irect(r)).Push(p.ops)
	pp := pointer.PassOp{}.Push(p.ops)
	event.Op(p.ops, &p.s.ptag)
	pp.Pop()
	ar.Pop()
	op.Defer(p.ops, m.Stop())
}

// backdrop paints a modal dialog's ::backdrop over the window and makes
// the page under it inert: a click there goes to the dialog (as in
// browsers, where the backdrop's events target the dialog) and never to
// what is below.
func (p *painter) backdrop(k *box, ox, oy float32) {
	bd := k.backdrop
	if bd == nil {
		return
	}
	r := rectF{-ox, -oy, -ox + p.v.vw, -oy + p.v.vh}
	saveClip := p.clip
	p.clip = noClip
	if p.s != nil {
		p.s.bounds = p.s.bounds.union(r) // the scope's pointer handler covers it
	}
	if bd.st.Visibility == css.Visible {
		p.background(bd, r, [4]float32{})
	}
	if s := p.s; s != nil && k.st.PointerEvents {
		p.hit(k, r)
		ar := clip.Rect(irect(r)).Push(p.ops)
		pointer.CursorDefault.Add(p.ops)
		event.Op(p.ops, &s.stag)
		ar.Pop()
	}
	p.clip = saveClip
}

// areas registers cursor and shield areas under the box.
func (p *painter) areas(b *box, r rectF) {
	s := p.s
	if s == nil || !b.st.PointerEvents || r.empty() {
		return
	}
	ir := irect(r.inter(p.clip))
	if ir.Empty() {
		return
	}
	shield := p.v.overlay && (b.parent == nil || b.layered) || b.st.Position == css.PosFixed
	if b.kind == kAnon && b.parent == nil && p.v.root == OverlayBody {
		// document.body's own box spans the window (exact constraints), but
		// in a browser the body never blocks the page: only its children
		// with pointer-events take clicks where they are. A shield here ate
		// every click as soon as a plugin put anything in the body (oneko's
		// pointer-events:none cat).
		shield = false
	}
	cur := b.cursor
	pc := css.CursorAuto
	if b.parent != nil {
		pc = b.parent.cursor
	}
	if b.isElem() && (cur != pc || b.parent == nil) && cur != css.CursorAuto || b.repl == rInput || b.repl == rTextarea {
		ar := clip.Rect(ir).Push(p.ops)
		pp := pointer.PassOp{}.Push(p.ops)
		gc := gioCursor(cur)
		if (b.repl == rInput || b.repl == rTextarea) && cur == css.CursorAuto {
			gc = pointer.CursorText
		}
		if cur == css.CursorAuto && b.n != nil {
			switch b.n.T {
			case "a", "button":
				gc = pointer.CursorPointer
			}
		}
		gc.Add(p.ops)
		event.Op(p.ops, &s.ctag)
		pp.Pop()
		ar.Pop()
	}
	if shield {
		ar := clip.Rect(ir).Push(p.ops)
		event.Op(p.ops, &s.stag)
		ar.Pop()
	}
}

func gioCursor(c css.Cursor) pointer.Cursor {
	switch c {
	case css.CursorPointer:
		return pointer.CursorPointer
	case css.CursorText:
		return pointer.CursorText
	case css.CursorMove:
		return pointer.CursorAllScroll
	case css.CursorGrab:
		return pointer.CursorGrab
	case css.CursorGrabbing:
		return pointer.CursorGrabbing
	case css.CursorNotAllowed:
		return pointer.CursorNotAllowed
	case css.CursorWait:
		return pointer.CursorWait
	case css.CursorProgress:
		return pointer.CursorProgress
	case css.CursorCrosshair:
		return pointer.CursorCrosshair
	case css.CursorNone:
		return pointer.CursorNone
	case css.CursorColResize, css.CursorEWResize:
		return pointer.CursorColResize
	case css.CursorRowResize, css.CursorNSResize:
		return pointer.CursorRowResize
	case css.CursorNESWResize:
		return pointer.CursorNorthEastSouthWestResize
	case css.CursorNWSEResize:
		return pointer.CursorNorthWestSouthEastResize
	case css.CursorHelp:
		return pointer.CursorDefault
	}
	return pointer.CursorDefault
}

// --- decorations ---

func (p *painter) background(b *box, r rectF, rad [4]float32) {
	bg := &b.st.Background
	if bg.Color.A > 0 {
		p.fillRR(r, rad, bg.Color)
	}
	if bg.Gradient != nil && len(bg.Gradient.Stops) > 0 {
		p.gradient(bg.Gradient, r, rad)
	}
	if bg.Image != "" {
		p.bgImage(b, r, rad)
	}
}

func stopPositions(g *css.Gradient, out []float32) []float32 {
	n := len(g.Stops)
	out = out[:0]
	for i, s := range g.Stops {
		pos := s.Pos
		if pos < 0 {
			switch {
			case i == 0:
				pos = 0
			case i == n-1:
				pos = 1
			default:
				pos = -1
			}
		}
		out = append(out, pos)
	}
	// distribute auto stops between their neighbours
	for i := 0; i < n; i++ {
		if out[i] >= 0 {
			continue
		}
		j := i
		for j < n && out[j] < 0 {
			j++
		}
		a, bp := out[i-1], out[j]
		for k := i; k < j; k++ {
			out[k] = a + (bp-a)*float32(k-i+1)/float32(j-i+1)
		}
		i = j
	}
	for i := 1; i < n; i++ {
		if out[i] < out[i-1] {
			out[i] = out[i-1]
		}
	}
	return out
}

func (p *painter) gradient(g *css.Gradient, r rectF, rad [4]float32) {
	var posBuf [16]float32
	pos := stopPositions(g, posBuf[:0])
	w, h := r.x1-r.x0, r.y1-r.y0
	cx, cy := r.x0+w/2, r.y0+h/2
	var cs clip.Stack
	if hasRadius(rad) {
		cs = clip.Outline{Path: rrectPath(p.ops, r, rad, false)}.Op().Push(p.ops)
	} else {
		cs = clip.Rect(irectSnap(r)).Push(p.ops)
	}
	defer cs.Pop()
	if g.Radial {
		// concentric ellipses from the outside in (farthest-corner)
		rx, ry := w/2*math.Sqrt2, h/2*math.Sqrt2
		const steps = 28
		for i := steps; i >= 0; i-- {
			t := float32(i) / steps
			c := gradAt(g, pos, t)
			e := image.Rect(int(cx-rx*t), int(cy-ry*t), int(math.Ceil(float64(cx+rx*t))), int(math.Ceil(float64(cy+ry*t))))
			if e.Empty() {
				continue
			}
			paint.FillShape(p.ops, p.col(c), clip.Ellipse(e).Op(p.ops))
		}
		return
	}
	sa, ca := float32(math.Sin(float64(g.Angle))), float32(math.Cos(float64(g.Angle)))
	dx, dy := sa, -ca
	l := abs32(w*sa) + abs32(h*ca)
	at := func(t float32) f32.Point { return f32.Pt(cx+dx*l*(t-0.5), cy+dy*l*(t-0.5)) }
	n := len(g.Stops)
	if n == 1 {
		paint.FillShape(p.ops, p.col(g.Stops[0].Color), clip.Rect(irectSnap(r)).Op())
		return
	}
	big := w + h + 10
	px, py := -dy, dx // perpendicular
	band := func(t0, t1 float32) clip.PathSpec {
		a, b := at(t0), at(t1)
		var pa clip.Path
		pa.Begin(p.ops)
		pa.MoveTo(f32.Pt(a.X+px*big, a.Y+py*big))
		pa.LineTo(f32.Pt(b.X+px*big, b.Y+py*big))
		pa.LineTo(f32.Pt(b.X-px*big, b.Y-py*big))
		pa.LineTo(f32.Pt(a.X-px*big, a.Y-py*big))
		pa.Close()
		return pa.End()
	}
	// before the first and after the last stop: solid
	if pos[0] > 0 {
		st := clip.Outline{Path: band(pos[0]-10, pos[0])}.Op().Push(p.ops)
		paint.ColorOp{Color: p.col(g.Stops[0].Color)}.Add(p.ops)
		paint.PaintOp{}.Add(p.ops)
		st.Pop()
	}
	if pos[n-1] < 1 {
		st := clip.Outline{Path: band(pos[n-1], pos[n-1]+10)}.Op().Push(p.ops)
		paint.ColorOp{Color: p.col(g.Stops[n-1].Color)}.Add(p.ops)
		paint.PaintOp{}.Add(p.ops)
		st.Pop()
	}
	for i := 0; i+1 < n; i++ {
		t0, t1 := pos[i], pos[i+1]
		if t1-t0 <= 1e-4 {
			continue
		}
		st := clip.Outline{Path: band(t0, t1)}.Op().Push(p.ops)
		paint.LinearGradientOp{Stop1: at(t0), Color1: p.col(g.Stops[i].Color), Stop2: at(t1), Color2: p.col(g.Stops[i+1].Color)}.Add(p.ops)
		paint.PaintOp{}.Add(p.ops)
		st.Pop()
	}
}

func gradAt(g *css.Gradient, pos []float32, t float32) color.NRGBA {
	n := len(g.Stops)
	if t <= pos[0] {
		return g.Stops[0].Color
	}
	for i := 0; i+1 < n; i++ {
		if t <= pos[i+1] {
			span := pos[i+1] - pos[i]
			if span <= 0 {
				return g.Stops[i+1].Color
			}
			return theme.Mix(g.Stops[i].Color, g.Stops[i+1].Color, (t-pos[i])/span)
		}
	}
	return g.Stops[n-1].Color
}

func (p *painter) bgImage(b *box, r rectF, rad [4]float32) {
	im := env().Images
	if im == nil {
		return
	}
	bg := &b.st.Background
	img, ok := im.Get(bg.Image, 0)
	if !ok {
		p.v.pendingImg = true
		return
	}
	sz := img.Size()
	if sz.X <= 0 || sz.Y <= 0 {
		return
	}
	w, h := r.x1-r.x0, r.y1-r.y0
	iw, ih := float32(sz.X)*p.v.dp, float32(sz.Y)*p.v.dp
	switch bg.Size {
	case "cover":
		s := max(w/iw, h/ih)
		iw, ih = iw*s, ih*s
	case "contain":
		s := min(w/iw, h/ih)
		iw, ih = iw*s, ih*s
	case "100% 100%":
		iw, ih = w, h
	}
	x, y := r.x0, r.y0
	switch bg.Position {
	case "center", "center center", "50% 50%":
		x, y = r.x0+(w-iw)/2, r.y0+(h-ih)/2
	}
	var cs clip.Stack
	if hasRadius(rad) {
		cs = clip.Outline{Path: rrectPath(p.ops, r, rad, false)}.Op().Push(p.ops)
	} else {
		cs = clip.Rect(irectSnap(r)).Push(p.ops)
	}
	t := op.Affine(f32.AffineId().Scale(f32.Point{}, f32.Pt(iw/float32(sz.X), ih/float32(sz.Y))).Offset(f32.Pt(x, y))).Push(p.ops)
	img.Add(p.ops)
	paint.PaintOp{}.Add(p.ops)
	t.Pop()
	cs.Pop()
}

func (p *painter) border(b *box, r rectF, rad [4]float32) {
	bd := b.bd
	if bd == [4]float32{} {
		return
	}
	st := &b.st
	inner := rectF{r.x0 + bd[3], r.y0 + bd[0], r.x1 - bd[1], r.y1 - bd[2]}
	irad := insetRad(rad, bd[0], bd[1], bd[2], bd[3])
	c := st.BorderColor
	same := c[0] == c[1] && c[1] == c[2] && c[2] == c[3]
	dashed := false
	for i := 0; i < 4; i++ {
		if bd[i] > 0 && (st.BorderStyle[i] == css.BorderDashed || st.BorderStyle[i] == css.BorderDotted) {
			dashed = true
		}
	}
	if dashed && !hasRadius(rad) {
		p.dashedBorder(b, r)
		return
	}
	if same {
		if c[0].A == 0 {
			return
		}
		paint.FillShape(p.ops, p.col(c[0]), clip.Outline{Path: ringPath(p.ops, r, rad, inner, irad)}.Op())
		return
	}
	// per-side colors: clip to the ring, fill four trapezoids
	cs := clip.Outline{Path: ringPath(p.ops, r, rad, inner, irad)}.Op().Push(p.ops)
	defer cs.Pop()
	trap := func(a, b, c2, d f32.Point, col color.NRGBA) {
		if col.A == 0 {
			return
		}
		var pa clip.Path
		pa.Begin(p.ops)
		pa.MoveTo(a)
		pa.LineTo(b)
		pa.LineTo(c2)
		pa.LineTo(d)
		pa.Close()
		paint.FillShape(p.ops, p.col(col), clip.Outline{Path: pa.End()}.Op())
	}
	o0, o1, o2, o3 := f32.Pt(r.x0, r.y0), f32.Pt(r.x1, r.y0), f32.Pt(r.x1, r.y1), f32.Pt(r.x0, r.y1)
	i0, i1, i2, i3 := f32.Pt(inner.x0, inner.y0), f32.Pt(inner.x1, inner.y0), f32.Pt(inner.x1, inner.y1), f32.Pt(inner.x0, inner.y1)
	if bd[0] > 0 {
		trap(o0, o1, i1, i0, c[0])
	}
	if bd[1] > 0 {
		trap(o1, o2, i2, i1, c[1])
	}
	if bd[2] > 0 {
		trap(o2, o3, i3, i2, c[2])
	}
	if bd[3] > 0 {
		trap(o3, o0, i0, i3, c[3])
	}
}

func (p *painter) dashedBorder(b *box, r rectF) {
	st := &b.st
	bd := b.bd
	side := func(i int, x0, y0, x1, y1 float32, horiz bool) {
		w := bd[i]
		if w <= 0 || st.BorderColor[i].A == 0 {
			return
		}
		col := p.col(st.BorderColor[i])
		if st.BorderStyle[i] != css.BorderDashed && st.BorderStyle[i] != css.BorderDotted {
			paint.FillShape(p.ops, col, clip.Rect(irectSnap(rectF{x0, y0, x1, y1})).Op())
			return
		}
		dash := w * 3
		if st.BorderStyle[i] == css.BorderDotted {
			dash = w
		}
		l := x1 - x0
		if !horiz {
			l = y1 - y0
		}
		n := int(l / (2 * dash))
		if n < 1 {
			n = 1
		}
		step := l / float32(2*n)
		for k := 0; k < 2*n; k += 2 {
			a := float32(k) * step
			if horiz {
				paint.FillShape(p.ops, col, clip.Rect(irectSnap(rectF{x0 + a, y0, x0 + a + step, y1})).Op())
			} else {
				paint.FillShape(p.ops, col, clip.Rect(irectSnap(rectF{x0, y0 + a, x1, y0 + a + step})).Op())
			}
		}
	}
	side(0, r.x0, r.y0, r.x1, r.y0+bd[0], true)
	side(2, r.x0, r.y1-bd[2], r.x1, r.y1, true)
	side(3, r.x0, r.y0, r.x0+bd[3], r.y1, false)
	side(1, r.x1-bd[1], r.y0, r.x1, r.y1, false)
}

func (p *painter) outline(b *box, r rectF, rad [4]float32) {
	o := &b.st.Outline
	if o.Width <= 0 || o.Style == css.BorderNone || o.Style == css.BorderHidden || o.Color.A == 0 {
		return
	}
	w := snapBorder(o.Width * p.v.dp)
	off := o.Offset * p.v.dp
	inner := rectF{r.x0 - off, r.y0 - off, r.x1 + off, r.y1 + off}
	outer := rectF{inner.x0 - w, inner.y0 - w, inner.x1 + w, inner.y1 + w}
	orad := rad
	for i := range orad {
		if orad[i] > 0 {
			orad[i] += off + w
		}
	}
	irad := shrinkRad(orad, w)
	paint.FillShape(p.ops, p.col(o.Color), clip.Outline{Path: ringPath(p.ops, outer, orad, inner, irad)}.Op())
}

// shadows draws outer (inset=false) or inner box shadows; blur is
// approximated by stacked translucent layers.
func (p *painter) shadows(b *box, r rectF, rad [4]float32, inset bool) {
	for i := len(b.st.BoxShadow) - 1; i >= 0; i-- {
		sh := &b.st.BoxShadow[i]
		if sh.Inset != inset || sh.Color.A == 0 {
			continue
		}
		dp := p.v.dp
		ox, oy, blur, spread := sh.X*dp, sh.Y*dp, sh.Blur*dp, sh.Spread*dp
		col := p.col(sh.Color)
		if !inset {
			// clip out the box itself so translucent boxes don't show it
			var pa clip.Path
			pa.Begin(p.ops)
			big := rectF{r.x0 - 4*blur - abs32(ox) - spread - 50, r.y0 - 4*blur - abs32(oy) - spread - 50, r.x1 + 4*blur + abs32(ox) + spread + 50, r.y1 + 4*blur + abs32(oy) + spread + 50}
			addRRect(&pa, big, [4]float32{}, false)
			addRRect(&pa, r, rad, true)
			cs := clip.Outline{Path: pa.End()}.Op().Push(p.ops)
			base := rectF{r.x0 + ox - spread, r.y0 + oy - spread, r.x1 + ox + spread, r.y1 + oy + spread}
			brad := rad
			for j := range brad {
				if brad[j] > 0 || spread > 0 {
					brad[j] = max(brad[j]+spread, 0)
				}
			}
			if blur < 0.5 {
				p.fillRaw(base, brad, col)
			} else {
				const n = 6
				c := col
				c.A = uint8(float32(col.A)/n*1.15 + 0.5)
				for k := 0; k < n; k++ {
					t := (float32(k)+0.5)/n*2 - 1 // -1..1 across the blur band
					d := t * blur / 2
					rr := rectF{base.x0 - d, base.y0 - d, base.x1 + d, base.y1 + d}
					rrad := brad
					for j := range rrad {
						rrad[j] = max(rrad[j]+d, 0)
					}
					p.fillRaw(rr, rrad, c)
				}
			}
			cs.Pop()
			continue
		}
		// inset: clip to the padding box, draw a ring around a shrunk hole
		pr := rectF{r.x0 + b.bd[3], r.y0 + b.bd[0], r.x1 - b.bd[1], r.y1 - b.bd[2]}
		prad := insetRad(rad, b.bd[0], b.bd[1], b.bd[2], b.bd[3])
		cs := clip.Outline{Path: rrectPath(p.ops, pr, prad, false)}.Op().Push(p.ops)
		const n = 5
		c := col
		if blur >= 0.5 {
			c.A = uint8(float32(col.A)/n*1.2 + 0.5)
		}
		layers := 1
		if blur >= 0.5 {
			layers = n
		}
		for k := 0; k < layers; k++ {
			d := spread
			if layers > 1 {
				d += blur * (float32(k) + 0.5) / float32(layers)
			}
			hole := rectF{pr.x0 + ox + d, pr.y0 + oy + d, pr.x1 + ox - d, pr.y1 + oy - d}
			outer := rectF{pr.x0 - 1e3, pr.y0 - 1e3, pr.x1 + 1e3, pr.y1 + 1e3}
			paint.FillShape(p.ops, c, clip.Outline{Path: ringPath(p.ops, outer, [4]float32{}, hole, shrinkRad(prad, d))}.Op())
		}
		cs.Pop()
	}
}

func (p *painter) fillRaw(r rectF, rad [4]float32, c color.NRGBA) {
	if r.empty() {
		return
	}
	paint.FillShape(p.ops, c, clip.Outline{Path: rrectPath(p.ops, r, rad, false)}.Op())
}

// marker draws a list-item bullet/number in the box's left padding.
func (p *painter) marker(b *box, x, y float32) {
	st := &b.st
	fs := st.FontSize * p.v.sp
	base := b.base
	if base < 0 {
		base = b.bd[0] + b.pad[0] + p.v.lineHeight(st)*0.75
	}
	cx := x + b.bd[3] + b.pad[3] - fs*0.6
	cy := y + base - fs*0.3
	col := p.col(st.Color)
	switch b.listMarker {
	case 1, 3, 4:
		d := fs * 0.36
		rr := image.Rect(int(cx-d/2), int(cy-d/2), int(cx+d/2+0.5), int(cy+d/2+0.5))
		switch b.listMarker {
		case 1:
			paint.FillShape(p.ops, col, clip.Ellipse(rr).Op(p.ops))
		case 3:
			paint.FillShape(p.ops, col, clip.Stroke{Path: clip.Ellipse(rr).Path(p.ops), Width: max(1, fs/14)}.Op())
		case 4:
			paint.FillShape(p.ops, col, clip.Rect(rr).Op())
		}
	case 2:
		idx := b.listIndex + 1
		s := itoa(idx) + "."
		sh := word(p.v.face(st), s)
		p.drawWord(sh, cx+fs*0.4-sh.adv-fs*0.25, y+base, col, st)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	n := len(b)
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		n--
		b[n] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		n--
		b[n] = '-'
	}
	return string(b[n:])
}

// --- text ---

// drawWord paints a shaped word with its baseline at (x, y).
func (p *painter) drawWord(sh *shaped, x, y float32, col color.NRGBA, st *css.Style) {
	if sh == nil || len(sh.glyphs) == 0 || col.A == 0 {
		return
	}
	shp := shaper()
	path, bm := sh.pathOf(shp)
	t := op.Affine(f32.AffineId().Offset(f32.Pt(x, float32(math.Round(float64(y)))))).Push(p.ops)
	for i := range st.TextShadow {
		ts := &st.TextShadow[i]
		if ts.Color.A == 0 {
			continue
		}
		s := op.Affine(f32.AffineId().Offset(f32.Pt(ts.X*p.v.dp, ts.Y*p.v.dp))).Push(p.ops)
		paint.FillShape(p.ops, p.col(ts.Color), clip.Outline{Path: path}.Op())
		s.Pop()
	}
	paint.FillShape(p.ops, col, clip.Outline{Path: path}.Op())
	if e := theme.EmboldenFor(col); e > 0 {
		paint.FillShape(p.ops, col, clip.Stroke{Path: path, Width: e}.Op())
	}
	if bm != (op.CallOp{}) {
		bm.Add(p.ops)
	}
	t.Pop()
}

// run paints an inline formatting context; (x, y) is the container's
// border box (scroll-adjusted).
func (p *painter) run(b *box, r *run, x, y float32) {
	f := &r.f
	ox := x + b.bd[3] + b.pad[3]
	oy := y + b.bd[0] + b.pad[0] + r.y
	// inline box backgrounds, borders and hit areas
	for i := range f.spans {
		sp := &f.spans[i]
		k := sp.b
		rr := rectF{ox + sp.x0, oy + sp.top, ox + sp.x1, oy + sp.bot}
		p.rect(k, rr)
		if k.isElem() && k.st.PointerEvents {
			p.hit(k, rr)
		}
		if k.isElem() || k.kind == kPseudo {
			p.areas(k, rr)
		}
		if k.st.Visibility != css.Visible {
			continue
		}
		rad := p.v.radii(k, rr.x1-rr.x0, rr.y1-rr.y0)
		if !sp.first {
			rad[0], rad[3] = 0, 0
		}
		if !sp.last {
			rad[1], rad[2] = 0, 0
		}
		if k.st.Background.Color.A > 0 || k.st.Background.Gradient != nil {
			p.background(k, rr, rad)
		}
		if k.bd != [4]float32{} {
			save := k.bd
			if !sp.first {
				k.bd[3] = 0
			}
			if !sp.last {
				k.bd[1] = 0
			}
			p.border(k, rr, rad)
			k.bd = save
		}
	}
	for fi := range f.frags {
		fr := &f.frags[fi]
		if fr.hidden {
			continue
		}
		it := &f.items[fr.it]
		switch it.kind {
		case iWord, iSpace:
			st := &it.b.st
			if st.Visibility != css.Visible {
				continue
			}
			col := p.col(st.Color)
			if it.kind == iWord {
				p.drawWord(fr.sh, ox+fr.x, oy+fr.y, col, st)
			}
			if it.deco.Lines() != 0 {
				p.decorate(it, ox+fr.x, oy+fr.y, fr.w, st)
			}
		case iEmoji:
			st := &it.b.st
			if st.Visibility != css.Visible {
				continue
			}
			p.emoji(it.url, ox+fr.x, oy+fr.y, it.w-p.v.dp)
		case iAtomic:
			k := it.b
			if k.layered || k.st.Display == css.DisplayNone {
				continue
			}
			p.paintBox(k, x+k.x+k.relX, y+k.y+k.relY)
		}
	}
}

func (p *painter) decorate(it *item, x, base, w float32, st *css.Style) {
	col := p.col(it.dcol)
	if col.A == 0 {
		col = p.col(st.Color)
	}
	drawDecoration(p.ops, it.deco, col, x, base, w, st.FontSize*p.v.sp)
}

// drawDecoration draws d's lines (straight, or wavy) for a run of text
// w wide at baseline base, in font size fs (px).
func drawDecoration(ops *op.Ops, d css.TextDecoration, col color.NRGBA, x, base, w, fs float32) {
	th := max(1, float32(math.Round(float64(fs/15))))
	line := func(y float32) {
		yy := float32(math.Round(float64(y)))
		if d&css.DecoWavy == 0 {
			paint.FillShape(ops, col, clip.Rect(irectSnap(rectF{x, yy, x + w, yy + th})).Op())
			return
		}
		// a zigzag of quarter-wave segments, amplitude th, wavelength 4th
		amp, step := th, 2*th
		var path clip.Path
		path.Begin(ops)
		path.MoveTo(f32.Pt(x, yy))
		up := true
		for cx := x; cx < x+w; cx += step {
			nx := min(cx+step, x+w)
			dy := amp
			if up {
				dy = -amp
			}
			path.QuadTo(f32.Pt((cx+nx)/2, yy+2*dy), f32.Pt(nx, yy))
			up = !up
		}
		paint.FillShape(ops, col, clip.Stroke{Path: path.End(), Width: th}.Op())
	}
	if d&css.DecoUnderline != 0 {
		line(base + fs*0.1)
	}
	if d&css.DecoLineThrough != 0 {
		line(base - fs*0.3)
	}
	if d&css.DecoOverline != 0 {
		line(base - fs*0.95)
	}
}

// DrawDecoration draws text-decoration lines d (underline, line-through,
// overline; straight or wavy) in col under a run of text at x (px), w
// wide, on baseline base, in font size fs px. For natives that record
// their own text ops (richview's message body).
func DrawDecoration(ops *op.Ops, d css.TextDecoration, col color.NRGBA, x, base, w, fs float32) {
	drawDecoration(ops, d, col, x, base, w, fs)
}

// Decorate draws st's text-decoration (underline, line-through, overline;
// straight or wavy) under a native widget's text run: x and w in px, base
// the baseline, fg the text's color (for currentColor). Natives call it for
// text drawn outside the host tree (a sender name with a plugin's class).
func Decorate(gtx layout.Context, s Styled, x, base, w int, fg color.NRGBA) {
	st := s.Style()
	if st == nil || st.TextDecoration.Lines() == 0 {
		return
	}
	col := st.DecoColor
	if col.A == 0 {
		col = fg
	}
	fs := st.FontSize * gtx.Metric.PxPerSp
	if fs <= 0 {
		fs = 15 * gtx.Metric.PxPerSp
	}
	drawDecoration(gtx.Ops, st.TextDecoration, col, float32(x), float32(base), float32(w), fs)
}

func (p *painter) emoji(url string, x, base, es float32) {
	im := env().Images
	if im == nil || url == "" {
		return
	}
	img, ok := im.Get(url, int(es*2))
	if !ok {
		return
	}
	sz := img.Size()
	if sz.X <= 0 {
		return
	}
	top := base - es*0.8
	s := es / float32(sz.X)
	t := op.Affine(f32.AffineId().Scale(f32.Point{}, f32.Pt(s, s)).Offset(f32.Pt(x, float32(math.Round(float64(top)))))).Push(p.ops)
	img.Add(p.ops)
	paint.PaintOp{}.Add(p.ops)
	t.Pop()
}

// scrollbar draws a thin thumb for scroll containers with overflow.
func (p *painter) scrollbar(b *box, r rectF) {
	st := &b.st
	if b.ns == nil {
		return
	}
	scrollY := st.OverflowY == css.OverflowScroll || st.OverflowY == css.OverflowAuto
	scrollX := st.OverflowX == css.OverflowScroll || st.OverflowX == css.OverflowAuto
	if !scrollY && !scrollX {
		return
	}
	th := env().Theme
	col := color.NRGBA{A: 0x60}
	if th != nil {
		col = th.C.ScrollThumb
	}
	show := b.ns.hover || st.OverflowY == css.OverflowScroll || b.ns.dragging
	if !show {
		return
	}
	dp := p.v.dp
	ch := b.h - b.bd[0] - b.bd[2]
	cw := b.w - b.bd[1] - b.bd[3]
	if scrollY && b.contentH > ch+0.5 {
		tw := 6 * dp
		thh := max(ch*ch/(b.contentH), 20*dp)
		maxS := b.contentH - ch
		ty := r.y0 + b.bd[0] + (ch-thh)*b.ns.scrollY/maxS
		tx := r.x1 - b.bd[1] - tw - 2*dp
		p.fillRR(rectF{tx, ty, tx + tw, ty + thh}, [4]float32{tw / 2, tw / 2, tw / 2, tw / 2}, col)
		b.ns.thumb = rectF{tx - 2*dp, ty, tx + tw + 2*dp, ty + thh}
	}
	if scrollX && b.contentW > cw+0.5 {
		thh := 6 * dp
		tw := max(cw*cw/b.contentW, 20*dp)
		maxS := b.contentW - cw
		tx := r.x0 + b.bd[3] + (cw-tw)*b.ns.scrollX/maxS
		ty := r.y1 - b.bd[2] - thh - 2*dp
		p.fillRR(rectF{tx, ty, tx + tw, ty + thh}, [4]float32{thh / 2, thh / 2, thh / 2, thh / 2}, col)
	}
}

// native places the native widget's recorded draw at the content box.
func (p *painter) native(b *box, x, y float32) {
	if !b.natDrawn {
		return
	}
	t := op.Offset(image.Pt(int(math.Round(float64(x))), int(math.Round(float64(y))))).Push(p.ops)
	b.natCall.Add(p.ops)
	t.Pop()
}

package slackc

import (
	"image"
	"math"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc/css"
)

// wrapNative draws the CSS box effects a rule set on a native widget
// (Styled.Wrap): margin, background, border, radius, box-shadow, opacity,
// filter, transform, min/max sizes and ::before/::after content. The
// widget's own padding and text styling are its business.

var wrapState struct {
	ver   uint64
	boxes map[*nativeStyle]*box
	v     *View
}

func wrapBox(ns *nativeStyle) *box {
	if ver := Styles.Version(); ver != wrapState.ver || wrapState.boxes == nil {
		wrapState.ver = ver
		wrapState.boxes = map[*nativeStyle]*box{}
	}
	b := wrapState.boxes[ns]
	if b == nil {
		if len(wrapState.boxes) > 4096 {
			wrapState.boxes = map[*nativeStyle]*box{}
		}
		b = &box{kind: kAnon, st: ns.style}
		wrapState.boxes[ns] = b
	}
	return b
}

func wrapNative(gtx layout.Context, ns *nativeStyle, w layout.Widget) layout.Dimensions {
	if wrapState.v == nil {
		wrapState.v = NewView(0)
	}
	v := wrapState.v
	v.setMetrics(gtx)
	b := wrapBox(ns)
	b.v = v
	st := &b.st
	cs := gtx.Constraints
	v.edges(b, float32(cs.Max.X))
	b.pad = [4]float32{}
	for i := range b.m {
		if st.Margin[i].IsAuto() {
			b.m[i] = 0
		}
	}
	ml, mr, mt, mb := b.m[3], b.m[1], b.m[0], b.m[2]
	hb, vb := b.bd[1]+b.bd[3], b.bd[0]+b.bd[2]

	// generated content
	var bsh, ash *shaped
	var bw, aw float32
	if ns.before != nil && ns.before.Content != "" {
		bsh = word(v.face(ns.before), ns.before.Content)
		bw = bsh.adv
	}
	if ns.after != nil && ns.after.Content != "" {
		ash = word(v.face(ns.after), ns.after.Content)
		aw = ash.adv
	}

	inner := cs
	shrinkX := ceilI(ml + mr + hb + bw + aw)
	shrinkY := ceilI(mt + mb + vb)
	inner.Max.X = max(inner.Max.X-shrinkX, 0)
	inner.Min.X = max(inner.Min.X-shrinkX, 0)
	if inner.Max.Y < 1<<24 {
		inner.Max.Y = max(inner.Max.Y-shrinkY, 0)
	}
	inner.Min.Y = max(inner.Min.Y-shrinkY, 0)
	cbW := float32(cs.Max.X)
	lo, hi := v.minMaxW(b, cbW)
	if hi < inf/2 {
		inner.Max.X = min(inner.Max.X, ceilI(hi))
	}
	inner.Min.X = max(inner.Min.X, ceilI(lo))
	hlo, hhi := v.minMaxH(b, -1)
	if hhi < inf/2 {
		inner.Max.Y = min(inner.Max.Y, ceilI(hhi))
	}
	inner.Min.Y = max(inner.Min.Y, ceilI(hlo))
	if inner.Min.X > inner.Max.X {
		inner.Min.X = inner.Max.X
	}
	if inner.Min.Y > inner.Max.Y {
		inner.Min.Y = inner.Max.Y
	}

	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = inner
	dims := w(g)
	call := m.Stop()

	contentW := float32(dims.Size.X) + bw + aw
	contentH := float32(dims.Size.Y)
	if bsh != nil {
		contentH = max(contentH, v.lineHeight(ns.before))
	}
	if ash != nil {
		contentH = max(contentH, v.lineHeight(ns.after))
	}
	b.w, b.h = contentW+hb, contentH+vb

	p := painter{v: v, ops: gtx.Ops, clip: noClip}
	outer := op.Offset(image.Pt(int(math.Round(float64(ml))), int(math.Round(float64(mt))))).Push(gtx.Ops)
	var ts op.TransformStack
	if len(st.Transform) > 0 {
		ts = op.Affine(v.transform(b, 0, 0)).Push(gtx.Ops)
	}
	var os paint.OpacityStack
	if st.Opacity < 1 {
		os = paint.PushOpacity(gtx.Ops, st.Opacity)
	}
	rec := op.Record(gtx.Ops)
	r := rectF{0, 0, b.w, b.h}
	rad := v.radii(b, b.w, b.h)
	if st.Visibility == css.Visible {
		p.shadows(b, r, rad, false)
		p.background(b, r, rad)
		p.border(b, r, rad)
		p.shadows(b, r, rad, true)
	}
	x := b.bd[3]
	if bsh != nil {
		a, _, _, _ := v.metrics(ns.before)
		p.drawWord(bsh, x, b.bd[0]+(contentH-v.lineHeight(ns.before))/2+a, ns.before.Color, ns.before)
		x += bw
	}
	t := op.Offset(image.Pt(int(math.Round(float64(x))), int(math.Round(float64(b.bd[0]))))).Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()
	x += float32(dims.Size.X)
	if ash != nil {
		a, _, _, _ := v.metrics(ns.after)
		p.drawWord(ash, x, b.bd[0]+(contentH-v.lineHeight(ns.after))/2+a, ns.after.Color, ns.after)
	}
	if st.Visibility == css.Visible {
		p.outline(b, r, rad)
	}
	content := rec.Stop()
	if st.Filter.Blur > 0.3 {
		p.blurCall(content, st.Filter.Blur*v.dp)
	} else {
		content.Add(gtx.Ops)
	}
	if st.Opacity < 1 {
		os.Pop()
	}
	if len(st.Transform) > 0 {
		ts.Pop()
	}
	outer.Pop()
	out := layout.Dimensions{Size: image.Pt(ceilI(b.w+ml+mr), ceilI(b.h+mt+mb))}
	out.Size = cs.Constrain(out.Size)
	if dims.Baseline != 0 {
		out.Baseline = dims.Baseline + ceilI(mb+b.bd[2])
	}
	return out
}

var _ = f32.Point{}

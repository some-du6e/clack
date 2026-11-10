package slackc

import (
	"image"
	"sync"
	"sync/atomic"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

// Reserved plugin slots: a plugin that wraps a component in more UI (Slick's
// ChatTabs puts a tab bar above RenderedViewContents' content) changes the
// component's layout when its first tree arrives. When that's after the
// client is shown (a first start that compiles the JS engine), the content
// would jump. So the insets of the original inside the plugin's tree are
// learned (Learned, kept by the ui root in ui.json) and, on the next start,
// until the component's first tree arrives, the native draws inside them
// (SetReserved): the plugin's output then fills space that was already
// there. A component no plugin patched once plugins started is released
// (UnreserveAll).

// Insets are the original's distances from the component's edges, in dp.
type Insets struct {
	T, R, B, L float32
}

func (i Insets) zero() bool { return i.T == 0 && i.R == 0 && i.B == 0 && i.L == 0 }

var (
	reserved atomic.Pointer[map[string]Insets]

	learnedMu  sync.Mutex
	learned    = map[string]Insets{}
	learnedVer atomic.Uint64
)

// SetReserved installs the insets to keep free until each component's
// first plugin tree (nil or empty: none).
func SetReserved(m map[string]Insets) {
	cp := map[string]Insets{}
	for k, v := range m {
		if !v.zero() {
			cp[k] = v
		}
	}
	if len(cp) == 0 {
		reserved.Store(nil)
		return
	}
	reserved.Store(&cp)
}

// UnreserveAll releases every reservation (plugins started and settled).
func UnreserveAll() { reserved.Store(nil) }

func unreserve(name string) {
	for {
		p := reserved.Load()
		if p == nil {
			return
		}
		if _, ok := (*p)[name]; !ok {
			return
		}
		cp := make(map[string]Insets, len(*p))
		for k, v := range *p {
			if k != name {
				cp[k] = v
			}
		}
		var next *map[string]Insets
		if len(cp) > 0 {
			next = &cp
		}
		if reserved.CompareAndSwap(p, next) {
			return
		}
	}
}

func reservedFor(name string) (Insets, bool) {
	p := reserved.Load()
	if p == nil {
		return Insets{}, false
	}
	i, ok := (*p)[name]
	return i, ok
}

// Learned returns the insets seen for singleton components (one live
// instance) and a version that changes when they do.
func Learned() (map[string]Insets, uint64) {
	learnedMu.Lock()
	out := make(map[string]Insets, len(learned))
	for k, v := range learned {
		out[k] = v
	}
	learnedMu.Unlock()
	counts := map[string]int{}
	for k := range insts {
		counts[k.name]++
	}
	for k := range out {
		if counts[k] != 1 {
			delete(out, k)
		}
	}
	return out, learnedVer.Load()
}

func learn(name string, i Insets) {
	learnedMu.Lock()
	if learned[name] != i {
		learned[name] = i
		learnedVer.Add(1)
	}
	learnedMu.Unlock()
}

// drawReserved draws native inside the reserved insets (exact constraints
// only: a component that fills its space).
func drawReserved(gtx layout.Context, ins Insets, native func(gtx layout.Context) layout.Dimensions) layout.Dimensions {
	c := gtx.Constraints
	if c.Min != c.Max || c.Max.X <= 0 || c.Max.Y <= 0 {
		return native(gtx)
	}
	t, r, b, l := gtx.Dp(unit.Dp(ins.T)), gtx.Dp(unit.Dp(ins.R)), gtx.Dp(unit.Dp(ins.B)), gtx.Dp(unit.Dp(ins.L))
	in := image.Pt(c.Max.X-l-r, c.Max.Y-t-b)
	if in.X <= 0 || in.Y <= 0 {
		return native(gtx)
	}
	st := op.Offset(image.Pt(l, t)).Push(gtx.Ops)
	g := gtx
	g.Constraints = layout.Exact(in)
	native(g)
	st.Pop()
	return layout.Dimensions{Size: c.Max}
}

// learnOriginal records where the original (the "@" box named name) sits
// in a laid-out plugin tree of size sz.
func (v *View) learnOriginal(name string, sz image.Point, pxPerDp float32) {
	if name == "" || sz.X <= 0 || sz.Y <= 0 || pxPerDp <= 0 {
		return
	}
	var found *box
	var walk func(bs []*box)
	walk = func(bs []*box) {
		for _, b := range bs {
			if found != nil {
				return
			}
			if b.native && b.n != nil && b.n.N == name {
				found = b
				return
			}
			walk(b.kids)
		}
	}
	walk(v.top)
	if found == nil {
		return
	}
	var x, y float32
	for p := found; p != nil && p != &v.main.root; p = p.lp {
		x += p.x + p.relX
		y += p.y + p.relY
	}
	ins := Insets{
		T: y / pxPerDp, L: x / pxPerDp,
		R: (float32(sz.X) - x - found.w) / pxPerDp,
		B: (float32(sz.Y) - y - found.h) / pxPerDp,
	}
	if ins.T < 0 || ins.L < 0 || ins.R < 0 || ins.B < 0 {
		return
	}
	learn(name, ins)
}

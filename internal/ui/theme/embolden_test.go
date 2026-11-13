package theme

import (
	"fmt"
	"math"
	"testing"

	"gioui.org/f32"
	"gioui.org/font/gofont"
	"gioui.org/text"
	gtot "github.com/go-text/typesetting/font/opentype"
	"golang.org/x/image/math/fixed"
)

func square(x0, y0, x1, y1 float32, cw bool) ([]f32.Point, []gtot.SegmentOp) {
	pts := []f32.Point{{X: x0, Y: y0}, {X: x1, Y: y0}, {X: x1, Y: y1}, {X: x0, Y: y1}, {X: x0, Y: y0}}
	if cw {
		pts = []f32.Point{{X: x0, Y: y0}, {X: x0, Y: y1}, {X: x1, Y: y1}, {X: x1, Y: y0}, {X: x0, Y: y0}}
	}
	return pts, []gtot.SegmentOp{gtot.SegmentOpMoveTo, gtot.SegmentOpLineTo, gtot.SegmentOpLineTo, gtot.SegmentOpLineTo, gtot.SegmentOpLineTo}
}

func bbox(pts, shift []f32.Point) (lo, hi f32.Point) {
	lo, hi = f32.Pt(1e9, 1e9), f32.Pt(-1e9, -1e9)
	for i, p := range pts {
		p = p.Add(shift[i])
		lo.X, lo.Y = min(lo.X, p.X), min(lo.Y, p.Y)
		hi.X, hi.Y = max(hi.X, p.X), max(hi.Y, p.Y)
	}
	return
}

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-4 }

func TestEmboldenGrowsEitherWinding(t *testing.T) {
	for _, cw := range []bool{false, true} {
		pts, ops := square(0, 0, 10, 10, cw)
		sh := emboldenPoints(pts, ops, 0.25, nil)
		lo, hi := bbox(pts, sh)
		if !near(lo.X, -0.25) || !near(lo.Y, -0.25) || !near(hi.X, 10.25) || !near(hi.Y, 10.25) {
			t.Errorf("cw=%v: grown box %v..%v, want -0.25..10.25", cw, lo, hi)
		}
		if sh[len(sh)-1] != sh[0] {
			t.Errorf("cw=%v: closing point shift %v differs from the first %v", cw, sh[len(sh)-1], sh[0])
		}
	}
}

func TestEmboldenShrinksHoles(t *testing.T) {
	// outer contour one way, hole the other (as in an "o")
	op, oo := square(0, 0, 10, 10, false)
	ip, io := square(3, 3, 7, 7, true)
	pts := append(op, ip...)
	ops := append(oo, io...)
	sh := emboldenPoints(pts, ops, 0.5, nil)
	lo, hi := bbox(pts[5:], sh[5:])
	if !near(lo.X, 3.5) || !near(hi.X, 6.5) || !near(lo.Y, 3.5) || !near(hi.Y, 6.5) {
		t.Errorf("hole %v..%v, want 3.5..6.5", lo, hi)
	}
	lo, hi = bbox(pts[:5], sh[:5])
	if !near(lo.X, -0.5) || !near(hi.X, 10.5) {
		t.Errorf("outer %v..%v, want -0.5..10.5", lo, hi)
	}
}

func TestEmboldenedShapeRegistered(t *testing.T) {
	sh := NewShaperFor(false)
	sh.LayoutString(text.Parameters{Font: Font(Regular), PxPerEm: fixed.I(15), MaxWidth: 1 << 20}, "Hello world")
	var gs []text.Glyph
	for g, ok := sh.NextGlyph(); ok; g, ok = sh.NextGlyph() {
		gs = append(gs, g)
	}
	if len(gs) == 0 {
		t.Fatal("no glyphs")
	}
	p1, ok := EmboldenedShape(sh, gs, 0.45)
	if !ok {
		t.Fatal("EmboldenedShape on a theme shaper: not ok")
	}
	p2, _ := EmboldenedShape(sh, gs, 0.45)
	if p1 != p2 {
		t.Error("second call did not hit the run cache")
	}
	other := text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
	if _, ok := EmboldenedShape(other, gs, 0.45); ok {
		t.Error("unregistered shaper should fall back")
	}
}

func BenchmarkEmboldenedShapeMiss(b *testing.B) {
	sh := NewShaperFor(false)
	sh.LayoutString(text.Parameters{Font: Font(Regular), PxPerEm: fixed.I(15), MaxWidth: 1 << 20}, "The quick brown fox jumps over the lazy dog")
	var gs []text.Glyph
	for g, ok := sh.NextGlyph(); ok; g, ok = sh.NextGlyph() {
		gs = append(gs, g)
	}
	v, _ := shaperFaces.Load(sh)
	e := v.(*emboldener)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		e.build(gs, 0.45)
	}
}

// The emboldened-run cache is bounded by its outlines' bytes as well as by
// count (maxRunBytes: message words filled 1,500 runs with 117 MB): a
// stream of distinct runs keeps it under the budget, and its byte count
// is the sum of what it holds.
func TestEmboldenCacheByteBudget(t *testing.T) {
	sh := NewShaperFor(false)
	v, _ := shaperFaces.Load(sh)
	e := v.(*emboldener)
	line := func(i int) []text.Glyph {
		sh.LayoutString(text.Parameters{Font: Font(Regular), PxPerEm: fixed.I(15), MaxWidth: 1 << 20},
			fmt.Sprintf("The quick brown fox %d jumps over the lazy dog %d times", i, i*7))
		var gs []text.Glyph
		for g, ok := sh.NextGlyph(); ok; g, ok = sh.NextGlyph() {
			gs = append(gs, g)
		}
		return gs
	}
	most := 0
	for i := 0; i < 3000; i++ {
		if _, ok := EmboldenedShape(sh, line(i), 0.45); !ok {
			t.Fatal("EmboldenedShape on a theme shaper: not ok")
		}
		e.mu.Lock()
		most = max(most, e.bytes)
		e.mu.Unlock()
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	sum := 0
	for el := e.lru.Front(); el != nil; el = el.Next() {
		sum += el.Value.(*runEntry).bytes
	}
	if sum != e.bytes {
		t.Errorf("the cache counts %d bytes, its runs hold %d", e.bytes, sum)
	}
	if most > maxRunBytes+(1<<20) {
		t.Errorf("the cache held %d bytes, budget %d", most, maxRunBytes)
	}
	if e.lru.Len() > maxRuns || e.lru.Len() < 10 {
		t.Errorf("%d runs cached", e.lru.Len())
	}
}

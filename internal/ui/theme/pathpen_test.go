package theme

import (
	"testing"

	"gioui.org/f32"
	"gioui.org/op"
)

func penTri(ops *op.Ops, x float32) (q PathPen) {
	q.Begin(ops)
	q.MoveTo(f32.Pt(0, 0))
	q.LineTo(f32.Pt(x, 0))
	q.LineTo(f32.Pt(x, x))
	q.Close()
	return q
}

func TestPathPenDedupesByContent(t *testing.T) {
	var frame op.Ops
	a, b, c := penTri(&frame, 10), penTri(&frame, 10), penTri(&frame, 11)
	sa, sb, sc := a.End(), b.End(), c.End()
	if sa != sb {
		t.Error("same shape built twice should give the same PathSpec")
	}
	if sa == sc {
		t.Error("different shapes share a PathSpec")
	}
	if RRectPath(20, 10, 4) != RRectPath(20, 10, 4) || RRectPath(20, 10, 4) == RRectPath(21, 10, 4) {
		t.Error("RRectPath cache keyed wrong")
	}
}

func BenchmarkPathPenHit(b *testing.B) {
	var frame op.Ops
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		q := penTri(&frame, 10)
		q.End()
	}
}

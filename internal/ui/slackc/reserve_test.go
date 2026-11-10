package slackc

import (
	"image"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

// A component kept for a plugin draws its native inside the reserved
// insets, at full size, until released; then it fills the space again.
func TestReservedSlot(t *testing.T) {
	t.Cleanup(UnreserveAll)
	var got layout.Constraints
	native := func(_ struct{}, gtx layout.Context, _ Props) layout.Dimensions {
		got = gtx.Constraints
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	frame := func() layout.Dimensions {
		var ops op.Ops
		gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 2, PxPerSp: 2}, Constraints: layout.Exact(image.Pt(800, 600))}
		return Component(gtx, "RenderedViewContents", struct{}{}, nil, native)
	}
	SetReserved(map[string]Insets{"RenderedViewContents": {T: 40}, "Zero": {}})
	if d := frame(); d.Size != image.Pt(800, 600) {
		t.Fatalf("reserved component reports %v, want its full 800x600", d.Size)
	}
	if got.Max != image.Pt(800, 600-80) {
		t.Fatalf("native drew in %v, want 800x520 (40 dp at 2 px/dp kept free)", got.Max)
	}
	UnreserveAll()
	frame()
	if got.Max != image.Pt(800, 600) {
		t.Fatalf("after release the native drew in %v, want 800x600", got.Max)
	}
	if reserved.Load() != nil {
		t.Fatal("a zero inset was kept")
	}
}

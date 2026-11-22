package richview

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// An attachment's remove button has AttachmentDeleteButton's Tooltip:
// "Remove preview", above it, after BaseTooltip's 150 ms; Escape or a
// menu opening (the TooltipManager) hides it until the pointer leaves.
func TestRemoveButtonTooltip(t *testing.T) {
	st := newTestStyle()
	v := New(embedDoc(t, "x post"), Options{Own: true})
	var r input.Router
	var ops op.Ops
	const w, h = 900, 400
	t0 := time.Unix(1000, 0)
	frame := func(now time.Time) {
		ops.Reset()
		paint.FillShape(&ops, st.Palette.Panel, clip.Rect{Max: image.Pt(w, h)}.Op())
		gtx := layout.Context{Ops: &ops, Source: r.Source(), Now: now, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(w, h))}
		v.Layout(gtx, st, &removeRec{})
		r.Frame(&ops)
	}
	frame(t0)
	if len(v.d.removes) != 1 {
		t.Fatalf("%d remove buttons", len(v.d.removes))
	}
	card := cardBox(t, v)
	r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(card.Min.X+20), float32(card.Min.Y+20))})
	frame(t0)
	rm := v.d.removes[0].r
	c := f32.Pt(float32(rm.Min.X+rm.Max.X)/2, float32(rm.Min.Y+rm.Max.Y)/2)
	r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: c})
	frame(t0)
	above := image.Rect(rm.Min.X-60, rm.Min.Y-40, rm.Max.X+60, rm.Min.Y-6).Intersect(image.Rect(0, 0, w, h))
	frame(t0.Add(100 * time.Millisecond))
	if removeTipUpT(v, t0.Add(100*time.Millisecond)) {
		t.Error("the remove button's tooltip showed before BaseTooltip's 150 ms")
	}
	frame(t0.Add(170 * time.Millisecond))
	if !removeTipUpT(v, t0.Add(170*time.Millisecond)) {
		t.Fatal("no tooltip over the remove button after 150 ms")
	}
	hideTipsT()
	frame(t0.Add(300 * time.Millisecond))
	if removeTipUpT(v, t0.Add(300*time.Millisecond)) {
		t.Error("the tooltip stayed after HideTooltips (Escape, a menu opening)")
	}
	// off the button and back: it waits its delay again, then shows; the
	// pixels (CI's GPU steps; skipped without one)
	r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(card.Min.X+20), float32(card.Min.Y+20))})
	frame(t0.Add(400 * time.Millisecond))
	t1 := t0.Add(500 * time.Millisecond)
	r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: c})
	frame(t1)
	frame(t1.Add(50 * time.Millisecond))
	if removeTipUpT(v, t1.Add(50*time.Millisecond)) {
		t.Error("back on the button, the tooltip showed before its delay")
	}
	early := pixeltest.Render(t, w, h, &ops)
	frame(t1.Add(170 * time.Millisecond))
	if !removeTipUpT(v, t1.Add(170*time.Millisecond)) {
		t.Fatal("back on the button, no tooltip after its delay")
	}
	late := pixeltest.Render(t, w, h, &ops)
	if n := pixeltest.Count(early, above, st.Palette.TooltipBg, 8); n > 30 {
		t.Errorf("tooltip pixels before the delay (%d)", n)
	}
	if n := pixeltest.Count(late, above, st.Palette.TooltipBg, 8); n < 100 {
		t.Errorf("no tooltip drawn above the remove button (%d px)", n)
	}
}

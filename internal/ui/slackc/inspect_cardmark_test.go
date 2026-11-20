package slackc

import (
	"image"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/op"
)

// A popup that lays its content out before it draws its card (ProfileCard,
// every popups.Popover) records the card first, as an inspect mark, and
// closes it at the card's box (EndCardAt): the content is inside the card
// (its chain reaches the card's element), its components are what the
// picker finds over them, the card's element wherever nothing inside it
// is, and the layer takes the card as its box. The element the card enters
// for its styles (recorded after the content) has no area.
func TestInspectCardMarkHoldsContent(t *testing.T) {
	cardClass := Class("p-test_card", "role", "dialog")
	card := image.Rect(100, 80, 300, 240)
	var r input.Router
	var f InspectFrame
	frame := func() {
		gtx := testGtx(400, 300)
		gtx.Source = r.Source()
		InspectBegin(&f, r.Source())
		lm := InspectOverlay("profile card (test)")
		cm := cardClass.Inspect(0)
		m := op.Record(gtx.Ops)
		func() {
			defer op.Offset(image.Pt(20, 30)).Push(gtx.Ops).Pop()
			InspectAs("Inner").End(gtx, image.Pt(80, 40))
		}()
		call := m.Stop()
		st := cardClass.Enter(gtx, 0, 0)
		func() {
			defer op.Offset(card.Min).Push(gtx.Ops).Pop()
			call.Add(gtx.Ops)
		}()
		st.Leave()
		cm.EndCardAt(gtx, card)
		lm.EndCard(gtx)
		InspectEnd()
		r.Frame(gtx.Ops)
	}
	frame()
	if got := pickedAt(&r, &f, f32.Pt(150, 130)); got != "Inner" {
		t.Errorf("over the card's content: picked %q, want its component Inner", got)
	}
	inner, layer := -1, -1
	for i := range f.Nodes {
		switch n := &f.Nodes[i]; {
		case n.Kind == InspectComponent && n.Name == "Inner":
			inner = i
		case n.Kind == InspectLayer:
			layer = i
		}
	}
	inCard := false
	for _, j := range f.Chain(inner) {
		if n := &f.Nodes[j]; n.Kind == InspectElement && n.Sized() && n.Tag() == "div" {
			inCard = true
		}
	}
	if !inCard {
		t.Error("the content's component isn't inside the card's element")
	}
	if layer < 0 || f.Nodes[layer].Size != card.Size() {
		t.Errorf("the layer's box: node %d, want the card's %v", layer, card.Size())
	}
	frame()
	if got := pickedAt(&r, &f, f32.Pt(280, 220)); got != "div.p-test_card" {
		t.Errorf("over the card's padding: picked %q, want the card's div.p-test_card", got)
	}
}

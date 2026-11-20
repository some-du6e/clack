package slackc

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// pickedAt moves the pointer to p over the areas f put down in the frame
// just routed and names what the picker would take there: of the nodes
// hit, the one recorded last (an element by its selector).
func pickedAt(r *input.Router, f *InspectFrame, p f32.Point) string {
	r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p})
	hits := f.ReadHits(r.Source(), func(time.Duration) (f32.Point, bool) { return p, true })
	best := -1
	for _, h := range hits {
		best = max(best, h.Node)
	}
	if best < 0 {
		return ""
	}
	n := &f.Nodes[best]
	if n.Kind == InspectElement {
		return n.Tag() + "." + strings.Join(n.Classes(), ".")
	}
	return n.Name
}

// A popup that lays out what its card holds before it enters the card's
// element (ProfileCard, every popups.Popover), then boxes the card
// (InspectCard): the card's element, recorded after the content, gets no
// area of its own, so the content's components are what the picker finds
// over them (it keeps the node recorded last), and the layer, given the
// card as its box, everywhere else on the card (review of a8fa243c: the
// card's element came out on top everywhere).
func TestInspectCardAfterContent(t *testing.T) {
	cardClass := Class("p-test_card", "role", "dialog")
	card := image.Rect(100, 80, 300, 240)
	var r input.Router
	var f InspectFrame
	frame := func() {
		gtx := testGtx(400, 300)
		gtx.Source = r.Source()
		InspectBegin(&f, r.Source())
		lm := InspectOverlay("popover (test)")
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
		InspectCard(gtx, card)
		st.Leave()
		lm.EndCard(gtx)
		InspectEnd()
		r.Frame(gtx.Ops)
	}
	frame()
	if got := pickedAt(&r, &f, f32.Pt(150, 130)); got != "Inner" {
		t.Errorf("over the card's content: picked %q, want its component Inner", got)
	}
	frame()
	if got := pickedAt(&r, &f, f32.Pt(280, 220)); got != "popover (test)" {
		t.Errorf("over the card's padding: picked %q, want the popup's layer (the card its box)", got)
	}
}

// A plugin's overlay root (api.modal, api.menu, a <dialog> shown modal:
// LayoutOverlays) is a popup layer of the inspect tool's record, as the
// shell's overlays are: what its tree draws reports the layer as its
// region (review of a8fa243c: picks in a Taut or Slick modal reported the
// window column under it).
func TestInspectPluginOverlayLayer(t *testing.T) {
	withStyles(t, func(n *Node, s *css.Style) {
		if n.ID == "card" {
			s.Position = css.PosFixed
			s.Left, s.Top = px(120), px(90)
			s.Width, s.Height = px(160), px(100)
			s.PointerEvents = true
		}
	})
	windowSize = image.Pt(400, 300)
	const root = 1<<52 | 77
	deliverOverlay(root, mustTree(t, `[{"t":"div","k":1,"p":{"id":"card","className":"c-sk-modal"}}]`))
	defer deliverOverlay(root, nil)
	var r input.Router
	var f InspectFrame
	var ops op.Ops
	func() {
		LockFrame()
		defer UnlockFrame()
		gtx := layout.Context{Ops: &ops, Now: time.Unix(5000, 0), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(400, 300)), Source: r.Source()}
		Frame(gtx)
		InspectBegin(&f, r.Source())
		LayoutOverlays(gtx)
		InspectEnd()
	}()
	r.Frame(&ops)
	tree := -1
	for i := range f.Nodes {
		if f.Nodes[i].Kind == InspectTree {
			tree = i
		}
	}
	if tree < 0 {
		t.Fatalf("no plugin tree recorded (%d nodes)", len(f.Nodes))
	}
	layer := ""
	for _, j := range f.Chain(tree) {
		if n := &f.Nodes[j]; n.Kind == InspectLayer {
			layer = n.Name
			break
		}
	}
	if !strings.HasPrefix(layer, "plugin") {
		t.Errorf("the plugin overlay's tree is in layer %q, want a plugin layer of its own", layer)
	}
}

// A plugin tree's title tooltip (drawTip) is a tooltip layer of the
// record in both themes (review of a8fa243c: only dark mode's outline
// went through theme.TooltipShape, so in a light theme, Karim's, the
// tooltip had no area and the picker went through it).
func TestInspectTreeTooltipEitherTheme(t *testing.T) {
	withStyles(t, func(n *Node, s *css.Style) {
		s.PointerEvents = true
		if n.ID == "btn" {
			s.Display = css.DisplayBlock
			s.Width, s.Height = px(80), px(30)
		}
	})
	windowSize = image.Pt(400, 300)
	prev := env()
	t.Cleanup(func() { Init(prev) })
	for _, dark := range []bool{false, true} {
		Init(Env{Theme: theme.New(dark)})
		v := NewView(91)
		tree := mustTree(t, `{"t":"button","k":1,"p":{"id":"btn","title":"Mute notifications"}}`)
		var r input.Router
		var f InspectFrame
		now := time.Unix(2000, 0)
		frame := func(record bool) {
			gtx := testGtx(400, 300)
			gtx.Now, gtx.Source = now, r.Source()
			if record {
				InspectBegin(&f, r.Source())
			}
			func() {
				defer op.Offset(image.Pt(100, 150)).Push(gtx.Ops).Pop()
				v.Layout(gtx, tree, "Orig", nil, nil)
			}()
			if record {
				InspectEnd()
			}
			r.Frame(gtx.Ops)
		}
		frame(false)
		b := v.main.hitTest(f32.Pt(10, 10))
		if b == nil {
			t.Fatal("no box under the button")
		}
		// resting on the button past the tooltip's 500 ms
		v.tip = tipState{b: b, text: "Mute notifications", since: now}
		now = now.Add(600 * time.Millisecond)
		frame(true)
		found := false
		for i := range f.Nodes {
			if n := &f.Nodes[i]; n.Kind == InspectLayer && n.Name == "tooltip" && n.Sized() {
				found = true
			}
		}
		if !found {
			t.Errorf("dark %v: the tree's title tooltip isn't a tooltip layer of the record", dark)
		}
		v.Close()
	}
}

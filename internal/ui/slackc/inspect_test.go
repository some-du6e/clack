package slackc

import (
	"image"
	"image/color"
	"slices"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"

	"github.com/some-du6e/clack/internal/ui/slackc/css"
)

var (
	inspMsgClass   = Class("c-message_kit__background c-message_kit__message", "data-qa", "message_container")
	inspReactClass = Class("c-button-unstyled c-reaction", "data-qa", "reactji").Tag("button").Mods("c-reaction--reacted")
	inspItemClass  = Class("c-virtual_list__item", "role", "listitem")
)

// inspectScene lays out a message (20,30 300×60) with a reaction pill
// inside (40,70 50×20), the pill's element entered reacted, and a plain
// Gio handler (not pass-through) drawn after it over the message's right
// part (x ≥ 220), as a toolbar would be.
func inspectScene(gtx layout.Context, blocker *int) {
	func() {
		defer op.Offset(image.Pt(20, 30)).Push(gtx.Ops).Pop()
		item := inspItemClass.EnterAttr(gtx, 0, 0, "data-item-key", "1700000000.000100")
		defer item.Leave()
		Render(gtx, "Message", Props{"channelId": "C0TESTCH01", "$key": "1700000000.000100"}, func(gtx layout.Context, _ Props) layout.Dimensions {
			el := inspMsgClass.Enter(gtx, 0, 0)
			defer el.Leave()
			func() {
				defer op.Offset(image.Pt(20, 40)).Push(gtx.Ops).Pop()
				Render(gtx, "Reaction", nil, func(gtx layout.Context, _ Props) layout.Dimensions {
					st := inspReactClass.Enter(gtx, 0, 1)
					defer st.Leave()
					return layout.Dimensions{Size: image.Pt(50, 20)}
				})
			}()
			return layout.Dimensions{Size: image.Pt(300, 60)}
		})
	}()
	ar := clip.Rect(image.Rect(220, 30, 320, 90)).Push(gtx.Ops)
	event.Op(gtx.Ops, blocker)
	ar.Pop()
}

func names(f *InspectFrame, hits []InspectHit) []string {
	var out []string
	for _, h := range hits {
		out = append(out, f.Nodes[h.Node].Name)
	}
	return out
}

// While a frame is recorded, every component boundary gets a
// pass-through area, and Gio's hit test says which are under the pointer
// and where they are; what a later handler covers isn't.
func TestInspectHitsComponents(t *testing.T) {
	var r input.Router
	var f InspectFrame
	var blocker int
	ptr := f32.Point{}
	winAt := func(time.Duration) (f32.Point, bool) { return ptr, true }
	frame := func() {
		gtx := testGtx(400, 300)
		gtx.Source = r.Source()
		InspectBegin(&f, r.Source())
		gtx.Event(pointer.Filter{Target: &blocker, Kinds: pointer.Move | pointer.Press})
		inspectScene(gtx, &blocker)
		InspectEnd()
		r.Frame(gtx.Ops)
	}
	move := func(x, y float32) []InspectHit {
		ptr = f32.Pt(x, y)
		r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: ptr})
		return f.ReadHits(r.Source(), winAt)
	}
	frame()
	if Inspecting() || CurrentInspectFrame() != nil {
		t.Fatal("still recording after InspectEnd")
	}

	hits := move(50, 75) // on the pill
	if got := names(&f, hits); !slices.Equal(got, []string{"Message", "Reaction"}) {
		t.Fatalf("on the pill: hits %v, want Message and Reaction", got)
	}
	for _, h := range hits {
		want := map[string]image.Point{"Message": {20, 30}, "Reaction": {40, 70}}[f.Nodes[h.Node].Name]
		if h.Origin != want {
			t.Errorf("%s's origin %v, want %v", f.Nodes[h.Node].Name, h.Origin, want)
		}
	}
	pill := hits[1].Node
	n := &f.Nodes[pill]
	if n.Size != image.Pt(50, 20) {
		t.Errorf("the pill's size %v, want 50×20", n.Size)
	}
	// its element: the class entered inside it, reacted
	var el *InspectNode
	for i := range f.Nodes {
		if f.Nodes[i].Parent == pill && f.Nodes[i].Kind == InspectElement {
			el = &f.Nodes[i]
		}
	}
	if el == nil {
		t.Fatal("no element recorded inside the pill")
	}
	if el.Tag() != "button" || !slices.Equal(el.Classes(), []string{"c-button-unstyled", "c-reaction", "c-reaction--reacted"}) {
		t.Errorf("the pill's element %s %v", el.Tag(), el.Classes())
	}
	if a := el.Attrs(); !slices.Equal(a, []string{"data-qa", "reactji"}) {
		t.Errorf("the pill's attributes %v", a)
	}
	// the chain from the pill out: elements and components as they nested
	var chain []string
	for _, i := range f.Chain(pill) {
		x := &f.Nodes[i]
		if x.Kind == InspectComponent {
			chain = append(chain, x.Name)
		} else {
			chain = append(chain, x.Classes()[0])
		}
	}
	if want := []string{"Reaction", "c-message_kit__background", "Message", "c-virtual_list__item"}; !slices.Equal(chain, want) {
		t.Errorf("the pill's chain %v, want %v", chain, want)
	}
	item := &f.Nodes[f.Chain(pill)[3]]
	if k, v := item.RuntimeAttr(); k != "data-item-key" || v != "1700000000.000100" {
		t.Errorf("the list item's run-time attribute %q=%q", k, v)
	}
	if p := f.Nodes[f.Chain(pill)[2]].Props(); p["channelId"] != "C0TESTCH01" {
		t.Errorf("the message's props %v", p)
	}

	if got := names(&f, move(100, 40)); !slices.Equal(got, []string{"Message"}) {
		t.Errorf("on the message beside the pill: hits %v, want Message", got)
	}
	if got := names(&f, move(250, 60)); len(got) != 0 {
		t.Errorf("on the handler drawn over the message: hits %v, want none", got)
	}
	if got := names(&f, move(10, 10)); len(got) != 0 {
		t.Errorf("outside everything: hits %v, want none", got)
	}

	// a new frame: new areas, and the Enter Gio sends areas that appear
	// under a still pointer
	ptr = f32.Pt(50, 75)
	r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: ptr})
	frame()
	if got := names(&f, f.ReadHits(r.Source(), winAt)); !slices.Equal(got, []string{"Message", "Reaction"}) {
		t.Errorf("after a new frame under a still pointer: hits %v, want Message and Reaction", got)
	}
}

// Experiment reads go to the node drawing.
func TestInspectNotesExperiments(t *testing.T) {
	var r input.Router
	var f InspectFrame
	gtx := testGtx(100, 100)
	gtx.Source = r.Source()
	InspectBegin(&f, r.Source())
	Render(gtx, "Outer", nil, func(gtx layout.Context, _ Props) layout.Dimensions {
		f.NoteExperiment("outer_exp", "on")
		Render(gtx, "Inner", nil, func(gtx layout.Context, _ Props) layout.Dimensions {
			f.NoteExperiment("inner_exp", "")
			f.NoteExperiment("inner_exp", "")
			f.NoteExperiment("b_exp", "on")
			return layout.Dimensions{Size: image.Pt(10, 10)}
		})
		return layout.Dimensions{Size: image.Pt(50, 50)}
	})
	InspectEnd()
	got := map[string][]string{}
	for _, n := range f.Nodes {
		got[n.Name] = n.Exps
	}
	if !slices.Equal(got["Outer"], []string{"outer_exp=on"}) || !slices.Equal(got["Inner"], []string{"b_exp=on", "inner_exp=(none)"}) {
		t.Errorf("experiments by node: %v", got)
	}
}

// A plugin's host tree is one node whose boxes the scope's own hit test
// finds: the innermost element under the point, then its ancestors, with
// classes, data-qa and computed colours.
func TestInspectTreeBoxes(t *testing.T) {
	barBg := color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff}
	chipFg := color.NRGBA{R: 0xff, A: 0xff}
	withStyles(t, func(n *Node, s *css.Style) {
		s.PointerEvents = true
		switch n.ID {
		case "bar":
			s.Width, s.Height = px(200), px(30)
			s.Padding[0] = px(10)
			s.Background.Color = barBg
		case "chip":
			s.Display = css.DisplayBlock
			s.Width, s.Height = px(60), px(20)
			s.Color = chipFg
		}
	})
	windowSize = image.Pt(400, 300)
	var r input.Router
	var f InspectFrame
	v := NewView(7)
	tree := mustTree(t, `{"t":"div","k":1,"p":{"id":"bar","className":"p-bar p-bar--wide"},"c":[{"t":"button","k":2,"p":{"id":"chip","className":"c-chip","data-qa":"chip_button","aria-label":"Alice's chip"}}]}`)
	frame := func() {
		gtx := testGtx(400, 300)
		gtx.Source = r.Source()
		InspectBegin(&f, r.Source())
		func() {
			defer op.Offset(image.Pt(30, 50)).Push(gtx.Ops).Pop()
			v.Layout(gtx, tree, "Orig", nil, nil)
		}()
		InspectEnd()
		r.Frame(gtx.Ops)
	}
	frame()
	ptr := f32.Pt(40, 65) // on the chip: the tree's 10,15
	r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: ptr})
	hits := f.ReadHits(r.Source(), func(time.Duration) (f32.Point, bool) { return ptr, true })
	tree0 := -1
	for _, h := range hits {
		if f.Nodes[h.Node].Kind == InspectTree {
			tree0 = h.Node
			if h.Origin != image.Pt(30, 50) {
				t.Errorf("the tree's origin %v, want 30,50", h.Origin)
			}
			bs := f.Boxes(h.Node, h.Local)
			if len(bs) != 2 {
				t.Fatalf("boxes under the chip: %+v, want the chip and the bar", bs)
			}
			chip, bar := bs[0], bs[1]
			if chip.Tag != "button" || !slices.Equal(chip.Classes, []string{"c-chip"}) || !slices.Equal(chip.Attrs, []string{"data-qa", "chip_button"}) {
				t.Errorf("the chip's box %+v (no aria-label: plugins fill it with words)", chip)
			}
			if chip.Rect != image.Rect(0, 10, 60, 30) || chip.Color != chipFg {
				t.Errorf("the chip's rect %v colour %v", chip.Rect, chip.Color)
			}
			if !slices.Equal(bar.Classes, []string{"p-bar", "p-bar--wide"}) || bar.Background != barBg {
				t.Errorf("the bar's box %+v", bar)
			}
		}
	}
	if tree0 < 0 {
		t.Fatalf("no tree node under the chip: hits %+v", hits)
	}
}

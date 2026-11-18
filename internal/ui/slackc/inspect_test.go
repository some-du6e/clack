package slackc

import (
	"image"
	"image/color"
	"slices"
	"strings"
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

// A held frame (Hold: the inspect tool's freeze draws a frame's ops
// again instead of recording a new one) keeps each area as its own events
// left it. Under a still pointer the areas stay hit with no new Enter,
// a move over the frame moves the hits, the handler drawn over the message still covers it, and
// the pointer leaving the window leaves them all. Recording the frame
// afresh ends the hold.
func TestInspectHeldHits(t *testing.T) {
	var r input.Router
	var f InspectFrame
	var blocker int
	ptr := f32.Point{}
	winAt := func(time.Duration) (f32.Point, bool) { return ptr, true }
	gtx := testGtx(400, 300)
	gtx.Source = r.Source()
	InspectBegin(&f, r.Source())
	gtx.Event(pointer.Filter{Target: &blocker, Kinds: pointer.Move | pointer.Press})
	inspectScene(gtx, &blocker)
	InspectEnd()
	r.Frame(gtx.Ops)
	frozen := gtx.Ops
	var show op.Ops
	again := func() { // the frozen frame's ops drawn once more
		show.Reset()
		frozen.Whole().Add(&show)
		r.Frame(&show)
	}
	move := func(x, y float32) {
		ptr = f32.Pt(x, y)
		r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: ptr})
	}
	pill := []string{"Message", "Reaction"}
	move(50, 75)
	hits := f.ReadHits(r.Source(), winAt)
	if got := names(&f, hits); !slices.Equal(got, pill) {
		t.Fatalf("on the pill: hits %v, want %v", got, pill)
	}
	f.Hold(hits)
	if !f.Held() {
		t.Fatal("Hold didn't hold")
	}
	for i := range 3 {
		again()
		hits = f.ReadHits(r.Source(), winAt)
		if got := names(&f, hits); !slices.Equal(got, pill) {
			t.Fatalf("held frame %d, the pointer still: hits %v, want %v", i, got, pill)
		}
	}
	if hits[1].Origin != image.Pt(40, 70) {
		t.Errorf("the held pill's origin %v, want 40,70", hits[1].Origin)
	}
	move(100, 40)
	if got := names(&f, f.ReadHits(r.Source(), winAt)); !slices.Equal(got, []string{"Message"}) {
		t.Errorf("held, on the message beside the pill: hits %v, want Message", got)
	}
	again()
	if got := names(&f, f.ReadHits(r.Source(), winAt)); !slices.Equal(got, []string{"Message"}) {
		t.Errorf("held, still beside the pill a frame later: hits %v, want Message", got)
	}
	move(250, 60)
	if got := names(&f, f.ReadHits(r.Source(), winAt)); len(got) != 0 {
		t.Errorf("held, on the handler drawn over the message: hits %v, want none", got)
	}
	again()
	move(50, 75)
	if got := names(&f, f.ReadHits(r.Source(), winAt)); !slices.Equal(got, pill) {
		t.Errorf("held, back on the pill a frame later: hits %v, want %v", got, pill)
	}
	r.Queue(pointer.Event{Kind: pointer.Leave, Source: pointer.Mouse, Position: f32.Pt(500, 75)})
	if got := names(&f, f.ReadHits(r.Source(), winAt)); len(got) != 0 {
		t.Errorf("held, the pointer out of the window: hits %v, want none", got)
	}
	InspectBegin(&f, r.Source())
	InspectEnd()
	if f.Held() {
		t.Error("recording afresh left the frame held")
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

var (
	inspBarClass  = Class("p-view_header")
	inspTabClass  = Class("c-tabs__tab", "role", "tab").Tag("button").Mods("c-tabs__tab--active")
	inspNameClass = Class("c-tabs__tab_content").Tag("span")
	inspRailClass = Class("p-tab_rail")
)

// The chrome's elements marked for the tool alone (ClassDef.Inspect)
// get areas the picker hits, nest as they were drawn (a component's
// render inside them is inside them), and touch nothing styles see: no
// element goes on the element stack. An entered element given a box
// (InspectArea) is hit too. Outside a session nothing is recorded.
func TestInspectMarks(t *testing.T) {
	var r input.Router
	var f InspectFrame
	ptr := f32.Point{}
	winAt := func(time.Duration) (f32.Point, bool) { return ptr, true }
	depth := -1
	scene := func(gtx layout.Context) {
		// a rail at 0,0 70×300: marked, closed at its rect
		rail := inspRailClass.Inspect(0)
		rail.EndAt(gtx, image.Rect(0, 0, 70, 300))
		// a header at 100,0 400×50, entered and given its box at the end;
		// a tab inside it at 120,10 80×30 (active) with its title at
		// +30,5 40×20, then a component beside it
		defer op.Offset(image.Pt(100, 0)).Push(gtx.Ops).Pop()
		bar := inspBarClass.Enter(gtx, 0, 0)
		func() {
			defer op.Offset(image.Pt(20, 10)).Push(gtx.Ops).Pop()
			defer inspTabClass.Inspect(1).End(gtx, image.Pt(80, 30))
			func() {
				defer op.Offset(image.Pt(30, 5)).Push(gtx.Ops).Pop()
				mk := inspNameClass.Inspect(0)
				depth = len(stack)
				mk.End(gtx, image.Pt(40, 20))
			}()
		}()
		func() {
			defer op.Offset(image.Pt(200, 10)).Push(gtx.Ops).Pop()
			Render(gtx, "AvatarStack", nil, func(gtx layout.Context, _ Props) layout.Dimensions {
				return layout.Dimensions{Size: image.Pt(60, 30)}
			})
		}()
		InspectArea(gtx, image.Rectangle{Max: image.Pt(400, 50)})
		bar.Leave()
	}
	frame := func() {
		gtx := testGtx(600, 400)
		gtx.Source = r.Source()
		InspectBegin(&f, r.Source())
		before := len(stack)
		scene(gtx)
		if depth != before {
			t.Errorf("Inspect changed the element stack: %d entries inside, %d before", depth, before)
		}
		InspectEnd()
		r.Frame(gtx.Ops)
	}
	label := func(i int) string {
		n := &f.Nodes[i]
		if n.Kind == InspectComponent {
			return n.Name
		}
		return strings.Join(n.Classes(), ".")
	}
	move := func(x, y float32) (out []string, hits []InspectHit) {
		ptr = f32.Pt(x, y)
		r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: ptr})
		hits = f.ReadHits(r.Source(), winAt)
		for _, h := range hits {
			out = append(out, label(h.Node))
		}
		return out, hits
	}
	frame()

	got, hits := move(160, 25) // the tab's title
	if want := []string{"p-view_header", "c-tabs__tab.c-tabs__tab--active", "c-tabs__tab_content"}; !slices.Equal(got, want) {
		t.Fatalf("on the tab's title: hits %v, want %v", got, want)
	}
	for _, h := range hits {
		n := &f.Nodes[h.Node]
		want := map[string]image.Rectangle{
			"p-view_header":                   image.Rect(100, 0, 500, 50),
			"c-tabs__tab.c-tabs__tab--active": image.Rect(120, 10, 200, 40),
			"c-tabs__tab_content":             image.Rect(150, 15, 190, 35),
		}[label(h.Node)]
		if got := (image.Rectangle{Min: h.Origin, Max: h.Origin.Add(n.Size)}); got != want || !n.Sized() {
			t.Errorf("%s: at %v (sized %v), want %v", label(h.Node), got, n.Sized(), want)
		}
	}
	title := hits[2].Node
	var chain []string
	for _, i := range f.Chain(title) {
		chain = append(chain, label(i))
	}
	if want := []string{"c-tabs__tab_content", "c-tabs__tab.c-tabs__tab--active", "p-view_header"}; !slices.Equal(chain, want) {
		t.Errorf("the title's chain %v, want %v", chain, want)
	}
	if n := &f.Nodes[title]; n.Tag() != "span" || n.Kind != InspectElement {
		t.Errorf("the title: %v %q", n.Kind, n.Tag())
	}
	if got, _ := move(320, 20); !slices.Equal(got, []string{"p-view_header", "AvatarStack"}) {
		t.Errorf("on the component beside the tab: hits %v", got)
	}
	if p := f.Nodes[f.Chain(hits[0].Node)[0]].Parent; p != -1 {
		t.Errorf("the header's parent %d, want none", p)
	}
	if got, _ := move(30, 200); !slices.Equal(got, []string{"p-tab_rail"}) {
		t.Errorf("on the rail: hits %v", got)
	}
	if got, _ := move(550, 200); len(got) != 0 {
		t.Errorf("outside everything: hits %v", got)
	}

	// no session: no record, nothing on the stack, no op
	if m := inspTabClass.Inspect(0); m != (InspectMark{}) {
		t.Errorf("a mark outside a session: %+v", m)
	}
	nodes := len(f.Nodes)
	gtx := testGtx(100, 100)
	inspTabClass.Inspect(0).End(gtx, image.Pt(10, 10))
	InspectArea(gtx, image.Rect(0, 0, 10, 10))
	if len(f.Nodes) != nodes || Inspecting() {
		t.Error("marks outside a session recorded something")
	}
}

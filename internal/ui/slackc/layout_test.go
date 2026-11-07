package slackc

import (
	"encoding/json"
	"image"
	"image/color"
	"math"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc/css"
)

// withStyles installs computed styles per node (the CSS engine may be a
// stub): UA display plus whatever f sets, keyed by node id.
func withStyles(t testing.TB, f func(n *Node, s *css.Style)) {
	styleHook = func(n *Node, parent *css.Style) (css.Style, bool) {
		s := inheritOnly(parent, color.NRGBA{A: 0xff})
		if n.T == "@" {
			s.Display = css.DisplayInlineBlock
		} else {
			s.Display = uaDisplay(n.T, replFor(n))
		}
		f(n, &s)
		return s, true
	}
	t.Cleanup(func() { styleHook = nil })
}

func testGtx(w, h int) layout.Context {
	return layout.Context{
		Ops:         new(op.Ops),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Constraints{Max: image.Pt(w, h)},
		Now:         time.Unix(1000, 0),
	}
}

func mustTree(t testing.TB, js string) []*Node {
	t.Helper()
	nodes, err := ParseTree([]byte(js))
	if err != nil {
		t.Fatal(err)
	}
	return nodes
}

func layoutJS(t testing.TB, js string, w, h int) (*View, layout.Dimensions) {
	t.Helper()
	windowSize = image.Pt(1000, 800)
	v := NewView(1)
	gtx := testGtx(w, h)
	d := v.Layout(gtx, mustTree(t, js), "Orig", nil, nil)
	return v, d
}

func byID(v *View, id string) *box {
	var find func(list []*box) *box
	find = func(list []*box) *box {
		for _, b := range list {
			if b.n != nil && b.n.ID == id {
				return b
			}
			if r := find(b.kids); r != nil {
				return r
			}
		}
		return nil
	}
	return find(v.top)
}

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 0.6 }

func checkBox(t *testing.T, v *View, id string, x, y, w, h float32) {
	t.Helper()
	b := byID(v, id)
	if b == nil {
		t.Fatalf("no box %q", id)
	}
	// absolute position: sum layout-parent offsets
	ax, ay := b.x+b.relX, b.y+b.relY
	for p := b.lp; p != nil; p = p.lp {
		ax += p.x + p.relX
		ay += p.y + p.relY
	}
	if x >= 0 && !near(ax, x) || y >= 0 && !near(ay, y) || w >= 0 && !near(b.w, w) || h >= 0 && !near(b.h, h) {
		t.Errorf("%s: got (%.1f,%.1f %.1fx%.1f), want (%v,%v %vx%v)", id, ax, ay, b.w, b.h, x, y, w, h)
	}
}

func px(v float32) css.Length { return css.Px(v) }

func TestFlexGrow(t *testing.T) {
	withStyles(t, func(n *Node, s *css.Style) {
		switch n.ID {
		case "row":
			s.Display = css.DisplayFlex
			s.Width = px(300)
		case "a":
			s.Width, s.Height = px(50), px(10)
		case "b":
			s.FlexGrow = 1
			s.Height = px(10)
		case "c":
			s.FlexGrow = 2
			s.FlexBasis = px(0)
			s.Height = px(10)
		}
	})
	v, _ := layoutJS(t, `{"t":"div","k":1,"p":{"id":"row"},"c":[{"t":"div","k":2,"p":{"id":"a"}},{"t":"div","k":3,"p":{"id":"b"}},{"t":"div","k":4,"p":{"id":"c"}}]}`, 1000, 1000)
	checkBox(t, v, "a", 0, 0, 50, 10)
	checkBox(t, v, "b", 50, 0, 250.0/3, 10)
	checkBox(t, v, "c", 50+250.0/3, 0, 500.0/3, 10)
}

func TestFlexShrinkAndMin(t *testing.T) {
	withStyles(t, func(n *Node, s *css.Style) {
		switch n.ID {
		case "row":
			s.Display = css.DisplayFlex
			s.Width = px(200)
		case "a", "b":
			s.Width, s.Height = px(150), px(10)
		case "c":
			s.Width, s.Height = px(150), px(10)
			s.MinWidth = px(120)
		}
	})
	v, _ := layoutJS(t, `{"t":"div","k":1,"p":{"id":"row"},"c":[{"t":"div","k":2,"p":{"id":"a"}},{"t":"div","k":3,"p":{"id":"b"}}]}`, 1000, 1000)
	checkBox(t, v, "a", 0, 0, 100, 10)
	checkBox(t, v, "b", 100, 0, 100, 10)
	// min-width clamps one item; the other absorbs the rest
	v, _ = layoutJS(t, `{"t":"div","k":1,"p":{"id":"row"},"c":[{"t":"div","k":2,"p":{"id":"a"}},{"t":"div","k":3,"p":{"id":"c"}}]}`, 1000, 1000)
	checkBox(t, v, "c", 80, 0, 120, 10)
	checkBox(t, v, "a", 0, 0, 80, 10)
}

func TestFlexWrapGapJustify(t *testing.T) {
	withStyles(t, func(n *Node, s *css.Style) {
		switch {
		case n.ID == "row":
			s.Display = css.DisplayFlex
			s.FlexWrap = css.Wrap
			s.Width = px(200)
			s.ColGap, s.RowGap = px(10), px(5)
			s.JustifyContent = css.AlignCenter
		case strings.HasPrefix(n.ID, "i"):
			s.Width, s.Height = px(80), px(20)
		}
	})
	v, d := layoutJS(t, `{"t":"div","k":1,"p":{"id":"row"},"c":[{"t":"div","k":2,"p":{"id":"i1"}},{"t":"div","k":3,"p":{"id":"i2"}},{"t":"div","k":4,"p":{"id":"i3"}}]}`, 1000, 1000)
	// line 1: 80+10+80 = 170, centered in 200 → starts at 15
	checkBox(t, v, "i1", 15, 0, 80, 20)
	checkBox(t, v, "i2", 105, 0, 80, 20)
	checkBox(t, v, "i3", 60, 25, 80, 20)
	if d.Size.Y != 45 {
		t.Errorf("height %d, want 45", d.Size.Y)
	}
}

func TestFlexAlignAndColumn(t *testing.T) {
	withStyles(t, func(n *Node, s *css.Style) {
		switch n.ID {
		case "row":
			s.Display = css.DisplayFlex
			s.Width, s.Height = px(100), px(60)
			s.AlignItems = css.AlignCenter
		case "a":
			s.Width, s.Height = px(20), px(20)
		case "col":
			s.Display = css.DisplayFlex
			s.FlexDirection = css.Column
			s.Width, s.Height = px(50), px(100)
		case "x":
			s.FlexGrow = 1
		case "y":
			s.Height = px(30)
		case "z":
			s.Width = px(10)
			s.Height = px(10)
			s.AlignSelf = css.AlignEnd
		}
	})
	v, _ := layoutJS(t, `{"t":"div","k":1,"p":{"id":"row"},"c":[{"t":"div","k":2,"p":{"id":"a"}}]}`, 1000, 1000)
	checkBox(t, v, "a", 0, 20, 20, 20)
	v, _ = layoutJS(t, `{"t":"div","k":1,"p":{"id":"col"},"c":[{"t":"div","k":2,"p":{"id":"x"}},{"t":"div","k":3,"p":{"id":"y"}},{"t":"div","k":4,"p":{"id":"z"}}]}`, 1000, 1000)
	checkBox(t, v, "x", 0, 0, 50, 60) // stretched across, grows into the free 60
	checkBox(t, v, "y", 0, 60, 50, 30)
	checkBox(t, v, "z", 40, 90, 10, 10)
}

func TestAbsolutePositioning(t *testing.T) {
	withStyles(t, func(n *Node, s *css.Style) {
		switch n.ID {
		case "box":
			s.Position = css.PosRelative
			s.Width, s.Height = px(200), px(100)
			s.Padding = [4]css.Length{px(5), px(5), px(5), px(5)}
		case "br":
			s.Position = css.PosAbsolute
			s.Right, s.Bottom = px(10), px(10)
			s.Width, s.Height = px(20), px(20)
		case "fill":
			s.Position = css.PosAbsolute
			s.Left, s.Right, s.Top, s.Bottom = px(0), px(0), px(0), px(0)
		case "rel":
			s.Position = css.PosRelative
			s.Left, s.Top = px(3), px(4)
			s.Height = px(10)
		}
	})
	v, _ := layoutJS(t, `{"t":"div","k":1,"p":{"id":"box"},"c":[{"t":"div","k":5,"p":{"id":"rel"}},{"t":"div","k":2,"p":{"id":"br"}},{"t":"div","k":3,"p":{"id":"fill"}}]}`, 1000, 1000)
	// box is 210x110 (content-box sizing + padding)
	checkBox(t, v, "br", 180, 80, 20, 20)
	checkBox(t, v, "fill", 0, 0, 210, 110)
	checkBox(t, v, "rel", 8, 9, 200, 10)
	if b := byID(v, "br"); !b.layered {
		t.Error("absolute box not layered")
	}
}

func TestMarginsCollapseAndAuto(t *testing.T) {
	withStyles(t, func(n *Node, s *css.Style) {
		switch n.ID {
		case "a":
			s.Height = px(10)
			s.Margin[2] = px(20)
		case "b":
			s.Height = px(10)
			s.Margin[0] = px(30)
		case "c":
			s.Width, s.Height = px(100), px(10)
			s.Margin[1], s.Margin[3] = css.Auto, css.Auto
		case "outer":
			s.Width = px(300)
		}
	})
	v, d := layoutJS(t, `{"t":"div","k":1,"p":{"id":"outer"},"c":[{"t":"div","k":2,"p":{"id":"a"}},{"t":"div","k":3,"p":{"id":"b"}},{"t":"div","k":4,"p":{"id":"c"}}]}`, 1000, 1000)
	checkBox(t, v, "a", 0, 0, 300, 10)
	checkBox(t, v, "b", 0, 40, 300, 10) // max(20, 30), not 50
	checkBox(t, v, "c", 100, 50, 100, 10)
	if d.Size.Y != 60 {
		t.Errorf("height %d, want 60", d.Size.Y)
	}
}

func TestInlineWrap(t *testing.T) {
	withStyles(t, func(n *Node, s *css.Style) {
		if n.ID == "p" {
			s.Width = px(70)
		}
	})
	v, _ := layoutJS(t, `{"t":"div","k":1,"p":{"id":"p"},"c":[{"t":"#","v":"alpha beta gamma delta epsilon"}]}`, 1000, 1000)
	b := byID(v, "p")
	if len(b.runs) != 1 {
		t.Fatalf("%d runs", len(b.runs))
	}
	f := &b.runs[0].f
	if len(f.lines) < 3 {
		t.Errorf("%d lines, want ≥3", len(f.lines))
	}
	for _, ln := range f.lines {
		if ln.w > 70.5 {
			t.Errorf("line width %.1f > 70", ln.w)
		}
	}
	lh := float32(15 * 1.2)
	if !near(b.h, lh*float32(len(f.lines))) {
		t.Errorf("height %.1f, want %d lines of %.1f", b.h, len(f.lines), lh)
	}
	// mixed runs across inline elements stay on shared lines
	v, _ = layoutJS(t, `{"t":"div","k":1,"p":{"id":"p"},"c":[{"t":"#","v":"ab "},{"t":"b","k":2,"c":["cd"]},{"t":"#","v":" ef"}]}`, 1000, 1000)
	if n := len(byID(v, "p").runs[0].f.lines); n != 1 {
		t.Errorf("mixed inline: %d lines, want 1", n)
	}
}

func TestEllipsisAndClamp(t *testing.T) {
	withStyles(t, func(n *Node, s *css.Style) {
		switch n.ID {
		case "e":
			s.Width = px(60)
			s.WhiteSpace = css.WSNoWrap
			s.OverflowX, s.OverflowY = css.OverflowHidden, css.OverflowHidden
			s.TextOverflowEllipsis = true
		case "c":
			s.Width = px(60)
			s.LineClamp = 2
			s.OverflowX, s.OverflowY = css.OverflowHidden, css.OverflowHidden
		}
	})
	text := "a long sender name that overflows"
	v, _ := layoutJS(t, `{"t":"div","k":1,"p":{"id":"e"},"c":["`+text+`"]}`, 1000, 1000)
	f := &byID(v, "e").runs[0].f
	if len(f.lines) != 1 {
		t.Fatalf("%d lines", len(f.lines))
	}
	ell := false
	for _, fr := range f.frags {
		if fr.ell {
			ell = true
			if fr.x+fr.w > 60.5 {
				t.Errorf("ellipsis ends at %.1f > 60", fr.x+fr.w)
			}
		}
		if !fr.hidden && !fr.ell && fr.x+fr.w > 60.5 && f.items[fr.it].kind == iWord {
			t.Errorf("visible word ends at %.1f", fr.x+fr.w)
		}
	}
	if !ell {
		t.Error("no ellipsis")
	}
	v, _ = layoutJS(t, `{"t":"div","k":1,"p":{"id":"c"},"c":["`+text+` and keeps going for a while"]}`, 1000, 1000)
	b := byID(v, "c")
	if n := len(b.runs[0].f.lines); n != 2 {
		t.Errorf("clamped to %d lines", n)
	}
	if !near(b.h, 2*15*1.2) {
		t.Errorf("clamped height %.1f", b.h)
	}
}

func TestNativeAndHost(t *testing.T) {
	RegisterHost(func(gtx layout.Context, h *Host) layout.Dimensions {
		return layout.Dimensions{Size: image.Pt(30, 12)}
	}, "TestBadge")
	withStyles(t, func(n *Node, s *css.Style) {
		if n.ID == "row" {
			s.Display = css.DisplayInlineFlex // inline-level root: shrink-to-fit
			s.ColGap = px(4)
		}
	})
	windowSize = image.Pt(1000, 800)
	v := NewView(1)
	calls := 0
	native := func(gtx layout.Context, p Props) layout.Dimensions {
		calls++
		if p.StrOr("name", "") != "Ann" {
			t.Errorf("props %v", p)
		}
		return layout.Dimensions{Size: image.Pt(50, 20), Baseline: 5}
	}
	nodes := mustTree(t, `{"t":"div","k":1,"p":{"id":"row"},"c":[{"t":"@","n":"MessageSender","k":2,"p":{"name":"Ann"}},{"t":"@","n":"TestBadge","k":3}]}`)
	gtx := testGtx(500, 500)
	d := v.Layout(gtx, nodes, "MessageSender", Props{"name": "Ann"}, native)
	if d.Size.X != 84 || d.Size.Y != 20 {
		t.Errorf("dims %v, want 84x20", d.Size)
	}
	if v.fr.native != nil || v.fr.orig != nil {
		t.Error("frame retained native/orig")
	}
	// steady state: the native is drawn once per frame
	calls = 0
	gtx.Ops.Reset()
	v.Layout(gtx, nodes, "MessageSender", Props{"name": "Ann"}, native)
	if calls != 1 {
		t.Errorf("steady frame: native called %d times, want 1", calls)
	}
}

func TestEventTargeting(t *testing.T) {
	withStyles(t, func(n *Node, s *css.Style) {
		switch n.ID {
		case "outer":
			s.Width, s.Height = px(200), px(100)
		case "btn":
			s.Display = css.DisplayBlock
			s.Width, s.Height = px(50), px(30)
			s.Margin[0], s.Margin[3] = px(10), px(20)
		}
	})
	type ev struct {
		k   int64
		typ string
		raw map[string]any
	}
	var got []ev
	testDispatch = func(k int64, b []byte) {
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Errorf("bad event json %s: %v", b, err)
		}
		got = append(got, ev{k, m["type"].(string), m})
	}
	defer func() { testDispatch = nil }()
	windowSize = image.Pt(1000, 800)
	nodes := mustTree(t, `{"t":"div","k":1,"p":{"id":"outer"},"on":{"click":7},"c":[{"t":"button","k":2,"p":{"id":"btn"},"c":["Go"]}]}`)
	v := NewView(1)
	var r input.Router
	frame := func() {
		gtx := testGtx(400, 400)
		gtx.Source = r.Source()
		v.Layout(gtx, nodes, "", nil, nil)
		r.Frame(gtx.Ops)
	}
	frame()
	frame()
	p := f32.Pt(30, 20) // inside the button (20..70, 10..40)
	r.Queue(
		pointer.Event{Kind: pointer.Move, Position: p, Source: pointer.Mouse, Time: 10 * time.Millisecond},
		pointer.Event{Kind: pointer.Press, Position: p, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Time: 20 * time.Millisecond},
		pointer.Event{Kind: pointer.Release, Position: p, Source: pointer.Mouse, Time: 30 * time.Millisecond},
	)
	frame()
	frame() // the press focused the button; Gio delivers the rest a frame later
	var types []string
	clickK := int64(-1)
	for _, e := range got {
		types = append(types, e.typ)
		if e.typ == "click" {
			clickK = e.k
			rects, _ := e.raw["rects"].(map[string]any)
			if _, ok := rects["2"]; !ok {
				t.Errorf("click rects lack the target: %v", rects)
			}
			if _, ok := rects["1"]; !ok {
				t.Errorf("click rects lack the ancestor: %v", rects)
			}
		}
	}
	if clickK != 2 {
		t.Fatalf("click target %d, want 2; events %v", clickK, types)
	}
	want := []string{"mouseover", "mouseenter", "mousedown", "mouseup", "click"}
	for _, w := range want {
		found := false
		for _, ty := range types {
			if ty == w {
				found = true
			}
		}
		if !found {
			t.Errorf("missing %s in %v", w, types)
		}
	}
	// hover state reached the box
	if b := byID(v, "btn"); b.state&css.StateHover == 0 {
		t.Error("button not :hover")
	}
	// leaving the button sends mouseleave to it
	got = nil
	q := f32.Pt(150, 80)
	r.Queue(pointer.Event{Kind: pointer.Move, Position: q, Source: pointer.Mouse, Time: 40 * time.Millisecond})
	frame()
	left := false
	for _, e := range got {
		if e.typ == "mouseleave" && e.k == 2 {
			left = true
		}
		if e.typ == "mouseleave" && e.k == 1 {
			t.Error("mouseleave on the still-hovered parent")
		}
	}
	if !left {
		t.Errorf("no mouseleave on the button: %v", got)
	}
}

func TestControlledInput(t *testing.T) {
	windowSize = image.Pt(1000, 800)
	v := NewView(1)
	tree := func(val string) []*Node {
		return mustTree(t, `{"t":"input","k":5,"p":{"value":"`+val+`"}}`)
	}
	gtx := testGtx(300, 100)
	v.Layout(gtx, tree("a"), "", nil, nil)
	b := v.byK[5]
	ed := v.editor(b)
	if ed.Text() != "a" {
		t.Fatalf("initial %q", ed.Text())
	}
	ns := v.state(b)
	// the user typed "b" then "c"; JS is behind
	ed.SetText("abc")
	ns.lastText = "abc"
	ns.sent = []string{"ab", "abc"}
	// echo of the first keystroke: keep the user's text
	v.Layout(gtx, tree("ab"), "", nil, nil)
	if ed.Text() != "abc" {
		t.Errorf("stale echo overwrote: %q", ed.Text())
	}
	// echo of the latest: in sync
	v.Layout(gtx, tree("abc"), "", nil, nil)
	if ed.Text() != "abc" || len(ns.sent) != 0 {
		t.Errorf("sync: %q sent=%v", ed.Text(), ns.sent)
	}
	// JS sets its own value (e.g. clears after submit): adopt it
	nodes := tree("x")
	v.Layout(gtx, nodes, "", nil, nil)
	if ed.Text() != "x" {
		t.Errorf("controlled value not adopted: %q", ed.Text())
	}
	// same tree again: the user's edits stand
	ed.SetText("xy")
	v.Layout(gtx, nodes, "", nil, nil)
	v.Layout(gtx, nodes, "", nil, nil)
	if ed.Text() != "xy" {
		t.Errorf("unchanged tree reverted edits: %q", ed.Text())
	}
}

func TestSlackRoute(t *testing.T) {
	cases := map[string]string{
		"https://hackclub.slack.com/archives/C0B4DEFKFCJ":                   "c/C0B4DEFKFCJ",
		"https://hackclub.slack.com/archives/C0B4DEFKFCJ/p1730000000123456": "c/C0B4DEFKFCJ/m/1730000000.123456",
		"slack://channel?team=T1&id=C9":                                     "c/C9",
		"https://example.com/archives/C1":                                   "",
	}
	for in, want := range cases {
		if got := slackRoute(in); got != want {
			t.Errorf("%s → %q, want %q", in, got, want)
		}
	}
}

// senderTree is a ~15-node patched sender: name, badge, svg icon, the
// original component and a timestamp.
const senderTree = `{"t":"span","k":1,"p":{"id":"root"},"c":[
 {"t":"span","k":2,"p":{"id":"name"},"c":["Karim Example"]},
 {"t":"span","k":3,"p":{"id":"badge"},"c":[
   {"t":"svg","k":4,"p":{"viewBox":"0 0 24 24","width":12,"height":12},"c":[{"t":"path","k":5,"p":{"d":"M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-2 15l-5-5 1.41-1.41L10 14.17l7.59-7.59L19 8l-9 9z"}}]},
   {"t":"span","k":6,"c":["he/him"]}]},
 {"t":"@","n":"MessageSender","k":7,"p":{"userId":"U1"}},
 {"t":"span","k":8,"p":{"id":"ts"},"c":[{"t":"a","k":9,"p":{"href":"https://x.slack.com/archives/C1/p1"},"c":["12:30 PM"]}]},
 {"t":"span","k":10,"p":{"id":"pill"},"c":["APP"]}
]}`

func senderStyles(t testing.TB) {
	withStyles(t, func(n *Node, s *css.Style) {
		switch n.ID {
		case "root":
			s.Display = css.DisplayInlineFlex
			s.AlignItems = css.AlignBaseline
			s.ColGap = px(4)
		case "name":
			s.FontWeight = 900
		case "badge":
			s.Display = css.DisplayInlineFlex
			s.AlignItems = css.AlignCenter
			s.Background.Color = color.NRGBA{R: 0xe8, G: 0xf5, B: 0xfa, A: 0xff}
			s.Radius = [4]css.Length{px(4), px(4), px(4), px(4)}
			s.Padding = [4]css.Length{px(0), px(4), px(0), px(4)}
			s.FontSize = 12
		case "ts":
			s.FontSize = 12
			s.Color = color.NRGBA{R: 0x61, G: 0x60, B: 0x61, A: 0xff}
		case "pill":
			s.FontSize = 10
			s.FontWeight = 700
			s.Background.Color = color.NRGBA{A: 0x14}
			s.Padding = [4]css.Length{px(1), px(3), px(1), px(3)}
		}
	})
}

func senderNative(gtx layout.Context, p Props) layout.Dimensions {
	return layout.Dimensions{Size: image.Pt(60, 22), Baseline: 6}
}

func TestSteadyFrameAllocs(t *testing.T) {
	senderStyles(t)
	windowSize = image.Pt(1000, 800)
	v := NewView(1)
	nodes := mustTree(t, senderTree)
	gtx := testGtx(600, 200)
	for i := 0; i < 3; i++ {
		gtx.Ops.Reset()
		v.Layout(gtx, nodes, "MessageSender", nil, senderNative)
	}
	n := testing.AllocsPerRun(200, func() {
		gtx.Ops.Reset()
		v.Layout(gtx, nodes, "MessageSender", nil, senderNative)
	})
	t.Logf("steady frame: %.1f allocs", n)
	if n > 8 {
		t.Errorf("steady frame allocates %.1f times", n)
	}
}

func BenchmarkSenderSteady(b *testing.B) {
	senderStyles(b)
	windowSize = image.Pt(1000, 800)
	v := NewView(1)
	nodes := mustTree(b, senderTree)
	gtx := testGtx(600, 200)
	v.Layout(gtx, nodes, "MessageSender", nil, senderNative)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gtx.Ops.Reset()
		v.Layout(gtx, nodes, "MessageSender", nil, senderNative)
	}
}

func BenchmarkSenderRelayout(b *testing.B) {
	senderStyles(b)
	windowSize = image.Pt(1000, 800)
	v := NewView(1)
	nodes := mustTree(b, senderTree)
	gtx := testGtx(600, 200)
	v.Layout(gtx, nodes, "MessageSender", nil, senderNative)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gtx.Ops.Reset()
		v.dirtyGen++
		v.Layout(gtx, nodes, "MessageSender", nil, senderNative)
	}
}

func BenchmarkSenderNewTree(b *testing.B) {
	senderStyles(b)
	windowSize = image.Pt(1000, 800)
	v := NewView(1)
	trees := [2][]*Node{mustTree(b, senderTree), mustTree(b, senderTree)}
	gtx := testGtx(600, 200)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gtx.Ops.Reset()
		v.Layout(gtx, trees[i&1], "MessageSender", nil, senderNative)
	}
}

func BenchmarkParseSenderTree(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := ParseTree([]byte(senderTree)); err != nil {
			b.Fatal(err)
		}
	}
}

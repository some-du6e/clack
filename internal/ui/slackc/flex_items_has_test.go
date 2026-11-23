package slackc

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/slackc/css"
)

// OnlineCount's count beside the channel header's avatar stack. In Slack's
// DOM AutoClog's div.p-autoclog__hook (display:contents in Slack's sheet)
// holds the stack's button and the count's span: Slack's Tooltip clones
// its child and adds no element of its own. The plugin's
// .p-autoclog__hook:has(> .slick-online-count) makes the hook a flex row,
// centred, with 6px between the stack and the count. In Clack the hook is
// a FlexItems native and the patched tree's top nodes are its children:
// its :has() sees the span through the Tooltip (RegisterHostNoElement), so
// the stack and the count lay out as its flex items. The Tooltip's box is
// as big as the span and no rule reaches it (a className on Slack's
// Tooltip styles its tip, not its trigger), and a child combinator from
// the hook reaches the span. Without the count the hook stays
// display:contents: the stack lays out alone, as before. The stack's own
// rules see the hook with the tree too (:has() from the native's
// cascade), a hover on the count reaches the hook's :has() (and a hover
// its rules don't see keeps its style and key), and a tree of text alone
// leaves the hook not :empty, as its text would in Slack's DOM.
func TestFlexItemsHasSeesTree(t *testing.T) {
	defer func() { stack = stack[:0] }()
	windowSize = image.Pt(1000, 800)
	RegisterHost(func(gtx layout.Context, h *Host) layout.Dimensions { return h.Children(gtx) }, "TestNoElementTip")
	RegisterHostNoElement("TestNoElementTip")
	SetStyle("test:online-count", `
      .p-autoclog__hook:has(> .slick-online-count) { display: flex; align-items: center; gap: 6px; }
      .slick-online-count { display: inline-flex; align-items: center; gap: 5px; padding-right: 4px; font-size: 13px; font-weight: 700; white-space: nowrap; }
      .slick-online-count::before { content: ''; width: 8px; height: 8px; border-radius: 50%; background: rgb(10, 200, 10); }
      .p-autoclog__hook > .slick-online-count { color: rgb(1, 2, 3); }
      .test-tip-class { padding: 20px; }
      .p-autoclog__hook:has(> .slick-online-count) .test-stack-inner { --hook-has-count: y; }
      .p-autoclog__hook:has(> .slick-online-count:hover) { --count-hovered: y; }
      .p-autoclog__hook:empty { --hook-empty: y; }`)
	defer RemoveStyle("test:online-count")
	hook := Class("p-autoclog__hook").FlexItems()
	inner := Class("test-stack-inner")
	var innerVar string // the stack's own element's --hook-has-count
	stackNative := func(gtx layout.Context, p Props) layout.Dimensions {
		s := inner.Enter(gtx, 0, 0)
		defer s.Leave()
		innerVar = ""
		if st := s.Style(); st != nil {
			innerVar = st.Vars["--hook-has-count"]
		}
		return layout.Dimensions{Size: image.Pt(60, 28)}
	}
	layNodes := func(v *View, nodes []*Node) layout.Dimensions {
		g := testGtx(400, 28)
		h := hook.Enter(g, 0, 0)
		defer h.Leave()
		return v.Layout(g, nodes, "Stack", nil, stackNative)
	}
	lay := func(tree string) (*View, layout.Dimensions) {
		t.Helper()
		v := NewView(9001)
		return v, layNodes(v, mustTree(t, tree))
	}

	v, d := lay(`[{"t":"@","n":"Stack","k":1},` +
		`{"t":"@","n":"TestNoElementTip","k":2,"p":{"id":"tip","className":"test-tip-class","tip":"4 online, 2 away"},"c":[` +
		`{"t":"span","k":3,"p":{"id":"count","className":"slick-online-count"},"c":[{"t":"#","v":"4"}]}]}]`)
	tip, count := byID(v, "tip"), byID(v, "count")
	if tip == nil || count == nil {
		t.Fatal("no Tooltip or count box")
	}
	if got := count.Parent(); got != v.topEl {
		t.Errorf("the count's parent as selectors see it is %v, not the hook", got)
	}
	if c := count.st.Color; c != (color.NRGBA{R: 1, G: 2, B: 3, A: 0xff}) {
		t.Errorf(".p-autoclog__hook > .slick-online-count didn't reach the span through the Tooltip: color %v", c)
	}
	if v.topStyle == nil || !isFlex(v.topStyle.Display) {
		t.Fatalf("the hook isn't a flex row with the count in it (display %v)", v.topStyle)
	}
	stackBox := v.top[0]
	checkBox(t, v, "tip", 60+6, -1, -1, -1)
	if !near(tip.x, stackBox.x+stackBox.w+6) {
		t.Errorf("the count starts at x=%.1f, want 6px after the stack's right edge %.1f", tip.x, stackBox.x+stackBox.w)
	}
	if want := (28 - tip.h) / 2; !near(tip.y, want) {
		t.Errorf("the count is at y=%.1f, not centred beside the 28px stack (want %.1f)", tip.y, want)
	}
	if tip.pad != [4]float32{} || !near(tip.w, count.w) || !near(tip.h, count.h) {
		t.Errorf("the Tooltip's box is %.1fx%.1f (padding %v), not as big as the count's %.1fx%.1f", tip.w, tip.h, tip.pad, count.w, count.h)
	}
	if b := count.before; b == nil || !near(b.w, 8) || !near(b.h, 8) {
		t.Errorf("the count's ::before dot: %+v, want an 8x8 box", b)
	}
	if want := 60 + 6 + int(tip.w+0.999); d.Size.X < want-1 || d.Size.X > want+1 {
		t.Errorf("the row is %dpx wide, want the stack, the gap and the count (%d)", d.Size.X, want)
	}
	if innerVar != "y" {
		t.Error(".p-autoclog__hook:has(> .slick-online-count) .test-stack-inner didn't reach the stack's own element")
	}

	// hovering the stack: nothing the hook's rules see; hovering the count:
	// its :has(> .slick-online-count:hover)
	key := v.topKey
	if v.topStyle.Vars["--count-hovered"] != "" {
		t.Error("the hook matched the count's :hover before a hover")
	}
	v.setState(stackBox, css.StateHover, true)
	layNodes(v, v.nodes)
	if v.topKey != key || v.topStyle.Vars["--count-hovered"] != "" {
		t.Errorf("hovering the stack: the hook's key moved %v, --count-hovered %q; want neither", v.topKey != key, v.topStyle.Vars["--count-hovered"])
	}
	v.setState(count, css.StateHover, true)
	layNodes(v, v.nodes)
	if v.topStyle.Vars["--count-hovered"] != "y" {
		t.Error("hovering the count: the hook's :has(> .slick-online-count:hover) didn't follow")
	}

	// no count: the hook is display:contents and the stack lays out alone
	v2, d2 := lay(`[{"t":"@","n":"Stack","k":1}]`)
	if v2.topStyle != nil && isFlex(v2.topStyle.Display) {
		t.Errorf("the hook became a flex row without the count (display %v)", v2.topStyle.Display)
	}
	if d2.Size.X != 60 || d2.Size.Y != 28 {
		t.Errorf("the stack alone lays out %v, want 60x28", d2.Size)
	}
	if innerVar != "" {
		t.Error("the stack without the count matched .p-autoclog__hook:has(> .slick-online-count): another tree's hook")
	}

	// text alone in the hook: not :empty
	v3, _ := lay(`[{"t":"#","v":"3 online"}]`)
	if v3.topStyle == nil || v3.topStyle.Vars["--hook-empty"] != "" {
		t.Errorf("the hook holding text matched :empty (%v)", v3.topStyle)
	}
}

package slackc

import (
	"image"
	"testing"

	"gioui.org/layout"
)

var testRowClass = Class("c-message_kit__background c-message_kit__message")

// A row-level native a plugin wraps in a div (MessageLogger's
// <div className=…><Original/></div> around MessageWrapper), or returns
// bare (BetterBlock), is as tall as the native: its root is a <div>, as in
// Slack's DOM, so it lays out as a block and no line box (no strut descent
// under the row) is added. Nothing tells slackc that but the native itself.
func TestWrappedBlockNativeAddsNoLine(t *testing.T) {
	SetStyle("test:wrap", `.slick-ml-deleted { color: red }`) // CSS active, as with any plugin sheet
	defer RemoveStyle("test:wrap")
	native := func(gtx layout.Context, _ Props) layout.Dimensions {
		st := testRowClass.Enter(gtx, 0, 0)
		defer st.Leave()
		return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 104)}
	}
	for i, tc := range []struct{ name, comp, tree string }{
		{"div", "TestRowA", `{"t":"div","k":1,"c":[{"t":"@","n":"TestRowA","k":2}]}`},
		{"div.class", "TestRowB", `{"t":"div","k":1,"p":{"className":"slick-ml-deleted"},"c":[{"t":"@","n":"TestRowB","k":2}]}`},
		{"bare", "TestRowC", `{"t":"@","n":"TestRowC","k":2}`},
		// two plugins patching the same row: one wraps the other's wrapper
		{"div>div", "TestRowD", `{"t":"div","k":1,"c":[{"t":"div","k":3,"c":[{"t":"@","n":"TestRowD","k":2}]}]}`},
	} {
		v := NewView(uint64(900 + i))
		var d layout.Dimensions
		for f := 0; f < 3; f++ {
			d = v.Layout(testGtx(600, 400), mustTree(t, tc.tree), tc.comp, nil, native)
		}
		if d.Size.Y != 104 {
			t.Errorf("%s: the view is %d tall around a 104px row", tc.name, d.Size.Y)
		}
	}
}

// An inline native (a span: the sender, a timestamp) stays inline.
func TestInlineNativeStaysInline(t *testing.T) {
	SetStyle("test:wrap", `.slick-ml-deleted { color: red }`)
	defer RemoveStyle("test:wrap")
	span := Class("c-message__sender").Tag("span")
	native := func(gtx layout.Context, _ Props) layout.Dimensions {
		st := span.Enter(gtx, 0, 0)
		defer st.Leave()
		return layout.Dimensions{Size: image.Pt(40, 10), Baseline: 2}
	}
	v := NewView(960)
	tree := `{"t":"div","k":1,"c":[{"t":"@","n":"TestSender","k":2},{"t":"@","n":"TestSender","k":3}]}`
	var d layout.Dimensions
	for f := 0; f < 3; f++ {
		d = v.Layout(testGtx(600, 400), mustTree(t, tree), "TestSender", nil, native)
	}
	if b := v.byK[3]; b == nil || b.rect.y0 != v.byK[2].rect.y0 || b.rect.x0 < 40 {
		t.Errorf("two inline natives aren't side by side on one line (view %v)", d.Size)
	}
}

// A native that draws nothing (BaseVisuallyHiddenMessageText, Slack's
// offscreen span) under a bare <Original/> (BetterBlock) takes no space.
func TestEmptyNativeTakesNoSpace(t *testing.T) {
	SetStyle("test:wrap", `.slick-ml-deleted { color: red }`)
	defer RemoveStyle("test:wrap")
	empty := func(gtx layout.Context, _ Props) layout.Dimensions { return layout.Dimensions{} }
	v := NewView(950)
	var d layout.Dimensions
	for i := 0; i < 3; i++ {
		d = v.Layout(testGtx(600, 400), mustTree(t, `{"t":"@","n":"TestHiddenText","k":2}`), "TestHiddenText", nil, empty)
	}
	if d.Size.Y != 0 {
		t.Errorf("the view is %d tall around an empty native", d.Size.Y)
	}
}

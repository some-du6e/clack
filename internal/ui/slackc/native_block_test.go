package slackc

import (
	"image"
	"testing"

	"gioui.org/layout"
)

// A row-level native a plugin wraps in a div (MessageLogger's
// <div className=…><Original/></div> around MessageWrapper) is as tall as
// the native: Slack's original renders a block div there, so no line box
// (and no strut descent under the row) is added.
func TestWrappedBlockNativeAddsNoLine(t *testing.T) {
	SetBlockComponents("MessageWrapper")                      // messages registers its row parts
	SetStyle("test:wrap", `.slick-ml-deleted { color: red }`) // CSS active, as with any plugin sheet
	defer RemoveStyle("test:wrap")
	native := func(gtx layout.Context, _ Props) layout.Dimensions {
		return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 104)}
	}
	for _, tc := range []struct{ name, tree string }{
		{"div", `{"t":"div","k":1,"c":[{"t":"@","n":"MessageWrapper","k":2}]}`},
		{"div.class", `{"t":"div","k":1,"p":{"className":"slick-ml-deleted"},"c":[{"t":"@","n":"MessageWrapper","k":2}]}`},
		{"bare", `{"t":"@","n":"MessageWrapper","k":2}`},
	} {
		v := NewView(uint64(900 + len(tc.name)))
		var d layout.Dimensions
		for i := 0; i < 2; i++ {
			d = v.Layout(testGtx(600, 400), mustTree(t, tc.tree), "MessageWrapper", nil, native)
		}
		if d.Size.Y != 104 {
			t.Errorf("%s: the view is %d tall around a 104px row", tc.name, d.Size.Y)
		}
	}
}

package blockkit

import (
	"image"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// nestedElements is a Surface as richview's Elements host for the blocks
// richview draws in a modal or App Home (Surface.block's default case): a
// container's or a callout's children, a fallback's payload. Their
// actions runs and accessories are the Surface's own widgets, laid out as
// its top-level ones are, so a click sends block_actions with the child
// block's id. Slack renders child_blocks through the surface's
// renderables with its onAction (Container yWQF provides the blockKit
// context; Callout's createRenderBlocks a+Oj inherits it).
type nestedElements struct {
	s   *Surface
	ops op.Ops
}

func (n *nestedElements) scratch(m unit.Metric, maxW int) layout.Context {
	n.ops.Reset()
	return layout.Context{Ops: &n.ops, Metric: m, Now: time.Now(), Constraints: layout.Constraints{Max: image.Pt(maxW, 1<<20)}}
}

// of is the block (and element) a run of buttons came from.
func (n *nestedElements) of(bs []richtext.Button) (*Block, *Element) {
	if len(bs) == 0 {
		return nil, nil
	}
	r, ok := find(n.s.blocks, bs[0].BlockID, bs[0].ActionID)
	if !ok {
		return nil, nil
	}
	return r.block, r.el
}

func (n *nestedElements) ElementsSize(bs []richtext.Button, maxW int, accessory bool, m unit.Metric) (image.Point, bool) {
	b, e := n.of(bs)
	if b == nil || iconButtonHidden(e, n.s.selfID()) {
		return image.Point{}, false
	}
	gtx := n.scratch(m, maxW)
	if b.Type == "actions" {
		return image.Pt(maxW, n.s.flowRun(gtx, b, maxW, false)), false
	}
	w := n.s.widget(b, e)
	if stacks(e) {
		return image.Pt(maxW, w.size(gtx, maxW, true).Y), true
	}
	return w.size(gtx, maxW/2, true), false
}

func (n *nestedElements) LayoutElements(gtx layout.Context, bs []richtext.Button, accessory bool) layout.Dimensions {
	sz := gtx.Constraints.Max
	b, e := n.of(bs)
	switch {
	case b == nil || iconButtonHidden(e, n.s.selfID()):
	case b.Type == "actions":
		n.s.flowRun(gtx, b, sz.X, true)
	default:
		n.s.widget(b, e).layout(fixed(gtx, sz), sz)
	}
	return layout.Dimensions{Size: sz}
}

func (n *nestedElements) ElementsVersion() uint32 { return n.s.gen }

var _ richview.Elements = (*nestedElements)(nil)

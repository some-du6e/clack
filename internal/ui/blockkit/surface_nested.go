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
	switch {
	case b.Type == "actions":
		h := n.s.flowRun(gtx, b, maxW, false)
		if n.s.blkErr[b.BlockID] != "" { // its error line (Surface.block's)
			h += dp(gtx, 24)
		}
		return image.Pt(maxW, h), false
	case !accessory:
		return n.row(gtx, bs, false), false
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
		// the Actions renderable's error line under a failed action
		// (/jJF renderBlock: isError, actionError), as at the top level
		h := n.s.flowRun(gtx, b, sz.X, true)
		if msg := n.s.blkErr[b.BlockID]; msg != "" {
			th := n.s.h.Theme()
			at(gtx, image.Pt(0, h+dp(gtx, 4)), func(gtx C) D { return errorLine(gtx, th, colorsOf(th), sz.X, msg) })
		}
	case !accessory:
		n.row(gtx, bs, true)
	default:
		n.s.widget(b, e).layout(fixed(gtx, sz), sz)
	}
	return layout.Dimensions{Size: sz}
}

// row lays out a run of a card's buttons (not an actions block): each
// its own widget, 8px apart (.actionsLeft / .actionsRight column-gap).
func (n *nestedElements) row(gtx C, bs []richtext.Button, draw bool) image.Point {
	x, h := 0, 0
	for i := range bs {
		r, ok := find(n.s.blocks, bs[i].BlockID, bs[i].ActionID)
		if !ok {
			continue
		}
		w := n.s.widget(r.block, r.el)
		sz := w.size(gtx, gtx.Constraints.Max.X, false)
		if sz.X == 0 {
			continue
		}
		if x > 0 {
			x += dp(gtx, 8)
		}
		if draw {
			at(gtx, image.Pt(x, 0), func(gtx C) D { return w.layout(fixed(gtx, sz), sz) })
		}
		x += sz.X
		h = max(h, sz.Y)
	}
	return image.Pt(x, h)
}

func (n *nestedElements) ElementsVersion() uint32 { return n.s.gen }

var _ richview.Elements = (*nestedElements)(nil)

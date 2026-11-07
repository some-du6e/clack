package slackc

// STUB: replaced by the host-tree layout engine. Delete when it lands.

import (
	"gioui.org/layout"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc/css"
)

// View lays out one root's host trees and keeps per-node UI state.
type View struct {
	root    uint64
	overlay bool
}

type frame struct {
	name   string
	orig   Props
	native Native
}

// NewView returns the view for root.
func NewView(root uint64) *View { return &View{root: root} }

// Layout lays out nodes; "@" nodes named name call native.
func (v *View) Layout(gtx layout.Context, nodes []*Node, name string, orig Props, native Native) layout.Dimensions {
	for _, n := range nodes {
		if n.T == "@" && n.N == name && native != nil {
			return native(gtx, n.P)
		}
	}
	return layout.Dimensions{}
}

// Close releases the view's state.
func (v *View) Close() {}

func (v *View) layoutKids(gtx layout.Context, fr *frame, n *Node, st *css.Style) layout.Dimensions {
	return layout.Dimensions{}
}

func (v *View) layoutNodes(gtx layout.Context, fr *frame, nodes []*Node, parent *Node, st *css.Style) layout.Dimensions {
	return layout.Dimensions{}
}

func (v *View) layoutText(gtx layout.Context, fr *frame, s string, st *css.Style) layout.Dimensions {
	return layout.Dimensions{}
}

func (v *View) hostState(k int64, mk func() any) any { return mk() }

func wrapNative(gtx layout.Context, ns *nativeStyle, w layout.Widget) layout.Dimensions {
	return w(gtx)
}

func flushRects(b Bridge) {}

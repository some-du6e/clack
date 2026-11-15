package inspect

import "testing"

// Of two plugin trees under the pointer, the one with its own element
// there wins over one whose box there is a native's "@" box (the tree a
// native lays its children out in, inside the patched component's tree),
// then the smaller box, then the tree drawn later.
func TestTreeBoxOver(t *testing.T) {
	outer := treeBox{node: 9, native: true, area: 400 * 22} // <Original>: BaseBroadcastPreamble's "@" box
	span := treeBox{node: 5, area: 50 * 16}                 // the pronouns, in the children's tree
	if !span.over(outer) || outer.over(span) {
		t.Error("a tree's own element must win over a native's box in another tree")
	}
	big := treeBox{node: 7, area: 400 * 22}
	if !span.over(big) || big.over(span) {
		t.Error("the smaller box must win")
	}
	later := treeBox{node: 8, area: 50 * 16}
	if !later.over(span) || span.over(later) {
		t.Error("between equal boxes the tree drawn later must win")
	}
}
